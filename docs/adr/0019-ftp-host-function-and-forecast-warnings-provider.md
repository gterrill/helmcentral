# ADR 0019: Custom FTP Host Function and Pluggable Forecast Warnings Provider

## Status
Accepted

## Context
`backend/bom_marine_warnings.go` + `backend/bom_marine_zones.go` (~600 lines) was a real subsystem — its own cache, an hourly background poller, a handler — with zero abstraction: no interface, no registry, a single hardcoded implementation calling Australia's Bureau of Meteorology (BOM) directly. It was Australia-only (state bounding boxes + BOM product IDs; SA/NT had no product IDs at all, TAS/WA had product IDs but no zone tables so zone-matching always failed there), so it was silently useless for any non-Australian install — the same shape of problem ADR-0017 (tides) and ADR-0018 (weather/waves) already solved for their domains. This ADR generalizes warnings into a pluggable "Forecast Warnings" provider, renamed from "Marine Warnings" to reflect that it's no longer BOM-specific, with BOM shipping as the default implementation.

**A real architectural blocker, absent from tide/weather/wave, had to be resolved first.** BOM's warnings data is fetched over anonymous FTP (`ftp.bom.gov.au`), not HTTP. Live-tested during planning: BOM's website actively detects and blocks automated HTTP access to the same product text (`https://www.bom.gov.au/fwo/IDQ20085.txt` returns `403` with an explicit "does not support web scraping" message on repeated/unspoofed requests) — FTP is BOM's own sanctioned public channel; HTTP scraping is something they've explicitly asked people not to do, a materially different posture than the tide-BOM plugin's HTML scraping (ADR-0017), which has no equivalent anti-bot notice. This repo's WASM sandbox (Extism/wazero) only gave guests HTTP access before this ADR (`pdk.NewHTTPRequest`, gated by `<name>.allowed_hosts.json`) — no FTP support in the Extism PDK, no custom host functions registered anywhere (`extism.NewCompiledPlugin(...)` was always called with an empty `[]extism.HostFunction{}`).

Two decisions were made explicitly, before finalizing the design:
1. **Build a custom Extism host function bridging anonymous FTP into the WASM sandbox**, rather than keeping BOM native (the alternative, and the lower-risk choice — it's what ADR-0017 did for BOM's tide data, for a different reason: HTML-scraping fragility, not protocol unavailability). Chosen because it keeps every provider in this project WASM-sandboxed, and because a disposable feasibility spike (see "Decision" below) proved it works cleanly with this codebase's exact SDK versions.
2. **Also build a second, genuinely-WASM reference plugin** — the US National Weather Service's `api.weather.gov` — confirmed live: free, keyless, explicitly documented as open for programmatic/automated use, with working zone-lookup-by-point and zone-scoped-active-alerts endpoints. This gives real non-Australian coverage, addressing the region-lock problem this ADR was about, the same way the NOAA tide plugin does for tides.

## Decision

### New capability: the `ftp_fetch` custom Extism host function

Verified via a disposable, hard-gate feasibility spike (`backend/wasm_ftp_spike_test.go`, `backend/testdata/wasm_plugins/src/ftpfetchspike/`) before any real code was built on top of it: a TinyGo guest can call a custom host function and get real anonymous-FTP-fetched bytes back through the WASM sandbox boundary. Confirmed against real, live BOM data (fetched `IDQ20085.txt`, the actual QLD marine wind warning bulletin), not a mock.

**Host side** (`github.com/extism/go-sdk@v1.7.1`): a `HostFunction` is built via `extism.NewHostFunctionWithStack(name, callback, params, returns)`. The callback (`func(ctx, plugin *extism.CurrentPlugin, stack []uint64)`) is low-level — no automatic JSON marshaling like a normal exported guest-function call gets. A `uint64` on the stack is a memory offset; the host reads the guest's request via `plugin.ReadBytes(offset)` and writes a response back via `plugin.WriteBytes(...)`, returning the new offset on the stack. Registered via the same already-present `[]extism.HostFunction{...}` argument to `extism.NewCompiledPlugin` that every plugin type already passes (previously always empty).

