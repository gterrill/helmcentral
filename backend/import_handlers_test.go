package main

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// Handler-level tests for the /api/import routes: the multipart intake, the
// status codes, and the JSON the wizard reads.

func importMultipartContext(t *testing.T, target string, fields map[string]string, fileName string, file []byte, params map[string]string) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			t.Fatalf("field: %v", err)
		}
	}
	if fileName != "" {
		fw, err := w.CreateFormFile("file", fileName)
		if err != nil {
			t.Fatalf("file part: %v", err)
		}
		fw.Write(file)
	}
	w.Close()
	req := httptest.NewRequest(http.MethodPost, target, body)
	req.Header.Set(echo.HeaderContentType, w.FormDataContentType())
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	setParams(c, params)
	return c, rec
}

func setParams(c echo.Context, params map[string]string) {
	var names, values []string
	for k, v := range params {
		names = append(names, k)
		values = append(values, v)
	}
	c.SetParamNames(names...)
	c.SetParamValues(values...)
}

func importJSONContext(method, target, body string, params map[string]string) (echo.Context, *httptest.ResponseRecorder) {
	c, rec := newDocumentEchoContext(method, target, body, "")
	setParams(c, params)
	return c, rec
}

func fixtureBytes(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/yachtwave/export_redacted.html")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return raw
}

func uploadFixtureRun(t *testing.T) importRun {
	t.Helper()
	c, rec := importMultipartContext(t, "/api/import/runs", map[string]string{"source": "yachtwave"}, "Export_Pikorua.html", fixtureBytes(t), nil)
	if err := createImportRunHandler(c); err != nil {
		t.Fatalf("createImportRunHandler: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var run importRun
	if err := json.Unmarshal(rec.Body.Bytes(), &run); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return run
}

func TestCreateImportRunHandler_StagesTheExport(t *testing.T) {
	withTestDocumentStore(t)
	run := uploadFixtureRun(t)

	if run.ID == "" || run.Status != "draft" || run.Source != "yachtwave" {
		t.Fatalf("run = %+v", run)
	}
	if run.FileSHA256 != sha256Hex(fixtureBytes(t)) {
		t.Fatalf("file_sha256 = %s", run.FileSHA256)
	}
	if !strings.Contains(run.SourceLabel, "Pikorua") || !strings.Contains(run.SourceLabel, "Export_Pikorua.html") {
		t.Fatalf("source_label = %q", run.SourceLabel)
	}
	if len(run.Staged.Equipment) != 14 || len(run.Decisions.Records) == 0 {
		t.Fatalf("staged/decisions missing: %d %d", len(run.Staged.Equipment), len(run.Decisions.Records))
	}
	if run.AlreadyImported == nil {
		t.Fatalf("already_imported must be a list")
	}
}

func TestCreateImportRunHandler_RejectsBadUploads(t *testing.T) {
	withTestDocumentStore(t)

	cases := map[string]struct {
		fields   map[string]string
		fileName string
		file     []byte
		contains string
	}{
		"not an export":  {map[string]string{"source": "yachtwave"}, "x.html", []byte("<html><title>Nope</title></html>"), "YachtWave Vessel Export"},
		"no file":        {map[string]string{"source": "yachtwave"}, "", nil, "file"},
		"unknown source": {map[string]string{"source": "navionics"}, "x.html", []byte("x"), "source"},
		"missing source": {map[string]string{}, "x.html", []byte("x"), "source"},
		"empty file":     {map[string]string{"source": "yachtwave"}, "x.html", []byte{}, "empty"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c, rec := importMultipartContext(t, "/api/import/runs", tc.fields, tc.fileName, tc.file, nil)
			if err := createImportRunHandler(c); err != nil {
				t.Fatalf("handler error: %v", err)
			}
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), tc.contains) {
				t.Fatalf("expected 400 mentioning %q, got %d %s", tc.contains, rec.Code, rec.Body.String())
			}
		})
	}
	if n := countImportRows(t, globalDocumentStore, `SELECT COUNT(*) FROM import_runs`); n != 0 {
		t.Fatalf("a rejected upload must not leave a run behind, found %d", n)
	}
}

