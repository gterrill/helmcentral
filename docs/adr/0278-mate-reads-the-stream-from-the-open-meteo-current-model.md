# ADR 0278: Mate Reads the Stream From the Open-Meteo Current Model

## Status

Accepted (2026-11-11). Extends the fair-tide departure of
[ADR 0155](0155-mate-times-departures-for-a-fair-tide.md), which timed the
stream's phase and left the set to Mate and the rate unknown. Adds one optional
export to the wave-plugin contract.

## Context

`plan_tidal_departure` ranked departures by the stream's phase, taken from the
tide station's high and low water. It needed the flood's set direction, and the
only source was Mate: the standing notes if they said, otherwise its own general
knowledge. A guessed direction was the one input the tool could not check, and it
knew no rate at all.

Open-Meteo's marine API publishes hourly `ocean_current_velocity` (km/h) and
`ocean_current_direction` (degrees the water flows toward). It is the **total**
current: tide plus the ocean currents. That is what matters for passage planning.
Off Fraser Island the East Australian Current runs 2 to 3 kn south (checked live
at 25.0 S, 153.6 E: 2.1 to 2.8 kn toward 155 to 160 degrees), against 0.1 to 0.6 kn
inside Hervey Bay at 25.0 S, 152.6 E. Offshore it swamps the tide.

## Decision

### The plugin gives the route's currents in one request

The wave contract gains an optional export:

```
fetch_currents({"points": [{"lat", "lon"}, ...], "days": n})
  -> {"points": [{"hourly": [{time, current_velocity_ms?, current_set_deg?}]}, ...]}
```

The answer has one entry per requested point, in order. A plugin gives both
figures of an hour or neither. A missing key reads as absent on the host, a
reported 0 m/s is kept, and the host rejects a velocity outside 0 to 10 m/s, a
set outside 0 to 360, only one of the pair, or an answer for a different number of
points than asked.

A call carries at most 100 points. The host errors above that and never splits a
route into several requests, because the cap keeps the URL near 2 KB and one
request is what the design is for.

`fetch_waves` is untouched. Its request pins `models=ncep_gfswave025`, and
GFS-Wave carries no ocean currents (every value comes back null, checked live),
so currents cannot ride on it. The reference plugin makes one request of its own,
only when the host calls `fetch_currents`: unpinned, the two current variables
alone, with comma-separated latitude and longitude lists. Open-Meteo answers a
list of locations with a JSON array, one object each in request order, and a
single location with a plain object. The fixture in the plugin's tests is a real
response, including a location whose hours are all null.

The request uses `timezone=GMT`, so the times are UTC and one `utc_offset_seconds`
cannot be wrong across a daylight-saving change, as it could with `timezone=auto`.
It adds `past_days=1`, so the series starts at 00:00 UTC yesterday and a window that
starts earlier today is bracketed.
A response whose offset is missing or not zero is an error. The plugin converts
km/h to m/s and refuses any other velocity unit. Only a JSON null in both arrays
means "no current that hour". Everything else that is not a clean answer is an
error returned to the host: a missing or mistyped array, unequal lengths, an
unparseable time, a missing unit, a null in only one of the pair, a negative
velocity or a direction outside 0 to 360.

The host detects the export when it loads the plugin. An older plugin lacks it,
which is not a load failure: the tool treats that as the model not covering the
run, and says "install the updated open-meteo-marine plugin".

**A small memory cache, no disk.** The answer is one request to a free keyless API.
A repeated question, such as the operator asking again with a different wind
in mind, would ask Open-Meteo for the same thing, so the host keeps the last 16
answers in memory for 30 minutes, keyed by the points, the forecast days and the UTC
date (the series starts at 00:00 UTC yesterday). An expired entry is never served,
not even when the new request fails, and a failure is not remembered. Nothing is
written to disk: an earlier design cached each point on disk, wrote the file up to 60
times a call, and fanned out one request per point; this replaces it. Each plugin
call runs in its own instance, so a slow currents request does not hold up the
dashboard's wave fetches. The call checks its context before and after; one call
cannot be cancelled in flight, so at most one request is left running when a
context ends, and its answer is dropped.

### The tool ranks on the total current where the model covers the run

Deriving a "flood set" from the total was rejected: the ocean current swamps the
tide offshore, so the vector says nothing about which way the flood sets.

- **Route.** `route` is the points in order, as `estimate_passage` returns them.
  The route may start in open water off the creek or berth the boat is in, so a
  start more than 2 nm from `lat` and `lon` is used and the result's note names the
  distance. More than 20 nm apart is a different passage and an error. The tool
  takes the boat along the polyline at a steady speed, sampling every five minutes,
  with the course of the leg each sample is on. A repeated point makes a
  zero-length leg, which takes the next leg's course. A longitude jump over 180
  degrees is the short way round the antimeridian. A route whose points are all one
  position is an input error. **With no route the model is not used at all**: one
  cell is not the whole run, so the answer is `tide_phase` with
  `modelled_current_missing` saying no route was given. Mate is told to pass the
  route, and to say so when `estimate_passage` left it out (`route_omitted`).
