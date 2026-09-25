# ADR 0129: Current Conditions Shows True Wind

## Status

Accepted (2026-09-25).

## Context

The Current Conditions tile ([ADR 0092](0092-wall-display-tiles.md)) has
always shown apparent wind speed: the wind as the boat's own motion bends
it, not the wind actually blowing over the water. Apparent reads low
running downwind and high beating into it, which is exactly backwards from
what the tile exists for - a glance
that can be checked against today's forecast band without doing the
arithmetic in your head first. A live example, underway: apparent wind
read 9 knots while the true wind was 15 - close enough to shrug off, far
enough to change whether a reef was due.

The apparent figure isn't wrong; the Apparent Wind tile (the wind compass)
still shows it, unchanged, because the compass exists to answer a different
question - what the sails and rig are actually feeling right now, relative
to the bow. Current Conditions exists to answer a different one: what the
wind out there is doing, the number a forecast bulletin and a Beaufort
table are both talking about. One boat asks both questions; one tile
should not have to answer both with the same number.

The live SignalK server already carries both: `environment.wind.speedTrue`
and `environment.wind.directionTrue`, both published under a `derived-data`
source (SignalK, or a plugin on it, doing the vector subtraction of the
boat's own motion for us). Nothing else in the tile has to compute
anything.

## Decision

### Read `speedTrue`/`directionTrue` straight from SignalK

`backend/signalk.go`'s `fetchSignalKVesselState` mirrors the existing
`speedApparent`/`angleApparent` parsing (value-wrapper lookup with a bare
fallback, radians-to-degrees conversion, the same `windDataRecent`
staleness gate) for two new fields, `WindSpeedTrueKts` and
`WindDirectionTrueDeg` (normalized 0-360, the direction the wind blows
*from*). Both carry the -1 absent sentinel on their own terms: a vessel with
no true-wind source reports absent, never the apparent figure relabelled.
That is a deliberate difference from `WindSpeedApparentKts`, which defaults
to 0 when stale or unpublished, because "no apparent wind" is itself a real
reading on a boat sitting still; "no true-wind source" is not a wind
reading at all, and showing 0 there would read as a becalmed sea that might
not exist.

`speedTrue` is water-referenced (True Wind Speed, the figure the SignalK
spec and the wind-instrument world both mean by "true wind"), not
`speedOverGround`-referenced (ground wind, sometimes called TWS-over-ground
on some plotters). Chosen because it is what the marine instruments this
tile stands next to already label TWS, and what a forecast's own wind
figure is comparable to. The two agree at anchor with no current running
and diverge from each other exactly where a current is running - a
current-referenced true wind and a ground-referenced one are different
numbers on the same boat, and this tile reports the water-referenced one an
operator would call "true wind" without qualification.

### The tile's own readout, marker and arrow

`frontend/src/components/current-conditions-tile.tsx` swaps
`windSpeedApparentKts` for `windSpeedTrueKts`, relabels the readout "True
Wind", and adds a small inline arrow next to the speed digits, rotated to
point the way the wind is blowing (downwind - direction + 180, the
weather-map convention) rather than the direction it's blowing from. A
secondary "SE 126°" line sits beside it. Direction absent draws no arrow at
all, never a fake 0°. The arrow lives inside the row the speed digits
already occupy rather than a new row underneath - the tile is sized for
the wall display's fixed fold (ADR 0092) and gains no extra height budget
to spend.

The tile's bullet-gauge "obs" marker - the last hour's highest recorded
reading, shown against today's forecast band - moves from
`max_gust_kts['1h']` (apparent, gust-ladder scale) to a new
`max_true_wind_kts_1h` field: the max of `trueWindSpeedHistory` (already
recorded, in raw m/s off `environment.wind.speedTrue`, for the Law of
Storms squash-zone signature) over the last hour, converted to knots once
inside `inMemoryMaxTrueWindKts`. Left on the apparent scale, that marker
would
have kept comparing a true-wind readout against an apparent-wind
watermark - two different quantities on the same axis. `max_gust_kts` and
the Apparent Wind tile that reads it are unchanged; this is a second,
parallel figure, not a rename of the first.

## Consequences

- The tile's number now agrees with the forecast band next to it and with
  what a Beaufort table or a weather bulletin means by "wind speed" -
  closing the 9-vs-15-knot gap the apparent reading used to leave open.
- A boat with no derived true-wind source (no SignalK plugin computing it)
  shows a dash on this tile rather than apparent wind under a "True Wind"
  label, which would be a wrong number wearing the right name.
- The Apparent Wind tile (wind compass) is untouched: apparent wind remains
  the number that tile exists to show, for the question it exists to
  answer.
- True wind is aged on its own timestamps, so a stalled true-wind
  calculation reads as dashes even while apparent wind keeps arriving.

**Update (2026-09-25):** the "Apparent Wind tile is unchanged/untouched"
claim above (Context and this Consequences list) no longer holds.
[ADR 0130](0130-wind-tile-true-wind-and-north-up.md), merged the same day,
renamed that tile to Wind and gave it its own Apparent/True toggle reading
`speedTrue`/`directionTrue` directly (plus `angleTrueWater`, which this ADR
never touched) - the wind compass no longer shows apparent wind
exclusively. Everything else here stands as written: the
`speedTrue`/`directionTrue` parsing and its per-leaf recency gate are now
shared between the two tiles rather than duplicated (see ADR 0130's own
Status section), and the Current Conditions tile's own readout, marker and
arrow are unaffected.

**Update (2026-09-25, later the same day):** the tile's "obs" marker no
longer has its own `max_true_wind_kts_1h` field, described under Decision
above. A code review found that field's separate buffer
(`trueWindSpeedHistory`) and ADR 0130's `max_gust_true_kts` ladder
(`trueWindGustHistory`) were recorded behind two different freshness gates,
so the two tiles could show different "last hour" true wind figures for the
same moment - see [ADR 0130](0130-wind-tile-true-wind-and-north-up.md)'s own
amendment for the fix. The marker now reads `max_gust_true_kts['1h']`
instead; everything else in this ADR (the speed/direction readout and
arrow) is unaffected.

## Related

- [ADR 0092](0092-wall-display-tiles.md) for the tile's own wall-display
  sizing and forecast-band layout, unchanged here.
- [ADR 0130](0130-wind-tile-true-wind-and-north-up.md) for the Wind tile's
  own True/Apparent toggle, which supersedes this ADR's "Apparent Wind tile
  is unchanged" claim above - see the note at the foot of Consequences.
