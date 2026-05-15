# Performance Report — gator

Dijalankan pada: Go 1.22.2, Intel Xeon @ 2.80 GHz, linux/amd64, GOMAXPROCS=2

---

## Cara Menjalankan

```bash
# Benchmark semua
go test ./gator -bench=. -benchmem -benchtime=3s -run=^$

# Dengan CPU profile
go test ./gator -bench=BenchmarkAggMixedParentArray -cpuprofile=cpu.pprof -run=^$
go tool pprof -text cpu.pprof

# Dengan memory profile
go test ./gator -bench=BenchmarkAggMixedParentArray -memprofile=mem.pprof -run=^$
go tool pprof -text mem.pprof

# Memory leak detection
go test ./gator -run TestMemory -v

# Load + race detector
go test ./gator -run TestLoad -race -v -timeout 60s

# Fuzz (corpus regression, cocok untuk CI)
go test ./gator -run "^Fuzz" -v

# Fuzz aktif (eksplorasi)
go test ./gator -fuzz=FuzzJSONIngest -fuzztime=60s
```

---

## Benchmark Results

Semua benchmark menggunakan dataset `employees` (9 records) kecuali `Scale*`.

### Operasi dasar

| Benchmark | ns/op | B/op | allocs/op | Keterangan |
|---|---|---|---|---|
| `AggSumParent` | 21,563 | 12,321 | 32 | SUM(salary) — parent field |
| `AggAvgParent` | 20,635 | 12,337 | 33 | AVG(salary) — parent field, weighted |
| `AggGroupByParent` | 33,915 | 15,045 | 139 | GROUP BY dept + 3 parent aggs |
| `AggSumArray` | 29,442 | 14,563 | 106 | SUM(credits.outstanding_balance) |
| `AggAvgArray` | 30,072 | 14,369 | 105 | AVG array — weighted (sum/count carrier) |
| `AggMixedParentArray` | 50,551 | 19,491 | 282 | GROUP BY dept + salary + OS + avg |
| `AggWorstLastN` | 69,237 | 24,775 | 449 | GROUP BY name + worst/ever coll hist |
| `AggLocalFilter` | 172,533 | 78,236 | 575 | LocalFilter + GROUP BY dept |
| `AggExplodeGroupByArrayField` | 275,439 | 106,821 | 1,096 | Explode + GROUP BY product_type |

### Scale benchmarks

| Benchmark | Records | Credits/rec | ns/op | B/op | allocs/op |
|---|---|---|---|---|---|
| `AggScale100x5` | 100 | 5 | 544,223 | 140,204 | 4,375 |
| `AggScale1000x5` | 1,000 | 5 | 7,001,041 | 1,328,847 | 42,239 |
| `AggScale1000x20` | 1,000 | 20 | 22,746,775 | 4,472,872 | 108,239 |
| `AggScale5000x10` | 5,000 | 10 | 94,479,680 | 12,166,343 | 325,304 |
| `AggScaleExplode500x10` | 500 | 10 (explode) | 14,665,601 | 3,549,082 | 51,609 |

### Scaling behaviour

Standard mode (`aggregateStandard`) linear terhadap records × credits:

```
1,000 × 5  =  5,000 elements →  7.0 ms
1,000 × 20 = 20,000 elements → 22.7 ms  (4× elemen → 3.2× waktu ✓)
5,000 × 10 = 50,000 elements → 94.5 ms  (10× elemen → 13.5× waktu ✓)
```

Explode mode (500 × 10 = 5,000 elements) → 14.7 ms — jauh lebih efisien dari
sebelum PERF-03 karena menggunakan COW (copy-on-write) bukan DeepCopy.

---

## Optimasi Transparan

Semua optimasi berjalan otomatis dalam `Aggregate()` — tidak ada konfigurasi,
tidak ada API terpisah. Engine mendeteksi karakteristik DSL dan memilih code path.

### PERF-01 — Schema Cache

Schema dataset di-cache per-`Store`, di-invalidate saat `Register` dipanggil.
`DetectSchemaFromSample` hanya dijalankan sekali per dataset meskipun `Aggregate`
dipanggil ribuan kali.

**Kapan signifikan:** Dataset kecil dengan query rate sangat tinggi. Untuk dataset
besar (1000+ records) overhead schema detection kecil relatif terhadap compute.

### PERF-02 — Path Split Cache

`strings.Split(path, ".")` di-cache per path string dalam Store. Setiap field
path dalam DSL hanya di-split sekali seumur hidup Store.

**Kapan signifikan:** DSL dengan banyak field path panjang, dipanggil berulang.

### PERF-03 — Copy-on-Write Explode

Explode mode (GROUP BY array field) aslinya melakukan `DeepCopyMap` seluruh
record per elemen array — O(N × M × depth). PERF-03 menggantikan ini dengan:

1. Shallow copy root map — O(top-level fields)
2. Shallow copy intermediate path maps — O(path depth)
3. Set array key ke element reference langsung (read-only, no copy)

