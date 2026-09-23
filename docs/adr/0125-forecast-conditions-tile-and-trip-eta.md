# ADR 0125: Forecast Conditions Tile And Trip ETA

## Status
Accepted. Supersedes ADR 0092 §5 (the forecast-days/sea-state tile split) and §2 (the clock tile's next-waypoint ETA).

## Context

Two problems showed up once the wall-display tiles from ADR 0092 had been
living on the boat for a while:

- `forecast-days-tile.tsx` (five day cards) and `sea-state-tile.tsx` (a
  wind/wave chart over the same five days) were always placed as a stacked
  pair, on both the Flybridge wall layout and everywhere else anyone tried
  them, because a card row with no chart under it or a chart with no cards
  above it reads as half a forecast. Two independently-sized widgets that
  are always used together is exactly the case ADR 0109's "widget vs tile"
  distinction doesn't help with: they are always the same tile,
  conceptually, and the operator had no way to say so.
- Measuring the pair against each other in a real browser (not just
  vitest's jsdom, which has no layout engine) showed the card boundaries
  and the chart's dashed day-boundary lines did not actually line up - close
  enough to look intentional, wrong enough to read as sloppy once you
  looked for it. Chasing that down turned up two real bugs, not one
  eyeballing problem: `sea-state-chart.tsx`'s day-boundary math derived its
  day count from `series.length`, so a forecast with fewer than five real
  days *compressed* the columns instead of leaving the missing days blank;
  and the chart's own `<Customized>` glyph layer and wave-steepness
  gradient computed pixel positions from `margin.left` alone, when Recharts
  actually adds each `<YAxis>`'s own `width` on top of the chart's `margin`
  rather than inside it. The real left inset of the plotted data is
  `margin.left + yAxisWidth` (80px, not 40, at this chart's settings),
  confirmed by rendering the chart and reading the actual pixel `x` of its
  own `<Line>` path and `<ReferenceLine>` elements rather than by assuming
  Recharts' internals matched the margin object. Both bugs were invisible
  to the existing test suite, which only asserted glyph *counts*, never
  their pixel positions.
- Separately, an operator running a chartplotter go-to (SignalK's `nextPoint`,
  set from the plotter's own UI, no Helmcentral route involved) found the
  clock tile's ETA line permanently blank. `useRouteActivation` and
  `lib/next-waypoint.ts` only ever looked at `activeRoute`; a `nextPoint`
  with no `activeRoute` behind it was invisible to the tile even though
  SignalK's Course API reports it just as readily. HelmCast's old wall page
  showed an ETA for wherever the plotter was steering toward, not only for
  a route planned inside Helmcentral, and losing that on migration was a
  regression, not a simplification.
- Also on the boat: the clock tile's ETA line clipped off the bottom of the
  wall display at its placed height. The tile's three stacked blocks (time,
  sun/moon, place chip) plus a fourth un-grouped ETA paragraph measured
  taller in a real browser than the tile's h7 placement (and its own minH 6
  floor) ever gave it - again invisible to jsdom, which cannot report a
  real overflow.

## Decision

### 1. One tile: `forecast-conditions`

`forecast-days` and `sea-state` are retired from `DASHBOARD_WIDGET_IDS`,
`validDashboardWidgetIDs` (`backend/dashboard_pages.go`) and every registry
that listed them (`WIDGET_CONSTRAINTS`, `DASHBOARD_WIDGET_DEFAULT_SIZE`,
`DASHBOARD_WIDGET_CATEGORY`, `App.tsx`'s `renderWidget`). A single
`forecast-conditions` id (display name "Forecast Conditions", category
`weather`) replaces both, rendered by the new
`components/forecast-conditions-tile.tsx`: the five day cards on top, the
sea-state chart below taking the tile's remaining height and full width.
Default size `w:6 h:7`; `WIDGET_CONSTRAINTS` floor `minW:4 minH:6` - wider
than either half's old floor, since a five-column card row and a two-axis
chart both need more width than either needed alone.

Both old ids also went into `retiredDashboardWidgetIDs` (same map
`rode-scope` uses, ADR 0047's strip-on-load mechanism): the boat's own
"Wall: Conditions" page still held both, and without this a saved page
holding either id 400's on every PATCH - `validateDashboardWidgets` rejects
the whole request over one unknown id, not just the widget carrying it, so
the page couldn't be edited at all until this landed. `loadDashboardPages`
strips the two ids from that page's `widgets` array the next time the
backend starts, and rewrites the file - it does not re-place
`forecast-conditions` on that page automatically, since there is no single
correct position to put it in; the operator adds it by hand, once, the same
way they'd place any other tile.

`lib/sea-state-series.ts`'s `buildSeaStateSeries` gained a `startDay`
parameter (default 0, the tile passes 1): it slices `forecastDays` starting
there rather than the tile slicing the *result* afterward, so the series'
own `index`/`dayKey` pairing stays keyed to the days actually shown instead
of to `forecastDays[0]` (today), which the tile never displays. This is
also what makes the card row and the chart provably show the same five
days: the tile computes `forecast.slice(1, 6)` for its cards and
`buildSeaStateSeries(forecast, waveForecastDays, 5, 1)` for its chart from
the same `forecast` array and the same offset, rather than two independent
slicing decisions that could silently drift apart.

### 2. Alignment by construction, not by eyeballing

`sea-state-chart.tsx` now takes an explicit `dayCount` prop (default
`SEA_STATE_DAY_COUNT = 5`) and computes its x-domain, day-boundary
`<ReferenceLine>` positions, midday tick positions and glyph spacing from
`dayCount * 24` total hour-slots - never from `series.length`. A forecast
with fewer than five real days therefore still divides the chart into five
equal columns, the trailing ones simply undrawn, instead of stretching
what real data exists across the whole width. `SeaStatePoint`'s existing
"real days only, no fabricated padding" rule in `buildSeaStateSeries` is
unchanged; the padding described here is purely which fraction of the
*pixel width* each real day's data is drawn across, not a change to what
data exists.

The chart exports two pure functions, `xForIndex(index, width, dayCount?)`
and `xForDayBoundary(day, width, dayCount?)`, as the one source of truth for
where an hour-slot centre or a day boundary lands in pixels - used
internally by the `<Customized>` barb/arrow layer and the wave-steepness
gradient's stop offsets, and unit-tested directly against a rendered
chart's own `<Line>`/`<ReferenceLine>` pixel output rather than only
indirectly through glyph counts. That direct check is what caught the
YAxis-width bug described above: both functions correctly account for
`SEA_STATE_PLOT_INSET` (`margin.left/right + the y-axis's own width`, 80px
each side at this chart's settings), a distinct constant from
`SEA_STATE_CHART_MARGIN` (the object literally passed to `<ComposedChart>`'s
`margin` prop and each `<YAxis>`'s `width`) precisely because Recharts adds
the two rather than nesting one inside the other.

`forecast-conditions-tile.tsx`'s card row is inset horizontally by
`SEA_STATE_PLOT_INSET.left/right` as inline `paddingLeft`/`paddingRight` -
the same constant the chart's own geometry now correctly uses - so the two
can never drift apart by a hand-tuned number again. The row is a gapless
`grid-cols-5` with each cell supplying its own visual gap via `px-1` rather
than a `gap-*` on the grid itself, which is what puts a card boundary at
the exact pixel fraction `xForDayBoundary` predicts for any tile width, not
just the ones someone happened to test at.

### 3. The clock tile's ETA is for the trip, not the next leg

`backend/route_activation.go`'s `fetchSignalKCourseStatus` now also parses
the Course API's `nextPoint.position`/`nextPoint.type`, independent of
whether `activeRoute` is set. `GET /api/routes/active` gains a
`destination: {lat, lon, name}` key when there is no active route but the
plotter still has a `nextPoint` set (omitted entirely, not `null`, when
there is no next point at all). `name` comes from the same cached
place-name machinery the vessel's own position uses
(`backend/place_name.go`'s `resolvePlaceName`/`placeNameCache`/backoff) via
a new `resolveDestinationPlaceName`: cache-first, and a cache miss starts
exactly one background resolve (its own single-flight guard, deliberately
separate from the vessel tick's, so a slow destination lookup can never
starve the tick's own resolution of its turn or vice versa) rather than
blocking the 15s poll on a live provider round trip.

`lib/next-waypoint.ts` gained `etaToRouteEnd` (ETA to a Helmcentral route's
FINAL waypoint in traversal order - the vessel's distance to the current
target plus every remaining leg to the end, same SOG-or-planning-speed
basis rule as the existing `etaToWaypoint`) and `etaToDestination` (ETA to
a bare chartplotter destination, SOG only - there is no route behind it and
therefore no planning speed to fall back to, so a becalmed boat gets no
honest ETA here even though the same boat would still get one, on plan
speed, toward an actual route). `useRouteActivation`'s `inactive` status
variant carries the destination through (`destination: {lat, lon, name} |
null`) so `App.tsx` can build whichever of the two the current situation
calls for - route active, chartplotter destination only, or neither - and
hand the clock tile one label/etaAt/basis triple either way. A destination
with no resolved name yet shows the ETA with no label ("ETA 11:13") rather
than inventing one.

`clock-tile.tsx`'s ETA line now also prefixes a 3-letter weekday
(`formatWeekday`/`isSameLocalDate`, new exports of `use-vessel-identity.ts`,
same vessel-local-zone-aware pattern as `formatClock`/`formatDate`) when the
ETA falls on a different vessel-local calendar day than "now" - a multi-day
passage's arrival no longer looks like it's landing today just because the
line doesn't say otherwise. HelmCast's old wall page did the same thing.

### 4. The clock tile's layout fix

The hero time drops one Tailwind step (`text-7xl` to `text-6xl`) and the
place-name chip and the ETA line now share one rounded box - the ETA as a
second line inside the chip - rather than the ETA sitting in its own
margined paragraph below a separate chip. Both trims exist for one reason:
measured in a real browser (not jsdom, which has no layout engine and
could not have caught this), the tile's four stacked blocks were taller
than the tile's placed height (h7 on the wall, minH 6 as its resize floor)
and clipped the ETA line off the bottom of the display. The time stays the
single largest thing in the tile; it is one step smaller, not small.

### 5. Smaller barbs and wave arrows in the tile

`direction-glyphs.tsx`'s `WindBarb` and `WaveDirectionArrow` take a `scale`
prop (default 1, which keeps the forecast drawer's doubled glyphs exactly as
they were). The tile's chart draws them at `SEA_STATE_GLYPH_SCALE = 0.6`,
strokes included. At full size they crowded a chart that now shares its
height with the day-card row, and read louder than the wind and wave lines
they annotate.

### 6. What "destination" means for a TimeZero route

Checked live on 2026-09-23 with a route activated in TimeZero: SignalK
receives only `RMB`, `APB` and `XTE` from the Vesper Cortex. There are no
`RTE`/`WPL` sentences and no route PGN, so `navigation.currentRoute`
(which HelmCast read) does not exist and the Course API holds a bare
`nextPoint` of type `Location`, with `activeRoute` null. The trip ETA in
that case is therefore the ETA to the route's current waypoint, and moves
on as TimeZero advances it. Getting the whole route to SignalK is an
instrument-network change (TimeZero's route output, or whatever filters it
before SignalK), not something this tile can derive.

HelmCast's polar-aware ETA (a model learned from Influx history, blended
with a forecast-driven speed factor per leg) is deliberately not ported
here: the ETA stays remaining distance over SOG. Porting it is its own
cycle.

## Consequences

- One widget id to place, resize and reason about instead of two that only
  ever made sense together; a shorter forecast (fewer than five real days)
  degrades the same way on both halves of the tile now, instead of the
  card row showing dash cards while the chart silently stretched two real
  days across a five-day-wide plot.
- The chart's own glyph and gradient positions are pixel-correct against
  its axis ticks and reference lines for the first time - a latent bug
  (misaligned by a full y-axis width) that shipped with ADR 0092 and was
  invisible to a test suite that only checked glyph counts.
- The clock tile reports an ETA for a chartplotter go-to now, not only for
  a route activated inside Helmcentral - closing a real regression against
  what HelmCast's wall page used to show.
- The destination place name adds one more consumer of the shared
  place-name cache/backoff machinery, on its own single-flight guard so it
  cannot compete with the vessel tick's own resolution for the same
  in-flight slot.
- The two retired widget ids are stripped automatically (ADR 0047's
  mechanism) so no saved page can 400 a PATCH over them, but there is no
  automatic replacement: the one page that placed them loses both tiles on
  the next backend start and needs `forecast-conditions` placed by hand.

## Related

- ADR 0092 (wall-display tiles), superseded in part by this one - see that
  ADR's Status line.
- ADR 0109 (tiles not widgets), the vocabulary this ADR's "always the same
  tile" framing for the forecast-days/sea-state merge is stated in.
- ADR 0056 / ADR 0101 (place-name resolution ladder and cache), the
  machinery `resolveDestinationPlaceName` reuses rather than duplicating.
