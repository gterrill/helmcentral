package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// ── helpers ────────────────────────────────────────────────────────────────────

// makeTracksServer serves the tracks plugin endpoint over HTTP — that one is a
// REST API the delta stream does not carry — while seeding vessel data into the
// snapshot, which is where every vessel lookup now reads from (ADR 0037). Self
// is seeded too so name-based self-filtering keeps working.
func makeTracksServer(t *testing.T, tracksBody map[string]any, vesselsBody map[string]any) *httptest.Server {
	t.Helper()
	tracksJSON, _ := json.Marshal(tracksBody)
	vesselsJSON, _ := json.Marshal(vesselsBody)

	snapshot := newSignalKSnapshot()
	for id, tree := range vesselsBody {
		if asMap, ok := tree.(map[string]any); ok {
			snapshot.contexts[vesselContextPrefix+id] = asMap
		}
	}
	snapshot.contexts["vessels.self"] = map[string]any{"name": "TESTSELF"}
	snapshot.setSelfContext("vessels.self")
	withGlobalSnapshot(t, snapshot)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/api/tracks") || strings.Contains(r.URL.Path, "v1/api/tracks"):
			w.Write(tracksJSON)
		case strings.Contains(r.URL.Path, "/vessels/self"):
			w.Write([]byte(`{"name":"TESTSELF"}`))
		case strings.HasSuffix(r.URL.Path, "/vessels"):
			w.Write(vesselsJSON)
		default:
			http.NotFound(w, r)
		}
	}))
	return srv
}

func settingsFileForServer(t *testing.T, serverURL string) string {
	t.Helper()
	// buildSignalKURL accepts a full http:// URL as address; use that directly.
	content := []byte("signalk:\n  address: " + serverURL + "\n  port: 3000\n")
	dir := t.TempDir()
	path := dir + "/settings.yaml"
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("could not write temp settings: %v", err)
	}
	return path
}

func resetTracksStateForTest(t *testing.T) {
	t.Helper()
	trackMu.Lock()
	selfTrack = newVesselTrail()
	trackMu.Unlock()

	tracksAISTrails = newTracksAISTrailsCache()
}

// ── tests ──────────────────────────────────────────────────────────────────────

func TestFetchSignalKAISTrails_ReturnsNamedTrails(t *testing.T) {
	tracksBody := map[string]any{
		"vessels.urn:mrn:imo:mmsi:123456789": map[string]any{
			"type": "MultiLineString",
			"coordinates": [][][]float64{
				{{152.91, -25.29}, {152.92, -25.30}},
			},
		},
	}
	vesselsBody := map[string]any{
		"urn:mrn:imo:mmsi:123456789": map[string]any{
			"name": "PEGASUS",
		},
	}

	srv := makeTracksServer(t, tracksBody, vesselsBody)
	defer srv.Close()

	settingsPath := settingsFileForServer(t, srv.URL)
	t.Setenv("SIGNALK_TRACKS_PATH", "/v1/api/tracks")
	t.Setenv("SIGNALK_VESSELS_PATH", "/vessels")
	t.Setenv("SIGNALK_VESSEL_PATH", "/vessels/self")

	trails := fetchSignalKAISTrails(settingsPath)

	if len(trails) != 1 {
		t.Fatalf("expected 1 trail, got %d", len(trails))
	}
	pts, ok := trails["PEGASUS"]
	if !ok {
		t.Fatal("expected trail keyed by vessel name PEGASUS")
	}
	if len(pts) != 2 {
		t.Fatalf("expected 2 points, got %d", len(pts))
	}
	// SignalK/tracks uses [lon, lat] order; adapter must flip to lat/lon.
	if pts[0].Lat != -25.29 || pts[0].Lon != 152.91 {
		t.Errorf("first point lat/lon mismatch: got %+v", pts[0])
	}
}

func TestFetchSignalKAISTrails_ExcludesSelf(t *testing.T) {
	tracksBody := map[string]any{
		"vessels.self": map[string]any{
			"type": "MultiLineString",
			"coordinates": [][][]float64{
				{{152.91, -25.29}},
			},
		},
	}
	srv := makeTracksServer(t, tracksBody, map[string]any{})
	defer srv.Close()

	settingsPath := settingsFileForServer(t, srv.URL)
	t.Setenv("SIGNALK_TRACKS_PATH", "/v1/api/tracks")
	t.Setenv("SIGNALK_VESSELS_PATH", "/vessels")
	t.Setenv("SIGNALK_VESSEL_PATH", "/vessels/self")

	trails := fetchSignalKAISTrails(settingsPath)
	if len(trails) != 0 {
		t.Fatalf("expected self vessel excluded, got %d trails", len(trails))
	}
}

func TestFetchSignalKAISTrails_EmptyOnBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not-json"))
	}))
	defer srv.Close()

	settingsPath := settingsFileForServer(t, srv.URL)
	t.Setenv("SIGNALK_TRACKS_PATH", "/v1/api/tracks")
	t.Setenv("SIGNALK_VESSELS_PATH", "/vessels")
	t.Setenv("SIGNALK_VESSEL_PATH", "/vessels/self")

	trails := fetchSignalKAISTrails(settingsPath)
	if len(trails) != 0 {
		t.Fatalf("expected empty result on bad JSON, got %d trails", len(trails))
	}
}

