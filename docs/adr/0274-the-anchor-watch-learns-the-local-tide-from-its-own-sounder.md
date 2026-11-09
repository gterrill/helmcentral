# ADR 0274: The Anchor Watch Learns the Local Tide from Its Own Sounder

## Status

Accepted (2026-11-09). Amends [ADR 0272](0272-the-low-water-clearance-is-worked-out-on-the-server-and-mate-reads-it.md):
the depth at the next low can now come from the sounder's own readings at this
anchorage instead of the tide station's prediction. The server still holds the
one calculation, the page and Mate still read it, and every rule in ADR 0272
about inputs, staleness, rounding and the gate carries over.

It builds on position-based tide providers working through the source identity
below. A provider with no stations, which names its "station" by the boat's
rounded position, is one source like any other: the calibration needs no station
coordinates and no station list, and its heights may be negative.

## Context

On 2026-11-08 the boat lay in Great Keppel. At 10.18 AM the sounder read 4.6 m
and the station, Middle Island, stood at 3.68 m, falling to a 0.64 m low at
2.45 PM. The line took the station's fall and projected 1.6 m. The sounder
bottomed at 2.36 m. Two things were wrong, and they need different fixes:

- **The water here has its own range.** At the anchorage it rose and fell about
  0.83 of what Middle Island predicts.
- **The boat moves over the seabed.** At 40 m of rode the swing circle is about
  65 m across, so a change in depth is part tide and part seabed.

Comparing depth against time alone mixes the two. Comparing readings taken over
the same patch of seabed at different heights of tide separates them. The
design note and the prototype are in `docs/research/local-tide-calibration.md`.

## Decision

### 1. What it learns, and from what

While an anchor watch is set the server fits

    depth = seabed[cell] + k × tide(t + offset)

to the sounder's readings. `tide` is the station's prediction (the provider's
highs and lows, cosine-interpolated). A cell is a 2.5 m square of seabed in an
east/north frame from the anchor. `k` is the share of the station's range the
water here has. `offset` is how far ahead the station's curve is read: a positive
offset is a local tide that runs earlier than the station's, a negative one
later. It is searched across plus and minus 30 minutes: a coarse pass in 5 minute
steps, then every minute (the recording interval) around the best of them.
Every fit searches the whole span, so the offsets that stay plausible are
decided by the readings alone.

The fit is Huber-weighted least squares, solved by iterative reweighting. `k`
comes from the readings within a cell, so a swinging boat that crosses a cell
at two heights of tide separates tide from seabed. The minutes just after
the drop, while the boat settles on its chain, are not skipped: the Huber
weights already count them down. A fit whose `k` has no detectable tide in it,
meaning `k`'s 95% interval includes zero, is a failed fit (`implausible_fit`)
and is never used. There is no fixed range for `k`.

