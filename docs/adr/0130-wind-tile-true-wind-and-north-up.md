# ADR 0130: Wind Tile Gains True Wind and North Up

## Status

Accepted (2026-09-25). Renames the Apparent Wind tile
([ADR 0008](0008-wind-tile-corner-masks.md)) to Wind and adds a true-wind
reading and a North Up orientation alongside the apparent/Course Up view it
has always shown, plus a parallel true-wind gust history and ladder next to
the apparent one ([ADR 0030](0030-selectable-max-gust-windows.md)). Landed
the same day as, and merged alongside,
[ADR 0129](0129-current-conditions-shows-true-wind.md), which independently
added the Current Conditions tile's own true-wind readout - the two share
`vesselStateData`'s `WindSpeedTrueKts`/`WindDirectionTrueDeg` fields and
`fetchSignalKVesselState`'s per-leaf speedTrue/directionTrue parsing outright
(one definition, two readers); this ADR covers what it adds on top:
`angleTrueWater` (`WindAngleTrueDeg`/`WindSideTrue`/`WindAngleTrueRelativeDeg`,
gated the same per-leaf way), the True/North-Up toggles, and the Wind tile's
own gust ladder.

## Context

The Wind tile has only ever shown apparent wind, spun by the boat's own
heading (Course Up): what you feel standing at the helm, not what is
actually moving over the water. Apparent wind is the wrong number for
planning a passage or checking the boat's own instruments against a
forecast - it is warped by the boat's own speed and heading, sometimes
badly, close-hauled or running downwind. The wind instrument's
`environment.wind` subtree already carries true wind alongside apparent
(`speedTrue`, `angleTrueWater`, `directionTrue`), confirmed live on the
boat's own SignalK server, in the same units and shape apparent's fields
already use (`$source` derived-data, radians, signed bow-relative angle).

A second, independent request landed alongside it: a north-up compass, for
relating the wind to a chart or a compass bearing rather than to the boat's
own bow.

## Decision

### Two toggles, not two tiles or a settings dialog

Wind mode (apparent/true) and orientation (course-up/north-up) are two
independent chip buttons in the tile's title bar, each a plain flip
showing its current state as text ("Apparent"/"True",
"Course Up"/"North Up"), persisted per device under
`windTile.windMode`/`windTile.orientation` in `localStorage` - the same
per-device-setting pattern the tile's MAX GUST window cycling already uses
(`windTile.gustWindow.left`/`.right`). Two toggles rather than one
apparent/true/north-up/course-up four-way cycle because the two axes are
genuinely independent (any wind reading can be viewed in either
orientation) and a combined cycle would make "go back to what I just had"
take up to three extra taps instead of one.

The chips live in `Tile`'s existing `titleExtra` slot
(`components/ui/tile.tsx`), which needed no change - it already renders
whatever a tile hands it, flush at the trailing end of the title bar,
exactly where the autopilot tile's own mode badge sits. The tile's title
itself becomes the plain string "Wind": neither toggle's state belongs in
the title text, since both are now visible, editable controls of their
own.

### True wind is parsed and gated per leaf, but never substituted for
### apparent

`fetchSignalKVesselState` (`signalk.go`) parses `speedTrue`/`angleTrueWater`/
`directionTrue` the same way it has always parsed
`speedApparent`/`angleApparent`: a `{"value": ...}` wrapper with a bare-number
fallback, its own recency gate (`defaultWindMaxAge`, 5 minutes),
radian-to-degree conversion, and side (port/starboard) from the signed
angle's sign. It is a parallel code path, not a shared one and not a
fallback for one another - a boat with a wind instrument that publishes
apparent but not true (or whose true-wind input has stopped, e.g. a dead
boat-speed sensor feeding the wind unit) must show apparent normally and
read true wind as unknown, never the other way around.

