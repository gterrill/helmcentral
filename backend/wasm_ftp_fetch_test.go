package main

import (
	"encoding/json"
	"strings"
	"testing"

	extism "github.com/extism/go-sdk"
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
func TestFTPFetch_AllowedHostRealBOMRoundTripThroughNewWasmPluginBase(t *testing.T) {
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
