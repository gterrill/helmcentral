package main

import (
	"encoding/json"
	"fmt"
	"math"
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

// anchorTestEnv points ANCHOR_WATCH_FILE and SETTINGS_FILE at fresh temp
// files so these tests never touch the real cache/settings.yaml, and don't
// bleed anchor state into other tests via the package-level anchorWatchState.
func anchorTestEnv(t *testing.T, gpsFromBowM float64) string {
	t.Helper()
	dir := t.TempDir()
	stub := newPublishStub(t)
	withServiceAccount(t, stub)

	// setAnchorWatch's apply_bow_offset path calls fetchSignalKVesselState,
	// which runs GNSS position validation backed by package-level "last
	// trusted fix" state (gnss_validation.go). Every test in this package
	// that touches that path resets it on the way in and out, or it leaks
	// into unrelated tests that run later in the same binary.
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	t.Setenv("ANCHOR_WATCH_FILE", filepath.Join(dir, "anchor_watch.json"))

	settingsPath := filepath.Join(dir, "settings.yaml")
	body := fmt.Sprintf("signalk:\n  address: %s\nanchor:\n  gps_from_bow_m: %g\n", stub.server.URL, gpsFromBowM)
	if err := os.WriteFile(settingsPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	t.Setenv("SETTINGS_FILE", settingsPath)

	return settingsPath
}

// seedHeadingTrue seeds the self vessel tree with a headingTrue value already
// in degrees (i.e. > 2*pi so the payload parser doesn't mistake it for
// radians), matching how setAnchorWatch reads it via
// fetchSignalKVesselState/state.HeadingTrue.
func seedHeadingTrue(t *testing.T, headingDeg float64) {
	t.Helper()
	seedSelfTree(t, fmt.Sprintf(`{"navigation": {"headingTrue": {"value": %f}}}`, headingDeg+360))
}

// seedNoHeading seeds a self vessel tree with no heading at all, so
// state.HeadingTrue reads as the -1 "unavailable" sentinel.
func seedNoHeading(t *testing.T) {
	t.Helper()
	seedSelfTree(t, `{"navigation": {}}`)
}

func postAnchorWatch(t *testing.T, body map[string]any) (int, map[string]any) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/anchor-watch", strings.NewReader(string(raw)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()

	if err := setAnchorWatch(e.NewContext(req, rec)); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var decoded map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &decoded)
	return rec.Code, decoded
}

// Test 1: destinationPoint round-trips against haversineMeters.
func TestDestinationPoint_EightMetersNorth(t *testing.T) {
	lat, lon := -21.1113, 149.2276

	newLat, newLon := destinationPoint(lat, lon, 0, 8)

	dist := haversineMeters(lat, lon, newLat, newLon)
	if math.Abs(dist-8) > 0.01 {
		t.Fatalf("expected ~8m from origin, got %.4fm", dist)
	}
	if newLat <= lat {
		t.Fatalf("bearing 0 (north) should increase latitude: got %v -> %v", lat, newLat)
	}
	if math.Abs(newLon-lon) > 1e-9 {
		t.Fatalf("bearing 0 (north) should not change longitude: got %v -> %v", lon, newLon)
	}
}

// Test 2: setting with apply_bow_offset:true, heading 0, d=8 stores a point
// ~8m north of the raw fix.
func TestSetAnchorWatch_AppliesBowOffsetWhenHeadingAvailable(t *testing.T) {
	anchorTestEnv(t, 8)
	seedHeadingTrue(t, 0)

	fixLat, fixLon := -21.1113, 149.2276
	code, resp := postAnchorWatch(t, map[string]any{
		"lat":              fixLat,
		"lon":              fixLon,
		"apply_bow_offset": true,
	})

	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", code, resp)
	}
	if applied, _ := resp["bow_offset_applied"].(bool); !applied {
		t.Fatalf("expected bow_offset_applied true, got %+v", resp)
	}
	if got, _ := resp["bow_offset_m"].(float64); got != 8 {
		t.Fatalf("expected bow_offset_m 8, got %v", resp["bow_offset_m"])
	}

	storedLat, _ := resp["lat"].(float64)
	storedLon, _ := resp["lon"].(float64)

	dist := haversineMeters(fixLat, fixLon, storedLat, storedLon)
	if math.Abs(dist-8) > 0.01 {
		t.Fatalf("expected stored point ~8m from raw fix, got %.4fm", dist)
	}
	if storedLat <= fixLat {
		t.Fatalf("heading 0 should project the anchor north of the fix: fix=%v stored=%v", fixLat, storedLat)
	}
}

// Test 3: gps_from_bow_m: 0 stores the raw fix and reports bow_offset_applied: false.
func TestSetAnchorWatch_NoCorrectionWhenGPSFromBowUnconfigured(t *testing.T) {
	anchorTestEnv(t, 0)
	seedHeadingTrue(t, 0)

	fixLat, fixLon := -21.1113, 149.2276
	code, resp := postAnchorWatch(t, map[string]any{
		"lat":              fixLat,
		"lon":              fixLon,
		"apply_bow_offset": true,
	})

	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", code, resp)
	}
	if applied, _ := resp["bow_offset_applied"].(bool); applied {
		t.Fatalf("expected bow_offset_applied false when gps_from_bow_m is 0, got %+v", resp)
	}
	if got, _ := resp["lat"].(float64); got != fixLat {
		t.Fatalf("expected raw fix latitude stored, got %v want %v", got, fixLat)
	}
	if got, _ := resp["lon"].(float64); got != fixLon {
		t.Fatalf("expected raw fix longitude stored, got %v want %v", got, fixLon)
	}
}

