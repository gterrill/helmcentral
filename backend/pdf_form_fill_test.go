package main

import (
	"math"
	"os"
	"strings"
	"testing"

	"github.com/ledongthuc/pdf"
)

func fieldNamed(fields []pdfFormField, name string) (pdfFormField, bool) {
	for _, f := range fields {
		if f.Name == name {
			return f, true
		}
	}
	return pdfFormField{}, false
}

func TestListAcroFormFields_NamesKindsOptionsAndValues(t *testing.T) {
	fields, err := listAcroFormFields(acroFormFixture(t))
	if err != nil {
		t.Fatalf("listAcroFormFields: %v", err)
	}
	owner, ok := fieldNamed(fields, "owner_name")
	if !ok || owner.Kind != "text" || owner.Value != "" {
		t.Fatalf("expected an empty text field owner_name, got %+v", fields)
	}
	if shore, ok := fieldNamed(fields, "shore_power"); !ok || shore.Kind != "checkbox" || shore.Value != false {
		t.Fatalf("expected an unticked check box, got %+v", fields)
	}
	if berth, ok := fieldNamed(fields, "berth_type"); !ok || berth.Kind != "radio" || len(berth.Options) != 2 || berth.Value != "Alongside" {
		t.Fatalf("expected the radio group with its options and current value, got %+v", fields)
	}
	if cur, ok := fieldNamed(fields, "currency"); !ok || cur.Kind != "combobox" || len(cur.Options) != 3 {
		t.Fatalf("expected the combo box with its options, got %+v", fields)
	}
}

func TestListAcroFormFields_FlatFormHasNone(t *testing.T) {
	fields, err := listAcroFormFields(flatFormFixture(t))
	if err != nil || len(fields) != 0 {
		t.Fatalf("a flat form has no fields, got %+v err=%v", fields, err)
	}
}

func TestFillPDFForm_AcroFormValuesAreSet(t *testing.T) {
	path := writeFixturePDF(t, "berth.pdf", acroFormFixture(t))
	res, err := fillPDFForm(path, []formEntry{
		{Field: "owner_name", Value: "Sam Example"},
		{Field: "vessel_length", Value: "18.3 m"},
		{Field: "shore_power", Value: "yes"},
		{Field: "berth_type", Value: "Stern to"},
		{Field: "currency", Value: "NZD"},
	})
	if err != nil {
		t.Fatalf("fillPDFForm: %v", err)
	}
	if res.PageCount != 1 {
		t.Fatalf("expected one page, got %d", res.PageCount)
	}
	fields, err := listAcroFormFields(res.Data)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"owner_name": "Sam Example", "vessel_length": "18.3 m", "shore_power": true, "berth_type": "Stern to", "currency": "NZD"}
	for name, v := range want {
		if f, ok := fieldNamed(fields, name); !ok || f.Value != v {
			t.Errorf("%s: want %v, got %+v", name, v, f)
		}
	}
}

