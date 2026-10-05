package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

func summaryTestDraft() assistantSummaryDraft {
	return assistantSummaryDraft{
		Title:       "Reverso polisher sampling",
		Established: []string{"The Reverso discharge manifold has no sampling port."},
		RuledOut: []assistantSummaryRuledOut{
			{Option: "Drain the port Racor bowl", Why: "It would let air into the port engine's supply."},
		},
		Procedure: []string{"Close the supply valve.", "Open the inspection cap."},
		Equipment: []string{"Reverso fuel polisher"},
	}
}

func TestRenderAssistantSummaryNote_AllSectionsAndBackLink(t *testing.T) {
	when := time.Date(2026, 10, 5, 3, 0, 0, 0, time.UTC)
	body, ok := renderAssistantSummaryNote(summaryTestDraft(), "Fuel polisher chat", "conv-1", when)
	if !ok {
		t.Fatal("expected a note")
	}
	for _, want := range []string{
		"## What we established\n\n- The Reverso discharge manifold has no sampling port.",
		"## Ruled out\n\n- Drain the port Racor bowl: It would let air into the port engine's supply.",
		"## Procedure that worked\n\n1. Close the supply valve.\n2. Open the inspection cap.",
		"From the Mate conversation [Fuel polisher chat](/mate/conv-1), 2026-10-05.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q:\n%s", want, body)
		}
	}
	if !strings.HasSuffix(strings.TrimSpace(body), "2026-10-05.") {
		t.Errorf("back-link should end the note:\n%s", body)
	}
}

func TestRenderAssistantSummaryNote_OmitsEmptySectionsAndRefusesAllEmpty(t *testing.T) {
	d := assistantSummaryDraft{Title: "t", Established: []string{"A fact."}}
	body, ok := renderAssistantSummaryNote(d, "c", "id", time.Now())
	if !ok {
		t.Fatal("expected a note")
	}
	if strings.Contains(body, "## Ruled out") || strings.Contains(body, "## Procedure that worked") {
		t.Errorf("empty sections must be omitted:\n%s", body)
	}
	if _, ok := renderAssistantSummaryNote(assistantSummaryDraft{Title: "t", Established: []string{"  "}}, "c", "id", time.Now()); ok {
		t.Error("an all-blank draft must not render")
	}
}

func TestAssistantSummaryNoteType(t *testing.T) {
	if got := assistantSummaryNoteType(summaryTestDraft()); got != noteTypeQuirk {
		t.Errorf("mixed draft: got %q, want quirk", got)
	}
	d := assistantSummaryDraft{Established: []string{"a"}, Procedure: []string{"1", "2", "3", "4"}}
	if got := assistantSummaryNoteType(d); got != noteTypeProcedure {
		t.Errorf("procedure-heavy draft: got %q, want procedure", got)
	}
}

func TestCreateAssistantSchema_AddsSummaryNoteColumnToExistingDB(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "old.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE conversations (id TEXT PRIMARY KEY, title TEXT NOT NULL, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if err := createAssistantSchema(db); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if err := createAssistantSchema(db); err != nil {
		t.Fatalf("second run must be idempotent: %v", err)
	}
	if _, err := db.Exec(`SELECT summary_note_id FROM conversations`); err != nil {
		t.Fatalf("column missing: %v", err)
	}
}

func TestAssistantStore_SetSummaryNoteSetsAndClears(t *testing.T) {
	store := newTestAssistantStore(t)
	conv, _ := store.CreateConversation("c")
	if err := store.SetSummaryNote(conv.ID, "note-1"); err != nil {
		t.Fatal(err)
	}
	got, _, _ := store.GetConversation(conv.ID)
	if got.SummaryNoteID == nil || *got.SummaryNoteID != "note-1" {
		t.Fatalf("not set: %+v", got.SummaryNoteID)
	}
	if err := store.SetSummaryNote(conv.ID, ""); err != nil {
		t.Fatal(err)
	}
	got, _, _ = store.GetConversation(conv.ID)
	if got.SummaryNoteID != nil {
		t.Fatalf("not cleared: %v", *got.SummaryNoteID)
	}
	if err := store.SetSummaryNote("missing", "x"); err == nil {
		t.Fatal("expected not-found error")
	}
}