func TestImportRunHandlers_GetPatchDelete(t *testing.T) {
	withTestDocumentStore(t)
	run := uploadFixtureRun(t)
	params := map[string]string{"id": run.ID}

	c, rec := importJSONContext(http.MethodGet, "/api/import/runs/"+run.ID, "", params)
	if err := getImportRunHandler(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("GET: %v %d", err, rec.Code)
	}
	var got importRun
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got.ID != run.ID || len(got.Staged.Spares) != 36 {
		t.Fatalf("GET body = %+v", got.ID)
	}

	c, rec = importJSONContext(http.MethodGet, "/api/import/runs/nope", "", map[string]string{"id": "nope"})
	getImportRunHandler(c)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown run: %d", rec.Code)
	}

	key := run.Staged.Equipment[0].Key
	c, rec = importJSONContext(http.MethodPatch, "/api/import/runs/"+run.ID, `{"decisions":{"records":{"`+key+`":{"action":"skip","target_id":""}}}}`, params)
	if err := patchImportRunHandler(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("PATCH: %v %d %s", err, rec.Code, rec.Body.String())
	}
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Decisions.Records[key].Action != "skip" {
		t.Fatalf("patch not applied: %+v", got.Decisions.Records[key])
	}

	c, rec = importJSONContext(http.MethodPatch, "/api/import/runs/"+run.ID, `{"decisions":{"records":{"`+key+`":{"action":"explode"}}}}`, params)
	patchImportRunHandler(c)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "explode") {
		t.Fatalf("bad action should be a 400 that says so, got %d %s", rec.Code, rec.Body.String())
	}

	c, rec = importJSONContext(http.MethodPatch, "/api/import/runs/"+run.ID, `{"nonsense":true}`, params)
	patchImportRunHandler(c)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a body with no decisions is a 400, got %d", rec.Code)
	}

	c, rec = importJSONContext(http.MethodDelete, "/api/import/runs/"+run.ID, "", params)
	if err := deleteImportRunHandler(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("DELETE: %v %d", err, rec.Code)
	}
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Status != "abandoned" {
		t.Fatalf("status after delete = %s", got.Status)
	}

	c, rec = importJSONContext(http.MethodPatch, "/api/import/runs/"+run.ID, `{"decisions":{}}`, params)
	patchImportRunHandler(c)
	if rec.Code != http.StatusConflict {
		t.Fatalf("patching an abandoned run: %d", rec.Code)
	}
}

