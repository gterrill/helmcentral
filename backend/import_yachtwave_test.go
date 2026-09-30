package main

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

// These tests parse a redacted copy of a real YachtWave "Vessel Export"
// (testdata/yachtwave/export_redacted.html): structure exactly as exported,
// with the credentials, the HIN and the analytics token replaced. The shapes
// the assertions lean on (the newline-separated locker rows, the " · "
// separated detail line, the ambiguous engine names) are the real ones, not
// invented for the test.

func parseFixture(t *testing.T) stagedImport {
	t.Helper()
	raw, err := os.ReadFile("testdata/yachtwave/export_redacted.html")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	staged, err := parseYachtWaveExport(raw)
	if err != nil {
		t.Fatalf("parseYachtWaveExport: %v", err)
	}
	return staged
}

func issuesWithCode(st stagedImport, code string) []stagedIssue {
	var out []stagedIssue
	for _, is := range st.Issues {
		if is.Code == code {
			out = append(out, is)
		}
	}
	return out
}

func TestParseYachtWave_RejectsAFileThatIsNotAVesselExport(t *testing.T) {
	cases := map[string]string{
		"empty":        "",
		"random html":  "<html><head><title>Hello</title></head><body><p>hi</p></body></html>",
		"wrong report": "<html><head><title>As Fitted: Pikorua</title></head><body><td id=\"particulars\"></td></body></html>",
		"not html":     "just some text that is not a report",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseYachtWaveExport([]byte(body)); err == nil {
				t.Fatalf("expected an error for %s", name)
			} else if !strings.Contains(err.Error(), "YachtWave Vessel Export") {
				t.Fatalf("expected the error to say what was expected, got %v", err)
			}
		})
	}
}

func TestParseYachtWave_VesselAndSections(t *testing.T) {
	st := parseFixture(t)
	if st.Source != "yachtwave" {
		t.Fatalf("source = %q", st.Source)
	}
	if st.Vessel.Name != "Pikorua" || st.Vessel.GeneratedAt != "30 Sep 2026, 19:28 UTC" {
		t.Fatalf("vessel = %+v", st.Vessel)
	}
}

func TestParseYachtWave_Particulars(t *testing.T) {
	st := parseFixture(t)
	by := map[string]stagedParticular{}
	for _, p := range st.Particulars {
		by[p.Label] = p
	}

	want := map[string]struct {
		field   string
		value   string
		signalk bool
	}{
		"Hull ID (HIN)":   {"hin", "XXTST00001A000", false},
		"Displacement":    {"displacement_kg", "32 kg", false},
		"Hull type":       {"hull_type", "Catamaran", false},
		"Hull material":   {"hull_material", "Fiberglass", false},
		"Brand":           {"builder", "Granocean", false},
		"Model":           {"model", "W-60", false},
		"Year":            {"year", "2024", false},
		"Flag":            {"flag", "Cook Islands", false},
		"Hailing port":    {"hailing_port", "Sydney, Australia", false},
		"Shore power":     {"shore_power", "Dual × 125/250 volt | 30/50 amp", false},
		"System voltage":  {"system_voltage", "24v", false},
		"Date acquired":   {"date_acquired", "", false},
		"Registration":    {"registration", "", false},
		"IMO":             {"imo", "", false},
		"EPIRB beacon ID": {"epirb_id", "", false},
		"Length overall":  {"", "17.9 m", true},
		"Beam":            {"", "8.7 m", true},
		"Draft":           {"", "1.2 m", true},
		"Clearance":       {"", "5.64 m", true},
		"MMSI":            {"", "123456789", true},
		"Call sign":       {"", "XX1234", true},
		"Name":            {"", "Pikorua", true},
	}
	for label, w := range want {
		got, ok := by[label]
		if !ok {
			t.Errorf("missing particular %q", label)
			continue
		}
		if got.Field != w.field || got.Value != w.value || got.SignalK != w.signalk {
			t.Errorf("%s = %+v, want field=%q value=%q signalk=%v", label, got, w.field, w.value, w.signalk)
		}
		if got.Key == "" {
			t.Errorf("%s has no key", label)
		}
	}

	// The HIN chip ("Unverified") is not part of the value.
	if strings.Contains(by["Hull ID (HIN)"].Value, "Unverified") {
		t.Errorf("chip text leaked into the HIN value")
	}
}

