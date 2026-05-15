package gator_test

// memleak_test.go — memory leak detection.
//
// Strategi: jalankan aggregasi berulang-ulang dalam satu goroutine, paksa GC
// sebelum dan sesudah, bandingkan heap allocation.  Leak sejati akan terlihat
// sebagai pertumbuhan HeapInuse yang terus naik meski GC sudah berjalan.
//
// Menjalankan:
//
//	go test ./gator/... -run TestMemory -v -count=1
//	go test ./gator/... -run TestMemory -memprofile=mem.pprof -v
//	go tool pprof -top mem.pprof

import (
	"runtime"
	"runtime/debug"
	"testing"

	"aggregator/gator"
)

// gcAndRead memaksa full GC dua kali (double-GC pattern) lalu membaca MemStats.
// Double-GC diperlukan karena finalizer berjalan di GC pertama tapi objek baru
// dibebaskan di GC kedua.
func gcAndRead() runtime.MemStats {
	runtime.GC()
	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return ms
}

// TestMemoryLeakAggregate menguji bahwa heap tidak tumbuh secara signifikan
// setelah menjalankan 500 aggregasi berulang.  Threshold 10MB dipilih cukup
// longgar untuk variasi GC normal, tapi cukup ketat untuk mendeteksi leak nyata.
func TestMemoryLeakAggregate(t *testing.T) {
	store := newStore()
	req := gator.AggregateRequest{
		Dataset: "employees",
		GroupBy: []string{"department"},
		Aggregations: []gator.AggConfig{
			{Field: "salary", Op: "sum", Alias: "total_salary"},
			{Field: "credits.outstanding_balance", Op: "sum", Alias: "total_os"},
			{Field: "credits.outstanding_balance", Op: "avg", Alias: "avg_os"},
			{Field: "credits.last_24_coll_hist", Op: "worst_last_n", Alias: "worst_coll",
				Params: map[string]interface{}{"n": 6.0}},
		},
	}

	// Warmup: biarkan Go runtime mencapai steady-state dulu.
	for i := 0; i < 50; i++ {
		gator.Aggregate(store, req) //nolint
	}

	before := gcAndRead()
	const iterations = 500

	for i := 0; i < iterations; i++ {
		rows, err := gator.Aggregate(store, req)
		if err != nil {
			t.Fatalf("Aggregate error: %v", err)
		}
		_ = rows // ensure result is used, prevents compiler elision
	}

	after := gcAndRead()

	heapGrowth := int64(after.HeapInuse) - int64(before.HeapInuse)
	allocGrowth := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	const leakThreshold = 10 * 1024 * 1024 // 10 MB

	t.Logf("before: HeapInuse=%d KB  HeapAlloc=%d KB  Sys=%d KB",
		before.HeapInuse/1024, before.HeapAlloc/1024, before.Sys/1024)
	t.Logf("after:  HeapInuse=%d KB  HeapAlloc=%d KB  Sys=%d KB",
		after.HeapInuse/1024, after.HeapAlloc/1024, after.Sys/1024)
	t.Logf("delta:  HeapInuse=%+d KB  HeapAlloc=%+d KB",
		heapGrowth/1024, allocGrowth/1024)
	t.Logf("total allocs during test: %d  TotalAlloc growth: %d KB",
		after.Mallocs-before.Mallocs, int64(after.TotalAlloc-before.TotalAlloc)/1024)

	if heapGrowth > leakThreshold {
		t.Errorf("✗ potential memory leak: HeapInuse grew by %d KB after %d iterations (threshold %d KB)",
			heapGrowth/1024, iterations, leakThreshold/1024)
	} else {
		t.Logf("✓ no leak detected: HeapInuse delta = %+d KB (threshold %d KB)",
			heapGrowth/1024, leakThreshold/1024)
	}
}

// TestMemoryLeakExplodeMode menguji explode mode yang membuat lebih banyak
// alokasi (DeepCopyMap per element) untuk leak.
func TestMemoryLeakExplodeMode(t *testing.T) {
	store := newStore()
	req := gator.AggregateRequest{
		Dataset: "employees",
		LocalFilter: map[string]map[string]map[string]interface{}{
			"credits": {"credits.loan_status": {"$eq": "active"}},
		},
		GroupBy: []string{"department", "credits.product_type"},
		Aggregations: []gator.AggConfig{
			{Field: "credits.outstanding_balance", Op: "sum", Alias: "total_os"},
			{Field: "credits.outstanding_balance", Op: "avg", Alias: "avg_os"},
			{Field: "salary", Op: "avg", Alias: "avg_salary"},
		},
	}

	for i := 0; i < 50; i++ {
		gator.Aggregate(store, req) //nolint
	}

	before := gcAndRead()
	const iterations = 300

	for i := 0; i < iterations; i++ {
		rows, err := gator.Aggregate(store, req)
		if err != nil {
			t.Fatalf("Aggregate error: %v", err)
		}
		_ = rows
	}

	after := gcAndRead()

	heapGrowth := int64(after.HeapInuse) - int64(before.HeapInuse)
	const leakThreshold = 15 * 1024 * 1024 // 15 MB — explode mode allocates more

	t.Logf("explode mode delta: HeapInuse=%+d KB  HeapAlloc=%+d KB",
		heapGrowth/1024, (int64(after.HeapAlloc)-int64(before.HeapAlloc))/1024)

	if heapGrowth > leakThreshold {
		t.Errorf("✗ potential leak in explode mode: %d KB growth (threshold %d KB)",
			heapGrowth/1024, leakThreshold/1024)
	} else {
		t.Logf("✓ no leak in explode mode: %+d KB (threshold %d KB)",
			heapGrowth/1024, leakThreshold/1024)
	}
}

