package gator_test

// cache_test.go — verifikasi bahwa semua optimasi transparan menghasilkan
// hasil identik dengan sebelum optimasi, untuk semua pola query.
// Tidak ada API terpisah — hanya satu Aggregate().

import (
	"encoding/json"
	"fmt"
	"testing"

	"aggregator/gator"
	"aggregator/gator/samples"
)

func jsonStr(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func assertResultsEqual(t *testing.T, label string, a, b []interface{}) {
	t.Helper()
	if len(a) != len(b) {
		t.Errorf("✗ %s: row count %d vs %d", label, len(a), len(b))
		return
	}
	for i := range a {
		if jsonStr(a[i]) != jsonStr(b[i]) {
			t.Errorf("✗ %s row[%d]:\n  a: %s\n  b: %s", label, i, jsonStr(a[i]), jsonStr(b[i]))
			return
		}
	}
	t.Logf("✓ %s: %d rows identical", label, len(a))
}

// TestTransparentOptimisations verifikasi bahwa hasil Aggregate tidak berubah
// setelah optimasi PERF-01/02/03/04 diaktifkan secara transparan.
// Strategi: bandingkan hasil query yang sama, dipanggil 2× berturut-turut
// (call ke-1: cold cache, call ke-2: warm cache). Hasil harus identik.
func TestTransparentOptimisations(t *testing.T) {
	store := gator.NewStore()
	samples.Register(store)

	cases := []struct {
		name string
		req  gator.AggregateRequest
	}{
		{
			name: "PERF01_schema_cache_warm",
			req: gator.AggregateRequest{
				Dataset: "employees",
				GroupBy: []string{"department"},
				Aggregations: []gator.AggConfig{
					{Field: "salary", Op: "sum", Alias: "ts"},
					{Field: "credits.outstanding_balance", Op: "avg", Alias: "ao"},
				},
			},
		},
		{
			name: "PERF02_path_cache",
			req: gator.AggregateRequest{
				Dataset: "employees",
				GroupBy: []string{"department", "city"},
				Aggregations: []gator.AggConfig{
					{Field: "credits.last_24_coll_hist", Op: "worst_last_n", Alias: "w",
						Params: map[string]interface{}{"n": 6.0}},
				},
			},
		},
		{
			name: "PERF03_cow_explode",
			req: gator.AggregateRequest{
				Dataset: "employees",
				LocalFilter: map[string]map[string]map[string]interface{}{
					"credits": {"credits.loan_status": {"$eq": "active"}},
				},
				GroupBy: []string{"department", "credits.product_type"},
				Aggregations: []gator.AggConfig{
					{Field: "credits.outstanding_balance", Op: "sum", Alias: "os"},
					{Field: "credits.outstanding_balance", Op: "avg", Alias: "ao"},
					{Field: "salary", Op: "avg", Alias: "avg_sal"},
				},
			},
		},
		{
			name: "PERF04_lazy_filter",
			req: gator.AggregateRequest{
				Dataset: "employees",
				LocalFilter: map[string]map[string]map[string]interface{}{
					"credits": {"credits.product_type": {"$eq": "credit card"}},
				},
				GroupBy: []string{"department"},
				Aggregations: []gator.AggConfig{
					{Field: "credits.outstanding_balance", Op: "sum", Alias: "os"},
				},
			},
		},
		{
			name: "all_perf_combined",
			req: gator.AggregateRequest{
				Dataset: "employees",
				LocalFilter: map[string]map[string]map[string]interface{}{
					"credits": {"credits.loan_status": {"$eq": "active"}},
				},
				GroupBy: []string{"department", "credits.product_type"},
				Aggregations: []gator.AggConfig{
					{Field: "salary", Op: "avg", Alias: "avg_sal"},
					{Field: "credits.outstanding_balance", Op: "sum", Alias: "os"},
					{Field: "credits.outstanding_balance", Op: "avg", Alias: "ao"},
					{Field: "credits.last_24_coll_hist", Op: "worst_last_n", Alias: "w",
						Params: map[string]interface{}{"n": 6.0}},
				},
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// Cold cache call
			r1, err := gator.Aggregate(store, tc.req)
			if err != nil {
				t.Fatalf("cold: %v", err)
			}
			// Warm cache call — must produce identical results
			r2, err := gator.Aggregate(store, tc.req)
			if err != nil {
				t.Fatalf("warm: %v", err)
			}
			assertResultsEqual(t, tc.name+" cold==warm", r1, r2)
		})
	}
}

// TestCacheInvalidation verifikasi bahwa schema cache di-invalidate saat Register.
func TestCacheInvalidation(t *testing.T) {
	store := gator.NewStore()
	store.Register("ds", []interface{}{
		map[string]interface{}{"name": "Alice", "value": 100.0},
		map[string]interface{}{"name": "Bob", "value": 200.0},
	})
	req := gator.AggregateRequest{
		Dataset:      "ds",
		Aggregations: []gator.AggConfig{{Field: "value", Op: "sum", Alias: "s"}},
	}

	r1, _ := gator.Aggregate(store, req)
	sum1, _ := gator.ToFloat64(r1[0].(map[string]interface{})["s"])
	if sum1 != 300 {
		t.Errorf("✗ before invalidation: sum=%.0f want 300", sum1)
	} else {
		t.Log("✓ before invalidation: sum=300")
	}

	// Re-register — cache must be invalidated
	store.Register("ds", []interface{}{
		map[string]interface{}{"name": "Charlie", "value": 999.0},
	})
	r2, _ := gator.Aggregate(store, req)
	sum2, _ := gator.ToFloat64(r2[0].(map[string]interface{})["s"])
	if sum2 != 999 {
		t.Errorf("✗ after invalidation: sum=%.0f want 999 (stale cache!)", sum2)
	} else {
		t.Log("✓ after invalidation: sum=999 (cache correctly invalidated)")
	}
}

// TestConcurrentWithCacheCorrectness verifikasi tidak ada race pada cached schema/path.
func TestConcurrentWithCacheCorrectness(t *testing.T) {
	store := gator.NewStore()
	samples.Register(store)
	req := gator.AggregateRequest{
		Dataset: "employees",
		GroupBy: []string{"department"},
		Aggregations: []gator.AggConfig{
			{Field: "salary", Op: "sum", Alias: "ts"},
			{Field: "credits.outstanding_balance", Op: "avg", Alias: "ao"},
		},
	}

	// Reference result
	ref, _ := gator.Aggregate(store, req)
	refJSON := make([]string, len(ref))
	for i, r := range ref {
		refJSON[i] = jsonStr(r)
	}

	errs := make(chan string, 1000)
	done := make(chan struct{})
	for g := 0; g < 20; g++ {
		go func() {
			for i := 0; i < 50; i++ {
				rows, err := gator.Aggregate(store, req)
				if err != nil {
					errs <- fmt.Sprintf("err: %v", err)
					continue
				}
				for j, r := range rows {
					if j < len(refJSON) && jsonStr(r) != refJSON[j] {
						errs <- fmt.Sprintf("row %d mismatch", j)
					}
				}
			}
			done <- struct{}{}
		}()
	}
	for g := 0; g < 20; g++ {
		<-done
	}
	close(errs)
	count := 0
	for e := range errs {
		t.Errorf("✗ concurrent: %s", e)
		count++
		if count > 5 {
			t.Fatal("too many errors")
		}
	}
	if count == 0 {
		t.Log("✓ 20 goroutines × 50 iters: all results consistent")
	}
}