func TestParseYachtWave_DisplacementIsFlaggedNotFixed(t *testing.T) {
	st := parseFixture(t)
	var disp stagedParticular
	for _, p := range st.Particulars {
		if p.Field == "displacement_kg" {
			disp = p
		}
	}
	if disp.Value != "32 kg" {
		t.Fatalf("displacement must stay as exported, got %q", disp.Value)
	}
	found := false
	for _, is := range issuesWithCode(st, issueImplausibleValue) {
		if is.Key == disp.Key && strings.Contains(is.Message, "32") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an implausible_value issue on the displacement, got %+v", st.Issues)
	}
}

func TestParseYachtWave_EngineAndEquipmentRows(t *testing.T) {
	st := parseFixture(t)
	if len(st.Equipment) != 14 {
		t.Fatalf("expected 3 engines + 11 equipment = 14, got %d", len(st.Equipment))
	}

	var engines, equip int
	for _, e := range st.Equipment {
		switch e.Kind {
		case "engine":
			engines++
		case "equipment":
			equip++
		default:
			t.Errorf("unexpected kind %q", e.Kind)
		}
	}
	if engines != 3 || equip != 11 {
		t.Fatalf("engines=%d equipment=%d", engines, equip)
	}

	byName := map[string][]stagedEquipment{}
	for _, e := range st.Equipment {
		byName[e.Name] = append(byName[e.Name], e)
	}

	qsb := byName["Cummins QSB 6.7"]
	if len(qsb) != 2 {
		t.Fatalf("expected two Cummins QSB 6.7 engines, got %d", len(qsb))
	}
	if qsb[0].Serial != "10000001" || qsb[1].Serial != "10000002" {
		t.Fatalf("engine serials = %q, %q", qsb[0].Serial, qsb[1].Serial)
	}
	if qsb[0].Type != "Inboard" || qsb[0].Installed != "2025-01-27" {
		t.Fatalf("engine = %+v", qsb[0])
	}
	if qsb[0].Detail != "Port · 550 hp · Diesel" {
		t.Fatalf("engine detail = %q", qsb[0].Detail)
	}
	if qsb[0].Key == qsb[1].Key {
		t.Fatalf("two engines share a key")
	}

	onan := byName["Cummins Onan"][0]
	if onan.Serial != "" {
		t.Fatalf("a -- serial must normalise to empty, got %q", onan.Serial)
	}

	radar := byName["DRS4D-NX-E"][0]
	if radar.Kind != "equipment" || radar.Location != "Flybridge" || radar.Manufacturer != "Furuno" ||
		radar.Type != "Electronics (Radar)" || radar.Installed != "2024-06-17" {
		t.Fatalf("radar = %+v", radar)
	}
	// "Flybridge · Electronics": everything before the last segment is the
	// location detail, the last is the source's own category.
	if radar.Detail != "Flybridge" || radar.Category != "Electronics" {
		t.Fatalf("radar detail/category = %q/%q", radar.Detail, radar.Category)
	}

	zen := byName["ZEN 100"][0]
	if zen.Detail != "" || zen.Category != "Water" || zen.Location != "Engine Room - Port" {
		t.Fatalf("zen = %+v", zen)
	}

	epirb := byName["GlobalFix V5"][0]
	if epirb.Category != "Safety & Security" || epirb.Serial != "EPIRB-REDACTED" {
		t.Fatalf("epirb = %+v (entities must be decoded)", epirb)
	}

	if byName["BR1-PRO 5G"][0].Installed != "" {
		t.Fatalf("-- install date must normalise to empty")
	}
}

