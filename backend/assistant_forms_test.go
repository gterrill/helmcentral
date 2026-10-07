package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// inspect_form, fill_form and the form draft card's routes (ADR 0164).

type formEnv struct {
	docs *documentStore
	asst *assistantStore
	conv assistantConversation
	deps assistantToolDeps
}

func newFormEnv(t *testing.T) *formEnv {
	t.Helper()
	docs := withTestDocumentStore(t)
	asst := withTestAssistantStore(t)
	conv, err := asst.CreateConversation("Forms")
	if err != nil {
		t.Fatal(err)
	}
	return &formEnv{docs: docs, asst: asst, conv: conv, deps: assistantToolDeps{
		documents:     func() *documentStore { return docs },
		conversations: func() *assistantStore { return asst },
	}}
}

// addDocument stores bytes as a document, the way an upload does.
func (e *formEnv) addDocument(t *testing.T, filename, mime string, data []byte) document {
	t.Helper()
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	dir := documentsDirPath()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, sha), data, 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := e.docs.Insert(document{SHA256: sha, Filename: filename, MIME: mime, SizeBytes: int64(len(data))})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	return doc
}

func (e *formEnv) tool(t *testing.T, name, args string) (string, error) {
	t.Helper()
	return runRecordTool(t, e.deps, name, args)
}

func (e *formEnv) draftCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := e.asst.db.QueryRow(`SELECT COUNT(*) FROM assistant_form_drafts`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func decodeInspect(t *testing.T, raw string) assistantInspectFormResult {
	t.Helper()
	var res assistantInspectFormResult
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, raw)
	}
	return res
}

func TestInspectForm_FlatFormReturnsLinesAndLabelledBoxes(t *testing.T) {
	e := newFormEnv(t)
	doc := e.addDocument(t, "storm.pdf", "application/pdf", flatFormFixture(t))
	raw, err := e.tool(t, "inspect_form", fmt.Sprintf(`{"document_id":%q}`, doc.ID))
	if err != nil {
		t.Fatalf("inspect_form: %v", err)
	}
	res := decodeInspect(t, raw)
	if res.Kind != "flat" || res.PageCount != 1 || len(res.Pages) != 1 || len(res.Fields) != 0 {
		t.Fatalf("expected one flat page, got %s", raw)
	}
	p := res.Pages[0]
	if _, ok := p.line("Policy number"); !ok {
		t.Errorf("expected the Policy number line, got %+v", p.Lines)
	}
	if b, ok := p.box("North Harbour"); !ok || b.W < 8 {
		t.Errorf("expected the North Harbour box, got %+v", p.Boxes)
	}
	if strings.Contains(raw, "No tick boxes were detected") {
		t.Errorf("boxes were found, so no warning is due")
	}
}

func TestInspectForm_SaysSoWhenNoBoxesWereFound(t *testing.T) {
	e := newFormEnv(t)
	doc := e.addDocument(t, "plain.pdf", "application/pdf", createPDF(t, `{
 "paper": "A4P", "origin": "LowerLeft",
 "pages": {"1": {"content": {"text": [{"value": "Name", "pos": [50, 700], "font": {"name": "Helvetica", "size": 11}}]}}}}`))
	raw, err := e.tool(t, "inspect_form", fmt.Sprintf(`{"document_id":%q}`, doc.ID))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, "No tick boxes were detected") {
		t.Fatalf("expected the no-boxes note, got %s", raw)
	}
}

func TestInspectForm_AcroFormReturnsFields(t *testing.T) {
	e := newFormEnv(t)
	doc := e.addDocument(t, "berth.pdf", "application/pdf", acroFormFixture(t))
	raw, err := e.tool(t, "inspect_form", fmt.Sprintf(`{"document_id":%q}`, doc.ID))
	if err != nil {
		t.Fatalf("inspect_form: %v", err)
	}
	res := decodeInspect(t, raw)
	if res.Kind != "fields" || len(res.Pages) != 0 || res.PageCount != 1 {
		t.Fatalf("expected fields and no layout, got %s", raw)
	}
	if f, ok := fieldNamed(res.Fields, "berth_type"); !ok || f.Kind != "radio" || len(f.Options) != 2 {
		t.Fatalf("expected the radio group, got %+v", res.Fields)
	}
}

