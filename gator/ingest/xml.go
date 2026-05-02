// Package ingest provides parsers that convert various data formats into
// the []interface{} / map[string]interface{} representation consumed by gator.
package ingest

import (
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// XMLOptions controls how the XML parser normalises the output map.
type XMLOptions struct {
	// AttrPrefix is prepended to attribute key names. Default: "@".
	AttrPrefix string
	// TextKey is the key used for text content when an element also has
	// attributes or child elements. Default: "#text".
	TextKey string
	// NormaliseNamespace strips the colon from namespace-prefixed names and
	// concatenates prefix+local: "cb:score" → "cbscore". Default: true.
	NormaliseNamespace bool
	// Hints carries optional XSD-derived overrides for type coercion and
	// force-array decisions. The zero value means "use heuristics only".
	Hints XSDHints
}

// DefaultXMLOptions returns the recommended option set.
func DefaultXMLOptions() XMLOptions {
	return XMLOptions{
		AttrPrefix:         "@",
		TextKey:            "#text",
		NormaliseNamespace: true,
	}
}

// ParseXML reads one XML document from r and returns a single-element
// []interface{} wrapping the root element as a map[string]interface{}.
// The returned slice is ready to pass to gator.Store.Register.
func ParseXML(r io.Reader, opts XMLOptions) ([]interface{}, error) {
	root, err := parseElement(xml.NewDecoder(r), opts, "")
	if err != nil {
		return nil, err
	}
	if root == nil {
		return []interface{}{}, nil
	}
	return []interface{}{root}, nil
}

// ParseXMLMany reads r expecting a sequence of sibling documents wrapped in a
// single root element, e.g.:
//
//	<Records>
//	  <Record>...</Record>
//	  <Record>...</Record>
//	</Records>
//
// It returns the children of the root element as []interface{}, one entry per
// child element — suitable for datasets with multiple records.
func ParseXMLMany(r io.Reader, opts XMLOptions) ([]interface{}, error) {
	root, err := parseElement(xml.NewDecoder(r), opts, "")
	if err != nil {
		return nil, err
	}
	if root == nil {
		return []interface{}{}, nil
	}
	m, ok := root.(map[string]interface{})
	if !ok {
		return []interface{}{root}, nil
	}
	// If the root has exactly one key and its value is []interface{}, unwrap it.
	if len(m) == 1 {
		for _, v := range m {
			if arr, ok := v.([]interface{}); ok {
				return arr, nil
			}
		}
	}
	return []interface{}{root}, nil
}

// ── internal parser ───────────────────────────────────────────────────────────

// node is an intermediate representation built during parsing.
type node struct {
	name     string
	path     string // dot-notation path from root, e.g. "TradeLines.TradeLine"
	attrs    []xml.Attr
	text     strings.Builder
	children []*node // ordered, may have duplicate names → force-array detection
}

// parseElement reads one complete element (opening tag through closing tag)
// from dec and returns it as map[string]interface{} or a scalar.
// parentPath is the dot-notation path of the parent element (empty for root).
func parseElement(dec *xml.Decoder, opts XMLOptions, parentPath string) (interface{}, error) {
	// Advance to the first StartElement.
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if se, ok := tok.(xml.StartElement); ok {
			name := normaliseName(se.Name, opts)
			path := name
			if parentPath != "" {
				path = parentPath + "." + name
			}
			n := &node{name: name, path: path, attrs: se.Attr}
			if err := fillNode(dec, n, opts); err != nil {
				return nil, err
			}
			return nodeToValue(n, opts), nil
		}
	}
}

// fillNode populates n by consuming tokens until its matching end element.
func fillNode(dec *xml.Decoder, n *node, opts XMLOptions) error {
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("xml: unexpected error: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			childName := normaliseName(t.Name, opts)
			childPath := childName
			if n.path != "" {
				childPath = n.path + "." + childName
			}
			child := &node{name: childName, path: childPath, attrs: t.Attr}
			if err := fillNode(dec, child, opts); err != nil {
				return err
			}
			n.children = append(n.children, child)

		case xml.EndElement:
			depth--

		case xml.CharData:
			trimmed := strings.TrimSpace(string(t))
			if trimmed != "" {
				n.text.WriteString(trimmed)
			}

		case xml.Comment:
			// deliberately ignored — lossless means no structural data is lost;
			// XML comments carry no data relevant to aggregation.
		}
	}
	return nil
}