True wind's recency gate is deliberately narrower than apparent's, not an
exact copy of it, and narrower again than this ADR's own first draft.
Apparent's `windTimestamp` falls back to the generic
`environment.wind.timestamp`, and its `windDataRecent` falls back again to
the GNSS fix time (`state.Datetime`) if even that is missing - apparent has
always effectively read as "recent enough" whenever *anything* in the wind
subtree, or the boat's own clock, looked current. [ADR 0129](0129-current-conditions-shows-true-wind.md)
established the true-wind rule instead: `speedTrue` and `directionTrue` are
each gated on *their own* leaf's timestamp only
(`windSpeedTrueRecent`/`windDirectionTrueRecent`), with no fallback to
`state.Datetime` and no fallback to a sibling leaf's timestamp either. This
ADR's `angleTrueWater` parse (`WindAngleTrueDeg`/`WindSideTrue`/
`WindAngleTrueRelativeDeg`) follows the identical pattern
(`windAngleTrueRecent`, its own leaf's timestamp only) rather than the
combined, all-three-leaves-share-one-gate approach this ADR shipped with
initially - merging alongside ADR 0129 surfaced that a combined gate lets
one fresh leaf (say `angleTrueWater` still ticking) paper over a sibling
leaf whose own source has actually gone quiet (`speedTrue`'s derived-data
calculation stalled), reviving a frozen reading exactly the way apparent's
old GNSS fallback did. No timestamp, or a stale one, on a given leaf now
means only that leaf's own field(s) stay at their sentinel - independent of
whatever the other two leaves or the GPS fix are doing.

Where apparent's absent/stale case has always defaulted speed to `0` and
angle to `0`/starboard (a long-standing quirk, left alone here since fixing
it is a separate concern), true wind's four new fields
(`WindSpeedTrueKts`, `WindAngleTrueDeg`, `WindAngleTrueRelativeDeg`,
`WindDirectionTrueDeg`) default to the ordinary `-1` "unknown" sentinel
`lookupNumber` already uses elsewhere (`WindSideTrue` to `""`), read by the
frontend the same way every other sentinel-bearing vessel-state field is:
`typeof x === 'number' && x >= 0 ? x : null`. Wind tile's True mode then
shows a dash rather than a fabricated 0°/0kt reading. `wind_last_update_age_s`
is shared between the two - both live in the same `environment.wind`
subtree - so no new age field was needed.

### True wind gets its own gust history and ladder

`windGustHistory` (`telemetry_history.go`) already ran a 24-hour ring buffer
of apparent wind speed, sampled on every 5-second poll tick
(`sampleTracks`, `tracks.go`), to answer the MAX GUST cards' windowed
maxima. A second buffer, `trueWindGustHistory`, records
`state.WindSpeedTrueKts` on the same tick, gated on its own `>= 0` sentinel
check independent of whether apparent recorded anything that tick. The
per-window-ladder walk (`inMemoryMaxWindGustKtsFor`'s single backward pass
over the ring, computing every window's max in one walk because the ladder
is nested) was extracted into a shared `maxGustKtsForBuffer(buffer,
windows)`, with `inMemoryMaxWindGustKtsFor`/`inMemoryMaxTrueWindGustKtsFor`
now both thin wrappers over it. The vessel-state payload's `max_gust_kts`
gains a sibling, `max_gust_true_kts`, carrying the same four-window ladder,
but *not* the same clamp apparent's ladder uses. Apparent's clamp turns a
window with no samples into `0` (dead calm) and then enforces every longer
window's value is never less than the previous, already-clamped one,
starting that walk from `0`. Copied verbatim onto the true ladder, that
clamp read a boat with no true-wind source - or one that had just
restarted, with an empty ring buffer - as a confident `0kt` calm rather
than unknown. `max_gust_true_kts`'s clamp instead leaves a window with no
samples at its `-1` sentinel untouched, and only starts enforcing the
longer-never-less-than-shorter rule once a shorter window has actually
produced a real (`>= 0`) value to enforce it against; the running
"previous" value itself starts at `-1` rather than `0`; and is only ever
updated from a real value, never from an untouched sentinel. A boat with
a true-wind source that just came back online after ten minutes of silence
therefore correctly shows the 10m/30m windows as unknown while 1h/24h,
if they still hold older samples, report real numbers.

`trueWindSpeedHistory` (ADR 0070's heavy-weather trends) was *not* reused
for this, despite the similar name: it records raw, unconverted m/s off the
live delta-stream snapshot for a slope/tendency calculation with no
recency gate of its own, on a different contract than a gust ladder needs.
Reusing it would have meant either double-converting units or silently
losing the gust-specific staleness gate; a second buffer, matching
`windGustHistory`'s own contract (already-converted knots, recency-gated at
the point of recording) was the simpler, correct choice. This is also the
buffer ADR 0129's own `max_true_wind_kts_1h` (the Current Conditions tile's
single last-hour "obs" marker) reads, via `inMemoryMaxTrueWindKts` - a
different figure, on a different scale, sourced from `trueWindSpeedHistory`
rather than `trueWindGustHistory`, so the two features' true-wind numbers
stay independently correct rather than one borrowing the other's buffer.

