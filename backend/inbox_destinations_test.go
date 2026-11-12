package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Filing Inbox photos onto an equipment item or an open job (ADR 0281, PR 3).

func fileToEquipment(t *testing.T, equipmentID string, ids ...string) (int, []map[string]any) {
	t.Helper()
	items := make([]map[string]string, len(ids))
	for i, id := range ids {
		items[i] = map[string]string{"id": id}
	}
	raw, _ := json.Marshal(map[string]any{"equipment_id": equipmentID, "items": items})
	code, body := send(t, fileInboxToEquipmentHandler, http.MethodPost, string(raw))
	return code, resultsOf(body)
}

func fileToJob(t *testing.T, jobID string, ids ...string) (int, []map[string]any) {
	t.Helper()
	items := make([]map[string]string, len(ids))
	for i, id := range ids {
		items[i] = map[string]string{"id": id}
	}
	raw, _ := json.Marshal(map[string]any{"job_id": jobID, "items": items})
	code, body := send(t, fileInboxToOpenJobHandler, http.MethodPost, string(raw))
	return code, resultsOf(body)
}

func resultsOf(body map[string]any) []map[string]any {
	raw, _ := body["results"].([]any)
	out := make([]map[string]any, len(raw))
	for i, r := range raw {
		out[i] = r.(map[string]any)
	}
	return out
}

func uploadPDFForFiling(t *testing.T) string {
	t.Helper()
	pdf, err := os.ReadFile(testdataPath("two_page.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	code, body := inboxUpload(t, pdf, "manual.pdf", nil)
	if code != http.StatusCreated {
		t.Fatal(code, body)
	}
	return body["item"].(map[string]any)["id"].(string)
}

func TestInboxFileToEquipmentMovesThePhotoAndClearsTheItem(t *testing.T) {
	w := newInboxWorld(t)
	pump, err := w.docs.CreateEquipment(equipmentItem{Name: "Bilge pump", Category: "general"})
	if err != nil {
		t.Fatal(err)
	}
	src := strings.Repeat("c", 64)
	jpg := inboxTestJPEG(t, 60)
	code, body := inboxUpload(t, jpg, "pump.jpg", map[string]string{"source_sha256": src, "thumb": string(jpg)})
	if code != http.StatusCreated {
		t.Fatal(code, body)
	}
	id := body["item"].(map[string]any)["id"].(string)
	it, err := w.inbox.Get(id)
	if err != nil {
		t.Fatal(err)
	}

	code, results := fileToEquipment(t, pump.ID, id)
	if code != 200 || len(results) != 1 || results[0]["ok"] != true || results[0]["document_id"] == "" {
		t.Fatal(code, results)
	}
	got, err := w.docs.GetEquipment(pump.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.PhotoIDs) != 1 || got.PhotoIDs[0] != results[0]["document_id"] {
		t.Fatalf("photo ids %v, result %v", got.PhotoIDs, results[0])
	}
	if n, _ := w.inbox.Count(); n != 0 {
		t.Errorf("inbox count %d", n)
	}
	for _, p := range []string{inboxFilePath(it.SHA256), inboxThumbPath(it.SHA256)} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s still there: %v", p, err)
		}
	}
	if where, _ := w.inbox.SourceWhere(src, w.docs.HasSHA); where != inboxWhereFiled {
		t.Errorf("original remembered as %q", where)
	}
}

func TestInboxFileToEquipmentRefusesAPDFAndKeepsIt(t *testing.T) {
	w := newInboxWorld(t)
	pump, _ := w.docs.CreateEquipment(equipmentItem{Name: "Bilge pump", Category: "general"})
	pdf := uploadPDFForFiling(t)
	photo := uploadForFiling(t, 61, nil)

	code, results := fileToEquipment(t, pump.ID, pdf, photo)
	if code != 200 || len(results) != 2 {
		t.Fatal(code, results)
	}
	if results[0]["ok"] == true || !strings.Contains(results[0]["error"].(string), "only photos can go on equipment") {
		t.Errorf("pdf: %v", results[0])
	}
	if results[1]["ok"] != true {
		t.Errorf("photo: %v", results[1])
	}
	if _, err := w.inbox.Get(pdf); err != nil {
		t.Errorf("the PDF should stay: %v", err)
	}
	if n, _ := w.inbox.Count(); n != 1 {
		t.Errorf("count %d", n)
	}
}

