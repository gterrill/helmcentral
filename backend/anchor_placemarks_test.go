package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// placemarkTestEnv isolates both the anchor-watch and placemark state files,
// and clears the package-level state both ways so placemarks never bleed
// between tests in this binary.
func placemarkTestEnv(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("ANCHOR_WATCH_FILE", filepath.Join(dir, "anchor_watch.json"))
	t.Setenv("ANCHOR_PLACEMARKS_FILE", filepath.Join(dir, "anchor_placemarks.json"))

	reset := func() {
		anchorWatchMu.Lock()
		anchorWatchState = nil
		anchorWatchMu.Unlock()
		placemarkMu.Lock()
		placemarks = nil
		placemarkMu.Unlock()
	}
	reset()
	t.Cleanup(reset)
}

// activateAnchorWatch sets the package-level watch state directly, which is
// all the placemark handlers care about — it avoids dragging the bow-offset
// and SignalK plumbing of setAnchorWatch into these tests.
func activateAnchorWatch(t *testing.T) {
	t.Helper()
	anchorWatchMu.Lock()
	anchorWatchState = &anchorWatchData{Lat: -21.1113, Lon: 149.2276, RadiusMeters: 30}
	anchorWatchMu.Unlock()
}

func callPlacemarks(t *testing.T, method, target, body string, handler echo.HandlerFunc, paramNames, paramValues []string) (int, map[string]any) {
	t.Helper()
	e := echo.New()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, target, nil)
	} else {
		req = httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	}
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	if len(paramNames) > 0 {
		c.SetParamNames(paramNames...)
		c.SetParamValues(paramValues...)
	}
	if err := handler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	var decoded map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &decoded)
	return rec.Code, decoded
}

func createPlacemark(t *testing.T, lat, lon float64, label string) (int, map[string]any) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"lat": lat, "lon": lon, "label": label})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return callPlacemarks(t, http.MethodPost, "/api/anchor-watch/placemarks", string(body), createPlacemarkHandler, nil, nil)
}

func listPlacemarks(t *testing.T) (int, map[string]any) {
	t.Helper()
	return callPlacemarks(t, http.MethodGet, "/api/anchor-watch/placemarks", "", listPlacemarksHandler, nil, nil)
}

func placemarkList(t *testing.T, resp map[string]any) []any {
	t.Helper()
	raw, ok := resp["placemarks"]
	if !ok {
		t.Fatalf("response has no placemarks key: %v", resp)
	}
	if raw == nil {
		return nil
	}
	list, ok := raw.([]any)
	if !ok {
		t.Fatalf("placemarks is not a list: %T", raw)
	}
	return list
}

// A placemark created during an active watch is listed back, with a
// server-assigned id and the exact coordinates posted.
func TestCreatePlacemark_IsListedBack(t *testing.T) {
	placemarkTestEnv(t)
	activateAnchorWatch(t)

	code, created := createPlacemark(t, -21.1120, 149.2280, "kelp patch")
	if code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%v)", code, created)
	}
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("expected a server-assigned id, got %v", created)
	}

	code, resp := listPlacemarks(t)
	if code != http.StatusOK {
		t.Fatalf("expected 200 from list, got %d", code)
	}
	list := placemarkList(t, resp)
	if len(list) != 1 {
		t.Fatalf("expected 1 placemark, got %d (%v)", len(list), resp)
	}
	got, _ := list[0].(map[string]any)
	if got["id"] != id {
		t.Fatalf("listed id %v != created id %v", got["id"], id)
	}
	if got["lat"] != -21.1120 || got["lon"] != 149.2280 {
		t.Fatalf("coordinates round-tripped wrong: %v", got)
	}
	if got["label"] != "kelp patch" {
		t.Fatalf("label round-tripped wrong: %v", got)
	}
}

// Placemarks are session-bound: with no anchor watch running there is no
// session to bind to, so the create fails loudly rather than storing an
// orphan the next session would inherit.
func TestCreatePlacemark_RejectedWithoutActiveWatch(t *testing.T) {
	placemarkTestEnv(t)

	code, resp := createPlacemark(t, -21.1120, 149.2280, "")
	if code != http.StatusConflict {
		t.Fatalf("expected 409 without an active watch, got %d (%v)", code, resp)
	}

	_, listResp := listPlacemarks(t)
	if n := len(placemarkList(t, listResp)); n != 0 {
		t.Fatalf("expected nothing stored, got %d", n)
	}
}

func TestCreatePlacemark_RejectsOutOfRangeCoordinates(t *testing.T) {
	placemarkTestEnv(t)
	activateAnchorWatch(t)

	code, resp := createPlacemark(t, 91, 0, "")
	if code != http.StatusBadRequest {
		t.Fatalf("expected 400 for lat 91, got %d (%v)", code, resp)
	}
	code, resp = createPlacemark(t, 0, 181, "")
	if code != http.StatusBadRequest {
		t.Fatalf("expected 400 for lon 181, got %d (%v)", code, resp)
	}
}

