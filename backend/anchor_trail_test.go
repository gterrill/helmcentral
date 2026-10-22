package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
