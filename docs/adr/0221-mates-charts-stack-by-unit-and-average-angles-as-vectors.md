# ADR 0221: Mate's charts stack by unit, and angles are averaged as vectors

## Status

Accepted (2026-10-28). Amends [ADR 0191](0191-mate-cards-come-from-tool-results.md):
the telemetry chart no longer has a left and a right axis, and the set of card
kinds gains `wind_rose`. Builds on
[ADR 0193](0193-signalk-units-come-from-the-boats-meta.md) (declared units).

## Context

Asked how the wind had trended over a week, Mate drew one `telemetry_chart` card
with true wind speed in knots on the left axis and true wind direction on the
right. Four faults sat behind that card:

1. **The direction was in radians.** Radians have no display unit
   (`telemetry_units.go`), so the right axis read 0 to 6 beside a left axis in knots.
2. **Any second unit went to the right axis,** whatever it measured, so the tool
   would pair an angle with any magnitude.
3. **Nothing averaged a direction as a direction.** Bucket means were arithmetic
   means. Winds of 350° and 10° average to 180°, which is the opposite of where
   they blew from. Mate's prose repeated the figure.
4. **The card drew means only.** A gust never showed. And Helmcentral had no
   native wind rose, only a Grafana panel inside an Embed tile.

## Decision

**One unit to a panel, and no second axis.** A time chart draws stacked panels
that share the time axis. Each series carries a `panel` number, set by the server
in the order each unit first appears. Series in the same unit share a panel. An
angle gets a panel of its own, and so does a series whose unit the boat does not
report, because two unreported units are not known to be the same. The old
"at most two units" error is gone. The dual axis is removed outright: no case in
ADR 0191 needs it, since the only reason to share a plot was to compare shapes,
and the X against Y view does that without inventing a common scale. Four series
now fit as four panels.

**Angles are degrees, and they are points.** A path is an angle when the boat
declares its unit as `rad`. Never from its name, and never when no unit is
declared. A bearing (the path names a direction, heading, course or bearing) reads
0 to 360 and is labelled °T or °M by the path. Any other angle reads -180 to 180
and is labelled plain °. The panel draws dots on that scale with N, E, S, W, N
ticks, so a swing through north is a jump between dots and not a line across the
chart. Old stored cards that still say `rad` are shown in degrees too: the
endpoint decides from the declared unit, and accepts `rad` as the request unit.

**Circular averaging lives on the server, in `telemetry_angles.go`.** The unit of
work is the vector: InfluxDB returns the mean of the sine and the mean of the
cosine of each short window (new aggregates `sin_mean` and `cos_mean`, a `map`
before `aggregateWindow`), and Go recombines them with `atan2`. Both
`/api/telemetry/series` and `get_path_history` go through it, so the card and the
prose cannot disagree. The browser never averages an angle.

- **Weighting.** For true and magnetic wind direction, each window of a bucket is
  weighted by the true wind speed over that window, so an afternoon blow outweighs
  a dawn breeze. The windows are a sixth of a bucket (`circularSubWidth`). If wind
  speed is not logged in the range, or two devices log it, the mean is unweighted
  and the result says so in its `method` text. Direction itself is never averaged
  across devices.
- **No answer when there is none.** The length of the mean vector (1 when every
  sample agrees, 0 when they cancel) must reach 0.1. Below that, the bucket has no
  mean direction: it is left out of the points, counted in `undefined_buckets`,
  and shown as "No direction in N buckets". A dead calm has weight 0 and is
  undefined in the same way. The range's own mean uses the same rule, so opposed
  winds give no overall figure and the model summary says so in a `note`.
- **Spread.** `get_path_history` gives each angle bucket a circular standard
  deviation in degrees instead of min and max, which an angle has no meaning for.

**Bands for scalars.** `/api/telemetry/series` now also asks for each bucket's min
and max (three queries instead of one) and returns them beside the mean. The card
draws them as a pale area behind the line. For wind speed the top of the band is
the gust. An angle returns neither.

**A `wind_rose` card.** `show_wind_rose` counts the logged true wind over a range
into 8, 16 or 32 sectors (default 16, sector 0 centred on north) and speed bands
in knots (default edges 5, 10, 15, 20, 25). A sample is one time bucket, at most
2,000 for a range. The direction is the vector mean of the bucket, weighted by
speed within it. A bucket under 1 kn is calm and a bucket with wind but no mean
direction is variable. Both are counted apart and in no sector. Buckets that have
one of the two readings and not the other are counted as `unpaired`.

- **It stores the counts, not a reference.** Unlike `telemetry_chart`, the card
  holds a snapshot (sectors by bands, about 100 integers), because the prose beside
  it was written about exactly those figures and the card has no date control. A
  changed range is a new question to Mate.
- **Fail fast.** The tool refuses when the boat does not declare the direction in
  radians or the speed in m/s, when either is not logged in the range, and when two
  devices logged either. It names `direction_source` and `speed_source` to choose.
- **Typed, with a golden fixture.** The Go struct is `windRosePayload`. The
  fixture in `testdata/mate_events/message_source.json` is decoded with unknown
  fields disallowed in Go, and by the strict decoder in TypeScript, which also
  checks that the counts add up to the samples. There is no dashboard tile in this
  change: a tile can reuse the same aggregation later.

**Prompt.** A wind trend is a wind rose, or true wind speed and direction in
separate panels, never one chart with two axes. "Where has the wind been coming
from" gets the rose. Mate quotes the vector mean and never averages a direction
itself.

## Alternatives considered

- **Keep the right axis for an explicit request.** Rejected: nobody asked for it,
  and the failure it caused was an angle read against a magnitude.
- **Convert radians to degrees and keep the linear mean.** Rejected: that fixes
  the label and leaves the number wrong.
- **Average angles in the browser.** Rejected: Mate's prose and the card must agree.
- **Fall back to an unweighted or linear mean without saying so.** Rejected under
  the fallback policy. Where weighting is not possible, the method text says so.
- **A wind rose reference card that refetches.** Rejected for now: it needs a second
  endpoint for a figure that is small and fixed.

## Consequences

- `/api/telemetry/series` costs three Influx queries per scalar series. An angle
  costs two, plus one for its weight.
- A stored `telemetry_chart` with `axis` and no `panel` decodes as panel 0 and 1.
- `get_path_history` on an angle path now returns degrees, a `spread`, a `unit` and a
  `method`, and no min or max.
- Not checked against a live InfluxDB: the Flux for `sin_mean` and `cos_mean` is
  covered by text tests only.
