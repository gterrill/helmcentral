package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func trustedSignalKPayload(latitude, longitude float64) string {
	return fmt.Sprintf(`{
		"navigation": {
			"datetime": {"value": %q},
			"state": {"value": "anchored"},
			"position": {"value": {"latitude": %f, "longitude": %f}},
			"gnss": {
				"methodQuality": {"value": 1},
				"horizontalDilution": {"value": 0.9},
				"satellites": {"value": 8}
			}
		}
	}`, time.Now().UTC().Format(time.RFC3339), latitude, longitude)
}

// trustedSignalKPayloadServer both seeds the delta-stream snapshot and serves
// the same payload over HTTP. Telemetry reads the snapshot (ADR 0037) while
// probe and settings-validation tests still need a real address to fetch, and
// callers of this helper span both.
func trustedSignalKPayloadServer(t *testing.T, latitude, longitude float64) *httptest.Server {
	t.Helper()
	body := trustedSignalKPayload(latitude, longitude)
	seedSelfTree(t, body)

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
}

func approxEqual(a, b, tolerance float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff <= tolerance
}

// TestNormalizeSettingsPayload_PersistsUnregisteredTideProviderAsSubmitted
// guards against a regression of the bug where any tide_provider value not
// currently in the registry (e.g. a WASM plugin not yet built/loaded, or a
// simple typo) was silently rewritten to a hardcoded default provider id on
// every settings save - discarding the caller's choice without any error.
// tide_provider should round-trip exactly like tide_station_id/tide_station_name already
// do a few lines below it; registry validation belongs at read time
// (tideToday, tideChartHandler, ...), not at write time.
func TestNormalizeSettingsPayload_PersistsUnregisteredTideProviderAsSubmitted(t *testing.T) {
	req := settingsPayload{}
	req.UI.TideProvider = "not-a-real-provider"

	normalized := normalizeSettingsPayload(req)

	if normalized.UI.TideProvider != "not-a-real-provider" {
		t.Fatalf("expected tide_provider to be persisted as submitted, got %q", normalized.UI.TideProvider)
	}
}

// TestBuildSettingsPayload_SurfacesUnregisteredTideProviderFromDisk guards
// the read-side counterpart: GET /api/settings must reflect whatever is
// actually stored in settings.yaml, even if the configured provider isn't
// currently registered (e.g. between a settings save and a container
// restart that loads the new plugin) - not silently substitute a hardcoded
// default provider id, which would both mislead the Settings UI and risk
// permanently clobbering the real value on the next save.
func TestBuildSettingsPayload_SurfacesUnregisteredTideProviderFromDisk(t *testing.T) {
	settings := map[string]any{
		"ui": map[string]any{
			"tide_provider": "not-a-real-provider",
		},
	}

	payload := buildSettingsPayload(settings)

	if payload.UI.TideProvider != "not-a-real-provider" {
		t.Fatalf("expected tide_provider to surface the stored value, got %q", payload.UI.TideProvider)
	}
}

// TestNormalizeSettingsPayload_RoundTripsInfluxdbSection mirrors the
// tide-provider round-trip test above: the influxdb section (enabled/url/
// org/bucket) must be persisted as submitted, trimmed of whitespace, with no
// silent defaulting or dropping of fields. Note there is deliberately no
// token field here - INFLUXDB_TOKEN is env-only and never round-trips
// through settings.
func TestNormalizeSettingsPayload_RoundTripsInfluxdbSection(t *testing.T) {
	req := settingsPayload{}
	req.Influxdb.Enabled = true
	req.Influxdb.URL = " http://localhost:8086 "
	req.Influxdb.Org = " myorg "
	req.Influxdb.Bucket = " mybucket "

	normalized := normalizeSettingsPayload(req)

	if !normalized.Influxdb.Enabled {
		t.Fatalf("expected influxdb.enabled to round-trip true")
	}
	if normalized.Influxdb.URL != "http://localhost:8086" {
		t.Fatalf("expected influxdb.url to be trimmed, got %q", normalized.Influxdb.URL)
	}
	if normalized.Influxdb.Org != "myorg" {
		t.Fatalf("expected influxdb.org to be trimmed, got %q", normalized.Influxdb.Org)
	}
	if normalized.Influxdb.Bucket != "mybucket" {
		t.Fatalf("expected influxdb.bucket to be trimmed, got %q", normalized.Influxdb.Bucket)
	}
}