func TestFetchSignalKAISTrails_EmptyOnNonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	settingsPath := settingsFileForServer(t, srv.URL)
	t.Setenv("SIGNALK_TRACKS_PATH", "/v1/api/tracks")
	t.Setenv("SIGNALK_VESSELS_PATH", "/vessels")
	t.Setenv("SIGNALK_VESSEL_PATH", "/vessels/self")

	trails := fetchSignalKAISTrails(settingsPath)
	if len(trails) != 0 {
		t.Fatalf("expected empty result on non-OK status, got %d trails", len(trails))
	}
}

// TestFetchSignalKAISTrailsResult_404IsALegitimateAbsentEndpointNotAnError
// pins the boat's actual current state (backend perf audit #10): the
// SignalK tracks plugin is not installed there, so every fetch gets a 404.
// That is a configuration fact, not a failure -- the same idiom
// fetchSignalKNotificationsTree already uses for a 404'd sub-resource
// (notification_sync.go) -- so it must not be reported as an error, only
// return empty trails.
func TestFetchSignalKAISTrailsResult_404IsALegitimateAbsentEndpointNotAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	settingsPath := settingsFileForServer(t, srv.URL)
	t.Setenv("SIGNALK_TRACKS_PATH", "/v1/api/tracks")

	trails, err := fetchSignalKAISTrailsResult(settingsPath)
	if err != nil {
		t.Fatalf("a 404 tracks endpoint must not surface as an error, got %v", err)
	}
	if len(trails) != 0 {
		t.Fatalf("expected empty trails for a 404, got %d", len(trails))
	}
}

// TestFetchSignalKAISTrailsResult_404LogsOncePerStateChange: /api/tracks
// refreshes on demand every 15 s while a map is open, so a line per 404 would
// flood the log buffer on a boat without the tracks plugin.
func TestFetchSignalKAISTrailsResult_404LogsOncePerStateChange(t *testing.T) {
	var found atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !found.Load() {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	settingsPath := settingsFileForServer(t, srv.URL)
	t.Setenv("SIGNALK_TRACKS_PATH", "/v1/api/tracks")
	tracksEndpointMissing.Store(false)
	t.Cleanup(func() { tracksEndpointMissing.Store(false) })

	var buf strings.Builder
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	for i := 0; i < 3; i++ {
		if _, err := fetchSignalKAISTrailsResult(settingsPath); err != nil {
			t.Fatalf("fetch %d: %v", i, err)
		}
	}
	if got := strings.Count(buf.String(), "(404)"); got != 1 {
		t.Fatalf("expected one 404 log line across three fetches, got %d:\n%s", got, buf.String())
	}

	found.Store(true)
	for i := 0; i < 2; i++ {
		if _, err := fetchSignalKAISTrailsResult(settingsPath); err != nil {
			t.Fatalf("fetch after recovery %d: %v", i, err)
		}
	}
	if got := strings.Count(buf.String(), "answered again"); got != 1 {
		t.Fatalf("expected one recovery log line, got %d:\n%s", got, buf.String())
	}
}

// TestFetchSignalKAISTrailsResult_OtherNonOKStatusReturnsAnError guards the
// fallback policy (AGENTS.md): unlike 404 (an absent, optional endpoint), a
// 5xx or other unexpected status is a real failure and must surface as an
// error rather than silently becoming an empty success -- the distinction
// TestTracksAISTrailsCache_PreservesLastGoodTrailsOnTransientUpstreamError
// below depends on below the cache layer.
func TestFetchSignalKAISTrailsResult_OtherNonOKStatusReturnsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	settingsPath := settingsFileForServer(t, srv.URL)
	t.Setenv("SIGNALK_TRACKS_PATH", "/v1/api/tracks")

	if _, err := fetchSignalKAISTrailsResult(settingsPath); err == nil {
		t.Fatalf("expected a real error for a non-404 non-OK status")
	}
}

// TestTracksAISTrailsCache_PreservesLastGoodTrailsOnTransientUpstreamError is
// the end-to-end reason fetchSignalKAISTrailsResult must distinguish a real
// error from the 404-as-absent-feature case above: finishRefresh keeps the
// last-good snapshot on an error rather than overwriting it with empty, so a
// plugin restart or a blip does not blank the map until the next successful
// poll. Before this fix every non-OK status (the 503 here included) returned
// a nil error, so this clobbered good data with empty on the very first
// blip.
func TestTracksAISTrailsCache_PreservesLastGoodTrailsOnTransientUpstreamError(t *testing.T) {
	resetTracksStateForTest(t)

	tracksBody := map[string]any{
		"vessels.urn:mrn:imo:mmsi:123456789": map[string]any{
			"type":        "MultiLineString",
			"coordinates": [][][]float64{{{152.91, -25.29}}},
		},
	}
	vesselsBody := map[string]any{
		"urn:mrn:imo:mmsi:123456789": map[string]any{"name": "PEGASUS"},
	}
	tracksJSON, _ := json.Marshal(tracksBody)
	vesselsJSON, _ := json.Marshal(vesselsBody)

	var failNext atomic.Bool

	snapshot := newSignalKSnapshot()
	for id, tree := range vesselsBody {
		if asMap, ok := tree.(map[string]any); ok {
			snapshot.contexts[vesselContextPrefix+id] = asMap
		}
	}
	snapshot.contexts["vessels.self"] = map[string]any{"name": "TESTSELF"}
	snapshot.setSelfContext("vessels.self")
	withGlobalSnapshot(t, snapshot)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "v1/api/tracks"):
			if failNext.Load() {
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
				return
			}
			w.Write(tracksJSON)
		case strings.Contains(r.URL.Path, "/vessels/self"):
			w.Write([]byte(`{"name":"TESTSELF"}`))
		case strings.HasSuffix(r.URL.Path, "/vessels"):
			w.Write(vesselsJSON)
		}
	}))
	defer srv.Close()

	settingsPath := settingsFileForServer(t, srv.URL)
	t.Setenv("SIGNALK_TRACKS_PATH", "/v1/api/tracks")
	t.Setenv("SIGNALK_VESSELS_PATH", "/vessels")
	t.Setenv("SIGNALK_VESSEL_PATH", "/vessels/self")

	good := tracksAISTrails.get(settingsPath)
	if len(good) != 1 {
		t.Fatalf("expected the initial fetch to warm the cache with 1 trail, got %d", len(good))
	}

	// Force the next fetch to fail and expire the cache so get() re-fetches.
	failNext.Store(true)
	tracksAISTrails.snapshot.fetchedAt = time.Now().Add(-1 * time.Minute)

	stillGood := tracksAISTrails.get(settingsPath)
	if len(stillGood) != 1 {
		t.Fatalf("a transient upstream error must not clobber the last-good trails, got %d", len(stillGood))
	}
}