func TestUploadImportFileHandler(t *testing.T) {
	store := withTestDocumentStore(t)
	run := uploadFixtureRun(t)

	var photo, doc stagedFile
	for _, f := range run.Staged.Files {
		if f.Kind == "photo" && photo.Key == "" {
			photo = f
		}
		if f.Kind == "document" && doc.Key == "" {
			doc = f
		}
	}
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0x01}, 64)...)

	c, rec := importMultipartContext(t, "/api/import/runs/x/files/y", nil, "zen.png", png, map[string]string{"id": run.ID, "key": photo.Key})
	if err := uploadImportFileHandler(c); err != nil {
		t.Fatalf("upload: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		DocumentID string    `json:"document_id"`
		Duplicate  bool      `json:"duplicate"`
		Run        importRun `json:"run"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.DocumentID == "" || resp.Duplicate {
		t.Fatalf("response = %+v (%v) %s", resp, err, rec.Body.String())
	}
	if resp.Run.Decisions.Files[photo.Key].DocumentID != resp.DocumentID {
		t.Fatalf("the run's decision must carry the document id: %+v", resp.Run.Decisions.Files[photo.Key])
	}
	stored, err := store.Get(resp.DocumentID)
	if err != nil || stored.MIME != "image/png" || stored.Title != photo.Label {
		t.Fatalf("stored document = %+v (%v)", stored, err)
	}
	// Not in a folder, not linked to anything yet: commit does the linking.
	if stored.FolderID != nil {
		t.Fatalf("an imported file lands unfiled")
	}

	// The same bytes again are the same document.
	c, rec = importMultipartContext(t, "/x", nil, "zen.png", png, map[string]string{"id": run.ID, "key": photo.Key})
	uploadImportFileHandler(c)
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if rec.Code != http.StatusOK || !resp.Duplicate {
		t.Fatalf("duplicate upload: %d %s", rec.Code, rec.Body.String())
	}

	// A photo slot refuses a non-image.
	c, rec = importMultipartContext(t, "/x", nil, "notes.txt", []byte("hello there"), map[string]string{"id": run.ID, "key": photo.Key})
	uploadImportFileHandler(c)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a photo must be an image: %d %s", rec.Code, rec.Body.String())
	}

	// A document slot takes a PDF.
	pdf := []byte("%PDF-1.4\n1 0 obj<<>>endobj\n")
	c, rec = importMultipartContext(t, "/x", nil, "receipt.pdf", pdf, map[string]string{"id": run.ID, "key": doc.Key})
	uploadImportFileHandler(c)
	if rec.Code != http.StatusCreated {
		t.Fatalf("pdf upload: %d %s", rec.Code, rec.Body.String())
	}

	// Unknown key, unknown run.
	c, rec = importMultipartContext(t, "/x", nil, "a.pdf", pdf, map[string]string{"id": run.ID, "key": "zzzz"})
	uploadImportFileHandler(c)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown file key: %d", rec.Code)
	}
	c, rec = importMultipartContext(t, "/x", nil, "a.pdf", pdf, map[string]string{"id": "nope", "key": doc.Key})
	uploadImportFileHandler(c)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown run: %d", rec.Code)
	}
	if _, err := io.ReadAll(rec.Body); err != nil {
		t.Fatal(err)
	}
}

func TestCommitImportRunHandler(t *testing.T) {
	withTestDocumentStore(t)
	run := uploadFixtureRun(t)
	params := map[string]string{"id": run.ID}

	// Not ready: files undecided and log entries ambiguous -> 409 with the reason.
	c, rec := importJSONContext(http.MethodPost, "/api/import/runs/"+run.ID+"/commit", "", params)
	if err := commitImportRunHandler(c); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "file") {
		t.Fatalf("expected 409 naming the files, got %d %s", rec.Code, rec.Body.String())
	}

	ready := readyToCommit(t, globalDocumentStore, run)
	c, rec = importJSONContext(http.MethodPost, "/api/import/runs/"+ready.ID+"/commit", "", params)
	if err := commitImportRunHandler(c); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var res importCommitResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if res.Run.Status != "committed" || res.Summary.Counts["notes"].Created != 19 || len(res.Summary.Records) == 0 {
		t.Fatalf("result = %+v", res.Summary.Counts)
	}

	// Committing again is a 409.
	c, rec = importJSONContext(http.MethodPost, "/api/import/runs/"+ready.ID+"/commit", "", params)
	commitImportRunHandler(c)
	if rec.Code != http.StatusConflict {
		t.Fatalf("second commit: %d", rec.Code)
	}
	// And a committed run cannot be abandoned.
	c, rec = importJSONContext(http.MethodDelete, "/api/import/runs/"+ready.ID, "", params)
	deleteImportRunHandler(c)
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete of a committed run: %d", rec.Code)
	}
}

func TestCreateImportRun_OversizeUploadIs413(t *testing.T) {
	store := withTestDocumentStore(t)
	_ = store
	big := bytes.Repeat([]byte("x"), importMaxUploadBytes+1024)
	c, rec := importMultipartContext(t, "/api/import/runs", map[string]string{"source": "yachtwave"}, "Export.html", big, nil)
	if err := createImportRunHandler(c); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if rec.Code != http.StatusRequestEntityTooLarge || !strings.Contains(rec.Body.String(), "the export file is too large") {
		t.Fatalf("want 413 too large, got %d: %s", rec.Code, rec.Body.String())
	}
}
