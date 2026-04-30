package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ============================================================================
// DATA STRUCTURES
// ============================================================================

// CreditAccount represents a credit/loan account for an employee
type CreditAccount struct {
	AccountNo       string    `json:"account_no"`
	OpenDate        string    `json:"open_date"` // YYYY-MM-DD format
	InitialLimit    float64   `json:"initial_limit"`
	ProductType     string    `json:"product_type"` // "credit card", "personal loan", "mortgage", "multipurpose", "paylater"
	OutstandingBal  float64   `json:"outstanding_balance"`
	LoanStatus      string    `json:"loan_status"` // "active", "paid-off", "written-off", "restructured"
	CollectabilityCode int    `json:"collectability_code"` // 1-5
	Delinquency     int       `json:"delinquency"` // days past due
	Last24DelqHist  []int     `json:"last_24_delq_hist"`  // last 24 months delinquency days
	Last24CollHist  []int     `json:"last_24_coll_hist"`  // last 24 months collectability codes
}

// Employee represents the main data structure with nested credit accounts
type Employee struct {
	ID           int            `json:"id"`
	Name         string         `json:"name"`
	Department   string         `json:"department"`
	City         string         `json:"city"`
	Age          int            `json:"age"`
	Salary       float64        `json:"salary"`
	YearsOfWork  int            `json:"years_of_work"`
	Credits      []CreditAccount `json:"credits"`
}

// ============================================================================
// MOCK DATA GENERATION
// ============================================================================

