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
				"horizontalDilution": {"value": 0.9}
			}
		}
	}`, time.Now().UTC().Format(time.RFC3339), latitude, longitude))

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
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
