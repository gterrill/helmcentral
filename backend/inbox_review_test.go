package main

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	shaA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	shaB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func fileOne(t *testing.T, id, title string) map[string]any {
	t.Helper()
	code, body := send(t, fileInboxToPassageHandler, http.MethodPost, `{"items":[{"id":"`+id+`","title":"`+title+`"}]}`)
	if code != 200 {
		t.Fatal(code, body)
	}
	return body["results"].([]any)[0].(map[string]any)
}

func TestInboxConcurrentFilingOfOneItemMakesOneSign(t *testing.T) {
	w := newInboxWorld(t)
	seedPassage(t, w.passages, passageEpoch, passageEpoch.Add(2*time.Hour))
	id := uploadForFiling(t, 120, map[string]string{"capture_ms": ms(passageEpoch.Add(time.Hour))})
	var wg sync.WaitGroup
	oks := make(chan bool, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, rec := newDocumentEchoContext(http.MethodPost, "/x", `{"items":[{"id":"`+id+`","title":"Once"}]}`, "")
			_ = fileInboxToPassageHandler(c)
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
	if signs, _ := w.passages.ListSigns(0, 0); n != 1 || len(signs) != 1 {
		t.Fatalf("ok=%d signs=%d", n, len(signs))
	}
}

func TestInboxRetryAfterAFailedClearDoesNotMakeASecondSign(t *testing.T) {
	w := newInboxWorld(t)
	seedPassage(t, w.passages, passageEpoch, passageEpoch.Add(2*time.Hour))
	id := uploadForFiling(t, 121, map[string]string{"capture_ms": ms(passageEpoch.Add(time.Hour))})
	prev := inboxRemoveItem
	inboxRemoveItem = func(*inboxStore, inboxItem) error { return errors.New("disk gone") }
	if r := fileOne(t, id, "First"); r["ok"] == true {
		t.Fatal("clearing failed, so this must report an error")
	}
	inboxRemoveItem = prev
	r := fileOne(t, id, "Second")
	if r["ok"] == true || !strings.Contains(r["error"].(string), "already") {
		t.Fatalf("retry: %v", r)
	}
	if signs, _ := w.passages.ListSigns(0, 0); len(signs) != 1 {
		t.Fatalf("signs %d", len(signs))
	}
}

func TestInboxPhotoInAHiddenPassageIsNotUnderway(t *testing.T) {
	w := newInboxWorld(t)
	p := seedPassage(t, w.passages, passageEpoch, passageEpoch.Add(2*time.Hour))
	if _, err := w.passages.PatchPassage(p.ID, passagePatch{Hidden: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	addTrack(t, w.passages, passageEpoch.Add(time.Hour), -20, 149, 5)
	uploadForFiling(t, 122, map[string]string{"capture_ms": ms(passageEpoch.Add(time.Hour + time.Minute))})
	items := inboxList(t)
	if k := items[0]["group"].(map[string]any)["kind"]; k != "off_passage" {
		t.Errorf("group %v", k)
	}
}

func TestInboxTitlesGiveUpAtTheDeadlineAndLeaveBlanks(t *testing.T) {
	w := newInboxWorld(t)
	at := passageEpoch.Add(time.Hour)
	addTrack(t, w.passages, at, -20, 149, 4)
	release := make(chan struct{})
	prevL, prevD := inboxPlaceLookup, inboxTitleDeadline
	inboxPlaceLookup = func(float64, float64) string { <-release; return "Late" }
	inboxTitleDeadline = 50 * time.Millisecond
	t.Cleanup(func() { close(release); inboxPlaceLookup, inboxTitleDeadline = prevL, prevD })
	id := uploadForFiling(t, 123, map[string]string{"capture_ms": ms(at)})
	start := time.Now()
	code, body := getJSON(t, inboxSuggestedTitlesHandler, "/x?ids="+id, "")
	if code != 200 || body["titles"].(map[string]any)[id] != "" || time.Since(start) > 2*time.Second {
		t.Fatal(code, body, time.Since(start))
	}
}

func TestInboxFilingErrorsDoNotLeakInternalDetail(t *testing.T) {
	w := newInboxWorld(t)
	seedPassage(t, w.passages, passageEpoch, passageEpoch.Add(2*time.Hour))
	id := uploadForFiling(t, 124, map[string]string{"capture_ms": ms(passageEpoch.Add(time.Hour))})
	prev := inboxCreateSign
	inboxCreateSign = func(*passageStore, replaySign) (replaySign, error) {
		return replaySign{}, errors.New("open /srv/helm/data/x.sqlite: permission denied")
	}
	t.Cleanup(func() { inboxCreateSign = prev })
	r := fileOne(t, id, "X")
	msg, _ := r["error"].(string)
	if msg == "" || strings.Contains(msg, "/srv") || strings.Contains(msg, "sqlite") {
		t.Fatalf("error %q", msg)
	}
}

func TestInboxDedupesOnTheOriginalFileHash(t *testing.T) {
	w := newInboxWorld(t)
	seedPassage(t, w.passages, passageEpoch, passageEpoch.Add(2*time.Hour))
	// Two different re-encodes of one original.
	code, body := inboxUpload(t, inboxTestJPEG(t, 130), "a.jpg", map[string]string{"source_sha256": shaA, "capture_ms": ms(passageEpoch.Add(time.Hour))})
	if code != http.StatusCreated {
		t.Fatal(code, body)
	}
	id := body["item"].(map[string]any)["id"].(string)
	code, body = inboxUpload(t, inboxTestJPEG(t, 131), "a2.jpg", map[string]string{"source_sha256": shaA})
	if code != http.StatusOK || body["status"] != "already_uploaded" || body["where"] != "inbox" {
		t.Fatal(code, body)
	}
	if r := fileOne(t, id, "Filed"); r["ok"] != true {
		t.Fatal(r)
	}
	code, body = inboxUpload(t, inboxTestJPEG(t, 132), "a3.jpg", map[string]string{"source_sha256": shaA})
	if code != http.StatusOK || body["status"] != "already_uploaded" || body["where"] != "filed" {
		t.Fatal(code, body)
	}
	// A different original is still new, and a bad hash is refused.
	if code, _ = inboxUpload(t, inboxTestJPEG(t, 133), "b.jpg", map[string]string{"source_sha256": shaB}); code != http.StatusCreated {
		t.Errorf("other original: %d", code)
	}
	if code, _ = inboxUpload(t, inboxTestJPEG(t, 134), "c.jpg", map[string]string{"source_sha256": "xyz"}); code != http.StatusBadRequest {
		t.Errorf("bad hash: %d", code)
	}
}
