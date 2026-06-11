# ADR 0005: SignalK/Tracks-Backed AIS Trail History

## Status
Accepted

## Context
AIS vessel trails for the anchor-watch map were previously accumulated in server-side in-memory ring buffers inside Helmcentral. The backend polled SignalK for nearby vessel positions every 5 seconds and appended them to per-vessel `vesselTrail` ring buffers keyed by vessel name. The frontend `useAisTrails` hook would also accumulate trail points client-side from the live `useNearbyVessels` feed as a secondary source.

Problems with this approach:
- Trail history was reset on every server restart, leaving the anchor-watch map blank until enough new positions accumulated.
- Two separate accumulators (server ring-buffer and client hook) could diverge or duplicate points.
- The backend polling loop for AIS was duplicating work already done by the SignalK `@signalk/tracks` plugin, which the operator installed and which already maintains timestamped position history per vessel with configurable retention.

## Decision
Replace the backend AIS ring-buffer accumulator with a read-through adapter that fetches the current track snapshot from the SignalK `@signalk/tracks` plugin API on each trail request.

Specifically:
- `backend/tracks.go` now calls `GET /signalk/v1/api/tracks` (configurable via `SIGNALK_TRACKS_PATH`) on every `/api/tracks` and `/api/anchor-watch/trails/ais*` response cycle.
- The tracks plugin returns `MultiLineString` GeoJSON keyed by vessel context (`vessels.<id>`). The adapter resolves each context key to a display name using a secondary call to the SignalK vessels endpoint, then maps coordinates from [lon, lat] order to Helmcentral's `{lat, lon, timestamp}` wire shape.
- AIS trails continue to be keyed by vessel name for compatibility with the existing frontend contract.
- The frontend `useServerTrails` hook now treats the AIS payload as a full replacement snapshot per poll rather than appending incrementally, since history management is delegated to the plugin.
- The legacy client-side `useAisTrails` hook (which appended trail points from live nearby-vessel polling) is now dead code with no callers; the `useServerTrails` hook is the sole AIS trail consumer.

The motoring approach trail and the self post-anchor ring-buffer remain on the existing Helmcentral-owned paths (Influx-seeded + live sampling), because those serve a distinct purpose (anchor reposition context) that is unrelated to AIS history.

## Consequences
Positive:
- AIS trail history survives server restarts; the plugin retains history across Helmcentral restarts.
- Single source of truth for AIS trails: the SignalK plugin, not two parallel accumulators.
- Removed ~120 lines of ring-buffer recording code, 3 now-unused accessor functions, and the per-poll AIS sampling loop.
- Adapter is covered by 5 unit tests (`TestFetchSignalKAISTrails_*`).

Negative:
- Each trail request now incurs 2–3 outbound HTTP calls to SignalK (tracks, vessels, vessels/self). These are bounded to 4-second timeouts; latency is acceptable for a 5-second client poll cycle.
- The `since` incremental-update contract is no longer applied to AIS trails: every poll returns the full retained snapshot from the plugin. This is a semantic change but harmless because the frontend now replaces rather than appends, and the plugin's sliding-window retention is the effective history window.
- If the `@signalk/tracks` plugin is not installed or is stopped, AIS trails will return empty rather than stale ring-buffer data. This is consistent with the repo's fail-fast policy (see AGENTS.md).

## Related
- ADR 0001: Server-Owned Trail Sampling
- ADR 0002: Separate Motoring and Anchor Trails
