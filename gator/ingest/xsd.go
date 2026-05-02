package ingest

import (
	"encoding/xml"
	"io"
	"strings"
)

// XSDHints holds the type and cardinality constraints extracted from an XSD.
// It is used by the XML parser to override heuristic coercion and force-array
// decisions on a per-element basis.
//
// The zero value (empty maps) is safe: the parser falls back to heuristics
// for any element not covered by the hints.
type XSDHints struct {
	// StringPaths is the set of dot-notation element paths whose XSD type
	// mandates string treatment. Coercion to number/bool is suppressed.
	// Examples: "ZipCode", "TradeLines.TradeLine.AccountNumber"
	StringPaths map[string]bool

	// ArrayPaths is the set of dot-notation element paths that should always
	// be treated as []interface{}, even when only a single child appears.
	// Derived from maxOccurs="unbounded" or maxOccurs > 1 in the XSD.
	// Examples: "TradeLines.TradeLine", "Scores.Score"
	ArrayPaths map[string]bool
}

// ParseXSD reads an XSD document from r and extracts type and cardinality hints
// for use with ParseXMLWithHints.
//
// It handles the most common XSD constructs:
//   - xs:element with type="xs:string" / "xs:integer" / "xs:decimal" / etc.
//   - xs:element with maxOccurs="unbounded" or maxOccurs > 1
//   - Nested xs:complexType / xs:sequence / xs:choice
//
// Unknown constructs are silently ignored, making the parser robust against
// exotic XSD features. The caller is free to augment the returned XSDHints
// with manually specified paths before passing it to ParseXMLWithHints.
func ParseXSD(r io.Reader) (XSDHints, error) {
	hints := XSDHints{
		StringPaths: map[string]bool{},
		ArrayPaths:  map[string]bool{},
	}

	dec := xml.NewDecoder(r)

	// stack holds element names only (structural nodes are NOT pushed).
	// currentPath is rebuilt by joining the stack with ".".
	var stack []string

	currentPath := func() string {
		var parts []string
		for _, s := range stack {
			if s != "\x00" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ".")
	}

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return hints, err
		}

		switch t := tok.(type) {
		case xml.StartElement:
			localName := t.Name.Local

			if localName == "element" {
				name := attrVal(t.Attr, "name")
				typAttr := attrVal(t.Attr, "type")
				maxOccurs := attrVal(t.Attr, "maxOccurs")
				ref := attrVal(t.Attr, "ref")

				if name == "" && ref != "" {
					if idx := strings.LastIndex(ref, ":"); idx >= 0 {
						name = ref[idx+1:]
					} else {
						name = ref
					}
				}

				if name != "" {
					stack = append(stack, name)
					path := currentPath()

					if maxOccurs == "unbounded" || (maxOccurs != "" && maxOccurs != "0" && maxOccurs != "1") {
						hints.ArrayPaths[path] = true
					}
					if isStringXSDType(typAttr) {
						hints.StringPaths[path] = true
					}
				} else {
					// anonymous inline element with no name — push sentinel to balance EndElement
					stack = append(stack, "\x00")
				}

			} else if isXSDStructural(localName) {
				// Structural wrapper — push sentinel so EndElement can pop symmetrically
				// without adding a segment to the path.
				stack = append(stack, "\x00")
			}

		case xml.EndElement:
			localName := t.Name.Local
			if localName == "element" || isXSDStructural(localName) {
				if len(stack) > 0 {
					stack = stack[:len(stack)-1]
				}
			}
		}
	}

	return hints, nil
}

// cleanStack returns stack with sentinel values removed, for path building.
// (Not needed with the current approach but kept for clarity.)
func cleanStack(s []string) []string {
	out := s[:0:len(s)]
	for _, v := range s {
		if v != "\x00" {
			out = append(out, v)
		}
	}
	return out
}

// isStringXSDType returns true for XSD built-in types that should not be
// coerced to a Go number.
func isStringXSDType(t string) bool {
	// Strip namespace prefix (xs:string → string, xsd:string → string).
	if idx := strings.LastIndex(t, ":"); idx >= 0 {
		t = t[idx+1:]
	}
	switch strings.ToLower(t) {
	case "string", "normalizedstring", "token", "language", "name",
		"ncname", "id", "idref", "idrefs", "anyuri",
		"date", "time", "datetime", "gyearmonth", "gyear", "gmonthday",
		"gday", "gmonth", "duration", "hexbinary", "base64binary":
		return true
	}
	return false
}

// isXSDStructural returns true for XSD elements that define structure but
// should not add a segment to the element path.
func isXSDStructural(name string) bool {
	switch name {
	case "complexType", "simpleType", "sequence", "choice", "all",
		"group", "attributeGroup", "annotation", "documentation",
		"restriction", "extension", "complexContent", "simpleContent":
		return true
	}
	return false
}

// attrVal returns the value of the named attribute, or "" if absent.
func attrVal(attrs []xml.Attr, name string) string {
	for _, a := range attrs {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

// MergeXSDHints returns a new XSDHints merging a and b (b takes precedence
// on conflicts). Useful for combining parsed hints with manual overrides.
func MergeXSDHints(a, b XSDHints) XSDHints {
	out := XSDHints{
		StringPaths: map[string]bool{},
		ArrayPaths:  map[string]bool{},
	}
	for k, v := range a.StringPaths {
		out.StringPaths[k] = v
	}
	for k, v := range b.StringPaths {
		out.StringPaths[k] = v
	}
	for k, v := range a.ArrayPaths {
		out.ArrayPaths[k] = v
	}
	for k, v := range b.ArrayPaths {
		out.ArrayPaths[k] = v
	}
	return out
}
