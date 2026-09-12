# ADR 0095: The Law of Storms Barometer Rules

## Status
Accepted

Extends ADR 0070 (heavy-weather indicators) and ADR 0087 (forecast warnings
are alarms). Retires the three barometer-rate rules of ADR 0070 section 5:
Barometer falling, Barometer plummeting, and Barometer down 3mb in three
hours.

## Context

ADR 0070 seeded five rules from Steve and Linda Dashew's *Surviving the
Storm*, three of which describe the same falling barometer three different
ways: a -1 mb/hr slope, a -3mb/3h tendency, and a -2 mb/hr slope. Two of the
three fire on the same falling barometer. None of the three has a
rising-pressure counterpart, none gates on the absolute pressure the fall
starts from, and none looks further out than three hours.

R. J. Ellis, "Secret Law of Storms", worldstormcentral.co (Rules for storms
and gales page), gives a coherent ladder built on the same three-hour
pressure tendency every marine forecast already quotes:

| Last 3 h | Meaning |
| --- | --- |
| ▲10 mb or ▼10 mb | gale (34-47 kt); applies to rise or fall |
| ▲6 mb or ▼6 mb | strong wind (26-33 kt); applies to rise or fall |
| ▼4 mb with pressure < 1009 hPa | storm / thunderstorm (3 mb is the minimum, 4 "a margin of comfort") |
| ▼4 mb in 3 h and ▼8 mb in 12 h with pressure < 1005 hPa | severe thunderstorm |
| ▼24 mb in 24 h | weather bomb (defined at 60° latitude) |
| ±1.1 to 2.7 mb | rain and wind, poorer weather; not alarm material |

Unlike the Dashew set, a rise carries the same weight as a fall, and two of
the six meaningful rows gate the tendency on the barometer's actual height
rather than treating a 4 mb fall as significant regardless of whether it
starts from 1030 or 1005.

## Decision

### 1. The page's ladder becomes the rule set

The table above replaces the three overlapping Dashew rate rules. Gale and
strong wind read the three-hour tendency alone, in either direction. Storm,
severe thunderstorm and weather bomb also read the tendency, but gated on
absolute pressure or on a second, longer window, following the page as
written rather than reducing it back to a single rate the way the Dashew set
did.

### 2. Four new derived paths

`pressureChange3h` already existed (ADR 0070). Four more are added under
`helmcentral.environment.`, computed the same way and riding the same
gauge-values stream:

| Path | Units | Definition |
| --- | --- | --- |
| `pressureChange12h` | Pa | Twelve-hour tendency: last sample minus first, over the twelve-hour window. |
| `pressureChange24h` | Pa | Twenty-four-hour tendency, same definition, and the figure the weather-bomb rule reads. |
| `stormIndex` | none | 1 when the three-hour tendency has fallen 4 mb or more and the current pressure is under 1009 mb, 0 otherwise. Absent whenever the three-hour tendency itself is absent. |
| `severeThunderstormIndex` | none | 1 when the three-hour tendency has fallen 4 mb or more, the twelve-hour tendency has fallen 8 mb or more, and the current pressure is under 1005 mb. Absent unless both tendencies are present. |

Each is nil-seeded and absence-first, the same contract as every other
`helmcentral.` path: a rule bound to one of these never fires on a reading
that has not actually arrived.

### 3. Three windows, two span rules

A tendency is last-sample-minus-first-sample over a window, and it needs the
window to actually be covered by history or it reports a fraction of the real
fall as if it were the whole thing.

The three-hour path keeps the thirty-minute minimum span it has had since ADR
0070 (`trendMinimumSpan`), not three hours minus a margin. Holding it to the
stricter rule would leave a boat blind to a falling glass for two and a half
hours after every restart or every dev rebuild, and a reading taken over a
short span still under-reports a monotonic fall rather than over-reporting
one. That is the safe direction for a rule meant to raise attention, not the
dangerous one.

The twelve-hour and twenty-four-hour paths use a thirty-minute slack against
their own window instead (`tendencySpanSlack`): twelve hours reports once 11.5
hours of history exists, twenty-four once 23.5 hours does. There is no
restart-blindness argument for these two the way there is for the three-hour
path; a rule watching a twelve-hour trend that fully engages after eleven and
a half hours is not meaningfully different from one that waits for the full
twelve, and the slack keeps it from flickering in and out right at the
boundary.

### 4. Seven seeded rules, one of them off

