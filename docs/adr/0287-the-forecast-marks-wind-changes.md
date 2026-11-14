# ADR 0287: The Forecast Marks Wind Changes

## Status

Accepted (2026-11-14). Extends the single-day turn rule of ADR 0266 across the
whole forecast horizon.

## Context

The operator had to reach The Boatworks by Tuesday 17 November. After several
days of northerlies they assumed a spare day in Mooloolaba was safe, and missed
a south-easterly change due on Tuesday. The forecast showed it, but nothing made
it stand out. A day card shows the day's dominant direction, so a turn at 2 PM
disappears behind "N". `buildWindSummary` named such a turn inside one day's
sentence, and only there.

## Decision

A detector reads the whole hourly forecast as one series and returns a list of
changes. Every day in the forecast response carries the changes that start on
it. There is no separate top-level list, because the drawer reads only the
per-day copies.

- **A change is one settled wind giving way to another.** A settled wind is a
  period of ADR 0266's segmentation (hours within 45 degrees of the period's own
  mean, ended by a calm) that has six hours at 5 kts or more. Light hours inside
  it neither break it nor count towards the six. The next settled wind has to
  blow 90 degrees or more from the anchor (below). Up to 12 hours of light,
  variable or unsettled air may lie between the settled wind just before it,
  whatever its direction, and the new wind, counted from the last hour that
  wind still blew, and that air is part of the change. A settled wind within 45
  degrees of the one just before it is that wind carrying on after a lull, not
  a new wind. A short burst that comes
  straight back is no settled wind, so it is no change, and two settled winds
  less than 90 degrees apart are a drift, not a change, however they got there.
- **Each settled wind is measured against the anchor.** The anchor is the wind
  the last change ended on: the first settled wind to begin with, then each
  change's new wind. A settled wind 90 degrees or more from the anchor is a
  change from the anchor's direction to it, and it becomes the new anchor. A
  wind that veers in 6-hour steps of 60 degrees (north, ENE, ESE, south) is one
  change when the turn from the anchor first reaches 90 degrees, and the next
  step is judged from the new anchor, so it is no second change. "From" names
  the anchor, the wind that set the pattern. A drift of under 90 degrees is not
  a new settled wind, so it never becomes the "from"; it only counts as the
  wind just before the new one when the gap is measured, so a north wind that
  drifts to 60 degrees for 14 hours and then settles south-east is one change
  from north to south-east. Comparing each
  wind with its neighbour would have missed a slow veer entirely and named the
  wrong "from" for a wind that passed through an in-between step.
- **The time is when the new wind sets in.** It is the first hour of the new
  settled wind, moved back over the firm hours just before it that were already
  within 90 degrees of it, never past the end of the old wind. That is the
  "turning S around 7 AM" the day line gives. The direction and speeds of the new
  wind are read over its first 12 hours. A new wind needs six firm hours inside
  the forecast, so a change near the end of the series waits until the forecast
  can show it holding.
- **One segmentation across the horizon.** The detector runs the day summary's
  `splitWindSegs` and `markVariableHours` once over every hourly point, across
  midnight, instead of day by day. A change that straddles midnight is therefore
  one change, and the period notion is the summary's own. The summary keeps its
  per-day grouping and wording, so it still describes a day's shape and can name
  a turn that gets no chip.
- **Only direction and time name it.** "N→SE 2 PM". The detector holds no
  hemisphere assumption: a south-easterly change in Queensland and a westerly
  veer in the North Atlantic are the same event.
- **It is never called a front.** A front needs a pressure trough and rise plus
  a temperature drop. The weather plugins carry hourly temperature but no
  hourly pressure (`weatherHourPoint`), so the test cannot run. Widening the
  plugin contract was out of scope. Until a provider sends hourly pressure, a
  change stays a plain wind change, and there is no guessed label.
- **Each change carries its confidence.** A change more than 72 hours ahead is
  lower confidence, and the drawer words it that way.
- **Prominent means the operator's own limit.** A change is prominent when the
  top of the wind after it, as shown (rounded to whole knots, the way the plan
  table judges it), reaches the lowest marginal wind across the sectors with a no-go set (`lowestMarginalWind`,
  marginal being 70% of no-go as in ADR 0191). No limits set means nothing is
  prominent. There is no constant knot threshold and no vessel default.
