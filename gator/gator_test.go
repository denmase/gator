package gator_test

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"testing"

	"aggregator/gator"
	"aggregator/gator/samples"
)

// ── helpers ──────────────────────────────────────────────────────────────────

func newStore() *gator.Store {
	s := gator.NewStore()
	samples.Register(s)
	return s
}

// rowMap converts a result row (OrderedMap or map[string]interface{}) to a
// plain map for easy field access in tests.
func rowMap(v interface{}) map[string]interface{} {
	if om, ok := v.(gator.OrderedMap); ok {
		return om.Values
	}
	if m, ok := v.(map[string]interface{}); ok {
		return m
	}
	return map[string]interface{}{}
}

func assertF(t *testing.T, row map[string]interface{}, alias string, expected float64, label string) {
	t.Helper()
	v, exists := row[alias]
	if !exists {
		t.Errorf("✗ %s: key %q missing", label, alias)
		return
	}
	got, ok := gator.ToFloat64(v)
	if !ok {
		t.Errorf("✗ %s: value %v not numeric", label, v)
		return
	}
	if got != expected {
		t.Errorf("✗ %-42s got=%.0f  expected=%.0f", label, got, expected)
	} else {
		t.Logf("✓ %-42s = %.0f", label, got)
	}
}

// ── Pefindo (single-object upload, mixed parent + array agg) ─────────────────

func TestPefindo(t *testing.T) {
	store := gator.NewStore()
	store.Register("pefindo", []interface{}{
		map[string]interface{}{
			"inquiryData": map[string]interface{}{"nik": "3201234567890001"},
			"summary": map[string]interface{}{
				"totalOutstanding":   50000000.0,
				"totalLimit":         100000000.0,
				"highestDaysPastDue": 0.0,
			},
			"detailedFacilities": []interface{}{
				map[string]interface{}{"facilityType": "Credit Card", "status": "Active", "outstandingBalance": 10000000.0, "limit": 20000000.0},
				map[string]interface{}{"facilityType": "KPR", "status": "Active", "outstandingBalance": 40000000.0, "limit": 80000000.0},
			},
		},
	})

	req := gator.AggregateRequest{
		Dataset: "pefindo",
		LocalFilter: map[string]map[string]map[string]interface{}{
			"detailedFacilities": {
				"detailedFacilities.status": {"$eq": "Active"},
			},
		},
		GroupBy: []string{"inquiryData.nik"},
		Aggregations: []gator.AggConfig{
			{Field: "summary.highestDaysPastDue", Op: "max", Alias: "highestdpd"},
			{Field: "summary.totalLimit", Op: "sum", Alias: "totallimitsummary"},
			{Field: "detailedFacilities.limit", Op: "sum", Alias: "totallimitdetail"},
			{Field: "summary.totalOutstanding", Op: "sum", Alias: "totalOSSummary"},
			{Field: "detailedFacilities.outstandingBalance", Op: "sum", Alias: "totalOSDetail"},
		},
	}

	results, err := gator.Aggregate(store, req)
	if err != nil { t.Fatalf("Aggregate error: %v", err) }
	if len(results) != 1 {
		t.Fatalf("expected 1 row, got %d", len(results))
	}
	row := rowMap(results[0])
	assertF(t, row, "highestdpd",        0,         "highestDaysPastDue")
	assertF(t, row, "totallimitsummary",  100000000, "summary.totalLimit (no doubling)")
	assertF(t, row, "totallimitdetail",   100000000, "detailedFacilities.limit sum (20M+80M)")
	assertF(t, row, "totalOSSummary",     50000000,  "summary.totalOutstanding (no doubling)")
	assertF(t, row, "totalOSDetail",      50000000,  "detailedFacilities.outstandingBalance (10M+40M)")
}

// ── Employees: mixed parent + array aggregation ───────────────────────────────

func TestEmployeesMixedAgg(t *testing.T) {
	store := newStore()
	req := gator.AggregateRequest{
		Dataset: "employees",
		GroupBy: []string{"department"},
		Aggregations: []gator.AggConfig{
			{Field: "salary", Op: "sum", Alias: "total_salary"},
			{Field: "credits.outstanding_balance", Op: "sum", Alias: "total_os"},
		},
	}
	results, err := gator.Aggregate(store, req)
	if err != nil { t.Fatalf("Aggregate error: %v", err) }
	if len(results) == 0 {
		t.Fatal("expected results, got none")
	}
	for _, r := range results {
		row := rowMap(r)
		if row["department"] == "IT" {
			// Budi(35M)+Eka(22M)+Irfan(15M) = 72M
			assertF(t, row, "total_salary", 72000000, "IT total_salary")
		}
	}
}

// ── Local filter: no double-counting on parent fields ────────────────────────

func TestLocalFilterNoDoubling(t *testing.T) {
	store := newStore()
	req := gator.AggregateRequest{
		Dataset: "employees",
		LocalFilter: map[string]map[string]map[string]interface{}{
			"credits": {
				"credits.product_type": {"$eq": "credit card"},
			},
		},
		Where:   map[string]map[string]interface{}{"name": {"$eq": "Budi"}},
		GroupBy: []string{"name"},
		Aggregations: []gator.AggConfig{
			{Field: "salary", Op: "max", Alias: "sal"},
			{Field: "credits.outstanding_balance", Op: "sum", Alias: "cc_os"},
		},
	}
	results, err := gator.Aggregate(store, req)
	if err != nil { t.Fatalf("Aggregate error: %v", err) }
	if len(results) != 1 {
		t.Fatalf("expected 1 row, got %d", len(results))
	}
	row := rowMap(results[0])
	assertF(t, row, "sal",   35000000, "Budi salary (not doubled)")
	// CC-010(25M)+CC-011(55M)+CC-012(38M)+CC-013(0) = 118M
	assertF(t, row, "cc_os", 118000000, "Budi CC outstanding sum")
}