func TestFetchSignalKAISTrails_SkipsInvalidCoordinates(t *testing.T) {
	tracksBody := map[string]any{
		"vessels.urn:mrn:imo:mmsi:111": map[string]any{
			"type": "MultiLineString",
			"coordinates": [][][]float64{
				{{999.0, 999.0}}, // out of range
			},
		},
	}
	srv := makeTracksServer(t, tracksBody, map[string]any{})
	defer srv.Close()

	settingsPath := settingsFileForServer(t, srv.URL)
	t.Setenv("SIGNALK_TRACKS_PATH", "/v1/api/tracks")
	t.Setenv("SIGNALK_VESSELS_PATH", "/vessels")
	t.Setenv("SIGNALK_VESSEL_PATH", "/vessels/self")

	trails := fetchSignalKAISTrails(settingsPath)
	if len(trails) != 0 {
		t.Fatalf("expected invalid coordinates skipped, got %d trails", len(trails))
	}
}

// TestMotoringTrail_StartsEmptyAndFillsOnlyViaRecordMotoringPoint guards the
// removal of Influx startup seeding: the motoring trail must start empty and
// only ever gain points through recordMotoringPoint (live sampling), with no
// seed call populating it from history.
func TestMotoringTrail_StartsEmptyAndFillsOnlyViaRecordMotoringPoint(t *testing.T) {
	motoringTrailMu.Lock()
	motoringTrail = newVesselTrail()
	motoringTrailMu.Unlock()

	motoringTrailMu.RLock()
	pts := motoringTrail.pointsSince(time.Time{})
	motoringTrailMu.RUnlock()
	if len(pts) != 0 {
		t.Fatalf("expected motoring trail to start empty, got %d points", len(pts))
	}

	recordMotoringPoint(-25.29, 152.91)

	motoringTrailMu.RLock()
	pts = motoringTrail.pointsSince(time.Time{})
	motoringTrailMu.RUnlock()
	if len(pts) != 1 {
		t.Fatalf("expected 1 point after recordMotoringPoint, got %d", len(pts))
	}
}

// TestAddPoint_StoresMillisecondPrecision guards the wire round-trip: the
// client parses timestamps through JS Date (millisecond precision) and sends
// `since` back via toISOString(), which is also millisecond precision. If
// addPoint stores the full nanosecond-precision time.Now(), that round-trip
// truncates on the way out and back in, so the stored timestamp is always
// microscopically later than what the client can ever send as `since` - and
// pointsSince (which uses After, not equal-or-after) re-sends the newest
// point on every single poll.
func TestAddPoint_StoresMillisecondPrecision(t *testing.T) {
	vt := newVesselTrail()
	vt.addPoint(-25.29, 152.91)

	pts := vt.pointsSince(time.Time{})
	if len(pts) != 1 {
		t.Fatalf("expected 1 point, got %d", len(pts))
	}
	ts := pts[0].Timestamp
	if ts.Nanosecond()%int(time.Millisecond) != 0 {
		t.Fatalf("expected addPoint to store millisecond precision, got nanosecond remainder %d (timestamp %s)", ts.Nanosecond()%int(time.Millisecond), ts)
	}
}

// TestPointsSince_MillisecondPrecisionSinceDoesNotResendNewestPoint is the
// end-to-end regression guard for the same defect, exercised the way the
// real client hits it: record a point through the real addPoint, format its
// timestamp the way JS Date.toISOString() would, parse that back as `since`,
// and confirm the just-recorded point is not handed back again. Before the
// fix this is red whenever time.Now() lands off a millisecond boundary,
// which on a real clock is effectively always.
func TestPointsSince_MillisecondPrecisionSinceDoesNotResendNewestPoint(t *testing.T) {
	vt := newVesselTrail()
	vt.addPoint(-25.29, 152.91)
	stored := vt.pointsSince(time.Time{})[0].Timestamp

	sinceStr := stored.Format("2006-01-02T15:04:05.000Z07:00")
	since, err := time.Parse(time.RFC3339, sinceStr)
	if err != nil {
		t.Fatalf("could not parse since timestamp %q: %v", sinceStr, err)
	}

	pts := vt.pointsSince(since)
	if len(pts) != 0 {
		t.Fatalf("expected the just-recorded point not to be re-sent when since matches its millisecond, got %d points", len(pts))
	}

	// A point at a strictly later millisecond must still come through.
	vt.addPointWithTimestamp(-25.30, 152.92, stored.Add(time.Millisecond))
	pts = vt.pointsSince(since)
	if len(pts) != 1 {
		t.Fatalf("expected the later point to be returned, got %d points", len(pts))
	}
}