// Test 4: heading unavailable (-1) stores the raw fix, reports the reason,
// and still returns 200 — never silently assume d=0 or substitute COG.
func TestSetAnchorWatch_HeadingUnavailableStoresRawFixWithReason(t *testing.T) {
	anchorTestEnv(t, 8)
	seedNoHeading(t)

	fixLat, fixLon := -21.1113, 149.2276
	code, resp := postAnchorWatch(t, map[string]any{
		"lat":              fixLat,
		"lon":              fixLon,
		"apply_bow_offset": true,
	})

	if code != http.StatusOK {
		t.Fatalf("expected 200 even when heading is unavailable, got %d: %+v", code, resp)
	}
	if applied, _ := resp["bow_offset_applied"].(bool); applied {
		t.Fatalf("expected bow_offset_applied false, got %+v", resp)
	}
	if got, _ := resp["bow_offset_reason"].(string); got != "heading unavailable" {
		t.Fatalf("expected bow_offset_reason %q, got %q", "heading unavailable", got)
	}
	if got, _ := resp["lat"].(float64); got != fixLat {
		t.Fatalf("expected raw fix latitude stored, got %v want %v", got, fixLat)
	}
	if got, _ := resp["lon"].(float64); got != fixLon {
		t.Fatalf("expected raw fix longitude stored, got %v want %v", got, fixLon)
	}
}

// Test 5: reposition (no apply_bow_offset) stores the exact point given,
// never shifted — dragging the anchor on the map must not shove it forward.
func TestSetAnchorWatch_RepositionNeverShiftsThePoint(t *testing.T) {
	anchorTestEnv(t, 8)
	seedHeadingTrue(t, 0)

	fixLat, fixLon := -21.1113, 149.2276
	code, resp := postAnchorWatch(t, map[string]any{
		"lat": fixLat,
		"lon": fixLon,
		// apply_bow_offset omitted entirely, like updatePosition() does.
	})

	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", code, resp)
	}
	if applied, _ := resp["bow_offset_applied"].(bool); applied {
		t.Fatalf("expected bow_offset_applied false on reposition, got %+v", resp)
	}
	if got, _ := resp["lat"].(float64); got != fixLat {
		t.Fatalf("reposition must store the exact latitude given, got %v want %v", got, fixLat)
	}
	if got, _ := resp["lon"].(float64); got != fixLon {
		t.Fatalf("reposition must store the exact longitude given, got %v want %v", got, fixLon)
	}
}

