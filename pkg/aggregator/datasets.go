package aggregator

import (
	"encoding/json"
	"fmt"
)

// datasets contains built-in example datasets
var datasets = map[string]string{
	"employees": employeesJSON,
}

// GetDataset returns a built-in dataset by name
func GetDataset(name string) []map[string]interface{} {
	jsonStr, ok := datasets[name]
	if !ok {
		return nil
	}

	var data []map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		fmt.Printf("Error parsing dataset %s: %v\n", name, err)
		return nil
	}

	return data
}

// GetAvailableDatasets returns list of available dataset names
func GetAvailableDatasets() []string {
	names := make([]string, 0, len(datasets))
	for name := range datasets {
		names = append(names, name)
	}
	return names
}

// employeesJSON is the mock employee data with nested credit accounts
const employeesJSON = `[
  {
    "employee_id": "EMP001",
    "name": "Andi Pratama",
    "department": "Engineering",
    "city": "Jakarta",
    "age": 32,
    "years_of_service": 8,
    "salary": 15000000,
    "credits": [
      {
        "account_no": "CC-001-2020",
        "open_date": "2020-03-15",
        "initial_limit": 50000000,
        "product_type": "credit card",
        "outstanding_balance": 12500000,
        "loan_status": "active",
        "collectability_code": 1,
        "delinquency": 0,
        "last_24_delq_hist": [0, 0, 0, 0, 0, 0, 5, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
        "last_24_coll_hist": [1, 1, 1, 1, 1, 1, 2, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1]
      },
      {
        "account_no": "PL-001-2021",
        "open_date": "2021-06-20",
        "initial_limit": 100000000,
        "product_type": "personal loan",
        "outstanding_balance": 45000000,
        "loan_status": "active",
        "collectability_code": 1,
        "delinquency": 0,
        "last_24_delq_hist": [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
        "last_24_coll_hist": [1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1]
      },
      {
        "account_no": "MTG-001-2019",
        "open_date": "2019-01-10",
        "initial_limit": 500000000,
        "product_type": "mortgage",
        "outstanding_balance": 350000000,
        "loan_status": "active",
        "collectability_code": 1,
        "delinquency": 0,
        "last_24_delq_hist": [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
        "last_24_coll_hist": [1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1]
      },
      {
        "account_no": "MP-001-2022",
        "open_date": "2022-09-05",
        "initial_limit": 75000000,
        "product_type": "multipurpose",
        "outstanding_balance": 25000000,
        "loan_status": "active",
        "collectability_code": 1,
        "delinquency": 0,
        "last_24_delq_hist": [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
        "last_24_coll_hist": [1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1]
      },
      {
        "account_no": "PAY-001-2023",
        "open_date": "2023-02-14",
        "initial_limit": 10000000,
        "product_type": "paylater",
        "outstanding_balance": 3500000,
        "loan_status": "active",
        "collectability_code": 1,
        "delinquency": 0,
        "last_24_delq_hist": [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
        "last_24_coll_hist": [1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1]
      }
    ]
  },
  {
    "employee_id": "EMP002",
    "name": "Budi Santoso",
    "department": "Finance",
    "city": "Surabaya",
    "age": 45,
    "years_of_service": 15,
    "salary": 22000000,
    "credits": [
      {
        "account_no": "CC-002-2018",
        "open_date": "2018-05-20",
        "initial_limit": 80000000,
        "product_type": "credit card",
        "outstanding_balance": 25000000,
        "loan_status": "active",
        "collectability_code": 1,
        "delinquency": 0,
        "last_24_delq_hist": [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
        "last_24_coll_hist": [1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1]
      },
      {
        "account_no": "CC-002-2019",
        "open_date": "2019-08-15",
        "initial_limit": 60000000,
        "product_type": "credit card",
        "outstanding_balance": 18000000,
        "loan_status": "active",
        "collectability_code": 1,
        "delinquency": 0,
        "last_24_delq_hist": [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
        "last_24_coll_hist": [1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1]
      },
      {
        "account_no": "PL-002-2020",
        "open_date": "2020-02-10",
        "initial_limit": 150000000,
        "product_type": "personal loan",
        "outstanding_balance": 75000000,
        "loan_status": "active",
        "collectability_code": 2,
        "delinquency": 45,
        "last_24_delq_hist": [45, 30, 15, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
        "last_24_coll_hist": [2, 2, 2, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1]
      },
      {
        "account_no": "MTG-002-2017",
        "open_date": "2017-11-25",
        "initial_limit": 800000000,
        "product_type": "mortgage",
        "outstanding_balance": 520000000,
        "loan_status": "active",
        "collectability_code": 1,
        "delinquency": 0,
        "last_24_delq_hist": [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
        "last_24_coll_hist": [1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1]
      },
      {
        "account_no": "MP-002-2021",
        "open_date": "2021-04-18",
        "initial_limit": 50000000,
        "product_type": "multipurpose",
        "outstanding_balance": 20000000,
        "loan_status": "paid-off",
        "collectability_code": 1,
        "delinquency": 0,
        "last_24_delq_hist": [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
        "last_24_coll_hist": [1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1]
      },
      {
        "account_no": "CC-002-2022",
        "open_date": "2022-07-22",
        "initial_limit": 40000000,
        "product_type": "credit card",
        "outstanding_balance": 8500000,
        "loan_status": "active",
        "collectability_code": 1,
        "delinquency": 0,
        "last_24_delq_hist": [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
        "last_24_coll_hist": [1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1]
      },
      {
        "account_no": "PAY-002-2023",
        "open_date": "2023-01-08",
        "initial_limit": 15000000,
        "product_type": "paylater",
        "outstanding_balance": 5200000,
        "loan_status": "active",
        "collectability_code": 1,
        "delinquency": 0,
        "last_24_delq_hist": [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
        "last_24_coll_hist": [1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1]
      },
      {
        "account_no": "PL-002-2023",
        "open_date": "2023-06-12",
        "initial_limit": 200000000,
        "product_type": "personal loan",
        "outstanding_balance": 180000000,
        "loan_status": "active",
        "collectability_code": 1,
        "delinquency": 0,
        "last_24_delq_hist": [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
        "last_24_coll_hist": [1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1]
      },
      {
        "account_no": "CC-002-2024",
        "open_date": "2024-03-01",
        "initial_limit": 35000000,
        "product_type": "credit card",
        "outstanding_balance": 12000000,
        "loan_status": "active",
        "collectability_code": 1,
        "delinquency": 0,
        "last_24_delq_hist": [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
        "last_24_coll_hist": [1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1]
      },
      {
        "account_no": "MP-002-2024",
        "open_date": "2024-08-20",
        "initial_limit": 80000000,
        "product_type": "multipurpose",
        "outstanding_balance": 70000000,
        "loan_status": "active",
        "collectability_code": 1,
        "delinquency": 0,
        "last_24_delq_hist": [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
        "last_24_coll_hist": [1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1]
      }
    ]
  },
  {
    "employee_id": "EMP003",
    "name": "Citra Dewi",
    "department": "Marketing",
    "city": "Bandung",
    "age": 28,
    "years_of_service": 3,
    "salary": 12000000,
    "credits": [
      {
        "account_no": "CC-003-2022",
        "open_date": "2022-04-10",
        "initial_limit": 30000000,
        "product_type": "credit card",
        "outstanding_balance": 8500000,
        "loan_status": "active",
        "collectability_code": 1,
        "delinquency": 0,
        "last_24_delq_hist": [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
        "last_24_coll_hist": [1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1]
      }
    ]
  },
  {
    "employee_id": "EMP004",
    "name": "Dewi Lestari",
    "department": "HR",
    "city": "Jakarta",
    "age": 35,
    "years_of_service": 7,
    "salary": 14000000,
    "credits": []
  },
  {
    "employee_id": "EMP005",
    "name": "Eko Wijaya",
    "department": "Engineering",
    "city": "Yogyakarta",
    "age": 29,
    "years_of_service": 4,
    "salary": 13500000,
    "credits": [
      {
        "account_no": "CC-005-2021",
        "open_date": "2021-09-15",
        "initial_limit": 45000000,
        "product_type": "credit card",
        "outstanding_balance": 15000000,
        "loan_status": "active",
        "collectability_code": 2,
        "delinquency": 35,
        "last_24_delq_hist": [35, 20, 10, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
        "last_24_coll_hist": [2, 2, 2, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1]
      },
      {
        "account_no": "PL-005-2022",
        "open_date": "2022-11-20",
        "initial_limit": 80000000,
        "product_type": "personal loan",
        "outstanding_balance": 55000000,
        "loan_status": "active",
        "collectability_code": 1,
        "delinquency": 0,
        "last_24_delq_hist": [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
        "last_24_coll_hist": [1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1]
      },
      {
        "account_no": "PAY-005-2023",
        "open_date": "2023-05-08",
        "initial_limit": 12000000,
        "product_type": "paylater",
        "outstanding_balance": 4200000,
        "loan_status": "active",
        "collectability_code": 1,
        "delinquency": 0,
        "last_24_delq_hist": [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
        "last_24_coll_hist": [1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1]
      }
    ]
  },
  {
    "employee_id": "EMP006",
    "name": "Fajar Nugraha",
    "department": "Finance",
    "city": "Medan",
    "age": 41,
    "years_of_service": 12,
    "salary": 19000000,
    "credits": [
      {
        "account_no": "MTG-006-2016",
        "open_date": "2016-07-14",
        "initial_limit": 650000000,
        "product_type": "mortgage",
        "outstanding_balance": 380000000,
        "loan_status": "active",
        "collectability_code": 1,
        "delinquency": 0,
        "last_24_delq_hist": [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
        "last_24_coll_hist": [1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1]
      },
      {
        "account_no": "CC-006-2019",
        "open_date": "2019-03-22",
        "initial_limit": 70000000,
        "product_type": "credit card",
        "outstanding_balance": 22000000,
        "loan_status": "active",
        "collectability_code": 3,
        "delinquency": 95,
        "last_24_delq_hist": [95, 120, 85, 60, 30, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
        "last_24_coll_hist": [3, 3, 3, 2, 2, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1]
      }
    ]
  },
  {
    "employee_id": "EMP007",
    "name": "Grace Tan",
    "department": "Engineering",
    "city": "Jakarta",
    "age": 33,
    "years_of_service": 6,
    "salary": 16500000,
    "credits": [
      {
        "account_no": "CC-007-2020",
        "open_date": "2020-08-18",
        "initial_limit": 55000000,
        "product_type": "credit card",
        "outstanding_balance": 18500000,
        "loan_status": "active",
        "collectability_code": 1,
        "delinquency": 0,
        "last_24_delq_hist": [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
        "last_24_coll_hist": [1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1]
      },
      {
        "account_no": "PL-007-2021",
        "open_date": "2021-12-05",
        "initial_limit": 120000000,
        "product_type": "personal loan",
        "outstanding_balance": 68000000,
        "loan_status": "active",
        "collectability_code": 1,
        "delinquency": 0,
        "last_24_delq_hist": [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
        "last_24_coll_hist": [1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1]
      }
    ]
  },
  {
    "employee_id": "EMP008",
    "name": "Hendra Gunawan",
    "department": "Marketing",
    "city": "Surabaya",
    "age": 38,
    "years_of_service": 9,
    "salary": 17000000,
    "credits": [
      {
        "account_no": "CC-008-2019",
        "open_date": "2019-06-30",
        "initial_limit": 65000000,
        "product_type": "credit card",
        "outstanding_balance": 28000000,
        "loan_status": "active",
        "collectability_code": 1,
        "delinquency": 0,
        "last_24_delq_hist": [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
        "last_24_coll_hist": [1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1]
      },
      {
        "account_no": "MP-008-2020",
        "open_date": "2020-10-12",
        "initial_limit": 90000000,
        "product_type": "multipurpose",
        "outstanding_balance": 42000000,
        "loan_status": "active",
        "collectability_code": 1,
        "delinquency": 0,
        "last_24_delq_hist": [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
        "last_24_coll_hist": [1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1]
      },
      {
        "account_no": "PAY-008-2022",
        "open_date": "2022-02-28",
        "initial_limit": 18000000,
        "product_type": "paylater",
        "outstanding_balance": 6800000,
        "loan_status": "active",
        "collectability_code": 1,
        "delinquency": 0,
        "last_24_delq_hist": [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
        "last_24_coll_hist": [1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1]
      }
    ]
  },
  {
    "employee_id": "EMP009",
    "name": "Indah Sari",
    "department": "HR",
    "city": "Bandung",
    "age": 31,
    "years_of_service": 5,
    "salary": 13000000,
    "credits": [
      {
        "account_no": "CC-009-2021",
        "open_date": "2021-07-19",
        "initial_limit": 35000000,
        "product_type": "credit card",
        "outstanding_balance": 9200000,
        "loan_status": "active",
        "collectability_code": 1,
        "delinquency": 0,
        "last_24_delq_hist": [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
        "last_24_coll_hist": [1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1]
      },
      {
        "account_no": "PL-009-2023",
        "open_date": "2023-04-25",
        "initial_limit": 100000000,
        "product_type": "personal loan",
        "outstanding_balance": 85000000,
        "loan_status": "active",
        "collectability_code": 4,
        "delinquency": 150,
        "last_24_delq_hist": [150, 180, 165, 140, 120, 95, 60, 30, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
        "last_24_coll_hist": [4, 4, 4, 4, 3, 3, 2, 2, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1]
      }
    ]
  },
  {
    "employee_id": "EMP010",
    "name": "Joko Susilo",
    "department": "Engineering",
    "city": "Jakarta",
    "age": 27,
    "years_of_service": 2,
    "salary": 11000000,
    "credits": [
      {
        "account_no": "PAY-010-2023",
        "open_date": "2023-08-15",
        "initial_limit": 8000000,
        "product_type": "paylater",
        "outstanding_balance": 2500000,
        "loan_status": "active",
        "collectability_code": 1,
        "delinquency": 0,
        "last_24_delq_hist": [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
        "last_24_coll_hist": [1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1]
      },
      {
        "account_no": "CC-010-2024",
        "open_date": "2024-01-20",
        "initial_limit": 25000000,
        "product_type": "credit card",
        "outstanding_balance": 7800000,
        "loan_status": "active",
        "collectability_code": 1,
        "delinquency": 0,
        "last_24_delq_hist": [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
        "last_24_coll_hist": [1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1]
      }
    ]
  }
]`
