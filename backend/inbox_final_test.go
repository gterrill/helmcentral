package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// oneJSONBody fails unless the response holds exactly one JSON value.
func oneJSONBody(t *testing.T, raw string) map[string]any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(raw))
	var out map[string]any
	if err := dec.Decode(&out); err != nil {
		t.Fatalf("body does not parse: %v: %q", err, raw)
	}
	if dec.More() {
		t.Fatalf("more than one JSON body: %q", raw)
	}
	return out
}

func TestInboxUploadAnswersOnceWhenThereIsNoFile(t *testing.T) {
	newInboxWorld(t)
	c, rec := newDocumentUploadContext(t, []documentUploadField{{name: "capture_ms", content: []byte("1")}})
	if err := uploadInboxHandler(c); err != nil {
		t.Fatal(err)
	}
	body := oneJSONBody(t, rec.Body.String())
	if rec.Code != http.StatusBadRequest || body["error"] != "file is required" {
		t.Fatal(rec.Code, body)
	}
}

func TestInboxUploadAnswersOnceWhenTheFileIsTooBig(t *testing.T) {
	newInboxWorld(t)
	prev := documentMaxUploadBytes
	documentMaxUploadBytes = 200
	t.Cleanup(func() { documentMaxUploadBytes = prev })
	c, rec := newDocumentUploadContext(t, []documentUploadField{{name: "file", filename: "big.jpg", content: make([]byte, 5000)}})
	if err := uploadInboxHandler(c); err != nil {
		t.Fatal(err)
	}
	oneJSONBody(t, rec.Body.String())
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatal(rec.Code, rec.Body.String())
	}
}

func TestInboxFilingReportsADatabaseErrorAsAFailureNotAMissingPassage(t *testing.T) {
	w := newInboxWorld(t)
	seedPassage(t, w.passages, passageEpoch, passageEpoch.Add(2*time.Hour))
	id := uploadForFiling(t, 130, map[string]string{"capture_ms": ms(passageEpoch.Add(time.Hour))})
	if err := w.passages.db.Close(); err != nil {
		t.Fatal(err)
	}
	r := fileOne(t, id, "Broken")
	msg, _ := r["error"].(string)
	if r["ok"] == true || strings.Contains(msg, "not inside") || !strings.Contains(msg, "log says why") {
		t.Fatalf("%v", r)
	}
}

func TestInboxBinDoesNotCallAnItemFiledInBetweenBusy(t *testing.T) {
	w := newInboxWorld(t)
	busyID := uploadForFiling(t, 131, nil)
	goneID := uploadForFiling(t, 132, nil)
	gone, err := w.inbox.Get(goneID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.inbox.Claim(busyID); err != nil {
		t.Fatal(err)
	}
	// Another request holds the content lock while it finishes filing, so the
	// bin has read the item before the filing clears it.
	unlock := lockDocumentSHA(gone.SHA256)
	done := make(chan map[string]any, 1)
	go func() {
		_, body := send(t, binInboxHandler, http.MethodPost, `{"ids":["`+busyID+`","`+goneID+`"]}`)
		done <- body
	}()
	time.Sleep(300 * time.Millisecond)
	if err := w.inbox.Delete(goneID); err != nil {
		t.Fatal(err)
	}
	unlock()
	body := <-done
	busy, _ := body["busy"].([]any)
	if len(busy) != 1 || busy[0].(map[string]any)["id"] != busyID || body["deleted"] != float64(0) {
		t.Fatalf("%v", body)
	}
}

func TestInboxGPSMismatchAllowsForHowFarTheBoatMovedSinceTheFix(t *testing.T) {
	w := newInboxWorld(t)
	fixAt := passageEpoch.Add(time.Hour)
	addTrack(t, w.passages, fixAt, -20, 149, 7)
	at := ms(fixAt.Add(10 * time.Minute))
	// 7 kn for 10 minutes is 1.17 nm; the allowance is 1 nm plus that.
	ahead := uploadForFiling(t, 133, map[string]string{"capture_ms": at, "phone_lat": "-19.98", "phone_lon": "149"}) // 1.2 nm
	off := uploadForFiling(t, 134, map[string]string{"capture_ms": at, "phone_lat": "-19.9167", "phone_lon": "149"}) // 5 nm
	got := map[string]map[string]any{}
	for _, it := range inboxList(t) {
		got[it["id"].(string)] = it
	}
	if got[ahead]["gps_mismatch"] == true {
		t.Errorf("a phone 1.2 nm ahead of a 7 kn boat must not warn")
	}
	if got[off]["gps_mismatch"] != true {
		t.Errorf("a phone 5 nm off must warn")
	}
}