func TestParseYachtWave_EquipmentIssues(t *testing.T) {
	st := parseFixture(t)

	junk := issuesWithCode(st, issueJunkName)
	if len(junk) != 1 || !strings.Contains(junk[0].Message, "xys") {
		t.Fatalf("expected one junk_name issue for xys, got %+v", junk)
	}

	dups := issuesWithCode(st, issueDuplicate)
	var brDup *stagedIssue
	for i := range dups {
		if dups[i].Section == "Equipment" {
			brDup = &dups[i]
		}
	}
	if brDup == nil || !strings.Contains(brDup.Message, "BR1") {
		t.Fatalf("expected BR1 PRO 5G / BR1-PRO 5G flagged as duplicates, got %+v", dups)
	}
	// The flagged one is the later copy, and its key is a real staged key.
	var later stagedEquipment
	for _, e := range st.Equipment {
		if e.Name == "BR1-PRO 5G" {
			later = e
		}
	}
	if brDup.Key != later.Key {
		t.Fatalf("duplicate issue key = %q, want the later record %q", brDup.Key, later.Key)
	}
}

func TestParseYachtWave_Spares(t *testing.T) {
	st := parseFixture(t)
	if len(st.Spares) != 36 {
		t.Fatalf("expected 36 inventory rows, got %d", len(st.Spares))
	}

	byName := map[string]stagedSpare{}
	for _, s := range st.Spares {
		byName[s.Name] = s
	}

	seal := byName["Racor Seal Kit"]
	if seal.Kind != "item" || seal.OnHand != 0 || seal.Required == nil || *seal.Required != 1 ||
		seal.PartNumber != "21669" || seal.Location != "Flybridge" ||
		seal.Detail != "Locker 2 - Under FB Dining Table" || seal.Category != "Engine" {
		t.Fatalf("seal kit = %+v", seal)
	}

	// On-hand only: the required figure is absent, not zero.
	oil := byName["Hydraulic Oil - Tenderlift"]
	if oil.OnHand != 1 || oil.Required != nil {
		t.Fatalf("on-hand-only row = %+v", oil)
	}
	if oil.Detail != "Mobil DTE 25 ULTRA Q/320585 MTC 314" || oil.Notes != "" {
		t.Fatalf("a single segment is the location detail: %+v", oil)
	}

	// " · " separates location detail from the note.
	bridle := byName["Bridle"]
	if bridle.Detail != "Large Starboard Forward Locker" || bridle.Notes != "Bridle used for anchoring" {
		t.Fatalf("bridle = %+v", bridle)
	}
	zinc := byName["Penoil Zinc Anode - Cummins"]
	if zinc.Detail != "Locker 2 - Under FB Dining Table" || zinc.Notes != "0130-4434" || zinc.PartNumber != "0130-4434" {
		t.Fatalf("zinc = %+v", zinc)
	}

	// A locker row whose detail is one item per line is a bin.
	locker := byName["FB Lounge - Locker 1"]
	if locker.Kind != "bin" {
		t.Fatalf("locker kind = %q", locker.Kind)
	}
	wantItems := []string{"SS Nut Kit", "SS Screws", "Tape", "Hose Clamps", "Socket Set"}
	if !reflect.DeepEqual(locker.Items, wantItems) {
		t.Fatalf("locker items = %#v", locker.Items)
	}
	bins := 0
	for _, s := range st.Spares {
		if s.Kind == "bin" {
			bins++
		}
		if s.Kind == "item" && s.Items == nil {
			// Items is always a slice, never null.
			t.Fatalf("%s: Items must be [] not null", s.Name)
		}
	}
	if bins != 4 {
		t.Fatalf("expected four locker bins, got %d", bins)
	}

	// Unknown part number: normalised to empty and flagged.
	if byName["Air Filter - Cummins"].PartNumber != "" {
		t.Fatalf("Unknown part number must normalise to empty")
	}
	unk := issuesWithCode(st, issueUnknownPartNumber)
	if len(unk) != 2 {
		t.Fatalf("expected two unknown_part_number issues, got %+v", unk)
	}
	if byName["Hydraulic Oil - PTO and/or Crane"].PartNumber != "" {
		t.Fatalf("-- part number must normalise to empty")
	}
}

