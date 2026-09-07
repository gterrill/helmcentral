# ADR 0083: Ages Ride the Gauge-Values Stream

## Status
Accepted

Extends ADR 0068's staleness contract to every path bound through
`gauge-values`. Shipped alongside ADR 0080, 0081 and 0082, from the same
evaluation of MV Dirona's N2KView screen.

## Context

ADR 0068 gave the Solar and Battery & Power tiles an age, computed backend
side against the vessel clock, so a frozen `0 W` could be told apart from a
genuinely idle array. That ADR named the gap plainly: coverage stopped at
those two tiles, and everything else bound through `gauge-values` (every
configurable gauge, gauge group, engine cluster and lamp, plus the pinned
ribbon ADR 0082 just added) had no age at all. A frozen SignalK value rendered
there exactly as a live one, for as long as the source stayed silent.

That gap stopped being theoretical during this evaluation. On the morning of
2026-09-08 both `propulsion.{port,starboard}.fuel.rate` carried timestamps
about 20 hours old, from before the engines were last shut down, and the
Cluster preview page's two engine clusters were still drawing 698 RPM from
that same stale data with the engines cold. The Economy tile, whose figure is
computed from the fuel rate and the boat's speed, read 0.02 nm/L. Not wrong
arithmetic, a number honestly computed from a SignalK path that had stopped
updating almost a day earlier.

## Decision

### 1. Ages beside values, same event, same tick

`buildGaugeValuesPayload` gains an `"ages"` map next to `"values"`, one entry
per path a gauge is bound to, computed against a single `time.Now().UTC()`
taken once per build so every path in the payload is judged against the same
instant. The frontend gets a sibling hook, `useGaugeAges()`, subscribing to
the same `gauge-values` event `useGaugeValues()` already reads. No new
stream, no new subscription protocol. The same reasoning ADR 0052 gave
`gaugeBoundPaths()` for lamps applies again here.

### 2. The newer of two clocks, and what a restart does to them

For a snapshot path, the age is the fresher of two readings: the alarm
engine's own arrival-time record (`pathSeen`, the host clock's timestamp when
the delta landed) and the newest SignalK `timestamp` string anywhere on that
path's node, both already available (`alarmSample.LastSeen` and
`freshestTimestampAge`). Taking the fresher of the two matters because they
are different clocks. The host's arrival time and the source's own declared
time can disagree by a little even on a healthy feed, and a path is only as
stale as its most current evidence says.

The plan behind this change assumed the node timestamp would also be what
"survives a backend restart" if a frozen source never sent again. That is not
true of this codebase as it stands. `globalSignalKSnapshot` is populated
exclusively by `applyDelta`, called only from the live WebSocket stream
(`signalk_stream.go`), and `applyDelta` sets a path's tree node and its
`pathSeen` entry in the same call, from the same delta. There is no REST-tree
seeding anywhere; nothing calls `GET /signalk/v1/api/vessels/self` to prime
the snapshot at startup. A restart empties both records together, and a path
whose source never sends again after that reads `-1` from both sources
identically, forever.

There is a second, more operationally relevant wrinkle the live check turned
up. `signalk_stream.go`'s own comment already notes that "SignalK replays its
whole model as the initial state dump on every subscribe." That dump refreshes
`pathSeen` for every already-known path the moment the connection
(re)subscribes, whether that is a full backend restart or an ordinary network
drop, and this backend's stream reconnects routinely (every ten to thirty
minutes against this vessel's link, going by its own logs) rather than only
on rare failures. A path whose source genuinely stopped days ago gets its
`pathSeen` reset to "now" on every one of those reconnects, and reads as fresh
for the two minutes it takes the threshold to re-elapse. The screenshot below
caught this directly: an `air` rebuild mid-development restarted the backend
and forced a resubscribe, and the Cluster preview page briefly reported
"Stale 16m" against a fuel-rate feed that had in fact been silent for about a
day, because sixteen minutes was how long it had been since that restart's
resubscribe, not since the feed last said anything real. The marker still
correctly read stale at sixteen minutes past the two-minute threshold; a
narrower window right after a reconnect is where it would not have.

The dual-source computation still earns its keep for the ordinary clock-skew
case above. It is not the restart-survival mechanism the plan expected, and a
resubscribe (restart or reconnect alike) buys a dead path a brief, bounded
window of looking fresher than it is. This ADR says so rather than leaving
either assumption uncorrected.

### 3. A derived path carries its oldest input's age

`derivedPathValues()` gains a sibling, `derivedPathAges()`, computed in the
same pass (`computeDerivedPaths`) so a build of the payload does not walk the
ring buffers and the snapshot twice for the same numbers.

- Vessel fuel economy's age is the oldest of the SOG sample's age and every
  burning engine's rate-path age, not the newest. A figure is only as fresh
  as its stalest contributing input, which is exactly the frozen-fuel-rate
  case above: SOG was current, one engine's rate path was 20 hours stale, and
  the economy figure it produced is 20 hours stale with it.
- The barometric and squash-zone trends read their age off the newest sample
  in their own ring-buffer window, not the oldest. A slope covers the whole
  three-hour window, but it is only as fresh as the newest point still
  arriving into it. A buffer that stopped receiving samples 30 minutes ago
  makes the slope 30 minutes stale even though every point behind it still
  carries a real timestamp from inside the window. squashZoneIndex depends on
  three such buffers (pressure, wind speed, wind direction), so its age is
  the oldest of the three individual newest-sample ages.
- An empty buffer, or a path with no defined value right now, reports `-1`,
  the same "absence, not a guess" rule this repo applies everywhere else.