// summaryHarness wires a ready assistant, document store and a scripted
// OpenRouter doer.
type summaryHarness struct {
	store *assistantStore
	docs  *documentStore
	doer  *fakeOpenRouterDoer
	conv  assistantConversation
}

func newSummaryHarness(t *testing.T, modelReplies ...string) *summaryHarness {
	t.Helper()
	docs := withTestDocumentStore(t)
	store := withTestAssistantStore(t)
	secrets := withTestSecretsStore(t)
	if err := secrets.Set("OPENROUTER_API_KEY", "sk-test"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SETTINGS_FILE", writeAssistantSettingsFixture(t, true, "openai/gpt-4o"))

	doer := &fakeOpenRouterDoer{}
	for _, r := range modelReplies {
		payload, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": r}}}})
		doer.responses = append(doer.responses, openRouterFakeResponse(http.StatusOK, string(payload)))
	}
	prev := assistantOpenRouterDoer
	assistantOpenRouterDoer = doer
	t.Cleanup(func() { assistantOpenRouterDoer = prev })

	conv, err := store.CreateConversation("Fuel polisher chat")
	if err != nil {
		t.Fatal(err)
	}
	return &summaryHarness{store: store, docs: docs, doer: doer, conv: conv}
}

func (h *summaryHarness) say(t *testing.T, role, content string, atts ...assistantAttachment) {
	t.Helper()
	if _, err := h.store.AppendMessage(assistantMessage{ConversationID: h.conv.ID, Role: role, Content: content, Attachments: atts}); err != nil {
		t.Fatal(err)
	}
}

func (h *summaryHarness) draft(t *testing.T) (int, map[string]any) {
	t.Helper()
	c, rec := newAssistantEchoContext(http.MethodPost, "/api/assistant/conversations/"+h.conv.ID+"/summary-draft", "", h.conv.ID)
	if err := postAssistantSummaryDraftHandler(c); err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

const summaryModelJSON = `{"title":"Reverso polisher sampling","established":["The discharge manifold has no sampling port."],"ruled_out":[{"option":"Drain the port Racor bowl","why":"Air into the supply."}],"procedure":[],"equipment":["Reverso fuel polisher","Nonexistent gadget"]}`

func TestSummaryDraft_BuildsNoteWithEquipmentAndAttachmentSummaries(t *testing.T) {
	h := newSummaryHarness(t, summaryModelJSON)
	polisher := mustToolEquipment(t, h.docs, equipmentItem{Name: "Reverso fuel polisher"})
	photo, err := h.docs.Insert(document{SHA256: "sha-photo", Filename: "manifold.jpg", MIME: "image/jpeg"})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.docs.SetSuggested(photo.ID, "Discharge manifold", "Plain brass manifold, no drain cock visible.", nil); err != nil {
		t.Fatal(err)
	}
	h.say(t, "user", "Is there a sampling port?", assistantAttachment{DocumentID: photo.ID, Filename: "manifold.jpg"})
	h.say(t, "assistant", "Try the manifold port.")
	h.say(t, "watch", "WATCH-ROW-MARKER")

	code, out := h.draft(t)
	if code != http.StatusOK {
		t.Fatalf("status %d: %v", code, out)
	}
	if out["type"] != "quirk" {
		t.Errorf("type = %v", out["type"])
	}
	body, _ := out["body"].(string)
	if !strings.Contains(body, "## What we established") || !strings.Contains(body, "/mate/"+h.conv.ID) {
		t.Errorf("body: %s", body)
	}
	eq, _ := out["equipment"].([]any)
	if len(eq) != 1 || eq[0].(map[string]any)["id"] != polisher.ID {
		t.Errorf("equipment suggestions = %v (only real matches)", out["equipment"])
	}
	if _, has := out["existing_note_id"]; has {
		t.Errorf("no existing note expected")
	}

	sent := string(h.doer.bodies[0])
	for _, want := range []string{"manifold.jpg", "Plain brass manifold, no drain cock visible.", "Is there a sampling port?", "Try the manifold port."} {
		if !strings.Contains(sent, want) {
			t.Errorf("model input missing %q", want)
		}
	}
	if strings.Contains(sent, "WATCH-ROW-MARKER") {
		t.Error("watch rows must not reach the model")
	}
}

func TestSummaryDraft_NothingToKeepIs422(t *testing.T) {
	h := newSummaryHarness(t, `{"title":"x","established":[],"ruled_out":[],"procedure":[],"equipment":[]}`)
	h.say(t, "user", "hi")
	h.say(t, "assistant", "hello")
	code, out := h.draft(t)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d: %v", code, out)
	}
	if out["error"] != "Nothing in this conversation to keep yet." {
		t.Errorf("error = %v", out["error"])
	}
}

func TestSummaryDraft_NoAssistantReplyIs422WithoutCallingTheModel(t *testing.T) {
	h := newSummaryHarness(t)
	h.say(t, "user", "hi")
	code, _ := h.draft(t)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d", code)
	}
	if h.doer.calls != 0 {
		t.Errorf("model was called %d times", h.doer.calls)
	}
}

