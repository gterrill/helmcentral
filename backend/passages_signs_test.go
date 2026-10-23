package main

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

func signBody(tms int64, extra string) string {
	b := `{"t_ms":` + strconv.FormatInt(tms, 10) + `,"title":"Whale"`
	if extra != "" {
		b += "," + extra
	}
	return b + "}"
}

func TestSignCreateListPatchDelete(t *testing.T) {
	store := withPassageStore(t)
	seedPassage(t, store, passageEpoch, passageEpoch.Add(2*time.Hour))
	at := passageEpoch.Add(time.Hour).UnixMilli()

	code, body := send(t, createSignHandler, http.MethodPost, signBody(at, `"note":"Humpback off the bow"`))
	if code != http.StatusCreated {
		t.Fatal(code, body)
	}
	sign := body["sign"].(map[string]any)
	id := sign["id"].(string)
	// Defaults: passing, 4 s, no photo.
	if id == "" || sign["visibility"] != "passing" || sign["show_s"] != float64(4) || sign["note"] != "Humpback off the bow" || sign["document_id"] != nil || sign["photo_url"] != nil {
		t.Fatalf("%v", sign)
	}
	if code, body = send(t, createSignHandler, http.MethodPost, signBody(at-60000, `"visibility":"always"`)); code != http.StatusCreated {
		t.Fatal(code, body)
	}
	got, err := store.ListSigns(0, 0)
	if err != nil || len(got) != 2 || got[0].Visibility != "always" || got[1].ID != id {
		t.Fatalf("%+v %v", got, err)
	}
	if code, body = send(t, patchSignHandler, http.MethodPatch, `{"title":"Humpback","visibility":"after","show_s":9}`, "id", id); code != 200 {
		t.Fatal(code, body)
	}
	s := body["sign"].(map[string]any)
	if s["title"] != "Humpback" || s["visibility"] != "after" || s["show_s"] != float64(9) || s["note"] != "Humpback off the bow" {
		t.Fatalf("%v", s)
	}
	if code, body = send(t, deleteSignHandler, http.MethodDelete, "", "id", id); code != 200 {
		t.Fatal(code, body)
	}
	if code, _ = send(t, deleteSignHandler, http.MethodDelete, "", "id", id); code != 404 {
		t.Errorf("second delete: %d", code)
	}
	if code, _ = send(t, patchSignHandler, http.MethodPatch, `{"title":"x"}`, "id", id); code != 404 {
		t.Errorf("patch gone: %d", code)
	}
}

func TestSignValidation(t *testing.T) {
	store := withPassageStore(t)
	docs := withTestDocumentStore(t)
	seedPassage(t, store, passageEpoch, passageEpoch.Add(2*time.Hour))
	at := passageEpoch.Add(time.Hour).UnixMilli()
	pdf := insertTestDocumentWithFile(t, docs, strings.Repeat("a", 64), "m.pdf", "application/pdf", []byte("x"))
	cases := map[string]string{
		"no title":      `{"t_ms":` + strconv.FormatInt(at, 10) + `}`,
		"blank title":   `{"t_ms":` + strconv.FormatInt(at, 10) + `,"title":"  "}`,
		"long title":    `{"t_ms":` + strconv.FormatInt(at, 10) + `,"title":"` + strings.Repeat("x", 81) + `"}`,
		"long note":     signBody(at, `"note":"`+strings.Repeat("x", 281)+`"`),
		"visibility":    signBody(at, `"visibility":"sometimes"`),
		"show_s low":    signBody(at, `"show_s":0`),
		"show_s high":   signBody(at, `"show_s":31`),
		"outside":       signBody(passageEpoch.Add(5*time.Hour).UnixMilli(), ""),
		"no t_ms":       `{"title":"x"}`,
		"unknown field": signBody(at, `"id":"x"`),
		"missing doc":   signBody(at, `"document_id":"nope"`),
		"not an image":  signBody(at, `"document_id":"`+pdf.ID+`"`),
	}
	for name, b := range cases {
		if code, body := send(t, createSignHandler, http.MethodPost, b); code != http.StatusBadRequest {
			t.Errorf("%s: %d %v", name, code, body)
		}
	}
	if n, _ := store.ListSigns(0, 0); len(n) != 0 {
		t.Errorf("rejected signs were stored: %d", len(n))
	}
	_, body := send(t, createSignHandler, http.MethodPost, signBody(at, ""))
	id := body["sign"].(map[string]any)["id"].(string)
	for _, b := range []string{`{"title":""}`, `{"show_s":99}`, `{"visibility":"x"}`, `{"t_ms":1}`, `{"nope":1}`, `{"document_id":"` + pdf.ID + `"}`} {
		if code, _ := send(t, patchSignHandler, http.MethodPatch, b, "id", id); code != 400 {
			t.Errorf("patch %s: %d", b, code)
		}
	}
	got, _ := store.ListSigns(0, 0)
	if got[0].Title != "Whale" || got[0].ShowS != 4 {
		t.Errorf("%+v", got[0])
	}
}