// TestBuildSettingsPayload_SurfacesInfluxdbSectionFromDisk is the read-side
// counterpart: GET /api/settings must reflect whatever influxdb section is
// actually stored in settings.yaml.
func TestBuildSettingsPayload_SurfacesInfluxdbSectionFromDisk(t *testing.T) {
	settings := map[string]any{
		"influxdb": map[string]any{
			"enabled": true,
			"url":     "http://localhost:8086",
			"org":     "myorg",
			"bucket":  "mybucket",
		},
	}

	payload := buildSettingsPayload(settings)

	if !payload.Influxdb.Enabled {
		t.Fatalf("expected influxdb.enabled to surface as true")
	}
	if payload.Influxdb.URL != "http://localhost:8086" || payload.Influxdb.Org != "myorg" || payload.Influxdb.Bucket != "mybucket" {
		t.Fatalf("unexpected influxdb section: %+v", payload.Influxdb)
	}
}

// TestNormalizeSettingsPayload_PreservesGPSFromBowMZero guards the anchor
// bow-offset correction (see docs on setAnchorWatch): gps_from_bow_m
// defaults to 0, meaning "no correction", so 0 is a meaningful explicit
// value rather than an absent one. Unlike every other anchor field, it must
// survive load -> normalize -> emit without being clamped up to a
// non-zero default — a guessed antenna position is worse than none.
func TestNormalizeSettingsPayload_PreservesGPSFromBowMZero(t *testing.T) {
	req := settingsPayload{}
	req.Anchor.GPSFromBowM = 0

	normalized := normalizeSettingsPayload(req)

	if normalized.Anchor.GPSFromBowM != 0 {
		t.Fatalf("expected gps_from_bow_m to stay 0, got %v", normalized.Anchor.GPSFromBowM)
	}
}

// TestNormalizeSettingsPayload_RoundTripsGPSFromBowMNonZero is the
// counterpart: a configured, non-zero value must also round-trip exactly.
func TestNormalizeSettingsPayload_RoundTripsGPSFromBowMNonZero(t *testing.T) {
	req := settingsPayload{}
	req.Anchor.GPSFromBowM = 8.2

	normalized := normalizeSettingsPayload(req)

	if normalized.Anchor.GPSFromBowM != 8.2 {
		t.Fatalf("expected gps_from_bow_m to round-trip as 8.2, got %v", normalized.Anchor.GPSFromBowM)
	}
}

// TestBuildSettingsPayload_SurfacesGPSFromBowMZeroFromDisk is the read-side
// counterpart: an explicit 0 stored on disk must surface as 0, not be
// mistaken for "unset" and replaced with a default.
func TestBuildSettingsPayload_SurfacesGPSFromBowMZeroFromDisk(t *testing.T) {
	settings := map[string]any{
		"anchor": map[string]any{
			"gps_from_bow_m": 0,
		},
	}

	payload := buildSettingsPayload(settings)

	if payload.Anchor.GPSFromBowM != 0 {
		t.Fatalf("expected gps_from_bow_m to surface as 0, got %v", payload.Anchor.GPSFromBowM)
	}
}

// TestBuildSettingsPayload_SurfacesGPSFromBowMFromDisk mirrors the above for
// a configured non-zero value.
func TestBuildSettingsPayload_SurfacesGPSFromBowMFromDisk(t *testing.T) {
	settings := map[string]any{
		"anchor": map[string]any{
			"gps_from_bow_m": 8.5,
		},
	}

	payload := buildSettingsPayload(settings)

	if payload.Anchor.GPSFromBowM != 8.5 {
		t.Fatalf("expected gps_from_bow_m to surface as 8.5, got %v", payload.Anchor.GPSFromBowM)
	}
}

func TestParseSignalKCurrent_ReadsSetTrueAndDrift(t *testing.T) {
	payload := map[string]any{
		"environment": map[string]any{
			"current": map[string]any{
				"drift":   map[string]any{"value": 0.643},  // m/s -> ~1.2 kt
				"setTrue": map[string]any{"value": 2.4697}, // radians -> ~141.5 deg
			},
		},
	}

	drift, setDeg, _ := parseSignalKCurrent(payload)
	if !approxEqual(drift, 1.2, 0.05) {
		t.Fatalf("expected drift ~1.2 kt, got %v", drift)
	}
	if !approxEqual(setDeg, 141.5, 0.1) {
		t.Fatalf("expected set ~141.5 deg, got %v", setDeg)
	}
}