func TestParseYachtWave_Locations(t *testing.T) {
	st := parseFixture(t)
	names := map[string]int{}
	for _, l := range st.Locations {
		if l.Key == "" {
			t.Errorf("location %q has no key", l.Name)
		}
		if _, dup := names[l.Name]; dup {
			t.Errorf("location %q listed twice", l.Name)
		}
		names[l.Name] = l.Count
	}
	for _, n := range []string{"Engine Room - Port", "Flybridge", "Flybridge - Starboard", "Lockers - Starboard", "Salon - Starboard", "Radar Arch"} {
		if _, ok := names[n]; !ok {
			t.Errorf("missing location %q in %v", n, names)
		}
	}
	if names["Flybridge"] < 10 {
		t.Errorf("Flybridge count = %d, expected the equipment and spares filed there", names["Flybridge"])
	}
}

func TestParseYachtWave_MaintenanceLog(t *testing.T) {
	st := parseFixture(t)
	if len(st.LogEntries) != 13 {
		t.Fatalf("expected 13 log entries, got %d", len(st.LogEntries))
	}

	var gen, watermaker stagedLogEntry
	for _, e := range st.LogEntries {
		if e.Date == "2026-01-26" && e.Type == "Generator" {
			gen = e
		}
		if e.Title == "Watermaker Service" {
			watermaker = e
		}
	}
	if gen.EquipmentName != "Cummins Onan" || gen.Hours == nil || *gen.Hours != 467 {
		t.Fatalf("generator entry = %+v", gen)
	}
	if gen.EquipmentKey == "" || len(gen.Candidates) != 0 {
		t.Fatalf("a unique name must resolve to its one equipment key: %+v", gen)
	}
	if !strings.HasPrefix(gen.Body, "Generator service:\n- Replace generator oil.") {
		t.Fatalf("body must keep its line breaks, got %q", gen.Body[:60])
	}
	if watermaker.EquipmentKey == "" {
		t.Fatalf("ZEN 100 should resolve to the staged ZEN 100")
	}

	// "Cummins QSB 6.7" names two engines: ambiguous, never guessed.
	amb := 0
	for _, e := range st.LogEntries {
		if e.EquipmentName == "Cummins QSB 6.7" {
			amb++
			if e.EquipmentKey != "" || len(e.Candidates) != 2 {
				t.Errorf("ambiguous entry resolved or lacks candidates: %+v", e)
			}
		}
	}
	if amb != 6 {
		t.Fatalf("expected six QSB entries, got %d", amb)
	}
	if n := len(issuesWithCode(st, issueAmbiguousEquipment)); n != 6 {
		t.Fatalf("expected six ambiguous_equipment issues, got %d", n)
	}

	// No equipment named: stays unlinked, title only.
	for _, e := range st.LogEntries {
		if e.Title == "Turn thru hole seacocks on and off" {
			if e.EquipmentName != "" || e.EquipmentKey != "" || e.Hours != nil || e.Body != "" {
				t.Errorf("seacock entry = %+v", e)
			}
		}
	}

	// The two identical Nov 2024 "Service" rows: distinct keys, the second
	// flagged as a duplicate.
	var nov []stagedLogEntry
	for _, e := range st.LogEntries {
		if e.Date == "2024-11-16" {
			nov = append(nov, e)
		}
	}
	if len(nov) != 2 || nov[0].Key == nov[1].Key {
		t.Fatalf("expected two Nov 2024 entries with distinct keys: %+v", nov)
	}
	flagged := false
	for _, is := range issuesWithCode(st, issueDuplicate) {
		if is.Section == "Maintenance Log" && is.Key == nov[1].Key {
			flagged = true
		}
		if is.Section == "Maintenance Log" && is.Key == nov[0].Key {
			t.Errorf("the first copy must not be flagged")
		}
	}
	if !flagged {
		t.Fatalf("second copy not flagged as duplicate")
	}
}