func generateMockData() []Employee {
	now := time.Now()
	
	data := []Employee{
		{
			ID: 1, Name: "Andi", Department: "Sales", City: "Jakarta", Age: 35, Salary: 15000000, YearsOfWork: 8,
			Credits: []CreditAccount{
				{AccountNo: "CC001", OpenDate: now.AddDate(-3, -6, 0).Format("2006-01-02"), InitialLimit: 50000000, ProductType: "credit card", OutstandingBal: 25000000, LoanStatus: "active", CollectabilityCode: 1, Delinquency: 0, Last24DelqHist: make([]int, 24), Last24CollHist: repeatInt(1, 24)},
				{AccountNo: "PL001", OpenDate: now.AddDate(-2, -3, 0).Format("2006-01-02"), InitialLimit: 100000000, ProductType: "personal loan", OutstandingBal: 45000000, LoanStatus: "active", CollectabilityCode: 2, Delinquency: 45, Last24DelqHist: concatInts(repeatInt(0, 20), repeatInt(30, 2)), Last24CollHist: concatInts(repeatInt(1, 20), repeatInt(2, 2))},
				{AccountNo: "MTG001", OpenDate: now.AddDate(-5, 0, 0).Format("2006-01-02"), InitialLimit: 500000000, ProductType: "mortgage", OutstandingBal: 350000000, LoanStatus: "active", CollectabilityCode: 1, Delinquency: 0, Last24DelqHist: make([]int, 24), Last24CollHist: repeatInt(1, 24)},
				{AccountNo: "MP001", OpenDate: now.AddDate(-1, -6, 0).Format("2006-01-02"), InitialLimit: 30000000, ProductType: "multipurpose", OutstandingBal: 10000000, LoanStatus: "active", CollectabilityCode: 1, Delinquency: 0, Last24DelqHist: make([]int, 24), Last24CollHist: repeatInt(1, 24)},
				{AccountNo: "PL002", OpenDate: now.AddDate(-4, -2, 0).Format("2006-01-02"), InitialLimit: 75000000, ProductType: "personal loan", OutstandingBal: 0, LoanStatus: "paid-off", CollectabilityCode: 1, Delinquency: 0, Last24DelqHist: concatInts(repeatInt(0, 22), []int{15, 0}), Last24CollHist: concatInts(repeatInt(1, 22), []int{2, 1})},
			},
		},
		{
			ID: 2, Name: "Budi", Department: "Engineering", City: "Bandung", Age: 42, Salary: 22000000, YearsOfWork: 12,
			Credits: []CreditAccount{
				{AccountNo: "CC002", OpenDate: now.AddDate(-4, -1, 0).Format("2006-01-02"), InitialLimit: 80000000, ProductType: "credit card", OutstandingBal: 60000000, LoanStatus: "active", CollectabilityCode: 3, Delinquency: 95, Last24DelqHist: concatInts(repeatInt(0, 18), repeatInt(60, 3), repeatInt(95, 3)), Last24CollHist: concatInts(repeatInt(1, 18), repeatInt(2, 3), repeatInt(3, 3))},
				{AccountNo: "CC003", OpenDate: now.AddDate(-2, -8, 0).Format("2006-01-02"), InitialLimit: 40000000, ProductType: "credit card", OutstandingBal: 35000000, LoanStatus: "active", CollectabilityCode: 2, Delinquency: 30, Last24DelqHist: concatInts(repeatInt(0, 21), repeatInt(30, 3)), Last24CollHist: concatInts(repeatInt(1, 21), repeatInt(2, 3))},
				{AccountNo: "PL003", OpenDate: now.AddDate(-1, -2, 0).Format("2006-01-02"), InitialLimit: 50000000, ProductType: "personal loan", OutstandingBal: 40000000, LoanStatus: "active", CollectabilityCode: 1, Delinquency: 0, Last24DelqHist: make([]int, 24), Last24CollHist: repeatInt(1, 24)},
				{AccountNo: "PAY001", OpenDate: now.AddDate(-0, -8, 0).Format("2006-01-02"), InitialLimit: 10000000, ProductType: "paylater", OutstandingBal: 5000000, LoanStatus: "active", CollectabilityCode: 1, Delinquency: 0, Last24DelqHist: make([]int, 24), Last24CollHist: repeatInt(1, 24)},
				{AccountNo: "MP002", OpenDate: now.AddDate(-3, -4, 0).Format("2006-01-02"), InitialLimit: 25000000, ProductType: "multipurpose", OutstandingBal: 8000000, LoanStatus: "active", CollectabilityCode: 1, Delinquency: 0, Last24DelqHist: make([]int, 24), Last24CollHist: repeatInt(1, 24)},
				{AccountNo: "CC004", OpenDate: now.AddDate(-5, -6, 0).Format("2006-01-02"), InitialLimit: 60000000, ProductType: "credit card", OutstandingBal: 0, LoanStatus: "paid-off", CollectabilityCode: 1, Delinquency: 0, Last24DelqHist: concatInts(repeatInt(0, 20), repeatInt(45, 2), repeatInt(0, 2)), Last24CollHist: concatInts(repeatInt(1, 20), repeatInt(2, 2), repeatInt(1, 2))},
				{AccountNo: "PL004", OpenDate: now.AddDate(-2, -1, 0).Format("2006-01-02"), InitialLimit: 80000000, ProductType: "personal loan", OutstandingBal: 65000000, LoanStatus: "active", CollectabilityCode: 2, Delinquency: 60, Last24DelqHist: concatInts(repeatInt(0, 19), repeatInt(45, 3), repeatInt(60, 2)), Last24CollHist: concatInts(repeatInt(1, 19), repeatInt(2, 5))},
				{AccountNo: "MTG002", OpenDate: now.AddDate(-6, -3, 0).Format("2006-01-02"), InitialLimit: 700000000, ProductType: "mortgage", OutstandingBal: 500000000, LoanStatus: "active", CollectabilityCode: 1, Delinquency: 0, Last24DelqHist: make([]int, 24), Last24CollHist: repeatInt(1, 24)},
				{AccountNo: "PAY002", OpenDate: now.AddDate(-0, -4, 0).Format("2006-01-02"), InitialLimit: 5000000, ProductType: "paylater", OutstandingBal: 2000000, LoanStatus: "active", CollectabilityCode: 1, Delinquency: 0, Last24DelqHist: make([]int, 24), Last24CollHist: repeatInt(1, 24)},
				{AccountNo: "CC005", OpenDate: now.AddDate(-1, -10, 0).Format("2006-01-02"), InitialLimit: 35000000, ProductType: "credit card", OutstandingBal: 28000000, LoanStatus: "active", CollectabilityCode: 1, Delinquency: 0, Last24DelqHist: make([]int, 24), Last24CollHist: repeatInt(1, 24)},
			},
		},
		{
			ID: 3, Name: "Citra", Department: "Marketing", City: "Surabaya", Age: 29, Salary: 12000000, YearsOfWork: 4,
			Credits: []CreditAccount{
				{AccountNo: "CC006", OpenDate: now.AddDate(-1, -5, 0).Format("2006-01-02"), InitialLimit: 30000000, ProductType: "credit card", OutstandingBal: 18000000, LoanStatus: "active", CollectabilityCode: 1, Delinquency: 0, Last24DelqHist: make([]int, 24), Last24CollHist: repeatInt(1, 24)},
			},
		},
		{
			ID: 4, Name: "Dewi", Department: "HR", City: "Jakarta", Age: 38, Salary: 14000000, YearsOfWork: 10,
			Credits: []CreditAccount{},
		},
		{
			ID: 5, Name: "Eka", Department: "Finance", City: "Medan", Age: 45, Salary: 20000000, YearsOfWork: 15,
			Credits: []CreditAccount{
				{AccountNo: "MTG003", OpenDate: now.AddDate(-7, -2, 0).Format("2006-01-02"), InitialLimit: 600000000, ProductType: "mortgage", OutstandingBal: 280000000, LoanStatus: "active", CollectabilityCode: 1, Delinquency: 0, Last24DelqHist: make([]int, 24), Last24CollHist: repeatInt(1, 24)},
				{AccountNo: "CC007", OpenDate: now.AddDate(-3, -8, 0).Format("2006-01-02"), InitialLimit: 70000000, ProductType: "credit card", OutstandingBal: 45000000, LoanStatus: "active", CollectabilityCode: 2, Delinquency: 35, Last24DelqHist: concatInts(repeatInt(0, 20), repeatInt(20, 2), repeatInt(35, 2)), Last24CollHist: concatInts(repeatInt(1, 20), repeatInt(2, 4))},
				{AccountNo: "PL005", OpenDate: now.AddDate(-0, -6, 0).Format("2006-01-02"), InitialLimit: 40000000, ProductType: "personal loan", OutstandingBal: 35000000, LoanStatus: "active", CollectabilityCode: 1, Delinquency: 0, Last24DelqHist: make([]int, 24), Last24CollHist: repeatInt(1, 24)},
			},
		},
		{
			ID: 6, Name: "Fajar", Department: "Sales", City: "Makassar", Age: 31, Salary: 13000000, YearsOfWork: 5,
			Credits: []CreditAccount{
				{AccountNo: "CC008", OpenDate: now.AddDate(-2, -4, 0).Format("2006-01-02"), InitialLimit: 45000000, ProductType: "credit card", OutstandingBal: 40000000, LoanStatus: "active", CollectabilityCode: 4, Delinquency: 150, Last24DelqHist: concatInts(repeatInt(0, 15), repeatInt(60, 4), repeatInt(120, 3), repeatInt(150, 2)), Last24CollHist: concatInts(repeatInt(1, 15), repeatInt(2, 4), repeatInt(4, 3), repeatInt(4, 2))},
				{AccountNo: "PL006", OpenDate: now.AddDate(-1, -8, 0).Format("2006-01-02"), InitialLimit: 35000000, ProductType: "personal loan", OutstandingBal: 28000000, LoanStatus: "active", CollectabilityCode: 3, Delinquency: 105, Last24DelqHist: concatInts(repeatInt(0, 18), repeatInt(75, 4), repeatInt(105, 2)), Last24CollHist: concatInts(repeatInt(1, 18), repeatInt(2, 4), repeatInt(3, 2))},
			},
		},
		{
			ID: 7, Name: "Grace", Department: "Engineering", City: "Jakarta", Age: 27, Salary: 16000000, YearsOfWork: 3,
			Credits: []CreditAccount{
				{AccountNo: "PAY003", OpenDate: now.AddDate(-0, -3, 0).Format("2006-01-02"), InitialLimit: 8000000, ProductType: "paylater", OutstandingBal: 3000000, LoanStatus: "active", CollectabilityCode: 1, Delinquency: 0, Last24DelqHist: make([]int, 24), Last24CollHist: repeatInt(1, 24)},
				{AccountNo: "CC009", OpenDate: now.AddDate(-1, -2, 0).Format("2006-01-02"), InitialLimit: 25000000, ProductType: "credit card", OutstandingBal: 12000000, LoanStatus: "active", CollectabilityCode: 1, Delinquency: 0, Last24DelqHist: make([]int, 24), Last24CollHist: repeatInt(1, 24)},
			},
		},
		{
			ID: 8, Name: "Hadi", Department: "Operations", City: "Semarang", Age: 50, Salary: 25000000, YearsOfWork: 20,
			Credits: []CreditAccount{
				{AccountNo: "MTG004", OpenDate: now.AddDate(-10, -1, 0).Format("2006-01-02"), InitialLimit: 800000000, ProductType: "mortgage", OutstandingBal: 200000000, LoanStatus: "active", CollectabilityCode: 1, Delinquency: 0, Last24DelqHist: make([]int, 24), Last24CollHist: repeatInt(1, 24)},
				{AccountNo: "CC010", OpenDate: now.AddDate(-6, -5, 0).Format("2006-01-02"), InitialLimit: 100000000, ProductType: "credit card", OutstandingBal: 30000000, LoanStatus: "active", CollectabilityCode: 1, Delinquency: 0, Last24DelqHist: concatInts(repeatInt(0, 21), repeatInt(25, 2), []int{0}), Last24CollHist: concatInts(repeatInt(1, 21), repeatInt(2, 2), []int{1})},
				{AccountNo: "PL007", OpenDate: now.AddDate(-3, -6, 0).Format("2006-01-02"), InitialLimit: 120000000, ProductType: "personal loan", OutstandingBal: 55000000, LoanStatus: "active", CollectabilityCode: 2, Delinquency: 55, Last24DelqHist: concatInts(repeatInt(0, 19), repeatInt(40, 3), repeatInt(55, 2)), Last24CollHist: concatInts(repeatInt(1, 19), repeatInt(2, 5))},
				{AccountNo: "MP003", OpenDate: now.AddDate(-2, -2, 0).Format("2006-01-02"), InitialLimit: 50000000, ProductType: "multipurpose", OutstandingBal: 32000000, LoanStatus: "active", CollectabilityCode: 1, Delinquency: 0, Last24DelqHist: make([]int, 24), Last24CollHist: repeatInt(1, 24)},
			},
		},
		{
			ID: 9, Name: "Indah", Department: "Marketing", City: "Bandung", Age: 33, Salary: 15000000, YearsOfWork: 7,
			Credits: []CreditAccount{
				{AccountNo: "CC011", OpenDate: now.AddDate(-2, -6, 0).Format("2006-01-02"), InitialLimit: 55000000, ProductType: "credit card", OutstandingBal: 48000000, LoanStatus: "active", CollectabilityCode: 5, Delinquency: 210, Last24DelqHist: concatInts(repeatInt(0, 12), repeatInt(45, 4), repeatInt(100, 4), repeatInt(180, 4)), Last24CollHist: concatInts(repeatInt(1, 12), repeatInt(2, 4), repeatInt(4, 4), repeatInt(5, 4))},
				{AccountNo: "PL008", OpenDate: now.AddDate(-1, -4, 0).Format("2006-01-02"), InitialLimit: 60000000, ProductType: "personal loan", OutstandingBal: 52000000, LoanStatus: "restructured", CollectabilityCode: 4, Delinquency: 165, Last24DelqHist: concatInts(repeatInt(0, 16), repeatInt(80, 4), repeatInt(140, 3), []int{165}), Last24CollHist: concatInts(repeatInt(1, 16), repeatInt(3, 4), repeatInt(4, 4))},
			},
		},
		{
			ID: 10, Name: "Joko", Department: "Finance", City: "Surabaya", Age: 40, Salary: 19000000, YearsOfWork: 11,
			Credits: []CreditAccount{
				{AccountNo: "MTG005", OpenDate: now.AddDate(-4, -8, 0).Format("2006-01-02"), InitialLimit: 450000000, ProductType: "mortgage", OutstandingBal: 280000000, LoanStatus: "active", CollectabilityCode: 1, Delinquency: 0, Last24DelqHist: make([]int, 24), Last24CollHist: repeatInt(1, 24)},
				{AccountNo: "CC012", OpenDate: now.AddDate(-3, -2, 0).Format("2006-01-02"), InitialLimit: 65000000, ProductType: "credit card", OutstandingBal: 22000000, LoanStatus: "active", CollectabilityCode: 1, Delinquency: 0, Last24DelqHist: make([]int, 24), Last24CollHist: repeatInt(1, 24)},
				{AccountNo: "PL009", OpenDate: now.AddDate(-0, -9, 0).Format("2006-01-02"), InitialLimit: 45000000, ProductType: "personal loan", OutstandingBal: 38000000, LoanStatus: "active", CollectabilityCode: 1, Delinquency: 0, Last24DelqHist: make([]int, 24), Last24CollHist: repeatInt(1, 24)},
			},
		},
	}
	
	return data
}

