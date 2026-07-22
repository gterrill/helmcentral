# ADR 0021: Solar State Source Priority And Influx Fallback

## Status
Accepted

## Context
Solar V2 introduces a dedicated `/api/solar-state` endpoint with aggregate and per-controller metrics. SignalK payloads vary by plugin and vessel setup: some installations expose only real-time panel power, while others also expose daily yield and peak values.

We need deterministic behavior when daily metrics are partially missing, without inventing data. The project fallback policy requires explicit source handling and visibility rather than silently masking data-source gaps.

## Decision
1. `/api/solar-state` uses source priority for daily energy and peak metrics:
- Prefer native SignalK values when present.
- Fill only missing fields from InfluxDB when Influx telemetry is enabled and configured.
- Leave fields unavailable (`-1` backend sentinel) when neither source provides data.

2. Real-time solar power priority remains:
- Prefer `electrical.venus.totalPanelPower` when available.
- Otherwise aggregate `electrical.solar.*.panelPower`.

3. `trend_24h_total` behavior:
- Preserve trend values already present on the state object.
- If trend is empty and Influx is enabled, fill from Influx 24h aggregate window samples.

4. Source transparency in API response:
- `signalk` when SignalK data is used and no Influx fill was needed.
- `signalk+influx-fallback` when SignalK is primary and Influx fills missing fields.
- `influx` when SignalK is unavailable and Influx supplies values.
- `backend-fallback` when neither source yields usable data.

## Consequences
Positive:
- Operators get robust daily yield and peak metrics even when SignalK omits those fields.
- Native SignalK data is never overwritten when present.
- API source tagging is explicit, making diagnostics straightforward.

Tradeoffs:
- Influx fallback quality depends on measurement and field mapping (`INFLUX_SOLAR_MEASUREMENT`, `INFLUX_SOLAR_FIELD`).
- Querying additional Influx windows adds backend query load.

## Related
- ADR 0020: In-Memory Telemetry History, Optional InfluxDB
- `backend/main.go` (`solarState`, `applySolarInfluxFallback`)
- `backend/influx.go` (`queryInfluxSolarTodayKWh`, `queryInfluxSolarYesterdayKWh`, `queryInfluxSolarPeakTodayW`, `queryInfluxSolarTrend24h`)
