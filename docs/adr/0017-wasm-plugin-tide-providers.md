# ADR 0017: Sandboxed WASM Plugin Tide Providers

## Status
Accepted

## Context
Adding support for a new region's government tide-data source today means writing a new Go file implementing `tideProvider` (`backend/tide_providers.go`) and rebuilding/redeploying Helmcentral — something only the maintainer can do. Users outside Australia (NOAA for the US, UKHO for the UK, and so on) have no way to add their own region's tide source without forking the app and knowing Go.

The interface is already a clean extension point:
```go
type tideProvider interface {
    ID() string
    Name() string
    TTLSeconds() int64
    SearchStations(query string, limit int) []tideStation
    FetchTideChart(stationID string) (tideChartResult, error)
}
```
`interpolateTideNow(extremes, now)` in the same file computes current height/direction from a station's extremes and is shared by every provider — none compute it themselves. Two native providers exist (`tide_provider_bom.go`, `tide_provider_stormglass.go`), registered in `main.go`. The routes (`/api/tide-providers`, `/api/tide-stations`, `/api/tide-chart`) and the frontend (`use-tide-providers.ts`, `signalk-settings-panel.tsx`) already work generically off the provider registry — a new provider needs zero frontend changes to appear in the settings dropdown. What's missing is a way to add a new implementation of that interface without a Go rebuild.

Separately, the SignalK project's `signalk/signalk#213` discussion flags a real gap in most tide implementations: no spring/neap phase labeling, and no detection of days with a double high or double low water (common in some estuaries and along parts of the US East Coast). This ADR also closes that gap.

## Decision

### Sandbox runtime: Lua vs. WASM, evaluated head to head
Three mechanisms were rejected outright regardless of sandbox choice:
- **Build-time Go source discovery** — fails the goal outright; still requires an image rebuild per plugin.
- **Host compiles dropped-in Go source at container startup** — no rebuild needed, but permanently bundles the Go toolchain into the `alpine:latest` runtime image, reversing the minimal-image posture ADR-0011 established when it rejected `mattn/go-sqlite3` specifically to avoid cgo/toolchain baggage in prod.
- **Runtime subprocess + manifest** (precompiled binary, JSON-over-stdio) — no existing IPC precedent in this codebase, forces plugin authors to cross-compile per container arch, and gives unrestricted OS access instead of real sandboxing.

That left two genuinely competitive, embeddable, `CGO_ENABLED=0`-safe options: `github.com/yuin/gopher-lua` and `wazero` + `github.com/extism/go-sdk`. `wazero` advertises itself as "the only zero dependency WebAssembly runtime written in Go... by avoiding CGO, wazero avoids prerequisites such as shared libraries or libc," and `extism/go-sdk` is built directly on `wazero` and is itself cgo-free — both verified against their own docs, not assumed.

| Dimension | Lua (`gopher-lua`) | WASM (`wazero` + `extism/go-sdk`) |
|---|---|---|
| `CGO_ENABLED=0` safe | Yes — pure Go | Yes — pure Go, verified |
| Sandbox boundary | Library allowlist (`SkipOpenLibs` + explicit `open` calls) — safe *if* we never forget to leave something dangerous unregistered. A boundary we own and must get right. | WASM linear-memory isolation — a guest module cannot address host memory at all except through an explicit host function call. Enforced by the WASM spec/runtime itself, not by our care in registering globals. Structurally stronger. |
| Network access + SSRF mitigation | Hand-rolled `http_get` bound-in function; scheme/host allowlisting to block SSRF is something we'd design, implement, and have to get right ourselves. | `extism_http_request` is a built-in Extism host function; the manifest's `AllowedHosts []string` field is off-the-shelf SSRF mitigation — empty = deny all hosts, explicit per-plugin allowlist, wildcards supported. We inherit a maintained solution instead of inventing one. |
| Execution timeout | Hand-rolled: `context.WithTimeout` + gopher-lua's context-check-between-instructions — something we wire up ourselves. | `Manifest.Timeout` (`uint64`, milliseconds) is a first-class field Extism enforces itself — no custom timeout plumbing needed. |
| Plugin authoring language | Lua only | Author's choice: Rust, TinyGo, Zig, C, AssemblyScript, C++, Haskell — whichever PDK they prefer. |
| Authoring barrier | Lowest possible — a text editor, drop a `.lua` file, no compiler needed at all. | Real barrier — needs a WASM-capable toolchain (TinyGo, Rust + `wasm32` target, etc.) installed locally, and a compile step to produce the `.wasm` binary that gets dropped in. |
| Distribution | Plain text, human-readable/auditable by inspection. | Compiled binary — architecture-independent (one `.wasm` runs on any host), but not directly readable; community-shared prebuilt `.wasm` files become the realistic distribution unit. |
| New dependency surface | 1 new dependency. | 2 new dependencies (`extism/go-sdk`, transitively `wazero`) — heavier, but both are actively maintained and purpose-built for exactly this "host app + untrusted third-party plugin" pattern. |
| Security-critical code we own | All of it — the `http_get`/`json_decode`/`json_encode` binding layer and its safety properties are entirely our own bespoke code. | Mostly inherited — HTTP access control, host-function linking, timeout enforcement, and sandboxing primitives are upstream Extism/wazero's problem to get right and keep maintained. |