func repeatInt(val int, count int) []int {
	result := make([]int, count)
	for i := 0; i < count; i++ {
		result[i] = val
	}
	return result
}

func concatInts(slices ...[]int) []int {
	var result []int
	for _, s := range slices {
		result = append(result, s...)
	}
	return result
}

// ============================================================================
// SCHEMA DETECTION & FLATTENING
// ============================================================================

// FieldInfo stores information about a detected field
type FieldInfo struct {
	Name string      `json:"name"`
	Type string      `json:"type"`
	Path string      `json:"path"` // full path like "credits.outstanding_balance"
	IsArray bool     `json:"is_array"`
}

// detectSchema analyzes sample data and returns field information
func detectSchema(data interface{}) []FieldInfo {
	fields := make(map[string]FieldInfo)
	detectFieldsRecursive("", data, fields)
	
	result := make([]FieldInfo, 0, len(fields))
	for _, f := range fields {
		result = append(result, f)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

func detectFieldsRecursive(prefix string, data interface{}, fields map[string]FieldInfo) {
	if data == nil {
		return
	}
	
	val := reflect.ValueOf(data)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	
	switch val.Kind() {
	case reflect.Map:
		for _, key := range val.MapKeys() {
			keyStr := fmt.Sprintf("%v", key.Interface())
			newPath := prefix
			if prefix != "" {
				newPath = prefix + "." + keyStr
			} else {
				newPath = keyStr
			}
			detectFieldsRecursive(newPath, val.MapIndex(key).Interface(), fields)
		}
	case reflect.Struct:
		typ := val.Type()
		for i := 0; i < val.NumField(); i++ {
			field := typ.Field(i)
			jsonTag := field.Tag.Get("json")
			if jsonTag == "-" {
				continue
			}
			fieldName := strings.Split(jsonTag, ",")[0]
			if fieldName == "" {
				fieldName = field.Name
			}
			newPath := prefix
			if prefix != "" {
				newPath = prefix + "." + fieldName
			} else {
				newPath = fieldName
			}
			detectFieldsRecursive(newPath, val.Field(i).Interface(), fields)
		}
	case reflect.Slice:
		if val.Len() > 0 {
			elem := val.Index(0).Interface()
			elemVal := reflect.ValueOf(elem)
			if elemVal.Kind() == reflect.Struct || elemVal.Kind() == reflect.Map {
				// This is an array of objects - we need to expose nested fields
				if prefix == "" {
					// Top-level array case
					detectFieldsRecursive("", elem, fields)
				} else {
					// Nested array - extract fields with array prefix
					detectFieldsRecursiveFromArray(prefix, elem, fields)
				}
			}
		}
	case reflect.String:
		fields[prefix] = FieldInfo{Name: prefix, Type: "string", Path: prefix, IsArray: false}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		fields[prefix] = FieldInfo{Name: prefix, Type: "integer", Path: prefix, IsArray: false}
	case reflect.Float32, reflect.Float64:
		fields[prefix] = FieldInfo{Name: prefix, Type: "number", Path: prefix, IsArray: false}
	case reflect.Bool:
		fields[prefix] = FieldInfo{Name: prefix, Type: "boolean", Path: prefix, IsArray: false}
	}
}

// detectFieldsRecursiveFromArray detects fields from array elements with dotted notation
func detectFieldsRecursiveFromArray(arrayPrefix string, data interface{}, fields map[string]FieldInfo) {
	val := reflect.ValueOf(data)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	
	switch val.Kind() {
	case reflect.Map:
		for _, key := range val.MapKeys() {
			keyStr := fmt.Sprintf("%v", key.Interface())
			newPath := arrayPrefix + "." + keyStr
			detectFieldsRecursiveFromArray(newPath, val.MapIndex(key).Interface(), fields)
		}
	case reflect.Struct:
		typ := val.Type()
		for i := 0; i < val.NumField(); i++ {
			field := typ.Field(i)
			jsonTag := field.Tag.Get("json")
			if jsonTag == "-" {
				continue
			}
			fieldName := strings.Split(jsonTag, ",")[0]
			if fieldName == "" {
				fieldName = field.Name
			}
			newPath := arrayPrefix + "." + fieldName
			fieldVal := val.Field(i).Interface()
			
			// Check if it's a nested struct or array
			fieldValRef := reflect.ValueOf(fieldVal)
			if fieldValRef.Kind() == reflect.Slice && fieldValRef.Len() > 0 {
				// Array field within array element (e.g., last_24_delq_hist)
				fields[newPath] = FieldInfo{Name: newPath, Type: "array", Path: newPath, IsArray: true}
			} else if fieldValRef.Kind() == reflect.Struct || fieldValRef.Kind() == reflect.Map {
				detectFieldsRecursiveFromArray(newPath, fieldVal, fields)
			} else {
			FieldType:
				switch fieldVal.(type) {
				case string:
					fields[newPath] = FieldInfo{Name: newPath, Type: "string", Path: newPath, IsArray: false}
				case int, int8, int16, int32, int64:
					fields[newPath] = FieldInfo{Name: newPath, Type: "integer", Path: newPath, IsArray: false}
				case float32, float64:
					fields[newPath] = FieldInfo{Name: newPath, Type: "number", Path: newPath, IsArray: false}
				case bool:
					fields[newPath] = FieldInfo{Name: newPath, Type: "boolean", Path: newPath, IsArray: false}
				default:
					// Try to determine type dynamically
					if fieldValRef.Kind() == reflect.Int || fieldValRef.Kind() == reflect.Int64 {
						fields[newPath] = FieldInfo{Name: newPath, Type: "integer", Path: newPath, IsArray: false}
					} else if fieldValRef.Kind() == reflect.Float64 {
						fields[newPath] = FieldInfo{Name: newPath, Type: "number", Path: newPath, IsArray: false}
					} else {
						break FieldType
					}
				}
			}
		}
	}
}

// flattenData flattens records with nested arrays into multiple rows
// Each element in a nested array becomes a separate row combined with parent data
func flattenData(data []interface{}) []map[string]interface{} {
	var result []map[string]interface{}
	
	for _, record := range data {
		flattened := flattenRecord(record)
		result = append(result, flattened...)
	}
	
	return result
}

func flattenRecord(record interface{}) []map[string]interface{} {
	val := reflect.ValueOf(record)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	
	// Start with parent fields
	parentMap := make(map[string]interface{})
	extractParentFields(val, "", parentMap)
	
	// Find nested arrays and flatten
	return flattenWithArrays(parentMap, val)
}

func extractParentFields(val reflect.Value, prefix string, result map[string]interface{}) {
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	
	switch val.Kind() {
	case reflect.Struct:
		typ := val.Type()
		for i := 0; i < val.NumField(); i++ {
			field := typ.Field(i)
			jsonTag := field.Tag.Get("json")
			if jsonTag == "-" {
				continue
			}
			fieldName := strings.Split(jsonTag, ",")[0]
			if fieldName == "" {
				fieldName = field.Name
			}
			newPath := prefix
			if prefix != "" {
				newPath = prefix + "." + fieldName
			} else {
				newPath = fieldName
			}
			fieldVal := val.Field(i)
			
			// Don't add arrays to parent map - they'll be handled separately
			if fieldVal.Kind() != reflect.Slice {
				result[newPath] = fieldVal.Interface()
			}
		}
	case reflect.Map:
		for _, key := range val.MapKeys() {
			keyStr := fmt.Sprintf("%v", key.Interface())
			newPath := prefix
			if prefix != "" {
				newPath = prefix + "." + keyStr
			} else {
				newPath = keyStr
			}
			mapVal := val.MapIndex(key)
			if mapVal.Kind() != reflect.Slice {
				result[newPath] = mapVal.Interface()
			}
		}
	}
}

func flattenWithArrays(parentMap map[string]interface{}, recordVal reflect.Value) []map[string]interface{} {
	if recordVal.Kind() == reflect.Ptr {
		recordVal = recordVal.Elem()
	}
	
	// Find all nested arrays
	var arraysFound []struct {
		name  string
		value reflect.Value
	}
	
	if recordVal.Kind() == reflect.Struct {
		typ := recordVal.Type()
		for i := 0; i < recordVal.NumField(); i++ {
			field := typ.Field(i)
			jsonTag := field.Tag.Get("json")
			if jsonTag == "-" {
				continue
			}
			fieldName := strings.Split(jsonTag, ",")[0]
			if fieldName == "" {
				fieldName = field.Name
			}
			fieldVal := recordVal.Field(i)
			if fieldVal.Kind() == reflect.Slice && fieldVal.Len() > 0 {
				arraysFound = append(arraysFound, struct {
					name  string
					value reflect.Value
				}{name: fieldName, value: fieldVal})
			}
		}
	}
	
	// If no arrays, return single record
	if len(arraysFound) == 0 {
		return []map[string]interface{}{parentMap}
	}
	
	// Flatten by the first array found (for simplicity, we handle one level at a time)
	// In practice, we'll iterate through the primary nested array (credits)
	firstArray := arraysFound[0]
	
	var result []map[string]interface{}
	
	for i := 0; i < firstArray.value.Len(); i++ {
		row := make(map[string]interface{})
		
		// Copy parent fields
		for k, v := range parentMap {
			row[k] = v
		}
		
		// Add array element fields with prefix
		arrayElem := firstArray.value.Index(i)
		addNestedFields(row, "", arrayElem.Interface(), firstArray.name)
		
		result = append(result, row)
	}
	
	return result
}

func addNestedFields(result map[string]interface{}, prefix string, data interface{}, arrayName string) {
	val := reflect.ValueOf(data)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	
	switch val.Kind() {
	case reflect.Struct:
		typ := val.Type()
		for i := 0; i < val.NumField(); i++ {
			field := typ.Field(i)
			jsonTag := field.Tag.Get("json")
			if jsonTag == "-" {
				continue
			}
			fieldName := strings.Split(jsonTag, ",")[0]
			if fieldName == "" {
				fieldName = field.Name
			}
			fullPath := arrayName + "." + fieldName
			fieldVal := val.Field(i).Interface()
			
			// For array fields within nested objects (like last_24_delq_hist), store as JSON or special format
			fieldValRef := reflect.ValueOf(fieldVal)
			if fieldValRef.Kind() == reflect.Slice {
				result[fullPath] = fieldVal
			} else {
				result[fullPath] = fieldVal
			}
		}
	case reflect.Map:
		for _, key := range val.MapKeys() {
			keyStr := fmt.Sprintf("%v", key.Interface())
			fullPath := arrayName + "." + keyStr
			result[fullPath] = val.MapIndex(key).Interface()
		}
	}
}

// ============================================================================
// AGGREGATION ENGINE
// ============================================================================

// AggregateRequest represents the aggregation query
type AggregateRequest struct {
	Dataset      string                 `json:"dataset"`
	Data         []interface{}          `json:"data,omitempty"`
	Where        map[string]interface{} `json:"where,omitempty"`
	GroupBy      []string               `json:"groupBy,omitempty"`
	Aggregations []Aggregation          `json:"aggregations,omitempty"`
}

// Aggregation represents a single aggregation operation
type Aggregation struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Alias string `json:"alias"`
}

// AggregateResponse is the result of aggregation
type AggregateResponse []map[string]interface{}

// Aggregate performs the aggregation operation
func Aggregate(req AggregateRequest, allData map[string][]interface{}) (AggregateResponse, error) {
	var data []interface{}
	
	// Get data from dataset or inline
	if req.Data != nil && len(req.Data) > 0 {
		data = req.Data
	} else if req.Dataset != "" {
		if ds, ok := allData[req.Dataset]; ok {
			data = ds
		} else {
			return nil, fmt.Errorf("dataset '%s' not found", req.Dataset)
		}
	}
	
	if len(data) == 0 {
		return AggregateResponse{}, nil
	}
	
	// Step 1: Detect if we need to flatten (if query touches nested array fields)
	needsFlatten := false
	touchedArrayFields := make(map[string]bool)
	
	for _, agg := range req.Aggregations {
		if strings.Contains(agg.Field, ".") {
			parts := strings.SplitN(agg.Field, ".", 2)
			if isArrayField(data, parts[0]) {
				needsFlatten = true
				touchedArrayFields[parts[0]] = true
			}
		}
	}
	
	for field := range req.Where {
		if strings.Contains(field, ".") {
			parts := strings.SplitN(field, ".", 2)
			if isArrayField(data, parts[0]) {
				needsFlatten = true
				touchedArrayFields[parts[0]] = true
			}
		}
	}
	
	for _, gb := range req.GroupBy {
		if strings.Contains(gb, ".") {
			parts := strings.SplitN(gb, ".", 2)
			if isArrayField(data, parts[0]) {
				needsFlatten = true
				touchedArrayFields[parts[0]] = true
			}
		}
	}
	
	// Step 2: Flatten if needed
	var workingData []map[string]interface{}
	if needsFlatten {
		workingData = flattenData(data)
	} else {
		// Convert to map format
		for _, rec := range data {
			flat := make(map[string]interface{})
			convertToMap(rec, flat, "")
			workingData = append(workingData, flat)
		}
	}
	
	// Step 3: Apply WHERE filter
	if req.Where != nil && len(req.Where) > 0 {
		filtered := make([]map[string]interface{}, 0)
		for _, row := range workingData {
			if matchesWhere(row, req.Where) {
				filtered = append(filtered, row)
			}
		}
		workingData = filtered
	}
	
	// Step 4: Group and aggregate
	if len(req.GroupBy) > 0 {
		return groupAndAggregate(workingData, req.GroupBy, req.Aggregations)
	} else {
		return aggregateAll(workingData, req.Aggregations)
	}
}

func isArrayField(data []interface{}, arrayName string) bool {
	if len(data) == 0 {
		return false
	}
	
	val := reflect.ValueOf(data[0])
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	
	if val.Kind() == reflect.Struct {
		for i := 0; i < val.NumField(); i++ {
			field := val.Type().Field(i)
			jsonTag := field.Tag.Get("json")
			fieldName := strings.Split(jsonTag, ",")[0]
			if fieldName == arrayName {
				return val.Field(i).Kind() == reflect.Slice
			}
		}
	}
	
	return false
}

func convertToMap(data interface{}, result map[string]interface{}, prefix string) {
	val := reflect.ValueOf(data)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	
	switch val.Kind() {
	case reflect.Struct:
		typ := val.Type()
		for i := 0; i < val.NumField(); i++ {
			field := typ.Field(i)
			jsonTag := field.Tag.Get("json")
			if jsonTag == "-" {
				continue
			}
			fieldName := strings.Split(jsonTag, ",")[0]
			if fieldName == "" {
				fieldName = field.Name
			}
			newPath := prefix
			if prefix != "" {
				newPath = prefix + "." + fieldName
			} else {
				newPath = fieldName
			}
			fieldVal := val.Field(i)
			if fieldVal.Kind() == reflect.Slice {
				// Skip arrays in non-flatten mode
				continue
			}
			result[newPath] = fieldVal.Interface()
		}
	}
}

func matchesWhere(row map[string]interface{}, where map[string]interface{}) bool {
	for field, condition := range where {
		val, exists := row[field]
		if !exists {
			return false
		}
		
		switch cond := condition.(type) {
		case map[string]interface{}:
			if !evaluateCondition(val, cond) {
				return false
			}
		default:
			// Simple equality
			if val != cond {
				return false
			}
		}
	}
	return true
}

func evaluateCondition(val interface{}, cond map[string]interface{}) bool {
	for op, expected := range cond {
		switch op {
		case "$eq":
			if !compareEqual(val, expected) {
				return false
			}
		case "$ne":
			if compareEqual(val, expected) {
				return false
			}
		case "$gt":
			if !compareGreater(val, expected) {
				return false
			}
		case "$gte":
			if !compareGreaterOrEqual(val, expected) {
				return false
			}
		case "$lt":
			if !compareLess(val, expected) {
				return false
			}
		case "$lte":
			if !compareLessOrEqual(val, expected) {
				return false
			}
		case "$in":
			if !compareIn(val, expected) {
				return false
			}
		}
	}
	return true
}

func compareEqual(a, b interface{}) bool {
	return a == b
}

func compareGreater(a, b interface{}) bool {
	return toFloat64(a) > toFloat64(b)
}

func compareGreaterOrEqual(a, b interface{}) bool {
	return toFloat64(a) >= toFloat64(b)
}

func compareLess(a, b interface{}) bool {
	return toFloat64(a) < toFloat64(b)
}

func compareLessOrEqual(a, b interface{}) bool {
	return toFloat64(a) <= toFloat64(b)
}

func compareIn(val interface{}, expected interface{}) bool {
	if arr, ok := expected.([]interface{}); ok {
		for _, item := range arr {
			if val == item {
				return true
			}
		}
	}
	return false
}

func toFloat64(val interface{}) float64 {
	switch v := val.(type) {
	case int:
		return float64(v)
	case int8:
		return float64(v)
	case int16:
		return float64(v)
	case int32:
		return float64(v)
	case int64:
		return float64(v)
	case float32:
		return float64(v)
	case float64:
		return v
	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0
		}
		return f
	default:
		return 0
	}
}

