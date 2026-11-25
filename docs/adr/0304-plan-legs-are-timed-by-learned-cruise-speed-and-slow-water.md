# ADR 0304: Plan Legs Are Timed By Learned Cruise Speed And Slow Water

## Status

Accepted (2026-11-24). Replaces the ETA speed in
[ADR 0286](0286-passage-plan-verdicts-and-suggested-departures.md). This is PR 4a
of the leg-timing work. PR 4b brings the current into the ETA, in two steps: the
plumbing at zero current (see Amendment below), then the current in the speed.

## Context

ADR 0286 timed every leg at one speed: the median of each visible passage's
distance over its time under way. One number was wrong at both ends of a day.
A harbour leg starts and ends in a berth, a fairway or a lock, where the boat
crawls at tickover. The median, pulled down by those slow passages, made an open
water leg arrive late. The same median, applied to a leg that begins in a marina,
made the first mile arrive early. The ETA also never said what it was worked out
on, so the operator could not tell a good figure from a guess.

## Decision

A leg is timed at the speed the boat cruises at, except through the stretches
where it has been seen crawling. Both are learned from the boat's own recorded
passages in the SQLite store, never from InfluxDB, which stays optional.

### What is learned

The fit reads every visible passage that began in the last 12 months and walks
each into intervals between track points. An interval counts when it joins two
points of one recorded segment and was covered at 1 knot or better. One faster
than a boat can go is a jump in the fixes and is dropped. Speed is from position,
which every point has.

- **The cruise band.** Among intervals with engine rpm above zero, the rpm window
  that holds the most time under way. The window is 10 percent of the
  time-weighted 95th percentile rpm wide, so it scales with the engine. A tie goes
  to the higher rpm.
- **The cruise speed.** The time-weighted median speed over the ground of the
  intervals in the band. The current the boat met is inside it. It is a record,
  not a speed through the water.
- **Slow water.** The grid is 0.005 degrees of latitude and longitude, about 0.3
  nm, and a point on a cell boundary goes to the cell by floor. A cell is slow
  water when at least 2 passages went through it, its median rpm is below the
  band's low edge and its median speed is at most 0.75 times the cruise speed.
  The cell's speed is that median.
- **A second pass.** The cruise speed is worked out again with the slow cells'
  intervals left out, once, so a berth's crawl at cruising revs does not drag it
  down. No third pass. The slow cells are judged against the first-pass cruise speed,
  with the slow intervals still in it, and legs are timed at the second-pass
  figure.
- **The least it learns from.** 3 passages and 2 hours in the band. Below that the
  boat has no cruise speed yet.

Sailing never counts as low revs and never as in the band, because an interval
with no rpm is not an engine interval at all.

### How a leg is timed

The server walks the leg's full routed path in steps of at most 0.15 nm. A step
whose midpoint falls in a slow cell runs at that cell's speed. Every other step
runs at the cruise speed. Without the current the duration does not depend on the
departure, so the steps are built once per leg and each departure's pace is added
up over them.
The leg's ETA and the comfort and wind-change spans read that same timeline.

Each leg carries `eta_basis`, worded on the server: "Cruise 10.3 kn, 40 min in
slow water." or "Cruise 10.3 kn." when there is no slow water. Minutes are
rounded to 5, and an hour or more reads "1 h 10 min". It is null unless the row
shows an arrival still ahead, so a leg under way whose ETA has passed has none. `speed_kn` is the average speed the hours were worked out at, the leg's
distance over its hours, so hours times `speed_kn` is the distance; the cruise
speed is in `eta_basis`. A leg the boat is on with its departure past still
gets the hours and `speed_kn`. The plan table shows the
basis as a wrapping line under the times, because a phone has no tooltip.
`get_plans` passes it through.

### Where the fit runs

Nothing fits in the background. The passage store keeps a counter that moves
whenever what the passages hold changes: a passage recorded, hidden, edited,
split, merged, trimmed, imported, or given rpm by the motion backfill. The comfort
worker hears the same changes, except a passage the runner newly records, which
reaches it once its figures are stored; changes to the sea a passage was judged on
reach comfort alone and never move the counter.