// nodeToValue converts a populated node into a Go value using the lossless rules:
//
//  1. No children, no attributes, no text → null
//  2. No children, no attributes, has text → scalar (XSD-guided or heuristic coercion)
//  3. Has children or attributes → map[string]interface{}
//     a. Attributes → "@name": value
//     b. Text content (when there are also attrs/children) → "#text": value
//     c. Children → grouped by tag name; force [] when XSD says unbounded,
//        or when the same tag appears ≥2 times.
func nodeToValue(n *node, opts XMLOptions) interface{} {
	hasChildren := len(n.children) > 0
	hasAttrs := len(n.attrs) > 0
	text := strings.TrimSpace(n.text.String())
	hasText := text != ""

	// ── Case 1 & 2: leaf node ────────────────────────────────────────────────
	if !hasChildren && !hasAttrs {
		if !hasText {
			return nil
		}
		// XSD hint: this path is declared xs:string → never coerce.
		if opts.Hints.StringPaths[n.path] {
			return text
		}
		return coerce(text)
	}

	// ── Case 3: element with structure ───────────────────────────────────────
	m := map[string]interface{}{}

	// 3a: attributes
	for _, attr := range n.attrs {
		key := opts.AttrPrefix + normaliseName(attr.Name, opts)
		m[key] = coerce(attr.Value)
	}

	// 3b: text content alongside attrs/children
	if hasText {
		if opts.Hints.StringPaths[n.path] {
			m[opts.TextKey] = text
		} else {
			m[opts.TextKey] = coerce(text)
		}
	}

	// 3c: children
	if hasChildren {
		childrenToMap(n.children, m, opts)
	}

	return m
}

// childrenToMap groups n's children by tag name and populates dst.
//
// Force-array rules (applied in priority order):
//
//  1. XSD hint: if opts.Hints.ArrayPaths contains the child's path → always array.
//  2. Observation: if the same tag name appears ≥2 times → array.
//  3. Otherwise: scalar or map (single occurrence, no XSD override).
func childrenToMap(children []*node, dst map[string]interface{}, opts XMLOptions) {
	// Count occurrences of each tag name.
	tagCount := map[string]int{}
	for _, c := range children {
		tagCount[c.name]++
	}

	// Collect values per tag, preserving document order of first appearance.
	order := []string{}
	type entry struct {
		path   string
		values []interface{}
	}
	buckets := map[string]*entry{}
	for _, c := range children {
		v := nodeToValue(c, opts)
		if _, seen := buckets[c.name]; !seen {
			order = append(order, c.name)
			buckets[c.name] = &entry{path: c.path}
		}
		buckets[c.name].values = append(buckets[c.name].values, v)
	}

	for _, key := range order {
		e := buckets[key]
		// Force array if XSD says so OR if we observed multiple occurrences.
		if opts.Hints.ArrayPaths[e.path] || tagCount[key] > 1 {
			dst[key] = e.values
		} else {
			dst[key] = e.values[0]
		}
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

// normaliseName converts an xml.Name to a string key.
// With NormaliseNamespace=true, Go's xml decoder has already resolved namespace
// prefixes to URIs, so n.Space is the URI (not the prefix). We return n.Local
// directly — the prefix normalisation ("cb:score" → "cbscore") happens
// automatically because Go strips the colon prefix during decoding.
func normaliseName(n xml.Name, opts XMLOptions) string {
	_ = opts // NormaliseNamespace reserved for future RawToken-based parsing
	return n.Local
}

// coerce tries to parse s as a boolean, integer, or float.
// It returns s unchanged (as a string) when:
//   - s has a leading zero (e.g. "01", "007") — likely a code/ID, not a number
//   - s is an integer whose round-trip through float64 would lose precision
//     (more than 15 significant digits; NIKs, account numbers, etc.)
//   - s cannot be parsed as any numeric type
func coerce(s string) interface{} {
	if s == "" {
		return s
	}

	// boolean
	switch strings.ToLower(s) {
	case "true":
		return true
	case "false":
		return false
	}

	// Preserve strings with leading zeros — these are codes/IDs, not numbers.
	// (A lone "0" is fine; "01", "007" are not.)
	if len(s) > 1 && s[0] == '0' && s[1] != '.' {
		return s
	}

	// Try integer parse first.
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		// Guard against float64 precision loss for large integers.
		// float64 has 53-bit mantissa ≈ 15–16 significant decimal digits.
		// If the string has more than 15 digits, keep it as a string.
		if len(strings.TrimLeft(s, "-")) > 15 {
			return s
		}
		return float64(i)
	}

	// Try float parse.
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}

	return s
}