func groupAndAggregate(data []map[string]interface{}, groupBy []string, aggregations []Aggregation) (AggregateResponse, error) {
	// Group data
	groups := make(map[string][]map[string]interface{})
	
	for _, row := range data {
		keyParts := make([]string, len(groupBy))
		for i, field := range groupBy {
			val, exists := row[field]
			if !exists {
				val = "null"
			}
			keyParts[i] = fmt.Sprintf("%v", val)
		}
		key := strings.Join(keyParts, "|")
		groups[key] = append(groups[key], row)
	}
	
	// Aggregate each group
	var result AggregateResponse
	
	for key, group := range groups {
		row := make(map[string]interface{})
		
		// Set group by fields
		keyParts := strings.Split(key, "|")
		for i, field := range groupBy {
			if i < len(keyParts) {
				// Try to preserve original type
				row[field] = parseValue(keyParts[i])
			}
		}
		
		// Calculate aggregations
		for _, agg := range aggregations {
			value := calculateAggregation(group, agg.Field, agg.Op)
			row[agg.Alias] = value
		}
		
		result = append(result, row)
	}
	
	// Sort for consistent output
	sort.Slice(result, func(i, j int) bool {
		iKey := ""
		jKey := ""
		for _, field := range groupBy {
			iKey += fmt.Sprintf("%v,", result[i][field])
			jKey += fmt.Sprintf("%v,", result[j][field])
		}
		return iKey < jKey
	})
	
	return result, nil
}

