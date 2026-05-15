package ingest_test

import (
	"encoding/json"
	"os"
	"testing"

	"aggregator/gator"
	"aggregator/gator/ingest"
)

// loadXMLDataset parses an XML file into a gator Store dataset.
func loadXMLDataset(t *testing.T, path, name string) *gator.Store {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Skipf("%s not available: %v", path, err)
	}
	defer f.Close()

	rows, err := ingest.ParseXML(f, ingest.DefaultXMLOptions())
	if err != nil {
		t.Fatalf("ParseXML %s: %v", path, err)
	}
	store := gator.NewStore()
	store.Register(name, rows)
	return store
}

// ── ideb.xml: nested array aggregation ────────────────────────────────────────
//
// Structure after parsing:
//   root
//     Debtor.NIK = "3201234567890001"
//     Summary.TotalOutstanding = 680000000
//     FasilitasList.Fasilitas[]          ← level-1 array
//       [0] NomorKontrak=KRD-001234567, JenisKredit=KPR, Outstanding=320000000
//           RiwayatKolektibilitas[]      ← level-2 array
//             {Bulan:2026-04, HariTunggakan:0,  Kolektibilitas:1}
//             {Bulan:2026-03, HariTunggakan:0,  Kolektibilitas:1}
//             {Bulan:2026-02, HariTunggakan:35, Kolektibilitas:2}
//             {Bulan:2026-01, HariTunggakan:0,  Kolektibilitas:1}
//             {Bulan:2025-05, HariTunggakan:0,  Kolektibilitas:1}
//       [1] NomorKontrak=KRD-009876543, JenisKredit=KMG, Outstanding=85000000
//           RiwayatKolektibilitas[]
//             {Bulan:2026-04, HariTunggakan:45, Kolektibilitas:2}
//             {Bulan:2026-03, HariTunggakan:30, Kolektibilitas:2}
//
// Query: group by Debtor.NIK
//   1. sum of FasilitasList.Fasilitas.Outstanding (level-1 array sum)
//   2. max FasilitasList.Fasilitas.RiwayatKolektibilitas.HariTunggakan (nested, all time)
//   3. max HariTunggakan last 3 months (date_field="Bulan", ref_date=2026-04-30)
//      last 3m from 2026-04-30 = >= 2026-01-30:
//        KPR: 2026-04(0), 2026-03(0), 2026-02(35) → max=35
//        KMG: 2026-04(45), 2026-03(30)             → max=45
//        cross-Fasilitas max(35,45) = 45
//   4. ever has Kolektibilitas >= 2 in last 3 months (ever_has_last_n on array of objects)
//      For this we use localFilter to pick only recent months, then max Kolektibilitas
//      Alternatively, use max with date_field to get max Kolektibilitas in last 3m.
//      KPR: max(1,1,2) = 2; KMG: max(2,2) = 2 → cross-max = 2 ≥ 2 → ever=1

