# Code Audit — Visual Aggregator (`gator`)

Audit dilakukan terhadap semua source file non-test setelah sesi pengembangan selesai.
Status di bawah mencerminkan kondisi **setelah** partial fix (BUG-04 sudah di-fix).

---

## Ringkasan Temuan

| ID | Severity | Status | File | Judul |
|---|---|---|---|---|
| BUG-01 | LOW | **FIXED** | `gator.go` | `GetFieldValue` silently returns non-scalar |
| BUG-02 | HIGH | **FIXED** | `gator.go` | `explodeRecords` gagal untuk nested arrayPath |
| BUG-03 | LOW | **FIXED** | `compute.go` | `avg` cross-record adalah mean-of-means bukan weighted |
| BUG-04 | HIGH | **FIXED** | `gator.go` | `Store` tidak thread-safe |
| BUG-05 | MEDIUM | **FIXED** | `gator.go` | Explode mode: `count(parent_field)` terduplikasi N× |
| BUG-06 | LOW | **FIXED** | `compute.go` | Float equality di `ever_has_last_n` |
| BUG-07 | MEDIUM | **FIXED** | `schema.go` | `DetectSchema` hanya inspeksi `data[0]` |
| BUG-08 | LOW | **FIXED** | `main.go` | `loadFileDataset` tidak menerapkan XSD hints |
| BUG-09 | LOW | **FIXED** | `main.go` | `handleAggregate` tidak validate HTTP method |
| BUG-10 | HIGH | **FIXED** | `xsdStore` di `main.go` | `xsdStore` tidak thread-safe |
| QUIRK-11 | LOW | **FIXED** | `gator.go` | `explodeRecords` shallow copy — mitigated by BUG-02 (DeepCopyMap) |
| QUIRK-12 | MEDIUM | **FIXED** | `xsd.go` vs `xml.go` | XSD path vs schema path mismatch (root element prefix) |

---

## Detail Temuan

---

### BUG-01 — `GetFieldValue` silently returns non-scalar

**Severity:** LOW
**Status:** OPEN
**File:** `gator/schema.go:82-99`

#### Deskripsi

`GetFieldValue` mengembalikan `(value, true)` untuk **path apapun** yang berhasil di-traverse, termasuk ketika hasil traversalnya adalah `[]interface{}` (array) atau `map[string]interface{}` (object), bukan scalar.

```go
func GetFieldValue(obj map[string]interface{}, path string) (interface{}, bool) {
    // ...traversal...
    return current, true  // current bisa berupa array atau map
}
```

#### Impact

Jika user salah ketik field path di aggregasi — misalnya `"field": "credits"` alih-alih `"field": "credits.outstanding_balance"` — `GetFieldValue` mengembalikan seluruh array `credits`. `ComputeAggregation("sum", ...)` kemudian memanggil `ToFloat64([]interface{}{...})` yang return `(0, false)`, sehingga hasilnya SUM = 0. **Tidak ada error, hasilnya diam-diam salah.** User tidak mendapat petunjuk bahwa query-nya keliru.

Berlaku juga untuk GROUP BY: jika field salah resolve ke object/array, `buildGroupKey` memanggil `fmt.Sprintf("%v", array_of_maps)` yang menghasilkan string `[map[...] map[...]]`.

#### Reproduksi

```json
{
  "dataset": "employees",
  "aggregations": [
    { "field": "credits", "op": "sum", "alias": "broken" }
  ]
}
```
→ Hasil: `{ "broken": 0 }` tanpa error.

#### Saran Perbaikan

Tambah `ValidateAggregation` yang memeriksa bahwa field path dalam aggregation resolve ke scalar atau array primitif di schema. Return `400 Bad Request` dengan pesan yang jelas jika field resolve ke `array_object` atau `object`.

```go
func validateAggField(field string, schema []FieldInfo) error {
    for _, fi := range schema {
        if fi.Path == field {
            switch fi.Type {
            case "array_object", "object":
                return fmt.Errorf("field %q is an object/array — use a sub-field", field)
            }
            return nil
        }
    }
    return nil // unknown field → treated as parent-level, silently ignored
}
```

---

### BUG-02 — `explodeRecords` gagal untuk nested arrayPath

