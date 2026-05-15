package gator_test

// load_test.go — load testing: concurrent reads, writes, dan mixed workloads.
//
// Menjalankan:
//
//	go test ./gator/... -run TestLoad -v -count=1
//	go test ./gator/... -run TestLoad -race -v   # dengan race detector

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"aggregator/gator"
	"aggregator/gator/samples"
)

// ── Concurrent read safety ────────────────────────────────────────────────────

// TestLoadConcurrentReads verifikasi bahwa concurrent Aggregate calls terhadap
// dataset yang sama tidak menyebabkan data race atau panic.
func TestLoadConcurrentReads(t *testing.T) {
	store := gator.NewStore()
	samples.Register(store)

	req := gator.AggregateRequest{
		Dataset: "employees",
		GroupBy: []string{"department"},
		Aggregations: []gator.AggConfig{
			{Field: "salary", Op: "sum", Alias: "total_salary"},
			{Field: "credits.outstanding_balance", Op: "sum", Alias: "total_os"},
			{Field: "credits.outstanding_balance", Op: "avg", Alias: "avg_os"},
		},
	}

	const goroutines = 50
	const itersPerGoroutine = 100

	var wg sync.WaitGroup
	var errCount atomic.Int64
	var totalOps atomic.Int64

	start := time.Now()
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < itersPerGoroutine; i++ {
				rows, err := gator.Aggregate(store, req)
				if err != nil {
					errCount.Add(1)
					return
				}
				if len(rows) == 0 {
					errCount.Add(1)
					return
				}
				totalOps.Add(1)
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	total := totalOps.Load()
	errs := errCount.Load()
	throughput := float64(total) / elapsed.Seconds()

	t.Logf("concurrent reads: goroutines=%d iters=%d total_ops=%d errors=%d elapsed=%s throughput=%.0f ops/s",
		goroutines, itersPerGoroutine, total, errs, elapsed.Round(time.Millisecond), throughput)

	if errs > 0 {
		t.Errorf("✗ %d errors in concurrent reads", errs)
	} else {
		t.Logf("✓ %d concurrent read ops, 0 errors", total)
	}
}

// ── Concurrent read + write (upload) safety ────────────────────────────────

// TestLoadConcurrentReadWrite menguji thread-safety dengan simultaneous
// Register (write) dan Aggregate (read) ke Store yang sama.
func TestLoadConcurrentReadWrite(t *testing.T) {
	store := gator.NewStore()
	samples.Register(store)

	req := gator.AggregateRequest{
		Dataset:      "employees",
		Aggregations: []gator.AggConfig{{Field: "salary", Op: "sum", Alias: "s"}},
	}

	var wg sync.WaitGroup
	var readOps, writeOps atomic.Int64
	var readErrors, writeErrors atomic.Int64

	// Readers
	for g := 0; g < 20; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				rows, err := gator.Aggregate(store, req)
				if err != nil || rows == nil {
					readErrors.Add(1)
					return
				}
				readOps.Add(1)
			}
		}()
	}

	// Writers (simulate concurrent uploads)
	for g := 0; g < 5; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				name := fmt.Sprintf("upload_%d_%d", id, i)
				data := []interface{}{
					map[string]interface{}{
						"name":   name,
						"salary": float64(id * 1_000_000),
					},
				}
				store.Register(name, data)
				writeOps.Add(1)
			}
		}(g)
	}

	// Concurrent Names() reads
	for g := 0; g < 5; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				names := store.Names()
				_ = names
				readOps.Add(1)
			}
		}()
	}

	wg.Wait()

	t.Logf("reads=%d writes=%d readErrors=%d writeErrors=%d",
		readOps.Load(), writeOps.Load(), readErrors.Load(), writeErrors.Load())

	if readErrors.Load() > 0 || writeErrors.Load() > 0 {
		t.Errorf("✗ errors during concurrent read+write: reads=%d writes=%d",
			readErrors.Load(), writeErrors.Load())
	} else {
		t.Logf("✓ concurrent read+write safe: %d reads, %d writes",
			readOps.Load(), writeOps.Load())
	}
}

// ── Throughput measurement ────────────────────────────────────────────────────

