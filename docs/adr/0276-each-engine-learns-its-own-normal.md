# ADR 0276: Each Engine Learns Its Own Normal

## Status

Accepted (2026-11-10). Amends [ADR 0132](0132-anomaly-detection-cycle-1.md): the
twin gate gains a single-engine form, and the live detector is untouched.
Amends [ADR 0255](0255-a-watch-report-gives-each-part-one-job.md): the report
gains a block that is not a section and not a finding.

## Context

The engine detectors Helmcentral has are rules. The twin differentials of
[ADR 0132](0132-anomaly-detection-cycle-1.md) compare an engine with its peer, so a
one-engine boat gets nothing, and they judge one reading at a time. A fault often
shows first as readings that are each ordinary and wrong together: fuel rate high
for the load, oil pressure low for the rpm. No threshold names that.

The boat already records what it needs. InfluxDB holds every engine reading,
and a month of one-minute means from both of Pikorua's engines is on file
(`docs/research/anomaly-detection-kmeans-mahalanobis.md`, local).

## Decision

For each engine, learn from its own history the mean and covariance of its valid
readings in each 200 rpm band. Score a past window minute by minute by its
Mahalanobis distance within its band. Three or more unusual minutes in a row make
a stretch, explained by the reading that adds most to the distance, against that
band's usual value, in the operator's units.

It is a suggestion and never an alarm. Nothing is raised, acknowledged or
notified, and nothing is scored live.

### The measure

For one band of one engine with d readings, let x be a minute's readings, μ and s the
band's learned means and standard deviations, and R the learned correlation matrix. The
standardised minute is z = (x − μ) / s, and its squared Mahalanobis distance is

    D² = zᵀ (R + λI)⁻¹ z,    λ = 0.1

The ridge λ lives in correlation space, so it means the same for rpm as for oil
pressure. Because R carries how the readings move together, a minute whose readings
each sit inside their own spread can still score high when the combination is wrong,
and two readings that rise together are not counted twice.