func TestFillPDFForm_UnknownFieldBadOptionAndBadTickAreNamedAndNothingIsWritten(t *testing.T) {
	path := writeFixturePDF(t, "berth.pdf", acroFormFixture(t))
	_, err := fillPDFForm(path, []formEntry{
		{Field: "owner_name", Value: "Sam"},
		{Field: "ownr_name", Value: "typo"},
		{Field: "currency", Value: "GBP"},
		{Field: "shore_power", Value: "maybe"},
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{`field "ownr_name": no such field`, "owner_name", `"GBP" is not one of its options`, "takes yes or no", "Signature fields are never filled in"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected %q in %v", want, err)
		}
	}
}

func TestFillPDFForm_NeedsExactlyOneKindPerEntry(t *testing.T) {
	path := writeFixturePDF(t, "berth.pdf", acroFormFixture(t))
	if _, err := fillPDFForm(path, []formEntry{{Field: "owner_name", Anchor: "Owner name", Value: "x"}}); err == nil {
		t.Fatal("an entry naming both a field and an anchor is refused")
	}
	if _, err := fillPDFForm(path, nil); err == nil {
		t.Fatal("no entries is refused")
	}
}

// pageOps collects the operators of a page's content, for checking what a
// stamp drew.
func pageOps(t *testing.T, data []byte) map[string]int {
	t.Helper()
	path := writeFixturePDF(t, "ops.pdf", data)
	f, r, err := pdf.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ops := map[string]int{}
	pdf.Interpret(r.Page(1).V.Key("Contents"), func(stk *pdf.Stack, op string) {
		for stk.Len() > 0 {
			stk.Pop()
		}
		ops[op]++
	})
	return ops
}

func TestFillPDFForm_FlatTextLandsAfterItsLabelAndReadsBack(t *testing.T) {
	path := writeFixturePDF(t, "storm.pdf", flatFormFixture(t))
	before, err := analysePDFLayout(path)
	if err != nil {
		t.Fatal(err)
	}
	label, _ := before.Pages[0].line("Vessel owner")

	res, err := fillPDFForm(path, []formEntry{
		{Page: 1, Anchor: "Vessel owner", Value: "Sam Example"},
		{Page: 1, Anchor: "Policy number", Value: "POL-123 (AU)"},
	})
	if err != nil {
		t.Fatalf("fillPDFForm: %v", err)
	}

	after, err := analysePDFLayout(writeFixturePDF(t, "filled.pdf", res.Data))
	if err != nil {
		t.Fatalf("re-reading the filled form: %v", err)
	}
	p := after.Pages[0]
	// Four points after the label, the value reads as the same line.
	var owner pdfLayoutLine
	for _, l := range p.Lines {
		if strings.HasPrefix(l.Text, "Vessel owner ") {
			owner = l
		}
	}
	if owner.Text != "Vessel owner Sam Example" {
		t.Fatalf("the value reads back after its label, got %+v", p.Lines)
	}
	if math.Abs(owner.X-label.X) > 0.5 {
		t.Errorf("the line still starts at the label: %.1f vs %.1f", owner.X, label.X)
	}
	wantEnd := label.X + label.W + stampGap + helveticaWidth("Sam Example", 11)
	if math.Abs(owner.X+owner.W-wantEnd) > 1.5 {
		t.Errorf("value should end at %.1f, the line ends at %.1f", wantEnd, owner.X+owner.W)
	}
	if math.Abs(owner.Y-label.Y) > 2 {
		t.Errorf("value sits on the label's baseline: label %.1f, line %.1f", label.Y, owner.Y)
	}
	found := false
	for _, l := range p.Lines {
		if strings.Contains(l.Text, "POL-123 (AU)") {
			found = true
		}
	}
	if !found {
		t.Errorf("parentheses in a value are escaped and read back, got %+v", p.Lines)
	}
	// The label itself and the original boxes are untouched.
	if len(p.Boxes) != 2 {
		t.Errorf("the form's own content is intact, got %+v", p)
	}
}

func TestFillPDFForm_FlatTickIsDrawnInTheBox(t *testing.T) {
	path := writeFixturePDF(t, "storm.pdf", flatFormFixture(t))
	orig, _ := readFileForTest(t, path)
	beforeOps := pageOps(t, orig)

	res, err := fillPDFForm(path, []formEntry{{Page: 1, Box: "South Cove"}})
	if err != nil {
		t.Fatalf("fillPDFForm: %v", err)
	}
	afterOps := pageOps(t, res.Data)
	if afterOps["l"] != beforeOps["l"]+2 || afterOps["S"] != beforeOps["S"]+1 {
		t.Errorf("a tick is one stroked path of three points, before %v after %v", beforeOps, afterOps)
	}
	again, err := analysePDFLayout(writeFixturePDF(t, "filled.pdf", res.Data))
	if err != nil || len(again.Pages[0].Boxes) != 2 {
		t.Fatalf("both boxes are still found after a tick: %v %+v", err, again)
	}
}

func TestFillPDFForm_FlatErrorsNameTheEntry(t *testing.T) {
	path := writeFixturePDF(t, "storm.pdf", flatFormFixture(t))
	cases := []struct {
		name    string
		entry   formEntry
		wantErr string
	}{
		{"unknown anchor", formEntry{Page: 1, Anchor: "Vessel ownerr", Value: "x"}, `anchor "Vessel ownerr" on page 1: not found`},
		{"unknown box", formEntry{Page: 1, Box: "West Bay"}, `box "West Bay" on page 1: not found`},
		{"bad page", formEntry{Page: 3, Anchor: "Vessel owner", Value: "x"}, "the document has 1 pages"},
		{"signature", formEntry{Page: 1, Anchor: "Signed", Value: "Sam"}, "signatures are left for the operator"},
		{"too long", formEntry{Page: 1, Anchor: "Vessel owner", Value: strings.Repeat("W", 120)}, "too long for the space"},
		{"not latin", formEntry{Page: 1, Anchor: "Vessel owner", Value: "山田"}, "cannot be written"},
		{"empty", formEntry{Page: 1, Anchor: "Vessel owner"}, "no value to write"},
	}
	for _, c := range cases {
		_, err := fillPDFForm(path, []formEntry{{Page: 1, Anchor: "Policy number", Value: "ok"}, c.entry})
		if err == nil || !strings.Contains(err.Error(), c.wantErr) {
			t.Errorf("%s: want %q, got %v", c.name, c.wantErr, err)
		}
	}
}

func TestFillPDFForm_AmbiguousAnchorNeedsAnOccurrence(t *testing.T) {
	path := writeFixturePDF(t, "dup.pdf", createPDF(t, `{
 "paper": "A4P", "origin": "LowerLeft",
 "pages": {"1": {"content": {"text": [
  {"value": "Name", "pos": [50, 700], "font": {"name": "Helvetica", "size": 11}},
  {"value": "Name", "pos": [50, 600], "font": {"name": "Helvetica", "size": 11}}
 ]}}}}`))
	_, err := fillPDFForm(path, []formEntry{{Page: 1, Anchor: "Name", Value: "Sam"}})
	if err == nil || !strings.Contains(err.Error(), "found 2 times") {
		t.Fatalf("expected an ambiguity error, got %v", err)
	}
	res, err := fillPDFForm(path, []formEntry{{Page: 1, Anchor: "Name", Occurrence: 2, Value: "Sam"}})
	if err != nil {
		t.Fatalf("occurrence 2: %v", err)
	}
	l, _ := analysePDFLayout(writeFixturePDF(t, "dup-filled.pdf", res.Data))
	stamped, _ := l.Pages[0].line("Sam")
	lower, _ := l.Pages[0].line("Name")
	_ = lower
	if stamped.Y > 650 {
		t.Errorf("occurrence 2 is the lower Name, but the value landed at y=%.1f", stamped.Y)
	}
}

func readFileForTest(t *testing.T, path string) ([]byte, error) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b, nil
}

