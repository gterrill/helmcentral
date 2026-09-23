# ADR 0092: Wall-Display Tiles

## Status
Accepted. Superseded in part by ADR 0125.

## Context

ADR 0089 got an existing dashboard page onto the wall display's rotation,
but the dashboard itself has no clock, no multi-day forecast, no wave
chart, and no moon or sun times: those live only in the forecast drawer, a
2400-line panel nobody was going to duplicate onto a 1920x360 strip by
hand. HelmCast's own wall display carried exactly this content (time,
weather, waves) on its main page, and this phase is what lets that page
retire in favour of Helmcentral's own tiles.

Facts that shaped the design:

- The forecast drawer already has the glyph vocabulary this needs: wind
  barbs, wave-direction arrows, a weather-condition icon, moon phase labels
  and emoji. All four were private to that one file. Extracting them into
  `lib/moon-phase.ts`, `components/forecast/weather-condition-icon.tsx` and
  `components/forecast/direction-glyphs.tsx` (pure moves, no behaviour
  change) is what lets the new tiles draw the same glyphs instead of
  reinventing a second wind-barb renderer that could quietly drift from the
  drawer's.
- `today-now-tile.tsx` had its own copy of `fahrenheitToCelsius`, unaware
  that `lib/units.ts` already exported the identical function (the forecast
  drawer already imports it from there). No new `lib/temperature.ts` was
  needed or created; fixing the duplicate to import the existing shared
  function was the smaller, more honest change.
- ADR 0012 keeps every dashboard gauge hand-rolled SVG, no chart library.
  The wall-display tiles mostly follow that rule (`BulletGauge`, a bullet
  chart in `gauge-tile.tsx`'s `BarGauge` box), but the sea-state chart is
  the one dashboard chart drawn with Recharts, because a five-day, two-axis
  time series with barbs and arrows overlaid is not something an SVG bar
  gauge's vocabulary can express, and the forecast drawer already carries
  Recharts as a dependency for exactly this kind of chart.
- `backend/route_activation.go` passes SignalK's `pointIndex`/`reverse`
  through with no transformation of its own. Confirmed against
  signalk-server's course-provider-plugin reference implementation: when
  `reverse` is false, `pointIndex` indexes the route's waypoint array in
  its authored order directly; when `reverse` is true, the same index
  counts from the *end* of that array instead (`waypoints[length - 1 -
  pointIndex]`). The frontend has to apply this itself, once, in
  `lib/next-waypoint.ts`.
- `frontend/src/lib/dashboard-widgets.ts` already listed `radar-targets` as
  a real, renderable widget, but `backend/dashboard_pages.go`'s
  `validDashboardWidgetIDs` never included it: a latent 400 on save for any
  page that placed it. Found and fixed while adding the four new ids to the
  same map, since it is the identical bug.

## Decision

### 1. Four extractions, no behaviour change

`lib/moon-phase.ts` (`MOON_PHASE_LABELS`, `MOON_PHASE_EMOJI`,
`moonPhaseLabel`, `moonPhaseEmoji`), `components/forecast/weather-condition-icon.tsx`
(`WeatherConditionIcon`, replacing the old `getWeatherIcon` function with an
equivalent component) and `components/forecast/direction-glyphs.tsx`
(`WindBarb`, `WaveDirectionArrow`, verbatim, `data-testid`s intact) all
moved out of `forecast-drawer.tsx`, which now imports them back. The
drawer's own colour-literal and arbitrary-font-size guard tests
(`forecast-drawer.test.tsx`) were extended to scan all three extracted
files as well, so a raw hex value or a one-off `text-[13px]` creeping into
any of them fails the suite exactly as it would inside the drawer itself.

### 2. `lib/next-waypoint.ts` resolves `pointIndex` once

`nextWaypoint(route, status)` builds a traversal-order array (`reverse ?
[...waypoints].reverse() : waypoints`) and indexes it by `pointIndex`,
clamped into range rather than throwing on a stale or out-of-range value.
`etaToWaypoint(...)` picks SOG when it is above 0.5kt, otherwise the
route's planning speed, and returns a `null` ETA (never a fabricated time
or an `Infinity`) when the resolved speed cannot produce a finite arrival:
a genuinely becalmed boat with no planning speed set has no honest ETA to
report, and the clock tile shows the waypoint's name with a dash for the
time in that case rather than hiding the whole line.

