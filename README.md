# Visual Aggregator - SQL-like JSON Aggregation

Sistem agregasi data JSON dengan kemampuan seperti SQL (MAX, MIN, AVG, SUM, COUNT, GROUP BY, WHERE) yang bersifat generic dan tidak bergantung pada skema JSON tertentu.

## 🚀 Cara Menjalankan

```bash
cd /workspace
go run main.go
```

Server akan berjalan di **http://localhost:8080**

## 📁 Struktur File

```
/workspace/
├── main.go              # Backend Go (standar library only)
├── static/
│   └── index.html       # Frontend HTML/CSS/JS (single file)
└── README.md            # Dokumentasi ini
```

## 🔧 Pendekatan Nested Array

### Strategi: **Flattening On-Demand**

Sistem menggunakan pendekatan **flattening** yang hanya dilakukan ketika query menyentuh field dari nested array. Berikut cara kerjanya:

1. **Deteksi Otomatis**: Backend mendeteksi apakah query mengakses field nested array (misalnya `credits.outstanding_balance`)
2. **Flattening Bersyarat**: Jika query menyentuh nested array, setiap elemen array akan "dipipihkan" menjadi baris terpisah yang menggabungkan data parent dengan data child
3. **Notasi Titik**: Field dalam nested array diakses dengan notasi titik, misalnya `credits.product_type`, `credits.last_24_delq_hist`

### Contoh Flattening

Data asli:
```json
{
  "name": "Andi",
  "department": "Sales",
  "credits": [
    {"account_no": "CC001", "product_type": "credit card", "outstanding_balance": 25000000},
    {"account_no": "PL001", "product_type": "personal loan", "outstanding_balance": 45000000}
  ]
}
```

Setelah flattening (untuk query yang menyentuh `credits.*`):
```
Row 1: {name: "Andi", department: "Sales", credits.account_no: "CC001", credits.product_type: "credit card", credits.outstanding_balance: 25000000}
Row 2: {name: "Andi", department: "Sales", credits.account_no: "PL001", credits.product_type: "personal loan", credits.outstanding_balance: 45000000}
```

### Keunggulan Pendekatan Ini

- **Efisien**: Flattening hanya terjadi saat diperlukan
- **Intuitif**: Pengguna menggunakan notasi titik seperti `credits.field_name`
- **Generic**: Dapat menangani nested array bertingkat apapun tanpa hardcode
- **Support Time-Series**: Array historis seperti `last_24_delq_hist` dapat dianalisis per bulan

## 📊 Dataset Contoh (Mock Data)

Dataset `employees` berisi 10 karyawan dengan struktur kompleks:

```go
type Employee struct {
    ID          int
    Name        string
    Department  string  // Sales, Engineering, Marketing, HR, Finance, Operations
    City        string  // Jakarta, Bandung, Surabaya, dll
    Age         int
    Salary      float64
    YearsOfWork int
    Credits     []CreditAccount  // Nested array!
}

type CreditAccount struct {
    AccountNo          string
    OpenDate           string
    InitialLimit       float64
    ProductType        string  // credit card, personal loan, mortgage, multipurpose, paylater
    OutstandingBalance float64
    LoanStatus         string  // active, paid-off, written-off, restructured
    CollectabilityCode int     // 1-5 (1 = lancar, 5 = macet total)
    Delinquency        int     // hari tunggakan
    Last24DelqHist     []int   // histori tunggakan 24 bulan
    Last24CollHist     []int   // histori collectability 24 bulan
}
```

### Aturan Collectability Code

| Code | Kategori    | Delinquency (hari) |
|------|-------------|-------------------|
| 1    | Lancar      | 0                 |
| 2    | Kurang Lancar | 1-90            |
| 3    | Diragukan   | 91-120            |
| 4    | Macet       | 121-180           |
| 5    | Macet Total | >180              |

## 🌐 API Endpoints

### GET /api/datasets
Daftar dataset yang tersedia.

```bash
curl http://localhost:8080/api/datasets
```

### GET /api/schema?dataset=employees
Mendapatkan skema field dari dataset.

```bash
curl "http://localhost:8080/api/schema?dataset=employees"
```

### POST /api/aggregate
Melakukan agregasi data.

```bash
curl -X POST http://localhost:8080/api/aggregate \
  -H "Content-Type: application/json" \
  -d '{
    "dataset": "employees",
    "where": {"credits.product_type": {"$eq": "credit card"}},
    "groupBy": ["city"],
    "aggregations": [
      {"field": "credits.initial_limit", "op": "sum", "alias": "total_limit"},
      {"field": "*", "op": "count", "alias": "credit_count"}
    ]
  }'
```

### POST /api/upload
Upload file JSON custom.

```bash
curl -X POST -F "file=@data.json" http://localhost:8080/api/upload
```

## 📝 Format Request Aggregate

```json
{
  "dataset": "nama_dataset",
  "data": [],  // opsional: kirim data langsung
  "where": {
    "field_name": {"$operator": value}
  },
  "groupBy": ["field1", "field2"],
  "aggregations": [
    {"field": "field_name", "op": "sum|avg|min|max|count", "alias": "nama_alias"}
  ]
}
```

### Operator WHERE