// ── blanks printed as characters, and tick values ──────────────────────

func blankFormFixture(t *testing.T) []byte {
	return createPDF(t, `{
 "paper": "A4P", "origin": "LowerLeft",
 "pages": {"1": {"content": {"text": [
  {"value": "Owner name: ____________________________", "pos": [50, 700], "font": {"name": "Helvetica", "size": 11}},
  {"value": "Policy number .....................................", "pos": [50, 660], "font": {"name": "Helvetica", "size": 11}},
  {"value": "Berth", "pos": [50, 620], "font": {"name": "Helvetica", "size": 11}},
  {"value": "________________", "pos": [120, 620], "font": {"name": "Helvetica", "size": 11}}
 ]}}}}`)
}

// glyphXs gives where each glyph of a page with the given text sits.
func glyphXs(t *testing.T, data []byte, text string) []float64 {
	t.Helper()
	f, r, err := openPDFReader(writeFixturePDF(t, "g.pdf", data))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	scan, err := scanPDFPage(r.Page(1))
	if err != nil {
		t.Fatal(err)
	}
	var xs []float64
	for _, g := range scan.glyphs {
		if g.text == text {
			xs = append(xs, g.x)
		}
	}
	return xs
}

func TestAnalysePDFLayout_BlanksPrintedAsCharactersAreNotPartOfTheLabel(t *testing.T) {
	p := layoutOf(t, blankFormFixture(t)).Pages[0]
	owner, ok := p.line("Owner name:")
	if !ok || owner.BlankX < owner.X+owner.W || owner.BlankW < 100 {
		t.Fatalf("an underscore blank is split from its label and measured, got %+v", p.Lines)
	}
	policy, ok := p.line("Policy number")
	if !ok || policy.BlankW < 100 {
		t.Fatalf("a dot leader is split from its label, got %+v", p.Lines)
	}
	berth, ok := p.line("Berth")
	if !ok || berth.BlankX < 115 || berth.BlankW < 60 {
		t.Fatalf("a blank printed as its own run is attached to the label before it, got %+v", p.Lines)
	}
	for _, l := range p.Lines {
		if strings.Contains(l.Text, "___") || strings.Contains(l.Text, "....") {
			t.Fatalf("no blank is left in a line's text: %+v", l)
		}
	}
}

