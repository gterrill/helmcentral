package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

func anchorTrailEnv(t *testing.T) *passageStore {
	t.Helper()
	store := withPassageStore(t)
	anchorPublishEnv(t)
	return store
}

func getAnchorTrailBody(t *testing.T, since string) (int, struct {
	Points         []trackPoint `json:"points"`
	TrailStartedAt *string      `json:"trail_started_at"`
}) {
	t.Helper()
	target := "/api/anchor-watch/trails/self"
	if since != "" {
		target += "?since=" + since
	}
	rec := httptest.NewRecorder()
	if err := getSelfTrailHandler(echo.New().NewContext(httptest.NewRequest(http.MethodGet, target, nil), rec)); err != nil {
		t.Fatal(err)
	}
	var body struct {
		Points         []trackPoint `json:"points"`
		TrailStartedAt *string      `json:"trail_started_at"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body
}

func TestAnchorTrailNotRecordedWithoutWatch(t *testing.T) {
	store := anchorTrailEnv(t)
	recordAnchorTrailPoint(-20.0001, 149.0001, time.Now().UTC())
	var n int
	_ = store.db.QueryRow(`SELECT COUNT(*) FROM anchor_trail_points`).Scan(&n)
	if n != 0 {
		t.Fatalf("expected no rows without a watch, got %d", n)
	}
	code, body := getAnchorTrailBody(t, "")
	if code != http.StatusOK || len(body.Points) != 0 || body.TrailStartedAt != nil {
		t.Fatalf("expected empty trail, got %d %+v", code, body)
	}
}

func TestAnchorTrailRecordsAndServesSinceDrop(t *testing.T) {
	anchorTrailEnv(t)
	if code, resp := postAnchorWatch(t, map[string]any{"lat": -20.0, "lon": 149.0}); code != http.StatusOK {
		t.Fatalf("drop: %d %v", code, resp)
	}
	t0 := time.Now().UTC().Truncate(time.Millisecond)
	recordAnchorTrailPoint(-20.0001, 149.0001, t0.Add(time.Second))
	recordAnchorTrailPoint(-20.0002, 149.0002, t0.Add(6*time.Second))
	recordAnchorTrailPoint(-1, -1, t0.Add(11*time.Second)) // no-fix sentinel

	_, body := getAnchorTrailBody(t, "")
	if len(body.Points) != 2 || body.TrailStartedAt == nil {
		t.Fatalf("expected 2 points and a start, got %+v", body)
	}
	if body.Points[0].Lat != -20.0001 || body.Points[1].Lat != -20.0002 {
		t.Fatalf("points out of order: %+v", body.Points)
	}
	since := body.Points[0].Timestamp.UTC().Format(time.RFC3339Nano)
	_, inc := getAnchorTrailBody(t, since)
	if len(inc.Points) != 1 || inc.Points[0].Lat != -20.0002 {
		t.Fatalf("expected only the newer point after since, got %+v", inc.Points)
	}
}

func TestAnchorTrailResetsOnPositionChangeNotRadius(t *testing.T) {
	anchorTrailEnv(t)
	if code, resp := postAnchorWatch(t, map[string]any{"lat": -20.0, "lon": 149.0}); code != http.StatusOK {
		t.Fatalf("drop: %d %v", code, resp)
	}
	now := time.Now().UTC()
	recordAnchorTrailPoint(-20.0001, 149.0001, now.Add(time.Second))
	_, before := getAnchorTrailBody(t, "")

	if code, resp := patchAnchorWatchForTest(t, map[string]any{"radius_meters": 40.0}); code != http.StatusOK {
		t.Fatalf("radius patch: %d %v", code, resp)
	}
	_, same := getAnchorTrailBody(t, "")
	if len(same.Points) != 1 || *same.TrailStartedAt != *before.TrailStartedAt {
		t.Fatalf("radius-only patch must keep the trail: %+v", same)
	}

	time.Sleep(5 * time.Millisecond)
	if code, resp := patchAnchorWatchForTest(t, map[string]any{"lat": -20.5, "lon": 149.5}); code != http.StatusOK {
		t.Fatalf("move patch: %d %v", code, resp)
	}
	_, after := getAnchorTrailBody(t, "")
	if len(after.Points) != 0 || after.TrailStartedAt == nil || *after.TrailStartedAt == *before.TrailStartedAt {
		t.Fatalf("expected a restarted trail with a new start, got %+v (before %v)", after, *before.TrailStartedAt)
	}
}

func TestAnchorTrailRepositionByPostRestarts(t *testing.T) {
	anchorTrailEnv(t)
	postAnchorWatch(t, map[string]any{"lat": -20.0, "lon": 149.0})
	recordAnchorTrailPoint(-20.0001, 149.0001, time.Now().UTC().Add(time.Second))
	if code, resp := postAnchorWatch(t, map[string]any{"lat": -20.2, "lon": 149.2}); code != http.StatusOK {
		t.Fatalf("reposition: %d %v", code, resp)
	}
	if _, body := getAnchorTrailBody(t, ""); len(body.Points) != 0 {
		t.Fatalf("expected trail cleared by re-POST, got %+v", body.Points)
	}
}

func TestAnchorTrailClearedOnRaise(t *testing.T) {
	store := anchorTrailEnv(t)
	postAnchorWatch(t, map[string]any{"lat": -20.0, "lon": 149.0})
	recordAnchorTrailPoint(-20.0001, 149.0001, time.Now().UTC().Add(time.Second))
	if rec := raiseAnchor(t); rec.Code != http.StatusOK {
		t.Fatalf("raise: %d", rec.Code)
	}
	var n int
	_ = store.db.QueryRow(`SELECT COUNT(*) FROM anchor_trail_points`).Scan(&n)
	if n != 0 {
		t.Fatalf("expected the table emptied by raise, got %d", n)
	}
}

func TestAnchorTrailStalePointDroppedAfterReset(t *testing.T) {
	store := anchorTrailEnv(t)
	postAnchorWatch(t, map[string]any{"lat": -20.0, "lon": 149.0})
	// A tick sampled before the reset but written after it must not land in the new trail.
	recordAnchorTrailPoint(-20.0001, 149.0001, time.Now().UTC().Add(-time.Hour))
	var n int
	_ = store.db.QueryRow(`SELECT COUNT(*) FROM anchor_trail_points`).Scan(&n)
	if n != 0 {
		t.Fatalf("expected the stale point dropped, got %d", n)
	}
}

func TestAnchorTrailKeepsOnlyTheNewestPoints(t *testing.T) {
	store := anchorTrailEnv(t)
	postAnchorWatch(t, map[string]any{"lat": -20.0, "lon": 149.0})
	base := time.Now().UTC().Add(time.Second)
	tx, _ := store.db.Begin()
	for i := 0; i < anchorTrailMaxPoints+50; i++ {
		_, _ = tx.Exec(`INSERT INTO anchor_trail_points (t_ms, lat, lon) VALUES (?,?,?)`, base.Add(time.Duration(i)*time.Second).UnixMilli(), -20.0, 149.0)
	}
	_ = tx.Commit()
	now := time.Now().UTC().Add(4 * time.Hour) // after the preloaded rows
	for i := 0; i < 2*anchorTrailPruneEvery; i++ {
		recordAnchorTrailPoint(-20.0-float64(i)*1e-4, 149.0, now.Add(time.Duration(i+1)*time.Second))
	}
	var n int
	_ = store.db.QueryRow(`SELECT COUNT(*) FROM anchor_trail_points`).Scan(&n)
	// Trimmed every anchorTrailPruneEvery stores, so it may sit that far over.
	if n > anchorTrailMaxPoints+anchorTrailPruneEvery {
		t.Fatalf("expected the table trimmed to about %d, got %d", anchorTrailMaxPoints, n)
	}
}

func TestAnchorTrailThinsFixesThatHaveNotMoved(t *testing.T) {
	store := anchorTrailEnv(t)
	postAnchorWatch(t, map[string]any{"lat": -20.0, "lon": 149.0})
	t0 := time.Now().UTC().Add(time.Second)
	recordAnchorTrailPoint(-20.0001, 149.0001, t0)
	recordAnchorTrailPoint(-20.0001, 149.00010001, t0.Add(5*time.Second)) // ~1 cm: skipped
	recordAnchorTrailPoint(-20.00011, 149.0001, t0.Add(10*time.Second))   // ~1.1 m: kept
	recordAnchorTrailPoint(-20.00011, 149.0001, t0.Add(30*time.Second))   // still: skipped
	recordAnchorTrailPoint(-20.00011, 149.0001, t0.Add(75*time.Second))   // heartbeat: kept
	var n int
	_ = store.db.QueryRow(`SELECT COUNT(*) FROM anchor_trail_points`).Scan(&n)
	if n != 3 {
		t.Fatalf("expected 3 stored points, got %d", n)
	}
}

func TestAnchorTrailDoesNotRestartAfterRaise(t *testing.T) {
	store := anchorTrailEnv(t)
	postAnchorWatch(t, map[string]any{"lat": -20.0, "lon": 149.0})
	if rec := raiseAnchor(t); rec.Code != http.StatusOK {
		t.Fatalf("raise: %d", rec.Code)
	}
	// A tick that read the watch as active before the raise lands afterwards.
	if err := store.appendAnchorTrailPoint(time.Now().UTC().Add(time.Second), -20.0001, 149.0001); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = store.db.QueryRow(`SELECT COUNT(*) FROM anchor_trail_points`).Scan(&n)
	started, _ := store.anchorTrailStarted()
	if n != 0 || !started.IsZero() {
		t.Fatalf("a late tick must not restart a raised trail: %d rows, started %v", n, started)
	}
}

func TestStartRestoredAnchorTrailStartsOnceForAWatchThatPredatesTheTable(t *testing.T) {
	store := anchorTrailEnv(t)
	anchorWatchMu.Lock()
	anchorWatchState = &anchorWatchData{Lat: -20, Lon: 149, SetAt: time.Now().UTC()}
	anchorWatchMu.Unlock()
	if err := startRestoredAnchorTrail(); err != nil {
		t.Fatal(err)
	}
	first, _ := store.anchorTrailStarted()
	if first.IsZero() {
		t.Fatal("expected the trail to start")
	}
	time.Sleep(5 * time.Millisecond)
	if err := startRestoredAnchorTrail(); err != nil {
		t.Fatal(err)
	}
	if again, _ := store.anchorTrailStarted(); !again.Equal(first) {
		t.Fatalf("a running trail must not be restarted at startup: %v then %v", first, again)
	}
	recordAnchorTrailPoint(-20.0001, 149.0001, time.Now().UTC().Add(time.Second))
	if _, body := getAnchorTrailBody(t, ""); len(body.Points) != 1 {
		t.Fatalf("expected the first fix recorded, got %+v", body)
	}
}

func TestAnchorDropFailsWhenTheTrailCannotBeReset(t *testing.T) {
	anchorPublishEnv(t)
	prev := globalPassageStore
	globalPassageStore = nil
	t.Cleanup(func() { globalPassageStore = prev })
	code, resp := postAnchorWatch(t, map[string]any{"lat": -20.0, "lon": 149.0})
	if code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d %v", code, resp)
	}
	anchorWatchMu.RLock()
	active := anchorWatchState != nil
	anchorWatchMu.RUnlock()
	if active {
		t.Fatal("a failed trail reset must leave no watch behind")
	}
}

func TestAnchorPositionPatchFailsWhenTheTrailCannotBeReset(t *testing.T) {
	anchorPublishEnv(t)
	postAnchorWatch(t, map[string]any{"lat": -20.0, "lon": 149.0})
	prev := globalPassageStore
	globalPassageStore = nil
	t.Cleanup(func() { globalPassageStore = prev })
	if code, resp := patchAnchorWatchForTest(t, map[string]any{"lat": -20.5, "lon": 149.5}); code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d %v", code, resp)
	}
	anchorWatchMu.RLock()
	lat := anchorWatchState.Lat
	anchorWatchMu.RUnlock()
	if lat != -20.0 {
		t.Fatalf("watch must keep its old position, got %v", lat)
	}
}

func TestAnchorRaiseReportsAFailedTrailClear(t *testing.T) {
	anchorPublishEnv(t)
	postAnchorWatch(t, map[string]any{"lat": -20.0, "lon": 149.0})
	prev := globalPassageStore
	globalPassageStore = nil
	t.Cleanup(func() { globalPassageStore = prev })
	if rec := raiseAnchor(t); rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestAnchorTrailResponseCapKeepsNewest(t *testing.T) {
	store := anchorTrailEnv(t)
	postAnchorWatch(t, map[string]any{"lat": -20.0, "lon": 149.0})
	base := time.Now().UTC().Add(time.Second)
	tx, _ := store.db.Begin()
	for i := 0; i < anchorTrailMaxPoints+5; i++ {
		_, _ = tx.Exec(`INSERT INTO anchor_trail_points (t_ms, lat, lon) VALUES (?,?,?)`, base.Add(time.Duration(i)*time.Second).UnixMilli(), -20.0, 149.0+float64(i)*1e-7)
	}
	_ = tx.Commit()
	_, body := getAnchorTrailBody(t, "")
	if len(body.Points) != anchorTrailMaxPoints {
		t.Fatalf("expected %d points, got %d", anchorTrailMaxPoints, len(body.Points))
	}
	if !body.Points[len(body.Points)-1].Timestamp.Equal(base.Add(time.Duration(anchorTrailMaxPoints+4) * time.Second).Truncate(time.Millisecond)) {
		t.Fatalf("newest point missing from capped response")
	}
}

func TestAnchorTrailStoreUnavailableIsAnError(t *testing.T) {
	anchorPublishEnv(t)
	postAnchorWatch(t, map[string]any{"lat": -20.0, "lon": 149.0})
	prev := globalPassageStore
	globalPassageStore = nil
	t.Cleanup(func() { globalPassageStore = prev })
	if code, _ := getAnchorTrailBody(t, ""); code != http.StatusInternalServerError {
		t.Fatalf("expected 500 with no store, got %d", code)
	}
}

// setTrailReset stands in a trail store for the test; the restore is
// registered first so it runs after any wait the test registers later.
func setTrailReset(t *testing.T, fn func(bool) error) {
	t.Helper()
	prev := anchorTrailResetter.Load()
	anchorTrailResetter.Store(&fn)
	t.Cleanup(func() { anchorTrailResetter.Store(prev) })
}

func failTrailReset(t *testing.T) {
	t.Helper()
	setTrailReset(t, func(bool) error { return errors.New("trail store unavailable") })
}

// dropAnchor drops a watch and waits for the background place-name resolve.
func dropAnchor(t *testing.T, lat, lon float64) {
	t.Helper()
	if code, resp := postAnchorWatch(t, map[string]any{"lat": lat, "lon": lon}); code != http.StatusOK {
		t.Fatalf("drop: %d %v", code, resp)
	}
	waitForPlaceNameResolveIdle(t)
}

func setTestPlacemarks(t *testing.T) {
	t.Helper()
	t.Setenv("ANCHOR_PLACEMARKS_FILE", filepath.Join(t.TempDir(), "placemarks.json"))
	placemarkMu.Lock()
	placemarks = []*placemark{{ID: "p1", Lat: -20.0, Lon: 149.0}}
	placemarkMu.Unlock()
	t.Cleanup(func() { placemarkMu.Lock(); placemarks = nil; placemarkMu.Unlock() })
}

func placemarkCount() int {
	placemarkMu.RLock()
	defer placemarkMu.RUnlock()
	return len(placemarks)
}

func TestAnchorRaiseClearsPlacemarksEvenWhenTheTrailResetFails(t *testing.T) {
	anchorPublishEnv(t)
	setTestPlacemarks(t)
	dropAnchor(t, -20.0, 149.0)
	failTrailReset(t)

	if rec := raiseAnchor(t); rec.Code != http.StatusInternalServerError {
		t.Fatalf("the trail failure must be reported, got %d", rec.Code)
	}
	anchorWatchMu.RLock()
	gone := anchorWatchState == nil
	anchorWatchMu.RUnlock()
	if !gone {
		t.Fatal("the watch should have been raised")
	}
	if n := placemarkCount(); n != 0 {
		t.Fatalf("placemarks must be cleared even when the trail reset fails, %d left", n)
	}
}

func TestAnchorRaiseReportsTrailStageAfterCleanup(t *testing.T) {
	anchorPublishEnv(t)
	dropAnchor(t, -20.0, 149.0)
	failTrailReset(t)
	err := raiseAnchorWatch()
	if anchorRaiseFailureStage(err) != anchorRaiseStageTrail {
		t.Fatalf("expected the trail stage, got %v", err)
	}
}

func TestAnchorRaiseReportsAPlacemarksFileThatCannotBeRemoved(t *testing.T) {
	anchorPublishEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "placemarks.json")
	if err := os.WriteFile(path, []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANCHOR_PLACEMARKS_FILE", path)
	dropAnchor(t, -20.0, 149.0)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if f, err := os.OpenFile(filepath.Join(dir, "probe"), os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		f.Close()
		t.Skip("directory permissions are not enforced here (running as root?)")
	}

	rec := raiseAnchor(t)
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "placemarks") {
		t.Fatalf("a stale placemarks file must be reported, got %d %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the file should still be there: %v", err)
	}
	// A missing file is fine.
	_ = os.Chmod(dir, 0o700)
	_ = os.Remove(path)
	if rec := raiseAnchor(t); rec.Code != http.StatusOK {
		t.Fatalf("a missing placemarks file is not an error, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestAnchorPositionPatchDoesNotHoldTheWriteLockDuringTheTrailReset(t *testing.T) {
	anchorPublishEnv(t)
	dropAnchor(t, -20.0, 149.0)
	entered, release := make(chan struct{}), make(chan struct{})
	setTrailReset(t, func(bool) error {
		close(entered)
		<-release
		return nil
	})

	done := make(chan struct {
		code int
		err  error
	}, 1)
	go func() {
		// Not patchAnchorWatchForTest: it calls t.Fatalf, which must not run here.
		raw, _ := json.Marshal(map[string]any{"lat": -20.5, "lon": 149.5})
		req := httptest.NewRequest(http.MethodPatch, "/api/anchor-watch", strings.NewReader(string(raw)))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		err := patchAnchorWatch(echo.New().NewContext(req, rec))
		done <- struct {
			code int
			err  error
		}{rec.Code, err}
	}()
	var once sync.Once
	releaseOnce := func() { once.Do(func() { close(release) }) }
	t.Cleanup(func() {
		releaseOnce()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	})
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("trail reset never started")
	}
	read := make(chan struct{})
	go func() {
		rec := httptest.NewRecorder()
		_ = getAnchorWatch(echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/api/anchor-watch", nil), rec))
		close(read)
	}()
	select {
	case <-read:
	case <-time.After(2 * time.Second):
		t.Fatal("anchor GET stalled behind the trail reset")
	}
	releaseOnce()
	res := <-done
	if res.err != nil || res.code != http.StatusOK {
		t.Fatalf("expected the PATCH to succeed, got %d %v", res.code, res.err)
	}
	waitForPlaceNameResolveIdle(t)
}

func TestAnchorMoveHidesTheHalfMovedStateFromTrailReadersAndTicks(t *testing.T) {
	store := anchorTrailEnv(t)
	dropAnchor(t, -20.0, 149.0)
	entered, release := make(chan struct{}), make(chan struct{})
	setTrailReset(t, func(bool) error {
		close(entered)
		<-release
		return nil
	})
	done := make(chan int, 1)
	go func() {
		raw, _ := json.Marshal(map[string]any{"lat": -20.5, "lon": 149.5})
		req := httptest.NewRequest(http.MethodPatch, "/api/anchor-watch", strings.NewReader(string(raw)))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		_ = patchAnchorWatch(echo.New().NewContext(req, rec))
		done <- rec.Code
	}()
	var once sync.Once
	releaseOnce := func() { once.Do(func() { close(release) }) }
	t.Cleanup(func() {
		releaseOnce()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	})
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("trail reset never started")
	}

	// A tick during the move is skipped, not recorded against the old anchor.
	recordAnchorTrailPoint(-20.0001, 149.0001, time.Now().UTC().Add(time.Second))

	// A trail read waits for the move to finish.
	read := make(chan struct{})
	go func() {
		rec := httptest.NewRecorder()
		_ = getSelfTrailHandler(echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/api/anchor-watch/trails/self", nil), rec))
		close(read)
	}()
	select {
	case <-read:
		t.Fatal("trail read returned in the middle of a move")
	case <-time.After(150 * time.Millisecond):
	}
	releaseOnce()
	select {
	case <-read:
	case <-time.After(5 * time.Second):
		t.Fatal("trail read never finished")
	}
	if code := <-done; code != http.StatusOK {
		t.Fatalf("move: %d", code)
	}
	waitForPlaceNameResolveIdle(t)
	if pts, _, _ := store.anchorTrailSince(time.Time{}); len(pts) != 0 {
		t.Fatalf("the tick taken during the move must not be in the new trail: %v", pts)
	}
}

func lastFrameIsAt(t *testing.T, frames [][]byte, lat, lon float64) {
	t.Helper()
	var delta signalKDelta
	if err := json.Unmarshal(frames[len(frames)-1], &delta); err != nil {
		t.Fatal(err)
	}
	for _, u := range delta.Updates {
		for _, v := range u.Values {
			b, _ := json.Marshal(v.Value)
			var pos struct{ Latitude, Longitude float64 }
			if json.Unmarshal(b, &pos) == nil && pos.Latitude == lat && pos.Longitude == lon {
				return
			}
		}
	}
	t.Fatalf("last SignalK frame is not at %v,%v: %s", lat, lon, frames[len(frames)-1])
}

func TestAnchorPositionPatchTrailFailureRollsSignalKBackAndKeepsTheWatch(t *testing.T) {
	stub := anchorPublishEnv(t)
	dropAnchor(t, -20.0, 149.0)
	failTrailReset(t)

	code, resp := patchAnchorWatchForTest(t, map[string]any{"lat": -20.5, "lon": 149.5})
	if code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d %v", code, resp)
	}
	if msg, _ := resp["error"].(string); !strings.Contains(msg, "swing trail") {
		t.Fatalf("the error must say the trail failed, got %q", msg)
	}
	anchorWatchMu.RLock()
	lat, lon := anchorWatchState.Lat, anchorWatchState.Lon
	anchorWatchMu.RUnlock()
	if lat != -20.0 || lon != 149.0 {
		t.Fatalf("watch must keep its old position, got %v,%v", lat, lon)
	}
	frames := stub.captured()
	if len(frames) != 3 { // drop, the PATCH's publish, the rollback
		t.Fatalf("expected drop + patch + rollback publishes, got %d", len(frames))
	}
	lastFrameIsAt(t, frames, -20.0, 149.0)
}

func TestAnchorPositionPatchSaveFailureAfterTrailResetRollsSignalKBack(t *testing.T) {
	stub := anchorPublishEnv(t)
	dropAnchor(t, -20.0, 149.0)
	notADir := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(notADir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANCHOR_WATCH_FILE", filepath.Join(notADir, "anchor.json"))

	code, resp := patchAnchorWatchForTest(t, map[string]any{"lat": -20.5, "lon": 149.5})
	if code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d %v", code, resp)
	}
	msg, _ := resp["error"].(string)
	if !strings.Contains(msg, "swing trail was restarted") || !strings.Contains(msg, "put back") {
		t.Fatalf("the error must say the trail was restarted and SignalK put back, got %q", msg)
	}
	anchorWatchMu.RLock()
	lat := anchorWatchState.Lat
	anchorWatchMu.RUnlock()
	if lat != -20.0 {
		t.Fatalf("watch must keep its old position, got %v", lat)
	}
	frames := stub.captured()
	if len(frames) != 3 {
		t.Fatalf("expected drop + patch + rollback publishes, got %d", len(frames))
	}
	lastFrameIsAt(t, frames, -20.0, 149.0)
}