func TestInboxFileToEquipmentUnknownTargetKeepsTheItem(t *testing.T) {
	w := newInboxWorld(t)
	id := uploadForFiling(t, 62, nil)
	code, results := fileToEquipment(t, "nope", id)
	if code != 200 || len(results) != 1 || results[0]["ok"] == true {
		t.Fatal(code, results)
	}
	if msg, _ := results[0]["error"].(string); !strings.Contains(msg, "no longer exists") {
		t.Errorf("error %q", msg)
	}
	if n, _ := w.inbox.Count(); n != 1 {
		t.Errorf("count %d", n)
	}
	if docs, _ := w.docs.List(nil, false, "", 50, 0); len(docs) != 0 {
		t.Errorf("a document was left behind: %+v", docs)
	}
	if code, _ := send(t, fileInboxToEquipmentHandler, http.MethodPost, `{"items":[{"id":"`+id+`"}]}`); code != 400 {
		t.Errorf("no equipment_id: %d", code)
	}
}

func TestInboxPhotoRefusalWording(t *testing.T) {
	if err := inboxPhotoRefusal(inboxItem{MIME: "image/png"}, "equipment"); err != nil {
		t.Errorf("png: %v", err)
	}
	if err := inboxPhotoRefusal(inboxItem{MIME: "application/pdf"}, "a job"); err == nil || err.Error() != "only photos can go on a job; this is a PDF" {
		t.Errorf("pdf: %v", err)
	}
}

// The target is looked up once, before any item is claimed: a missing item
// refuses all of them with the same words, whatever each file is.
func TestInboxFileToMissingEquipmentRefusesEveryItemAndCopiesNothing(t *testing.T) {
	w := newInboxWorld(t)
	ids := []string{uploadPDFForFiling(t), uploadForFiling(t, 70, nil), uploadForFiling(t, 71, nil)}

	code, results := fileToEquipment(t, "nope", ids...)
	if code != 200 || len(results) != 3 {
		t.Fatal(code, results)
	}
	for i, r := range results {
		if msg, _ := r["error"].(string); r["ok"] == true || msg != "that equipment item no longer exists" {
			t.Errorf("item %d: %v", i, r)
		}
	}
	if docs, _ := w.docs.List(nil, false, "", 50, 0); len(docs) != 0 {
		t.Errorf("a document was made: %+v", docs)
	}
	if n, _ := w.inbox.Count(); n != 3 {
		t.Errorf("count %d", n)
	}
}

func TestInboxConcurrentFilingToEquipmentMakesOnePhoto(t *testing.T) {
	w := newInboxWorld(t)
	pump, _ := w.docs.CreateEquipment(equipmentItem{Name: "Bilge pump", Category: "general"})
	id := uploadForFiling(t, 63, nil)
	var wg sync.WaitGroup
	oks := make(chan bool, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, rec := newDocumentEchoContext(http.MethodPost, "/x", `{"equipment_id":"`+pump.ID+`","items":[{"id":"`+id+`"}]}`, "")
			_ = fileInboxToEquipmentHandler(c)
			oks <- strings.Contains(rec.Body.String(), `"ok":true`)
		}()
	}
	wg.Wait()
	close(oks)
	n := 0
	for ok := range oks {
		if ok {
			n++
		}
	}
	got, _ := w.docs.GetEquipment(pump.ID)
	if n != 1 || len(got.PhotoIDs) != 1 {
		t.Fatalf("ok=%d photos=%d", n, len(got.PhotoIDs))
	}
}

