# ADR 0286: Passage Plan Verdicts and Suggested Departures

## Status

Accepted (2026-11-14). Extends [ADR 0285](0285-passage-plans-replace-the-planned-stay.md),
whose table left the ETA blank and had no verdict. This is step 2a. Change marks
and progress tracking (2b), Mate's plan tools, and bar crossings follow.

## Context

ADR 0285 gave the Forecast page a table of every day of a run: the distance, and
the wind, gusts and waves read at the middle of each leg. It could not say
whether the leg was fit to sail. Mate already can. `assess_passage_window`
(ADR 0191, amended by 0194) judges a leg hour by hour against the operator's own
passage limits and names the figure that decided it, and `plan_tidal_departure`
(`assistant_tidal_departure.go`) ranks departures by the stream along the route.
Both are deterministic. Neither was reachable from a page.

## Decision

### One assessment endpoint, built from Mate's own rules

`GET /api/plans/{id}/assessment` (read tier) judges every routed leg of the plan
and reads the warnings on its stops. It builds each leg from the PR 1 legs, so
the vias and the routed path are the ones the table already shows, and it judges
the leg with `passageAssessor.cell`, the function `assess_passage_window` calls
per leg. Nothing asks a model. The operator's `assistant.passage_limits` supply
the thresholds, and marginal stays 0.7 of no-go.

The refactor that made this possible changed no Mate output:

- `newPassageAssessor` and `prefetchLegs` pull the assessor's set-up out of
  `assessItinerary`. `cell` takes the zone its days are read in as an argument,
  so one assessor, with its forecast cache, judges legs in different zones.
- `cell` also hands back what a plan needs in typed form: a short phrase beside
  the card's sentence ("gusts 32 kn ahead"), a reason for `no_data` (past the
  last forecast hour, or a hole inside it), and how far past its limit the
  worst figure stood. Mate's card and model text are built as before.
- An option on the assessor leaves wind against tide out of a cell and out of
  `not_checked`. A plan sets it, since it has no flood direction.
- The assessor's, the ranking's and the speed read's errors carry no tool name.
  Each tool adds its own at its boundary (`assess_passage_window:`,
  `plan_tidal_departure:`, `estimate_passage:`), so Mate sees the same text and
  the page never has to strip a name.
- `passageSources` pulls the weather, wave and tide wiring out of
  `executeAssessPassageWindow`.
- `rankTidalDepartures` pulls the ranking out of `plan_tidal_departure` as a
  typed function. The tool turns its JSON arguments into a query and the ranking
  into its result. A plan calls the function directly, so no JSON is built and
  parsed between two pieces of one program.
- `civilDawn` joins `sunTimes` on a shared zenith routine. First light is the sun
  6 degrees below the horizon.

The wind check judges against each segment's bearing and reads one forecast per
segment, so `planPassageLeg` keeps at most 10 points of a route
(`passageWindowMaxRoutePoints`, the bound `assess_passage_window` already puts on
its arguments). It runs Douglas-Peucker at the smallest tolerance that leaves 10
points, so the headlands stay. Every caller gets the same bound, and a route of
10 points or fewer comes through unchanged. The current and the ranking read the
full routed path.

### What a leg gets

- **A departure.** The operator's `HH:MM` when set, never changed. When not set,
  a suggestion, flagged `depart_suggested`.
- **An ETA.** The leg's routed distance over the boat's cruising speed, which is
  the median speed of the passages Helmcentral itself recorded: every visible
  passage that began in the last 12 months and ran at least 3 nm and 30 minutes
  under way, each taken as its distance over its time under way, from the figures
  kept on the passage row in the SQLite store. The short passages go because a
  marina move or a drift says nothing about how the boat cruises. This is a
  different source from `estimate_passage`, which reads the engine history in
  InfluxDB and keeps doing so, because Influx is optional here and Mate's tool
  needs the burn as well as the speed. A boat with no such passage gets "No ETA:
  no recorded passages yet", no time and no verdict. There is no default speed.
- **A verdict**, assessed at the departure used: `go`, `marginal`, `no_go` or
  `no_data`, and `limiting`, the figure that decided it in the table's words
  ("gusts 32 kn ahead"). It is the card's sentence without the leg's name, the
  direction the wind blows from, and the marginal threshold. `not_checked` lists
  what the assessor could not read, such as waves with no wave forecast. Wind
  against tide is left out of that list: a plan carries no flood direction, so it
  never applies.
