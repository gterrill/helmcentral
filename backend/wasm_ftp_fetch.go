// This file provides a generic "ftp_fetch" Extism host function, available
// to every WASM plugin type (tide, weather, wave, and future types such as
// forecast-warnings) via newWasmPluginBase in wasm_plugin.go - not
// special-cased to any one plugin type. It exists because BOM's marine
// warnings data (the planned "Forecast Warnings" plugin's default
// implementation) can only be reliably fetched over anonymous FTP - BOM's
// HTTP surface actively bot-blocks scraping of the same text - and a WASM
// guest cannot open raw sockets itself. Extism's own built-in HTTP host
// function only speaks HTTP, so a custom host function is the only way to
// bridge FTP into the sandbox.
//
// The host-allowlist enforcement below reuses the exact same
// <name>.allowed_hosts.json companion file every plugin already has for
// HTTP (manifest.AllowedHosts), with the identical host-matching semantics
// Extism's own built-in http_request host function uses (exact string
// match OR github.com/gobwas/glob pattern match against just the hostname,
// not the full "host:port" authority) - one file governs both protocols.
//
// This is the generalized, production version of the mechanism proven by
// the disposable spike in wasm_ftp_spike_test.go: same request/response
// contract and FTP fetch logic, plus the allowlist enforcement and
// newWasmPluginBase wiring the spike deliberately left out of scope.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"time"

	extism "github.com/extism/go-sdk"
	"github.com/gobwas/glob"
	"github.com/jlaffaye/ftp"
)

// ftpFetchRequest/ftpFetchResponse are the JSON contract exchanged with the
// guest across the WASM memory boundary:
//
//	request:  {"host": "ftp.bom.gov.au:21", "path": "/anon/gen/fwo/IDQ20085.txt"}
//	response: {"body": "<file contents as text>", "error": ""}
//
// error is non-empty on a legitimate FTP-operation failure (dial timeout,
// 550 no such file, etc.) - a disallowed host is NOT reported this way, it
// panics instead (see newFTPFetchHostFunction).
type ftpFetchRequest struct {
	Host string `json:"host"`
	Path string `json:"path"`
}

type ftpFetchResponse struct {
	Body  string `json:"body"`
	Error string `json:"error"`
}

// fetchOverFTP dials host with a timeout, logs in anonymously, RETRs path,
// and reads the whole file into memory. Identical logic to
// bom_marine_warnings.go's fetchBomMarineWarningProduct (and the spike's
// fetchOverFTPSpike), generalized to take host+path instead of hardcoded
// BOM constants. A fetch failure here is an ordinary, expected error - the
// caller turns it into a structured {"error": "..."} response for the
// guest, never a panic.
func fetchOverFTP(host, path string) (string, error) {
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

// hostAllowedForFTP reports whether host (an FTP "host" value such as
// "ftp.bom.gov.au:21") is permitted by allowedHosts, using EXACTLY the same
// enforcement go-sdk@v1.7.1's host.go httpRequest uses for the built-in HTTP
// host function: for each allowedHost, an exact string match against the
// hostname, or a github.com/gobwas/glob pattern match. The one difference is
// where the hostname comes from - httpRequest matches against a parsed
// url.URL's Hostname() (which never includes a port); here it's derived by
// stripping an optional ":<port>" suffix from host, since the FTP contract's
// host field is a bare "host[:port]" string, not a URL.
//
// A nil or empty allowedHosts rejects every host - the same default-deny
// posture manifestForWasmPlugin already documents for HTTP: no
// <name>.allowed_hosts.json file means no network access for that plugin.
func hostAllowedForFTP(host string, allowedHosts []string) bool {
	hostname := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		hostname = h
	}

	for _, allowedHost := range allowedHosts {
		if allowedHost == hostname {
			return true
		}
		if glob.MustCompile(allowedHost).Match(hostname) {
			return true
		}
	}

	return false
}

// newFTPFetchHostFunction builds the "ftp_fetch" custom Extism host
// function, closing over allowedHosts (one instance per plugin, built at
// plugin-construction time from that plugin's own manifest.AllowedHosts -
// see newWasmPluginBase in wasm_plugin.go). Follows the same
// NewHostFunctionWithStack low-level pattern (a uint64 stack value is a
// guest memory offset; read the request via plugin.ReadBytes, write the
// response via plugin.WriteBytes) as the spike's ftpFetchSpikeHostFunc.
//
// A malformed/unparseable request from the guest, or a WriteBytes failure,
// panics with a clear message - these are "this should never happen"
// conditions, not legitimate operational failures. A disallowed host ALSO
// panics (mirroring Extism's own built-in http_request behavior for a
// disallowed host) - this is a security-boundary violation, not a normal
// fetch failure. wasmPluginBase.call already wraps every guest call in a
// deferred recover(), so any of these panics surface to the caller as a
// clean Go error, not a crash. A legitimate FTP fetch failure (dial
// timeout, 550 no such file, etc.) is NOT a panic: it's returned to the
// guest as a normal {"body":"","error":"..."} response, since a plugin
// might reasonably want to try a different path if one fetch fails.
func newFTPFetchHostFunction(allowedHosts []string) extism.HostFunction {
	callback := func(ctx context.Context, plugin *extism.CurrentPlugin, stack []uint64) {
		reqBytes, err := plugin.ReadBytes(stack[0])
		if err != nil {
			panic(fmt.Errorf("ftp_fetch: failed to read request from guest memory: %v", err))
		}

		var req ftpFetchRequest
		if err := json.Unmarshal(reqBytes, &req); err != nil {
			panic(fmt.Errorf("ftp_fetch: invalid request JSON from guest: %v", err))
		}

		if !hostAllowedForFTP(req.Host, allowedHosts) {
			panic(fmt.Errorf("ftp_fetch: FTP request to %q is not allowed", req.Host))
		}

		var resp ftpFetchResponse
		body, ferr := fetchOverFTP(req.Host, req.Path)
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

	return extism.NewHostFunctionWithStack(
		"ftp_fetch",
		callback,
		[]extism.ValueType{extism.ValueTypePTR},
		[]extism.ValueType{extism.ValueTypePTR},
	)
}
