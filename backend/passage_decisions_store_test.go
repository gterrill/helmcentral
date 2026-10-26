package main

import (
	"database/sql"
	"testing"
	"time"
)

func pdsSeed(t *testing.T) (*assistantStore, int64) {
	t.Helper()
	asst := newTestAssistantStore(t)
	conv, _ := asst.CreateConversation("c")
	msg, err := appendCards(t, asst, conv.ID, pdCard(1, pdRoutes(1), "2026-10-23", "2026-10-24"))
	if err != nil {
		t.Fatal(err)
	}
	return asst, msg.Cards[0].Decision.ID
}

func pdsRow(t *testing.T, asst *assistantStore, id int64) (state string, rowDate, passage sql.NullString, source string) {
	t.Helper()
	if err := asst.db.QueryRow(`SELECT state, row_date, passage_id, source FROM passage_decisions WHERE id = ?`, id).Scan(&state, &rowDate, &passage, &source); err != nil {
		t.Fatal(err)
	}
	return
}

func TestApplyPassageDecisionUpdates_InferredWriteWritesABlankRow(t *testing.T) {
	asst, id := pdsSeed(t)
	n, err := asst.ApplyPassageDecisionUpdates([]decisionUpdate{{ID: id, State: "went", RowDate: "2026-10-23", PassageID: "p1"}})
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	state, rd, p, src := pdsRow(t, asst, id)
	if state != "went" || rd.String != "2026-10-23" || p.String != "p1" || src != "inferred" {
		t.Fatalf("%q %v %v %q", state, rd, p, src)
	}
	// Back to blank: source '' and NULLs.
	if _, err := asst.ApplyPassageDecisionUpdates([]decisionUpdate{{ID: id}}); err != nil {
		t.Fatal(err)
	}
	state, rd, p, src = pdsRow(t, asst, id)
	if state != "" || rd.Valid || p.Valid || src != "" {
		t.Fatalf("%q %v %v %q", state, rd, p, src)
	}
}

func TestApplyPassageDecisionUpdates_AnInferredWriteLosesToAnOperatorWrite(t *testing.T) {
	asst, id := pdsSeed(t)
	// The reconcile read the blank row, then the operator answered.
	if _, err := asst.SetOperatorPassageDecision(id, "just_looking", nil); err != nil {
		t.Fatal(err)
	}
	n, err := asst.ApplyPassageDecisionUpdates([]decisionUpdate{{ID: id, State: "went", RowDate: "2026-10-23", PassageID: "p1"}})
	if err != nil || n != 0 {
		t.Fatalf("an inferred write must change nothing on an operator row: n=%d err=%v", n, err)
	}
	if state, _, p, src := pdsRow(t, asst, id); state != "just_looking" || p.Valid || src != "operator" {
		t.Fatalf("%q %v %q", state, p, src)
	}
	// An operator blank is not refilled either.
	if _, err := asst.SetOperatorPassageDecision(id, "", nil); err != nil {
		t.Fatal(err)
	}
	if n, _ := asst.ApplyPassageDecisionUpdates([]decisionUpdate{{ID: id, State: "stayed"}}); n != 0 {
		t.Fatalf("operator blank was refilled")
	}
}

func TestApplyPassageDecisionUpdates_LinkNeedsTheOperatorsOwnWent(t *testing.T) {
	asst, id := pdsSeed(t)
	date := "2026-10-23"
	if _, err := asst.SetOperatorPassageDecision(id, "went", &date); err != nil {
		t.Fatal(err)
	}
	link := decisionUpdate{ID: id, Link: true, State: "went", RowDate: date, PassageID: "p1"}
	if n, err := asst.ApplyPassageDecisionUpdates([]decisionUpdate{link}); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if _, _, p, _ := pdsRow(t, asst, id); p.String != "p1" {
		t.Fatalf("link = %v", p)
	}
	// The operator changed their mind between the read and the write.
	other := "2026-10-24"
	if _, err := asst.SetOperatorPassageDecision(id, "went", &other); err != nil {
		t.Fatal(err)
	}
	if n, _ := asst.ApplyPassageDecisionUpdates([]decisionUpdate{link}); n != 0 {
		t.Fatal("a link computed for another row date must not apply")
	}
	if _, err := asst.SetOperatorPassageDecision(id, "stayed", nil); err != nil {
		t.Fatal(err)
	}
	if n, _ := asst.ApplyPassageDecisionUpdates([]decisionUpdate{link}); n != 0 {
		t.Fatal("a link must not apply once the operator said stayed")
	}
	if _, _, p, _ := pdsRow(t, asst, id); p.Valid {
		t.Fatalf("link survived: %v", p)
	}
}

func TestPassageDecisionsForInference_RecentCardsPlusAnythingLinked(t *testing.T) {
	asst := newTestAssistantStore(t)
	conv, _ := asst.CreateConversation("c")
	var ids []int64
	for i := 0; i < 4; i++ {
		msg, err := appendCards(t, asst, conv.ID, pdCard(1, pdRoutes(1), "2026-10-23"))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, msg.Cards[0].Decision.ID)
	}
	old := time.Now().Add(-200 * 24 * time.Hour).UnixMilli()
	for _, id := range ids {
		asst.db.Exec(`UPDATE passage_decisions SET card_created_ms = ? WHERE id = ?`, old, id)
	}
	asst.db.Exec(`UPDATE passage_decisions SET card_created_ms = ? WHERE id = ?`, time.Now().UnixMilli(), ids[0]) // recent blank
	date := "2026-10-23"
	asst.SetOperatorPassageDecision(ids[1], "went", &date)                              // old operator went, unlinked
	asst.db.Exec(`UPDATE passage_decisions SET passage_id = 'p1' WHERE id = ?`, ids[2]) // old and linked
	// ids[3]: old blank
	// An old decision linked to a passage the tick did not wake is left alone.
	rows, err := asst.PassageDecisionsForInference(time.Now().Add(-120*24*time.Hour), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[int64]bool{}
	for _, r := range rows {
		got[r.ID] = true
	}
	if !got[ids[0]] || got[ids[1]] || got[ids[2]] || got[ids[3]] || len(rows) != 1 {
		t.Fatalf("rows = %v (ids %v)", got, ids)
	}
	// It comes in when its passage is among those asked for (woken or gone).
	linkedRows, err := asst.PassageDecisionsForInference(time.Now().Add(-120*24*time.Hour), nil, []string{"p1"})
	if err != nil || len(linkedRows) != 2 {
		t.Fatalf("an old decision linked to a named passage comes in: %d %v", len(linkedRows), err)
	}
	if linked, err := asst.LinkedPassageIDs(); err != nil || len(linked) != 1 || linked[0] != "p1" {
		t.Fatalf("linked ids = %v %v", linked, err)
	}
	forced, err := asst.PassageDecisionsForInference(time.Now().Add(-120*24*time.Hour), []int64{ids[3]}, nil)
	if err != nil || len(forced) != 2 {
		t.Fatalf("an included id comes in whatever its age: %d %v", len(forced), err)
	}
	one, err := asst.PassageDecisionRow(ids[1])
	if err != nil || one.State != "went" || one.RowDate != date || one.Source != "operator" || one.PayloadJSON == "" || one.RoutesJSON == "" {
		t.Fatalf("row = %+v err=%v", one, err)
	}
	if _, err := asst.PassageDecisionRow(9999); err == nil {
		t.Fatal("unknown id must error")
	}
}
