package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// POST /api/settings is a full-payload replace with no connectivity check,
// which is how a browser test that filled #signalk-address with a throwaway
// value and clicked "Save and Continue" repointed the live dashboard at a
// dead host (see docs/adr/0026). These tests pin the probe onto the bulk
// path — which, since ADR 0028 removed POST /api/settings/signalk, is now the
// only way the address gets written at all.
//
// The load-bearing asymmetry: the probe fires only when the address actually
// *changes*. Validating on every save would mean an unreachable vessel — a
// completely normal state for this app, e.g. configuring from home — blocks
// edits to tank labels, anchor geometry and units that have nothing to do
// with SignalK. That would be a worse bug than the one being fixed.

func writeTestSettings(t *testing.T, address string, port int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.yaml")
	body := fmt.Sprintf(`anchor:
    bow_roller_height_m: 1.5
    chain_onboard_m: 150
    chain_size_mm: 12
    hull_type: power_cat
    windage_area_m2: 35
boat:
    house_battery_capacity_ah: 1440
    model: Test Boat
    vessel_prefix: M/V
signalk:
    address: %s
    port: %d
ui:
    tank_labels:
        fuel.2: PORT FWD
    vessel_state_refresh_seconds: 10
units: metric
`, address, port)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	return path
}

// postSettings drives updateSettingsHandler the way the real UI does: read
// the current payload, mutate it, send the whole thing back.
func postSettings(t *testing.T, settingsPath string, mutate func(*settingsPayload)) (int, map[string]any) {
	t.Helper()
	t.Setenv("SETTINGS_FILE", settingsPath)

	current, err := readSettings(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	payload := buildSettingsPayload(current)
	mutate(&payload)

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader(string(body)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()

	if err := updateSettingsHandler(e.NewContext(req, rec)); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var decoded map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &decoded)
	return rec.Code, decoded
}

func persistedSignalK(t *testing.T, settingsPath string) (string, int) {
	t.Helper()
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		t.Fatalf("load signalk settings: %v", err)
	}
	return address, port
}

// hostPort splits an httptest server URL into the address/port pair the
// settings file stores.
func hostPort(t *testing.T, rawURL string) (string, int) {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatalf("parse server port: %v", err)
	}
	return parsed.Hostname(), port
}

func TestUpdateSettings_RejectsUnreachableNewSignalKAddress(t *testing.T) {
	srv := trustedSignalKPayloadServer(t, -21.1, 149.2)
	defer srv.Close()
	host, port := hostPort(t, srv.URL)
	settingsPath := writeTestSettings(t, host, port)

	// 203.0.113.0/24 is TEST-NET-3 (RFC 5737): reserved for documentation and
	// guaranteed not to be a live host, so this fails without depending on
	// the test machine's network.
	code, body := postSettings(t, settingsPath, func(p *settingsPayload) {
		p.Signalk.Address = "203.0.113.9"
	})

	if code != http.StatusBadGateway {
		t.Fatalf("expected 502 for unreachable new address, got %d (body %v)", code, body)
	}
	if msg, _ := body["error"].(string); !strings.Contains(msg, "203.0.113.9") {
		t.Errorf("error should name the address that failed, got %q", msg)
	}
	if field, _ := body["field"].(string); field != "signalk.address" {
		t.Errorf("expected field \"signalk.address\", got %q", field)
	}

	gotAddress, gotPort := persistedSignalK(t, settingsPath)
	if gotAddress != host || gotPort != port {
		t.Fatalf("rejected save must not persist: got %s:%d, want %s:%d", gotAddress, gotPort, host, port)
	}
}

// A rejected save must not half-apply: the unrelated fields travelling in the
// same payload stay untouched too.
func TestUpdateSettings_RejectedSavePersistsNothingAtAll(t *testing.T) {
	srv := trustedSignalKPayloadServer(t, -21.1, 149.2)
	defer srv.Close()
	host, port := hostPort(t, srv.URL)
	settingsPath := writeTestSettings(t, host, port)

	before, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read before: %v", err)
	}

	code, _ := postSettings(t, settingsPath, func(p *settingsPayload) {
		p.Signalk.Address = "203.0.113.9"
		p.Boat.Model = "Changed Boat"
		p.Anchor.ChainOnboardM = 999
		p.Units = "imperial"
	})
	if code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", code)
	}

	after, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("settings file changed despite rejected save:\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}
}