func TestSummaryDraft_ModelFailuresAre502WithTheReason(t *testing.T) {
	h := newSummaryHarness(t, `not json at all`)
	h.say(t, "user", "hi")
	h.say(t, "assistant", "hello")
	code, out := h.draft(t)
	if code != http.StatusBadGateway {
		t.Fatalf("unparseable: status %d", code)
	}
	if msg, _ := out["error"].(string); !strings.Contains(msg, "not json at all") {
		t.Errorf("reason missing: %v", out["error"])
	}

	h2 := newSummaryHarness(t)
	h2.doer.responses = []*http.Response{openRouterFakeResponse(http.StatusInternalServerError, `{"error":{"message":"upstream down"}}`)}
	h2.say(t, "user", "hi")
	h2.say(t, "assistant", "hello")
	code, out = h2.draft(t)
	if code != http.StatusBadGateway {
		t.Fatalf("upstream: status %d", code)
	}
	if msg, _ := out["error"].(string); !strings.Contains(msg, "upstream down") {
		t.Errorf("reason missing: %v", out["error"])
	}
}

func TestSummaryDraft_ExistingNoteReportedAndDeletedNoteTreatedAsNone(t *testing.T) {
	h := newSummaryHarness(t, summaryModelJSON, summaryModelJSON)
	h.say(t, "user", "hi")
	h.say(t, "assistant", "hello")
	note := mustCreateTestNote(t, "# Old\n\nbody", "Old")
	if err := h.store.SetSummaryNote(h.conv.ID, note.ID); err != nil {
		t.Fatal(err)
	}
	_, out := h.draft(t)
	if out["existing_note_id"] != note.ID {
		t.Errorf("existing_note_id = %v", out["existing_note_id"])
	}

	if _, err := h.docs.Delete(note.ID); err != nil {
		t.Fatal(err)
	}
	_, out = h.draft(t)
	if _, has := out["existing_note_id"]; has {
		t.Errorf("a deleted note must read as none, got %v", out["existing_note_id"])
	}

	c, rec := newAssistantEchoContext(http.MethodGet, "/api/assistant/conversations/"+h.conv.ID, "", h.conv.ID)
	if err := getAssistantConversationHandler(c); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rec.Body.String(), "summary_note_id") {
		t.Errorf("GET must not report a deleted note: %s", rec.Body.String())
	}
}