func TestInboxFileToOpenJobMovesThePhotoAndClearsTheItem(t *testing.T) {
	w := newInboxWorld(t)
	job := mustOpenJob(t, w.docs, "2026-11-07", openJobReq("Leaking hose"))
	jpg := inboxTestJPEG(t, 65)
	code, body := inboxUpload(t, jpg, "hose.jpg", map[string]string{"thumb": string(jpg)})
	if code != http.StatusCreated {
		t.Fatal(code, body)
	}
	id := body["item"].(map[string]any)["id"].(string)
	it, _ := w.inbox.Get(id)

	code, results := fileToJob(t, job.ID, id)
	if code != 200 || results[0]["ok"] != true {
		t.Fatal(code, results)
	}
	got, err := w.docs.GetOpenJob(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.PhotoIDs) != 1 || got.PhotoIDs[0] != results[0]["document_id"] {
		t.Fatalf("photo ids %v", got.PhotoIDs)
	}
	if n, _ := w.inbox.Count(); n != 0 {
		t.Errorf("count %d", n)
	}
	if _, err := os.Stat(inboxThumbPath(it.SHA256)); !os.IsNotExist(err) {
		t.Errorf("thumbnail still there: %v", err)
	}
}

func TestInboxFileToOpenJobRefusesAPDFAndAnUnknownOrClosedJob(t *testing.T) {
	w := newInboxWorld(t)
	job := mustOpenJob(t, w.docs, "2026-11-07", openJobReq("Leaking hose"))
	pdf := uploadPDFForFiling(t)
	photo := uploadForFiling(t, 66, nil)

	_, results := fileToJob(t, job.ID, pdf)
	if results[0]["ok"] == true || !strings.Contains(results[0]["error"].(string), "only photos can go on a job") {
		t.Errorf("pdf: %v", results[0])
	}
	_, results = fileToJob(t, "nope", photo)
	if results[0]["ok"] == true || !strings.Contains(results[0]["error"].(string), "no longer exists") {
		t.Errorf("unknown: %v", results[0])
	}
	if _, err := w.docs.DropOpenJobCmd(mustParseDate(t, "2026-11-08"), job.ID, "not needed"); err != nil {
		t.Fatal(err)
	}
	_, results = fileToJob(t, job.ID, photo)
	if results[0]["ok"] == true || !strings.Contains(results[0]["error"].(string), "closed") {
		t.Errorf("closed: %v", results[0])
	}
	if n, _ := w.inbox.Count(); n != 2 {
		t.Errorf("both should stay, count %d", n)
	}
	if docs, _ := w.docs.List(nil, false, "", 50, 0); len(docs) != 0 {
		t.Errorf("a document was left behind: %+v", docs)
	}
}

func TestInboxConcurrentFilingToOpenJobMakesOnePhoto(t *testing.T) {
	w := newInboxWorld(t)
	job := mustOpenJob(t, w.docs, "2026-11-07", openJobReq("Leaking hose"))
	id := uploadForFiling(t, 67, nil)
	var wg sync.WaitGroup
	oks := make(chan bool, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, rec := newDocumentEchoContext(http.MethodPost, "/x", `{"job_id":"`+job.ID+`","items":[{"id":"`+id+`"}]}`, "")
			_ = fileInboxToOpenJobHandler(c)
			oks <- strings.Contains(rec.Body.String(), `"ok":true`)
		}()
	}
	wg.Wait()
	close(oks)
	n := 0
	for ok := range oks {
		if ok {
			n++
		}
	}
	got, _ := w.docs.GetOpenJob(job.ID)
	if n != 1 || len(got.PhotoIDs) != 1 {
		t.Fatalf("ok=%d photos=%d", n, len(got.PhotoIDs))
	}
}