// TestSampleTracks_RecordsWindAndDepthHistoryEvenWithoutValidPosition is a
// regression guard for the in-memory telemetry history buffers: wind gust
// and depth are not position-dependent, so sampleTracks must record them
// even when the vessel has no GPS fix (e.g. dockside), unlike the
// position-gated trail recording a few lines below it.
func TestSampleTracks_RecordsWindAndDepthHistoryEvenWithoutValidPosition(t *testing.T) {
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	windGustHistory = newTelemetryRingBuffer(telemetryHistoryCapacity)
	depthHistory = newTelemetryRingBuffer(telemetryHistoryCapacity)

	body := []byte(`{
		"navigation": {
			"datetime": {"value": "` + time.Now().UTC().Format(time.RFC3339) + `"},
			"state": {"value": "anchored"}
		},
		"environment": {
			"depth": {"belowTransducer": {"value": 12.5}},
			"wind": {"speedApparent": {"value": 5.0}}
		}
	}`)
	seedSelfTree(t, string(body))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer srv.Close()

	settingsPath := settingsFileForServer(t, srv.URL)

	sampleTracks(settingsPath)

	windPts := windGustHistory.since(time.Time{})
	if len(windPts) != 1 {
		t.Fatalf("expected 1 wind gust sample recorded despite missing position, got %d", len(windPts))
	}

	depthPts := depthHistory.since(time.Time{})
	if len(depthPts) != 1 {
		t.Fatalf("expected 1 depth sample recorded despite missing position, got %d", len(depthPts))
	}
}

// TestSampleTracks_RecordsTrueWindGustHistoryAlongsideApparent (ADR 0130)
// proves each poll tick that publishes true wind also records it into its
// own trueWindGustHistory buffer, on the same tick as the apparent
// recording above - not a converted copy of the apparent sample.
func TestSampleTracks_RecordsTrueWindGustHistoryAlongsideApparent(t *testing.T) {
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	windGustHistory = newTelemetryRingBuffer(telemetryHistoryCapacity)
	original := trueWindGustHistory
	t.Cleanup(func() { trueWindGustHistory = original })
	trueWindGustHistory = newTelemetryRingBuffer(telemetryHistoryCapacity)

	fresh := time.Now().UTC().Format(time.RFC3339)
	body := []byte(`{
		"navigation": {
			"datetime": {"value": "` + fresh + `"},
			"state": {"value": "anchored"}
		},
		"environment": {
			"wind": {
				"speedApparent": {"value": 5.0},
				"speedTrue": {"value": 7.2, "timestamp": "` + fresh + `"}
			}
		}
	}`)
	seedSelfTree(t, string(body))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer srv.Close()

	settingsPath := settingsFileForServer(t, srv.URL)

	sampleTracks(settingsPath)

	windPts := windGustHistory.since(time.Time{})
	if len(windPts) != 1 {
		t.Fatalf("expected 1 apparent wind gust sample, got %d", len(windPts))
	}

	truePts := trueWindGustHistory.since(time.Time{})
	if len(truePts) != 1 {
		t.Fatalf("expected 1 true wind gust sample recorded on the same tick, got %d", len(truePts))
	}
	wantKts := 7.2 * metersPerSecondToKnots
	if !approxEqual(truePts[0].Value, wantKts, 0.01) {
		t.Fatalf("expected true wind gust sample of %.2f kts, got %v", wantKts, truePts[0].Value)
	}
}

// TestSampleTracks_SkipsTrueWindGustRecordingWhenAbsent proves a tick with
// apparent wind but no true wind records only the apparent sample - no
// fallback value is invented for the true-wind buffer.
func TestSampleTracks_SkipsTrueWindGustRecordingWhenAbsent(t *testing.T) {
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	windGustHistory = newTelemetryRingBuffer(telemetryHistoryCapacity)
	original := trueWindGustHistory
	t.Cleanup(func() { trueWindGustHistory = original })
	trueWindGustHistory = newTelemetryRingBuffer(telemetryHistoryCapacity)

	body := []byte(`{
		"navigation": {
			"datetime": {"value": "` + time.Now().UTC().Format(time.RFC3339) + `"},
			"state": {"value": "anchored"}
		},
		"environment": {
			"wind": {"speedApparent": {"value": 5.0}}
		}
	}`)
	seedSelfTree(t, string(body))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer srv.Close()

	settingsPath := settingsFileForServer(t, srv.URL)

	sampleTracks(settingsPath)

	truePts := trueWindGustHistory.since(time.Time{})
	if len(truePts) != 0 {
		t.Fatalf("expected no true wind gust sample recorded when speedTrue is absent, got %d", len(truePts))
	}
}

