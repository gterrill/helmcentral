package main

import (
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// This file is the commit: one run's decisions turned into registry records
// in a single transaction. Notes are the exception to "single transaction" -
// a note is a file on disk as well as a row, created by the same function the
// capture box uses - so they are written first and deleted again if the
// transaction then fails, which leaves the operator with all of the import or
// none of it.

// importCount is one line of the commit summary.
type importCount struct {
	Created int `json:"created"`
	Matched int `json:"matched"`
	Skipped int `json:"skipped"`
}

// importSummaryRecord is one record the commit wrote or matched, so the
// summary page can link to it. Kind is zone, bin, equipment, log_entry, note
// or document; Action is created or matched.
type importSummaryRecord struct {
	Kind   string `json:"kind"`
	Key    string `json:"key"`
	ID     string `json:"id"`
	Label  string `json:"label"`
	Action string `json:"action"`
}

// importSummary is what a commit reports. Counts is keyed by particulars,
// zones, bins, equipment, log_entries, notes and files.
type importSummary struct {
	Counts  map[string]importCount `json:"counts"`
	Records []importSummaryRecord  `json:"records"`
}

// importCommitResult is the commit route's response.
type importCommitResult struct {
	Run     importRun     `json:"run"`
	Summary importSummary `json:"summary"`
}

// importNoteBody is an imported note's body: the note as exported with a
// provenance line under it.
func importNoteBody(n stagedNote) string {
	body := strings.TrimRight(n.Body, "\n")
	footer := "_Imported from YachtWave."
	if n.Date != "" {
		footer = "_Imported from YachtWave. Dated " + n.Date + "._"
	} else {
		footer += "_"
	}
	if body == "" {
		return footer
	}
	return body + "\n\n" + footer
}

// commitImportRun writes the run, or writes nothing. See the file comment for
// why notes go first.
func commitImportRun(runID string) (importCommitResult, error) {
	store := globalDocumentStore
	run, err := store.GetImportRun(runID)
	if err != nil {
		return importCommitResult{}, err
	}
	if run.Status != importStatusDraft {
		return importCommitResult{}, errImportRunNotDraft
	}
	if err := store.preflightImportCommit(run); err != nil {
		return importCommitResult{}, err
	}

	noteDocs := map[string]string{}
	cleanup := func() {
		for _, id := range noteDocs {
			sha, err := store.Delete(id)
			if err != nil {
				log.Printf("import: roll back note %s: %v", id, err)
				continue
			}
			if err := removeDocumentFile(sha); err != nil {
				log.Printf("import: roll back note %s: remove file: %v", id, err)
			}
		}
	}
	for _, n := range run.Staged.Notes {
		if n.Skip || run.Decisions.Records[n.Key].Action != importActionCreate {
			continue
		}
		doc, err := createNoteDocument(createNoteRequest{Title: n.Title, Body: importNoteBody(n)})
		if err != nil {
			cleanup()
			return importCommitResult{}, fmt.Errorf("import note %q: %w", n.Title, err)
		}
		noteDocs[n.Key] = doc.ID
	}

	result, err := store.commitImportRunTx(run, noteDocs)
	if err != nil {
		cleanup()
		return importCommitResult{}, err
	}
	return result, nil
}

// preflightImportCommit runs the checks that need no write, so a commit that
// is going to be refused is refused before any note exists.
func (s *documentStore) preflightImportCommit(run importRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return checkImportReady(s.db, run)
}

// checkImportReady is everything a commit needs to be true before it writes:
// the decisions validate, every file is settled, every ambiguous log entry has
// a choice, nothing to be created was already imported, every match target
// exists. Called once before the notes are written and again inside the
// transaction.
func checkImportReady(q sqlQueryer, run importRun) error {
	st, d := run.Staged, run.Decisions
	if err := validateImportDecisions(st, d); err != nil {
		return err
	}

	var undecided []string
	for _, f := range st.Files {
		fd, ok := d.Files[f.Key]
		if !ok || (!fd.Skipped && fd.DocumentID == "") {
			undecided = append(undecided, f.Label)
		}
	}
	if len(undecided) > 0 {
		return importConflictf("%d file(s) have no decision yet (hand over the file or skip it): %s", len(undecided), joinFirst(undecided, 5))
	}

	var unchosen []string
	for _, e := range st.LogEntries {
		if d.Records[e.Key].Action != importActionCreate || len(e.Candidates) == 0 {
			continue
		}
		if _, ok := d.LogEquipment[e.Key]; !ok {
			unchosen = append(unchosen, fmt.Sprintf("%s (%s)", e.Title, e.Date))
		}
	}
	if len(unchosen) > 0 {
		return importConflictf("%d log entr(ies) name equipment that matches more than one item; choose the equipment: %s", len(unchosen), joinFirst(unchosen, 5))
	}

	recorded, err := importRecordedKeys(q, st.Source)
	if err != nil {
		return err
	}
	var already []string
	note := func(key, kind, label string) {
		if _, ok := recorded[key][kind]; ok {
			already = append(already, label)
		}
	}
	for _, l := range st.Locations {
		if d.Zones[l.Key].Action == importActionCreate {
			note(l.Key, importTargetZone, l.Name)
		}
	}
	for _, e := range st.Equipment {
		if d.Records[e.Key].Action == importActionCreate {
			note(e.Key, importTargetEquipment, e.Name)
		}
	}
	for _, sp := range st.Spares {
		if d.Records[sp.Key].Action != importActionCreate {
			continue
		}
		if sp.Kind == stagedSpareBin {
			note(sp.Key, importTargetBin, sp.Name)
		} else {
			note(sp.Key, importTargetEquipment, sp.Name)
		}
	}
	for _, e := range st.LogEntries {
		if d.Records[e.Key].Action == importActionCreate {
			note(e.Key, importTargetLogEntry, e.Title)
		}
	}
	for _, n := range st.Notes {
		if d.Records[n.Key].Action == importActionCreate {
			note(n.Key, importTargetNote, n.Title)
		}
	}
	for _, f := range st.Files {
		if fd := d.Files[f.Key]; !fd.Skipped && fd.DocumentID != "" {
			note(f.Key, "document", f.Label)
		}
	}
	if len(already) > 0 {
		return importConflictf("%d record(s) already imported by an earlier run (skip them or match them): %s", len(already), joinFirst(already, 5))
	}

	return checkImportMatchTargets(q, st, d, importConflictf)
}

func joinFirst(items []string, n int) string {
	if len(items) <= n {
		return strings.Join(items, ", ")
	}
	return strings.Join(items[:n], ", ") + fmt.Sprintf(", and %d more", len(items)-n)
}

// importWriter carries one commit's transaction and bookkeeping.
type importWriter struct {
	tx      *sql.Tx
	run     importRun
	now     time.Time
	summary importSummary

	zoneIDs      map[string]string // lower(location name) -> zone id, "" when skipped
	equipmentIDs map[string]string // staged equipment key -> equipment id
}

// resolveEquipment turns a staged equipment key into an equipment id: the
// item this run wrote or matched, else the one an earlier committed run of the
// same source recorded under that key (a re-import skips equipment it already
// has, so it is not in this run's lookup). "" means the key was never
// imported. A recorded item that has since been deleted is a conflict naming
// the entry that needed it, not a silent null.
func (w *importWriter) resolveEquipment(key, entry string) (string, error) {
	if id := w.equipmentIDs[key]; id != "" {
		return id, nil
	}
	var id string
	err := w.tx.QueryRow(`SELECT target_id FROM import_records WHERE source = ? AND external_key = ? AND target_kind = ? AND run_id != ?`,
		w.run.Source, key, importTargetEquipment, w.run.ID).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("commit import: look up earlier import of %q: %w", key, err)
	}
	ok, err := rowExists(w.tx, `SELECT 1 FROM equipment WHERE id = ?`, id)
	if err != nil {
		return "", fmt.Errorf("commit import: check earlier import of %q: %w", key, err)
	}
	if !ok {
		return "", importConflictf("%q belongs to equipment an earlier import created, which has since been deleted; choose other equipment or none", entry)
	}
	return id, nil
}

