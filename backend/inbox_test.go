package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// ── fixtures ─────────────────────────────────────────────────────────────

// inboxTestJPEG is a real, tiny JPEG whose bytes differ with shade.
func inboxTestJPEG(t *testing.T, shade uint8) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 8, 8))
	for x := 0; x < 8; x++ {
		for y := 0; y < 8; y++ {
			img.SetGray(x, y, color.Gray{Y: shade})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// withInboxStore opens a fresh inbox store and directory and installs them.
func withInboxStore(t *testing.T) *inboxStore {
	t.Helper()
	store, err := newInboxStore(filepath.Join(t.TempDir(), "inbox.sqlite"))
	if err != nil {
		t.Fatalf("newInboxStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	t.Setenv("INBOX_DIR", t.TempDir())
	prev := globalInboxStore
	globalInboxStore = store
	t.Cleanup(func() { globalInboxStore = prev })
	return store
}

// inboxWorld is everything the inbox handlers read.
type inboxWorld struct {
	inbox    *inboxStore
	passages *passageStore
	docs     *documentStore
}

func newInboxWorld(t *testing.T) inboxWorld {
	t.Helper()
	w := inboxWorld{inbox: withInboxStore(t), passages: withPassageStore(t), docs: withTestDocumentStore(t)}
	withTestSecretsStore(t)
	return w
}

func inboxUpload(t *testing.T, content []byte, filename string, fields map[string]string) (int, map[string]any) {
	t.Helper()
	parts := []documentUploadField{{name: "file", filename: filename, content: content}}
	for k, v := range fields {
		parts = append(parts, documentUploadField{name: k, content: []byte(v)})
	}
	c, rec := newDocumentUploadContext(t, parts)
	if err := uploadInboxHandler(c); err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func inboxList(t *testing.T) []map[string]any {
	t.Helper()
	code, body := getJSON(t, listInboxHandler, "/api/inbox", "")
	if code != http.StatusOK {
		t.Fatal(code, body)
	}
	raw, _ := body["items"].([]any)
	out := make([]map[string]any, len(raw))
	for i, r := range raw {
		out[i] = r.(map[string]any)
	}
	return out
}

func ms(t time.Time) string { return strconv.FormatInt(t.UnixMilli(), 10) }

func addTrack(t *testing.T, s *passageStore, at time.Time, lat, lon, sog float64) {
	t.Helper()
	if err := s.AppendTrackPoint(recordedTrackPoint{At: at, Lat: lat, Lon: lon, SOGKts: pfloat(sog)}); err != nil {
		t.Fatal(err)
	}
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// ── store ────────────────────────────────────────────────────────────────

func TestInboxStoreInsertGetListDelete(t *testing.T) {
	s := withInboxStore(t)
	cap1, cap2 := int64(2000), int64(1000)
	a, err := s.Insert(inboxItem{SHA256: "a", Filename: "a.jpg", MIME: "image/jpeg", SizeBytes: 3, CaptureMs: &cap1, Fingerprint: "ff", Sharpness: 1.5})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Insert(inboxItem{SHA256: "b", Filename: "b.jpg", MIME: "image/jpeg", SizeBytes: 3, CaptureMs: &cap2})
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Insert(inboxItem{SHA256: "c", Filename: "c.pdf", MIME: "application/pdf", SizeBytes: 3})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == "" || a.ID == b.ID {
		t.Fatalf("ids %q %q", a.ID, b.ID)
	}
	if _, err := s.Insert(inboxItem{SHA256: "a", Filename: "again.jpg", MIME: "image/jpeg"}); err == nil || err != errInboxDuplicate {
		t.Fatalf("duplicate sha: %v", err)
	}
	got, err := s.Get(a.ID)
	if err != nil || got.SHA256 != "a" || got.CaptureMs == nil || *got.CaptureMs != 2000 || got.Fingerprint != "ff" || got.Sharpness != 1.5 || got.BearingDeg != nil {
		t.Fatalf("%+v %v", got, err)
	}
	if _, ok, err := s.GetBySHA("b"); err != nil || !ok {
		t.Fatalf("GetBySHA %v %v", ok, err)
	}
	if _, ok, _ := s.GetBySHA("nope"); ok {
		t.Fatal("found a sha that is not there")
	}
	// Capture order, items with no time last.
	list, err := s.List()
	if err != nil || len(list) != 3 || list[0].ID != b.ID || list[1].ID != a.ID || list[2].ID != c.ID {
		t.Fatalf("%+v %v", list, err)
	}
	if n, _ := s.Count(); n != 3 {
		t.Errorf("count %d", n)
	}
	if err := s.Delete(b.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(b.ID); err != errInboxItemNotFound {
		t.Errorf("second delete: %v", err)
	}
	if _, err := s.Get(b.ID); err != errInboxItemNotFound {
		t.Errorf("get gone: %v", err)
	}
}

// ── upload and dedupe ───────────────────────────────────────────────────

func TestInboxUploadStoresThenSkipsAnIdenticalFile(t *testing.T) {
	w := newInboxWorld(t)
	jpg := inboxTestJPEG(t, 10)
	code, body := inboxUpload(t, jpg, "IMG_1.jpg", map[string]string{
		"capture_ms": "1760400000000", "capture_offset_min": "600", "bearing_deg": "123.5",
		"phone_lat": "-20.1", "phone_lon": "149.2", "fingerprint": "00ff00ff00ff00ff", "sharpness": "42.5",
	})
	if code != http.StatusCreated || body["status"] != "stored" {
		t.Fatal(code, body)
	}
	item := body["item"].(map[string]any)
	if item["has_bearing"] != true || item["bearing_deg"] != 123.5 || item["capture_ms"] != float64(1760400000000) || item["mime"] != "image/jpeg" || item["filename"] != "IMG_1.jpg" {
		t.Fatalf("%v", item)
	}
	names := dirNames(t, inboxDirPath())
	if len(names) != 1 {
		t.Fatalf("inbox dir: %v", names)
	}

	code, body = inboxUpload(t, jpg, "IMG_1 copy.jpg", nil)
	if code != http.StatusOK || body["status"] != "already_uploaded" || body["where"] != "inbox" {
		t.Fatal(code, body)
	}
	if n, _ := w.inbox.Count(); n != 1 {
		t.Errorf("count %d", n)
	}
	if names := dirNames(t, inboxDirPath()); len(names) != 1 {
		t.Errorf("a temp file was left: %v", names)
	}
}

func TestInboxUploadSkipsAFileAlreadyInDocuments(t *testing.T) {
	w := newInboxWorld(t)
	jpg := inboxTestJPEG(t, 20)
	sha := sha256Hex(jpg)
	insertTestDocumentWithFile(t, w.docs, sha, "old.jpg", "image/jpeg", jpg)
	code, body := inboxUpload(t, jpg, "new.jpg", nil)
	if code != http.StatusOK || body["status"] != "already_uploaded" || body["where"] != "documents" {
		t.Fatal(code, body)
	}
	if n, _ := w.inbox.Count(); n != 0 {
		t.Errorf("count %d", n)
	}
	if names := dirNames(t, inboxDirPath()); len(names) != 0 {
		t.Errorf("inbox dir: %v", names)
	}
}

func TestInboxUploadRefusesWhatIsNotAPhotoOrPDF(t *testing.T) {
	newInboxWorld(t)
	if code, body := inboxUpload(t, []byte("just some text"), "notes.txt", nil); code != http.StatusUnsupportedMediaType {
		t.Errorf("text: %d %v", code, body)
	}
	if code, body := inboxUpload(t, []byte("\x00\x00\x00\x18ftypheic"), "x.heic", nil); code != http.StatusUnsupportedMediaType {
		t.Errorf("heic: %d %v", code, body)
	}
	jpg := inboxTestJPEG(t, 30)
	for name, f := range map[string]map[string]string{
		"lat range":    {"phone_lat": "91", "phone_lon": "10"},
		"lon range":    {"phone_lat": "10", "phone_lon": "181"},
		"lat alone":    {"phone_lat": "10"},
		"bearing":      {"bearing_deg": "360"},
		"not a number": {"capture_ms": "yesterday"},
		"sharpness":    {"sharpness": "-1"},
		"fingerprint":  {"fingerprint": "not hex!"},
	} {
		if code, body := inboxUpload(t, jpg, "a.jpg", f); code != http.StatusBadRequest {
			t.Errorf("%s: %d %v", name, code, body)
		}
	}
	if names := dirNames(t, inboxDirPath()); len(names) != 0 {
		t.Errorf("a rejected upload left files: %v", names)
	}
}

func TestInboxUploadTreatsZeroPhonePositionAsMissing(t *testing.T) {
	w := newInboxWorld(t)
	code, body := inboxUpload(t, inboxTestJPEG(t, 40), "a.jpg", map[string]string{"phone_lat": "0", "phone_lon": "0"})
	if code != http.StatusCreated {
		t.Fatal(code, body)
	}
	items, _ := w.inbox.List()
	if len(items) != 1 || items[0].PhoneLat != nil || items[0].PhoneLon != nil {
		t.Fatalf("%+v", items)
	}
}

func TestInboxUploadPDFKeepsNoPhotoFields(t *testing.T) {
	newInboxWorld(t)
	pdf, err := os.ReadFile(testdataPath("two_page.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	code, body := inboxUpload(t, pdf, "receipt.pdf", nil)
	if code != http.StatusCreated || body["item"].(map[string]any)["mime"] != "application/pdf" {
		t.Fatal(code, body)
	}
}

// ── list: groups and the position check ────────────────────────────────

func TestInboxListGroupsByTime(t *testing.T) {
	w := newInboxWorld(t)
	p := seedPassage(t, w.passages, passageEpoch, passageEpoch.Add(2*time.Hour))
	// The boat is moving up to and past the end of the recorded passage, then
	// sits still.
	addTrack(t, w.passages, passageEpoch.Add(time.Hour), -20.0, 149.0, 5)
	addTrack(t, w.passages, passageEpoch.Add(4*time.Hour), -20.0, 149.0, 0.1)
	addTrack(t, w.passages, passageEpoch.Add(5*time.Hour-time.Minute), -20.0, 149.0, 5)

	up := func(shade uint8, f map[string]string) string {
		code, body := inboxUpload(t, inboxTestJPEG(t, shade), "p"+strconv.Itoa(int(shade))+".jpg", f)
		if code != http.StatusCreated {
			t.Fatal(code, body)
		}
		return body["item"].(map[string]any)["id"].(string)
	}
	onPassage := up(1, map[string]string{"capture_ms": ms(passageEpoch.Add(time.Hour))})
	atRest := up(2, map[string]string{"capture_ms": ms(passageEpoch.Add(4*time.Hour + time.Minute))})
	underway := up(3, map[string]string{"capture_ms": ms(passageEpoch.Add(5 * time.Hour))})
	before := up(4, map[string]string{"capture_ms": ms(passageEpoch.Add(-3 * time.Hour))})
	noTime := up(5, nil)
	// 1 degree of latitude is 60 nm from the track.
	far := up(6, map[string]string{"capture_ms": ms(passageEpoch.Add(time.Hour + time.Minute)), "phone_lat": "-21", "phone_lon": "149"})
	near := up(7, map[string]string{"capture_ms": ms(passageEpoch.Add(time.Hour + 2*time.Minute)), "phone_lat": "-20.005", "phone_lon": "149"})

	got := map[string]map[string]any{}
	for _, it := range inboxList(t) {
		got[it["id"].(string)] = it
	}
	group := func(id string) (string, any) {
		g := got[id]["group"].(map[string]any)
		return g["kind"].(string), g["passage_id"]
	}
	if k, pid := group(onPassage); k != "passage" || pid != p.ID {
		t.Errorf("on passage: %s %v", k, pid)
	}
	if k, _ := group(atRest); k != "off_passage" {
		t.Errorf("at rest after the passage: %s", k)
	}
	if k, _ := group(underway); k != "underway" {
		t.Errorf("moving after the last passage: %s", k)
	}
	if k, _ := group(before); k != "off_passage" {
		t.Errorf("before any passage: %s", k)
	}
	if k, _ := group(noTime); k != "no_time" {
		t.Errorf("no capture time: %s", k)
	}
	if got[far]["gps_mismatch"] != true {
		t.Errorf("60 nm from the track should warn: %v", got[far])
	}
	if got[near]["gps_mismatch"] == true || got[onPassage]["gps_mismatch"] == true {
		t.Errorf("close or absent phone position must not warn")
	}
}

func TestInboxListIsEmptyNotNull(t *testing.T) {
	newInboxWorld(t)
	code, body := getJSON(t, listInboxHandler, "/api/inbox", "")
	if code != 200 {
		t.Fatal(code)
	}
	if items, ok := body["items"].([]any); !ok || len(items) != 0 {
		t.Fatalf("%v", body)
	}
	code, body = getJSON(t, inboxCountHandler, "/api/inbox/count", "")
	if code != 200 || body["count"] != float64(0) {
		t.Fatal(code, body)
	}
}

// ── content and bin ──────────────────────────────────────────────────────

func TestInboxContentServesTheFile(t *testing.T) {
	newInboxWorld(t)
	jpg := inboxTestJPEG(t, 50)
	_, body := inboxUpload(t, jpg, "a.jpg", nil)
	id := body["item"].(map[string]any)["id"].(string)
	c, rec := newDocumentEchoContext(http.MethodGet, "/x", "", id)
	if err := inboxContentHandler(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 200 || !bytes.Equal(rec.Body.Bytes(), jpg) || rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("%d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	c, rec = newDocumentEchoContext(http.MethodGet, "/x", "", "nope")
	_ = inboxContentHandler(c)
	if rec.Code != 404 {
		t.Errorf("unknown id: %d", rec.Code)
	}
}

func TestInboxBinRemovesRowsAndFiles(t *testing.T) {
	w := newInboxWorld(t)
	var ids []string
	for shade := uint8(60); shade < 63; shade++ {
		_, body := inboxUpload(t, inboxTestJPEG(t, shade), "a.jpg", nil)
		ids = append(ids, body["item"].(map[string]any)["id"].(string))
	}
	code, body := send(t, binInboxHandler, http.MethodPost, `{"ids":["`+ids[0]+`","`+ids[1]+`","unknown"]}`)
	if code != 200 || body["deleted"] != float64(2) {
		t.Fatal(code, body)
	}
	if n, _ := w.inbox.Count(); n != 1 {
		t.Errorf("count %d", n)
	}
	if names := dirNames(t, inboxDirPath()); len(names) != 1 {
		t.Errorf("files: %v", names)
	}
	if code, _ := send(t, binInboxHandler, http.MethodPost, `{"ids":[]}`); code != 400 {
		t.Errorf("empty bin: %d", code)
	}
}

// ── file to a passage ────────────────────────────────────────────────────

func uploadForFiling(t *testing.T, shade uint8, f map[string]string) string {
	t.Helper()
	code, body := inboxUpload(t, inboxTestJPEG(t, shade), "shot.jpg", f)
	if code != http.StatusCreated {
		t.Fatal(code, body)
	}
	return body["item"].(map[string]any)["id"].(string)
}

func TestInboxFileToPassageMakesPhotoSignAndLeavesTheInbox(t *testing.T) {
	w := newInboxWorld(t)
	seedPassage(t, w.passages, passageEpoch, passageEpoch.Add(2*time.Hour))
	at := passageEpoch.Add(time.Hour)
	id := uploadForFiling(t, 70, map[string]string{"capture_ms": ms(at)})

	code, body := send(t, fileInboxToPassageHandler, http.MethodPost, `{"items":[{"id":"`+id+`","title":"Humpbacks"}]}`)
	if code != 200 {
		t.Fatal(code, body)
	}
	res := body["results"].([]any)[0].(map[string]any)
	if res["ok"] != true || res["sign_id"] == "" || res["document_id"] == "" {
		t.Fatalf("%v", res)
	}
	signs, _ := w.passages.ListSigns(0, 0)
	if len(signs) != 1 || signs[0].Title != "Humpbacks" || signs[0].TMs != at.UnixMilli() || signs[0].Visibility != signPassing || signs[0].ShowS != signShowDef ||
		signs[0].PhotoMoment == nil || !*signs[0].PhotoMoment || signs[0].DocumentID == nil {
		t.Fatalf("%+v", signs)
	}
	doc, err := w.docs.Get(*signs[0].DocumentID)
	if err != nil || doc.MIME != "image/jpeg" || doc.FolderID == nil {
		t.Fatalf("%+v %v", doc, err)
	}
	folders, _, _ := w.docs.ListFolder(nil)
	if len(folders) != 1 || folders[0].Name != "Passage photos" || folders[0].ID != *doc.FolderID {
		t.Fatalf("%+v", folders)
	}
	if _, err := os.Stat(filepath.Join(documentsDirPath(), doc.SHA256)); err != nil {
		t.Errorf("the photo is not in the library: %v", err)
	}
	if n, _ := w.inbox.Count(); n != 0 {
		t.Errorf("item still in the inbox")
	}
	if names := dirNames(t, inboxDirPath()); len(names) != 0 {
		t.Errorf("inbox file left: %v", names)
	}

	// A second photo reuses the folder.
	id2 := uploadForFiling(t, 71, map[string]string{"capture_ms": ms(at.Add(time.Minute))})
	if code, body := send(t, fileInboxToPassageHandler, http.MethodPost, `{"items":[{"id":"`+id2+`","title":"Again"}]}`); code != 200 || body["results"].([]any)[0].(map[string]any)["ok"] != true {
		t.Fatal(code, body)
	}
	if folders, _, _ := w.docs.ListFolder(nil); len(folders) != 1 {
		t.Errorf("folder made twice: %+v", folders)
	}
}

func TestInboxFileToPassageReportsEachFailureAndKeepsThoseItems(t *testing.T) {
	w := newInboxWorld(t)
	seedPassage(t, w.passages, passageEpoch, passageEpoch.Add(2*time.Hour))
	in := passageEpoch.Add(time.Hour)
	good := uploadForFiling(t, 80, map[string]string{"capture_ms": ms(in)})
	noTitle := uploadForFiling(t, 81, map[string]string{"capture_ms": ms(in)})
	longTitle := uploadForFiling(t, 82, map[string]string{"capture_ms": ms(in)})
	outside := uploadForFiling(t, 83, map[string]string{"capture_ms": ms(passageEpoch.Add(9 * time.Hour))})
	noTime := uploadForFiling(t, 84, nil)
	pdf, _ := os.ReadFile(testdataPath("two_page.pdf"))
	_, pb := inboxUpload(t, pdf, "r.pdf", map[string]string{"capture_ms": ms(in)})
	pdfID := pb["item"].(map[string]any)["id"].(string)

	req, _ := json.Marshal(map[string]any{"items": []map[string]any{
		{"id": good, "title": "Fine"},
		{"id": noTitle, "title": "  "},
		{"id": longTitle, "title": strings.Repeat("x", 81)},
		{"id": outside, "title": "Outside"},
		{"id": noTime, "title": "No time"},
		{"id": pdfID, "title": "A PDF"},
		{"id": "ghost", "title": "Ghost"},
	}})
	code, body := send(t, fileInboxToPassageHandler, http.MethodPost, string(req))
	if code != 200 {
		t.Fatal(code, body)
	}
	results := body["results"].([]any)
	if len(results) != 7 {
		t.Fatalf("%v", results)
	}
	for i, wantOK := range []bool{true, false, false, false, false, false, false} {
		r := results[i].(map[string]any)
		if (r["ok"] == true) != wantOK {
			t.Errorf("item %d: %v", i, r)
		}
		if !wantOK && (r["error"] == nil || r["error"] == "") {
			t.Errorf("item %d has no reason: %v", i, r)
		}
	}
	if n, _ := w.inbox.Count(); n != 5 {
		t.Errorf("the five failures stay in the inbox, got %d", n)
	}
	signs, _ := w.passages.ListSigns(0, 0)
	if len(signs) != 1 {
		t.Errorf("signs: %d", len(signs))
	}
	// Failures made no library document.
	if all, _ := w.docs.List(nil, false, "", 50, 0); len(all) != 1 {
		t.Errorf("documents: %d", len(all))
	}
}

func TestInboxFileToPassageHonoursATimeOverride(t *testing.T) {
	w := newInboxWorld(t)
	seedPassage(t, w.passages, passageEpoch, passageEpoch.Add(2*time.Hour))
	// Taken 3 hours after the passage: not on a passage; the operator picks its end.
	id := uploadForFiling(t, 90, map[string]string{"capture_ms": ms(passageEpoch.Add(5 * time.Hour))})
	end := passageEpoch.Add(2 * time.Hour).UnixMilli()
	code, body := send(t, fileInboxToPassageHandler, http.MethodPost, `{"items":[{"id":"`+id+`","title":"Back at the dock","t_ms":`+strconv.FormatInt(end, 10)+`}]}`)
	if code != 200 || body["results"].([]any)[0].(map[string]any)["ok"] != true {
		t.Fatal(code, body)
	}
	signs, _ := w.passages.ListSigns(0, 0)
	if len(signs) != 1 || signs[0].TMs != end {
		t.Fatalf("%+v", signs)
	}
}

func TestInboxFileToPassageUndoesTheDocumentWhenTheSignFails(t *testing.T) {
	w := newInboxWorld(t)
	seedPassage(t, w.passages, passageEpoch, passageEpoch.Add(2*time.Hour))
	id := uploadForFiling(t, 91, map[string]string{"capture_ms": ms(passageEpoch.Add(time.Hour))})
	// The sign is refused after the library copy exists.
	prev := inboxCreateSign
	inboxCreateSign = func(*passageStore, replaySign) (replaySign, error) { return replaySign{}, invalidf("refused") }
	t.Cleanup(func() { inboxCreateSign = prev })
	_, body := send(t, fileInboxToPassageHandler, http.MethodPost, `{"items":[{"id":"`+id+`","title":"X"}]}`)
	if body["results"].([]any)[0].(map[string]any)["ok"] == true {
		t.Fatal("should fail")
	}
	if n, _ := w.inbox.Count(); n != 1 {
		t.Errorf("item lost")
	}
	if all, _ := w.docs.List(nil, false, "", 50, 0); len(all) != 0 {
		t.Errorf("a library copy was left behind: %d", len(all))
	}
	if names := dirNames(t, documentsDirPath()); len(names) != 0 {
		t.Errorf("library files left behind: %v", names)
	}
}

// ── file to documents ────────────────────────────────────────────────────

func TestInboxFileToDocuments(t *testing.T) {
	w := newInboxWorld(t)
	folder, err := w.docs.CreateFolder("Receipts", nil)
	if err != nil {
		t.Fatal(err)
	}
	pdf, _ := os.ReadFile(testdataPath("two_page.pdf"))
	_, pb := inboxUpload(t, pdf, "chandlery.pdf", nil)
	pdfID := pb["item"].(map[string]any)["id"].(string)
	photo := uploadForFiling(t, 100, nil)
	missing := uploadForFiling(t, 101, nil)

	req, _ := json.Marshal(map[string]any{"folder_id": folder.ID, "items": []map[string]any{
		{"id": pdfID, "title": "Chandlery May"},
		{"id": photo},
	}})
	code, body := send(t, fileInboxToDocumentsHandler, http.MethodPost, string(req))
	if code != 200 {
		t.Fatal(code, body)
	}
	for i, r := range body["results"].([]any) {
		if r.(map[string]any)["ok"] != true {
			t.Fatalf("item %d: %v", i, r)
		}
	}
	docs, _ := w.docs.List(&folder.ID, false, "", 50, 0)
	if len(docs) != 2 {
		t.Fatalf("%+v", docs)
	}
	titles := map[string]bool{}
	for _, d := range docs {
		titles[d.Title] = true
		if d.Status != "pending" {
			t.Errorf("not queued for the indexer: %+v", d)
		}
	}
	if !titles["Chandlery May"] || !titles[""] {
		t.Errorf("titles %v", titles)
	}
	if n, _ := w.inbox.Count(); n != 1 {
		t.Errorf("count %d", n)
	}

	// A folder that is not there keeps the item.
	code, body = send(t, fileInboxToDocumentsHandler, http.MethodPost, `{"folder_id":"nope","items":[{"id":"`+missing+`"}]}`)
	if code != 200 || body["results"].([]any)[0].(map[string]any)["ok"] == true {
		t.Fatal(code, body)
	}
	if n, _ := w.inbox.Count(); n != 1 {
		t.Errorf("count %d", n)
	}
	// A folder is required.
	if code, _ = send(t, fileInboxToDocumentsHandler, http.MethodPost, `{"items":[{"id":"`+missing+`"}]}`); code != 400 {
		t.Errorf("no folder: %d", code)
	}
}

// ── suggested titles ─────────────────────────────────────────────────────

func TestInboxSuggestedTitlesNameTheNearestPlaceAtCaptureTime(t *testing.T) {
	w := newInboxWorld(t)
	at := passageEpoch.Add(time.Hour)
	addTrack(t, w.passages, at.Add(-time.Minute), -20.25, 149.5, 4)
	var asked [][2]float64
	prev := inboxPlaceLookup
	inboxPlaceLookup = func(lat, lon float64) string { asked = append(asked, [2]float64{lat, lon}); return "Hook Island" }
	t.Cleanup(func() { inboxPlaceLookup = prev })

	withTrack := uploadForFiling(t, 110, map[string]string{"capture_ms": ms(at)})
	noTrack := uploadForFiling(t, 111, map[string]string{"capture_ms": ms(passageEpoch.Add(-48 * time.Hour))})
	noTime := uploadForFiling(t, 112, nil)

	code, body := getJSON(t, inboxSuggestedTitlesHandler, "/api/inbox/suggested-titles?ids="+withTrack+","+noTrack+","+noTime, "")
	if code != 200 {
		t.Fatal(code, body)
	}
	titles := body["titles"].(map[string]any)
	if titles[withTrack] != "Hook Island" || titles[noTrack] != "" || titles[noTime] != "" {
		t.Fatalf("%v", titles)
	}
	if len(asked) != 1 || asked[0] != [2]float64{-20.25, 149.5} {
		t.Errorf("lookups %v", asked)
	}
}

func TestInboxSuggestedTitleIsCutToTheSignLimit(t *testing.T) {
	w := newInboxWorld(t)
	at := passageEpoch.Add(time.Hour)
	addTrack(t, w.passages, at, -20, 149, 4)
	prev := inboxPlaceLookup
	inboxPlaceLookup = func(float64, float64) string { return strings.Repeat("a", 200) }
	t.Cleanup(func() { inboxPlaceLookup = prev })
	id := uploadForFiling(t, 113, map[string]string{"capture_ms": ms(at)})
	_, body := getJSON(t, inboxSuggestedTitlesHandler, "/api/inbox/suggested-titles?ids="+id, "")
	if got := body["titles"].(map[string]any)[id].(string); len(got) != signTitleMax {
		t.Errorf("len %d", len(got))
	}
}

// ── routes ───────────────────────────────────────────────────────────────

func TestInboxRoutesAreAuthTiered(t *testing.T) {
	want := map[string]apiTier{
		"GET /api/inbox":                    tierRead,
		"GET /api/inbox/count":              tierRead,
		"GET /api/inbox/suggested-titles":   tierRead,
		"GET /api/inbox/:id/content":        tierRead,
		"POST /api/inbox":                   tierWrite,
		"POST /api/inbox/bin":               tierWrite,
		"POST /api/inbox/file-to-passage":   tierWrite,
		"POST /api/inbox/file-to-documents": tierWrite,
	}
	e := echo.New()
	for _, r := range inboxRoutes() {
		k := r.Method + " " + r.Path
		if w, ok := want[k]; ok {
			if r.Tier != w {
				t.Errorf("%s tier %v", k, r.Tier)
			}
			delete(want, k)
		} else {
			t.Errorf("unexpected route %s", k)
		}
		e.Add(r.Method, r.Path, func(c echo.Context) error { return nil })
	}
	for k := range want {
		t.Errorf("missing route %s", k)
	}
	c := e.NewContext(httptest.NewRequest(http.MethodGet, "/api/inbox/count", nil), httptest.NewRecorder())
	e.Router().Find(http.MethodGet, "/api/inbox/count", c)
	if c.Path() != "/api/inbox/count" {
		t.Errorf("count resolved to %q", c.Path())
	}
}
