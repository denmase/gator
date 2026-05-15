package ingest_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"aggregator/gator/ingest"
)

var opts = ingest.DefaultXMLOptions()

// ── helper ────────────────────────────────────────────────────────────────────

func parseStr(t *testing.T, src string) interface{} {
	t.Helper()
	rows, err := ingest.ParseXML(strings.NewReader(src), opts)
	if err != nil {
		t.Fatalf("ParseXML: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("ParseXML: empty result")
	}
	return rows[0]
}

func get(t *testing.T, v interface{}, path ...string) interface{} {
	t.Helper()
	cur := v
	for _, key := range path {
		m, ok := cur.(map[string]interface{})
		if !ok {
			t.Fatalf("get(%v): not a map at key %q, got %T", path, key, cur)
		}
		cur = m[key]
	}
	return cur
}

func assertEq(t *testing.T, label string, got, want interface{}) {
	t.Helper()
	gs := toString(got)
	ws := toString(want)
	if gs != ws {
		t.Errorf("✗ %-50s got=%v  want=%v", label, got, want)
	} else {
		t.Logf("✓ %-50s = %v", label, got)
	}
}

func toString(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// ── Unit tests ────────────────────────────────────────────────────────────────

func TestPureTextScalar(t *testing.T) {
	root := parseStr(t, `<root><Score>750</Score><Name>John</Name></root>`)
	assertEq(t, "Score numeric", get(t, root, "Score"), 750.0)
	assertEq(t, "Name string",   get(t, root, "Name"),  "John")
}

func TestEmptyElement(t *testing.T) {
	root := parseStr(t, `<root><Note/></root>`)
	assertEq(t, "empty element → null", get(t, root, "Note"), nil)
}

func TestBooleanCoerce(t *testing.T) {
	root := parseStr(t, `<root><Active>true</Active><Closed>false</Closed></root>`)
	assertEq(t, "true coerced",  get(t, root, "Active"), true)
	assertEq(t, "false coerced", get(t, root, "Closed"), false)
}

func TestAttrPlusText(t *testing.T) {
	// <loanid status="active">1234567890</loanid>
	// → "loanid": {"@status": "active", "#text": 1234567890}
	root := parseStr(t, `<root><loanid status="active">1234567890</loanid></root>`)
	loan := get(t, root, "loanid")
	assertEq(t, "loanid @status",  get(t, loan, "@status"), "active")
	assertEq(t, "loanid #text",    get(t, loan, "#text"),   1234567890.0)
}

func TestAttrOnly(t *testing.T) {
	root := parseStr(t, `<root><Status active="true"/></root>`)
	st := get(t, root, "Status")
	assertEq(t, "Status @active", get(t, st, "@active"), true)
}

// Container with single child → scalar (tag appears once → not forced to array).
// This is correct: force-array only triggers when the same tag appears 2+ times.
// For schema consistency across datasets, callers should use a representative
// first record (one with multiple items) or handle both types in their queries.
func TestSingleChildIsScalar(t *testing.T) {
	xml := `<root>
	  <Scores>
	    <Score><Value>748</Value></Score>
	  </Scores>
	</root>`
	root := parseStr(t, xml)
	scores := get(t, root, "Scores")
	// Score appears once → scalar map, not array
	score, ok := get(t, scores, "Score").(map[string]interface{})
	if !ok {
		t.Fatalf("✗ Score (single child) should be map[string]interface{}, got %T", get(t, scores, "Score"))
	}
	assertEq(t, "Score.Value scalar", get(t, score, "Value"), 748.0)
	t.Log("✓ single child stays scalar (not forced to array)")
}

// Container with multiple same-tag children → forced to array.
func TestForceArraySingleChild(t *testing.T) {
	xml := `<root>
	  <Scores>
	    <Score><Value>748</Value></Score>
	    <Score><Value>710</Value></Score>
	  </Scores>
	</root>`
	root := parseStr(t, xml)
	scores := get(t, root, "Scores")
	scoreArr, ok := get(t, scores, "Score").([]interface{})
	if !ok {
		t.Fatalf("✗ Score (2 children) should be []interface{}, got %T", get(t, scores, "Score"))
	}
	t.Logf("✓ Score is []interface{} with %d elements (force array)", len(scoreArr))
	assertEq(t, "Score[0].Value", get(t, scoreArr[0], "Value"), 748.0)
	assertEq(t, "Score[1].Value", get(t, scoreArr[1], "Value"), 710.0)
}

// Container pattern: multiple children same tag → array.
func TestForceArrayMultipleChildren(t *testing.T) {
	xml := `<root>
	  <TradeLines>
	    <TradeLine><Balance>2450</Balance></TradeLine>
	    <TradeLine><Balance>18500</Balance></TradeLine>
	    <TradeLine><Balance>320000</Balance></TradeLine>
	  </TradeLines>
	</root>`
	root := parseStr(t, xml)
	tl := get(t, root, "TradeLines")
	arr, ok := get(t, tl, "TradeLine").([]interface{})
	if !ok {
		t.Fatalf("✗ TradeLine should be []interface{}, got %T", get(t, tl, "TradeLine"))
	}
	if len(arr) != 3 {
		t.Fatalf("✗ expected 3 TradeLines, got %d", len(arr))
	}
	assertEq(t, "TradeLine[0].Balance", get(t, arr[0], "Balance"), 2450.0)
	assertEq(t, "TradeLine[2].Balance", get(t, arr[2], "Balance"), 320000.0)
	t.Logf("✓ 3 TradeLines parsed as array")
}

// Non-container: children have different tags → scalar, not array.
func TestNonContainerScalar(t *testing.T) {
	xml := `<root>
	  <Fasilitas>
	    <NomorKontrak>KRD-001</NomorKontrak>
	    <Plafond>500000000</Plafond>
	  </Fasilitas>
	</root>`
	root := parseStr(t, xml)
	fas := get(t, root, "Fasilitas")
	assertEq(t, "NomorKontrak scalar", get(t, fas, "NomorKontrak"), "KRD-001")
	assertEq(t, "Plafond scalar",      get(t, fas, "Plafond"),      500000000.0)
}

// Primitive array: <Code>01</Code><Code>05</Code> → ["01","05"]
func TestPrimitiveArray(t *testing.T) {
	xml := `<root>
	  <FactorCodes>
	    <Code>01</Code>
	    <Code>05</Code>
	    <Code>09</Code>
	  </FactorCodes>
	</root>`
	root := parseStr(t, xml)
	fc := get(t, root, "FactorCodes")
	codes, ok := get(t, fc, "Code").([]interface{})
	if !ok {
		t.Fatalf("✗ Code should be []interface{}, got %T", get(t, fc, "Code"))
	}
	assertEq(t, "Code[0]", codes[0], "01")
	assertEq(t, "Code[2]", codes[2], "09")
	t.Logf("✓ %d Codes as primitive array", len(codes))
}

// Root-level namespace attribute (version="2.45") → just "@version".
func TestRootAttribute(t *testing.T) {
	xml := `<Response version="2.45"><Status>OK</Status></Response>`
	root := parseStr(t, xml)
	// version="2.45" → coerced to float64 (no leading zero, valid number)
	assertEq(t, "@version attr on root", get(t, root, "@version"), 2.45)
	assertEq(t, "Status", get(t, root, "Status"), "OK")
}

// Alert: plain text children in container → primitive array.
func TestAlertPrimitiveArray(t *testing.T) {
	xml := `<root>
	  <Alerts>
	    <Alert>High Risk Fraud Alert</Alert>
	    <Alert>Address Mismatch Detected</Alert>
	  </Alerts>
	</root>`
	root := parseStr(t, xml)
	al := get(t, root, "Alerts")
	alerts, ok := get(t, al, "Alert").([]interface{})
	if !ok {
		t.Fatalf("✗ Alert should be []interface{}, got %T", get(t, al, "Alert"))
	}
	assertEq(t, "Alert[0]", alerts[0], "High Risk Fraud Alert")
	assertEq(t, "Alert[1]", alerts[1], "Address Mismatch Detected")
}

// ── Integration: tuxml.xml ────────────────────────────────────────────────────

func TestTUXML(t *testing.T) {
	f, err := os.Open("/mnt/user-data/uploads/tuxml.xml")
	if err != nil {
		t.Skipf("tuxml.xml not available: %v", err)
	}
	defer f.Close()

	rows, err := ingest.ParseXML(f, opts)
	if err != nil {
		t.Fatalf("ParseXML tuxml: %v", err)
	}
	root := rows[0]

	b, _ := json.MarshalIndent(root, "", "  ")
	t.Logf("tuxml parsed:\n%s", string(b))

	prod := get(t, root, "Product")

	// Scalar fields
	assertEq(t, "ResponseType", get(t, prod, "ResponseType"), "07000")
	assertEq(t, "ResponseDate", get(t, prod, "ResponseDate"), "2026-05-01")

	cf := get(t, prod, "ConsumerFile")

	// Scores.Score → single child → scalar map (not array)
	scores := get(t, cf, "Scores")
	score, ok := get(t, scores, "Score").(map[string]interface{})
	if !ok {
		t.Fatalf("✗ Score (single) should be map[string]interface{}, got %T", get(t, scores, "Score"))
	}
	t.Log("✓ Scores.Score is scalar map (single child)")
	assertEq(t, "Score.Value", get(t, score, "Value"), 748.0)
	assertEq(t, "Score.Model", get(t, score, "Model"), "FICO 9")

	// FactorCodes.Code → 3 items → primitive array
	fc := get(t, score, "FactorCodes")
	codes, ok := get(t, fc, "Code").([]interface{})
	if !ok {
		t.Fatalf("✗ FactorCodes.Code should be []interface{}")
	}
	assertEq(t, "FactorCodes len=3", len(codes), 3)

	// TradeLines → 3 TradeLine → array
	tl := get(t, cf, "TradeLines")
	tlArr, ok := get(t, tl, "TradeLine").([]interface{})
	if !ok {
		t.Fatalf("✗ TradeLine should be []interface{}")
	}
	assertEq(t, "TradeLine count",          len(tlArr),                      3)
	assertEq(t, "TradeLine[0].Balance",     get(t, tlArr[0], "Balance"),     2450.0)
	assertEq(t, "TradeLine[0].DaysPastDue", get(t, tlArr[0], "DaysPastDue"), 0.0)
	assertEq(t, "TradeLine[1].Balance",     get(t, tlArr[1], "Balance"),     18500.0)

	// PublicRecords.PublicRecord → single child → scalar map
	pr := get(t, cf, "PublicRecords")
	prMap, ok := get(t, pr, "PublicRecord").(map[string]interface{})
	if !ok {
		t.Fatalf("✗ PublicRecord (single) should be map[string]interface{}, got %T", get(t, pr, "PublicRecord"))
	}
	assertEq(t, "PublicRecord.Amount", get(t, prMap, "Amount"), 45000.0)
	t.Log("✓ PublicRecord is scalar map (single child)")

	// Inquiries → 2 Inquiry → array
	inq := get(t, cf, "Inquiries")
	inqArr, ok := get(t, inq, "Inquiry").([]interface{})
	if !ok {
		t.Fatalf("✗ Inquiry should be []interface{}")
	}
	assertEq(t, "Inquiry count", len(inqArr), 2)

	// Alerts → 2 items → primitive array
	al := get(t, cf, "Alerts")
	alerts, ok := get(t, al, "Alert").([]interface{})
	if !ok {
		t.Fatalf("✗ Alert should be []interface{}")
	}
	assertEq(t, "Alert count", len(alerts), 2)

	// ResponseSummary scalar
	rs := get(t, root, "ResponseSummary")
	assertEq(t, "TotalTradeLines",           get(t, rs, "TotalTradeLines"),           12.0)
	assertEq(t, "NegativeItems",             get(t, rs, "NegativeItems"),             2.0)
	assertEq(t, "NumberOfInquiriesLast30Days",get(t, rs, "NumberOfInquiriesLast30Days"),1.0)
}

// ── Integration: ideb.xml ─────────────────────────────────────────────────────

func TestIDEB(t *testing.T) {
	f, err := os.Open("/mnt/user-data/uploads/ideb.xml")
	if err != nil {
		t.Skipf("ideb.xml not available: %v", err)
	}
	defer f.Close()

	rows, err := ingest.ParseXML(f, opts)
	if err != nil {
		t.Fatalf("ParseXML ideb: %v", err)
	}
	root := rows[0]

	b, _ := json.MarshalIndent(root, "", "  ")
	t.Logf("ideb parsed:\n%s", string(b))

	// Top-level scalars
	assertEq(t, "Status",    get(t, root, "Status"),    "SUCCESS")
	assertEq(t, "InquiryId", get(t, root, "InquiryId"), "INQ-20260501-ABC12345")

	// Debtor
	debtor := get(t, root, "Debtor")
	assertEq(t, "NIK",  get(t, debtor, "NIK"),  "3201234567890001")
	assertEq(t, "Nama", get(t, debtor, "Nama"), "AGUNG SAPUTRA")

	// CreditScore
	cs := get(t, root, "CreditScore")
	assertEq(t, "CreditScore.Score", get(t, cs, "Score"), 720.0)
	assertEq(t, "CreditScore.Grade", get(t, cs, "Grade"), "A")

	// Summary
	sum := get(t, root, "Summary")
	assertEq(t, "TotalPlafond",      get(t, sum, "TotalPlafond"),      1250000000.0)
	assertEq(t, "TotalOutstanding",  get(t, sum, "TotalOutstanding"),  680000000.0)

	// FasilitasList.Fasilitas → array (2 elements)
	fl := get(t, root, "FasilitasList")
	fasArr, ok := get(t, fl, "Fasilitas").([]interface{})
	if !ok {
		t.Fatalf("✗ Fasilitas should be []interface{}, got %T", get(t, fl, "Fasilitas"))
	}
	assertEq(t, "Fasilitas count", len(fasArr), 2)

	fas0 := fasArr[0]
	assertEq(t, "Fasilitas[0].NomorKontrak", get(t, fas0, "NomorKontrak"), "KRD-001234567")
	assertEq(t, "Fasilitas[0].JenisKredit",  get(t, fas0, "JenisKredit"),  "KPR")
	assertEq(t, "Fasilitas[0].Outstanding",  get(t, fas0, "Outstanding"),  320000000.0)
	assertEq(t, "Fasilitas[0].StatusRestrukturisasi", get(t, fas0, "StatusRestrukturisasi"), false)

	// RiwayatKolektibilitas inside Fasilitas[0] → array (5 entries in sample)
	rk, ok := get(t, fas0, "RiwayatKolektibilitas").([]interface{})
	if !ok {
		t.Fatalf("✗ RiwayatKolektibilitas should be []interface{}, got %T", get(t, fas0, "RiwayatKolektibilitas"))
	}
	t.Logf("✓ RiwayatKolektibilitas len=%d", len(rk))
	assertEq(t, "RiwKol[0].Bulan",          get(t, rk[0], "Bulan"),         "2026-04")
	assertEq(t, "RiwKol[0].Kolektibilitas", get(t, rk[0], "Kolektibilitas"), 1.0)
	assertEq(t, "RiwKol[0].HariTunggakan", get(t, rk[0], "HariTunggakan"),  0.0)
	assertEq(t, "RiwKol[2].HariTunggakan", get(t, rk[2], "HariTunggakan"),  35.0) // 2026-02

	// Fasilitas[1]
	fas1 := fasArr[1]
	assertEq(t, "Fasilitas[1].NomorKontrak",          get(t, fas1, "NomorKontrak"),          "KRD-009876543")
	assertEq(t, "Fasilitas[1].KolektibilitasSaatIni", get(t, fas1, "KolektibilitasSaatIni"), 2.0)
	assertEq(t, "Fasilitas[1].StatusRestrukturisasi", get(t, fas1, "StatusRestrukturisasi"), true)

	rk1, ok := get(t, fas1, "RiwayatKolektibilitas").([]interface{})
	if !ok {
		t.Fatalf("✗ Fasilitas[1].RiwayatKolektibilitas should be []interface{}")
	}
	assertEq(t, "Fasilitas[1] RiwKol len", len(rk1), 2)
	assertEq(t, "Fasilitas[1] RiwKol[0].HariTunggakan", get(t, rk1[0], "HariTunggakan"), 45.0)

	// WarningList.Warning → array
	wl := get(t, root, "WarningList")
	warnings, ok := get(t, wl, "Warning").([]interface{})
	if !ok {
		t.Fatalf("✗ Warning should be []interface{}")
	}
	assertEq(t, "Warning count",      len(warnings),                    2)
	assertEq(t, "Warning[0].Kode",    get(t, warnings[0], "Kode"),     "W001")
	assertEq(t, "Warning[1].Kode",    get(t, warnings[1], "Kode"),     "W002")
}