// A photo is returned only while its document exists; the sign keeps its
// words either way and the stored link survives, since nothing here deletes
// implicitly.
func TestSignPhotoOnlyWhileDocumentExists(t *testing.T) {
	store := withPassageStore(t)
	docs := withTestDocumentStore(t)
	seedPassage(t, store, passageEpoch, passageEpoch.Add(2*time.Hour))
	at := passageEpoch.Add(time.Hour).UnixMilli()
	img := insertTestDocumentWithFile(t, docs, strings.Repeat("b", 64), "w.jpg", "image/jpeg", []byte("x"))

	code, body := send(t, createSignHandler, http.MethodPost, signBody(at, `"document_id":"`+img.ID+`"`))
	if code != 201 {
		t.Fatal(code, body)
	}
	sign := body["sign"].(map[string]any)
	if sign["document_id"] != img.ID || sign["photo_url"] != "/api/documents/"+img.ID+"/content" {
		t.Fatalf("%v", sign)
	}
	id := sign["id"].(string)

	if _, err := docs.Delete(img.ID); err != nil {
		t.Fatal(err)
	}
	code, body = send(t, listSignsHandler, http.MethodGet, "")
	if code != 200 {
		t.Fatal(code, body)
	}
	l := body["signs"].([]any)[0].(map[string]any)
	if l["id"] != id || l["title"] != "Whale" || l["document_id"] != nil || l["photo_url"] != nil {
		t.Fatalf("deleted photo still shown: %v", l)
	}
	// Patching other fields is fine after the photo is gone.
	if code, body = send(t, patchSignHandler, http.MethodPatch, `{"note":"still here"}`, "id", id); code != 200 {
		t.Fatal(code, body)
	}
	if s := body["sign"].(map[string]any); s["document_id"] != nil || s["note"] != "still here" {
		t.Fatalf("%v", s)
	}
	// Removing a photo with null, and setting one.
	img2 := insertTestDocumentWithFile(t, docs, strings.Repeat("c", 64), "x.png", "image/png", []byte("x"))
	if code, body = send(t, patchSignHandler, http.MethodPatch, `{"document_id":"`+img2.ID+`"}`, "id", id); code != 200 || body["sign"].(map[string]any)["document_id"] != img2.ID {
		t.Fatal(code, body)
	}
	if code, body = send(t, patchSignHandler, http.MethodPatch, `{"document_id":null}`, "id", id); code != 200 || body["sign"].(map[string]any)["document_id"] != nil {
		t.Fatal(code, body)
	}
}

// Signs are keyed by real time, so splitting, merging or trimming a passage
// leaves them where they are, and a trim never deletes one.
func TestSignsSurviveSplitMergeAndTrim(t *testing.T) {
	store := withPassageStore(t)
	p := seedPassage(t, store, passageEpoch, passageEpoch.Add(4*time.Hour))
	at := passageEpoch.Add(3 * time.Hour).UnixMilli()
	if code, body := send(t, createSignHandler, http.MethodPost, signBody(at, "")); code != 201 {
		t.Fatal(code, body)
	}
	first, second, err := store.SplitPassage(p.ID, passageEpoch.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := store.ListSigns(0, 0); len(got) != 1 {
		t.Fatalf("after split: %d", len(got))
	}
	if _, err := store.MergePassages(first.ID, second.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.TrimPassage(first.ID, passageEpoch, passageEpoch.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.ListSigns(0, 0); len(got) != 1 || got[0].TMs != at {
		t.Fatalf("after trim: %+v", got)
	}
}

func TestListSignsRange(t *testing.T) {
	store := withPassageStore(t)
	seedPassage(t, store, passageEpoch, passageEpoch.Add(4*time.Hour))
	for _, h := range []int{1, 2, 3} {
		if code, body := send(t, createSignHandler, http.MethodPost, signBody(passageEpoch.Add(time.Duration(h)*time.Hour).UnixMilli(), "")); code != 201 {
			t.Fatal(code, body)
		}
	}
	from := passageEpoch.Add(90 * time.Minute).UnixMilli()
	to := passageEpoch.Add(150 * time.Minute).UnixMilli()
	e := echo.New()
	for query, want := range map[string]int{"?from=bad": 400, "?to=1.5": 400, "?from=" + strconv.FormatInt(from, 10) + "&to=" + strconv.FormatInt(to, 10): 200} {
		rec := httptest.NewRecorder()
		c := e.NewContext(httptest.NewRequest(http.MethodGet, "/x"+query, nil), rec)
		if err := listSignsHandler(c); err != nil || rec.Code != want {
			t.Errorf("%s: %d %v", query, rec.Code, err)
		}
		if want == 200 && strings.Count(rec.Body.String(), `"id"`) != 1 {
			t.Errorf("range body %s", rec.Body.String())
		}
	}
}

func TestSignRoutesAreAuthTieredAndNotShadowed(t *testing.T) {
	want := map[string]apiTier{
		"GET /api/passages/signs":        tierRead,
		"POST /api/passages/signs":       tierWrite,
		"PATCH /api/passages/signs/:id":  tierWrite,
		"DELETE /api/passages/signs/:id": tierWrite,
	}
	e := echo.New()
	for _, r := range passageRoutes() {
		k := r.Method + " " + r.Path
		if w, ok := want[k]; ok {
			if r.Tier != w {
				t.Errorf("%s tier %v", k, r.Tier)
			}
			delete(want, k)
		}
		e.Add(r.Method, r.Path, func(c echo.Context) error { return nil })
	}
	for k := range want {
		t.Errorf("missing route %s", k)
	}
	// /signs must reach its own handler, not GET /api/passages/:id.
	c := e.NewContext(httptest.NewRequest(http.MethodGet, "/api/passages/signs", nil), httptest.NewRecorder())
	e.Router().Find(http.MethodGet, "/api/passages/signs", c)
	if c.Path() != "/api/passages/signs" {
		t.Errorf("GET /api/passages/signs resolved to %q", c.Path())
	}
	c = e.NewContext(httptest.NewRequest(http.MethodPatch, "/api/passages/signs/x", nil), httptest.NewRecorder())
	e.Router().Find(http.MethodPatch, "/api/passages/signs/x", c)
	if c.Path() != "/api/passages/signs/:id" {
		t.Errorf("PATCH sign resolved to %q", c.Path())
	}
}
