package main

import (
	"encoding/json"
	"errors"
	"net/textproto"
	"strings"
	"testing"

	extism "github.com/extism/go-sdk"
	"github.com/jlaffaye/ftp"
)

const ftpFetchFixtureWasm = "testdata/wasm_plugins/ftpfetch.wasm"

// --- hostAllowedForFTP: same exact-match-OR-glob-match semantics as
// Extism's own built-in http_request allowlist enforcement (host.go's
// httpRequest), just matched against an FTP "host:port" value's hostname
// instead of a parsed URL's Hostname(). ---

func TestHostAllowedForFTP_ExactMatchAllowed(t *testing.T) {
	if !hostAllowedForFTP("ftp.bom.gov.au:21", []string{"ftp.bom.gov.au"}) {
		t.Fatalf("expected an exact hostname match (with port stripped) to be allowed")
	}
}

func TestHostAllowedForFTP_GlobPatternAllowed(t *testing.T) {
	if !hostAllowedForFTP("ftp.bom.gov.au:21", []string{"*.bom.gov.au"}) {
		t.Fatalf("expected a glob pattern matching the hostname to be allowed")
	}
}

func TestHostAllowedForFTP_NonMatchingHostRejected(t *testing.T) {
	if hostAllowedForFTP("evil.example.com:21", []string{"ftp.bom.gov.au"}) {
		t.Fatalf("expected a non-matching host to be rejected")
	}
}

func TestHostAllowedForFTP_EmptyAllowedHostsRejectsEverything(t *testing.T) {
	if hostAllowedForFTP("ftp.bom.gov.au:21", nil) {
		t.Fatalf("expected nil allowedHosts to reject every host (same default-deny posture as HTTP)")
	}
	if hostAllowedForFTP("ftp.bom.gov.au:21", []string{}) {
		t.Fatalf("expected empty allowedHosts to reject every host")
	}
}

func TestHostAllowedForFTP_NoPortInHostStillMatches(t *testing.T) {
	if !hostAllowedForFTP("ftp.bom.gov.au", []string{"ftp.bom.gov.au"}) {
		t.Fatalf("expected a bare hostname (no port) to still match exactly")
	}
}

// TestHostAllowedForFTP_NonStandardPortRejected is the E-6 security-audit
// finding: net.SplitHostPort strips the port before matching, so an
// allowlisted "ftp.bom.gov.au" previously let a guest dial ANY port on that
// host - "ftp.bom.gov.au:8080" passed the hostname check even though 8080
// has nothing to do with FTP. allowed_hosts.json has no existing convention
// for encoding a port (it's shared verbatim with the HTTP allowlist, which
// also only ever matches a hostname), so the fix pins every FTP connection
// to the standard control port rather than trusting a port the guest itself
// supplied in the request.
func TestHostAllowedForFTP_NonStandardPortRejected(t *testing.T) {
	if hostAllowedForFTP("ftp.bom.gov.au:8080", []string{"ftp.bom.gov.au"}) {
		t.Fatalf("expected a non-standard port on an otherwise-allowed host to be rejected")
	}
}

// TestHostAllowedForFTP_StandardPortExplicitlyStatedStillMatches proves the
// fix doesn't break the ordinary case: a request that explicitly names the
// standard FTP port must still match exactly like before.
func TestHostAllowedForFTP_StandardPortExplicitlyStatedStillMatches(t *testing.T) {
	if !hostAllowedForFTP("ftp.bom.gov.au:21", []string{"ftp.bom.gov.au"}) {
		t.Fatalf("expected the standard FTP port stated explicitly to still match")
	}
}

// --- End-to-end: prove the wiring in newWasmPluginBase actually works, not
// just wasm_ftp_spike_test.go's standalone ad-hoc manifest. Both tests below
// use the SAME compiled fixture (ftpfetch.wasm, which unlike the spike's
// fixture accepts {"host","path"} as input rather than hardcoding BOM's
// product), varying only the manifest's AllowedHosts and the requested
// host, so a single guest exercises both the allow and deny paths. ---

type ftpFetchFixtureInput struct {
	Host string `json:"host"`
	Path string `json:"path"`
}