**Severity:** HIGH
**Status:** OPEN
**File:** `gator/gator.go:218-249`

#### Deskripsi

`explodeRecords` menggunakan `shallowCopyMapExcept(rec, arrayKey)` di mana `arrayKey` adalah **last segment** dari `arrayPath`. Untuk top-level array (mis. `credits`), ini benar: `arrayKey = "credits"`, dan `rec["credits"]` ada di root.

Namun untuk nested arrayPath (mis. `FasilitasList.Fasilitas`), `arrayKey = "Fasilitas"`, tapi `rec["Fasilitas"]` **tidak ada di root** — ia ada di `rec["FasilitasList"]["Fasilitas"]`. Akibatnya:

1. `shallowCopyMapExcept(rec, "Fasilitas")` **tidak menghapus apa-apa** karena key tidak ada di root.
2. `flat["Fasilitas"] = elemMap` menambahkan element di root level yang tidak akan ter-resolve oleh `GetFieldValue("FasilitasList.Fasilitas.Outstanding")`.
3. `FasilitasList` tetap ada intact di flat row dengan array penuh — tidak di-explode.

#### Reproduksi

```json
{
  "dataset": "ideb",
  "groupBy": ["Debtor.NIK", "FasilitasList.Fasilitas.JenisKredit"],
  "aggregations": [
    { "field": "FasilitasList.Fasilitas.Outstanding", "op": "sum", "alias": "os" }
  ]
}
```
Jika `FasilitasList.Fasilitas.JenisKredit` ada di GROUP BY → explode mode dipicu, tapi hasil akan salah karena path nested tidak di-handle.

#### Saran Perbaikan

`explodeRecords` harus melakukan deep copy record, navigate ke parent dari array, replace array dengan single element, bukan manipulasi shallow copy root:

```go
func explodeRecords(records []map[string]interface{}, arrayPath string) []map[string]interface{} {
    parts := strings.Split(arrayPath, ".")
    var result []map[string]interface{}
    for _, rec := range records {
        base := DeepCopyMap(rec)
        parentMap, key, found := NavigateToParent(base, parts)
        if !found {
            // zero out the array key in deep copy
            result = append(result, base)
            continue
        }
        arr, ok := parentMap[key].([]interface{})
        if !ok || len(arr) == 0 {
            parentMap[key] = nil
            result = append(result, base)
            continue
        }
        for _, elem := range arr {
            copy := DeepCopyMap(rec) // fresh copy per element
            pm, k, _ := NavigateToParent(copy, parts)
            if elemMap, ok := elem.(map[string]interface{}); ok {
                pm[k] = DeepCopyMap(elemMap)
            } else {
                pm[k] = elem
            }
            result = append(result, copy)
        }
    }
    return result
}
```

Catatan: Ini lebih expensive (full deep copy per element) tapi correct. Untuk dataset besar, bisa dioptimasi dengan copy-on-write.

---

### BUG-03 — `avg` cross-record adalah mean-of-means, bukan weighted average

**Severity:** LOW
**Status:** OPEN
**File:** `gator/compute.go:387-397`

#### Deskripsi

`CrossRecordOp("avg")` mengembalikan `"avg"`, sehingga engine menghitung rata-rata dari per-record averages. Ini adalah **mean of means**, yang memberikan bobot yang sama ke setiap parent record terlepas berapa banyak elemen array yang dimilikinya.

```go
default:
    return op  // avg → avg across per-record scalars
```

#### Contoh Konkret

```
Andi: 5 credits, avg outstanding = 50M
Budi: 10 credits, avg outstanding = 80M

Mean of means: (50M + 80M) / 2 = 65M
Correct weighted avg: (5×50M + 10×80M) / 15 = 70M
```

Perbedaan semakin besar ketika jumlah elemen sangat tidak seimbang antar record.

#### Impact

Kecil untuk GROUP BY yang sudah memisahkan record individu. Lebih signifikan untuk GROUP BY pada field parent yang mengagregasi banyak record heterogen.

#### Saran Perbaikan

Dua opsi:

**Opsi A (direkomendasikan):** Dokumentasikan sebagai known limitation di API docs. Sarankan user menghitung `SUM / COUNT` secara manual untuk weighted average.

