package gator

import (
	"fmt"
	"strings"
)

// EvaluateWhere returns true if record satisfies all conditions in the where map.
// The where map has the shape: { "fieldPath": { "$op": value, ... }, ... }.
// Multiple field conditions are ANDed; multiple ops on the same field are also ANDed.
func EvaluateWhere(record map[string]interface{}, where map[string]map[string]interface{}) bool {
	for field, ops := range where {
		val, found := GetFieldValue(record, field)
		for op, opVal := range ops {
			if !evaluateOp(val, found, op, opVal) {
				return false
			}
		}
	}
	return true
}

func evaluateOp(val interface{}, found bool, op string, opVal interface{}) bool {
	switch op {
	case "$eq":
		return found && compareValues(val, opVal) == 0
	case "$ne":
		return !found || compareValues(val, opVal) != 0
	case "$gt":
		return found && compareValues(val, opVal) > 0
	case "$gte":
		return found && compareValues(val, opVal) >= 0
	case "$lt":
		return found && compareValues(val, opVal) < 0
	case "$lte":
		return found && compareValues(val, opVal) <= 0
	case "$in":
		if !found {
			return false
		}
		arr, ok := opVal.([]interface{})
		if !ok {
			return false
		}
		for _, item := range arr {
			if compareValues(val, item) == 0 {
				return true
			}
		}
		return false
	case "$contains":
		if !found {
			return false
		}
		s, ok := val.(string)
		sub, ok2 := opVal.(string)
		return ok && ok2 && strings.Contains(s, sub)
	case "$notnull":
		return found && val != nil
	case "$null":
		return !found || val == nil
	default:
		return true
	}
}

// compareValues compares two values numerically if both are numeric,
// otherwise lexicographically as strings.  Returns -1, 0, or 1.
func compareValues(a, b interface{}) int {
	af, aOk := ToFloat64(a)
	bf, bOk := ToFloat64(b)
	if aOk && bOk {
		switch {
		case af < bf:
			return -1
		case af > bf:
			return 1
		default:
			return 0
		}
	}
	as := fmt.Sprintf("%v", a)
	bs := fmt.Sprintf("%v", b)
	switch {
	case as < bs:
		return -1
	case as > bs:
		return 1
	default:
		return 0
	}
}

// ApplyLocalFilters filters elements inside named array fields in-place (on a deep
// copy) BEFORE any grouping or aggregation.
//
// localFilter shape:
//
//	{ "arrayPath": { "arrayPath.fieldName": { "$op": value } } }
//
// Field names inside the conditions use the full dot-notation path (e.g.
// "credits.loan_status"), which we strip to the relative key ("loan_status")
// before evaluating against each array element map.
func ApplyLocalFilters(data []interface{}, localFilter map[string]map[string]map[string]interface{}) []interface{} {
	if len(localFilter) == 0 {
		return data
	}
	result := make([]interface{}, len(data))
	for i, record := range data {
		m, ok := record.(map[string]interface{})
		if !ok {
			result[i] = record
			continue
		}
		copied := DeepCopyMap(m)
		for arrayPath, conditions := range localFilter {
			if len(conditions) == 0 {
				continue
			}
			// Strip the array path prefix: "credits.loan_status" → "loan_status"
			stripped := make(map[string]map[string]interface{}, len(conditions))
			for field, ops := range conditions {
				rel := strings.TrimPrefix(field, arrayPath+".")
				stripped[rel] = ops
			}

			parts := strings.Split(arrayPath, ".")
			parent, key, found := NavigateToParent(copied, parts)
			if !found {
				continue
			}
			arr, ok := parent[key].([]interface{})
			if !ok {
				continue
			}
			var kept []interface{}
			for _, elem := range arr {
				if elemMap, ok := elem.(map[string]interface{}); ok {
					if EvaluateWhere(elemMap, stripped) {
						kept = append(kept, elem)
					}
				}
			}
			parent[key] = kept
		}
		result[i] = copied
	}
	return result
}