// The link runs under the content lock, as the photo upload endpoints' does:
// a Documents delete of these bytes cannot slip in between the copy and the link.
func TestInboxPhotoLinkRunsUnderTheContentLock(t *testing.T) {
	w := newInboxWorld(t)
	a, _ := w.docs.CreateEquipment(equipmentItem{Name: "Bilge pump", Category: "general"})
	id := uploadForFiling(t, 80, nil)
	it, _ := w.inbox.Get(id)
	dest := equipmentDestination(w.docs)
	real := dest.link
	dest.link = func(target, documentID string) error {
		acquired := make(chan struct{})
		go func() {
			unlock := lockDocumentSHA(it.SHA256)
			close(acquired)
			unlock()
		}()
		select {
		case <-acquired:
			t.Error("the link ran without the content lock")
		case <-time.After(200 * time.Millisecond):
		}
		return real(target, documentID)
	}
	res, err := fileInboxItemAsPhoto(w.inbox, newInboxLibrary(w.docs), id, a.ID, dest)
	if err != nil || !res.OK {
		t.Fatal(res, err)
	}
}

// A link that fails (the job closed since the check) leaves no new document,
// no file for it, and the item in the Inbox.
func TestInboxFailedLinkLeavesNoNewDocument(t *testing.T) {
	w := newInboxWorld(t)
	job := mustOpenJob(t, w.docs, "2026-11-07", openJobReq("Leaking hose"))
	id := uploadForFiling(t, 81, nil)
	it, _ := w.inbox.Get(id)
	if _, err := w.docs.DropOpenJobCmd(mustParseDate(t, "2026-11-08"), job.ID, "not needed"); err != nil {
		t.Fatal(err)
	}
	_, err := fileInboxItemAsPhoto(w.inbox, newInboxLibrary(w.docs), id, job.ID, openJobDestination(w.docs))
	if err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("expected the closed refusal, got %v", err)
	}
	if docs, _ := w.docs.List(nil, false, "", 50, 0); len(docs) != 0 {
		t.Errorf("a document was left behind: %+v", docs)
	}
	if _, err := os.Stat(filepath.Join(documentsDirPath(), it.SHA256)); !os.IsNotExist(err) {
		t.Errorf("the copied file is still there: %v", err)
	}
	if _, err := w.inbox.Get(id); err != nil {
		t.Errorf("the item should stay: %v", err)
	}
}

// A photo that was linked but failed to clear from the Inbox can be cleared by
// filing it again, even though the job has closed since: the photo is already
// on the job, which is what the operator asked for.
func TestInboxRefilingALinkedPhotoClearsItEvenAfterTheJobClosed(t *testing.T) {
	w := newInboxWorld(t)
	job := mustOpenJob(t, w.docs, "2026-11-07", openJobReq("Leaking hose"))
	id := uploadForFiling(t, 84, nil)

	prev := inboxRemoveItem
	inboxRemoveItem = func(*inboxStore, inboxItem) error { return errors.New("disk gone") }
	code, results := fileToJob(t, job.ID, id)
	inboxRemoveItem = prev
	if code != 200 || results[0]["ok"] != false || !strings.Contains(results[0]["error"].(string), "could not be cleared") {
		t.Fatal(code, results)
	}
	if _, err := w.docs.DropOpenJobCmd(mustParseDate(t, "2026-11-08"), job.ID, "not needed"); err != nil {
		t.Fatal(err)
	}

	code, results = fileToJob(t, job.ID, id)
	if code != 200 || results[0]["ok"] != true || results[0]["document_id"] == "" {
		t.Fatalf("expected the item to clear, got %d %v", code, results)
	}
	if _, err := w.inbox.Get(id); err == nil {
		t.Error("the item should be gone from the Inbox")
	}
}

