package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// resetAnchorWatchLoadError clears the package-level load-error state
// directly, the same way resetAnchorWatchState resets anchorWatchState, so a
// failure recorded by one test never bleeds into the next.
func resetAnchorWatchLoadError(t *testing.T) {
	t.Helper()
	anchorWatchMu.Lock()
	anchorWatchLoadErr = ""
	anchorWatchMu.Unlock()
}

// A corrupt anchor_watch.json must never take the rest of the backend down
// with it — main.go no longer calls log.Fatalf on this path. Instead the
// anchor watch alone goes into an explicit error state, which
// recordAnchorWatchLoadFailure is what puts it into: GET /api/anchor-watch
// must report the error rather than an invented or empty watch.
func TestRecordAnchorWatchLoadFailure_SurfacesOnGet(t *testing.T) {
	resetAnchorWatchState(t)
	resetAnchorWatchLoadError(t)
	original := globalAlarmLogStore
	globalAlarmLogStore = newTestAlarmLog(t)
	t.Cleanup(func() { globalAlarmLogStore = original })

	loadErr := errors.New("parsing anchor watch state (data/anchor_watch.json): unexpected end of JSON input")
	recordAnchorWatchLoadFailure(loadErr)

	e := echo.New()
	rec := httptest.NewRecorder()
	if err := getAnchorWatch(e.NewContext(httptest.NewRequest(http.MethodGet, "/api/anchor-watch", nil), rec)); err != nil {
		t.Fatalf("getAnchorWatch: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if active, _ := resp["active"].(bool); active {
		t.Fatalf("expected active:false while the watch is unreadable, got %+v", resp)
	}
	got, _ := resp["error"].(string)
	if !strings.Contains(got, "data/anchor_watch.json") {
		t.Fatalf("expected the GET error to name the file path, got %q", got)
	}
	if !strings.Contains(got, "unexpected end of JSON input") {
		t.Fatalf("expected the GET error to carry the parse error, got %q", got)
	}
	if _, present := resp["lat"]; present {
		t.Fatalf("expected no invented lat/lon while errored, got %+v", resp)
	}
}

// The warning must go through the same alarm/notification mechanism as every
// other static, non-rule-driven system warning (the collision-profile
// syncer's refusal, the stream watchdog) rather than a silent log line only
// an operator tailing the console would ever see.
func TestRecordAnchorWatchLoadFailure_RaisesAWarningNamingThePath(t *testing.T) {
	resetAnchorWatchState(t)
	resetAnchorWatchLoadError(t)
	store := newTestAlarmLog(t)
	original := globalAlarmLogStore
	globalAlarmLogStore = store
	t.Cleanup(func() { globalAlarmLogStore = original })

	recordAnchorWatchLoadFailure(errors.New("parsing anchor watch state (data/anchor_watch.json): boom"))

	entries, err := store.Recent(10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly one logged warning, got %+v", entries)
	}
	entry := entries[0]
	if entry.State != alarmStateWarn {
		t.Fatalf("expected state %q, got %q", alarmStateWarn, entry.State)
	}
	if !strings.Contains(entry.Message, "data/anchor_watch.json") {
		t.Fatalf("expected the warning to name the file path, got %q", entry.Message)
	}
	if entry.ClearedAt != nil {
		t.Fatalf("expected the warning to still be open, got cleared at %v", entry.ClearedAt)
	}
}

// Recovery is manual — dropping a new anchor or an explicit Raise — never
// automatic. Nothing should call clearAnchorWatchLoadFailure, and therefore
// recordAlarmEvent, when there was never a failure to clear: every ordinary
// Drop and Raise passes through here, and turning that into a "cleared"
// alarm log entry every single time would flood the log with resolutions to
// a problem that never existed. globalAlarmLogStore is left nil so a
// misplaced call would panic instead of passing quietly.
func TestClearAnchorWatchLoadFailure_NoOpWhenNothingWasWrong(t *testing.T) {
	resetAnchorWatchState(t)
	resetAnchorWatchLoadError(t)
	original := globalAlarmLogStore
	globalAlarmLogStore = nil
	t.Cleanup(func() { globalAlarmLogStore = original })

	clearAnchorWatchLoadFailure()

	anchorWatchMu.RLock()
	got := anchorWatchLoadErr
	anchorWatchMu.RUnlock()
	if got != "" {
		t.Fatalf("expected no load error, got %q", got)
	}
}

// The other half: a real failure must actually clear, and the clear itself
// must go through the alarm mechanism too, so a client watching the alarm
// log (not just polling GET /api/anchor-watch) also learns the watch is
// readable again.
func TestClearAnchorWatchLoadFailure_ClearsARealFailure(t *testing.T) {
	resetAnchorWatchState(t)
	resetAnchorWatchLoadError(t)
	store := newTestAlarmLog(t)
	original := globalAlarmLogStore
	globalAlarmLogStore = store
	t.Cleanup(func() { globalAlarmLogStore = original })

	recordAnchorWatchLoadFailure(errors.New("parsing anchor watch state (data/anchor_watch.json): boom"))
	clearAnchorWatchLoadFailure()

	anchorWatchMu.RLock()
	got := anchorWatchLoadErr
	anchorWatchMu.RUnlock()
	if got != "" {
		t.Fatalf("expected the load error to clear, got %q", got)
	}

	entries, err := store.Recent(10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(entries) != 1 || entries[0].ClearedAt == nil {
		t.Fatalf("expected the logged warning to be marked cleared, got %+v", entries)
	}

	e := echo.New()
	rec := httptest.NewRecorder()
	if err := getAnchorWatch(e.NewContext(httptest.NewRequest(http.MethodGet, "/api/anchor-watch", nil), rec)); err != nil {
		t.Fatalf("getAnchorWatch: %v", err)
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if _, present := resp["error"]; present {
		t.Fatalf("expected no error field once cleared, got %+v", resp)
	}
}

// Dropping a new anchor is the operator's ordinary recovery from a corrupt
// anchor_watch.json: setAnchorWatch always writes the file atomically
// (saveAnchorWatch), so a fresh Drop overwrites whatever bad bytes were
// there, and it must also retract the warning the bad file raised.
func TestSetAnchorWatch_DropOverwritesABadFileAndClearsTheWarning(t *testing.T) {
	anchorTestEnv(t, 0)
	resetAnchorWatchState(t)
	resetAnchorWatchLoadError(t)
	store := newTestAlarmLog(t)
	original := globalAlarmLogStore
	globalAlarmLogStore = store
	t.Cleanup(func() { globalAlarmLogStore = original })

	path := anchorWatchFilePath()
	if err := os.WriteFile(path, []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}
	if err := loadAnchorWatch(); err == nil {
		t.Fatalf("expected loadAnchorWatch to fail against the corrupt seed file")
	} else {
		recordAnchorWatchLoadFailure(err)
	}

	code, resp := postAnchorWatch(t, map[string]any{
		"lat": -21.1113,
		"lon": 149.2276,
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200 from the drop, got %d: %+v", code, resp)
	}
	if active, _ := resp["active"].(bool); !active {
		t.Fatalf("expected the drop to install an active watch, got %+v", resp)
	}

	anchorWatchMu.RLock()
	got := anchorWatchLoadErr
	anchorWatchMu.RUnlock()
	if got != "" {
		t.Fatalf("expected the drop to clear the load error, got %q", got)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read anchor watch file after drop: %v", err)
	}
	var onDisk anchorWatchData
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatalf("expected the drop to have overwritten the corrupt file with valid JSON, got parse error: %v (raw: %s)", err, raw)
	}
	if onDisk.Lat != -21.1113 {
		t.Fatalf("expected the on-disk file to hold the new drop, got %+v", onDisk)
	}
}

// An explicit Raise is the operator's other recovery path: raiseAnchorWatch
// already removes anchor_watch.json unconditionally (anchor_raise.go), so it
// must also retract a previously raised "unreadable" warning rather than
// leaving it stuck open against a file that no longer exists.
func TestDeleteAnchorWatch_RemovesABadFileAndClearsTheWarning(t *testing.T) {
	anchorTestEnv(t, 0)
	resetAnchorWatchState(t)
	resetAnchorWatchLoadError(t)
	store := newTestAlarmLog(t)
	original := globalAlarmLogStore
	globalAlarmLogStore = store
	t.Cleanup(func() { globalAlarmLogStore = original })

	path := anchorWatchFilePath()
	if err := os.WriteFile(path, []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}
	if err := loadAnchorWatch(); err == nil {
		t.Fatalf("expected loadAnchorWatch to fail against the corrupt seed file")
	} else {
		recordAnchorWatchLoadFailure(err)
	}

	e := echo.New()
	rec := httptest.NewRecorder()
	if err := deleteAnchorWatch(e.NewContext(httptest.NewRequest(http.MethodDelete, "/api/anchor-watch", nil), rec)); err != nil {
		t.Fatalf("deleteAnchorWatch: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from Raise, got %d: %s", rec.Code, rec.Body.String())
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected the bad file to be removed by Raise, stat err: %v", err)
	}

	anchorWatchMu.RLock()
	got := anchorWatchLoadErr
	anchorWatchMu.RUnlock()
	if got != "" {
		t.Fatalf("expected Raise to clear the load error, got %q", got)
	}
}