// Test 6: the regression that motivates the whole change. A boat on a fixed
// rode, swinging through headings 0/90/180/270 relative to the heading it
// was set at, must show a CONSTANT antenna-to-anchor distance once the
// stored anchor is bow-corrected — and a 2d-wide spread (oscillating between
// r_h and r_h+2d) when it is not, exactly as derived in the plan:
//
//	S(no correction) = raw fix = A - d*hHat(h0)
//	G(theta)          = A - (r_h+d)*hHat(theta)
//	|S - G(theta)|    = r_h at theta=h0, r_h+2d at theta=h0+180
//	|A - G(theta)|    = r_h+d for every theta (that's what the fix restores)
func TestAnchorSwing_BowCorrectionRemovesTheOscillation(t *testing.T) {
	const (
		d       = 8.0  // GPS antenna aft of the bow roller
		rH      = 20.0 // horizontal rode extent once the rode is taut
		h0      = 45.0 // heading at the moment the anchor was set
		trueLat = -21.1113
		trueLon = 149.2276
	)

	// F0: the raw GPS fix at set time. The boat is still over its anchor
	// (r_h ~= 0), so the antenna sits d metres behind the bow along h0 —
	// i.e. the true anchor is d metres *ahead* of the raw fix along h0.
	f0Lat, f0Lon := destinationPoint(trueLat, trueLon, deg2rad(h0+180), d)

	anchorTestEnv(t, d)
	seedHeadingTrue(t, h0)
	code, resp := postAnchorWatch(t, map[string]any{
		"lat":              f0Lat,
		"lon":              f0Lon,
		"apply_bow_offset": true,
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", code, resp)
	}
	if applied, _ := resp["bow_offset_applied"].(bool); !applied {
		t.Fatalf("expected the offset to apply, got %+v", resp)
	}
	correctedLat, _ := resp["lat"].(float64)
	correctedLon, _ := resp["lon"].(float64)

	// Sanity: the corrected point should land very close to the true anchor.
	if dist := haversineMeters(correctedLat, correctedLon, trueLat, trueLon); dist > 0.1 {
		t.Fatalf("corrected anchor should be ~on the true anchor, got %.4fm away", dist)
	}

	swingAngles := []float64{0, 90, 180, 270}
	var correctedDistances, uncorrectedDistances []float64

	for _, delta := range swingAngles {
		theta := h0 + delta
		// G(theta): antenna position while the boat is at heading theta with
		// the rode taut at r_h — d metres further aft than the bow, which
		// itself sits r_h out from the true anchor along theta.
		gLat, gLon := destinationPoint(trueLat, trueLon, deg2rad(theta+180), rH+d)

		correctedDistances = append(correctedDistances, haversineMeters(correctedLat, correctedLon, gLat, gLon))
		uncorrectedDistances = append(uncorrectedDistances, haversineMeters(f0Lat, f0Lon, gLat, gLon))
	}

	// With the correction on: constant at r_h+d across every swing angle.
	for i, dist := range correctedDistances {
		if math.Abs(dist-(rH+d)) > 0.5 {
			t.Fatalf("corrected distance at swing %v should be ~%.1fm (r_h+d), got %.4fm", swingAngles[i], rH+d, dist)
		}
	}
	maxCorrected, minCorrected := correctedDistances[0], correctedDistances[0]
	for _, dist := range correctedDistances {
		if dist > maxCorrected {
			maxCorrected = dist
		}
		if dist < minCorrected {
			minCorrected = dist
		}
	}
	if maxCorrected-minCorrected > 0.5 {
		t.Fatalf("corrected distances should be constant, spread was %.4fm: %v", maxCorrected-minCorrected, correctedDistances)
	}

	// With the correction off (raw fix as centre): spreads across a 2d band,
	// from r_h (aligned with h0) to r_h+2d (opposite h0).
	maxUncorrected, minUncorrected := uncorrectedDistances[0], uncorrectedDistances[0]
	for _, dist := range uncorrectedDistances {
		if dist > maxUncorrected {
			maxUncorrected = dist
		}
		if dist < minUncorrected {
			minUncorrected = dist
		}
	}
	if spread := maxUncorrected - minUncorrected; math.Abs(spread-2*d) > 0.5 {
		t.Fatalf("uncorrected spread should be ~2d (%.1fm), got %.4fm: %v", 2*d, spread, uncorrectedDistances)
	}
	if math.Abs(minUncorrected-rH) > 0.5 {
		t.Fatalf("uncorrected minimum (aligned with h0) should be ~r_h (%.1fm), got %.4fm", rH, minUncorrected)
	}
	if math.Abs(maxUncorrected-(rH+2*d)) > 0.5 {
		t.Fatalf("uncorrected maximum (opposite h0) should be ~r_h+2d (%.1fm), got %.4fm", rH+2*d, maxUncorrected)
	}
}

func deg2rad(deg float64) float64 { return deg * math.Pi / 180 }

// Test 7: setAnchorWatch resolves the anchorage's place name once in the
// background (docs/adr/0056) - the immediate response must not block on a
// place-names provider round trip, and the resolved name must round-trip
// through persistence (saveAnchorWatch/loadAnchorWatch) and both GET and
// PATCH.
func TestSetAnchorWatch_ResolvesAndPinsPlaceNameAsync(t *testing.T) {
	anchorTestEnv(t, 0)
	seedNoHeading(t)
	resetPlaceNameCache(t)
	resetPlaceNameTickState(t) // the resolve guard is process-wide; don't inherit another test's in-flight flag

	provider := &fakePlaceNameProvider{id: "fake-place-names", results: map[int]placeNameResult{
		400: {Name: "Goldsmith Island", Kind: "island"},
	}}
	withFakePlaceNameProviderResolver(t, provider)

	code, resp := postAnchorWatch(t, map[string]any{
		"lat": goldsmithLat,
		"lon": goldsmithLon,
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", code, resp)
	}
	if got, _ := resp["place_name"].(string); got != "" {
		t.Fatalf("expected place_name empty in the immediate response (resolved asynchronously), got %q", got)
	}

	waitForCondition(t, 2*time.Second, func() bool {
		anchorWatchMu.RLock()
		resolved := anchorWatchState != nil && anchorWatchState.PlaceName == "Goldsmith Island"
		anchorWatchMu.RUnlock()
		if !resolved {
			return false
		}
		// resolveAndPinAnchorWatchPlaceName updates the in-memory state and
		// THEN calls saveAnchorWatch (writeJSONFileAtomic, which now
		// fsyncs) - both on the same background goroutine, but with no
		// signal back to this goroutine for "the save has landed too".
		// Checking the file's own content directly here (rather than
		// racing straight into the memory-wipe-and-reload below the moment
		// the in-memory flag flips) is what makes this wait actually wait
		// for the persisted write, not just the update that precedes it.
		raw, err := os.ReadFile(anchorWatchFilePath())
		return err == nil && strings.Contains(string(raw), "Goldsmith Island")
	})

	// Persisted, not just held in memory: reload from disk the way a
	// server restart would.
	anchorWatchMu.Lock()
	anchorWatchState = nil
	anchorWatchMu.Unlock()
	loadAnchorWatch()

	anchorWatchMu.RLock()
	reloaded := anchorWatchState
	anchorWatchMu.RUnlock()
	if reloaded == nil || reloaded.PlaceName != "Goldsmith Island" {
		t.Fatalf("expected persisted place_name %q after reload, got %+v", "Goldsmith Island", reloaded)
	}

	// GET reflects the pinned name.
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/anchor-watch", nil)
	rec := httptest.NewRecorder()
	if err := getAnchorWatch(e.NewContext(req, rec)); err != nil {
		t.Fatalf("getAnchorWatch: %v", err)
	}
	var getResp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &getResp)
	if got, _ := getResp["place_name"].(string); got != "Goldsmith Island" {
		t.Fatalf("expected GET /api/anchor-watch place_name %q, got %q", "Goldsmith Island", got)
	}

	// PATCH (updating an unrelated field) must carry the pinned name
	// through untouched.
	patchCode, patchResp := patchAnchorWatchForTest(t, map[string]any{"sea_state": "choppy"})
	if patchCode != http.StatusOK {
		t.Fatalf("expected 200 from patch, got %d: %+v", patchCode, patchResp)
	}
	if got, _ := patchResp["place_name"].(string); got != "Goldsmith Island" {
		t.Fatalf("expected PATCH response to carry place_name %q through, got %q", "Goldsmith Island", got)
	}
}

// Test 8: a resolution failure (the place-names provider is unreachable)
// must log explicitly and leave PlaceName empty rather than caching or
// fabricating a blank name - the regular poll tick is what retries, per
// AGENTS.md's fail-fast policy.
func TestSetAnchorWatch_PlaceNameResolutionFailureLeavesFieldEmpty(t *testing.T) {
	anchorTestEnv(t, 0)
	seedNoHeading(t)
	resetPlaceNameCache(t)
	resetPlaceNameTickState(t) // the resolve guard is process-wide; don't inherit another test's in-flight flag

	provider := &fakePlaceNameProvider{id: "fake-place-names", errs: map[int]error{
		400: fmt.Errorf("simulated transport failure"),
	}}
	withFakePlaceNameProviderResolver(t, provider)

	code, resp := postAnchorWatch(t, map[string]any{
		"lat": goldsmithLat,
		"lon": goldsmithLon,
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", code, resp)
	}

	waitForCondition(t, 2*time.Second, func() bool { return provider.callCount() >= 1 })

	anchorWatchMu.RLock()
	got := anchorWatchState.PlaceName
	anchorWatchMu.RUnlock()
	if got != "" {
		t.Fatalf("expected place_name to remain empty after a resolution failure, got %q", got)
	}
}

// patchAnchorWatchForTest mirrors postAnchorWatch for the PATCH handler.
func patchAnchorWatchForTest(t *testing.T, body map[string]any) (int, map[string]any) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodPatch, "/api/anchor-watch", strings.NewReader(string(raw)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()

	if err := patchAnchorWatch(e.NewContext(req, rec)); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var decoded map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &decoded)
	return rec.Code, decoded
}

// resetAnchorWatchState clears the package-level anchor watch so a test that
// needs a genuine drop (current == nil) isn't at the mercy of whatever an
// earlier test in this binary happened to leave behind. anchorTestEnv only
// isolates the on-disk file and settings; anchorWatchState is an in-memory
// package var that outlives it.
func resetAnchorWatchState(t *testing.T) {
	t.Helper()
	anchorWatchMu.Lock()
	anchorWatchState = nil
	anchorWatchMu.Unlock()
}

// Test 9: POST persists the planning_depth_m / planning_tide_height_ft pair
// seeded at the moment of drop, and it reads back both from the POST
// response itself and from a subsequent GET.
func TestSetAnchorWatch_StoresThePlanningDepthAndTide(t *testing.T) {
	anchorTestEnv(t, 0)
	resetAnchorWatchState(t)

	code, resp := postAnchorWatch(t, map[string]any{
		"lat":                     -21.1113,
		"lon":                     149.2276,
		"planning_depth_m":        6.2,
		"planning_tide_height_ft": 1.4,
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", code, resp)
	}
	if got, _ := resp["planning_depth_m"].(float64); got != 6.2 {
		t.Fatalf("expected planning_depth_m 6.2 in POST response, got %v", resp["planning_depth_m"])
	}
	if got, _ := resp["planning_tide_height_ft"].(float64); got != 1.4 {
		t.Fatalf("expected planning_tide_height_ft 1.4 in POST response, got %v", resp["planning_tide_height_ft"])
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/anchor-watch", nil)
	rec := httptest.NewRecorder()
	if err := getAnchorWatch(e.NewContext(req, rec)); err != nil {
		t.Fatalf("getAnchorWatch: %v", err)
	}
	var getResp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &getResp)
	if got, _ := getResp["planning_depth_m"].(float64); got != 6.2 {
		t.Fatalf("expected GET planning_depth_m 6.2, got %v", getResp["planning_depth_m"])
	}
	if got, _ := getResp["planning_tide_height_ft"].(float64); got != 1.4 {
		t.Fatalf("expected GET planning_tide_height_ft 1.4, got %v", getResp["planning_tide_height_ft"])
	}
}

// Test 10: with neither field sent and no prior watch, planning_depth_m and
// planning_tide_height_ft default to the -1 "not captured" sentinel, never 0
// - 0 is a valid depth/tide reading on its own and must not be confused
// with "nothing was recorded".
func TestSetAnchorWatch_PlanningDepthDefaultsToSentinelWhenOmitted(t *testing.T) {
	anchorTestEnv(t, 0)
	resetAnchorWatchState(t)

	code, resp := postAnchorWatch(t, map[string]any{
		"lat": -21.1113,
		"lon": 149.2276,
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", code, resp)
	}
	if got, _ := resp["planning_depth_m"].(float64); got != -1 {
		t.Fatalf("expected planning_depth_m -1 when omitted, got %v", resp["planning_depth_m"])
	}
	if got, _ := resp["planning_tide_height_ft"].(float64); got != -1 {
		t.Fatalf("expected planning_tide_height_ft -1 when omitted, got %v", resp["planning_tide_height_ft"])
	}
}

// Test 11: the most important guard in this file. patchAnchorWatch rebuilds
// the whole record field-by-field from `current` before applying the PATCH
// (the `updated := &anchorWatchData{...}` literal); omitting the planning
// depth pair from that copy silently zeroes it out, and a working plan goes
// dark because the operator picked "choppy". A sea-state PATCH must carry
// planning_depth_m/planning_tide_height_ft through untouched, both in the
// response and across a disk round trip via loadAnchorWatch.
func TestPatchAnchorWatch_CarriesPlanningDepthThrough(t *testing.T) {
	anchorTestEnv(t, 0)
	resetAnchorWatchState(t)

	code, resp := postAnchorWatch(t, map[string]any{
		"lat":                     -21.1113,
		"lon":                     149.2276,
		"planning_depth_m":        6.2,
		"planning_tide_height_ft": 1.4,
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200 from post, got %d: %+v", code, resp)
	}

	patchCode, patchResp := patchAnchorWatchForTest(t, map[string]any{"sea_state": "choppy"})
	if patchCode != http.StatusOK {
		t.Fatalf("expected 200 from patch, got %d: %+v", patchCode, patchResp)
	}
	if got, _ := patchResp["planning_depth_m"].(float64); got != 6.2 {
		t.Fatalf("expected PATCH response to carry planning_depth_m 6.2 through, got %v", patchResp["planning_depth_m"])
	}
	if got, _ := patchResp["planning_tide_height_ft"].(float64); got != 1.4 {
		t.Fatalf("expected PATCH response to carry planning_tide_height_ft 1.4 through, got %v", patchResp["planning_tide_height_ft"])
	}

	// Survives a disk round trip, not just held in memory.
	resetAnchorWatchState(t)
	loadAnchorWatch()

	anchorWatchMu.RLock()
	reloaded := anchorWatchState
	anchorWatchMu.RUnlock()
	if reloaded == nil {
		t.Fatalf("expected a watch to load from disk")
	}
	if reloaded.PlanningDepthM != 6.2 {
		t.Fatalf("expected reloaded PlanningDepthM 6.2, got %v", reloaded.PlanningDepthM)
	}
	if reloaded.PlanningTideHeightFt != 1.4 {
		t.Fatalf("expected reloaded PlanningTideHeightFt 1.4, got %v", reloaded.PlanningTideHeightFt)
	}
}

// Test 12: the planning depth pair is directly patchable - it's how the
// operator corrects a wrong or missing depth after the drop, since the
// revert-to-capture button is gone and editing now overwrites in place. A
// PATCH setting both fields updates them, in the response and across a disk
// round trip via loadAnchorWatch, and planning_depth_m: -1 clears both - a
// half-cleared pair is not a thing.
func TestPatchAnchorWatch_UpdatesPlanningDepthAndPersists(t *testing.T) {
	anchorTestEnv(t, 0)
	resetAnchorWatchState(t)

	code, resp := postAnchorWatch(t, map[string]any{
		"lat": -21.1113,
		"lon": 149.2276,
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200 from post, got %d: %+v", code, resp)
	}

	setCode, setResp := patchAnchorWatchForTest(t, map[string]any{
		"planning_depth_m":        8.5,
		"planning_tide_height_ft": 0.9,
	})
	if setCode != http.StatusOK {
		t.Fatalf("expected 200 setting the planning depth, got %d: %+v", setCode, setResp)
	}
	if got, _ := setResp["planning_depth_m"].(float64); got != 8.5 {
		t.Fatalf("expected planning_depth_m 8.5, got %v", setResp["planning_depth_m"])
	}
	if got, _ := setResp["planning_tide_height_ft"].(float64); got != 0.9 {
		t.Fatalf("expected planning_tide_height_ft 0.9, got %v", setResp["planning_tide_height_ft"])
	}

	// Survives a disk round trip, not just held in memory.
	resetAnchorWatchState(t)
	loadAnchorWatch()

	anchorWatchMu.RLock()
	reloaded := anchorWatchState
	anchorWatchMu.RUnlock()
	if reloaded == nil {
		t.Fatalf("expected a watch to load from disk")
	}
	if reloaded.PlanningDepthM != 8.5 {
		t.Fatalf("expected reloaded PlanningDepthM 8.5, got %v", reloaded.PlanningDepthM)
	}
	if reloaded.PlanningTideHeightFt != 0.9 {
		t.Fatalf("expected reloaded PlanningTideHeightFt 0.9, got %v", reloaded.PlanningTideHeightFt)
	}

	clearCode, clearResp := patchAnchorWatchForTest(t, map[string]any{"planning_depth_m": -1.0})
	if clearCode != http.StatusOK {
		t.Fatalf("expected 200 clearing the planning depth, got %d: %+v", clearCode, clearResp)
	}
	if got, _ := clearResp["planning_depth_m"].(float64); got != -1 {
		t.Fatalf("expected planning_depth_m -1 after clearing, got %v", clearResp["planning_depth_m"])
	}
	if got, _ := clearResp["planning_tide_height_ft"].(float64); got != -1 {
		t.Fatalf("expected planning_tide_height_ft -1 after clearing (a half-cleared pair is not a thing), got %v", clearResp["planning_tide_height_ft"])
	}
}

// Test 13: the pair rule, enforced at the boundary. A positive
// planning_depth_m arriving without planning_tide_height_ft in the same
// PATCH is rejected - there is no way to know what instant the depth
// reading is from otherwise.
func TestPatchAnchorWatch_RejectsPlanningDepthWithoutItsTideStamp(t *testing.T) {
	anchorTestEnv(t, 0)
	resetAnchorWatchState(t)

	code, resp := postAnchorWatch(t, map[string]any{
		"lat": -21.1113,
		"lon": 149.2276,
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200 from post, got %d: %+v", code, resp)
	}

	patchCode, patchResp := patchAnchorWatchForTest(t, map[string]any{"planning_depth_m": 8.5})
	if patchCode != http.StatusBadRequest {
		t.Fatalf("expected 400 when planning_depth_m arrives without its tide stamp, got %d: %+v", patchCode, patchResp)
	}
}

// Test 14: a clear-only body of just planning_depth_m: -1 must not trip the
// "no patch fields provided" 400 - it's a legitimate patch (clearing the
// planning depth), not an empty one.
func TestPatchAnchorWatch_PlanningDepthOnlyBodyIsNotEmpty(t *testing.T) {
	anchorTestEnv(t, 0)
	resetAnchorWatchState(t)

	code, resp := postAnchorWatch(t, map[string]any{
		"lat": -21.1113,
		"lon": 149.2276,
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200 from post, got %d: %+v", code, resp)
	}

	patchCode, patchResp := patchAnchorWatchForTest(t, map[string]any{"planning_depth_m": -1.0})
	if patchCode != http.StatusOK {
		t.Fatalf("expected 200, not the empty-body 400, got %d: %+v", patchCode, patchResp)
	}
	if got, _ := patchResp["error"].(string); got == "no patch fields provided" {
		t.Fatalf("planning_depth_m: -1 alone must not be treated as an empty patch body")
	}
}

// Test 15: updatePosition (a map marker drag) is a POST, and it must not
// wipe the planning depth pair the way a naive full-replace would. A drop
// with the pair, followed by a reposition POST with neither
// apply_bow_offset nor the pair, must carry the pair forward from the
// previous record.
func TestSetAnchorWatch_RepositionCarriesPlanningDepthForward(t *testing.T) {
	anchorTestEnv(t, 8)
	seedHeadingTrue(t, 0)
	resetAnchorWatchState(t)

	dropCode, dropResp := postAnchorWatch(t, map[string]any{
		"lat":                     -21.1113,
		"lon":                     149.2276,
		"apply_bow_offset":        true,
		"planning_depth_m":        6.2,
		"planning_tide_height_ft": 1.4,
	})
	if dropCode != http.StatusOK {
		t.Fatalf("expected 200 from drop, got %d: %+v", dropCode, dropResp)
	}

	repositionCode, repositionResp := postAnchorWatch(t, map[string]any{
		"lat": -21.1120,
		"lon": 149.2280,
		// apply_bow_offset and the depth/tide pair both omitted, exactly
		// like a map marker drag (updatePosition) does.
	})
	if repositionCode != http.StatusOK {
		t.Fatalf("expected 200 from reposition, got %d: %+v", repositionCode, repositionResp)
	}
	if got, _ := repositionResp["planning_depth_m"].(float64); got != 6.2 {
		t.Fatalf("expected planning_depth_m 6.2 to survive the reposition, got %v", repositionResp["planning_depth_m"])
	}
	if got, _ := repositionResp["planning_tide_height_ft"].(float64); got != 1.4 {
		t.Fatalf("expected planning_tide_height_ft 1.4 to survive the reposition, got %v", repositionResp["planning_tide_height_ft"])
	}
}

// Test 16: a legacy anchor_watch.json written before this change has neither
// of the new fields. loadAnchorWatch must decode it without error, and the
// missing fields must read as 0 (not -1) and be reported as 0 - the "not
// captured" state, with no migration, per the read predicate (>0 for depth,
// >=0 for tide) documented on anchorWatchData.
func TestLoadAnchorWatch_LegacyFileWithNoPlanningDepthReadsAsUnset(t *testing.T) {
	anchorTestEnv(t, 0)

	legacy := `{
		"lat": -21.1113,
		"lon": 149.2276,
		"radius_meters": 20,
		"rode_deployed_m": 30,
		"sea_state": "calm",
		"seabed_type": "sand",
		"set_at": "2026-01-01T00:00:00Z",
		"bow_offset_m": 0,
		"bow_offset_applied": false,
		"bow_offset_reason": "",
		"heading_at_set_deg": -1,
		"place_name": "Legacy Cove"
	}`
	if err := os.WriteFile(anchorWatchFilePath(), []byte(legacy), 0o644); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}

	resetAnchorWatchState(t)
	loadAnchorWatch()

	anchorWatchMu.RLock()
	loaded := anchorWatchState
	anchorWatchMu.RUnlock()
	if loaded == nil {
		t.Fatalf("expected the legacy file to load")
	}
	if loaded.PlanningDepthM != 0 {
		t.Fatalf("expected PlanningDepthM 0 for a legacy file with no depth fields, got %v", loaded.PlanningDepthM)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/anchor-watch", nil)
	rec := httptest.NewRecorder()
	if err := getAnchorWatch(e.NewContext(req, rec)); err != nil {
		t.Fatalf("getAnchorWatch: %v", err)
	}
	var getResp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &getResp)
	if got, _ := getResp["planning_depth_m"].(float64); got != 0 {
		t.Fatalf("expected GET planning_depth_m 0 for a legacy record, got %v", getResp["planning_depth_m"])
	}
}

// Test 17: the other half of the pair rule. A PATCH body containing only
// planning_tide_height_ft, with no planning_depth_m, must be rejected -
// applying it would fall through to the apply block's stamp-only branch and
// re-stamp the EXISTING planning depth with a tide height read at a
// different instant. That is the same datum-mixing error the pair rule
// exists to prevent (see Test 13), just arriving from the other direction,
// and it must not silently corrupt a stored pair.
func TestPatchAnchorWatch_RejectsTideStampWithoutItsDepth(t *testing.T) {
	anchorTestEnv(t, 0)
	resetAnchorWatchState(t)

	code, resp := postAnchorWatch(t, map[string]any{
		"lat": -21.1113,
		"lon": 149.2276,
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200 from post, got %d: %+v", code, resp)
	}

	setCode, setResp := patchAnchorWatchForTest(t, map[string]any{
		"planning_depth_m":        8.5,
		"planning_tide_height_ft": 0.9,
	})
	if setCode != http.StatusOK {
		t.Fatalf("expected 200 establishing the planning depth pair, got %d: %+v", setCode, setResp)
	}

	patchCode, patchResp := patchAnchorWatchForTest(t, map[string]any{"planning_tide_height_ft": 4.0})
	if patchCode != http.StatusBadRequest {
		t.Fatalf("expected 400 when planning_tide_height_ft arrives without planning_depth_m, got %d: %+v", patchCode, patchResp)
	}

	// The stored pair must be untouched by the rejected patch - not
	// re-stamped with the new tide height and not partially applied.
	anchorWatchMu.RLock()
	depth := anchorWatchState.PlanningDepthM
	tide := anchorWatchState.PlanningTideHeightFt
	anchorWatchMu.RUnlock()
	if depth != 8.5 {
		t.Fatalf("expected planning_depth_m to remain 8.5 after the rejected patch, got %v", depth)
	}
	if tide != 0.9 {
		t.Fatalf("expected planning_tide_height_ft to remain 0.9 after the rejected patch, got %v", tide)
	}
}

// Test 18: patchAnchorWatch does a read-modify-write of the whole record
// (see the comment on Test 11). The old implementation released its RLock
// immediately after reading `current` and only took the write Lock at the
// very end, after saveAnchorWatch's real disk write - a wide-enough window
// for two concurrent PATCHes to both read the same `current` and for the
// second writer's full-record rebuild (built from the now-stale snapshot) to
// silently discard the first writer's change. This is documented as a known
// trade in docs/adr/0063-planning-depth-captured-at-set.md's Consequences
// section, and became reachable in practice because the Rode Planner
// debounces depth on its own timer alongside the existing rode/sea-state/
// seabed timer, so a sea-state change and a depth change landing in the same
// 800ms window fire two overlapping PATCHes.
//
// Fires N concurrent PATCHes, each setting a different field, and requires
// every one of them to have landed. Repeated over several trials because a
// lost update is scheduler-dependent, not guaranteed on any single run -
// against the old RLock-then-Lock implementation this reliably loses at
// least one field within a handful of trials; against the fixed
// whole-handler Lock it cannot, because the mutex makes the five PATCHes
// strictly sequential.
func TestPatchAnchorWatch_ConcurrentPatchesDoNotLoseUpdates(t *testing.T) {
	anchorTestEnv(t, 0)

	const trials = 5
	for trial := 0; trial < trials; trial++ {
		resetAnchorWatchState(t)
		code, resp := postAnchorWatch(t, map[string]any{
			"lat": -21.1113,
			"lon": 149.2276,
		})
		if code != http.StatusOK {
			t.Fatalf("trial %d: expected 200 from post, got %d: %+v", trial, code, resp)
		}

		radius := 30.0 + float64(trial)
		rode := 40.0 + float64(trial)
		seaState := "choppy"
		if trial%2 == 1 {
			seaState = "rough"
		}
		seabedType := "rock"
		if trial%2 == 1 {
			seabedType = "grass"
		}
		planningDepth := 8.0 + float64(trial)*0.1
		planningTide := 0.5 + float64(trial)*0.1

		bodies := []map[string]any{
			{"radius_meters": radius},
			{"rode_deployed_m": rode},
			{"sea_state": seaState},
			{"seabed_type": seabedType},
			{"planning_depth_m": planningDepth, "planning_tide_height_ft": planningTide},
		}

		var wg sync.WaitGroup
		start := make(chan struct{})
		errCh := make(chan error, len(bodies))

		for _, body := range bodies {
			wg.Add(1)
			// Build the request/recorder up front and block each goroutine
			// on the start gate right before the handler call, so closing
			// the gate releases all of them as close to simultaneously as
			// the scheduler allows.
			go func(body map[string]any) {
				defer wg.Done()
				raw, err := json.Marshal(body)
				if err != nil {
					errCh <- fmt.Errorf("marshal %+v: %w", body, err)
					return
				}
				e := echo.New()
				req := httptest.NewRequest(http.MethodPatch, "/api/anchor-watch", strings.NewReader(string(raw)))
				req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
				rec := httptest.NewRecorder()

				<-start
				if err := patchAnchorWatch(e.NewContext(req, rec)); err != nil {
					errCh <- fmt.Errorf("handler for %+v: %w", body, err)
					return
				}
				if rec.Code != http.StatusOK {
					errCh <- fmt.Errorf("patch %+v: expected 200, got %d: %s", body, rec.Code, rec.Body.String())
				}
			}(body)
		}
		close(start)
		wg.Wait()
		close(errCh)
		for err := range errCh {
			t.Fatalf("trial %d: %v", trial, err)
		}

		anchorWatchMu.RLock()
		final := *anchorWatchState
		anchorWatchMu.RUnlock()

		if final.RadiusMeters != radius {
			t.Fatalf("trial %d: expected radius_meters %v to land, got %v (lost update)", trial, radius, final.RadiusMeters)
		}
		if final.RodeDeployedM != rode {
			t.Fatalf("trial %d: expected rode_deployed_m %v to land, got %v (lost update)", trial, rode, final.RodeDeployedM)
		}
		if final.SeaState != seaState {
			t.Fatalf("trial %d: expected sea_state %q to land, got %q (lost update)", trial, seaState, final.SeaState)
		}
		if final.SeabedType != seabedType {
			t.Fatalf("trial %d: expected seabed_type %q to land, got %q (lost update)", trial, seabedType, final.SeabedType)
		}
		if final.PlanningDepthM != planningDepth {
			t.Fatalf("trial %d: expected planning_depth_m %v to land, got %v (lost update)", trial, planningDepth, final.PlanningDepthM)
		}
		if final.PlanningTideHeightFt != planningTide {
			t.Fatalf("trial %d: expected planning_tide_height_ft %v to land, got %v (lost update)", trial, planningTide, final.PlanningTideHeightFt)
		}
	}
}

// Test 19: set_at identifies the anchoring session, so it is minted at the
// drop and carried forward across a reposition. A map marker drag corrects
// where you believe the hook lies; it does not begin a new anchorage (the
// same rule that keeps placemarks alive across a reposition, ADR 0048), and
// clients key their map view off set_at to decide whether the view they are
// showing belongs to the anchorage now under the boat.
func TestSetAnchorWatch_RepositionKeepsTheSessionSetAt(t *testing.T) {
	anchorTestEnv(t, 8)
	seedHeadingTrue(t, 0)
	resetAnchorWatchState(t)

	dropCode, dropResp := postAnchorWatch(t, map[string]any{
		"lat":              -21.1113,
		"lon":              149.2276,
		"apply_bow_offset": true,
	})
	if dropCode != http.StatusOK {
		t.Fatalf("expected 200 from drop, got %d: %+v", dropCode, dropResp)
	}

	// Backdate the session rather than sleeping: set_at serialises at
	// RFC3339 second resolution, so a re-minted stamp could otherwise
	// coincide with the drop's and pass by accident.
	dropped := time.Date(2026, 8, 20, 6, 30, 0, 0, time.UTC)
	anchorWatchMu.Lock()
	anchorWatchState.SetAt = dropped
	anchorWatchMu.Unlock()
	want := dropped.Format(time.RFC3339)

	repositionCode, repositionResp := postAnchorWatch(t, map[string]any{
		"lat": -21.1120,
		"lon": 149.2280,
		// apply_bow_offset omitted, exactly like a map marker drag.
	})
	if repositionCode != http.StatusOK {
		t.Fatalf("expected 200 from reposition, got %d: %+v", repositionCode, repositionResp)
	}
	if got, _ := repositionResp["set_at"].(string); got != want {
		t.Fatalf("expected set_at %q to survive the reposition, got %q", want, got)
	}

	// GET must agree with the POST echo — clients poll it, not the echo.
	e := echo.New()
	rec := httptest.NewRecorder()
	if err := getAnchorWatch(e.NewContext(httptest.NewRequest(http.MethodGet, "/api/anchor-watch", nil), rec)); err != nil {
		t.Fatalf("getAnchorWatch: %v", err)
	}
	var state map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &state)
	if got, _ := state["set_at"].(string); got != want {
		t.Fatalf("expected GET set_at %q, got %q", want, got)
	}
}

// Test 20: the other half of Test 19. Raising ends the session, so the next
// drop is a new anchorage and must mint a fresh set_at — that difference is
// the only signal a client has that the boat is somewhere else now.
func TestSetAnchorWatch_DropAfterRaiseMintsANewSetAt(t *testing.T) {
	anchorTestEnv(t, 8)
	seedHeadingTrue(t, 0)
	resetAnchorWatchState(t)

	if code, resp := postAnchorWatch(t, map[string]any{
		"lat":              -21.1113,
		"lon":              149.2276,
		"apply_bow_offset": true,
	}); code != http.StatusOK {
		t.Fatalf("expected 200 from the first drop, got %d: %+v", code, resp)
	}

	first := time.Date(2026, 8, 20, 6, 30, 0, 0, time.UTC)
	anchorWatchMu.Lock()
	anchorWatchState.SetAt = first
	anchorWatchMu.Unlock()

	e := echo.New()
	rec := httptest.NewRecorder()
	if err := deleteAnchorWatch(e.NewContext(httptest.NewRequest(http.MethodDelete, "/api/anchor-watch", nil), rec)); err != nil {
		t.Fatalf("deleteAnchorWatch: %v", err)
	}

	_, secondResp := postAnchorWatch(t, map[string]any{
		"lat":              -21.2500,
		"lon":              149.3000,
		"apply_bow_offset": true,
	})
	secondSetAt, _ := secondResp["set_at"].(string)
	if secondSetAt == "" {
		t.Fatalf("expected a set_at on the second drop, got %+v", secondResp)
	}
	if secondSetAt == first.Format(time.RFC3339) {
		t.Fatalf("expected a new session set_at after a raise, got the previous session's %q", secondSetAt)
	}
}

// Test 21: a reposition must not wipe the rode and conditions the operator
// entered. Dragging the marker says where the hook lies, nothing about how
// much chain is out or what the seabed is, so those carry forward from the
// active watch the way the radius and the planning depth already do. On a
// genuine drop there is nothing to carry from, so the defaults stand.
func TestSetAnchorWatch_RepositionCarriesRodeAndConditionsForward(t *testing.T) {
	anchorTestEnv(t, 8)
	seedHeadingTrue(t, 0)
	resetAnchorWatchState(t)

	if code, resp := postAnchorWatch(t, map[string]any{
		"lat":              -21.1113,
		"lon":              149.2276,
		"apply_bow_offset": true,
	}); code != http.StatusOK {
		t.Fatalf("expected 200 from drop, got %d: %+v", code, resp)
	}

	if code, resp := patchAnchorWatchForTest(t, map[string]any{
		"rode_deployed_m": 42.5,
		"sea_state":       "choppy",
		"seabed_type":     "mud",
	}); code != http.StatusOK {
		t.Fatalf("expected 200 from the rode/conditions PATCH, got %d: %+v", code, resp)
	}

	code, resp := postAnchorWatch(t, map[string]any{
		"lat": -21.1120,
		"lon": 149.2280,
		// apply_bow_offset omitted, exactly like a map marker drag.
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200 from reposition, got %d: %+v", code, resp)
	}
	if got, _ := resp["rode_deployed_m"].(float64); got != 42.5 {
		t.Fatalf("expected rode_deployed_m 42.5 to survive the reposition, got %v", resp["rode_deployed_m"])
	}
	if got, _ := resp["sea_state"].(string); got != "choppy" {
		t.Fatalf("expected sea_state %q to survive the reposition, got %q", "choppy", got)
	}
	if got, _ := resp["seabed_type"].(string); got != "mud" {
		t.Fatalf("expected seabed_type %q to survive the reposition, got %q", "mud", got)
	}

	// GET must agree with the POST echo — the planner reads the poll.
	e := echo.New()
	rec := httptest.NewRecorder()
	if err := getAnchorWatch(e.NewContext(httptest.NewRequest(http.MethodGet, "/api/anchor-watch", nil), rec)); err != nil {
		t.Fatalf("getAnchorWatch: %v", err)
	}
	var state map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &state)
	if got, _ := state["rode_deployed_m"].(float64); got != 42.5 {
		t.Fatalf("expected GET rode_deployed_m 42.5, got %v", state["rode_deployed_m"])
	}
	if got, _ := state["sea_state"].(string); got != "choppy" {
		t.Fatalf("expected GET sea_state %q, got %q", "choppy", got)
	}
	if got, _ := state["seabed_type"].(string); got != "mud" {
		t.Fatalf("expected GET seabed_type %q, got %q", "mud", got)
	}
}

// Test 22: the other side of Test 21. A genuine drop starts from the
// defaults, so last anchorage's 42.5 m of chain and rough-water settings
// cannot follow the boat into a new bay.
func TestSetAnchorWatch_DropAfterRaiseResetsRodeAndConditions(t *testing.T) {
	anchorTestEnv(t, 8)
	seedHeadingTrue(t, 0)
	resetAnchorWatchState(t)

	if code, resp := postAnchorWatch(t, map[string]any{
		"lat":              -21.1113,
		"lon":              149.2276,
		"apply_bow_offset": true,
	}); code != http.StatusOK {
		t.Fatalf("expected 200 from the first drop, got %d: %+v", code, resp)
	}
	if code, resp := patchAnchorWatchForTest(t, map[string]any{
		"rode_deployed_m": 42.5,
		"sea_state":       "rough",
		"seabed_type":     "rock",
	}); code != http.StatusOK {
		t.Fatalf("expected 200 from the rode/conditions PATCH, got %d: %+v", code, resp)
	}

	e := echo.New()
	rec := httptest.NewRecorder()
	if err := deleteAnchorWatch(e.NewContext(httptest.NewRequest(http.MethodDelete, "/api/anchor-watch", nil), rec)); err != nil {
		t.Fatalf("deleteAnchorWatch: %v", err)
	}

	_, resp := postAnchorWatch(t, map[string]any{
		"lat":              -21.2500,
		"lon":              149.3000,
		"apply_bow_offset": true,
	})
	if got, _ := resp["rode_deployed_m"].(float64); got != 0 {
		t.Fatalf("expected a new anchorage to start with no rode recorded, got %v", resp["rode_deployed_m"])
	}
	if got, _ := resp["sea_state"].(string); got != "calm" {
		t.Fatalf("expected a new anchorage to start at sea_state calm, got %q", got)
	}
	if got, _ := resp["seabed_type"].(string); got != "sand" {
		t.Fatalf("expected a new anchorage to start at seabed_type sand, got %q", got)
	}
}
