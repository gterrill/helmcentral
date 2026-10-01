package main

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
)

// These tests drive the import store end to end against the redacted real
// export: defaults, validation, and the single transaction that writes
// everything. They use the real store and a real documents directory, so a
// note created by an import is the note the capture box would create.

func stageFixtureRun(t *testing.T, store *documentStore) importRun {
	t.Helper()
	staged := parseFixture(t)
	decisions, err := store.DefaultImportDecisions(staged)
	if err != nil {
		t.Fatalf("DefaultImportDecisions: %v", err)
	}
	run, err := store.CreateImportRun(importSourceYachtWave, "test export", "sha-of-file", staged, decisions)
	if err != nil {
		t.Fatalf("CreateImportRun: %v", err)
	}
	return run
}

// readyToCommit settles what the wizard would have: a choice for every
// ambiguous log entry, and every file skipped.
func readyToCommit(t *testing.T, store *documentStore, run importRun) importRun {
	t.Helper()
	patch := importDecisions{
		LogEquipment: map[string]importEquipmentRef{},
		Files:        map[string]importFileDecision{},
	}
	for _, e := range run.Staged.LogEntries {
		if len(e.Candidates) > 0 {
			patch.LogEquipment[e.Key] = importEquipmentRef{EquipmentKey: e.Candidates[0]}
		}
	}
	for _, f := range run.Staged.Files {
		d := run.Decisions.Files[f.Key]
		d.Skipped = true
		patch.Files[f.Key] = d
	}
	out, err := store.MergeImportDecisions(run.ID, patch)
	if err != nil {
		t.Fatalf("MergeImportDecisions: %v", err)
	}
	return out
}

func countImportRows(t *testing.T, store *documentStore, query string, args ...any) int {
	t.Helper()
	var n int
	if err := store.db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func TestImportRun_CreateAndGetRoundTrip(t *testing.T) {
	store := withTestDocumentStore(t)
	run := stageFixtureRun(t, store)

	if run.ID == "" || run.Status != "draft" || run.Source != "yachtwave" || run.FileSHA256 != "sha-of-file" {
		t.Fatalf("run = %+v", run)
	}
	got, err := store.GetImportRun(run.ID)
	if err != nil {
		t.Fatalf("GetImportRun: %v", err)
	}
	if len(got.Staged.Equipment) != 14 || got.Staged.Vessel.Name != "Pikorua" {
		t.Fatalf("staged payload did not survive the round trip: %+v", got.Staged.Vessel)
	}
	if got.AlreadyImported == nil || len(got.AlreadyImported) != 0 {
		t.Fatalf("already_imported must be an empty list, got %#v", got.AlreadyImported)
	}
	if _, err := store.GetImportRun("nope"); !errors.Is(err, errImportRunNotFound) {
		t.Fatalf("unknown id: %v", err)
	}
}

func TestImportDefaults_ComeFromTheIssuesAndTheRegistry(t *testing.T) {
	store := withTestDocumentStore(t)
	zone, err := store.CreateZone("Flybridge")
	if err != nil {
		t.Fatalf("CreateZone: %v", err)
	}
	existing, err := store.CreateEquipment(equipmentItem{Name: "ZEN 100", Quantity: 1})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}

	st := parseFixture(t)
	d, err := store.DefaultImportDecisions(st)
	if err != nil {
		t.Fatalf("DefaultImportDecisions: %v", err)
	}

	for _, l := range st.Locations {
		got := d.Zones[l.Key]
		if l.Name == "Flybridge" {
			if got.Action != "match" || got.ZoneID != zone.ID {
				t.Errorf("an existing zone of the same name should default to match: %+v", got)
			}
		} else if got.Action != "create" {
			t.Errorf("zone %s default = %+v", l.Name, got)
		}
	}

	byName := map[string]stagedEquipment{}
	for _, e := range st.Equipment {
		byName[e.Name] = e
	}
	if got := d.Records[byName["ZEN 100"].Key]; got.Action != "match" || got.TargetID != existing.ID {
		t.Errorf("existing equipment of the same name should default to match: %+v", got)
	}
	if got := d.Records[byName["xys"].Key]; got.Action != "skip" {
		t.Errorf("a junk name defaults to skip: %+v", got)
	}
	if got := d.Records[byName["BR1-PRO 5G"].Key]; got.Action != "skip" {
		t.Errorf("the flagged duplicate defaults to skip: %+v", got)
	}
	if got := d.Records[byName["TZT2BB"].Key]; got.Action != "create" {
		t.Errorf("an ordinary item defaults to create: %+v", got)
	}

	for _, n := range st.Notes {
		want := "create"
		if n.Skip {
			want = "skip"
		}
		if got := d.Records[n.Key]; got.Action != want {
			t.Errorf("note %q default = %+v, want %s", n.Title, got, want)
		}
	}

	// The second of two identical log rows starts skipped.
	var nov []stagedLogEntry
	for _, e := range st.LogEntries {
		if e.Date == "2024-11-16" {
			nov = append(nov, e)
		}
	}
	if d.Records[nov[0].Key].Action != "create" || d.Records[nov[1].Key].Action != "skip" {
		t.Errorf("duplicate log rows: %+v / %+v", d.Records[nov[0].Key], d.Records[nov[1].Key])
	}

	// Ambiguous entries get no pre-made choice: the operator picks.
	for _, e := range st.LogEntries {
		if _, chosen := d.LogEquipment[e.Key]; chosen && len(e.Candidates) > 0 {
			t.Errorf("ambiguous entry %q was pre-decided", e.Title)
		}
	}

	// Only importable particulars start as apply.
	for _, p := range st.Particulars {
		got, present := d.Particulars[p.Key]
		importable := p.Field != "" && !p.SignalK && p.Value != ""
		if importable && got != "apply" {
			t.Errorf("particular %s should start as apply", p.Label)
		}
		if !importable && present {
			t.Errorf("particular %s should have no decision", p.Label)
		}
	}

	if len(d.Files) != len(st.Files) {
		t.Errorf("every file gets a decision slot, got %d of %d", len(d.Files), len(st.Files))
	}
	for _, f := range st.Files {
		if got := d.Files[f.Key]; got.Skipped || got.DocumentID != "" {
			t.Errorf("file %s should start undecided: %+v", f.Label, got)
		}
	}
}

