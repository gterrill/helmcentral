package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	pdffont "github.com/pdfcpu/pdfcpu/pkg/font"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/form"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// Reading and filling PDF forms (ADR 0165). A form with AcroForm fields is
// filled through pdfcpu's own form support. A flat form, with no fields, is
// filled by writing text and tick marks onto the page at the positions the
// layout reader (pdf_form_layout.go) found, in plain Helvetica.

func init() {
	// pdfcpu keeps a config file under the user's config directory by default;
	// a service on a boat has no business writing one.
	model.ConfigPath = "disable"
}

// pdfFormField is one AcroForm field as Mate sees it.
type pdfFormField struct {
	Name     string   `json:"name"`
	Label    string   `json:"label,omitempty"` // the field's tooltip, when the form has one
	Kind     string   `json:"kind"`            // text, date, checkbox, radio, combobox, listbox
	Options  []string `json:"options,omitempty"`
	Value    any      `json:"value"`
	ReadOnly bool     `json:"read_only,omitempty"`
	Page     int      `json:"page,omitempty"`
}

func formName(id, name string) string {
	if name != "" {
		return name
	}
	return id
}

func firstPage(pages []int) int {
	if len(pages) == 0 {
		return 0
	}
	return pages[0]
}

func pdfConf(cmd model.CommandMode) *model.Configuration {
	conf := model.NewDefaultConfiguration()
	conf.Cmd = cmd
	return conf
}

// exportAcroForm reads the form group, nil when the PDF has no form fields.
func exportAcroForm(data []byte) (g *form.FormGroup, err error) {
	defer func() {
		if r := recover(); r != nil {
			g, err = nil, fmt.Errorf("read form fields: %v", r)
		}
	}()
	g, err = api.ExportForm(context.Background(), bytes.NewReader(data), "form.pdf", pdfConf(model.EXPORTFORMFIELDS))
	if err != nil {
		if strings.Contains(err.Error(), "no form") {
			return nil, nil
		}
		return nil, fmt.Errorf("read form fields: %w", err)
	}
	if len(g.Forms) == 0 {
		return nil, nil
	}
	return g, nil
}

// listAcroFormFields lists the fields of a PDF, empty when it has none.
// Signature fields are never listed: they are the operator's to sign.
func listAcroFormFields(data []byte) ([]pdfFormField, error) {
	g, err := exportAcroForm(data)
	if err != nil || g == nil {
		return nil, err
	}
	f := g.Forms[0]
	var out []pdfFormField
	for _, x := range f.TextFields {
		out = append(out, pdfFormField{Name: formName(x.ID, x.Name), Label: x.AltName, Kind: "text", Value: x.Value, ReadOnly: x.Locked, Page: firstPage(x.Pages)})
	}
	for _, x := range f.DateFields {
		out = append(out, pdfFormField{Name: formName(x.ID, x.Name), Label: x.AltName, Kind: "date", Value: x.Value, ReadOnly: x.Locked, Page: firstPage(x.Pages)})
	}
	for _, x := range f.CheckBoxes {
		out = append(out, pdfFormField{Name: formName(x.ID, x.Name), Label: x.AltName, Kind: "checkbox", Value: x.Value, ReadOnly: x.Locked, Page: firstPage(x.Pages)})
	}
	for _, x := range f.RadioButtonGroups {
		out = append(out, pdfFormField{Name: formName(x.ID, x.Name), Label: x.AltName, Kind: "radio", Options: x.Options, Value: x.Value, ReadOnly: x.Locked, Page: firstPage(x.Pages)})
	}
	for _, x := range f.ComboBoxes {
		out = append(out, pdfFormField{Name: formName(x.ID, x.Name), Label: x.AltName, Kind: "combobox", Options: x.Options, Value: x.Value, ReadOnly: x.Locked, Page: firstPage(x.Pages)})
	}
	for _, x := range f.ListBoxes {
		var v any = ""
		if len(x.Values) > 0 {
			v = x.Values[0]
		}
		out = append(out, pdfFormField{Name: formName(x.ID, x.Name), Label: x.AltName, Kind: "listbox", Options: x.Options, Value: v, ReadOnly: x.Locked, Page: firstPage(x.Pages)})
	}
	// A fixed order, so reading a long list on by offset never skips or repeats.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Page != out[j].Page {
			return out[i].Page < out[j].Page
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// formEntry is one thing Mate asks to be filled in: an AcroForm field, the
// text after a label on a flat form, or a tick box.
type formEntry struct {
	Field      string `json:"field,omitempty"`
	Page       int    `json:"page,omitempty"`
	Anchor     string `json:"anchor,omitempty"`
	Box        string `json:"box,omitempty"`
	Occurrence int    `json:"occurrence,omitempty"`
	Value      string `json:"value,omitempty"`
}

func (e formEntry) describe() string {
	switch {
	case e.Field != "":
		return fmt.Sprintf("field %q", e.Field)
	case e.Anchor != "":
		return fmt.Sprintf("anchor %q on page %d", e.Anchor, e.Page)
	default:
		return fmt.Sprintf("box %q on page %d", e.Box, e.Page)
	}
}

func parseTick(v string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "yes", "on", "checked", "x", "1":
		return true, nil
	case "false", "no", "off", "unchecked", "", "0":
		return false, nil
	}
	return false, fmt.Errorf("a check box takes yes or no, not %q", v)
}