func TestParseSignalKCurrent_DoesNotTreatSetMagneticAsTrue(t *testing.T) {
	payload := map[string]any{
		"environment": map[string]any{
			"current": map[string]any{
				"drift":       map[string]any{"value": 0.5},
				"setMagnetic": map[string]any{"value": 1.5707963267948966}, // 90 deg magnetic
			},
		},
	}

	_, setDeg, _ := parseSignalKCurrent(payload)
	if setDeg != -1 {
		t.Fatalf("expected -1 (no reliable true bearing) when only setMagnetic is present, got %v", setDeg)
	}
}

func TestParseSignalKCurrent_ReadsDriftImpact(t *testing.T) {
	payload := map[string]any{
		"environment": map[string]any{
			"current": map[string]any{
				"drift":       map[string]any{"value": 0.5},
				"driftImpact": map[string]any{"value": -0.643}, // m/s -> ~-1.2 kt
			},
		},
	}

	_, _, driftImpactKts := parseSignalKCurrent(payload)
	if driftImpactKts == nil {
		t.Fatalf("expected driftImpactKts to be present")
	}
	if !approxEqual(*driftImpactKts, -1.2, 0.05) {
		t.Fatalf("expected driftImpactKts ~-1.2 kt, got %v", *driftImpactKts)
	}
}

func TestParseSignalKCurrent_NilDriftImpactWhenMissing(t *testing.T) {
	payload := map[string]any{
		"environment": map[string]any{
			"current": map[string]any{
				"drift": map[string]any{"value": 0.5},
			},
		},
	}

	_, _, driftImpactKts := parseSignalKCurrent(payload)
	if driftImpactKts != nil {
		t.Fatalf("expected nil driftImpactKts when driftImpact is absent, got %v", *driftImpactKts)
	}
}

// With no stream data and no prior fix there is nothing to freeze at, so the
// position must read as the unknown sentinel rather than a plausible-looking
// coordinate.
func TestFetchSignalKVesselState_MarksCriticalWithNoStreamDataAndNoPriorFix(t *testing.T) {
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)
	withGlobalSnapshot(t, newSignalKSnapshot())

	state, err := fetchSignalKVesselState()
	if err == nil {
		t.Fatalf("expected error when the stream carries no data")
	}
	if !state.GNSSCriticalAlert {
		t.Fatalf("expected gnss critical alert when the stream carries no data")
	}
	if state.GNSSValidationReason == "" {
		t.Fatalf("expected a validation reason explaining the failure")
	}
	if state.GNSSSatellites != -1 {
		t.Fatalf("expected -1 satellites with no stream data, got %d", state.GNSSSatellites)
	}
	if state.Latitude != -1 || state.Longitude != -1 {
		t.Fatalf("expected -1,-1 with no prior trusted fix, got %.4f %.4f", state.Latitude, state.Longitude)
	}
}

// The anchor alarm must be able to tell "the stream went away" apart from "the
// vessel moved". Losing the stream has to freeze position at the last trusted
// fix, never report a jump.
func TestFetchSignalKVesselState_FreezesLastTrustedPositionWhenStreamStops(t *testing.T) {
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	seedSelfTree(t, trustedSignalKPayload(-25.2939, 152.9103))

	state, err := fetchSignalKVesselState()
	if err != nil {
		t.Fatalf("unexpected error establishing trusted fix: %v", err)
	}
	if state.GNSSCriticalAlert {
		t.Fatalf("expected trusted fix to not be critical")
	}
	if state.Latitude != -25.2939 || state.Longitude != 152.9103 {
		t.Fatalf("expected trusted fix to pass through, got %.4f %.4f", state.Latitude, state.Longitude)
	}
	if state.GNSSSatellites != 8 {
		t.Fatalf("expected 8 satellites from trusted payload, got %d", state.GNSSSatellites)
	}

	// The stream drops: no data for any context.
	withGlobalSnapshot(t, newSignalKSnapshot())

	state, err = fetchSignalKVesselState()
	if err == nil {
		t.Fatalf("expected error once the stream stops carrying data")
	}
	if !state.GNSSCriticalAlert {
		t.Fatalf("expected gnss critical alert once the stream stops")
	}
	if state.Latitude != -25.2939 || state.Longitude != 152.9103 {
		t.Fatalf("expected position frozen at last trusted fix, got %.4f %.4f", state.Latitude, state.Longitude)
	}
}