// TestFTPFetch_AllowedHostRealBOMRoundTripThroughNewWasmPluginBase proves a
// plugin built the REAL way (manifestForWasmPlugin-shaped manifest ->
// newWasmPluginBase -> wasmPluginBase.call) can successfully fetch real BOM
// data over FTP when its AllowedHosts includes the target host - the
// production wiring path, not the spike's standalone manifest/host function
// construction.
// It hits BOM's live FTP server, so -short skips it to keep the default
// suite offline-clean.
func TestFTPFetch_AllowedHostRealBOMRoundTripThroughNewWasmPluginBase(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live BOM FTP round-trip in short mode")
	}

	manifest := extism.Manifest{
		Wasm:         []extism.Wasm{extism.WasmFile{Path: ftpFetchFixtureWasm}},
		AllowedHosts: []string{"ftp.bom.gov.au"},
	}

	base, err := newWasmPluginBase(manifest, "plugins/test")
	if err != nil {
		t.Fatalf("newWasmPluginBase failed: %v", err)
	}

	input, err := json.Marshal(ftpFetchFixtureInput{
		Host: "ftp.bom.gov.au:21",
		Path: "/anon/gen/fwo/IDQ20085.txt",
	})
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}

	out, err := base.call("fetch_ftp", input)
	if err != nil {
		t.Fatalf("fetch_ftp call failed: %v", err)
	}

	var resp ftpFetchResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal fetch_ftp output failed: %v (raw: %s)", err, out)
	}
	if resp.Error != "" {
		t.Fatalf("expected no fetch error for an allowed host, got: %s", resp.Error)
	}
	if resp.Body == "" {
		t.Fatalf("expected a non-empty BOM bulletin body, got an empty string")
	}
	if !strings.Contains(resp.Body, "IDQ20085") && !strings.Contains(resp.Body, "Queensland") && !strings.Contains(resp.Body, "Marine Wind Warning") {
		t.Fatalf("guest output does not look like the expected BOM bulletin (missing IDQ20085/Queensland/Marine Wind Warning), got:\n%s", resp.Body)
	}
}

// TestFTPFetch_DisallowedHostPanicsAndIsRecoveredIntoCleanError proves the
// disallowed-host security boundary works through the REAL call path:
// newFTPFetchHostFunction panics on a disallowed host (mirroring Extism's
// own built-in http_request behavior), and wasmPluginBase.call's existing
// defer recover() converts that panic into a clean Go error, not a crash or
// hang - end to end through a real newWasmPluginBase-constructed plugin.
func TestFTPFetch_DisallowedHostPanicsAndIsRecoveredIntoCleanError(t *testing.T) {
	manifest := extism.Manifest{
		Wasm:         []extism.Wasm{extism.WasmFile{Path: ftpFetchFixtureWasm}},
		AllowedHosts: []string{"example.com"}, // deliberately does NOT include ftp.bom.gov.au
	}

	base, err := newWasmPluginBase(manifest, "plugins/test")
	if err != nil {
		t.Fatalf("newWasmPluginBase failed: %v", err)
	}

	input, err := json.Marshal(ftpFetchFixtureInput{
		Host: "ftp.bom.gov.au:21",
		Path: "/anon/gen/fwo/IDQ20085.txt",
	})
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}

	_, err = base.call("fetch_ftp", input)
	if err == nil {
		t.Fatalf("expected a clean Go error for a disallowed host, got nil")
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("expected the error to mention the host was not allowed, got: %v", err)
	}
}

// TestValidateFTPPath_ControlCharactersRejected is the E-5 security-audit
// finding: conn.Retr(path) reaches jlaffaye/ftp's textproto.Writer.PrintfLine
// as `RETR <path>`, which appends the protocol's CRLF terminator but does
// not itself strip or escape one embedded in path - so a guest sending
// "/x\r\nPORT 10,0,0,5,0,80" would inject a second, arbitrary FTP command
// onto the already-authenticated control connection. Any ASCII control
// character (not just CR/LF) must be rejected before the path ever reaches
// Retr.
func TestValidateFTPPath_ControlCharactersRejected(t *testing.T) {
	cases := []string{
		"/x\r\nPORT 10,0,0,5,0,80",
		"/x\nPORT 10,0,0,5,0,80",
		"/x\rPORT 10,0,0,5,0,80",
		"/x\x00y",
		"/x\x7fy", // DEL
	}
	for _, path := range cases {
		if err := validateFTPPath(path); err == nil {
			t.Errorf("expected validateFTPPath to reject %q", path)
		}
	}
}

