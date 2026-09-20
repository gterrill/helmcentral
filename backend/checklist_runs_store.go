package main

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// This file is the checklist-run half of documentStore (documents_store.go,
// notes_store.go): plan "Notes and the Boat's Manual" §3, ADR 0118. A
// checklist run stores WHICH items are ticked, keyed to a hash of the
// item's own normalised plain text plus an occurrence index
// (parseChecklistItems, notes_format.go) - never to position. Reordering
// the note's body, or editing a line that isn't the ticked one, must never
// invalidate a tick; editing the TICKED line's meaningful content is the
// one thing that legitimately should, and does, by simply no longer
// producing a matching key - there is no reconciliation step, because
// AGENTS.md forbids exactly the fuzzy re-matching that would be.
//
// The read path (buildChecklistRunView) is the same three-way join on
// every call: parse the CURRENT body into ordered items, left-join the
// run's own tick rows by (item_key, occurrence). An item with a matching
// tick is checked; an item with none is unchecked; a tick with no matching
// item is "changed" - surfaced with its stored text, never silently
// carried forward onto a different item and never silently dropped
// (AGENTS.md's fallback policy, applied to the one case here where quiet
// behaviour would be actively dangerous: a skipper believing a step is
// done when the step itself changed).

var (
	// errChecklistRunNotFound is returned by every runID-scoped method
	// (TickChecklistItem, CompleteChecklistRun, AbandonChecklistRun) when
	// runID names no row at all.
	errChecklistRunNotFound = errors.New("checklist run not found")

	// errNoteHasNoChecklist is StartOrResumeChecklistRun's own sentinel for
	// a note that parses to zero checklist items - starting a run over an
	// empty item list would just be a run nothing can ever tick.
	errNoteHasNoChecklist = errors.New("note has no checklist items")

	// errChecklistRunClosed is returned by TickChecklistItem,
	// CompleteChecklistRun and AbandonChecklistRun against a run that
	// already carries a completed_at or abandoned_at - "closed" covers
	// both, uniformly, for all three: a completed run cannot be ticked,
	// completed again or abandoned; an abandoned run cannot be either.
	errChecklistRunClosed = errors.New("checklist run is already closed")

	// errChecklistItemNotFound is TickChecklistItem's sentinel for an
	// (item_key, occurrence) pair that is not among the note's CURRENT
	// items - either the pair was never real, or the item it named has
	// itself become a "changed" entry since the client last read the run.
	// Either way, ticking it would tick something that no longer exists;
	// AGENTS.md's fallback policy says surface that rather than silently
	// accept it.
	errChecklistItemNotFound = errors.New("checklist item not found in the current run")
)

// checklistItemView is one item of checklistRunView.Items: parseChecklistItems'
// own checklistItem plus this run's tick state for it. CheckedAt is nil for
// an unticked item.
type checklistItemView struct {
	ItemKey    string     `json:"item_key"`
	Occurrence int        `json:"occurrence"`
	Text       string     `json:"text"`
	Depth      int        `json:"depth"`
	Checked    bool       `json:"checked"`
	CheckedAt  *time.Time `json:"checked_at,omitempty"`
}

// checklistChangedView is one tick row whose (item_key, occurrence) matches
// no CURRENT item - plan §3's "edited since you ticked it - re-check".
// Text is the STORED text (what the operator actually ticked), not
// anything derived from today's body, which is the whole point: the
// operator needs to see what they checked off, to judge whether whatever
// replaced it still means the same thing.
type checklistChangedView struct {
	ItemKey    string    `json:"item_key"`
	Occurrence int       `json:"occurrence"`
	Text       string    `json:"text"`
	CheckedAt  time.Time `json:"checked_at"`
}

// checklistRunView is StartOrResumeChecklistRun/ActiveChecklistRun/
// TickChecklistItem/CompleteChecklistRun's shared return shape - "the
// whole run" plan §3 says PATCH .../items returns, so the client never
// merges tick state locally. Items is in the CURRENT body's own order
// (never re-sorted by checked state - plan §7's runner UI keeps completed
// rows in place), Changed in the ticks' own checked_at order.
type checklistRunView struct {
	ID           string                 `json:"id"`
	DocumentID   string                 `json:"document_id"`
	StartedAt    time.Time              `json:"started_at"`
	CompletedAt  *time.Time             `json:"completed_at,omitempty"`
	AbandonedAt  *time.Time             `json:"abandoned_at,omitempty"`
	Items        []checklistItemView    `json:"items"`
	Changed      []checklistChangedView `json:"changed"`
	Total        int                    `json:"total"`
	CheckedCount int                    `json:"checked_count"`
}