// TestFetchSignalKNearbyVessels_ParsesStringMMSI is a regression test for a
// real bug found via live verification against this app's actual SignalK
// server: it encodes "mmsi" as a JSON string (e.g. "316042555"), not a JSON
// number. lookupNumber only handles float64/int, so it silently returned -1
// for every real-world vessel and Mmsi was always "" - contact tracking was
// unknowingly running entirely on the name-based fallback key. The fix must
// read mmsi as a string first, since that's the real-world format.
func TestFetchSignalKNearbyVessels_ParsesStringMMSI(t *testing.T) {
	body := []byte(`{
		"self": {
			"mmsi": "518999323",
			"name": "Pikorua",
			"navigation": {"position": {"value": {"latitude": -21.595297, "longitude": 149.796444}}}
		},
		"urn:mrn:imo:mmsi:316042555": {
			"mmsi": "316042555",
			"name": "TAKU X",
			"navigation": {"position": {"value": {"latitude": -21.592353, "longitude": 149.780485}}}
		}
	}`)

	seedVesselTrees(t, string(body))

	vessels, err := fetchSignalKNearbyVessels(-21.595297, 149.796444, time.Now().UTC(), nil)
	if err != nil {
		t.Fatalf("fetchSignalKNearbyVessels: %v", err)
	}
	if len(vessels) != 1 {
		t.Fatalf("expected 1 nearby vessel, got %d", len(vessels))
	}
	if vessels[0].Mmsi != "316042555" {
		t.Fatalf("expected MMSI '316042555' parsed from a JSON string field, got %q", vessels[0].Mmsi)
	}
}

// TestFetchSignalKNearbyVessels_ParsesNumericMMSI covers the other valid
// SignalK encoding (a bare JSON number), so the fix doesn't regress a
// server that sends mmsi that way instead.
func TestFetchSignalKNearbyVessels_ParsesNumericMMSI(t *testing.T) {
	body := []byte(`{
		"self": {
			"mmsi": 518999323,
			"name": "Pikorua",
			"navigation": {"position": {"value": {"latitude": -21.595297, "longitude": 149.796444}}}
		},
		"urn:mrn:imo:mmsi:316042555": {
			"mmsi": 316042555,
			"name": "TAKU X",
			"navigation": {"position": {"value": {"latitude": -21.592353, "longitude": 149.780485}}}
		}
	}`)

	seedVesselTrees(t, string(body))

	vessels, err := fetchSignalKNearbyVessels(-21.595297, 149.796444, time.Now().UTC(), nil)
	if err != nil {
		t.Fatalf("fetchSignalKNearbyVessels: %v", err)
	}
	if len(vessels) != 1 {
		t.Fatalf("expected 1 nearby vessel, got %d", len(vessels))
	}
	if vessels[0].Mmsi != "316042555" {
		t.Fatalf("expected MMSI '316042555' parsed from a JSON number field, got %q", vessels[0].Mmsi)
	}
}

// vesselTreeAt returns a single-vessel GET .../vessels-shaped fixture at the
// given position, for the staleness tests below where each vessel's age is
// what's under test, not its lat/lon.
func vesselTreeAt(id, name string, latitude, longitude float64) string {
	return fmt.Sprintf(`{%q: {"name": %q, "navigation": {"position": {"value": {"latitude": %f, "longitude": %f}}}}}`, id, name, latitude, longitude)
}

