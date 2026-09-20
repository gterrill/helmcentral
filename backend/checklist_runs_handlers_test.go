package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// This file is the handler-level half of plan §3/§4's checklist runs (see
// checklist_runs_handlers.go's own top comment). Fixtures reuse
// mustCreateTestNote/newDocumentEchoContext from notes_handlers_test.go;
// the one new thing this file needs is a path param named "runId" rather
// than "id" - newRunEchoContext below is newDocumentEchoContext's same
// shape with that one difference.

func newRunEchoContext(method, target, body, runID string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	if runID != "" {
		c.SetParamNames("runId")
		c.SetParamValues(runID)
	}
	return c, rec
}

type checklistRunResponse struct {
	Run     checklistRunView `json:"run"`
	Resumed bool             `json:"resumed"`
}

func mustStartTestChecklistRun(t *testing.T, noteID string) checklistRunView {
	t.Helper()
	c, rec := newDocumentEchoContext(http.MethodPost, "/api/notes/"+noteID+"/checklist-runs", "", noteID)
	if err := createChecklistRunHandler(c); err != nil {
		t.Fatalf("createChecklistRunHandler: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp checklistRunResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return resp.Run
}

// ── POST /api/notes/:id/checklist-runs ───────────────────────────────────

func TestCreateChecklistRunHandler_FreshStartReturns201ResumedFalse(t *testing.T) {
	withTestDocumentStore(t)
	note := mustCreateTestNote(t, "- [ ] Seacocks open", "Shutdown")

	c, rec := newDocumentEchoContext(http.MethodPost, "/api/notes/"+note.ID+"/checklist-runs", "", note.ID)
	if err := createChecklistRunHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp checklistRunResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Resumed {
		t.Fatalf("expected resumed=false on a fresh start")
	}
	if resp.Run.ID == "" {
		t.Fatalf("expected a run id back")
	}
	if resp.Run.Total != 1 || resp.Run.CheckedCount != 0 {
		t.Fatalf("expected a fresh unticked run, got %+v", resp.Run)
	}
}

// TestCreateChecklistRunHandler_ResumeReturns200WithResumedTrue is plan
// §3/§4's own worked example: "POST on a note that already has an active
// run returns 200 {run, resumed:true} ... Not 409, not a second run."
func TestCreateChecklistRunHandler_ResumeReturns200WithResumedTrue(t *testing.T) {
	withTestDocumentStore(t)
	note := mustCreateTestNote(t, "- [ ] Seacocks open", "Shutdown")
	first := mustStartTestChecklistRun(t, note.ID)

	c, rec := newDocumentEchoContext(http.MethodPost, "/api/notes/"+note.ID+"/checklist-runs", "", note.ID)
	if err := createChecklistRunHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on a resume, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp checklistRunResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.Resumed {
		t.Fatalf("expected resumed=true")
	}
	if resp.Run.ID != first.ID {
		t.Fatalf("expected the SAME run id back, got %q want %q", resp.Run.ID, first.ID)
	}
}

func TestCreateChecklistRunHandler_NoteWithNoChecklistReturns409(t *testing.T) {
	withTestDocumentStore(t)
	note := mustCreateTestNote(t, "Just some prose.", "A note")

	c, rec := newDocumentEchoContext(http.MethodPost, "/api/notes/"+note.ID+"/checklist-runs", "", note.ID)
	if err := createChecklistRunHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 errNoteHasNoChecklist, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ── GET /api/notes/:id/checklist-runs/active ──────────────────────────────

func TestGetActiveChecklistRunHandler_NoActiveRunReturnsNullRun(t *testing.T) {
	withTestDocumentStore(t)
	note := mustCreateTestNote(t, "- [ ] Seacocks open", "Shutdown")

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/notes/"+note.ID+"/checklist-runs/active", "", note.ID)
	if err := getActiveChecklistRunHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Run *checklistRunView `json:"run"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Run != nil {
		t.Fatalf("expected run: null, got %+v", resp.Run)
	}
}

func TestGetActiveChecklistRunHandler_ReturnsTheActiveRun(t *testing.T) {
	withTestDocumentStore(t)
	note := mustCreateTestNote(t, "- [ ] Seacocks open", "Shutdown")
	started := mustStartTestChecklistRun(t, note.ID)

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/notes/"+note.ID+"/checklist-runs/active", "", note.ID)
	if err := getActiveChecklistRunHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	var resp struct {
		Run *checklistRunView `json:"run"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Run == nil || resp.Run.ID != started.ID {
		t.Fatalf("expected the active run back, got %+v", resp.Run)
	}
}

// ── PATCH /api/checklist-runs/:runId/items ────────────────────────────────

// TestTickChecklistItemHandler_ReturnsTheWholeRun is plan §3/§4's own
// requirement: PATCH returns the WHOLE run, not a per-item ack, so the
// client re-renders from the response rather than merging state locally.
func TestTickChecklistItemHandler_ReturnsTheWholeRun(t *testing.T) {
	withTestDocumentStore(t)
	note := mustCreateTestNote(t, "- [ ] Seacocks open\n- [ ] Check bilge", "Shutdown")
	run := mustStartTestChecklistRun(t, note.ID)
	item := run.Items[0]

	body := `{"item_key":"` + item.ItemKey + `","occurrence":` + jsonInt(item.Occurrence) + `,"checked":true}`
	c, rec := newRunEchoContext(http.MethodPatch, "/api/checklist-runs/"+run.ID+"/items", body, run.ID)
	if err := tickChecklistItemHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Run checklistRunView `json:"run"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Run.Items) != 2 {
		t.Fatalf("expected the WHOLE run's items back (2), got %d: %+v", len(resp.Run.Items), resp.Run.Items)
	}
	if resp.Run.CheckedCount != 1 {
		t.Fatalf("expected checked_count 1, got %d", resp.Run.CheckedCount)
	}
	if !resp.Run.Items[0].Checked {
		t.Fatalf("expected the ticked item to be checked in the response")
	}
}

func TestTickChecklistItemHandler_UnknownRunReturns404(t *testing.T) {
	withTestDocumentStore(t)
	c, rec := newRunEchoContext(http.MethodPatch, "/api/checklist-runs/does-not-exist/items", `{"item_key":"x","occurrence":0,"checked":true}`, "does-not-exist")
	if err := tickChecklistItemHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 errChecklistRunNotFound, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTickChecklistItemHandler_UnknownItemKeyReturns404(t *testing.T) {
	withTestDocumentStore(t)
	note := mustCreateTestNote(t, "- [ ] Seacocks open", "Shutdown")
	run := mustStartTestChecklistRun(t, note.ID)

	c, rec := newRunEchoContext(http.MethodPatch, "/api/checklist-runs/"+run.ID+"/items", `{"item_key":"not-a-real-key","occurrence":0,"checked":true}`, run.ID)
	if err := tickChecklistItemHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 errChecklistItemNotFound, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTickChecklistItemHandler_ClosedRunReturns409(t *testing.T) {
	withTestDocumentStore(t)
	note := mustCreateTestNote(t, "- [ ] Seacocks open", "Shutdown")
	run := mustStartTestChecklistRun(t, note.ID)

	c, rec := newRunEchoContext(http.MethodPost, "/api/checklist-runs/"+run.ID+"/complete", "", run.ID)
	if err := completeChecklistRunHandler(c); err != nil {
		t.Fatalf("completeChecklistRunHandler: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 completing the run, got %d: %s", rec.Code, rec.Body.String())
	}

	item := run.Items[0]
	body := `{"item_key":"` + item.ItemKey + `","occurrence":` + jsonInt(item.Occurrence) + `,"checked":true}`
	c, rec = newRunEchoContext(http.MethodPatch, "/api/checklist-runs/"+run.ID+"/items", body, run.ID)
	if err := tickChecklistItemHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 errChecklistRunClosed, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ── POST /api/checklist-runs/:runId/complete ─────────────────────────────

func TestCompleteChecklistRunHandler_StampsCompletedAt(t *testing.T) {
	withTestDocumentStore(t)
	note := mustCreateTestNote(t, "- [ ] Seacocks open", "Shutdown")
	run := mustStartTestChecklistRun(t, note.ID)

	c, rec := newRunEchoContext(http.MethodPost, "/api/checklist-runs/"+run.ID+"/complete", "", run.ID)
	if err := completeChecklistRunHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Run checklistRunView `json:"run"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Run.CompletedAt == nil {
		t.Fatalf("expected completed_at to be set")
	}
}

func TestCompleteChecklistRunHandler_UnknownRunReturns404(t *testing.T) {
	withTestDocumentStore(t)
	c, rec := newRunEchoContext(http.MethodPost, "/api/checklist-runs/does-not-exist/complete", "", "does-not-exist")
	if err := completeChecklistRunHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ── DELETE /api/checklist-runs/:runId ─────────────────────────────────────

func TestAbandonChecklistRunHandler_Returns204AndSetsAbandonedAt(t *testing.T) {
	store := withTestDocumentStore(t)
	note := mustCreateTestNote(t, "- [ ] Seacocks open", "Shutdown")
	run := mustStartTestChecklistRun(t, note.ID)

	c, rec := newRunEchoContext(http.MethodDelete, "/api/checklist-runs/"+run.ID, "", run.ID)
	if err := abandonChecklistRunHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	if _, found, err := store.ActiveChecklistRun(note.ID); err != nil || found {
		t.Fatalf("expected no active run after abandon: found=%v err=%v", found, err)
	}
}

func TestAbandonChecklistRunHandler_UnknownRunReturns404(t *testing.T) {
	withTestDocumentStore(t)
	c, rec := newRunEchoContext(http.MethodDelete, "/api/checklist-runs/does-not-exist", "", "does-not-exist")
	if err := abandonChecklistRunHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func jsonInt(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