- **A current**, the mean along-course modelled current over the run at the
  departure used, from the same Open-Meteo current model, grid cells and
  coverage rule as the ranking (at least half the run covered). Positive helps
  the boat. When the model has nothing, `current_kn` is null and `current_note`
  says why. For a suggested departure the ranking has already read it, so the
  chosen departure's figure is the one shown and the model is not asked again. A
  departure the operator set needs its own read, through `planCurrentAlong`.

### Suggested departure

A leg with no operator time gets one only when a window exists:

- **Earliest:** first light at the leg's start, rounded up to the quarter hour,
  and no earlier than the next quarter hour from now.
- **Latest:** with the operator's `daylight_arrival` limit on, sunset at the
  destination less the hours under way, using the same sunrise and sunset rule
  the assessor judges an arrival by (`legEndSun`). With the limit off, sunset at
  the start, so a suggestion still leaves in daylight. Rounded down to the
  quarter hour.

No window gives the note "No daylight window", no time and no verdict.

Inside a window, `rankTidalDepartures` scores every quarter hour on the modelled
current along the route, and the plan judges those departures with the same
`cell` that judges the leg:

1. It considers only departures that are not a No-Go.
2. Of those it prefers Go to Marginal, and Marginal to a time the forecast cannot
   judge.
3. Among departures with the same verdict it takes the fairest current, which is
   the ranking's own order.

If every departure is a No-Go, the plan shows the least bad one, the one whose
worst figure stands closest to its limit, with its No-Go verdict. A bad time is
shown as bad and never hidden. The plan walks the ranking from the fairest
current down and stops at the first Go, so a fair day judges one departure.

Plans carry no flood direction, so the tide phase method cannot rank, and the
ranking says so in the operator's words. When the model has no current for the
run, or no limits are set, the leg gets no time and the note "No departure
suggested: <reason>". There is no fallback to the middle of the window or to
first light. A suggested time is flagged for the page, which draws it in italics
as "Suggested 6.40 AM".

### How the work is shared out

The assessment does as much at once as the providers allow:

- A leg's own checks (is it routed, is its day still to come, its ETA, its
  window) need no provider and run first.
- The forecast along every assessable leg, the ranking or current of each leg
  (three legs at a time, as `planLegs` routes them), and the warnings then run
  together. The forecast fetch does not depend on which departure is chosen, so
  it overlaps the ranking.
- When all have answered, the plan judges the departures and fills in the
  verdicts, one leg at a time.

The warnings provider is resolved once and a place is read once, however many
stops share it to two decimal places of a degree. The read goes through the same
check the warnings fetch loop makes, so a provider serving only a stale cache is
a failure here too, not "no warnings in force".

### Where a leg has no verdict

Each of these leaves `verdict` empty and names itself in `note`, and none is
guessed:

- **Not routed.** The leg failed to route.
- **Date has passed.** The leg's day at its start is before today there.
- **No ETA.** The boat has no recorded passage to take a speed from.
- **Overnight legs are not assessed yet.** More than 24 hours under way. ADR 0285
  keeps a leg to one day.
- **No forecast yet.** The leg's day is more than ten days out (nothing is
  fetched, and no departure is suggested), or its arrival falls after the last
  forecast hour any point holds. A hole inside the forecast is `no_data`.
- **No daylight window**, or **No departure suggested**, above.

A failure that stops every leg (limits not set or unreadable, no weather
provider, the forecast fetch failing) is also said once in `problem`, and each
affected leg's note repeats it with `note_kind` "problem", so the table says
"Not checked" in the cell and the sentence above the table. Every note carries a
`note_kind` (`no_forecast`, `not_routed`, `past`, `overnight`, `no_window`,
`no_departure`, `gap`, `other`), so the page decides what to show from the kind
and never from the words.

### Stops: warnings

A stop with nights gets the official warnings in force on those nights, from the
configured forecast-warnings provider at the stop's position. A bulletin section
names its day as a weekday, so only nights in the next seven days are matched,
by weekday name, or "today" and "tonight" for tonight. A stop row shows its own
night. The night of arrival belongs to no stop row, since the passage row is that
day, so the passage row shows it under its verdict. A provider that fails sets
`warnings_error`, said once above the table. An empty column then does not mean
no warnings.

### Re-check