// The bytes are already in Documents: the photo links to that document, and a
// link that fails leaves it exactly as it was.
func TestInboxPhotoLinksAnExistingDocumentAndAFailureKeepsIt(t *testing.T) {
	w := newInboxWorld(t)
	a, _ := w.docs.CreateEquipment(equipmentItem{Name: "Bilge pump", Category: "general"})
	id := uploadForFiling(t, 82, nil)
	it, _ := w.inbox.Get(id)
	existing, created, err := newInboxLibrary(w.docs).copy(it, nil, "", nil)
	if err != nil || !created {
		t.Fatal(created, err)
	}

	dest := equipmentDestination(w.docs)
	real := dest.link
	dest.link = func(_, _ string) error { return errors.New("link failed") }
	if _, err := fileInboxItemAsPhoto(w.inbox, newInboxLibrary(w.docs), id, a.ID, dest); err == nil {
		t.Fatal("expected the link failure")
	}
	if _, err := w.docs.Get(existing.ID); err != nil {
		t.Fatalf("the existing document was touched: %v", err)
	}
	if _, err := os.Stat(filepath.Join(documentsDirPath(), it.SHA256)); err != nil {
		t.Errorf("its file went: %v", err)
	}

	dest.link = real
	res, err := fileInboxItemAsPhoto(w.inbox, newInboxLibrary(w.docs), id, a.ID, dest)
	if err != nil || res.DocumentID != existing.ID {
		t.Fatal(res, err)
	}
	got, _ := w.docs.GetEquipment(a.ID)
	if len(got.PhotoIDs) != 1 || got.PhotoIDs[0] != existing.ID {
		t.Fatalf("photo ids %v", got.PhotoIDs)
	}
}

// A Documents delete of the same bytes starts just before the link: it waits
// for the content lock, so the link lands first and the delete then removes
// document and link together. Linked outside the lock, the delete wins and the
// link fails.
func TestInboxConcurrentDocumentsDeleteCannotLeaveADanglingLink(t *testing.T) {
	w := newInboxWorld(t)
	a, _ := w.docs.CreateEquipment(equipmentItem{Name: "Bilge pump", Category: "general"})
	id := uploadForFiling(t, 83, nil)
	it, _ := w.inbox.Get(id)
	dest := equipmentDestination(w.docs)
	real := dest.link
	deleted := make(chan error, 1)
	dest.link = func(target, documentID string) error {
		go func() {
			unlock := lockDocumentSHA(it.SHA256)
			defer unlock()
			_, err := w.docs.Delete(documentID)
			deleted <- err
		}()
		time.Sleep(200 * time.Millisecond)
		return real(target, documentID)
	}
	res, err := fileInboxItemAsPhoto(w.inbox, newInboxLibrary(w.docs), id, a.ID, dest)
	if err != nil || !res.OK {
		t.Fatal(res, err)
	}
	if err := <-deleted; err != nil {
		t.Fatal(err)
	}
	got, _ := w.docs.GetEquipment(a.ID)
	if len(got.PhotoIDs) != 0 {
		t.Errorf("a link to a deleted document: %v", got.PhotoIDs)
	}
}

func TestInboxPhotoRefusalNamesGIFAndWebP(t *testing.T) {
	for mime, want := range map[string]string{
		"image/gif":  "only JPEG or PNG photos can go on equipment; this one is a GIF",
		"image/webp": "only JPEG or PNG photos can go on equipment; this one is a WebP",
		"image/tiff": "only JPEG or PNG photos can go on equipment; this file isn't one",
	} {
		err := inboxPhotoRefusal(inboxItem{MIME: mime}, "equipment")
		if err == nil || err.Error() != want {
			t.Errorf("%s: %v, want %q", mime, err, want)
		}
	}
}

func TestInboxEquipmentExists(t *testing.T) {
	w := newInboxWorld(t)
	pump, _ := w.docs.CreateEquipment(equipmentItem{Name: "Bilge pump", Category: "general"})
	if ok, err := w.docs.EquipmentExists(pump.ID); err != nil || !ok {
		t.Errorf("existing: %v %v", ok, err)
	}
	if ok, err := w.docs.EquipmentExists("nope"); err != nil || ok {
		t.Errorf("missing: %v %v", ok, err)
	}
}