- **Cells.** Samples are grouped into cells of 1/12 degree, the ocean model's own
  grid, with cell centres at k/12 + 1/24 degrees (live answers report -24.958336,
  152.62502 and -25.458336, 153.79167), so one host cell is one model cell.
  Open-Meteo prefers the nearest sea cell, so a point near shore can be answered
  from a neighbour. The request carries the first actual sample position in each cell, not the cell
  centre: snapping could move a near-shore water point onto a land cell, where the
  model has nothing. A route needing more than 100 cells is an error.
- **Along-course figures.** Each sample projects the interpolated current vector
  on its course. Per candidate the tool reports `mean_along_course_kn` and
  `peak_along_course_kn` (largest in size, with its sign), positive with the boat,
  in both methods whenever the model covers the run.
- **Gaps are listed, not switched on.** Earlier designs decided per run whether
  the model "covered" the route, with a rule for the river or marina at each end.
  Each rule needed another exception. With a route and any model data the tool
  now always ranks on the model (`ranking_basis` `modelled_current`). For each
  departure it works the mean, the peak and the fair share over the samples that
  have a current, never a null counted as 0. A sample has none when its cell is null
  or the model's hours do not bracket its time; both are the same kind of gap. Each
  candidate carries `covered_fraction` and `uncovered`, a list of `from_nm` to
  `to_nm` stretches of the route (adjacent samples merged, a single sample counting
  as one sample spacing), so Mate can name the start, middle and end stretches and
  tell the operator to check them against the tidal stream atlas. A departure with
  no covered sample has no figures and a reason, and one covered less than half
  is never offered as best or worst while another is covered more. When none
  reaches half, those with data still rank and the note says every run was
  mostly uncovered.
- **Two methods.** `tide_phase`, the earlier method, is used only when there is no
  route, or the model has no current for any sample of any departure (no wave
  plugin, a plugin without the export, every cell null). It needs `flood_set_deg`
  and `flood_set_source` (standing notes, general knowledge or the operator), and
  `modelled_current_missing` says why the model gave nothing. A result never mixes
  the two methods, and the tool logs a line each time it uses `tide_phase`.
- **No tide data under the model.** `modelled_current` reads no tide station and no
  extremes, and reports no station, stream or phases. They come from a different
  source and could lead Mate to say "leave on the flood" about a ranking that
  ignored the tide. Times are in the departure position's zone. `tide_phase` needs
  a station and its extremes as before.

### A failed request is not a gap

A plugin, HTTP or parse error, or a cancelled context, is an error returned to
Mate. It never becomes a switch to the tide, because the model not answering is a
fault to report, not a place with no data. With no flood direction, `tide_phase`
errors naming both gaps. It never invents a direction.

No data at all means: no route, no wave plugin installed (a blank `ui.wave_provider`
with the default plugin missing), a plugin without the export, a passage ending
beyond the 16-day forecast, or every cell null. A wave provider that cannot be
resolved (unreadable settings, `ui.wave_provider` naming a plugin that is not
installed) is a configuration fault: an error. The series ends at 23:00 UTC on the
last forecast day, so the tool asks for enough days that the last sample is
bracketed, plus a day of margin, in buckets of 7 or 16. The window starts no
earlier than now, and a default latest is 24 hours after that start.

### How Mate words it

The basis and rule say the figures are the Open-Meteo model's forecast of the total
current, not an observation, on a grid of about 9 km, so river entrances, narrow
straits and shoals are not resolved and rates there
are likely understated. Mate still points to the tidal stream atlas. Under
`tide_phase` Mate says the stream was timed from high and low water and where the
direction came from, and only then do the station, stream and phases appear. A
guessed direction is never presented as the operator's own notes. Mate passes the
route from `estimate_passage`, and names the uncovered stretches of each candidate.

## What it does not do

- Each model cell is read at the first route sample in it. Open-Meteo answers from
  the nearest sea cell, but a cell whose answer is null is uncovered for every
  sample in it.
- It does not read the current at the boat's actual speed or heading, only along
  the route at a steady speed over the passage hours.
- It does not tell a modelled current from a real one near shore. Where the model
  is null the stretch is listed as uncovered, and the figures are over the rest.

## Consequences

- The operator needs the rebuilt `open-meteo-marine` plugin to get currents. The
  changelog lists the replacement under Breaking, because an operator who placed the
  plugin by hand must place the new file. Without it the tool times the stream from
  the tide, or says the stream cannot be timed.
- Another wave plugin may export `fetch_currents` from its own model.
