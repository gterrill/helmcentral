# ADR 0070: Heavy-Weather Indicators from a Seamanship Text

## Status
Accepted

Extends ADR 0038 (alarms) and ADR 0055 (host-derived vessel paths). Fixes a defect in the latter.

Amended by ADR 0095, which retires the three barometer-rate rules of section 5 below (Barometer falling, Barometer plummeting, Barometer down 3mb in three hours) in favour of the Law of Storms ladder.

## Context

Steve and Linda Dashew's *Surviving the Storm* (Beowulf, 1999) was read against the forecast page and the alarm engine to see what of it could be encoded. The book is a heavy-weather seamanship text built from first-hand accounts and interviews with NOAA and Bureau of Meteorology forecasters, and it carries an unusual amount of quantitative advice: thresholds with numbers attached, most of them attributed.

The existing alarm model could not express most of the advice: `alarmRule` compares one path against one scalar, while the book describes rates and relationships. Examples include falling pressure, rising wind with steady pressure, and sea height increasing faster than its period lengthens.

The forecast page had the opposite problem. It already received everything it needed, hourly, and displayed the wrong part. Wave height was the headline number, and the book is blunt about why that is wrong (p613): "Regardless of size or steepness, if the wave isn't breaking, there's nothing to fear." A 3m sea at 12 seconds is a non-event. A 2m sea at 5 seconds is breaking. Both rendered as a height.

Two findings shaped the work.

`derived_paths.go` already existed and was exactly the right mechanism: `helmcentral.`-namespaced synthetic paths, host-computed, riding the gauge-values stream so anything that binds a path binds them. It had one entry.

ADR 0055 stated that an alarm rule could bind a derived path, but the implementation did not support it. `evaluateAlarmsOnce` used `snapshotAlarmReader`, which reads only the SignalK tree. A rule naming `helmcentral.*` resolved to absence and never fired, without reporting that the configured rule could not evaluate its input.

## Decision

### 1. Steepness, not height, leads the wave forecast

Steepness is `H / L` with `L = gT²/2π`. The book gives the same relation in feet at p218 and works an example at p252 that this reproduces exactly: a 14-second period comes out at 1,004 feet against its stated "1,000 to 1,300 feet" for 14 to 16 seconds.

| Ratio | S | Band | Source |
| --- | --- | --- | --- |
| flatter than 1:25 | < 0.04 | rolling | p252, 1:25 "not normally considered steep enough to break" |
| 1:25 to 1:14 | 0.04 to 0.07 | building | interpolated, no separate authority |
| 1:14 to 1:10 | 0.07 to 0.10 | steep | interpolated |
| 1:10 or steeper | >= 0.10 | breaking | p231 and p240 |

**The breaking edge is 1:10, not 1:7.** Page 231 reports both and discounts the latter: "Theory and tank test data indicate that waves become unstable at a slope ratio of 1 to 7 or steeper. However, observations in the real world indicate this figure is more like 1 to 10." Page 240 states it as a number. 1:10 is both the conservative choice and the authors' own.

The chart strokes its wave line with a hard-stepped gradient tinted by band. Only the two escalating bands take an alert colour; rolling and building keep the ordinary wave colour, because most days are one of those and a line always painted "a colour" teaches the eye to skip it.

**Colour never carries the band alone.** The ratio is written beside every direction arrow and the band is named in the tooltip and the day summary. This is a requirement, not a nicety: the palette validator puts the light-theme wave and gust tokens under 3:1 against the card. Both CVD checks pass in both themes (worst adjacent ΔE 14.0 light, 13.4 dark), and the ramp reuses existing alert tokens so it survives the instrument skin.

### 2. Leading indicators, each with a citation

| Indicator | Rule | Source |
| --- | --- | --- |
| Wave front | Hs up 3m inside any 3h | p246, Prosise's own screening criterion |
| Rapid build | Hs and period both up 50% in 1h | p250, Chesneau, "a certain danger signal" |
| Period step | Period up 3s in 1h | p250, the 8s-to-11s example |
| Cross sea | Wind wave and swell >= 60 degrees apart | p233 |
| Anomalous sea | Hs (ft) > 0.8 x wind (kt) | p233 |
| Air/sea delta | Sea warmer than air by >= 4F | p310 |

The last two need the weather forecast as well as the wave one and are joined in the drawer, since those arrive from separate endpoints already keyed by day.

The page also shows the largest wave at 1.87 x Hs (p243). A 3m forecast plans as 5.6m, and the headline number alone understates what you meet.

### 3. Cross seas needed two guards that only live data revealed

Adding per-component direction and period crossed the WASM guest contract, so `fetch_waves` gained four fields and the reference plugin was rebuilt.

Two things were then found by querying the provider at the vessel's own position rather than assuming a shape.

**A component with no waves in it reports height, period and direction all as 0, not null.** A naive angle reads a flat swell's placeholder "0 degrees" against a real wind wave and manufactures a large separation on an ordinary single-system day. The presence test is therefore the period: a wave train with no period is not a wave train.

**The model reports negligible components.** Across ten days it offered a 0.02m swell beside a 1.44m wind sea, 75 degrees away. That is arithmetic, not a crossing sea. The smaller train must be at least a third of the larger, which is the point where p233's argument holds, that a secondary system matters because it swings the boat off its alignment with the primary one. With both guards, three days of a benign ten-day forecast flag rather than six, and every remaining case has comparable component heights.

