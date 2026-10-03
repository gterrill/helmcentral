package main

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// The changeset core (ADR 0158) is tested against two small fake record
// types kept in a table of their own, so its rules are checked without any
// real record type's behaviour in the way: a mooring (name, no delete) and a
// buoy (name, depth, a mooring reference).

type changesetEnv struct {
	store *documentStore
	reg   *recordRegistry
	clock time.Time
}

func newChangesetEnv(t *testing.T) *changesetEnv {
	t.Helper()
	store := newTestDocumentStore(t)
	env := &changesetEnv{store: store, clock: time.Date(2026, 10, 3, 8, 0, 0, 0, time.UTC)}
	store.now = func() time.Time { return env.clock }
	if _, err := store.db.Exec(`CREATE TABLE test_things (
		id TEXT PRIMARY KEY, kind TEXT NOT NULL, name TEXT NOT NULL, depth REAL, mooring_id TEXT, updated_at INTEGER NOT NULL)`); err != nil {
		t.Fatalf("create test table: %v", err)
	}
	env.reg = newRecordRegistry()
	env.reg.register(fakeThingType("mooring", []string{changeCreate, changeUpdate}))
	env.reg.register(fakeThingType("buoy", []string{changeCreate, changeUpdate, changeDelete}))
	return env
}

func (e *changesetEnv) advance() { e.clock = e.clock.Add(time.Hour) }

func (e *changesetEnv) seed(t *testing.T, kind, name string) string {
	t.Helper()
	id := uuid.NewString()
	if _, err := e.store.db.Exec(`INSERT INTO test_things (id, kind, name, updated_at) VALUES (?, ?, ?, ?)`, id, kind, name, e.clock.Unix()); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return id
}

func (e *changesetEnv) names(t *testing.T) []string {
	t.Helper()
	rows, err := e.store.db.Query(`SELECT kind || ':' || name FROM test_things ORDER BY kind, name`)
	if err != nil {
		t.Fatalf("names: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, s)
	}
	return out
}

func fakeThingType(kind string, actions []string) *recordType {
	fields := []recordField{{Name: "name", Kind: kindString, Writable: true, Description: "what it is called"}}
	if kind == "buoy" {
		fields = append(fields,
			recordField{Name: "depth", Kind: kindNumber, Writable: true, Nullable: true, Description: "metres"},
			recordField{Name: "mooring_id", Kind: kindID, Writable: true, Nullable: true, Ref: "mooring"},
		)
	}
	fields = append(fields, recordField{Name: "kind", Kind: kindString})
	get := func(q sqlQueryer, id string) (recordSnapshot, error) {
		var name string
		var depth sql.NullFloat64
		var mooring sql.NullString
		var updated int64
		err := q.QueryRow(`SELECT name, depth, mooring_id, updated_at FROM test_things WHERE id = ? AND kind = ?`, id, kind).Scan(&name, &depth, &mooring, &updated)
		if errors.Is(err, sql.ErrNoRows) {
			return recordSnapshot{}, errRecordNotFound
		}
		if err != nil {
			return recordSnapshot{}, err
		}
		f := map[string]any{"name": name, "kind": kind}
		if kind == "buoy" {
			f["depth"], f["mooring_id"] = nil, nil
			if depth.Valid {
				f["depth"] = depth.Float64
			}
			if mooring.Valid {
				f["mooring_id"] = mooring.String
			}
		}
		return recordSnapshot{ID: id, Label: name, Version: time.Unix(updated, 0).UTC().Format(time.RFC3339), Fields: f}, nil
	}
	return &recordType{
		Name: kind, Label: kind, Summary: "a fake " + kind, Fields: fields, Actions: actions,
		Get: get,
		List: func(s *documentStore, filter map[string]string) ([]recordSnapshot, error) {
			return nil, nil
		},
		Create: func(env changeEnv, f fieldSet) (createdRecord, error) {
			name, _ := f["name"].(string)
			if strings.TrimSpace(name) == "" {
				return createdRecord{}, &inventoryValidationError{Field: "name", Message: "name is required"}
			}
			id := uuid.NewString()
			var depth, mooring any
			if v, ok := f["depth"]; ok {
				depth = v
			}
			if v, ok := f["mooring_id"]; ok {
				mooring = v
			}
			_, err := env.tx.Exec(`INSERT INTO test_things (id, kind, name, depth, mooring_id, updated_at) VALUES (?, ?, ?, ?, ?, ?)`, id, kind, name, depth, mooring, env.now.Unix())
			return createdRecord{ID: id}, err
		},
		Update: func(env changeEnv, before recordSnapshot, f fieldSet) error {
			merged := mergeFields(before.Fields, f)
			if name, _ := merged["name"].(string); strings.TrimSpace(name) == "" {
				return &inventoryValidationError{Field: "name", Message: "name is required"}
			}
			_, err := env.tx.Exec(`UPDATE test_things SET name = ?, depth = ?, mooring_id = ?, updated_at = ? WHERE id = ?`,
				merged["name"], merged["depth"], merged["mooring_id"], env.now.Unix(), before.ID)
			return err
		},
		Delete: func(env changeEnv, before recordSnapshot) error {
			_, err := env.tx.Exec(`DELETE FROM test_things WHERE id = ?`, before.ID)
			return err
		},
		Effects: func(q sqlQueryer, before recordSnapshot) ([]string, error) {
			if before.Fields["mooring_id"] != nil {
				return []string{"leaves its mooring empty"}, nil
			}
			return nil, nil
		},
		Href: func(r recordSnapshot) string { return "/things/" + r.ID },
	}
}

func (e *changesetEnv) prepare(t *testing.T, ops []changeOp) ([]changeOp, error) {
	t.Helper()
	return e.reg.prepare(e.store, time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC), ops)
}

