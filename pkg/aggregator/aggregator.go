package aggregator

import (
"encoding/json"
"fmt"
"sort"
"strconv"
"strings"
"time"
)

// AggregateRequest represents the request structure for aggregation
type AggregateRequest struct {
Dataset      string                 `json:"dataset"`
Data         []map[string]interface{} `json:"data,omitempty"`
Where        map[string]interface{}   `json:"where,omitempty"`
GroupBy      []string               `json:"groupBy,omitempty"`
Aggregations []Aggregation          `json:"aggregations,omitempty"`
SpecialAggs  []SpecialAggregation   `json:"specialAggregations,omitempty"`
MonthsCount  int                    `json:"monthsCount,omitempty"`
}

// Aggregation represents a single aggregation operation
type Aggregation struct {
Field string `json:"field"`
Op    string `json:"op"`
Alias string `json:"alias"`
}

// SpecialAggregation represents time-series special aggregations
type SpecialAggregation struct {
Type  string `json:"type"` // worst_delinquency, max_collectability, ever_has_collectability
Field string `json:"field"` // typically credits.last_24_delq_hist or credits.last_24_coll_hist
Alias string `json:"alias"`
}

// AggregateResponse represents the response structure
type AggregateResponse struct {
Data       []map[string]interface{} `json:"data"`
Schema     []string                 `json:"schema"`
Flattened  []map[string]interface{} `json:"flattened,omitempty"`
TotalRows  int                      `json:"totalRows"`
}

// Flattener handles flattening of nested JSON structures
type Flattener struct {
arrayFields []string
}

// NewFlattener creates a new Flattener instance
func NewFlattener() *Flattener {
return &Flattener{
arrayFields: make([]string, 0),
}
}