func TestValidateFTPPath_OrdinaryPathAccepted(t *testing.T) {
	if err := validateFTPPath("/anon/gen/fwo/IDQ20085.txt"); err != nil {
		t.Errorf("expected an ordinary path to be accepted, got: %v", err)
	}
}

// TestFTPFetch_CRLFInjectedPathPanicsAndIsRecoveredIntoCleanError proves the
// CR/LF rejection is wired into the real host-function call path (not just
// the standalone validateFTPPath helper) and, like the disallowed-host
// check right next to it in newFTPFetchHostFunction, panics rather than
// returning an ordinary {"error": "..."} response - this is a
// security-boundary violation from a misbehaving guest, not a legitimate
// operational fetch failure, so it must be treated the same way. This
// never dials the network: the check runs before fetchOverFTP is ever
// called, the same way the disallowed-host check does.
func TestFTPFetch_CRLFInjectedPathPanicsAndIsRecoveredIntoCleanError(t *testing.T) {
	manifest := extism.Manifest{
		Wasm:         []extism.Wasm{extism.WasmFile{Path: ftpFetchFixtureWasm}},
		AllowedHosts: []string{"ftp.bom.gov.au"},
	}

	base, err := newWasmPluginBase(manifest, "plugins/test")
	if err != nil {
		t.Fatalf("newWasmPluginBase failed: %v", err)
	}

	input, err := json.Marshal(ftpFetchFixtureInput{
		Host: "ftp.bom.gov.au:21",
		Path: "/anon/gen/fwo/IDQ20085.txt\r\nPORT 10,0,0,5,0,80",
	})
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}

	_, err = base.call("fetch_ftp", input)
	if err == nil {
		t.Fatalf("expected a clean Go error for a CRLF-injected path, got nil")
	}
	if !strings.Contains(err.Error(), "control character") {
		t.Errorf("expected the error to mention the control character, got: %v", err)
	}
}

// BOM publishes a warning product only while that warning is in force, so a
// 550 for IDQ20085 means "no marine wind warning for Queensland today", not
// "BOM is broken". The two are indistinguishable in the error text, so the
// host reports the FTP reply code structurally and lets the guest decide.
func TestFTPErrorIsNotFound_550IsNotFound(t *testing.T) {
	// Built by the same function the fetch path uses. Asserting against a
	// hand-wrapped error instead would pass even when fetchOverFTP formats the
	// cause away with %v, which is exactly the bug this pair has to catch.
	err := retrError("/anon/gen/fwo/IDQ20085.txt", "ftp.bom.gov.au:21",
		&textproto.Error{Code: ftp.StatusFileUnavailable, Msg: "Failed to open file."})

	if !ftpErrorIsNotFound(err) {
		t.Fatalf("a wrapped 550 must report as not-found: %v", err)
	}
	if !strings.Contains(err.Error(), "550") {
		t.Fatalf("the server's own message must survive for the log: %v", err)
	}
}

// Every other protocol-level failure stays a failure. A 421 is the server
// throwing us off, which is exactly the upstream problem the fail-fast policy
// wants surfaced rather than reported as an empty result.
func TestFTPErrorIsNotFound_OtherProtocolCodesAreNot(t *testing.T) {
	for _, code := range []int{ftp.StatusNotAvailable, ftp.StatusBadCommand, ftp.StatusNotLoggedIn} {
		err := retrError("/x", "h", &textproto.Error{Code: code, Msg: "nope"})
		if ftpErrorIsNotFound(err) {
			t.Fatalf("FTP code %d must not report as not-found", code)
		}
	}
}

// A dial timeout never reaches the protocol layer, so there is no code to
// read. It is a genuine fetch failure.
func TestFTPErrorIsNotFound_NonProtocolErrorIsNot(t *testing.T) {
	if ftpErrorIsNotFound(errors.New("failed to connect to FTP host ftp.bom.gov.au:21: i/o timeout")) {
		t.Fatal("a dial failure must not report as not-found")
	}
	if ftpErrorIsNotFound(nil) {
		t.Fatal("a nil error must not report as not-found")
	}
}