**Opsi B:** Implementasikan `avg` cross-record menggunakan weighted sum:
- Simpan `(sum, count)` per-record alih-alih scalar average.
- Combine dengan `(total_sum, total_count)` lalu bagi.
- Breaking change pada semantik yang ada.

---

### BUG-04 — `Store` tidak thread-safe ✅ FIXED

**Severity:** HIGH
**Status:** **FIXED** (commit: added `sync.RWMutex` to `Store`)
**File:** `gator/gator.go:36-64`

#### Deskripsi (sebelum fix)

`Store.datasets` adalah plain `map[string][]interface{}` tanpa proteksi mutex. `http.ListenAndServe` melayani request secara concurrent. Concurrent read (GET /api/aggregate) dan write (POST /api/upload) ke map yang sama adalah **data race** di Go — dapat menyebabkan crash atau corruption.

#### Fix yang Diterapkan

```go
type Store struct {
    mu       sync.RWMutex
    datasets map[string][]interface{}
}

func (s *Store) Register(name string, data []interface{}) {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.datasets[name] = data
}

func (s *Store) Get(name string) ([]interface{}, bool) {
    s.mu.RLock()
    defer s.mu.RUnlock()
    d, ok := s.datasets[name]
    return d, ok
}
```

#### Sisa yang Masih OPEN

`xsdStore` di `main.go` (BUG-10) menggunakan plain map dan belum dilindungi mutex — lihat BUG-10.

---

### BUG-05 — Explode mode: `count(parent_field)` terduplikasi N×

**Severity:** MEDIUM
**Status:** OPEN
**File:** `gator/gator.go:471-492`

#### Deskripsi

Dalam `aggregateExploded`, semua aggregasi diperlakukan sebagai parent-level pada flat rows. Jika user mengagregasi field yang bukan bagian dari exploded array (mis. `salary` saat explode `credits`), setiap flat row membawa nilai `salary` yang sama (dari parent record). Akibatnya:

```
Andi salary = 25.000.000
Andi punya 5 credits → 5 flat rows
COUNT(salary) = 5  ← seharusnya 1
SUM(salary)   = 125.000.000  ← seharusnya 25.000.000
```

Ini secara teknis konsisten dengan SQL behavior (`COUNT(salary)` di SELECT ... GROUP BY product_type memang menghitung rows), tapi bisa sangat membingungkan jika user bermaksud menghitung parent records.

#### Reproduksi

```json
{
  "dataset": "employees",
  "groupBy": ["name", "credits.product_type"],
  "aggregations": [
    { "field": "salary", "op": "sum", "alias": "total_salary" },
    { "field": "credits.outstanding_balance", "op": "sum", "alias": "total_os" }
  ]
}
```
→ Untuk Andi (5 credits), `total_salary` = 5 × 25M = 125M ← salah.

#### Saran Perbaikan

Di `aggregateExploded`, setelah grouping, untuk setiap aggregation:
- Jika field berasal dari exploded array → aggregate semua flat rows (current behavior).
- Jika field adalah parent-level → deduplikasi flat rows per parent sebelum aggregate (ambil nilai dari first row per unique parent key).

```go
// Detect if field is from the exploded array
isArrayField := strings.HasPrefix(agg.Field, explodePath+".")

if isArrayField {
    // aggregate across all flat rows in group (current behavior)
} else {
    // aggregate only from distinct parent rows (dedup by parent identity)
    seen := map[string]bool{}
    for _, r := range rows {
        parentKey := buildParentKey(r, explodePath)
        if !seen[parentKey] {
            seen[parentKey] = true
            // collect value
        }
    }
}
```

---

### BUG-06 — Float equality di `ever_has_last_n`

**Severity:** LOW
**Status:** OPEN
**File:** `gator/compute.go:158`

#### Deskripsi

```go
if f, ok := ToFloat64(arr[i]); ok && f == targetVal {
```

Perbandingan floating-point dengan `==` bisa miss untuk nilai yang tidak representable secara tepat dalam float64 (misalnya `0.1 + 0.2 != 0.3`).

#### Impact