// ── Dewi (zero credits) must appear in results ───────────────────────────────

func TestDewi(t *testing.T) {
	store := newStore()
	req := gator.AggregateRequest{
		Dataset: "employees",
		GroupBy: []string{"name"},
		Aggregations: []gator.AggConfig{
			{Field: "credits.last_24_delq_hist", Op: "worst_last_n", Alias: "worst_delq_6m",
				Params: map[string]interface{}{"n": 6.0}},
			{Field: "credits.last_24_coll_hist", Op: "ever_has_last_n", Alias: "ever_coll3",
				Params: map[string]interface{}{"n": 3.0, "value": 3.0}},
			{Field: "credits.open_date", Op: "count_date_last_n", Alias: "new_acc",
				Params: map[string]interface{}{"n": 3.0, "ref_date": "2026-04-30"}},
		},
	}
	results, err := gator.Aggregate(store, req)
	if err != nil { t.Fatalf("Aggregate error: %v", err) }
	byName := map[string]map[string]interface{}{}
	for _, r := range results {
		row := rowMap(r)
		byName[fmt.Sprintf("%v", row["name"])] = row
	}
	if _, ok := byName["Dewi"]; !ok {
		t.Fatal("✗ Dewi missing from results")
	}
	t.Log("✓ Dewi present")
	dewi := byName["Dewi"]
	assertF(t, dewi, "ever_coll3", 0, "Dewi ever_coll3 (no credits)")
	assertF(t, dewi, "new_acc",    0, "Dewi new_acc (no credits)")
}

// ── count_date_last_n — generic date window count ────────────────────────────

func TestCountDateLastN(t *testing.T) {
	store := gator.NewStore()
	store.Register("zara", []interface{}{
		map[string]interface{}{
			"name": "Zara",
			"credits": []interface{}{
				map[string]interface{}{"account_no": "X-001", "open_date": "2026-03-15"}, // ✓ within 3m
				map[string]interface{}{"account_no": "X-002", "open_date": "2026-04-01"}, // ✓ within 3m
				map[string]interface{}{"account_no": "X-003", "open_date": "2025-12-01"}, // ✗ outside
			},
		},
	})
	req := gator.AggregateRequest{
		Dataset: "zara",
		GroupBy: []string{"name"},
		Aggregations: []gator.AggConfig{
			{Field: "credits.open_date", Op: "count_date_last_n", Alias: "new_3m",
				Params: map[string]interface{}{"n": 3.0, "ref_date": "2026-04-30", "unit": "month"}},
		},
	}
	results, err := gator.Aggregate(store, req)
	if err != nil { t.Fatalf("Aggregate error: %v", err) }
	if len(results) != 1 {
		t.Fatalf("expected 1 row, got %d", len(results))
	}
	assertF(t, rowMap(results[0]), "new_3m", 2, "Zara count_date_last_n 3m")
}

// ── Full credit.json DSL ─────────────────────────────────────────────────────

func loadCreditJSON(t *testing.T) []interface{} {
	t.Helper()
	b, err := os.ReadFile("/mnt/user-data/uploads/credit.json")
	if err != nil {
		t.Skipf("credit.json not available: %v", err)
	}
	var data []interface{}
	if err := json.Unmarshal(b, &data); err != nil {
		t.Fatalf("parse credit.json: %v", err)
	}
	return data
}

func TestCreditFullDSL(t *testing.T) {
	store := gator.NewStore()
	store.Register("credit", loadCreditJSON(t))

	req := gator.AggregateRequest{
		Dataset: "credit",
		GroupBy: []string{"name"},
		Aggregations: []gator.AggConfig{
			{Field: "credits.last_24_delq_hist", Op: "worst_last_n",    Alias: "worst_delq_6m",     Params: map[string]interface{}{"n": 6.0}},
			{Field: "credits.last_24_delq_hist", Op: "worst_last_n",    Alias: "worst_delq_12m",    Params: map[string]interface{}{"n": 12.0}},
			{Field: "credits.last_24_coll_hist", Op: "worst_last_n",    Alias: "max_coll_3m",       Params: map[string]interface{}{"n": 3.0}},
			{Field: "credits.last_24_coll_hist", Op: "worst_last_n",    Alias: "max_coll_12m",      Params: map[string]interface{}{"n": 12.0}},
			{Field: "credits.last_24_coll_hist", Op: "ever_has_last_n", Alias: "ever_coll_gte3_3m", Params: map[string]interface{}{"n": 3.0, "value": 3.0}},
			{Field: "credits.open_date",         Op: "count_date_last_n", Alias: "new_accounts_3m", Params: map[string]interface{}{"n": 3.0, "ref_date": "2026-04-30"}},
		},
	}

	results, err := gator.Aggregate(store, req)
	if err != nil { t.Fatalf("Aggregate error: %v", err) }
	b, _ := json.MarshalIndent(results, "", "  ")
	t.Logf("\n%s", string(b))

	byName := map[string]map[string]interface{}{}
	for _, r := range results {
		row := rowMap(r)
		byName[fmt.Sprintf("%v", row["name"])] = row
	}

	// Dewi must appear
	if _, ok := byName["Dewi"]; !ok {
		t.Error("✗ Dewi missing")
	} else {
		t.Log("✓ Dewi present")
		assertF(t, byName["Dewi"], "ever_coll_gte3_3m", 0, "Dewi ever_coll_gte3_3m")
		assertF(t, byName["Dewi"], "new_accounts_3m",   0, "Dewi new_accounts_3m")
	}

	checks := []struct {
		name   string
		alias  string
		expect float64
	}{
		{"Andi",  "worst_delq_6m",     100},
		{"Andi",  "worst_delq_12m",    100},
		{"Andi",  "max_coll_3m",         3},
		{"Andi",  "max_coll_12m",         3},
		{"Andi",  "ever_coll_gte3_3m",   1},
		{"Andi",  "new_accounts_3m",      0},
		{"Budi",  "worst_delq_6m",     150},
		{"Budi",  "worst_delq_12m",    150},
		{"Budi",  "max_coll_3m",         4},
		{"Budi",  "max_coll_12m",         4},
		{"Budi",  "ever_coll_gte3_3m",   1},
		{"Budi",  "new_accounts_3m",      0},
		{"Citra", "worst_delq_6m",       0},
		{"Citra", "max_coll_3m",          1},
		{"Citra", "ever_coll_gte3_3m",   0},
		{"Fajar", "worst_delq_6m",     200},
		{"Fajar", "worst_delq_12m",    200},
		{"Fajar", "max_coll_3m",         5},
		{"Fajar", "ever_coll_gte3_3m",  1},
		{"Irfan", "worst_delq_6m",     100},
		{"Irfan", "max_coll_3m",         3},
		{"Irfan", "ever_coll_gte3_3m",  1},
		{"Irfan", "new_accounts_3m",     0},
	}
	for _, c := range checks {
		row, ok := byName[c.name]
		if !ok {
			t.Errorf("✗ %s missing from results", c.name)
			continue
		}
		assertF(t, row, c.alias, c.expect, c.name+" "+c.alias)
	}
}

