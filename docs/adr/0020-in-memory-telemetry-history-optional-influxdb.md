# ADR 0020: In-Memory Telemetry History, Optional InfluxDB

## Status
Accepted

## Context
InfluxDB previously backed three read paths in the backend: wind-gust max (`queryInfluxMaxWindGustKts`), depth-trend/tide detection (`queryInfluxDepthTrend`), and motoring-trail startup seeding (`seedMotoringTrailFromInflux`, ADR-0003). All three treated InfluxDB as a hard prerequisite — without it configured, wind-gust and depth-trend silently returned empty/zero data, and a fresh install required standing up InfluxDB plus the `signalk-to-influxdb-v2` plugin before the dashboard was fully useful.

The motoring-trail seed was confirmed safe to remove outright: the motoring ring buffer already degrades gracefully to empty, and live sampling (`sampleTracks`, ADR-0001) is fully independent of the seed.

For wind-gust and depth-trend, the 5s server-owned poller (`sampleTracks`) already fetches `vesselStateData` with `Depth` and `WindSpeedApparentKts` on every tick — a natural feed point for an in-memory alternative, requiring no new polling.

`AGENTS.md`'s Fallback Policy prohibits masking upstream problems with silent fallback behavior: "do not add graceful fallback behavior that masks upstream data/source problems... add fallback only when explicitly requested, gated behind a flag with explicit logs." This means Influx vs. in-memory selection must branch on **configuration state** (is Influx enabled and fully configured), never on query error or emptiness — if an operator enables Influx and it starts failing, that must surface as before (sentinel `-1`/`nil`), not silently fall back to in-memory and mask the problem.

## Decision
1. **Remove motoring-trail Influx seeding entirely.** `seedMotoringTrailFromInflux` and its now-unreachable helpers (`queryInfluxLastMotoringToStationaryTransition`, `queryInfluxMotoringTrailDownsampled`, plus already-dead code `queryInfluxLastStationaryStart`, `queryInfluxPositionTrailRange`, `queryInfluxDepthTrendSince`, `isStationaryNavState`, `isMotoringNavState`) are deleted from `backend/influx.go` and `backend/tracks.go`. The motoring trail now starts empty and fills purely from live sampling — ADR-0003 is superseded by this ADR.

2. **In-memory telemetry history is the new default** for wind-gust-max and depth-trend/tide-detection. `backend/telemetry_history.go` adds a scalar ring buffer (`telemetryRingBuffer`, mirroring `anchor.go`'s `vesselTrail` pattern) storing `{Value, Timestamp}` pairs, with two package-level instances (`windGustHistory`, `depthHistory`) each capped at `telemetryHistoryCapacity` (4320 samples, ~6h at the 5s poll cadence). `sampleTracks()` records into both on every tick, gated only on `err == nil` and per-field sentinel checks — deliberately *not* nested inside the existing position-validity gate, since wind/depth are not position-dependent and Influx never required a GPS fix to record them either. `inMemoryMaxWindGustKts(window)` and `inMemoryDepthTrend(window)` mirror the exact output contracts of `queryInfluxMaxWindGustKts`/`queryInfluxDepthTrend` (same sentinels, same `depthTrendPoint` shape), so `findLastTideTurningPoint` and the HTTP response shapes are unchanged regardless of source.

3. **InfluxDB becomes an opt-in enhancement**, configured from the Settings UI rather than env vars alone. A new `influxdb` section in `settings.yaml` (`enabled`, `url`, `org`, `bucket`) is added to `settingsPayload` and round-tripped through `GET`/`POST /api/settings` the same way `anchor`/`boat` already are. `INFLUXDB_TOKEN` remains the only env var, since it's a secret that should never round-trip through the settings API. `loadInfluxSettings` reads the four values (three from settings.yaml, one from env) and reports `ok` only when `enabled: true` and all four are non-empty; `newInfluxClient` is rewritten to source from `loadInfluxSettings` instead of `INFLUXDB_URL`/`INFLUXDB_ORG`/`INFLUXDB_BUCKET` env vars, which are removed rather than kept as a parallel fallback path.

4. **Dispatch branches on configuration state only**, per the Fallback Policy reasoning above:

   ```go
   points := inMemoryDepthTrend(window)
   if influxTelemetryConfigured() {
       points = queryInfluxDepthTrend(window)
   }
   ```

   The equivalent pattern applies to `vesselState()`'s wind-gust computation. If Influx is enabled and configured but a query fails or returns no data, the handler surfaces Influx's own sentinel (`-1`/`nil`) rather than silently falling back to the in-memory buffer — a failing Influx instance should be visible as a problem, not masked.

## Consequences
Positive:
- Zero-dependency default install: wind-gust-max, depth-trend, and tide detection all work out of the box against SignalK alone, no InfluxDB or `signalk-to-influxdb-v2` plugin required.
- Motoring-trail startup seeding removes ~200 lines of now-dead-adjacent Influx query code with no behavioral loss (live sampling already fully populates the trail within one poll cycle).
- InfluxDB remains available for operators who want it, now configured consistently through the same Settings UI as every other provider/section, rather than as bespoke env vars.
- Wind-gust and depth-trend share identical output types (`depthTrendPoint`, float64 sentinel) across both code paths, so `findLastTideTurningPoint` and the frontend (`use-vessel-state`, `use-depth-trend`) need zero changes.

Negative:
- In-memory history resets on every server restart, and is capped at ~6h (`telemetryHistoryCapacity`) versus InfluxDB's arbitrary retention — an operator who wants tide/wind history spanning days must still enable InfluxDB.
- Two parallel code paths now exist for wind-gust and depth-trend (Influx vs. in-memory), doubling the surface area for those two read paths, though both share the same output contract and the dispatch itself is a small, explicit branch.

## Related
- ADR 0001: Server-Owned Trail Sampling
- ADR 0003: Downsample Influx Seed For Motoring History (superseded by this ADR)