func TestInboxPhotoBodyRejectsUnknownKeysAndWrongTypes(t *testing.T) {
	w := newInboxWorld(t)
	pump, _ := w.docs.CreateEquipment(equipmentItem{Name: "Bilge pump", Category: "general"})
	id := uploadForFiling(t, 82, nil)
	items := `"items":[{"id":"` + id + `"}]`
	for name, body := range map[string]string{
		"misspelt key":   `{"equpiment_id":"` + pump.ID + `",` + items + `}`,
		"other target":   `{"equipment_id":"` + pump.ID + `","job_id":"x",` + items + `}`,
		"wrong type":     `{"equipment_id":5,` + items + `}`,
		"items not list": `{"equipment_id":"` + pump.ID + `","items":"x"}`,
		"unknown item key": `{"equipment_id":"` + pump.ID + `","items":[{"id":"` + id + `","idd":"x"}]}`,
	} {
		code, resp := send(t, fileInboxToEquipmentHandler, http.MethodPost, body)
		if code != 400 || resp["error"] != "invalid body" {
			t.Errorf("%s: %d %v", name, code, resp)
		}
	}
	code, resp := send(t, fileInboxToOpenJobHandler, http.MethodPost, `{"equipment_id":"x","job_id":"y","items":[{"id":"`+id+`"}]}`)
	if code != 400 || resp["error"] != "invalid body" {
		t.Errorf("job endpoint with equipment_id: %d %v", code, resp)
	}
}

// A filed original is remembered only while the copy it became is still held.
func TestInboxFiledOriginalIsForgottenWhenItsCopyIsDeleted(t *testing.T) {
	w := newInboxWorld(t)
	pump, err := w.docs.CreateEquipment(equipmentItem{Name: "Bilge pump", Category: "general"})
	if err != nil {
		t.Fatal(err)
	}
	src := strings.Repeat("d", 64)
	jpg := inboxTestJPEG(t, 61)
	code, body := inboxUpload(t, jpg, "pump.jpg", map[string]string{"source_sha256": src})
	if code != http.StatusCreated {
		t.Fatal(code, body)
	}
	id := body["item"].(map[string]any)["id"].(string)
	code, results := fileToEquipment(t, pump.ID, id)
	if code != 200 || len(results) != 1 || results[0]["ok"] != true {
		t.Fatal(code, results)
	}
	docID := results[0]["document_id"].(string)

	known := inboxRouteHandler(t, http.MethodPost, "/api/inbox/known")
	knownNow := func() bool {
		code, body := send(t, known, http.MethodPost, `{"hashes":["`+src+`"]}`)
		if code != 200 {
			t.Fatal(code, body)
		}
		return len(body["known"].([]any)) == 1
	}
	if !knownNow() {
		t.Fatal("original not remembered while its copy is held")
	}
	code, body = inboxUpload(t, inboxTestJPEG(t, 62), "again.jpg", map[string]string{"source_sha256": src})
	if code != http.StatusOK || body["status"] != "already_uploaded" || body["where"] != "filed" {
		t.Fatalf("while held: %d %v", code, body)
	}

	if _, err := w.docs.Delete(docID); err != nil {
		t.Fatal(err)
	}
	if knownNow() {
		t.Error("original still reported known after its copy was deleted")
	}
	code, body = inboxUpload(t, inboxTestJPEG(t, 62), "again.jpg", map[string]string{"source_sha256": src})
	if code != http.StatusCreated || body["status"] != "stored" {
		t.Fatalf("after delete: %d %v", code, body)
	}
}

// A row with no recorded copy keeps skipping the original, the safe side.
func TestInboxFiledOriginalWithoutACopyIsRemembered(t *testing.T) {
	w := newInboxWorld(t)
	src := strings.Repeat("e", 64)
	if err := w.inbox.MarkFiled(src, ""); err != nil {
		t.Fatal(err)
	}
	held := func(string) (bool, error) { return false, nil }
	if where, err := w.inbox.SourceWhere(src, held); err != nil || where != inboxWhereFiled {
		t.Fatalf("where %q, err %v", where, err)
	}
	if known, err := w.inbox.KnownSources([]string{src}, held); err != nil || len(known) != 1 {
		t.Fatalf("known %v, err %v", known, err)
	}
}