### 4. Groups and clusters mark the one reading that froze

`reading()` in `lib/cluster-readings.ts`, the shared conversion both
`GaugeGroupTile` and `EngineClusterTile` already used, takes an optional
`ages` map and returns `stale` and `age` alongside the existing fields. When
a slot's age is stale, its raw value is blanked to `null` before conversion,
so `text`, `converted` and `zone` all read exactly as they do for a path that
has never reported at all. Every caller downstream (the dash rendering, the
zone bar, `worstZoneState`, a telltale's grey) already knows how to handle an
absent reading, so staleness needed no separate branch in any of them.

A stale member additionally carries a small badge beside its label (the same
`text-[10px]` amber-outlined treatment `Tile`'s own badge uses) and
`grayscale` on its own readout, rather than the whole tile taking the
treatment. The tile itself only goes stale, and only then drops its state
dot, once every member whose age is actually known has frozen. A group or
cluster with no known ages at all is neither fresh nor stale, and a single
dead sensor among several live ones marks itself without dragging the rest
down.

### 5. A stale lamp is unlit, not a frozen "on"

`LampStripTile` gains a fourth lamp state, `stale`, checked before the
existing on/off/no-data logic. A lamp whose path has frozen renders in the
same unlit style `no data` already uses and gets `data-state="stale"` plus an
accessible label carrying the age (`"GEN: stale 5m"`), rather than staying lit
on the last value it was ever given. A generator that shut down twenty
minutes ago must not still show green because the last report it sent
happened to say "running." A frozen source left lit is a worse failure than
a frozen source left dark, because a lit lamp asserts a fact rather than
merely failing to update one. The CHK lamp is unaffected: it reads the alarm
rollup, not a bound path, so it has no age of its own to go stale on.

### 6. `-1` and `null` stay unknown, never stale

Every new code path follows the rule ADR 0068 already set: `-1` on the wire,
`null` in the browser (`ageFromPayload`), and `isStale(null)` is `false`. A
path that has never reported carries no evidence either way, and flagging it
as stale would teach the operator to ignore the marker the first time it hit
a path that was simply never going to have an age at all.

## Consequences

- Every widget bound through `gauge-values`, not just Solar and Battery &
  Power, now tells a frozen reading from a live one, using the mechanism ADR
  0068 built and never had to be redesigned to reach the rest of the board.
- The dual-clock age computation (pathSeen vs. node timestamp) buys less than
  the plan assumed. It does not survive a backend restart in this codebase,
  because nothing seeds the snapshot from the REST tree, and a resubscribe of
  any kind (restart or a plain network reconnect, which this backend does
  every ten to thirty minutes in ordinary operation) briefly refreshes
  `pathSeen` for every path, dead ones included, until the two-minute
  threshold re-elapses. It remains useful for ordinary clock skew between the
  host's arrival time and a source's declared timestamp, which is a real if
  smaller thing to guard against.
- `computeDerivedPaths` is now the one place that computes a derived value and
  its age together. `derivedPathValues()` and `derivedPathAges()` are thin
  wrappers around it, kept so existing tests and callers that only want one
  of the two are undisturbed.
- A gauge group or engine cluster with one dead sensor among several reads
  correctly at a glance: the one bad reading is marked, the rest keep
  reporting, and the tile edge does not fall silent just because one part of
  it did.

## Verification

Backend: `go test -short ./...`, covering the payload carrying `ages`
alongside `values`; a path seen a known number of seconds ago reporting
approximately that age from `pathSeen`; a bound path never seen and with no
node timestamp reporting `-1`; a node carrying only a SignalK timestamp with
no `pathSeen` entry (the shape a REST-seeded tree would have, built with the
same `seedSelfTree` fixture idiom `signalk_payload_test.go` already uses)
reporting the age from that timestamp; a derived fuel-economy path reporting
its oldest contributing input's age in both directions (a stale engine
against a fresh SOG, and the reverse); the barometric trends reporting the
newest sample in their ring-buffer window rather than `-1` once there is
history, and `-1` with none; squashZoneIndex reporting the oldest of its
three inputs. 1129 to 1140 tests passing.

Frontend: `npx vitest run`, `npx tsc --noEmit`, `npm run lint`, covering
`useGaugeAges` against a mocked `subscribeTelemetry` (including the `-1` to
`null` mapping); a stale gauge tile showing the dash, the stale badge, and no
zone; a stale gauge-group member showing its own badge without staling the
tile, and the tile going stale only once every known-age member has; a stale
engine-cluster corner row showing the dash and its badge without staling the
tile, the ring blanking when its own path is stale, and the tile going stale
only once every known-age slot has; a lamp with a stale path reading unlit
with `data-state="stale"` and the accessible label carrying the age, while
the CHK lamp is untouched. 1812 to 1831 tests passing, no new lint warnings.

Checked against the dev stack (`docker compose -f docker-compose.dev.yml`,
read-only, no writes): the Cluster preview page at 1600x1000, before and
after. Before, both engine clusters read 698 RPM and the Economy tile read
0.01 to 0.02 nm/L, matching the frozen `propulsion.{port,starboard}.fuel.rate`
confirmed live against SignalK (`GET
/signalk/v1/api/vessels/self/propulsion/port/fuel/rate`, timestamp
2026-09-07T00:59Z, about a day old). After, both clusters and the Economy
tile went stale, each showing the dash and an amber "Stale 16m" badge in
place of the frozen numbers, the sixteen minutes tracing back to the
`air` rebuild's own restart-driven resubscribe rather than the feed's real
age, exactly the boundary case section 2 above describes.