One gap, left open rather than built out here: `max_gust_kts` has an
Influx-backed path for boats with InfluxDB configured
(`cachedMaxGustKtsFor`, refreshed by a background ticker), so its gust
history survives a backend restart. `max_gust_true_kts` has only the
in-memory ring buffer - no Influx query was added for it. True gusts are
lost across a restart on an Influx-configured boat the way apparent gusts
are not, today. Revisit if that gap matters in practice; nothing about the
in-memory path needs to change to add the Influx one later.

### North Up moves the bow marker; `WindCompass` stays pure

Course Up has always rotated the compass ring under a bow fixed at 12
o'clock, computing each tick/label's position from `geo - heading`. North
Up instead holds the ring still (`heading` treated as `0` for every
tick/label/cardinal, so true north always renders at the top) and moves a
small bow-triangle marker around the ring to the vessel's actual heading
instead - the inverse of Course Up's fixed-bow, rotating-ring shape.

`WindCompass` (`wind-compass.tsx`) takes no mode or orientation of its own.
It draws exactly three numbers it is handed: `ringRotationDeg` (what every
tick/label subtracts its own bearing from), `bowRotationDeg` and
`arrowAngleDeg` (both "degrees clockwise from the ring's own 12 o'clock",
the same frame token twice over so the same rotate-and-transition
machinery drives both). `WindTile` computes all three from
`headingTrue`/orientation/mode:

| | Course Up | North Up |
| --- | --- | --- |
| `ringRotationDeg` | `headingTrue ?? 0` | `0` |
| `bowRotationDeg` | `0` (fixed) | `headingTrue` |
| `arrowAngleDeg`, apparent | `windAngleApparentDeg` | `normalize(headingTrue + windAngleApparentDeg)` |
| `arrowAngleDeg`, true | `windAngleTrueDeg` | `windDirectionTrueDeg` |

`bowRotationDeg`/`arrowAngleDeg` are `number | null`; `null` hides that
element entirely rather than guessing a heading or angle North Up has
nothing real to show. This only bites North Up: the bow marker always needs
`headingTrue` to place there, so it disappears whenever heading is unknown,
regardless of wind mode. The apparent-wind arrow also needs `headingTrue`
(to convert its bow-relative angle to an absolute bearing), so it
disappears alongside the bow. The true-wind arrow does not - `directionTrue`
is already an absolute compass bearing - so it keeps showing even with no
heading, while the bow marker next to it is still absent. The centre speed
and side/angle readout never depend on any of this; they read straight off
whichever mode is active.

The bow marker's rotation reuses the wind arrow's existing shortest-path
angle-unwrap (`useShortestPathRotation`, extracted from the arrow's own
inline logic so both call it) and the same 650ms `ease-out` CSS transition,
so switching to North Up with the boat mid-turn sweeps the bow to its new
position rather than snapping or spinning the long way round the 0°/360°
wrap.

