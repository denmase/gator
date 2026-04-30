package aggregator

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

// AggregateRequest represents the aggregation request DSL
type AggregateRequest struct {
	Dataset      string                 `json:"dataset,omitempty"`
	Data         []map[string]interface{} `json:"data,omitempty"`
	Where        map[string]interface{}   `json:"where,omitempty"`
	GroupBy      []string               `json:"groupBy,omitempty"`
	Aggregations []Aggregation          `json:"aggregations,omitempty"`
}

// Aggregation represents a single aggregation operation
type Aggregation struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Alias string `json:"alias"`
}

// AggregateResponse is the result of an aggregation query
type AggregateResponse struct {
	Data       []map[string]interface{} `json:"data"`
	Schema     []string                 `json:"schema,omitempty"`
	Flattened  []map[string]interface{} `json:"flattened,omitempty"` // For debugging
}

// SchemaField represents a field in the detected schema
type SchemaField struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nested   bool   `json:"nested,omitempty"`
	Array    bool   `json:"array,omitempty"`
}

// Flattener handles flattening of nested JSON structures
type Flattener struct {
	arrayFields []string // Fields that come from array elements
}

// NewFlattener creates a new Flattener instance
func NewFlattener() *Flattener {
	return &Flattener{
		arrayFields: make([]string, 0),
	}
}

// Flatten converts nested objects and arrays into flat key-value pairs using dot notation
// For arrays, it creates one row per array element (cross join behavior)
func (f *Flattener) Flatten(data []map[string]interface{}) []map[string]interface{} {
	if len(data) == 0 {
		return data
	}

	result := make([]map[string]interface{}, 0)

	for _, item := range data {
		flattenedItems := f.flattenObject(item, "")
		result = append(result, flattenedItems...)
	}

	return result
}

// flattenObject recursively flattens a single object
func (f *Flattener) flattenObject(obj map[string]interface{}, prefix string) []map[string]interface{} {
	result := make([]map[string]interface{}, 1)
	result[0] = make(map[string]interface{})

	for key, value := range obj {
		fullKey := key
		if prefix != "" {
			fullKey = prefix + "." + key
		}

		switch v := value.(type) {
		case map[string]interface{}:
			// Nested object - recurse
			nestedResults := f.flattenObject(v, fullKey)
			// Cross join with current results
			newResult := make([]map[string]interface{}, 0)
			for _, base := range result {
				for _, nested := range nestedResults {
					merged := make(map[string]interface{})
					for k, val := range base {
						merged[k] = val
					}
					for k, val := range nested {
						merged[k] = val
					}
					newResult = append(newResult, merged)
				}
			}
			result = newResult

		case []interface{}:
			// Array - create one row per element
			if len(v) == 0 {
				// Empty array - set to empty or null
				for i := range result {
					result[i][fullKey] = make([]interface{}, 0)
				}
			} else {
				// Non-empty array - expand
				newResult := make([]map[string]interface{}, 0)
				for _, base := range result {
					for idx, elem := range v {
						row := make(map[string]interface{})
						for k, val := range base {
							row[k] = val
						}
						
						switch elemVal := elem.(type) {
						case map[string]interface{}:
							// Object in array - flatten with index suffix for array fields
							elemFlat := f.flattenObject(elemVal, fullKey)
							for _, ef := range elemFlat {
								rowCopy := make(map[string]interface{})
								for k, val := range row {
									rowCopy[k] = val
								}
								for k, val := range ef {
									rowCopy[k] = val
									// Track this as an array-derived field
									if !contains(f.arrayFields, k) {
										f.arrayFields = append(f.arrayFields, k)
									}
								}
								newResult = append(newResult, rowCopy)
							}
						default:
							// Primitive in array - use index notation
							idxKey := fmt.Sprintf("%s[%d]", fullKey, idx)
							row[idxKey] = elemVal
							if !contains(f.arrayFields, idxKey) {
								f.arrayFields = append(f.arrayFields, idxKey)
							}
							newResult = append(newResult, row)
						}
					}
				}
				result = newResult
			}

		default:
			// Primitive value
			for i := range result {
				result[i][fullKey] = v
			}
		}
	}

	return result
}

// GetArrayFields returns list of fields derived from arrays
func (f *Flattener) GetArrayFields() []string {
	return f.arrayFields
}