// TestSampleTracks_SkipsTrueWindGustRecordingWhenTrueWindIsStale
// (code-review fix, 2026-09-25) proves a stale true-wind reading - fresh
// GPS/navigation.datetime, but speedTrue/angleTrueWater timestamps past
// defaultWindMaxAge - records nothing into trueWindGustHistory. sampleTracks
// itself gates purely on state.WindSpeedTrueKts >= 0
// (fetchSignalKVesselState's own sentinel), so this is really confirming
// signalk.go's recency fix (no GNSS-datetime fallback for true wind)
// actually reaches this far downstream, not a second, independent gate.
func TestSampleTracks_SkipsTrueWindGustRecordingWhenTrueWindIsStale(t *testing.T) {
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	windGustHistory = newTelemetryRingBuffer(telemetryHistoryCapacity)
	original := trueWindGustHistory
	t.Cleanup(func() { trueWindGustHistory = original })
	trueWindGustHistory = newTelemetryRingBuffer(telemetryHistoryCapacity)

	fresh := time.Now().UTC().Format(time.RFC3339)
	stale := time.Now().UTC().Add(-30 * time.Minute).Format(time.RFC3339)
	body := []byte(`{
		"navigation": {
			"datetime": {"value": "` + fresh + `"},
			"state": {"value": "anchored"}
		},
		"environment": {
			"wind": {
				"speedApparent": {"value": 5.0, "timestamp": "` + fresh + `"},
				"speedTrue": {"value": 9.5, "timestamp": "` + stale + `"},
				"angleTrueWater": {"value": 0.5, "timestamp": "` + stale + `"}
			}
		}
	}`)
	seedSelfTree(t, string(body))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer srv.Close()

	settingsPath := settingsFileForServer(t, srv.URL)

	sampleTracks(settingsPath)

	windPts := windGustHistory.since(time.Time{})
	if len(windPts) != 1 {
		t.Fatalf("expected the fresh apparent wind gust sample to still record, got %d", len(windPts))
	}

	truePts := trueWindGustHistory.since(time.Time{})
	if len(truePts) != 0 {
		t.Fatalf("expected no true wind gust sample recorded while true wind is stale (even with fresh apparent/GPS on the same tick), got %d", len(truePts))
	}
}

// TestSampleTracks_DoesNotRefreshAISTrailsCache guards the removal of the
// poller's direct tracksAISTrails.refresh call (backend perf audit #10):
// getTracksHandler's own get() already refreshes on demand within the
// cache's 15s freshness window, so the poller hitting the same upstream
// endpoint on every 5s tick regardless of whether a client had the map open
// just doubled the load for nothing. The position here defaults to the
// -1,-1 sentinel (no navigation.position in the body), which is exactly what
// the two tests above use to reach this same block without tripping
// hasUsableVesselPosition's Overpass lookup -- the loose range check a few
// lines up in sampleTracks treats it as "in range" regardless.
func TestSampleTracks_DoesNotRefreshAISTrailsCache(t *testing.T) {
	resetTracksStateForTest(t)
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	body := []byte(`{
		"navigation": {
			"datetime": {"value": "` + time.Now().UTC().Format(time.RFC3339) + `"},
			"state": {"value": "anchored"}
		}
	}`)
	seedSelfTree(t, string(body))

	var tracksCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "tracks") {
			tracksCalls.Add(1)
			w.Write([]byte(`{}`))
			return
		}
		w.Write(body)
	}))
	defer srv.Close()

	settingsPath := settingsFileForServer(t, srv.URL)

	sampleTracks(settingsPath)

	if got := tracksCalls.Load(); got != 0 {
		t.Fatalf("sampleTracks must not hit the tracks endpoint directly, got %d call(s)", got)
	}

	// The cache is still cold; a handler's own get() must still warm it on
	// demand -- this is the "handler refreshes on demand" half of the fix,
	// confirming the removal above didn't leave the cache permanently cold.
	tracksAISTrails.get(settingsPath)
	if got := tracksCalls.Load(); got != 1 {
		t.Fatalf("expected get() to warm the cold cache exactly once, got %d", got)
	}
}

// TestStartTrackPoller_StopsOnContextCancellation pins the shutdown wiring
// (backend perf audit #10): the poller used to run forever regardless of
// streamCtx, so main.go's shutdown never stopped it. Now that
// tracksAISTrails.refresh is gone (the test above), sampleTracks makes no
// HTTP call at all for a self vessel with no usable position -- every field
// it reads comes off the in-memory snapshot -- so ticking is observed
// through windGustHistory (a plain, position-independent per-tick side
// effect) rather than an upstream request count.
func TestStartTrackPoller_StopsOnContextCancellation(t *testing.T) {
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	windGustHistory = newTelemetryRingBuffer(telemetryHistoryCapacity)

	body := []byte(`{
		"navigation": {
			"datetime": {"value": "` + time.Now().UTC().Format(time.RFC3339) + `"},
			"state": {"value": "anchored"}
		},
		"environment": {
			"wind": {"speedApparent": {"value": 5.0}}
		}
	}`)
	seedSelfTree(t, string(body))
	t.Setenv("SETTINGS_FILE", settingsFileForServer(t, "http://127.0.0.1:1"))

	ctx, cancel := context.WithCancel(context.Background())
	done := startTrackPoller(ctx, 5*time.Millisecond)

	waitFor(t, 2*time.Second, "at least one poll tick to land", func() bool {
		return len(windGustHistory.since(time.Time{})) > 0
	})

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("startTrackPoller did not stop after context cancellation")
	}

	// done closing is the synchronization point: everything the poller
	// goroutine ever did, including its last read of globalSignalKSnapshot,
	// happened-before this. A plain sleep here would give no such guarantee
	// against seedSelfTree's own cleanup swapping that global back out.
	afterCancel := len(windGustHistory.since(time.Time{}))
	time.Sleep(20 * time.Millisecond)
	if got := len(windGustHistory.since(time.Time{})); got != afterCancel {
		t.Fatalf("expected no further polls after context cancellation, had %d then %d", afterCancel, got)
	}
}

