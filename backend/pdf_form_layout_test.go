package main

import (
	"math"
	"testing"
)

func layoutOf(t *testing.T, data []byte) *pdfLayout {
	t.Helper()
	l, err := analysePDFLayout(writeFixturePDF(t, "form.pdf", data))
	if err != nil {
		t.Fatalf("analysePDFLayout: %v", err)
	}
	return l
}

func (p pdfLayoutPage) line(text string) (pdfLayoutLine, bool) {
	for _, l := range p.Lines {
		if l.Text == text {
			return l, true
		}
	}
	return pdfLayoutLine{}, false
}

func (p pdfLayoutPage) box(label string) (pdfLayoutBox, bool) {
	for _, b := range p.Boxes {
		if b.Label == label {
			return b, true
		}
	}
	return pdfLayoutBox{}, false
}

func TestAnalysePDFLayout_FlatFormLinesAndBoxes(t *testing.T) {
	l := layoutOf(t, flatFormFixture(t))
	if len(l.Pages) != 1 {
		t.Fatalf("expected one page, got %d", len(l.Pages))
	}
	p := l.Pages[0]
	if math.Abs(p.Width-595.28) > 1 || math.Abs(p.Height-841.89) > 1 {
		t.Fatalf("expected an A4 page, got %.1f x %.1f", p.Width, p.Height)
	}
	owner, ok := p.line("Vessel owner")
	if !ok {
		t.Fatalf("expected the Vessel owner line, got %+v", p.Lines)
	}
	if math.Abs(owner.X-50) > 1.5 || math.Abs(owner.Y-730) > 12 || owner.W < 40 || owner.H < 9 {
		t.Fatalf("Vessel owner is at x=50 y=730 about 11pt, got %+v", owner)
	}
	if len(p.Lines) < 7 {
		t.Fatalf("expected every text line, got %d: %+v", len(p.Lines), p.Lines)
	}
	// Top to bottom.
	for i := 1; i < len(p.Lines); i++ {
		if p.Lines[i].Y > p.Lines[i-1].Y+1 {
			t.Fatalf("lines are listed top to bottom, got %+v", p.Lines)
		}
	}

	if len(p.Boxes) != 2 {
		t.Fatalf("expected the two tick boxes, got %+v", p.Boxes)
	}
	north, ok := p.box("North Harbour")
	if !ok {
		t.Fatalf("expected a box labelled North Harbour, got %+v", p.Boxes)
	}
	if north.LabelSide != "right" || math.Abs(north.X-50) > 1.5 || math.Abs(north.W-10) > 1.5 || math.Abs(north.H-10) > 1.5 || math.Abs(north.Y-628) > 1.5 {
		t.Fatalf("North Harbour box should be 10x10 at 50,628 labelled from the right, got %+v", north)
	}
	if _, ok := p.box("South Cove"); !ok {
		t.Fatalf("expected a box labelled South Cove, got %+v", p.Boxes)
	}
}

func TestAnalysePDFLayout_AcroFormPageStillHasItsText(t *testing.T) {
	l := layoutOf(t, acroFormFixture(t))
	p := l.Pages[0]
	if _, ok := p.line("Owner name"); !ok {
		t.Fatalf("expected the Owner name label, got %+v", p.Lines)
	}
}

func TestAnalysePDFLayout_NotAPDFFailsPlainly(t *testing.T) {
	if _, err := analysePDFLayout(writeFixturePDF(t, "x.pdf", []byte("not a pdf"))); err == nil {
		t.Fatal("expected an error for a file that is not a PDF")
	}
}

// A font program with two glyphs, the second a box 0.1 to 0.9 em wide and
// 0.2 to 0.8 em tall. This is how a glyph tick box is sized: from its own
// outline.
func tinyTTF() []byte {
	be16 := func(v int) []byte { return []byte{byte(v >> 8), byte(v)} }
	be32 := func(v int) []byte { return []byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)} }
	head := make([]byte, 54)
	copy(head[18:], be16(1000)) // units per em
	copy(head[50:], be16(0))    // short loca
	loca := append(append(be16(0), be16(0)...), be16(6)...)
	glyf := append(append(append(append(append(be16(1), be16(100)...), be16(200)...), be16(900)...), be16(800)...), be16(0)...)

	out := append(be32(0x00010000), be16(3)...)
	out = append(out, make([]byte, 6)...)
	off := 12 + 16*3
	var body []byte
	for _, t := range []struct {
		tag  string
		data []byte
	}{{"head", head}, {"loca", loca}, {"glyf", glyf}} {
		out = append(out, []byte(t.tag)...)
		out = append(out, be32(0)...)
		out = append(out, be32(off+len(body))...)
		out = append(out, be32(len(t.data))...)
		body = append(body, t.data...)
	}
	return append(out, body...)
}

func TestParseTTF_GlyphBoundingBox(t *testing.T) {
	f, err := parseTTF(tinyTTF())
	if err != nil {
		t.Fatalf("parseTTF: %v", err)
	}
	r, ok := f.bbox(1)
	if !ok || math.Abs(r.x0-0.1) > 1e-9 || math.Abs(r.y0-0.2) > 1e-9 || math.Abs(r.x1-0.9) > 1e-9 || math.Abs(r.y1-0.8) > 1e-9 {
		t.Fatalf("expected the box 0.1,0.2 to 0.9,0.8, got %+v ok=%v", r, ok)
	}
	if _, ok := f.bbox(0); ok {
		t.Error("an empty glyph has no box")
	}
	if _, ok := f.bbox(9); ok {
		t.Error("a glyph past the end of the font has no box")
	}
	if _, err := parseTTF([]byte("nope")); err == nil {
		t.Error("a short file is not a font")
	}
}

func TestBoxesOf_GlyphBoxUsesItsOutlineAndRectsNeedToBeSquareAndStroked(t *testing.T) {
	scan := &pdfPageScan{
		boxGlyph: []pdfBoxGlyph{
			{x: 100, y: 500, fs: 12, bbox: &pdfRect{x0: 101, y0: 502, x1: 109.7, y1: 510.7}},
			{x: 200, y: 500, fs: 10}, // outline unreadable: estimated and flagged
		},
		rects: []pdfRect{
			{x0: 300, y0: 500, x1: 310, y1: 510}, // a box
			{x0: 300, y0: 400, x1: 500, y1: 414}, // a table cell
			{x0: 300, y0: 300, x1: 302, y1: 302}, // a bullet
		},
	}
	boxes := boxesOf(scan)
	if len(boxes) != 3 {
		t.Fatalf("expected the two glyph boxes and the one square, got %+v", boxes)
	}
	if boxes[0].Approximate || math.Abs(boxes[0].W-8.7) > 0.01 {
		t.Errorf("a readable outline sizes the box exactly, got %+v", boxes[0])
	}
	if !boxes[1].Approximate || math.Abs(boxes[1].W-7) > 0.01 {
		t.Errorf("an unreadable outline gives an estimated, flagged box, got %+v", boxes[1])
	}
}
