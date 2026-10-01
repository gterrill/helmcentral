package main

import (
	"fmt"
	"strconv"
	"strings"
)

// This file is the rulebook for import decisions: what a valid set looks like
// (validateImportDecisions), what the wizard starts with
// (DefaultImportDecisions), and whether the records a "match" points at are
// still there (checkImportMatchTargets).

// The kinds a decidable staged record key can have, so validation can say
// which actions each allows.
const (
	recordKindEquipment = "equipment"
	recordKindSpare     = "spare"
	recordKindBin       = "bin"
	recordKindLog       = "log"
	recordKindNote      = "note"
)

func importRecordKinds(st stagedImport) map[string]string {
	kinds := map[string]string{}
	for _, e := range st.Equipment {
		kinds[e.Key] = recordKindEquipment
	}
	for _, s := range st.Spares {
		if s.Kind == stagedSpareBin {
			kinds[s.Key] = recordKindBin
		} else {
			kinds[s.Key] = recordKindSpare
		}
	}
	for _, e := range st.LogEntries {
		kinds[e.Key] = recordKindLog
	}
	for _, n := range st.Notes {
		kinds[n.Key] = recordKindNote
	}
	return kinds
}

// particularUnreadable says why a particular's value cannot be written to the
// vessel record ("" when it can): the commit would refuse it, so it is never
// applied by default and cannot be chosen.
func particularUnreadable(p stagedParticular) string {
	switch p.Field {
	case "year":
		year, err := strconv.Atoi(p.Value)
		if err != nil {
			return fmt.Sprintf("year %q is not a number", p.Value)
		}
		if year < vesselYearMin || year > vesselYearMax {
			return fmt.Sprintf("year %q is not between %d and %d", p.Value, vesselYearMin, vesselYearMax)
		}
	case "displacement_kg":
		if _, err := parseDisplacementKg(p.Value); err != nil {
			return err.Error()
		}
	}
	return ""
}

func validAction(a string) bool {
	return a == importActionCreate || a == importActionMatch || a == importActionSkip
}

// validateImportDecisions checks a decision set against the payload it is
// for. It looks at shape only (keys exist, actions make sense); whether a
// match target still exists in the registry is checkImportMatchTargets.
func validateImportDecisions(st stagedImport, d importDecisions) error {
	particulars := map[string]stagedParticular{}
	for _, p := range st.Particulars {
		particulars[p.Key] = p
	}
	for key, action := range d.Particulars {
		p, ok := particulars[key]
		if !ok {
			return importInvalidf("no particular %q in this export", key)
		}
		switch action {
		case importParticularSkip:
		case importParticularApply:
			if p.SignalK {
				return importInvalidf("%s is published by the boat's own data feed and is never written", p.Label)
			}
			if p.Field == "" {
				return importInvalidf("%s has no place in the vessel record to be written to", p.Label)
			}
			if p.Value == "" {
				return importInvalidf("%s has no value to apply", p.Label)
			}
			if reason := particularUnreadable(p); reason != "" {
				return importInvalidf("%s cannot be applied: %s; skip it", p.Label, reason)
			}
		default:
			return importInvalidf("particular %s: unknown action %q (want apply or skip)", p.Label, action)
		}
	}

	zones := map[string]bool{}
	for _, l := range st.Locations {
		zones[l.Key] = true
	}
	for key, z := range d.Zones {
		if !zones[key] {
			return importInvalidf("no location %q in this export", key)
		}
		if !validAction(z.Action) {
			return importInvalidf("location %q: unknown action %q", key, z.Action)
		}
		if z.Action == importActionMatch && strings.TrimSpace(z.ZoneID) == "" {
			return importInvalidf("location %q: match needs a zone_id", key)
		}
	}

	kinds := importRecordKinds(st)
	skipNotes := map[string]bool{}
	for _, n := range st.Notes {
		if n.Skip {
			skipNotes[n.Key] = true
		}
	}
	for key, r := range d.Records {
		kind, ok := kinds[key]
		if !ok {
			return importInvalidf("no record %q in this export", key)
		}
		if !validAction(r.Action) {
			return importInvalidf("record %q: unknown action %q", key, r.Action)
		}
		if r.Action == importActionMatch {
			if kind == recordKindLog || kind == recordKindNote {
				return importInvalidf("record %q: a %s cannot be matched to an existing record", key, kind)
			}
			if strings.TrimSpace(r.TargetID) == "" {
				return importInvalidf("record %q: match needs a target_id", key)
			}
		}
		if r.Action == importActionCreate && skipNotes[key] {
			return importInvalidf("record %q holds a credential and is never imported", key)
		}
	}

	logs := map[string]bool{}
	for _, e := range st.LogEntries {
		logs[e.Key] = true
	}
	equipment := map[string]bool{}
	for _, e := range st.Equipment {
		equipment[e.Key] = true
	}
	for key, ref := range d.LogEquipment {
		if !logs[key] {
			return importInvalidf("no log entry %q in this export", key)
		}
		if ref.EquipmentKey != "" && ref.EquipmentID != "" {
			return importInvalidf("log entry %q: give equipment_key or equipment_id, not both", key)
		}
		if ref.EquipmentKey != "" && !equipment[ref.EquipmentKey] {
			return importInvalidf("log entry %q: no equipment %q in this export", key, ref.EquipmentKey)
		}
	}

	files := map[string]bool{}
	for _, f := range st.Files {
		files[f.Key] = true
	}
	for key, f := range d.Files {
		if !files[key] {
			return importInvalidf("no file %q in this export", key)
		}
		if f.EquipmentKey != "" && !equipment[f.EquipmentKey] {
			return importInvalidf("file %q: no equipment %q in this export", key, f.EquipmentKey)
		}
	}
	return nil
}

