// Package gator provides a generic JSON/XML aggregation engine.
// It supports SQL-like operations (GROUP BY, WHERE, SUM, MAX, MIN, AVG, COUNT,
// and time-window ops) over arbitrary nested structures — including nested arrays
// up to any depth — without requiring a fixed schema.
package gator

import (
	"fmt"
	"sort"
	"strings"
)

// ===================== Public Types =====================

// AggregateRequest is the DSL sent by clients to /api/aggregate.
type AggregateRequest struct {
	Dataset      string                                       `json:"dataset"`
	Data         []interface{}                                `json:"data"`
	LocalFilter  map[string]map[string]map[string]interface{} `json:"localFilter"`
	Where        map[string]map[string]interface{}            `json:"where"`
	GroupBy      []string                                     `json:"groupBy"`
	Aggregations []AggConfig                                  `json:"aggregations"`
}

// AggConfig describes one aggregation metric.
type AggConfig struct {
	Field  string                 `json:"field"`
	Op     string                 `json:"op"`
	Alias  string                 `json:"alias"`
	Params map[string]interface{} `json:"params"`
}

// ===================== Dataset Registry =====================

// Store is the in-memory dataset registry.
type Store struct {
	datasets map[string][]interface{}
}

// NewStore creates an empty Store.
func NewStore() *Store {
	return &Store{datasets: map[string][]interface{}{}}
}

// Register adds or replaces a named dataset.
func (s *Store) Register(name string, data []interface{}) {
	s.datasets[name] = data
}

// Get retrieves a dataset by name.
func (s *Store) Get(name string) ([]interface{}, bool) {
	d, ok := s.datasets[name]
	return d, ok
}

