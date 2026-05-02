package gator_test

import (
	"encoding/json"
	"fmt"
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

	results := gator.Aggregate(store, req)
	if len(results) != 1 {
		t.Fatalf("expected 1 row, got %d", len(results))
	}
	row := results[0].(map[string]interface{})
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
	results := gator.Aggregate(store, req)
	if len(results) == 0 {
		t.Fatal("expected results, got none")
	}
	for _, r := range results {
		row := r.(map[string]interface{})
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
	results := gator.Aggregate(store, req)
	if len(results) != 1 {
		t.Fatalf("expected 1 row, got %d", len(results))
	}
	row := results[0].(map[string]interface{})
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
	results := gator.Aggregate(store, req)
	byName := map[string]map[string]interface{}{}
	for _, r := range results {
		row := r.(map[string]interface{})
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
	results := gator.Aggregate(store, req)
	if len(results) != 1 {
		t.Fatalf("expected 1 row, got %d", len(results))
	}
	assertF(t, results[0].(map[string]interface{}), "new_3m", 2, "Zara count_date_last_n 3m")
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

	results := gator.Aggregate(store, req)
	b, _ := json.MarshalIndent(results, "", "  ")
	t.Logf("\n%s", string(b))

	byName := map[string]map[string]interface{}{}
	for _, r := range results {
		row := r.(map[string]interface{})
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

	results := gator.Aggregate(store, req)
	if len(results) == 0 {
		t.Fatal("expected results, got none")
	}

	t.Logf("Andi GROUP BY product_type — %d rows:", len(results))
	byType := map[string]map[string]interface{}{}
	for _, r := range results {
		row := r.(map[string]interface{})
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

	results := gator.Aggregate(store, req)
	t.Logf("%d total rows", len(results))

	foundDewi := false
	andiRows := 0
	for _, r := range results {
		row := r.(map[string]interface{})
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

	results := gator.Aggregate(store, req)
	t.Logf("%d rows", len(results))

	for _, r := range results {
		row := r.(map[string]interface{})
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
		row := r.(map[string]interface{})
		if fmt.Sprintf("%v", row["credits.product_type"]) == "credit card" {
			assertF(t, row, "worst_coll_L3M", 3, "Andi CC worst_coll_L3M")
			assertF(t, row, "active_count",   2, "Andi CC count (CC-001+CC-004)")
			assertF(t, row, "active_os", 43000000, "Andi CC os (15M+28M)")
		}
	}
}