// fillAcroForm sets the named fields and returns the new PDF. Every entry is
// checked before anything is written.
func fillAcroForm(data []byte, entries []formEntry) (out []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			out, err = nil, fmt.Errorf("fill form fields: %v", r)
		}
	}()
	g, err := exportAcroForm(data)
	if err != nil {
		return nil, err
	}
	if g == nil {
		return nil, fmt.Errorf("this PDF has no form fields; use page, anchor and value to write onto a flat form instead")
	}
	src := g.Forms[0]
	var send form.Form
	var problems []string
	seen := map[string]bool{}
	for _, e := range entries {
		matches := func(id, name string) bool { return e.Field == id || (name != "" && e.Field == name) }
		if seen[e.Field] {
			problems = append(problems, fmt.Sprintf("%s is given twice", e.describe()))
			continue
		}
		seen[e.Field] = true
		found := false
		for _, x := range src.TextFields {
			if matches(x.ID, x.Name) {
				found = true
				if x.Locked {
					problems = append(problems, fmt.Sprintf("%s is read only", e.describe()))
				} else {
					send.TextFields = append(send.TextFields, &form.TextField{ID: x.ID, Name: x.Name, Value: e.Value})
				}
			}
		}
		for _, x := range src.DateFields {
			if matches(x.ID, x.Name) {
				found = true
				if x.Locked {
					problems = append(problems, fmt.Sprintf("%s is read only", e.describe()))
				} else {
					send.DateFields = append(send.DateFields, &form.DateField{ID: x.ID, Name: x.Name, Format: x.Format, Value: e.Value})
				}
			}
		}
		for _, x := range src.CheckBoxes {
			if matches(x.ID, x.Name) {
				found = true
				on, perr := parseTick(e.Value)
				switch {
				case x.Locked:
					problems = append(problems, fmt.Sprintf("%s is read only", e.describe()))
				case perr != nil:
					problems = append(problems, fmt.Sprintf("%s: %v", e.describe(), perr))
				default:
					send.CheckBoxes = append(send.CheckBoxes, &form.CheckBox{ID: x.ID, Name: x.Name, Value: on})
				}
			}
		}
		for _, x := range src.RadioButtonGroups {
			if matches(x.ID, x.Name) {
				found = true
				switch {
				case x.Locked:
					problems = append(problems, fmt.Sprintf("%s is read only", e.describe()))
				case !containsString(x.Options, e.Value):
					problems = append(problems, fmt.Sprintf("%s: %q is not one of its options (%s)", e.describe(), e.Value, strings.Join(x.Options, ", ")))
				default:
					send.RadioButtonGroups = append(send.RadioButtonGroups, &form.RadioButtonGroup{ID: x.ID, Name: x.Name, Options: x.Options, Value: e.Value})
				}
			}
		}
		for _, x := range src.ComboBoxes {
			if matches(x.ID, x.Name) {
				found = true
				switch {
				case x.Locked:
					problems = append(problems, fmt.Sprintf("%s is read only", e.describe()))
				case !x.Editable && !containsString(x.Options, e.Value):
					problems = append(problems, fmt.Sprintf("%s: %q is not one of its options (%s)", e.describe(), e.Value, strings.Join(x.Options, ", ")))
				default:
					send.ComboBoxes = append(send.ComboBoxes, &form.ComboBox{ID: x.ID, Name: x.Name, Editable: x.Editable, Options: x.Options, Value: e.Value})
				}
			}
		}
		for _, x := range src.ListBoxes {
			if matches(x.ID, x.Name) {
				found = true
				switch {
				case x.Locked:
					problems = append(problems, fmt.Sprintf("%s is read only", e.describe()))
				case !containsString(x.Options, e.Value):
					problems = append(problems, fmt.Sprintf("%s: %q is not one of its options (%s)", e.describe(), e.Value, strings.Join(x.Options, ", ")))
				default:
					send.ListBoxes = append(send.ListBoxes, &form.ListBox{ID: x.ID, Name: x.Name, Multi: x.Multi, Options: x.Options, Values: []string{e.Value}})
				}
			}
		}
		if !found {
			names := []string{}
			fields, _ := listAcroFormFields(data)
			for _, f := range fields {
				names = append(names, f.Name)
			}
			problems = append(problems, fmt.Sprintf("%s: no such field; the fields are %s. Signature fields are never filled in", e.describe(), strings.Join(names, ", ")))
		}
	}
	if len(problems) > 0 {
		return nil, errors.New(strings.Join(problems, "; "))
	}

	payload, err := json.Marshal(form.FormGroup{Header: g.Header, Forms: []form.Form{send}})
	if err != nil {
		return nil, fmt.Errorf("encode form values: %w", err)
	}
	var buf bytes.Buffer
	if err := api.FillForm(context.Background(), bytes.NewReader(data), bytes.NewReader(payload), &buf, pdfConf(model.FILLFORMFIELDS)); err != nil {
		return nil, fmt.Errorf("fill form fields: %w", err)
	}
	return buf.Bytes(), nil
}