// Flatten converts nested objects and arrays into flat key-value pairs using dot notation
// For arrays of objects, it creates one row per array element (cross join behavior)
// For arrays of primitives, it keeps them as-is without expansion
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
// Array - check if it's an array of objects or primitives
if len(v) == 0 {
// Empty array - set to empty
for i := range result {
result[i][fullKey] = make([]interface{}, 0)
}
} else {
// Check if array contains objects (maps) or primitives
if _, isObject := v[0].(map[string]interface{}); isObject {
// Array of objects - expand (cross join)
newResult := make([]map[string]interface{}, 0)
for _, base := range result {
for _, elem := range v {
row := make(map[string]interface{})
for k, val := range base {
row[k] = val
}
// Recursively flatten the object element
elemObj, ok := elem.(map[string]interface{})
if ok {
elemResults := f.flattenObject(elemObj, fullKey)
for _, elemRow := range elemResults {
merged := make(map[string]interface{})
for k, val := range row {
merged[k] = val
}
for k, val := range elemRow {
merged[k] = val
// Track this as an array-derived field
if !contains(f.arrayFields, k) {
f.arrayFields = append(f.arrayFields, k)
}
}
newResult = append(newResult, merged)
}
} else {
row[fullKey] = elem
newResult = append(newResult, row)
}
}
}
result = newResult
} else {
// Array of primitives - keep as-is (don't expand)
for i := range result {
result[i][fullKey] = v
}
}
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
func DetectSchema(data []map[string]interface{}) []FieldInfo {
if len(data) == 0 {
return []FieldInfo{}
}

fields := make(map[string]FieldInfo)
flattener := NewFlattener()
sample := flattener.Flatten([]map[string]interface{}{data[0]})

if len(sample) == 0 {
return []FieldInfo{}
}

for key, value := range sample[0] {
fieldType := detectType(value)
isArray := false
isNested := strings.Contains(key, ".")

// Check if this is from an array field
if contains(flattener.GetArrayFields(), key) {
isArray = true
}

fields[key] = FieldInfo{
Name:   key,
Type:   fieldType,
Array:  isArray,
Nested: isNested,
}
}

// Convert to slice and sort
result := make([]FieldInfo, 0, len(fields))
for _, field := range fields {
result = append(result, field)
}

sort.Slice(result, func(i, j int) bool {
return result[i].Name < result[j].Name
})

return result
}

// FieldInfo contains information about a field
type FieldInfo struct {
Name   string `json:"name"`
Type   string `json:"type"`
Array  bool   `json:"array,omitempty"`
Nested bool   `json:"nested,omitempty"`
}

func detectType(value interface{}) string {
switch v := value.(type) {
case nil:
return "null"
case bool:
return "boolean"
case float64:
if v == float64(int64(v)) {
return "integer"
}
return "number"
case string:
return "string"
case []interface{}:
return "array"
case map[string]interface{}:
return "object"
default:
return "unknown"
}
}

// contains checks if a string exists in a slice
func contains(slice []string, item string) bool {
for _, s := range slice {
if s == item {
return true
}
}
return false
}

// MarshalJSONWithIndent formats data as pretty-printed JSON
func MarshalJSONWithIndent(data interface{}) (string, error) {
bytes, err := json.MarshalIndent(data, "", "  ")
if err != nil {
return "", err
}
return string(bytes), nil
}

// Aggregate performs the main aggregation logic
func Aggregate(req AggregateRequest) (*AggregateResponse, error) {
// Get data
var data []map[string]interface{}
if req.Data != nil && len(req.Data) > 0 {
data = req.Data
} else if req.Dataset != "" {
data = GetDataset(req.Dataset)
if data == nil {
return nil, fmt.Errorf("dataset '%s' not found", req.Dataset)
}
} else {
return nil, fmt.Errorf("no data or dataset provided")
}

// Detect schema from original data
schema := DetectSchema(data)
schemaNames := make([]string, len(schema))
for i, f := range schema {
schemaNames[i] = f.Name
}

// Flatten data for querying
flattener := NewFlattener()
flattened := flattener.Flatten(data)

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

if len(req.GroupBy) == 0 && len(req.Aggregations) == 0 && len(req.SpecialAggs) == 0 {
// No aggregation, return filtered data
result = flattened
} else {
// Group and aggregate
groups := make(map[string][]map[string]interface{})

if len(req.GroupBy) == 0 {
// Single group (all rows)
groups["__all__"] = flattened
} else {
// Multiple groups
for _, row := range flattened {
keyParts := make([]string, len(req.GroupBy))
for i, field := range req.GroupBy {
val := getNestedValue(row, field)
keyParts[i] = fmt.Sprintf("%v", val)
}
key := strings.Join(keyParts, "|")
groups[key] = append(groups[key], row)
}
}

// Calculate aggregations for each group
result = make([]map[string]interface{}, 0)
for _, rows := range groups {
	rowResult := make(map[string]interface{})
	
	// Add group by fields
	if len(req.GroupBy) > 0 {
		for _, field := range req.GroupBy {
			rowResult[field] = getNestedValue(rows[0], field)
		}
	}

// Calculate standard aggregations
for _, agg := range req.Aggregations {
value := calculateAggregation(rows, agg)
rowResult[agg.Alias] = value
}

// Calculate special aggregations
for _, spec := range req.SpecialAggs {
value := calculateSpecialAggregation(rows, spec, req.MonthsCount)
rowResult[spec.Alias] = value
}

result = append(result, rowResult)
}
}

return &AggregateResponse{
Data:      result,
Schema:    schemaNames,
Flattened: flattened,
TotalRows: len(result),
}, nil
}

// getNestedValue retrieves a value from a nested map using dot notation
func getNestedValue(data map[string]interface{}, path string) interface{} {
parts := strings.Split(path, ".")
current := data

for i, part := range parts {
val, ok := current[part]
if !ok {
return nil
}

if i == len(parts)-1 {
return val
}

if next, ok := val.(map[string]interface{}); ok {
current = next
} else {
return nil
}
}

return nil
}

// matchesWhere checks if a row matches the where conditions
func matchesWhere(row map[string]interface{}, where map[string]interface{}) bool {
for field, condition := range where {
fieldValue := getNestedValue(row, field)

switch cond := condition.(type) {
case map[string]interface{}:
// Complex condition with operator
for op, value := range cond {
if !evaluateOperator(fieldValue, op, value) {
return false
}
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

// evaluateOperator evaluates a single operator condition
func evaluateOperator(fieldValue interface{}, op string, value interface{}) bool {
switch op {
case "eq":
return compareValues(fieldValue, value) == 0
case "ne":
return compareValues(fieldValue, value) != 0
case "gt":
return compareValues(fieldValue, value) > 0
case "gte":
return compareValues(fieldValue, value) >= 0
case "lt":
return compareValues(fieldValue, value) < 0
case "lte":
return compareValues(fieldValue, value) <= 0
case "in":
if arr, ok := value.([]interface{}); ok {
for _, v := range arr {
if compareValues(fieldValue, v) == 0 {
return true
}
}
return false
}
return false
default:
return false
}
}

// compareValues compares two values, handling type conversions
func compareValues(a, b interface{}) int {
// Handle nil
if a == nil && b == nil {
return 0
}
if a == nil {
return -1
}
if b == nil {
return 1
}

// Convert to comparable types
aNum, aOk := toNumber(a)
bNum, bOk := toNumber(b)

if aOk && bOk {
if aNum < bNum {
return -1
}
if aNum > bNum {
return 1
}
return 0
}

// String comparison
aStr := fmt.Sprintf("%v", a)
bStr := fmt.Sprintf("%v", b)

if aStr < bStr {
return -1
}
if aStr > bStr {
return 1
}
return 0
}

// toNumber attempts to convert a value to float64
func toNumber(v interface{}) (float64, bool) {
switch val := v.(type) {
case float64:
return val, true
case int:
return float64(val), true
case int64:
return float64(val), true
case string:
if num, err := strconv.ParseFloat(val, 64); err == nil {
return num, true
}
}
return 0, false
}

// calculateAggregation calculates a single aggregation
func calculateAggregation(rows []map[string]interface{}, agg Aggregation) interface{} {
values := make([]float64, 0)

for _, row := range rows {
val := getNestedValue(row, agg.Field)
if num, ok := toNumber(val); ok {
values = append(values, num)
}
}

if len(values) == 0 {
return nil
}

switch agg.Op {
case "sum":
sum := 0.0
for _, v := range values {
sum += v
}
return sum
case "avg":
sum := 0.0
for _, v := range values {
sum += v
}
return sum / float64(len(values))
case "min":
min := values[0]
for _, v := range values[1:] {
if v < min {
min = v
}
}
return min
case "max":
max := values[0]
for _, v := range values[1:] {
if v > max {
max = v
}
}
return max
case "count":
if agg.Field == "*" || agg.Field == "" {
return len(rows)
}
return len(values)
default:
return nil
}
}

// calculateSpecialAggregation calculates special time-series aggregations
func calculateSpecialAggregation(rows []map[string]interface{}, spec SpecialAggregation, monthsCount int) interface{} {
if monthsCount <= 0 {
monthsCount = 6 // default
}

switch spec.Type {
case "worst_delinquency":
// Find maximum delinquency in last N months from last_24_delq_hist
maxDelq := 0
for _, row := range rows {
histVal := getNestedValue(row, spec.Field)
if hist, ok := histVal.([]interface{}); ok {
// Get last N months (from end of array)
startIdx := len(hist) - monthsCount
if startIdx < 0 {
startIdx = 0
}
for i := startIdx; i < len(hist); i++ {
if delq, ok := toNumber(hist[i]); ok {
if int(delq) > maxDelq {
maxDelq = int(delq)
}
}
}
}
}
return maxDelq

case "max_collectability":
// Find maximum collectability code in last N months from last_24_coll_hist
maxColl := 0
for _, row := range rows {
histVal := getNestedValue(row, spec.Field)
if hist, ok := histVal.([]interface{}); ok {
startIdx := len(hist) - monthsCount
if startIdx < 0 {
startIdx = 0
}
for i := startIdx; i < len(hist); i++ {
if coll, ok := toNumber(hist[i]); ok {
if int(coll) > maxColl {
maxColl = int(coll)
}
}
}
}
}
return maxColl

case "ever_has_collectability":
// Check if any record ever had a specific collectability code in last N months
// Returns true/false or count
count := 0
for _, row := range rows {
histVal := getNestedValue(row, spec.Field)
if hist, ok := histVal.([]interface{}); ok {
startIdx := len(hist) - monthsCount
if startIdx < 0 {
startIdx = 0
}
for i := startIdx; i < len(hist); i++ {
if coll, ok := toNumber(hist[i]); ok {
if int(coll) >= 3 { // Bad collectability (3, 4, 5)
count++
break
}
}
}
}
}
return count

default:
return nil
}
}

// ParseDate parses a date string
func ParseDate(dateStr string) (time.Time, error) {
layouts := []string{
"2006-01-02",
"2006/01/02",
"02-01-2006",
"02/01/2006",
time.RFC3339,
}

for _, layout := range layouts {
if t, err := time.Parse(layout, dateStr); err == nil {
return t, nil
}
}

return time.Time{}, fmt.Errorf("unable to parse date: %s", dateStr)
}