func TestImportDecisions_Validation(t *testing.T) {
	store := withTestDocumentStore(t)
	run := stageFixtureRun(t, store)
	st := run.Staged

	var someEquip, someLog, noteSkip, signalK, noField string
	someEquip = st.Equipment[0].Key
	someLog = st.LogEntries[0].Key
	for _, n := range st.Notes {
		if n.Skip {
			noteSkip = n.Key
		}
	}
	for _, p := range st.Particulars {
		if p.SignalK {
			signalK = p.Key
		}
		if p.Field == "" && !p.SignalK {
			noField = p.Key
		}
	}
	if noteSkip == "" || signalK == "" || noField == "" {
		t.Fatalf("fixture lacks the records this test needs")
	}

	bad := map[string]importDecisions{
		"unknown action":            {Records: map[string]importRecordDecision{someEquip: {Action: "destroy"}}},
		"match without target":      {Records: map[string]importRecordDecision{someEquip: {Action: "match"}}},
		"unknown record key":        {Records: map[string]importRecordDecision{"zzzz": {Action: "skip"}}},
		"match on a log entry":      {Records: map[string]importRecordDecision{someLog: {Action: "match", TargetID: "x"}}},
		"creating a skipped note":   {Records: map[string]importRecordDecision{noteSkip: {Action: "create"}}},
		"zone match without id":     {Zones: map[string]importZoneDecision{st.Locations[0].Key: {Action: "match"}}},
		"unknown zone key":          {Zones: map[string]importZoneDecision{"zzzz": {Action: "create"}}},
		"apply a signalk value":     {Particulars: map[string]string{signalK: "apply"}},
		"apply an unmapped value":   {Particulars: map[string]string{noField: "apply"}},
		"unknown particular action": {Particulars: map[string]string{st.Particulars[1].Key: "maybe"}},
		"unknown log entry":         {LogEquipment: map[string]importEquipmentRef{"zzzz": {}}},
		"both equipment refs":       {LogEquipment: map[string]importEquipmentRef{someLog: {EquipmentKey: someEquip, EquipmentID: "x"}}},
		"unknown equipment key":     {LogEquipment: map[string]importEquipmentRef{someLog: {EquipmentKey: "zzzz"}}},
		"unknown file key":          {Files: map[string]importFileDecision{"zzzz": {Skipped: true}}},
	}
	for name, patch := range bad {
		t.Run(name, func(t *testing.T) {
			_, err := store.MergeImportDecisions(run.ID, patch)
			if !errors.Is(err, errImportInvalid) {
				t.Fatalf("expected errImportInvalid, got %v", err)
			}
		})
	}

	// A rejected patch changes nothing.
	after, _ := store.GetImportRun(run.ID)
	if after.Decisions.Records[someEquip].Action != "create" {
		t.Fatalf("a rejected patch must leave the decisions alone")
	}
}

