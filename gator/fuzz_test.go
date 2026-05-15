package gator_test

// fuzz_test.go — fuzz testing untuk JSON/XML ingest dan DSL.
//
// Go 1.18+ fuzz testing: engine tidak boleh panic untuk input apapun.
//
// Menjalankan fuzz (beberapa detik, menemukan corpus baru):
//
//	go test ./gator/... -fuzz=FuzzJSONIngest -fuzztime=30s
//	go test ./gator/... -fuzz=FuzzXMLIngest -fuzztime=30s
//	go test ./gator/... -fuzz=FuzzAggregateRequest -fuzztime=30s
//
// Menjalankan corpus yang tersimpan saja (regression, masuk CI):
//
//	go test ./gator/... -run=FuzzJSONIngest
//	go test ./gator/... -run=FuzzXMLIngest
//	go test ./gator/... -run=FuzzAggregateRequest

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"aggregator/gator"
	"aggregator/gator/ingest"
)

// ── FuzzJSONIngest ────────────────────────────────────────────────────────────
//
// Fuzz JSON parsing: arbitrary bytes → json.Unmarshal → gator.Aggregate.
// Invariant: must never panic, must return (rows, err) without crashing.

func FuzzJSONIngest(f *testing.F) {
	// Seed corpus: valid and interesting edge cases
	seeds := []string{
		`[]`,
		`{}`,
		`[{}]`,
		`[{"a":1}]`,
		`[{"credits":[]}]`,
		`[{"credits":[{"balance":0}]}]`,
		`[{"credits":[{"balance":1e308}]}]`,
		`[{"credits":[{"balance":-1e308}]}]`,
		`[{"x":null,"y":true,"z":false}]`,
		`[{"a":"hello"},{"a":"world"}]`,
		`[{"nested":{"deep":{"deeper":42}}}]`,
		`[{"arr":[1,2,3],"obj":{"k":"v"}}]`,
		// Edge cases for engine
		`[{"dept":"IT","salary":0}]`,
		`[{"dept":"IT","credits":[{"bal":1},{"bal":2}]},{"dept":"IT","credits":[]}]`,
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		// Only fuzz valid UTF-8 to keep the corpus meaningful
		if !utf8.Valid(data) {
			return
		}

		var parsed interface{}
		if err := json.Unmarshal(data, &parsed); err != nil {
			return // not valid JSON — skip, we're fuzzing the engine not the parser
		}

		var records []interface{}
		switch v := parsed.(type) {
		case []interface{}:
			records = v
		case map[string]interface{}:
			records = []interface{}{v}
		default:
			return
		}
		if len(records) == 0 {
			return
		}

		store := gator.NewStore()
		store.Register("fuzz", records)

		// Try multiple aggregation patterns — none should panic
		reqs := []gator.AggregateRequest{
			{Dataset: "fuzz", Aggregations: []gator.AggConfig{{Field: "x", Op: "sum", Alias: "s"}}},
			{Dataset: "fuzz", GroupBy: []string{"dept"}, Aggregations: []gator.AggConfig{{Field: "salary", Op: "avg", Alias: "a"}}},
			{Dataset: "fuzz", Aggregations: []gator.AggConfig{{Field: "credits.balance", Op: "max", Alias: "m"}}},
			{Dataset: "fuzz", Aggregations: []gator.AggConfig{{Field: "credits.balance", Op: "avg", Alias: "a"}}},
		}
		for _, req := range reqs {
			// Must not panic — error is acceptable
			rows, _ := gator.Aggregate(store, req)
			_ = rows
		}
	})
}

// ── FuzzXMLIngest ─────────────────────────────────────────────────────────────
//
// Fuzz XML parsing: arbitrary bytes → ingest.ParseXML.
// Invariant: must never panic, must return (rows, err).