// ── flat forms: text and ticks written onto the page ────────────────────

const (
	stampFontKey  = "HcFill"
	stampGap      = 4.0 // between a label and the text written after it
	stampMinSize  = 7.0
	stampMaxSize  = 12.0
	stampBaseLift = 1.0 // above the label's baseline, clear of a rule drawn under it
	stampRightGap = 24.0
)

// winAnsiBytes encodes text for a WinAnsi font, and says which character it
// cannot write.
func winAnsiBytes(s string) ([]byte, error) {
	cp1252 := map[rune]byte{'€': 0x80, '‚': 0x82, 'ƒ': 0x83, '„': 0x84, '…': 0x85, '†': 0x86, '‡': 0x87, 'ˆ': 0x88, '‰': 0x89,
		'Š': 0x8A, '‹': 0x8B, 'Œ': 0x8C, 'Ž': 0x8E, '‘': 0x91, '’': 0x92, '“': 0x93, '”': 0x94, '•': 0x95, '–': 0x96,
		'—': 0x97, '˜': 0x98, '™': 0x99, 'š': 0x9A, '›': 0x9B, 'œ': 0x9C, 'ž': 0x9E, 'Ÿ': 0x9F}
	out := make([]byte, 0, len(s))
	for _, r := range s {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			out = append(out, ' ')
		case r >= 0x20 && r < 0x7F, r >= 0xA0 && r <= 0xFF:
			out = append(out, byte(r))
		default:
			b, ok := cp1252[r]
			if !ok {
				return nil, fmt.Errorf("the character %q cannot be written onto a flat form, which uses plain Latin text", r)
			}
			out = append(out, b)
		}
	}
	return out, nil
}