**Guest side** (`github.com/extism/go-pdk@v1.1.3`): no generic "call a custom host function" helper exists — this is go-pdk's own documented pattern for custom host functions: a hand-written `//go:wasmimport extism:host/user ftp_fetch` declaration (module name matching the `HostFunction`'s default `Namespace`), combined with existing public PDK memory helpers (`pdk.AllocateJSON`, `mem.Offset()`, `pdk.FindMemory(ptr).ReadBytes()`) to marshal the request/response — the same mechanism the PDK's own built-in HTTP support (`ExtismHTTPRequest`) is implemented with internally.

**Security model**: `Manifest.AllowedHosts` enforcement lives entirely inside Extism's *built-in* `http_request` function — it has zero automatic effect on a custom host function. `backend/wasm_ftp_fetch.go` reuses the exact same `<name>.allowed_hosts.json` file every plugin already has for HTTP: `newFTPFetchHostFunction(manifest.AllowedHosts)` builds a per-plugin closure over that list at construction time (`wasm_plugin.go`'s `newWasmPluginBase`, which already has the built manifest), enforcing identical host-matching semantics to Extism's own `http_request` (`github.com/gobwas/glob`, exact match or glob match, mirrored verbatim by reading Extism's own enforcement code). One file governs both protocols. A disallowed host **panics** — mirroring Extism's own `http_request` behavior for a disallowed host — caught by `wasmPluginBase.call()`'s existing `defer recover()` and converted into a clean Go error, no new panic-handling machinery needed. A legitimate fetch failure (dial timeout, 550 no such file, etc.) is instead a structured `{"body":"", "error":"..."}` response the guest can inspect and react to — never a panic, since one product ID failing shouldn't necessarily fail an entire `fetch_warnings` call.

**Made generic**, not special-cased to the warnings plugin type — every WASM plugin (tide, weather, wave, and any future type) gets `ftp_fetch` for free, gated by its own allowlist, with zero additional host-side wiring. Confirmed by the full existing test suite passing unchanged after wiring — a plugin that never calls `ftp_fetch` is completely unaffected by its presence.

### The Forecast Warnings plugin contract — a deliberate departure from tide/weather/wave

Tide/weather/wave plugins return raw provider data and the **host** does all shared derivation (interpolation, day-bucketing, moon phase, condition-vocabulary mapping) because there's a universal, shareable algorithm underneath every provider's numbers. Warnings don't have that: BOM's zone taxonomy (state bounding boxes → named coastal zones) and NWS's (UGC marine zone codes like `AMZ135`/`GMZ554`, resolved by point) are fundamentally incompatible namespaces, and "is this warning still active" is determined differently per provider — BOM via free-text section parsing (a `Cancellation` section type means not-active), NWS via structured CAP alert fields (`status: "Actual"`, `messageType !== "Cancel"`). There is nothing universal to factor out host-side.

**Decision: each plugin resolves its own zone(s) for the given lat/lon, fetches only the relevant bulletins, and returns only currently-active ones — the host does no zone-matching or active/cancelled filtering of its own.**

```
id() -> string
name() -> string
ttl_seconds() -> string(int)  (optional, default 3600; BOM declares 5400 matching the prior native TTL; NWS declares 1800)
fetch_warnings(input: {"lat": float64, "lon": float64}) -> {
  "region": string,           // human-readable, opaque to host, e.g. "QLD — Mackay Coast" or a NWS zone name
  "bulletins": [
    {
      "id": string,            // stable identifier (BOM product ID, NWS alert URN)
      "title": string,
      "category": string,      // opaque short tag, e.g. "wind", "surf" — the frontend treats this as opaque except a "wind" special-case
      "issued_at": string,     // RFC3339, optional/empty if unknown
      "details_url": string,
      "sections": [{"day": string, "warning_type": string}]   // BOM's forward-looking multi-day bulletins map to multiple sections; NWS's point-in-time alerts map to exactly one
    }
  ]
}
```
An empty `bulletins: []` is a legitimate, common, non-error response ("no warnings right now," or "this position has no coverage from this provider" — e.g. BOM outside Australia, NWS outside US coastal waters) — never conflated with a fetch failure.

### Reference plugins