func TestImportDecisions_PatchMergesAndNeverSetsADocument(t *testing.T) {
	store := withTestDocumentStore(t)
	run := stageFixtureRun(t, store)
	f := run.Staged.Files[0]
	e := run.Staged.Equipment[0]

	// The document id belongs to the upload route; a patch cannot set it.
	got, err := store.MergeImportDecisions(run.ID, importDecisions{
		Files:   map[string]importFileDecision{f.Key: {DocumentID: "sneaky", Skipped: true}},
		Records: map[string]importRecordDecision{e.Key: {Action: "skip"}},
	})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if got.Decisions.Files[f.Key].DocumentID != "" || !got.Decisions.Files[f.Key].Skipped {
		t.Fatalf("file decision = %+v", got.Decisions.Files[f.Key])
	}
	if got.Decisions.Records[e.Key].Action != "skip" {
		t.Fatalf("record decision not merged")
	}
	// Untouched entries survive.
	if got.Decisions.Records[run.Staged.Equipment[1].Key].Action != "create" {
		t.Fatalf("merge must keep the entries the patch did not name")
	}

	doc := insertTestDocumentWithFile(t, store, "sha-one", "one.pdf", "application/pdf", []byte("%PDF-1.4 x"))
	got, err = store.SetImportFileDocument(run.ID, f.Key, doc.ID)
	if err != nil {
		t.Fatalf("SetImportFileDocument: %v", err)
	}
	if d := got.Decisions.Files[f.Key]; d.DocumentID != doc.ID || d.Skipped {
		t.Fatalf("after upload: %+v", d)
	}
	// A later patch keeps it.
	got, err = store.MergeImportDecisions(run.ID, importDecisions{Files: map[string]importFileDecision{f.Key: {Skipped: false}}})
	if err != nil || got.Decisions.Files[f.Key].DocumentID != doc.ID {
		t.Fatalf("a patch must not drop the uploaded document: %+v %v", got.Decisions.Files[f.Key], err)
	}

	if _, err := store.SetImportFileDocument(run.ID, "zzzz", doc.ID); !errors.Is(err, errImportInvalid) {
		t.Fatalf("unknown file key: %v", err)
	}
	if _, err := store.SetImportFileDocument(run.ID, f.Key, "no-such-document"); !errors.Is(err, errDocumentNotFound) {
		t.Fatalf("unknown document: %v", err)
	}
}

func TestImportRun_AbandonedAndCommittedRunsRefuseChange(t *testing.T) {
	store := withTestDocumentStore(t)
	run := stageFixtureRun(t, store)

	abandoned, err := store.AbandonImportRun(run.ID)
	if err != nil || abandoned.Status != "abandoned" {
		t.Fatalf("Abandon: %+v %v", abandoned, err)
	}
	if _, err := store.MergeImportDecisions(run.ID, importDecisions{}); !errors.Is(err, errImportRunNotDraft) {
		t.Fatalf("patching an abandoned run: %v", err)
	}
	if _, err := store.AbandonImportRun(run.ID); !errors.Is(err, errImportRunNotDraft) {
		t.Fatalf("abandoning twice: %v", err)
	}
	if _, err := commitImportRun(run.ID); !errors.Is(err, errImportRunNotDraft) {
		t.Fatalf("committing an abandoned run: %v", err)
	}
}

