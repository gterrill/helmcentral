# ADR 0051: Trend Gauges Backed by InfluxDB

## Status
Accepted

Extends ADR 0039, which recorded that "gauges show instantaneous values only" and that trend for an arbitrary path needed a generic history store. This decision uses the existing InfluxDB integration instead of building a new store.

## Context

Dirona's N2KView setup keeps "all NMEA2000 telemetry stored every five seconds for multiple years" behind a dedicated trending screen, and the reason given for it is diagnostic: seeing a house-battery draw, you want to know "if it's been doing that for a while or if it's just a spike". A single instantaneous number cannot answer that.

Helmcentral's `telemetry_history.go` held exactly two ring buffers — `depthHistory` and `windGustHistory` — each hardcoded, RAM-only, and capped at roughly six hours. Depth got a sparkline; nothing else could have one, whatever the boat published.

ADR 0039 assumed closing this meant generalising those buffers into a per-path store. That was the wrong read of the existing code.

## Decision

### 1. InfluxDB carries history; no store is built

The `signalk-to-influxdb-v2` plugin already writes **the SignalK path as the Influx measurement name**. `queryInfluxDepthTrend` was therefore already a per-path query with its path hardcoded as a constant. Generalising it to `queryInfluxPathTrend(path, window)` is threading a parameter, not building infrastructure.

Using the existing dependency avoids implementing a per-path in-memory store with ring buffers, a memory budget and path-selection rules, while retaining access to longer histories.

ADR 0020 made InfluxDB an opt-in enhancement, and this makes it a **prerequisite for one feature** rather than for the app. Everything that worked without it still works without it; trend gauges are the one thing that does not.

### 2. Absent InfluxDB is a 503, never an empty series

`GET /api/telemetry/history` returns `503` with a reason when `influxTelemetryConfigured()` is false, and `502` with the underlying error when a configured Influx fails.

An empty or fabricated series must not hide a missing database or failed query, consistent with ADR 0039's treatment of missing readings and AGENTS.md's fallback policy. The tile renders the server's own message, "history needs InfluxDB, which is not configured", and keeps showing the live reading, which does not depend on it.

### 3. `window` is an allowlist, because it reaches a Flux query

`telemetryHistoryWindows` is five literal strings. A duration parse would happily accept `1h) |> yield(` as far as the grammar is concerned, and the value is interpolated into the query. The allowlist doubles as the picker's option list, so the two cannot disagree.

Resolution is derived from the window (`1h`→`1m`, `24h`→`15m`) so a week of data does not return thousands of points for a sparkline a few hundred pixels wide.

### 4. A fifth display kind, not a sixth widget

`display: 'trend'` joins numeric/radial/bar/lamp inside `GaugeBody` (ADR 0049), so it works standalone *and* as a member of a gauge group with no extra wiring. A Dirona-style historical screen is a page of them; an engine cluster with one trend member is a group.

Hand-rolled SVG following `depth-sparkline.tsx`, since the dashboard carries no chart library (ADR 0012). The live value stays the hero readout and the line sits beneath it, per AGENTS.md's rule that trend presentation stays secondary to the real-time number. Zones shade as horizontal bands, so the same red that colours the dial marks the region on the line.

The trend scale reuses the gauge's own `min`/`max` when set, so a dial and a trend bound to the same path use the same scale.

### 5. Polling matched to the window, not to the tick

`useTelemetryHistory` re-reads every minute for a 1h window and every fifteen for a day, rather than riding the 1s `gauge-values` stream. A trend over hours does not change meaningfully between frames, and history is a query against a time-series database rather than a value already in a snapshot the server holds.

## Consequences

- Any published path can be trended, closing the gap ADR 0039 left open and the one Dirona's historical screen occupies.
- A fresh install with no InfluxDB gets no trends — and says so plainly, on the tile, with the reason. This is a real reduction in out-of-the-box capability relative to building a store, and it was chosen deliberately.
- `queryInfluxDepthTrend` and `queryInfluxPathTrend` now sit side by side. The former keeps `depthTrendPoint`'s shape for `/api/depth-trend` and its nil-on-failure contract, which the tide-turning-point code depends on; the latter returns an error because its caller must distinguish an empty series from a failed query. Folding them together would mean changing the depth contract, which is not worth it here.
- Bilge run-rate, still outstanding from ADR 0039, is now buildable on the same query rather than waiting on a store.
- Trend gauges are the first feature to make InfluxDB load-bearing. If that proves unwelcome on a small boat, an in-memory per-path store can be added behind the same endpoint later — the frontend asks for history and does not care where it comes from.

## Verification

`go test -short ./...` and the frontend suite pass. The backend tests cover the 503 with a reason, the 400s for a missing path and an unknown window, and the injection attempt through `window`. The frontend tests cover a rendered line and a 503 rendering the message rather than an empty chart.

Not yet exercised against a live InfluxDB. The end-to-end check still owed: a trend gauge saying it needs InfluxDB, then drawing a real line once one is configured.