- **`docs/examples/forecast-warnings-plugins/bom/`** (default, `id: "bom"`) — a faithful port of the deleted native implementation's `bomMarineWarningProducts`, `stateForPosition`/`zoneForPosition`, and bulletin-text parsing/active-filtering logic into TinyGo, using the new `ftp_fetch` host function for network access instead of a Go FTP client (TinyGo guests can't open raw sockets). Carries forward the native implementation's known coverage gaps (SA/NT unmapped, TAS/WA zone tables incomplete) as documented limitations, not something this port needed to fix. `bom.allowed_hosts.json`: `["ftp.bom.gov.au"]`.
- **`docs/examples/forecast-warnings-plugins/nws/`** (reference, `id: "nws"`) — a normal HTTP-based plugin (`pdk.NewHTTPRequest`, no host function needed) against `api.weather.gov`: `zones?type=marine&point=<lat>,<lon>` resolves the containing marine zone, `alerts/active?zone=<id>` fetches its active CAP alerts, mapped onto the contract (one NWS alert → one bulletin with exactly one section, since NWS alerts are point-in-time rather than BOM's forward-looking multi-day format). `nws.allowed_hosts.json`: `["api.weather.gov"]`.

### Settings persistence: a real bug found and fixed along the way

While live-verifying this feature's Settings UI dropdown, `ui.forecast_warnings_provider` (and, it turned out, `ui.weather_provider`/`ui.wave_provider` from ADR-0018 — never caught because that work's live verification never specifically exercised the Settings-panel save path for those two fields) silently failed to persist on save: `backend/signalk.go`'s typed `settingsPayload` struct was never extended with these three fields, so `updateSettingsHandler`'s `c.Bind(&req)` silently dropped them from the request body (standard Go JSON-unmarshal behavior for unrecognized fields), and `buildSettingsPayload` never surfaced them back out either. `GET /api/forecast-warnings`/`GET /api/weather-today`/`GET /api/wave-forecast` all worked correctly regardless (they resolve their provider by reading the raw settings map directly, not through the typed struct), which is exactly why this went unnoticed — only the Settings UI's save round-trip was affected. Fixed by adding `WeatherProvider`/`WaveProvider`/`ForecastWarningsProvider` string fields to `settingsPayload.UI`, wired through `normalizeSettingsPayload` (trim + pass through, no registry validation — consistent with `TideStationID`/`TideStationName`'s existing pattern and with ADR's established "validate at read time, not write time" principle) and `buildSettingsPayload`/`updateSettingsHandler`. Verified live: a configured value now round-trips through `POST`/`GET /api/settings` correctly, and an unregistered value produces the expected clear 502 from the relevant data endpoint rather than being silently discarded.

## Consequences

Positive:
- Zero-rebuild extensibility for warnings, matching tide/weather/wave — a new warnings source is a `.wasm` file (+ allowlist) dropped into `plugins/forecast-warnings/`, picked up on restart.
- A fresh install is useful for both Australian and US-coastal operators out of the box (BOM/NWS, both keyless) — previously Australia-only.
- The `ftp_fetch` host function is a genuinely reusable, generic capability, not a one-off hack — any future plugin type needing FTP inherits it for free, gated by the same allowlist file every plugin already has.
- A real, previously-unnoticed settings-persistence bug (affecting weather/wave provider selection too, not just this feature) was found and fixed as a direct result of this work's live-verification discipline.

Negative / explicitly deferred:
- The custom host function is genuinely new territory for this codebase (no prior precedent, hand-written low-level guest-side glue via `//go:wasmimport`) — a materially higher-complexity mechanism than everything else in the plugin system, accepted specifically because it keeps BOM sandboxed rather than carving out a native exception.
- No plugin hot-reload, same limitation every prior WASM plugin type already has — `plugins/forecast-warnings/` is scanned once at startup.
- BOM's known zone-coverage gaps (SA/NT, TAS/WA) are carried forward unresolved — out of scope for this port, same as the native implementation before it.
- Debugging a future BOM bulletin-format change is harder inside a sandboxed WASM guest than it would be in native Go — the same accepted tradeoff ADR-0017 made for BOM's tide-table scraping, now extended to a second BOM plugin.

## Related
- ADR-0017: Sandboxed WASM Plugin Tide Providers — the original plugin-registry pattern this ADR extends to a fourth domain, and the origin of the native-vs-WASM tradeoff reasoning this ADR revisits (and resolves differently) for BOM.
- ADR-0018: Sandboxed WASM Plugin Weather and Wave Forecast Providers — the shared `wasm_plugin.go` host layer this ADR's `ftp_fetch` function extends, and the "host derives nothing when providers have incompatible semantics" precedent (there: none needed; here: zone/active-filtering) this ADR follows explicitly for the first time.