// checkImportMatchTargets confirms every zone, equipment item and bin a
// decision matches to exists. errf builds the error: a 400 when a client has
// just sent it, a 409 when a commit finds it gone.
func checkImportMatchTargets(q sqlQueryer, st stagedImport, d importDecisions, errf func(string, ...any) error) error {
	for key, z := range d.Zones {
		if z.Action != importActionMatch {
			continue
		}
		ok, err := rowExists(q, `SELECT 1 FROM inventory_zones WHERE id = ?`, z.ZoneID)
		if err != nil {
			return fmt.Errorf("check match targets: %w", err)
		}
		if !ok {
			return errf("location %q is matched to zone %q, which does not exist", key, z.ZoneID)
		}
	}
	kinds := importRecordKinds(st)
	for key, r := range d.Records {
		if r.Action != importActionMatch {
			continue
		}
		table := "equipment"
		if kinds[key] == recordKindBin {
			table = "inventory_bins"
		}
		ok, err := rowExists(q, `SELECT 1 FROM `+table+` WHERE id = ?`, r.TargetID)
		if err != nil {
			return fmt.Errorf("check match targets: %w", err)
		}
		if !ok {
			return errf("record %q is matched to %q, which does not exist", key, r.TargetID)
		}
	}
	for key, ref := range d.LogEquipment {
		if ref.EquipmentID == "" {
			continue
		}
		ok, err := rowExists(q, `SELECT 1 FROM equipment WHERE id = ?`, ref.EquipmentID)
		if err != nil {
			return fmt.Errorf("check match targets: %w", err)
		}
		if !ok {
			return errf("log entry %q names equipment %q, which does not exist", key, ref.EquipmentID)
		}
	}
	return nil
}

