package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

func getInsetView(t *testing.T, query string) (int, map[string]any) {
	t.Helper()
	e := echo.New()
	rec := httptest.NewRecorder()
	c := e.NewContext(httptest.NewRequest(http.MethodGet, "/x"+query, nil), rec)
	if err := getInsetViewHandler(c); err != nil {
		t.Fatal(err)
	}
	out := map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func insetBody(at int64, corner string, offset string) string {
	return `{"at_ms":` + strconv.FormatInt(at, 10) + `,"corner":"` + corner + `","zoom_offset":` + offset + `}`
}

func TestInsetViewDefaultsThenSaved(t *testing.T) {
	store := withPassageStore(t)
	seedPassage(t, store, passageEpoch, passageEpoch.Add(2*time.Hour))
	at := passageEpoch.UnixMilli()

	code, body := getInsetView(t, "?at="+strconv.FormatInt(at, 10))
	v, _ := body["inset_view"].(map[string]any)
	if code != 200 || v["corner"] != "top-right" || v["zoom_offset"] != float64(0) {
		t.Fatalf("defaults: %d %v", code, body)
	}
	if code, body = send(t, putInsetViewHandler, http.MethodPut, insetBody(at, "bottom-left", "-2")); code != 200 {
		t.Fatal(code, body)
	}
	code, body = getInsetView(t, "?at="+strconv.FormatInt(at, 10))
	v, _ = body["inset_view"].(map[string]any)
	if code != 200 || v["corner"] != "bottom-left" || v["zoom_offset"] != float64(-2) {
		t.Fatalf("saved: %d %v", code, body)
	}
	// Saving again replaces rather than adds.
	if code, body = send(t, putInsetViewHandler, http.MethodPut, insetBody(at, "top-left", "3")); code != 200 {
		t.Fatal(code, body)
	}
	code, body = getInsetView(t, "?at="+strconv.FormatInt(at, 10))
	v, _ = body["inset_view"].(map[string]any)
	if v["corner"] != "top-left" || v["zoom_offset"] != float64(3) {
		t.Fatalf("replaced: %d %v", code, body)
	}
}

func TestInsetViewValidation(t *testing.T) {
	store := withPassageStore(t)
	seedPassage(t, store, passageEpoch, passageEpoch.Add(2*time.Hour))
	at := passageEpoch.UnixMilli()
	cases := map[string]string{
		"corner":         insetBody(at, "middle", "0"),
		"empty corner":   insetBody(at, "", "0"),
		"offset high":    insetBody(at, "top-left", "4"),
		"offset low":     insetBody(at, "top-left", "-4"),
		"fractional":     insetBody(at, "top-left", "1.5"),
		"outside":        insetBody(passageEpoch.Add(5*time.Hour).UnixMilli(), "top-left", "0"),
		"missing at":     `{"corner":"top-left","zoom_offset":0}`,
		"missing corner": `{"at_ms":` + strconv.FormatInt(at, 10) + `,"zoom_offset":0}`,
		"missing offset": `{"at_ms":` + strconv.FormatInt(at, 10) + `,"corner":"top-left"}`,
		"unknown":        strings.Replace(insetBody(at, "top-left", "0"), `{`, `{"id":"x",`, 1),
	}
	for name, b := range cases {
		if code, body := send(t, putInsetViewHandler, http.MethodPut, b); code != http.StatusBadRequest {
			t.Errorf("%s: %d %v", name, code, body)
		}
	}
	if code, _ := getInsetView(t, ""); code != http.StatusBadRequest {
		t.Errorf("missing at on read: %d", code)
	}
	if code, _ := getInsetView(t, "?at=1.5"); code != http.StatusBadRequest {
		t.Errorf("fractional at on read: %d", code)
	}
	if code, body := getInsetView(t, "?at="+strconv.FormatInt(at, 10)); code != 200 || body["inset_view"].(map[string]any)["corner"] != "top-right" {
		t.Errorf("rejected writes were stored: %d %v", code, body)
	}
}

func TestInsetViewRoutesAreAuthTiered(t *testing.T) {
	want := map[string]apiTier{
		"GET /api/passages/inset-view": tierRead,
		"PUT /api/passages/inset-view": tierWrite,
	}
	for _, r := range passageRoutes() {
		k := r.Method + " " + r.Path
		if w, ok := want[k]; ok {
			if r.Tier != w {
				t.Errorf("%s tier %v", k, r.Tier)
			}
			delete(want, k)
		}
	}
	for k := range want {
		t.Errorf("missing route %s", k)
	}
}