func aggregateAll(data []map[string]interface{}, aggregations []Aggregation) (AggregateResponse, error) {
	row := make(map[string]interface{})
	
	for _, agg := range aggregations {
		value := calculateAggregation(data, agg.Field, agg.Op)
		row[agg.Alias] = value
	}
	
	return AggregateResponse{row}, nil
}

func calculateAggregation(data []map[string]interface{}, field string, op string) interface{} {
	switch op {
	case "count":
		if field == "" || field == "*" {
			return len(data)
		}
		// Count non-null values
		count := 0
		for _, row := range data {
			if val, exists := row[field]; exists && val != nil {
				count++
			}
		}
		return count
		
	case "sum":
		sum := 0.0
		for _, row := range data {
			if val, exists := row[field]; exists && val != nil {
				sum += toFloat64(val)
			}
		}
		return sum
		
	case "avg":
		sum := 0.0
		count := 0
		for _, row := range data {
			if val, exists := row[field]; exists && val != nil {
				sum += toFloat64(val)
				count++
			}
		}
		if count == 0 {
			return 0
		}
		return sum / float64(count)
		
	case "min":
		if len(data) == 0 {
			return nil
		}
		min := math.MaxFloat64
		found := false
		for _, row := range data {
			if val, exists := row[field]; exists && val != nil {
				v := toFloat64(val)
				if !found || v < min {
					min = v
					found = true
				}
			}
		}
		if !found {
			return nil
		}
		return min
		
	case "max":
		if len(data) == 0 {
			return nil
		}
		max := -math.MaxFloat64
		found := false
		for _, row := range data {
			if val, exists := row[field]; exists && val != nil {
				v := toFloat64(val)
				if !found || v > max {
					max = v
					found = true
				}
			}
		}
		if !found {
			return nil
		}
		return max
		
	default:
		return nil
	}
}