### 3. Forecast bands are neutral ranges, never severity zones

`lib/forecast-bands.ts`'s `next24hWindBand` and `todayTempBand` feed
`BulletGauge`'s band prop, which paints at `hsl(var(--muted-foreground) /
0.25)`, a plain, low-opacity range marker, not one of the alarm-severity
colours `BarGauge`'s zones use. A forecast range is context for a live
reading, not a warning about it; colouring it like one would teach the eye
to read "outside the neutral band" as an alarm when it is, on most tiles,
simply "windier than the last 24 hours were." `nextRain` follows the same
no-data-is-not-no-rain rule the rest of this codebase holds to: it returns
`null` (not `'none'`) when the forecast has no precipitation-chance data at
all in the window, and only returns the literal `'none'` when real data was
present and every hour of it stayed under the threshold.

### 4. `BulletGauge` is hand-rolled SVG, same box as `BarGauge`

`components/ui/bullet-gauge.tsx` uses the identical `viewBox="0 0 100 8"`
`BarGauge` (`gauge-tile.tsx`) already uses, per ADR 0012. It adds two
things `BarGauge` doesn't need: a neutral band (see above) and up to two
reference-marker ticks (first solid, second dashed), both because the
wall-display tiles compare a live reading against a known range rather
than only showing which zone a value currently sits in.

### 5. Five days, tomorrow is day one

`forecast-days-tile.tsx` shows `days.slice(1, 6)`: tomorrow through five
days out. Today is the current-conditions tile's job, matching the split
HelmCast's own main page already made between "now" and "the week". Fewer
than five real days always render five card slots regardless, the missing
ones as dash cards: hiding them would mean the tile changes shape
depending on how much of the forecast has loaded, which is a worse failure
mode than an honest dash.

### 6. The sea-state chart: the one Recharts chart on the dashboard

`lib/sea-state-series.ts` joins the weather and wave forecast hooks into
one 120-point (5 days x 24 hours) series by `dayKey`, purely: a day with no
matching wave entry still emits 24 points, wind fields populated and every
wave field `null`: a dead wave feed for one day must not make the whole
series disappear, and a missing hour must never read as a fabricated calm
(`0`) instead of `null`. When the weather forecast itself has fewer real
days than the five requested, the series stops at however many exist
rather than padding fabricated all-null days onto the end: a day that
hasn't arrived yet is a different thing from a day whose feed died, and
only the latter earns the null-fields treatment.

`components/forecast/sea-state-chart.tsx` is purely presentational: wind
and gust on the left axis, wave height on the right coloured by steepness
band exactly the way the forecast drawer's own wave chart is (the same
four-colour, hard-edged-gradient technique, duplicated locally as a
four-entry colour map rather than exported from the drawer, since coupling
a wall-display chart to that file's internals for four colours was the
worse trade), a wind-barb and wave-direction-arrow row every three hours,
and a dashed boundary between each pair of adjacent days. `connectNulls`
is explicitly `false` on every series, so a dead hour draws as a visible
gap rather than a line quietly bridging over it. Day-name tick labels are
derived from each day's own `dayKey` (parsed as UTC midnight, formatted
with an explicit `timeZone: 'UTC'`) rather than a separate label prop, so
the component's signature stays the four props the chart actually needs:
`series`, `width`, `height`, `waveUnit`.

Positioning the barb/arrow row and the day boundaries uses the same
fixed-margin arithmetic the forecast drawer already relies on for its own
overlay glyphs, rather than reading Recharts' internal x-scale: the
`<Customized>` layer needs real pixel coordinates, and matching the same
margin object passed to `<ComposedChart>` keeps the two in agreement
without depending on an internal API that could change across a Recharts
version.

`sea-state-tile.tsx` measures its own content box with a ref and a
`ResizeObserver`, the same technique `useFitScale` (`lib/cluster-canvas.ts`)
uses for the engine cluster canvas, adapted to capture both width and
height since this chart draws at the tile's real pixel size rather than
scaling a fixed design down. A wave-feed error shows the forecast drawer's
own "Wave data unavailable" message in place of the chart; a failed fetch
is not an empty series, and drawing a flat, empty chart in its place would
read as "no waves today" rather than "the wave feed failed".

### 7. Registration, and the `radar-targets` bug it exposed

`clock`, `current-conditions`, `forecast-days` and `sea-state` join
`DASHBOARD_WIDGET_IDS`, get an entry each in `WIDGET_CONSTRAINTS` sized to
what their content actually needs (`{2,6}`, `{3,6}`, `{3,4}`, `{4,5}`, all
comfortably inside the kiosk's seven-row, 344px fold), and a case each
in `App.tsx`'s `renderWidget`. `backend/dashboard_pages.go`'s
`validDashboardWidgetIDs` gained the same four ids, plus `radar-targets`,
which had been a real, placeable frontend widget with no backend
counterpart since before this change: a page that placed it got a 400 on
save with nothing in the UI to explain why. A new vitest test reads
`backend/dashboard_pages.go` off disk and asserts its map's keys equal
`DASHBOARD_WIDGET_IDS` exactly, the same "read the source directly so drift
fails the suite immediately" technique the forecast drawer's colour-token
guards already use, so this specific class of bug cannot reoccur silently.

### 8. The frameless embed, unblocked

`EmbedWidgetConfig` gained `frameless?: boolean`. When set and the
dashboard is not in layout-editing mode, `embed-tile.tsx` renders the
iframe in a plain container with no `Tile` title bar and no padding,
falling back to the framed rendering whenever editing (the gear and title
need to stay reachable) or whenever the embed has no usable URL yet (there
is no chrome to drop from an empty state that still needs a way to
configure it). This is what lets helmcam's camera feed fill an entire
wall-display page instead of losing a third of its height to a title bar
it has no use for.

## Consequences

- A wall display can now show a clock, current conditions, a five-day
  outlook and a sea-state chart with no code beyond placing four widgets on
  a page, the same widget picker and the same layout editor as any other
  tile.
- Every wall-display tile that touches forecast data inherits the
  null-means-no-data discipline `lib/forecast-bands.ts` and
  `lib/sea-state-series.ts` enforce: a dead feed reads as a dash or a gap,
  never as a fabricated calm, zero, or "no rain".
- The forecast drawer's glyph vocabulary (wind barbs, wave arrows, the
  condition icon, moon phase) is now shared code with a guard test
  extended to cover it, rather than something only that one file could
  draw correctly.
- `radar-targets` and the four new tiles both now round-trip through save
  correctly; the parity test guards against either list drifting from the
  other again.
- The frameless embed and the four tiles remain independent of Phase 3
  (the points-of-interest plugin kind and its widget), tracked separately.

## Related

- ADR 0012 (hand-rolled SVG gauges), the rule `BulletGauge` follows and the
  sea-state chart is the documented exception to.
- ADR 0089 (kiosk feed is a page flag), the phase this one builds tiles for.
- ADR 0068 (stale-source marking on tiles), which `current-conditions-tile.tsx`
  follows for its own staleness badge.
- ADR 0073 (humidity and visibility come from the provider, never computed
  arithmetic), the same "an omitted field must stay distinguishable from a
  real zero" discipline `lib/forecast-bands.ts` and `lib/sea-state-series.ts`
  both carry forward for wind, temperature and wave data.
