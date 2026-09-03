# ADR 0071: Upper-Air (500mb) Outlook

## Status
Accepted

Extends ADR 0070 (heavy-weather indicators). Adds a fifth provider category alongside ADR 0018's four.

## Context

ADR 0070 encoded the surface half of *Surviving the Storm*. This is the upper-air half, and the book is emphatic that it is the more important one (p61): "Of all the skills you can acquire to keep yourself out of difficult weather, understanding what happens at the 500mb level is the most important."

The reason is mechanical rather than mystical. The upper trough is what vents a surface low and lets it deepen. Without one, "the surface low will be anemic, or won't develop at all" (p62). The book's worked example (p62-63) tracks two upper troughs converging across four charts and concludes that even with benign surface forecasts, "these 500mb charts indicate a high probability of the weather bombing." Its instruction is to watch the pattern "for ten days to two weeks before your departure date."

That window is now available. Open-Meteo exposes 500hPa and 1000hPa geopotential height, 500mb wind and 500mb temperature at 16 days, which is a close match for what the book asks you to watch.

This belongs on the forecast page, not in the alarm centre. The book's 500mb use is passage planning days ahead, and an alarm firing on an upper trough while the boat sits at anchor reports something the operator cannot act on.

## Decision

### 1. Upper air is its own provider category

The original plan folded it into the weather provider, on the reasoning that the Open-Meteo weather plugin already queries the exact endpoint that serves pressure levels. That reasoning was sound and the premise was wrong: **this vessel runs Apple WeatherKit for weather, and WeatherKit carries no pressure levels at all.**

Folding the two together would have forced a choice between a good surface forecast and any upper air whatsoever, and would have left the feature silently dormant for anyone whose weather source happens not to carry pressure levels. It was caught by deploying the folded version and watching every day report "no upper air".

So `fetch_upper_air` is a fifth category with its own registry, adapter and reference plugin, mirroring the existing four. A boat runs WeatherKit for surface weather and Open-Meteo for the upper pattern, and neither knows about the other.

The category is **optional in a way the others are not**. An unset provider with nothing installed returns 200 and an empty day list rather than a 502, because a boat with no upper-air plugin is a normal configuration. A provider that is named but missing is still an error, since that is a real mistake.

The contract carries an unused optional `grid` field from the start, so trough detection over an area can be added later without a breaking change.

### 2. Everything is relative to the forecast window

**The book gives no numeric 500mb thresholds.** Its method is reading successive charts and recognising patterns. Any constant here would have been invented and then, worse, attributed to it.

So each day is judged against the rest of the window at this position: where its mean 500mb height sits among the other days, and how far heights fell coming into it. A day is marked when it sits in the **lowest quintile of the window after a real fall**.

This travels. At the vessel this week 500mb heights ran 5835 to 5907m; in the Southern Ocean they would be hundreds of metres lower, and a percentile needs no retuning between them. The claim it supports is honest and relative: the lowest heights of the coming fortnight, after a sustained fall.

Two guards stop it firing on arithmetic:

- A window whose whole range is under 20m has no pattern in it. Some day is always the lowest, and marking it every week trains the operator to ignore the marker.
- The fall into the day must cover at least a fifth of the window's own range.

### 3. The run-up is measured across two days, not one

This is worth recording because the first version was wrong in a way tests did not catch.

The rule was originally "lowest quintile and still falling", using the 24-hour tendency. Against live data it marked the trough bottom on a tendency of **-0.167 m/24h**, which is indistinguishable from flat. Had the model produced +0.167 instead, the lowest day of the fortnight would not have been marked at all. A flag that turns on the sign of a meaningless number is a coin toss.

What is actually meaningful is the fall *into* a day. The trough bottom sits at the end of a large fall even when its own last 24 hours are flat. Measuring the run-up over two days marks both the approach and the bottom, excludes the recovery, and has no knife-edge.

A day in the first two of the window reports no run-up rather than a partial one. Whether heights are arriving or leaving is genuinely unknowable with no history behind them, and saying nothing is the honest answer.

### 4. A marker on the card, the reasoning in the panel

The day cards are 150px and already carry five lines, so a flagged day gets a small `500MB` marker beside its weather icon and nothing else. The expanded panel carries the sentence: "500mb 5835 m, lowest of the window after a sustained fall. Conditions aloft support a surface low developing."

A day with upper-air data but no flag still shows its numbers, without any claim about development. A day with no data shows nothing at all, rather than an empty section that would read as "clear".

### 5. The window is drawn as a window (phase 2)

Section 4's marker and sentence were the right call for the day cards and the
wrong shape for the underlying method. The book reads a trough off the shape of
successive charts; the strip presented that as one scalar per day. "500mb
5899 m, jet 23 kt" is a statement about a sequence with the sequence removed,
and it cannot be correlated against anything.

So the forecast page now carries a trace of the whole window, and three
decisions inside it are worth recording.

It sits in a panel of its own. The first cut put it inside the selected day's
detail card, between the wave and tide charts, which are both 24-hour views of
one day. A 16-day chart nested inside "here is Tuesday" is a category error,
and it read as one. The page is now three sibling panels on one shell, today /
ten days / sixteen days, each header carrying its span and a meter showing the
proportion. Making them peers is the whole point: the panels differ by horizon
and by nothing else, which is exactly the distinction a reader needs.

