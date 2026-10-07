package main

import (
	"context"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
	pdffont "github.com/pdfcpu/pdfcpu/pkg/font"
)

// The layout of a flat PDF form: where each piece of text sits on the page
// and where the tick boxes are, in PDF points with the origin at the lower
// left of the page (the same space the stamps in pdf_form_fill.go are placed
// in). A flat form has no fields to read, so the layout is all Mate has to
// work out which blank belongs to which question.
//
// The content stream is read with the vendored ledongthuc/pdf lexer and a
// small interpreter of our own, because the library's own text extraction
// ignores word spacing, cannot read two-byte font widths and forgets the
// current transformation for rectangles. Everything here is read only.

// pdfLayoutLine is one run of text on one baseline with no wide gap in it:
// a label, a heading, a sentence of a paragraph. Y is the baseline.
type pdfLayoutLine struct {
	Text string  `json:"text"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	W    float64 `json:"w"`
	H    float64 `json:"h"`
	// BlankX and BlankW place a blank printed as a run of underscores or dot
	// leaders after the label. The run is not part of Text, and a value is
	// written where it starts. Zero when the line has no such blank.
	BlankX float64 `json:"blank_x,omitempty"`
	BlankW float64 `json:"blank_w,omitempty"`
	// fontSize is what the stamps and the box search need.
	fontSize float64
	// pureBlank marks a run with no label of its own, joined to the label
	// before it and then dropped.
	pureBlank bool
}

// A run of this many underscores or dots is a blank to write on, not text.
const pdfBlankRun = 4

func isBlankChar(s string) bool { return s == "_" || s == "." || s == "…" }

// pdfLayoutBox is an empty tick box and the text beside it. X and Y are its
// lower left corner.
type pdfLayoutBox struct {
	Label     string  `json:"label"`
	LabelSide string  `json:"label_side,omitempty"` // left or right of the box, empty when there is no label
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	W         float64 `json:"w"`
	H         float64 `json:"h"`
	// Approximate is set when the box was drawn as a font glyph whose outline
	// could not be read, so its size is estimated from the font size.
	Approximate bool `json:"approximate,omitempty"`
}

type pdfLayoutPage struct {
	Page   int             `json:"page"`
	Width  float64         `json:"width"`
	Height float64         `json:"height"`
	Rotate int             `json:"rotate,omitempty"`
	Lines  []pdfLayoutLine `json:"lines"`
	Boxes  []pdfLayoutBox  `json:"boxes"`
}

type pdfLayout struct {
	Pages []pdfLayoutPage
	// Notes are things the reader of the layout must know: text that was
	// skipped, boxes that could not be placed.
	Notes []string
}

// ── affine maths ────────────────────────────────────────────────────────

type pdfMat [6]float64

var pdfIdent = pdfMat{1, 0, 0, 1, 0, 0}

// mul is m followed by n, the order a PDF writes its cm and Tm operators in.
func (m pdfMat) mul(n pdfMat) pdfMat {
	return pdfMat{
		m[0]*n[0] + m[1]*n[2], m[0]*n[1] + m[1]*n[3],
		m[2]*n[0] + m[3]*n[2], m[2]*n[1] + m[3]*n[3],
		m[4]*n[0] + m[5]*n[2] + n[4], m[4]*n[1] + m[5]*n[3] + n[5],
	}
}

func (m pdfMat) apply(x, y float64) (float64, float64) {
	return m[0]*x + m[2]*y + m[4], m[1]*x + m[3]*y + m[5]
}

func matFromArgs(args []pdf.Value) (pdfMat, bool) {
	if len(args) != 6 {
		return pdfMat{}, false
	}
	var m pdfMat
	for i := range m {
		m[i] = args[i].Float64()
	}
	return m, true
}

// ── interpreter ─────────────────────────────────────────────────────────

type pdfGlyph struct {
	x, y, w, fs float64
	text        string
}

type pdfRect struct{ x0, y0, x1, y1 float64 }

type pdfBoxGlyph struct {
	x, y, fs float64
	bbox     *pdfRect // glyph outline in user space, nil when unreadable
}

type pdfGState struct {
	ctm        pdfMat
	tc, tw, th float64
	tl, tfs    float64
	rise       float64
	font       *pdfFontInfo
}

// pdfPageScan is everything read off one page's content.
type pdfPageScan struct {
	glyphs   []pdfGlyph
	boxGlyph []pdfBoxGlyph
	rects    []pdfRect
	rotated  int // glyphs skipped for being rotated or sheared
	problems []string
}

type pdfFontInfo struct {
	name   string // BaseFont without its subset prefix
	font   *pdf.Font
	type0  bool
	enc    pdf.TextEncoding
	ttf    *ttfFont
	gidMap []byte // CIDToGIDMap stream, nil means identity
	ttfErr string
}

type pdfScanner struct {
	page  pdf.Page
	scan  *pdfPageScan
	fonts map[string]*pdfFontInfo // keyed by the font dictionary's own text, so scopes never collide
	depth int
}

func scanPDFPage(page pdf.Page) (scan *pdfPageScan, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("read page content: %v", r)
		}
	}()
	s := &pdfScanner{page: page, scan: &pdfPageScan{}, fonts: map[string]*pdfFontInfo{}}
	if page.V.IsNull() || page.V.Key("Contents").Kind() == pdf.Null {
		return s.scan, nil
	}
	s.run(page.V.Key("Contents"), page.Resources(), pdfIdent)
	return s.scan, nil
}

func (s *pdfScanner) fontFor(res pdf.Value, name string) *pdfFontInfo {
	fv := res.Key("Font").Key(name)
	key := fv.String()
	if f, ok := s.fonts[key]; ok {
		return f
	}
	info := newPDFFontInfo(fv)
	s.fonts[key] = info
	return info
}

func (s *pdfScanner) run(stream, res pdf.Value, base pdfMat) {
	g := pdfGState{ctm: base, th: 1}
	var stack []pdfGState
	tm, tlm := pdfIdent, pdfIdent

	var pathRects []pdfRect
	pathOther := false

	showGlyphs := func(raw string) {
		if g.font == nil {
			return
		}
		codes := splitPDFCodes(raw, g.font.type0)
		for _, code := range codes {
			w0 := g.font.width(code)
			text := g.font.decode(code)
			trm := pdfMat{g.tfs * g.th, 0, 0, g.tfs, 0, g.rise}.mul(tm).mul(g.ctm)
			if math.Abs(trm[1]) > 1e-6 || math.Abs(trm[2]) > 1e-6 || trm[0] <= 0 || trm[3] <= 0 {
				if strings.TrimSpace(text) != "" {
					s.scan.rotated++
				}
			} else if g.font.isBoxGlyph(code, text) {
				s.scan.boxGlyph = append(s.scan.boxGlyph, g.font.boxAt(code, trm))
			} else if text != "" {
				s.scan.glyphs = append(s.scan.glyphs, pdfGlyph{x: trm[4], y: trm[5], w: w0 / 1000 * trm[0], fs: trm[3], text: text})
			}
			tx := w0/1000*g.tfs + g.tc
			if code == 32 && !g.font.type0 {
				tx += g.tw
			}
			tm = pdfMat{1, 0, 0, 1, tx * g.th, 0}.mul(tm)
		}
	}
	newLine := func(tx, ty float64) {
		tlm = pdfMat{1, 0, 0, 1, tx, ty}.mul(tlm)
		tm = tlm
	}

	endPath := func(stroked bool) {
		if stroked && !pathOther {
			s.scan.rects = append(s.scan.rects, pathRects...)
		}
		pathRects, pathOther = nil, false
	}

	pdf.Interpret(stream, func(stk *pdf.Stack, op string) {
		n := stk.Len()
		args := make([]pdf.Value, n)
		for i := n - 1; i >= 0; i-- {
			args[i] = stk.Pop()
		}
		num := func(i int) float64 {
			if i < len(args) {
				return args[i].Float64()
			}
			return 0
		}
		switch op {
		case "q":
			stack = append(stack, g)
		case "Q":
			if len(stack) > 0 {
				g = stack[len(stack)-1]
				stack = stack[:len(stack)-1]
			}
		case "cm":
			if m, ok := matFromArgs(args); ok {
				g.ctm = m.mul(g.ctm)
			}
		case "BT":
			tm, tlm = pdfIdent, pdfIdent
		case "Tc":
			g.tc = num(0)
		case "Tw":
			g.tw = num(0)
		case "Tz":
			g.th = num(0) / 100
		case "TL":
			g.tl = num(0)
		case "Ts":
			g.rise = num(0)
		case "Tf":
			if len(args) == 2 {
				g.font = s.fontFor(res, args[0].Name())
				g.tfs = args[1].Float64()
			}
		case "Td":
			newLine(num(0), num(1))
		case "TD":
			g.tl = -num(1)
			newLine(num(0), num(1))
		case "Tm":
			if m, ok := matFromArgs(args); ok {
				tm, tlm = m, m
			}
		case "T*":
			newLine(0, -g.tl)
		case "Tj":
			if len(args) == 1 {
				showGlyphs(args[0].RawString())
			}
		case "'":
			newLine(0, -g.tl)
			if len(args) == 1 {
				showGlyphs(args[0].RawString())
			}
		case "\"":
			if len(args) == 3 {
				g.tw, g.tc = num(0), num(1)
				newLine(0, -g.tl)
				showGlyphs(args[2].RawString())
			}
		case "TJ":
			if len(args) == 1 {
				arr := args[0]
				for i := 0; i < arr.Len(); i++ {
					el := arr.Index(i)
					if el.Kind() == pdf.String {
						showGlyphs(el.RawString())
					} else {
						tm = pdfMat{1, 0, 0, 1, -el.Float64() / 1000 * g.tfs * g.th, 0}.mul(tm)
					}
				}
			}
		case "re":
			if len(args) == 4 {
				x, y, w, h := num(0), num(1), num(2), num(3)
				x0, y0 := g.ctm.apply(x, y)
				x1, y1 := g.ctm.apply(x+w, y+h)
				// A rotated rectangle is not a tick box.
				if math.Abs(g.ctm[1]) > 1e-6 || math.Abs(g.ctm[2]) > 1e-6 {
					pathOther = true
				} else {
					pathRects = append(pathRects, pdfRect{math.Min(x0, x1), math.Min(y0, y1), math.Max(x0, x1), math.Max(y0, y1)})
				}
			}
		case "m", "l", "c", "v", "y", "h":
			if op != "h" {
				pathOther = true
			}
		case "S", "s", "B", "B*", "b", "b*":
			endPath(true)
		case "f", "F", "f*", "n":
			endPath(false)
		case "Do":
			if len(args) == 1 && s.depth < 4 {
				xo := res.Key("XObject").Key(args[0].Name())
				if xo.Key("Subtype").Name() == "Form" {
					m := pdfIdent
					if mv := xo.Key("Matrix"); mv.Kind() == pdf.Array && mv.Len() == 6 {
						vals := make([]pdf.Value, 6)
						for i := range vals {
							vals[i] = mv.Index(i)
						}
						m, _ = matFromArgs(vals)
					}
					xres := xo.Key("Resources")
					if xres.IsNull() {
						xres = res
					}
					s.depth++
					s.run(xo, xres, m.mul(g.ctm))
					s.depth--
				}
			}
		}
	})
}

// ── fonts ───────────────────────────────────────────────────────────────

func newPDFFontInfo(fv pdf.Value) (info *pdfFontInfo) {
	f := pdf.Font{V: fv}
	info = &pdfFontInfo{font: &f}
	name := f.BaseFont()
	if i := strings.Index(name, "+"); i >= 0 {
		name = name[i+1:]
	}
	info.name = name
	info.type0 = fv.Key("Subtype").Name() == "Type0"
	defer func() {
		if r := recover(); r != nil {
			info.enc = nil
		}
	}()
	info.enc = f.Encoder()
	return info
}

// splitPDFCodes cuts a shown string into character codes: one byte each, or
// two for a composite (Type0) font.
func splitPDFCodes(raw string, type0 bool) []int {
	if !type0 {
		out := make([]int, len(raw))
		for i := 0; i < len(raw); i++ {
			out[i] = int(raw[i])
		}
		return out
	}
	out := make([]int, 0, len(raw)/2)
	for i := 0; i+1 < len(raw); i += 2 {
		out = append(out, int(raw[i])<<8|int(raw[i+1]))
	}
	return out
}

func (f *pdfFontInfo) decode(code int) (text string) {
	defer func() {
		if r := recover(); r != nil {
			text = ""
		}
	}()
	if f.enc == nil {
		return ""
	}
	raw := string([]byte{byte(code)})
	if f.type0 {
		raw = string([]byte{byte(code >> 8), byte(code)})
	}
	return f.enc.Decode(raw)
}

// width is the glyph advance in thousandths of an em.
func (f *pdfFontInfo) width(code int) float64 {
	if !f.type0 {
		if w := f.font.Width(code); w != 0 {
			return w
		}
		// The standard 14 fonts need no /Widths array, so a form using
		// Helvetica carries none; their metrics are built into pdfcpu.
		if f.font.V.Key("Widths").IsNull() && pdffont.IsCoreFont(f.name) {
			if r, _ := utf8.DecodeRuneInString(f.decode(code)); r != utf8.RuneError {
				if w, err := pdffont.CharWidth(context.Background(), f.name, r); err == nil {
					return float64(w)
				}
			}
		}
		return 0
	}
	d := f.font.V.Key("DescendantFonts").Index(0)
	w := d.Key("DW")
	dw := 1000.0
	if !w.IsNull() {
		dw = w.Float64()
	}
	arr := d.Key("W")
	for i := 0; i < arr.Len(); {
		first := int(arr.Index(i).Float64())
		next := arr.Index(i + 1)
		if next.Kind() == pdf.Array {
			if code >= first && code < first+next.Len() {
				return next.Index(code - first).Float64()
			}
			i += 2
			continue
		}
		last := int(next.Float64())
		if code >= first && code <= last {
			return arr.Index(i + 2).Float64()
		}
		i += 3
	}
	return dw
}

// Box glyphs: the characters a form uses for an empty tick box.
var pdfBoxRunes = map[rune]bool{'□': true, '☐': true, '▢': true, '❏': true, '❐': true, '❑': true, '❒': true, '◻': true, '⬜': true}

// Wingdings keeps its symbols in the private use area; these are its empty
// squares (o, p, q, r and 0xA8).
var pdfWingdingsBoxes = map[rune]bool{0xF06F: true, 0xF070: true, 0xF071: true, 0xF072: true, 0xF0A8: true}

func (f *pdfFontInfo) isBoxGlyph(_ int, text string) bool {
	r, size := utf8.DecodeRuneInString(text)
	if size == 0 || size != len(text) {
		return false
	}
	if pdfBoxRunes[r] {
		return true
	}
	return pdfWingdingsBoxes[r] && strings.Contains(strings.ToLower(f.name), "wingdings")
}

// boxAt places a box glyph. With a readable TrueType outline the box is the
// glyph's own bounding box; otherwise it is estimated from the font size and
// flagged.
func (f *pdfFontInfo) boxAt(code int, trm pdfMat) pdfBoxGlyph {
	b := pdfBoxGlyph{x: trm[4], y: trm[5], fs: trm[3]}
	if r, ok := f.glyphBBox(code); ok {
		b.bbox = &pdfRect{
			x0: trm[4] + r.x0*trm[0], y0: trm[5] + r.y0*trm[3],
			x1: trm[4] + r.x1*trm[0], y1: trm[5] + r.y1*trm[3],
		}
	}
	return b
}

// glyphBBox is the glyph's bounding box in em units, read from the embedded
// TrueType program.
func (f *pdfFontInfo) glyphBBox(code int) (pdfRect, bool) {
	if f.ttf == nil && f.ttfErr == "" {
		f.loadFontProgram()
	}
	if f.ttf == nil {
		return pdfRect{}, false
	}
	gid := code
	if f.type0 && f.gidMap != nil {
		if 2*code+1 >= len(f.gidMap) {
			return pdfRect{}, false
		}
		gid = int(f.gidMap[2*code])<<8 | int(f.gidMap[2*code+1])
	} else if !f.type0 {
		// A simple TrueType font needs its cmap to turn a code into a glyph;
		// not read here.
		return pdfRect{}, false
	}
	return f.ttf.bbox(gid)
}

func (f *pdfFontInfo) loadFontProgram() {
	defer func() {
		if r := recover(); r != nil {
			f.ttfErr = fmt.Sprint(r)
		}
	}()
	f.ttfErr = "none"
	fd := f.font.V.Key("FontDescriptor")
	if f.type0 {
		d := f.font.V.Key("DescendantFonts").Index(0)
		fd = d.Key("FontDescriptor")
		if g := d.Key("CIDToGIDMap"); g.Kind() == pdf.Stream {
			if data, err := io.ReadAll(g.Reader()); err == nil {
				f.gidMap = data
			}
		}
	}
	ff := fd.Key("FontFile2")
	if ff.Kind() != pdf.Stream {
		return
	}
	data, err := io.ReadAll(ff.Reader())
	if err != nil {
		f.ttfErr = err.Error()
		return
	}
	t, err := parseTTF(data)
	if err != nil {
		f.ttfErr = err.Error()
		return
	}
	f.ttf, f.ttfErr = t, ""
}

// ── a minimal TrueType reader: glyph bounding boxes only ────────────────

type ttfFont struct {
	data       []byte
	unitsPerEm float64
	locaLong   bool
	loca, glyf []byte
}

func be16(b []byte) int { return int(b[0])<<8 | int(b[1]) }
func be32(b []byte) int { return int(b[0])<<24 | int(b[1])<<16 | int(b[2])<<8 | int(b[3]) }

func parseTTF(data []byte) (*ttfFont, error) {
	if len(data) < 12 {
		return nil, fmt.Errorf("font program too short")
	}
	n := be16(data[4:])
	tables := map[string][]byte{}
	for i := 0; i < n; i++ {
		e := 12 + 16*i
		if e+16 > len(data) {
			return nil, fmt.Errorf("font table directory runs off the end")
		}
		off, length := be32(data[e+8:]), be32(data[e+12:])
		if off < 0 || length < 0 || off+length > len(data) {
			return nil, fmt.Errorf("font table out of range")
		}
		tables[string(data[e:e+4])] = data[off : off+length]
	}
	head, loca, glyf := tables["head"], tables["loca"], tables["glyf"]
	if len(head) < 54 || loca == nil || glyf == nil {
		return nil, fmt.Errorf("font program has no outlines")
	}
	t := &ttfFont{data: data, unitsPerEm: float64(be16(head[18:])), locaLong: be16(head[50:]) != 0, loca: loca, glyf: glyf}
	if t.unitsPerEm == 0 {
		return nil, fmt.Errorf("font program has no units per em")
	}
	return t, nil
}

func (t *ttfFont) bbox(gid int) (pdfRect, bool) {
	var start, end int
	if t.locaLong {
		if 4*gid+8 > len(t.loca) {
			return pdfRect{}, false
		}
		start, end = be32(t.loca[4*gid:]), be32(t.loca[4*gid+4:])
	} else {
		if 2*gid+4 > len(t.loca) {
			return pdfRect{}, false
		}
		start, end = 2*be16(t.loca[2*gid:]), 2*be16(t.loca[2*gid+2:])
	}
	if end <= start || start+10 > len(t.glyf) {
		return pdfRect{}, false
	}
	g := t.glyf[start:]
	s16 := func(b []byte) float64 { return float64(int16(be16(b))) }
	return pdfRect{
		x0: s16(g[2:]) / t.unitsPerEm, y0: s16(g[4:]) / t.unitsPerEm,
		x1: s16(g[6:]) / t.unitsPerEm, y1: s16(g[8:]) / t.unitsPerEm,
	}, true
}

// ── from glyphs to lines and boxes ──────────────────────────────────────

// Tick boxes drawn as rectangles are small, roughly square and stroked.
const (
	pdfBoxMin = 4.0
	pdfBoxMax = 22.0
	// A label further than this from its box is not its label.
	pdfLabelReach = 24.0
)

func round1(v float64) float64 { return math.Round(v*10) / 10 }

// boxesOf turns a scan's glyph and rectangle findings into boxes.
func boxesOf(scan *pdfPageScan) []pdfLayoutBox {
	var out []pdfLayoutBox
	for _, g := range scan.boxGlyph {
		if g.bbox != nil {
			out = append(out, pdfLayoutBox{X: g.bbox.x0, Y: g.bbox.y0, W: g.bbox.x1 - g.bbox.x0, H: g.bbox.y1 - g.bbox.y0})
			continue
		}
		side := 0.7 * g.fs
		out = append(out, pdfLayoutBox{X: g.x, Y: g.y + 0.1*g.fs, W: side, H: side, Approximate: true})
	}
	for _, r := range scan.rects {
		w, h := r.x1-r.x0, r.y1-r.y0
		if w < pdfBoxMin || w > pdfBoxMax || h < pdfBoxMin || h > pdfBoxMax {
			continue
		}
		if math.Abs(w-h) > math.Max(1, 0.15*w) {
			continue
		}
		out = append(out, pdfLayoutBox{X: r.x0, Y: r.y0, W: w, H: h})
	}
	return out
}

// linesOf groups glyphs into lines: same baseline, no wide gap, and no tick
// box in the gap.
func linesOf(glyphs []pdfGlyph, boxes []pdfLayoutBox) []pdfLayoutLine {
	if len(glyphs) == 0 {
		return nil
	}
	gs := append([]pdfGlyph(nil), glyphs...)
	sort.SliceStable(gs, func(i, j int) bool { return gs[i].y > gs[j].y })

	var clusters [][]pdfGlyph
	for _, g := range gs {
		n := len(clusters)
		if n > 0 && math.Abs(clusters[n-1][0].y-g.y) <= 0.3*math.Max(g.fs, clusters[n-1][0].fs) {
			clusters[n-1] = append(clusters[n-1], g)
			continue
		}
		clusters = append(clusters, []pdfGlyph{g})
	}

	boxBetween := func(x0, x1, y, fs float64) bool {
		for _, b := range boxes {
			mid := y + 0.35*fs
			if mid < b.Y-2 || mid > b.Y+b.H+2 {
				continue
			}
			if b.X >= x0-1 && b.X+b.W <= x1+1 {
				return true
			}
		}
		return false
	}

	var lines []pdfLayoutLine
	for _, c := range clusters {
		sort.SliceStable(c, func(i, j int) bool { return c[i].x < c[j].x })
		var cur *pdfLayoutLine
		var sb strings.Builder
		var lastEnd, ySum, blankX, labelEnd float64
		var count, blankN int
		blankAt := -1
		flush := func() {
			if cur == nil {
				return
			}
			text := strings.TrimSpace(sb.String())
			cur.W = lastEnd - cur.X
			if blankAt >= 0 && blankN >= pdfBlankRun {
				cur.BlankX, cur.BlankW = blankX, lastEnd-blankX
				text = strings.TrimSpace(sb.String()[:blankAt])
				cur.W = labelEnd - cur.X
				if text == "" {
					cur.pureBlank, text = true, "_"
				}
			}
			if text != "" {
				cur.Text = text
				cur.Y = ySum / float64(count)
				lines = append(lines, *cur)
			}
			cur, lastEnd, ySum, count = nil, 0, 0, 0
			blankAt, blankN, labelEnd = -1, 0, 0
			sb.Reset()
		}
		blankAt = -1
		for _, g := range c {
			isSpace := strings.TrimSpace(g.text) == ""
			if cur != nil {
				gap := g.x - lastEnd
				if gap > 1.0*math.Max(g.fs, cur.fontSize) || boxBetween(lastEnd, g.x, g.y, g.fs) {
					flush()
				} else if gap > 0.25*g.fs && !strings.HasSuffix(sb.String(), " ") && !isSpace {
					sb.WriteString(" ")
				}
			}
			if cur == nil {
				if isSpace {
					continue
				}
				cur = &pdfLayoutLine{X: g.x, fontSize: g.fs}
			}
			if isBlankChar(g.text) {
				if blankAt < 0 {
					blankAt, blankX, blankN = sb.Len(), g.x, 0
				}
				blankN++
			} else if !isSpace {
				blankAt, blankN, labelEnd = -1, 0, g.x+g.w
			}
			sb.WriteString(g.text)
			if !isSpace {
				lastEnd = g.x + g.w
			}
			if g.fs > cur.fontSize {
				cur.fontSize = g.fs
			}
			ySum += g.y
			count++
		}
		flush()
	}
	// A blank printed on its own belongs to the label just before it on the
	// same baseline; either way it is not a line to write after.
	for i := range lines {
		if !lines[i].pureBlank {
			continue
		}
		best, bestGap := -1, math.Inf(1)
		for j := range lines {
			l := lines[j]
			if l.pureBlank || l.BlankW > 0 || math.Abs(l.Y-lines[i].Y) > 0.5*l.fontSize {
				continue
			}
			if gap := lines[i].BlankX - (l.X + l.W); gap > -1 && gap < bestGap && gap <= 8*l.fontSize {
				best, bestGap = j, gap
			}
		}
		if best >= 0 {
			lines[best].BlankX, lines[best].BlankW = lines[i].BlankX, lines[i].BlankW
		}
	}
	kept := lines[:0]
	for _, l := range lines {
		if !l.pureBlank {
			kept = append(kept, l)
		}
	}
	lines = kept
	for i := range lines {
		lines[i].H = lines[i].fontSize
	}
	sort.SliceStable(lines, func(i, j int) bool {
		if math.Abs(lines[i].Y-lines[j].Y) > 0.3*lines[i].fontSize {
			return lines[i].Y > lines[j].Y
		}
		return lines[i].X < lines[j].X
	})
	return lines
}

// labelBoxes names each box by the text next to it: the nearer of the line
// ending at its left edge and the line starting at its right edge, with no
// other box in between.
func labelBoxes(boxes []pdfLayoutBox, lines []pdfLayoutLine) {
	for i := range boxes {
		b := &boxes[i]
		inBand := func(l pdfLayoutLine) bool {
			mid := l.Y + 0.35*l.fontSize
			return mid >= b.Y-2 && mid <= b.Y+b.H+2
		}
		otherBoxBetween := func(x0, x1 float64) bool {
			for j, o := range boxes {
				if j == i {
					continue
				}
				if o.Y < b.Y+b.H && o.Y+o.H > b.Y && o.X >= x0-1 && o.X+o.W <= x1+1 {
					return true
				}
			}
			return false
		}
		bestLeft, bestRight := -1, -1
		gapLeft, gapRight := math.Inf(1), math.Inf(1)
		for k, l := range lines {
			if !inBand(l) {
				continue
			}
			if end := l.X + l.W; end <= b.X+1 {
				if gap := b.X - end; gap < gapLeft && !otherBoxBetween(end, b.X) {
					gapLeft, bestLeft = gap, k
				}
			}
			if l.X >= b.X+b.W-1 {
				if gap := l.X - (b.X + b.W); gap < gapRight && !otherBoxBetween(b.X+b.W, l.X) {
					gapRight, bestRight = gap, k
				}
			}
		}
		switch {
		case bestLeft >= 0 && gapLeft <= pdfLabelReach && gapLeft <= gapRight:
			b.Label, b.LabelSide = lines[bestLeft].Text, "left"
		case bestRight >= 0 && gapRight <= pdfLabelReach:
			b.Label, b.LabelSide = lines[bestRight].Text, "right"
		}
	}
}

// pageValue reads an inheritable page attribute.
func pdfInherited(page pdf.Page, key string) pdf.Value {
	for v, i := page.V, 0; v.Kind() == pdf.Dict && i < 32; v, i = v.Key("Parent"), i+1 {
		if x := v.Key(key); !x.IsNull() {
			return x
		}
	}
	return pdf.Value{}
}

// analysePDFLayout reads every page of a PDF on disk.
func analysePDFLayout(path string) (layout *pdfLayout, err error) {
	defer func() {
		if r := recover(); r != nil {
			layout, err = nil, fmt.Errorf("read PDF: %v", r)
		}
	}()
	f, reader, err := openPDFReader(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	pages, err := readPDFPageCount(reader)
	if err != nil {
		return nil, err
	}

	layout = &pdfLayout{}
	for n := 1; n <= pages; n++ {
		page := reader.Page(n)
		pl := pdfLayoutPage{Page: n, Lines: []pdfLayoutLine{}, Boxes: []pdfLayoutBox{}}
		if mb := pdfInherited(page, "MediaBox"); mb.Kind() == pdf.Array && mb.Len() == 4 {
			pl.Width = mb.Index(2).Float64() - mb.Index(0).Float64()
			pl.Height = mb.Index(3).Float64() - mb.Index(1).Float64()
		}
		if r := pdfInherited(page, "Rotate"); !r.IsNull() {
			pl.Rotate = int(r.Int64())
		}
		scan, serr := scanPDFPage(page)
		if serr != nil {
			return nil, fmt.Errorf("page %d: %w", n, serr)
		}
		boxes := boxesOf(scan)
		lines := linesOf(scan.glyphs, boxes)
		labelBoxes(boxes, lines)
		sort.SliceStable(boxes, func(i, j int) bool {
			if math.Abs(boxes[i].Y-boxes[j].Y) > 3 {
				return boxes[i].Y > boxes[j].Y
			}
			return boxes[i].X < boxes[j].X
		})
		for _, l := range lines {
			l.X, l.Y, l.W, l.H = round1(l.X), round1(l.Y), round1(l.W), round1(l.H)
			l.BlankX, l.BlankW = round1(l.BlankX), round1(l.BlankW)
			pl.Lines = append(pl.Lines, l)
		}
		for _, b := range boxes {
			b.X, b.Y, b.W, b.H = round1(b.X), round1(b.Y), round1(b.W), round1(b.H)
			pl.Boxes = append(pl.Boxes, b)
		}
		if scan.rotated > 0 {
			layout.Notes = append(layout.Notes, fmt.Sprintf("Page %d: %d rotated characters (margin text) are not listed.", n, scan.rotated))
		}
		if pl.Rotate != 0 {
			layout.Notes = append(layout.Notes, fmt.Sprintf("Page %d is rotated %d degrees; positions are in the page's own unrotated space and it cannot be filled in.", n, pl.Rotate))
		}
		approx := 0
		for _, b := range pl.Boxes {
			if b.Approximate {
				approx++
			}
		}
		if approx > 0 {
			layout.Notes = append(layout.Notes, fmt.Sprintf("Page %d: %d tick boxes are drawn as font symbols whose outline could not be read, so their size is estimated.", n, approx))
		}
		layout.Pages = append(layout.Pages, pl)
	}
	return layout, nil
}