Untuk use case saat ini (collectability codes 1–5, delinquency days 0/30/45/60/90/100/120/150/200), semua nilai adalah integer yang representable dengan tepat → **tidak ada masalah praktis**. Potensi bug jika operator ini digunakan untuk nilai decimal di dataset lain.

#### Saran Perbaikan

Tambah epsilon tolerance untuk perbandingan, atau convert ke string sebelum compare untuk nilai non-integer:

```go
const eps = 1e-9
if f, ok := ToFloat64(arr[i]); ok && math.Abs(f-targetVal) < eps {
    return true
}
```

---

### BUG-07 — `DetectSchema` hanya inspeksi `data[0]`

**Severity:** MEDIUM
**Status:** OPEN
**File:** `gator/schema.go:28`

#### Deskripsi

```go
func DetectSchema(data interface{}, prefix string, currentArrayPath string) []FieldInfo
// dipanggil sebagai:
schema := DetectSchema(data[0], "", "")
```

Schema seluruh dataset ditentukan hanya dari record pertama. Jika record pertama adalah edge case, schema yang dihasilkan salah untuk seluruh dataset.

#### Kasus Kritis

**Kasus A:** Record pertama memiliki `credits: []` (array kosong).
- Schema mendeteksi `credits` sebagai `array_empty`.
- `classifyAggregations` tidak menemukan `credits` sebagai `array_object`.
- Semua aggregasi pada `credits.*` menjadi `levelParent`.
- `GetFieldValue(record, "credits.outstanding_balance")` mengembalikan `nil` untuk semua records.
- **Hasil: semua aggregasi = 0/null, tanpa error.**

**Kasus B:** Record pertama memiliki satu `credit` (scalar karena XML single-child), tapi records lain memiliki multiple credits (array).
- Schema mendeteksi `credits` sebagai object (non-array).
- Aggregasi array-level tidak dilakukan.

#### Reproduksi untuk Kasus A

```go
// Jika Dewi adalah record pertama di dataset employees:
data := []interface{}{dewi, andi, budi, ...}
// DetectSchema dari Dewi (credits:[]) → credits = array_empty
// SUM(credits.outstanding_balance) = 0 untuk SEMUA orang
```

#### Saran Perbaikan

Scan beberapa record awal dan merge schema. Prioritaskan record yang memiliki array non-kosong:

```go
func DetectSchemaFromSample(data []interface{}) []FieldInfo {
    const maxSample = 10
    // Cari record pertama yang memiliki non-empty arrays
    for i, rec := range data {
        if i >= maxSample { break }
        schema := DetectSchema(rec, "", "")
        hasNonEmptyArray := false
        for _, fi := range schema {
            if fi.Type == "array_object" {
                hasNonEmptyArray = true
                break
            }
        }
        if hasNonEmptyArray {
            return schema
        }
    }
    // Fallback: schema dari record pertama
    if len(data) > 0 {
        return DetectSchema(data[0], "", "")
    }
    return nil
}
```

---

### BUG-08 — `loadFileDataset` tidak menerapkan XSD hints

**Severity:** LOW
**Status:** OPEN
**File:** `main.go:234-244`

#### Deskripsi

Dataset XML yang di-load dari `config.yml` saat server startup selalu menggunakan `DefaultXMLOptions()` tanpa XSD hints:

```go
if isXML {
    return ingest.ParseXML(bytes.NewReader(b), ingest.DefaultXMLOptions())
}
```

Bahkan jika user upload XSD via `/api/upload/xsd` setelah server berjalan, dataset yang sudah di-parse saat startup tidak mendapat manfaatnya.

#### Saran Perbaikan

Tambah field `xsd_file` di `DatasetEntry`:

```yaml
datasets:
  - name: ideb
    file: data/ideb.xml
    xsd_file: data/ideb.xsd   # opsional
```

Dan di `config.go`:

```go
type DatasetEntry struct {
    Name    string
    File    string
    XSDFile string  // opsional
}
```

Di `main.go`, load XSD sebelum parse XML:

```go
opts := ingest.DefaultXMLOptions()
if ds.XSDFile != "" {
    if xsdBytes, err := os.ReadFile(ds.XSDFile); err == nil {
        if hints, err := ingest.ParseXSD(bytes.NewReader(xsdBytes)); err == nil {
            opts.Hints = hints
            srv.xsdStore[ds.Name] = hints
        }
    }
}
data, err = ingest.ParseXML(bytes.NewReader(b), opts)
```

