// Package gator provides a generic JSON/XML aggregation engine.
// It supports SQL-like operations (GROUP BY, WHERE, SUM, MAX, MIN, AVG, COUNT,
// and time-window ops) over arbitrary nested structures — including nested arrays
// up to any depth — without requiring a fixed schema.
package gator

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
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
	// FilterMode controls WHERE auto-classification:
	//   "auto" (default) — WHERE conditions on array fields are automatically
	//                      routed to localFilter; parent fields stay in WHERE.
	//   "manual"         — WHERE and localFilter used exactly as sent.
	FilterMode string `json:"filterMode"`
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
// All methods are safe for concurrent use.
// PERF-01 (schema cache) and PERF-02 (path cache) are embedded transparently.
type Store struct {
	mu       sync.RWMutex
	datasets map[string][]interface{}
	storeCacheFields // transparent PERF-01 + PERF-02 caches
}

// NewStore creates an empty Store with all optimisations active.
func NewStore() *Store {
	return &Store{
		datasets:         map[string][]interface{}{},
		storeCacheFields: newStoreCacheFields(),
	}
}

// Register adds or replaces a named dataset and invalidates its schema cache.
func (s *Store) Register(name string, data []interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.datasets[name] = data
	// PERF-01: invalidate so next Aggregate recomputes schema.
	s.schemaMu.Lock()
	delete(s.schemas, name)
	s.schemaMu.Unlock()
}

// Get retrieves a dataset by name.
func (s *Store) Get(name string) ([]interface{}, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.datasets[name]
	return d, ok
}

// Names returns the sorted list of registered dataset names.
func (s *Store) Names() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
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

// explodeRecords expands each parent record by the array at arrayPath into
// one flat row per array element.
//
// The array may be at any nesting depth (e.g. "credits" or
// "FasilitasList.Fasilitas").  For each parent record a full deep copy is made,
// the array element is embedded under the array's own key inside its parent
// map, and that single element replaces the full array.  GetFieldValue still
// resolves dot-notation paths correctly because the nested structure is
// preserved — only the target array is replaced by its element.
//
// Records whose array is absent or empty produce exactly one ghost row where
// the array key is set to nil, preserving the guarantee that every parent
// record appears in at least one output row.
func explodeRecords(records []map[string]interface{}, arrayPath string) []map[string]interface{} {
	parts := strings.Split(arrayPath, ".")

	var result []map[string]interface{}
	for _, rec := range records {
		// Navigate on the original to find the array (read-only peek).
		parentMap, key, found := NavigateToParent(rec, parts)
		if !found {
			flat := DeepCopyMap(rec)
			// Null-out the leaf key so ghost rows are clean.
			if pm, k, ok := navigateToParentInCopy(flat, parts); ok {
				pm[k] = nil
			}
			_ = parentMap // silence unused warning
			result = append(result, flat)
			continue
		}

		arr, ok := parentMap[key].([]interface{})
		if !ok || len(arr) == 0 {
			flat := DeepCopyMap(rec)
			if pm, k, ok2 := navigateToParentInCopy(flat, parts); ok2 {
				pm[k] = nil
			}
			result = append(result, flat)
			continue
		}

		for _, elem := range arr {
			// Full deep copy per element — each flat row is independent.
			flat := DeepCopyMap(rec)
			pm, k, _ := navigateToParentInCopy(flat, parts)
			if elemMap, ok2 := elem.(map[string]interface{}); ok2 {
				pm[k] = DeepCopyMap(elemMap)
			} else {
				pm[k] = elem
			}
			result = append(result, flat)
		}
	}
	return result
}

// navigateToParentInCopy is NavigateToParent adapted for the 3-return signature
// used inside explodeRecords so we can inline the ok check.
func navigateToParentInCopy(m map[string]interface{}, parts []string) (map[string]interface{}, string, bool) {
	return NavigateToParent(m, parts)
}

// shallowCopyMapExcept returns a shallow copy of m omitting the given key.
// Kept for reference; explodeRecords now uses DeepCopyMap instead.
func shallowCopyMapExcept(m map[string]interface{}, except string) map[string]interface{} {
	cp := make(map[string]interface{}, len(m))
	for k, v := range m {
		if k != except {
			cp[k] = v
		}
	}
	return cp
}