func TestMapImportSystem(t *testing.T) {
	for in, want := range map[string]string{
		"Inboard":                 "propulsion",
		"Generator":               "electrical",
		"Water (Maker)":           "water",
		"Electronics (Radar)":     "navigation",
		"Radio (VHF)":             "navigation",
		"Antenna (Satellite TV)":  "navigation",
		"Beacon (EPIRB)":          "safety",
		"A/V (Television)":        "appliances",
		"Engine":                  "propulsion",
		"Plumbing":                "water",
		"High Voltage":            "electrical",
		"Tools":                   "other",
		"Other":                   "other",
		"":                        "other",
		"Rigging and Sails":       "anchoring",
		"HVAC":                    "hvac",
		"Food (General)":          "other",
		"Electronics Satellite":   "navigation",
		"Audio/Visual (AV) plain": "appliances",
	} {
		if got := mapImportSystem(in); got != want {
			t.Errorf("mapImportSystem(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestImportCommit_WritesTheWholeExportInOneGo(t *testing.T) {
	store := withTestDocumentStore(t)
	run := readyToCommit(t, store, stageFixtureRun(t, store))

	// One real document to link to ZEN 100 at commit.
	var zen stagedEquipment
	for _, e := range run.Staged.Equipment {
		if e.Name == "ZEN 100" {
			zen = e
		}
	}
	doc := insertTestDocumentWithFile(t, store, "sha-manual", "zen.pdf", "application/pdf", []byte("%PDF-1.4 zen"))
	photo := insertTestDocumentWithFile(t, store, "sha-photo", "zen.png", "image/png", []byte("\x89PNG\r\n\x1a\nxx"))
	files := run.Staged.Files
	merged, err := store.SetImportFileDocument(run.ID, files[0].Key, doc.ID)
	if err != nil {
		t.Fatalf("SetImportFileDocument: %v", err)
	}
	if _, err := store.SetImportFileDocument(run.ID, files[1].Key, photo.ID); err != nil {
		t.Fatalf("SetImportFileDocument: %v", err)
	}
	_, err = store.MergeImportDecisions(merged.ID, importDecisions{Files: map[string]importFileDecision{
		files[1].Key: {EquipmentKey: zen.Key},
	}})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}

	result, err := commitImportRun(run.ID)
	if err != nil {
		t.Fatalf("commitImportRun: %v", err)
	}
	if result.Run.Status != "committed" || result.Run.CommittedAt == nil {
		t.Fatalf("run after commit = %+v", result.Run)
	}

	// Zones.
	if n := countImportRows(t, store, `SELECT COUNT(*) FROM inventory_zones`); n != 9 {
		t.Errorf("zones = %d, want 9", n)
	}

	// Particulars: the importable ones landed, SignalK-owned ones did not.
	p, err := store.GetVesselParticulars()
	if err != nil {
		t.Fatalf("GetVesselParticulars: %v", err)
	}
	if p.Builder != "Granocean" || p.Model != "W-60" || p.HIN != "XXTST00001A000" || p.Year == nil || *p.Year != 2024 ||
		p.Flag != "Cook Islands" || p.HullType != "Catamaran" || p.SystemVoltage != "24v" {
		t.Errorf("particulars = %+v", p)
	}
	if p.DisplacementKG == nil || *p.DisplacementKG != 32 {
		t.Errorf("displacement must land exactly as exported (32), got %v", p.DisplacementKG)
	}

	// Equipment: 14 engines and items minus the junk and the duplicate.
	if n := countImportRows(t, store, `SELECT COUNT(*) FROM equipment WHERE status = 'deployed'`); n != 12 {
		t.Errorf("deployed equipment = %d, want 12", n)
	}
	if n := countImportRows(t, store, `SELECT COUNT(*) FROM equipment WHERE name = 'xys' OR name = 'BR1-PRO 5G'`); n != 0 {
		t.Errorf("flagged records must not be created, found %d", n)
	}
	var sys, detail, inst, notes, zoneName string
	if err := store.db.QueryRow(`SELECT e.system, e.location_detail, e.install_date, e.notes, z.name FROM equipment e JOIN inventory_zones z ON z.id = e.zone_id WHERE e.name = 'DRS4D-NX-E'`).
		Scan(&sys, &detail, &inst, &notes, &zoneName); err != nil {
		t.Fatalf("radar row: %v", err)
	}
	if sys != "navigation" || detail != "Flybridge" || inst != "2024-06-17" || zoneName != "Flybridge" || !strings.Contains(notes, "Electronics (Radar)") {
		t.Errorf("radar = %s/%s/%s/%s/%q", sys, detail, inst, zoneName, notes)
	}

	// Spares: stock figures, bins and the items in them.
	var pn string
	var qty int
	var req *int
	var status string
	if err := store.db.QueryRow(`SELECT part_number, quantity, required_quantity, status FROM equipment WHERE name = 'Racor Seal Kit'`).Scan(&pn, &qty, &req, &status); err != nil {
		t.Fatalf("seal kit: %v", err)
	}
	if pn != "21669" || qty != 0 || req == nil || *req != 1 || status != "stored" {
		t.Errorf("seal kit = %s/%d/%v/%s", pn, qty, req, status)
	}
	if n := countImportRows(t, store, `SELECT COUNT(*) FROM inventory_bins`); n != 4 {
		t.Errorf("bins = %d, want 4", n)
	}
	if n := countImportRows(t, store, `SELECT COUNT(*) FROM equipment e JOIN inventory_bins b ON b.id = e.bin_id WHERE b.code = 'FB Lounge - Locker 1'`); n != 5 {
		t.Errorf("locker 1 items = %d, want 5", n)
	}
	if n := countImportRows(t, store, `SELECT COUNT(*) FROM equipment WHERE status = 'stored'`); n != 32+15 {
		t.Errorf("stored items = %d, want 47 (32 stock rows + 15 locker items)", n)
	}

	// Maintenance log.
	if n := countImportRows(t, store, `SELECT COUNT(*) FROM maintenance_log_entries`); n != 12 {
		t.Errorf("log entries = %d, want 12 (13 less the repeated row)", n)
	}
	var kind, desc, performed string
	var hours *float64
	var eqName *string
	if err := store.db.QueryRow(`SELECT l.kind, l.description, l.performed_at, l.hours, e.name FROM maintenance_log_entries l LEFT JOIN equipment e ON e.id = l.equipment_id
		WHERE l.performed_at = '2026-01-26' AND e.name = 'Cummins Onan'`).Scan(&kind, &desc, &performed, &hours, &eqName); err != nil {
		t.Fatalf("generator entry: %v", err)
	}
	if kind != "maintenance" || hours == nil || *hours != 467 || !strings.HasPrefix(desc, "Complete Service\n\nGenerator service:") {
		t.Errorf("generator entry = %s %v %q", kind, hours, desc[:40])
	}
	if n := countImportRows(t, store, `SELECT COUNT(*) FROM maintenance_log_entries WHERE equipment_id IS NULL`); n != 5 {
		t.Errorf("entries without equipment = %d, want 5", n)
	}

	// Notes: 13 + 9 tasks, less the three with credentials.
	notesList, err := store.ListNotes(false, "", "", 100, 0)
	if err != nil {
		t.Fatalf("ListNotes: %v", err)
	}
	if len(notesList) != 19 {
		t.Errorf("notes = %d, want 19", len(notesList))
	}
	titles := map[string]bool{}
	for _, n := range notesList {
		titles[n.Title] = true
	}
	for _, want := range []string{"Task: Racor Filter valve", "Haulout", "Autopilot"} {
		if !titles[want] {
			t.Errorf("note %q missing", want)
		}
	}
	for _, banned := range []string{"Wifi", "Windows Password", "Handover Tips"} {
		if titles[banned] {
			t.Errorf("note %q holds a credential and must not exist", banned)
		}
	}

	// Files: the document is linked to ZEN 100 (as a photo, being a PNG).
	if n := countImportRows(t, store, `SELECT COUNT(*) FROM equipment_documents ed JOIN equipment e ON e.id = ed.equipment_id WHERE e.name = 'ZEN 100' AND ed.document_id = ?`, photo.ID); n != 1 {
		t.Errorf("photo link = %d, want 1", n)
	}
	if n := countImportRows(t, store, `SELECT COUNT(*) FROM equipment_documents WHERE document_id = ?`, doc.ID); n != 0 {
		t.Errorf("a file with no equipment decision is imported as a document only")
	}

	// Provenance.
	if n := countImportRows(t, store, `SELECT COUNT(*) FROM import_records WHERE run_id = ? AND target_kind = 'zone'`, run.ID); n != 9 {
		t.Errorf("zone records = %d", n)
	}
	if n := countImportRows(t, store, `SELECT COUNT(*) FROM import_records WHERE run_id = ? AND target_kind = 'note'`, run.ID); n != 19 {
		t.Errorf("note records = %d", n)
	}
	if n := countImportRows(t, store, `SELECT COUNT(*) FROM import_records WHERE run_id = ? AND target_kind = 'equipment'`, run.ID); n != 12+32+15 {
		t.Errorf("equipment records = %d, want 59", n)
	}

	if result.Summary.Counts["equipment"].Created != 12+32+15 || result.Summary.Counts["notes"].Created != 19 ||
		result.Summary.Counts["zones"].Created != 9 || result.Summary.Counts["bins"].Created != 4 || result.Summary.Counts["log_entries"].Created != 12 {
		t.Errorf("summary counts = %+v", result.Summary.Counts)
	}
	if len(result.Summary.Records) == 0 {
		t.Errorf("summary lists the records written")
	}

	// A run commits once.
	if _, err := commitImportRun(run.ID); !errors.Is(err, errImportRunNotDraft) {
		t.Fatalf("second commit: %v", err)
	}

	// A fresh run of the same file now knows what is already in.
	again := stageFixtureRun(t, store)
	if len(again.AlreadyImported) == 0 {
		t.Fatalf("expected already_imported keys on the second run")
	}
	for _, e := range again.Staged.Equipment {
		if e.Name == "TZT2BB" {
			imported := false
			for _, k := range again.AlreadyImported {
				if k == e.Key {
					imported = true
				}
			}
			if !imported {
				t.Errorf("TZT2BB should be reported as already imported")
			}
			if again.Decisions.Records[e.Key].Action != "skip" {
				t.Errorf("an already-imported record defaults to skip, got %+v", again.Decisions.Records[e.Key])
			}
		}
	}
}

func TestImportCommit_MatchWritesNothingToTheMatchedRecord(t *testing.T) {
	store := withTestDocumentStore(t)
	existing, err := store.CreateEquipment(equipmentItem{Name: "ZEN 100", Manufacturer: "Mine", Quantity: 1})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	run := readyToCommit(t, store, stageFixtureRun(t, store))
	if _, err := commitImportRun(run.ID); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if n := countImportRows(t, store, `SELECT COUNT(*) FROM equipment WHERE name = 'ZEN 100'`); n != 1 {
		t.Fatalf("a matched item must not be created again, found %d", n)
	}
	got, _ := store.GetEquipment(existing.ID)
	if got.Manufacturer != "Mine" {
		t.Fatalf("a match must not overwrite the existing record: %+v", got)
	}
	// The log entry for ZEN 100 points at the existing record.
	if n := countImportRows(t, store, `SELECT COUNT(*) FROM maintenance_log_entries WHERE equipment_id = ?`, existing.ID); n != 1 {
		t.Fatalf("log entry should attach to the matched equipment, found %d", n)
	}
	if n := countImportRows(t, store, `SELECT COUNT(*) FROM import_records WHERE target_id = ? AND target_kind = 'equipment'`, existing.ID); n != 1 {
		t.Fatalf("the match is remembered")
	}
}

func TestImportCommit_RefusesWhatIsNotReady(t *testing.T) {
	store := withTestDocumentStore(t)

	t.Run("unresolved files", func(t *testing.T) {
		run := stageFixtureRun(t, store)
		// Settle the ambiguous entries only; leave the files undecided.
		patch := importDecisions{LogEquipment: map[string]importEquipmentRef{}}
		for _, e := range run.Staged.LogEntries {
			if len(e.Candidates) > 0 {
				patch.LogEquipment[e.Key] = importEquipmentRef{EquipmentKey: e.Candidates[0]}
			}
		}
		if _, err := store.MergeImportDecisions(run.ID, patch); err != nil {
			t.Fatalf("merge: %v", err)
		}
		_, err := commitImportRun(run.ID)
		if !errors.Is(err, errImportConflict) || !strings.Contains(err.Error(), "file") {
			t.Fatalf("expected a conflict naming the undecided files, got %v", err)
		}
		assertNothingWritten(t, store, run.ID)
	})

	t.Run("ambiguous log entry without a choice", func(t *testing.T) {
		run := stageFixtureRun(t, store)
		patch := importDecisions{Files: map[string]importFileDecision{}}
		for _, f := range run.Staged.Files {
			patch.Files[f.Key] = importFileDecision{Skipped: true}
		}
		if _, err := store.MergeImportDecisions(run.ID, patch); err != nil {
			t.Fatalf("merge: %v", err)
		}
		_, err := commitImportRun(run.ID)
		if !errors.Is(err, errImportConflict) || !strings.Contains(err.Error(), "equipment") {
			t.Fatalf("expected a conflict asking for an equipment choice, got %v", err)
		}
		assertNothingWritten(t, store, run.ID)
	})
}

// assertNothingWritten checks a refused or failed commit left the registry,
// the notes and the run exactly as they were.
func assertNothingWritten(t *testing.T, store *documentStore, runID string) {
	t.Helper()
	for table, want := range map[string]int{"inventory_zones": 0, "equipment": 0, "maintenance_log_entries": 0, "inventory_bins": 0, "import_records": 0, "vessel_particulars": 0} {
		if n := countImportRows(t, store, `SELECT COUNT(*) FROM `+table); n != want {
			t.Errorf("%s has %d rows after a refused commit, want %d", table, n, want)
		}
	}
	if n := countImportRows(t, store, `SELECT COUNT(*) FROM documents WHERE kind = 'note'`); n != 0 {
		t.Errorf("%d notes left behind by a refused commit", n)
	}
	run, err := store.GetImportRun(runID)
	if err != nil || run.Status != "draft" {
		t.Errorf("run after a refused commit = %+v %v", run.Status, err)
	}
}

func TestImportCommit_FailureRollsBackEverythingIncludingNotes(t *testing.T) {
	store := withTestDocumentStore(t)
	run := readyToCommit(t, store, stageFixtureRun(t, store))

	// A zone named like a location the run would create appears after the
	// run was staged. The preflight cannot see it coming; the transaction
	// finds it after the notes have already been written, and must undo them.
	if _, err := store.CreateZone(run.Staged.Locations[0].Name); err != nil {
		t.Fatalf("CreateZone: %v", err)
	}

	_, err := commitImportRun(run.ID)
	if !errors.Is(err, errImportConflict) {
		t.Fatalf("expected the commit to fail with a conflict, got %v", err)
	}
	// Everything except the zone the test made itself is gone.
	if n := countImportRows(t, store, `SELECT COUNT(*) FROM inventory_zones`); n != 1 {
		t.Errorf("zones = %d, want only the one planted", n)
	}
	for table, want := range map[string]int{"equipment": 0, "maintenance_log_entries": 0, "inventory_bins": 0, "import_records": 0, "vessel_particulars": 0} {
		if n := countImportRows(t, store, `SELECT COUNT(*) FROM `+table); n != want {
			t.Errorf("%s has %d rows after a failed commit, want %d", table, n, want)
		}
	}
	if n := countImportRows(t, store, `SELECT COUNT(*) FROM documents WHERE kind = 'note'`); n != 0 {
		t.Errorf("%d notes left behind by a failed commit", n)
	}
	if got, _ := store.GetImportRun(run.ID); got.Status != "draft" {
		t.Errorf("run status = %s, want draft", got.Status)
	}

	// And the notes' files are gone with their rows.
	entries := documentsDirEntries(t)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("a note file survived the rollback: %s", e.Name())
		}
	}
}

