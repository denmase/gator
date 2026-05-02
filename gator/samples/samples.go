// Package samples provides built-in datasets for the Visual Aggregator demo.
// It is intentionally separate from the engine so the engine itself has no
// dependency on domain-specific data shapes.
package samples

import "aggregator/gator"

// Register loads all built-in sample datasets into the given Store.
func Register(store *gator.Store) {
	store.Register("employees", employees())
}

// ── Delinquency / collectability helpers ────────────────────────────────────

// dh encodes a 24-character string into a 24-element delinquency history slice.
// Character → days-past-due mapping:
//
//	'0'=0  '3'=30  '5'=45  '6'=60  '9'=90
//	'a'=100 'c'=120 'f'=150 'h'=200
func dh(s string) []interface{} {
	enc := map[byte]float64{
		'0': 0, '3': 30, '5': 45, '6': 60, '9': 90,
		'a': 100, 'c': 120, 'f': 150, 'h': 200,
	}
	hist := make([]interface{}, 24)
	for i := 0; i < 24 && i < len(s); i++ {
		hist[i] = enc[s[i]]
	}
	return hist
}

func delqToColl(d float64) float64 {
	switch {
	case d == 0:
		return 1
	case d <= 90:
		return 2
	case d <= 120:
		return 3
	case d <= 180:
		return 4
	default:
		return 5
	}
}

func collHist(delqHistory []interface{}) []interface{} {
	out := make([]interface{}, len(delqHistory))
	for i, v := range delqHistory {
		d, _ := gator.ToFloat64(v)
		out[i] = delqToColl(d)
	}
	return out
}

// credit builds one credit facility record.
func credit(accountNo, openDate string, limit, balance float64,
	productType, loanStatus string, delqStr string) map[string]interface{} {
	dHist := dh(delqStr)
	cHist := collHist(dHist)
	lastDelq, _ := gator.ToFloat64(dHist[23])
	lastColl, _ := gator.ToFloat64(cHist[23])
	return map[string]interface{}{
		"account_no":          accountNo,
		"open_date":           openDate,
		"initial_limit":       limit,
		"product_type":        productType,
		"outstanding_balance": balance,
		"loan_status":         loanStatus,
		"collectability_code": lastColl,
		"delinquency":         lastDelq,
		"last_24_delq_hist":   dHist,
		"last_24_coll_hist":   cHist,
	}
}

// ── Employee records ─────────────────────────────────────────────────────────

