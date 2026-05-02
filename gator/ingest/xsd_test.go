package ingest_test

import (
	"strings"
	"testing"

	"aggregator/gator/ingest"
)

const sampleXSD = `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="Response">
    <xs:complexType>
      <xs:sequence>
        <xs:element name="Product">
          <xs:complexType>
            <xs:sequence>
              <xs:element name="TradeLines">
                <xs:complexType>
                  <xs:sequence>
                    <xs:element name="TradeLine" maxOccurs="unbounded">
                      <xs:complexType>
                        <xs:sequence>
                          <xs:element name="AccountNumber" type="xs:string"/>
                          <xs:element name="Balance"       type="xs:decimal"/>
                          <xs:element name="ZipCode"       type="xs:string"/>
                        </xs:sequence>
                      </xs:complexType>
                    </xs:element>
                  </xs:sequence>
                </xs:complexType>
              </xs:element>
              <xs:element name="Scores">
                <xs:complexType>
                  <xs:sequence>
                    <xs:element name="Score" maxOccurs="unbounded">
                      <xs:complexType>
                        <xs:sequence>
                          <xs:element name="Value" type="xs:integer"/>
                          <xs:element name="Code"  type="xs:string"/>
                        </xs:sequence>
                      </xs:complexType>
                    </xs:element>
                  </xs:sequence>
                </xs:complexType>
              </xs:element>
            </xs:sequence>
          </xs:complexType>
        </xs:element>
      </xs:sequence>
    </xs:complexType>
  </xs:element>
</xs:schema>`

func TestParseXSD(t *testing.T) {
	hints, err := ingest.ParseXSD(strings.NewReader(sampleXSD))
	if err != nil {
		t.Fatalf("ParseXSD: %v", err)
	}

	// Force-array paths
	wantArrays := []string{
		"Response.Product.TradeLines.TradeLine",
		"Response.Product.Scores.Score",
	}
	for _, p := range wantArrays {
		if !hints.ArrayPaths[p] {
			t.Errorf("✗ ArrayPaths missing: %q", p)
		} else {
			t.Logf("✓ ArrayPaths: %q", p)
		}
	}

	// String paths
	wantStrings := []string{
		"Response.Product.TradeLines.TradeLine.AccountNumber",
		"Response.Product.TradeLines.TradeLine.ZipCode",
		"Response.Product.Scores.Score.Code",
	}
	for _, p := range wantStrings {
		if !hints.StringPaths[p] {
			t.Errorf("✗ StringPaths missing: %q", p)
		} else {
			t.Logf("✓ StringPaths: %q", p)
		}
	}

	// Numeric type → NOT in StringPaths
	numericPath := "Response.Product.TradeLines.TradeLine.Balance"
	if hints.StringPaths[numericPath] {
		t.Errorf("✗ Balance (xs:decimal) should NOT be in StringPaths")
	} else {
		t.Logf("✓ Balance not in StringPaths (numeric)")
	}
}

// TestXSDHintsForceArray verifies that a single-child element is forced to
// []interface{} when the XSD says maxOccurs="unbounded".
func TestXSDHintsForceArray(t *testing.T) {
	xmlSrc := `<Response>
  <Product>
    <Scores>
      <Score><Value>748</Value><Code>01</Code></Score>
    </Scores>
    <TradeLines>
      <TradeLine><AccountNumber>XXXX-1234</AccountNumber><Balance>2450</Balance><ZipCode>10001</ZipCode></TradeLine>
    </TradeLines>
  </Product>
</Response>`

	hints, err := ingest.ParseXSD(strings.NewReader(sampleXSD))
	if err != nil {
		t.Fatalf("ParseXSD: %v", err)
	}

	opts := ingest.DefaultXMLOptions()
	opts.Hints = hints

	rows, err := ingest.ParseXML(strings.NewReader(xmlSrc), opts)
	if err != nil {
		t.Fatalf("ParseXML: %v", err)
	}
	root := rows[0]

	prod := get(t, root, "Product")

	// Score: single child but maxOccurs=unbounded → must be []interface{}
	scores := get(t, prod, "Scores")
	scoreArr, ok := get(t, scores, "Score").([]interface{})
	if !ok {
		t.Fatalf("✗ Score (single, XSD force-array) should be []interface{}, got %T", get(t, scores, "Score"))
	}
	t.Logf("✓ Score is []interface{} (XSD force-array), len=%d", len(scoreArr))

	// TradeLine: single child, XSD force-array
	tl := get(t, prod, "TradeLines")
	tlArr, ok := get(t, tl, "TradeLine").([]interface{})
	if !ok {
		t.Fatalf("✗ TradeLine should be []interface{}, got %T", get(t, tl, "TradeLine"))
	}
	t.Logf("✓ TradeLine is []interface{}, len=%d", len(tlArr))

	// ZipCode: xs:string → must stay "10001" not 10001 float
	tlMap := tlArr[0]
	assertEq(t, "ZipCode stays string", get(t, tlMap, "ZipCode"), "10001")

	// AccountNumber: xs:string → no coercion
	assertEq(t, "AccountNumber stays string", get(t, tlMap, "AccountNumber"), "XXXX-1234")

	// Balance: xs:decimal → coerced to float
	assertEq(t, "Balance coerced to float", get(t, tlMap, "Balance"), 2450.0)

	// Score Code: xs:string → stays "01" not 1
	assertEq(t, "Score Code stays string (leading zero)", get(t, scoreArr[0], "Code"), "01")
}

// TestXSDHintsStringNoCoerce verifies xs:string suppresses numeric coercion.
func TestXSDHintsStringNoCoerce(t *testing.T) {
	xsdSrc := `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="Data">
    <xs:complexType><xs:sequence>
      <xs:element name="ZipCode"  type="xs:string"/>
      <xs:element name="Score"    type="xs:integer"/>
    </xs:sequence></xs:complexType>
  </xs:element>
</xs:schema>`

	xmlSrc := `<Data><ZipCode>10001</ZipCode><Score>750</Score></Data>`

	hints, _ := ingest.ParseXSD(strings.NewReader(xsdSrc))
	opts := ingest.DefaultXMLOptions()
	opts.Hints = hints

	rows, _ := ingest.ParseXML(strings.NewReader(xmlSrc), opts)
	root := rows[0]

	// ZipCode → xs:string → must be "10001" (string), not 10001 (float)
	zipCode := get(t, root, "ZipCode")
	if s, ok := zipCode.(string); !ok || s != "10001" {
		t.Errorf("✗ ZipCode should be string %q, got %T %v", "10001", zipCode, zipCode)
	} else {
		t.Logf("✓ ZipCode = %q (string, not float)", s)
	}

	// Score → xs:integer → coerced to float64
	assertEq(t, "Score coerced to number", get(t, root, "Score"), 750.0)
}

func TestMergeXSDHints(t *testing.T) {
	a := ingest.XSDHints{
		StringPaths: map[string]bool{"foo": true},
		ArrayPaths:  map[string]bool{"bar": true},
	}
	b := ingest.XSDHints{
		StringPaths: map[string]bool{"baz": true},
		ArrayPaths:  map[string]bool{"bar": true, "qux": true},
	}
	merged := ingest.MergeXSDHints(a, b)
	if !merged.StringPaths["foo"] || !merged.StringPaths["baz"] {
		t.Error("StringPaths not merged")
	}
	if !merged.ArrayPaths["bar"] || !merged.ArrayPaths["qux"] {
		t.Error("ArrayPaths not merged")
	}
	t.Log("✓ MergeXSDHints correct")
}