// ── GROUP BY array-level field (explode mode) ────────────────────────────────
//
// Masalah yang dilaporkan: GROUP BY "credits.product_type" menghasilkan
// [object Object] karena field ini ada di dalam array, bukan di parent record.
// Fix: engine mendeteksi array-level GROUP BY dan melakukan EXPLODE terlebih
// dahulu — setiap credit element menjadi satu flat row.

func TestGroupByArrayField(t *testing.T) {
	store := newStore()
	req := gator.AggregateRequest{
		Dataset: "employees",
		LocalFilter: map[string]map[string]map[string]interface{}{
			"credits": {"credits.loan_status": {"$eq": "active"}},
		},
		Where:   map[string]map[string]interface{}{"name": {"$eq": "Andi"}},
		GroupBy: []string{"department", "name", "credits.product_type"},
		Aggregations: []gator.AggConfig{
			{Field: "credits.outstanding_balance", Op: "sum", Alias: "active_os"},
			{Field: "credits.account_no", Op: "count", Alias: "active_count"},
		},
	}

	results, err := gator.Aggregate(store, req)
	if err != nil { t.Fatalf("Aggregate error: %v", err) }
	if len(results) == 0 {
		t.Fatal("expected results, got none")
	}

	t.Logf("Andi GROUP BY product_type — %d rows:", len(results))
	byType := map[string]map[string]interface{}{}
	for _, r := range results {
		row := rowMap(r)
		pt := fmt.Sprintf("%v", row["credits.product_type"])
		byType[pt] = row
		t.Logf("  %-22s os=%-12v count=%v", pt, row["active_os"], row["active_count"])
	}

	// credits.product_type must be a plain string, not "[object Object]"
	for pt := range byType {
		if pt == "[object Object]" || pt == "map[]" {
			t.Errorf("✗ credits.product_type = %q (should be a string)", pt)
		}
	}

	// Andi active credit cards: CC-001(15M) + CC-004(28M) = 43M, count=2
	if cc, ok := byType["credit card"]; ok {
		assertF(t, cc, "active_os",    43000000, "Andi CC os (15M+28M)")
		assertF(t, cc, "active_count",        2, "Andi CC count")
	} else {
		t.Error("✗ 'credit card' row missing")
	}
	// Andi active personal loan: PL-002(45M), count=1
	if pl, ok := byType["personal loan"]; ok {
		assertF(t, pl, "active_os", 45000000, "Andi PL os")
		assertF(t, pl, "active_count",      1, "Andi PL count")
	} else {
		t.Error("✗ 'personal loan' row missing")
	}
}

func TestGroupByArrayFieldGhostRow(t *testing.T) {
	store := newStore()
	// Andi (has credits) + Dewi (no credits) in same query
	req := gator.AggregateRequest{
		Dataset: "employees",
		LocalFilter: map[string]map[string]map[string]interface{}{
			"credits": {"credits.loan_status": {"$eq": "active"}},
		},
		Where:   map[string]map[string]interface{}{"name": {"$in": []interface{}{"Andi", "Dewi"}}},
		GroupBy: []string{"name", "credits.product_type"},
		Aggregations: []gator.AggConfig{
			{Field: "credits.outstanding_balance", Op: "sum", Alias: "active_os"},
			{Field: "credits.account_no", Op: "count", Alias: "active_count"},
		},
	}

	results, err := gator.Aggregate(store, req)
	if err != nil { t.Fatalf("Aggregate error: %v", err) }
	t.Logf("%d total rows", len(results))

	foundDewi := false
	andiRows := 0
	for _, r := range results {
		row := rowMap(r)
		name := fmt.Sprintf("%v", row["name"])
		pt := fmt.Sprintf("%v", row["credits.product_type"])
		t.Logf("  name=%-8s product_type=%-20s os=%v count=%v", name, pt, row["active_os"], row["active_count"])

		if name == "Dewi" {
			foundDewi = true
			assertF(t, row, "active_os",    0, "Dewi ghost os=0")
			assertF(t, row, "active_count", 0, "Dewi ghost count=0")
		}
		if name == "Andi" {
			andiRows++
		}
	}
	if !foundDewi {
		t.Error("✗ Dewi missing — ghost row not generated")
	} else {
		t.Log("✓ Dewi present as ghost row")
	}
	// Andi has 4 distinct product_types: credit card, personal loan, mortgage, paylater
	if andiRows != 4 {
		t.Errorf("✗ Andi should have 4 product_type rows, got %d", andiRows)
	} else {
		t.Logf("✓ Andi has %d product_type rows", andiRows)
	}
}

