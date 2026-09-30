package main

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// This file reads a YachtWave "Vessel Export" report (an HTML page, one
// section per topic) into the platform-neutral payload in import_staged.go.
// The report has no ids and no machine-readable form, so the parser leans on
// the structure YachtWave's own template emits: every table cell carries a
// data-label naming its column, a record's name sits in a <b> with the detail
// line beneath it in span.vx-sub, and each section is a td with a fixed id.
//
// Fail fast, per AGENTS.md: a file that is not recognisably a Vessel Export
// is rejected with a message saying so, a cell that cannot be read as the
// number it must be is an error, and nothing is repaired on the way through.
// A value that looks wrong (a 32 kg displacement, a part number of
// "Unknown") is kept as exported and raised as an issue for the operator to
// read on the review page.

var errNotYachtWaveExport = errors.New("this file is not a YachtWave Vessel Export (expected the report YachtWave generates under Reports, Vessel Export)")

// yachtWaveSectionIDs are the section anchors the template emits, in page
// order.
var yachtWaveSectionIDs = []string{
	"particulars", "crew", "engines", "equipment", "inventory", "services",
	"maintenance", "cruises", "general", "readings", "tasks", "checklists",
	"notes", "documents", "expenses", "photos",
}

// yachtWaveMissing are the cell texts the export uses for "nothing here".
// Matching is case-insensitive.
var yachtWaveMissing = map[string]bool{"--": true, "-": true, "—": true, "–": true, "not set": true, "unknown": true}

// normYW trims s and collapses the export's "nothing here" markers to "".
func normYW(s string) string {
	s = strings.TrimSpace(s)
	if yachtWaveMissing[strings.ToLower(s)] {
		return ""
	}
	return s
}

// ── HTML helpers ─────────────────────────────────────────────────────────

func ywAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func ywHasClass(n *html.Node, class string) bool {
	for _, c := range strings.Fields(ywAttr(n, "class")) {
		if c == class {
			return true
		}
	}
	return false
}

func ywIsElem(n *html.Node, tag string) bool {
	return n != nil && n.Type == html.ElementNode && n.Data == tag
}

// ywFind returns the first descendant of n (document order) that match
// accepts, or nil.
func ywFind(n *html.Node, match func(*html.Node) bool) *html.Node {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && match(c) {
			return c
		}
		if got := ywFind(c, match); got != nil {
			return got
		}
	}
	return nil
}

// ywFindAll returns every descendant of n, in document order, that match
// accepts.
func ywFindAll(n *html.Node, match func(*html.Node) bool) []*html.Node {
	var out []*html.Node
	var walk func(*html.Node)
	walk = func(p *html.Node) {
		for c := p.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && match(c) {
				out = append(out, c)
			}
			walk(c)
		}
	}
	walk(n)
	return out
}

func ywByTag(tag string) func(*html.Node) bool {
	return func(n *html.Node) bool { return n.Data == tag }
}

func ywByClass(class string) func(*html.Node) bool {
	return func(n *html.Node) bool { return ywHasClass(n, class) }
}

// ywRawText is n's text with <br> as a newline. A newline in the source
// straight after a <br> is formatting, not content, and is dropped, so
// "a<br />\nb" reads as "a\nb". Chips (status badges) are skipped when
// skipChips is set: the HIN cell carries an "Unverified" badge that is not
// part of the value.
func ywRawText(n *html.Node, skipChips bool) string {
	var sb strings.Builder
	afterBreak := false
	var walk func(*html.Node)
	walk = func(p *html.Node) {
		for c := p.FirstChild; c != nil; c = c.NextSibling {
			switch {
			case c.Type == html.TextNode:
				t := strings.ReplaceAll(c.Data, "\r\n", "\n")
				if afterBreak {
					t = strings.TrimPrefix(t, "\n")
					afterBreak = false
				}
				sb.WriteString(t)
			case c.Type == html.ElementNode && c.Data == "br":
				sb.WriteString("\n")
				afterBreak = true
			case c.Type == html.ElementNode && skipChips && ywHasClass(c, "vx-chip"):
			case c.Type == html.ElementNode && (c.Data == "script" || c.Data == "style"):
			case c.Type == html.ElementNode:
				walk(c)
			}
		}
	}
	walk(n)
	return sb.String()
}

// ywLineText is n's text as one line: whitespace runs collapsed.
func ywLineText(n *html.Node, skipChips bool) string {
	return strings.Join(strings.Fields(ywRawText(n, skipChips)), " ")
}