| Rule | Path | Op | Value | Hysteresis | Dwell | State | Ships |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Barometer up 6 mb in three hours | pressureChange3h | above | 6 mb | 0.5 mb | 900 s | warn | on |
| Barometer down 6 mb in three hours | pressureChange3h | below | -6 mb | 0.5 mb | 900 s | warn | on |
| Barometer up 10 mb in three hours | pressureChange3h | above | 10 mb | 0.5 mb | 900 s | alarm | on |
| Barometer down 10 mb in three hours | pressureChange3h | below | -10 mb | 0.5 mb | 900 s | alarm | on |
| Storm signature | stormIndex | above | 0.5 | 0 | 1800 s | alert | off |
| Severe thunderstorm signature | severeThunderstormIndex | above | 0.5 | 0 | 900 s | alarm | on |
| Weather bomb | pressureChange24h | below | -24 mb | 1 mb | 1800 s | emergency | on |

Severities follow the wind ranges the page attributes to each rung: strong
wind at warn, gale at alarm, weather bomb at emergency. Values are stored in
pascals (`6 * pascalsPerMillibar`, and so on) the same way every other
pressure path already is.

Unlike ADR 0070's set, six of these seven ship enabled. The page describes a
single coherent ladder rather than three rules restating one condition, the
windows match what a marine forecast already quotes, and the severities map
directly onto the wind ranges the page states. The storm signature is the
exception and ships disabled: the page itself calls 3 mb the minimum and 4 mb
only "a margin of comfort" above it, and a 4 mb fall under 1009 mb is a
routine afternoon on a temperate coast, not the storm the label promises. It
is there to turn on wherever the operator judges it means something.

The seeding function, `seedLawOfStormsRules`, does not force every rule to
one blanket enabled state the way the other two seeders do; it takes each
rule's `Enabled` flag from the table above. This is worth flagging because it
differs from both of its predecessors, which each set one value across their
whole set.

### 5. The three Dashew rate rules are retired, by hand

Barometer falling, Barometer plummeting and Barometer down 3mb in three hours
are deleted from the seed file. Squash zone and Tropical barometer anomaly
stay exactly as ADR 0070 shipped them; neither restates a rate the new ladder
now covers. `pressureRate` (Pa/s) keeps publishing, since a gauge or a graph
can still use it even though no seeded rule reads it anymore.

There is no migration deleting these three rules on an existing installation.
The operator removes them by hand, once, on the boat, after this ships. A
migration would either delete a rule the operator had already retuned and was
relying on, or would need to detect that case and skip it, which is exactly
the kind of conditional, multi-tier ceremony not worth building for one boat
with one operator.

### 6. The frontend sentences

`alarm-display.ts` exports the five path constants (`PRESSURE_CHANGE_3H_PATH`
through `SEVERE_THUNDERSTORM_INDEX_PATH`) and a `TENDENCY_WINDOW_WORDS` map
(`three hours`, `twelve hours`, `twenty-four hours`). `lawOfStormsSentence(alarm)`
is called immediately after `forecastWarningSentence`, so a stale alarm still
reads "No data" before either specific sentence gets a chance to run.

A tendency rule reads, for example: "Up 6.0 mb in three hours. Clears once
the rise eases to 5.5 mb." or "Down 24.0 mb in twenty-four hours. Clears once
the fall eases to 23.0 mb." The sign of the configured value has to agree
with the operator (rising values above, falling values below); a rule that
does not match falls through to the generic sentence rather than printing
something backwards.

The two index rules spell out the condition rather than the bare number:
"Storm signature: barometer down 4 mb in three hours below 1009 mb." and
"Severe thunderstorm signature: barometer down 4 mb in three hours and 8 mb
in twelve hours below 1005 mb." This prose duplicates the backend's threshold
constants, the same trade ADR 0087's forecast sentences already make.

`alarm-rules-view.ts`, `alarms-drawer.tsx`, `alarm-banner.tsx`,
`quantities.ts` and `alarm_units.go` needed no change: the Pa-to-mb
conversion already exists on both sides, an unbound unit already falls back
to a bare number, and grouping by a path's first segment already puts every
one of these under `environment`.

## What was rejected

**A latitude-scaled weather-bomb threshold.** Sanders and Gyakum's original
definition scales the 24 mb figure by sin(latitude)/sin(60°), which comes out
to roughly 17 mb at 37°S. Applying it would mean the alarm engine needs a
position-aware threshold, which nothing else in it does. The page's own 24 mb
is already defined at 60° latitude, which makes it the more conservative
number at any latitude closer to the equator; an operator cruising somewhere
the lower figure applies can lower the rule's value themselves.