// TestFetchSignalKNearbyVessels_DropsStaleVessels is the core regression test
// for the ghost-contact bug: a vessel whose position hasn't been refreshed in
// over nearbyVesselMaxAge must not be reported as a live target.
func TestFetchSignalKNearbyVessels_DropsStaleVessels(t *testing.T) {
	body := `{
		"fresh-vessel": {"name": "FRESH", "navigation": {"position": {"value": {"latitude": -21.592353, "longitude": 149.780485}}}},
		"stale-vessel": {"name": "GHOST", "navigation": {"position": {"value": {"latitude": -21.592453, "longitude": 149.780585}}}}
	}`
	now := time.Now().UTC()
	seedVesselTreesAged(t, body, map[string]time.Duration{
		"fresh-vessel": 30 * time.Second,
		"stale-vessel": 11 * time.Minute,
	}, now)

	vessels, err := fetchSignalKNearbyVessels(-21.595297, 149.796444, now, nil)
	if err != nil {
		t.Fatalf("fetchSignalKNearbyVessels: %v", err)
	}
	if len(vessels) != 1 {
		t.Fatalf("expected 1 nearby vessel (the fresh one), got %d: %+v", len(vessels), vessels)
	}
	if vessels[0].Name != "FRESH" {
		t.Fatalf("expected the fresh vessel to survive, got %q", vessels[0].Name)
	}
}

// TestFetchSignalKNearbyVessels_KeepsVesselAtCutoffBoundary asserts the
// comparison is strictly greater-than: a vessel aged exactly nearbyVesselMaxAge
// has not yet exceeded it and must still be reported.
func TestFetchSignalKNearbyVessels_KeepsVesselAtCutoffBoundary(t *testing.T) {
	body := vesselTreeAt("boundary-vessel", "BOUNDARY", -21.592353, 149.780485)
	now := time.Now().UTC()
	seedVesselTreesAged(t, body, map[string]time.Duration{"boundary-vessel": nearbyVesselMaxAge}, now)

	vessels, err := fetchSignalKNearbyVessels(-21.595297, 149.796444, now, nil)
	if err != nil {
		t.Fatalf("fetchSignalKNearbyVessels: %v", err)
	}
	if len(vessels) != 1 {
		t.Fatalf("expected the vessel at exactly the cutoff to be kept, got %d vessels", len(vessels))
	}
}

// TestFetchSignalKNearbyVessels_DropsVesselWithNoPositionDelta covers a
// vessel context that exists in the tree but has never had a
// navigation.position delta recorded in pathSeen. There is nothing to age
// against, so it must be dropped rather than silently reported as age 0 -
// the same masking fallback the old ageSeconds parse failure produced.
func TestFetchSignalKNearbyVessels_DropsVesselWithNoPositionDelta(t *testing.T) {
	body := vesselTreeAt("no-delta-vessel", "NODELTA", -21.592353, 149.780485)
	seedVesselTreesAged(t, body, map[string]time.Duration{}, time.Now().UTC())
	// Remove the freshness stamp seedVesselTreesAged would otherwise apply,
	// reproducing a tree entry with no recorded position delta at all.
	delete(globalSignalKSnapshot.pathSeen, vesselContextPrefix+"no-delta-vessel|navigation.position")

	vessels, err := fetchSignalKNearbyVessels(-21.595297, 149.796444, time.Now().UTC(), nil)
	if err != nil {
		t.Fatalf("fetchSignalKNearbyVessels: %v", err)
	}
	if len(vessels) != 0 {
		t.Fatalf("expected a vessel with no position delta to be dropped, got %d: %+v", len(vessels), vessels)
	}
}

// TestFetchSignalKNearbyVessels_AgeSecondsFromReceiveTime asserts age_seconds
// tracks delta receive time, not the AIS-reported navigation.position.timestamp
// - including when that timestamp disagrees with receive time, and when it is
// unparseable (which must be filtered normally on receive time, not silently
// reported as age 0 as the old timestamp-parse fallback did).
func TestFetchSignalKNearbyVessels_AgeSecondsFromReceiveTime(t *testing.T) {
	now := time.Now().UTC()
	body := fmt.Sprintf(`{
		"disagreeing-vessel": {
			"name": "DISAGREE",
			"navigation": {"position": {"value": {"latitude": -21.592353, "longitude": 149.780485}, "timestamp": %q}}
		},
		"unparseable-vessel": {
			"name": "BADTS",
			"navigation": {"position": {"value": {"latitude": -21.592453, "longitude": 149.780585}, "timestamp": "not-a-timestamp"}}
		}
	}`, now.Add(-2*time.Hour).Format(time.RFC3339))

	seedVesselTreesAged(t, body, map[string]time.Duration{
		"disagreeing-vessel": 45 * time.Second,
		"unparseable-vessel": 45 * time.Second,
	}, now)

	vessels, err := fetchSignalKNearbyVessels(-21.595297, 149.796444, now, nil)
	if err != nil {
		t.Fatalf("fetchSignalKNearbyVessels: %v", err)
	}
	if len(vessels) != 2 {
		t.Fatalf("expected both vessels within the age cutoff to be reported, got %d: %+v", len(vessels), vessels)
	}
	for _, v := range vessels {
		if v.AgeSeconds < 44 || v.AgeSeconds > 46 {
			t.Fatalf("expected age_seconds ~45s from receive time regardless of the AIS timestamp field, got %d for %q", v.AgeSeconds, v.Name)
		}
	}
}