// checklistRunRow is the bare note_checklist_runs row, the shape every
// method below reads before doing anything else with it.
type checklistRunRow struct {
	ID           string
	DocumentID   string
	SourceSHA256 string
	StartedAt    time.Time
	CompletedAt  *time.Time
	AbandonedAt  *time.Time
}

func (r checklistRunRow) closed() bool {
	return r.CompletedAt != nil || r.AbandonedAt != nil
}

// noteAndBody reads doc (confirming kind='note') and its current body off
// disk, the same pair notes_handlers.go's getNoteOrError+readNoteBody
// gives a handler - duplicated here rather than called, because every
// caller in this file already holds s.mu (a plain, non-reentrant
// sync.Mutex - see documents_store.go), and Get/readNoteBody's own callers
// are never inside that lock themselves. errDocumentNotFound if id doesn't
// exist, errNotANote if it exists but isn't kind='note'.
func (s *documentStore) noteAndBody(id string) (document, string, error) {
	doc, err := scanDocument(s.db.QueryRow(`SELECT `+documentColumns+` FROM documents WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return document{}, "", errDocumentNotFound
	}
	if err != nil {
		return document{}, "", fmt.Errorf("checklist run: read document: %w", err)
	}
	if doc.Kind != "note" {
		return document{}, "", errNotANote
	}
	body, err := readNoteBody(doc)
	if err != nil {
		return document{}, "", fmt.Errorf("checklist run: read note body: %w", err)
	}
	return doc, body, nil
}

// readChecklistRunRow reads runID's bare row, s.mu already held.
func readChecklistRunRow(q sqlQueryer, runID string) (checklistRunRow, error) {
	var row checklistRunRow
	var startedAt int64
	var completedAt, abandonedAt sql.NullInt64
	err := q.QueryRow(
		`SELECT id, document_id, source_sha256, started_at, completed_at, abandoned_at FROM note_checklist_runs WHERE id = ?`,
		runID,
	).Scan(&row.ID, &row.DocumentID, &row.SourceSHA256, &startedAt, &completedAt, &abandonedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return checklistRunRow{}, errChecklistRunNotFound
	}
	if err != nil {
		return checklistRunRow{}, fmt.Errorf("checklist run: read run: %w", err)
	}
	row.StartedAt = time.Unix(startedAt, 0).UTC()
	if completedAt.Valid {
		t := time.Unix(completedAt.Int64, 0).UTC()
		row.CompletedAt = &t
	}
	if abandonedAt.Valid {
		t := time.Unix(abandonedAt.Int64, 0).UTC()
		row.AbandonedAt = &t
	}
	return row, nil
}

// buildChecklistRunView is the read path's whole join, described in this
// file's own top comment: parse the CURRENT body (parseChecklistItems),
// read every tick row this run has, and merge. items retains
// parseChecklistItems' own document order throughout - it is never
// re-sorted by checked state.
func buildChecklistRunView(q sqlQueryer, run checklistRunRow, body string) (checklistRunView, error) {
	items := parseChecklistItems(body)

	type tickRow struct {
		text      string
		checkedAt time.Time
	}
	ticks := map[string]tickRow{} // key: item_key + "\x00" + occurrence
	rows, err := q.Query(`SELECT item_key, occurrence, text, checked_at FROM note_checklist_run_ticks WHERE run_id = ? ORDER BY checked_at`, run.ID)
	if err != nil {
		return checklistRunView{}, fmt.Errorf("checklist run: read ticks: %w", err)
	}
	type rawTick struct {
		key        string
		occurrence int
		text       string
		checkedAt  int64
	}
	var raw []rawTick
	for rows.Next() {
		var rt rawTick
		if err := rows.Scan(&rt.key, &rt.occurrence, &rt.text, &rt.checkedAt); err != nil {
			rows.Close()
			return checklistRunView{}, fmt.Errorf("checklist run: scan tick: %w", err)
		}
		raw = append(raw, rt)
	}
	scanErr := rows.Err()
	rows.Close()
	if scanErr != nil {
		return checklistRunView{}, fmt.Errorf("checklist run: read ticks: %w", scanErr)
	}

	consumed := make(map[string]bool, len(raw))
	for _, rt := range raw {
		mapKey := checklistTickMapKey(rt.key, rt.occurrence)
		ticks[mapKey] = tickRow{text: rt.text, checkedAt: time.Unix(rt.checkedAt, 0).UTC()}
	}

	view := checklistRunView{
		ID:          run.ID,
		DocumentID:  run.DocumentID,
		StartedAt:   run.StartedAt,
		CompletedAt: run.CompletedAt,
		AbandonedAt: run.AbandonedAt,
		Items:       make([]checklistItemView, 0, len(items)),
		// Non-nil, like Items: a nil slice marshals as `null`, and
		// use-checklist-run.ts declares `changed: ChecklistChangedItem[]`
		// while checklist-runner.tsx dereferences `run.changed.length`
		// unguarded. A fresh run - the normal path - has nothing changed,
		// so this is the difference between the runner opening and the
		// render tree throwing.
		Changed: make([]checklistChangedView, 0),
	}
	for _, item := range items {
		mapKey := checklistTickMapKey(item.Key, item.Occurrence)
		iv := checklistItemView{ItemKey: item.Key, Occurrence: item.Occurrence, Text: item.Text, Depth: item.Depth}
		if tick, ok := ticks[mapKey]; ok {
			consumed[mapKey] = true
			iv.Checked = true
			t := tick.checkedAt
			iv.CheckedAt = &t
		}
		view.Items = append(view.Items, iv)
		if iv.Checked {
			view.CheckedCount++
		}
	}
	view.Total = len(view.Items)

	// Changed: any tick NOT consumed by a current item, in the same
	// checked_at order the query above already returned them in - this is
	// the exact-match miss plan §3 says must surface loudly, never be
	// fuzzy-matched onto a differently-keyed item (AGENTS.md).
	for _, rt := range raw {
		mapKey := checklistTickMapKey(rt.key, rt.occurrence)
		if consumed[mapKey] {
			continue
		}
		view.Changed = append(view.Changed, checklistChangedView{
			ItemKey:    rt.key,
			Occurrence: rt.occurrence,
			Text:       rt.text,
			CheckedAt:  time.Unix(rt.checkedAt, 0).UTC(),
		})
	}

	return view, nil
}

func checklistTickMapKey(itemKey string, occurrence int) string {
	return fmt.Sprintf("%s\x00%d", itemKey, occurrence)
}

// StartOrResumeChecklistRun is POST /api/notes/:id/checklist-runs (plan
// §3/§4): if documentID already has an active run (completed_at AND
// abandoned_at both NULL - the schema's own partial unique index),
// resumed=true and that SAME run comes back, rebuilt against the note's
// CURRENT body (which may have changed since the run started - that is
// exactly what the changed-item path is for). Otherwise a fresh run is
// inserted, with nothing ticked - a "[x]" already in the body is never
// read here at all (plan §3: "the note is the template; the run is the
// state").
func (s *documentStore) StartOrResumeChecklistRun(documentID string) (checklistRunView, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	doc, body, err := s.noteAndBody(documentID)
	if err != nil {
		return checklistRunView{}, false, err
	}

	existing, found, err := s.activeChecklistRunRowLocked(documentID)
	if err != nil {
		return checklistRunView{}, false, err
	}
	if found {
		view, err := buildChecklistRunView(s.db, existing, body)
		return view, true, err
	}

	items := parseChecklistItems(body)
	if len(items) == 0 {
		return checklistRunView{}, false, errNoteHasNoChecklist
	}

	now := s.now()
	row := checklistRunRow{
		ID:           uuid.NewString(),
		DocumentID:   documentID,
		SourceSHA256: doc.SHA256,
		StartedAt:    now,
	}
	if _, err := s.db.Exec(
		`INSERT INTO note_checklist_runs (id, document_id, source_sha256, started_at) VALUES (?, ?, ?, ?)`,
		row.ID, row.DocumentID, row.SourceSHA256, now.Unix(),
	); err != nil {
		return checklistRunView{}, false, fmt.Errorf("start checklist run: %w", err)
	}

	view, err := buildChecklistRunView(s.db, row, body)
	return view, false, err
}

// activeChecklistRunRowLocked reads documentID's active run row (found is
// false, no error, when there is none) - s.mu already held.
func (s *documentStore) activeChecklistRunRowLocked(documentID string) (checklistRunRow, bool, error) {
	var row checklistRunRow
	var startedAt int64
	err := s.db.QueryRow(
		`SELECT id, document_id, source_sha256, started_at FROM note_checklist_runs
			WHERE document_id = ? AND completed_at IS NULL AND abandoned_at IS NULL`,
		documentID,
	).Scan(&row.ID, &row.DocumentID, &row.SourceSHA256, &startedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return checklistRunRow{}, false, nil
	}
	if err != nil {
		return checklistRunRow{}, false, fmt.Errorf("checklist run: read active run: %w", err)
	}
	row.StartedAt = time.Unix(startedAt, 0).UTC()
	return row, true, nil
}

// ActiveChecklistRun is GET /api/notes/:id/checklist-runs/active, and also
// what getNoteHandler calls to fill GET /api/notes/:id's own active_run
// field (plan §4). found is false, with no error, when documentID has no
// active run right now - "you have not started one" is a normal state, not
// a 404.
func (s *documentStore) ActiveChecklistRun(documentID string) (checklistRunView, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, body, err := s.noteAndBody(documentID)
	if err != nil {
		return checklistRunView{}, false, err
	}
	row, found, err := s.activeChecklistRunRowLocked(documentID)
	if err != nil || !found {
		return checklistRunView{}, false, err
	}
	view, err := buildChecklistRunView(s.db, row, body)
	return view, true, err
}

// TickChecklistItem is PATCH /api/checklist-runs/:runId/items (plan §3/§4):
// checked=true upserts a tick row carrying the item's CURRENT text (what
// "re-check" will show if the line changes later); checked=false deletes
// the tick row outright - no row at all is this store's definition of
// "unticked", the same state a never-ticked item is already in. Returns
// the whole run, rebuilt, so the caller never merges state locally
// (plan §3: "it may be the last thing that happens before the operator
// walks away from the screen").
func (s *documentStore) TickChecklistItem(runID, itemKey string, occurrence int, checked bool) (checklistRunView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	run, err := readChecklistRunRow(s.db, runID)
	if err != nil {
		return checklistRunView{}, err
	}
	if run.closed() {
		return checklistRunView{}, errChecklistRunClosed
	}

	_, body, err := s.noteAndBody(run.DocumentID)
	if err != nil {
		return checklistRunView{}, err
	}
	items := parseChecklistItems(body)

	var matched *checklistItem
	for i := range items {
		if items[i].Key == itemKey && items[i].Occurrence == occurrence {
			matched = &items[i]
			break
		}
	}
	if matched == nil {
		return checklistRunView{}, errChecklistItemNotFound
	}

	if checked {
		now := s.now().Unix()
		if _, err := s.db.Exec(
			`INSERT INTO note_checklist_run_ticks (run_id, item_key, occurrence, text, checked_at)
				VALUES (?, ?, ?, ?, ?)
				ON CONFLICT(run_id, item_key, occurrence) DO UPDATE SET text = excluded.text, checked_at = excluded.checked_at`,
			runID, itemKey, occurrence, matched.Text, now,
		); err != nil {
			return checklistRunView{}, fmt.Errorf("tick checklist item: %w", err)
		}
	} else {
		if _, err := s.db.Exec(
			`DELETE FROM note_checklist_run_ticks WHERE run_id = ? AND item_key = ? AND occurrence = ?`,
			runID, itemKey, occurrence,
		); err != nil {
			return checklistRunView{}, fmt.Errorf("untick checklist item: %w", err)
		}
	}

	return buildChecklistRunView(s.db, run, body)
}

// CompleteChecklistRun is POST /api/checklist-runs/:runId/complete: stamps
// completed_at and returns the final view. errChecklistRunClosed if the
// run is already completed or abandoned - completing a closed run twice is
// a real error, not an idempotent no-op, the same reasoning
// TickChecklistItem's own closed check gives.
func (s *documentStore) CompleteChecklistRun(runID string) (checklistRunView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	run, err := readChecklistRunRow(s.db, runID)
	if err != nil {
		return checklistRunView{}, err
	}
	if run.closed() {
		return checklistRunView{}, errChecklistRunClosed
	}

	now := s.now()
	if _, err := s.db.Exec(`UPDATE note_checklist_runs SET completed_at = ? WHERE id = ?`, now.Unix(), runID); err != nil {
		return checklistRunView{}, fmt.Errorf("complete checklist run: %w", err)
	}
	run.CompletedAt = &now

	_, body, err := s.noteAndBody(run.DocumentID)
	if err != nil {
		return checklistRunView{}, err
	}
	return buildChecklistRunView(s.db, run, body)
}

// AbandonChecklistRun is DELETE /api/checklist-runs/:runId: stamps
// abandoned_at and NEVER deletes the row (plan §3/§4) - ON DELETE CASCADE
// on note_checklist_run_ticks means the row staying put is exactly what
// keeps a stopped run's own tick history intact rather than orphaning it.
// errChecklistRunClosed if the run is already completed or abandoned.
func (s *documentStore) AbandonChecklistRun(runID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	run, err := readChecklistRunRow(s.db, runID)
	if err != nil {
		return err
	}
	if run.closed() {
		return errChecklistRunClosed
	}

	res, err := s.db.Exec(`UPDATE note_checklist_runs SET abandoned_at = ? WHERE id = ?`, s.now().Unix(), runID)
	if err != nil {
		return fmt.Errorf("abandon checklist run: %w", err)
	}
	return checkRowsAffected(res, errChecklistRunNotFound)
}
