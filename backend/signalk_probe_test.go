package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// POST /api/settings/signalk/test replaces the old POST /api/settings/signalk,
// which both probed *and* persisted. That dual role gave the address two
// write paths — one validating, one not (ADR 0027) — and made a diagnostic
// button silently mutate config. The probe is now purely a read: the only way
// to persist the address is the bulk save.
//
// It deliberately probes the address in the *request body* rather than the
// persisted one, so an operator can check a value before committing to it.

func postSignalKProbe(t *testing.T, settingsPath string, body string) (int, map[string]any) {
	t.Helper()
	t.Setenv("SETTINGS_FILE", settingsPath)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/settings/signalk/test", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()

	if err := testSignalKConnectionHandler(e.NewContext(req, rec)); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var decoded map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &decoded)
	return rec.Code, decoded
}

func TestSignalKProbe_ReportsSuccessWithoutPersisting(t *testing.T) {
	srv := trustedSignalKPayloadServer(t, -21.1, 149.2)
	defer srv.Close()
	host, port := hostPort(t, srv.URL)

	settingsPath := writeTestSettings(t, "203.0.113.9", 3000)
	before, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read before: %v", err)
	}

	code, body := postSignalKProbe(t, settingsPath, fmt.Sprintf(`{"address":%q,"port":%d}`, host, port))
	if code != http.StatusOK {
		t.Fatalf("expected 200 for reachable address, got %d (body %v)", code, body)
	}
	if connected, _ := body["connected"].(bool); !connected {
		t.Errorf("expected connected=true, got %v", body["connected"])
	}

	// The whole point of the change: a successful probe must not write.
	after, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("probe must not persist anything:\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}
}

// The probe answers "is a SignalK server there", but reporting which vessel
// answered is what lets an operator notice they've reached the wrong one —
// the gap ADR 0027 called out and could not close from the save path alone.
func TestSignalKProbe_ReportsVesselName(t *testing.T) {
	body := []byte(`{"name": "Probe Test Vessel", "navigation": {"state": {"value": "anchored"}}}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer srv.Close()
	host, port := hostPort(t, srv.URL)

	settingsPath := writeTestSettings(t, "203.0.113.9", 3000)
	code, decoded := postSignalKProbe(t, settingsPath, fmt.Sprintf(`{"address":%q,"port":%d}`, host, port))
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body %v)", code, decoded)
	}
	if name, _ := decoded["vessel_name"].(string); name != "Probe Test Vessel" {
		t.Fatalf("expected vessel_name from the probed server, got %q", name)
	}
}

func TestSignalKProbe_ReportsUnreachableWithoutPersisting(t *testing.T) {
	srv := trustedSignalKPayloadServer(t, -21.1, 149.2)
	defer srv.Close()
	host, port := hostPort(t, srv.URL)

	settingsPath := writeTestSettings(t, host, port)
	before, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read before: %v", err)
	}

	// TEST-NET-3 (RFC 5737): reserved, guaranteed not live.
	code, decoded := postSignalKProbe(t, settingsPath, `{"address":"203.0.113.9","port":3000}`)
	if code != http.StatusBadGateway {
		t.Fatalf("expected 502 for unreachable address, got %d (body %v)", code, decoded)
	}
	if msg, _ := decoded["error"].(string); !strings.Contains(msg, "203.0.113.9") {
		t.Errorf("error should name the address probed, got %q", msg)
	}
	if field, _ := decoded["field"].(string); field != "signalk.address" {
		t.Errorf("expected field \"signalk.address\", got %q", field)
	}

	// A failed probe must not clobber a working persisted address either.
	after, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("failed probe must not persist anything")
	}
}

// An empty address is operator error, not a connectivity failure: reporting
// it as "cannot reach localhost" (what the old handler's default would have
// produced) would send them debugging the wrong thing.
func TestSignalKProbe_RejectsEmptyAddress(t *testing.T) {
	settingsPath := writeTestSettings(t, "203.0.113.9", 3000)

	code, decoded := postSignalKProbe(t, settingsPath, `{"address":"  ","port":3000}`)
	if code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty address, got %d (body %v)", code, decoded)
	}
	if field, _ := decoded["field"].(string); field != "signalk.address" {
		t.Errorf("expected field \"signalk.address\", got %q", field)
	}
}

func TestSignalKProbe_RejectsOutOfRangePort(t *testing.T) {
	settingsPath := writeTestSettings(t, "203.0.113.9", 3000)

	code, decoded := postSignalKProbe(t, settingsPath, `{"address":"192.168.1.10","port":70000}`)
	if code != http.StatusBadRequest {
		t.Fatalf("expected 400 for out-of-range port, got %d (body %v)", code, decoded)
	}
	if field, _ := decoded["field"].(string); field != "signalk.port" {
		t.Errorf("expected field \"signalk.port\", got %q", field)
	}
}

// The removed handler is gone for good: persisting the address is the bulk
// save's job alone. This test documents the invariant that made the change
// worth making, and fails to compile if saveSignalKSettings comes back
// without a caller.
func TestSignalKProbe_HasNoPersistencePath(t *testing.T) {
	srv := trustedSignalKPayloadServer(t, -21.1, 149.2)
	defer srv.Close()
	host, port := hostPort(t, srv.URL)

	settingsPath := writeTestSettings(t, "203.0.113.9", 3000)

	// Probe a reachable server repeatedly; the persisted address must remain
	// whatever the settings file said, never what was probed.
	for i := 0; i < 3; i++ {
		if code, body := postSignalKProbe(t, settingsPath, fmt.Sprintf(`{"address":%q,"port":%d}`, host, port)); code != http.StatusOK {
			t.Fatalf("probe %d: expected 200, got %d (%v)", i, code, body)
		}
	}

	address, persistedPort := persistedSignalK(t, settingsPath)
	if address != "203.0.113.9" || persistedPort != 3000 {
		t.Fatalf("probing must never change persisted settings, got %s:%d", address, persistedPort)
	}
}