func pdfEscape(b []byte) string {
	var sb strings.Builder
	for _, c := range b {
		switch c {
		case '(', ')', '\\':
			sb.WriteByte('\\')
			sb.WriteByte(c)
		default:
			sb.WriteByte(c)
		}
	}
	return sb.String()
}

func helveticaWidth(text string, size float64) float64 {
	total := 0
	for _, r := range text {
		w, err := pdffont.CharWidth(context.Background(), "Helvetica", r)
		if err != nil {
			continue
		}
		total += w
	}
	return float64(total) / 1000 * size
}

// signatureAnchor reports a label that asks for a signature. Helmcentral
// never writes one: it is the operator's to give.
func signatureAnchor(anchor string) bool {
	a := strings.ToLower(strings.TrimSpace(anchor))
	return strings.Contains(a, "signature") || a == "sign" || strings.HasPrefix(a, "signed") || strings.HasPrefix(a, "sign here")
}

// stampPlan is the content to append to each page.
type stampPlan struct {
	pages map[int]*bytes.Buffer
}

func (p *stampPlan) page(n int) *bytes.Buffer {
	if p.pages[n] == nil {
		p.pages[n] = &bytes.Buffer{}
	}
	return p.pages[n]
}

func pickOne[T any](matches []T, occurrence int, what string) (T, error) {
	var zero T
	switch {
	case len(matches) == 0:
		return zero, fmt.Errorf("%s: not found. Use the exact text from inspect_form", what)
	case occurrence == 0 && len(matches) > 1:
		return zero, fmt.Errorf("%s: found %d times; add occurrence (1 to %d, top to bottom) to say which", what, len(matches), len(matches))
	case occurrence == 0:
		return matches[0], nil
	case occurrence < 0 || occurrence > len(matches):
		return zero, fmt.Errorf("%s: occurrence %d does not exist, there are %d", what, occurrence, len(matches))
	}
	return matches[occurrence-1], nil
}

