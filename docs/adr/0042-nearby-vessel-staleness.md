# ADR 0042: Nearby Vessel Staleness Filtering

## Status
Accepted

Works around a gap left open by ADR 0037 (delta-stream ingestion): the snapshot never evicts contexts. This ADR filters at read time rather than closing that gap - see Consequences below.

Extended by ADR 0044 (nearby-vessel encounter confirmation): the 10-minute staleness window this ADR introduces can keep a single optimistic fix looking "present" long enough for `recordContactIfNew` to record a spurious sighting; ADR 0044's confirmation dwell, and its position-refresh requirement in particular, closes that interaction.

## Context

The Nearby Vessels tile showed contacts that had left hours ago. In a reported screenshot, `VIVA` at 12 m and `GREY GOOSE` at 28 m sat at the top of the list while `232060792` read `(1211m ago)` - 20 hours stale - and `AURORA` read `(2648m ago)`, 44 hours stale. Every one of them rendered as a current target at a frozen position.

Two independent defects produced this:

1. **The snapshot never expires anything.** `signalKSnapshot.applyDelta` (`backend/signalk_snapshot.go`) only inserts and merges; nothing in the backend ever deletes a context. A vessel that transmitted AIS once and sailed away stays in `contexts` for the life of the process with its last position and SOG frozen.
2. **Nothing downstream filtered on age.** `fetchSignalKNearbyVessels` (`backend/signalk.go`) filtered only on position sanity, excluded name, and range, then sorted by range ascending and truncated to 10. `age_seconds` was computed and shipped to the client but was never used to drop, sort, or de-emphasise anything.

Because the list is sorted by distance and capped at 10, frozen ghosts near the anchorage actively **crowded out live targets**. The same array is also passed to the anchor-watch map as `aisVessels`, so ghost contacts were drawn there too.

There was also a **third, quieter bug** the same fix resolves. `recordNearbyVesselContacts` (`backend/tracks.go`) calls `fetchSignalKNearbyVessels` on every 5s poll tick and feeds each result to `recordContactIfNew`. For a ghost, that call refreshed the in-memory `lastSeen[key]` on every tick with a position that never moved, so `contactSessionGap` (1h) never elapsed and `contactSessionMoveThresholdMeters` (100m) was never crossed. The encounter never closed, so when that boat genuinely returned days later no new sighting row was written and `seen_count` never incremented. Relatedly, `nearbyContactStore.summary` (`backend/nearby_contacts.go`) documents a hard contract - "this assumes vesselKey is currently visible" - that ghosts silently violated. Filtering restores that guarantee rather than weakening it: `summary`'s only caller is the `/api/nearby-vessels` handler, iterating vessels it just received from SignalK, and that iteration is now guaranteed to be live traffic.

Two adjacent defects in the same data path were folded into this change, since they touch the same struct and the same two files:

- **Vessel identity was the display name.** Names are not unique - two boats can share one, and unnamed vessels all fall back to the same `compactVesselID` shape. The frontend keyed React lists on `vessel.name` and matched the selected AIS marker with `transient?.label === vessel.name`, so clicking one vessel visibly highlighted *every* vessel sharing its name. MMSI is closer to real identity but is `omitempty` on the wire, so the frontend had no guaranteed-present unique field.
- **Range round-tripped through feet.** The backend computed `haversineMeters`, rounded to whole feet, and shipped `range_ft`; the tile then divided by 3.28084 and rounded again to render metres. Metric users got a double-rounded number, and the cutoffs (`30`, `16404`) were unreadable magic constants whose own comment had to translate them back to metric.

## Decision

### 1. Drop AIS contacts older than 10 minutes, server-side

`nearbyVesselMaxAge = 10 * time.Minute` in `fetchSignalKNearbyVessels`. AIS Class A transmits at least every 3 minutes when stationary, and Class B does too; 10 minutes is roughly 3 missed reports - enough headroom never to drop a genuinely anchored neighbour overnight, tight enough that ghosts clear quickly.

The filter runs server-side, inside `fetchSignalKNearbyVessels`, so the tile, the anchor-watch map, and the sighting-history poller (`recordNearbyVesselContacts`) are all fixed at one choke point rather than three.

It is applied **after the range check and before the sort/top-10 truncation** (`backend/signalk.go`). Sorting by range and capping at 10 is exactly the mechanism that let close ghosts crowd out a live, more distant target, so the filter has to run before the cap, not after.

### 2. Age comes from delta receive time, not the AIS-reported timestamp

`fetchSignalKNearbyVessels` derives both the filter and the shipped `age_seconds` from `globalSignalKSnapshot.lastSeen(vesselContextPrefix+vesselID, "navigation.position")`, i.e. when the backend's own delta stream last saw a position update for that context - not `navigation.position.timestamp`, the time value AIS itself claims.

Receive time is always present for any vessel that exists in the snapshot at all (a context only exists because a delta created it), is immune to transmitter clock skew, and resets cleanly with the process. Deriving the shipped `age_seconds` from the same clock that drives the filter also guarantees the number on screen agrees with the filter that produced the row - there is exactly one age, not two that can disagree.

A vessel context with no recorded `navigation.position` delta at all (`lastSeen` returns the zero `time.Time`) is dropped outright: there is nothing to age against, and the position sitting in the tree cannot be trusted.