func employees() []interface{} {
	return []interface{}{
		map[string]interface{}{
			"name": "Andi", "employee_id": "EMP-001",
			"department": "Risk Management", "city": "Jakarta",
			"age": 35.0, "years_of_service": 8.0, "salary": 25000000.0,
			"credits": []interface{}{
				credit("CC-001", "2022-01-15", 50000000, 15000000, "credit card", "active", "000000000000000000000000"),
				credit("PL-002", "2021-06-20", 100000000, 45000000, "personal loan", "active", "000000000000000000000000"),
				credit("MG-003", "2019-03-10", 500000000, 420000000, "mortgage", "active", "000000000000000000000000"),
				credit("CC-004", "2023-08-05", 30000000, 28000000, "credit card", "active", "00000000000000000000aaa"),
				credit("PY-005", "2025-03-10", 10000000, 5000000, "paylater", "active", "000000000000000000000000"),
			},
		},
		map[string]interface{}{
			"name": "Budi", "employee_id": "EMP-002",
			"department": "IT", "city": "Bandung",
			"age": 42.0, "years_of_service": 15.0, "salary": 35000000.0,
			"credits": []interface{}{
				credit("CC-010", "2020-03-15", 80000000, 25000000, "credit card", "active", "000000000000000000000000"),
				credit("CC-011", "2021-07-22", 60000000, 55000000, "credit card", "active", "00000000000000000005aaa"),
				credit("CC-012", "2022-11-30", 40000000, 38000000, "credit card", "active", "000000000000000000066fff"),
				credit("CC-013", "2023-04-18", 25000000, 0, "credit card", "paid-off", "a0a060300000000000000000"),
				credit("PL-014", "2020-09-01", 200000000, 120000000, "personal loan", "active", "000000000000000000000000"),
				credit("PL-015", "2022-02-14", 75000000, 60000000, "personal loan", "active", "000000000000000000000555"),
				credit("PL-016", "2023-10-05", 50000000, 48000000, "personal loan", "active", "000000000000000000000066"),
				credit("MG-017", "2018-05-20", 800000000, 650000000, "mortgage", "active", "000000000000000000000000"),
				credit("MP-018", "2025-04-01", 30000000, 15000000, "multipurpose", "active", "000000000000000000000000"),
				credit("PY-019", "2025-05-15", 5000000, 3000000, "paylater", "active", "000000000000000000000000"),
			},
		},
		map[string]interface{}{
			"name": "Citra", "employee_id": "EMP-003",
			"department": "Finance", "city": "Jakarta",
			"age": 28.0, "years_of_service": 3.0, "salary": 18000000.0,
			"credits": []interface{}{
				credit("CC-020", "2025-01-15", 20000000, 8000000, "credit card", "active", "000000000000000000000000"),
			},
		},
		map[string]interface{}{
			"name": "Dewi", "employee_id": "EMP-004",
			"department": "Marketing", "city": "Surabaya",
			"age": 31.0, "years_of_service": 5.0, "salary": 20000000.0,
			"credits": []interface{}{},
		},
		map[string]interface{}{
			"name": "Eka", "employee_id": "EMP-005",
			"department": "IT", "city": "Jakarta",
			"age": 29.0, "years_of_service": 4.0, "salary": 22000000.0,
			"credits": []interface{}{
				credit("CC-030", "2022-05-20", 35000000, 20000000, "credit card", "active", "000000000000000000000000"),
				credit("PL-031", "2023-03-15", 50000000, 35000000, "personal loan", "active", "000000000000000000000333"),
				credit("PY-032", "2025-05-20", 8000000, 2000000, "paylater", "active", "000000000000000000000000"),
			},
		},
		map[string]interface{}{
			"name": "Fajar", "employee_id": "EMP-006",
			"department": "Risk Management", "city": "Bandung",
			"age": 38.0, "years_of_service": 12.0, "salary": 30000000.0,
			"credits": []interface{}{
				credit("CC-040", "2019-08-10", 70000000, 45000000, "credit card", "active", "606003000000000000000000"),
				credit("CC-041", "2020-12-05", 55000000, 52000000, "credit card", "active", "000000000000000000356aaa"),
				credit("CC-042", "2022-04-18", 40000000, 0, "credit card", "written-off", "000000000000000666aaahhh"),
				credit("PL-043", "2021-01-25", 150000000, 90000000, "personal loan", "active", "000000000000000000000000"),
				credit("PL-044", "2022-09-30", 80000000, 75000000, "personal loan", "restructured", "000000000000000000055aaa"),
				credit("MG-045", "2017-11-15", 600000000, 500000000, "mortgage", "active", "000000000000000000000000"),
				credit("MP-046", "2025-03-30", 25000000, 10000000, "multipurpose", "active", "000000000000000000000000"),
			},
		},
		map[string]interface{}{
			"name": "Gina", "employee_id": "EMP-007",
			"department": "Finance", "city": "Surabaya",
			"age": 33.0, "years_of_service": 6.0, "salary": 24000000.0,
			"credits": []interface{}{
				credit("CC-050", "2021-03-08", 45000000, 30000000, "credit card", "active", "000000000000000000000000"),
				credit("PL-051", "2025-04-25", 60000000, 55000000, "personal loan", "active", "000000000000000000000033"),
			},
		},
		map[string]interface{}{
			"name": "Hendra", "employee_id": "EMP-008",
			"department": "Marketing", "city": "Jakarta",
			"age": 45.0, "years_of_service": 20.0, "salary": 40000000.0,
			"credits": []interface{}{
				credit("CC-060", "2018-06-12", 100000000, 60000000, "credit card", "active", "000000000000000000000000"),
				credit("CC-061", "2020-09-25", 75000000, 70000000, "credit card", "active", "000000000000000000666fff"),
				credit("PL-062", "2019-12-01", 250000000, 180000000, "personal loan", "active", "000000000000000000000000"),
				credit("MG-063", "2016-04-15", 1000000000, 800000000, "mortgage", "active", "000000000000000000000000"),
			},
		},
		map[string]interface{}{
			"name": "Irfan", "employee_id": "EMP-009",
			"department": "IT", "city": "Surabaya",
			"age": 26.0, "years_of_service": 2.0, "salary": 15000000.0,
			"credits": []interface{}{
				credit("CC-070", "2024-06-01", 15000000, 12000000, "credit card", "active", "000000000000000000000000"),
				credit("CC-071", "2024-09-15", 10000000, 8000000, "credit card", "active", "000000000000000000003333"),
				credit("CC-072", "2025-01-20", 20000000, 18000000, "credit card", "active", "0000000000000000000036aa"),
				credit("PL-073", "2024-03-10", 30000000, 25000000, "personal loan", "active", "000000000000000000000000"),
				credit("PL-074", "2025-04-05", 25000000, 20000000, "personal loan", "active", "000000000000000000000036"),
				credit("MP-075", "2025-05-10", 15000000, 5000000, "multipurpose", "active", "000000000000000000000000"),
			},
		},
	}
}