// ywBlockText is n's text as written, with each line's trailing space
// removed and the ends trimmed. Line breaks are kept: a maintenance log body
// and a locker's contents are lists, one thing per line.
func ywBlockText(n *html.Node) string {
	lines := strings.Split(ywRawText(n, false), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t\r")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// ywRow is one table row keyed by its cells' data-label.
type ywRow struct {
	cells map[string]*html.Node
	group string
	order []string
}

func (r ywRow) line(label string) string {
	if c, ok := r.cells[label]; ok {
		return ywLineText(c, false)
	}
	return ""
}

// name is the record name: the text of the first <b> in the row's primary
// cell.
func (r ywRow) name(label string) string {
	c, ok := r.cells[label]
	if !ok {
		return ""
	}
	if b := ywFind(c, ywByTag("b")); b != nil {
		return ywLineText(b, false)
	}
	return ywLineText(c, false)
}

// sub is the detail line under the name (span.vx-sub), as written.
func (r ywRow) sub(label string) string {
	c, ok := r.cells[label]
	if !ok {
		return ""
	}
	if s := ywFind(c, ywByClass("vx-sub")); s != nil {
		// Runs of spaces inside a line are layout, not content; line breaks
		// are kept (a locker's contents are one per line).
		lines := strings.Split(ywBlockText(s), "\n")
		for i, l := range lines {
			lines[i] = strings.Join(strings.Fields(l), " ")
		}
		return strings.Join(lines, "\n")
	}
	return ""
}

// ywRows returns every data row under section, in document order, each with
// the nearest group heading above it (the equipment section groups its
// tables by location).
func ywRows(section *html.Node) []ywRow {
	var rows []ywRow
	group := ""
	var walk func(*html.Node)
	walk = func(p *html.Node) {
		for c := p.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			if c.Data == "div" && ywHasClass(c, "vx-grp") {
				group = ywGroupName(c)
				continue
			}
			if c.Data == "tr" {
				row := ywRow{cells: map[string]*html.Node{}, group: group}
				for td := c.FirstChild; td != nil; td = td.NextSibling {
					if ywIsElem(td, "td") {
						if l := ywAttr(td, "data-label"); l != "" {
							row.cells[l] = td
							row.order = append(row.order, l)
						}
					}
				}
				if len(row.cells) > 0 {
					rows = append(rows, row)
					continue
				}
				// A layout row (the section's own wrapper table): the
				// data tables live inside it.
			}
			walk(c)
		}
	}
	walk(section)
	return rows
}

// ywGroupName is a group heading's own text, without the trailing count span
// ("Flybridge" from "Flybridge<span>5 items</span>").
func ywGroupName(n *html.Node) string {
	var sb strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			sb.WriteString(c.Data)
		}
	}
	return strings.Join(strings.Fields(sb.String()), " ")
}

// ── dates and numbers ────────────────────────────────────────────────────

// ywDate converts the export's "17 Jun 2024" into 2024-06-17. "" (after
// normalising "--" and friends) stays "". Anything else that does not parse
// comes back as ok=false with the raw text, for the caller to raise as an
// issue - it is not guessed at.
func ywDate(raw string) (iso string, ok bool) {
	raw = normYW(raw)
	if raw == "" {
		return "", true
	}
	t, err := time.Parse("2 Jan 2006", raw)
	if err != nil {
		return "", false
	}
	return t.Format("2006-01-02"), true
}

var displacementPattern = regexp.MustCompile(`^([0-9][0-9,]*(?:\.[0-9]+)?)\s*(kg|kgs|t|tonne|tonnes|tons?)?$`)

// parseDisplacementKg reads "32 kg", "24.5 t" or "24,500" into kilograms. A
// bare number is read as kilograms, the unit the vessel_particulars column
// holds. Any other unit is an error: pounds would need a conversion nobody
// asked for, so it is refused rather than misread.
func parseDisplacementKg(raw string) (float64, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	m := displacementPattern.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("displacement %q is not a number of kg or tonnes", raw)
	}
	v, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", ""), 64)
	if err != nil {
		return 0, fmt.Errorf("displacement %q: %w", raw, err)
	}
	if strings.HasPrefix(m[2], "t") {
		v *= 1000
	}
	return v, nil
}

// ywInt reads a whole number; the error names the row it came from.
func ywInt(raw, what string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%s: %q is not a whole number", what, raw)
	}
	return n, nil
}

// ── the parser ───────────────────────────────────────────────────────────

type ywParser struct {
	st       stagedImport
	sections map[string]*html.Node
	titles   map[string]string
	zones    map[string]*stagedLocation
	zoneSeq  []string
	issue    func(code, severity, section, key, msg string)
}

