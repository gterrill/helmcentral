package main

import (
	"encoding/json"
	"net/http"
	"testing"
)

// ── GET /api/manuals ─────────────────────────────────────────────────────

// TestListManualsHandler_EmptyReturnsEmptyArrayNot404 pins plan §4: "you
// have not started one" is a normal state, not a 404.
func TestListManualsHandler_EmptyReturnsEmptyArrayNot404(t *testing.T) {
	withTestDocumentStore(t)

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/manuals", "", "")
	if err := listManualsHandler(c); err != nil {
		t.Fatalf("listManualsHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Manuals []manualSummary `json:"manuals"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Manuals == nil {
		t.Fatalf("expected an empty array, got a null manuals field")
	}
	if len(resp.Manuals) != 0 {
		t.Fatalf("expected no manuals, got %+v", resp.Manuals)
	}
}

func TestListManualsHandler_ReturnsSeveral(t *testing.T) {
	withTestDocumentStore(t)

	if _, err := globalDocumentStore.CreateManual("Operations Manual"); err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	if _, err := globalDocumentStore.CreateManual("Crew Training"); err != nil {
		t.Fatalf("CreateManual: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/manuals", "", "")
	if err := listManualsHandler(c); err != nil {
		t.Fatalf("listManualsHandler returned error: %v", err)
	}
	var resp struct {
		Manuals []manualSummary `json:"manuals"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Manuals) != 2 {
		t.Fatalf("expected both manuals, got %+v", resp.Manuals)
	}
}

// ── POST /api/manuals ────────────────────────────────────────────────────

func TestCreateManualHandler_CreatesByName(t *testing.T) {
	withTestDocumentStore(t)

	c, rec := newDocumentEchoContext(http.MethodPost, "/api/manuals", `{"name":"Operations Manual"}`, "")
	if err := createManualHandler(c); err != nil {
		t.Fatalf("createManualHandler returned error: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var manual manualSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &manual); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if manual.Name != "Operations Manual" {
		t.Fatalf("expected the given name, got %q", manual.Name)
	}
}

func TestCreateManualHandler_FlagsExistingFolderByID(t *testing.T) {
	withTestDocumentStore(t)

	folder, err := globalDocumentStore.CreateFolder("Crew Training", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodPost, "/api/manuals", `{"folder_id":"`+folder.ID+`"}`, "")
	if err := createManualHandler(c); err != nil {
		t.Fatalf("createManualHandler returned error: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var manual manualSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &manual); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if manual.ID != folder.ID {
		t.Fatalf("expected the flagged folder's own id, got %q", manual.ID)
	}
}

// TestCreateManualHandler_NonTopLevelFolderReturns409 pins plan §4:
// "409 on a non-top-level folder".
func TestCreateManualHandler_NonTopLevelFolderReturns409(t *testing.T) {
	withTestDocumentStore(t)

	parent, err := globalDocumentStore.CreateFolder("Manuals", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	child, err := globalDocumentStore.CreateFolder("Engine", &parent.ID)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodPost, "/api/manuals", `{"folder_id":"`+child.ID+`"}`, "")
	if err := createManualHandler(c); err != nil {
		t.Fatalf("createManualHandler returned error: %v", err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestCreateManualHandler_DuplicateNameReturns409 pins plan §4: "409 on ...
// a duplicate name".
func TestCreateManualHandler_DuplicateNameReturns409(t *testing.T) {
	withTestDocumentStore(t)

	if _, err := globalDocumentStore.CreateManual("Operations Manual"); err != nil {
		t.Fatalf("CreateManual: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodPost, "/api/manuals", `{"name":"Operations Manual"}`, "")
	if err := createManualHandler(c); err != nil {
		t.Fatalf("createManualHandler returned error: %v", err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateManualHandler_MissingNameAndFolderIDReturns400(t *testing.T) {
	withTestDocumentStore(t)

	c, rec := newDocumentEchoContext(http.MethodPost, "/api/manuals", `{}`, "")
	if err := createManualHandler(c); err != nil {
		t.Fatalf("createManualHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ── DELETE /api/manuals/:id ──────────────────────────────────────────────

func TestClearManualHandler_DemotesAndReturns204(t *testing.T) {
	withTestDocumentStore(t)

	manual, err := globalDocumentStore.CreateManual("Operations Manual")
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodDelete, "/api/manuals/"+manual.ID, "", manual.ID)
	if err := clearManualHandler(c); err != nil {
		t.Fatalf("clearManualHandler returned error: %v", err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	manuals, err := globalDocumentStore.ListManuals()
	if err != nil {
		t.Fatalf("ListManuals: %v", err)
	}
	if len(manuals) != 0 {
		t.Fatalf("expected the manual to be demoted, got %+v", manuals)
	}
}

func TestClearManualHandler_NonManualReturns409(t *testing.T) {
	withTestDocumentStore(t)

	folder, err := globalDocumentStore.CreateFolder("Recipes", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodDelete, "/api/manuals/"+folder.ID, "", folder.ID)
	if err := clearManualHandler(c); err != nil {
		t.Fatalf("clearManualHandler returned error: %v", err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ── GET /api/manuals/:id/tree ────────────────────────────────────────────

func TestManualTreeHandler_ReturnsOrderedTree(t *testing.T) {
	withTestDocumentStore(t)

	manual, err := globalDocumentStore.CreateManual("Operations Manual")
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	if _, err := globalDocumentStore.CreateFolder("Getting Underway", &manual.ID); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/manuals/"+manual.ID+"/tree", "", manual.ID)
	if err := manualTreeHandler(c); err != nil {
		t.Fatalf("manualTreeHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var tree manualTreeNode
	if err := json.Unmarshal(rec.Body.Bytes(), &tree); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if tree.ID != manual.ID || len(tree.Children) != 1 {
		t.Fatalf("expected the manual's own tree with one child, got %+v", tree)
	}
}

func TestManualTreeHandler_UnknownIDReturns404(t *testing.T) {
	withTestDocumentStore(t)

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/manuals/does-not-exist/tree", "", "does-not-exist")
	if err := manualTreeHandler(c); err != nil {
		t.Fatalf("manualTreeHandler returned error: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ── POST /api/manuals/:id/reorder ────────────────────────────────────────

func TestReorderManualHandler_AppliesAndReturnsUpdatedTree(t *testing.T) {
	withTestDocumentStore(t)

	manual, err := globalDocumentStore.CreateManual("Operations Manual")
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	a, err := globalDocumentStore.CreateFolder("A", &manual.ID)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	b, err := globalDocumentStore.CreateFolder("B", &manual.ID)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}

	body := `{"parent_id":"` + manual.ID + `","items":[
		{"kind":"folder","id":"` + b.ID + `","sort_index":0},
		{"kind":"folder","id":"` + a.ID + `","sort_index":1}
	]}`
	c, rec := newDocumentEchoContext(http.MethodPost, "/api/manuals/"+manual.ID+"/reorder", body, manual.ID)
	if err := reorderManualHandler(c); err != nil {
		t.Fatalf("reorderManualHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var tree manualTreeNode
	if err := json.Unmarshal(rec.Body.Bytes(), &tree); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(tree.Children) != 2 || tree.Children[0].ID != b.ID || tree.Children[1].ID != a.ID {
		t.Fatalf("expected B then A, got %+v", tree.Children)
	}
}

// TestReorderManualHandler_UnknownItemIDRejectsWholeCall is a required
// Verification case: reorder rejects the whole call on an unknown id.
func TestReorderManualHandler_UnknownItemIDRejectsWholeCall(t *testing.T) {
	withTestDocumentStore(t)

	manual, err := globalDocumentStore.CreateManual("Operations Manual")
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	a, err := globalDocumentStore.CreateFolder("A", &manual.ID)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}

	body := `{"parent_id":"` + manual.ID + `","items":[
		{"kind":"folder","id":"` + a.ID + `","sort_index":5},
		{"kind":"folder","id":"does-not-exist","sort_index":6}
	]}`
	c, rec := newDocumentEchoContext(http.MethodPost, "/api/manuals/"+manual.ID+"/reorder", body, manual.ID)
	if err := reorderManualHandler(c); err != nil {
		t.Fatalf("reorderManualHandler returned error: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestReorderManualHandler_InvalidItemKindReturns400(t *testing.T) {
	withTestDocumentStore(t)

	manual, err := globalDocumentStore.CreateManual("Operations Manual")
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}

	body := `{"parent_id":"` + manual.ID + `","items":[{"kind":"bogus","id":"x","sort_index":0}]}`
	c, rec := newDocumentEchoContext(http.MethodPost, "/api/manuals/"+manual.ID+"/reorder", body, manual.ID)
	if err := reorderManualHandler(c); err != nil {
		t.Fatalf("reorderManualHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