// TestFetchSignalKNearbyVessels_StaleFilterAppliedBeforeTopTenCap is the
// regression test for the reported screenshot: 11 stale, close ghosts must
// not crowd a fresh, more distant vessel out of the top-10 truncation.
func TestFetchSignalKNearbyVessels_StaleFilterAppliedBeforeTopTenCap(t *testing.T) {
	now := time.Now().UTC()
	trees := make([]string, 0, 12)
	ages := map[string]time.Duration{}
	for i := 0; i < 11; i++ {
		id := fmt.Sprintf("ghost-%d", i)
		// A tiny lat offset per ghost keeps them all close (well under 1km)
		// and at distinct positions, all closer than the fresh vessel below.
		trees = append(trees, fmt.Sprintf(`%q: {"name": "GHOST%d", "navigation": {"position": {"value": {"latitude": %f, "longitude": 149.780485}}}}`, id, i, -21.593000-float64(i)*0.0001))
		ages[id] = 11 * time.Minute
	}
	// A fresh vessel roughly 3km out - farther than every ghost, but it must
	// still survive because the ghosts are dropped before the top-10 cap.
	trees = append(trees, `"fresh-distant": {"name": "FARAWAY", "navigation": {"position": {"value": {"latitude": -21.622000, "longitude": 149.780485}}}}`)
	ages["fresh-distant"] = 30 * time.Second

	body := "{" + strings.Join(trees, ",") + "}"
	seedVesselTreesAged(t, body, ages, now)

	vessels, err := fetchSignalKNearbyVessels(-21.595297, 149.796444, now, nil)
	if err != nil {
		t.Fatalf("fetchSignalKNearbyVessels: %v", err)
	}
	if len(vessels) != 1 {
		t.Fatalf("expected only the fresh distant vessel to survive, got %d: %+v", len(vessels), vessels)
	}
	if vessels[0].Name != "FARAWAY" {
		t.Fatalf("expected FARAWAY to survive the stale-then-cap filtering, got %q", vessels[0].Name)
	}
}

// TestFetchSignalKNearbyVessels_ReportsRangeInMeters asserts RangeM against a
// known haversine distance, and that the 5000m cutoff is a metres boundary,
// not a feet-derived one.
func TestFetchSignalKNearbyVessels_ReportsRangeInMeters(t *testing.T) {
	const selfLat, selfLon = -21.595297, 149.796444
	const otherLat, otherLon = -21.592353, 149.780485
	wantRangeM := math.Round(haversineMeters(selfLat, selfLon, otherLat, otherLon)*10) / 10

	body := vesselTreeAt("known-distance-vessel", "KNOWNDIST", otherLat, otherLon)
	now := time.Now().UTC()
	seedVesselTreesAged(t, body, nil, now)

	vessels, err := fetchSignalKNearbyVessels(selfLat, selfLon, now, nil)
	if err != nil {
		t.Fatalf("fetchSignalKNearbyVessels: %v", err)
	}
	if len(vessels) != 1 {
		t.Fatalf("expected 1 nearby vessel, got %d", len(vessels))
	}
	if vessels[0].RangeM != wantRangeM {
		t.Fatalf("expected RangeM %v, got %v", wantRangeM, vessels[0].RangeM)
	}

	// Just inside vs. just beyond the 5000m horizon.
	const closeLat = -21.595297
	nearOffsetDeg := 4900.0 / 111320.0 // ~4900m north
	farOffsetDeg := 5100.0 / 111320.0  // ~5100m north
	insideBody := vesselTreeAt("inside-vessel", "INSIDE", closeLat+nearOffsetDeg, selfLon)
	outsideBody := vesselTreeAt("outside-vessel", "OUTSIDE", closeLat+farOffsetDeg, selfLon)
	combined := `{` +
		`"inside-vessel": ` + mustExtractSingleVesselTree(t, insideBody, "inside-vessel") + `,` +
		`"outside-vessel": ` + mustExtractSingleVesselTree(t, outsideBody, "outside-vessel") +
		`}`
	seedVesselTreesAged(t, combined, nil, now)

	vessels, err = fetchSignalKNearbyVessels(selfLat, selfLon, now, nil)
	if err != nil {
		t.Fatalf("fetchSignalKNearbyVessels: %v", err)
	}
	if len(vessels) != 1 || vessels[0].Name != "INSIDE" {
		t.Fatalf("expected only the vessel inside 5000m to be kept, got %+v", vessels)
	}
}