**The series is sub-daily.** `buildUpperAirInputs` was averaging the provider's
hourly data down to one value per day and discarding the rest. A trough drawn at
one point per day is a sawtooth, and the fall into it is the part being read.
`buildUpperAirSeries` thins the same fetch to one sample per six-hour block of
local time, which is roughly the synoptic chart interval and holds 16 days to
about 64 points. It costs nothing upstream. Buckets are cut on the hour's
position in the local day rather than on an exact clock hour, so a provider
reporting at 01/07/13/19 keeps its full resolution instead of silently
returning nothing.

**The band is computed once, on the backend.** `upperAirWindowFor` reports the
window's low, high, and the height at which a day stops counting as the lowest
quintile, all cut from the same sorted list `percentileOf` uses. Re-deriving
that edge in TypeScript from rounded values would have let the drawn band and
the marked days disagree, which is the one thing a chart like this must not do.
The invariant is tested directly: every day at or below the reported edge is a
day the flag would accept, and the first day above it is not.

**Surface gust rides the same axis.** This is the correlation the feature
exists for. The book's claim is causal and lagged, and a lag is only legible if
both series share a time axis. The surface forecast is ten days and the
upper-air one sixteen, so the surface series ends first and is left null rather
than extrapolated: the trace's last six days are upper pattern only, and
drawing a continuation there would be inventing a forecast.

Two rendering details found by looking at it against live data rather than by
tests. Consecutive flagged days drawn to their own last sample leave a visible
gap between them, which reads as the trough letting up in the middle; a band now
runs to where the next day begins. And the gust series is on a hidden axis, so
its full scale goes in the legend, or the amber line is a shape with no
magnitude behind it.

### What was rejected

**Trough detection over a grid**, for now. It was prototyped and it works, but it is a much larger piece and Phase 1 delivers most of the value. The findings are recorded here so the work does not have to be redone.

Naive local minima in raw geopotential **do not work**. At 5° spacing axes vanished on 4 of 16 days and jumped 20-30° of longitude between days; at 2° the field produced a crowd of spurious 1-4m "troughs". The fix is three steps, all required: anomaly from the zonal mean, then smoothing along longitude, then a depth floor. Resolution and smoothing interact, and 5° over-smooths badly enough to lose a real -102m feature.

At 2.5° over a 238-point grid, 16 days at 6-hourly resolution costs 484 KB and 2.7 seconds, and produces coherent axes spanning several latitude bands:

```
2026-09-07  35S/178E -101   30S/178E -72   25S/178E -42
2026-09-10  40S/172E -191   35S/170E -152
```

**Calling a bomb** is deliberately not attempted, and would not be even with the grid. The book's actual precursor is two troughs coming into phase, which it judges by eye across successive charts. The raw material is visibly present in the data above, but deciding two features are phasing is a meteorological judgement this codebase is not in a position to encode, and a wrong call on that particular signal is exactly the confident nonsense the fallback policy exists to prevent.

**A fixed metre-per-24h threshold** was rejected with the percentile approach for the reasons in section 2.

## Consequences

- The forecast strip carries a multi-day upper-air outlook whose thresholds are all relative, so it needs no retuning as the boat moves.
- A fifth provider category exists. The shared WASM machinery made it cheap, but it is still a registry, an adapter, a reference plugin and a build to maintain for one data source.
- `plugins/upper-air/` is a new plugin directory. A deployment without it loses the feature silently and correctly, since the category is optional by design.
- The Open-Meteo weather plugin was briefly modified to carry pressure levels and then reverted. Anyone running Open-Meteo for weather gets upper air from the separate plugin like everyone else, and there is one source for the data rather than two.
- Upper air refreshes every six hours rather than hourly. The global models behind it run four times a day, so a shorter interval re-fetches identical numbers.
- The `/api/upper-air` response carries `series` and `window` alongside `days`. Both are additive, and a provider with no pressure levels still lands on an empty series and an absent window rather than on zeros.

## Verification

`go test -short ./...`, the plugin's own `go test ./...`, and 1245 frontend tests pass.

Every threshold in the derivation is tested at its boundary, including the cases that must **not** flag: a flat window, rising heights, a 1m drift to the bottom of a window, and the first two days where there is no run-up to measure.

The parse is pinned to values captured from a live response at the vessel's position rather than an assumed shape, per the standing fixture rule: 5878m at 500hPa over 186m at 1000hPa, 500mb wind 9.74 m/s. Two encodings were confirmed against the live API before any parse code was written. `wind_speed_unit=ms` applies to pressure-level winds as well as surface ones, so nothing converts them. And the API returns nulls at the tail of a 16-day run and omits the arrays entirely for a model without pressure levels, both of which land on the contract's zero-means-absent.

End to end against the live boat, the outlook marked 12 and 13 September out of 16 days, with heights falling from 5907m to 5835m and recovering afterwards. The surface forecast corroborates independently: drizzle and rising precipitation probability across 10 to 12 September, building into the upper trough.

Phase 2 was checked against the same live window at 1600, 768 and 390px. The trace shows 63 six-hourly samples across 16 days, the band edge lands at 5839m, and the surface gust series makes the lag visible without any prose: gusts run 11 to 13 kts through 8 September, jump to 32 kts across 9 to 11 September as heights fall, and the marked days sit at the bottom of that fall.
