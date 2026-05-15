# Visual Aggregator — `gator`

SQL-like aggregation engine untuk JSON dan XML, tanpa schema yang ditetapkan terlebih dahulu. Dirancang untuk analitik data kredit, tapi bersifat **generic** untuk format JSON/XML apapun.

---

## Cara Menjalankan

```bash
go run main.go              # pakai config.yml di direktori saat ini
go run main.go myconfig.yml # pakai config custom
go test ./...               # jalankan semua test
```

Buka `http://localhost:8888` setelah server berjalan.

---

## Struktur Direktori

```
aggregator/
├── main.go                        # HTTP server, routing, startup
├── config.yml                     # konfigurasi server
├── go.mod                         # Go module (stdlib only, zero external deps)
│
├── config/
│   ├── config.go                  # YAML loader (stdlib only)
│   └── config_test.go
│
├── gator/                         # engine package — inti agregasi
│   ├── gator.go                   # Aggregate(), Store, klasifikasi level
│   ├── schema.go                  # DetectSchemaFromSample(), GetFieldValue()
│   ├── filter.go                  # EvaluateWhere(), ApplyLocalFilters()
│   ├── compute.go                 # ComputeAggregation(), weighted avg
│   ├── cache.go                   # PERF-01/02/03/04 — transparan dalam Store
│   ├── gator_test.go              # unit tests
│   ├── cache_test.go              # correctness + cache invalidation + concurrent
│   ├── bench_test.go              # benchmark suite
│   ├── memleak_test.go            # memory leak detection
│   ├── fuzz_test.go               # fuzz testing
│   ├── load_test.go               # load + concurrency tests
│   │
│   ├── ingest/
│   │   ├── xml.go                 # lossless XML → map[string]interface{}
│   │   ├── xsd.go                 # XSD hint parser (opsional, untuk XML)
│   │   ├── xml_test.go
│   │   ├── xsd_test.go
│   │   └── xml_aggregate_test.go  # end-to-end: parse XML → Aggregate
│   │
│   └── samples/
│       └── samples.go             # built-in dataset "employees"
│
└── static/
    └── index.html                 # frontend single-file (HTML + CSS + JS)
```

---

## API

Satu-satunya API yang perlu diketahui:

```go
store := gator.NewStore()
store.Register("mydata", data)          // []interface{} dari JSON atau XML
rows, err := gator.Aggregate(store, req)
```

Tidak ada konfigurasi, tidak ada mode opsional. Semua optimasi performa aktif otomatis di dalam `NewStore()` dan `Aggregate()` — engine mendeteksi karakteristik query dan memilih code path terbaik.

---

## Konfigurasi (`config.yml`)

```yaml
port: 8888
static_dir: static
enable_samples: true   # daftarkan dataset built-in "employees"
log_requests: false

datasets:
  - name: ideb
    file: data/ideb.xml
    xsd_file: data/ideb.xsd   # opsional — untuk XML saja
  - name: kredit
    file: data/kredit.json
```

---

## DSL — Bahasa Query

`POST /api/aggregate` dengan body JSON:

```json
{
  "dataset": "nama_dataset",

  "localFilter": {
    "credits": {
      "credits.loan_status": { "$eq": "active" }
    }
  },

  "where": {
    "department": { "$eq": "IT" }
  },

  "groupBy": ["department", "city"],

  "aggregations": [
    { "field": "salary",                       "op": "sum",  "alias": "total_salary" },
    { "field": "credits.outstanding_balance",  "op": "avg",  "alias": "avg_os" },
    {
      "field": "FasilitasList.Fasilitas.RiwayatKolektibilitas.HariTunggakan",
      "op": "max",
      "alias": "worst_dpd_3m",
      "params": { "date_field": "Bulan", "ref_date": "2026-04-30", "n": 3, "unit": "month" }
    }
  ]
}
```

### `localFilter`

Filter elemen di dalam array **sebelum** aggregasi. Record parent yang arraynya kosong setelah filter **tetap muncul** di hasil dengan nilai `0`/`null` (ghost row).

### `where`

Filter terhadap record parent. Operator: `$eq`, `$ne`, `$gt`, `$gte`, `$lt`, `$lte`, `$in`, `$contains`, `$notnull`, `$null`.

### `groupBy`

Field path dapat mengacu ke field parent maupun field di dalam array:
- `["department"]` → standard mode, satu baris per department
- `["department", "credits.product_type"]` → explode mode, satu baris per (department, product_type)

