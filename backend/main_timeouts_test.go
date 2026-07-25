package main

import (
	"os"
	"testing"
	"time"
)

// TestMain shortens the network timeouts that the suite would otherwise spend
// in real wall-clock waiting.
//
// Nothing in this package talks to a real remote SignalK server: every test
// either points at an httptest server on loopback (sub-millisecond) or at a
// deliberately unroutable reserved address (RFC 5737 TEST-NET, RFC 1918 ranges
// with nothing on them) precisely so the connection *cannot* succeed. The
// production defaults are sized for a boat's LAN over Wi-Fi, so on those
// unroutable cases each test paid the full default before it could assert -
// three seconds per SignalK probe, and 600ms x 254 hosts per discovery sweep.
// That waiting was the single largest cost in the suite.
//
// These are the same env knobs an operator has, set here to loopback-scale
// values. Tests that care about timeout behaviour itself still set their own
// via t.Setenv, which overrides these and restores them afterwards.
func TestMain(m *testing.M) {
	// Generous for a loopback httptest server, ~12x faster than the 3s default
	// on an address that will never answer.
	os.Setenv("SIGNALK_READ_TIMEOUT_MS", "250")

	// A discovery sweep dials 254 hosts at discoveryConcurrency=64, so the
	// blackholed /24 the handler tests use costs 4 batches of this value.
	os.Setenv("HELMCENTRAL_DISCOVERY_DIAL_TIMEOUT_MS", "25")

	os.Exit(m.Run())
}

// Because TestMain overrides both knobs for the whole suite, no other test
// exercises the values production actually runs with. These pin the documented
// defaults so shortening the suite can't quietly reduce the real timeouts:
// getEnv treats empty as unset, so this is the unset path.
func TestNetworkTimeoutDefaults(t *testing.T) {
	t.Setenv("SIGNALK_READ_TIMEOUT_MS", "")
	if got := signalKReadTimeout(); got != 3*time.Second {
		t.Errorf("default SignalK read timeout: expected 3s, got %s", got)
	}

	t.Setenv("HELMCENTRAL_DISCOVERY_DIAL_TIMEOUT_MS", "")
	if got := discoveryDialTimeout(); got != 600*time.Millisecond {
		t.Errorf("default discovery dial timeout: expected 600ms, got %s", got)
	}
}

// An unparseable or non-positive override must not silently become "no
// timeout" (a hung read) or "zero timeout" (nothing can ever connect).
func TestNetworkTimeoutsRejectInvalidOverrides(t *testing.T) {
	for _, raw := range []string{"soon", "0", "-5"} {
		t.Setenv("SIGNALK_READ_TIMEOUT_MS", raw)
		if got := signalKReadTimeout(); got != 3*time.Second {
			t.Errorf("SIGNALK_READ_TIMEOUT_MS=%q: expected fallback to 3s, got %s", raw, got)
		}

		t.Setenv("HELMCENTRAL_DISCOVERY_DIAL_TIMEOUT_MS", raw)
		if got := discoveryDialTimeout(); got != 600*time.Millisecond {
			t.Errorf("HELMCENTRAL_DISCOVERY_DIAL_TIMEOUT_MS=%q: expected fallback to 600ms, got %s", raw, got)
		}
	}
}