---

### BUG-09 — `handleAggregate` tidak validate HTTP method

**Severity:** LOW
**Status:** OPEN
**File:** `main.go:70`

#### Deskripsi

Handler lain (`handleUpload`, `handleUploadXSD`) melakukan check `r.Method != http.MethodPost`, tapi `handleAggregate` tidak:

```go
func (s *server) handleAggregate(w http.ResponseWriter, r *http.Request) {
    body, err := io.ReadAll(r.Body)  // ← langsung baca body tanpa cek method
```

GET request ke `/api/aggregate` akan mencoba membaca empty body → `json.Unmarshal("")` → error `unexpected end of JSON input` dengan status 400.

#### Saran Perbaikan

```go
func (s *server) handleAggregate(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
        return
    }
    // ...
}
```

---

### BUG-10 — `xsdStore` di `main.go` tidak thread-safe

**Severity:** HIGH (sama dengan BUG-04)
**Status:** OPEN
**File:** `main.go:32-35`

#### Deskripsi

`server.xsdStore` adalah plain `map[string]ingest.XSDHints{}` tanpa mutex:

```go
type server struct {
    store    *gator.Store       // ← sudah fixed (BUG-04)
    xsdStore map[string]ingest.XSDHints  // ← NOT thread-safe
    // ...
}
```

Concurrent write (`POST /api/upload/xsd`) dan read (`GET /api/xsd/info`, `POST /api/upload` yang membaca hints) dapat menyebabkan data race.

#### Saran Perbaikan

Enkapsulasi `xsdStore` ke struct dengan mutex, analog dengan `Store`:

```go
type xsdStoreT struct {
    mu    sync.RWMutex
    hints map[string]ingest.XSDHints
}

func (x *xsdStoreT) Set(name string, h ingest.XSDHints) {
    x.mu.Lock(); defer x.mu.Unlock()
    x.hints[name] = h
}

func (x *xsdStoreT) Get(name string) (ingest.XSDHints, bool) {
    x.mu.RLock(); defer x.mu.RUnlock()
    h, ok := x.hints[name]
    return h, ok
}

func (x *xsdStoreT) Names() []string {
    x.mu.RLock(); defer x.mu.RUnlock()
    names := make([]string, 0, len(x.hints))
    for k := range x.hints { names = append(names, k) }
    return names
}
```

---

### QUIRK-11 — `explodeRecords` shallow copy — element maps shared

**Severity:** LOW
**Status:** OPEN
**File:** `gator/gator.go:239-241`

#### Deskripsi

```go
flat[arrayKey] = elemMap  // shared reference
```

Semua flat rows yang berasal dari credits array yang sama berbagi reference ke element map yang sama dari original record. Mutasi ke element map dari satu flat row akan mempengaruhi flat row lain.

#### Impact Saat Ini

**Tidak ada** — engine saat ini hanya membaca flat rows, tidak pernah menulis ke dalamnya. Bug latent yang bisa muncul jika engine diperluas dengan fitur mutasi (mis. computed fields, transformation pipeline).

#### Saran

Tambahkan comment eksplisit:

```go
// flat[arrayKey] holds a read-only reference to the original element map.
// Do not mutate — shared across flat rows from the same parent record.
flat[arrayKey] = elemMap
```

Atau gunakan `DeepCopyMap(elemMap)` jika immutability guarantee diperlukan.

---

### QUIRK-12 — XSD path vs schema path mismatch (root element prefix)

**Severity:** MEDIUM
**Status:** OPEN
**File:** `gator/ingest/xsd.go` vs `gator/ingest/xml.go`, `gator/schema.go`

#### Deskripsi

Ada dua representasi path yang berbeda dalam sistem:

**Path A — XSD parsed hints** (digunakan saat parse-time di `childrenToMap`):
Dibangun dari nama element saat parsing XSD, **termasuk root element name**:
```
"Response.Product.TradeLines.TradeLine"  ← XSD path
```

