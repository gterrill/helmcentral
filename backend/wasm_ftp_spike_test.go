// Disposable feasibility spike (see
// backend/testdata/wasm_plugins/src/ftpfetchspike/main.go for the "why").
// Proves whether a custom (non-built-in) Extism host function - the
// mechanism the planned "Forecast Warnings" plugin type needs, since its BOM
// default implementation can only reliably fetch bulletins over anonymous
// FTP, and a WASM guest cannot open raw sockets - actually works end to end:
// host registers a HostFunction that does a REAL FTP fetch, a TinyGo guest
// calls it via a hand-written //go:wasmimport declaration, and the guest
// gets real bytes back.
//
// Deliberately out of scope here (left for the real integration, which has
// its own security requirements): host allowlisting of the FTP target via
// manifest.AllowedHosts (a disallowed host is meant to panic, mirroring
// Extism's own built-in http_request behavior - this spike doesn't
// exercise that path), retry/fallback logic, and wiring into
// wasm_plugin.go's newWasmPluginBase (every real plugin type currently
// compiles with an empty []extism.HostFunction{} - this spike does not
// change that).
//
// This file is NOT deleted once the real feature is built - like
// wasm_es256_spike_test.go, it stays in the suite as a permanent contract
// test proving the underlying mechanism (custom host function + real
// network I/O across the WASM boundary) keeps working against the exact
// go-sdk/go-pdk API surface it was verified against here.
//
// Fixture regeneration (shares the same go.mod as the other
// backend/testdata/wasm_plugins/src/<name>/ fixtures - see
// backend/wasm_plugin_test.go's regeneration comment):
//
//	docker run --rm -v $(pwd):/src -w /src tinygo/tinygo:0.41.1 sh -c "
//	  cd backend/testdata/wasm_plugins/src &&
//	  go mod tidy &&
//	  tinygo build -o /src/backend/testdata/wasm_plugins/ftpfetchspike.wasm -target wasip1 -buildmode c-shared ./ftpfetchspike
//	"
//
// This test requires real internet access to ftp.bom.gov.au:21 (BOM's own
// sanctioned public anonymous FTP mirror, not scraping) and will fail if
// that host is unreachable from the environment running the test - that is
// the point: a passing result here is direct evidence a real FTP fetch
// crossed the WASM host/guest boundary, not a mocked stand-in for one.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	extism "github.com/extism/go-sdk"
	"github.com/jlaffaye/ftp"
)

const ftpFetchSpikeFixtureWasm = "testdata/wasm_plugins/ftpfetchspike.wasm"

type ftpFetchSpikeRequest struct {
	Host string `json:"host"`
	Path string `json:"path"`
}

type ftpFetchSpikeResponse struct {
	Body  string `json:"body"`
	Error string `json:"error"`
}

// fetchOverFTPSpike is a generalized (host+path instead of hardcoded BOM
// constants) port of bom_marine_warnings.go's fetchBomMarineWarningProduct:
// dial with a timeout, anonymous login, RETR, read all. A legitimate fetch
// failure (dial timeout, 550 no such file, etc.) is returned as an error to
// the caller, which turns it into a structured {"body":"","error":"..."}
// response for the guest - never a panic, which is reserved for a
// disallowed-host security violation (not exercised by this spike).
func fetchOverFTPSpike(host, path string) (string, error) {
	conn, err := ftp.Dial(host, ftp.DialWithTimeout(10*time.Second))
	if err != nil {
		return "", fmt.Errorf("failed to connect to FTP host %s: %v", host, err)
	}
	defer conn.Quit()

	if err := conn.Login("anonymous", "anonymous@"); err != nil {
		return "", fmt.Errorf("failed to log in to FTP host %s: %v", host, err)
	}

	resp, err := conn.Retr(path)
	if err != nil {
		return "", fmt.Errorf("failed to retrieve %s from %s: %v", path, host, err)
	}
	defer resp.Close()

	body, err := io.ReadAll(resp)
	if err != nil {
		return "", fmt.Errorf("failed to read %s from %s: %v", path, host, err)
	}

	return string(body), nil
}