func TestGetTracksHandler_CachesAISFetchAndRevalidatesETag(t *testing.T) {
	resetTracksStateForTest(t)

	recordTrackSelf(-25.29, 152.91)

	tracksBody := map[string]any{
		"vessels.urn:mrn:imo:mmsi:123456789": map[string]any{
			"type":        "MultiLineString",
			"coordinates": [][][]float64{{{152.91, -25.29}, {152.92, -25.30}}},
		},
	}
	vesselsBody := map[string]any{
		"urn:mrn:imo:mmsi:123456789": map[string]any{
			"name": "PEGASUS",
		},
	}

	tracksJSON, _ := json.Marshal(tracksBody)
	vesselsJSON, _ := json.Marshal(vesselsBody)

	var trackCalls atomic.Int32
	started := make(chan struct{}, 1)
	release := make(chan struct{})

	snapshot := newSignalKSnapshot()
	for id, tree := range vesselsBody {
		if asMap, ok := tree.(map[string]any); ok {
			snapshot.contexts[vesselContextPrefix+id] = asMap
		}
	}
	snapshot.contexts["vessels.self"] = map[string]any{"name": "TESTSELF"}
	snapshot.setSelfContext("vessels.self")
	withGlobalSnapshot(t, snapshot)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/api/tracks") || strings.Contains(r.URL.Path, "v1/api/tracks"):
			if trackCalls.Add(1) == 1 {
				select {
				case started <- struct{}{}:
				default:
				}
			}
			<-release
			w.Write(tracksJSON)
		case strings.Contains(r.URL.Path, "/vessels/self"):
			w.Write([]byte(`{"name":"TESTSELF"}`))
		case strings.HasSuffix(r.URL.Path, "/vessels"):
			w.Write(vesselsJSON)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	settingsPath := settingsFileForServer(t, srv.URL)
	t.Setenv("SETTINGS_FILE", settingsPath)
	t.Setenv("SIGNALK_TRACKS_PATH", "/v1/api/tracks")
	t.Setenv("SIGNALK_VESSELS_PATH", "/vessels")
	t.Setenv("SIGNALK_VESSEL_PATH", "/vessels/self")

	type responseResult struct {
		rec *httptest.ResponseRecorder
		err error
	}
	doRequest := func(etag string) responseResult {
		req := httptest.NewRequest(http.MethodGet, "/api/tracks", nil)
		if etag != "" {
			req.Header.Set("If-None-Match", etag)
		}
		rec := httptest.NewRecorder()
		if err := getTracksHandler(echo.New().NewContext(req, rec)); err != nil {
			return responseResult{err: err}
		}
		return responseResult{rec: rec}
	}

	results := make(chan responseResult, 3)
	go func() { results <- doRequest("") }()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first tracks request never reached the upstream server")
	}

	go func() { results <- doRequest("") }()
	go func() { results <- doRequest("") }()

	close(release)

	var firstETag string
	for i := 0; i < 3; i++ {
		res := <-results
		if res.err != nil {
			t.Fatalf("tracks handler returned error: %v", res.err)
		}
		if res.rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", res.rec.Code)
		}
		if firstETag == "" {
			firstETag = res.rec.Header().Get("ETag")
		}
	}

	if got := trackCalls.Load(); got != 1 {
		t.Fatalf("expected one upstream AIS fetch to satisfy three concurrent requests, got %d", got)
	}
	if firstETag == "" {
		t.Fatal("expected an ETag on the cached tracks response")
	}

	revalidated := doRequest(firstETag)
	if revalidated.err != nil {
		t.Fatalf("ETag revalidation request failed: %v", revalidated.err)
	}
	if revalidated.rec.Code != http.StatusNotModified {
		t.Fatalf("expected 304 Not Modified for matching ETag, got %d", revalidated.rec.Code)
	}
	if body := strings.TrimSpace(revalidated.rec.Body.String()); body != "" {
		t.Fatalf("expected empty body for 304 response, got %q", body)
	}
}

func TestSampleTracks_RecordsSolarHistoryOnEachTick(t *testing.T) {
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	solarStats = &solarDayStats{yesterdayKWh: -1, peakTodayW: -1}
	solarPowerHistory = newTelemetryRingBuffer(solarTrendHistoryCapacity)

	body := []byte(`{
		"navigation": {
			"datetime": {"value": "` + time.Now().UTC().Format(time.RFC3339) + `"},
			"state": {"value": "anchored"}
		},
		"electrical": {
			"venus": {
				"totalPanelPower": {"value": 654.3}
			}
		}
	}`)
	seedSelfTree(t, string(body))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer srv.Close()

	settingsPath := settingsFileForServer(t, srv.URL)

	sampleTracks(settingsPath)

	solarPts := solarPowerHistory.since(time.Time{})
	if len(solarPts) != 1 {
		t.Fatalf("expected 1 solar sample recorded, got %d", len(solarPts))
	}
	if solarPts[0].Value != 654.3 {
		t.Fatalf("expected solar sample value 654.3, got %v", solarPts[0].Value)
	}

	if got := inMemorySolarPeakTodayW(); got != 654.3 {
		t.Fatalf("expected peak_today_w 654.3 fed from the same tick, got %v", got)
	}
}

