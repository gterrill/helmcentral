package main

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

// pdrWorld is a real passage store with one passage on the planned route and a
// real assistant store holding a card for it.
type pdrWorld struct {
	asst      *assistantStore
	passages  *passageStore
	passage   passageRecord
	decisions []int64
	rec       *decisionReconciler
}

func pdrCardPayload(depart time.Time, date string) string {
	return `{"timezone":"Australia/Brisbane","legs":[{"name":"x"}],"rows":[{"depart_date":"` + date + `","verdict":"go","cells":[{"leg":0,"date":"` + date +
		`","depart":"` + depart.Format(time.RFC3339) + `","arrive":"` + depart.Add(8*time.Hour).Format(time.RFC3339) + `","state":"go","not_checked":[]}]}]}`
}

func newPDRWorld(t *testing.T) *pdrWorld {
	t.Helper()
	w := &pdrWorld{asst: newTestAssistantStore(t), passages: newTestPassageStore(t)}
	start := pdiDepart.Add(time.Hour).UTC()
	for i := 0; i <= 30; i++ {
		if err := w.passages.AppendTrackPoint(recordedTrackPoint{At: start.Add(time.Duration(i) * time.Minute), Lat: pdiStartLat + float64(i)*pdiNM, Lon: pdiLon}); err != nil {
			t.Fatal(err)
		}
	}
	rec, err := w.passages.InsertPassage(passageRecord{Start: start, End: start.Add(30 * time.Minute), Source: trackSourceRecorded})
	if err != nil {
		t.Fatal(err)
	}
	w.passage = rec
	conv, _ := w.asst.CreateConversation("c")
	card := mateCard{Kind: "passage_window", Payload: []byte(pdrCardPayload(pdiDepart, "2026-10-23")), routes: pdiRoutes(), created: pdiCard}
	msg, err := appendCards(t, w.asst, conv.ID, card)
	if err != nil {
		t.Fatal(err)
	}
	w.decisions = []int64{msg.Cards[0].Decision.ID}
	// appendCards stamps created_ms from the card; keep it recent for the 60 day window.
	w.asst.db.Exec(`UPDATE passage_decisions SET card_created_ms = ?`, pdiCard.UnixMilli())
	w.rec = &decisionReconciler{passages: w.passages, assistant: func() *assistantStore { return w.asst }, now: func() time.Time { return pdiNow }}
	return w
}