The page asks again on the forecast refresh interval (`FORECAST_REFRESH_SECONDS`,
ten minutes) and whenever a place, via, date, night count or departure time
changes. A change is acted on half a second after the plan stops changing, so a
run of edits is one question. From the edit until the answer for the edited plan
lands, the page shows no assessment: the verdict and current cells read "…", and
a failed check shows its error. It never carries an earlier answer over, because
a verdict on screen must be true of the plan on screen. A refetch of an
unchanged plan keeps the current answer until the new one lands, since that
answer is still the plan's. `as_of` is when the verdicts were worked out and
`forecast_as_of` the fetch time of the oldest forecast they read. The page shows
both above the table. Providers cache, so a refresh that finds an unchanged
forecast shows an unchanged `forecast_as_of`. There are no change marks yet.

### What the server keeps

Every request recomputes the whole assessment. The server keeps two things
between requests, both for ten minutes and neither one a failure:

- **The forecast at each point.** A point is keyed by its provider, zone and
  place, so an edit to one stop asks the provider again only for the places it
  moved. A failed read is never kept.
- **The cruising speed** from the recorded passages. Only its age invalidates
  it, since no single place writes a passage.

Nothing keeps the assessment as a whole. A whole-plan memo would have to know
everything a verdict depends on (the clock, the provider, the limits, the
speed), and a key that missed one would serve a wrong verdict. The point cache
holds the slow part, the provider calls, and what is recomputed from it is
cheap. A cold first read is still slow, about twenty seconds against the live
providers, and that is accepted for now.

## What it does not know yet

- **Lightning.** The table has no Lightning column. ADR 0284 holds exactly one
  Met Office grid cell, the boat's, and the scheduler's saved state, refresh
  clock and retry rules are written around that one cell. Each window is a whole
  grid download of about 334 KB, and a refresh of one cell is about 15 plugin
  calls inside a 60 second budget. Reading a leg means another cell per leg and
  its own cache and schedule, and a plan of five legs would multiply the
  downloads. That is a feature of its own and is not built here. A column of
  guesses would be worse than none.
- **Bars.** A leg across a bar is judged on the wind and waves along its route.
  Bar crossings come later and nothing marks them here.
- **Wind against tide.** Needs a flood direction a plan does not hold.
- **Change marks and progress.** 2b.
- **First light** is civil twilight, a calculation like the sunrise one. It is
  not checked against a published almanac.

## Rejected

- **Letting Mate judge the legs.** The verdict has to be repeatable and free to
  refresh every ten minutes, and the limits are the operator's own numbers.
- **Duplicating `cell()`.** A second set of rules would drift from Mate's the
  first time either changed. The refactor is small and Mate's tests held.
- **Falling back to first light or the window's start when ranking fails.** That
  is a guess dressed as a suggestion, and the fallback policy says to surface
  the missing source.
- **Suggesting the fairest current whatever its verdict.** It could put the leg
  at a time the page then called No-Go, when a calmer time the same day cost a
  little current.
- **Reading the ETA speed from InfluxDB.** Influx is optional here, and the
  passages the boat has recorded are already in the SQLite store with their
  distance and time under way.
- **Assuming a speed.** A boat with no recorded passage has no ETA. The reason
  shows.
- **Keeping the whole assessment for ten minutes.** The key would need the
  clock, the limits, the speed and the provider, and any it missed would show a
  stale verdict. The per-point forecast cache gets the saving without that risk.
- **Showing the last assessment, marked "Updating…", while a changed plan is
  checked.** Its legs would have to be matched to the edited plan by stop name,
  and a verdict could sit on a leg it was never judged for. The cells read "…"
  instead.
- **Showing the stop row's warning only.** It would hide a gale warning on the
  night of arrival.
- **A second ranking written for plans.** `plan_tidal_departure` already ranks,
  handles the tide-phase case and has its own tests. The plan calls the same
  typed function with a window and judges what it returns.
- **Calling the tool in process and reading its JSON.** That builds a JSON
  argument, parses a JSON result and keeps only its three best departures, when
  the plan needs every one to judge.

## Consequences

- A plan open on the Forecast page makes one assessment request per refresh.
  Each does the leg routing (cached), one forecast fetch per distinct sample
  point (answered from the point cache when it is fresh), one ranking per leg without a set time (its current comes with it), one
  current read per leg with a set time, and one warnings read per distinct place
  with nights in the next week.
- Assessment and suggestion run on the server, so the verdict is the same on the
  phone, the tablet and any browser. The kiosk does not show the table.
- `assistantToolDeps` gains no new field. The plan wires its readers
  (`planAssessDeps`) from the same production deps Mate uses, so a change to a
  provider's resolution applies to both.