Gonum (`gonum.org/v1/gonum`, the Student's t quantile only) is the first maths
dependency, kept to that one import.

### 2. What it records

The poll tick gathers the depth and the GPS antenna's position every 5 seconds.
At the end of each minute the server stores the median of that minute's depths
(at least half the samples the poll interval would give, 6 at 5 seconds), and the median position in `tide_calibration_readings`, keyed by the watch's `set_at`:

| Column | Meaning |
| --- | --- |
| `set_at_ms` | The watch the minute belongs to. |
| `t_ms` | The middle of the minute, Unix milliseconds. |
| `lat`, `lon` | The antenna's position. No transducer offset. |
| `depth_m` | Median depth of the minute. |

The primary key is `(set_at_ms, t_ms)`. About 1,400 rows a day, in the same
SQLite file as the anchor trail, so the feature needs no InfluxDB. Rows are kept
for good; nothing prunes them.

The highs and lows go in `tide_calibration_extremes` (`set_at_ms`, `source`,
`t_ms`, `height_m`, `high`), upserted when a chart's content changes. The source
is the provider and station id; for a position-based provider the id is the
rounded position, so a boat that moves starts another source. A chart supersedes
what the same source stored for the span it covers, whether its times or its
heights changed, and the extremes before that span stay. Charts are told apart
by a hash of their content. The fit uses the current source's extremes alone, so
a change of source mid-stay is never spliced: readings the new source does not
reach are not used, and the hours reported say so. The fit builds its
tide curve from these, so a stay of several days keeps its history after the
chart has moved on, and a stored stay can be fitted on its own. That is why the
readings carry no station height of their own. A reading is stored whether or not
the chart can be read at that moment; only the fit waits for it.

- **Drop** starts a new `set_at` and so a fresh set of readings.
- **Adjust** keeps `set_at`. The readings stay, and the cells are recomputed
  from the corrected anchor position on the next request.
- **Raise** stops recording and keeps what was stored. Nothing deletes it.
- **A restart** loses only the minute in hand. The worker loads the stay from
  SQLite once and makes the first fit in the background. Until it publishes,
  the page and Mate say the calibration is being worked out
  (`working_it_out`).

### 3. Noise

No filter drops readings. A filter that rejects spikes in time order throws away
real readings when the boat swings onto a different patch of seabed, so the
Huber weights of the fit count spikes down instead, and a test pins that spikes
move the estimate less than its interval. The median over a minute's
five-second samples has already removed most spikes before a minute is stored.

The stay's noise level is the floor of the solver's scale. It is the robust scale
(1.4826 × MAD) of the readings' deviations from a straight line fitted to the
minutes around each, which is the tide taken out. A sounder that reports in steps
of 0.1 m scatters by about a twentieth of a metre about that line, so a run of
readings that agree exactly does not turn every other reading into an outlier.

### 4. How sure it is

Neighbouring minutes agree with each other, so the fit treats the lag-1
autocorrelation of its residuals as lost information: the effective sample size
is `n(1-ρ)/(1+ρ)`, and the variances grow by its inverse. That gives a 95%
interval on every prediction from the Student's t distribution.

The time offset is not known exactly early on, and the early offset swings the
answer by tenths of a metre. Every offset the readings cannot rule out at the
95% level (a likelihood-ratio test) goes into the prediction, and the interval
widens to take in all of them. The backtest on the first version, which picked
one offset and trusted it, called a stay ready after 65 minutes at 1.75 m when
the sounder went on to read 2.36 m.

### 5. What it predicts

For the next low and the next high:

- the cell where the boat lies now (that of the latest stored minute, no
  older than the sounder's staleness limit plus one recording minute);
- the shallowest cell sounded, for the low, and the deepest, for the high. Cells
  are ranked by the side of their interval the operator can be sure of: the
  shallowest has the lowest upper bound, the deepest the highest lower bound.
  A cell the boat crossed once by GPS jitter has a wide interval and does not win
  on a lucky value.

The fit reads the station's curve at `t + offset`, so the local tide reaches the
station's extreme at `t - offset`. Each figure is `seabed + k ×` the extreme's
height, with its 95% half-width, and it is reported at that local time: the low
and high times the page and Mate give are the station's less the offset. The next
low and high are the first whose local time is still ahead, walking the stored
extremes, so a local low that falls after a station low that has passed is not
skipped and a time that has gone by is never reported. The high
is separate. When it can't be predicted (the curve doesn't reach it, or there is
no upcoming high) the high-water figures are empty with their own reason, and the
low stands. The 0.2 m
allowance in the verdict (the boat may never lie on that exact spot at low water) and the 0.2 m difference that earns the shallowest and
deepest spots a line are one operator-agreed constant. The
station estimate stays as ADR 0272 has it, the depth now less the predicted
fall, with no interval.

### 6. Readiness

No hard-coded hours or metres. The calibration is ready when the 95% half-width
on the next-low prediction for the boat's own cell is narrower than the
operator's low-water clearance margin. With the margin at zero there is nothing
to judge by, and no threshold stands in: the reason is `no_clearance_set`. Until
then the line uses the station
estimate, labelled "Depth based on tide station prediction", and says why the
calibration is not ready:

- `too_few_readings`: the interval is wide because the readings are few; the
  line shows the hours so far.
- `readings_too_scattered`: one reading's own 95% scatter is wider than the
  margin, so more readings narrow the interval only slowly. A noisy sounder
  keeps the interval wide and says so.
- `implausible_fit`, `no_recent_reading`, `no_tide_curve`, `unavailable`: the
  readings show no tide, the latest minute is stale, the tide curve or the store
  is the problem.

Once ready, the line reads "Locally calibrated depth estimate (4 h of
readings)" with the figure and its interval, for example "2.4 m ±0.2". The
hours count up.

### 7. The verdict

`too_shallow` when the bottom of the 95% interval of the most cautious
low-water figure is under draft plus margin. The most cautious figure is the
lowest bottom of:

- the calibrated depth over the boat's cell;
- the station estimate, which has no interval, so its bottom is itself;
- the shallowest sounded cell plus 0.2 m.

The 0.2 m allowance comes from the backtest, below. ADR 0272's other rules hold:
the clearance is judged as displayed, below zero is always too shallow, the
gate is unchanged, and a failed poll keeps the last answer marked stale.

The headline stays the figure for where the boat lies now. When the shallowest
sounded spot (or the station) set the verdict, the line says so and shows that figure: the station estimate
without an interval, or the shallowest spot with its own. The shallowest
sounded spot also shows when it is at least 0.2 m shallower than the headline,
and the deepest for high water when it is at least 0.2 m deeper.

