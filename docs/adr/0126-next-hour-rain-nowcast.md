# ADR 0126: Next-Hour Rain Nowcast

## Status

Accepted (2026-09-23).

## Context

The Current Conditions wall tile (ADR 0092, `current-conditions-tile.tsx`)
shows depth, apparent wind and outside temperature in its top half. Its
bottom half carried a single line, `rainLine`, built from `nextRain`
(`lib/forecast-bands.ts`): the first hour in the next 24 that crosses a 40%
chance threshold, e.g. "Rain likely from 2PM (45%)". That is an hourly
forecast reading, not a nowcast - it says nothing about whether rain is
starting in the next few minutes, which is the question that actually
matters at the helm or on deck right now. HelmCast's wall page had this: a
60-minute bar chart plus a status line ("Rain expected now" / "Rain expected
in 12 minutes" / falling back to the hourly and daily forecast), built on
Apple WeatherKit's `forecastNextHour` dataset.

Helmcentral's weather-provider plugin contract (`backend/weather_providers.go`)
had no minute-by-minute field at all - `fetch_forecast`'s output stopped at
`current`/`days`/`hourly`. Adding one meant deciding what a provider without
that resolution should do, and Helmcentral has exactly one keyless default
(Open-Meteo) plus one keyed reference plugin (WeatherKit) with genuinely
different nowcast resolutions: WeatherKit's `forecastNextHour` is
minute-by-minute; Open-Meteo's closest equivalent, `minutely_15`, is
15-minute-resolution and modelled rather than the short-range blend
WeatherKit's nowcast is.

## Decision

### 1. `next_hour` is optional, in the plugin's own resolution

`fetch_forecast`'s output gains `next_hour: [{time, precipitation_chance_pct,
precipitation_mm_per_h}]`, in time order, at whatever cadence the provider
has. Absent or empty means "no nowcast for this position" - never backfilled
from `hourly` inside the plugin (AGENTS.md's fallback policy: a provider
that doesn't supply next-hour data means "no nowcast", shown honestly, never
faked from something else without saying so). `precipitation_chance_pct`
keeps the same negative-is-absent convention every other
`precipitation_chance_pct` field in this contract already uses
(`sentinelPrecipitationPct`'s doc comment) - a provider whose nowcast has
intensity but no probability at this resolution sends a negative chance
rather than inventing one. The host does not require or normalize the
cadence: `buildWeatherNextHourResponse` (`backend/weather_providers.go`)
infers `step_minutes` from the gap between the first two points, so
WeatherKit's 1-minute and Open-Meteo's 15-minute data both reach the wire
correctly labelled, and a single point (no second point to diff against)
carries `step_minutes: 0`.

### 2. WeatherKit: `forecastNextHour`, mapped minute by minute

`weatherKitRequestURL` adds `forecastNextHour` to the merged `dataSets`
query. `forecastNextHour.minutes[]` (`startTime`, `precipitationChance` a
0-1 fraction, `precipitationIntensity` mm/hr) maps onto `next_hour` with the
same fraction-to-percentage conversion `precipitationChancePct` already
does for `current`/`days`/`hourly`. WeatherKit omits the whole
`forecastNextHour` key, rather than sending an empty `minutes[]`, outside
its nowcast coverage area - confirmed there is no test fixture for this in
this codebase or in HelmCast's, since neither had cause to request the
dataset from an uncovered position; `parseWeatherKitResponse` treats a nil
`ForecastNextHour` as "no next_hour data" either way, the same path an
empty array would take.

### 3. Open-Meteo: `minutely_15`, accepted as a real endpoint parameter first

Before writing any mapping, the plugin's planned request parameters were
tested live against `api.open-meteo.com` (lat -18.65, lon 146.48, and
several other positions, 2026-09-23): an invalid `minutely_15` variable name
gets a `400` "Invalid value" response, while `precipitation` and
`precipitation_probability` both return genuine, position-distinct,
non-duplicated values - confirming both are real, accepted API fields at
this resolution, not something this plugin needs the "not supplied"
sentinel for. Two live captures went into
`docs/examples/weather-plugins/open-meteo/testdata/` as real, unedited
fixtures: `open_meteo_response_minutely15_mackay.json` (dry at capture
time) and `open_meteo_response_minutely15_singapore_rain.json` (actively
raining, precipitation and chance both ramping across the window) - see
"Verify fixtures against live data" in this project's working notes for why
an assumed shape is not good enough here.

**This section originally went further and claimed the data was "confirmed
real at that resolution" - genuinely independent 15-minute observations,
everywhere.** That claim was wrong and is corrected by §7 below: "returns a
distinct, non-duplicated value" only rules out the crudest possible
fallback (repeating the same number, or literally copying the hourly
array). It does not rule out interpolation, which produces exactly this
kind of position-distinct, smoothly-varying output too. Open-Meteo's own
docs say plainly that `minutely_15` is native only over North America
(NOAA HRRR) and Central Europe (DWD ICON-D2 / Météo-France AROME), and
interpolated from the hourly model everywhere else - Mackay, this section's
own test position, is outside both regions, and a closer look at that same
fixture (§7) shows the interpolation directly.

`openMeteoRequestURL` adds `minutely_15=precipitation,precipitation_probability`
bounded to `forecast_minutely_15=8` (2 hours of 15-minute points).
Open-Meteo's undocumented default is 288 points/3 days if left unbounded -
confirmed live, and far more than a next-hour nowcast needs; 8 points leaves
enough cushion past 60 minutes that the display window (below) is always
covered even right at a 15-minute boundary. `precipitation` (mm per 15
minutes) is multiplied by 4 to reach this contract's mm/h.

### 4. The host does not trust the fetch time; the frontend does not either

A nowcast goes stale far faster than the hourly forecast around it - a
15-minute-cadence provider's window has fully aged out after an hour, and
even the backend's own weather cache can serve a bundle several minutes
old. The backend makes no special TTL exception for `next_hour`: it shares
the existing weather bundle's cache/TTL and is simply carried through to
`GET /api/weather-forecast`'s top-level `next_hour` field
(`{start, step_minutes, points}`, omitted rather than `null` when the
provider supplied none). All the staleness handling lives in the frontend,
and works off each point's own timestamp rather than elapsed time since
fetch - simpler than HelmCast's approach (which tracked `fetched_at` and
slid its array by elapsed minutes) because every point already carries a
real RFC3339 time: `buildNowcastBars` (`frontend/src/lib/nowcast.ts`) drops
any point that ended before "now" outright, and clips a point straddling
"now" (started before, hasn't ended yet) to start at offset 0 rather than
dropping it or letting it render before the "Now" tick.

### 5. `lib/nowcast.ts` reuses `nextRain`, does not reimplement it

`computeNowcastStatus` picks the status line in HelmCast's priority order:
the nowcast itself when `hasNowcastRain` finds a signal in the window. The
start is the first bucket with forecast rainfall (`mmPerH > 0`), not the
first with a positive chance, and the line names the hour's peak intensity
("Moderate rain expected in 20 minutes"); a window with chance but no
rainfall says "N% chance of rain in the next hour" instead of claiming rain
is on the way. Otherwise, otherwise the hourly
fallback, reusing `nextRain` (`lib/forecast-bands.ts`, threshold 1 rather
than its 40%-default "likely" gate, so it fires on any positive chance the
same way HelmCast's hourly fallback did) instead of a second hourly rain
finder; otherwise a daily fallback; otherwise "No rain expected in the next
6 days"; otherwise a structural dash when nothing in the cascade has real
data. `hasNowcastRain` treats a positive chance as signal on its own, and a
null (not-supplied) chance alongside positive intensity as signal too - null
chance alone is neither evidence of rain nor of dry.

One deliberate deviation from HelmCast here: HelmCast's daily fallback
reports an amount in mm (`daily.precipitation_amount`), because WeatherKit's
`forecastDaily` carries one. Helmcentral's `weatherForecastDayResponse` has
no per-day mm figure - only `precipitation_pct` and a text
`precipitation_summary` - and inventing an amount the provider never gave
would itself violate the fallback policy this ADR leans on elsewhere. The
daily tier here reports the day's chance-of-precipitation percentage
instead ("60% chance of rain Tuesday"). Adding a real per-day mm field to
the weather contract is future work, not bundled into this cycle.

### 6. The tile: hand-rolled SVG, drawn only when there is something to draw

The strip (`NowcastStrip` in `current-conditions-tile.tsx`) is hand-rolled
SVG per ADR 0012's rule for dashboard charts, not a charting library - one
bar per `next_hour` point (so 1-minute-wide bars for WeatherKit, 15-minute
for Open-Meteo), 0/20/40/60-minute ticks, coloured from the existing
`--chart-precip` token the forecast drawer's own precipitation bars already
use. Bar height comes from intensity (mm/h) only, never fabricated from
chance alone; a chance-only point still gets a faint fixed-opacity wash so
the strip does not read as silently empty. The strip is only rendered when
`computeNowcastStatus` reports real rain signal in the window - a dry
nowcast, or no nowcast at all, shows only the fallback status line, never an
empty strip (which would read as a confident "definitely dry" the provider
never actually said).

### 7. Addendum (2026-09-23): `next_hour_source`, and a preceding-window timing bug

Two problems turned up checking this ADR's Open-Meteo work against the
provider's own documentation and against live data at the boat (lat -18.65,
lon 146.48, the same Mackay position §3's fixture already captures):

- **`minutely_15` is not real 15-minute data everywhere.** §3 above
  concluded `precipitation_probability` was "real data at 15-minute
  resolution" because requesting it never 400s and returns position-distinct
  values. That conclusion doesn't follow: Open-Meteo's own forecast API docs
  state `minutely_15` is natively modelled only over North America (NOAA
  HRRR) and Central Europe (DWD ICON-D2 / Météo-France AROME), and
  *"interpolated from 1-hourly to 15-minutely"* everywhere else -
  `precipitation_probability` additionally has no documented `minutely_15`
  entry at all, in any region (only the hourly resolution documents one).
  Mackay - outside both native-resolution regions - confirms this directly:
  its `minutely_15.precipitation_probability` steps smoothly between the
  surrounding hourly readings (84, 82, 80 -> 84, 83, 83, 82, 82, 81...),
  exactly the shape interpolation produces, and its `minutely_15.precipitation`
  read `0.0` at a point where the hourly figure was `0.1`. The strip was
  never wrong to draw from this data (the operator's decision here: keep
  doing so), but presenting it as an equal, independent nowcast next to
  WeatherKit's genuine minute-by-minute data was.

  **Fix:** the plugin contract gains `next_hour_source: "nowcast" | "hourly"`
  alongside `next_hour`, required whenever `next_hour` is non-empty
  (`mapWasmFetchForecastOutput`, `backend/wasm_weather_provider.go`, fails
  fast on a missing/unrecognized value - the same "never silently default"
  policy this contract already applies to every other required field).
  WeatherKit always sends `"nowcast"` (`forecastNextHour` has no interpolated
  case). Open-Meteo's plugin computes it per request from the response's own
  echoed lat/lon (`nextHourSourceForPosition`, `open-meteo.go`) against two
  conservative bounding boxes - drawn slightly inside each model's own
  documented grid extent, since a position near a model's edge is exactly
  where "nowcast" is the least defensible claim:

  - North America / NOAA HRRR: documented CONUS grid roughly 21.1-52.6N,
    134.1-59.1W; box used: 22.0-52.0N, 133.0-60.0W.
  - Central Europe / DWD ICON-D2 (+ AROME): ICON-D2's documented public grid
    43.18-58.08N, 3.94W-20.34E; box used: 43.5-58.0N, 3.5W-20.0E.

  `GET /api/weather-forecast`'s `next_hour` object carries it through as
  `source`. The frontend (`useWeatherForecast`) exposes it on
  `WeatherNextHour.source`, defaulting an unrecognized/missing wire value to
  `"hourly"` (the honest/uncertain direction, never `"nowcast"`).
  `computeNowcastStatus` (`lib/nowcast.ts`) appends `" (hourly forecast)"` to
  the nowcast-tier status line when `source === "hourly"` and the strip is
  showing (`isHourlySourced`) - the hourly/daily *fallback* tiers are left
  alone, since their own phrasing ("rain expected after 4PM") doesn't
  misrepresent its resolution the way an uncaptioned nowcast-style line
  would. `NowcastStrip` (`current-conditions-tile.tsx`) draws a small
  `text-[10px] text-muted-foreground` "Hourly forecast" caption absolutely
  positioned over its own SVG box (zero added flow height, so the tile's h7
  placement and minH floor are unaffected) and the same caption folds into
  the strip's own `aria-label`.

- **Open-Meteo's `minutely_15` timestamps were one step late.** Open-Meteo
  documents `minutely_15.precipitation` as a *"Preceding 15 minutes sum"* -
  the value at timestamp T covers `[T-15min, T)`, counting backward - but
  this contract's `next_hour` says a point's `time` marks the START of
  forward coverage, `[time, time+step)` (now stated explicitly in
  `backend/weather_providers.go`'s top doc comment, which previously left
  this meaning implicit). The plugin passed Open-Meteo's timestamp straight
  through, so rain that had been falling since 12:00 - reported at
  Open-Meteo's `T=12:15` point - showed up at 12:05 as "expected in 10
  minutes" instead of "already falling". **Fix:** the plugin now emits each
  point's `time` as Open-Meteo's own timestamp minus 15 minutes
  (`open-meteo.go`'s `minutely_15` mapping). `precipitation_probability` has
  no separately documented convention at this resolution (it isn't
  documented at all - see above), so the same shift is applied to it too via
  the shared per-point `time`, the only consistent treatment available given
  the contract carries one timestamp per point. **Checked and left
  unchanged:** WeatherKit's `forecastNextHour.minutes[].startTime` is
  already the start of the minute it describes (Apple's own naming), so its
  mapping needed no change.

Both fixes are covered by the existing real Mackay/Singapore fixtures
(now asserting `next_hour_source: "hourly"` and the shifted timestamp) plus
new tests that reuse the real Mackay fixture's `minutely_15` payload with
its echoed lat/lon overridden into each native-resolution region
(`nextHourSourceForPosition` is purely geometric, so this validates the
region gate without needing a live capture from either region).

## Consequences

- Both reference weather plugins now report a next-hour nowcast where their
  upstream API has one, at whatever cadence they actually have - the host
  and frontend handle both a 1-minute and a 15-minute provider without
  either needing to know about the other's resolution.
- Current Conditions' bottom half now answers "is it about to rain" first,
  falling back to the hourly/daily forecast only when the nowcast has
  nothing to say - closing a real gap against HelmCast's old wall page.
- The daily fallback line reports a percentage, not an amount, because
  Helmcentral's weather contract has no per-day mm figure. A future ADR
  adding one should update this tier to match HelmCast's wording.
- Nowcast staleness is handled entirely by comparing each point's own
  timestamp to the viewer's clock, with no new cache/TTL machinery on the
  backend - a nowcast several minutes old (a cached bundle) degrades
  gracefully by dropping expired points rather than needing a shorter TTL.
- (§7) Outside North America and Central Europe, Open-Meteo's nowcast strip
  is now honestly labelled as interpolated-from-hourly rather than presented
  as an equal-footing short-range nowcast - which is most of the world,
  including this boat's own home waters. The strip still draws (the
  operator's explicit call), it just no longer overclaims what it is.
- (§7) Every `next_hour` timestamp this system has ever shown from
  Open-Meteo was one 15-minute step later than the data it labelled -
  "expected in 10 minutes" for rain already falling. Fixed at the source
  (the plugin), not patched in the frontend, so the contract's "time is the
  start of forward coverage" rule holds for every current and future
  provider without a per-provider special case downstream.

## Related

- ADR 0092 (wall-display tiles), which placed `current-conditions-tile.tsx`
  and established the stale-source/structural-dash conventions this tile
  still follows.
- ADR 0012 (hand-rolled SVG gauges), the rule the nowcast strip follows.
- ADR 0035 (weather local-day boundaries), the precedent for the
  negative-is-absent `precipitation_chance_pct` convention `next_hour`
  reuses.
- ADR 0125 (forecast-conditions tile and trip ETA), the tile-merge cycle
  immediately before this one on the same branch.
