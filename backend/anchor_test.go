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
	"testing"

	"github.com/labstack/echo/v4"
)

// anchorTestEnv points ANCHOR_WATCH_FILE and SETTINGS_FILE at fresh temp
// files so these tests never touch the real cache/settings.yaml, and don't
// bleed anchor state into other tests via the package-level anchorWatchState.
func anchorTestEnv(t *testing.T, gpsFromBowM float64) string {
	t.Helper()
	dir := t.TempDir()

	// setAnchorWatch's apply_bow_offset path calls fetchSignalKVesselState,
	// which runs GNSS position validation backed by package-level "last
	// trusted fix" state (gnss_validation.go). Every test in this package
	// that touches that path resets it on the way in and out, or it leaks
	// into unrelated tests that run later in the same binary.
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	t.Setenv("ANCHOR_WATCH_FILE", filepath.Join(dir, "anchor_watch.json"))

	settingsPath := filepath.Join(dir, "settings.yaml")
	body := fmt.Sprintf("anchor:\n  gps_from_bow_m: %g\n", gpsFromBowM)
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