### 8. High water

The same fit gives the depth at the next high, for the boat's cell and for the
deepest cell sounded, each with its interval. There is no verdict and no
warning on it.

### 9. The tide-match note

Once calibrated, when the calibrated and station depths for the next low differ
by more than the margin, the tide line says so: "The tide here doesn't follow
Middle Island (11 km away): it runs 64% of its range." The distance is from the
station's coordinates (the provider's chart, or its station list when a
station-based chart has none) to the anchor, and is left out if neither has them.
A tide with no station, a model for the boat's position, reads "The tide here
doesn't follow the tide model" with no distance. The test for such a provider is
the station id being a position, in one function, until the plugin's explicit
position-based flag is wired. An empty station list counts as a failed search
and is tried again when the chart changes. Before
calibration nothing extra shows.

### 10. Mate

The page's answer carries no method field: a calibration with `ready` set is the
calibrated one. `get_vessel_status` `low_water` gains a `method` label (`station`
or `calibrated`, derived from that), `depth_at_low_half_width`, `cautious_source`
and a `calibration` block: ready or the reason it is not, hours of readings used (the span of the readings the fit used, not the stay), the
tide here as a percentage of the station's with its interval, how many minutes
later the tide here runs than the station's (negative means earlier), the shallowest sounded spot, the next high and the deepest sounded
spot (each a value with its half width), the station's name and distance, and
the tide-match note. The tide percentage, minutes later, station name and distance are
Mate's alone; the page does not receive them. On the wire, each interval figure
is `{value_m, half_width_m}`. Figures are in the
operator's units and times are boat-time labels, as elsewhere in the tool.
None of the calibration's figures is given until it is ready. The Anchor Watch
screen sentence tells Mate to name the method and quote the ±.

## Evidence

The prototype replayed the 2026-11-08 Great Keppel stay minute by minute. Over
the whole stay `k` settled at 0.83 to 0.85 and the model fitted every reading
to within 2 cm.

The backtest ran on every anchor-watch stay from 2 to 8 November: Western Bay,
Rescue Bay, Island Head Creek and both Great Keppel stays, each against its
nearest BoM station. There were 11 low-water estimates.

| Lows | Median error | More than 0.1 m too deep |
| --- | --- | --- |
| Station estimate | 0.27 m | 4 |
| Calibrated | 0.19 m | 6 |
| Lowest of calibrated, station, and shallowest + 0.2 m | 0.27 m | 1 |

- **Open bays behave.** The calibrated figure was mostly within 0.3 m and
  beat the station's median.
- **Island Head Creek broke every method.** The stream turned the boat end for
  end, read 4.9 to 12.7 m, and at low water the boat lay over ground it had
  not crossed. Every estimate was 3 m or more too deep. No figure for where the
  boat lay could have warned of it. That is why the verdict uses the cautious
  figure and not the headline alone, and why the map tint of sounded cells is
  the next piece of work.
- **Unseen ground is the norm.** At 6 of the 11 lows and 10 of the 12 highs,
  most readings near the extreme were over cells not crossed when the estimate
  was made.

## Failure modes

- **No watch:** no recording, no calibration; the station estimate stands with
  no calibration block.
- **The store cannot be read, or the tide chart cannot be fetched:** the
  calibration comes back `unavailable` with the cause, and the station estimate
  stays on screen under its own label. Nothing stands in for the missing
  figures.
- **The tide curve does not reach the readings or the next extreme:**
  `no_tide_curve`.
- **The boat has not given a minute within the sounder's staleness limit plus a minute:** `no_recent_reading`; the
  boat's own cell is unknown.
- **Unsounded ground:** no fit can sound seabed the boat has not crossed. The
  cautious figure and the shallowest-spot line cover what the boat has crossed.
  The tint on the map will show what it has not.

## Not in this decision

The map tint of sounded cells, the keel line, the rode planner's depth at high
water, reusing `k` at a saved place and a transducer offset setting.

## Consequences

- The Anchor Watch page and Mate give one figure with one stated method.
- The calibration learns only from this watch's own readings, so it needs hours
  at anchor. It helps least at the moment the operator most wants it, just
  after the drop, and says so.
- A tide station far from the anchorage no longer misleads silently: the note
  says how much of the range the water here runs.
- A single background worker fits the stay after each stored minute, coalescing
  wake-ups. It is woken by a stored minute or a changed watch or anchor, never by
  the tick, so a tide provider that is down is asked at most once a minute. It
  calls no plugin with its lock held, and publishes the result. A request only predicts from the published
  fit: no chart fetch, no store read, no lock the recorder holds. After an Adjust
  or a new Drop the page says the calibration is being worked out until the
  worker has published a fit for that anchor.
- Old stays' readings are kept and never pruned.