// TestMemoryLeakLocalFilter menguji ApplyLocalFilters yang melakukan DeepCopyMap
// per record setiap kali dipanggil.
func TestMemoryLeakLocalFilter(t *testing.T) {
	store := scaledStore(200, 10)
	req := gator.AggregateRequest{
		Dataset: "bench",
		LocalFilter: map[string]map[string]map[string]interface{}{
			"credits": {"credits.loan_status": {"$eq": "active"}},
		},
		GroupBy: []string{"department"},
		Aggregations: []gator.AggConfig{
			{Field: "credits.outstanding_balance", Op: "sum", Alias: "os"},
		},
	}

	for i := 0; i < 20; i++ {
		gator.Aggregate(store, req) //nolint
	}

	before := gcAndRead()
	const iterations = 200

	for i := 0; i < iterations; i++ {
		rows, err := gator.Aggregate(store, req)
		if err != nil {
			t.Fatalf("Aggregate error: %v", err)
		}
		_ = rows
	}

	after := gcAndRead()

	heapGrowth := int64(after.HeapInuse) - int64(before.HeapInuse)
	const leakThreshold = 20 * 1024 * 1024

	t.Logf("localFilter 200rec×10credits delta: HeapInuse=%+d KB",
		heapGrowth/1024)

	if heapGrowth > leakThreshold {
		t.Errorf("✗ potential leak with localFilter: %d KB (threshold %d KB)",
			heapGrowth/1024, leakThreshold/1024)
	} else {
		t.Logf("✓ no leak with localFilter: %+d KB (threshold %d KB)",
			heapGrowth/1024, leakThreshold/1024)
	}
}

// TestMemoryAllocsPerOp melaporkan jumlah alokasi per operasi aggregasi
// menggunakan testing.AllocsPerRun.  Berguna sebagai early warning ketika
// refactor menambah alokasi yang tidak perlu.
func TestMemoryAllocsPerOp(t *testing.T) {
	store := newStore()

	cases := []struct {
		name string
		req  gator.AggregateRequest
	}{
		{
			name: "sum_parent",
			req: gator.AggregateRequest{
				Dataset:      "employees",
				Aggregations: []gator.AggConfig{{Field: "salary", Op: "sum", Alias: "s"}},
			},
		},
		{
			name: "sum_array",
			req: gator.AggregateRequest{
				Dataset:      "employees",
				Aggregations: []gator.AggConfig{{Field: "credits.outstanding_balance", Op: "sum", Alias: "s"}},
			},
		},
		{
			name: "avg_array_weighted",
			req: gator.AggregateRequest{
				Dataset:      "employees",
				Aggregations: []gator.AggConfig{{Field: "credits.outstanding_balance", Op: "avg", Alias: "a"}},
			},
		},
		{
			name: "group_by_department_mixed",
			req: gator.AggregateRequest{
				Dataset: "employees",
				GroupBy: []string{"department"},
				Aggregations: []gator.AggConfig{
					{Field: "salary", Op: "sum", Alias: "ts"},
					{Field: "credits.outstanding_balance", Op: "sum", Alias: "os"},
					{Field: "credits.outstanding_balance", Op: "avg", Alias: "ao"},
				},
			},
		},
		{
			name: "worst_last_n",
			req: gator.AggregateRequest{
				Dataset: "employees",
				GroupBy: []string{"name"},
				Aggregations: []gator.AggConfig{
					{Field: "credits.last_24_coll_hist", Op: "worst_last_n", Alias: "w",
						Params: map[string]interface{}{"n": 6.0}},
				},
			},
		},
	}

	// Disable GC during measurement for stable alloc counts
	debug.SetGCPercent(-1)
	defer debug.SetGCPercent(100)

	for _, tc := range cases {
		tc := tc
		allocs := testing.AllocsPerRun(100, func() {
			gator.Aggregate(store, tc.req) //nolint
		})
		t.Logf("%-40s  allocs/op = %.0f", tc.name, allocs)
	}
}