// DetectSchema analyzes sample data and returns available fields
func DetectSchema(data []map[string]interface{}) []SchemaField {
	if len(data) == 0 {
		return []SchemaField{}
	}

	fields := make(map[string]SchemaField)
	
	// Analyze first few records (up to 10) for schema detection
	sampleSize := 10
	if len(data) < sampleSize {
		sampleSize = len(data)
	}

	for i := 0; i < sampleSize; i++ {
		analyzeObject(data[i], "", fields)
	}

	// Convert map to slice and sort
	result := make([]SchemaField, 0, len(fields))
	for _, field := range fields {
		result = append(result, field)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result
}

// analyzeObject recursively analyzes an object to detect fields
func analyzeObject(obj map[string]interface{}, prefix string, fields map[string]SchemaField) {
	for key, value := range obj {
		fullKey := key
		if prefix != "" {
			fullKey = prefix + "." + key
		}

		switch v := value.(type) {
		case map[string]interface{}:
			// Nested object
			if existing, ok := fields[fullKey]; ok {
				existing.Nested = true
				fields[fullKey] = existing
			} else {
				fields[fullKey] = SchemaField{
					Name:   fullKey,
					Type:   "object",
					Nested: true,
				}
			}
			analyzeObject(v, fullKey, fields)

		case []interface{}:
			// Array
			arrayType := "array"
			isNested := false
			if len(v) > 0 {
				if _, ok := v[0].(map[string]interface{}); ok {
					arrayType = "object_array"
					isNested = true
					// Analyze first element for nested fields
					if elem, ok := v[0].(map[string]interface{}); ok {
						analyzeObject(elem, fullKey, fields)
					}
				} else {
					arrayType = getPrimitiveType(v[0])
				}
			}
			
			if existing, ok := fields[fullKey]; ok {
				existing.Array = true
				existing.Type = arrayType
				fields[fullKey] = existing
			} else {
				fields[fullKey] = SchemaField{
					Name:  fullKey,
					Type:  arrayType,
					Array: true,
					Nested: isNested,
				}
			}

		default:
			// Primitive
			valueType := getPrimitiveType(v)
			if existing, ok := fields[fullKey]; ok {
				// Keep the more specific type
				if existing.Type == "interface" || existing.Type == "nil" {
					existing.Type = valueType
					fields[fullKey] = existing
				}
			} else {
				fields[fullKey] = SchemaField{
					Name: fullKey,
					Type: valueType,
				}
			}
		}
	}
}

// getPrimitiveType returns the type name for a primitive value
func getPrimitiveType(value interface{}) string {
	if value == nil {
		return "nil"
	}
	
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
	     reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "number"
	case reflect.Float32, reflect.Float64:
		return "number"
	case reflect.String:
		return "string"
	default:
		return "interface"
	}
}

// Aggregate executes the aggregation query
func Aggregate(req AggregateRequest) (*AggregateResponse, error) {
	var data []map[string]interface{}

	// Use provided data or load from dataset
	if len(req.Data) > 0 {
		data = req.Data
	} else if req.Dataset != "" {
		loadedData := GetDataset(req.Dataset)
		if loadedData == nil {
			return nil, fmt.Errorf("dataset '%s' not found", req.Dataset)
		}
		data = loadedData
	} else {
		return nil, fmt.Errorf("no data provided")
	}

	if len(data) == 0 {
		return &AggregateResponse{Data: []map[string]interface{}{}}, nil
	}

	// Create flatterner and flatten data
	flattenner := NewFlattener()
	flattened := flattenner.Flatten(data)

	// Apply WHERE filter
	if req.Where != nil && len(req.Where) > 0 {
		filtered := make([]map[string]interface{}, 0)
		for _, row := range flattened {
			if matchesWhere(row, req.Where) {
				filtered = append(filtered, row)
			}
		}
		flattened = filtered
	}

	// Perform grouping and aggregation
	var result []map[string]interface{}
	
	if len(req.GroupBy) > 0 {
		result = groupAndAggregate(flattened, req.GroupBy, req.Aggregations)
	} else {
		// No grouping - aggregate all rows into single result
		if len(req.Aggregations) > 0 {
			aggRow := make(map[string]interface{})
			for _, agg := range req.Aggregations {
				value := computeAggregate(flattened, agg.Field, agg.Op)
				key := agg.Alias
				if key == "" {
					key = fmt.Sprintf("%s_%s", agg.Op, agg.Field)
				}
				aggRow[key] = value
			}
			result = []map[string]interface{}{aggRow}
		} else {
			// No aggregations either - return filtered data
			result = flattened
		}
	}

	// Detect schema for response
	schema := DetectSchema(data)
	schemaFields := make([]string, len(schema))
	for i, s := range schema {
		schemaFields[i] = s.Name
	}

	response := &AggregateResponse{
		Data:      result,
		Schema:    schemaFields,
		Flattened: flattened, // Include flattened data for debugging/preview
	}

	return response, nil
}