func (w *importWriter) count(kind string, f func(*importCount)) {
	c := w.summary.Counts[kind]
	f(&c)
	w.summary.Counts[kind] = c
}

// record writes one import_records row and adds the record to the summary.
// A match whose record already exists (the same zone matched again by a later
// run) is not an error.
func (w *importWriter) record(kind, key, targetID, label, action string) error {
	verb := "INSERT"
	if action == "matched" {
		verb = "INSERT OR IGNORE"
	} else {
		dup, err := rowExists(w.tx, `SELECT 1 FROM import_records WHERE source = ? AND external_key = ? AND target_kind = ?`, w.run.Source, key, kind)
		if err != nil {
			return fmt.Errorf("record import of %q: %w", label, err)
		}
		if dup {
			return importConflictf("%q was already imported by an earlier run (skip it or match it)", label)
		}
	}
	if _, err := w.tx.Exec(verb+` INTO import_records (run_id, source, external_key, target_kind, target_id, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		w.run.ID, w.run.Source, key, kind, targetID, w.now.Unix()); err != nil {
		return fmt.Errorf("record import of %q: %w", label, err)
	}
	summaryKind := kind
	w.summary.Records = append(w.summary.Records, importSummaryRecord{Kind: summaryKind, Key: key, ID: targetID, Label: label, Action: action})
	return nil
}

// commitImportRunTx writes everything in one transaction.
func (s *documentStore) commitImportRunTx(run importRun, noteDocs map[string]string) (importCommitResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return importCommitResult{}, fmt.Errorf("commit import: begin: %w", err)
	}
	defer tx.Rollback()

	// Re-read inside the transaction: the decisions that were checked a
	// moment ago are the ones that get written, and nothing else can change
	// them now.
	fresh, err := importRunByID(tx, run.ID)
	if err != nil {
		return importCommitResult{}, err
	}
	if fresh.Status != importStatusDraft {
		return importCommitResult{}, errImportRunNotDraft
	}
	if err := checkImportReady(tx, fresh); err != nil {
		return importCommitResult{}, err
	}

	w := &importWriter{
		tx: tx, run: fresh, now: s.now(),
		summary:      importSummary{Counts: map[string]importCount{}, Records: []importSummaryRecord{}},
		zoneIDs:      map[string]string{},
		equipmentIDs: map[string]string{},
	}
	for _, kind := range []string{"particulars", "zones", "bins", "equipment", "log_entries", "notes", "files"} {
		w.summary.Counts[kind] = importCount{}
	}

	steps := []func() error{w.writeParticulars, w.writeZones, w.writeEquipment, w.writeSpares, w.writeLog, w.writeNotes(noteDocs), w.writeFiles}
	for _, step := range steps {
		if err := step(); err != nil {
			return importCommitResult{}, err
		}
	}

	if _, err := tx.Exec(`UPDATE import_runs SET status = 'committed', committed_at = ?, updated_at = ? WHERE id = ?`,
		w.now.Unix(), w.now.Unix(), fresh.ID); err != nil {
		return importCommitResult{}, fmt.Errorf("commit import: mark committed: %w", err)
	}
	out, err := importRunByID(tx, fresh.ID)
	if err != nil {
		return importCommitResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return importCommitResult{}, fmt.Errorf("commit import: commit: %w", err)
	}
	return importCommitResult{Run: out, Summary: w.summary}, nil
}

// ── particulars ──────────────────────────────────────────────────────────

func (w *importWriter) writeParticulars() error {
	st, d := w.run.Staged, w.run.Decisions
	cur, err := vesselParticularsFrom(w.tx)
	if err != nil {
		return err
	}
	applied := 0
	for _, p := range st.Particulars {
		if d.Particulars[p.Key] != importParticularApply {
			continue
		}
		applied++
		switch p.Field {
		case "builder":
			cur.Builder = p.Value
		case "model":
			cur.Model = p.Value
		case "hin":
			cur.HIN = p.Value
		case "flag":
			cur.Flag = p.Value
		case "hailing_port":
			cur.HailingPort = p.Value
		case "hull_type":
			cur.HullType = p.Value
		case "hull_material":
			cur.HullMaterial = p.Value
		case "shore_power":
			cur.ShorePower = p.Value
		case "system_voltage":
			cur.SystemVoltage = p.Value
		case "registration":
			cur.Registration = p.Value
		case "imo":
			cur.IMO = p.Value
		case "epirb_id":
			cur.EPIRBID = p.Value
		case "date_acquired":
			cur.DateAcquired = p.Value
		case "year":
			y, err := strconv.Atoi(p.Value)
			if err != nil {
				return importInvalidf("year %q is not a number", p.Value)
			}
			cur.Year = &y
		case "displacement_kg":
			kg, err := parseDisplacementKg(p.Value)
			if err != nil {
				return importInvalidf("%v", err)
			}
			cur.DisplacementKG = &kg
		default:
			return importInvalidf("particular %s maps to unknown field %q", p.Label, p.Field)
		}
	}
	if applied == 0 {
		return nil
	}
	cur = trimParticulars(cur)
	if verr := validateParticulars(cur); verr != nil {
		return importInvalidf("%s", verr.Error())
	}
	if err := upsertVesselParticularsTx(w.tx, cur, w.now); err != nil {
		return err
	}
	w.count("particulars", func(c *importCount) { c.Created = applied })
	return nil
}

// ── zones ────────────────────────────────────────────────────────────────

func (w *importWriter) writeZones() error {
	for _, l := range w.run.Staged.Locations {
		zd := w.run.Decisions.Zones[l.Key]
		lower := strings.ToLower(l.Name)
		switch zd.Action {
		case importActionCreate:
			taken, err := rowExists(w.tx, `SELECT 1 FROM inventory_zones WHERE lower(name) = lower(?)`, l.Name)
			if err != nil {
				return fmt.Errorf("commit import: check zone: %w", err)
			}
			if taken {
				return importConflictf("a zone named %q already exists; match the location to it instead", l.Name)
			}
			id := uuid.NewString()
			if _, err := w.tx.Exec(`INSERT INTO inventory_zones (id, name, sort_index, created_at, updated_at) VALUES (?, ?, 0, ?, ?)`,
				id, l.Name, w.now.Unix(), w.now.Unix()); err != nil {
				return fmt.Errorf("commit import: create zone %q: %w", l.Name, err)
			}
			w.zoneIDs[lower] = id
			w.count("zones", func(c *importCount) { c.Created++ })
			if err := w.record(importTargetZone, l.Key, id, l.Name, "created"); err != nil {
				return err
			}
		case importActionMatch:
			w.zoneIDs[lower] = zd.ZoneID
			w.count("zones", func(c *importCount) { c.Matched++ })
			if err := w.record(importTargetZone, l.Key, zd.ZoneID, l.Name, "matched"); err != nil {
				return err
			}
		default:
			w.zoneIDs[lower] = ""
			w.count("zones", func(c *importCount) { c.Skipped++ })
		}
	}
	return nil
}

// zoneFor resolves a location name to (zone id or nil, location detail): when
// the location was skipped the name is not thrown away, it moves in front of
// the detail text.
func (w *importWriter) zoneFor(location, detail string) (zoneID any, locationDetail string) {
	if location == "" {
		return nil, detail
	}
	if id := w.zoneIDs[strings.ToLower(location)]; id != "" {
		return id, detail
	}
	if detail == "" {
		return nil, location
	}
	return nil, location + " · " + detail
}

// ── equipment ────────────────────────────────────────────────────────────

// importedEquipment is one equipment row to insert.
type importedEquipment struct {
	Name, Manufacturer, Serial, PartNumber, System, Status string
	Quantity                                               int
	Required                                               *int
	ZoneID, BinID                                          any
	LocationDetail, InstallDate, Notes                     string
}

func (w *importWriter) insertEquipment(e importedEquipment) (string, error) {
	name := strings.TrimSpace(e.Name)
	if name == "" {
		return "", importInvalidf("an equipment record has no name")
	}
	if !validEquipmentSystems[e.System] {
		return "", fmt.Errorf("commit import: %q mapped to unknown system %q", name, e.System)
	}
	if e.InstallDate != "" && !installDatePattern.MatchString(e.InstallDate) {
		return "", importInvalidf("%q has install date %q, not YYYY-MM-DD", name, e.InstallDate)
	}
	var required any
	if e.Required != nil {
		required = *e.Required
	}
	id := uuid.NewString()
	if _, err := w.tx.Exec(`
		INSERT INTO equipment (id, name, category, system, manufacturer, model, serial, part_number, quantity, required_quantity, status,
			zone_id, bin_id, location_detail, install_date, hour_meter_path, profile_id, aliases_json, verified_aboard, notes, created_at, updated_at)
		VALUES (?, ?, 'general', ?, ?, '', ?, ?, ?, ?, ?, ?, ?, ?, ?, '', '', '[]', 0, ?, ?, ?)`,
		id, name, e.System, e.Manufacturer, e.Serial, e.PartNumber, e.Quantity, required, e.Status,
		e.ZoneID, e.BinID, e.LocationDetail, e.InstallDate, e.Notes, w.now.Unix(), w.now.Unix()); err != nil {
		return "", fmt.Errorf("commit import: create %q: %w", name, err)
	}
	return id, nil
}

func joinNotes(parts ...string) string {
	var out []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, strings.TrimSpace(p))
		}
	}
	return strings.Join(out, "\n")
}

func typeLine(t string) string {
	if strings.TrimSpace(t) == "" {
		return ""
	}
	return "Type: " + strings.TrimSpace(t)
}

func (w *importWriter) writeEquipment() error {
	for _, e := range w.run.Staged.Equipment {
		rd := w.run.Decisions.Records[e.Key]
		switch rd.Action {
		case importActionCreate:
			zoneID, detail := w.zoneFor(e.Location, e.Detail)
			sys := mapImportSystem(e.Type + " " + e.Category)
			if e.Kind == stagedKindEngine {
				sys = mapImportSystem(e.Type)
				if sys == "other" {
					sys = "propulsion"
				}
			}
			id, err := w.insertEquipment(importedEquipment{
				Name: e.Name, Manufacturer: e.Manufacturer, Serial: e.Serial, System: sys, Status: "deployed", Quantity: 1,
				ZoneID: zoneID, BinID: nil, LocationDetail: detail, InstallDate: e.Installed,
				Notes: joinNotes(typeLine(e.Type), e.Notes),
			})
			if err != nil {
				return err
			}
			w.equipmentIDs[e.Key] = id
			w.count("equipment", func(c *importCount) { c.Created++ })
			if err := w.record(importTargetEquipment, e.Key, id, e.Name, "created"); err != nil {
				return err
			}
		case importActionMatch:
			w.equipmentIDs[e.Key] = rd.TargetID
			w.count("equipment", func(c *importCount) { c.Matched++ })
			if err := w.record(importTargetEquipment, e.Key, rd.TargetID, e.Name, "matched"); err != nil {
				return err
			}
		default:
			w.count("equipment", func(c *importCount) { c.Skipped++ })
		}
	}
	return nil
}

func (w *importWriter) writeSpares() error {
	for _, sp := range w.run.Staged.Spares {
		rd := w.run.Decisions.Records[sp.Key]
		if rd.Action == importActionSkip || rd.Action == "" {
			if sp.Kind == stagedSpareBin {
				w.count("bins", func(c *importCount) { c.Skipped++ })
			} else {
				w.count("equipment", func(c *importCount) { c.Skipped++ })
			}
			continue
		}
		if sp.Kind == stagedSpareBin {
			if err := w.writeBin(sp, rd); err != nil {
				return err
			}
			continue
		}
		if rd.Action == importActionMatch {
			w.count("equipment", func(c *importCount) { c.Matched++ })
			if err := w.record(importTargetEquipment, sp.Key, rd.TargetID, sp.Name, "matched"); err != nil {
				return err
			}
			continue
		}
		zoneID, detail := w.zoneFor(sp.Location, sp.Detail)
		id, err := w.insertEquipment(importedEquipment{
			Name: sp.Name, PartNumber: sp.PartNumber, System: mapImportSystem(sp.Category), Status: "stored",
			Quantity: sp.OnHand, Required: sp.Required, ZoneID: zoneID, LocationDetail: detail,
			Notes: joinNotes(typeLine(sp.Category), sp.Notes),
		})
		if err != nil {
			return err
		}
		w.count("equipment", func(c *importCount) { c.Created++ })
		if err := w.record(importTargetEquipment, sp.Key, id, sp.Name, "created"); err != nil {
			return err
		}
	}
	return nil
}

// writeBin creates (or matches) a locker's bin and files one equipment item
// per line in it.
func (w *importWriter) writeBin(sp stagedSpare, rd importRecordDecision) error {
	var binID, binZone string
	if rd.Action == importActionMatch {
		binID = rd.TargetID
		var zone string
		if err := w.tx.QueryRow(`SELECT zone_id FROM inventory_bins WHERE id = ?`, binID).Scan(&zone); err != nil {
			return importConflictf("bin %q no longer exists", binID)
		}
		binZone = zone
		w.count("bins", func(c *importCount) { c.Matched++ })
		if err := w.record(importTargetBin, sp.Key, binID, sp.Name, "matched"); err != nil {
			return err
		}
	} else {
		zoneID, _ := w.zoneFor(sp.Location, "")
		z, ok := zoneID.(string)
		if !ok || z == "" {
			return importConflictf("locker %q has no location; a bin has to live in a zone, so create or match the location %q", sp.Name, sp.Location)
		}
		taken, err := rowExists(w.tx, `SELECT 1 FROM inventory_bins WHERE lower(code) = lower(?)`, sp.Name)
		if err != nil {
			return fmt.Errorf("commit import: check bin code: %w", err)
		}
		if taken {
			return importConflictf("a bin coded %q already exists; match the locker to it instead", sp.Name)
		}
		binID = uuid.NewString()
		binZone = z
		if _, err := w.tx.Exec(`INSERT INTO inventory_bins (id, zone_id, code, name, sort_index, created_at, updated_at) VALUES (?, ?, ?, '', 0, ?, ?)`,
			binID, z, sp.Name, w.now.Unix(), w.now.Unix()); err != nil {
			return fmt.Errorf("commit import: create bin %q: %w", sp.Name, err)
		}
		w.count("bins", func(c *importCount) { c.Created++ })
		if err := w.record(importTargetBin, sp.Key, binID, sp.Name, "created"); err != nil {
			return err
		}
	}

	for i, item := range sp.Items {
		// An item an earlier run already filed is not filed again.
		itemKey := binItemKey(sp.Key, i)
		done, err := rowExists(w.tx, `SELECT 1 FROM import_records WHERE source = ? AND external_key = ? AND target_kind = ?`,
			w.run.Source, itemKey, importTargetEquipment)
		if err != nil {
			return fmt.Errorf("commit import: check item %q: %w", item, err)
		}
		if done {
			w.count("equipment", func(c *importCount) { c.Skipped++ })
			continue
		}
		id, err := w.insertEquipment(importedEquipment{
			Name: item, System: mapImportSystem(sp.Category), Status: "stored", Quantity: 1,
			ZoneID: binZone, BinID: binID, Notes: typeLine(sp.Category),
		})
		if err != nil {
			return err
		}
		w.count("equipment", func(c *importCount) { c.Created++ })
		if err := w.record(importTargetEquipment, itemKey, id, item, "created"); err != nil {
			return err
		}
	}
	return nil
}

// ── log ──────────────────────────────────────────────────────────────────

func (w *importWriter) writeLog() error {
	for _, e := range w.run.Staged.LogEntries {
		if w.run.Decisions.Records[e.Key].Action != importActionCreate {
			w.count("log_entries", func(c *importCount) { c.Skipped++ })
			continue
		}
		if e.Date == "" {
			return importInvalidf("log entry %q has no readable date and cannot be imported; skip it", e.Title)
		}

		equipmentID, err := w.logEquipment(e)
		if err != nil {
			return err
		}

		desc := e.Title
		if e.Body != "" {
			desc += "\n\n" + e.Body
		}
		var hours any
		if e.Hours != nil {
			hours = *e.Hours
		}
		id := uuid.NewString()
		// Every YachtWave log row is a service or a repair done on the boat;
		// none is tagged repair or improvement in the export, so all are
		// maintenance, with the title and the body kept in the description.
		if _, err := w.tx.Exec(`
			INSERT INTO maintenance_log_entries (id, equipment_id, rule_id, performed_at, hours, kind, description, who, cost, currency, created_at, updated_at)
			VALUES (?, ?, NULL, ?, ?, 'maintenance', ?, '', NULL, '', ?, ?)`,
			id, equipmentID, e.Date, hours, desc, w.now.Unix(), w.now.Unix()); err != nil {
			return fmt.Errorf("commit import: create log entry %q: %w", e.Title, err)
		}
		w.count("log_entries", func(c *importCount) { c.Created++ })
		if err := w.record(importTargetLogEntry, e.Key, id, e.Title, "created"); err != nil {
			return err
		}
	}
	return nil
}

// logEquipment decides which equipment a log entry belongs to: the operator's
// explicit choice if there is one (which may be "none"), else the one the
// export's name resolved to, if that item came in with this run.
func (w *importWriter) logEquipment(e stagedLogEntry) (any, error) {
	if ref, chosen := w.run.Decisions.LogEquipment[e.Key]; chosen {
		switch {
		case ref.EquipmentID != "":
			return ref.EquipmentID, nil
		case ref.EquipmentKey != "":
			id, err := w.resolveEquipment(ref.EquipmentKey, e.Title)
			if err != nil {
				return nil, err
			}
			if id != "" {
				return id, nil
			}
			return nil, importConflictf("log entry %q was given equipment that is not being imported; choose another or pick none", e.Title)
		default:
			return nil, nil
		}
	}
	if e.EquipmentKey != "" {
		id, err := w.resolveEquipment(e.EquipmentKey, e.Title)
		if err != nil {
			return nil, err
		}
		if id != "" {
			return id, nil
		}
	}
	return nil, nil
}

// ── notes, files ─────────────────────────────────────────────────────────

func (w *importWriter) writeNotes(noteDocs map[string]string) func() error {
	return func() error {
		for _, n := range w.run.Staged.Notes {
			id, created := noteDocs[n.Key]
			if !created {
				w.count("notes", func(c *importCount) { c.Skipped++ })
				continue
			}
			w.count("notes", func(c *importCount) { c.Created++ })
			if err := w.record(importTargetNote, n.Key, id, n.Title, "created"); err != nil {
				return err
			}
		}
		return nil
	}
}

func (w *importWriter) writeFiles() error {
	for _, f := range w.run.Staged.Files {
		fd := w.run.Decisions.Files[f.Key]
		if fd.Skipped || fd.DocumentID == "" {
			w.count("files", func(c *importCount) { c.Skipped++ })
			continue
		}
		ok, err := rowExists(w.tx, `SELECT 1 FROM documents WHERE id = ?`, fd.DocumentID)
		if err != nil {
			return fmt.Errorf("commit import: check document: %w", err)
		}
		if !ok {
			return importConflictf("the stored document for %q no longer exists", f.Label)
		}

		if fd.EquipmentKey != "" {
			equipmentID, err := w.resolveEquipment(fd.EquipmentKey, f.Label)
			if err != nil {
				return err
			}
			if equipmentID != "" {
				if err := w.linkDocument(equipmentID, fd.DocumentID); err != nil {
					return err
				}
			}
		}
		w.count("files", func(c *importCount) { c.Created++ })
		if err := w.record("document", f.Key, fd.DocumentID, f.Label, "created"); err != nil {
			return err
		}
	}
	return nil
}

// linkDocument attaches a document to an equipment item the way the photo
// route does: one equipment_documents row, sort_index after the item's
// existing photos. An existing link is left alone.
func (w *importWriter) linkDocument(equipmentID, documentID string) error {
	already, err := rowExists(w.tx, `SELECT 1 FROM equipment_documents WHERE equipment_id = ? AND document_id = ?`, equipmentID, documentID)
	if err != nil {
		return fmt.Errorf("commit import: check link: %w", err)
	}
	if already {
		return nil
	}
	var maxSort sql.NullInt64
	if err := w.tx.QueryRow(`
		SELECT MAX(ed.sort_index) FROM equipment_documents ed
		JOIN documents d ON d.id = ed.document_id AND `+photoMIMEsClause+`
		WHERE ed.equipment_id = ?`, equipmentID).Scan(&maxSort); err != nil {
		return fmt.Errorf("commit import: link sort: %w", err)
	}
	next := 0
	if maxSort.Valid {
		next = int(maxSort.Int64) + 1
	}
	if _, err := w.tx.Exec(`INSERT INTO equipment_documents (equipment_id, document_id, source, sort_index, created_at) VALUES (?, ?, 'operator', ?, ?)`,
		equipmentID, documentID, next, w.now.Unix()); err != nil {
		return fmt.Errorf("commit import: link document: %w", err)
	}
	return nil
}

// ── system mapping ───────────────────────────────────────────────────────

// importSystemKeywords maps words in a source's type or category text to the
// registry's own system. First match wins, so the order matters: "Electronics
// (Satellite)" is navigation because "electronics" comes before anything that
// would read "satellite" differently. Anything unmatched is "other".
var importSystemKeywords = []struct {
	word   string
	system string
}{
	{"generator", "electrical"},
	{"inboard", "propulsion"}, {"outboard", "propulsion"}, {"engine", "propulsion"}, {"propeller", "propulsion"}, {"gearbox", "propulsion"},
	{"water", "water"}, {"plumbing", "water"}, {"watermaker", "water"},
	{"fuel", "fuel"},
	{"bilge", "bilge"},
	{"anchor", "anchoring"}, {"windlass", "anchoring"}, {"rigging", "anchoring"},
	{"beacon", "safety"}, {"epirb", "safety"}, {"safety", "safety"}, {"fire", "safety"}, {"life", "safety"},
	{"hvac", "hvac"}, {"air condition", "hvac"}, {"heating", "hvac"},
	{"electronic", "navigation"}, {"radio", "navigation"}, {"radar", "navigation"}, {"chartplotter", "navigation"},
	{"antenna", "navigation"}, {"satellite", "navigation"}, {"navigation", "navigation"},
	{"high voltage", "electrical"}, {"electrical", "electrical"}, {"battery", "electrical"}, {"charger", "electrical"},
	{"inverter", "electrical"}, {"solar", "electrical"},
	{"a/v", "appliances"}, {"audio", "appliances"}, {"television", "appliances"}, {"appliance", "appliances"},
	{"structure", "structure"}, {"hull", "structure"}, {"deck", "structure"},
}

// mapImportSystem picks the registry system for a source type/category text.
func mapImportSystem(text string) string {
	lower := strings.ToLower(text)
	for _, k := range importSystemKeywords {
		if strings.Contains(lower, k.word) {
			return k.system
		}
	}
	return "other"
}