func TestInspectForm_RefusesWhatIsNotAPDF(t *testing.T) {
	e := newFormEnv(t)
	doc := e.addDocument(t, "notes.txt", "text/plain", []byte("hello"))
	if _, err := e.tool(t, "inspect_form", fmt.Sprintf(`{"document_id":%q}`, doc.ID)); err == nil || !strings.Contains(err.Error(), "not a PDF") {
		t.Fatalf("expected a not-a-PDF error, got %v", err)
	}
	if _, err := e.tool(t, "inspect_form", `{"document_id":"nope"}`); err == nil || !strings.Contains(err.Error(), `unknown document "nope"`) {
		t.Fatalf("expected an unknown document error, got %v", err)
	}
}

func TestInspectForm_ABigFormComesBackAPageAtATime(t *testing.T) {
	e := newFormEnv(t)
	var texts []string
	for i := 0; i < 70; i++ {
		texts = append(texts, fmt.Sprintf(`{"value": "Question number %d about the vessel and its equipment on board", "pos": [30, %d], "font": {"name": "Helvetica", "size": 9}}`, i, 800-i*11))
	}
	page := `{"content": {"text": [` + strings.Join(texts, ",") + `]}}`
	doc := e.addDocument(t, "big.pdf", "application/pdf", createPDF(t, `{"paper": "A4P", "origin": "LowerLeft", "pages": {"1": `+page+`, "2": `+page+`}}`))

	raw, err := e.tool(t, "inspect_form", fmt.Sprintf(`{"document_id":%q}`, doc.ID))
	if err != nil {
		t.Fatal(err)
	}
	res := decodeInspect(t, raw)
	if !res.Truncated || res.NextPage != 2 || len(res.Pages) != 1 || res.Pages[0].Page != 1 || res.PageCount != 2 {
		t.Fatalf("expected page 1 with next_page 2, got truncated=%v next=%d pages=%d", res.Truncated, res.NextPage, len(res.Pages))
	}
	raw, err = e.tool(t, "inspect_form", fmt.Sprintf(`{"document_id":%q,"page":2}`, doc.ID))
	if err != nil {
		t.Fatal(err)
	}
	if res = decodeInspect(t, raw); len(res.Pages) != 1 || res.Pages[0].Page != 2 {
		t.Fatalf("expected page 2, got %s", raw[:200])
	}
	if _, err := e.tool(t, "inspect_form", fmt.Sprintf(`{"document_id":%q,"page":9}`, doc.ID)); err == nil {
		t.Fatal("a page that does not exist is an error")
	}
}

func fillFormArgs(docID, entries string) string {
	return fmt.Sprintf(`{"document_id":%q,"entries":%s,"title":"Storm declaration 2026","folder":"Insurance/2026"}`, docID, entries)
}

func TestFillForm_MakesADraftAndNothingElse(t *testing.T) {
	e := newFormEnv(t)
	doc := e.addDocument(t, "storm.pdf", "application/pdf", flatFormFixture(t))
	before, _ := e.docs.Count()

	raw, err := e.tool(t, "fill_form", fillFormArgs(doc.ID, `[{"page":1,"anchor":"Vessel owner","value":"Sam Example"},{"page":1,"box":"North Harbour"}]`))
	if err != nil {
		t.Fatalf("fill_form: %v", err)
	}
	draft, err := assistantFormDraftFromToolResult(raw)
	if err != nil || draft == nil {
		t.Fatalf("expected a draft in the result, got %v %s", err, raw)
	}
	if draft.SourceDocumentID != doc.ID || draft.Title != "Storm declaration 2026" || draft.Folder != "Insurance/2026" ||
		draft.PageCount != 1 || draft.Status != formDraftPending || draft.Filename != "storm (filled in).pdf" {
		t.Fatalf("unexpected draft %+v", draft)
	}
	if !strings.Contains(raw, "never say it is saved") && !strings.Contains(raw, "Never say it is saved") {
		t.Errorf("the result tells Mate the draft is not saved, got %s", raw)
	}
	after, _ := e.docs.Count()
	if after != before {
		t.Fatalf("a draft is not a document: %d documents before, %d after", before, after)
	}
	_, content, err := e.asst.FormDraftContent(draft.ID)
	if err != nil || !bytes.HasPrefix(content, []byte("%PDF")) {
		t.Fatalf("expected the filled PDF bytes, got %v", err)
	}
	l, err := analysePDFLayout(writeFixturePDF(t, "d.pdf", content))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ln := range l.Pages[0].Lines {
		if strings.Contains(ln.Text, "Sam Example") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the draft carries the filled text, got %+v", l.Pages[0].Lines)
	}
}