// TestLoadThroughput mengukur throughput agregasi dalam operasi per detik
// untuk beberapa pola query yang berbeda.
func TestLoadThroughput(t *testing.T) {
	store := gator.NewStore()
	samples.Register(store)
	storeScale := scaledStore(500, 10)

	cases := []struct {
		name    string
		store   *gator.Store
		req     gator.AggregateRequest
		workers int
	}{
		{
			name:    "simple_sum_9records",
			store:   store,
			workers: 10,
			req: gator.AggregateRequest{
				Dataset:      "employees",
				Aggregations: []gator.AggConfig{{Field: "salary", Op: "sum", Alias: "s"}},
			},
		},
		{
			name:    "group_by_dept_mixed_9records",
			store:   store,
			workers: 10,
			req: gator.AggregateRequest{
				Dataset: "employees",
				GroupBy: []string{"department"},
				Aggregations: []gator.AggConfig{
					{Field: "salary", Op: "sum", Alias: "ts"},
					{Field: "credits.outstanding_balance", Op: "avg", Alias: "ao"},
					{Field: "credits.last_24_coll_hist", Op: "worst_last_n", Alias: "w",
						Params: map[string]interface{}{"n": 6.0}},
				},
			},
		},
		{
			name:    "explode_group_by_product_9records",
			store:   store,
			workers: 10,
			req: gator.AggregateRequest{
				Dataset: "employees",
				LocalFilter: map[string]map[string]map[string]interface{}{
					"credits": {"credits.loan_status": {"$eq": "active"}},
				},
				GroupBy: []string{"department", "credits.product_type"},
				Aggregations: []gator.AggConfig{
					{Field: "credits.outstanding_balance", Op: "sum", Alias: "os"},
					{Field: "credits.outstanding_balance", Op: "avg", Alias: "ao"},
				},
			},
		},
		{
			name:    "sum_500records_10credits",
			store:   storeScale,
			workers: 8,
			req: gator.AggregateRequest{
				Dataset: "bench",
				GroupBy: []string{"department"},
				Aggregations: []gator.AggConfig{
					{Field: "salary", Op: "sum", Alias: "ts"},
					{Field: "credits.outstanding_balance", Op: "sum", Alias: "os"},
					{Field: "credits.outstanding_balance", Op: "avg", Alias: "ao"},
				},
			},
		},
	}

	const duration = 2 * time.Second

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var ops atomic.Int64
			var errs atomic.Int64

			ctx, cancel := time.AfterFunc(duration, func() {}), func() {}
			_ = ctx
			cancel()

			done := make(chan struct{})
			time.AfterFunc(duration, func() { close(done) })

			var wg sync.WaitGroup
			for w := 0; w < tc.workers; w++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for {
						select {
						case <-done:
							return
						default:
							rows, err := gator.Aggregate(tc.store, tc.req)
							if err != nil {
								errs.Add(1)
							} else {
								_ = rows
								ops.Add(1)
							}
						}
					}
				}()
			}
			wg.Wait()

			total := ops.Load()
			opsPerSec := float64(total) / duration.Seconds()
			t.Logf("%-45s  workers=%d  ops=%d  errs=%d  throughput=%.0f ops/s",
				tc.name, tc.workers, total, errs.Load(), opsPerSec)

			if errs.Load() > 0 {
				t.Errorf("✗ %d errors during load", errs.Load())
			}
			// Soft minimum: at least 100 ops for any case (sanity check only)
			if total < 100 {
				t.Errorf("✗ suspiciously low ops count: %d in %s", total, duration)
			}
		})
	}
}

// ── Correctness under concurrency ─────────────────────────────────────────────

// TestLoadResultConsistency verifikasi bahwa hasil aggregasi konsisten
// ketika dipanggil concurrent dari banyak goroutine — tidak ada race yang
// menyebabkan nilai berbeda untuk query yang sama.
func TestLoadResultConsistency(t *testing.T) {
	store := gator.NewStore()
	samples.Register(store)

	req := gator.AggregateRequest{
		Dataset: "employees",
		GroupBy: []string{"department"},
		Aggregations: []gator.AggConfig{
			{Field: "salary", Op: "sum", Alias: "total_salary"},
			{Field: "credits.outstanding_balance", Op: "sum", Alias: "total_os"},
			{Field: "credits.outstanding_balance", Op: "avg", Alias: "avg_os"},
		},
	}

	// Get reference result from single-threaded call
	refRows, err := gator.Aggregate(store, req)
	if err != nil {
		t.Fatalf("reference aggregate: %v", err)
	}
	// Build reference map: dept → total_salary
	refSalary := map[string]float64{}
	refOS := map[string]float64{}
	for _, r := range refRows {
		row := r.(map[string]interface{})
		dept := fmt.Sprintf("%v", row["department"])
		if v, ok := gator.ToFloat64(row["total_salary"]); ok {
			refSalary[dept] = v
		}
		if v, ok := gator.ToFloat64(row["total_os"]); ok {
			refOS[dept] = v
		}
	}

	const goroutines = 30
	const iters = 50
	var inconsistencies atomic.Int64

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				rows, err := gator.Aggregate(store, req)
				if err != nil {
					inconsistencies.Add(1)
					return
				}
				for _, r := range rows {
					row := r.(map[string]interface{})
					dept := fmt.Sprintf("%v", row["department"])
					if v, ok := gator.ToFloat64(row["total_salary"]); ok {
						if ref, has := refSalary[dept]; has && v != ref {
							inconsistencies.Add(1)
						}
					}
					if v, ok := gator.ToFloat64(row["total_os"]); ok {
						if ref, has := refOS[dept]; has && v != ref {
							inconsistencies.Add(1)
						}
					}
				}
			}
		}()
	}
	wg.Wait()

	inc := inconsistencies.Load()
	if inc > 0 {
		t.Errorf("✗ %d inconsistent results under concurrency (race condition?)", inc)
	} else {
		t.Logf("✓ results consistent across %d goroutines × %d iterations",
			goroutines, iters)
	}
}