// Names returns the sorted list of registered dataset names.
func (s *Store) Names() []string {
	names := make([]string, 0, len(s.datasets))
	for n := range s.datasets {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// ===================== Aggregation classification =====================

// AggLevel describes how an aggregation should be executed.
type AggLevel int

const (
	levelParent AggLevel = iota // field lives at root record level
	levelArray                  // field inside one array (1 level deep)
	levelNested                 // field inside array-within-array (2+ levels)
)

// aggClassified bundles an AggConfig with its resolved execution metadata.
type aggClassified struct {
	AggConfig
	level          AggLevel
	arrayAncestors []string // ordered outermost→innermost array paths
}

// alias returns the output column name.
func (a aggClassified) alias() string {
	if a.Alias != "" {
		return a.Alias
	}
	return a.Op + "_" + strings.ReplaceAll(a.Field, ".", "_")
}

// classifyAggregations inspects each aggregation field against the schema and
// determines how deep into nested arrays the engine must descend.
//
// For each aggregation, it walks every prefix of the dot-notation field path
// and collects all prefixes that correspond to an array type in the schema.
// The resulting slice (arrayAncestors) is ordered outermost-first.
//
// Examples (ideb.xml schema):
//
//	"Summary.TotalOutstanding"
//	  → ancestors: []  → levelParent
//
//	"FasilitasList.Fasilitas.Outstanding"
//	  → ancestors: ["FasilitasList.Fasilitas"]  → levelArray
//
//	"FasilitasList.Fasilitas.RiwayatKolektibilitas.HariTunggakan"
//	  → ancestors: ["FasilitasList.Fasilitas",
//	                "FasilitasList.Fasilitas.RiwayatKolektibilitas"]  → levelNested
func classifyAggregations(aggs []AggConfig, schema []FieldInfo) []aggClassified {
	lookup := map[string]FieldInfo{}
	for _, f := range schema {
		lookup[f.Path] = f
	}

	arrayTypes := map[string]bool{
		"array_object": true, // only object arrays can be intermediate ancestors
	}

	result := make([]aggClassified, 0, len(aggs))
	for _, agg := range aggs {
		parts := strings.Split(agg.Field, ".")
		var ancestors []string
		for i := 1; i <= len(parts); i++ {
			prefix := strings.Join(parts[:i], ".")
			if fi, ok := lookup[prefix]; ok && arrayTypes[fi.Type] {
				ancestors = append(ancestors, prefix)
			}
		}

		var level AggLevel
		switch len(ancestors) {
		case 0:
			level = levelParent
		case 1:
			level = levelArray
		default:
			level = levelNested
		}

		result = append(result, aggClassified{
			AggConfig:      agg,
			level:          level,
			arrayAncestors: ancestors,
		})
	}
	return result
}

// paramString extracts a string param by key (same package as compute.go).
func paramString(params map[string]interface{}, key string) string {
	return getParamString(params, key)
}

// paramInt extracts an int param by key with a default.
func paramInt(params map[string]interface{}, key string, def int) int {
	return getParamInt(params, key, def)
}

// ===================== Group-by classification =====================

// groupByLevel describes where a GROUP BY field lives.
type groupByLevel int

const (
	gbParent groupByLevel = iota // field is a scalar on the parent record
	gbArray                      // field lives inside an array (needs explode)
)

// groupByInfo classifies one GROUP BY field.
type groupByInfo struct {
	field     string
	level     groupByLevel
	arrayPath string // the array path this field belongs to (if gbArray)
}

// classifyGroupBy inspects each groupBy field against the schema.
// Returns the classified list and the "explode array path" — the outermost
// array path that covers all array-level GROUP BY fields. Empty string means
// no array-level fields exist and explode is not needed.
func classifyGroupBy(groupBy []string, schema []FieldInfo) ([]groupByInfo, string) {
	lookup := map[string]FieldInfo{}
	for _, f := range schema {
		lookup[f.Path] = f
	}

	var infos []groupByInfo
	explodePath := ""

	for _, gb := range groupBy {
		fi, ok := lookup[gb]
		if ok && fi.ArrayPath != "" {
			infos = append(infos, groupByInfo{field: gb, level: gbArray, arrayPath: fi.ArrayPath})
			// Track outermost (shortest) array path.
			if explodePath == "" || len(fi.ArrayPath) < len(explodePath) {
				explodePath = fi.ArrayPath
			}
		} else {
			infos = append(infos, groupByInfo{field: gb, level: gbParent})
		}
	}
	return infos, explodePath
}

// ===================== Explode =====================

// explodeRecords expands each parent record by its array at arrayPath into
// one flat row per array element.
//
// Each flat row is a shallow copy of the parent with the array element
// embedded under the last segment of arrayPath (e.g. "credits"). This
// means GetFieldValue still resolves "credits.product_type" → flat["credits"]["product_type"]
// via normal nested traversal.
//
// Records whose array is empty or absent produce exactly one ghost row with
// the array key set to nil — preserving the guarantee that every parent
// record appears in at least one output row.
func explodeRecords(records []map[string]interface{}, arrayPath string) []map[string]interface{} {
	parts := strings.Split(arrayPath, ".")
	arrayKey := parts[len(parts)-1]

	var result []map[string]interface{}
	for _, rec := range records {
		parentMap, key, found := NavigateToParent(rec, parts)
		if !found {
			flat := shallowCopyMapExcept(rec, arrayKey)
			flat[arrayKey] = nil
			result = append(result, flat)
			continue
		}
		arr, ok := parentMap[key].([]interface{})
		if !ok || len(arr) == 0 {
			flat := shallowCopyMapExcept(rec, arrayKey)
			flat[arrayKey] = nil
			result = append(result, flat)
			continue
		}
		for _, elem := range arr {
			flat := shallowCopyMapExcept(rec, arrayKey)
			if elemMap, ok2 := elem.(map[string]interface{}); ok2 {
				flat[arrayKey] = elemMap
			} else {
				flat[arrayKey] = elem
			}
			result = append(result, flat)
		}
	}
	return result
}

// shallowCopyMapExcept returns a shallow copy of m omitting the given key.
func shallowCopyMapExcept(m map[string]interface{}, except string) map[string]interface{} {
	cp := make(map[string]interface{}, len(m))
	for k, v := range m {
		if k != except {
			cp[k] = v
		}
	}
	return cp
}

func buildGroupKey(m map[string]interface{}, groupBy []string) string {
	parts := make([]string, len(groupBy))
	for i, gb := range groupBy {
		if val, ok := GetFieldValue(m, gb); ok {
			parts[i] = fmt.Sprintf("%v", val)
		} else {
			parts[i] = "__nil__"
		}
	}
	return strings.Join(parts, "|||")
}

// ===================== Main entry point =====================

// Aggregate executes the DSL request against store and returns result rows.
//
// Execution pipeline:
//  1. Resolve data source (inline > named dataset).
//  2. Detect schema from first record.
//  3. Apply local filters (pre-filter array elements in-place on deep copies).
//  4. Apply global WHERE (filter parent records).
//  5. Classify GROUP BY fields: parent-level vs array-level.
//  6a. If GROUP BY contains array-level fields → EXPLODE mode:
//      Flatten records by expanding the array into one row per element.
//      Each flat row is grouped and aggregated as if it were a parent record.
//      Aggregations on fields of the exploded array become simple parent-level
//      aggs on the flat rows (field resolves via embedded element map).
//  6b. If GROUP BY contains only parent-level fields → STANDARD mode:
//      Group parent records and apply two-pass aggregation
//      (parent-level direct, array-level per-record then combine).
func Aggregate(store *Store, req AggregateRequest) []interface{} {
	var data []interface{}
	if len(req.Data) > 0 {
		data = req.Data
	} else if ds, ok := store.Get(req.Dataset); ok {
		data = ds
	} else {
		return []interface{}{}
	}
	if len(data) == 0 {
		return []interface{}{}
	}

	schema := DetectSchema(data[0], "", "")

	if len(req.LocalFilter) > 0 {
		data = ApplyLocalFilters(data, req.LocalFilter)
	}

	// Step 4: global WHERE against parent records.
	var filtered []map[string]interface{}
	for _, record := range data {
		if m, ok := record.(map[string]interface{}); ok {
			if EvaluateWhere(m, req.Where) {
				filtered = append(filtered, m)
			}
		}
	}

	// Step 5: classify GROUP BY fields.
	_, explodePath := classifyGroupBy(req.GroupBy, schema)

	if explodePath != "" {
		// ── EXPLODE MODE ────────────────────────────────────────────────────
		// Flatten records by the array, then aggregate on flat rows.
		return aggregateExploded(filtered, req, schema, explodePath)
	}

	// ── STANDARD MODE ───────────────────────────────────────────────────────
	classified := classifyAggregations(req.Aggregations, schema)
	return aggregateStandard(filtered, req, classified)
}

// aggregateStandard is the original two-pass aggregation for queries whose
// GROUP BY fields are all at the parent level.
func aggregateStandard(filtered []map[string]interface{}, req AggregateRequest, classified []aggClassified) []interface{} {
	groups := map[string][]map[string]interface{}{}
	var groupOrder []string
	if len(req.GroupBy) == 0 {
		groups["__all__"] = filtered
		groupOrder = []string{"__all__"}
	} else {
		for _, m := range filtered {
			key := buildGroupKey(m, req.GroupBy)
			if _, exists := groups[key]; !exists {
				groupOrder = append(groupOrder, key)
			}
			groups[key] = append(groups[key], m)
		}
	}

	results := make([]interface{}, 0, len(groupOrder))
	for _, key := range groupOrder {
		records := groups[key]
		row := map[string]interface{}{}

		if key != "__all__" && len(records) > 0 {
			for _, gb := range req.GroupBy {
				if val, ok := GetFieldValue(records[0], gb); ok {
					row[gb] = val
				} else {
					row[gb] = nil
				}
			}
		}

		for _, ca := range classified {
			alias := ca.alias()
			var val interface{}

			switch ca.level {
			case levelParent:
				var vals []interface{}
				for _, r := range records {
					if v, ok := GetFieldValue(r, ca.Field); ok {
						vals = append(vals, v)
					}
				}
				val = ComputeAggregation(vals, ca.Op, ca.Params)

			case levelArray:
				outerPath := ca.arrayAncestors[0]
				var perRecord []interface{}
				for _, r := range records {
					v, exists := AggregateArrayField(r, ca.AggConfig, outerPath)
					if exists && v != nil {
						perRecord = append(perRecord, v)
					}
				}
				val = ComputeAggregation(perRecord, CrossRecordOp(ca.Op), ca.Params)

			case levelNested:
				var perRecord []interface{}
				for _, r := range records {
					v := aggregateNestedField(r, ca, 0)
					if v != nil {
						perRecord = append(perRecord, v)
					}
				}
				val = ComputeAggregation(perRecord, CrossRecordOp(ca.Op), ca.Params)
			}

			if val == nil {
				if z, ok := zeroForOp(ca.Op); ok {
					val = z
				}
			}
			row[alias] = val
		}

		results = append(results, row)
	}
	return results
}

// aggregateExploded handles queries where GROUP BY contains array-level fields.
//
// Strategy:
//  1. Explode filtered records by explodePath into flat rows (one per element).
//  2. Re-detect schema on the flat rows (the array element fields are now
//     accessible as nested map under the array key, so GetFieldValue still
//     works via dot-notation).
//  3. Re-classify aggregations on the flat schema — fields that were
//     array-level before explode are now parent-level on the flat rows
//     (e.g. "credits.outstanding_balance" resolves via flat["credits"]["outstanding_balance"]).
//  4. Group flat rows and aggregate using standard parent-level logic only.
//
// Ghost rows (empty array) appear with array-level GROUP BY fields = null
// and aggregation values = 0/null.
func aggregateExploded(filtered []map[string]interface{}, req AggregateRequest, schema []FieldInfo, explodePath string) []interface{} {
	flatRows := explodeRecords(filtered, explodePath)

	// Flat rows have a different shape than parent records.
	// Re-classify aggregations: on flat rows, "credits.outstanding_balance"
	// is now a simple nested lookup (not inside an array), so ALL aggs become
	// levelParent. We don't re-detect schema; just treat everything as parent-level
	// since the array has been inlined.
	results := make([]interface{}, 0)
	groups := map[string][]map[string]interface{}{}
	var groupOrder []string

	if len(req.GroupBy) == 0 {
		groups["__all__"] = flatRows
		groupOrder = []string{"__all__"}
	} else {
		for _, m := range flatRows {
			key := buildGroupKey(m, req.GroupBy)
			if _, exists := groups[key]; !exists {
				groupOrder = append(groupOrder, key)
			}
			groups[key] = append(groups[key], m)
		}
	}

	for _, key := range groupOrder {
		rows := groups[key]
		row := map[string]interface{}{}

		// Emit GROUP BY values from the first row in the group.
		if key != "__all__" && len(rows) > 0 {
			for _, gb := range req.GroupBy {
				if val, ok := GetFieldValue(rows[0], gb); ok {
					row[gb] = val
				} else {
					row[gb] = nil
				}
			}
		}

		// All aggregations are now parent-level on flat rows.
		// Ghost rows (array was empty) have nil under the array key,
		// so GetFieldValue will miss and the value is excluded from agg.
		for _, agg := range req.Aggregations {
			alias := agg.Alias
			if alias == "" {
				alias = agg.Op + "_" + strings.ReplaceAll(agg.Field, ".", "_")
			}
			var vals []interface{}
			for _, r := range rows {
				if v, ok := GetFieldValue(r, agg.Field); ok && v != nil {
					vals = append(vals, v)
				}
			}
			val := ComputeAggregation(vals, agg.Op, agg.Params)
			if val == nil {
				if z, ok := zeroForOp(agg.Op); ok {
					val = z
				}
			}
			row[alias] = val
		}

		results = append(results, row)
	}
	return results
}

// aggregateNestedField recursively descends through arrayAncestors.
//
// At each depth it navigates to the array at arrayAncestors[depth], iterates
// over its elements, and recurses deeper. At the leaf (depth == last ancestor)
// it collects the target field values and calls ComputeAggregation.
// Per-element scalars from each level are combined with CrossRecordOp before
// bubbling up to the caller.
func aggregateNestedField(record map[string]interface{}, ca aggClassified, depth int) interface{} {
	outerPath := ca.arrayAncestors[depth]
	parts := strings.Split(outerPath, ".")
	parent, key, found := NavigateToParent(record, parts)
	if !found {
		return nil
	}
	arr, ok := parent[key].([]interface{})
	if !ok {
		return nil
	}

	isLeaf := depth == len(ca.arrayAncestors)-1

	if isLeaf {
		relField := strings.TrimPrefix(ca.Field, outerPath+".")
		dateField := paramString(ca.Params, "date_field")

		// Check if the array holds primitives (floats, strings) rather than maps.
		// Example: last_24_delq_hist = [0, 0, 30, 45, ...] — elements are float64.
		// In that case relField == "" (the array IS the value) and we pass it directly.
		isPrimitive := len(arr) > 0
		for _, elem := range arr {
			if _, isMap := elem.(map[string]interface{}); isMap {
				isPrimitive = false
				break
			}
		}

		if isPrimitive || relField == "" {
			// Primitive array — pass directly to ComputeAggregation.
			// date_field filtering is not meaningful for primitive arrays.
			vals := make([]interface{}, len(arr))
			copy(vals, arr)
			if len(vals) == 0 {
				if z, ok := zeroForOp(ca.Op); ok {
					return z
				}
				return nil
			}
			return ComputeAggregation(vals, ca.Op, ca.Params)
		}

		// Object array — extract relField per element with optional date window filter.
		var vals []interface{}
		for _, elem := range arr {
			elemMap, ok := elem.(map[string]interface{})
			if !ok {
				continue
			}
			if dateField != "" {
				if dateVal, ok2 := GetFieldValue(elemMap, dateField); ok2 {
					if dateStr, ok3 := dateVal.(string); ok3 {
						n := paramInt(ca.Params, "n", 6)
						unit := paramString(ca.Params, "unit")
						if unit == "" {
							unit = "month"
						}
						refDate := paramString(ca.Params, "ref_date")
						if !IsWithinPeriod(dateStr, n, refDate, unit) {
							continue
						}
					}
				}
			}
			if v, ok2 := GetFieldValue(elemMap, relField); ok2 {
				vals = append(vals, v)
			}
		}
		if len(vals) == 0 {
			if z, ok := zeroForOp(ca.Op); ok {
				return z
			}
			return nil
		}
		return ComputeAggregation(vals, ca.Op, ca.Params)
	}

	// Intermediate level: paths inside elements are relative to this array level.
	subAncestors := make([]string, len(ca.arrayAncestors)-depth-1)
	for i, a := range ca.arrayAncestors[depth+1:] {
		subAncestors[i] = strings.TrimPrefix(a, outerPath+".")
	}
	subField := strings.TrimPrefix(ca.Field, outerPath+".")
	subCA := aggClassified{
		AggConfig:      AggConfig{Field: subField, Op: ca.Op, Alias: ca.Alias, Params: ca.Params},
		level:          levelNested,
		arrayAncestors: subAncestors,
	}

	var perElem []interface{}
	for _, elem := range arr {
		elemMap, ok := elem.(map[string]interface{})
		if !ok {
			continue
		}
		v := aggregateNestedField(elemMap, subCA, 0)
		if v != nil {
			perElem = append(perElem, v)
		}
	}
	if len(perElem) == 0 {
		if z, ok := zeroForOp(ca.Op); ok {
			return z
		}
		return nil
	}
	return ComputeAggregation(perElem, CrossRecordOp(ca.Op), ca.Params)
}
