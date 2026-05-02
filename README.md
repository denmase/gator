# Visual Aggregator — `gator`

SQL-like aggregation engine untuk JSON dan XML, tanpa schema yang ditetapkan terlebih dahulu.
Dirancang untuk kebutuhan analitik data kredit, tapi bersifat **generic** dan bisa dipakai untuk format JSON/XML apapun.

---

## Daftar Isi

1. [Fitur](#fitur)
2. [Arsitektur & Struktur Direktori](#arsitektur--struktur-direktori)
3. [Cara Menjalankan](#cara-menjalankan)
4. [Konfigurasi (`config.yml`)](#konfigurasi-configyml)
5. [DSL — Bahasa Query](#dsl--bahasa-query)
6. [Aggregation Operators](#aggregation-operators)
7. [Format Data](#format-data)
   - [JSON](#json)
   - [XML — Konvensi Representasi](#xml--konvensi-representasi)
   - [XSD Hints (Opsi B)](#xsd-hints-opsi-b)
8. [Desain Engine](#desain-engine)
   - [Two-Pass Aggregation](#two-pass-aggregation)
   - [Nested Array (Multi-Level)](#nested-array-multi-level)
   - [Array Path Derivation](#array-path-derivation)
   - [Date-Field Window Filtering](#date-field-window-filtering)
9. [HTTP API](#http-api)
10. [Frontend](#frontend)
11. [Test Suite](#test-suite)
12. [Keputusan Desain yang Didiskusikan](#keputusan-desain-yang-didiskusikan)

---

## Fitur

| Kategori | Kemampuan |
|---|---|
| **Aggregasi** | `sum`, `avg`, `min`, `max`, `count`, `count_distinct` |
| **Window (index)** | `worst_last_n`, `max_last_n`, `ever_has_last_n`, `count_last_n`, `sum_last_n` |
| **Window (tanggal)** | `max`/`min` + `date_field` param, `count_date_last_n` dengan `unit: month\|day\|year` |
| **Grouping** | `GROUP BY` satu atau lebih field (dot-notation) |
| **Filter** | `WHERE` global, `localFilter` per-array, operator: `$eq $ne $gt $gte $lt $lte $in $contains $notnull $null` |
| **Nested array** | Aggregasi rekursif sampai kedalaman tak terbatas (N level) |
| **Format input** | JSON, XML (lossless), XSD hints (opsional) |
| **Schema** | Auto-detect dari record pertama — tidak perlu mendefinisikan schema sebelumnya |
| **Frontend** | Visual builder HTML/JS, upload drag-drop, DSL preview, hasil tabel |

---

## Arsitektur & Struktur Direktori

```
aggregator/
├── main.go                        # HTTP server, routing, startup
├── config.yml                     # Konfigurasi server
├── go.mod                         # Go module (stdlib only, no external deps)
│
├── config/
│   ├── config.go                  # YAML loader (stdlib, no external deps)
│   └── config_test.go
│
├── gator/                         # Engine package — inti agregasi
│   ├── gator.go                   # Aggregate(), Store, AggregateRequest, klasifikasi level
│   ├── schema.go                  # DetectSchema(), GetFieldValue(), NavigateToParent(), DeepCopyMap()
│   ├── filter.go                  # EvaluateWhere(), ApplyLocalFilters()
│   ├── compute.go                 # ComputeAggregation(), AggregateArrayField(), CrossRecordOp()
│   ├── gator_test.go              # Test engine: pefindo, employees, credit DSL, Dewi, nested
│   │
│   ├── ingest/                    # Parser format data
│   │   ├── xml.go                 # ParseXML(), ParseXMLMany(), XMLOptions + XSDHints
│   │   ├── xsd.go                 # ParseXSD(), XSDHints (StringPaths, ArrayPaths)
│   │   ├── xml_test.go            # 14 unit test parser XML
│   │   ├── xsd_test.go            # 4 test XSD: parse, force-array, string-no-coerce, merge
│   │   └── xml_aggregate_test.go  # End-to-end: ideb.xml + tuxml.xml → Aggregate()
│   │
│   └── samples/
│       └── samples.go             # Built-in dataset "employees" (9 orang, nested credits[])
│
└── static/
    └── index.html                 # Frontend single-file (HTML + CSS + JS)
```

**Prinsip arsitektur:**
- `gator/` — murni engine, tidak tahu apa-apa soal HTTP, format file, atau domain spesifik
- `gator/ingest/` — konversi format → `[]interface{}`, tidak tahu apa-apa soal aggregasi
- `gator/samples/` — data demo, terpisah dari engine agar engine tidak punya dependency domain
- `main.go` — wiring: HTTP ↔ engine ↔ ingest
- Zero external dependencies — hanya Go standard library

---

## Cara Menjalankan

```bash
# Clone / salin file ke direktori
cd aggregator

# Jalankan server (default: port 8888)
go run main.go

# Atau dengan config custom
go run main.go myconfig.yml

# Akses frontend
open http://localhost:8888

# Jalankan test
go test ./...
```

Server akan otomatis mendaftarkan dataset built-in `employees` jika `enable_samples: true` di config.

---

## Konfigurasi (`config.yml`)

```yaml
# TCP port server
port: 8888

# Direktori web root (berisi index.html)
static_dir: static

# true = daftarkan dataset built-in "employees"
enable_samples: true

# true = log setiap HTTP request
log_requests: false

# Dataset file yang di-load saat startup
# Format auto-detect dari ekstensi: .json → JSON, .xml → XML
datasets:
  - name: ideb
    file: data/ideb.xml
  - name: pefindo
    file: data/pefindo.json
```

Config parser ditulis dengan `bufio.Scanner` (tanpa library YAML eksternal) dan mendukung:
- Key-value di level root
- List `datasets:` dengan sub-key `name` dan `file`
- Komentar inline (`# ...`)
- Fallback ke nilai default jika key tidak ada

---

## DSL — Bahasa Query

Request body ke `POST /api/aggregate`:

```json
{
  "dataset": "nama_dataset",

  "localFilter": {
    "credits": {
      "credits.loan_status": { "$eq": "active" }
    }
  },

  "where": {
    "department": { "$eq": "IT" },
    "salary": { "$gte": 20000000 }
  },

  "groupBy": ["department", "city"],

  "aggregations": [
    { "field": "salary", "op": "sum", "alias": "total_salary" },
    {
      "field": "credits.outstanding_balance",
      "op": "sum",
      "alias": "total_os"
    },
    {
      "field": "FasilitasList.Fasilitas.RiwayatKolektibilitas.HariTunggakan",
      "op": "max",
      "alias": "worst_dpd_3m",
      "params": {
        "date_field": "Bulan",
        "ref_date":   "2026-04-30",
        "n":          3,
        "unit":       "month"
      }
    },
    {
      "field": "credits.open_date",
      "op": "count_date_last_n",
      "alias": "new_acc_3m",
      "params": { "n": 3, "ref_date": "2026-04-30", "unit": "month" }
    }
  ]
}
```

### `localFilter`

Filter elemen di dalam array **sebelum** aggregasi. Shape:
```
{ "arrayPath": { "arrayPath.fieldName": { "$op": value } } }
```

Record parent yang arraynya kosong setelah filter **tetap muncul** di hasil (ghost row) dengan nilai 0/null untuk field array-level.

### `where`

Filter terhadap record parent **setelah** local filter. Field path mendukung dot-notation.

### `groupBy`

Array field path. Aggregasi tanpa groupBy menghasilkan satu baris (`__all__`).

### `aggregations`

Setiap entri: `{ field, op, alias, params }`. `alias` opsional (default: `op_field_underscored`). `params` berisi parameter operator-spesifik.

---

## Aggregation Operators

### Scalar (berlaku untuk field apapun)

| Op | Keterangan |
|---|---|
| `sum` | Jumlah nilai numerik |
| `avg` | Rata-rata |
| `min` | Nilai minimum |
| `max` | Nilai maksimum. Jika `params.date_field` diisi → filter elemen berdasarkan window tanggal sebelum mencari max |
| `count` | Jumlah baris/elemen |
| `count_distinct` | Jumlah nilai unik |

### Window — berbasis index array

Beroperasi pada field bertipe array numerik (mis. `last_24_delq_hist = [0, 0, 30, 45, ...]`). "Last N" artinya N elemen terakhir berdasarkan posisi index.

| Op | Params | Keterangan |
|---|---|---|
| `worst_last_n` | `n` | MAX dari N elemen terakhir array |
| `max_last_n` | `n` | Alias untuk `worst_last_n` |
| `ever_has_last_n` | `n`, `value` | 1.0 jika `value` muncul di N elemen terakhir, 0.0 jika tidak |
| `count_last_n` | `n`, `value` | Jumlah array di mana `value` muncul di N elemen terakhir |
| `sum_last_n` | `n` | Jumlah N elemen terakhir |

### Window — berbasis tanggal

Beroperasi pada field string tanggal di dalam array of objects. Filter elemen berdasarkan window tanggal, baru aggregate nilai target.

| Op | Params | Keterangan |
|---|---|---|
| `max` + `date_field` | `date_field`, `n`, `ref_date`, `unit` | MAX field target dari elemen yang tanggalnya dalam window |
| `min` + `date_field` | idem | MIN dengan filter tanggal |
| `count_date_last_n` | `n`, `ref_date`, `unit` | Hitung string tanggal yang masuk dalam window |

**Params untuk date window:**

| Param | Tipe | Default | Keterangan |
|---|---|---|---|
| `date_field` | string | — | Nama field tanggal dalam element array, mis. `"Bulan"` |
| `n` | int | 6 | Jumlah periode |
| `ref_date` | string `YYYY-MM-DD` | today | Tanggal referensi |
| `unit` | `"month"` \| `"day"` \| `"year"` | `"month"` | Satuan periode |

Format tanggal yang didukung: `YYYY-MM-DD` dan `YYYY-MM` (partial, diperlakukan sebagai tanggal pertama bulan itu).

---

## Format Data

### JSON

Upload `application/json`. Struktur apapun yang valid:
- Array of objects → langsung digunakan
- Single object → di-wrap otomatis jadi `[object]`

### XML — Konvensi Representasi

Gator mengkonversi XML ke `map[string]interface{}` menggunakan konvensi **lossless** berikut:

| Kasus XML | Hasil Go |
|---|---|
| `<Score>750</Score>` | `"Score": 750` (coerce ke number) |
| `<Code>01</Code>` | `"Code": "01"` (leading zero → tetap string) |
| `<NIK>3201234567890001</NIK>` | `"NIK": "3201234567890001"` (>15 digit → tetap string, hindari float64 precision loss) |
| `<Active>true</Active>` | `"Active": true` (bool) |
| `<Note/>` | `"Note": null` |
| `<loanid status="active">123</loanid>` | `"loanid": {"@status":"active","#text":123}` |
| `<Status active="true"/>` | `"Status": {"@active":true}` |
| Tag muncul ≥2x dalam satu parent | `"TradeLine": [{...}, {...}]` (array) |
| Tag muncul 1x dalam satu parent | `"Score": {...}` (scalar/map) |
| Namespace prefix | Go's decoder meresolve ke URI, local name dipertahankan |

**Prefix key conventions:**
- Attribute → `"@attrName": value`
- Text content (ketika ada attr/child) → `"#text": value`

**Force-array rule:** Tag di-force menjadi `[]interface{}` **hanya jika** tag tersebut muncul ≥2 kali dalam satu parent, atau jika XSD menyatakan `maxOccurs="unbounded"`.

**Konsekuensi single-item container:** Dokumen XML dengan `<Scores><Score>...</Score></Scores>` (satu Score) menghasilkan `Score` sebagai map, bukan array. Dokumen lain dengan dua Score menghasilkan array. Ini bisa menyebabkan schema inkonsisten antar dokumen — gunakan XSD hints untuk mengatasi ini.

### XSD Hints (Opsi B)

XSD bersifat **opsional**. Tanpanya, engine tetap berjalan dengan heuristik. Dengan XSD, dua hal ditangani lebih akurat:

**1. Force-array pada single-item container**

```xml
<!-- XSD -->
<xs:element name="TradeLine" maxOccurs="unbounded"/>

<!-- XML dengan 1 TradeLine -->
<!-- Tanpa XSD: "TradeLine": {...} (scalar) -->
<!-- Dengan XSD: "TradeLine": [{...}] (array) ✓ -->
```

**2. Mencegah coercion field yang secara domain adalah string**

```xml
<!-- XSD -->
<xs:element name="ZipCode" type="xs:string"/>

<!-- XML -->
<ZipCode>10001</ZipCode>
<!-- Tanpa XSD: 10001 (float64) — mungkin salah untuk filter $eq "10001" -->
<!-- Dengan XSD: "10001" (string) ✓ -->
```

XSD types yang diklasifikasikan sebagai string: `xs:string`, `xs:normalizedString`, `xs:token`, `xs:date`, `xs:dateTime`, `xs:anyURI`, `xs:hexBinary`, dan lainnya.

XSD types yang tetap dicoerce ke number: `xs:integer`, `xs:decimal`, `xs:float`, `xs:double`, `xs:long`, dll.

---

## Desain Engine

### Two-Pass Aggregation

**Masalah:** Query yang mengandung field dari level parent (mis. `summary.totalLimit`) dan field dari dalam array (mis. `detailedFacilities.limit`) tidak bisa diselesaikan dengan naive flattening. Flattening menduplikasi parent row sebanyak N child, sehingga `SUM(parent.field)` terhitung N kali.

**Solusi:** Dua pass terpisah per grup.

```
Pass 1 — parent-level:
  Kumpulkan nilai field dari records langsung.
  SUM(summary.totalLimit) → [100_000_000] → result: 100_000_000 ✓

Pass 2 — array-level:
  Per record: masuk ke array, aggregate field target → scalar
  Per grup: combine scalars dengan CrossRecordOp
  SUM(detailedFacilities.limit) → per record: [20M, 80M] → 100M → result: 100_000_000 ✓
```

### Nested Array (Multi-Level)

Untuk field seperti `FasilitasList.Fasilitas.RiwayatKolektibilitas.HariTunggakan` (2 level array), engine menggunakan `aggregateNestedField` yang rekursif:

```
Per record (satu debtor):
  Masuk ke Fasilitas[]:
    Untuk tiap Fasilitas:
      Masuk ke RiwayatKolektibilitas[]:
        Filter elemen berdasarkan date_field jika ada
        Collect HariTunggakan → [0, 0, 35]
        → computeAggregation("max") = 35           ← per-Fasilitas scalar
    Combine per-Fasilitas: max(35, 45) = 45         ← per-record scalar
  Combine per-record: max(45) = 45                  ← group result
```

### Array Path Derivation

Engine **tidak memerlukan** `arrayPath` eksplisit di DSL. Ia menderive level eksekusi secara otomatis dari schema yang dideteksi:

```
Field path: "FasilitasList.Fasilitas.RiwayatKolektibilitas.HariTunggakan"

Prefix walk:
  "FasilitasList"                                 → type: object      → skip
  "FasilitasList.Fasilitas"                       → type: array_object → ancestor[0]
  "FasilitasList.Fasilitas.RiwayatKolektibilitas" → type: array_object → ancestor[1]
  "...HariTunggakan"                              → type: number      → leaf

arrayAncestors = ["FasilitasList.Fasilitas",
                  "FasilitasList.Fasilitas.RiwayatKolektibilitas"]
level = levelNested
```

Hanya `array_object` yang dihitung sebagai ancestor. `array_number` dan `array_string` adalah leaf (mis. `last_24_delq_hist = [0, 0, 30, ...]`).

### Date-Field Window Filtering

Untuk query *"worst HariTunggakan 3 bulan terakhir"* di mana data per bulan adalah object:

```json
{ "op": "max",
  "params": { "date_field": "Bulan", "ref_date": "2026-04-30", "n": 3, "unit": "month" } }
```

Engine memfilter elemen array berdasarkan `elemMap["Bulan"]` sebelum mengumpulkan nilai. Elemen yang `Bulan`-nya di luar window `[ref_date - 3 months, ref_date]` dilewati.

Format tanggal partial `"2026-04"` (YYYY-MM) ditangani dengan memperlakukannya sebagai `"2026-04-01"`.

### CrossRecordOp

Hasil per-record dari aggregasi array harus digabungkan secara tepat ketika ada beberapa records dalam satu group:

| Op | CrossRecordOp | Alasan |
|---|---|---|
| `sum`, `count`, `count_date_last_n` | `sum` | Partial sum dijumlah |
| `ever_has_last_n` | `max` | 1 jika ada record yang punya kondisi |
| `max`, `worst_last_n` | `max` | Max dari per-record maxes |
| `min` | `min` | Min dari per-record mins |
| `avg` | `avg` | Mean of means (simple, equal-weight) |

### Schema Detection

`DetectSchema(data[0], "", "")` berjalan rekursif di record pertama dan menghasilkan `[]FieldInfo`:

```go
type FieldInfo struct {
    Path      string // "FasilitasList.Fasilitas.Outstanding"
    Type      string // "number" | "string" | "boolean" | "null" |
                     // "array_object" | "array_number" | "array_string" |
                     // "array_primitive" | "array_empty"
    ArrayPath string // path ancestor array terdekat, atau "" jika di root
}
```

Field dengan `ArrayPath != ""` adalah kandidat array-level aggregation.

---

## HTTP API

| Method | Endpoint | Keterangan |
|---|---|---|
| `GET` | `/api/datasets` | Daftar nama dataset |
| `GET` | `/api/schema?dataset=<name>` | `[]FieldInfo` dari dataset |
| `GET` | `/api/data?dataset=<name>` | Raw data (untuk preview) |
| `POST` | `/api/aggregate` | Eksekusi DSL, return `[]map` |
| `POST` | `/api/upload` | Upload JSON atau XML. Auto-detect dari Content-Type atau byte pertama |
| `POST` | `/api/upload/xsd?dataset=<name>` | Upload XSD, simpan hints untuk `<name>` |
| `GET` | `/api/xsd` | Dataset yang punya XSD hints |
| `GET` | `/api/xsd/info?dataset=<name>` | Detail `stringPaths` dan `arrayPaths` dari XSD |

**Upload JSON:**
```bash
curl -X POST http://localhost:8888/api/upload \
  -H "Content-Type: application/json" \
  -d @data.json
# Response: {"name":"uploaded_1234","count":42,"format":"json"}
```

**Upload XML:**
```bash
curl -X POST http://localhost:8888/api/upload \
  -H "Content-Type: application/xml" \
  -d @data.xml
# Response: {"name":"uploaded_5678","count":1,"format":"xml"}
```

**Upload XSD (opsional, untuk dataset XML):**
```bash
curl -X POST "http://localhost:8888/api/upload/xsd?dataset=uploaded_5678" \
  -H "Content-Type: application/xml" \
  -d @schema.xsd
# Response: {"dataset":"uploaded_5678","arrayPaths":3,"stringPaths":8}
```

---

## Frontend

`static/index.html` adalah single-file application (HTML + CSS + JS, tanpa framework).

### Fitur UI

**Upload area:**
- Drag-and-drop atau klik untuk upload JSON/XML
- Format auto-detect dari ekstensi file
- Upload XSD opsional muncul otomatis setelah dataset XML dipilih

**Dataset list:**
- Badge format: `JSON` (biru) / `XML` (orange) / `builtin` (hijau)
- Badge `XSD` (ungu) jika hints sudah dimuat
- Panel detail XSD hints (array paths + string paths)

**Query builder:**
- **Local Filter** — filter elemen array sebelum aggregasi
- **WHERE** — filter parent record
- **GROUP BY** — multi-field dengan chip UI
- **Aggregations** — field + operator + alias
  - Window ops (index): input N
  - Date window ops: input `date_field`, `ref_date` (date picker), `N`, `unit`

**DSL Preview:** JSON yang akan dikirim ke `/api/aggregate` — realtime update.

**Hasil:** Tabel dengan scroll, nilai numerik rata-kanan, null di-display dengan style italic.

---

## Test Suite

29 test, 0 failure.

### `config/config_test.go` — 3 test
- `TestDefaults` — nilai default
- `TestLoad` — parse YAML lengkap
- `TestMissingFile` — file tidak ada → error

### `gator/gator_test.go` — 6 test
- `TestPefindo` — two-pass: verifikasi tidak ada double-counting pada mixed parent + array field
- `TestEmployeesMixedAgg` — group by department, salary (parent) + outstanding (array)
- `TestLocalFilterNoDoubling` — local filter + verifikasi parent field tidak ikut digandakan
- `TestDewi` — record dengan `credits: []` tetap muncul di hasil, nilai count/ever = 0 bukan nil
- `TestCountDateLastN` — operator `count_date_last_n`
- `TestCreditFullDSL` — DSL 6 aggregasi sekaligus dengan `worst_last_n`, `ever_has_last_n`, `count_date_last_n`

### `gator/ingest/xml_test.go` — 14 test unit parser
- Scalar, empty, boolean coerce, attr+text, attr-only
- Single-child → scalar, multi-child → array, non-container → scalar
- Primitive array, root attribute, alert primitive array
- Integration: `TestTUXML` (tuxml.xml), `TestIDEB` (ideb.xml)

### `gator/ingest/xsd_test.go` — 4 test
- `TestParseXSD` — parse XSD, verifikasi ArrayPaths dan StringPaths
- `TestXSDHintsForceArray` — single-child Score + TradeLine dipaksa jadi array oleh XSD
- `TestXSDHintsStringNoCoerce` — ZipCode `xs:string` tidak di-coerce ke float
- `TestMergeXSDHints` — merge dua XSDHints

### `gator/ingest/xml_aggregate_test.go` — 2 test end-to-end
- `TestIDEBAggregation` — parse ideb.xml → Aggregate: parent-level, level-1 array, level-2 nested (dengan date_field filter)
- `TestTUXMLAggregation` — parse tuxml.xml → Aggregate: sum balance, max DPD, count date last N

---

## Keputusan Desain yang Didiskusikan

### Mengapa tidak menggunakan flattening?

Flattening adalah pendekatan naif yang sering digunakan: denormalisasi parent-child menjadi baris flat, lalu aggregate semuanya. Masalahnya, ketika query mengandung field dari parent **dan** field dari dalam array di satu waktu, parent field terhitung N kali (N = jumlah child). Ini adalah bug yang tidak terdeteksi secara kasat mata — hasil terlihat wajar tapi nilainya salah dua kali lipat.

Contoh kasus yang memotivasi perbaikan ini: dataset pefindo.json dengan `summary.totalLimit = 100_000_000` dan dua `detailedFacilities`. Dengan naive flattening, `SUM(summary.totalLimit)` menghasilkan `200_000_000`.

### Mengapa `array_object` saja yang jadi ancestor, bukan `array_number`?

`array_number` seperti `last_24_delq_hist = [0, 30, 45, ...]` adalah leaf — isinya nilai yang langsung di-aggregate. Ia bukan container yang perlu di-iterate untuk masuk ke level berikutnya. Hanya `array_object` (array of maps) yang bisa jadi intermediate ancestor dalam path rekursif.

### XML: force-array berdasarkan observasi, bukan schema

Keputusan awal untuk "force array jika semua sibling punya tag sama" terbukti terlalu agresif — element seperti `<Score>` yang hanya punya `<Value>` satu-satunya child tidak seharusnya dijadikan array. Keputusan final: **force array jika tag yang sama muncul ≥2 kali dalam satu parent**. Ini observable dari dokumen tanpa schema eksternal.

Konsekuensinya: dokumen XML dengan single-item container (`<Scores><Score>...</Score></Scores>`) menghasilkan scalar, bukan array. Ini diselesaikan dengan XSD hints.

### Mengapa XSD bersifat opsional (Opsi B)?

Opsi A (heuristik murni) mencakup 90% kasus tapi gagal di edge case seperti ZipCode yang terlihat numerik. Opsi C (full XSD validation wajib) terlalu invasif dan tidak mungkin untuk semua format XML yang ada di lapangan. Opsi B adalah sweet spot: engine tetap bekerja tanpa XSD, tapi bisa di-improve dengan XSD untuk dua hal spesifik yang heuristik tidak bisa selesaikan.

### Mengapa `date_field` di params bukan di DSL level atas?

Konsistensi dengan operator lain yang punya params (`n`, `value`). User bisa menentukan `date_field` yang berbeda untuk setiap aggregation di satu query — misalnya satu aggregation memakai `date_field: "TanggalMulai"` (open date) dan satu lagi `date_field: "Bulan"` (history date). Kalau `date_field` di level DSL atas, harus dibuat satu per query.

### Mengapa `count_date_last_n` bukan `count_open_last_n`?

Nama `count_open_last_n` spesifik ke konsep "tanggal buka akun". Setelah diskusi, digeneralisasi menjadi `count_date_last_n` — menghitung elemen array yang nilai field tanggalnya (`date_field`) jatuh dalam window. Bisa dipakai untuk `open_date`, `close_date`, `event_date`, `Bulan`, atau field tanggal apapun.

---

## Penambahan yang Mungkin di Masa Depan

- **Reparse endpoint** — apply XSD hints ke dataset yang sudah diupload tanpa upload ulang XML
- **Multi-record XML** — `ParseXMLMany` untuk XML yang berisi banyak record dalam satu root
- **CSV ingest** — tab pemisah yang sama dengan JSON/XML di `gator/ingest/csv.go`
- **Persist dataset** — simpan dataset yang diupload ke disk agar tidak hilang setelah restart
- **Export hasil** — download hasil aggregasi sebagai CSV atau JSON dari UI
