# ADR 0084: Derived Fuel Range and Time to Empty

## Status
Accepted

Extends ADR 0055 (host-derived vessel paths) and ADR 0083 (ages ride the
gauge-values stream). Part of the same evaluation of MV Dirona's N2KView
screen as ADR 0080 through 0083.

## Context

MV Dirona's 2016 Maretron N2KView main screen shows five tank quantities, a
burn rate, and a computed nm/gal economy figure, and stops there. Range is
left for the operator to work out by hand. The mockup built during the
evaluation to fix this drew "Range at current burn, 378nm, Basis 0.559
nm/gal, 60 min" beside the tank rail, naming both the number and what it
rests on. That phrasing is close to word for word what this ADR's
implementation uses.

Helmcentral already had the underlying arithmetic for the economy half:
`helmcentral.propulsion.fuelEconomy` (ADR 0055) is speed over ground divided
by total engine burn. What it did not have was a volume to multiply that
economy by, or a way to turn the same burn into a time instead of a
distance.

The evaluation also caught, live, why this cannot be built carelessly. At
capture time both `propulsion.{port,starboard}.fuel.rate` on the evaluation
vessel carried timestamps about 20 hours old, from before the engines were
last shut down, while the tank levels and speed over ground were current.
`vesselFuelEconomy` computed a figure from that frozen rate anyway, and the
Cluster preview page's Economy tile read 0.02 nm/L: an honestly computed
number, built from a burn reading that had stopped being true most of a day
earlier. A time-to-empty or a range figure built from the same burn rate
would inherit exactly that failure unless guarded against directly.

## Decisions

### 1. Three derived paths, absent rather than a stale number

`helmcentral.fuel.volume` (m3) sums `currentLevel × capacity` over every
`tanks.fuel.<id>` node that publishes both. A tank with only a level -- two
of this vessel's own tanks report a ratio with no known size -- contributes
nothing, because multiplying a ratio by an unknown capacity is not a volume.
The whole figure is absent only when no tank contributes anything at all,
rather than reporting a lonely zero.

`helmcentral.fuel.timeToEmpty` (s) is that volume divided by the boat's
total current burn, the same sum `vesselFuelEconomy` already computes.
Absent whenever nothing is burning or there is no volume to divide.

`helmcentral.fuel.rangeAtCurrentBurn` (m) is that same volume times
`fuelEconomy`, so it is absent under exactly the conditions that already
make economy absent: stopped, not burning, or no volume to plan a range
from.

All three ride the existing `gauge-values` mechanism (ADR 0055) with no new
widget code: a gauge or an alarm rule can bind any of them the way it binds
any other path.

### 2. The staleness guard sits in front of publication, not in the UI

ADR 0083 already marks a stale reading on any tile bound through
`gauge-values`: dimmed, with an age badge. That is a display treatment. The
number is still there underneath, and code that reads a derived sample's
`Present` field directly -- `derivedAwareAlarmReader`, the mechanism that
lets an alarm rule bind a derived path at all -- sees the value regardless
of how old the inputs behind it are.

`derivedInputMaxAge` (120s, matching the frontend's `STALE_AFTER_SECONDS` in
`lib/staleness.ts`) sits earlier than that instead: a derived fuel figure is
reported absent, not merely old, once any input it is built from is older
than that threshold. The same guard now applies to `fuelEconomy`, since it
is exactly the path the 0.02 nm/L reading came from. An alarm rule bound to
`helmcentral.fuel.rangeAtCurrentBurn` can never fire on arithmetic done
against a fuel-rate sensor that stopped reporting hours ago; it sees no
value, the same as if the path had never been published. The age is still
reported alongside the absent value, so a tile or a log can still say how
old the dead input is.

An age of -1 (unknown, ADR 0068) does not count as stale here either. A
source that has never carried a timestamp gives no evidence either way, and
treating "no evidence" as "too old" would blank a figure permanently the
first time it met an input with no timestamp of its own.

### 3. The built-in Tanks tile reads five fields from one pass

`buildTanksStatePayload` carries `fuel_volume_m3`, `fuel_volume_age_s`,
`fuel_time_to_empty_s`, `fuel_range_m` and `fuel_derived_age_s` (the oldest
input age behind whichever of the range and time figures needed it),
computed from the same `computeDerivedPaths` pass a gauge bound to the same
paths reads. The built-in tile and a configured gauge can never disagree
about either the number or its age, because both come from the same call.

The tank-level list in that same payload comes from a separate REST fetch
(`fetchSignalKTanksState`) and can fail over to a backend fallback on its
own. The fuel figures do not depend on that fetch succeeding, since they
read the delta-stream snapshot the rest of `gauge-values` already uses.

### 4. Instantaneous, not smoothed

Both range and time to empty are built from this moment's speed and this
moment's burn, with nothing averaged. The Tanks footer states this in its
own label, "at current burn", rather than in a tooltip, so the figure cannot
be mistaken for a trip estimate. A boat at displacement speed sees its range
figure move with every wave and every throttle change. Averaging that over a
window is a real improvement and real work of its own -- a ring buffer, a
decay, a decision about what "current" should mean when the reading it is
built from is itself noisy -- and is left for a later pass rather than
folded into this figure's first version.

## Consequences

- The `helmcentral.` namespace grows from four paths to seven, and the three
  new ones are bindable in a gauge, a lamp or an alarm rule with no new
  widget code, the same as `fuelEconomy` already was.
- `fuelEconomy` changes for anything reading it through `gauge-values` or an
  alarm rule: a figure that was previously published regardless of how old
  its inputs were now goes absent past two minutes of staleness, applied at
  the same point `computeDerivedPaths` already publishes it from. The
  Cluster preview page's Economy tile already carried ADR 0083's stale
  badge over that same frozen reading; now the number itself disappears at
  the same threshold rather than only being marked.
- A fuel tank that does not publish a capacity is silently excluded from
  fuel aboard rather than counted as a zero-sized tank. If it is ever fitted
  with a capacity sender, it starts contributing with no code change.
- A rule bound to one of these three paths behaves the way a rule bound to
  fuel economy already did once ADR 0070 fixed the reader gap: it can see
  the value, and it sees nothing rather than a stale number once an input
  has gone quiet.

## Verification

Backend: `go test -short ./...`, against fixtures captured from the live
vessel 2026-09-08 (`backend/testdata/fuel/`). Volume from tanks 2, 4, 5 and 7
only, about 3.0987 m3, with two tanks excluded for publishing no capacity.
With the fixture's own timestamps -- the burn rate about 20 hours older than
the tank and speed readings -- volume is present while time to empty and
range are both absent, each still reporting the frozen burn rate's age.
Recomputed with the burn rate moved fresh, time to empty is about 3.7185e6 s
and range about 2.257e5 m. Range stays absent when stopped even with a fresh
burn rate. Volume is absent with no tank publishing a capacity. The
tanks-state payload carries all five fields. 1141 to 1146 tests passing.

Frontend: `npx vitest run`, `npx tsc --noEmit`, `npm run lint`. The `length`
quantity gains `nm`. The Tanks tile footer renders from a mocked payload,
blanks each figure independently when its own age is stale (fuel aboard on
its own age, range and time together on the shared derived age), and does
not render at all with no fuel tank listed. 1831 to 1838 tests passing, the
pre-existing 18 lint warnings unchanged.

Checked read-only against the dev stack (`http://localhost:5173`, no
writes): `GET /api/tanks-state` carries the five new fields. With this
vessel's own fuel-rate feeds still reporting a day-old timestamp, volume
read live while range and time to empty read absent with an age near a day,
the same frozen input ADR 0083 found.
