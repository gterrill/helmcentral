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
at 25.0 S, 153.6 E: 2.1 to 2.8 kn toward 155 to 160 degrees), against 0.1 to 0.5 kn
inside Hervey Bay at 25.0 S, 152.6 E. Offshore it swamps the tide.

## Decision

### The plugin gives currents on request

The wave contract gains an optional export, `fetch_currents({lat, lon, days})`,
returning `{"hourly":[{time, current_velocity_ms?, current_set_deg?}]}`. A plugin
gives both figures of an hour or neither. A missing key reads as absent on the
host, a reported 0 m/s is kept, and the host rejects a velocity outside 0 to 10
m/s, a set outside 0 to 360, or only one of the pair.

`fetch_waves` is untouched. Its request pins `models=ncep_gfswave025`, and
GFS-Wave carries no ocean currents (every value comes back null, checked live),
so currents cannot ride on it. The reference plugin makes a request of its own,
unpinned and asking for the two current variables alone, only when the host calls
`fetch_currents`. Currents therefore cost a request only when the fair-tide tool
runs. The plugin converts km/h to m/s, refuses any other velocity unit, and leaves
both keys out of an hour whose two figures are both JSON null. Everything else
that is not a clean answer is an error returned to the host: a missing or
mistyped array, unequal lengths, an unparseable time, a missing unit, a null in
only one of the pair, a negative velocity or a direction outside 0 to 360. A
failed request is an error too.

The host detects the export when it loads the plugin. An older plugin lacks it,
which is not a load failure; the tool reports "the installed wave plugin has no
currents; install the updated open-meteo-marine plugin". Currents have a cache
file of their own (`currents_wasm_v1_*`), keyed on the grid cell and the number
of days, with the same TTL and singleflight merge as the waves. They have no
stale fallback: a failed fetch is reported as the failure, because an old
forecast is not what the operator asked about. Fetches honour the call's context.
The cache holds 240 entries (four routes' worth) and is written to disk once per
tool call, not once per point.

### The tool ranks on the total current where the model covers the run

Deriving a "flood set" from the total was rejected: the ocean current swamps the
tide offshore, so the vector says nothing about which way the flood sets. The
tool uses the total as it is.

- **Route.** `route` is optional: the points in order, as `estimate_passage`
  returns them. The tool takes the boat along the polyline at a steady speed,
  sampling every five minutes, and reads the current at each sample's position
  (rounded to a 1/12 degree grid, the ocean model's own 0.08 degree cells, so a
finer grid would fetch one cell twice; each distinct point fetched once per call)
  with the course of the leg the sample is on. With no route it uses the one
  position and `course_deg`. The path does not depend on the departure, so it is
  worked out once.
- **Along-course figures.** Each sample projects the interpolated current vector
  on its course. Per candidate the tool reports `mean_along_course_kn` and
  `peak_along_course_kn` (largest in size, with its sign), positive with the boat,
  in both methods whenever the model covers the run.
- **Two methods, one per result.** If the model has a current at every sample of
  every candidate, `ranking_basis` is `modelled_current`: candidates rank on the
  mean along-course current, and `fair_fraction` is the share of the run with a
  positive along-course current. Otherwise `ranking_basis` is `tide_phase`, the
  existing method, which needs `flood_set_deg` and `flood_set_source` (standing
  notes, general knowledge or the operator), and `modelled_current_missing` names
  the first point or hour that lacked data. A result never mixes the two.
- **Fail fast.** With the model missing and no `flood_set_deg`, the call errors,
  naming both gaps. It never invents a direction. A supplied direction that the
  model made unnecessary is not used, and the result's note says so.

### When it cannot read

- **No wave provider, or a plugin without currents.** Every sample is missing,
  with the reason above.
- **A failed fetch for a point.** That point's reason names the point and the
  error.
- **Gaps in the hours, null near shore.** A sample needs a current at both hours
  around it, no more than an hour apart. Otherwise the run is missing, never slack.
- **A route that stays put.** A repeated point makes a zero-length leg, which has
  no bearing, so its course is taken from the next leg. A route whose points are
  all the same is an input error.
- **Forecast days.** Open-Meteo's hourly series ends at 23:00 local on the last
  day, so the tool asks for enough days that the last sample is bracketed whatever
  the zone, plus a day of margin. A passage ending beyond the 16-day forecast
  counts as no coverage.
- **Too many points.** More than 60 distinct points in one call (a long route)
  counts as no coverage, so the result falls back to `tide_phase`.

### How Mate words it

The basis and rule say the figures are the Open-Meteo model's forecast of the total
current, not an observation, on a grid of about 8 km, so river entrances, narrow
straits such as the Great Sandy Strait and shoals are not resolved and rates there
are likely understated. Mate still points to the tidal stream atlas. Under
`tide_phase` Mate says the stream was timed from high and low water and where the
direction came from. A guessed direction is never presented as the operator's own
notes. Mate passes the route from `estimate_passage`.

## What it does not do

- It does not read the current at the boat's actual speed or heading, only along
  the route at a steady speed over the passage hours.
- It does not tell a modelled current from a real one near shore. Where the model
  is null the run is reported missing.

## Consequences

- The operator needs the rebuilt `open-meteo-marine` plugin to get currents. Without
  it, a supplied flood direction still ranks by tide phase. The changelog lists
  the replacement under Breaking, because an operator who placed the plugin by
  hand must place the new file.
- A route of tens of nautical miles fetches up to 60 points the first time, each
  cached for the plugin's TTL.
- Another wave plugin may export `fetch_currents` from its own model.