Engine mendeteksi mode secara otomatis.

### `aggregations`

| Op | Keterangan | Params |
|---|---|---|
| `sum`, `avg`, `min`, `max` | Scalar ops | `max`/`min` mendukung `date_field` |
| `count`, `count_distinct` | Hitung | — |
| `worst_last_n` / `max_last_n` | Max N elemen terakhir array numerik (by index) | `n` |
| `ever_has_last_n` | 1 jika nilai ada di N elemen terakhir | `n`, `value` |
| `count_last_n` | Jumlah array yang punya nilai di N terakhir | `n`, `value` |
| `sum_last_n` | Jumlah N elemen terakhir | `n` |
| `count_date_last_n` | Hitung string tanggal dalam window | `n`, `unit`, `ref_date` |

**Date window params:**

| Param | Default | Keterangan |
|---|---|---|
| `date_field` | — | Nama field tanggal dalam element array, mis. `"Bulan"` |
| `n` | 6 | Jumlah periode |
| `ref_date` | today | Tanggal referensi `YYYY-MM-DD` |
| `unit` | `"month"` | `"month"` \| `"day"` \| `"year"` |

Format tanggal yang didukung: `YYYY-MM-DD` dan `YYYY-MM`.

---

## Format Data

### JSON

- Array of objects → digunakan langsung
- Single object → di-wrap otomatis jadi `[object]`
- **JSON Schema tidak diperlukan** — JSON sudah self-describing. Nilai `750` sudah `float64`, `"01"` sudah `string`, `[]` sudah array. Tidak ada ambiguitas tipe yang perlu diselesaikan oleh schema eksternal.

### XML — Konvensi Lossless

| Kasus | XML | Hasil Go |
|---|---|---|
| Pure text | `<Score>750</Score>` | `"Score": 750` |
| Leading zero | `<Code>01</Code>` | `"Code": "01"` (string) |
| ID panjang >15 digit | `<NIK>3201...</NIK>` | `"NIK": "3201..."` (string) |
| Boolean | `<Active>true</Active>` | `"Active": true` |
| Empty | `<Note/>` | `"Note": null` |
| Text + attribute | `<loanid status="active">123</loanid>` | `"loanid": {"@status":"active","#text":123}` |
| Attribute only | `<Status active="true"/>` | `"Status": {"@active":true}` |
| Tag muncul ≥2× | `<TL>…</TL><TL>…</TL>` | `"TL": [{…},{…}]` (array) |
| Tag muncul 1× | `<Score>…</Score>` | `"Score": {…}` (scalar) |

Single-item container (`<Scores><Score>…</Score></Scores>`) menghasilkan `Score` sebagai scalar bukan array — gunakan XSD hints untuk memaksanya jadi array.

### XSD Hints (opsional, untuk XML saja)

```bash
# Upload XML lalu XSD-nya
curl -X POST /api/upload -H "Content-Type: application/xml" --data-binary @data.xml
curl -X POST "/api/upload/xsd?dataset=uploaded_xxx" -H "Content-Type: application/xml" --data-binary @schema.xsd
```

XSD menyelesaikan dua masalah yang tidak bisa diselesaikan heuristik:
1. **Force-array single-item** — `maxOccurs="unbounded"` → selalu `[]interface{}` meski hanya 1 child
2. **Protect string fields** — `type="xs:string"` → tidak di-coerce ke number (mis. `ZipCode: "10001"` bukan `10001`)

---

## Desain Engine

### Transparant Auto-Selection

`Aggregate()` mendeteksi karakteristik DSL dan memilih code path optimal secara otomatis:

| Kondisi query | Path yang dipilih |
|---|---|
| Selalu | **PERF-01**: schema dari cache per-dataset |
| Selalu | **PERF-02**: path split (`.`) dari cache per-string |
| `groupBy` mengandung array field | **PERF-03**: COW explode (8.8× lebih cepat dari DeepCopy) |
| `localFilter` hadir | **PERF-04**: lazy copy — hanya record yang punya target array |

### Two-Pass Aggregation (Standard Mode)

Mencegah double-counting ketika query mencampur field parent dan field array:

```
Pass 1 — parent fields:   SUM(salary)  → nilai dari record langsung
Pass 2 — array fields:    SUM(credits.balance) → per record → combine antar group
```