func TestGroupByArrayFieldWithWindowOp(t *testing.T) {
	// Reproduce bug report DSL — worst_last_n should work on exploded flat rows
	store := newStore()
	req := gator.AggregateRequest{
		Dataset: "employees",
		LocalFilter: map[string]map[string]map[string]interface{}{
			"credits": {"credits.loan_status": {"$eq": "active"}},
		},
		Where:   map[string]map[string]interface{}{"name": {"$eq": "Andi"}},
		GroupBy: []string{"department", "name", "credits.product_type"},
		Aggregations: []gator.AggConfig{
			{Field: "credits.outstanding_balance", Op: "sum", Alias: "active_os"},
			{Field: "credits.account_no", Op: "count", Alias: "active_count"},
			{Field: "credits.last_24_coll_hist", Op: "worst_last_n", Alias: "worst_coll_L6M", Params: map[string]interface{}{"n": 6.0}},
			{Field: "credits.last_24_coll_hist", Op: "worst_last_n", Alias: "worst_coll_L3M", Params: map[string]interface{}{"n": 3.0}},
			{Field: "credits.last_24_coll_hist", Op: "worst_last_n", Alias: "worst_coll_L12M", Params: map[string]interface{}{"n": 12.0}},
			{Field: "credits.last_24_coll_hist", Op: "worst_last_n", Alias: "worst_coll_L24M", Params: map[string]interface{}{"n": 24.0}},
		},
	}

	results, err := gator.Aggregate(store, req)
	if err != nil { t.Fatalf("Aggregate error: %v", err) }
	t.Logf("%d rows", len(results))

	for _, r := range results {
		row := rowMap(r)
		pt := fmt.Sprintf("%v", row["credits.product_type"])
		if pt == "[object Object]" {
			t.Errorf("✗ product_type = [object Object]")
			continue
		}
		t.Logf("  %-22s os=%-12v count=%v L3M=%v L6M=%v L12M=%v L24M=%v",
			pt, row["active_os"], row["active_count"],
			row["worst_coll_L3M"], row["worst_coll_L6M"],
			row["worst_coll_L12M"], row["worst_coll_L24M"])
	}

	// Andi credit card group (CC-001 all-1 coll + CC-004 ends 3,3,1):
	// flat rows for credit card: [CC-001 hist, CC-004 hist]
	// worst_last_n on each flat row's hist, then max across rows:
	// CC-001 L3M=1 (all 1s), CC-004 L3M=3 → group worst_L3M = 3
	for _, r := range results {
		row := rowMap(r)
		if fmt.Sprintf("%v", row["credits.product_type"]) == "credit card" {
			assertF(t, row, "worst_coll_L3M", 3, "Andi CC worst_coll_L3M")
			assertF(t, row, "active_count",   2, "Andi CC count (CC-001+CC-004)")
			assertF(t, row, "active_os", 43000000, "Andi CC os (15M+28M)")
		}
	}
}

// ── BUG-02: explodeRecords nested arrayPath ───────────────────────────────────
// Verifikasi bahwa GROUP BY pada field di dalam nested array
// (mis. FasilitasList.Fasilitas.JenisKredit di ideb.xml) bekerja benar.
// Kita simulasikan dengan dataset synthetic agar tidak bergantung pada file.

func TestExplodeNestedArrayPath(t *testing.T) {
	store := gator.NewStore()
	store.Register("nested_test", []interface{}{
		map[string]interface{}{
			"nik": "001",
			"FasilitasList": map[string]interface{}{
				"Fasilitas": []interface{}{
					map[string]interface{}{"JenisKredit": "KPR", "Outstanding": 320000000.0},
					map[string]interface{}{"JenisKredit": "KMG", "Outstanding": 85000000.0},
				},
			},
		},
		map[string]interface{}{
			"nik": "002",
			"FasilitasList": map[string]interface{}{
				"Fasilitas": []interface{}{
					map[string]interface{}{"JenisKredit": "KPR", "Outstanding": 200000000.0},
				},
			},
		},
	})

	req := gator.AggregateRequest{
		Dataset: "nested_test",
		GroupBy: []string{"FasilitasList.Fasilitas.JenisKredit"},
		Aggregations: []gator.AggConfig{
			{Field: "FasilitasList.Fasilitas.Outstanding", Op: "sum", Alias: "total_os"},
			{Field: "FasilitasList.Fasilitas.JenisKredit", Op: "count", Alias: "count_fac"},
		},
	}

	results, err := gator.Aggregate(store, req)
	if err != nil { t.Fatalf("Aggregate error: %v", err) }
	t.Logf("%d rows returned", len(results))

	byType := map[string]map[string]interface{}{}
	for _, r := range results {
		row := rowMap(r)
		jk := fmt.Sprintf("%v", row["FasilitasList.Fasilitas.JenisKredit"])
		byType[jk] = row
		t.Logf("  JenisKredit=%-8s os=%v count=%v", jk, row["total_os"], row["count_fac"])
	}

	// KPR: nik001(320M) + nik002(200M) = 520M, count=2
	if kpr, ok := byType["KPR"]; ok {
		assertF(t, kpr, "total_os", 520000000, "KPR total_os (320M+200M)")
		assertF(t, kpr, "count_fac", 2, "KPR count")
	} else {
		t.Error("✗ KPR row missing")
	}
	// KMG: nik001(85M), count=1
	if kmg, ok := byType["KMG"]; ok {
		assertF(t, kmg, "total_os", 85000000, "KMG total_os")
		assertF(t, kmg, "count_fac", 1, "KMG count")
	} else {
		t.Error("✗ KMG row missing")
	}
}