// finaliseAggValue converts internal avgAccum carriers to their final float64
// value before placing the result in an output row. All other types pass through.
func finaliseAggValue(v interface{}) interface{} {
	if acc, ok := v.(avgAccum); ok {
		if acc.Count == 0 {
			return nil
		}
		return acc.Sum / float64(acc.Count)
	}
	return v
}

// ===================== OrderedMap =====================

// OrderedMap is a map that preserves insertion order when marshalled to JSON.
// This ensures result columns appear in the expected order: GROUP BY fields
// first, followed by aggregations in DSL order — rather than alphabetical.
type OrderedMap struct {
	Keys   []string
	Values map[string]interface{}
}

func newOrderedMap() OrderedMap {
	return OrderedMap{Keys: []string{}, Values: map[string]interface{}{}}
}

// set adds or updates a key, maintaining insertion order for new keys.
func (om *OrderedMap) set(key string, val interface{}) {
	if _, exists := om.Values[key]; !exists {
		om.Keys = append(om.Keys, key)
	}
	om.Values[key] = val
}

// MarshalJSON writes keys in insertion order.
func (om OrderedMap) MarshalJSON() ([]byte, error) {
	var buf []byte
	buf = append(buf, '{')
	for i, key := range om.Keys {
		if i > 0 {
			buf = append(buf, ',')
		}
		kb, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		buf = append(buf, kb...)
		buf = append(buf, ':')
		vb, err := json.Marshal(om.Values[key])
		if err != nil {
			return nil, err
		}
		buf = append(buf, vb...)
	}
	buf = append(buf, '}')
	return buf, nil
}

// RowValues extracts the Values map from a result row, whether it is an
// OrderedMap (standard output) or a plain map[string]interface{} (legacy).
// Useful in tests and when processing results programmatically.
func RowValues(row interface{}) map[string]interface{} {
	if om, ok := row.(OrderedMap); ok {
		return om.Values
	}
	if m, ok := row.(map[string]interface{}); ok {
		return m
	}
	return map[string]interface{}{}
}

// ===================== Auto-classify WHERE =====================

