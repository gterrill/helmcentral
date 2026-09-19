package main

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ── E-3: the webhook URL had no address restriction at all ─────────────────
//
// alarm_service.go:455's /api/alarm-transports/test reflects whatever the
// configured webhook transport returns -- upstream status code or the raw
// connect error -- straight into the JSON response. Before this fix, nothing
// stopped an operator (or anyone who can reach Settings) from pointing the
// webhook at 127.0.0.1 or the container's own compose network and using that
// endpoint as a port scanner. The fix has two halves: this file rejects the
// URL at save time (validateWebhookURL, wired into validateAlarmTransports);
// alarm_service_test.go covers the other half, the test endpoint no longer
// echoing upstream detail.
//
// Private-address decision: RFC1918/ULA ranges are deliberately ALLOWED.
// Helmcentral runs on a boat LAN (this project's own deployment target is
// 192.168.50.240) where a webhook to a LAN-resident Home Assistant or
// Node-RED instance is a legitimate, expected configuration, not an attack.
// Blocking RFC1918 outright would break that for zero benefit, since an
// operator who controls Settings already controls the LAN. What has NO
// legitimate webhook use from this container is loopback (the container's
// own other listeners), link-local (which also covers the cloud metadata
// address 169.254.169.254 -- meaningless on a boat with no cloud metadata
// service, but blocked anyway since it costs nothing and matches the
// general-purpose SSRF guidance the finding cites), and unspecified
// (0.0.0.0/::). Because RFC1918 stays reachable, the oracle fix in
// alarm_service.go matters more here than it would if this file blocked
// everything private -- see that file's tests.
func writableWebhookConfig(url string) alarmTransportConfig {
	return alarmTransportConfig{Webhook: webhookConfig{Enabled: true, URL: url}}
}

func TestValidateAlarmTransportsRejectsLoopbackWebhookIPLiteral(t *testing.T) {
	config := writableWebhookConfig("http://127.0.0.1:9000/hook")
	if err := validateAlarmTransports(&config); err == nil {
		t.Fatal("expected a loopback webhook URL to be rejected")
	}
}

func TestValidateAlarmTransportsRejectsIPv6LoopbackWebhookIPLiteral(t *testing.T) {
	config := writableWebhookConfig("http://[::1]:9000/hook")
	if err := validateAlarmTransports(&config); err == nil {
		t.Fatal("expected an IPv6 loopback webhook URL to be rejected")
	}
}

func TestValidateAlarmTransportsRejectsLinkLocalWebhookIPLiteral(t *testing.T) {
	// 169.254.169.254 is the cloud metadata address on every major cloud;
	// it is also, uncoincidentally, link-local.
	config := writableWebhookConfig("http://169.254.169.254/latest/meta-data/")
	if err := validateAlarmTransports(&config); err == nil {
		t.Fatal("expected a link-local (cloud metadata) webhook URL to be rejected")
	}
}

func TestValidateAlarmTransportsRejectsUnspecifiedWebhookIPLiteral(t *testing.T) {
	config := writableWebhookConfig("http://0.0.0.0:9000/hook")
	if err := validateAlarmTransports(&config); err == nil {
		t.Fatal("expected an unspecified-address webhook URL to be rejected")
	}
}

// The documented tradeoff: a LAN address is this project's own real
// deployment target, and must keep working.
func TestValidateAlarmTransportsAllowsPrivateLANWebhookIPLiteral(t *testing.T) {
	config := writableWebhookConfig("http://192.168.50.240:9091/hook")
	if err := validateAlarmTransports(&config); err != nil {
		t.Fatalf("expected a private-LAN webhook URL to be allowed, got: %v", err)
	}
}

func TestValidateAlarmTransportsAllowsAnOrdinaryPublicWebhookHost(t *testing.T) {
	original := webhookURLResolver
	webhookURLResolver = func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("203.0.113.10")}, nil
	}
	t.Cleanup(func() { webhookURLResolver = original })

	config := writableWebhookConfig("https://hooks.example.com/notify")
	if err := validateAlarmTransports(&config); err != nil {
		t.Fatalf("expected an ordinary public hostname to be allowed, got: %v", err)
	}
}

// DNS rebinding: a hostname is fine to type, but if it resolves to loopback
// or link-local, allowing it would defeat the IP-literal checks above for
// zero effort from an attacker.
func TestValidateAlarmTransportsRejectsWebhookHostnameResolvingToLoopback(t *testing.T) {
	original := webhookURLResolver
	webhookURLResolver = func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("127.0.0.1")}, nil
	}
	t.Cleanup(func() { webhookURLResolver = original })

	config := writableWebhookConfig("http://attacker.example/hook")
	if err := validateAlarmTransports(&config); err == nil {
		t.Fatal("expected a hostname resolving to loopback to be rejected")
	}
}