// Verifikasi bahwa top-level explode (credits) masih benar setelah refactor.
func TestExplodeTopLevelStillCorrect(t *testing.T) {
	store := newStore()
	req := gator.AggregateRequest{
		Dataset: "employees",
		LocalFilter: map[string]map[string]map[string]interface{}{
			"credits": {"credits.loan_status": {"$eq": "active"}},
		},
		Where:   map[string]map[string]interface{}{"name": {"$eq": "Andi"}},
		GroupBy: []string{"name", "credits.product_type"},
		Aggregations: []gator.AggConfig{
			{Field: "credits.outstanding_balance", Op: "sum", Alias: "os"},
			{Field: "credits.account_no", Op: "count", Alias: "cnt"},
		},
	}
	results, err := gator.Aggregate(store, req)
	if err != nil { t.Fatalf("Aggregate error: %v", err) }

	byType := map[string]map[string]interface{}{}
	for _, r := range results {
		row := rowMap(r)
		byType[fmt.Sprintf("%v", row["credits.product_type"])] = row
	}

	// Sanity: credit card still 43M, count=2
	if cc, ok := byType["credit card"]; ok {
		assertF(t, cc, "os", 43000000, "Andi CC os after explode refactor")
		assertF(t, cc, "cnt", 2, "Andi CC count after explode refactor")
	} else {
		t.Error("✗ credit card row missing after refactor")
	}
}

// ── BUG-07: DetectSchemaFromSample ───────────────────────────────────────────
// Sebelumnya: jika record[0] punya credits:[], schema mendeteksi credits sebagai
// array_empty, sehingga aggregasi credits.* menjadi levelParent dan hasilnya 0.
// Setelah fix: engine scan hingga 10 record, pilih yang punya array_object.

func TestDetectSchemaFromSampleSkipsEmptyArray(t *testing.T) {
	store := gator.NewStore()
	// Record pertama: credits kosong (edge case yang menyebabkan bug)
	// Record kedua: credits terisi
	store.Register("empty_first", []interface{}{
		map[string]interface{}{
			"name":    "Dewi",
			"salary":  20000000.0,
			"credits": []interface{}{}, // ← empty array di record pertama
		},
		map[string]interface{}{
			"name":   "Andi",
			"salary": 25000000.0,
			"credits": []interface{}{
				map[string]interface{}{"product_type": "credit card", "outstanding_balance": 15000000.0},
				map[string]interface{}{"product_type": "personal loan", "outstanding_balance": 45000000.0},
			},
		},
	})

	req := gator.AggregateRequest{
		Dataset: "empty_first",
		GroupBy: []string{"name"},
		Aggregations: []gator.AggConfig{
			{Field: "credits.outstanding_balance", Op: "sum", Alias: "total_os"},
		},
	}

	results, err := gator.Aggregate(store, req)
	if err != nil { t.Fatalf("Aggregate error: %v", err) }
	byName := map[string]map[string]interface{}{}
	for _, r := range results {
		row := rowMap(r)
		byName[fmt.Sprintf("%v", row["name"])] = row
		t.Logf("  name=%-8s total_os=%v", row["name"], row["total_os"])
	}

	// Sebelum fix: Andi = 0 karena schema dari Dewi (credits:[]) → array_empty
	// Setelah fix: Andi = 60M (15M + 45M) karena schema dari record terbaik
	if andi, ok := byName["Andi"]; ok {
		assertF(t, andi, "total_os", 60000000, "Andi total_os (schema from best sample, not empty first)")
	} else {
		t.Error("✗ Andi missing")
	}
	// Dewi tetap muncul dengan 0
	if dewi, ok := byName["Dewi"]; ok {
		assertF(t, dewi, "total_os", 0, "Dewi total_os = 0 (ghost row)")
	} else {
		t.Error("✗ Dewi missing")
	}
}

// ── BUG-05: parent field dedup in explode mode ────────────────────────────────
// Sebelumnya: GROUP BY product_type + SUM(salary) → salary × N credits.
// Setelah fix: salary di-dedup per parent record sebelum di-aggregate.

func TestExplodeModeParentFieldDedup(t *testing.T) {
	store := newStore()
	req := gator.AggregateRequest{
		Dataset: "employees",
		Where:   map[string]map[string]interface{}{"name": {"$eq": "Andi"}},
		GroupBy: []string{"name", "credits.product_type"},
		Aggregations: []gator.AggConfig{
			// Parent field — must NOT be multiplied by number of credits
			{Field: "salary", Op: "max", Alias: "salary"},
			{Field: "salary", Op: "count", Alias: "salary_count"},
			// Array field — fine to aggregate per row
			{Field: "credits.outstanding_balance", Op: "sum", Alias: "total_os"},
		},
	}

	results, err := gator.Aggregate(store, req)
	if err != nil { t.Fatalf("Aggregate error: %v", err) }
	byType := map[string]map[string]interface{}{}
	for _, r := range results {
		row := rowMap(r)
		byType[fmt.Sprintf("%v", row["credits.product_type"])] = row
		t.Logf("  product_type=%-14s salary=%v salary_count=%v os=%v",
			row["credits.product_type"], row["salary"], row["salary_count"], row["total_os"])
	}

	// Andi has 5 credits. Without dedup, salary_count would be 5 per product_type.
	// With dedup: only 1 distinct parent (Andi) per product_type group → count=1.
	for pt, row := range byType {
		if pt == "<nil>" {
			continue // ghost row, skip
		}
		// salary must be 25_000_000 (Andi's actual salary, not multiplied)
		assertF(t, row, "salary", 25000000, pt+" salary not multiplied")
		// count of parent records contributing salary must be 1 (one Andi)
		assertF(t, row, "salary_count", 1, pt+" salary_count=1 (dedup)")
	}
}