func TestSampleTracks_SkipsSolarRecordingWhenCurrentWMissing(t *testing.T) {
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	solarStats = &solarDayStats{yesterdayKWh: -1, peakTodayW: -1}
	solarPowerHistory = newTelemetryRingBuffer(solarTrendHistoryCapacity)

	body := []byte(`{
		"navigation": {
			"datetime": {"value": "` + time.Now().UTC().Format(time.RFC3339) + `"},
			"state": {"value": "anchored"}
		}
	}`)
	seedSelfTree(t, string(body))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer srv.Close()

	settingsPath := settingsFileForServer(t, srv.URL)

	sampleTracks(settingsPath)

	solarPts := solarPowerHistory.since(time.Time{})
	if len(solarPts) != 0 {
		t.Fatalf("expected no solar sample recorded when current_w is missing, got %d", len(solarPts))
	}
	if got := inMemorySolarTodayKWh(); got != -1 {
		t.Fatalf("expected today_kwh sentinel -1 when nothing was recorded, got %v", got)
	}
}

// TestSampleTracks_RecordsTrueWindWhenPathIsFresh proves the happy path still
// works once recording is gated on each path's own LastSeen: a speedTrue/
// directionTrue delta that arrived moments ago is recorded.
func TestSampleTracks_RecordsTrueWindWhenPathIsFresh(t *testing.T) {
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	trueWindSpeedHistory = newTelemetryRingBuffer(windGustHistoryCapacity)
	trueWindDirectionHistory = newTelemetryRingBuffer(windGustHistoryCapacity)
	t.Cleanup(func() {
		trueWindSpeedHistory = newTelemetryRingBuffer(windGustHistoryCapacity)
		trueWindDirectionHistory = newTelemetryRingBuffer(windGustHistoryCapacity)
	})

	now := time.Now().UTC()
	snapshot := newSignalKSnapshot()
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{
			Timestamp: now.Format(time.RFC3339),
			Values: []signalKValue{
				{Path: "environment.wind.speedTrue", Value: 6.71},
				{Path: "environment.wind.directionTrue", Value: 2.233},
			},
		}},
	}, now)
	snapshot.setSelfContext("vessels.self")
	withGlobalSnapshot(t, snapshot)

	sampleTracks(filepath.Join(t.TempDir(), "settings.yaml"))

	speedPts := trueWindSpeedHistory.since(time.Time{})
	if len(speedPts) != 1 || speedPts[0].Value != 6.71 {
		t.Fatalf("expected one fresh true wind speed sample of 6.71, got %+v", speedPts)
	}
	directionPts := trueWindDirectionHistory.since(time.Time{})
	if len(directionPts) != 1 || directionPts[0].Value != 2.233 {
		t.Fatalf("expected one fresh true wind direction sample of 2.233, got %+v", directionPts)
	}
}

// TestSampleTracks_SkipsTrueWindWhenPathHasGoneStale is the regression test
// for the bug this gate fixes: derived-data can stop producing
// speedTrue/directionTrue while the rest of the snapshot (and the anemometer's
// own apparent-wind path) keeps updating. Before the LastSeen gate, the last
// value read off a since-gone-quiet path was recorded on every tick forever,
// pinning the Current Conditions tile's 1h "obs" marker (and feeding the
// storm/squash-zone derived paths) on a frozen number. The delta here landed
// well outside defaultWindMaxAge, so nothing should be recorded.
func TestSampleTracks_SkipsTrueWindWhenPathHasGoneStale(t *testing.T) {
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	trueWindSpeedHistory = newTelemetryRingBuffer(windGustHistoryCapacity)
	trueWindDirectionHistory = newTelemetryRingBuffer(windGustHistoryCapacity)
	t.Cleanup(func() {
		trueWindSpeedHistory = newTelemetryRingBuffer(windGustHistoryCapacity)
		trueWindDirectionHistory = newTelemetryRingBuffer(windGustHistoryCapacity)
	})

	staleAt := time.Now().UTC().Add(-10 * time.Minute)
	snapshot := newSignalKSnapshot()
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{
			Timestamp: staleAt.Format(time.RFC3339),
			Values: []signalKValue{
				{Path: "environment.wind.speedTrue", Value: 6.71},
				{Path: "environment.wind.directionTrue", Value: 2.233},
			},
		}},
	}, staleAt)
	snapshot.setSelfContext("vessels.self")
	withGlobalSnapshot(t, snapshot)

	sampleTracks(filepath.Join(t.TempDir(), "settings.yaml"))

	if pts := trueWindSpeedHistory.since(time.Time{}); len(pts) != 0 {
		t.Fatalf("expected no true wind speed sample recorded from a stale path, got %+v", pts)
	}
	if pts := trueWindDirectionHistory.since(time.Time{}); len(pts) != 0 {
		t.Fatalf("expected no true wind direction sample recorded from a stale path, got %+v", pts)
	}
}