func parseValue(s string) interface{} {
	// Try integer
	if i, err := strconv.Atoi(s); err == nil {
		return i
	}
	// Try float
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	// Return as string
	return s
}

// ============================================================================
// SPECIAL AGGREGATIONS FOR TIME-SERIES ARRAY FIELDS
// ============================================================================

// SpecialAggregationRequest extends the basic request for time-series operations
type SpecialAggregationRequest struct {
	AggregateRequest
	Months     int    `json:"months,omitempty"` // For "last N months" operations
	ArrayField string `json:"array_field,omitempty"` // e.g., "credits.last_24_delq_hist"
}

// calculateSpecialAggregation handles special queries like "worst delinquency in last 6 months"
func calculateSpecialAggregation(data []map[string]interface{}, req SpecialAggregationRequest) interface{} {
	// Extract the last N months from array fields
	n := req.Months
	if n <= 0 {
		n = 24
	}
	
	arrayField := req.ArrayField
	if arrayField == "" {
		return nil
	}
	
	var allValues []int
	
	for _, row := range data {
		if arrVal, exists := row[arrayField]; exists {
			if arr, ok := arrVal.([]interface{}); ok {
				// Get last N elements
				startIdx := len(arr) - n
				if startIdx < 0 {
					startIdx = 0
				}
				for i := startIdx; i < len(arr); i++ {
					if intVal, ok := arr[i].(float64); ok {
						allValues = append(allValues, int(intVal))
					} else if intVal, ok := arr[i].(int); ok {
						allValues = append(allValues, intVal)
					}
				}
			}
		}
	}
	
	if len(allValues) == 0 {
		return nil
	}
	
	// Determine operation based on the field name
	if strings.Contains(arrayField, "delq") {
		// For delinquency, return max
		maxVal := allValues[0]
		for _, v := range allValues {
			if v > maxVal {
				maxVal = v
			}
		}
		return maxVal
	} else if strings.Contains(arrayField, "coll") {
		// For collectability, could be max or check existence
		if req.Aggregations != nil && len(req.Aggregations) > 0 {
			op := req.Aggregations[0].Op
			if op == "ever_has" {
				// Check if any value meets certain criteria
				return len(allValues) > 0
			}
		}
		// Default: return max collectability code
		maxVal := allValues[0]
		for _, v := range allValues {
			if v > maxVal {
				maxVal = v
			}
		}
		return maxVal
	}
	
	return nil
}