- **The forecast does not depend on the limits.** When the passage limits
  cannot be read, the forecast still returns, no change is prominent, and the
  response carries `wind_changes_problem` with the reason. The drawer shows it
  under the wind sentence: "Wind changes not judged against your limits:
  <reason>". The error stays visible without making limits a hard dependency of
  the forecast. Quietly treating every change as unimportant would have been
  the failure this ADR exists to remove.
- **Only the hourly forecast is read.** The detector counts hours, and a
  provider that goes from hourly to 3- or 6-hourly further out gives entries it
  cannot count as hours. The scan stops at the first gap of more than an hour,
  at an entry with no time (which counts as a skipped hour, never dropped
  silently), or where the series ends before the forecast's last day. The
  response carries `wind_changes_checked_to`, the last hour read on the boat's
  clock, and `wind_changes_checked_reason`. The drawer and the watch report say
  "Wind changes checked to Thu 19 3 AM: the forecast skips an hour here" for a
  gap of two hours or less, and "...: the forecast is no longer hourly" for a
  longer gap or an hourly series that ends early. A cut inside the last day is
  reported too. Nothing is said when the whole forecast was read hourly. A
  change that falls on a day the forecast has no day card for would have no
  place to be drawn, so the response carries `wind_changes_unplaced` and the
  drawer counts it in its "not shown in full" line. The detector reads the wall
  clock for what is past and what is lower confidence, as the watch report
  does, not the GNSS datetime. An earlier version held each coarse entry across the
  hours up to the next and read changes from the result; that guessed at winds
  the provider had not forecast, and it was dropped.
- **The client words "today" and "tomorrow".** The forecast response is held by
  the client for up to an hour, so a label fixed when the server answered would
  call yesterday's change "tomorrow" after midnight. The wire carries no
  relative day: a change sits under its day, and the drawer works out "today",
  "tomorrow" or "Tue 17" from the boat's date when it draws. The watch report is
  written once, at its run, and words the day itself with the same three forms.
- **Why the first rule was replaced.** The first rule asked for a swing of 90
  degrees or more within 3 hours that then held for 6. On the live Mooloolaba
  forecast of 14 November it found nothing. The wind was NNW 5 to 10 kts
  through Monday, then light and variable from about midnight to 6 AM Tuesday,
  then south from 7 AM and south-east from about 10 AM, holding through
  Wednesday. No 3-hour window held a 90-degree swing, because the wind went
  through light air instead of swinging, yet the Tuesday day line said "turning
  S around 7 AM and settling SE". The chip and the day line now describe the
  same event, and Monday's south-west burst and light-air wobbles do not count.

## Surfaces

- **Forecast drawer.** A chip on the day card, muted or amber by prominence.
  A day with several changes shows the first prominent one, or the first when
  none is, plus "+N", and the card's spoken label names every change and which
  reach the marginal wind. The extended summary leads with a wind sentence
  built from the changes, says when the limits could not be read (only when
  there is a change they could have judged), and says how far the changes were
  checked when the hourly forecast stops early. The day's own direction stays on the card.
- **Watch report.** The weather section reads the forecast at the boat's own
  position, taken from the status the report already built (the boat's last
  trusted fix, the same place the forecast endpoint uses, however old), and adds one row
  and one line for the first change, with a count of those after it. When the
  limits cannot be read the changes are still listed, and a separate note gives
  the same words as the drawer. It says how far the changes were checked when
  the hourly forecast stops early. The planned stay is a
  different place, so it keeps its own fetch. It raises no finding, since a forecast is not a fault on board, and
  official warnings stay as ADR 0087 has them. When the forecast cannot be read
  the section says so.

## Not yet built

The plan table's Wind column, the `get_wind_forecast` tool for Mate, the plan
verdict rule and arrive-by slack come after the passage plans verdict work lands
(`plans.ts`, `plan-table.tsx` and `assistant_tools_forecast.go` are in flight on
another branch).

## Consequences

- A wind that never settles for six hours is no change. A squall line, or two
  turns less than six hours apart, give no chip. That is right for a boat
  deciding on a day, and wrong for a squall line, which the warnings cover.
- A slow drift that ends 90 degrees or more from where it began, with a settled
  wind at each end, is a change. The rule judges the two winds, not the speed of
  the turn between them.
- The chip and sentence follow whatever the provider forecasts. A change that
  the model moves tomorrow moves on the card.