None of this touches the four corner cards or their `computeCornerMasks`
geometry (ADR 0008) - Course Up's rendered output is unchanged, pixel for
pixel, and North Up is additive, built entirely from `WindCompass`'s
existing rotate-a-`<g>` machinery.

## Consequences

- The Wind tile can show either wind reading in either orientation, with
  the setting remembered per device rather than reset every visit.
- A boat with no true-wind source (most: it needs boat speed and heading
  feeding the wind instrument, not just wind alone) sees True mode read a
  dash, honestly, rather than a fabricated or borrowed number.
- `max_gust_true_kts` does not yet share `max_gust_kts`'s Influx-backed
  persistence across a backend restart - a known, deliberately deferred gap
  (see above), not an oversight.
- `WindCompass` remains a pure renderer of three pre-computed angles; a
  future third orientation or wind reading is a new case in `WindTile`'s
  table above, not a new prop threaded through the SVG component.

## Amendment 2026-09-25: max_true_wind_kts_1h retired, one buffer instead of two

A `/code-review high` pass found that the design above - `max_true_wind_kts_1h`
(ADR 0129's Current Conditions "obs" marker) sourced from `trueWindSpeedHistory`,
kept deliberately separate from this ADR's `max_gust_true_kts` ladder and its
`trueWindGustHistory` buffer, "so the two features' true-wind numbers stay
independently correct" - did not hold up: the two buffers are recorded on
different freshness gates (`trueWindSpeedHistory` via `alarmSampleAge(...) <=
defaultWindMaxAge`, the heavy-weather trend's own staleness check;
`trueWindGustHistory` via a bare `state.WindSpeedTrueKts >= 0`), so the same
"last hour" true wind figure could genuinely differ between Current
Conditions and the Wind tile at the same moment - independently WRONG, not
independently correct.

`max_true_wind_kts_1h` is retired. The vessel-state payload no longer carries
it; the frontend's Current Conditions tile now reads `max_gust_true_kts['1h']`
- the same ladder entry the Wind tile's True mode already showed - via
`useVesselState`'s existing `maxGustTrueKts`. `computeMaxGustTrueKtsFor` and
`trueWindGustHistory` are unchanged; `inMemoryMaxTrueWindKts` (the function
that read `trueWindSpeedHistory` for the old field) is deleted.
`trueWindSpeedHistory` itself is untouched and still backs ADR 0070's
heavy-weather slope/tendency calculation, which this amendment does not
touch - only the marker that used to also read it.

`go test -short ./...`/`go vet ./...` (backend) and `npx tsc --noEmit`/
`npx vitest run` (frontend) pass.

## Related

- [ADR 0129](0129-current-conditions-shows-true-wind.md) for the shared
  true-wind parsing this ADR builds on: `WindSpeedTrueKts`/
  `WindDirectionTrueDeg` and `fetchSignalKVesselState`'s per-leaf
  speedTrue/directionTrue gate. Its own `max_true_wind_kts_1h` reading is
  retired by this ADR's 2026-09-25 amendment above - the Current Conditions
  tile now reads this ADR's own `max_gust_true_kts['1h']` instead.
- [ADR 0008](0008-wind-tile-corner-masks.md) for the corner-mask geometry
  this leaves untouched.
- [ADR 0030](0030-selectable-max-gust-windows.md) for the gust window ladder
  both `max_gust_kts` and the new `max_gust_true_kts` share.
- [ADR 0068](0068-stale-source-marking-on-tiles.md) for the
  `last_update_age_s`/staleness contract `wind_last_update_age_s` follows,
  shared unchanged between apparent and true wind.
- [ADR 0070](0070-heavy-weather-indicators.md) for `trueWindSpeedHistory`,
  the differently-contracted buffer this ADR's `trueWindGustHistory` was
  kept separate from.