func TestReconcile_LinksAMatchingPassageAndFollowsItBeingHidden(t *testing.T) {
	w := newPDRWorld(t)
	if err := w.rec.reconcile(time.Time{}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	state, rd, p, src := pdsRow(t, w.asst, w.decisions[0])
	if state != "went" || rd.String != "2026-10-23" || p.String != w.passage.ID || src != "inferred" {
		t.Fatalf("%q %v %v %q", state, rd, p, src)
	}
	// Hide the passage: the inferred went goes with it.
	if _, err := w.passages.PatchPassage(w.passage.ID, passagePatch{Hidden: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	if err := w.rec.reconcile(w.passage.Start, w.passage.End); err != nil {
		t.Fatal(err)
	}
	state, _, p, src = pdsRow(t, w.asst, w.decisions[0])
	if state != "" || p.Valid || src != "" {
		t.Fatalf("hidden: %q %v %q", state, p, src)
	}
	// Unhide: it returns.
	w.passages.PatchPassage(w.passage.ID, passagePatch{Hidden: ptr(false)})
	if err := w.rec.reconcile(w.passage.Start, w.passage.End); err != nil {
		t.Fatal(err)
	}
	if state, _, p, _ := pdsRow(t, w.asst, w.decisions[0]); state != "went" || p.String != w.passage.ID {
		t.Fatalf("unhidden: %q %v", state, p)
	}
}

func TestReconcile_ABadZoneIsLoggedAndSkippedNotGuessed(t *testing.T) {
	w := newPDRWorld(t)
	w.asst.db.Exec(`UPDATE passage_decisions SET payload_json = REPLACE(payload_json, 'Australia/Brisbane', 'Nowhere/Land')`)
	if err := w.rec.reconcile(time.Time{}, time.Time{}); err != nil {
		t.Fatalf("one bad snapshot must not stop the pass: %v", err)
	}
	if state, _, _, _ := pdsRow(t, w.asst, w.decisions[0]); state != "" {
		t.Fatalf("a decision whose zone will not load must be left alone: %q", state)
	}
}

func TestWorker_WakesCoalesceToTheUnionOfTheirSpans(t *testing.T) {
	w := newPDRWorld(t)
	worker := newPassageDecisionsWorker(w.rec, time.Hour)
	worker.wake(w.passage.Start, w.passage.End)
	worker.wake(w.passage.End, w.passage.End.Add(time.Hour))
	from, to, pending := worker.take()
	if !pending || !from.Equal(w.passage.Start) || !to.Equal(w.passage.End.Add(time.Hour)) {
		t.Fatalf("wakes should coalesce to the union: %v %v %v", from, to, pending)
	}
	if _, _, again := worker.take(); again {
		t.Fatal("nothing pending after take")
	}
}

// pdrAddCard adds a card with the given row dates (06:00 local each) made at created.
func pdrAddCard(t *testing.T, w *pdrWorld, created time.Time, dates ...string) int64 {
	t.Helper()
	rows := ""
	for i, d := range dates {
		day, _ := time.ParseInLocation("2006-01-02", d, pdiBrisbane)
		dep := day.Add(6 * time.Hour)
		if i > 0 {
			rows += ","
		}
		rows += `{"depart_date":"` + d + `","verdict":"go","cells":[{"leg":0,"date":"` + d + `","depart":"` + dep.Format(time.RFC3339) + `","arrive":"` + dep.Add(8*time.Hour).Format(time.RFC3339) + `","state":"go","not_checked":[]}]}`
	}
	payload := `{"timezone":"Australia/Brisbane","legs":[{"name":"x"}],"rows":[` + rows + `]}`
	conv, _ := w.asst.CreateConversation("c")
	msg, err := appendCards(t, w.asst, conv.ID, mateCard{Kind: "passage_window", Payload: []byte(payload), routes: pdiRoutes(), created: created})
	if err != nil {
		t.Fatal(err)
	}
	return msg.Cards[0].Decision.ID
}

func TestReconcile_AWakeFarFromOneCardStillSeesEveryCard(t *testing.T) {
	w := newPDRWorld(t)
	// B offered the 21st to the 25th and is older. C offered only the 23rd and is later.
	b := pdrAddCard(t, w, pdiCard.Add(-time.Hour), "2026-10-21", "2026-10-22", "2026-10-23", "2026-10-24", "2026-10-25")
	c := pdrAddCard(t, w, pdiCard.Add(time.Hour), "2026-10-23")
	w.passages.SetDetectorResume(time.Date(2026, 10, 26, 0, 0, 0, 0, pdiBrisbane))
	w.asst.db.Exec(`DELETE FROM passage_decisions WHERE id = ?`, w.decisions[0])

	// A wake on the 27th reaches only B's dates; C has to be seen all the same.
	far := time.Date(2026, 10, 27, 0, 0, 0, 0, pdiBrisbane)
	if err := w.rec.reconcile(far, far.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if state, _, p, _ := pdsRow(t, w.asst, c); state != "went" || p.String != w.passage.ID {
		t.Fatalf("C should hold the passage: %q %v", state, p)
	}
	if state, rd, p, src := pdsRow(t, w.asst, b); state != "" || rd.Valid || p.Valid || src != "" {
		t.Fatalf("B must not go on C's passage, nor be called stayed while C supersedes it: %q %v %v %q", state, rd, p, src)
	}
}

func TestReconcile_OldCardsAreOnlyLookedAtWhenTheirPassageIsWoken(t *testing.T) {
	w := newPDRWorld(t)
	if err := w.rec.reconcile(time.Time{}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	w.passages.PatchPassage(w.passage.ID, passagePatch{Hidden: ptr(true)})
	w.rec.now = func() time.Time { return pdiNow.Add(100 * 24 * time.Hour) }

	if err := w.rec.reconcile(time.Time{}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if state, _, _, _ := pdsRow(t, w.asst, w.decisions[0]); state != "went" {
		t.Fatalf("a card past the 60 day window is left alone on a tick: %q", state)
	}
	if err := w.rec.reconcile(w.passage.Start, w.passage.End); err != nil {
		t.Fatal(err)
	}
	if state, _, p, _ := pdsRow(t, w.asst, w.decisions[0]); state != "" || p.Valid {
		t.Fatalf("a wake on the linked (now hidden) passage reaches it: %q %v", state, p)
	}
}

func TestReconcile_AnOldCardLinkedToAMergedAwayPassageIsRecomputed(t *testing.T) {
	w := newPDRWorld(t)
	w.rec.reconcile(time.Time{}, time.Time{})
	w.asst.db.Exec(`UPDATE passage_decisions SET passage_id = 'deleted-passage' WHERE id = ?`, w.decisions[0])
	w.rec.now = func() time.Time { return pdiNow.Add(100 * 24 * time.Hour) }
	if err := w.rec.reconcile(time.Time{}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	// Out of the window and its link names nothing, so it is looked at again;
	// the real passage is out of its 60 day reach only in time, not in match.
	if _, _, p, _ := pdsRow(t, w.asst, w.decisions[0]); p.String == "deleted-passage" {
		t.Fatalf("a link to a passage that no longer exists must be recomputed")
	}
}

func TestPassageTrack_ReadsOnlyThePassagesOwnSource(t *testing.T) {
	s := newTestPassageStore(t)
	start := pdiDepart.UTC()
	for _, p := range []recordedTrackPoint{
		{At: start.Add(-time.Second), Segment: importedSegment, Lat: -10, Lon: 140, Source: trackSourceImported},
		{At: start.Add(10*time.Minute + time.Second), Segment: importedSegment, Lat: -10, Lon: 140, Source: trackSourceImported},
		{At: start.Add(20*time.Minute + time.Second), Segment: importedSegment, Lat: -10, Lon: 140, Source: trackSourceImported},
	} {
		if err := s.AppendTrackPoint(p); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i <= 5; i++ {
		s.AppendTrackPoint(recordedTrackPoint{At: start.Add(time.Duration(i) * time.Minute), Lat: pdiStartLat + float64(i)*pdiNM, Lon: pdiLon, Source: trackSourceRecorded})
	}
	span := passageRecord{Start: start.Add(-time.Minute), End: start.Add(30 * time.Minute)}
	// More imported points than recorded ones, and still the recorded passage reads its own.
	span.Source = trackSourceRecorded
	pts, err := s.PassageTrack(span)
	if err != nil || len(pts) != 6 || pts[0].Source != trackSourceRecorded {
		t.Fatalf("recorded passage: %d points, err %v", len(pts), err)
	}
	span.Source = trackSourceImported
	if pts, _ = s.PassageTrack(span); len(pts) != 3 || pts[0].Source != trackSourceImported {
		t.Fatalf("imported passage: %+v", pts)
	}
	span.Source = ""
	if _, err := s.PassageTrack(span); err == nil {
		t.Fatal("a passage of unknown source has no track to match")
	}
}

func TestReconcile_APassageOfUnknownSourceIsNotMatchedAndNeverMeansStayed(t *testing.T) {
	w := newPDRWorld(t)
	w.passages.SetDetectorResume(time.Date(2026, 10, 26, 0, 0, 0, 0, pdiBrisbane))
	w.passages.db.Exec(`UPDATE passages SET source = ''`)
	if err := w.rec.reconcile(time.Time{}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if state, _, p, _ := pdsRow(t, w.asst, w.decisions[0]); state != "" || p.Valid {
		t.Fatalf("a passage whose source is unknown must not be guessed at, nor the card called stayed: %q %v", state, p)
	}
	// Logged once per passage, not on every pass.
	if !passageSourceUnknownLogged(w.passage.ID) {
		t.Fatal("the passage should have been noted")
	}
	// A hidden one is the operator's "ignore this".
	w.passages.PatchPassage(w.passage.ID, passagePatch{Hidden: ptr(true)})
	if err := w.rec.reconcile(time.Time{}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if state, _, _, _ := pdsRow(t, w.asst, w.decisions[0]); state != "stayed" {
		t.Fatalf("a hidden unreadable passage is ignored: %q", state)
	}
}

func TestReconcile_AMixedMergeLeavesTheCardBlank(t *testing.T) {
	w := newPDRWorld(t)
	w.passages.SetDetectorResume(time.Date(2026, 10, 26, 0, 0, 0, 0, pdiBrisbane))
	imp, _ := w.passages.InsertPassage(passageRecord{Start: w.passage.End.Add(10 * time.Minute), End: w.passage.End.Add(40 * time.Minute), Source: trackSourceImported})
	m, err := w.passages.MergePassages(w.passage.ID, imp.ID)
	if err != nil || m.Source != "" {
		t.Fatalf("merge: %+v %v", m, err)
	}
	if err := w.rec.reconcile(time.Time{}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if state, _, _, _ := pdsRow(t, w.asst, w.decisions[0]); state != "" {
		t.Fatalf("the merged passage left on the plan, so the card is blank: %q", state)
	}
}

func TestReconcile_ABrokenSnapshotIsLoggedOncePerDecision(t *testing.T) {
	w := newPDRWorld(t)
	w.asst.db.Exec(`UPDATE passage_decisions SET payload_json = 'not json'`)
	for i := 0; i < 3; i++ {
		if err := w.rec.reconcile(time.Time{}, time.Time{}); err != nil {
			t.Fatal(err)
		}
	}
	if n := decisionParseFailuresLogged(w.decisions[0]); n != 1 {
		t.Fatalf("logged %d times", n)
	}
}

func TestReconcile_MatchingIgnoresAnOverlappingImportsPoints(t *testing.T) {
	w := newPDRWorld(t)
	// A GPX import over the same hours, before the recorded points: one wild
	// point would become the passage's "first" if sources were mixed.
	w.passages.AppendTrackPoint(recordedTrackPoint{At: w.passage.Start.Add(-time.Second), Segment: importedSegment, Lat: -10, Lon: 140, Source: trackSourceImported})
	if err := w.rec.reconcile(time.Time{}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if state, _, p, _ := pdsRow(t, w.asst, w.decisions[0]); state != "went" || p.String != w.passage.ID {
		t.Fatalf("the passage's own points decide: %q %v", state, p)
	}
}

// pdrWorker runs a worker for the test and returns it.
func pdrWorker(t *testing.T, w *pdrWorld) *passageDecisionsWorker {
	t.Helper()
	worker := newPassageDecisionsWorker(w.rec, time.Hour)
	done := make(chan struct{})
	go worker.run(done)
	t.Cleanup(func() { close(done) })
	prev := passageDecisionsWorkerNow
	passageDecisionsWorkerNow = worker
	t.Cleanup(func() { passageDecisionsWorkerNow = prev })
	return worker
}

func TestPutPassageDecision_ReconcilesInTheSameRequest(t *testing.T) {
	w := newPDRWorld(t)
	prev, prevP := globalAssistantStore, globalPassageStore
	globalAssistantStore, globalPassageStore = w.asst, w.passages
	t.Cleanup(func() { globalAssistantStore, globalPassageStore = prev, prevP })
	pdrWorker(t, w)

	// Card C is later and holds the passage by inference.
	c := pdrAddCard(t, w, pdiCard.Add(time.Hour), "2026-10-23")
	if err := w.rec.reconcile(time.Time{}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if state, _, _, _ := pdsRow(t, w.asst, c); state != "went" {
		t.Fatalf("setup: C should hold the passage, %q", state)
	}

	rec := pdPut(t, "1", `{"state":"went","row_date":"2026-10-23"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"passage_id":"`+w.passage.ID+`"`) {
		t.Fatalf("code %d: %s", rec.Code, rec.Body)
	}
	if state, _, p, _ := pdsRow(t, w.asst, c); state != "" || p.Valid {
		t.Fatalf("the operator's went takes the passage from C in the same request: %q %v", state, p)
	}

	rec = pdPut(t, "1", `{"state":"stayed"}`)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), w.passage.ID) {
		t.Fatalf("code %d: %s", rec.Code, rec.Body)
	}
	if state, _, p, _ := pdsRow(t, w.asst, c); state != "went" || p.String != w.passage.ID {
		t.Fatalf("C gets the passage back in the same request: %q %v", state, p)
	}
}

func TestReconcileNow_StopsWaitingWhenTheRequestEnds(t *testing.T) {
	w := newPDRWorld(t)
	worker := newPassageDecisionsWorker(w.rec, time.Hour) // not running
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := worker.ReconcileNow(ctx); err == nil {
		t.Fatal("a worker that never answers must not hold the request past its context")
	}
}

func TestPutPassageDecision_ALinkOnACardOlderThanTheWindowIsStillMade(t *testing.T) {
	w := newPDRWorld(t)
	prev, prevP := globalAssistantStore, globalPassageStore
	globalAssistantStore, globalPassageStore = w.asst, w.passages
	t.Cleanup(func() { globalAssistantStore, globalPassageStore = prev, prevP })
	w.rec.now = func() time.Time { return pdiNow.Add(90 * 24 * time.Hour) }
	pdrWorker(t, w)
	rec := pdPut(t, "1", `{"state":"went","row_date":"2026-10-23"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"passage_id":"`+w.passage.ID+`"`) {
		t.Fatalf("code %d: %s", rec.Code, rec.Body)
	}
}

func TestPutPassageDecision_StopsWaitingAfterTheCapAndReturnsTheSavedChoice(t *testing.T) {
	w := newPDRWorld(t)
	prev, prevP := globalAssistantStore, globalPassageStore
	globalAssistantStore, globalPassageStore = w.asst, w.passages
	t.Cleanup(func() { globalAssistantStore, globalPassageStore = prev, prevP })
	prevWait := passageDecisionReconcileWait
	passageDecisionReconcileWait = 30 * time.Millisecond
	t.Cleanup(func() { passageDecisionReconcileWait = prevWait })

	worker := newPassageDecisionsWorker(w.rec, time.Hour)
	release := make(chan struct{})
	worker.reconcile = func(from, to time.Time, ids ...int64) error { <-release; return nil }
	done := make(chan struct{})
	go worker.run(done)
	t.Cleanup(func() { close(release); close(done) })
	prevW := passageDecisionsWorkerNow
	passageDecisionsWorkerNow = worker
	t.Cleanup(func() { passageDecisionsWorkerNow = prevW })

	start := time.Now()
	rec := pdPut(t, "1", `{"state":"stayed"}`)
	if rec.Code != http.StatusOK || time.Since(start) > 2*time.Second || !strings.Contains(rec.Body.String(), `"state":"stayed"`) {
		t.Fatalf("code %d after %v: %s", rec.Code, time.Since(start), rec.Body)
	}
}