// parseYachtWaveExport reads a Vessel Export report. The returned payload
// always has every list non-nil.
func parseYachtWaveExport(raw []byte) (stagedImport, error) {
	if !bytes.Contains(bytes.ToLower(raw), []byte("yachtwave")) {
		return stagedImport{}, errNotYachtWaveExport
	}
	doc, err := html.Parse(bytes.NewReader(raw))
	if err != nil {
		return stagedImport{}, fmt.Errorf("%w: %v", errNotYachtWaveExport, err)
	}
	title := ywFind(doc, ywByTag("title"))
	if title == nil || !strings.HasPrefix(ywLineText(title, false), "Vessel Export") {
		return stagedImport{}, errNotYachtWaveExport
	}

	p := &ywParser{
		sections: map[string]*html.Node{},
		titles:   map[string]string{},
		zones:    map[string]*stagedLocation{},
	}
	for _, id := range yachtWaveSectionIDs {
		id := id
		if n := ywFind(doc, func(n *html.Node) bool { return ywAttr(n, "id") == id }); n != nil {
			p.sections[id] = n
			if h := ywFind(n, ywByTag("h2")); h != nil {
				p.titles[id] = ywLineText(h, false)
			}
		}
	}
	if p.sections["particulars"] == nil {
		return stagedImport{}, errNotYachtWaveExport
	}

	p.st = stagedImport{
		Source:      importSourceYachtWave,
		Particulars: []stagedParticular{},
		Locations:   []stagedLocation{},
		Equipment:   []stagedEquipment{},
		Spares:      []stagedSpare{},
		LogEntries:  []stagedLogEntry{},
		Notes:       []stagedNote{},
		Files:       []stagedFile{},
		Issues:      []stagedIssue{},
		Sections:    []stagedSectionInfo{},
	}
	p.issue = func(code, severity, section, key, msg string) {
		p.st.Issues = append(p.st.Issues, stagedIssue{Code: code, Severity: severity, Section: section, Key: key, Message: msg})
	}

	p.vessel(doc)
	steps := []func(doc *html.Node) error{
		p.particulars, p.engines, p.equipment, p.inventory, p.maintenance,
		p.tasks, p.notes, p.documents, p.photos,
		p.checklists, p.unimportedSections,
	}
	for _, step := range steps {
		if err := step(doc); err != nil {
			return stagedImport{}, err
		}
	}
	p.resolveLogEquipment()
	p.resolveFileEquipment()
	p.finishLocations()
	p.sectionInfo()
	return p.st, nil
}

func (p *ywParser) title(id string) string {
	if t := p.titles[id]; t != "" {
		return t
	}
	return id
}

// vessel reads the boat's name from the report's title and the generation
// stamp from the page header.
func (p *ywParser) vessel(doc *html.Node) {
	if t := ywFind(doc, ywByTag("title")); t != nil {
		p.st.Vessel.Name = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(ywLineText(t, false), "Vessel Export"), ":"))
	}
	if g := ywFind(doc, ywByClass("header-date")); g != nil {
		p.st.Vessel.GeneratedAt = strings.TrimSpace(strings.TrimPrefix(ywLineText(g, false), "Generated"))
	}
}

// zone registers a location name and counts one more record filed there.
func (p *ywParser) zone(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	lower := strings.ToLower(name)
	if z, ok := p.zones[lower]; ok {
		z.Count++
		return
	}
	p.zones[lower] = &stagedLocation{Key: stagedKey("zone", lower), Name: name, Count: 1}
	p.zoneSeq = append(p.zoneSeq, lower)
}

func (p *ywParser) finishLocations() {
	for _, l := range p.zoneSeq {
		p.st.Locations = append(p.st.Locations, *p.zones[l])
	}
}

// ── particulars ──────────────────────────────────────────────────────────

// yachtWaveParticularFields maps the report's particular labels to the
// vessel_particulars column, and marks the ones the boat's own data feed
// publishes. A label not listed here is still staged (Field "") so the
// operator can see it; it is just not written anywhere.
var yachtWaveParticularFields = map[string]struct {
	field   string
	signalk bool
}{
	"Length overall":         {"", true},
	"Beam":                   {"", true},
	"Draft":                  {"", true},
	"Clearance":              {"", true},
	"MMSI":                   {"", true},
	"Call sign":              {"", true},
	"Displacement":           {"displacement_kg", false},
	"Hull type":              {"hull_type", false},
	"Hull material":          {"hull_material", false},
	"Brand":                  {"builder", false},
	"Date acquired":          {"date_acquired", false},
	"Hull ID (HIN)":          {"hin", false},
	"USCG documentation no.": {"", false},
	"Registration":           {"registration", false},
	"IMO":                    {"imo", false},
	"EPIRB beacon ID":        {"epirb_id", false},
	"Shore power":            {"shore_power", false},
	"System voltage":         {"system_voltage", false},
}

func (p *ywParser) addParticular(label, field string, signalk bool, value string) stagedParticular {
	sp := stagedParticular{Key: stagedKey("particular", label), Label: label, Field: field, Value: value, SignalK: signalk}
	p.st.Particulars = append(p.st.Particulars, sp)
	return sp
}