func TestFillForm_DefaultsTitleAndFolderFromTheSource(t *testing.T) {
	e := newFormEnv(t)
	folder, err := e.docs.CreateFolder("Insurance", nil)
	if err != nil {
		t.Fatal(err)
	}
	data := flatFormFixture(t)
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	os.MkdirAll(documentsDirPath(), 0o755)
	os.WriteFile(filepath.Join(documentsDirPath(), sha), data, 0o644)
	doc, err := e.docs.Insert(document{SHA256: sha, Filename: "storm.pdf", Title: "Storm form", MIME: "application/pdf", FolderID: &folder.ID})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := e.tool(t, "fill_form", fmt.Sprintf(`{"document_id":%q,"entries":[{"page":1,"anchor":"Vessel owner","value":"Sam"}]}`, doc.ID))
	if err != nil {
		t.Fatal(err)
	}
	draft, _ := assistantFormDraftFromToolResult(raw)
	if draft.Title != "Storm form (filled in)" || draft.Folder != "Insurance" {
		t.Fatalf("expected the source's title and folder, got %+v", draft)
	}
}

func TestFillForm_ABadEntryMakesNoDraft(t *testing.T) {
	e := newFormEnv(t)
	doc := e.addDocument(t, "storm.pdf", "application/pdf", flatFormFixture(t))
	_, err := e.tool(t, "fill_form", fillFormArgs(doc.ID, `[{"page":1,"anchor":"Vessel owner","value":"Sam"},{"page":1,"anchor":"Owner","value":"x"}]`))
	if err == nil || !strings.Contains(err.Error(), `anchor "Owner" on page 1: not found`) {
		t.Fatalf("expected the bad anchor to be named, got %v", err)
	}
	if n := e.draftCount(t); n != 0 {
		t.Fatalf("a failed fill leaves no draft, got %d", n)
	}
	if _, err := e.tool(t, "fill_form", fillFormArgs(doc.ID, `[{"page":1,"anchor":"Signed","value":"Sam"}]`)); err == nil || !strings.Contains(err.Error(), "signatures are left for the operator") {
		t.Fatalf("a signature is refused, got %v", err)
	}
}

func TestFormDraftRidesWithTheAssistantMessage(t *testing.T) {
	e := newFormEnv(t)
	doc := e.addDocument(t, "storm.pdf", "application/pdf", flatFormFixture(t))
	raw, _ := e.tool(t, "fill_form", fillFormArgs(doc.ID, `[{"page":1,"box":"South Cove"}]`))
	draft, _ := assistantFormDraftFromToolResult(raw)

	if _, err := e.asst.AppendMessage(assistantMessage{ConversationID: e.conv.ID, Role: "user", Content: "hi", FormDrafts: []assistantFormDraft{*draft}}); err == nil {
		t.Fatal("only an assistant message carries a draft")
	}
	msg, err := e.asst.AppendMessage(assistantMessage{ConversationID: e.conv.ID, Role: "assistant", Content: "Done.", FormDrafts: []assistantFormDraft{*draft}})
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	msgs, err := e.asst.ListMessages(e.conv.ID)
	if err != nil || len(msgs) != 1 || len(msgs[0].FormDrafts) != 1 || msgs[0].FormDrafts[0].ID != draft.ID || msgs[0].FormDrafts[0].MessageID != msg.ID {
		t.Fatalf("expected the draft on its message, got %v %+v", err, msgs)
	}
	// The same draft cannot ride on a second message.
	if _, err := e.asst.AppendMessage(assistantMessage{ConversationID: e.conv.ID, Role: "assistant", Content: "Again.", FormDrafts: []assistantFormDraft{*draft}}); err == nil {
		t.Fatal("a draft belongs to one message")
	}
	// What became of it is told to Mate on a later turn.
	if got := formDraftHistoryBlock(msgs[0].FormDrafts[0]); !strings.Contains(got, "still waiting") {
		t.Errorf("history block: %s", got)
	}
	// Deleting the conversation takes its drafts with it.
	if err := e.asst.DeleteConversation(e.conv.ID); err != nil {
		t.Fatal(err)
	}
	if n := e.draftCount(t); n != 0 {
		t.Fatalf("expected the draft to go with its conversation, got %d", n)
	}
}