### 4. Derived paths carry the rates, and the reader gap is fixed

`derivedAwareAlarmReader` resolves `helmcentral.*` before falling through to the snapshot, and the alarm loop now uses it. Without this nothing else in this ADR could fire.

| Path | Units | Definition |
| --- | --- | --- |
| `helmcentral.environment.pressureRate` | Pa/s | least-squares slope over 3h, absent under 30 min of history |
| `helmcentral.environment.pressureChange3h` | Pa | plain 3h tendency |
| `helmcentral.environment.squashZoneIndex` | none | 1 when wind rose >= 10kt over 3h, 3h pressure change < 1mb, and direction moved < 30 degrees |

A regression rather than first-versus-last, because the barometer is noisy at sensor resolution and one bad sample at either end would set the whole figure. History comes from ring buffers alongside the existing gust and depth ones, fed from the delta-stream snapshot rather than the vessel-state struct, since nothing else needs these three there.

Page 89 calls squash zones the cause of "the majority of all weather difficulties encountered by yachts" and describes the difficulty of detecting them from onboard data, "because wind direction and barometric pressure typically remain steady while the wind increases". Page 188 identifies the western South Pacific around New Zealand and Australia as particularly affected. The squash-zone path combines wind, direction and pressure to detect a pattern that a barometer threshold alone cannot express.

The index is a number rather than a bool because the engine compares values, and a rule binds it with "above 0.5". A boolean path would need a new operator, and the point of a derived path is that the rule engine does not change.

### 5. A seeded rule set, shipped disabled

Five rules, created once per installation and keyed on a `seeded_sets` marker in the rules file rather than on the file being empty, so a rule the operator deletes stays deleted.

| Rule | Path | Condition | State | Source |
| --- | --- | --- | --- | --- |
| Barometer falling | `pressureRate` | below -1 mb/hr | warn | p82, p85 |
| Barometer plummeting | `pressureRate` | below -2 mb/hr | alarm | p128, p459 |
| Barometer down 3mb in three hours | `pressureChange3h` | below -3 mb | warn | p82 |
| Squash zone | `squashZoneIndex` | above 0.5 | warn | p89, p188 |
| Tropical barometer anomaly | `pressureChange3h` | below -1.5 mb | alert | p187 |

**The rules ship disabled.** The thresholds come from a book published in 1999, several attributed to forecasters rather than derived from data shown in the text. None is calibrated against this vessel or coast. They are starting points for operator review, not measurements or rules to enable automatically.

### What was rejected

**Adding rate operators to the rule engine.** A `rateAbove` / `rateBelow` operator would have to be handled at every operator site, would need its own window configuration in the rule schema, and would still not express the squash-zone signature, which is a relationship between three paths rather than a rate on one. Derived paths keep dwell, hysteresis, staleness and the "any path" model intact, and the squash-zone case falls out for free.

**Correcting the barometric rate for closure speed.** Page 193 points out that the same rate means different things depending on whether you are closing with the weather or it is overtaking you: a system moving at 15 knots closes 190 miles a day on a boat running with it and 528 on one heading into it. Encoding that needs the system's track, which is not aboard. It is documented for the operator instead. At anchor it does not apply at all.

## Consequences

- Wave forecasts read in the terms the book argues actually matter, and a benign forecast stays visually quiet. Ten days of live data at the vessel put every hour in the rolling band, so no alert colour appeared, no indicator panel rendered and the summary added no steepness clause.
- The `helmcentral.*` namespace grew from one entry to four, and the mechanism is now genuinely usable by alarm rules rather than only claimed to be.
- Three ring buffers now record whether or not anything binds them. At a five-second poll over 24 hours that is a bounded, known cost, the same one the gust buffer already carries.
- The wave guest contract gained four fields. A plugin that omits them sends zeros, which read as "component absent" rather than "due north", so older plugins degrade to no cross-sea detection instead of to false positives.
- The seeded set introduces proposed configuration. The marker prevents deleted rules from being recreated and can be reused for future sets.
- The alert-colour ramp is the first use of `--destructive` in a chart. It is deliberately not reskinnable, per the standing rule that a warning has to look like a warning in either skin.

## Verification

`go test -short ./...` and 1236 frontend tests pass. `-short` skips two live-network BOM FTP tests unrelated to this work.

The numbers were checked against the book rather than assumed. Steepness is tested against the worked example on p252, and the band table's two cited edges are tested at their boundaries.

The cross-sea guards were both found by querying Open-Meteo Marine at the vessel's own position before writing the parse, per the standing rule that a fixture comes from the real server. The zero-component encoding and the negligible-component case are each pinned by a test carrying the real values.

End to end against the live boat: the rebuilt plugin serves all four new fields, matching a direct query of the provider hour for hour. Sixty-nine of 240 forecast hours carry both components, eleven exceed 60 degrees, and three days survive the one-third guard. The forecast page was screenshotted at 1600, 768 and 390, the three layouts of ADR 0032, with no label collision or overflow.

One layout defect was caught only by looking: the steepness ratio was first rendered on its own row at y=41, inside the plot area, which no test detects because jsdom has no geometry. It shares the period baseline instead.