func (h *summaryHarness) save(t *testing.T, body string) (int, map[string]any) {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues(h.conv.ID)
	if err := saveAssistantSummaryNoteHandler(c); err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestSummarySave_CreatesThenUpdatesTheSameNoteAndAdjustsLinks(t *testing.T) {
	h := newSummaryHarness(t)
	a := mustToolEquipment(t, h.docs, equipmentItem{Name: "Reverso"})
	b := mustToolEquipment(t, h.docs, equipmentItem{Name: "Racor"})

	code, out := h.save(t, `{"title":"First","body":"## What we established\n\n- one","type":"quirk","add_equipment_ids":["`+a.ID+`","`+b.ID+`"]}`)
	if code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("create: %d %v", code, out)
	}
	noteID, _ := out["note_id"].(string)
	if noteID == "" || out["created"] != true {
		t.Fatalf("create response: %v", out)
	}
	conv, _, _ := h.store.GetConversation(h.conv.ID)
	if conv.SummaryNoteID == nil || *conv.SummaryNoteID != noteID {
		t.Fatalf("conversation not linked to note")
	}
	docsA, _ := h.docs.EquipmentDocuments(a.ID)
	if len(docsA) != 1 {
		t.Fatalf("link to a missing: %v", docsA)
	}

	code, out = h.save(t, `{"title":"Second","body":"## What we established\n\n- two","remove_equipment_ids":["`+b.ID+`"]}`)
	if code != http.StatusOK {
		t.Fatalf("update: %d %v", code, out)
	}
	if out["note_id"] != noteID || out["created"] != false {
		t.Fatalf("update must reuse the note: %v", out)
	}
	notes, _ := h.docs.ListNotes(false, "", "", 50, 0)
	if len(notes) != 1 || notes[0].Title != "Second" {
		t.Fatalf("expected one replaced note, got %d (%+v)", len(notes), notes)
	}
	docsB, _ := h.docs.EquipmentDocuments(b.ID)
	docsA, _ = h.docs.EquipmentDocuments(a.ID)
	if len(docsB) != 0 || len(docsA) != 1 {
		t.Fatalf("links after update: a=%d b=%d", len(docsA), len(docsB))
	}
}

func TestSummarySave_AfterTheNoteWasDeletedCreatesAFreshOne(t *testing.T) {
	h := newSummaryHarness(t)
	_, out := h.save(t, `{"title":"First","body":"- one"}`)
	first := out["note_id"].(string)
	if _, err := h.docs.Delete(first); err != nil {
		t.Fatal(err)
	}
	_, out = h.save(t, `{"title":"Again","body":"- one"}`)
	if out["created"] != true || out["note_id"] == first {
		t.Fatalf("expected a new note: %v", out)
	}
}

func TestSummarySave_RejectsEmptyBodyAndUnknownEquipment(t *testing.T) {
	h := newSummaryHarness(t)
	if code, _ := h.save(t, `{"title":"x","body":"  "}`); code != http.StatusBadRequest {
		t.Errorf("empty body: %d", code)
	}
	if code, _ := h.save(t, `{"title":"x","body":"- a","add_equipment_ids":["nope"]}`); code != http.StatusNotFound {
		t.Errorf("unknown equipment: %d", code)
	}
}

func TestResolveSummaryEquipment_ExactBeatsAmbiguousAndDedupes(t *testing.T) {
	docs := newTestDocumentStore(t)
	mustToolEquipment(t, docs, equipmentItem{Name: "Racor port"})
	mustToolEquipment(t, docs, equipmentItem{Name: "Racor starboard"})
	exact := mustToolEquipment(t, docs, equipmentItem{Name: "Reverso fuel polisher", Manufacturer: "Reverso"})
	got, err := resolveSummaryEquipment(docs, []string{"reverso fuel polisher", "Reverso Fuel Polisher", "Racor", "Ghost"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != exact.ID {
		t.Fatalf("got %+v: exact match once; ambiguous and unknown names dropped", got)
	}
}