A plan request asks `leg_pace.go` for the pace. The fit made last serves the
request if the counter and the day (the UTC date of the request's clock, since the
12-month window rolls daily) are as they were when that fit began. Otherwise the
request fits again, reading the year's track points in one query and splitting
them by passage in memory, and keeps the result. The counter is read before the
fit, so a change that lands during it makes the next request fit again. One fit
runs at a time, on its own context with a two-minute bound, apart from the request
that started it. A request that arrives meanwhile waits on the same fit and leaves
on its own context if it gives up, while the fit carries on and is kept for the
next request. A fit that hits its bound or fails to read is not kept. The first
plan view after a change pays for a fit, about a tenth of a second on a boat with
20 passages of 4000 points, and a burst of changes (an import, a backfill) costs
one fit, not one for each. Each fit logs one line when it finds something new:
the band, the cruise speed, the slow cell count, the passages, the hours in the
band, and the hours by compass octant, so a record that is all one heading shows
in the log. The result lives in memory only.

## When it can't run

Each of these gives the leg "No ETA: " and the reason, with no time and no
verdict. No speed is made up.

- **No recorded passages yet.** The record holds no visible passage.
- **No passages recorded in the last twelve months.** Visible passages exist, but
  none began in the last 12 months.
- **No engine rpm recorded under way.** Passages exist but none has rpm under
  way. That is every sailing passage, every GPX import and every passage before
  rpm was recorded. Passages recorded since October gain rpm from the InfluxDB
  motion backfill (ADR 0296), when that history exists.
- **Cruise speed not learned yet.** Under the minimum, with the numbers, such as
  "1.5 h at cruising revs over 2 passages, needs 2 h over 3".
- **The passage record could not be read.** The request gets that error, and the
  failed fit is not kept, so the next request tries again. An older fit is never
  shown in its place, following the fallback policy. A fit that hits its
  bound has the same fate. The four reasons above are answers, not failures: they are kept
  until a passage changes or the day turns.

## Operator's decisions

- **No rpm, no ETA.** There is no speed-only band and no fallback to the old
  median. A boat that only sails loses its ETA until a passage is recorded under
  engine with the engine feed on.
- **The raw median SOG.** The recorded speed already holds the current the boat
  met, so the cruise speed is used as it is. A record that is all one heading
  would bias it, so the log shows the heading mix.
- **Fit on demand.** A background worker with a debounce and a daily tick was
  built first and dropped in review: it was a second worker to start, settle and
  test for a result only a plan request reads. The request fits when the counter or
  the day moved.

## What it does not know

- **A slow stretch at cruise revs.** A river against the stream is slow over the
  ground at cruising revs, so it is not slow water by this test, and a leg through
  it runs at the cruise speed. The cruise speed already takes in such a stretch
  where it is common.
- **The current.** Not in the ETA yet. Slow cells add none. The next step adds the
  modelled current hour by hour, which makes each departure its own duration. The
  window search and the leg under way, built in the amendment below, already
  expect it.
- **Mate's own estimate.** `estimate_passage` keeps its own speed from the engine
  history in InfluxDB.

## Rejected

- **One median for every leg.** Wrong at both harbour ends, as above.
- **A speed-only cruise band for a boat with no rpm.** It would be a guess dressed
  as a figure.
- **Falling back to the old median when the fit can't run.** The fallback policy
  says to surface the missing source.
- **Fitting on every request.** It reads a year of track points. Reusing the fit
  until the counter or the day moves keeps the plan requests after the first cheap.
- **A background worker.** See Operator's decisions.
- **Keeping an older fit after a failed read.** The plan would show a speed the
  record no longer supports.

## Amendment: the plumbing for the current (PR 4b-i)

The current goes into the speed in the next step, which makes every departure
its own duration. This step builds what that needs and leaves the speed alone:
each step still runs at the cruise speed or its slow cell's, so no time on a
plan moves except the two figures below. Each departure is now a run of the leg,
which carries its timeline and, beside it, what the modelled current says along
it.

### The window is found by each departure's own arrival

The latest daylight departure used to come from shifting one timeline to end at
sunset. That is exact only while the duration doesn't depend on the departure.
The leg now lays out the quarter hours from first light to an outer bound (sunset
at the leg's start, or with the daylight-arrival limit, sunset at its stop, since
a later departure cannot arrive before it) and runs each one. A departure is in the window when its own
arrival makes the daylight. Nothing assumes a later departure arrives later.

