# ADR 0044: Nearby Vessel Encounter Confirmation

## Status
Accepted

Extends ADR 0042 (nearby-vessel staleness filtering): that ADR stopped ghosts from lingering in the tile, but left `recordContactIfNew` (`backend/nearby_contacts.go`) free to write a sighting-history row from a single optimistic tick. This ADR closes that gap with a confirmation dwell, and separately widens the move threshold ADR 0042 didn't touch.

## Context

Osprey IV showed two sighting-history entries an hour apart (2026-08-20 04:28:23 and 05:28:39 UTC) for what was a single visit to our anchorage. Reading the live database (`backend/data/nearby-contacts.sqlite`) turned up two independent defects in `recordContactIfNew`.

**Defect 1 - ring-graze blips become sightings.** `fetchSignalKNearbyVessels` (`backend/signalk.go`) hard-cuts the contact set at `nearbyMaxRangeMeters = 5000.0`. Osprey IV was anchored at Keswick 5179 m from our position - 179 m outside the ring. Its neighbours were too (Rummage 5169 m, Moonlight 5107 m). Our anchor swing plus theirs plus GPS noise makes that computed range dip under 5000 m occasionally, and `recordContactIfNew` inserted on the very first tick a vessel looked to be inside the ring, with no confirmation. All three boats got a row from that ring - Moonlight 02:33, Rummage 03:44, Osprey IV 04:28 - none of which represent a real visit. Osprey IV then genuinely motored over at 05:28, and because it had been absent over an hour and moved 5186 m from the blip position, that arrival became a second row.

ADR 0042's 10-minute `nearbyVesselMaxAge` window makes this worse: once one optimistic fix puts a vessel inside the ring, its frozen position keeps it looking present for up to 10 more minutes until a fresh report arrives. Wall-clock presence in the contact set is therefore not proof of actual presence.

**Defect 2 - the 100 m move threshold is far tighter than real anchor swing.** Bucketing every consecutive row pair per vessel across the whole database:

| gap | target moved | pairs |
|---|---|---|
| 1h-24h | 100-600 m | 68 |
| 1h-24h | <=100 m | 3 |
| 1h-24h | >600 m | 4 |
| >24h | any | 334 |

The 1h-24h band is strictly bimodal: every pair is either <=555 m or >=5117 m - nothing in between. The 68 pairs in the low group are one boat that never left, re-recorded because anchor swing carried it past `contactSessionMoveThresholdMeters = 100.0` during an AIS dropout. The high group is genuine relocations (Osprey IV and Rummage moving anchorages).

Outcome wanted: one row per real visit. A boat parked beyond the detection ring should produce no row at all, and a boat swinging at anchor through a dropout should not produce a second one.

## Decision

### 1. Require a confirmed dwell before the first insert

`recordContactIfNew` no longer inserts a row the moment a contact looks like a new encounter. Instead it becomes (or continues) a `pendingContact` candidate, tracked in memory alongside `lastSeen`, and is only turned into a row once it has been continuously in range for `contactConfirmDwell` **and** its AIS position has been refreshed at least once during that window.

Two new constants in `nearby_contacts.go`, documented in the same style as the pre-existing three:

- `contactConfirmDwell = 5 * time.Minute` - longer than the ~3 minute stationary Class A/B AIS report interval already cited by `nearbyVesselMaxAge`'s comment (ADR 0042), so a single optimistic fix cannot carry a candidate to confirmation on dwell time alone.
- `contactConfirmMaxTickGap = 15 * time.Second` - 3x the 5-second server poll interval (`main.go`). A wider gap between ticks means the vessel actually dropped out of the contact set and later reappeared, so the dwell restarts from the current tick rather than resuming across the gap.

`pendingContact` holds `firstSeenAt` (the candidacy's start, backdated into the row on confirmation), `lastTickAt` (continuity check against `contactConfirmMaxTickGap`), `positionSeen` (the AIS receive time recorded at dwell start, held fixed for the candidate's lifetime), and the `lat`/`lon`/`geoname`/`navContext` observed at that same first tick.