**The ±1.1 to 2.7 mb tiers.** These sit inside the ordinary twice-daily
atmospheric tide the barometer already shows on a calm day. A rule built on
them would fire on most afternoons rather than on weather.

**The tropical six-hour-steady and seasonal-average rules.** Both need a
climatology, a table of what normal looks like for this location and this
time of year, that the boat does not carry and that Helmcentral has no
provider for.

**Rate operators in the rule engine, again.** ADR 0070 rejected `rateAbove` /
`rateBelow` operators because they would need their own window configuration
wherever an operator is handled, and would still not express the storm and
severe-thunderstorm signatures, each a relationship between a tendency and an
absolute pressure. That reasoning holds more strongly now that three windows
are in play instead of one; derived paths keep every rule the same shape.

**A migration that deletes the old rules.** Rejected for the reason given in
section 5 above: one operator, one boat, no installed base to script an
automatic cutover for.

## Consequences

- The twelve-hour and twenty-four-hour rules need the boat, or at least
  Helmcentral's own process, to have been running continuously for that long
  before they report anything. A boat that restarts its backend daily will
  rarely see the weather bomb rule engage at all.
- Every derived-path computation now pulls one twenty-four-hour slice of the
  barometer's ring buffer rather than a three-hour one, then slices that down
  locally for the twelve-hour and three-hour figures instead of scanning the
  buffer three separate times. `derivedAwareAlarmReader` recomputes every
  derived path on each derived read, once per rule per alarm tick, so the
  slice is taken several times a tick; the cost is bounded and known, the
  same shape ADR 0070 already accepted for the gust buffer.
- The frontend's sentence text duplicates the backend's threshold constants.
  A constant that changes in `weather_trend.go` without a matching edit in
  `alarm-display.ts` drifts silently until someone reads a card against the
  rule it is supposed to describe.
- The barometer's ring buffer (`windGustHistoryCapacity`, 17,280 slots at the
  five-second poll) is exactly twenty-four hours and no more. The
  twenty-four-hour tendency and the weather bomb rule sit right at the edge
  of what it holds, which is why the buffer's span is pinned by its own test
  rather than left to arithmetic that a future change to the poll interval or
  the slot count could quietly shorten.

## Verification

Backend, `go test -short ./...` in `backend/`: `TestTendencyOverWindow_AbsentUntilTheWindowIsNearlyCovered`,
`TestTendencyOverWindow_TwelveAndTwentyFourHourWindows`,
`TestStormSignature_FourMillibarFallBelow1009`,
`TestSevereThunderstormSignature_NeedsBothWindowsAndThePressure` and
`TestLawOfStormsThresholdsAreInPascals` in `weather_trend_test.go`;
`TestPressureChange12hAnd24hDerivedPathsReportTheTendency`,
`TestPressureChange24hAbsentUntilTheWindowIsCovered`,
`TestStormIndexDerivedPathFiresOnAFourMillibarFallBelow1009`,
`TestSevereThunderstormIndexAbsentUntilTwelveHoursOfHistory`,
`TestSevereThunderstormIndexDerivedPathFires`,
`TestDerivedPathAgesLawOfStormsPathsReportNewestBarometerSample` and
`TestBarometerHistoryCoversTheTwentyFourHourWindow` in `derived_paths_test.go`;
`TestSeedLawOfStormsRules_CreatesSevenRulesWithTheStormSignatureDisabled`,
`_ThresholdsAreInSIUnits`, `_RunsOnceAndDoesNotResurrectDeleted`,
`_SeedsIndependentlyOfTheOtherMarkers` and the rewritten
`TestSeedHeavyWeatherRules_KeepsSquashZoneAndTropicalAnomalyOnly` in
`alarm_rules_test.go`; and `TestLawOfStormsEngine_TwelveMillibarRiseRaisesTheUpRulesOnly`
at the engine level.

Frontend, `npm test` in `frontend/`: `alarm-display.test.ts` cases for a
three-hour rise, a twelve-hour fall, the twenty-four-hour bomb, a sign/op
mismatch falling through to the generic sentence, both index sentences, and a
stale alarm still reading "No data"; `alarm-rules-view.test.ts` confirming
`stormIndex` groups under `environment`.

`go vet ./...` is clean and `go test -short ./...` passes; `-short` skips the
two live-network BOM FTP tests unrelated to this work. The frontend suite
passes at 2325 tests across 207 files, with `tsc --noEmit` clean.