// planFlatEntry writes one entry's drawing operators into the plan.
func planFlatEntry(plan *stampPlan, layout *pdfLayout, e formEntry) error {
	if e.Page < 1 || e.Page > len(layout.Pages) {
		return fmt.Errorf("%s: the document has %d pages", e.describe(), len(layout.Pages))
	}
	pg := layout.Pages[e.Page-1]
	if pg.Rotate != 0 {
		return fmt.Errorf("%s: page %d is rotated, and a rotated page cannot be filled in", e.describe(), e.Page)
	}

	if e.Box != "" {
		// No value, or a yes, ticks the box; a no leaves it empty. The entry is
		// still checked, so an unknown box is an error either way.
		tick := true
		if strings.TrimSpace(e.Value) != "" {
			var perr error
			if tick, perr = parseTick(e.Value); perr != nil {
				return fmt.Errorf("%s: %w", e.describe(), perr)
			}
		}
		var matches []pdfLayoutBox
		for _, b := range pg.Boxes {
			if b.Label == e.Box {
				matches = append(matches, b)
			}
		}
		b, err := pickOne(matches, e.Occurrence, e.describe())
		if err != nil {
			return err
		}
		if !tick {
			return nil
		}
		w := plan.page(e.Page)
		line := math.Max(0.9, 0.11*math.Min(b.W, b.H))
		fmt.Fprintf(w, "q 0 G %.2f w 1 J 1 j %.2f %.2f m %.2f %.2f l %.2f %.2f l S Q\n", line,
			b.X+0.2*b.W, b.Y+0.52*b.H, b.X+0.42*b.W, b.Y+0.22*b.H, b.X+0.82*b.W, b.Y+0.80*b.H)
		return nil
	}

	if signatureAnchor(e.Anchor) {
		return fmt.Errorf("%s: signatures are left for the operator to sign", e.describe())
	}
	var matches []pdfLayoutLine
	for _, l := range pg.Lines {
		if l.Text == e.Anchor {
			matches = append(matches, l)
		}
	}
	l, err := pickOne(matches, e.Occurrence, e.describe())
	if err != nil {
		return err
	}
	text, err := winAnsiBytes(e.Value)
	if err != nil {
		return fmt.Errorf("%s: %w", e.describe(), err)
	}
	if len(text) == 0 {
		return fmt.Errorf("%s: there is no value to write", e.describe())
	}

	x := l.X + l.W + stampGap
	right := pg.Width - stampRightGap
	for _, o := range pg.Lines {
		if o.X > l.X+l.W && math.Abs(o.Y-l.Y) <= 0.5*l.fontSize && o.X-stampGap < right {
			right = o.X - stampGap
		}
	}
	for _, b := range pg.Boxes {
		if b.X > l.X+l.W && b.Y < l.Y+l.fontSize && b.Y+b.H > l.Y && b.X-stampGap < right {
			right = b.X - stampGap
		}
	}
	if l.BlankW > 0 {
		// The blank is printed: write on it, from where it starts, and keep
		// to its length.
		x, right = l.BlankX+1, l.BlankX+l.BlankW
	}
	size := math.Min(math.Max(l.fontSize, stampMinSize), stampMaxSize)
	for size > stampMinSize && x+helveticaWidth(e.Value, size) > right {
		size -= 0.5
	}
	if x+helveticaWidth(e.Value, size) > right {
		return fmt.Errorf("%s: the value is too long for the space after the label (%.0f points free)", e.describe(), right-x)
	}
	w := plan.page(e.Page)
	fmt.Fprintf(w, "BT 0 g /%s %.2f Tf 1 0 0 1 %.2f %.2f Tm (%s) Tj ET\n", stampFontKey, size, x, l.Y+stampBaseLift, pdfEscape(text))
	return nil
}

// stampPages appends each page's operators to its content, inside a saved
// graphics state, with the Helvetica font added to the page's resources.
func stampPages(data []byte, plan *stampPlan) (out []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			out, err = nil, fmt.Errorf("write onto the form: %v", r)
		}
	}()
	c := context.Background()
	ctx, err := api.ReadValidateAndOptimize(c, bytes.NewReader(data), pdfConf(model.ADDWATERMARKS), nil)
	if err != nil {
		return nil, fmt.Errorf("write onto the form: %w", err)
	}
	newStream := func(body string) (types.Object, error) {
		sd, err := ctx.NewStreamDictForBuf([]byte(body))
		if err != nil {
			return nil, err
		}
		if err := sd.Encode(); err != nil {
			return nil, err
		}
		ir, err := ctx.IndRefForNewObject(*sd)
		if err != nil {
			return nil, err
		}
		return *ir, nil
	}

	pages := make([]int, 0, len(plan.pages))
	for n := range plan.pages {
		pages = append(pages, n)
	}
	sort.Ints(pages)
	for _, n := range pages {
		d, _, attrs, err := ctx.PageDict(c, n, false)
		if err != nil {
			return nil, fmt.Errorf("write onto page %d: %w", n, err)
		}
		if attrs.Rotate != 0 {
			return nil, fmt.Errorf("write onto page %d: a rotated page cannot be filled in", n)
		}

		res := attrs.Resources
		if res == nil {
			res = types.NewDict()
		}
		fonts := types.NewDict()
		if fo, ok := res.Find("Font"); ok {
			if fd, derr := ctx.DereferenceDict(fo); derr == nil && fd != nil {
				fonts = fd
			}
		}
		fonts[stampFontKey] = types.Dict{
			"Type": types.Name("Font"), "Subtype": types.Name("Type1"),
			"BaseFont": types.Name("Helvetica"), "Encoding": types.Name("WinAnsiEncoding"),
		}
		res["Font"] = fonts
		d["Resources"] = res

		var existing types.Array
		if obj, ok := d.Find("Contents"); ok {
			switch o := obj.(type) {
			case types.IndirectRef:
				resolved, rerr := ctx.Dereference(o)
				if rerr != nil {
					return nil, fmt.Errorf("write onto page %d: %w", n, rerr)
				}
				if arr, isArr := resolved.(types.Array); isArr {
					existing = arr
				} else {
					existing = types.Array{o}
				}
			case types.Array:
				existing = o
			default:
				return nil, fmt.Errorf("write onto page %d: unexpected page content %T", n, obj)
			}
		}
		head, err := newStream("q\n")
		if err != nil {
			return nil, fmt.Errorf("write onto page %d: %w", n, err)
		}
		tail, err := newStream("Q\nq\n" + plan.pages[n].String() + "Q\n")
		if err != nil {
			return nil, fmt.Errorf("write onto page %d: %w", n, err)
		}
		contents := types.Array{head}
		contents = append(contents, existing...)
		d["Contents"] = append(contents, tail)
	}

	var buf bytes.Buffer
	if err := api.Write(c, ctx, &buf, pdfConf(model.ADDWATERMARKS)); err != nil {
		return nil, fmt.Errorf("write onto the form: %w", err)
	}
	return buf.Bytes(), nil
}