func TestImportCommit_RefusesARecordAlreadyImported(t *testing.T) {
	store := withTestDocumentStore(t)
	first := readyToCommit(t, store, stageFixtureRun(t, store))
	if _, err := commitImportRun(first.ID); err != nil {
		t.Fatalf("first commit: %v", err)
	}

	second := readyToCommit(t, store, stageFixtureRun(t, store))
	// Force one already-imported record back to create.
	var tzt stagedEquipment
	for _, e := range second.Staged.Equipment {
		if e.Name == "TZT2BB" {
			tzt = e
		}
	}
	if _, err := store.MergeImportDecisions(second.ID, importDecisions{Records: map[string]importRecordDecision{tzt.Key: {Action: "create"}}}); err != nil {
		t.Fatalf("merge: %v", err)
	}
	_, err := commitImportRun(second.ID)
	if !errors.Is(err, errImportConflict) || !strings.Contains(err.Error(), "already imported") {
		t.Fatalf("expected an already-imported conflict, got %v", err)
	}
}

// stageChangedRun stages the fixture after mutate has edited it, the way a
// newer export of the same boat would differ.
func stageChangedRun(t *testing.T, store *documentStore, mutate func(*stagedImport)) importRun {
	t.Helper()
	staged := parseFixture(t)
	mutate(&staged)
	decisions, err := store.DefaultImportDecisions(staged)
	if err != nil {
		t.Fatalf("DefaultImportDecisions: %v", err)
	}
	run, err := store.CreateImportRun(importSourceYachtWave, "newer export", "sha-newer", staged, decisions)
	if err != nil {
		t.Fatalf("CreateImportRun: %v", err)
	}
	return readyToCommit(t, store, run)
}

