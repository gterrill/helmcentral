package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
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

func TestFetchSignalKVesselState_MarksCriticalOnConnectionRefused(t *testing.T) {
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // guarantees connection refused

	state, err := fetchSignalKVesselState(url, "/signalk/v1/api/vessels/self")
	if err == nil {
		t.Fatalf("expected error for unreachable signalk")
	}
	if !state.GNSSCriticalAlert {
		t.Fatalf("expected gnss critical alert when signalk is unreachable")
	}
	if state.GNSSValidationReason == "" {
		t.Fatalf("expected a validation reason explaining the failure")
	}
	if state.GNSSSatellites != -1 {
		t.Fatalf("expected -1 satellites when signalk is unreachable, got %d", state.GNSSSatellites)
	}
	if state.Latitude != -1 || state.Longitude != -1 {
		t.Fatalf("expected -1,-1 with no prior trusted fix, got %.4f %.4f", state.Latitude, state.Longitude)
	}
}

func TestFetchSignalKVesselState_MarksCriticalOnNon200(t *testing.T) {
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	state, err := fetchSignalKVesselState(srv.URL, "/signalk/v1/api/vessels/self")
	if err == nil {
		t.Fatalf("expected error for non-200 signalk response")
	}
	if !state.GNSSCriticalAlert {
		t.Fatalf("expected gnss critical alert on non-200 response")
	}
}

// The anchor alarm must be able to tell "the stream went away" apart from "the
// vessel moved". Losing the stream has to freeze position at the last trusted
// fix, never report a jump.
func TestFetchSignalKVesselState_FreezesLastTrustedPositionWhenStreamStops(t *testing.T) {
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	seedSelfTree(t, trustedSignalKPayload(-25.2939, 152.9103))

	state, err := fetchSignalKVesselState("", "/signalk/v1/api/vessels/self")
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

	state, err = fetchSignalKVesselState("", "/signalk/v1/api/vessels/self")
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

	vessels, err := fetchSignalKNearbyVessels("", "/vessels", -21.595297, 149.796444, time.Now().UTC(), nil)
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

	vessels, err := fetchSignalKNearbyVessels("", "/vessels", -21.595297, 149.796444, time.Now().UTC(), nil)
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

	state, err := fetchSignalKElectricalState("", "/signalk/v1/api/vessels/self")
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

	state, err := fetchSignalKElectricalState("", "/signalk/v1/api/vessels/self")
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

	state, err := fetchSignalKSolarState("", "/signalk/v1/api/vessels/self")
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

	state, err := fetchSignalKSolarState("", "/signalk/v1/api/vessels/self")
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