// filledForm is what fillPDFForm made.
type filledForm struct {
	Data      []byte
	PageCount int
	Filled    []string
}

// fillPDFForm fills a PDF on disk: AcroForm entries through the form fields,
// flat entries onto the page. Every entry is checked first; one that cannot be
// done fails the whole call, naming it, and nothing is written.
func fillPDFForm(path string, entries []formEntry) (*filledForm, error) {
	if len(entries) == 0 {
		return nil, errors.New("entries is required and must list at least one thing to fill in")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read the document: %w", err)
	}

	var fields, flat []formEntry
	for _, e := range entries {
		kinds := 0
		for _, set := range []bool{e.Field != "", e.Anchor != "", e.Box != ""} {
			if set {
				kinds++
			}
		}
		if kinds != 1 {
			return nil, fmt.Errorf("each entry needs exactly one of field, anchor or box, got %+v", e)
		}
		if e.Field != "" {
			fields = append(fields, e)
		} else {
			flat = append(flat, e)
		}
	}

	var layout *pdfLayout
	plan := &stampPlan{pages: map[int]*bytes.Buffer{}}
	if len(flat) > 0 {
		layout, err = analysePDFLayout(path)
		if err != nil {
			return nil, err
		}
		var problems []string
		for _, e := range flat {
			if perr := planFlatEntry(plan, layout, e); perr != nil {
				problems = append(problems, perr.Error())
			}
		}
		if len(problems) > 0 {
			return nil, errors.New(strings.Join(problems, "; "))
		}
	}

	out := data
	if len(fields) > 0 {
		if out, err = fillAcroForm(out, fields); err != nil {
			return nil, err
		}
	}
	if len(flat) > 0 {
		if out, err = stampPages(out, plan); err != nil {
			return nil, err
		}
	}

	res := &filledForm{Data: out}
	for _, e := range entries {
		res.Filled = append(res.Filled, e.describe())
	}
	if layout != nil {
		res.PageCount = len(layout.Pages)
	} else if res.PageCount, err = pdfPageCountOf(out); err != nil {
		return nil, err
	}
	return res, nil
}

func pdfPageCountOf(data []byte) (n int, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("count pages: %v", r)
		}
	}()
	n, err = api.PageCount(context.Background(), bytes.NewReader(data), pdfConf(model.LISTINFO))
	if err != nil {
		return 0, fmt.Errorf("count pages: %w", err)
	}
	return n, nil
}