// matchesWhere checks if a row matches the WHERE conditions
func matchesWhere(row map[string]interface{}, where map[string]interface{}) bool {
	for field, condition := range where {
		fieldValue := getNestedValue(row, field)
		
		switch cond := condition.(type) {
		case map[string]interface{}:
			// Operator-based condition (e.g., {"$gt": 100})
			if !evaluateCondition(fieldValue, cond) {
				return false
			}
		default:
			// Simple equality
			if fieldValue != cond {
				return false
			}
		}
	}
	return true
}

// evaluateCondition evaluates operator-based conditions
func evaluateCondition(fieldValue interface{}, condition map[string]interface{}) bool {
	for op, expected := range condition {
		opName := strings.TrimPrefix(op, "$")
		
		switch opName {
		case "eq":
			if !compareEqual(fieldValue, expected) {
				return false
			}
		case "ne":
			if compareEqual(fieldValue, expected) {
				return false
			}
		case "gt":
			if !compareNumeric(fieldValue, expected, func(a, b float64) bool { return a > b }) {
				return false
			}
		case "gte":
			if !compareNumeric(fieldValue, expected, func(a, b float64) bool { return a >= b }) {
				return false
			}
		case "lt":
			if !compareNumeric(fieldValue, expected, func(a, b float64) bool { return a < b }) {
				return false
			}
		case "lte":
			if !compareNumeric(fieldValue, expected, func(a, b float64) bool { return a <= b }) {
				return false
			}
		case "in":
			if !isIn(fieldValue, expected) {
				return false
			}
		case "nin":
			if isIn(fieldValue, expected) {
				return false
			}
		case "contains":
			if !containsString(fieldValue, expected) {
				return false
			}
		case "startswith":
			if !startsWithString(fieldValue, expected) {
				return false
			}
		case "endswith":
			if !endsWithString(fieldValue, expected) {
				return false
			}
		}
	}
	return true
}

// getNestedValue retrieves a value using dot notation
func getNestedValue(obj map[string]interface{}, path string) interface{} {
	parts := strings.Split(path, ".")
	current := obj

	for i, part := range parts {
		// Handle array indexing like credits[0].balance
		if strings.Contains(part, "[") {
			bracketIdx := strings.Index(part, "[")
			key := part[:bracketIdx]
			indexStr := part[bracketIdx+1:]
			indexStr = strings.TrimSuffix(indexStr, "]")
			
			val, ok := current[key]
			if !ok {
				return nil
			}
			
			if arr, ok := val.([]interface{}); ok {
				idx, err := strconv.Atoi(indexStr)
				if err != nil || idx < 0 || idx >= len(arr) {
					return nil
				}
				if i == len(parts)-1 {
					return arr[idx]
				}
				if nextObj, ok := arr[idx].(map[string]interface{}); ok {
					current = nextObj
				} else {
					return nil
				}
			} else {
				return nil
			}
		} else {
			val, ok := current[part]
			if !ok {
				return nil
			}
			
			if i == len(parts)-1 {
				return val
			}
			
			if nextObj, ok := val.(map[string]interface{}); ok {
				current = nextObj
			} else {
				return nil
			}
		}
	}
	
	return current
}

// compareEqual compares two values for equality
func compareEqual(a, b interface{}) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	
	// Try numeric comparison
	aNum, aOk := toFloat64(a)
	bNum, bOk := toFloat64(b)
	if aOk && bOk {
		return aNum == bNum
	}
	
	// String comparison
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

