package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// Discovery exists because the operator shouldn't have to know their SignalK
// server's IP — on first install, or after the server moves. mDNS would be the
// obvious mechanism and the server does advertise itself, but the backend runs
// in a bridge container that can neither resolve .local (musl, no NSS-mDNS) nor
// emit multicast to the physical LAN. A bounded unicast sweep of the LAN /24
// does work from there, so that is what this implements (ADR 0029).
//
// The container is on 172.18.0.0/16, so it cannot infer the LAN to scan. Which
// network gets swept is therefore derived, and derived wrongly it would sweep
// something it shouldn't — hence the precedence and RFC1918 tests below.

func signalKServer(t *testing.T, vesselName string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/signalk", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"endpoints":{"v1":{"version":"2.24.0"}},"server":{"id":"signalk-server-node","version":"2.24.0"}}`)
	})
	mux.HandleFunc("/signalk/v1/api/vessels/self", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"name":%q,"navigation":{"state":{"value":"anchored"}}}`, vesselName)
	})
	return httptest.NewServer(mux)
}

func TestResolveDiscoveryNetwork_PrefersHintOverConfiguredAddress(t *testing.T) {
	t.Setenv("HELMCENTRAL_DISCOVERY_SUBNET", "")

	network, err := resolveDiscoveryNetwork("192.168.50.123", "10.0.0.5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := network.String(); got != "192.168.50.0/24" {
		t.Fatalf("expected the hint's /24, got %s", got)
	}
}

// Covers "the server moved": the dashboard may be open at localhost, so there
// is no usable hint, but the stale configured address still names the network
// the server is most likely still on.
func TestResolveDiscoveryNetwork_FallsBackToConfiguredAddress(t *testing.T) {
	t.Setenv("HELMCENTRAL_DISCOVERY_SUBNET", "")

	network, err := resolveDiscoveryNetwork("localhost", "192.168.50.240")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := network.String(); got != "192.168.50.0/24" {
		t.Fatalf("expected the configured address's /24, got %s", got)
	}
}

func TestResolveDiscoveryNetwork_FallsBackToEnvOverride(t *testing.T) {
	t.Setenv("HELMCENTRAL_DISCOVERY_SUBNET", "10.20.30.0/24")

	network, err := resolveDiscoveryNetwork("", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := network.String(); got != "10.20.30.0/24" {
		t.Fatalf("expected the env override, got %s", got)
	}
}

// Guessing a network to sweep would be a masking fallback: it would appear to
// work while scanning something arbitrary. Say so instead.
func TestResolveDiscoveryNetwork_ErrorsWhenNothingIsDerivable(t *testing.T) {
	t.Setenv("HELMCENTRAL_DISCOVERY_SUBNET", "")

	if _, err := resolveDiscoveryNetwork("localhost", ""); err == nil {
		t.Fatal("expected an error when no network can be derived")
	}
}

// The server must never be talked into sweeping a public range.
func TestResolveDiscoveryNetwork_RejectsNonPrivateAddresses(t *testing.T) {
	t.Setenv("HELMCENTRAL_DISCOVERY_SUBNET", "")

	for _, address := range []string{"8.8.8.8", "203.0.113.9"} {
		if _, err := resolveDiscoveryNetwork(address, ""); err == nil {
			t.Errorf("expected %s to be rejected as non-private", address)
		}
	}
}

func TestResolveDiscoveryNetwork_RejectsNonPrivateEnvOverride(t *testing.T) {
	t.Setenv("HELMCENTRAL_DISCOVERY_SUBNET", "8.8.8.0/24")

	if _, err := resolveDiscoveryNetwork("", ""); err == nil {
		t.Fatal("expected a public env override to be rejected")
	}
}

func TestScanForSignalK_FindsServerAndReportsVessel(t *testing.T) {
	srv := signalKServer(t, "Pikorua")
	defer srv.Close()
	host, port := hostPort(t, srv.URL)

	found := scanForSignalK(context.Background(), []string{host}, []int{port})
	if len(found) != 1 {
		t.Fatalf("expected exactly one server, got %d (%+v)", len(found), found)
	}
	if found[0].VesselName != "Pikorua" {
		t.Errorf("expected vessel name from the discovered server, got %q", found[0].VesselName)
	}
	if found[0].Version != "2.24.0" {
		t.Errorf("expected server version, got %q", found[0].Version)
	}
	if found[0].Port != port || found[0].Address != host {
		t.Errorf("expected %s:%d, got %s:%d", host, port, found[0].Address, found[0].Port)
	}
}

// Something listening on the port is not the same as SignalK being there.
// Reporting a random web server as a discovered vessel would send the operator
// to save a connection that can never work.
func TestScanForSignalK_ExcludesNonSignalKServices(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "<html>a router admin page</html>")
	}))
	defer plain.Close()
	host, port := hostPort(t, plain.URL)

	if found := scanForSignalK(context.Background(), []string{host}, []int{port}); len(found) != 0 {
		t.Fatalf("expected non-SignalK service to be excluded, got %+v", found)
	}
}

func TestScanForSignalK_FindsOnlyTheSignalKServerAmongSeveral(t *testing.T) {
	sig := signalKServer(t, "Pikorua")
	defer sig.Close()
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer plain.Close()

	sigHost, sigPort := hostPort(t, sig.URL)
	_, plainPort := hostPort(t, plain.URL)

	found := scanForSignalK(context.Background(), []string{sigHost}, []int{plainPort, sigPort})
	if len(found) != 1 || found[0].Port != sigPort {
		t.Fatalf("expected only the SignalK server, got %+v", found)
	}
}

func TestScanForSignalK_StopsAtContextDeadline(t *testing.T) {
	srv := signalKServer(t, "Pikorua")
	defer srv.Close()
	host, port := hostPort(t, srv.URL)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already done before the sweep starts

	start := time.Now()
	found := scanForSignalK(ctx, []string{host}, []int{port})
	if len(found) != 0 {
		t.Fatalf("expected no results from a cancelled scan, got %+v", found)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("cancelled scan should return promptly, took %s", elapsed)
	}
}

func postDiscover(t *testing.T, settingsPath string, body string) (int, map[string]any) {
	t.Helper()
	t.Setenv("SETTINGS_FILE", settingsPath)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/signalk/discover", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()

	if err := discoverSignalKHandler(e.NewContext(req, rec)); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var decoded map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &decoded)
	return rec.Code, decoded
}

func TestDiscoverHandler_ReportsWhichNetworkItSwept(t *testing.T) {
	t.Setenv("HELMCENTRAL_DISCOVERY_SUBNET", "")
	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)

	// 192.0.2.0/24 is TEST-NET-1 — nothing answers, so the sweep is quick and
	// the assertion is about the reported network, not the results.
	code, body := postDiscover(t, settingsPath, `{"hint":"10.77.0.5"}`)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%v)", code, body)
	}
	if got, _ := body["scanned_subnet"].(string); got != "10.77.0.0/24" {
		t.Fatalf("expected the swept network to be reported, got %q", got)
	}
}

func TestDiscoverHandler_ErrorsWhenNetworkUndeterminable(t *testing.T) {
	t.Setenv("HELMCENTRAL_DISCOVERY_SUBNET", "")
	// A hostname, not an address: nothing here yields a network to sweep.
	settingsPath := writeTestSettings(t, "signalk.example.invalid", 3000)

	code, body := postDiscover(t, settingsPath, `{"hint":"localhost"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("expected 400 when no network can be derived, got %d (%v)", code, body)
	}
	if field, _ := body["field"].(string); field != "signalk.address" {
		t.Errorf("expected field \"signalk.address\", got %q", field)
	}
}

// Discovery is a read, exactly like the connection probe: it reports what it
// found and the operator decides. Nothing it does may touch settings.yaml.
func TestDiscoverHandler_PersistsNothing(t *testing.T) {
	t.Setenv("HELMCENTRAL_DISCOVERY_SUBNET", "")
	settingsPath := writeTestSettings(t, "192.168.50.240", 3000)

	before, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read before: %v", err)
	}

	if code, body := postDiscover(t, settingsPath, `{"hint":"10.77.0.5"}`); code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%v)", code, body)
	}

	after, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("discovery must not persist anything:\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}
}