func TestUpdateSettings_AcceptsReachableNewSignalKAddress(t *testing.T) {
	oldSrv := trustedSignalKPayloadServer(t, -21.1, 149.2)
	defer oldSrv.Close()
	newSrv := trustedSignalKPayloadServer(t, -21.2, 149.3)
	defer newSrv.Close()

	oldHost, oldPort := hostPort(t, oldSrv.URL)
	newHost, newPort := hostPort(t, newSrv.URL)
	settingsPath := writeTestSettings(t, oldHost, oldPort)

	code, body := postSettings(t, settingsPath, func(p *settingsPayload) {
		p.Signalk.Address = newHost
		p.Signalk.Port = newPort
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200 for reachable new address, got %d (body %v)", code, body)
	}

	gotAddress, gotPort := persistedSignalK(t, settingsPath)
	if gotAddress != newHost || gotPort != newPort {
		t.Fatalf("expected %s:%d persisted, got %s:%d", newHost, newPort, gotAddress, gotPort)
	}
}

// The regression that matters most: a vessel that is simply offline must not
// lock the operator out of every other setting on the page.
func TestUpdateSettings_AllowsUnrelatedEditsWhileSignalKIsUnreachable(t *testing.T) {
	settingsPath := writeTestSettings(t, "203.0.113.9", 3000)

	code, body := postSettings(t, settingsPath, func(p *settingsPayload) {
		p.Boat.Model = "Renamed While Offline"
		p.Anchor.ChainOnboardM = 120
		p.UI.TankLabels = map[string]string{"fuel.2": "PORT AFT"}
	})
	if code != http.StatusOK {
		t.Fatalf("unchanged address must not be probed; got %d (body %v)", code, body)
	}

	saved, err := readSettings(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if model := buildSettingsPayload(saved).Boat.Model; model != "Renamed While Offline" {
		t.Fatalf("expected unrelated edit to persist, got model %q", model)
	}

	gotAddress, gotPort := persistedSignalK(t, settingsPath)
	if gotAddress != "203.0.113.9" || gotPort != 3000 {
		t.Fatalf("address should round-trip unchanged, got %s:%d", gotAddress, gotPort)
	}
}

// Port is half the address; changing it points at a different server and must
// be probed just the same.
func TestUpdateSettings_RejectsUnreachableNewSignalKPort(t *testing.T) {
	srv := trustedSignalKPayloadServer(t, -21.1, 149.2)
	defer srv.Close()
	host, port := hostPort(t, srv.URL)
	settingsPath := writeTestSettings(t, host, port)

	code, _ := postSettings(t, settingsPath, func(p *settingsPayload) {
		p.Signalk.Port = 9 // discard protocol; nothing listens
	})
	if code != http.StatusBadGateway {
		t.Fatalf("expected 502 for unreachable new port, got %d", code)
	}

	if _, gotPort := persistedSignalK(t, settingsPath); gotPort != port {
		t.Fatalf("rejected port change must not persist, got %d", gotPort)
	}
}

// Switching auth on must be refused when SignalK's security is off, at save
// time rather than at the next restart. Saving an unsatisfiable config and
// only discovering it on reboot is how an operator locks themselves out.
func TestValidateSettingsChange_RejectsAuthSignalKWhenSignalKSecurityIsDisabled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A server with security disabled answers the login probe with 404.
		http.NotFound(w, r)
	}))
	defer srv.Close()

	current := settingsPayload{}
	current.Signalk.Address = srv.URL
	current.Signalk.Port = 3000
	current.Auth.Mode = "none"

	next := current
	next.Auth.Mode = "signalk"

	invalid := validateSettingsChange(current, next)
	if invalid == nil {
		t.Fatalf("expected turning auth on against an unsecured SignalK to be refused")
	}
	if invalid.Field != "auth.mode" {
		t.Fatalf("error should pin to the field the operator changed, got %q", invalid.Field)
	}
}

// Turning auth off must always be allowed: it is the way out of a lockout, and
// gating it on a reachable SignalK would make a broken server unrecoverable.
func TestValidateSettingsChange_AlwaysAllowsTurningAuthOff(t *testing.T) {
	current := settingsPayload{}
	current.Signalk.Address = "203.0.113.1"
	current.Signalk.Port = 3000
	current.Auth.Mode = "signalk"

	next := current
	next.Auth.Mode = "none"

	if invalid := validateSettingsChange(current, next); invalid != nil {
		t.Fatalf("turning auth off must never be blocked, got %+v", invalid)
	}
}

// Only a change is probed, matching how the SignalK address check already
// behaves — re-saving an unrelated setting must not hit the network.
func TestValidateSettingsChange_DoesNotProbeWhenAuthModeIsUnchanged(t *testing.T) {
	current := settingsPayload{}
	current.Signalk.Address = "203.0.113.1"
	current.Signalk.Port = 3000
	current.Auth.Mode = "signalk"

	next := current
	next.UI.VesselStateRefreshSeconds = 30

	if invalid := validateSettingsChange(current, next); invalid != nil {
		t.Fatalf("an unchanged auth mode must not be re-probed, got %+v", invalid)
	}
}

func TestValidateSettingsChange_RejectsUnknownAuthMode(t *testing.T) {
	current := settingsPayload{}
	current.Auth.Mode = "none"

	next := current
	next.Auth.Mode = "sometimes"

	invalid := validateSettingsChange(current, next)
	if invalid == nil || invalid.Field != "auth.mode" {
		t.Fatalf("an unknown auth mode must be refused, got %+v", invalid)
	}
}

// The auth section must survive a settings save like every other section.
func TestSettingsPayload_RoundTripsAuthSection(t *testing.T) {
	settings := map[string]any{"auth": map[string]any{"mode": "signalk"}}

	if got := buildSettingsPayload(settings).Auth.Mode; got != "signalk" {
		t.Fatalf("auth.mode should surface from disk, got %q", got)
	}

	req := settingsPayload{}
	req.Auth.Mode = "signalk"
	if got := normalizeSettingsPayload(req).Auth.Mode; got != "signalk" {
		t.Fatalf("auth.mode should round-trip, got %q", got)
	}
}

// An install that has never set it reads as none, not empty.
func TestSettingsPayload_DefaultsAuthModeToNone(t *testing.T) {
	if got := buildSettingsPayload(map[string]any{}).Auth.Mode; got != "none" {
		t.Fatalf("missing auth section should default to none, got %q", got)
	}
}