func TestParseYachtWave_NotesAndPasswordDetection(t *testing.T) {
	st := parseFixture(t)

	var notes, tasks int
	byTitle := map[string]stagedNote{}
	for _, n := range st.Notes {
		switch n.Kind {
		case "note":
			notes++
		case "task":
			tasks++
		}
		byTitle[n.Title+"|"+n.Date] = n
	}
	if notes != 13 || tasks != 9 {
		t.Fatalf("notes=%d tasks=%d, want 13 and 9", notes, tasks)
	}

	skipped := map[string]bool{}
	for _, n := range st.Notes {
		if n.Skip {
			skipped[n.Title] = true
			if n.SkipReason == "" {
				t.Errorf("%s is skipped without a reason", n.Title)
			}
		}
	}
	wantSkipped := map[string]bool{"Windows Password": true, "Wifi": true, "Handover Tips": true}
	if !reflect.DeepEqual(skipped, wantSkipped) {
		t.Fatalf("skipped notes = %v, want %v", skipped, wantSkipped)
	}
	// The secret is dropped at the parser: it is not in the staged payload
	// that gets stored and served.
	for _, n := range st.Notes {
		if n.Skip && n.Body != "" {
			t.Errorf("skipped note %q kept its body", n.Title)
		}
	}
	blob, _ := json.Marshal(st)
	for _, secret := range []string{"hunter2-redacted", "Redacted1"} {
		if strings.Contains(string(blob), secret) {
			t.Errorf("the staged payload contains %q", secret)
		}
	}
	if n := len(issuesWithCode(st, issuePasswordNoteSkipped)); n != 3 {
		t.Fatalf("expected three password_note_skipped issues, got %d", n)
	}

	// A note that merely mentions wifi is not a credential.
	for _, n := range st.Notes {
		if n.Title == "CZone Config" && n.Skip {
			t.Fatalf("CZone Config mentions wifi but holds no credential")
		}
	}

	ha := byTitle["Haulout|2026-04-28"]
	if ha.Body != "- Close seacocks for AC, engine and generator when lifting\n- Open fridge\n- Flush water maker\n- Shut off breakers in flybridge and main panel (except galley)" {
		t.Fatalf("haulout body = %q", ha.Body)
	}
	alt := byTitle["Alternators|2026-08-14"]
	if alt.Key == "" || alt.Kind != "note" {
		t.Fatalf("alternators = %+v", alt)
	}

	// Tasks become notes carrying every field in the body.
	var racor, done stagedNote
	for _, n := range st.Notes {
		if n.Title == "Task: Racor Filter valve" {
			racor = n
		}
		if n.Title == "Task: Install Rustdesk on Ship Computer" {
			done = n
		}
	}
	for _, want := range []string{"Priority: Low", "Assigned to: Alex Morgan", "Due: 2025-07-31", "Status: Overdue"} {
		if !strings.Contains(racor.Body, want) {
			t.Errorf("task body missing %q: %q", want, racor.Body)
		}
	}
	for _, want := range []string{"Status: Done", "Completed: 2026-03-03"} {
		if !strings.Contains(done.Body, want) {
			t.Errorf("completed task body missing %q: %q", want, done.Body)
		}
	}
}

func TestParseYachtWave_Files(t *testing.T) {
	st := parseFixture(t)
	var docs, photos int
	for _, f := range st.Files {
		if f.Key == "" || f.URL == "" {
			t.Errorf("file without key or url: %+v", f)
		}
		switch f.Kind {
		case "document":
			docs++
		case "photo":
			photos++
		}
	}
	if docs != 4 || photos != 3 {
		t.Fatalf("docs=%d photos=%d", docs, photos)
	}

	var mackay, photo stagedFile
	for _, f := range st.Files {
		if f.Label == "Mackay Marina" {
			mackay = f
		}
		if f.Kind == "photo" && f.AttachedTo == "Pikorua" {
			photo = f
		}
	}
	if mackay.Type != "Receipt" || mackay.AttachedTo != "Pikorua" || mackay.Date != "2025-01-29" ||
		mackay.Size != "103 KB PDF" ||
		mackay.URL != "https://documents.yachtwave.com/00000000-0000-4000-8000-000000000000/00000000-0000-4000-8000-000000000001.pdf" {
		t.Fatalf("mackay = %+v", mackay)
	}
	if photo.Date != "2025-02-26" || !strings.HasPrefix(photo.URL, "https://images.yachtwave.com/") {
		t.Fatalf("photo = %+v", photo)
	}
}

