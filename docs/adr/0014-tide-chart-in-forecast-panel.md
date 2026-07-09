# ADR 0014: Tide Chart Moves Into the Forecast Panel, Day-Scoped

## Status
Accepted

## Context

The tide chart lived in its own standalone "Tides" sidebar panel (`TideDrawer`), a separate navigation-level entry alongside Forecast, Routes, Charts, Radar, Anchor Watch, and Settings. Tide state is forecast-adjacent marine data in exactly the same sense as wind, wave, precipitation, and cloud/temperature — all of which already live as chart cards inside one Forecast panel, day-scoped to whichever of the 10 forecast days the operator has selected. Tide was the odd one out: its own panel, its own nav entry, and a chart windowed as a fixed 96-hour rolling span from `Date.now()` rather than scoped to a selected day.

Folding Tide into Forecast removes a redundant navigation surface and puts all forecast-adjacent charts (Wind, Wave, Precipitation, Cloud & Temperature, Tide) in one place, selectable by the same day-tabs row. That relocation is not just a cut-and-paste: `TideChart`'s windowing, "Now" marker, phase-classification reference, and axis ticks were all hardcoded around a fixed 96-hour window from "now," which only made sense as the sole content of a standalone panel. Sitting among day-scoped siblings, it needed to become day-scoped too.

## Decision

### `TideChart` takes an explicit day window instead of computing one from `Date.now()`

`TideChartProps` gained `windowStart: Date` / `windowEnd: Date`, replacing the implicit `now` / `now + WINDOW_HOURS` (96h) internals. The "Now" dashed line/dot/label only renders when real time (`Date.now()`) actually falls within the given window (`showNowMarker = nowMs >= chartStartMs && nowMs < chartEndMs`) — so today's card shows it and every other day's card doesn't, with no extra prop needed since it's derived from real time vs. the passed-in window. Spring/neap phase classification (`classifyTidePhase`) uses `Date.now()` as its reference instant when showing today, and the window's midpoint otherwise, since a non-"now" day still needs *some* representative instant to classify against. The old weekday-labeled multi-day boundary ticks were replaced with hour ticks at 0/6/12/18/24h offsets from the window start, formatted as "12AM/6AM/12PM/6PM" — matching the convention every sibling chart in `forecast-drawer.tsx` already uses, for visual consistency now that Tide sits directly among them.

The empty-state check changed from chart-wide (`chart.extremes.length > 0`) to window-scoped (`hasExtremesInWindow`, checking whether any extreme falls inside `[windowStart, windowEnd)`), rendering the same `data-testid="forecast-tide-unavailable"` / "Tide forecast unavailable for this day" idiom the other charts already use for their own per-day gaps. This is what covers the real data-availability gap without any special-casing: BOM's provider fetches roughly 8 days of extremes starting yesterday, and Stormglass fetches today through +7 days, so both cover only the first ~7-8 of the forecast's 10 days. Days 8-10 simply have no extremes in their window and fall through to the same "unavailable" message every other chart uses for a data gap — not an error, and not a change from what the old fixed-window chart would have shown for those days either (it also had no data to plot that far out).

### The target day's window is derived from the local clock, not from `ForecastDay.date`

`ForecastDay.date` is a display string (e.g. "Jul 9"), not a reliably parseable ISO date. Since `selectedDayIndex` is already a simple offset into `forecast.slice(0, 10)`, and day 0 is always "today" for a sequential daily forecast, the window is computed directly as `startOfDay(new Date()) + dayOffset days` — sidestepping the display string entirely, and exactly consistent with the *old* code's own assumption that the device clock's "now" represents "today."

### Tide's data-fetching stays self-contained, diverging from the rest of the app's pattern

Every other panel's data (vessel state, weather forecast, routes, etc.) is fetched once in `App.tsx` and passed down as props. The new `ForecastTideSection` breaks that pattern deliberately: it owns `useTideSettings()`, the BOM auto-detect effect (`fetch('/api/tide-nearest')`), `useTideChart()`, and the "Change Station" picker toggle internally, exactly as `TideDrawer` did before it. Lifting this into `App.tsx` would mean threading tide settings/chart state and the auto-detect effect through the top-level component for a single card used in one place, with no other consumer — not worth the churn for a self-contained, single-use section. This mirrors a decision already made once for `TideDrawer` itself; relocating it doesn't change that reasoning.

### Placement: last card, after Precipitation

`ForecastTideSection` is inserted as the 5th and final chart card in the Forecast panel's details card, immediately after Precipitation, using the same `mt-3 rounded-md border bg-card/70 p-2` convention as its siblings and passing through the already-selected `dayOffset={selectedDayIndex}`. It was placed last (least frequently the primary reason someone opens Forecast, compared to wind/wave/rain) rather than reordering the existing four charts.

### Accepted caveat: gated behind `hasForecast` like every other section

`ForecastDrawer` has three whole-panel early returns (loading-without-data, error-without-data, no-data) that execute before the details card — including the new Tide card — is reached. This is consistent with the panel's existing design (every other chart card is equally unreachable until weather-forecast data has loaded at least once), not a new limitation introduced by this change.

## Consequences

Positive:
- One fewer top-level nav entry; all forecast-adjacent marine charts (Wind, Wave, Precipitation, Cloud & Temperature, Tide) now live in a single panel, switched by the same day-tabs row.
- The tide chart's window, "Now" marker, and ticks are now visually and behaviorally consistent with its new siblings, instead of being the one outlier chart with its own fixed 96-hour rolling window.
- The "unavailable for this day" gap (days 8-10, beyond either tide provider's coverage) is handled by the same idiom already used elsewhere, with no new special-casing.

Negative / explicitly deferred:
- `ForecastTideSection` is the one section in the Forecast panel that owns its own data-fetching rather than receiving props from `App.tsx`, diverging from the rest of the app's pattern — an accepted, scoped exception rather than a new general pattern.
- The Tide card is unreachable until the weather forecast has loaded at least once (`!hasForecast`), same as every other chart in this panel — not a new limitation, but worth naming since Tide data itself may be ready before the weather forecast is.
- Viewing tide data for days 8-10 of a 10-day forecast will always show "unavailable" given current provider coverage; extending either provider's fetch window was out of scope for this change.

## Related

- ADR 0013: Multi-Page Dashboard — unrelated mechanically, but the same sidebar (`PANEL_NAV_ITEMS` in `App.tsx`) that page is built around loses its "Tides" entry here.