// classifyWhereConditions splits a WHERE map into localFilter conditions
// (fields that live inside a named array) and global WHERE conditions
// (fields at the parent record level), based on the schema.
//
// This lets users send all conditions in "where" without having to understand
// the localFilter/where distinction. mode "auto" enables this; "manual" skips it.
func classifyWhereConditions(
	where map[string]map[string]interface{},
	schema []FieldInfo,
) (localFilter map[string]map[string]map[string]interface{}, globalWhere map[string]map[string]interface{}) {
	lookup := map[string]FieldInfo{}
	for _, f := range schema {
		lookup[f.Path] = f
	}

	localFilter = map[string]map[string]map[string]interface{}{}
	globalWhere = map[string]map[string]interface{}{}

	for field, ops := range where {
		info, ok := lookup[field]
		if ok && info.ArrayPath != "" {
			arrPath := info.ArrayPath
			if localFilter[arrPath] == nil {
				localFilter[arrPath] = map[string]map[string]interface{}{}
			}
			localFilter[arrPath][field] = ops
		} else {
			globalWhere[field] = ops
		}
	}
	return
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

// AggregateResult wraps the output of Aggregate.
type AggregateResult struct {
	Rows     []interface{} `json:"rows"`
	Warnings []string      `json:"warnings,omitempty"`
}

// ===================== Field validation =====================

// nonAggregableTypes are schema types that cannot be meaningfully aggregated
// as a scalar. If a user specifies such a field directly, we return an error.
var nonAggregableTypes = map[string]bool{
	"array_object": true,
	// array_number/string/primitive are valid for window ops — allowed
}

// validateRequest checks aggregation field paths against the schema.
// Returns an error for any field that resolves to a non-aggregable type
// (e.g. an array of objects), which would silently produce incorrect results.
func validateRequest(req AggregateRequest, schema []FieldInfo) error {
	lookup := map[string]FieldInfo{}
	for _, f := range schema {
		lookup[f.Path] = f
	}
	for _, agg := range req.Aggregations {
		if fi, ok := lookup[agg.Field]; ok {
			if nonAggregableTypes[fi.Type] {
				return fmt.Errorf(
					"aggregation field %q resolves to type %q — specify a sub-field (e.g. %q.someField)",
					agg.Field, fi.Type, agg.Field,
				)
			}
		}
	}
	return nil
}

// ===================== Main entry point =====================

// Aggregate executes the DSL request against store and returns result rows.
//
// All optimisations run automatically based on query shape:
//   - PERF-01: schema from per-Store cache (computed once, invalidated on Register)
//   - PERF-02: dot-notation paths cached in Store (strings.Split called once per path)
//   - PERF-03: COW explode auto-selected when GROUP BY contains an array-level field
//   - PERF-04: lazy filter auto-selected when localFilter is present
//
// FilterMode:
//   - "auto" (default): WHERE conditions on array fields are automatically routed
//     to localFilter; parent fields stay in global WHERE.
//   - "manual": WHERE and localFilter used exactly as sent.
func Aggregate(store *Store, req AggregateRequest) ([]interface{}, error) {
	var data []interface{}
	if len(req.Data) > 0 {
		data = req.Data
	} else if ds, ok := store.Get(req.Dataset); ok {
		data = ds
	} else {
		return nil, fmt.Errorf("dataset %q not found", req.Dataset)
	}
	if len(data) == 0 {
		return []interface{}{}, nil
	}

	// PERF-01: schema from cache.
	schema := store.schema(req.Dataset)
	if schema == nil {
		schema = DetectSchemaFromSample(data)
	}

	if err := validateRequest(req, schema); err != nil {
		return nil, err
	}

	// Resolve filter mode and auto-classify WHERE if requested.
	localFilter := req.LocalFilter
	globalWhere := req.Where
	if req.FilterMode != "manual" && len(req.Where) > 0 {
		// "auto" (default): route array-field conditions to localFilter.
		autoLocal, autoGlobal := classifyWhereConditions(req.Where, schema)
		// Merge auto-classified localFilter with any explicit localFilter.
		merged := map[string]map[string]map[string]interface{}{}
		for k, v := range autoLocal {
			merged[k] = v
		}
		for arrPath, conds := range req.LocalFilter {
			if merged[arrPath] == nil {
				merged[arrPath] = conds
			} else {
				for field, ops := range conds {
					merged[arrPath][field] = ops
				}
			}
		}
		localFilter = merged
		globalWhere = autoGlobal
	}

	// PERF-04: lazy copy — only deep-copy records that have the target array.
	if len(localFilter) > 0 {
		data = applyLocalFiltersLazy(data, localFilter, store)
	}

	var filtered []map[string]interface{}
	for _, record := range data {
		if m, ok := record.(map[string]interface{}); ok {
			if EvaluateWhere(m, globalWhere) {
				filtered = append(filtered, m)
			}
		}
	}

	_, explodePath := classifyGroupBy(req.GroupBy, schema)

	if explodePath != "" {
		// PERF-03: COW explode auto-selected for all explode-mode queries.
		return aggregateExplodedTransparent(filtered, req, explodePath, store), nil
	}

	classified := classifyAggregations(req.Aggregations, schema)
	return aggregateStandard(filtered, req, classified), nil
}

// aggregateStandard is the two-pass aggregation for parent-level GROUP BY.
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
		row := newOrderedMap()

		if key != "__all__" && len(records) > 0 {
			for _, gb := range req.GroupBy {
				if val, ok := GetFieldValue(records[0], gb); ok {
					row.set(gb, val)
				} else {
					row.set(gb, nil)
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
			row.set(alias, finaliseAggValue(val))
		}

		results = append(results, row)
	}
	return results
}

// aggregateExploded handles queries where GROUP BY contains array-level fields.
//
// After exploding records into flat rows, aggregations are split into two sets:
//  - arrayAggs: fields whose path starts with explodePath+"." — they live inside
//    the exploded array and are aggregated across all flat rows in the group.
//  - parentAggs: all other fields — they live on the parent record and are
//    duplicated across flat rows.  These are deduplicated before aggregation
//    by collecting one value per distinct parent (identified by the values of
//    all parent-level GROUP BY fields).
// aggregateExplodedTransparent wraps aggregateExploded using COW explode (PERF-03)
// and is called automatically by Aggregate when explode mode is detected.
func aggregateExplodedTransparent(filtered []map[string]interface{}, req AggregateRequest, explodePath string, store *Store) []interface{} {
	// Use COW explode instead of DeepCopyMap — 8.8× faster, 12.7× less memory.
	flatRows := explodeRecordsCOW(filtered, explodePath, store)

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

	var parentGBFields []string
	for _, gb := range req.GroupBy {
		if !strings.HasPrefix(gb, explodePath+".") {
			parentGBFields = append(parentGBFields, gb)
		}
	}

	results := make([]interface{}, 0, len(groupOrder))
	for _, key := range groupOrder {
		rows := groups[key]
		row := newOrderedMap()
		if key != "__all__" && len(rows) > 0 {
			for _, gb := range req.GroupBy {
				if val, ok := GetFieldValue(rows[0], gb); ok {
					row.set(gb, val)
				} else {
					row.set(gb, nil)
				}
			}
		}
		for _, agg := range req.Aggregations {
			alias := agg.Alias
			if alias == "" {
				alias = agg.Op + "_" + strings.ReplaceAll(agg.Field, ".", "_")
			}
			isArrayField := strings.HasPrefix(agg.Field, explodePath+".")
			var vals []interface{}
			if isArrayField {
				for _, r := range rows {
					if v, ok := GetFieldValue(r, agg.Field); ok && v != nil {
						vals = append(vals, v)
					}
				}
			} else {
				seen := map[string]bool{}
				for _, r := range rows {
					pk := buildGroupKey(r, parentGBFields)
					if seen[pk] {
						continue
					}
					seen[pk] = true
					if v, ok := GetFieldValue(r, agg.Field); ok && v != nil {
						vals = append(vals, v)
					}
				}
			}
			val := ComputeAggregation(vals, agg.Op, agg.Params)
			if val == nil {
				if z, ok := zeroForOp(agg.Op); ok {
					val = z
				}
			}
			row.set(alias, finaliseAggValue(val))
		}
		results = append(results, row)
	}
	return results
}

func aggregateExploded(filtered []map[string]interface{}, req AggregateRequest, schema []FieldInfo, explodePath string) []interface{} {
	flatRows := explodeRecords(filtered, explodePath)

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

	// Identify parent-level GROUP BY fields (used to build a dedup key per parent).
	var parentGBFields []string
	for _, gb := range req.GroupBy {
		if !strings.HasPrefix(gb, explodePath+".") {
			parentGBFields = append(parentGBFields, gb)
		}
	}

	results := make([]interface{}, 0, len(groupOrder))
	for _, key := range groupOrder {
		rows := groups[key]
		row := newOrderedMap()

		if key != "__all__" && len(rows) > 0 {
			for _, gb := range req.GroupBy {
				if val, ok := GetFieldValue(rows[0], gb); ok {
					row.set(gb, val)
				} else {
					row.set(gb, nil)
				}
			}
		}

		for _, agg := range req.Aggregations {
			alias := agg.Alias
			if alias == "" {
				alias = agg.Op + "_" + strings.ReplaceAll(agg.Field, ".", "_")
			}

			isArrayField := strings.HasPrefix(agg.Field, explodePath+".")

			var vals []interface{}
			if isArrayField {
				// Array-level field: aggregate across all flat rows.
				for _, r := range rows {
					if v, ok := GetFieldValue(r, agg.Field); ok && v != nil {
						vals = append(vals, v)
					}
				}
			} else {
				// Parent-level field: deduplicate by parent identity before aggregating.
				// Two flat rows belong to the same parent if they share identical values
				// for all parent-level GROUP BY fields.
				seen := map[string]bool{}
				for _, r := range rows {
					parentKey := buildGroupKey(r, parentGBFields)
					if seen[parentKey] {
						continue
					}
					seen[parentKey] = true
					if v, ok := GetFieldValue(r, agg.Field); ok && v != nil {
						vals = append(vals, v)
					}
				}
			}

			val := ComputeAggregation(vals, agg.Op, agg.Params)
			if val == nil {
				if z, ok := zeroForOp(agg.Op); ok {
					val = z
				}
			}
			row.set(alias, finaliseAggValue(val))
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