func TestFormDraft_OrphansAreSwept(t *testing.T) {
	e := newFormEnv(t)
	doc := e.addDocument(t, "storm.pdf", "application/pdf", flatFormFixture(t))
	e.tool(t, "fill_form", fillFormArgs(doc.ID, `[{"page":1,"box":"South Cove"}]`))
	if e.draftCount(t) != 1 {
		t.Fatal("expected one draft")
	}
	e.asst.now = func() time.Time { return time.Now().Add(48 * time.Hour) }
	e.tool(t, "fill_form", fillFormArgs(doc.ID, `[{"page":1,"box":"North Harbour"}]`))
	if n := e.draftCount(t); n != 1 {
		t.Fatalf("the unattached draft from two days ago is swept, got %d drafts", n)
	}
}

func savedDraft(t *testing.T, e *formEnv) assistantFormDraft {
	t.Helper()
	doc := e.addDocument(t, "storm.pdf", "application/pdf", flatFormFixture(t))
	raw, err := e.tool(t, "fill_form", fillFormArgs(doc.ID, `[{"page":1,"anchor":"Vessel owner","value":"Sam Example"}]`))
	if err != nil {
		t.Fatal(err)
	}
	draft, _ := assistantFormDraftFromToolResult(raw)
	if _, err := e.asst.AppendMessage(assistantMessage{ConversationID: e.conv.ID, Role: "assistant", Content: "Done.", FormDrafts: []assistantFormDraft{*draft}}); err != nil {
		t.Fatal(err)
	}
	return *draft
}

func TestSaveFormDraft_CreatesExactlyOneDocumentAndSavingTwiceReturnsIt(t *testing.T) {
	e := newFormEnv(t)
	draft := savedDraft(t, e)
	before, _ := e.docs.Count()

	res, err := saveFormDraft(e.asst, e.docs, draft.ID, "Storm declaration 2026", "Insurance/2026")
	if err != nil {
		t.Fatalf("saveFormDraft: %v", err)
	}
	doc := res.Document
	if doc.Title != "Storm declaration 2026" || doc.MIME != "application/pdf" || doc.Filename != "storm (filled in).pdf" || doc.FolderID == nil {
		t.Fatalf("unexpected document %+v", doc)
	}
	chain, _ := e.docs.FolderPath(*doc.FolderID)
	if len(chain) != 2 || chain[0].Name != "Insurance" || chain[1].Name != "2026" {
		t.Fatalf("the folder path is created, got %+v", chain)
	}
	if _, err := os.Stat(filepath.Join(documentsDirPath(), doc.SHA256)); err != nil {
		t.Fatalf("the file is stored like an upload: %v", err)
	}
	if after, _ := e.docs.Count(); after != before+1 {
		t.Fatalf("exactly one document is made: %d before, %d after", before, after)
	}

	again, err := saveFormDraft(e.asst, e.docs, draft.ID, "A different title", "Elsewhere")
	if err != nil || again.Document.ID != doc.ID {
		t.Fatalf("saving twice returns the same document, got %v %+v", err, again.Document)
	}
	if after, _ := e.docs.Count(); after != before+1 {
		t.Fatalf("saving twice makes no second document, got %d", after)
	}
	got, _ := e.asst.GetFormDraft(draft.ID)
	if got.Status != formDraftSaved || got.DocumentID != doc.ID || got.Title != "Storm declaration 2026" || got.Folder != "Insurance/2026" {
		t.Fatalf("the draft records its document, got %+v", got)
	}
	if _, err := e.asst.DismissFormDraft(draft.ID); err == nil {
		t.Fatal("a saved draft cannot be dismissed")
	}
}