D² splits across readings. With w = (R + λI)⁻¹ z, reading j contributes zⱼ·wⱼ, and the
parts sum to D² exactly. A contribution can be negative, so the named reading is the one
with the largest summed contribution over the stretch, among readings that may be named
(never rpm). A minute is unusual when D² is above the band's threshold,
max(P99.5 of the band's training D², χ²₀.₉₉(d)). The first term is what 1 in 200 of the
band's own minutes exceeded. The second is what chance alone passes 1 time in 100 if the
readings were normal, and it keeps a band with few distinct minutes from learning a line
that is too low.

Training fits twice. The first fit scores every row; rows above χ²₀.₉₉₉(d) are dropped,
except rows the operator answered normal, and the second fit, whose training D² set the
threshold, is the one that is saved. A standard deviation is the larger of the sample's
and max(observed step, MinChange) for that reading.

Mahalanobis rather than a z-score per reading, because the faults this is for are
readings that are each ordinary and wrong together (see Context), and a per-reading
z-score cannot see a correlation. Rather than k-means, because the regime is known from
rpm (see Rejected).

### A generic core, with engines first

`anomaly_normal.go` knows no engine and no path. A subsystem is a list of
readings, a function that places a minute in a regime, a gate and a rule for which
alarms belong to which subject (`normalSubsystem`). The saved model is keyed
subsystem, then regime, then statistics, so the house electrical system (a later
PR) adds a key and reshapes nothing. The engines are `anomaly_normal_engines.go`.

- **Subjects.** Every configured engine with an instance. There is no two-engine
  minimum: an engine is compared with itself. An engine is named as the alarm cards
  name it. When two come out alike, as two items of one model do, the position goes in
  front: "Port Cummins QSB 6.7 engine".
- **Regimes.** The engine's 200 rpm band, such as "1,800 to 2,000 rpm". Revolutions
  stay in the fit so the minute sits in its place on the band, and are never named
  as the reason a minute is unusual.
- **Readings offered.** Revolutions, coolant temperature, oil pressure, boost
  pressure, load, fuel rate, gear oil pressure, gear oil temperature and exhaust
  temperature. Exhaust temperature is a candidate like the rest. A sender that
  reports a constant drops out as constant and a working one joins on its own.
- **The gate.** The twin gate's single-engine form, `engineGateHolds`, pulled out of
  `twinGateHolds`: at least 900 rpm, coolant at least 65 degC, running ten minutes,
  and for the last five minutes rpm within 50 rpm and coolant warm. A minute missing
  rpm or coolant fails the gate and restarts the running clock, as does a missing
  minute.

### Which readings are used

Decided from the data, with a stated reason for each reading left out. Nothing is
named in the rule.

- **Switched off.** The reading is on the ignored-sensor list (below).
- **Two sources.** Another source reports more than 10% of the same minutes at the
  same time. The old behaviour, a quiet last-wins between sources, is gone for this
  feature.
- **Sender changed.** A reading handed from one gateway to another shares no minute
  and is one reading. Training pins it to the sender of its latest minute and saves
  that sender on the verdict. Scoring reads only that sender, never the one with the
  most minutes. When the latest sender has under an hour of minutes the reading is
  "sender changed, relearning": it is left out like a repaired sender that is joining,
  and the bands are fitted without it until it qualifies. A band that was learned
  with it is not carried, because the old sender's figures are not the new one's. If
  the reading is one the gate needs, the engine is not judged and the problem names it.
- **No readings, or missing:** present in under 80% of the judged minutes.
- **Impossible:** more than 5% of its minutes outside the physical limits.
  Exhaust temperature and fuel rate join the limits table in
  `anomaly_sensor_health.go` for this, which also lets the live impossible-reading
  check see them.
- **Constant:** fewer than three distinct values, one value in 98% of minutes, or
  a spread no larger than the smallest step, judged over every minute the engine was
  running, warm-up included, and not over the steady minutes alone. A thermostat holds
  coolant on one value for nearly every steady minute, but it climbs from ambient after
  each start. A sender that never changed at all does not. Values compare to nine significant
  digits so the float noise of an average is not a change. Pikorua's port gear oil
  temperature, a constant 310.8 K in the capture, drops out here and no path is
  written into the rule.

**A reading that starts working.** A repaired sender is judged over the judged
minutes since it first appeared, so it is not held out for the 90 days. A band
refits on the minutes that have every used reading. Until that fit has an hour from
two runs, the band keeps its previous normal, without the new reading, and the
status says "joins after 60 minutes, 14 so far". With no previous normal, the band
fits without it and lists it as joining.

### The fit

- Each reading standardises with its standard deviation floored at its observed
  resolution, the smallest step between its distinct values, and at the same
  figure across the whole engine for a reading that sat still in one band. It is
  also floored at the smallest change worth a look for that kind of reading
  (`normalMeaningfulChange`: 1 K for coolant and gear oil temperature, 10 K for
  exhaust, 10 kPa for oil pressure, 5 kPa for boost, 2 points of load, 1 L/h of
  fuel, 20 rpm). The observed step is measured on one-minute means, where a sender
  that steps in whole degrees averages to fractions of a step, so it alone cannot
  stop a one-degree dip from reading as many deviations.
- The correlation matrix takes a ridge of 0.1 on its diagonal, so two readings that
  move together cannot make it singular.
- **One reweighting pass** drops rows above chi-square 0.999 and refits, so an
  unlabelled fault in history does not become normal. Rows the operator answered
  normal are kept. One constant switches it off.
- **The threshold** is the larger of the 99.5th percentile of the band's training
  distances and chi-square 0.99 for its number of readings, saved with the band.
- **Contributions.** The distance splits across readings; the parts sum to it. The
  largest summed part among readings that may be named is the stretch's top reading.

### Where the minutes come from

An optional one-minute bucket (`influxdb.minute_bucket`) names a bucket that
already holds one-minute means (see
[Keep a longer history in InfluxDB](../how-to/keep-longer-history-in-influxdb.md)).
Blank rolls the main bucket up at query time with `aggregateWindow(every: 1m)`,
which does nothing to a bucket already at one minute, so both give the same
minutes.

- **A named bucket that fails is an error** that names the bucket. It is never
  answered from the main bucket.
- **No InfluxDB** means no learned normal, said plainly.
- **Scoring always reads the main bucket.** The downsampling task can leave the
  minute bucket up to an hour behind.
- The Flux builder rejects `$` and `{` in the bucket, path and field, and Settings
  rejects them in the main bucket and the one-minute bucket when the save changes
  them. The organization never enters a Flux literal, and a name already stored does
  not block a save of something else.
- **Only floats are averaged.** The query keeps `types.isType(v: r._value, type:
  "float")` before `aggregateWindow`, as the how-to's own task does.
- **The connection is resolved once per run** from the settings path the run was
  given, as a client of the run's own that is closed at its end, so a settings save
  in the middle cannot close it. Every chunk reads through it. A
  secrets store that cannot be read is reported as that, not as "InfluxDB is not set
  up".

### Training

`data/anomaly-normal.json` is relearned from scratch every 24 hours and when its inputs
change: the InfluxDB settings or token, an engine added, removed or given a new
instance when an equipment item is saved, or an engine reading switched on or off.
A house bank or alternator role is not an input. One hook, `normalInputsChanged`, wakes the trainer, which waits 30 seconds for a
burst of changes to end and runs once. At start it waits out the rest of the 24 hours
when the last run succeeded from the same source. Each
run reads the last 90 days of stored minutes, takes out the minutes that overlap an
alarm for the same engine (from ten minutes before it was raised to five after it
cleared, an open alarm to the end of the window), and fits every band that has an
hour of judged minutes from at least two runs.

- **Alarms.** `OverlappingBetween` returns every occurrence open at some moment of
  the window, spanning and open ones included and with no row cap, which
  `ChangedBetween` cannot do. An alarm belongs to an engine by its path:
  `propulsion.<id>.`, `helmcentral.anomaly.engines.<id>.` or
  `helmcentral.anomaly.propulsion.<id>.`, after a `notifications.` prefix is removed.
  A propulsion alarm takes minutes out only when it is about a reading the model
  uses, one of the used readings or one the gate needs. An alarm on any other path
  under `propulsion.<id>.` takes nothing out. An alarm that never cleared takes out
  a day after it was raised, because the log keeps no last-seen time and one
  forgotten open would otherwise take out everything after it. A log that is not
  open stops the run.
- **Runs.** A gap of under five minutes with the engine above idle either side is
  the same run, so a gateway reconnecting does not restart the running clock.
- **Bands with no new data** keep their last normal, with the date it was learned,
  for 183 days, then are dropped.
- **Shift.** A refitted band saves how far its new mean moved, seen through the old
  covariance. It is not shown yet.
- **Failure.** A run that fails leaves the saved model alone and records why in
  `last_attempt`. A run that learns no band where the saved model has some does the
  same. A file that does not parse is reported and left untouched.

### Scoring

On demand, over the main bucket, never stored. Scoring checks the ignored-sensor list
itself, so a reading switched off since the last training run is never judged or
named, whatever the saved model holds: a band that uses it is not judged and says
"boost pressure is switched off; relearning at the next training run". An engine whose
training left a problem is reported with that problem and never as "no history yet".
Each reading is read from the sender training pinned it to. When that sender has
gone quiet while the engine runs, whether another sender reports the reading or
nothing does, scoring says "oil pressure: the sender this was learned from (can0.1)
has gone quiet; relearning at the next training run", counts the minutes under it,
and asks for a training run. A minute that fails the
gate is not judged and is counted as not running steadily. A minute in a band with no
normal is counted by band. A minute missing readings its band uses is counted once,
under all of them, and is never filled in. Unusual means above the band's threshold. A stretch is three or
more unusual minutes in a row on one engine, which may cross a band. They rank by
peak distance over threshold, then length, and each carries a stable id,
`engines:<engine>@<start>`.

### Where it shows

- **The watch report** gains an `unusual` field with at most two lines and a note.
  It is not a section and not a finding, so `watchIssueCount` cannot count it, the
  push title and summary do not change, and Mate may call it worth a look and never
  a finding or a cause. The email carries it after the findings. The webhook carries
  it inside `report`. A watch that checks Instruments reads it; the others do not pay
  for the queries. Scoring has a 90 second budget, and when it runs out the block
  says so. A boat with no InfluxDB gets no block at all, since Settings already says
  it. An InfluxDB that is set up and fails still shows its error. A window cut to the
  last 7 days says so.
- **`get_unusual_engine_readings`** gives Mate, reviews and outside assistants the
  same stretches, with boat-time labels, UTC instants, the model's age, the minutes
  not judged and why. It is offered to outside assistants switched off.
- **Settings, InfluxDB** shows when and from where the normal was learned, the bands
  per engine, and each engine's readings with a switch and the verdict beside it.

### The switch is the ignored-sensor list

One list, not two. A reading is off for the learned normal when it is on the
ignored-sensor list, so a dead sender is ignored everywhere at once. Readings
default to on, and a repaired sender is picked up without a visit to Settings. The
two readings the gate needs may be switched off too, but the engine is then not
judged and says which reading is off. Its switch stays enabled, so it can be turned
back on. Gating on an ignored sender is not allowed, because it may be a dead one.

## What it cannot know

- **A fault from a change in how the boat is used.** A new propeller, a different
  fuel or a new habit changes the normal. It reads as unusual until it has been
  learned.
- **Why.** It names the reading and the direction and says nothing of a cause.
- **A change smaller than the floor.** A coolant change under 1 K or an oil pressure
  change under 10 kPa is never called unusual, however steady the reading has been.
- **A band the engine rarely visits** has no normal and is said not to be judged.
- **Exhaust temperature on another instance.** On Pikorua the exhaust senders
  publish under `propulsion.0` and `1`, while the rest of each engine is on `port`
  and `starboard` (ADR 0196). A feature path is `propulsion.<instance>.<key>`, so a
  repaired sender still on the other instance is not found. Fix it at the gateway, or
  decide a per-reading source override then.

## What fails, and how it shows

- **No InfluxDB:** "No learned normal: InfluxDB is not set up." in Settings and from
  Mate. The watch report leaves the block out.
- **A one-minute bucket that cannot be read:** the run fails and says
  `the one-minute bucket "X" could not be read`. The old model stays.
- **No model yet:** "No learned normal yet."
- **A scoring read that fails:** "Unusual readings could not be checked", with the reason.
- **A band with too little history:** "Not judged: 1,400 to 1,600 rpm has 22 minutes of
  history, needs 60", when nothing else was judged.

## Feedback

The operator can tell the model what a stretch was, and the maintenance log can say when
a fault started. Both change the next training and nothing else: nothing is rescored
and no model is edited by hand.

- **Answers.** The watch report's Unusual block keeps its `lines` as sentences and gains
  `stretches`, indexed like them, each with the stretch's id, subsystem, subject, engine
  name, start and end. An answer is recorded from those without scoring again. The
  operator answers real, normal or unknown, and can change it.
  `PUT /api/anomaly/normal/answers` takes `{watch_id, run_id, index, answer}`. The server
  reads the stretch from the stored run's `stretches[index]` and uses its subsystem,
  subject and times, so a client cannot answer a range it invented, and no id has to be
  escaped in a path. It refuses with a 400 a bad answer, a run that is not there, a line
  with no stretch, and an engine that is no longer set up ("This engine is no longer set
  up. It may have been renumbered or removed."). The file `data/anomaly-normal-answers.json`
  keeps one engine's answers as ranges that never overlap: the latest answer replaces the
  parts of earlier ones it overlaps, trimming or splitting them, so training, scoring, the
  page and Mate all read the same set. It cannot be rebuilt. The run as the page reads it
  carries `unusual_answers`, indexed like the stretches, read when the run is read
  (`watchRunsForPage`), so a changed answer shows on reload and the stored report is never
  rewritten. That read cannot fail the page: when the answers or a report cannot be read,
  the run says so in `unusual_answers_problem` and shows no buttons. A report saved before
  this has no stretches and so no buttons. Mate is told the lines only.
- **Real** is a fault. The stretch's minutes are left out of training like an alarm's,
  with no padding: the stretch's end is already the instant after its last minute. The
  entry in `excluded` reads `answered real`. Every range in `excluded` now includes its
  start and not its end, so an alarm's saved end is one minute later than it was, the
  same minutes as before.
- **Normal** is kept. The reweighting pass drops a row above chi-square 0.999, so an
  answered row carries a keep flag through `normalFitOptions.Keep` and is never
  dropped. `excluded` means taken out, and every entry in it is, so these go in a
  sibling list, `labelled`, with the reason `answered normal`. An alarm, a real answer
  or a repair wins over a normal answer for the same minute. Scoring also never flags a
  minute inside a range answered normal for that engine, so the answer takes effect at
  once and does not wait for the next training to widen the band.
- **Unknown** is recorded and changes nothing.
- **Repairs.** A log entry of kind repair gains an optional `started_at`, a date no
  later than `performed_at`. When the entry is on an engine's equipment item, the
  engine's minutes from the start of `started_at` to the end of `performed_at`, on the
  boat's clock, are left out (`repair`, with the entry id). Each date takes the offset
  in force on it, but the boat keeps no history of the zones it has been in, so a repair
  dated before the boat changed zone is read in the zone it is in now. The inventory has no
  containment between items, so only the engine item itself counts. Writing or deleting
  an entry that has, or had, a start date on a configured engine's item wakes the trainer;
  a repair on any other item wakes nothing. Training reads only the
  repairs that have a start date, not whole log entries. The log's CSV export carries the
  date in a last column, "started showing". The only importer reads another program's
  export, which has no such field, so there is no import to round-trip it.
- **Failure.** An answers file or a maintenance log that cannot be read stops the run
  and is recorded in `last_attempt`, as an unopened alarm log is. Neither is replaced by
  "no answers". A repair whose dates cannot be placed because the boat's zone is unknown
  stops the run the same way, with "The boat's time zone is unknown, so repair dates
  could not be placed.", and only when there is a repair to place.
- **Carry-forward.** One rule, `usableCarry`, serves every path that reuses an old regime
  (a thin band, and a subject with no usable minutes or a problem): an old regime is never
  reused if a real answer or a repair for that engine overlaps the window it was learned
  from, however old that window is. An alarm never stops it, since that fit left the alarm
  out too. A band not reused shows its thin or not-learned reason, and the answers and
  repairs used for the check are not cut to the current window. A repair that ended before
  the oldest window this run or any old regime was learned from is dropped by date alone,
  so it never asks for the boat's zone, and neither does a repair on another item.
- **Overlap.** The page and the Mate tool show a stretch the answer covering most of its
  minutes, and on a tie the later one (`overlappingAnswer`), so a stretch straddling two
  answers shows the larger. Training and scoring use the answers minute by minute: scoring
  sorts them once and walks them alongside the minutes. An engine's linked equipment item
  is saved with the model, so relinking it wakes the trainer. A model saved before this
  change retrains once on the first equipment save after the upgrade.
- **Mate.** `get_unusual_engine_readings` gives each stretch the operator's answer
  (`operator_answer`) from the latest answer whose range overlaps it, not only an exact
  id, since a stretch's start can move a minute between two scorings. A stretch called
  normal is not raised again.
- **What the replay found.** In the captured month, with a 20 minute oil pressure fault
  bent into the held-out week, an answer of real leaves exactly those minutes out. An
  answer of normal leaves nothing inside the answered range flagged, once the training
  has learned it as ordinary and scoring has been told to clear it. Training alone left a
  last four minutes a hair over the line, which is why scoring clears the range itself.

## Rejected

- **k-means.** The regime is known: the rpm band. Clustering would find regimes the
  operator cannot name.
- **Imputing a missing reading.** A minute missing a reading is not judged. A filled
  value would be a number the engine never sent.
- **Live scoring in this change.** The report and the tool score a past window on
  demand. A live scorer is a second detector with its own alarm question, which the
  operator has not asked for.
- **A SQLite roll-up of the minutes.** InfluxDB already holds them, and the model is
  small enough for one file.
- **Two lists, one for alarms and one for the learned normal.** A dead sender should
  be off everywhere at once.

## Replay of the captured month

`TestReplay_MonthOfCapturedMinutes` trains on 12 October to 3 November and scores 3 to
10 November from `testdata/anomaly/engine-month-1m-slice.json`, the minutes in which an
engine reported (3,696 of 41,977), rounded to seven significant digits. The defaults
above were set before the replay and are unchanged.

- **Learned.** Port and starboard each learn the 1,600 and 1,800 rpm bands, from 1,604
  judged minutes each. Bands from 1,000 to 1,600 rpm have too little history.
- **Left out.** Port gear oil temperature, as constant. Exhaust temperature on both,
  with no readings: the capture has none.
- **Held out week.** 2,574 judged minutes of 2,765 engine-running minutes (46.1 engine
  hours) give 1 stretch, 0.022 per engine hour, against an aim of about 0.1. It is
  starboard gear oil temperature low, 5 minutes on 8 November at 1.43 times threshold.
- **How it got there.** The first replay, without the meaningful-change floor, gave 10
  stretches (0.217), all naming coolant temperature. The coolant sender steps in whole
  kelvins and sat on one value for 97% of the learned minutes, so a one-kelvin dip held
  for twenty minutes read as 32 times threshold. The floor removed them. No constant was
  tuned to the slice. The test holds the aim as its ceiling.
- **Found.** An oil pressure drop to 60% for 20 minutes, and a fuel rate step to 160%,
  injected into a steady stretch of the held-out week, each yield a stretch naming
  that reading.

The stumble captures were not used as a direction check: they hold rpm and load only,
so no minute passes the gate, and the fault and onset captures sit at about 1,170 rpm
while the healthy one is at 1,800.

## Follow-ups

- **Feedback.** Built; see Feedback below. A replay of how early the score rose over a
  labelled range is still open.
- **Refresh cost.** After an answer, `refreshRuns` reads every loaded run again, older pages
  included, in pages of 100. That is fine for a handful of pages; a cheaper read waits for a
  watch with a long history.
- **Engine renumbering.** An answer is saved against an engine instance. If the instance is
  later given to another engine, the answer applies to the new one. Answers do not follow
  an engine through a renumbering, and nothing yet asks.
- **House electrical (later).** Charging sources by state of charge, with a gate of
  about ten minutes of a stable source set, in the same file and with the same trainer.
- **A drift line and live scoring** are out of this change.

## Consequences

- A boat with one engine gets suggestions, and so does a twin whose engines drift
  together.
- A band is judged once it has an hour of steady running from two runs, so bands
  fill in as the boat motors at different speeds. A new engine says "No learned
  normal yet" until then.
- Report scoring is one InfluxDB query per engine per chunk, all its readings in it. The
  filter is an OR chain of equalities, which InfluxDB pushes down to storage, where
  `contains()` is not and scans every series. The InfluxDB client cuts every query off at
  20 seconds whatever the context says, so a chunk is 8 days of a one-minute bucket and
  1 day of the raw main bucket (a point a second). A failure names the chunk and the
  source, such as "the main bucket could not be read for 3 Nov to 4 Nov". Training reads
  across up to 90 days the same way, skips a reading that is switched off, and holds one
  engine's minutes in memory at a time.
- The help pages for anomaly detection, watches, Mate's lookups and outside assistants
  say what the suggestion is and is not, and the configuration reference gives the
  bucket.