// ============================================================================
// HTTP HANDLERS
// ============================================================================

var mockData []Employee
var dataSet map[string][]interface{}

func init() {
	mockData = generateMockData()
	dataSet = make(map[string][]interface{})
	
	// Convert mock data to interface slice
	for _, emp := range mockData {
		dataSet["employees"] = append(dataSet["employees"], emp)
	}
}

func schemaHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	
	dataset := r.URL.Query().Get("dataset")
	if dataset == "" {
		dataset = "employees"
	}
	
	data, exists := dataSet[dataset]
	if !exists || len(data) == 0 {
		http.Error(w, "Dataset not found", http.StatusNotFound)
		return
	}
	
	schema := detectSchema(data[0])
	json.NewEncoder(w).Encode(schema)
}

func aggregateHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
	
	var req AggregateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	
	result, err := Aggregate(req, dataSet)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	
	json.NewEncoder(w).Encode(result)
}

func datasetsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	
	datasets := make([]map[string]interface{}, 0)
	for name := range dataSet {
		datasets = append(datasets, map[string]interface{}{
			"name":  name,
			"count": len(dataSet[name]),
		})
	}
	
	json.NewEncoder(w).Encode(datasets)
}

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()
	
	data, err := ioutil.ReadAll(file)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	
	var jsonData interface{}
	if err := json.Unmarshal(data, &jsonData); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	
	// Store temporarily (in production, use proper storage)
	datasetName := fmt.Sprintf("upload_%d", time.Now().UnixNano())
	
	var dataArray []interface{}
	if arr, ok := jsonData.([]interface{}); ok {
		dataArray = arr
	} else {
		dataArray = []interface{}{jsonData}
	}
	
	dataSet[datasetName] = dataArray
	
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"dataset": datasetName,
		"count":   fmt.Sprintf("%d", len(dataArray)),
	})
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "static/index.html")
}

