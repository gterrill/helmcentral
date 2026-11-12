package main

import (
	"bytes"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// inboxRouteHandler finds a handler through the route table, so a test names
// the URL the phone calls rather than a Go symbol.
func inboxRouteHandler(t *testing.T, method, path string) echo.HandlerFunc {
	t.Helper()
	for _, r := range inboxRoutes() {
		if r.Method == method && r.Path == path {
			return r.Handler
		}
	}
	t.Fatalf("no route %s %s", method, path)
	return nil
}

func TestInboxKnownAnswersTheOriginalsAlreadyHeld(t *testing.T) {
	w := newInboxWorld(t)
	known := inboxRouteHandler(t, http.MethodPost, "/api/inbox/known")
	if code, body := inboxUpload(t, inboxTestJPEG(t, 140), "a.jpg", map[string]string{"source_sha256": shaA}); code != http.StatusCreated {
		t.Fatal(code, body)
	}
	if err := w.inbox.MarkFiled(shaB, ""); err != nil {
		t.Fatal(err)
	}
	shaC := strings.Repeat("c", 64)
	code, body := send(t, known, http.MethodPost, `{"hashes":["`+shaA+`","`+shaB+`","`+shaC+`"]}`)
	if code != 200 {
		t.Fatal(code, body)
	}
	got := map[string]bool{}
	for _, h := range body["known"].([]any) {
		got[h.(string)] = true
	}
	if len(got) != 2 || !got[shaA] || !got[shaB] {
		t.Fatalf("known %v", got)
	}

	// Nothing known is an empty list, not null.
	if code, body = send(t, known, http.MethodPost, `{"hashes":["`+shaC+`"]}`); code != 200 {
		t.Fatal(code, body)
	}
	if list, ok := body["known"].([]any); !ok || len(list) != 0 {
		t.Fatalf("known %v", body["known"])
	}
	for name, req := range map[string]string{
		"empty":    `{"hashes":[]}`,
		"not hex":  `{"hashes":["xyz"]}`,
		"too many": `{"hashes":["` + strings.Repeat(shaC+`","`, 500) + shaC + `"]}`,
	} {
		if code, _ := send(t, known, http.MethodPost, req); code != http.StatusBadRequest {
			t.Errorf("%s: %d", name, code)
		}
	}
}

func TestInboxThumbnailIsStoredServedAndRemoved(t *testing.T) {
	w := newInboxWorld(t)
	seedPassage(t, w.passages, passageEpoch, passageEpoch.Add(2*time.Hour))
	thumbHandler := inboxRouteHandler(t, http.MethodGet, "/api/inbox/:id/thumb")
	at := map[string]string{"capture_ms": ms(passageEpoch.Add(time.Hour))}
	withThumb := func(shade uint8, thumb []byte) (string, map[string]any) {
		f := map[string]string{"capture_ms": at["capture_ms"], "thumb": string(thumb)}
		code, body := inboxUpload(t, inboxTestJPEG(t, shade), "a.jpg", f)
		if code != http.StatusCreated {
			t.Fatal(code, body)
		}
		item := body["item"].(map[string]any)
		return item["id"].(string), item
	}
	thumb := inboxTestJPEG(t, 7)
	id, item := withThumb(150, thumb)
	if item["thumb_url"] != "/api/inbox/"+id+"/thumb" {
		t.Fatalf("thumb_url %v", item["thumb_url"])
	}
	if listed := inboxList(t); listed[0]["thumb_url"] != item["thumb_url"] {
		t.Errorf("list thumb_url %v", listed[0]["thumb_url"])
	}
	c, rec := newDocumentEchoContext(http.MethodGet, "/x", "", id)
	if err := thumbHandler(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 200 || !bytes.Equal(rec.Body.Bytes(), thumb) || rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("%d %q", rec.Code, rec.Header().Get("Content-Type"))
	}

	// No thumbnail, no thumb_url, and the thumb route says so.
	pdf, _ := os.ReadFile(testdataPath("two_page.pdf"))
	_, pb := inboxUpload(t, pdf, "r.pdf", nil)
	pdfItem := pb["item"].(map[string]any)
	if url, _ := pdfItem["thumb_url"].(string); url != "" {
		t.Errorf("a PDF has no thumb_url, got %q", url)
	}
	c, rec = newDocumentEchoContext(http.MethodGet, "/x", "", pdfItem["id"].(string))
	_ = thumbHandler(c)
	if rec.Code != 404 {
		t.Errorf("thumb of a PDF: %d", rec.Code)
	}

	// A thumbnail that is not a JPEG is refused and leaves nothing behind.
	if code, _ := inboxUpload(t, inboxTestJPEG(t, 151), "b.jpg", map[string]string{"thumb": "not an image"}); code != http.StatusBadRequest {
		t.Errorf("bad thumb: %d", code)
	}

	// Filing removes the thumbnail with the item.
	if r := fileOne(t, id, "Whales"); r["ok"] != true {
		t.Fatal(r)
	}
	// Binning does too.
	binID, _ := withThumb(152, inboxTestJPEG(t, 8))
	if code, _ := send(t, binInboxHandler, http.MethodPost, `{"ids":["`+binID+`"]}`); code != 200 {
		t.Fatal(code)
	}
	// Only the PDF is still waiting.
	if left := dirNames(t, inboxDirPath()); len(left) != 1 {
		t.Errorf("files left in the inbox dir: %v", left)
	}
}