// compareNumeric compares two numeric values with a custom comparator
func compareNumeric(a, b interface{}, cmp func(float64, float64) bool) bool {
	aNum, aOk := toFloat64(a)
	bNum, bOk := toFloat64(b)
	if !aOk || !bOk {
		return false
	}
	return cmp(aNum, bNum)
}

// isIn checks if value is in a list
func isIn(value interface{}, list interface{}) bool {
	listArr, ok := list.([]interface{})
	if !ok {
		return false
	}
	
	for _, item := range listArr {
		if compareEqual(value, item) {
			return true
		}
	}
	return false
}

// containsString checks if string contains substring
func containsString(value, substr interface{}) bool {
	str, ok := toString(value)
	if !ok {
		return false
	}
	substrStr, ok := toString(substr)
	if !ok {
		return false
	}
	return strings.Contains(str, substrStr)
}

// startsWithString checks if string starts with prefix
func startsWithString(value, prefix interface{}) bool {
	str, ok := toString(value)
	if !ok {
		return false
	}
	prefixStr, ok := toString(prefix)
	if !ok {
		return false
	}
	return strings.HasPrefix(str, prefixStr)
}

// endsWithString checks if string ends with suffix
func endsWithString(value, suffix interface{}) bool {
	str, ok := toString(value)
	if !ok {
		return false
	}
	suffixStr, ok := toString(suffix)
	if !ok {
		return false
	}
	return strings.HasSuffix(str, suffixStr)
}

// toFloat64 converts a value to float64 if possible
func toFloat64(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case int32:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint64:
		return float64(v), true
	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err == nil {
			return f, true
		}
	}
	return 0, false
}

// toString converts a value to string
func toString(value interface{}) (string, bool) {
	if s, ok := value.(string); ok {
		return s, true
	}
	return fmt.Sprintf("%v", value), true
}

// groupAndAggregate performs grouping and aggregation
func groupAndAggregate(data []map[string]interface{}, groupBy []string, aggregations []Aggregation) []map[string]interface{} {
	// Group data
	groups := make(map[string][]map[string]interface{})
	
	for _, row := range data {
		keyParts := make([]string, len(groupBy))
		for i, field := range groupBy {
			val := getNestedValue(row, field)
			keyParts[i] = fmt.Sprintf("%v", val)
		}
		key := strings.Join(keyParts, "|")
		groups[key] = append(groups[key], row)
	}

	// Aggregate each group
	result := make([]map[string]interface{}, 0, len(groups))
	
	for key, group := range groups {
		row := make(map[string]interface{})
		
		// Set group by values
		keyParts := strings.Split(key, "|")
		for i, field := range groupBy {
			alias := field
			// Extract just the field name for display
			if strings.Contains(field, ".") {
				parts := strings.Split(field, ".")
				alias = parts[len(parts)-1]
			}
			row[alias] = keyParts[i]
		}
		
		// Compute aggregations
		for _, agg := range aggregations {
			value := computeAggregate(group, agg.Field, agg.Op)
			key := agg.Alias
			if key == "" {
				key = fmt.Sprintf("%s_%s", agg.Op, agg.Field)
			}
			row[key] = value
		}
		
		result = append(result, row)
	}

	return result
}

// computeAggregate computes a single aggregation over a set of rows
func computeAggregate(rows []map[string]interface{}, field string, op string) interface{} {
	if len(rows) == 0 {
		return nil
	}

	switch op {
	case "count":
		if field == "" || field == "*" {
			return float64(len(rows))
		}
		// Count non-null values
		count := 0
		for _, row := range rows {
			val := getNestedValue(row, field)
			if val != nil {
				count++
			}
		}
		return float64(count)

	case "sum":
		sum := 0.0
		hasValue := false
		for _, row := range rows {
			val := getNestedValue(row, field)
			if num, ok := toFloat64(val); ok {
				sum += num
				hasValue = true
			}
		}
		if !hasValue {
			return nil
		}
		return sum

	case "avg":
		sum := 0.0
		count := 0
		for _, row := range rows {
			val := getNestedValue(row, field)
			if num, ok := toFloat64(val); ok {
				sum += num
				count++
			}
		}
		if count == 0 {
			return nil
		}
		return sum / float64(count)

	case "min":
		var min float64
		hasValue := false
		for _, row := range rows {
			val := getNestedValue(row, field)
			if num, ok := toFloat64(val); ok {
				if !hasValue || num < min {
					min = num
					hasValue = true
				}
			}
		}
		if !hasValue {
			return nil
		}
		return min

	case "max":
		var max float64
		hasValue := false
		for _, row := range rows {
			val := getNestedValue(row, field)
			if num, ok := toFloat64(val); ok {
				if !hasValue || num > max {
					max = num
					hasValue = true
				}
			}
		}
		if !hasValue {
			return nil
		}
		return max

	case "first":
		if len(rows) == 0 {
			return nil
		}
		return getNestedValue(rows[0], field)

	case "last":
		if len(rows) == 0 {
			return nil
		}
		return getNestedValue(rows[len(rows)-1], field)

	default:
		return nil
	}
}