// ============================================================================
// MAIN
// ============================================================================

func main() {
	mux := http.NewServeMux()
	
	// API endpoints
	mux.HandleFunc("/api/schema", schemaHandler)
	mux.HandleFunc("/api/aggregate", aggregateHandler)
	mux.HandleFunc("/api/datasets", datasetsHandler)
	mux.HandleFunc("/api/upload", uploadHandler)
	
	// Static files
	mux.Handle("/", http.FileServer(http.Dir("static")))
	mux.HandleFunc("/index.html", indexHandler)
	
	fmt.Println("Visual Aggregator Server")
	fmt.Println("========================")
	fmt.Println("Starting server on http://localhost:8080")
	fmt.Println("")
	fmt.Println("Available endpoints:")
	fmt.Println("  GET  /                    - Frontend UI")
	fmt.Println("  GET  /api/datasets        - List available datasets")
	fmt.Println("  GET  /api/schema?dataset= - Get schema for a dataset")
	fmt.Println("  POST /api/aggregate       - Perform aggregation")
	fmt.Println("  POST /api/upload          - Upload custom JSON data")
	fmt.Println("")
	fmt.Println("Press Ctrl+C to stop")
	
	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}
	
	if err := server.ListenAndServe(); err != nil {
		fmt.Printf("Server error: %v\n", err)
	}
}