// ftpFetchSpikeHostFunc is the host-side implementation of the "ftp_fetch"
// custom Extism host function, following the exact
// NewHostFunctionWithStack pattern go-sdk's own TestHost_memory
// (extism_test.go) demonstrates: a uint64 on the stack is a memory offset,
// read the guest's request bytes via plugin.ReadBytes, write the response
// back via plugin.WriteBytes, and return the new offset via stack[0].
func ftpFetchSpikeHostFunc(ctx context.Context, plugin *extism.CurrentPlugin, stack []uint64) {
	reqBytes, err := plugin.ReadBytes(stack[0])
	if err != nil {
		panic(fmt.Errorf("ftp_fetch: failed to read request from guest memory: %v", err))
	}

	var req ftpFetchSpikeRequest
	if err := json.Unmarshal(reqBytes, &req); err != nil {
		panic(fmt.Errorf("ftp_fetch: invalid request JSON from guest: %v", err))
	}

	var resp ftpFetchSpikeResponse
	body, ferr := fetchOverFTPSpike(req.Host, req.Path)
	if ferr != nil {
		resp.Error = ferr.Error()
	} else {
		resp.Body = body
	}

	respBytes, err := json.Marshal(resp)
	if err != nil {
		panic(fmt.Errorf("ftp_fetch: failed to marshal response: %v", err))
	}

	offset, err := plugin.WriteBytes(respBytes)
	if err != nil {
		panic(fmt.Errorf("ftp_fetch: failed to write response to guest memory: %v", err))
	}

	stack[0] = offset
}

// TestFTPFetchSpike_RealBOMRoundTripThroughWasmGuest is the hard-gate
// feasibility test: a TinyGo guest (ftpfetchspike.wasm) calls the ftp_fetch
// host function, which performs a REAL anonymous FTP fetch of BOM's live
// "QLD Marine Wind Warning Summary" product, and the guest returns the
// fetched body back to the host as its own output. Asserting on
// recognizable bulletin content (not just "some bytes came back") is
// deliberate - a panic-swallowed empty response or a stale/wrong file would
// otherwise pass a weaker check.
func TestFTPFetchSpike_RealBOMRoundTripThroughWasmGuest(t *testing.T) {
	ftpFetchHostFunction := extism.NewHostFunctionWithStack(
		"ftp_fetch",
		ftpFetchSpikeHostFunc,
		[]extism.ValueType{extism.ValueTypePTR},
		[]extism.ValueType{extism.ValueTypePTR},
	)

	ctx := context.Background()
	manifest := extism.Manifest{
		Wasm: []extism.Wasm{extism.WasmFile{Path: ftpFetchSpikeFixtureWasm}},
	}

	compiled, err := extism.NewCompiledPlugin(ctx, manifest, extism.PluginConfig{EnableWasi: true}, []extism.HostFunction{ftpFetchHostFunction})
	if err != nil {
		t.Fatalf("NewCompiledPlugin failed: %v", err)
	}
	defer compiled.Close(ctx)

	instance, err := compiled.Instance(ctx, extism.PluginInstanceConfig{ModuleConfig: wasmModuleConfig()})
	if err != nil {
		t.Fatalf("compiled.Instance failed: %v", err)
	}
	defer instance.Close(ctx)

	exit, out, err := instance.Call("fetch", nil)
	if err != nil {
		t.Fatalf("guest fetch() call failed (exit=%d): %v (guest error: %s)", exit, err, instance.GetError())
	}

	body := string(out)
	if body == "" {
		t.Fatalf("expected a non-empty BOM bulletin body, got an empty string")
	}

	if !strings.Contains(body, "IDQ20085") && !strings.Contains(body, "Queensland") && !strings.Contains(body, "Marine Wind Warning") {
		t.Fatalf("guest output does not look like the expected BOM bulletin (missing IDQ20085/Queensland/Marine Wind Warning), got:\n%s", body)
	}
}
