# ADR 0304: Plan Legs Are Timed By Learned Cruise Speed And Slow Water

## Status

Accepted (2026-11-24). Replaces the ETA speed in
[ADR 0286](0286-passage-plan-verdicts-and-suggested-departures.md). This is PR 4a
of the leg-timing work. PR 4b brings the current into the ETA, in two steps: the
plumbing at zero current (first amendment below), then the current in the speed
(second amendment).

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
- **The current.** The ETA takes the modelled current in open water, hour by hour,
  which makes each departure its own duration (second amendment below). Slow cells
  add none, and a stretch the model has no current for runs at the cruise speed.
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
  any hour, it says "No daylight window". In this step neither read the current;
  the second amendment runs them with it.
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
request that fails blanks the figure and gives the reason. In this step it left the
verdict and the ETA as they were; the second amendment takes the ETA too.

Mate's own tools keep the one linear pace. Their ranking now refuses a set of
departures whose durations differ, since it samples every departure at the same
fractions of one duration.

### Rejected

- **Shifting the timeline to end at the ETA for the leg under way.** That holds
  the whole leg's duration against a boat that is part way along it.
- **Writing a run's pace into the plan's.** It races with the forecast fetch.

## Amendment: the modelled current is in the speed (PR 4b-ii)

Each open-water step now runs at the base speed plus the modelled current along the
track at the moment the boat enters the step. The base is the learned cruise speed,
which is the raw median over the ground: the current the boat met on its recorded
passages is already in it, and the model's current is added on top. The one
integrator walks the steps for every departure, so a departure has the duration the
current gives it, and the run, the verdict, the comfort walk, the wind changes and
the row's times all read that one arrival.

- **Slow steps and uncovered steps.** A step in slow water runs at its cell's speed
  and takes no current, since the model has none at a berth or fairway. A step the
  model has no current for (a gap, the end of a series, null near shore) runs at
  the cruise speed. A leg with a current gets no steady-leg shortcut: only a leg
  with no field, or none that covers any open water, keeps the exact minutes of
  linearPace. Mate's own tools keep linearPace.
- **A current against the boat stops the run.** A step where the base speed plus
  the current is 0.5 kn or less fails that run. Half a knot is where 0.15 nm takes
  18 minutes and the sum of such steps stops being an arrival. The operator reads:
  "the current against the boat leaves it under 0.5 kn part of the way", the
  threshold formatted from the constant. A suggested departure with it is left out,
  as a run that can't be made is. The rules for what a row says are in the third
  amendment below.
- **A failed current request.** The current is in the speed, so a failed request
  (the plugin errored, not null cells) leaves no ETA, with the detail in the log.
  This reverses the first amendment, which kept the ETA and blanked the figure.
- **Overnight is checked for each departure.** The still-water pre-check is
  unchanged and decides first, so a leg that is over a day with no current at all is
  refused as before. The current can then push one departure past 24 hours, or pull
  it back under: a run over 24 hours is left out of the suggestions, and when none
  is left the row says "Overnight legs are not assessed yet".
- **Past departures run with the current too.** Whether the day had a window is
  decided on the same arrivals as the suggestion, not on still-water ones.
- **A slip reads the current.** The slips price verdicts only, but a verdict is
  judged on an arrival, and the arrival now depends on the current. A slip with the
  operator's departure reads the leg's current so its ETA matches the page. The
  slips of a plan run side by side and share one request for each leg, whose points
  depend on the leg alone.
- **The basis names the current.** The line under the times says "Cruise 10.1 kn,
  current against you 0.4 kn, current not modelled for 2 nm, 5 min in slow water."
  The current clause is the row's one figure (the mean over the modelled open-water
  steps, to a tenth): "current with you", "current against you", or "no current
  along the course" when it rounds to 0.0. "Current not modelled for N nm" appears
  when the open water without a current is half a nautical mile or more, to whole
  miles. A clause with nothing to say is left out, and a leg with no open water has
  no current clause.

The timeline of a run with a current has a knot at each change of speed, which with
a varying current is about one for each 0.15 nm step. The timeline's reads are
binary searches, and the benchmark of a 60 nm leg across 48 departures is the cost
to watch.

### Follow-ups this leaves

- The plan's own pace (`planLeg.pace`, read by the window search for sunset at the
  stop and by the passage assessor's layout) is still the still-water one, so the
  daylight-arrival bound on the grid is an outer bound and each run decides its
  own fit. A tighter bound would save runs.
- The page asks for 16 days of current and Mate's plan read for 7, so the two make
  separate requests.
- The departures are judged one after another. They could be run side by side.
- A failure to read the current leaves the leg with no ETA for as long as the
  plugin is down, where the first amendment degraded to still water. If that proves
  harsh, the answer is a clearly flagged still-water ETA, which needs the
  operator's agreement.

## Amendment: one set of ETA rules (PR 4b-ii review)

The first draft of the current in the speed had a rule for each kind of leg: a leg
that could not be judged kept its still-water arrival, a suggested leg ran a
still-water grid to find its window before it learned the current was unreadable,
and a row with a note could carry still-water hours beside it. The operator chose
one set of rules instead, which replaces the earlier choice that a leg that cannot
be judged keeps main's still-water arrival.

- **Any leg with a departure reads the current, and its ETA includes it.** The
  departure is the operator's, or the boat's own on the leg under way. Whether the
  leg can be judged does not matter: a leg with its passage limits unset, or one the
  still-water pre-check calls overnight, still has an arrival with the current in
  it and shows the current figure. A day past the forecast's reach reads the
  current too, once, and gets the arrival the model's coverage allows.
- **A run with no ETA says so in place of the leg's other note.** A failed read
  ("No ETA: the modelled current could not be read"), a run the current stalls and
  a run that could not be made all leave the leg with no arrival, whatever else the
  leg said. "No ETA" is the more specific note, and the plan's one problem, such as
  passage limits not set, stays above the table. A run the current takes past a
  day is different: it has its timeline, so the arrival shows. A leg that was to be
  judged gets the overnight note, and one that could not be judged keeps its own.
- **A suggested leg whose read fails says so first.** "No departure suggested: the
  modelled current could not be read" comes before any window is worked out, so no
  still-water grid runs and no window or overnight note stands in for the reason.
- **A row with no departure or no ETA shows no hours, speed or basis.** The
  still-water timeline the first draft kept for the daylight window, the overnight
  check and the wire is now only the pre-check; only a run reaches the row. This
  covers no daylight window, window passed, overnight, stalled, a failed ranking, a
  failed judge, a day beyond the forecast for a suggested leg, and a failed read.
  The page reads only the arrival and the basis, so it relied on none of them.
- **When every departure ahead fails, the note names the most common cause.**
  Stalled, overnight and not run are counted, and a tie goes to stalled, then
  overnight, then the generic note. 39 overnight runs and one stalled run give the
  overnight note.
- **The leg under way counts the slow water ahead of the fix.** A fix past the
  berth no longer lists the berth's minutes in the basis, the same scope as the
  current clauses.
- **The memo key is the request.** What a leg's read gives is the series of the
  cells asked for, which each leg reads along its own steps, so the key is the
  points asked for alone, built when the memo looks a leg up.

One function words every failure (the leg's kind, "No ETA" or "No departure
suggested", and the cause), and one place (`takeEta`) turns a run into a row's ETA,
so the operator's departure and the leg under way cannot drift apart.