func (e *changesetEnv) apply(t *testing.T, ops []changeOp) ([]changeOpResult, error) {
	t.Helper()
	var res []changeOpResult
	err := e.store.RunTx(func(tx *sql.Tx, now time.Time) error {
		var err error
		res, err = e.reg.apply(tx, now, time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC), ops)
		return err
	})
	return res, err
}

func prepareError(t *testing.T, e *changesetEnv, ops []changeOp) string {
	t.Helper()
	_, err := e.prepare(t, ops)
	if err == nil {
		t.Fatalf("expected the changeset to be refused: %+v", ops)
	}
	return err.Error()
}

// ── shape ───────────────────────────────────────────────────────────────

func TestChangeset_RefusesBadShapes(t *testing.T) {
	env := newChangesetEnv(t)
	buoy := env.seed(t, "buoy", "North")
	mooring := env.seed(t, "mooring", "Pile")

	tooMany := make([]changeOp, changesetMaxOps+1)
	for i := range tooMany {
		tooMany[i] = changeOp{Type: "buoy", Action: changeCreate, Fields: map[string]any{"name": fmt.Sprint("b", i)}}
	}
	cases := []struct {
		name string
		ops  []changeOp
		want string
	}{
		{"no ops", nil, "at least one"},
		{"too many ops", tooMany, "at most 50"},
		{"unknown type", []changeOp{{Type: "anchor", Action: changeCreate}}, `operations[0]: type: unknown record type "anchor"; the types are buoy, mooring`},
		{"unknown action", []changeOp{{Type: "buoy", Action: "rename", ID: buoy}}, "action: must be create, update or delete"},
		{"action the type does not allow", []changeOp{{Type: "mooring", Action: changeDelete, ID: mooring}}, "operations[0] (mooring delete): action: mooring records cannot be deleted by a changeset"},
		{"create with an id", []changeOp{{Type: "buoy", Action: changeCreate, ID: "x", Fields: map[string]any{"name": "A"}}}, "id: a create has no id yet"},
		{"update without an id", []changeOp{{Type: "buoy", Action: changeUpdate, Fields: map[string]any{"name": "A"}}}, "id: is required"},
		{"update without fields", []changeOp{{Type: "buoy", Action: changeUpdate, ID: buoy}}, "fields: give at least one field to change"},
		{"delete with fields", []changeOp{{Type: "buoy", Action: changeDelete, ID: buoy, Fields: map[string]any{"name": "A"}}}, "fields: a delete takes no fields"},
		{"unknown field", []changeOp{{Type: "buoy", Action: changeUpdate, ID: buoy, Fields: map[string]any{"colour": "red"}}}, `colour: buoy has no such field; the fields are name, depth, mooring_id`},
		{"read-only field", []changeOp{{Type: "buoy", Action: changeUpdate, ID: buoy, Fields: map[string]any{"kind": "x"}}}, "kind: cannot be changed"},
		{"wrong kind", []changeOp{{Type: "buoy", Action: changeUpdate, ID: buoy, Fields: map[string]any{"depth": "deep"}}}, "depth: must be a number"},
		{"null on a field that cannot be cleared", []changeOp{{Type: "buoy", Action: changeUpdate, ID: buoy, Fields: map[string]any{"name": nil}}}, "name: cannot be cleared"},
		{"id field that is not a string", []changeOp{{Type: "buoy", Action: changeUpdate, ID: buoy, Fields: map[string]any{"mooring_id": 4.0}}}, "mooring_id: must be text"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if msg := prepareError(t, env, tc.ops); !strings.Contains(msg, tc.want) {
				t.Fatalf("expected %q in %q", tc.want, msg)
			}
		})
	}
}