// Removing one placemark leaves the others alone.
func TestDeletePlacemark_RemovesOnlyThatOne(t *testing.T) {
	placemarkTestEnv(t)
	activateAnchorWatch(t)

	_, first := createPlacemark(t, -21.1120, 149.2280, "a")
	_, second := createPlacemark(t, -21.1130, 149.2290, "b")
	firstID, _ := first["id"].(string)
	secondID, _ := second["id"].(string)

	code, _ := callPlacemarks(t, http.MethodDelete, "/api/anchor-watch/placemarks/"+firstID, "",
		deletePlacemarkHandler, []string{"id"}, []string{firstID})
	if code != http.StatusOK {
		t.Fatalf("expected 200 from delete, got %d", code)
	}

	_, resp := listPlacemarks(t)
	list := placemarkList(t, resp)
	if len(list) != 1 {
		t.Fatalf("expected 1 remaining, got %d (%v)", len(list), resp)
	}
	remaining, _ := list[0].(map[string]any)
	if remaining["id"] != secondID {
		t.Fatalf("wrong one survived: %v", remaining)
	}
}

func TestDeletePlacemark_UnknownIDIs404(t *testing.T) {
	placemarkTestEnv(t)
	activateAnchorWatch(t)

	code, resp := callPlacemarks(t, http.MethodDelete, "/api/anchor-watch/placemarks/nope", "",
		deletePlacemarkHandler, []string{"id"}, []string{"nope"})
	if code != http.StatusNotFound {
		t.Fatalf("expected 404 for an unknown id, got %d (%v)", code, resp)
	}
}

// Ending the anchor session drops every placemark with it — that is what
// "session bound" means — and takes the state file with it.
func TestDeleteAnchorWatch_ClearsPlacemarks(t *testing.T) {
	anchorTestEnv(t, 0)
	placemarkTestEnv(t)
	activateAnchorWatch(t)
	createPlacemark(t, -21.1120, 149.2280, "a")
	createPlacemark(t, -21.1130, 149.2290, "b")

	e := echo.New()
	rec := httptest.NewRecorder()
	if err := deleteAnchorWatch(e.NewContext(httptest.NewRequest(http.MethodDelete, "/api/anchor-watch", nil), rec)); err != nil {
		t.Fatalf("deleteAnchorWatch: %v", err)
	}

	_, resp := listPlacemarks(t)
	if n := len(placemarkList(t, resp)); n != 0 {
		t.Fatalf("expected placemarks cleared with the session, got %d", n)
	}
	if _, err := os.Stat(anchorPlacemarksFilePath()); !os.IsNotExist(err) {
		t.Fatalf("expected the placemark file removed, stat err = %v", err)
	}
}

// Placemarks survive a backend restart mid-session, the same way the anchor
// watch itself does — the session is still running, so the pins still apply.
func TestLoadPlacemarks_SurvivesRestartDuringSession(t *testing.T) {
	placemarkTestEnv(t)
	activateAnchorWatch(t)
	_, created := createPlacemark(t, -21.1120, 149.2280, "kelp patch")
	id, _ := created["id"].(string)

	// Simulate a restart: drop in-memory state, keep the watch active.
	placemarkMu.Lock()
	placemarks = nil
	placemarkMu.Unlock()
	loadAnchorPlacemarks()

	_, resp := listPlacemarks(t)
	list := placemarkList(t, resp)
	if len(list) != 1 {
		t.Fatalf("expected the placemark to survive the restart, got %d", len(list))
	}
	got, _ := list[0].(map[string]any)
	if got["id"] != id {
		t.Fatalf("restored the wrong placemark: %v", got)
	}
}

// A stale file left behind by a session that ended while the backend was
// down must not resurrect into the next session.
func TestLoadPlacemarks_DiscardsFileWhenNoWatchActive(t *testing.T) {
	placemarkTestEnv(t)
	activateAnchorWatch(t)
	createPlacemark(t, -21.1120, 149.2280, "a")

	anchorWatchMu.Lock()
	anchorWatchState = nil
	anchorWatchMu.Unlock()
	placemarkMu.Lock()
	placemarks = nil
	placemarkMu.Unlock()

	loadAnchorPlacemarks()

	_, resp := listPlacemarks(t)
	if n := len(placemarkList(t, resp)); n != 0 {
		t.Fatalf("expected a stale placemark file to be discarded, got %d", n)
	}
	if _, err := os.Stat(anchorPlacemarksFilePath()); !os.IsNotExist(err) {
		t.Fatalf("expected the stale file removed, stat err = %v", err)
	}
}