**Path B — Schema paths** (digunakan untuk UI dan aggregation classification):
Dibangun oleh `DetectSchema` dari hasil parsed XML. Root element sudah di-unwrap oleh `ParseXML`, sehingga `data[0]` adalah **isi** root element, tanpa root element name:
```
"Product.TradeLines.TradeLine"  ← schema path (tanpa "Response.")
```

#### Dimana Ini Berpengaruh

| Penggunaan | Path yang digunakan | Benar? |
|---|---|---|
| Force-array saat parse XML | XSD path (node.path includes root) | ✓ Benar |
| StringPaths saat coerce | XSD path (node.path includes root) | ✓ Benar |
| `/api/schema` response ke frontend | Schema path (no root prefix) | — |
| DSL aggregation field paths | Schema path (user menulis path dari UI) | ✓ Benar |
| XSD hints panel di UI | XSD paths dari `/api/xsd/info` | ✗ **Confusing** |

#### Impact

XSD hints **bekerja secara fungsional** (applied saat parse, path match). Tapi di UI, panel XSD hints menampilkan paths dengan root prefix (`"Response.Product.TradeLines.TradeLine"`) sementara schema dropdown menampilkan paths tanpa root prefix (`"Product.TradeLines.TradeLine"`). **User bingung** karena paths terlihat berbeda.

#### Reproduksi

1. Upload `tuxml.xml` → schema shows `"Product.ConsumerFile.TradeLines.TradeLine"`
2. Upload XSD → XSD panel shows `"Response.Product.ConsumerFile.TradeLines.TradeLine"`
3. User mencoba menulis aggregation `"field": "Response.Product...."` → tidak bekerja
4. Correct field adalah `"Product.ConsumerFile...."` → mismatch dengan XSD display

#### Saran Perbaikan

Opsi A: Strip root element prefix dari XSD hints sebelum store, agar konsisten dengan schema paths. Di `handleUploadXSD`:
```go
rootPrefix := inferRootPrefix(hints) // find common prefix of all paths
hints = stripPrefix(hints, rootPrefix)
```

Opsi B: Di `/api/xsd/info`, normalise paths to strip root prefix before returning.

Opsi C: Di `/api/schema`, tambahkan root element name sebagai prefix agar konsisten dengan XSD paths. (Breaking change)

**Opsi A direkomendasikan** karena paling transparan untuk user.

---

## Saran Prioritas Perbaikan

### Sprint 1 — High Severity ✅ SELESAI

| # | Item | Status |
|---|---|---|
| BUG-04 | Store thread-safety | ✅ FIXED |
| BUG-10 | xsdStore thread-safety | ✅ FIXED |
| BUG-02 | explodeRecords nested path | ✅ FIXED |

### Sprint 2 — Medium Severity ✅ SELESAI

| # | Item | Status |
|---|---|---|
| BUG-05 | Explode mode parent field dedup | ✅ FIXED |
| BUG-07 | DetectSchema dari sample terbaik | ✅ FIXED |
| QUIRK-12 | XSD path vs schema path normalisasi | ✅ FIXED |

### Sprint 3 — Low Severity ✅ SEBAGIAN SELESAI

| # | Item | Status |
|---|---|---|
| BUG-08 | loadFileDataset + xsd_file config | ✅ FIXED |
| BUG-09 | handleAggregate method check | ✅ FIXED |
| BUG-06 | Float equality epsilon | ✅ FIXED |
| BUG-01 | Validasi field resolve ke scalar | ✅ FIXED |
| BUG-03 | avg weighted (mathematically correct) | ✅ FIXED |
| QUIRK-11 | explodeRecords deep copy | ✅ FIXED (via BUG-02 DeepCopyMap) |

---

## Catatan Metodologi

Audit dilakukan dengan:
- **Static analysis manual** — membaca setiap fungsi dan menelusuri alur data
- **Trace semantik** — menjalankan DSL secara mental untuk edge cases
- **Cross-file analysis** — memeriksa konsistensi antar modul (xsd.go vs xml.go, gator.go vs compute.go)
- **Concurrency analysis** — memeriksa semua shared state yang diakses dari goroutine berbeda

Audit ini **tidak** mencakup:
- Performance profiling
- Memory leak detection
- Fuzz testing input XML/JSON
- Load testing concurrent requests