func TestFillPDFForm_ValueStartsAtTheBlankNotAfterIt(t *testing.T) {
	path := writeFixturePDF(t, "blank.pdf", blankFormFixture(t))
	before := layoutOf(t, blankFormFixture(t)).Pages[0]
	owner, _ := before.line("Owner name:")
	policy, _ := before.line("Policy number")
	berth, _ := before.line("Berth")

	res, err := fillPDFForm(path, []formEntry{
		{Page: 1, Anchor: "Owner name:", Value: "Sam Example"},
		{Page: 1, Anchor: "Policy number", Value: "POL-9"},
		{Page: 1, Anchor: "Berth", Value: "C14"},
	})
	if err != nil {
		t.Fatalf("fillPDFForm: %v", err)
	}
	near := func(xs []float64, want float64) bool {
		for _, x := range xs {
			if math.Abs(x-want) <= 2.5 {
				return true
			}
		}
		return false
	}
	if xs := glyphXs(t, res.Data, "S"); !near(xs, owner.BlankX) {
		t.Errorf("the owner starts at the underscore blank (%.1f), S at %v", owner.BlankX, xs)
	}
	if xs := glyphXs(t, res.Data, "P"); !near(xs, policy.BlankX) {
		t.Errorf("the policy starts at the dot leader (%.1f), P at %v", policy.BlankX, xs)
	}
	if xs := glyphXs(t, res.Data, "C"); !near(xs, berth.BlankX) {
		t.Errorf("the berth starts at its separate blank (%.1f), C at %v", berth.BlankX, xs)
	}

	_, err = fillPDFForm(path, []formEntry{{Page: 1, Anchor: "Berth", Value: strings.Repeat("W", 40)}})
	if err == nil || !strings.Contains(err.Error(), "too long for the space") {
		t.Fatalf("a value longer than the blank is refused, got %v", err)
	}
}

func TestFillPDFForm_BoxValueDecidesWhetherToTick(t *testing.T) {
	path := writeFixturePDF(t, "storm.pdf", flatFormFixture(t))
	orig, _ := readFileForTest(t, path)
	base := pageOps(t, orig)["l"]

	for _, v := range []string{"", "yes", "true", "x"} {
		res, err := fillPDFForm(path, []formEntry{{Page: 1, Box: "South Cove", Value: v}})
		if err != nil || pageOps(t, res.Data)["l"] != base+2 {
			t.Errorf("value %q ticks the box, got %v", v, err)
		}
	}
	for _, v := range []string{"no", "false", "off", "unchecked", "0"} {
		res, err := fillPDFForm(path, []formEntry{{Page: 1, Box: "South Cove", Value: v}})
		if err != nil || pageOps(t, res.Data)["l"] != base {
			t.Errorf("value %q leaves the box empty, got %v", v, err)
		}
	}
	if _, err := fillPDFForm(path, []formEntry{{Page: 1, Box: "South Cove", Value: "maybe"}}); err == nil || !strings.Contains(err.Error(), "takes yes or no") {
		t.Fatalf("a value that is neither is refused, got %v", err)
	}
	if _, err := fillPDFForm(path, []formEntry{{Page: 1, Box: "West Bay", Value: "no"}}); err == nil {
		t.Fatal("an unknown box is still an error when told to stay empty")
	}
}