func tztKey(t *testing.T, st stagedImport) string {
	t.Helper()
	for _, e := range st.Equipment {
		if e.Name == "TZT2BB" {
			return e.Key
		}
	}
	t.Fatal("fixture has no TZT2BB")
	return ""
}

func TestImportCommit_ReimportLogEntryAttachesToEarlierImportedEquipment(t *testing.T) {
	store := withTestDocumentStore(t)
	first := readyToCommit(t, store, stageFixtureRun(t, store))
	if _, err := commitImportRun(first.ID); err != nil {
		t.Fatalf("first commit: %v", err)
	}
	key := tztKey(t, first.Staged)
	var equipmentID string
	if err := store.db.QueryRow(`SELECT target_id FROM import_records WHERE external_key = ? AND target_kind = 'equipment'`, key).Scan(&equipmentID); err != nil {
		t.Fatalf("recorded id: %v", err)
	}

	second := stageChangedRun(t, store, func(st *stagedImport) {
		st.LogEntries = append(st.LogEntries, stagedLogEntry{Key: "log-new-1", Date: "2026-05-01", Title: "Changed oil", EquipmentKey: key})
	})
	if _, err := commitImportRun(second.ID); err != nil {
		t.Fatalf("second commit: %v", err)
	}
	var got sql.NullString
	if err := store.db.QueryRow(`SELECT equipment_id FROM maintenance_log_entries WHERE description LIKE 'Changed oil%'`).Scan(&got); err != nil {
		t.Fatalf("read log entry: %v", err)
	}
	if !got.Valid || got.String != equipmentID {
		t.Fatalf("new log entry equipment = %v, want %s", got, equipmentID)
	}
}