// A hostname resolving to more than one address must be rejected if ANY of
// them is disallowed, not just the first -- otherwise an attacker adds a
// second, permitted-looking A record and this becomes a coin flip.
func TestValidateAlarmTransportsRejectsWebhookHostnameWithAnyDisallowedAddress(t *testing.T) {
	original := webhookURLResolver
	webhookURLResolver = func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("203.0.113.10"), net.ParseIP("169.254.169.254")}, nil
	}
	t.Cleanup(func() { webhookURLResolver = original })

	config := writableWebhookConfig("http://multi-a-record.example/hook")
	if err := validateAlarmTransports(&config); err == nil {
		t.Fatal("expected rejection when any resolved address is disallowed")
	}
}

func TestValidateAlarmTransportsRejectsUnresolvableWebhookHostname(t *testing.T) {
	original := webhookURLResolver
	webhookURLResolver = func(host string) ([]net.IP, error) {
		return nil, fmt.Errorf("no such host")
	}
	t.Cleanup(func() { webhookURLResolver = original })

	config := writableWebhookConfig("http://typo.invalid/hook")
	if err := validateAlarmTransports(&config); err == nil {
		t.Fatal("expected an unresolvable webhook hostname to be rejected, not silently accepted")
	}
}

// A disabled webhook is never sent to, so an address that would otherwise be
// rejected must not block saving the rest of the config -- consistent with
// every other transport's "only validated when enabled" behaviour already in
// this file (see e.g. the ntfy/smtp checks above validateWebhookURL).
func TestValidateAlarmTransportsSkipsAddressCheckWhenWebhookDisabled(t *testing.T) {
	config := alarmTransportConfig{Webhook: webhookConfig{Enabled: false, URL: "http://127.0.0.1:9000/hook"}}
	if err := validateAlarmTransports(&config); err != nil {
		t.Fatalf("expected a disabled webhook's URL to be left unvalidated, got: %v", err)
	}
}

// setAlarmTransportsForTest writes directly to the package-level transports
// state, bypassing setAlarmTransports/validateAlarmTransports entirely.
//
// alarm_service_test.go's testAlarmTransportsHandler tests need a webhook
// transport already pointed at an httptest.Server, which listens on
// loopback -- exactly the address validateWebhookURL now refuses at save
// time (that's the point of this file's fix). Real Settings can never save
// such a config, but the /test endpoint doesn't re-validate what's already
// stored -- it only reads getAlarmTransports() -- so this mirrors "already
// saved, already validated at the time it was saved" rather than exercising
// the save path a second time.
func setAlarmTransportsForTest(t *testing.T, config alarmTransportConfig) {
	t.Helper()
	alarmTransportsMu.Lock()
	previous := alarmTransportsState
	alarmTransportsState = config
	alarmTransportsMu.Unlock()
	t.Cleanup(func() {
		alarmTransportsMu.Lock()
		alarmTransportsState = previous
		alarmTransportsMu.Unlock()
	})
}

func TestValidateWebhookURLErrorNamesNoUpstreamDetail(t *testing.T) {
	// Belt-and-suspenders on the error message itself: it must be safe to
	// have shown to whoever is filling in Settings, i.e. it must not, say,
	// echo back resolved internal IPs in a way that invites iterating them.
	// (The real oracle risk is the /test endpoint, covered in
	// alarm_service_test.go -- this just keeps this message sane too.)
	err := validateWebhookURL("http://127.0.0.1:9000/hook")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "webhook") {
		t.Fatalf("expected the error to say what it's complaining about, got: %v", err)
	}
}

// A URL that passes validateWebhookURL when saved can still answer a real
// alarm with a redirect into a range only Helmcentral can reach. E-3's
// save-time check settles the first hop; notifyHTTPClient's CheckRedirect is
// what makes it hold for the rest of the chain.
func TestNotifyHTTPClientRefusesRedirectIntoLoopback(t *testing.T) {
	loopback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer loopback.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, loopback.URL, http.StatusFound)
	}))
	defer redirector.Close()

	// httptest binds 127.0.0.1, so the redirect target is loopback by
	// construction - exactly the hop the policy must refuse.
	_, err := notifyHTTPClient().Get(redirector.URL)
	if err == nil {
		t.Fatal("expected the redirect into loopback to be refused, got no error")
	}
}