**This removes a masking fallback the fallback policy (`AGENTS.md`) forbids.** The previous code parsed `navigation.position.timestamp` with `time.Parse(time.RFC3339, ...)` and, when the field was missing or failed to parse, silently left `ageSeconds` at `0` - reporting a dead target as "0s ago" forever, and a value any age-based filter would have been permanently blind to. There is no compatibility path retained for the old timestamp field; it is not consulted at all anymore.

### 3. A new, always-present `id` field replaces name as list-reconciliation identity

`nearbyVessel.ID` (`backend/main.go`) is populated from the bare SignalK vessel id - the same key `fetchSignalKNearbyVessels` already reads off `vesselsTree()`, unique and non-empty by construction. It has no `omitempty`: the field is the identity and must always be present.

Frontend: `nearby-vessels-tile.tsx` keys its list on `vessel.id`, and `anchor-watch-map.tsx` does the same for its markers. `TransientInfo` gained an optional `vesselId` field so marker selection compares `transient?.vesselId === vessel.id` instead of `transient?.label === vessel.name` - `label` still carries display text and the `'pin'` sentinel for a plain map-click pin, unchanged.

`vesselContactKey` (`nearby-vessels-tile.tsx`) and the backend's own vessel-contact keying (`backend/nearby_contacts.go`) stay MMSI-based and are untouched: sighting history is deliberately keyed on MMSI, which is a different question from React reconciliation identity and remains so.

### 4. `range_ft` is replaced outright by `range_m`, rounded to 0.1 m

`nearbyVessel.RangeFt int` becomes `RangeM float64`, one canonical field rather than two - there is no `range_ft` left on the wire. `fetchSignalKNearbyVessels` computes `math.Round(haversineMeters(...)*10) / 10` and the cutoffs are named constants in metres: `nearbyMinRangeMeters = 9.144` (the 30 ft self-exclusion threshold this replaces) and `nearbyMaxRangeMeters = 5000.0`. The frontend's `formatRange` converts once, in the one direction the display needs - to feet, only when `distanceUnits === 'imperial'`.

**The 0.1 m rounding is required, not cosmetic.** `vessel_state_stream.go`'s SSE emitter change-gates each event on whether the serialised JSON differs from the previous tick. An unrounded `float64` range would differ by some small amount on essentially every tick as GPS noise moves the vessel a few centimetres, defeating that gating and turning a per-anchor-cycle emitter into a per-tick one. 0.1 m is also *finer* than the old whole-foot resolution, so no display precision is lost by rounding.

The boundary shifts by well under a metre versus the old whole-feet comparison, which effectively kept ~8.99 m-5000.15 m. That is immaterial for a self-exclusion distance and a 5 km horizon, so the constants were chosen to read cleanly in metres rather than contorted to reproduce the old feet boundary exactly.

## Consequences

- Ghost AIS contacts disappear from the tile and the anchor-watch map within `nearbyVesselMaxAge` of their last real position update, freeing the top-10 slots the range sort had let them squat in.
- `recordNearbyVesselContacts`'s session-gap/move-threshold logic now only ever sees live traffic, so an encounter with a ghost can no longer be kept artificially open by a vessel that has actually left.
- The wire contract changes: `range_ft` is gone, `range_m` and `id` are new required fields. `frontend/src/hooks/use-nearby-vessels.ts` validates both before accepting an item, matching its existing per-item validation style.
- `frontend/src/hooks/use-ais-trails.ts` still keys on `vessel.name` and was deliberately left alone - it is exported but never imported anywhere, and is out of scope here as dead code, not as an overlooked consumer.
- Context eviction in `signalKSnapshot` is still not implemented. Ghost contexts keep accumulating in memory for the life of the process; this ADR filters at read time rather than pruning. A future change could evict whole contexts, which would also help `fetchSignalKVesselNameMap` and bound memory on a long-running server, but whole-context eviction risks discarding static AIS data (name, MMSI, dimensions) that is only rebroadcast every ~6 minutes, so it needs its own design and is not attempted here.
- The cap of 10 vessels in `fetchSignalKNearbyVessels` is unchanged.

## Verification

1. `cd backend && go build ./... && go test ./...` - the new tests in `signalk_test.go` (`TestFetchSignalKNearbyVessels_DropsStaleVessels`, `_KeepsVesselAtCutoffBoundary`, `_DropsVesselWithNoPositionDelta`, `_AgeSecondsFromReceiveTime`, `_StaleFilterAppliedBeforeTopTenCap`, `_ReportsRangeInMeters`, `_PopulatesStableID`) plus the existing `nearby_contacts_test.go`, `signalk_snapshot_test.go`, and `vessel_state_stream_test.go` suites.
2. `cd frontend && npx vitest run` - `nearby-vessels-tile.test.tsx` and `anchor-watch-map-ui.test.tsx`, including new cases for duplicate-name rendering with no key warning and single-marker selection when two AIS vessels share a name.
3. `cd frontend && npx tsc --noEmit` - the `range_ft` → `range_m` rename and the added `id` field are both type-level breaking changes; this is what proves every consumer was found and updated.
4. Live check against a real SignalK server: every remaining row in the tile stays under `(10m ago)`, no multi-hour entries appear, and previously crowded-out live targets become visible. `GET /api/nearby-vessels` reports `source: "signalk"`, every entry has a non-empty `id`, `range_m` present with `range_ft` gone, and no `age_seconds` above 600.