// particulars reads the identity card (name, model, year, flag, hailing
// port) and the particulars grid.
func (p *ywParser) particulars(doc *html.Node) error {
	section := p.title("particulars")

	// Identity card: five big fields and a subline. Only what the grid
	// does not repeat is taken from it.
	identity := map[string]string{}
	for _, f := range ywFindAll(doc, ywByClass("hdr-field")) {
		lbl := ywFind(f, ywByTag("p"))
		val := ywFind(f, ywByTag("h1"))
		if lbl != nil && val != nil {
			identity[ywLineText(lbl, false)] = normYW(ywLineText(val, false))
		}
	}
	subline := map[string]string{}
	for _, it := range ywFindAll(doc, ywByClass("vx-sub-item")) {
		b := ywFind(it, ywByTag("b"))
		if b == nil {
			continue
		}
		val := ywLineText(b, false)
		label := strings.TrimSpace(strings.TrimSuffix(ywLineText(it, false), val))
		subline[label] = normYW(val)
	}

	p.addParticular("Name", "", true, identity["Name"])

	grid := ywFind(p.sections["particulars"], ywByClass("vx-particulars"))
	if grid != nil {
		for _, pf := range ywFindAll(grid, ywByClass("vx-pf")) {
			lblNode := ywFind(pf, ywByClass("vx-lbl"))
			valNode := ywFind(pf, ywByClass("vx-pv"))
			if lblNode == nil || valNode == nil {
				continue
			}
			label := ywLineText(lblNode, false)
			value := normYW(ywLineText(valNode, true))
			meta := yachtWaveParticularFields[label]
			sp := p.addParticular(label, meta.field, meta.signalk, value)
			if sp.Field == "displacement_kg" && value != "" {
				kg, err := parseDisplacementKg(value)
				switch {
				case err != nil:
					p.issue(issueImplausibleValue, issueSeverityWarning, section, sp.Key,
						fmt.Sprintf("Displacement %q could not be read as kilograms or tonnes.", value))
				case kg < 1000:
					p.issue(issueImplausibleValue, issueSeverityWarning, section, sp.Key,
						fmt.Sprintf("Displacement is exported as %q. No boat weighs that little; it is probably a wrong unit or a typo. It is imported exactly as exported unless you skip it.", value))
				}
			}
		}
	}

	if v := identity["Model"]; v != "" {
		p.addParticular("Model", "model", false, v)
	}
	if v := identity["Year"]; v != "" {
		sp := p.addParticular("Year", "year", false, v)
		if y, err := strconv.Atoi(v); err != nil || y < 1900 || y > time.Now().Year()+1 {
			p.issue(issueImplausibleValue, issueSeverityWarning, section, sp.Key, fmt.Sprintf("Year %q is not a plausible build year.", v))
		}
	}
	if v, ok := subline["Flag"]; ok {
		p.addParticular("Flag", "flag", false, v)
	}
	if v, ok := subline["Hailing port"]; ok {
		p.addParticular("Hailing port", "hailing_port", false, v)
	}

	// Date acquired is the only date-valued particular.
	for i, sp := range p.st.Particulars {
		if sp.Field == "date_acquired" && sp.Value != "" {
			iso, ok := ywDate(sp.Value)
			if !ok {
				p.issue(issueUnparsedDate, issueSeverityWarning, section, sp.Key, fmt.Sprintf("Date acquired %q is not a date this importer reads (expected like 17 Jun 2024).", sp.Value))
				p.st.Particulars[i].Value = ""
			} else {
				p.st.Particulars[i].Value = iso
			}
		}
	}
	return nil
}

// ── equipment ────────────────────────────────────────────────────────────

func (p *ywParser) date(section, key, raw string) string {
	iso, ok := ywDate(raw)
	if !ok {
		p.issue(issueUnparsedDate, issueSeverityWarning, section, key, fmt.Sprintf("Date %q is not a date this importer reads (expected like 17 Jun 2024); left blank.", strings.TrimSpace(raw)))
	}
	return iso
}

// junkNamePattern matches the throwaway names people type to test a form.
var junkNames = map[string]bool{"test": true, "asdf": true, "qwerty": true, "xxx": true, "foo": true, "bar": true, "abc": true}

