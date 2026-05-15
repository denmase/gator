package gator_test

// bench_test.go — benchmark suite untuk semua operasi aggregasi.
//
// Menjalankan:
//
//	# Semua benchmark (singkat)
//	go test ./gator/... -bench=. -benchmem -benchtime=3s
//
//	# Dengan CPU profile
//	go test ./gator/... -bench=BenchmarkAggregate -benchmem -cpuprofile=cpu.pprof
//	go tool pprof -text cpu.pprof
//
//	# Dengan memory profile
//	go test ./gator/... -bench=BenchmarkAggregate -benchmem -memprofile=mem.pprof
//	go tool pprof -text mem.pprof

import (
	"fmt"
	"testing"

	"aggregator/gator"
	"aggregator/gator/samples"
)

// ── Dataset generators ────────────────────────────────────────────────────────

func benchStore() *gator.Store {
	s := gator.NewStore()
	samples.Register(s)
	return s
}

// scaledStore generates N synthetic parent records each with M credit elements.
func scaledStore(n, m int) *gator.Store {
	s := gator.NewStore()
	data := make([]interface{}, n)
	for i := 0; i < n; i++ {
		credits := make([]interface{}, m)
		for j := 0; j < m; j++ {
			credits[j] = map[string]interface{}{
				"account_no":          fmt.Sprintf("ACC-%d-%d", i, j),
				"product_type":        []string{"credit card", "personal loan", "mortgage", "paylater"}[j%4],
				"outstanding_balance": float64((i*1000 + j) * 1_000_000),
				"loan_status":         []string{"active", "paid-off"}[j%2],
				"delinquency":         float64(j % 5 * 30),
				"last_24_coll_hist": func() []interface{} {
					h := make([]interface{}, 24)
					for k := range h {
						h[k] = float64(1 + (k+j)%5)
					}
					return h
				}(),
			}
		}
		data[i] = map[string]interface{}{
			"employee_id": fmt.Sprintf("EMP-%04d", i),
			"name":        fmt.Sprintf("Person%d", i),
			"department":  []string{"IT", "Finance", "Risk Management", "Marketing"}[i%4],
			"city":        []string{"Jakarta", "Bandung", "Surabaya"}[i%3],
			"salary":      float64(15_000_000 + i*500_000),
			"credits":     credits,
		}
	}
	s.Register("bench", data)
	return s
}

// ── Scalar aggregation benchmarks ────────────────────────────────────────────

func BenchmarkAggSumParent(b *testing.B) {
	store := benchStore()
	req := gator.AggregateRequest{
		Dataset:      "employees",
		Aggregations: []gator.AggConfig{{Field: "salary", Op: "sum", Alias: "total"}},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gator.Aggregate(store, req) //nolint
	}
}

func BenchmarkAggAvgParent(b *testing.B) {
	store := benchStore()
	req := gator.AggregateRequest{
		Dataset:      "employees",
		Aggregations: []gator.AggConfig{{Field: "salary", Op: "avg", Alias: "avg_sal"}},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gator.Aggregate(store, req) //nolint
	}
}

func BenchmarkAggGroupByParent(b *testing.B) {
	store := benchStore()
	req := gator.AggregateRequest{
		Dataset: "employees",
		GroupBy: []string{"department"},
		Aggregations: []gator.AggConfig{
			{Field: "salary", Op: "sum", Alias: "total_salary"},
			{Field: "salary", Op: "avg", Alias: "avg_salary"},
			{Field: "salary", Op: "count", Alias: "count_emp"},
		},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gator.Aggregate(store, req) //nolint
	}
}

// ── Array aggregation benchmarks ─────────────────────────────────────────────

func BenchmarkAggSumArray(b *testing.B) {
	store := benchStore()
	req := gator.AggregateRequest{
		Dataset:      "employees",
		Aggregations: []gator.AggConfig{{Field: "credits.outstanding_balance", Op: "sum", Alias: "total_os"}},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gator.Aggregate(store, req) //nolint
	}
}

func BenchmarkAggAvgArray(b *testing.B) {
	store := benchStore()
	req := gator.AggregateRequest{
		Dataset:      "employees",
		Aggregations: []gator.AggConfig{{Field: "credits.outstanding_balance", Op: "avg", Alias: "avg_os"}},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gator.Aggregate(store, req) //nolint
	}
}

func BenchmarkAggMixedParentArray(b *testing.B) {
	store := benchStore()
	req := gator.AggregateRequest{
		Dataset: "employees",
		GroupBy: []string{"department"},
		Aggregations: []gator.AggConfig{
			{Field: "salary", Op: "sum", Alias: "total_salary"},
			{Field: "credits.outstanding_balance", Op: "sum", Alias: "total_os"},
			{Field: "credits.outstanding_balance", Op: "avg", Alias: "avg_os"},
		},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gator.Aggregate(store, req) //nolint
	}
}

