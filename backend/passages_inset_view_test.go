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

func insetBody(from int64, corner string, offset string) string {
	return insetBodyRange(from, from+3600_000, corner, offset)
}

func insetBodyRange(from, to int64, corner string, offset string) string {
	return `{"from_ms":` + strconv.FormatInt(from, 10) + `,"to_ms":` + strconv.FormatInt(to, 10) + `,"corner":"` + corner + `","zoom_offset":` + offset + `}`
}

func rangeQuery(from, to int64) string {
	return "?from=" + strconv.FormatInt(from, 10) + "&to=" + strconv.FormatInt(to, 10)
}

func TestInsetViewDefaultsThenSaved(t *testing.T) {
	store := withPassageStore(t)
	seedPassage(t, store, passageEpoch, passageEpoch.Add(2*time.Hour))
	from, to := passageEpoch.UnixMilli(), passageEpoch.Add(2*time.Hour).UnixMilli()

	code, body := getInsetView(t, rangeQuery(from, to))
	v, _ := body["inset_view"].(map[string]any)
	if code != 200 || v["corner"] != "top-right" || v["zoom_offset"] != float64(0) {
		t.Fatalf("defaults: %d %v", code, body)
	}
	if code, body = send(t, putInsetViewHandler, http.MethodPut, insetBodyRange(from, to, "bottom-left", "-2")); code != 200 {
		t.Fatal(code, body)
	}
	code, body = getInsetView(t, rangeQuery(from, to))
	v, _ = body["inset_view"].(map[string]any)
	if code != 200 || v["corner"] != "bottom-left" || v["zoom_offset"] != float64(-2) {
		t.Fatalf("saved: %d %v", code, body)
	}
	if code, body = send(t, putInsetViewHandler, http.MethodPut, insetBodyRange(from, to, "top-left", "3")); code != 200 {
		t.Fatal(code, body)
	}
	code, body = getInsetView(t, rangeQuery(from, to))
	v, _ = body["inset_view"].(map[string]any)
	if v["corner"] != "top-left" || v["zoom_offset"] != float64(3) {
		t.Fatalf("replaced: %d %v", code, body)
	}
}

// A passage and a cruise that starts with it are different replays.
func TestInsetViewIsKeyedByTheWholeRange(t *testing.T) {
	store := withPassageStore(t)
	seedPassage(t, store, passageEpoch, passageEpoch.Add(2*time.Hour))
	seedPassage(t, store, passageEpoch.Add(24*time.Hour), passageEpoch.Add(26*time.Hour))
	from := passageEpoch.UnixMilli()
	oneEnd, cruiseEnd := passageEpoch.Add(2*time.Hour).UnixMilli(), passageEpoch.Add(26*time.Hour).UnixMilli()

	if code, body := send(t, putInsetViewHandler, http.MethodPut, insetBodyRange(from, oneEnd, "bottom-right", "2")); code != 200 {
		t.Fatal(code, body)
	}
	_, body := getInsetView(t, rangeQuery(from, cruiseEnd))
	if v := body["inset_view"].(map[string]any); v["corner"] != "top-right" || v["zoom_offset"] != float64(0) {
		t.Fatalf("cruise borrowed the passage's setting: %v", v)
	}
	if code, body := send(t, putInsetViewHandler, http.MethodPut, insetBodyRange(from, cruiseEnd, "top-left", "-1")); code != 200 {
		t.Fatal(code, body)
	}
	_, body = getInsetView(t, rangeQuery(from, oneEnd))
	if v := body["inset_view"].(map[string]any); v["corner"] != "bottom-right" || v["zoom_offset"] != float64(2) {
		t.Fatalf("passage lost its setting: %v", v)
	}
}

func TestInsetViewValidation(t *testing.T) {
	store := withPassageStore(t)
	seedPassage(t, store, passageEpoch, passageEpoch.Add(2*time.Hour))
	from, to := passageEpoch.UnixMilli(), passageEpoch.Add(2*time.Hour).UnixMilli()
	f, tt := strconv.FormatInt(from, 10), strconv.FormatInt(to, 10)
	ok := func(corner, offset string) string { return insetBodyRange(from, to, corner, offset) }
	cases := map[string]string{
		"corner":         ok("middle", "0"),
		"empty corner":   ok("", "0"),
		"offset high":    ok("top-left", "4"),
		"offset low":     ok("top-left", "-4"),
		"fractional":     ok("top-left", "1.5"),
		"start outside":  insetBodyRange(passageEpoch.Add(5*time.Hour).UnixMilli(), passageEpoch.Add(6*time.Hour).UnixMilli(), "top-left", "0"),
		"end outside":    insetBodyRange(from, passageEpoch.Add(5*time.Hour).UnixMilli(), "top-left", "0"),
		"end before":     insetBodyRange(to, from, "top-left", "0"),
		"missing from":   `{"to_ms":` + tt + `,"corner":"top-left","zoom_offset":0}`,
		"missing to":     `{"from_ms":` + f + `,"corner":"top-left","zoom_offset":0}`,
		"missing corner": `{"from_ms":` + f + `,"to_ms":` + tt + `,"zoom_offset":0}`,
		"missing offset": `{"from_ms":` + f + `,"to_ms":` + tt + `,"corner":"top-left"}`,
		"unknown":        strings.Replace(ok("top-left", "0"), `{`, `{"id":"x",`, 1),
	}
	for name, b := range cases {
		if code, body := send(t, putInsetViewHandler, http.MethodPut, b); code != http.StatusBadRequest {
			t.Errorf("%s: %d %v", name, code, body)
		}
	}
	for name, q := range map[string]string{"none": "", "no to": "?from=" + f, "no from": "?to=" + tt, "fractional": "?from=1.5&to=2", "reversed": rangeQuery(to, from)} {
		if code, _ := getInsetView(t, q); code != http.StatusBadRequest {
			t.Errorf("read %s: %d", name, code)
		}
	}
	if code, body := getInsetView(t, rangeQuery(from, to)); code != 200 || body["inset_view"].(map[string]any)["corner"] != "top-right" {
		t.Errorf("rejected writes were stored: %d %v", code, body)
	}
}

// A database that fails is a server fault, not the operator's bad request.
func TestInsetViewDatabaseFailureIs500(t *testing.T) {
	store := withPassageStore(t)
	seedPassage(t, store, passageEpoch, passageEpoch.Add(2*time.Hour))
	from, to := passageEpoch.UnixMilli(), passageEpoch.Add(2*time.Hour).UnixMilli()
	_ = store.db.Close()
	if code, body := send(t, putInsetViewHandler, http.MethodPut, insetBodyRange(from, to, "top-left", "0")); code != http.StatusInternalServerError {
		t.Errorf("put: %d %v", code, body)
	}
	if code, _ := getInsetView(t, rangeQuery(from, to)); code != http.StatusInternalServerError {
		t.Errorf("get: %d", code)
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
