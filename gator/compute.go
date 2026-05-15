package gator

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
)

// ToFloat64 converts common numeric types to float64.
func ToFloat64(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case json.Number:
		f, err := val.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

// ===================== Parameter helpers =====================

func getParamInt(params map[string]interface{}, key string, def int) int {
	if params == nil {
		return def
	}
	if v, ok := params[key]; ok {
		if f, ok := ToFloat64(v); ok {
			return int(f)
		}
	}
	return def
}

func getParamFloat(params map[string]interface{}, key string) float64 {
	if params == nil {
		return 0
	}
	if v, ok := params[key]; ok {
		if f, ok := ToFloat64(v); ok {
			return f
		}
	}
	return 0
}

func getParamString(params map[string]interface{}, key string) string {
	if params == nil {
		return ""
	}
	if v, ok := params[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// lastN returns the params["n"] integer, defaulting to 6.
func lastN(params map[string]interface{}) int {
	return getParamInt(params, "n", 6)
}

// ===================== Date window helper =====================

// IsWithinPeriod reports whether dateStr falls within the last N periods before
// refDateStr.  Supported date formats: "2006-01-02" (full) and "2006-01" (year-month,
// treated as the first day of that month).
//
// The unit is controlled by params["unit"]:
//
//	"month" (default) — calendar months via time.AddDate(0, -n, 0)
//	"day"             — calendar days  via time.AddDate(0, 0, -n)
//	"year"            — calendar years via time.AddDate(-n, 0, 0)
//
// If refDateStr is empty, the current date is used.
func IsWithinPeriod(dateStr string, n int, refDateStr string, unit string) bool {
	t, err := parseDateFlexible(dateStr)
	if err != nil {
		return false
	}
	var ref time.Time
	if refDateStr != "" {
		if ref, err = parseDateFlexible(refDateStr); err != nil {
			ref = time.Now()
		}
	} else {
		ref = time.Now()
	}
	var cutoff time.Time
	switch unit {
	case "day":
		cutoff = ref.AddDate(0, 0, -n)
	case "year":
		cutoff = ref.AddDate(-n, 0, 0)
	default: // "month"
		cutoff = ref.AddDate(0, -n, 0)
	}
	return !t.Before(cutoff) && !t.After(ref)
}

// parseDateFlexible parses "2006-01-02" or "2006-01" (→ first of month).
func parseDateFlexible(s string) (time.Time, error) {
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01", s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("unrecognised date format: %q", s)
}

// ===================== Slice-level helpers (operate on []interface{}) =====================

// worstInLastN returns the maximum numeric value in the last n elements of arr,
// or nil if no numeric values are found.
func worstInLastN(arr []interface{}, n int) *float64 {
	if len(arr) == 0 {
		return nil
	}
	start := len(arr) - n
	if start < 0 {
		start = 0
	}
	best := -math.MaxFloat64
	found := false
	for i := start; i < len(arr); i++ {
		if f, ok := ToFloat64(arr[i]); ok {
			if !found || f > best {
				best = f
				found = true
			}
		}
	}
	if !found {
		return nil
	}
	return &best
}

// floatEq reports whether two float64 values are equal within a small epsilon.
// This avoids false negatives from floating-point representation errors when
// comparing collectability codes, delinquency buckets, or other values that
// are logically integers but may be stored as float64.
const floatEps = 1e-9

func floatEq(a, b float64) bool {
	return math.Abs(a-b) < floatEps
}

// everHasInLastN returns true if any of the last n elements of arr equals targetVal.
func everHasInLastN(arr []interface{}, n int, targetVal float64) bool {
	if len(arr) == 0 {
		return false
	}
	start := len(arr) - n
	if start < 0 {
		start = 0
	}
	for i := start; i < len(arr); i++ {
		if f, ok := ToFloat64(arr[i]); ok && floatEq(f, targetVal) {
			return true
		}
	}
	return false
}

// sumLastN sums the last n numeric elements of arr.
func sumLastN(arr []interface{}, n int) float64 {
	if len(arr) == 0 {
		return 0
	}
	start := len(arr) - n
	if start < 0 {
		start = 0
	}
	total := 0.0
	for i := start; i < len(arr); i++ {
		if f, ok := ToFloat64(arr[i]); ok {
			total += f
		}
	}
	return total
}

// minInLastN returns the minimum numeric value in the last n elements of arr.
func minInLastN(arr []interface{}, n int) *float64 {
	if len(arr) == 0 {
		return nil
	}
	start := len(arr) - n
	if start < 0 {
		start = 0
	}
	best := math.MaxFloat64
	found := false
	for i := start; i < len(arr); i++ {
		if f, ok := ToFloat64(arr[i]); ok {
			if !found || f < best {
				best = f
				found = true
			}
		}
	}
	if !found {
		return nil
	}
	return &best
}

// everHasInLastNWithCmp checks whether any of the last n elements of arr
// satisfies comparator op against targetVal.
func everHasInLastNWithCmp(arr []interface{}, n int, targetVal float64, op string) bool {
	if len(arr) == 0 {
		return false
	}
	start := len(arr) - n
	if start < 0 {
		start = 0
	}
	for i := start; i < len(arr); i++ {
		if f, ok := ToFloat64(arr[i]); ok && compareWithOp(f, targetVal, op) {
			return true
		}
	}
	return false
}
//
// When avg is computed over an array (per-record pass), we cannot reduce to a
// scalar yet — we need to carry (sum, count) so the cross-record combine phase
// can produce a correct weighted average instead of a mean-of-means.
//
// avgAccum is used only internally within ComputeAggregation and CrossRecordOp.
// It is never returned as a final aggregation result — the last call to
// ComputeAggregation("avg",...) on a slice of avgAccums collapses them to float64.
type avgAccum struct {
	Sum   float64
	Count int
}

// toAvgAccum coerces a value to avgAccum.
// Accepts: avgAccum (pass-through), float64 (treat as sum=v, count=1).
func toAvgAccum(v interface{}) (avgAccum, bool) {
	switch val := v.(type) {
	case avgAccum:
		return val, true
	case float64:
		return avgAccum{Sum: val, Count: 1}, true
	}
	return avgAccum{}, false
}
//
// Supported operators:
//
//	Scalar ops (work on any []interface{} of numbers or already-aggregated scalars):
//	  count           — number of values in the slice
//	  count_distinct  — number of distinct values
//	  sum             — numeric sum
//	  avg             — arithmetic mean
//	  min / max       — minimum / maximum numeric value
//
//	Window ops (values are []interface{} numeric history slices, one per array element):
//	  worst_last_n    — max value across all elements in the last params["n"] positions
//	  ever_has_last_n — 1.0 if any element equals params["value"] in last n positions
//	  count_last_n    — count of elements where params["value"] appears in last n positions
//	  sum_last_n      — sum of last n positions across all elements
//
//	Date window op (values are date strings "YYYY-MM-DD"):
//	  count_date_last_n — count of date strings falling within the last params["n"]
//	                      periods (params["unit"]: "month"|"day"|"year", default "month")
//	                      before params["ref_date"] (default: today).
//	                      Also accepts already-aggregated float64 scalars (for cross-record
//	                      summing) so the same op is used at both the element and group level.

func ComputeAggregation(values []interface{}, op string, params map[string]interface{}) interface{} {
	switch op {

	// ── Scalar ops ──────────────────────────────────────────────────────────

	case "count":
		// SQL COUNT(field) — exclude nil values.
		c := 0
		for _, v := range values {
			if v != nil {
				c++
			}
		}
		return float64(c)

	case "count_rows":
		// SQL COUNT(*) — count all rows including nil.
		return float64(len(values))

	case "count_distinct":
		seen := map[string]bool{}
		for _, v := range values {
			if v == nil {
				continue
			}
			seen[fmt.Sprintf("%v", v)] = true
		}
		return float64(len(seen))

	case "sum":
		total := 0.0
		for _, v := range values {
			if f, ok := ToFloat64(v); ok {
				total += f
			}
		}
		return total

	case "avg":
		if len(values) == 0 {
			return nil
		}
		// Two modes:
		// A) values are raw numerics (first pass over array elements or parent records)
		//    → accumulate sum+count, return avgAccum for cross-record combining.
		// B) values are avgAccum carriers (cross-record combine pass after CrossRecordOp)
		//    → merge all accumulators, return final float64.
		//
		// We detect mode B if the first non-nil value is an avgAccum.
		// This ensures weighted averaging: (sum1+sum2+...) / (count1+count2+...).
		var totalSum float64
		var totalCount int
		for _, v := range values {
			if acc, ok := toAvgAccum(v); ok {
				totalSum += acc.Sum
				totalCount += acc.Count
			} else if f, ok2 := ToFloat64(v); ok2 {
				totalSum += f
				totalCount++
			}
		}
		if totalCount == 0 {
			return nil
		}
		// If all inputs were raw numerics (not avgAccum), we are in the "leaf"
		// call: return an avgAccum so callers can weight-combine further.
		// If any input was already an avgAccum, we are in the final combine phase:
		// return the collapsed float64.
		allRaw := true
		for _, v := range values {
			if _, isAcc := v.(avgAccum); isAcc {
				allRaw = false
				break
			}
		}
		if allRaw {
			// Leaf pass: return accumulator for upstream combining.
			return avgAccum{Sum: totalSum, Count: totalCount}
		}
		// Final combine pass: collapse to float64.
		return totalSum / float64(totalCount)

	case "min":
		if len(values) == 0 {
			return nil
		}
		best := math.MaxFloat64
		found := false
		for _, v := range values {
			if f, ok := ToFloat64(v); ok && (!found || f < best) {
				best = f
				found = true
			}
		}
		if !found {
			return nil
		}
		return best

	case "max":
		if len(values) == 0 {
			return nil
		}
		best := -math.MaxFloat64
		found := false
		for _, v := range values {
			if f, ok := ToFloat64(v); ok && (!found || f > best) {
				best = f
				found = true
			}
		}
		if !found {
			return nil
		}
		return best

	// ── Window ops on numeric history slices ────────────────────────────────

	case "worst_last_n", "max_last_n":
		// Each value may be a []interface{} history slice or an already-aggregated scalar.
		n := lastN(params)
		best := -math.MaxFloat64
		found := false
		for _, v := range values {
			if arr, ok := v.([]interface{}); ok {
				if w := worstInLastN(arr, n); w != nil && (!found || *w > best) {
					best = *w
					found = true
				}
			} else if f, ok := ToFloat64(v); ok && (!found || f > best) {
				best = f
				found = true
			}
		}
		if !found {
			return nil
		}
		return best

	case "ever_has_last_n":
		// params["comparator"]: "$eq"(default),"$gte","$gt","$lte","$lt"
		n := lastN(params)
		target := getParamFloat(params, "value")
		cmp := getParamString(params, "comparator")
		if cmp == "" {
			cmp = "$eq"
		}
		for _, v := range values {
			if arr, ok := v.([]interface{}); ok {
				if everHasInLastNWithCmp(arr, n, target, cmp) {
					return 1.0
				}
			} else if f, ok := ToFloat64(v); ok && compareWithOp(f, target, cmp) {
				return 1.0
			}
		}
		return 0.0

	case "count_last_n":
		// params["comparator"]: "$eq"(default),"$gte","$gt","$lte","$lt"
		n := lastN(params)
		target := getParamFloat(params, "value")
		cmp := getParamString(params, "comparator")
		if cmp == "" {
			cmp = "$eq"
		}
		count := 0
		for _, v := range values {
			if arr, ok := v.([]interface{}); ok {
				if everHasInLastNWithCmp(arr, n, target, cmp) {
					count++
				}
			} else if f, ok := ToFloat64(v); ok && compareWithOp(f, target, cmp) {
				count++
			}
		}
		return float64(count)

	case "min_last_n":
		// Min value across last n positions of each element's array.
		n := lastN(params)
		best := math.MaxFloat64
		found := false
		for _, v := range values {
			if arr, ok := v.([]interface{}); ok {
				if m := minInLastN(arr, n); m != nil && (!found || *m < best) {
					best = *m
					found = true
				}
			} else if f, ok := ToFloat64(v); ok && (!found || f < best) {
				best = f
				found = true
			}
		}
		if !found {
			return nil
		}
		return best

	case "count_since":
		// Alias for count_date_last_n using months — params["n"] = months.
		// count_since counts ISO date strings >= now - N months.
		n := lastN(params)
		threshold := time.Now().AddDate(0, -n, 0).Format("2006-01-02")
		total := 0.0
		for _, v := range values {
			if s, ok := v.(string); ok && s >= threshold {
				total++
			} else if f, ok := ToFloat64(v); ok {
				total += f
			}
		}
		return total

	case "sum_last_n":
		n := lastN(params)
		total := 0.0
		for _, v := range values {
			if arr, ok := v.([]interface{}); ok {
				total += sumLastN(arr, n)
			} else if f, ok := ToFloat64(v); ok {
				total += f
			}
		}
		return total

	// ── Generic date-window count ────────────────────────────────────────────
	//
	// count_date_last_n
	//   params["n"]        — number of periods (default 6)
	//   params["unit"]     — "month" | "day" | "year" (default "month")
	//   params["ref_date"] — reference date "YYYY-MM-DD" (default today)
	//
	// When applied to raw element values (strings), counts elements whose date
	// falls within the window.  When applied to already-aggregated scalars
	// (cross-record combining), sums them.
	case "count_date_last_n":
		n := lastN(params)
		unit := getParamString(params, "unit")
		if unit == "" {
			unit = "month"
		}
		refDate := getParamString(params, "ref_date")
		total := 0.0
		for _, v := range values {
			if s, ok := v.(string); ok {
				if IsWithinPeriod(s, n, refDate, unit) {
					total++
				}
			} else if f, ok := ToFloat64(v); ok {
				// already-aggregated per-record scalar → accumulate
				total += f
			}
		}
		return total

	default:
		return nil
	}
}

// ===================== Cross-record operator =====================

// CrossRecordOp returns the operator used to combine per-record scalar results
// across a group.  For most ops the same operator is re-applied (e.g. max of
// per-record maxes).  For ops that produce partial counts/sums per record,
// "sum" is used to accumulate them.  For ever_has_last_n, "max" is used so
// that a single positive record makes the whole group positive.
func CrossRecordOp(op string) string {
	switch op {
	case "sum", "count", "count_rows", "count_distinct", "count_last_n",
		"sum_last_n", "count_date_last_n", "count_since":
		return "sum"
	case "ever_has_last_n":
		return "max"
	default:
		return op
	}
}

func zeroForOp(op string) (interface{}, bool) {
	switch op {
	case "sum", "count", "count_rows", "count_distinct", "ever_has_last_n",
		"count_last_n", "sum_last_n", "count_date_last_n", "count_since", "min_last_n":
		return 0.0, true
	default:
		return nil, false
	}
}

// ===================== Array-field aggregation (per parent record) =====================

// AggregateArrayField extracts the nested array at arrayPath from record, collects
// values of the leaf field (relative to the array element), and computes the
// aggregation.
//
// Returns (value, true) when the array field exists on the record (even if empty).
// Returns (nil, false) when the field path is absent from the record entirely.
// The boolean lets the caller distinguish "record has no such array" from
// "array is present but empty → zero value".
func AggregateArrayField(record map[string]interface{}, agg AggConfig, arrayPath string) (interface{}, bool) {
	parts := strings.Split(arrayPath, ".")
	parent, key, found := NavigateToParent(record, parts)
	if !found {
		return nil, false
	}
	arr, ok := parent[key].([]interface{})
	if !ok {
		return nil, false
	}

	// Relative field within each array element
	relField := strings.TrimPrefix(agg.Field, arrayPath+".")

	var values []interface{}
	for _, elem := range arr {
		if elemMap, ok := elem.(map[string]interface{}); ok {
			if val, ok2 := GetFieldValue(elemMap, relField); ok2 {
				values = append(values, val)
			}
		}
	}

	if len(values) == 0 {
		if z, ok := zeroForOp(agg.Op); ok {
			return z, true
		}
		return nil, true // present but genuinely undefined (e.g. max of empty set)
	}

	return ComputeAggregation(values, agg.Op, agg.Params), true
}