func TestParseYachtWave_ChecklistsAreIssuesNotRecords(t *testing.T) {
	st := parseFixture(t)
	cl := issuesWithCode(st, issueChecklistWithoutSteps)
	if len(cl) != 7 {
		t.Fatalf("expected one issue per checklist (7), got %d", len(cl))
	}
	for _, is := range cl {
		if is.Section != "Checklists" || is.Message == "" {
			t.Errorf("bad checklist issue %+v", is)
		}
	}
	for _, n := range st.Notes {
		if strings.Contains(n.Title, "Arrival") || strings.Contains(n.Title, "Departure") {
			t.Fatalf("checklists must not become notes: %q", n.Title)
		}
	}
}

func TestParseYachtWave_EmptyAndUnimportedSections(t *testing.T) {
	st := parseFixture(t)
	empty := map[string]bool{}
	for _, is := range issuesWithCode(st, issueEmptySection) {
		empty[is.Section] = true
		if is.Severity != "info" {
			t.Errorf("empty section issue should be info: %+v", is)
		}
	}
	for _, n := range []string{"Service Schedules", "Cruise Log", "General Log", "Readings", "Expenses"} {
		if !empty[n] {
			t.Errorf("expected an empty_section issue for %s, got %v", n, empty)
		}
	}
	notImported := map[string]bool{}
	for _, is := range issuesWithCode(st, issueSectionNotImported) {
		notImported[is.Section] = true
	}
	if !notImported["Crew & Access"] {
		t.Errorf("crew has two people and is not imported: %v", notImported)
	}
}

func TestParseYachtWave_KeysAreStableAndUnique(t *testing.T) {
	a := parseFixture(t)
	b := parseFixture(t)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("two parses of one file differ")
	}

	seen := map[string]string{}
	add := func(kind, key string) {
		if key == "" {
			t.Errorf("%s with empty key", kind)
			return
		}
		if prev, dup := seen[key]; dup {
			t.Errorf("key %s shared by %s and %s", key, prev, kind)
		}
		seen[key] = kind
	}
	for _, x := range a.Particulars {
		add("particular "+x.Label, x.Key)
	}
	for _, x := range a.Locations {
		add("location "+x.Name, x.Key)
	}
	for _, x := range a.Equipment {
		add("equipment "+x.Name, x.Key)
	}
	for _, x := range a.Spares {
		add("spare "+x.Name, x.Key)
	}
	for _, x := range a.LogEntries {
		add("log "+x.Title, x.Key)
	}
	for _, x := range a.Notes {
		add("note "+x.Title, x.Key)
	}
	for _, x := range a.Files {
		add("file "+x.Label, x.Key)
	}
}

func TestParseYachtWave_ListsAreNeverNull(t *testing.T) {
	// A minimal valid export with no data still yields [] not null so the
	// browser never meets a null list.
	raw := `<html><head><title>Vessel Export: Tiny</title></head><body>
<img alt="YACHTWAVE"><p>Vessel Export</p>
<table><tr><td class="vx-section" id="particulars"><div class="vx-particulars"></div></td></tr></table>
</body></html>`
	st, err := parseYachtWaveExport([]byte(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if st.Particulars == nil || st.Locations == nil || st.Equipment == nil || st.Spares == nil ||
		st.LogEntries == nil || st.Notes == nil || st.Files == nil || st.Issues == nil || st.Sections == nil {
		t.Fatalf("expected every list non-nil: %+v", st)
	}
}