// TestSampleTracks_SkipsTrueWindWhenLastSeenIsUnknown covers the other half
// of the gate: a tree seeded without ever going through applyDelta (as
// seedSelfTree does for REST-shaped fixtures elsewhere in this package) has
// no pathSeen entry at all, so LastSeen reads as the zero time.Time - "no
// evidence of freshness" must not be treated as fresh.
func TestSampleTracks_SkipsTrueWindWhenLastSeenIsUnknown(t *testing.T) {
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	trueWindSpeedHistory = newTelemetryRingBuffer(windGustHistoryCapacity)
	trueWindDirectionHistory = newTelemetryRingBuffer(windGustHistoryCapacity)
	t.Cleanup(func() {
		trueWindSpeedHistory = newTelemetryRingBuffer(windGustHistoryCapacity)
		trueWindDirectionHistory = newTelemetryRingBuffer(windGustHistoryCapacity)
	})

	seedSelfTree(t, `{
		"navigation": {"datetime": {"value": "`+time.Now().UTC().Format(time.RFC3339)+`"}, "state": {"value": "anchored"}},
		"environment": {"wind": {"speedTrue": {"value": 6.71}, "directionTrue": {"value": 2.233}}}
	}`)

	sampleTracks(filepath.Join(t.TempDir(), "settings.yaml"))

	if pts := trueWindSpeedHistory.since(time.Time{}); len(pts) != 0 {
		t.Fatalf("expected no true wind speed sample recorded with no known LastSeen, got %+v", pts)
	}
	if pts := trueWindDirectionHistory.since(time.Time{}); len(pts) != 0 {
		t.Fatalf("expected no true wind direction sample recorded with no known LastSeen, got %+v", pts)
	}
}

// TestRecordNearbyVesselContacts_LogsOncePerVesselIDWithNoMMSI is item 6's
// other required test: recordNearbyVesselContacts runs on the server's own
// 5s poll tick (unlike buildNearbyVesselsPayload, which only runs per
// client request), so without a dedup a vessel with no MMSI - genuinely
// visible, but with nothing this store can key a contact on - would log a
// fresh line every 5s for as long as it stayed in range.
func TestRecordNearbyVesselContacts_LogsOncePerVesselIDWithNoMMSI(t *testing.T) {
	dir := t.TempDir()
	store, err := newNearbyContactStore(filepath.Join(dir, "test.sqlite"))
	if err != nil {
		t.Fatalf("newNearbyContactStore: %v", err)
	}
	t.Cleanup(func() { _ = store.close() })
	original := globalNearbyContactStore
	globalNearbyContactStore = store
	t.Cleanup(func() { globalNearbyContactStore = original })

	// The AIS target below carries no "mmsi" field at all - the shape
	// vesselContactKey rejects - while self does, so fetchSignalKNearbyVessels
	// still resolves a nearby-vessel list of exactly one, unkeyable, contact.
	body := []byte(`{
		"self": {
			"mmsi": "518999323",
			"name": "Pikorua",
			"navigation": {"position": {"value": {"latitude": -21.595297, "longitude": 149.796444}}}
		},
		"urn:mrn:imo:mmsi:unknown": {
			"name": "NO MMSI BOAT",
			"navigation": {"position": {"value": {"latitude": -21.592353, "longitude": 149.780485}}}
		}
	}`)
	seedVesselTrees(t, string(body))

	var buf strings.Builder
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	state := vesselStateData{Latitude: -21.595297, Longitude: 149.796444, Status: "anchored"}
	for i := 0; i < 3; i++ {
		recordNearbyVesselContacts("", "", "", state, "")
	}

	if got := strings.Count(buf.String(), "NO MMSI BOAT"); got != 1 {
		t.Fatalf("expected exactly 1 log line for a vessel with no MMSI across 3 poll ticks, got %d:\n%s", got, buf.String())
	}
}

// TestRecordNearbyVesselContacts_RecordsEveryVesselInRangeNotJustTheNearestTen
// is the direct regression test for a code-review finding (2026-09-25):
// recordNearbyVesselContacts used to fetch through fetchSignalKNearbyVessels,
// which caps at the map tile's own display limit of 10 - so an 11th or 12th
// vessel in a crowded anchorage never got a sighting-log row at all, even
// though get_nearby_vessels (ADR 0128) lists up to 25. 12 distinct vessels,
// all within nearbyMaxRangeMeters, confirm none of them are silently
// dropped once recordNearbyVesselContacts fetches with nearbyVesselsUnlimited
// instead.
func TestRecordNearbyVesselContacts_RecordsEveryVesselInRangeNotJustTheNearestTen(t *testing.T) {
	store := newTestNearbyContactStore(t) // dwell=0: confirms on the first tick
	original := globalNearbyContactStore
	globalNearbyContactStore = store
	t.Cleanup(func() { globalNearbyContactStore = original })

	selfLat, selfLon := -21.595297, 149.796444

	trees := make([]string, 0, 12)
	for i := 0; i < 12; i++ {
		id := fmt.Sprintf("urn:mrn:imo:mmsi:%09d", i)
		trees = append(trees, fmt.Sprintf(
			`%q: {"name": "V%d", "mmsi": "%09d", "navigation": {"position": {"value": {"latitude": %f, "longitude": 149.780485}}}}`,
			id, i, i, -21.592000-float64(i)*0.0005,
		))
	}
	body := "{" + strings.Join(trees, ",") + "}"
	seedVesselTrees(t, body)

	state := vesselStateData{Latitude: selfLat, Longitude: selfLon, Status: "anchored"}
	recordNearbyVesselContacts("", "", "", state, "")

	latest, err := store.latestContactsByName("", 100)
	if err != nil {
		t.Fatalf("latestContactsByName: %v", err)
	}
	if len(latest) != 12 {
		t.Fatalf("expected all 12 in-range vessels to have a sighting-log row, got %d: %+v", len(latest), latest)
	}
}