| Operator | Deskripsi | Contoh |
|----------|-----------|--------|
| `$eq`    | Sama dengan | `{"age": {"$eq": 30}}` |
| `$ne`    | Tidak sama dengan | `{"status": {"$ne": "active"}}` |
| `$gt`    | Lebih besar dari | `{"salary": {"$gt": 10000000}}` |
| `$gte`   | Lebih besar atau sama | `{"age": {"$gte": 25}}` |
| `$lt`    | Lebih kecil dari | `{"balance": {"$lt": 1000000}}` |
| `$lte`   | Lebih kecil atau sama | `{"delinquency": {"$lte": 30}}` |
| `$in`    | Dalam list | `{"code": {"$in": [3,4,5]}}` |

### Fungsi Agregasi

| Fungsi | Deskripsi | Contoh |
|--------|-----------|--------|
| `count` | Jumlah baris | `{"field": "*", "op": "count"}` |
| `sum`   | Total nilai | `{"field": "salary", "op": "sum"}` |
| `avg`   | Rata-rata | `{"field": "balance", "op": "avg"}` |
| `min`   | Nilai minimum | `{"field": "age", "op": "min"}` |
| `max`   | Nilai maksimum | `{"field": "delinquency", "op": "max"}` |

## 🎯 Contoh Query

### 1. Rata-rata Outstanding Balance per Departemen

```json
{
  "groupBy": ["department"],
  "aggregations": [
    {"field": "credits.outstanding_balance", "op": "avg", "alias": "avg_outstanding"}
  ]
}
```

### 2. Total Limit Kredit per Kota (Khusus Credit Card)

```json
{
  "where": {"credits.product_type": {"$eq": "credit card"}},
  "groupBy": ["city"],
  "aggregations": [
    {"field": "credits.initial_limit", "op": "sum", "alias": "total_limit"},
    {"field": "*", "op": "count", "alias": "credit_count"}
  ]
}
```

### 3. Maximum Delinquency Overall

```json
{
  "aggregations": [
    {"field": "credits.delinquency", "op": "max", "alias": "max_delinquency"}
  ]
}
```

### 4. Worst Delinquency in Last 6 Months

Untuk query time-series pada array `last_24_delq_hist`:

```json
{
  "groupBy": ["department"],
  "aggregations": [
    {
      "field": "credits.last_24_delq_hist",
      "op": "max",
      "alias": "worst_delq_last_6_months",
      "special": true,
      "months": 6
    }
  ]
}
```

### 5. Max Collectability Code in Last 3 Months

```json
{
  "aggregations": [
    {
      "field": "credits.last_24_coll_hist",
      "op": "max",
      "alias": "max_coll_last_3_months",
      "special": true,
      "months": 3
    }
  ]
}
```

### 6. Kredit dengan Collectability Code Buruk (3, 4, atau 5)

```json
{
  "where": {"credits.collectability_code": {"$in": [3, 4, 5]}},
  "aggregations": [
    {"field": "*", "op": "count", "alias": "bad_credit_count"},
    {"field": "credits.outstanding_balance", "op": "sum", "alias": "total_exposure"}
  ]
}
```

### 7. Statistik Kredit per Product Type

```json
{
  "groupBy": ["credits.product_type"],
  "aggregations": [
    {"field": "*", "op": "count", "alias": "account_count"},
    {"field": "credits.outstanding_balance", "op": "avg", "alias": "avg_balance"},
    {"field": "credits.delinquency", "op": "max", "alias": "max_delinquency"}
  ]
}
```

### 8. Jumlah Akun Dibuka dalam 3 Bulan Terakhir

```json
{
  "where": {"credits.open_date": {"$gte": "2024-09-01"}},
  "aggregations": [
    {"field": "*", "op": "count", "alias": "new_accounts_count"}
  ]
}
```

## 🎨 Fitur Frontend

Frontend single-page application dengan fitur:

1. **Visual Query Builder**
   - Dropdown filter dengan operator lengkap
   - Multiple GROUP BY fields
   - Multiple aggregations dengan alias custom

2. **DSL Preview**
   - Tampilan real-time JSON query yang akan dikirim

3. **Special Time-Series Aggregations**
   - Analisis histori delinquency N bulan terakhir
   - Analisis histori collectability N bulan terakhir

4. **Example Queries**
   - 8 contoh query siap pakai yang bisa diklik untuk langsung dieksekusi

5. **Upload Custom JSON**
   - Support upload file JSON sendiri

6. **Responsive Design**
   - Tampilan modern dan responsif

## 🔍 Penjelasan Teknis

### Schema Detection

Backend secara otomatis mendeteksi skema dari sample data pertama menggunakan reflection Go. Field dari nested array diekspos dengan notasi titik.

### Filtering Engine

Filter diterapkan sebelum grouping dan agregasi. Multiple conditions menggunakan AND logic.

### Grouping & Aggregation

1. Data difilter berdasarkan WHERE clause
2. Data dikelompokkan berdasarkan GROUP BY fields
3. Fungsi agregasi dihitung per group
4. Hasil diurutkan berdasarkan key group untuk konsistensi

### Special Time-Series Handling

Untuk array historis (last_24_delq_hist, last_24_coll_hist):
- Backend mengambil N elemen terakhir dari array
- Menghitung agregasi (max, avg, dll) pada subset tersebut
- Mendukung query seperti "worst delinquency in last 6 months"

## 📄 License

Free to use and modify.
