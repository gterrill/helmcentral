package main

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Filing to a passage makes the sign inside the library copy's link step, under
// the content lock, as filing to equipment and jobs does (ADR 0282). Made
// afterwards, a failed sign could only be undone by deleting the document, which
// by then another request's link to the same bytes might also depend on.

func TestInboxSignIsMadeUnderTheContentLock(t *testing.T) {
	w := newInboxWorld(t)
	seedPassage(t, w.passages, passageEpoch, passageEpoch.Add(2*time.Hour))
	id := uploadForFiling(t, 120, map[string]string{"capture_ms": ms(passageEpoch.Add(time.Hour))})
	it, _ := w.inbox.Get(id)

	real := inboxCreateSign
	inboxCreateSign = func(s *passageStore, in replaySign) (replaySign, error) {
		acquired := make(chan struct{})
		go func() {
			unlock := lockDocumentSHA(it.SHA256)
			close(acquired)
			unlock()
		}()
		select {
		case <-acquired:
			t.Error("the sign was made without the content lock")
		case <-time.After(200 * time.Millisecond):
		}
		return real(s, in)
	}
	t.Cleanup(func() { inboxCreateSign = real })

	code, body := send(t, fileInboxToPassageHandler, http.MethodPost, `{"items":[{"id":"`+id+`","title":"Humpbacks"}]}`)
	if code != 200 || body["results"].([]any)[0].(map[string]any)["ok"] != true {
		t.Fatal(code, body)
	}
}

// A sign that fails leaves a document that another filing already linked to the
// same bytes exactly as it was.
func TestInboxFailedSignKeepsADocumentSomethingElseUses(t *testing.T) {
	w := newInboxWorld(t)
	seedPassage(t, w.passages, passageEpoch, passageEpoch.Add(2*time.Hour))
	eq, _ := w.docs.CreateEquipment(equipmentItem{Name: "Bilge pump", Category: "general"})
	id := uploadForFiling(t, 121, map[string]string{"capture_ms": ms(passageEpoch.Add(time.Hour))})
	it, _ := w.inbox.Get(id)

	// The same bytes are already in the library and on a piece of equipment.
	existing, created, err := newInboxLibrary(w.docs).copy(it, nil, "", func(documentID string) error { return w.docs.AddEquipmentPhoto(eq.ID, documentID) })
	if err != nil || !created {
		t.Fatal(created, err)
	}

	real := inboxCreateSign
	inboxCreateSign = func(*passageStore, replaySign) (replaySign, error) { return replaySign{}, invalidf("refused") }
	t.Cleanup(func() { inboxCreateSign = real })

	_, body := send(t, fileInboxToPassageHandler, http.MethodPost, `{"items":[{"id":"`+id+`","title":"X"}]}`)
	if body["results"].([]any)[0].(map[string]any)["ok"] == true {
		t.Fatal("the sign was refused, so the filing should fail")
	}
	if _, err := w.docs.Get(existing.ID); err != nil {
		t.Fatalf("the existing document was deleted: %v", err)
	}
	got, _ := w.docs.GetEquipment(eq.ID)
	if len(got.PhotoIDs) != 1 || got.PhotoIDs[0] != existing.ID {
		t.Errorf("the equipment photo was lost: %v", got.PhotoIDs)
	}
	if _, err := os.Stat(filepath.Join(documentsDirPath(), existing.SHA256)); err != nil {
		t.Errorf("its file went: %v", err)
	}
	if _, err := w.inbox.Get(id); err != nil {
		t.Errorf("the item should stay in the Inbox: %v", err)
	}
	if signs, _ := w.passages.ListSigns(0, 0); len(signs) != 0 {
		t.Errorf("signs: %+v", signs)
	}
}

// A sign already shows this photo: filing again finishes the clear and makes no
// second sign, whether or not the sign step is reached.
func TestInboxFilingAnAlreadySignedPhotoClearsItWithoutAnotherSign(t *testing.T) {
	w := newInboxWorld(t)
	seedPassage(t, w.passages, passageEpoch, passageEpoch.Add(2*time.Hour))
	id := uploadForFiling(t, 122, map[string]string{"capture_ms": ms(passageEpoch.Add(time.Hour))})

	prev := inboxRemoveItem
	inboxRemoveItem = func(*inboxStore, inboxItem) error { return errors.New("disk gone") }
	_, body := send(t, fileInboxToPassageHandler, http.MethodPost, `{"items":[{"id":"`+id+`","title":"First"}]}`)
	inboxRemoveItem = prev
	if body["results"].([]any)[0].(map[string]any)["ok"] == true {
		t.Fatal("the clear was made to fail")
	}

	calls := 0
	real := inboxCreateSign
	inboxCreateSign = func(s *passageStore, in replaySign) (replaySign, error) { calls++; return real(s, in) }
	t.Cleanup(func() { inboxCreateSign = real })

	_, body = send(t, fileInboxToPassageHandler, http.MethodPost, `{"items":[{"id":"`+id+`","title":"Second"}]}`)
	res := body["results"].([]any)[0].(map[string]any)
	if res["ok"] != true || res["document_id"] == "" {
		t.Fatal(res)
	}
	if calls != 0 {
		t.Errorf("a second sign was attempted")
	}
	if signs, _ := w.passages.ListSigns(0, 0); len(signs) != 1 {
		t.Errorf("signs: %d", len(signs))
	}
}

// ── a job's status without its photos ────────────────────────────────────

func TestOpenJobStatusReadsOnlyTheStatus(t *testing.T) {
	docs := withTestDocumentStore(t)
	job := mustOpenJob(t, docs, "2026-11-07", openJobReq("Leaking hose"))
	if st, err := docs.OpenJobStatus(job.ID); err != nil || st != openJobOpen {
		t.Fatalf("%q %v", st, err)
	}
	if _, err := docs.DropOpenJobCmd(mustParseDate(t, "2026-11-08"), job.ID, "not needed"); err != nil {
		t.Fatal(err)
	}
	if st, err := docs.OpenJobStatus(job.ID); err != nil || st != openJobDropped {
		t.Fatalf("%q %v", st, err)
	}
	if _, err := docs.OpenJobStatus("ghost"); !errors.Is(err, errOpenJobNotFound) {
		t.Fatalf("got %v", err)
	}
}

// ── the Passage photos folder, one rule ──────────────────────────────────

func TestPassagePhotosFolderEndpointFindsOrCreatesTheFolder(t *testing.T) {
	docs := withTestDocumentStore(t)
	code, first := send(t, passagePhotosFolderHandler, http.MethodPost, "")
	if code != 200 || first["id"] == "" || first["name"] != "Passage photos" {
		t.Fatal(code, first)
	}
	code, again := send(t, passagePhotosFolderHandler, http.MethodPost, "")
	if code != 200 || again["id"] != first["id"] {
		t.Fatalf("a second call made another folder: %v", again)
	}
	if folders, _, _ := docs.ListFolder(nil); len(folders) != 1 {
		t.Errorf("folders: %+v", folders)
	}

	// A folder the operator named with other capitals is the same folder.
	other := withTestDocumentStore(t)
	made, _ := other.CreateFolder("passage PHOTOS", nil)
	_, body := send(t, passagePhotosFolderHandler, http.MethodPost, "")
	if body["id"] != made.ID {
		t.Errorf("did not find the existing folder: %v", body)
	}
}
