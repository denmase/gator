// Package gator provides a generic JSON aggregation engine.
// It supports SQL-like operations (GROUP BY, WHERE, SUM, MAX, MIN, AVG, COUNT, etc.)
// over arbitrary nested JSON structures, including nested arrays, without requiring
// a fixed schema.
package gator

import (
	"sort"
	"strings"
)

// FieldInfo describes one field discovered by schema detection.
type FieldInfo struct {
	// Path is the full dot-notation path from the root, e.g. "credits.open_date".
	Path string `json:"path"`
	// Type is the JSON type: "number", "string", "boolean", "null",
	// "array_object", "array_number", "array_string", "array_primitive", "array_empty".
	Type string `json:"type"`
	// ArrayPath is the dot-notation path of the nearest ancestor array.
	// Empty string means the field lives at the root (not inside any array).
	ArrayPath string `json:"arrayPath,omitempty"`
}

// DetectSchema inspects the first record of a dataset and returns a flat list
// of FieldInfo entries, one per leaf or array node.  It recurses into nested
// objects and marks fields inside arrays with their ancestor ArrayPath so the
// engine can decide whether to apply parent-level or array-level aggregation.
func DetectSchema(data interface{}, prefix string, currentArrayPath string) []FieldInfo {
	var fields []FieldInfo
	switch v := data.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			p := k
			if prefix != "" {
				p = prefix + "." + k
			}
			fields = append(fields, DetectSchema(v[k], p, currentArrayPath)...)
		}

	case []interface{}:
		if len(v) == 0 {
			fields = append(fields, FieldInfo{Path: prefix, Type: "array_empty", ArrayPath: currentArrayPath})
		} else {
			switch v[0].(type) {
			case map[string]interface{}:
				// Array of objects: record the array node itself, then recurse into
				// the first element with the array's own path as the new ArrayPath.
				fields = append(fields, FieldInfo{Path: prefix, Type: "array_object", ArrayPath: currentArrayPath})
				fields = append(fields, DetectSchema(v[0], prefix, prefix)...)
			default:
				typ := "array_primitive"
				switch v[0].(type) {
				case float64:
					typ = "array_number"
				case string:
					typ = "array_string"
				}
				fields = append(fields, FieldInfo{Path: prefix, Type: typ, ArrayPath: currentArrayPath})
			}
		}

	case float64:
		fields = append(fields, FieldInfo{Path: prefix, Type: "number", ArrayPath: currentArrayPath})
	case string:
		fields = append(fields, FieldInfo{Path: prefix, Type: "string", ArrayPath: currentArrayPath})
	case bool:
		fields = append(fields, FieldInfo{Path: prefix, Type: "boolean", ArrayPath: currentArrayPath})
	case nil:
		fields = append(fields, FieldInfo{Path: prefix, Type: "null", ArrayPath: currentArrayPath})
	}
	return fields
}

// GetFieldValue resolves a dot-notation path against a record map.
// It first tries true nested traversal (obj["a"]["b"]), then falls back to a
// literal key lookup (obj["a.b"]) for already-flattened rows.
func GetFieldValue(obj map[string]interface{}, path string) (interface{}, bool) {
	parts := strings.Split(path, ".")
	current := interface{}(obj)
	for _, part := range parts {
		m, ok := current.(map[string]interface{})
		if !ok {
			break
		}
		current, ok = m[part]
		if !ok {
			if v, ok2 := obj[path]; ok2 {
				return v, true
			}
			return nil, false
		}
	}
	return current, true
}

// NavigateToParent walks the parts slice through nested maps and returns
// (parentMap, finalKey, found).  Used to locate and mutate nested arrays.
func NavigateToParent(m map[string]interface{}, parts []string) (map[string]interface{}, string, bool) {
	if len(parts) == 0 {
		return nil, "", false
	}
	if len(parts) == 1 {
		return m, parts[0], true
	}
	current := m
	for i := 0; i < len(parts)-1; i++ {
		next, ok := current[parts[i]].(map[string]interface{})
		if !ok {
			return nil, "", false
		}
		current = next
	}
	return current, parts[len(parts)-1], true
}

// DeepCopyMap returns a deep copy of a map[string]interface{}, recursing into
// nested maps and slices so that mutations to the copy do not affect the original.
func DeepCopyMap(m map[string]interface{}) map[string]interface{} {
	cp := make(map[string]interface{}, len(m))
	for k, v := range m {
		switch val := v.(type) {
		case map[string]interface{}:
			cp[k] = DeepCopyMap(val)
		case []interface{}:
			arr := make([]interface{}, len(val))
			for i, item := range val {
				if sub, ok := item.(map[string]interface{}); ok {
					arr[i] = DeepCopyMap(sub)
				} else {
					arr[i] = item
				}
			}
			cp[k] = arr
		default:
			cp[k] = v
		}
	}
	return cp
}
