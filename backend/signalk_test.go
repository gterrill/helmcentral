package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func trustedSignalKPayloadServer(t *testing.T, latitude, longitude float64) *httptest.Server {
	t.Helper()
	body := []byte(fmt.Sprintf(`{
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
	}`, time.Now().UTC().Format(time.RFC3339), latitude, longitude))

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
}

func approxEqual(a, b, tolerance float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff <= tolerance
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

	drift, setDeg := parseSignalKCurrent(payload)
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

	_, setDeg := parseSignalKCurrent(payload)
	if setDeg != -1 {
		t.Fatalf("expected -1 (no reliable true bearing) when only setMagnetic is present, got %v", setDeg)
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

func TestFetchSignalKVesselState_FreezesLastTrustedPositionWhenConnectionLost(t *testing.T) {
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	goodSrv := trustedSignalKPayloadServer(t, -25.2939, 152.9103)

	state, err := fetchSignalKVesselState(goodSrv.URL, "/signalk/v1/api/vessels/self")
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

	goodURL := goodSrv.URL
	goodSrv.Close()

	state, err = fetchSignalKVesselState(goodURL, "/signalk/v1/api/vessels/self")
	if err == nil {
		t.Fatalf("expected error once signalk becomes unreachable")
	}
	if !state.GNSSCriticalAlert {
		t.Fatalf("expected gnss critical alert once signalk becomes unreachable")
	}
	if state.Latitude != -25.2939 || state.Longitude != 152.9103 {
		t.Fatalf("expected position frozen at last trusted fix, got %.4f %.4f", state.Latitude, state.Longitude)
	}
}