// looksLikeJunkName reports a name that is almost certainly a test entry: a
// lowercase run of letters three characters or shorter ("xys"), or one of a
// few known throwaways. A real short name ("Gen2", "TZT2BB") has a digit or a
// capital and is left alone.
func looksLikeJunkName(name string) bool {
	n := strings.TrimSpace(name)
	if junkNames[strings.ToLower(n)] {
		return true
	}
	if len(n) > 3 {
		return false
	}
	for _, r := range n {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	return n != ""
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// dupName is a name reduced to letters and digits so "BR1 PRO 5G" and
// "BR1-PRO 5G" compare equal.
func dupName(s string) string { return nonAlnum.ReplaceAllString(strings.ToLower(s), "") }

func (p *ywParser) engines(doc *html.Node) error {
	sec := p.sections["engines"]
	if sec == nil {
		return nil
	}
	section := p.title("engines")
	keys := newKeyAllocator()
	for _, r := range ywRows(sec) {
		name := r.name("Engine")
		if name == "" {
			return fmt.Errorf("%s: a row has no engine name", section)
		}
		e := stagedEquipment{
			Kind:      stagedKindEngine,
			Name:      name,
			Type:      normYW(r.line("Type")),
			Serial:    normYW(r.line("Serial")),
			Detail:    r.sub("Engine"),
			Hours:     normYW(r.line("Hours")),
			Installed: "",
		}
		e.Key, _ = keys.next("equipment", e.Kind, e.Name, e.Serial, e.Detail)
		e.Installed = p.date(section, e.Key, r.line("Installed"))
		p.st.Equipment = append(p.st.Equipment, e)
	}
	return nil
}

func (p *ywParser) equipment(doc *html.Node) error {
	sec := p.sections["equipment"]
	if sec == nil {
		return nil
	}
	section := p.title("equipment")
	keys := newKeyAllocator()
	for _, r := range ywRows(sec) {
		name := r.name("Item")
		if name == "" {
			return fmt.Errorf("%s: a row has no item name", section)
		}
		e := stagedEquipment{
			Kind:         stagedKindEquipment,
			Name:         name,
			Type:         normYW(r.line("Type")),
			Manufacturer: normYW(r.line("Manufacturer")),
			Serial:       normYW(r.line("Serial")),
			Location:     strings.TrimSpace(r.group),
		}
		// "Flybridge · Electronics": the last segment is the source's own
		// category, anything before it is where in the location the item is.
		if parts := splitDetail(r.sub("Item")); len(parts) > 0 {
			e.Category = parts[len(parts)-1]
			e.Detail = strings.Join(parts[:len(parts)-1], " · ")
		}
		e.Key, _ = keys.next("equipment", e.Kind, e.Name, e.Serial, e.Location, e.Detail)
		e.Installed = p.date(section, e.Key, r.line("Installed"))
		p.zone(e.Location)
		if looksLikeJunkName(e.Name) {
			p.issue(issueJunkName, issueSeverityWarning, section, e.Key, fmt.Sprintf("%q looks like a test entry, not a real item.", e.Name))
		}
		p.st.Equipment = append(p.st.Equipment, e)
	}

	// Near-duplicates: same name to letters and digits, same serial. The
	// later copy is the one flagged.
	seen := map[string]stagedEquipment{}
	for _, e := range p.st.Equipment {
		k := e.Kind + "|" + dupName(e.Name) + "|" + e.Serial
		if first, ok := seen[k]; ok && e.Kind == stagedKindEquipment {
			p.issue(issueDuplicate, issueSeverityWarning, section, e.Key,
				fmt.Sprintf("%q looks like the same item as %q listed earlier.", e.Name, first.Name))
			continue
		}
		seen[k] = e
	}
	return nil
}

// splitDetail splits a "a · b · c" detail line.
func splitDetail(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(s, " · ") {
		if t := strings.TrimSpace(part); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// ── inventory and spares ─────────────────────────────────────────────────

func (p *ywParser) inventory(doc *html.Node) error {
	sec := p.sections["inventory"]
	if sec == nil {
		return nil
	}
	section := p.title("inventory")
	keys := newKeyAllocator()
	seen := map[string]stagedSpare{}
	for _, r := range ywRows(sec) {
		name := r.name("Item")
		if name == "" {
			return fmt.Errorf("%s: a row has no item name", section)
		}
		rawPart := strings.TrimSpace(r.line("Part no."))
		s := stagedSpare{
			Kind:       stagedSpareItem,
			Name:       name,
			PartNumber: normYW(rawPart),
			Location:   normYW(r.line("Location")),
			Category:   normYW(r.line("Type")),
			Items:      []string{},
		}

		onHand, required, err := parseOnHand(r.line("On board / required"))
		if err != nil {
			return fmt.Errorf("%s: %q: %w", section, name, err)
		}
		s.OnHand, s.Required = onHand, required

		sub := r.sub("Item")
		if strings.Contains(sub, "\n") {
			// A locker row: its contents, one per line.
			for _, line := range strings.Split(sub, "\n") {
				if t := strings.TrimSpace(line); t != "" {
					s.Items = append(s.Items, t)
				}
			}
		}
		// A row named like a locker whose detail is a single plain line
		// ("Solar Controllers") is a bin with one thing in it.
		if len(s.Items) == 0 && lockerName.MatchString(name) && sub != "" && !strings.Contains(sub, " · ") {
			s.Items = []string{sub}
		}
		if len(s.Items) >= 2 || (len(s.Items) == 1 && lockerName.MatchString(name)) {
			s.Kind = stagedSpareBin
		} else {
			s.Items = []string{}
			parts := splitDetail(sub)
			if len(parts) > 0 {
				s.Detail = parts[0]
				s.Notes = strings.Join(parts[1:], " · ")
			}
		}

		s.Key, _ = keys.next("spare", s.Name, s.PartNumber, s.Location, s.Detail)
		p.zone(s.Location)

		if strings.EqualFold(rawPart, "unknown") {
			p.issue(issueUnknownPartNumber, issueSeverityInfo, section, s.Key, fmt.Sprintf("%q has a part number of \"Unknown\"; imported with the part number blank.", name))
		}
		if s.Kind == stagedSpareItem && looksLikeJunkName(s.Name) {
			p.issue(issueJunkName, issueSeverityWarning, section, s.Key, fmt.Sprintf("%q looks like a test entry, not a real item.", s.Name))
		}
		dk := dupName(s.Name) + "|" + dupName(s.PartNumber) + "|" + dupName(s.Location)
		if first, ok := seen[dk]; ok {
			p.issue(issueDuplicate, issueSeverityWarning, section, s.Key, fmt.Sprintf("%q looks like the same item as %q listed earlier.", s.Name, first.Name))
		} else {
			seen[dk] = s
		}
		p.st.Spares = append(p.st.Spares, s)
	}
	return nil
}

// lockerName matches an inventory row that names a locker rather than a
// thing ("FB Lounge - Locker 3").
var lockerName = regexp.MustCompile(`(?i)\blocker\b`)

// parseOnHand reads "2 / 2" (on hand / required) or "0" (on hand only).
func parseOnHand(raw string) (onHand int, required *int, err error) {
	raw = strings.TrimSpace(raw)
	have, req, both := strings.Cut(raw, "/")
	onHand, err = ywInt(have, "on board")
	if err != nil {
		return 0, nil, err
	}
	if both {
		n, err := ywInt(req, "required")
		if err != nil {
			return 0, nil, err
		}
		required = &n
	}
	return onHand, required, nil
}

// ── maintenance log ──────────────────────────────────────────────────────

func (p *ywParser) maintenance(doc *html.Node) error {
	sec := p.sections["maintenance"]
	if sec == nil {
		return nil
	}
	section := p.title("maintenance")
	keys := newKeyAllocator()
	for _, r := range ywRows(sec) {
		title := r.name("Work done")
		if title == "" {
			return fmt.Errorf("%s: a row has no title", section)
		}
		e := stagedLogEntry{
			Title:         title,
			Body:          r.sub("Work done"),
			Type:          normYW(r.line("Type")),
			EquipmentName: normYW(r.line("Engine / equipment")),
			Candidates:    []string{},
		}
		hoursText := normYW(r.line("Hours"))
		if hoursText != "" {
			h, err := strconv.ParseFloat(strings.ReplaceAll(hoursText, ",", ""), 64)
			if err != nil || h < 0 {
				return fmt.Errorf("%s: %q: hours %q is not a number", section, title, hoursText)
			}
			e.Hours = &h
		}
		rawDate := r.line("Date")
		hoursKey := ""
		if e.Hours != nil {
			hoursKey = strconv.FormatFloat(*e.Hours, 'f', -1, 64)
		}
		var occurrence int
		e.Key, occurrence = keys.next("log", rawDate, e.Title, e.EquipmentName, e.Type, hoursKey)
		e.Date = p.date(section, e.Key, rawDate)
		if occurrence > 0 {
			p.issue(issueDuplicate, issueSeverityWarning, section, e.Key,
				fmt.Sprintf("%q on %s is listed more than once with the same details; this is copy %d.", e.Title, strings.TrimSpace(rawDate), occurrence+1))
		}
		p.st.LogEntries = append(p.st.LogEntries, e)
	}
	return nil
}

// resolveLogEquipment points each log entry at the staged equipment its name
// matches. One match is taken; several are listed as candidates and left for
// the operator; none is raised as an issue. Runs after equipment is parsed.
func (p *ywParser) resolveLogEquipment() {
	section := p.title("maintenance")
	for i := range p.st.LogEntries {
		e := &p.st.LogEntries[i]
		if e.EquipmentName == "" {
			continue
		}
		var matches []string
		for _, q := range p.st.Equipment {
			if strings.EqualFold(strings.TrimSpace(q.Name), e.EquipmentName) {
				matches = append(matches, q.Key)
			}
		}
		switch len(matches) {
		case 0:
			p.issue(issueUnknownEquipmentInLog, issueSeverityWarning, section, e.Key,
				fmt.Sprintf("%q names %q, which is not in the export's engine or equipment lists; imported without equipment.", e.Title, e.EquipmentName))
		case 1:
			e.EquipmentKey = matches[0]
		default:
			e.Candidates = matches
			p.issue(issueAmbiguousEquipment, issueSeverityWarning, section, e.Key,
				fmt.Sprintf("%q on %s names %q, which matches %d items. Choose the right one.", e.Title, e.Date, e.EquipmentName, len(matches)))
		}
	}
}

// ── tasks, notes ─────────────────────────────────────────────────────────

// credentialBody matches a note body that states a credential: the word
// password (or a relative) followed by a colon or equals sign.
var credentialBody = regexp.MustCompile(`(?i)\b(password|passcode|passphrase|passwd|pin code|wpa key|wifi key)\s*[:=]`)

// credentialTitle matches a note titled like a stored credential.
var credentialTitle = regexp.MustCompile(`(?i)\b(password|passcode|passphrase|wi-?fi|wlan|pin|login|credentials?)\b`)

// noteHoldsCredential decides whether a note looks like it stores a secret.
// A body that states one ("Password: ...") is enough. A title like "Wifi" or
// "Windows Password" over a short single-line body is the other shape, where
// the body is the bare secret. A long note that merely mentions wifi is not
// flagged: flagging it would throw away a useful note for nothing.
func noteHoldsCredential(title, body string) (bool, string) {
	if credentialBody.MatchString(body) {
		return true, "The note states a password."
	}
	if credentialTitle.MatchString(title) && len(body) <= 120 && !strings.Contains(body, "\n") {
		return true, "The note is titled like a stored password or wifi key and is short enough to be just the secret."
	}
	return false, ""
}

func (p *ywParser) notes(doc *html.Node) error {
	sec := p.sections["notes"]
	if sec == nil {
		return nil
	}
	section := p.title("notes")
	keys := newKeyAllocator()
	for _, n := range ywFindAll(sec, ywByClass("vx-note")) {
		head := ywFind(n, ywByClass("vx-note-h"))
		if head == nil {
			return fmt.Errorf("%s: a note has no heading", section)
		}
		titleNode := ywFind(head, ywByTag("b"))
		if titleNode == nil {
			return fmt.Errorf("%s: a note has no title", section)
		}
		title := ywLineText(titleNode, false)
		dateRaw := ""
		if d := ywFind(head, ywByClass("vx-muted")); d != nil {
			dateRaw = ywLineText(d, false)
		}
		body := ""
		if bp := ywFind(n, ywByTag("p")); bp != nil {
			body = ywBlockText(bp)
		}
		note := stagedNote{Kind: stagedNoteKindNote, Title: title, Body: body}
		note.Key, _ = keys.next("note", note.Kind, title, dateRaw)
		note.Date = p.date(section, note.Key, dateRaw)
		if hit, why := noteHoldsCredential(title, body); hit {
			note.Skip = true
			note.SkipReason = why + " Helmcentral notes are not a place for secrets; it is not imported."
			// The secret does not travel any further than the parser: the
			// staged payload is stored and served, so a skipped note keeps
			// its title and date and loses its body.
			note.Body = ""
			p.issue(issuePasswordNoteSkipped, issueSeverityWarning, section, note.Key,
				fmt.Sprintf("%q was skipped: %s", title, why))
		}
		p.st.Notes = append(p.st.Notes, note)
	}
	return nil
}

// tasks converts each task to a note: there is no tasks table, the note body
// carries every field.
func (p *ywParser) tasks(doc *html.Node) error {
	sec := p.sections["tasks"]
	if sec == nil {
		return nil
	}
	section := p.title("tasks")
	keys := newKeyAllocator()
	for _, r := range ywRows(sec) {
		name := r.name("Task")
		if name == "" {
			return fmt.Errorf("%s: a row has no task name", section)
		}
		var lines []string
		add := func(label, v string) {
			if v != "" {
				lines = append(lines, label+": "+v)
			}
		}
		add("Priority", normYW(r.line("Priority")))
		add("Assigned to", normYW(r.line("Assigned to")))

		note := stagedNote{Kind: stagedNoteKindTask, Title: "Task: " + name}
		due, completed := r.line("Due"), r.line("Completed")
		note.Key, _ = keys.next("note", note.Kind, name, due, completed, r.line("Assigned to"))
		if _, ok := r.cells["Due"]; ok {
			iso := p.date(section, note.Key, due)
			add("Due", iso)
			note.Date = iso
		}
		add("Status", normYW(r.line("Status")))
		if _, ok := r.cells["Completed"]; ok {
			iso := p.date(section, note.Key, completed)
			add("Completed", iso)
			if note.Date == "" {
				note.Date = iso
			}
		}
		note.Body = strings.Join(lines, "\n")
		p.st.Notes = append(p.st.Notes, note)
	}
	return nil
}

// ── files ────────────────────────────────────────────────────────────────

func (p *ywParser) documents(doc *html.Node) error {
	sec := p.sections["documents"]
	if sec == nil {
		return nil
	}
	section := p.title("documents")
	keys := newKeyAllocator()
	for _, r := range ywRows(sec) {
		cell := r.cells["Document"]
		if cell == nil {
			return fmt.Errorf("%s: a row has no document cell", section)
		}
		link := ywFind(cell, ywByTag("a"))
		if link == nil || ywAttr(link, "href") == "" {
			return fmt.Errorf("%s: %q has no file link", section, r.name("Document"))
		}
		f := stagedFile{
			Kind:       stagedFileKindDocument,
			Label:      r.name("Document"),
			Type:       normYW(r.line("Type")),
			AttachedTo: normYW(r.line("Attached to")),
			Size:       normYW(r.line("File")),
			URL:        ywAttr(link, "href"),
		}
		f.Key, _ = keys.next("file", f.Kind, f.URL)
		f.Date = p.date(section, f.Key, r.line("Added"))
		p.st.Files = append(p.st.Files, f)
	}
	return nil
}

func (p *ywParser) photos(doc *html.Node) error {
	sec := p.sections["photos"]
	if sec == nil {
		return nil
	}
	section := p.title("photos")
	keys := newKeyAllocator()
	for _, t := range ywFindAll(sec, ywByClass("vx-thumb")) {
		img := ywFind(t, ywByTag("img"))
		if img == nil || ywAttr(img, "src") == "" {
			return fmt.Errorf("%s: a photo has no image link", section)
		}
		alt := strings.TrimSpace(ywAttr(img, "alt"))
		attached, date, _ := strings.Cut(alt, " · ")
		f := stagedFile{
			Kind:       stagedFileKindPhoto,
			Label:      alt,
			AttachedTo: strings.TrimSpace(attached),
			URL:        ywAttr(img, "src"),
		}
		f.Key, _ = keys.next("file", f.Kind, f.URL)
		f.Date = p.date(section, f.Key, date)
		p.st.Files = append(p.st.Files, f)
	}
	return nil
}

// resolveFileEquipment attaches a file to a staged equipment record when its
// "attached to" text names exactly one. Anything else (the vessel itself, the
// maintenance log, an ambiguous name) leaves it unattached: it is still
// imported as a document.
func (p *ywParser) resolveFileEquipment() {
	for i := range p.st.Files {
		f := &p.st.Files[i]
		if f.AttachedTo == "" {
			continue
		}
		var match string
		n := 0
		for _, e := range p.st.Equipment {
			if strings.EqualFold(e.Name, f.AttachedTo) {
				match = e.Key
				n++
			}
		}
		if n == 1 {
			f.EquipmentKey = match
		}
	}
}

// ── checklists and the sections this cycle does not import ───────────────

// checklists raises one issue per checklist. The export lists each one's
// name and step count but not the steps, so there is nothing to import; the
// review page says so rather than importing an empty shell.
func (p *ywParser) checklists(doc *html.Node) error {
	sec := p.sections["checklists"]
	if sec == nil {
		return nil
	}
	section := p.title("checklists")
	keys := newKeyAllocator()
	for _, r := range ywRows(sec) {
		name := r.name("Checklist")
		steps := r.line("Steps")
		key, _ := keys.next("checklist", name, steps, r.line("Last activity"), r.line("Created by"))
		p.issue(issueChecklistWithoutSteps, issueSeverityInfo, section, key,
			fmt.Sprintf("Checklist %q (%s steps) is listed without its steps in the export, so it is not imported.", name, steps))
	}
	return nil
}

// unimportedSections raises the empty-section and not-imported findings for
// the sections the importer does not turn into records.
func (p *ywParser) unimportedSections(doc *html.Node) error {
	notImported := []string{"crew", "services", "cruises", "general", "readings", "expenses"}
	for _, id := range notImported {
		sec := p.sections[id]
		if sec == nil {
			continue
		}
		name := p.title(id)
		if ywFind(sec, ywByClass("vx-empty")) != nil {
			p.issue(issueEmptySection, issueSeverityInfo, name, "", fmt.Sprintf("%s has nothing in it.", name))
			continue
		}
		count := len(ywRows(sec))
		p.issue(issueSectionNotImported, issueSeverityInfo, name, "",
			fmt.Sprintf("%s lists %d records. This import does not bring them in.", name, count))
	}
	return nil
}

// sectionInfo fills the section table the review page opens with: how many
// records each section listed and how many reach a staged list.
func (p *ywParser) sectionInfo() {
	importable := 0
	for _, sp := range p.st.Particulars {
		if sp.Field != "" && !sp.SignalK && sp.Value != "" {
			importable++
		}
	}
	notes, tasks, skipped := 0, 0, 0
	for _, n := range p.st.Notes {
		if n.Kind == stagedNoteKindTask {
			tasks++
		} else {
			notes++
			if n.Skip {
				skipped++
			}
		}
	}
	docs, photos := 0, 0
	for _, f := range p.st.Files {
		if f.Kind == stagedFileKindPhoto {
			photos++
		} else {
			docs++
		}
	}
	engines, equip := 0, 0
	for _, e := range p.st.Equipment {
		if e.Kind == stagedKindEngine {
			engines++
		} else {
			equip++
		}
	}

	listed := map[string]int{}
	imported := map[string]int{}
	for id, sec := range p.sections {
		if ywFind(sec, ywByClass("vx-empty")) != nil {
			continue
		}
		if id == "particulars" {
			listed[id] = len(p.st.Particulars)
			continue
		}
		if id == "notes" {
			listed[id] = len(ywFindAll(sec, ywByClass("vx-note")))
			continue
		}
		if id == "photos" {
			listed[id] = len(ywFindAll(sec, ywByClass("vx-thumb")))
			continue
		}
		listed[id] = len(ywRows(sec))
	}
	imported["particulars"] = importable
	imported["engines"] = engines
	imported["equipment"] = equip
	imported["inventory"] = len(p.st.Spares)
	imported["maintenance"] = len(p.st.LogEntries)
	imported["tasks"] = tasks
	imported["notes"] = notes - skipped
	imported["documents"] = docs
	imported["photos"] = photos

	for _, id := range yachtWaveSectionIDs {
		if p.sections[id] == nil {
			continue
		}
		p.st.Sections = append(p.st.Sections, stagedSectionInfo{Name: p.title(id), Listed: listed[id], Imported: imported[id]})
	}
}