func TestSaveFormDraft_NeedsATitleAndRefusesADismissedDraft(t *testing.T) {
	e := newFormEnv(t)
	draft := savedDraft(t, e)
	if _, err := saveFormDraft(e.asst, e.docs, draft.ID, "   ", ""); err != errFormDraftTitle {
		t.Fatalf("expected a title error, got %v", err)
	}
	if _, err := saveFormDraft(e.asst, e.docs, "nope", "T", ""); err != errFormDraftNotFound {
		t.Fatalf("expected not found, got %v", err)
	}
	if _, err := e.asst.DismissFormDraft(draft.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.asst.DismissFormDraft(draft.ID); err != nil {
		t.Fatalf("dismissing twice is fine, got %v", err)
	}
	if _, err := saveFormDraft(e.asst, e.docs, draft.ID, "T", ""); err != errFormDraftDismissed {
		t.Fatalf("a dismissed draft cannot be saved, got %v", err)
	}
	if _, _, err := e.asst.FormDraftContent(draft.ID); err != errFormDraftDismissed {
		t.Fatalf("a dismissed draft's bytes are gone, got %v", err)
	}
}

func formDraftRequest(method, path, body string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	return e.NewContext(req, rec), rec
}

func TestFormDraftRoutes_ContentSaveAndDismiss(t *testing.T) {
	e := newFormEnv(t)
	draft := savedDraft(t, e)

	c, rec := formDraftRequest(http.MethodGet, "/", "")
	c.SetParamNames("id")
	c.SetParamValues(draft.ID)
	if err := formDraftContentHandler(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "application/pdf" || !bytes.HasPrefix(rec.Body.Bytes(), []byte("%PDF")) ||
		!strings.HasPrefix(rec.Header().Get("Content-Disposition"), "inline") {
		t.Fatalf("expected an inline PDF, got %d %s %q", rec.Code, rec.Header().Get("Content-Type"), rec.Header().Get("Content-Disposition"))
	}

	c, rec = formDraftRequest(http.MethodPost, "/", `{"title":" ","folder":""}`)
	c.SetParamNames("id")
	c.SetParamValues(draft.ID)
	saveFormDraftHandler(c)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a blank title is a 400, got %d %s", rec.Code, rec.Body.String())
	}

	c, rec = formDraftRequest(http.MethodPost, "/", `{"title":"Storm declaration","folder":"Insurance/2026"}`)
	c.SetParamNames("id")
	c.SetParamValues(draft.ID)
	saveFormDraftHandler(c)
	var saved struct {
		Draft    assistantFormDraft `json:"draft"`
		Document documentJSON       `json:"document"`
	}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &saved) != nil || saved.Document.ID == "" || saved.Draft.Status != formDraftSaved {
		t.Fatalf("expected the saved document, got %d %s", rec.Code, rec.Body.String())
	}

	c, rec = formDraftRequest(http.MethodPost, "/", "")
	c.SetParamNames("id")
	c.SetParamValues(draft.ID)
	dismissFormDraftHandler(c)
	if rec.Code != http.StatusConflict {
		t.Fatalf("dismissing a saved draft is a 409, got %d", rec.Code)
	}

	c, rec = formDraftRequest(http.MethodGet, "/", "")
	c.SetParamNames("id")
	c.SetParamValues("nope")
	formDraftContentHandler(c)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("an unknown draft is a 404, got %d", rec.Code)
	}
}

func TestInspectForm_AFormWithManyFieldsPagesByOffsetAndNamesAlwaysComeBack(t *testing.T) {
	e := newFormEnv(t)
	var fields []string
	for i := 0; i < 260; i++ {
		fields = append(fields, fmt.Sprintf(`{"id": "applicant_declaration_field_%03d", "pos": [50, %d], "width": 100, "height": 12, "font": {"name": "Helvetica", "size": 8}}`, i, 20+(i%70)*10))
	}
	doc := e.addDocument(t, "many.pdf", "application/pdf", createPDF(t, `{"paper": "A4P", "origin": "LowerLeft", "pages": {"1": {"content": {"textfield": [`+strings.Join(fields, ",")+`]}}}}`))

	seen := map[string]bool{}
	offset, calls := 0, 0
	for {
		raw, err := e.tool(t, "inspect_form", fmt.Sprintf(`{"document_id":%q,"offset":%d}`, doc.ID, offset))
		if err != nil {
			t.Fatal(err)
		}
		res := decodeInspect(t, raw)
		if len(res.Fields) == 0 {
			t.Fatalf("names must always come back, got %.200s", raw)
		}
		for _, f := range res.Fields {
			seen[f.Name] = true
		}
		calls++
		if !res.Truncated {
			break
		}
		if res.NextOffset <= offset {
			t.Fatalf("next_offset must advance, got %d after %d", res.NextOffset, offset)
		}
		offset = res.NextOffset
	}
	if calls < 2 || len(seen) != 260 {
		t.Fatalf("expected all 260 field names over several calls, got %d names in %d calls", len(seen), calls)
	}
}