This plugin mechanism's entire premise is running **untrusted code from other people**, fetched over the internet — a materially higher bar than most "drop a file in" precedents in this codebase (ADR-0011's `sat_charts.go` uploads are inert data, never executed). The dimensions that matter most here are the sandbox boundary and how much security-critical glue code we end up owning, not authoring convenience — WASM wins clearly on both, at the real but one-time cost of a compiler toolchain instead of a text editor for plugin authors. **WASM via `wazero` + `extism/go-sdk` was chosen.**

### Design

**Runtime adapter** (`backend/wasm_tide_provider.go`): one `extism.CompiledPlugin` per discovered `.wasm` file, compiled once at startup; a fresh `Instance` per call, since a `Plugin`/`Instance` is not safe for concurrent use but `CompiledPlugin.Instance` is Extism's documented pattern for concurrent-safe access. `EnableWasi: true` is set in `extism.PluginConfig` — required for TinyGo-built plugins to initialize correctly.

**Narrowed plugin contract** — the same scope-narrowing considered for the rejected Lua design: a plugin returns only raw station and extreme data.
```
id() -> string
name() -> string
ttl_seconds() -> int (optional, default 3600)
search_stations(input: {"query": string, "limit": int}) -> [{"station_id", "name", "state", "lat", "lon", "timezone"}, ...]
fetch_tide_chart(input: station_id as raw bytes) -> {"station": {...}, "extremes": [{"time", "height_m", "high"}, ...]}
```
The host calls the existing shared `interpolateTideNow` to compute `CurrentHeightM`/`Direction`, and owns its own disk-backed cache per plugin (`cache/tide_wasm_<id>_cache.json`, TTL from `ttl_seconds()`, stale-on-error fallback — copying `bomTideCache`'s shape from `tide_provider_bom.go`). A plugin author never reimplements interpolation, caching, or the tidal-phase classification below.

**Discovery** (`main.go`, `loadWasmTideProviders(pluginsTidesDir())`): scans `plugins/tides/*.wasm` once at startup, right after the two native `registerTideProvider` calls. Because natives register first, a WASM plugin can never shadow `"bom"` or `"stormglass"` — first-registered wins. A corrupt or invalid plugin is logged and skipped, not fatal to startup — mirroring `sat_charts.go`'s "skip corrupt, keep going" idiom for uploaded content, applied here to discovered plugins instead. `pluginsTidesDir()` defaults to `plugins/tides`, overridable via `PLUGINS_TIDES_DIR`.

**Host allowlisting** (replaces the Lua plan's hand-rolled SSRF mitigation entirely): each plugin gets a companion `<name>.allowed_hosts.json` file (a JSON array of hostnames) next to its `.wasm` file, read at discovery time and passed into that plugin's `Manifest.AllowedHosts`. A missing companion file is the default — no network access for that plugin — so a plugin must explicitly declare what it talks to. `AllowedHosts` matches against `url.Hostname()` only (port stripped). This is visible, auditable, and enforced by Extism itself, not by us remembering to check a URL string at call time.

**Time containment**: `Manifest.Timeout` (ms), default 15000, overridable via `WASM_PLUGIN_TIMEOUT_MS`, enforced by Extism itself — no custom context-plumbing on our side.

**Panic/error containment**: `Instance.Call` returns a normal Go `(uint32, []byte, error)` — a guest-side fault surfaces as an `error`, not a Go panic. The adapter's public methods still wrap calls in `defer recover()` as defense-in-depth, matching `middleware.Recover()`'s role for HTTP handlers in `main.go`.

**A filename gotcha worth a permanent record**: the adapter file is named `wasm_tide_provider.go`, deliberately *not* `tide_provider_wasm.go` (which would otherwise match the `tide_provider_bom.go` / `tide_provider_stormglass.go` naming convention). A source file whose name ends in `_wasm.go` (or `_wasm_test.go` — Go strips a trailing `_test` before checking) is treated by the Go toolchain as implicitly constrained to `GOARCH=wasm`, per the filename-based build-constraint rules in `go help buildconstraint`. Such a file is silently excluded from every non-`wasm` build — confirmed empirically during implementation: it showed up in `go list`'s `IgnoredGoFiles` and produced `undefined:` errors at call sites rather than any error in the file itself. Any future `_wasm`-adjacent file in this codebase should keep "wasm" out of the trailing position of its filename.

**Reference example, not a BOM port**: `bomTideProvider` stays native Go. Two decisive reasons: `tideNearestHandler` does `p.(*bomTideProvider)` — a Go concrete-type assertion outside the `tideProvider` interface, so a plugin-backed BOM couldn't satisfy it without forcing every future provider (including unrelated plugins) to implement geographic nearest-station lookup; and BOM's HTML-scraping (`parseBomTidesTable`, regex-based against a fragile page format) would be materially harder and riskier to port into a WASM guest than the plugin mechanism itself is worth proving. Instead, `docs/examples/tide-plugins/noaa/main.go` ships as the worked example, backed by NOAA's public CO-OPS Tides & Currents JSON API (mdapi for station metadata, `datagetter` for high/low predictions) — a clean REST API with no scraping, written in TinyGo. Its compiled `.wasm` output is deliberately not committed anywhere in the live `plugins/tides/` directory, so a fresh install has zero active plugins and no surprise outbound traffic to a foreign government API; a user who wants NOAA builds it themselves (or downloads a prebuilt release artifact) into their own `plugins/tides/`.

### Spring/neap and double-tide labeling
Unaffected by the runtime choice — host-side Go logic in `tide_providers.go`, invoked from `tideChartHandler` and the `/api/tide-today` handler, not from either provider file. `tideChartResult` gained three `omitempty` fields, mirrored onto `tideChartResponse` and the `/api/tide-today` response types:
```go
TidalPhase      string `json:"tidal_phase,omitempty"`       // "springs", "springs+2", "neaps", "neaps-1", "" if indeterminate
DoubleHighToday bool   `json:"double_high_today,omitempty"`
DoubleLowToday  bool   `json:"double_low_today,omitempty"`
```
`classifyTidalPhase` finds the extreme-pair with max range (nearest local springs) and min range (nearest local neaps) in the fetched window, labels whichever is temporally closer to `now` with a day-offset if not today, and returns `""` when there's too little data to resolve. `hasDoubleTide` resolves "today" via `station.Timezone`, falling back to UTC with a logged warning if absent, and flags a double tide when either the high or low count within that local calendar day is ≥2. The frontend's existing binary heuristic (`frontend/src/lib/tide-phase.ts`, relative position within the fetched window's min/max) is preferred-over rather than replaced: `tidalPhase` is used when present, with the local heuristic as a fallback.

Source: `signalk/signalk#213` flagged both spring/neap labeling and double-tide-day detection as a common gap in tide implementations.

**Known limitation, stated plainly rather than hidden**: this is "days from nearest locally-observed range extremum," an approximation of true astronomical spring/neap phase (which tracks lunar synodic position, not just locally observed range — coastal/amphidromic effects can distort it at some sites). This is the same simplification the existing frontend heuristic already makes.

## Consequences

Positive:
- Zero-rebuild extensibility — a new region's tide source is a `.wasm` file and a JSON allowlist dropped into `plugins/tides/`, no fork or Go rebuild required, and it appears in the existing settings dropdown with zero frontend changes.
- Sandboxing is enforced by the WASM runtime itself (linear-memory isolation, `AllowedHosts`, `Manifest.Timeout`) rather than by hand-maintained equivalents we'd have to get right and keep right.
- Multi-language plugin authoring — TinyGo, Rust, Zig, C, AssemblyScript, C++, and Haskell PDKs all implement the identical contract; a plugin author isn't locked into Go.
- Off-the-shelf SSRF and timeout mitigation (`AllowedHosts`, `Manifest.Timeout`) instead of hand-rolled equivalents we'd own and have to audit ourselves.

Negative / explicitly deferred:
- Authoring requires a compiler toolchain (TinyGo, Rust + `wasm32`, etc.) — a real barrier relative to the Lua alternative's "just a text editor," accepted specifically because the sandbox-boundary and SSRF-mitigation dimensions mattered more than authoring convenience for code this untrusted.
- No shared cache utility yet — each WASM provider hand-rolls its own disk cache (copied from `bomTideCache`'s shape), rather than a factored-out helper all providers (native and WASM) could share.
- No plugin hot-reload — `plugins/tides/` is scanned once at startup; adding, removing, or updating a plugin requires a container restart.

## Related
- ADR-0011: In-App MBTiles Satellite Chart Upload-and-Serve — the `sat_charts.go` "drop a self-describing file in, validate at read time, skip-and-log corrupt entries rather than failing everything" precedent this partially mirrors, extended here from inert uploaded data to actually-executed sandboxed code.
- `signalk/signalk#213` (GitHub discussion) — external prior art for the spring/neap and double-tide-day gap this ADR closes.