func TestChangeset_PaddedIDIsTrimmedOnce(t *testing.T) {
	env := newChangesetEnv(t)
	buoy := env.seed(t, "buoy", "North")
	ops, err := env.prepare(t, []changeOp{{Type: "buoy", Action: changeUpdate, ID: "  " + buoy + " ", Fields: map[string]any{"name": "North Cardinal"}}})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if ops[0].ID != buoy {
		t.Fatalf("expected the trimmed id %q, got %q", buoy, ops[0].ID)
	}
}

func TestChangeset_UnknownTargetNamesTheRecord(t *testing.T) {
	env := newChangesetEnv(t)
	msg := prepareError(t, env, []changeOp{{Type: "buoy", Action: changeUpdate, ID: "nope", Fields: map[string]any{"name": "A"}}})
	if !strings.Contains(msg, `operations[0] (buoy update): id: no buoy with id "nope"`) {
		t.Fatalf("got %q", msg)
	}
}

// ── propose ─────────────────────────────────────────────────────────────

func TestChangeset_PrepareWritesNothingAndFillsWhatTheCardShows(t *testing.T) {
	env := newChangesetEnv(t)
	buoy := env.seed(t, "buoy", "North")
	env.advance()
	before := env.names(t)

	ops, err := env.prepare(t, []changeOp{
		{Type: "buoy", Action: changeUpdate, ID: buoy, Fields: map[string]any{"name": "North Cardinal", "depth": 4.5}},
		{Type: "buoy", Action: changeCreate, Fields: map[string]any{"name": "South"}},
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if got := env.names(t); strings.Join(got, ",") != strings.Join(before, ",") {
		t.Fatalf("prepare must write nothing, rows %v then %v", before, got)
	}
	up := ops[0]
	if up.Label != "North" || up.BaseVersion == "" || up.Description == "" {
		t.Fatalf("update op missing label, base version or description: %+v", up)
	}
	if up.Before["name"] != "North" || up.After["name"] != "North Cardinal" || up.After["depth"] != 4.5 || up.Before["depth"] != nil {
		t.Fatalf("expected before and after for the fields given, got before %v after %v", up.Before, up.After)
	}
	if _, has := up.After["mooring_id"]; has {
		t.Fatalf("before and after carry only the fields the operation names, got %v", up.After)
	}
	cr := ops[1]
	if cr.BaseVersion != "" || cr.Before != nil || cr.After["name"] != "South" || !strings.Contains(cr.Description, "South") {
		t.Fatalf("unexpected create op: %+v", cr)
	}
}

func TestChangeset_ModelSuppliedOutputFieldsAreIgnored(t *testing.T) {
	env := newChangesetEnv(t)
	buoy := env.seed(t, "buoy", "North")
	ops, err := env.prepare(t, []changeOp{{
		Type: "buoy", Action: changeUpdate, ID: buoy, Fields: map[string]any{"name": "N"},
		Description: "I made this up", Label: "Forged", Before: map[string]any{"name": "x"}, Watches: []recordWatch{{Type: "buoy", ID: "q"}},
	}})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if ops[0].Description == "I made this up" || ops[0].Label != "North" || len(ops[0].Watches) != 0 || ops[0].Before["name"] != "North" {
		t.Fatalf("output-only fields must come from the server, got %+v", ops[0])
	}
}

func TestChangeset_UnchangedUpdateIsRefused(t *testing.T) {
	env := newChangesetEnv(t)
	buoy := env.seed(t, "buoy", "North")
	msg := prepareError(t, env, []changeOp{{Type: "buoy", Action: changeUpdate, ID: buoy, Fields: map[string]any{"name": "North"}}})
	if !strings.Contains(msg, `fields: the change leaves "North" exactly as it is; propose only what differs`) {
		t.Fatalf("got %q", msg)
	}
}

func TestChangeset_BaseVersionMismatchIsRefusedAndMissingOneIsFilled(t *testing.T) {
	env := newChangesetEnv(t)
	buoy := env.seed(t, "buoy", "North")
	env.advance()
	msg := prepareError(t, env, []changeOp{{Type: "buoy", Action: changeUpdate, ID: buoy, BaseVersion: "2020-01-01T00:00:00Z", Fields: map[string]any{"name": "N"}}})
	if !strings.Contains(msg, "base_version: North has changed since you read it") {
		t.Fatalf("got %q", msg)
	}
	ops, err := env.prepare(t, []changeOp{{Type: "buoy", Action: changeUpdate, ID: buoy, Fields: map[string]any{"name": "N"}}})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if want := time.Date(2026, 10, 3, 8, 0, 0, 0, time.UTC).Format(time.RFC3339); ops[0].BaseVersion != want {
		t.Fatalf("expected the current version %q, got %q", want, ops[0].BaseVersion)
	}
}

func TestChangeset_DeleteDescriptionCarriesWhatGoesWithIt(t *testing.T) {
	env := newChangesetEnv(t)
	mooring := env.seed(t, "mooring", "Pile")
	buoy := env.seed(t, "buoy", "North")
	if _, err := env.store.db.Exec(`UPDATE test_things SET mooring_id = ? WHERE id = ?`, mooring, buoy); err != nil {
		t.Fatalf("link: %v", err)
	}
	ops, err := env.prepare(t, []changeOp{{Type: "buoy", Action: changeDelete, ID: buoy}})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if want := "Delete buoy North (leaves its mooring empty)"; ops[0].Description != want {
		t.Fatalf("description %q, want %q", ops[0].Description, want)
	}
	if ops[0].Before["name"] != "North" || ops[0].After != nil {
		t.Fatalf("a delete shows what is there and nothing after, got before %v after %v", ops[0].Before, ops[0].After)
	}
}

// ── local references ────────────────────────────────────────────────────

func TestChangeset_LocalReferenceNamesARecordCreatedEarlier(t *testing.T) {
	env := newChangesetEnv(t)
	ops, err := env.prepare(t, []changeOp{
		{Type: "mooring", Action: changeCreate, Fields: map[string]any{"name": "Pile"}},
		{Type: "buoy", Action: changeCreate, Fields: map[string]any{"name": "North", "mooring_id": "$1"}},
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if ops[1].Fields["mooring_id"] != "$1" {
		t.Fatalf("the proposal keeps the reference, the id exists only once applied: %+v", ops[1].Fields)
	}
	res, err := env.apply(t, ops)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(res) != 2 || res[0].ID == "" || res[1].ID == "" || res[0].Type != "mooring" || res[1].Action != changeCreate {
		t.Fatalf("unexpected results %+v", res)
	}
	var mooringID string
	if err := env.store.db.QueryRow(`SELECT mooring_id FROM test_things WHERE id = ?`, res[1].ID).Scan(&mooringID); err != nil || mooringID != res[0].ID {
		t.Fatalf("the buoy should sit on the new mooring %s, got %q (%v)", res[0].ID, mooringID, err)
	}
	if res[1].Label != "North" {
		t.Fatalf("result label %q", res[1].Label)
	}
}

func TestChangeset_LocalReferenceCanTargetALaterUpdateOfTheNewRecord(t *testing.T) {
	env := newChangesetEnv(t)
	ops, err := env.prepare(t, []changeOp{
		{Type: "buoy", Action: changeCreate, Fields: map[string]any{"name": "North"}},
		{Type: "buoy", Action: changeUpdate, ID: "$1", Fields: map[string]any{"depth": 3.0}},
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if _, err := env.apply(t, ops); err != nil {
		t.Fatalf("apply: %v", err)
	}
	var depth float64
	if err := env.store.db.QueryRow(`SELECT depth FROM test_things WHERE name = 'North'`).Scan(&depth); err != nil || depth != 3 {
		t.Fatalf("expected depth 3, got %v (%v)", depth, err)
	}
}

func TestChangeset_BadLocalReferencesAreRefused(t *testing.T) {
	env := newChangesetEnv(t)
	buoy := env.seed(t, "buoy", "North")
	cases := []struct {
		name string
		ops  []changeOp
		want string
	}{
		{"a later operation", []changeOp{
			{Type: "buoy", Action: changeCreate, Fields: map[string]any{"name": "A", "mooring_id": "$2"}},
			{Type: "mooring", Action: changeCreate, Fields: map[string]any{"name": "Pile"}},
		}, `mooring_id: "$2" must name an earlier operation`},
		{"an update, not a create", []changeOp{
			{Type: "buoy", Action: changeUpdate, ID: buoy, Fields: map[string]any{"name": "B"}},
			{Type: "buoy", Action: changeCreate, Fields: map[string]any{"name": "A", "mooring_id": "$1"}},
		}, `mooring_id: "$1" must name an operation that creates a record`},
		{"the wrong type", []changeOp{
			{Type: "buoy", Action: changeCreate, Fields: map[string]any{"name": "A"}},
			{Type: "buoy", Action: changeCreate, Fields: map[string]any{"name": "B", "mooring_id": "$1"}},
		}, `mooring_id: "$1" creates a buoy, but this field takes a mooring`},
		{"nonsense", []changeOp{
			{Type: "buoy", Action: changeCreate, Fields: map[string]any{"name": "A", "mooring_id": "$x"}},
		}, `mooring_id: "$x" is not a reference`},
		{"base version on a reference", []changeOp{
			{Type: "buoy", Action: changeCreate, Fields: map[string]any{"name": "A"}},
			{Type: "buoy", Action: changeUpdate, ID: "$1", BaseVersion: "x", Fields: map[string]any{"depth": 1.0}},
		}, "base_version: a record created in this changeset has no version"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if msg := prepareError(t, env, tc.ops); !strings.Contains(msg, tc.want) {
				t.Fatalf("expected %q in %q", tc.want, msg)
			}
		})
	}
}

// ── a refusal part way through ──────────────────────────────────────────

func TestChangeset_TwoOperationsOnOneRecordRunInOrder(t *testing.T) {
	env := newChangesetEnv(t)
	buoy := env.seed(t, "buoy", "North")
	// Both operations target the same record: the second is judged against
	// what the first leaves, not against the stored row.
	if _, err := env.prepare(t, []changeOp{
		{Type: "buoy", Action: changeUpdate, ID: buoy, Fields: map[string]any{"depth": 2.0}},
		{Type: "buoy", Action: changeUpdate, ID: buoy, Fields: map[string]any{"name": "South"}},
	}); err != nil {
		t.Fatalf("prepare: %v", err)
	}
}

func TestChangeset_ACommandRefusalNamesTheOperationAndField(t *testing.T) {
	env := newChangesetEnv(t)
	msg := prepareError(t, env, []changeOp{
		{Type: "buoy", Action: changeCreate, Fields: map[string]any{"name": "A"}},
		{Type: "buoy", Action: changeCreate, Fields: map[string]any{"name": " "}},
	})
	if !strings.Contains(msg, "operations[1] (buoy create): name: name is required") {
		t.Fatalf("got %q", msg)
	}
}

// ── apply ───────────────────────────────────────────────────────────────

func TestChangeset_ApplyIsAllOrNothing(t *testing.T) {
	env := newChangesetEnv(t)
	buoy := env.seed(t, "buoy", "North")
	ops, err := env.prepare(t, []changeOp{
		{Type: "buoy", Action: changeCreate, Fields: map[string]any{"name": "South"}},
		{Type: "buoy", Action: changeUpdate, ID: buoy, Fields: map[string]any{"depth": 2.0}},
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	// Make the second operation fail at apply time without touching its
	// version: name it so that the update's own check refuses.
	ops[1].Fields = map[string]any{"name": ""}
	before := env.names(t)
	_, err = env.apply(t, ops)
	if err == nil {
		t.Fatal("expected the second operation to fail")
	}
	var oerr *changeOpError
	if !errors.As(err, &oerr) || oerr.Index != 1 {
		t.Fatalf("expected a changeOpError for operation 1, got %v", err)
	}
	if got := env.names(t); strings.Join(got, ",") != strings.Join(before, ",") {
		t.Fatalf("a failing changeset writes nothing: %v then %v", before, got)
	}
}

func TestChangeset_ApplyGoesStaleWhenTheTargetChanged(t *testing.T) {
	env := newChangesetEnv(t)
	buoy := env.seed(t, "buoy", "North")
	env.advance()
	ops, err := env.prepare(t, []changeOp{
		{Type: "buoy", Action: changeCreate, Fields: map[string]any{"name": "South"}},
		{Type: "buoy", Action: changeUpdate, ID: buoy, Fields: map[string]any{"depth": 2.0}},
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	env.advance()
	if _, err := env.store.db.Exec(`UPDATE test_things SET name = 'North 2', updated_at = ? WHERE id = ?`, env.clock.Unix(), buoy); err != nil {
		t.Fatalf("edit: %v", err)
	}
	before := env.names(t)
	_, err = env.apply(t, ops)
	var stale *staleProposalError
	if !errors.As(err, &stale) {
		t.Fatalf("expected a stale error, got %v", err)
	}
	if !strings.Contains(stale.reason, "North changed since Mate proposed this, so nothing was applied") {
		t.Fatalf("the reason should name the record, got %q", stale.reason)
	}
	if got := env.names(t); strings.Join(got, ",") != strings.Join(before, ",") {
		t.Fatalf("a stale changeset writes nothing: %v then %v", before, got)
	}
}

func TestChangeset_ApplyGoesStaleWhenTheTargetIsGone(t *testing.T) {
	env := newChangesetEnv(t)
	buoy := env.seed(t, "buoy", "North")
	ops, err := env.prepare(t, []changeOp{{Type: "buoy", Action: changeUpdate, ID: buoy, Fields: map[string]any{"depth": 2.0}}})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if _, err := env.store.db.Exec(`DELETE FROM test_things WHERE id = ?`, buoy); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, err = env.apply(t, ops)
	var stale *staleProposalError
	if !errors.As(err, &stale) || !strings.Contains(stale.reason, "North was removed since Mate proposed this, so nothing was applied") {
		t.Fatalf("expected a stale error naming the record, got %v", err)
	}
}

func TestChangeset_ApplyNamesWhatItWroteForTheCardsLinks(t *testing.T) {
	env := newChangesetEnv(t)
	buoy := env.seed(t, "buoy", "North")
	env.advance()
	ops, err := env.prepare(t, []changeOp{
		{Type: "buoy", Action: changeUpdate, ID: buoy, Fields: map[string]any{"depth": 2.0}},
		{Type: "buoy", Action: changeDelete, ID: buoy},
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	res, err := env.apply(t, ops)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(res) != 2 || res[0].ID != buoy || res[0].Action != changeUpdate || res[1].Action != changeDelete || res[1].Label != "North" {
		t.Fatalf("unexpected results %+v", res)
	}
}