func TestIDEBAggregation(t *testing.T) {
	store := loadXMLDataset(t, "/mnt/user-data/uploads/ideb.xml", "ideb")

	req := gator.AggregateRequest{
		Dataset: "ideb",
		GroupBy: []string{"Debtor.NIK"},
		Aggregations: []gator.AggConfig{
			// Parent-level
			{Field: "Summary.TotalOutstanding", Op: "max", Alias: "total_os_summary"},

			// Level-1 array: sum of Outstanding across all Fasilitas
			{Field: "FasilitasList.Fasilitas.Outstanding", Op: "sum", Alias: "sum_outstanding"},

			// Level-2 nested: worst HariTunggakan across all months, all facilities
			{
				Field: "FasilitasList.Fasilitas.RiwayatKolektibilitas.HariTunggakan",
				Op:    "max",
				Alias: "worst_dpd_alltime",
			},

			// Level-2 nested: worst HariTunggakan in last 3 months (date_field filter)
			{
				Field: "FasilitasList.Fasilitas.RiwayatKolektibilitas.HariTunggakan",
				Op:    "max",
				Alias: "worst_dpd_3m",
				Params: map[string]interface{}{
					"date_field": "Bulan",
					"ref_date":   "2026-04-30",
					"n":          3.0,
					"unit":       "month",
				},
			},

			// Level-2 nested: worst Kolektibilitas in last 3 months
			{
				Field: "FasilitasList.Fasilitas.RiwayatKolektibilitas.Kolektibilitas",
				Op:    "max",
				Alias: "worst_kol_3m",
				Params: map[string]interface{}{
					"date_field": "Bulan",
					"ref_date":   "2026-04-30",
					"n":          3.0,
					"unit":       "month",
				},
			},

			// Level-1 array: count facilities opened in last 12 months
			{
				Field: "FasilitasList.Fasilitas.TanggalMulai",
				Op:    "count_date_last_n",
				Alias: "new_facilities_12m",
				Params: map[string]interface{}{
					"n":        12.0,
					"ref_date": "2026-04-30",
					"unit":     "month",
				},
			},
		},
	}

	results, err := gator.Aggregate(store, req)
	if err != nil { t.Fatalf("Aggregate error: %v", err) }
	if len(results) != 1 {
		t.Fatalf("expected 1 result row, got %d", len(results))
	}

	b, _ := json.MarshalIndent(results, "", "  ")
	t.Logf("ideb aggregation result:\n%s", string(b))

	row := results[0].(map[string]interface{})

	// Parent-level: Summary.TotalOutstanding = 680000000
	assertEq(t, "total_os_summary", get(t, row, "total_os_summary"), 680000000.0)

	// Level-1 array: 320000000 + 85000000 = 405000000
	assertEq(t, "sum_outstanding", get(t, row, "sum_outstanding"), 405000000.0)

	// Level-2 nested all-time: max across all months all facilities
	// KPR months: 0,0,35,0,0  → max=35
	// KMG months: 45,30        → max=45
	// cross-facility max(35,45) = 45
	assertEq(t, "worst_dpd_alltime", get(t, row, "worst_dpd_alltime"), 45.0)

	// Level-2 nested last 3m (>= 2026-01-30):
	// KPR: 2026-04(0), 2026-03(0), 2026-02(35) → max=35
	// KMG: 2026-04(45), 2026-03(30)             → max=45
	// cross max = 45
	assertEq(t, "worst_dpd_3m", get(t, row, "worst_dpd_3m"), 45.0)

	// Worst Kolektibilitas last 3m:
	// KPR: 2026-04(1), 2026-03(1), 2026-02(2) → max=2
	// KMG: 2026-04(2), 2026-03(2)             → max=2
	// cross max = 2
	assertEq(t, "worst_kol_3m", get(t, row, "worst_kol_3m"), 2.0)

	// New facilities in last 12m (>= 2025-04-30):
	// KRD-001234567: TanggalMulai=2023-06-01 → outside 12m ✗
	// KRD-009876543: TanggalMulai=2024-11-15 → outside 12m ✗
	// Neither opened after 2025-04-30 → 0
	assertEq(t, "new_facilities_12m", get(t, row, "new_facilities_12m"), 0.0)
}

// ── tuxml.xml: mixed parent + level-1 array aggregation ──────────────────────

func TestTUXMLAggregation(t *testing.T) {
	store := loadXMLDataset(t, "/mnt/user-data/uploads/tuxml.xml", "tuxml")

	req := gator.AggregateRequest{
		Dataset: "tuxml",
		// No groupBy → aggregate entire dataset
		Aggregations: []gator.AggConfig{
			// Parent scalar
			{Field: "ResponseSummary.TotalTradeLines", Op: "max", Alias: "total_tl"},

			// Level-1 array: sum Balance across all TradeLines
			{Field: "Product.ConsumerFile.TradeLines.TradeLine.Balance", Op: "sum", Alias: "total_balance"},

			// Level-1 array: max DaysPastDue
			{Field: "Product.ConsumerFile.TradeLines.TradeLine.DaysPastDue", Op: "max", Alias: "worst_dpd"},

			// Level-1 array: count TradeLines opened in last 5 years (>= 2021-04-30)
			{
				Field: "Product.ConsumerFile.TradeLines.TradeLine.DateOpened",
				Op:    "count_date_last_n",
				Alias: "tl_opened_5y",
				Params: map[string]interface{}{
					"n":        5.0,
					"unit":     "year",
					"ref_date": "2026-04-30",
				},
			},
		},
	}

	results, err := gator.Aggregate(store, req)
	if err != nil { t.Fatalf("Aggregate error: %v", err) }
	if len(results) != 1 {
		t.Fatalf("expected 1 result row, got %d", len(results))
	}

	b, _ := json.MarshalIndent(results, "", "  ")
	t.Logf("tuxml aggregation result:\n%s", string(b))

	row := results[0].(map[string]interface{})

	// ResponseSummary.TotalTradeLines = 12 (as stated in XML)
	assertEq(t, "total_tl", get(t, row, "total_tl"), 12.0)

	// Sum balance: 2450 + 18500 + 320000 = 340950
	assertEq(t, "total_balance", get(t, row, "total_balance"), 340950.0)

	// All DaysPastDue = 0 (Mortgage has no DaysPastDue field → ignored)
	assertEq(t, "worst_dpd", get(t, row, "worst_dpd"), 0.0)

	// DateOpened: 2022-03-15, 2023-11-05, 2021-08-20 → all after 2021-04-30 → 3
	assertEq(t, "tl_opened_5y", get(t, row, "tl_opened_5y"), 3.0)
}