// DefaultImportDecisions computes where the wizard starts. Nothing here is
// final; the operator changes any of it.
//
//   - A location that an earlier import already wrote, or that an existing
//     zone has the same name as, is matched to that zone; any other is created.
//   - An engine, item or locker that an earlier import wrote is skipped. One
//     that is flagged as a duplicate or a junk name is skipped. One with a
//     single existing equipment record of the same name (and serial, when the
//     export has one) or an existing bin of the same code is matched to it.
//     Everything else is created.
//   - A log entry is created unless it is a repeat or already imported. An
//     ambiguous one gets no pre-made equipment choice; the operator picks.
//   - A note is created unless it holds a credential or is already imported.
//   - A particular that can be written (has a value, a home, and is not the
//     data feed's) is applied.
//   - A file starts undecided, or skipped when an earlier import already
//     stored it.
func (s *documentStore) DefaultImportDecisions(st stagedImport) (importDecisions, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	d := normalizeDecisions(importDecisions{})

	recorded, err := importRecordedKeys(s.db, st.Source)
	if err != nil {
		return d, err
	}

	zoneRows, err := s.db.Query(`SELECT id, lower(name) FROM inventory_zones`)
	if err != nil {
		return d, fmt.Errorf("default import decisions: zones: %w", err)
	}
	zoneByName := map[string]string{}
	zoneExists := map[string]bool{}
	for zoneRows.Next() {
		var id, name string
		if err := zoneRows.Scan(&id, &name); err != nil {
			zoneRows.Close()
			return d, fmt.Errorf("default import decisions: zones: %w", err)
		}
		zoneByName[name] = id
		zoneExists[id] = true
	}
	zoneRows.Close()

	type existingEquipment struct{ id, serial string }
	equipRows, err := s.db.Query(`SELECT id, lower(name), serial FROM equipment`)
	if err != nil {
		return d, fmt.Errorf("default import decisions: equipment: %w", err)
	}
	equipByName := map[string][]existingEquipment{}
	for equipRows.Next() {
		var id, name, serial string
		if err := equipRows.Scan(&id, &name, &serial); err != nil {
			equipRows.Close()
			return d, fmt.Errorf("default import decisions: equipment: %w", err)
		}
		equipByName[name] = append(equipByName[name], existingEquipment{id, serial})
	}
	equipRows.Close()

	binRows, err := s.db.Query(`SELECT id, lower(code) FROM inventory_bins`)
	if err != nil {
		return d, fmt.Errorf("default import decisions: bins: %w", err)
	}
	binByCode := map[string]string{}
	for binRows.Next() {
		var id, code string
		if err := binRows.Scan(&id, &code); err != nil {
			binRows.Close()
			return d, fmt.Errorf("default import decisions: bins: %w", err)
		}
		binByCode[code] = id
	}
	binRows.Close()

	skipByIssue := map[string]bool{}
	for _, is := range st.Issues {
		if is.Key != "" && (is.Code == issueDuplicate || is.Code == issueJunkName) {
			skipByIssue[is.Key] = true
		}
	}
	alreadyIn := func(key string) bool { return len(recorded[key]) > 0 }

	for _, p := range st.Particulars {
		if p.Field != "" && !p.SignalK && p.Value != "" && particularUnreadable(p) == "" {
			d.Particulars[p.Key] = importParticularApply
		}
	}

	for _, l := range st.Locations {
		if rec, ok := recorded[l.Key][importTargetZone]; ok && zoneExists[rec.TargetID] {
			d.Zones[l.Key] = importZoneDecision{Action: importActionMatch, ZoneID: rec.TargetID}
		} else if id, ok := zoneByName[strings.ToLower(l.Name)]; ok {
			d.Zones[l.Key] = importZoneDecision{Action: importActionMatch, ZoneID: id}
		} else {
			d.Zones[l.Key] = importZoneDecision{Action: importActionCreate}
		}
	}

	matchEquipment := func(name, serial string) (string, bool) {
		var hits []string
		for _, c := range equipByName[strings.ToLower(strings.TrimSpace(name))] {
			if serial == "" || c.serial == serial {
				hits = append(hits, c.id)
			}
		}
		if len(hits) == 1 {
			return hits[0], true
		}
		return "", false
	}

	for _, e := range st.Equipment {
		switch {
		case alreadyIn(e.Key), skipByIssue[e.Key]:
			d.Records[e.Key] = importRecordDecision{Action: importActionSkip}
		default:
			if id, ok := matchEquipment(e.Name, e.Serial); ok {
				d.Records[e.Key] = importRecordDecision{Action: importActionMatch, TargetID: id}
			} else {
				d.Records[e.Key] = importRecordDecision{Action: importActionCreate}
			}
		}
	}
	for _, sp := range st.Spares {
		switch {
		case alreadyIn(sp.Key), skipByIssue[sp.Key]:
			d.Records[sp.Key] = importRecordDecision{Action: importActionSkip}
		case sp.Kind == stagedSpareBin:
			if id, ok := binByCode[strings.ToLower(strings.TrimSpace(sp.Name))]; ok {
				d.Records[sp.Key] = importRecordDecision{Action: importActionMatch, TargetID: id}
			} else {
				d.Records[sp.Key] = importRecordDecision{Action: importActionCreate}
			}
		default:
			if id, ok := matchEquipment(sp.Name, ""); ok {
				d.Records[sp.Key] = importRecordDecision{Action: importActionMatch, TargetID: id}
			} else {
				d.Records[sp.Key] = importRecordDecision{Action: importActionCreate}
			}
		}
	}
	for _, e := range st.LogEntries {
		if alreadyIn(e.Key) || skipByIssue[e.Key] {
			d.Records[e.Key] = importRecordDecision{Action: importActionSkip}
		} else {
			d.Records[e.Key] = importRecordDecision{Action: importActionCreate}
		}
	}
	for _, n := range st.Notes {
		if n.Skip || alreadyIn(n.Key) {
			d.Records[n.Key] = importRecordDecision{Action: importActionSkip}
		} else {
			d.Records[n.Key] = importRecordDecision{Action: importActionCreate}
		}
	}
	for _, f := range st.Files {
		d.Files[f.Key] = importFileDecision{Skipped: alreadyIn(f.Key), EquipmentKey: f.EquipmentKey}
	}
	return d, nil
}