// mustExtractSingleVesselTree pulls the inner vessel object back out of a
// vesselTreeAt fixture, so two single-vessel fixtures can be recombined into
// one multi-vessel body without hand-duplicating the JSON.
func mustExtractSingleVesselTree(t *testing.T, body, id string) string {
	t.Helper()
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("mustExtractSingleVesselTree: %v", err)
	}
	raw, ok := payload[id]
	if !ok {
		t.Fatalf("mustExtractSingleVesselTree: id %q not found in body", id)
	}
	return string(raw)
}

// TestFetchSignalKNearbyVessels_PopulatesStableID reproduces the duplicate
// React key bug at the seam where it originates: two vessels sharing a name
// must still come back with distinct, non-empty IDs.
func TestFetchSignalKNearbyVessels_PopulatesStableID(t *testing.T) {
	body := `{
		"urn:mrn:imo:mmsi:111111111": {"name": "SAME NAME", "navigation": {"position": {"value": {"latitude": -21.592353, "longitude": 149.780485}}}},
		"urn:mrn:imo:mmsi:222222222": {"name": "SAME NAME", "navigation": {"position": {"value": {"latitude": -21.592453, "longitude": 149.780585}}}}
	}`
	now := time.Now().UTC()
	seedVesselTreesAged(t, body, nil, now)

	vessels, err := fetchSignalKNearbyVessels(-21.595297, 149.796444, now, nil)
	if err != nil {
		t.Fatalf("fetchSignalKNearbyVessels: %v", err)
	}
	if len(vessels) != 2 {
		t.Fatalf("expected 2 nearby vessels, got %d", len(vessels))
	}
	if vessels[0].ID == "" || vessels[1].ID == "" {
		t.Fatalf("expected every vessel to have a non-empty ID, got %+v", vessels)
	}
	if vessels[0].ID == vessels[1].ID {
		t.Fatalf("expected distinct IDs for two vessels sharing a name, both got %q", vessels[0].ID)
	}
}

func TestFetchSignalKElectricalState_ReadsCharger0Fields(t *testing.T) {
	body := []byte(`{
		"timestamp": "2026-07-22T00:00:00Z",
		"electrical": {
			"chargers": {
				"0": {
					"current": {"value": 42.39},
					"acin": {
						"1": {
							"current": {"value": 8.14}
						}
					},
					"chargingMode": {"value": "bulk"},
					"error": {"value": "none"}
				}
			}
		}
	}`)

	seedSelfTree(t, string(body))

	state, err := fetchSignalKElectricalState()
	if err != nil {
		t.Fatalf("fetchSignalKElectricalState: %v", err)
	}

	if !approxEqual(state.Charger0.CurrentA, 42.4, 0.01) {
		t.Fatalf("expected charger_0_current_a 42.4, got %v", state.Charger0.CurrentA)
	}
	if !approxEqual(state.Charger0.ACIn1CurrentA, 8.1, 0.01) {
		t.Fatalf("expected charger_0_acin_1_current_a 8.1, got %v", state.Charger0.ACIn1CurrentA)
	}
	if state.Charger0.ChargingMode != "bulk" {
		t.Fatalf("expected charging mode 'bulk', got %q", state.Charger0.ChargingMode)
	}
	if state.Charger0.Error != "none" {
		t.Fatalf("expected error 'none', got %q", state.Charger0.Error)
	}
}