### Nested Array (Multi-Level)

Path seperti `FasilitasList.Fasilitas.RiwayatKolektibilitas.HariTunggakan` di-detect otomatis dari schema. Engine melakukan recursive descent: outer array → per elemen → inner array → collect values → aggregate.

### Explode Mode

Ketika `groupBy` mengandung field dari dalam array (mis. `credits.product_type`), engine melakukan explode: setiap credit element menjadi satu flat row. Field parent di-deduplikasi sebelum diagregasi sehingga `AVG(salary)` tidak terhitung N× per credit.

### Weighted Average

`avg` menggunakan akumulator `(sum, count)` per record yang di-combine dengan benar antar group:

```
Alice: 1 credit  balance=0   → accum(0, 1)
Bob:   3 credits balance=100 → accum(300, 3)
Total avg = (0+300) / (1+3) = 75  ← benar, bukan mean-of-means (50)
```

---

## HTTP API

| Method | Endpoint | Keterangan |
|---|---|---|
| `GET` | `/api/datasets` | Daftar dataset |
| `GET` | `/api/schema?dataset=<name>` | FieldInfo dari dataset |
| `GET` | `/api/data?dataset=<name>` | Raw data untuk preview |
| `POST` | `/api/aggregate` | Eksekusi DSL |
| `POST` | `/api/upload` | Upload JSON atau XML (auto-detect) |
| `POST` | `/api/upload/xsd?dataset=<name>` | Upload XSD hints untuk dataset XML |
| `GET` | `/api/xsd` | Dataset yang punya XSD hints |
| `GET` | `/api/xsd/info?dataset=<name>` | Detail string/array paths dari XSD |

---

## Test Suite

```bash
go test ./...                                    # semua test
go test ./gator -bench=. -benchmem               # benchmark
go test ./gator -run TestMemory -v               # memory leak
go test ./gator -run TestLoad -race -v           # concurrency + race detector
go test ./gator -run "^Fuzz" -v                 # fuzz corpus regression
go test ./gator -fuzz=FuzzJSONIngest -fuzztime=60s  # fuzz aktif
```

| Kategori | File | Tests |
|---|---|---|
| Unit engine | `gator_test.go` | 22 |
| Optimasi transparan | `cache_test.go` | 6 |
| Unit ingest XML | `ingest/xml_test.go` | 14 |
| Unit XSD | `ingest/xsd_test.go` | 5 |
| End-to-end XML→Aggregate | `ingest/xml_aggregate_test.go` | 2 |
| Config | `config/config_test.go` | 3 |
| Benchmark | `bench_test.go` | 14 |
| Memory leak | `memleak_test.go` | 4 |
| Fuzz | `fuzz_test.go` | 5 targets |
| Load/concurrent | `load_test.go` | 4 |
| **Total** | | **58+ tests** |

---

## Catatan Desain

### Mengapa tidak ada JSON Schema?

JSON adalah format self-describing. `750` sudah `float64`, `"01"` sudah `string`, `[]` sudah array. Tidak ada ambiguitas tipe seperti di XML. JSON Schema hanya berguna untuk validasi struktur — yang sudah ditangani oleh `validateRequest()` yang menolak field yang resolve ke `array_object`.

### Mengapa XSD opsional, bukan wajib?

Engine bekerja dengan heuristik yang mencakup 90%+ kasus XML riil. XSD wajib akan memblokir penggunaan dengan format yang tidak punya XSD (mayoritas API kredit bureau Indonesia). Opsi B (opsional) adalah sweet spot: engine tetap bekerja tanpa XSD, dengan akurasi penuh jika XSD disediakan.

### Mengapa `avg` bukan mean-of-means?

Di konteks financial institution, mean-of-means memberikan bobot yang sama ke setiap nasabah tanpa memperhatikan berapa banyak fasilitas yang dimiliki. Nasabah dengan 1 kredit dan nasabah dengan 10 kredit diperlakukan setara. Ini tidak benar secara matematis dan bisa menghasilkan keputusan kredit yang salah.

### Mengapa explode mode perlu dedup parent fields?

Ketika array di-explode, setiap elemen membawa copy field parent. `AVG(salary)` tanpa dedup akan menghitung salary yang sama N kali (N = jumlah elemen array). Dedup mengumpulkan satu nilai salary per parent record sebelum aggregate — hasilnya benar.