- **Departures before now** only settle what to say. If an earlier one fits and
  none from now on does, the row says "Today's window has passed". If none fits at
  any hour, it says "No daylight window". Neither reads the current.
- **A departure that can't be run** is never in the window, past or to come,
  because it has no arrival to test. The error goes to the log. When every
  departure from now on fails to run, the row says "No departure suggested: the
  leg's timeline could not be worked out", never the error's own words.
- **The window is decided before the forecast is read.** Each leg's current read
  (one cached request, depending on the leg alone) and its departure runs come
  first, three legs at a time. A leg with no window, or one that has passed, then
  never asks for a forecast, and a forecast that fails is not its problem.
- **Each candidate is judged on its own timeline.** The verdict, the comfort line
  and the arrival the row shows all come from one run. The plan's own pace is
  never overwritten by a run, because the forecast is fetched while the runs are
  made and reads that pace.

### The leg under way runs from the latest fix

The leg the boat is on used to take the arrival of the pace from the departure
time the operator set. It now takes the latest recorded fix of the open passage,
from the SQLite store and not InfluxDB, and runs the rest of the route from the
time of that fix. The timeline has a knot at the start of the passage, one at the
fix and one at the arrival, so the comfort and wind-change readings from now to the
ETA follow the boat. A leg under way no longer needs a departure time set.

Once the passage has begun the leg is given no suggestion. The row shows the
boat's own departure, when the passage began, and the ETA from the fix, so the
departure plus the hours is the arrival. The leg has no suggested departure, no
ranking and no new verdict, and a departure time the operator set before leaving
does not replace the passage's start. With no usable fix, the row still shows when
the passage began and says why it has no ETA.

- **The fix goes to the foot of the perpendicular.** The route's steps are its
  segments, and the fix is projected onto each. The boat is placed where the nearest
  segment's perpendicular meets it, at the share of the step already behind it, so a
  boat beside the route is neither pulled forward nor held back by the schedule.
- **A boat off the route.** More than 1 nm from the route gives "No ETA: the boat
  is N nm off the planned route", with N to a tenth. The boat is not on this leg.
- **A route that passes near itself.** Among the segments within 0.1 nm of the
  nearest whose perpendicular lands on them, the fix goes to the one the boat would
  reach in still water closest to the time since the passage began, so an
  out-and-back leg places a boat on its way out or its way back.
- **Recorded fixes only.** The latest fix comes from the points the recorder took.
  An imported point never is, whatever its time. A point stamped after the time of
  the assessment is not read, so the fix before it is the latest.
- **A stale fix.** A latest fix more than 15 minutes old at the time of the
  assessment gives "No ETA: the latest position is N min old", because it says
  where the boat was and not where it is.
- **An ETA that has passed.** The row still shows the passage's start and the ETA,
  and the wind changes and comfort say the leg's ETA has passed. Nothing is counted
  on from now.
- **A leg wholly in slow water.** With no open-water step there is no current to
  rank by, so its departures keep the grid's order, show no current figure and no
  reason, and the leg is judged as any other.

### One current figure

The Current cell, and later the line under the times, say one number: the mean
current along the track, weighted by distance, over the open-water steps of the
departure the row shows that the model has a current for, to a tenth of a knot.
The model has none in a berth or a fairway, so slow water is in neither the mean
nor the miles it is missing for. A step past the end of a series, or in a gap of
more than an hour, counts as not covered. A leg covered in part shows its figure
over that part. Ranking still prefers a departure the model covers by at least half
when another is covered less, and a leg with no current at all still gets no
suggested departure.

The model is asked once for each leg: one point at the first open-water step in
each 1/12 degree cell, for the full 16 days. What is asked depends on the leg
alone, so the page, a slip and Mate's plan answers share one cached read however
the window falls. A route over the 100 cells one request carries is an error. A
request that fails blanks the figure and gives the reason, and leaves the verdict
and the ETA as they were.

Mate's own tools keep the one linear pace. Their ranking now refuses a set of
departures whose durations differ, since it samples every departure at the same
fractions of one duration.

### Rejected

- **Shifting the timeline to end at the ETA for the leg under way.** That holds
  the whole leg's duration against a boat that is part way along it.
- **Writing a run's pace into the plan's.** It races with the forecast fetch.
