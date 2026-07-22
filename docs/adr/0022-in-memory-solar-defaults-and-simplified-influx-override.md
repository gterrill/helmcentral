# ADR 0022: In-Memory Solar Defaults And Simplified Influx Override

## Status
Accepted

## Context
ADR-0021 gave `/api/solar-state` a per-field fill-gap cascade: for each of `today_kwh`/`yesterday_kwh`/`peak_today_w`/`trend_24h_total`, InfluxDB was queried only if SignalK left that specific field unset, and a four-value `source` tag (`signalk`/`signalk+influx-fallback`/`influx`/`backend-fallback`) recorded which fields came from where. This made solar the only telemetry read path still hard-dependent on InfluxDB for a full-featured default install, and it duplicated per-field merge/tagging complexity that ADR-0020 had already replaced with a simpler pattern for wind-gust-max and depth-trend: an in-memory default fed by the existing 5s `sampleTracks()` poller, with Influx as a wholesale, configuration-gated override.

Charger card fields (572277c) were confirmed to have zero Influx dependency and needed no changes.

## Decision
1. **`backend/solar_history.go`** adds a day-scoped Riemann-sum accumulator (`solarDayStats`, package var `solarStats`) fed by `sampleTracks()` on every 5s poll tick, giving `today_kwh`/`yesterday_kwh`/`peak_today_w` an Influx-free default. A parallel `solarPowerHistory` ring buffer (reusing `telemetry_history.go`'s `newTelemetryRingBuffer`) feeds `inMemorySolarTrend24h()`, a 15-minute-bucketed 24h mean, copy-adapted from `inMemoryDepthTrend`'s bucketing loop rather than shared, to keep the diff scoped.

2. **Dispatch simplifies to a plain wholesale override**, matching ADR-0020's wind-gust/depth-trend pattern exactly:
   ```go
   state = applyInMemorySolarDefaults(state)
   if influxTelemetryConfigured() {
       state = applyInfluxSolarOverride(state)
   }
   ```
   `applyInMemorySolarDefaults` fills only fields SignalK didn't report; `applyInfluxSolarOverride` — called only when Influx is enabled and configured — wholesale-replaces all four fields, including ones already populated by SignalK or the in-memory tier. It does not fall back to the in-memory value if the Influx query itself fails, per the project Fallback Policy: once Influx is enabled, its failures must be visible (sentinel `-1`/`nil`), not silently patched over.

3. **`applySolarInfluxFallback` and its per-field merge/tagging logic are deleted outright**, along with the old `solarPeakMu`/`solarPeakDay`/`solarPeakTodayW`/`updateSolarPeakToday` per-request peak-tracking machinery — peak is now resolved at poller resolution (every 5s) by `solarDayStats`, not recomputed per HTTP request.

4. **`source` simplifies to exactly two values**, `signalk`/`backend-fallback`, identical semantics to `vesselState()`'s own `source` field: it reflects whether the live SignalK fetch succeeded, not which backend served which telemetry field. This drops ADR-0021's four-value vocabulary entirely.

## Consequences
Positive:
- Zero-dependency default install: solar's `today_kwh`/`yesterday_kwh`/`peak_today_w`/`trend_24h_total` now all work out of the box against SignalK alone, matching wind-gust/depth-trend/tide-detection (ADR-0020).
- One dispatch pattern for all Influx-backed telemetry (wind, depth, solar), rather than a bespoke per-field cascade unique to solar.
- `source` is a single, simple signal again, consistent with every other state endpoint.

Tradeoffs:
- **No cross-midnight integration**: the sample interval spanning the UTC day boundary is dropped from both days rather than split at the boundary — an accepted simplification versus Influx's own boundary-aware query.
- **`solarMaxSampleGap` (30s, ~6x the poll cadence)** caps Riemann-sum integration so a stale interval (server restart, missed ticks) isn't counted as continuous production; the single gapped interval is dropped, not the whole day.
- In-memory solar history resets on every server restart and is capped at ~24h (`solarTrendHistoryCapacity`), same tradeoff ADR-0020 already accepted for wind/depth.
- `sampleTracks()`'s per-tick SignalK HTTP calls double (1 → 2), since `fetchSignalKSolarState` performs its own independent full-payload GET rather than reusing the vessel-state fetch already made in the same tick — a pre-existing pattern (each handler already fetches the whole vessel tree independently), out of scope to fix here.
- Once Influx is enabled, a failing Influx instance surfaces as sentinel data on `/api/solar-state`, not a silent fallback to the in-memory tier — this is intentional (Fallback Policy), but is a behavior change from ADR-0021's per-field cascade, which could partially fall back per field.

## Related
- ADR 0020: In-Memory Telemetry History, Optional InfluxDB
- ADR 0021: Solar State Source Priority And Influx Fallback (superseded by this ADR)
- `backend/solar_history.go`, `backend/main.go` (`solarState`, `applyInMemorySolarDefaults`, `applyInfluxSolarOverride`), `backend/tracks.go` (`sampleTracks`)