func TestImportCommit_ReimportLogEntryRefusesWhenEarlierEquipmentIsGone(t *testing.T) {
	store := withTestDocumentStore(t)
	first := readyToCommit(t, store, stageFixtureRun(t, store))
	if _, err := commitImportRun(first.ID); err != nil {
		t.Fatalf("first commit: %v", err)
	}
	key := tztKey(t, first.Staged)
	if _, err := store.db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`DELETE FROM equipment WHERE id = (SELECT target_id FROM import_records WHERE external_key = ? AND target_kind = 'equipment')`, key); err != nil {
		t.Fatalf("delete: %v", err)
	}
	second := stageChangedRun(t, store, func(st *stagedImport) {
		st.LogEntries = append(st.LogEntries, stagedLogEntry{Key: "log-new-1", Date: "2026-05-01", Title: "Changed oil", EquipmentKey: key})
	})
	_, err := commitImportRun(second.ID)
	if !errors.Is(err, errImportConflict) || !strings.Contains(err.Error(), "Changed oil") {
		t.Fatalf("expected a conflict naming the entry, got %v", err)
	}
}

func TestImportCommit_ReimportMatchedLockerDoesNotRerecordItems(t *testing.T) {
	store := withTestDocumentStore(t)
	first := readyToCommit(t, store, stageFixtureRun(t, store))
	if _, err := commitImportRun(first.ID); err != nil {
		t.Fatalf("first commit: %v", err)
	}
	var bin stagedSpare
	for _, sp := range first.Staged.Spares {
		if sp.Kind == stagedSpareBin && len(sp.Items) > 0 {
			bin = sp
			break
		}
	}
	if bin.Key == "" {
		t.Fatal("fixture has no locker with items")
	}
	var binID string
	if err := store.db.QueryRow(`SELECT target_id FROM import_records WHERE external_key = ? AND target_kind = 'bin'`, bin.Key).Scan(&binID); err != nil {
		t.Fatalf("bin record: %v", err)
	}
	before := countImportRows(t, store, `SELECT COUNT(*) FROM equipment`)

	second := stageChangedRun(t, store, func(*stagedImport) {})
	if _, err := store.MergeImportDecisions(second.ID, importDecisions{Records: map[string]importRecordDecision{bin.Key: {Action: importActionMatch, TargetID: binID}}}); err != nil {
		t.Fatalf("merge: %v", err)
	}
	if _, err := commitImportRun(second.ID); err != nil {
		t.Fatalf("second commit must succeed without a raw constraint error: %v", err)
	}
	if after := countImportRows(t, store, `SELECT COUNT(*) FROM equipment`); after != before {
		t.Fatalf("items were duplicated: %d -> %d", before, after)
	}
}