func TestInspectForm_AnOversizedFlatPageCanBeReadOnByOffsetAndSaysSoOnce(t *testing.T) {
	e := newFormEnv(t)
	var texts []string
	for i := 0; i < 140; i++ {
		texts = append(texts, fmt.Sprintf(`{"value": "Question number %03d about the vessel and its equipment on board", "pos": [30, %d], "font": {"name": "Helvetica", "size": 5}}`, i, 830-i*5))
	}
	doc := e.addDocument(t, "dense.pdf", "application/pdf", createPDF(t, `{"paper": "A4P", "origin": "LowerLeft", "pages": {"1": {"content": {"text": [`+strings.Join(texts, ",")+`]}}}}`))

	seen := map[string]bool{}
	offset := 0
	for calls := 0; ; calls++ {
		if calls > 20 {
			t.Fatal("paging does not finish")
		}
		raw, err := e.tool(t, "inspect_form", fmt.Sprintf(`{"document_id":%q,"page":1,"offset":%d}`, doc.ID, offset))
		if err != nil {
			t.Fatal(err)
		}
		res := decodeInspect(t, raw)
		if n := strings.Count(strings.Join(res.Notes, "\n"), "cut to fit"); n > 1 {
			t.Fatalf("the cut note is given once, got %d times", n)
		}
		for _, l := range res.Pages[0].Lines {
			seen[l.Text] = true
		}
		if !res.Truncated {
			break
		}
		if res.NextOffset <= offset || res.NextPage != 1 {
			t.Fatalf("expected next_page 1 and an advancing next_offset, got %d/%d after %d", res.NextPage, res.NextOffset, offset)
		}
		offset = res.NextOffset
	}
	if len(seen) != 140 {
		t.Fatalf("every line is reachable, got %d of 140", len(seen))
	}
	if _, err := e.tool(t, "inspect_form", fmt.Sprintf(`{"document_id":%q,"offset":3}`, doc.ID)); err == nil {
		t.Fatal("an offset on a flat form needs a page")
	}
}

func TestFormDraft_MarkSavedIsAConditionalTransition(t *testing.T) {
	e := newFormEnv(t)
	draft := savedDraft(t, e)
	if _, err := e.asst.DismissFormDraft(draft.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.asst.markFormDraftSaved(draft.ID, "doc1", "T", ""); err != errFormDraftDismissed {
		t.Fatalf("a dismissed draft cannot become saved, got %v", err)
	}
	if got, _ := e.asst.GetFormDraft(draft.ID); got.Status != formDraftDismissed || got.DocumentID != "" {
		t.Fatalf("the draft stays dismissed, got %+v", got)
	}
}

func TestFormDraft_SaveAndDismissRacingLeaveOneConsistentOutcome(t *testing.T) {
	for i := 0; i < 15; i++ {
		e := newFormEnv(t)
		draft := savedDraft(t, e)
		before, _ := e.docs.Count()
		var wg sync.WaitGroup
		var saveErr, dismissErr error
		wg.Add(2)
		go func() { defer wg.Done(); _, saveErr = saveFormDraft(e.asst, e.docs, draft.ID, "T", "") }()
		go func() { defer wg.Done(); _, dismissErr = dismissFormDraftExclusive(e.asst, draft.ID) }()
		wg.Wait()

		got, _ := e.asst.GetFormDraft(draft.ID)
		after, _ := e.docs.Count()
		switch got.Status {
		case formDraftSaved:
			if saveErr != nil || dismissErr != errFormDraftSaved || got.DocumentID == "" || after != before+1 {
				t.Fatalf("saved: save=%v dismiss=%v doc=%q docs %d->%d", saveErr, dismissErr, got.DocumentID, before, after)
			}
			if _, content, err := e.asst.FormDraftContent(draft.ID); err != nil || len(content) == 0 {
				t.Fatalf("a saved draft keeps its PDF, got %v %d bytes", err, len(content))
			}
		case formDraftDismissed:
			if dismissErr != nil || saveErr != errFormDraftDismissed || after != before {
				t.Fatalf("dismissed: save=%v dismiss=%v docs %d->%d", saveErr, dismissErr, before, after)
			}
		default:
			t.Fatalf("expected a resolved draft, got %+v", got)
		}
	}
}