// contains checks if a slice contains a string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// Helper functions for time-series analysis on historical arrays

// GetLastNMonthsFromHist extracts last N months from a historical array (assumes array is [current, month-1, month-2, ...])
func GetLastNMonthsFromHist(histArray []interface{}, n int) []interface{} {
	if histArray == nil || len(histArray) == 0 {
		return []interface{}{}
	}
	
	end := n
	if end > len(histArray) {
		end = len(histArray)
	}
	
	return histArray[:end]
}

// ComputeMaxFromHist computes max from historical array values
func ComputeMaxFromHist(histArray []interface{}) float64 {
	if histArray == nil || len(histArray) == 0 {
		return 0
	}
	
	maxVal := math.Inf(-1)
	for _, v := range histArray {
		if num, ok := toFloat64(v); ok {
			if num > maxVal {
				maxVal = num
			}
		}
	}
	
	if math.IsInf(maxVal, -1) {
		return 0
	}
	return maxVal
}

// ComputeMinFromHist computes min from historical array values
func ComputeMinFromHist(histArray []interface{}) float64 {
	if histArray == nil || len(histArray) == 0 {
		return 0
	}
	
	minVal := math.Inf(1)
	for _, v := range histArray {
		if num, ok := toFloat64(v); ok {
			if num < minVal {
				minVal = num
			}
		}
	}
	
	if math.IsInf(minVal, 1) {
		return 0
	}
	return minVal
}

// HasValueInHist checks if historical array contains a specific value
func HasValueInHist(histArray []interface{}, target interface{}) bool {
	if histArray == nil || len(histArray) == 0 {
		return false
	}
	
	for _, v := range histArray {
		if compareEqual(v, target) {
			return true
		}
	}
	return false
}

// CountOccurrencesInHist counts occurrences of a value in historical array
func CountOccurrencesInHist(histArray []interface{}, target interface{}) int {
	if histArray == nil || len(histArray) == 0 {
		return 0
	}
	
	count := 0
	for _, v := range histArray {
		if compareEqual(v, target) {
			count++
		}
	}
	return count
}

// SumHistValues sums all values in historical array
func SumHistValues(histArray []interface{}) float64 {
	if histArray == nil || len(histArray) == 0 {
		return 0
	}
	
	sum := 0.0
	for _, v := range histArray {
		if num, ok := toFloat64(v); ok {
			sum += num
		}
	}
	return sum
}

// AvgHistValues computes average of historical array values
func AvgHistValues(histArray []interface{}) float64 {
	if histArray == nil || len(histArray) == 0 {
		return 0
	}
	
	sum := 0.0
	count := 0
	for _, v := range histArray {
		if num, ok := toFloat64(v); ok {
			sum += num
			count++
		}
	}
	
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

// MarshalJSONWithIndent marshals JSON with indentation for pretty printing
func MarshalJSONWithIndent(v interface{}) (string, error) {
	bytes, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// ParseDate parses a date string in various formats
func ParseDate(dateStr string) (time.Time, error) {
	formats := []string{
		"2006-01-02",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05",
		"02-01-2006",
		"01/02/2006",
		"2006/01/02",
	}
	
	for _, format := range formats {
		if t, err := time.Parse(format, dateStr); err == nil {
			return t, nil
		}
	}
	
	return time.Time{}, fmt.Errorf("unable to parse date: %s", dateStr)
}

// IsWithinMonths checks if a date is within the last N months from now
func IsWithinMonths(date time.Time, months int) bool {
	now := time.Now()
	cutoff := now.AddDate(0, -months, 0)
	return date.After(cutoff) || date.Equal(cutoff)
}
