package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
)

// Filing Inbox photos onto an equipment item or an open job (ADR 0279, PR 3).

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
	if where, _ := w.inbox.SourceWhere(src); where != inboxWhereFiled {
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