// ── BUG-01: field validation ──────────────────────────────────────────────────

func TestAggregateRejectsNonScalarField(t *testing.T) {
	store := newStore()
	req := gator.AggregateRequest{
		Dataset: "employees",
		Aggregations: []gator.AggConfig{
			// "credits" resolves to array_object — should be rejected
			{Field: "credits", Op: "sum", Alias: "broken"},
		},
	}
	_, err := gator.Aggregate(store, req)
	if err == nil {
		t.Error("✗ expected error for array_object field, got nil")
	} else {
		t.Logf("✓ correctly rejected: %v", err)
	}
}

func TestAggregateAcceptsValidFields(t *testing.T) {
	store := newStore()
	req := gator.AggregateRequest{
		Dataset: "employees",
		Aggregations: []gator.AggConfig{
			{Field: "credits.outstanding_balance", Op: "sum", Alias: "os"},
			{Field: "salary", Op: "avg", Alias: "avg_sal"},
		},
	}
	_, err := gator.Aggregate(store, req)
	if err != nil {
		t.Errorf("✗ unexpected error for valid fields: %v", err)
	} else {
		t.Log("✓ valid fields accepted")
	}
}

// ── BUG-03: avg mathematically correct (weighted) ────────────────────────────
//
// Mean-of-means bug: jika Andi punya 5 credits avg OS = 102.6M dan
// Budi punya 10 credits avg OS = 108.4M, mean-of-means = (102.6+108.4)/2 = 105.5M
// Tapi correct weighted avg = (5×102.6 + 10×108.4) / 15 = 106.47M
//
// Kasus lebih ekstrem: satu record dengan 1 elemen (nilai 0) dan satu record
// dengan 1000 elemen (nilai 100). Mean-of-means = 50. Weighted = ~100.

func TestAvgWeightedCorrect(t *testing.T) {
	store := gator.NewStore()
	store.Register("avg_test", []interface{}{
		// Alice: 1 credit, balance = 0
		map[string]interface{}{
			"name": "Alice",
			"credits": []interface{}{
				map[string]interface{}{"balance": 0.0},
			},
		},
		// Bob: 3 credits, balance = 100 each
		map[string]interface{}{
			"name": "Bob",
			"credits": []interface{}{
				map[string]interface{}{"balance": 100.0},
				map[string]interface{}{"balance": 100.0},
				map[string]interface{}{"balance": 100.0},
			},
		},
	})

	req := gator.AggregateRequest{
		Dataset: "avg_test",
		// No groupBy — aggregate all records
		Aggregations: []gator.AggConfig{
			{Field: "credits.balance", Op: "avg", Alias: "avg_balance"},
		},
	}

	results, err := gator.Aggregate(store, req)
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 row, got %d", len(results))
	}
	row := rowMap(results[0])
	got, ok := gator.ToFloat64(row["avg_balance"])
	if !ok {
		t.Fatalf("avg_balance not numeric: %v", row["avg_balance"])
	}

	// Total values: 0, 100, 100, 100 → correct avg = 300/4 = 75
	// Mean-of-means (WRONG): avg(0, avg(100,100,100)) = avg(0, 100) = 50
	const correct = 75.0
	const meanOfMeans = 50.0
	if math.Abs(got-correct) > 0.001 {
		if math.Abs(got-meanOfMeans) < 0.001 {
			t.Errorf("✗ avg_balance = %.4f — this is the mean-of-means bug! correct value = %.4f", got, correct)
		} else {
			t.Errorf("✗ avg_balance = %.4f, expected %.4f", got, correct)
		}
	} else {
		t.Logf("✓ avg_balance = %.4f (correct weighted average, not mean-of-means %.4f)", got, meanOfMeans)
	}
}

func TestAvgWeightedGroupBy(t *testing.T) {
	// GROUP BY department — each group has different number of employees.
	// IT: Budi(35M) + Eka(22M) + Irfan(15M) = 3 people, avg salary = 72M/3 = 24M
	// Finance: Citra(18M) + Gina(24M) = 2 people, avg salary = 42M/2 = 21M
	store := newStore()
	req := gator.AggregateRequest{
		Dataset: "employees",
		GroupBy: []string{"department"},
		Aggregations: []gator.AggConfig{
			{Field: "salary", Op: "avg", Alias: "avg_salary"},
			{Field: "salary", Op: "count", Alias: "count_emp"},
		},
	}
	results, err := gator.Aggregate(store, req)
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	byDept := map[string]map[string]interface{}{}
	for _, r := range results {
		row := rowMap(r)
		byDept[fmt.Sprintf("%v", row["department"])] = row
		t.Logf("  dept=%-20s avg_salary=%v count=%v", row["department"], row["avg_salary"], row["count_emp"])
	}

	if it, ok := byDept["IT"]; ok {
		avg, _ := gator.ToFloat64(it["avg_salary"])
		// (35M + 22M + 15M) / 3 = 24_000_000
		if math.Abs(avg-24000000) > 1 {
			t.Errorf("✗ IT avg_salary = %.0f, expected 24000000", avg)
		} else {
			t.Logf("✓ IT avg_salary = %.0f", avg)
		}
	}
	if finance, ok := byDept["Finance"]; ok {
		avg, _ := gator.ToFloat64(finance["avg_salary"])
		// (18M + 24M) / 2 = 21_000_000
		if math.Abs(avg-21000000) > 1 {
			t.Errorf("✗ Finance avg_salary = %.0f, expected 21000000", avg)
		} else {
			t.Logf("✓ Finance avg_salary = %.0f", avg)
		}
	}
}