func TestFetchSignalKElectricalState_ReadsCharger0MixedShapes(t *testing.T) {
	body := []byte(`{
		"timestamp": "2026-07-22T00:00:00Z",
		"electrical": {
			"chargers": {
				"0": {
					"current": 17.76,
					"acin": {
						"1": {
							"current": 5.26
						}
					},
					"chargingMode": "float",
					"error": ""
				}
			}
		}
	}`)

	seedSelfTree(t, string(body))

	state, err := fetchSignalKElectricalState()
	if err != nil {
		t.Fatalf("fetchSignalKElectricalState: %v", err)
	}

	if !approxEqual(state.Charger0.CurrentA, 17.8, 0.01) {
		t.Fatalf("expected charger_0_current_a 17.8, got %v", state.Charger0.CurrentA)
	}
	if !approxEqual(state.Charger0.ACIn1CurrentA, 5.3, 0.01) {
		t.Fatalf("expected charger_0_acin_1_current_a 5.3, got %v", state.Charger0.ACIn1CurrentA)
	}
	if state.Charger0.ChargingMode != "float" {
		t.Fatalf("expected charging mode 'float', got %q", state.Charger0.ChargingMode)
	}
	if state.Charger0.Error != "" {
		t.Fatalf("expected empty error string when absent, got %q", state.Charger0.Error)
	}
}

func TestFetchSignalKSolarState_ReadsControllersAndAggregate(t *testing.T) {
	body := []byte(`{
		"timestamp": "2026-07-22T00:00:00Z",
		"electrical": {
			"venus": {
				"totalPanelPower": {"value": 1120.4}
			},
			"solar": {
				"0": {
					"panelPower": {"value": 410.3},
					"yieldToday": {"value": 1.7},
					"yieldYesterday": {"value": 1.6},
					"chargingMode": {"value": "bulk"},
					"error": {"value": "none"}
				},
				"1": {
					"panelPower": {"value": 370.7},
					"yieldToday": {"value": 1.4},
					"yieldYesterday": {"value": 1.5},
					"mode": {"value": "absorption"},
					"error": {"value": ""}
				}
			}
		}
	}`)

	seedSelfTree(t, string(body))

	state, err := fetchSignalKSolarState()
	if err != nil {
		t.Fatalf("fetchSignalKSolarState: %v", err)
	}

	if !approxEqual(state.CurrentW, 1120.4, 0.01) {
		t.Fatalf("expected aggregate current from venus 1120.4, got %v", state.CurrentW)
	}
	if !approxEqual(state.TodayKWh, 3.1, 0.01) {
		t.Fatalf("expected today_kwh 3.1, got %v", state.TodayKWh)
	}
	if !approxEqual(state.YesterdayKWh, 3.1, 0.01) {
		t.Fatalf("expected yesterday_kwh 3.1, got %v", state.YesterdayKWh)
	}
	if len(state.Controllers) != 2 {
		t.Fatalf("expected 2 controllers, got %d", len(state.Controllers))
	}
	if state.Controllers[0].Label != "Port" {
		t.Fatalf("expected first controller label Port, got %q", state.Controllers[0].Label)
	}
	if state.Controllers[1].Mode != "absorption" {
		t.Fatalf("expected second controller mode absorption, got %q", state.Controllers[1].Mode)
	}
}

func TestFetchSignalKSolarState_NormalizesWhYieldToKWh(t *testing.T) {
	body := []byte(`{
		"timestamp": "2026-07-22T00:00:00Z",
		"electrical": {
			"solar": {
				"0": {
					"panelPower": 250,
					"yieldToday": 1450,
					"yieldYesterday": 1300
				}
			}
		}
	}`)

	seedSelfTree(t, string(body))

	state, err := fetchSignalKSolarState()
	if err != nil {
		t.Fatalf("fetchSignalKSolarState: %v", err)
	}

	if !approxEqual(state.TodayKWh, 1.45, 0.001) {
		t.Fatalf("expected yieldToday converted to 1.45 kWh, got %v", state.TodayKWh)
	}
	if !approxEqual(state.YesterdayKWh, 1.3, 0.001) {
		t.Fatalf("expected yieldYesterday converted to 1.3 kWh, got %v", state.YesterdayKWh)
	}
}