func FuzzXMLIngest(f *testing.F) {
	seeds := []string{
		`<root/>`,
		`<root></root>`,
		`<root><item>1</item></root>`,
		`<root><item><val>1</val></item><item><val>2</val></item></root>`,
		`<root><item active="true">hello</item></root>`,
		`<root><item><credits><credit><balance>100</balance></credit></credits></item></root>`,
		`<r><a>1e308</a></r>`,
		`<r><a>-1e308</a></r>`,
		`<r><a>0</a></r>`,
		`<r><a>true</a><b>false</b></r>`,
		`<r><a/></r>`,
		`<?xml version="1.0"?><root><item>text</item></root>`,
		`<r><a>01</a></r>`,    // leading zero
		`<r><a>007</a></r>`,   // leading zero
		`<r><a>3201234567890001</a></r>`, // long NIK-like number
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	opts := ingest.DefaultXMLOptions()
	f.Fuzz(func(t *testing.T, data []byte) {
		if !utf8.Valid(data) {
			return
		}
		// Must not panic
		rows, _ := ingest.ParseXML(bytes.NewReader(data), opts)
		if len(rows) == 0 {
			return
		}
		// Try aggregation on parsed result
		store := gator.NewStore()
		store.Register("fuzz_xml", rows)
		req := gator.AggregateRequest{
			Dataset:      "fuzz_xml",
			Aggregations: []gator.AggConfig{{Field: "item.val", Op: "sum", Alias: "s"}},
		}
		result, _ := gator.Aggregate(store, req)
		_ = result
	})
}

// ── FuzzAggregateRequest ──────────────────────────────────────────────────────
//
// Fuzz the DSL JSON: arbitrary AggregateRequest JSON bodies.
// Invariant: must never panic regardless of field names, ops, params.

func FuzzAggregateRequest(f *testing.F) {
	validReqs := []string{
		`{"dataset":"employees","aggregations":[{"field":"salary","op":"sum","alias":"s"}]}`,
		`{"dataset":"employees","groupBy":["department"],"aggregations":[{"field":"salary","op":"avg","alias":"a"}]}`,
		`{"dataset":"employees","aggregations":[{"field":"credits.outstanding_balance","op":"sum","alias":"o"}]}`,
		`{"dataset":"employees","aggregations":[{"field":"credits.last_24_coll_hist","op":"worst_last_n","alias":"w","params":{"n":6}}]}`,
		`{"dataset":"employees","aggregations":[{"field":"credits.last_24_coll_hist","op":"ever_has_last_n","alias":"e","params":{"n":6,"value":3}}]}`,
		`{"dataset":"employees","where":{"salary":{"$gt":20000000}},"aggregations":[{"field":"salary","op":"count","alias":"c"}]}`,
		// Invalid ops — should return error, not panic
		`{"dataset":"employees","aggregations":[{"field":"salary","op":"bogus","alias":"b"}]}`,
		`{"dataset":"employees","aggregations":[{"field":"","op":"sum","alias":"s"}]}`,
		`{"dataset":"employees","aggregations":[{"field":"credits","op":"sum","alias":"s"}]}`,
		// Weird params
		`{"dataset":"employees","aggregations":[{"field":"salary","op":"avg","params":{"n":-1}}]}`,
		`{"dataset":"employees","aggregations":[{"field":"salary","op":"avg","params":{"n":99999}}]}`,
	}
	for _, s := range validReqs {
		f.Add([]byte(s))
	}

	store := newStore()

	f.Fuzz(func(t *testing.T, data []byte) {
		if !utf8.Valid(data) {
			return
		}
		var req gator.AggregateRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return // not valid JSON struct — skip
		}
		// Force dataset to "employees" so we always have data
		req.Dataset = "employees"

		// Must not panic — error is acceptable
		rows, _ := gator.Aggregate(store, req)
		_ = rows
	})
}

// ── FuzzWhereClause ───────────────────────────────────────────────────────────
//
// Fuzz WHERE clause evaluation specifically.
// Invariant: EvaluateWhere must never panic.

func FuzzWhereClause(f *testing.F) {
	seeds := []string{
		`{"salary":{"$gt":0}}`,
		`{"salary":{"$eq":25000000}}`,
		`{"dept":{"$in":["IT","Finance"]}}`,
		`{"salary":{"$gt":0,"$lt":99999999}}`,
		`{"name":{"$contains":"an"}}`,
		`{"x":{"$notnull":true}}`,
		`{"x":{"$null":true}}`,
		`{"nonexistent":{"$eq":"value"}}`,
		`{"salary":{"$gt":"not_a_number"}}`,
		`{"salary":{"$in":"not_an_array"}}`,
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	store := newStore()

	f.Fuzz(func(t *testing.T, data []byte) {
		if !utf8.Valid(data) || !strings.HasPrefix(strings.TrimSpace(string(data)), "{") {
			return
		}
		var where map[string]map[string]interface{}
		if err := json.Unmarshal(data, &where); err != nil {
			return
		}
		req := gator.AggregateRequest{
			Dataset:      "employees",
			Where:        where,
			Aggregations: []gator.AggConfig{{Field: "salary", Op: "count", Alias: "c"}},
		}
		rows, _ := gator.Aggregate(store, req)
		_ = rows
	})
}

// ── FuzzXSDParse ─────────────────────────────────────────────────────────────
//
// Fuzz XSD parsing: arbitrary bytes → ingest.ParseXSD.
// Invariant: must never panic.

func FuzzXSDParse(f *testing.F) {
	seeds := []string{
		`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"></xs:schema>`,
		`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
		  <xs:element name="Root">
		    <xs:complexType><xs:sequence>
		      <xs:element name="Item" maxOccurs="unbounded">
		        <xs:complexType><xs:sequence>
		          <xs:element name="Id" type="xs:string"/>
		          <xs:element name="Value" type="xs:integer"/>
		        </xs:sequence></xs:complexType>
		      </xs:element>
		    </xs:sequence></xs:complexType>
		  </xs:element>
		</xs:schema>`,
		`<schema/>`,
		`<xs:schema/>`,
		`not xml at all`,
		`<xs:element name="x" maxOccurs="unbounded" type="xs:string"/>`,
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if !utf8.Valid(data) {
			return
		}
		hints, _ := ingest.ParseXSD(bytes.NewReader(data))
		_ = hints
		// StripRootPrefix must also not panic on any hints
		stripped := ingest.StripRootPrefix(hints)
		_ = stripped
	})
}