func TestAvgWeightedArrayField(t *testing.T) {
	// avg(credits.outstanding_balance) across all employees, no groupBy.
	// Andi: 5 credits [15M, 45M, 420M, 28M, 5M] = 513M / 5 = 102.6M per-record avg
	// Budi: 10 credits [...] = 1084M / 10 = 108.4M per-record avg (approx)
	// Correct total avg = (513M + ...) / total_credits across all employees
	// We just verify it's NOT mean-of-means by checking it's consistent with
	// sum/count arithmetic.
	store := newStore()

	// Get SUM and COUNT separately to verify avg = sum/count
	reqSum := gator.AggregateRequest{
		Dataset:      "employees",
		Aggregations: []gator.AggConfig{{Field: "credits.outstanding_balance", Op: "sum", Alias: "s"}},
	}
	reqCount := gator.AggregateRequest{
		Dataset:      "employees",
		Aggregations: []gator.AggConfig{{Field: "credits.outstanding_balance", Op: "count", Alias: "c"}},
	}
	reqAvg := gator.AggregateRequest{
		Dataset:      "employees",
		Aggregations: []gator.AggConfig{{Field: "credits.outstanding_balance", Op: "avg", Alias: "a"}},
	}
	rSum, _ := gator.Aggregate(store, reqSum)
	rCount, _ := gator.Aggregate(store, reqCount)
	rAvg, _ := gator.Aggregate(store, reqAvg)

	s, _ := gator.ToFloat64(gator.RowValues(rSum[0])["s"])
	c, _ := gator.ToFloat64(gator.RowValues(rCount[0])["c"])
	a, _ := gator.ToFloat64(gator.RowValues(rAvg[0])["a"])

	expected := s / c
	t.Logf("sum=%.0f count=%.0f expected_avg=%.4f got_avg=%.4f", s, c, expected, a)

	if math.Abs(a-expected) > 0.01 {
		t.Errorf("✗ avg(%.4f) != sum/count(%.4f) — weighted avg is incorrect", a, expected)
	} else {
		t.Logf("✓ avg = sum/count = %.4f (mathematically correct)", a)
	}
}

// ── Merge fitur dari uploaded impl ───────────────────────────────────────────

func TestNewWhereOperators(t *testing.T) {
	store := newStore()

	// $nin — not in list
	req := gator.AggregateRequest{
		Dataset:      "employees",
		Where:        map[string]map[string]interface{}{"department": {"$nin": []interface{}{"IT", "Finance"}}},
		Aggregations: []gator.AggConfig{{Field: "salary", Op: "count", Alias: "c"}},
	}
	rows, err := gator.Aggregate(store, req)
	if err != nil {
		t.Fatalf("$nin: %v", err)
	}
	n, _ := gator.ToFloat64(rowMap(rows[0])["c"])
	t.Logf("✓ $nin: count non-IT/Finance = %.0f", n)
	if n == 0 {
		t.Error("✗ $nin returned 0 — expected Marketing/Risk Management employees")
	}

	// $between — salary between 20M and 30M
	req2 := gator.AggregateRequest{
		Dataset:      "employees",
		Where:        map[string]map[string]interface{}{"salary": {"$between": []interface{}{20000000.0, 30000000.0}}},
		Aggregations: []gator.AggConfig{{Field: "name", Op: "count", Alias: "c"}},
	}
	rows2, err := gator.Aggregate(store, req2)
	if err != nil {
		t.Fatalf("$between: %v", err)
	}
	n2, _ := gator.ToFloat64(rowMap(rows2[0])["c"])
	t.Logf("✓ $between: count salary 20-30M = %.0f", n2)
	// Andi(25M) Dewi(20M) Eka(22M) Fajar(30M) Gina(24M) → ≥3
	if n2 < 3 {
		t.Errorf("✗ $between: expected ≥3, got %.0f", n2)
	}

	// $within_months — open_date within 12 months
	req3 := gator.AggregateRequest{
		Dataset: "employees",
		LocalFilter: map[string]map[string]map[string]interface{}{
			"credits": {"credits.open_date": {"$within_months": 12.0}},
		},
		Aggregations: []gator.AggConfig{{Field: "credits.account_no", Op: "count", Alias: "recent"}},
	}
	rows3, err := gator.Aggregate(store, req3)
	if err != nil {
		t.Fatalf("$within_months: %v", err)
	}
	recent, _ := gator.ToFloat64(rowMap(rows3[0])["recent"])
	t.Logf("✓ $within_months: recent credits (12mo) = %.0f", recent)
	if recent == 0 {
		t.Error("✗ $within_months: expected some recent credits")
	}
}