On each tick, `recordContactIfNew` keeps its existing `isNewEncounter` computation unchanged. If it's not a new encounter, any pending candidate is dropped and `lastSeen` advances as before. If it is a new encounter: a fresh candidate starts if none exists or the previous one's last tick is further back than `contactConfirmMaxTickGap`; otherwise the existing candidate continues. If the candidate hasn't yet held for `contactConfirmDwell`, or its AIS position hasn't changed since the candidacy started, the tick is absorbed into the pending state and `lastSeen` is deliberately **not** advanced - that's what keeps the same "looks new" branch evaluating on every subsequent tick instead of the continuation branch taking over. Once both conditions clear, the candidate is deleted and a row is inserted using the *candidate's* `firstSeenAt`/`lat`/`lon`/`geoname`/`navContext` - the arrival moment, not whichever tick happened to cross the dwell threshold - and only then does `lastSeen` advance, to the confirming tick's own position.

A freshly-created candidate is never judged stale on the tick that creates it, since there is nothing yet to compare its `positionSeen` against. This is what lets a store configured with zero dwell (the test helper's default, preserving the pre-existing test suite's one-call-one-row semantics) confirm on the very first tick, exactly as before this change.

**Known transient side effect, accepted rather than special-cased:** `summary` (`backend/nearby_contacts.go`) drops a vessel's newest row on the "this is the ongoing encounter" contract. During the dwell window a returning vessel is genuinely visible but has no row yet, so its `seen_count` reads one low for up to 5 minutes, then self-corrects the moment the candidate confirms. `summary`'s doc comment now records this rather than changing its contract.

**Plumbing `positionSeen`.** `fetchSignalKNearbyVessels` already computed this receive time (per ADR 0042) and discarded it after deriving `AgeSeconds`. `nearbyVessel` (`backend/main.go`) gained a `PositionSeen time.Time` field tagged `json:"-"`, keeping the wire format and the SSE payload byte-identical, populated in `signalk.go` and threaded through `recordNearbyVesselContacts` (`backend/tracks.go`) into `recordContactIfNew`.

### 2. Widen the move threshold from 100 m to 750 m

`contactSessionMoveThresholdMeters` moves from `100.0` to `750.0`. The measured distribution above is the justification: the largest observed non-relocation excursion in this database is 555 m, the smallest genuine relocation is 5117 m, so 750 m sits inside a ~4.5 km empty band and is not a finely-tuned number - it is comfortably clear of both edges. `contactSessionGap` and `contactSessionMaxGapForPositionOverride` are unchanged.

## Consequences

- A vessel that only ever grazes the detection ring on optimistic fixes - never confirmed by a sustained, position-refreshed presence - produces no sighting-history row at all, closing the defect that produced Osprey IV's spurious 04:28 row.
- A vessel that has genuinely arrived is recorded once, backdated to its actual arrival tick, once `contactConfirmDwell` has elapsed with a refreshed AIS position - not on whichever tick happens to cross the threshold.
- A stationary vessel swinging at anchor through an AIS dropout up to 750 m from its last recorded position no longer fragments into a second row, matching the measured bimodal distribution.
- `summary`'s `seen_count` undercounts by one for up to `contactConfirmDwell` while a returning vessel's candidacy is pending; this is a narrow, self-healing window and is not corrected here (see `summary`'s doc comment).
- The existing sighting-history database is **not** backfilled or cleaned by this change - previously-recorded spurious rows (including Osprey IV's) remain; only new recordings improve. A related but separate defect - duplicate rows and a corrupt index consistent with two backend processes sharing one SQLite file - was found during diagnosis and is out of scope here.
- The popover's place name and nav status describing **us** rather than the sighted vessel (`tracks.go` passing `state.Latitude`/`state.Status` for self, rendered by `nearby-vessels-tile.tsx` as if describing the target) is a separate, pre-existing defect, also out of scope here.

## Verification

1. `cd backend && go test ./... -run 'NearbyContact|RecordContactIfNew|Sighting|Summary' -v` - new tests in `nearby_contacts_test.go` cover the ring-graze regression (using the real Osprey IV timestamps), dwell confirmation and its backdating, dwell-not-yet-elapsed, dwell interrupted by a tick gap, a position that never refreshes, and the 750 m move-threshold boundary.
2. `cd backend && go build ./... && go vet ./...`.
3. `cd frontend && npm test -- nearby-vessels-tile use-vessel-sightings` - the wire format is unchanged (`PositionSeen` is `json:"-"`), so these pass without frontend edits.
4. Live check against the real SignalK feed: run the backend, confirm the tile still lists nearby vessels, and confirm no new sighting-history row appears for a vessel within the first 5 minutes of it entering range:
   `sqlite3 backend/data/nearby-contacts.sqlite "select datetime(seen_at,'unixepoch'), name from nearby_vessel_contacts order by seen_at desc limit 5;"`
