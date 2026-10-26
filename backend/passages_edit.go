package main

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Operator edits to passages and cruises (ADR 0176 decision 5). Every change
// to a passage marks it edited, which is what stops the detector recreating or
// altering it.

var (
	errPassageInvalid = errors.New("invalid passage request")
	errCruiseNotFound = errors.New("cruise not found")
)

const (
	passageNameMaxLen  = 200
	passageNotesMaxLen = 20000
)

func invalidf(format string, a ...any) error {
	return fmt.Errorf("%w: %s", errPassageInvalid, fmt.Sprintf(format, a...))
}

// passagePatch carries the fields a PATCH may change; nil means untouched.
// CruiseID is a pointer to a pointer in spirit: CruiseIDSet says the field was
// present, and a nil CruiseID with it set means unfile.
type passagePatch struct {
	Title       *string
	Notes       *string
	FromName    *string
	ToName      *string
	Hidden      *bool
	CruiseID    *string
	CruiseIDSet bool
}

func checkLen(field string, v *string, max int) error {
	if v != nil && len(*v) > max {
		return invalidf("%s is longer than %d characters", field, max)
	}
	return nil
}

func (s *passageStore) PatchPassage(id string, p passagePatch) (passageRecord, error) {
	for _, c := range []struct {
		n string
		v *string
		m int
	}{{"title", p.Title, passageNameMaxLen}, {"from_name", p.FromName, passageNameMaxLen}, {"to_name", p.ToName, passageNameMaxLen}, {"notes", p.Notes, passageNotesMaxLen}} {
		if err := checkLen(c.n, c.v, c.m); err != nil {
			return passageRecord{}, err
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return passageRecord{}, err
	}
	defer tx.Rollback()
	rec, err := scanPassage(tx.QueryRow(`SELECT `+passageColumns+` FROM passages WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return rec, errPassageNotFound
	}
	if err != nil {
		return rec, err
	}
	if p.Title != nil {
		rec.Title = *p.Title
	}
	if p.Notes != nil {
		rec.Notes = *p.Notes
	}
	if p.FromName != nil {
		rec.FromName, rec.FromNameEdited = *p.FromName, true
	}
	if p.ToName != nil {
		rec.ToName, rec.ToNameEdited = *p.ToName, true
	}
	if p.Hidden != nil {
		rec.Hidden = *p.Hidden
	}
	if p.CruiseIDSet {
		if p.CruiseID != nil {
			var n int
			if err := tx.QueryRow(`SELECT COUNT(*) FROM cruises WHERE id = ?`, *p.CruiseID).Scan(&n); err != nil {
				return rec, err
			}
			if n == 0 {
				return rec, errCruiseNotFound
			}
		}
		rec.CruiseID = p.CruiseID
	}
	rec.Edited = true
	if p.CruiseIDSet && marinaLookupWake != nil {
		defer marinaLookupWake()
	}
	if _, err := tx.Exec(`UPDATE passages SET title=?, notes=?, from_name=?, to_name=?, hidden=?, cruise_id=?, edited=1, from_name_edited=?, to_name_edited=? WHERE id=?`,
		rec.Title, rec.Notes, rec.FromName, rec.ToName, boolInt(rec.Hidden), rec.CruiseID, boolInt(rec.FromNameEdited), boolInt(rec.ToNameEdited), id); err != nil {
		return rec, fmt.Errorf("update passage: %w", err)
	}
	return rec, tx.Commit()
}

// splitSnap is how far from a titled join a split still lands on the join.
const splitSnap = 60 * time.Second

// SplitPassage cuts a passage in two at a moment strictly inside it. The first
// half keeps the id, the origin and the title; the second keeps the
// destination. Names the cut cannot know are left empty.
func (s *passageStore) SplitPassage(id string, at time.Time) (first, second passageRecord, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()
	rec, err := scanPassage(tx.QueryRow(`SELECT `+passageColumns+` FROM passages WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		err = errPassageNotFound
		return
	}
	if err != nil {
		return
	}
	if !at.After(rec.Start) || !at.Before(rec.End) {
		err = invalidf("split time must be inside the passage")
		return
	}
	// A titled join within a minute of the cut is where the operator meant to
	// cut: take the nearest one's exact time and name.
	var join string
	{
		lo, hi := at.Add(-splitSnap).UnixMilli(), at.Add(splitSnap).UnixMilli()
		var jt int64
		jerr := tx.QueryRow(`SELECT t_ms, title FROM replay_signs WHERE starts_leg = 1 AND title != '' AND t_ms >= ? AND t_ms <= ? AND t_ms > ? AND t_ms < ?
			ORDER BY ABS(t_ms - ?), id LIMIT 1`, lo, hi, rec.Start.UnixMilli(), rec.End.UnixMilli(), at.UnixMilli()).Scan(&jt, &join)
		if jerr != nil && !errors.Is(jerr, sql.ErrNoRows) {
			err = jerr
			return
		}
		if jerr == nil {
			at = time.UnixMilli(jt).UTC()
		}
	}
	first, second = rec, rec
	first.End, first.ToName, first.ToNameEdited, first.Edited = at, "", false, true
	second.ID, second.Start, second.FromName, second.FromNameEdited, second.Title, second.Edited = uuid.NewString(), at, "", false, "", true
	second.CreatedAt = time.Now().UTC()
	// A titled join at the cut is the place both halves meet: they take its
	// name and need no lookup.
	if join != "" {
		first.ToName, second.FromName = join, join
	}
	// The first half's destination is now the cut, which the old name did not
	// describe: clear it and queue the half for naming again, as a trim does.
	// The second half is new, so it is queued already; the wake after commit
	// gets both named promptly.
	if _, err = tx.Exec(`UPDATE passages SET end_ms=?, to_name=?, to_name_edited=0, edited=1, stats_json=NULL, name_attempted_ms=NULL WHERE id=?`, at.UnixMilli(), first.ToName, id); err != nil {
		return
	}
	if _, err = tx.Exec(`INSERT INTO passages (`+passageColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		second.ID, second.Start.UnixMilli(), second.End.UnixMilli(), second.FromName, second.ToName, "", "",
		boolInt(second.Hidden), 1, second.CruiseID, second.CreatedAt.UnixMilli(), 0, boolInt(second.ToNameEdited), second.Source); err != nil {
		return
	}
	second.Notes = ""
	if err = tx.Commit(); err != nil {
		return
	}
	if join == "" && passageNamesWake != nil {
		passageNamesWake()
	}
	return
}

// MergePassages joins two or more neighbouring passages into the earliest one
// and writes a join sign at each boundary, so the legs stay visible. They must
// be adjacent (no other passage, hidden or not, between them), visible, and in
// the same cruise or in none. The join sits at the earlier leg's end, named
// for the place that leg arrived at, else the later leg's origin, else left
// untitled for the naming lookup.
func (s *passageStore) MergePassages(ids ...string) (passageRecord, error) {
	if len(ids) < 2 {
		return passageRecord{}, invalidf("merge needs at least two passages")
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			return passageRecord{}, invalidf("merge needs different passages")
		}
		seen[id] = true
	}
	tx, err := s.db.Begin()
	if err != nil {
		return passageRecord{}, err
	}
	defer tx.Rollback()
	legs := make([]passageRecord, len(ids))
	for i, id := range ids {
		r, err := scanPassage(tx.QueryRow(`SELECT `+passageColumns+` FROM passages WHERE id = ?`, id))
		if errors.Is(err, sql.ErrNoRows) {
			return r, errPassageNotFound
		}
		if err != nil {
			return r, err
		}
		if r.Hidden {
			return r, invalidf("a hidden passage cannot be merged")
		}
		legs[i] = r
	}
	sort.Slice(legs, func(i, j int) bool { return legs[i].Start.Before(legs[j].Start) })
	m := legs[0]
	untitled := false
	now := time.Now().UTC().UnixMilli()
	for i := 1; i < len(legs); i++ {
		a, b := legs[i-1], legs[i]
		// Points of two sources cannot be read as one track, so a mixed merge
		// has no known source and is left out of decision matching.
		if m.Source != b.Source {
			m.Source = ""
		}
		if m.CruiseID != nil && b.CruiseID != nil && *m.CruiseID != *b.CruiseID {
			return m, invalidf("the passages are in different cruises")
		}
		var between int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM passages WHERE id NOT IN (?, ?) AND start_ms < ? AND end_ms > ?`,
			a.ID, b.ID, b.Start.UnixMilli(), a.End.UnixMilli()).Scan(&between); err != nil {
			return m, err
		}
		if between > 0 {
			return m, invalidf("the passages are not next to each other")
		}
		title := a.ToName
		if title == "" {
			title = b.FromName
		}
		title = joinTitle(title)
		// A split at a join leaves its sign behind; a second merge reuses it.
		var have int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM replay_signs WHERE starts_leg = 1 AND t_ms = ?`, a.End.UnixMilli()).Scan(&have); err != nil {
			return m, err
		}
		if have == 0 {
			untitled = untitled || title == ""
			if _, err := tx.Exec(`INSERT INTO replay_signs (`+signColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?)`,
				uuid.NewString(), a.End.UnixMilli(), title, "", nil, signAfter, signShowDef, 1, now, now); err != nil {
				return m, fmt.Errorf("insert join sign: %w", err)
			}
		}
		m.End, m.ToName, m.ToNameEdited, m.Edited = b.End, b.ToName, b.ToNameEdited, true
		if m.Title == "" {
			m.Title = b.Title
		}
		switch {
		case m.Notes == "":
			m.Notes = b.Notes
		case b.Notes != "":
			m.Notes = m.Notes + "\n\n" + b.Notes
		}
		if m.CruiseID == nil {
			m.CruiseID = b.CruiseID
		}
		if _, err := tx.Exec(`DELETE FROM passages WHERE id = ?`, b.ID); err != nil {
			return m, err
		}
	}
	if _, err := tx.Exec(`UPDATE passages SET end_ms=?, to_name=?, to_name_edited=?, title=?, notes=?, cruise_id=?, source=?, edited=1, stats_json=NULL WHERE id=?`,
		m.End.UnixMilli(), m.ToName, boolInt(m.ToNameEdited), m.Title, m.Notes, m.CruiseID, m.Source, m.ID); err != nil {
		return m, err
	}
	if err := tx.Commit(); err != nil {
		return m, err
	}
	if untitled && passageNamesWake != nil {
		passageNamesWake()
	}
	return m, nil
}

// TrimPassage moves a passage's ends inward. The track points are untouched.
func (s *passageStore) TrimPassage(id string, start, end time.Time) (passageRecord, error) {
	// One transaction, so the read the checks use is the row the write
	// replaces, and SQLite serialises it against every other passage writer.
	tx, err := s.db.Begin()
	if err != nil {
		return passageRecord{}, err
	}
	defer tx.Rollback()
	rec, err := scanPassage(tx.QueryRow(`SELECT `+passageColumns+` FROM passages WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return rec, errPassageNotFound
	}
	if err != nil {
		return rec, err
	}
	if !end.After(start) || start.Before(rec.Start) || end.After(rec.End) {
		return rec, invalidf("trim must stay inside the passage and leave something")
	}
	// A name filled in by the plugin described the old end. Where an end moved
	// and the operator did not type its name, clear it and queue it for naming
	// again; an operator's name stays whatever happens to the end. An end that
	// moved with no name yet is queued again too: a naming lookup already out
	// for the old end must not land on the new one.
	moveFrom := !start.Equal(rec.Start) && !rec.FromNameEdited
	moveTo := !end.Equal(rec.End) && !rec.ToNameEdited
	if moveFrom {
		rec.FromName = ""
	}
	if moveTo {
		rec.ToName = ""
	}
	if _, err := tx.Exec(`UPDATE passages SET start_ms=?, end_ms=?, from_name=?, to_name=?, edited=1, stats_json=NULL,
		name_attempted_ms = CASE WHEN ? THEN NULL ELSE name_attempted_ms END WHERE id=?`,
		start.UnixMilli(), end.UnixMilli(), rec.FromName, rec.ToName, moveFrom || moveTo, id); err != nil {
		return rec, fmt.Errorf("trim passage: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return rec, fmt.Errorf("trim passage: %w", err)
	}
	rec.Start, rec.End, rec.Edited = start, end, true
	if (moveFrom || moveTo) && passageNamesWake != nil {
		passageNamesWake()
	}
	return rec, nil
}

// ── cruises ──────────────────────────────────────────────────────────────

type cruisePatch struct {
	Name  *string
	Notes *string
}

func (s *passageStore) ListCruises() ([]cruiseRecord, error) {
	rows, err := s.db.Query(`SELECT id, name, notes, created_ms FROM cruises ORDER BY created_ms DESC, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []cruiseRecord
	for rows.Next() {
		c, err := scanCruise(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func scanCruise(r rowScanner) (cruiseRecord, error) {
	var c cruiseRecord
	var ms int64
	if err := r.Scan(&c.ID, &c.Name, &c.Notes, &ms); err != nil {
		return c, err
	}
	c.CreatedAt = time.UnixMilli(ms).UTC()
	return c, nil
}

func (s *passageStore) GetCruise(id string) (cruiseRecord, error) {
	c, err := scanCruise(s.db.QueryRow(`SELECT id, name, notes, created_ms FROM cruises WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return c, errCruiseNotFound
	}
	return c, err
}

func (s *passageStore) CreateCruise(name, notes string) (cruiseRecord, error) {
	if err := validateCruise(name, notes); err != nil {
		return cruiseRecord{}, err
	}
	return s.InsertCruise(cruiseRecord{Name: strings.TrimSpace(name), Notes: notes})
}

func validateCruise(name, notes string) error {
	if strings.TrimSpace(name) == "" {
		return invalidf("a cruise needs a name")
	}
	if len(name) > passageNameMaxLen || len(notes) > passageNotesMaxLen {
		return invalidf("name or notes too long")
	}
	return nil
}

func (s *passageStore) PatchCruise(id string, p cruisePatch) (cruiseRecord, error) {
	c, err := s.GetCruise(id)
	if err != nil {
		return c, err
	}
	if p.Name != nil {
		c.Name = *p.Name
	}
	if p.Notes != nil {
		c.Notes = *p.Notes
	}
	if err := validateCruise(c.Name, c.Notes); err != nil {
		return c, err
	}
	c.Name = strings.TrimSpace(c.Name)
	if _, err := s.db.Exec(`UPDATE cruises SET name=?, notes=? WHERE id=?`, c.Name, c.Notes, id); err != nil {
		return c, err
	}
	return c, nil
}

// DeleteCruise removes the cruise and unfiles its passages. The passages, and
// the water-rise labels that belonged to the cruise's span, are kept or go with
// the cruise respectively; no passage is ever deleted.
func (s *passageStore) DeleteCruise(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE passages SET cruise_id = NULL, edited = 1 WHERE cruise_id = ?`, id); err != nil {
		return err
	}
	res, err := tx.Exec(`DELETE FROM cruises WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errCruiseNotFound
	}
	return tx.Commit()
}

// CruisePassages returns a cruise's visible passages, oldest first.
func (s *passageStore) CruisePassages(cruiseID string) ([]passageRecord, error) {
	rows, err := s.db.Query(`SELECT `+passageColumns+` FROM passages WHERE cruise_id = ? AND hidden = 0 ORDER BY start_ms`, cruiseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []passageRecord
	for rows.Next() {
		p, err := scanPassage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