// ── Window operation benchmarks ───────────────────────────────────────────────

func BenchmarkAggWorstLastN(b *testing.B) {
	store := benchStore()
	req := gator.AggregateRequest{
		Dataset: "employees",
		GroupBy: []string{"name"},
		Aggregations: []gator.AggConfig{
			{Field: "credits.last_24_coll_hist", Op: "worst_last_n", Alias: "worst_6m",
				Params: map[string]interface{}{"n": 6.0}},
			{Field: "credits.last_24_coll_hist", Op: "worst_last_n", Alias: "worst_12m",
				Params: map[string]interface{}{"n": 12.0}},
			{Field: "credits.last_24_coll_hist", Op: "ever_has_last_n", Alias: "ever_bad",
				Params: map[string]interface{}{"n": 6.0, "value": 3.0}},
		},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gator.Aggregate(store, req) //nolint
	}
}

// ── Explode mode benchmark ────────────────────────────────────────────────────

func BenchmarkAggExplodeGroupByArrayField(b *testing.B) {
	store := benchStore()
	req := gator.AggregateRequest{
		Dataset: "employees",
		LocalFilter: map[string]map[string]map[string]interface{}{
			"credits": {"credits.loan_status": {"$eq": "active"}},
		},
		GroupBy: []string{"department", "credits.product_type"},
		Aggregations: []gator.AggConfig{
			{Field: "credits.outstanding_balance", Op: "sum", Alias: "total_os"},
			{Field: "credits.outstanding_balance", Op: "avg", Alias: "avg_os"},
			{Field: "credits.account_no", Op: "count", Alias: "count"},
		},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gator.Aggregate(store, req) //nolint
	}
}

// ── Local filter benchmark ────────────────────────────────────────────────────

func BenchmarkAggLocalFilter(b *testing.B) {
	store := benchStore()
	req := gator.AggregateRequest{
		Dataset: "employees",
		LocalFilter: map[string]map[string]map[string]interface{}{
			"credits": {"credits.loan_status": {"$eq": "active"}},
		},
		GroupBy: []string{"department"},
		Aggregations: []gator.AggConfig{
			{Field: "credits.outstanding_balance", Op: "sum", Alias: "active_os"},
		},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gator.Aggregate(store, req) //nolint
	}
}

// ── Scale benchmarks ─────────────────────────────────────────────────────────
// Menguji performa dengan dataset jauh lebih besar dari employees (9 records).

func BenchmarkAggScale100x5(b *testing.B) {
	benchmarkScale(b, 100, 5)
}
func BenchmarkAggScale1000x5(b *testing.B) {
	benchmarkScale(b, 1000, 5)
}
func BenchmarkAggScale1000x20(b *testing.B) {
	benchmarkScale(b, 1000, 20)
}
func BenchmarkAggScale5000x10(b *testing.B) {
	benchmarkScale(b, 5000, 10)
}

func benchmarkScale(b *testing.B, n, m int) {
	b.Helper()
	store := scaledStore(n, m)
	req := gator.AggregateRequest{
		Dataset: "bench",
		GroupBy: []string{"department"},
		Aggregations: []gator.AggConfig{
			{Field: "salary", Op: "sum", Alias: "total_salary"},
			{Field: "credits.outstanding_balance", Op: "sum", Alias: "total_os"},
			{Field: "credits.outstanding_balance", Op: "avg", Alias: "avg_os"},
			{Field: "credits.last_24_coll_hist", Op: "worst_last_n", Alias: "worst_coll",
				Params: map[string]interface{}{"n": 6.0}},
		},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gator.Aggregate(store, req) //nolint
	}
}

// BenchmarkAggScaleExplode menguji explode mode pada dataset besar.
func BenchmarkAggScaleExplode500x10(b *testing.B) {
	store := scaledStore(500, 10)
	req := gator.AggregateRequest{
		Dataset: "bench",
		GroupBy: []string{"department", "credits.product_type"},
		Aggregations: []gator.AggConfig{
			{Field: "credits.outstanding_balance", Op: "sum", Alias: "total_os"},
			{Field: "credits.outstanding_balance", Op: "avg", Alias: "avg_os"},
		},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gator.Aggregate(store, req) //nolint
	}
}

// ── Historical PERF benchmarks ──────────────────────────────────────────────
// The PERF-01/02/03/04 optimisations are now baked into Aggregate transparently.
// The benchmarks above measure the current (always-optimised) implementation.
// Historical before/after numbers are documented in PERF.md.