func TestImportDefaults_UnreadableParticularsAreSkippedAndCannotBeApplied(t *testing.T) {
	store := withTestDocumentStore(t)
	st := parseFixture(t)
	st.Particulars = append(st.Particulars,
		stagedParticular{Key: "p-bad-year", Label: "Year", Field: "year", Value: "nineteen"},
		stagedParticular{Key: "p-bad-disp", Label: "Displacement", Field: "displacement_kg", Value: "heavy"},
	)
	d, err := store.DefaultImportDecisions(st)
	if err != nil {
		t.Fatalf("DefaultImportDecisions: %v", err)
	}
	for _, k := range []string{"p-bad-year", "p-bad-disp"} {
		if got := d.Particulars[k]; got == importParticularApply {
			t.Errorf("%s defaults to apply; an unreadable value can never be written", k)
		}
	}
	d.Particulars["p-bad-year"] = importParticularApply
	if err := validateImportDecisions(st, d); !errors.Is(err, errImportInvalid) {
		t.Fatalf("applying an unreadable year must be a validation error, got %v", err)
	}
}

// A log entry's hours in the export are what the meter showed. Every other
// log write stores true hours, so an item with a recorded meter reset gets the
// offset applied here too.
func TestImportCommit_LogEntryHoursAreConvertedToTrueHours(t *testing.T) {
	store := withTestDocumentStore(t)
	first := readyToCommit(t, store, stageFixtureRun(t, store))
	if _, err := commitImportRun(first.ID); err != nil {
		t.Fatalf("first commit: %v", err)
	}
	key := tztKey(t, first.Staged)
	var equipmentID string
	if err := store.db.QueryRow(`SELECT target_id FROM import_records WHERE external_key = ? AND target_kind = 'equipment'`, key).Scan(&equipmentID); err != nil {
		t.Fatalf("recorded id: %v", err)
	}
	if _, err := store.db.Exec(`INSERT INTO hour_meter_resets (id, equipment_id, old_reading, new_reading, changed_at, created_at) VALUES ('h1', ?, 1000, 0, '2026-01-01', 1)`, equipmentID); err != nil {
		t.Fatalf("seed reset: %v", err)
	}

	hours := 50.0
	second := stageChangedRun(t, store, func(st *stagedImport) {
		st.LogEntries = append(st.LogEntries, stagedLogEntry{Key: "log-new-1", Date: "2026-05-01", Title: "Changed oil", EquipmentKey: key, Hours: &hours})
	})
	if _, err := commitImportRun(second.ID); err != nil {
		t.Fatalf("second commit: %v", err)
	}
	var got float64
	if err := store.db.QueryRow(`SELECT hours FROM maintenance_log_entries WHERE description LIKE 'Changed oil%'`).Scan(&got); err != nil {
		t.Fatalf("read log entry: %v", err)
	}
	if got != 1050 {
		t.Fatalf("hours = %v, want 1050 (gauge 50 plus the reset offset)", got)
	}
}

func TestParticularUnreadable_YearOutsideTheVesselRangeIsRefused(t *testing.T) {
	for _, v := range []string{"0", "85", "1799", "2201"} {
		if particularUnreadable(stagedParticular{Field: "year", Value: v}) == "" {
			t.Errorf("year %q is outside what the vessel record accepts but was called readable", v)
		}
	}
	for _, v := range []string{"1800", "1998", "2200"} {
		if got := particularUnreadable(stagedParticular{Field: "year", Value: v}); got != "" {
			t.Errorf("year %q should be readable, got %q", v, got)
		}
	}
}