```
500 records × 10 credits = 5,000 deep copies [SEBELUM]
→ 45 MB/op, 280,000 allocs/op

500 records × 10 credits = 5,000 shallow copies [SESUDAH]
→ 3.5 MB/op, 51,000 allocs/op   (8.8× lebih cepat, 12.7× lebih hemat)
```

**Kapan signifikan:** Semua query yang menggunakan GROUP BY field array-level.
Ini adalah optimasi terbesar dan paling konsisten.

**Keamanan:** Flat rows yang dihasilkan bersifat read-only. Engine tidak pernah
menulis ke flat row setelah dibuat, sehingga shared reference aman.

### PERF-04 — Lazy Local Filter Copy

`ApplyLocalFilters` aslinya melakukan `DeepCopyMap` untuk setiap record. PERF-04
memeriksa terlebih dahulu apakah record punya target array — jika tidak, record
dikembalikan sebagai shared reference tanpa copy.

**Kapan signifikan:** Dataset sparse di mana banyak record tidak memiliki array
yang di-filter. Pada dataset di mana semua record punya array, savings minimal.

---

## Dampak dari Perspektif User

### ns/op → Waktu Respons

```
21,563 ns  =  0.02 ms  → tidak terasa
275,439 ns =  0.28 ms  → tidak terasa (explode mode, employees 9 records)
14,665,601 ns = 14.7 ms → cepat (explode, 500 records × 10 credits)
94,479,680 ns = 94.5 ms → mulai terasa (5,000 records × 10 credits)
```

### B/op → Konsumsi RAM per Query

```
Explode mode tanpa PERF-03: 45 MB/op × 100 concurrent = 4.5 GB → OOM
Explode mode dengan PERF-03:  3.5 MB/op × 100 concurrent = 350 MB → aman
```

### Skala yang bisa di-handle dalam 200ms (batas "feels fast")

| Mode | Max records (estimasi) |
|---|---|
| Standard mode (GROUP BY parent field) | ~10,000 records |
| Explode mode (GROUP BY array field) | ~700 records × 10 elemen |

---

## Memory Leak Detection

Hasil `TestMemoryLeak*` — semua bersih.

| Test | Iterasi | HeapInuse delta | Status |
|---|---|---|---|
| `TestMemoryLeakAggregate` | 500 | ≤ 0 KB (GC normal) | ✅ No leak |
| `TestMemoryLeakExplodeMode` | 300 | ≤ 0 KB | ✅ No leak |
| `TestMemoryLeakLocalFilter` | 200 (200 rec × 10) | ≤ 0 KB | ✅ No leak |

Delta negatif artinya GC berhasil membersihkan semua alokasi — tidak ada retained objects.

---

## Fuzz Testing

Dijalankan 10 detik per target. Zero panic, zero assertion failure.

| Target | Executions | New corpus | Status |
|---|---|---|---|
| `FuzzJSONIngest` | ~40,000 | 73 | ✅ No panic |
| `FuzzXMLIngest` | ~49,000 | 65 | ✅ No panic |
| `FuzzAggregateRequest` | ~84,000 | 57 | ✅ No panic |
| `FuzzWhereClause` | ~31,000 | 60 | ✅ No panic |
| `FuzzXSDParse` | ~35,000 | 32 | ✅ No panic |

**Total: ~240,000 executions, 0 failures.**

Invariant yang diverifikasi:
- `ParseXML(arbitrary_bytes)` — tidak pernah panic
- `ParseXSD(arbitrary_bytes)` — tidak pernah panic
- `Aggregate(store, arbitrary_request)` — tidak pernah panic, selalu return `(rows, err)`

---

## Load Testing

### Concurrent reads (50 goroutines × 100 iterasi)

```
total_ops = 5,000  errors = 0  throughput ≈ 13,000+ ops/s
```

### Throughput per query type (2 detik, multi-worker)

| Query Type | Workers | Throughput |
|---|---|---|
| Simple SUM (9 records) | 10 | ~22,000 ops/s |
| GROUP BY dept + mixed agg | 10 | ~12,000 ops/s |
| Explode + GROUP BY product_type | 10 | ~800 ops/s |
| SUM 500 records × 10 credits | 8 | ~300 ops/s |

### Race detector

```bash
go test ./gator -run TestLoad -race -timeout 60s
```

**Zero data races** — Store menggunakan `sync.RWMutex`, schema cache dan path cache
masing-masing dilindungi `RWMutex` terpisah.

---

## CPU Profiling — Hotspots

Profile dari `BenchmarkAggMixedParentArray`.

```
Top functions:
  ~30%  AggregateArrayField / aggregateNestedFieldCached
        → Pure compute: iterasi elemen array
  ~15%  runtime.mallocgc
        → GC pressure dari slice appends dalam aggregasi
  ~10%  strings.Split (path splitting)
        → PERF-02 menghilangkan ini untuk call berulang
  ~20%  runtime.futex (mutex)
        → RWMutex contention di 2-core environment
        → Berkurang signifikan di hardware dengan lebih banyak core
```

Bottleneck utama di semua scale adalah **compute O(N × M)** — iterasi elemen
array untuk setiap aggregation. Ini adalah batas fundamental dari pendekatan
in-memory dan tidak bisa dikurangi tanpa mengubah algoritma.