func TestComparatorOnEverHasLastN(t *testing.T) {
	store := newStore()

	// ever_has_last_n with $gte comparator: ever coll >= 3 in last 3 months
	req := gator.AggregateRequest{
		Dataset: "employees",
		GroupBy: []string{"name"},
		Aggregations: []gator.AggConfig{
			{Field: "credits.last_24_coll_hist", Op: "ever_has_last_n", Alias: "ever_bad",
				Params: map[string]interface{}{"n": 3.0, "value": 3.0, "comparator": "$gte"}},
		},
	}
	rows, err := gator.Aggregate(store, req)
	if err != nil {
		t.Fatalf("ever_has_last_n comparator: %v", err)
	}
	byName := map[string]float64{}
	for _, r := range rows {
		row := rowMap(r)
		v, _ := gator.ToFloat64(row["ever_bad"])
		byName[fmt.Sprintf("%v", row["name"])] = v
		t.Logf("  name=%-8s ever_bad=%.0f", row["name"], v)
	}
	// Andi CC-004 has 'a' (100d = coll 5) in last 3 months — must be 1
	if byName["Andi"] != 1 {
		t.Errorf("✗ Andi ever_bad (coll>=3 last 3m) = %.0f, want 1", byName["Andi"])
	} else {
		t.Log("✓ Andi ever_bad = 1 (coll>=3 in last 3m)")
	}
	// Citra is clean (all zeros) — must be 0
	if byName["Citra"] != 0 {
		t.Errorf("✗ Citra ever_bad = %.0f, want 0", byName["Citra"])
	} else {
		t.Log("✓ Citra ever_bad = 0")
	}
}

func TestNewOps(t *testing.T) {
	store := newStore()

	// count_rows vs count — count_rows includes nil
	req := gator.AggregateRequest{
		Dataset: "employees",
		Aggregations: []gator.AggConfig{
			{Field: "name", Op: "count", Alias: "count_name"},
			{Field: "name", Op: "count_rows", Alias: "count_rows"},
		},
	}
	rows, err := gator.Aggregate(store, req)
	if err != nil {
		t.Fatalf("count_rows: %v", err)
	}
	row := rowMap(rows[0])
	cn, _ := gator.ToFloat64(row["count_name"])
	cr, _ := gator.ToFloat64(row["count_rows"])
	t.Logf("count=%v count_rows=%v", cn, cr)
	if cn != 9 {
		t.Errorf("✗ count(name) = %.0f, want 9", cn)
	} else {
		t.Log("✓ count(name) = 9")
	}
	if cr != 9 {
		t.Errorf("✗ count_rows = %.0f, want 9", cr)
	} else {
		t.Log("✓ count_rows = 9")
	}

	// min_last_n
	req2 := gator.AggregateRequest{
		Dataset: "employees",
		GroupBy: []string{"name"},
		Aggregations: []gator.AggConfig{
			{Field: "credits.last_24_delq_hist", Op: "min_last_n", Alias: "min_delq_3m",
				Params: map[string]interface{}{"n": 3.0}},
		},
	}
	rows2, err := gator.Aggregate(store, req2)
	if err != nil {
		t.Fatalf("min_last_n: %v", err)
	}
	found := false
	for _, r := range rows2 {
		row2 := rowMap(r)
		if fmt.Sprintf("%v", row2["name"]) == "Citra" {
			v, _ := gator.ToFloat64(row2["min_delq_3m"])
			t.Logf("✓ Citra min_delq_3m = %.0f (expect 0)", v)
			found = true
			if v != 0 {
				t.Errorf("✗ Citra min_delq_3m = %.0f, want 0", v)
			}
		}
	}
	if !found {
		t.Error("✗ Citra not found in min_last_n results")
	}
}

func TestAutoClassifyWhere(t *testing.T) {
	store := newStore()

	// filterMode="auto": WHERE condition on array field should be auto-routed to localFilter
	// credits.loan_status is an array field — auto mode should filter credits, not records
	req := gator.AggregateRequest{
		Dataset:    "employees",
		FilterMode: "auto",
		Where: map[string]map[string]interface{}{
			"credits.loan_status": {"$eq": "active"}, // array field → auto localFilter
			"department":          {"$eq": "IT"},     // parent field → stays in WHERE
		},
		GroupBy: []string{"name"},
		Aggregations: []gator.AggConfig{
			{Field: "credits.outstanding_balance", Op: "sum", Alias: "active_os"},
		},
	}
	rows, err := gator.Aggregate(store, req)
	if err != nil {
		t.Fatalf("auto-classify: %v", err)
	}

	// Should only have IT employees (Budi, Eka, Irfan)
	names := map[string]bool{}
	for _, r := range rows {
		row := rowMap(r)
		name := fmt.Sprintf("%v", row["name"])
		names[name] = true
		t.Logf("  name=%-8s active_os=%v", name, row["active_os"])
	}
	for _, it := range []string{"Budi", "Eka", "Irfan"} {
		if !names[it] {
			t.Errorf("✗ IT employee %s missing from auto-classify results", it)
		}
	}
	if names["Andi"] {
		t.Error("✗ Andi (Risk Management) should be excluded by WHERE department=$eq IT")
	} else {
		t.Log("✓ Andi correctly excluded (auto-classify WHERE parent field)")
	}
}

func TestOrderedMapOutput(t *testing.T) {
	store := newStore()
	req := gator.AggregateRequest{
		Dataset: "employees",
		GroupBy: []string{"department", "city"},
		Aggregations: []gator.AggConfig{
			{Field: "salary", Op: "sum", Alias: "total_salary"},
			{Field: "credits.outstanding_balance", Op: "sum", Alias: "total_os"},
		},
	}
	rows, err := gator.Aggregate(store, req)
	if err != nil {
		t.Fatalf("OrderedMap: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("no rows")
	}
	om, ok := rows[0].(gator.OrderedMap)
	if !ok {
		t.Fatalf("✗ result row is %T, not OrderedMap", rows[0])
	}
	// Keys must be in DSL order: department, city, total_salary, total_os
	expected := []string{"department", "city", "total_salary", "total_os"}
	if len(om.Keys) < len(expected) {
		t.Fatalf("✗ only %d keys in OrderedMap", len(om.Keys))
	}
	for i, k := range expected {
		if om.Keys[i] != k {
			t.Errorf("✗ column %d: got %q want %q", i, om.Keys[i], k)
		}
	}
	t.Logf("✓ OrderedMap keys: %v", om.Keys)
}
