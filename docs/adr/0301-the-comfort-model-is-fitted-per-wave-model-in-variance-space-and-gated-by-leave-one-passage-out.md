# ADR 0301: The Comfort Model Is Fitted Per Wave Model In Variance Space And Gated By Leave-One-Passage-Out

## Status

Accepted (2026-11-22). Builds on
[ADR 0299](0299-recorded-passages-keep-the-sea-state-behind-each-spell.md), which
stores the sea behind each spell, and
[ADR 0300](0300-the-logbook-caps-the-wind-wave-by-fetch-and-flags-enclosed-swell.md),
which caps the wind wave by fetch, flags enclosed swell, and names the follow-up
that reads only the spells it checked. Roll acceleration becomes sideways g as in
[ADR 0297](0297-sideways-g-is-worked-out-from-an-operator-entered-helm-height.md).

## Context

The logbook now holds, for each recorded spell, how far the boat swayed, how sharp
her snap was, how hard she pounded, and the sea she was in. The next step is to
say, from a forecast, how a passage will feel. That needs a model of this boat's
response to a sea, and the boat's own record is the only thing that can teach it.

This ADR builds the model, checks it against passages it has not seen, and shows
the boat's status in the logbook. It does not yet put a word on a plan leg. That is
the next ADR's job, and it reads the prediction interface built here.

## Decision

### 1. A spell trains the fit only when its sea can be trusted

A spell is used when:

- its stored hour has no missing reason, and was made by the wave model being
  fitted;
- it has a mean heading;
- its wind wave was checked against the shore (ADR 0300), and the fit reads the
  capped figures;
- its swell is neither enclosed nor unchecked.

The fit learns from passages that are not hidden, as the comfort bands do, and
hiding or showing one asks for a fit. A flat sea, with neither a swell nor a wind wave and a combined height under 0.1 m,
is usable and teaches the intercept. An hour with neither system and a combined
height of 0.1 m or more, or none stored, is a plugin that gave no split, and its
spells are set aside for that reason (ADR 0299). A system with no height is absent,
as on the forecast. Each way a spell can lack a sea, whether no position, an hour not fetched
yet, a wave plugin that can't be asked, the data source's refusal, an hour with no
figure, or no split, is its own reason, so the counts say which to fix. Snap and
pounding train only on spells with 10 Hz figures.

### 2. Each target is a sum of variances

For each wave system, steepness is s = πH/L, with L = gT²/2π, and θ is the
direction the system comes from relative to the heading. The four targets are:

| Target | Feature x | Recorded figure |
| --- | --- | --- |
| Roll | s·\|sin θ\| | roll RMS |
| Pitch | s·\|cos θ\| | pitch RMS |
| Snap | s·ω²·\|sin θ\|, ω = 2π/T | peak roll acceleration |
| Pounding | s·max(0, cos θ) | peak vertical acceleration |

Each target fits y² = a + b_s·x_s² + b_w·x_w², with the swell and the wind wave
each given a coefficient, and every coefficient at least zero. Variances add where
amplitudes don't, so two seas at once give the sum of what each gives alone. The
non-negative fit is exact: with three coefficients it tries every subset and keeps
the best one that has no negative value. Each passage weighs the same however long
it ran. A ridge a billion times smaller than the data keeps two systems that move
together solvable, and does nothing else.

Snap is stored as roll acceleration and turned into g only when it is read, with
the helm height now on record, so a changed helm height moves every prediction
with no refit. Sway is the root of the roll and pitch variances added together,
the same sum the felt figure uses, so it takes the same bands.

### 3. A target is used only after it predicts whole passages it has not seen

Each target has a floor on the record, then a check:

- Sway needs 8 passages and 60 spells with motion and sea.
- Snap and pounding each need 5 passages and 30 spells with 10 Hz motion.
- Leave-one-passage-out: for each passage in turn, the fit is made without it and
  scored on it. The score is the mean absolute error on that passage, and every
  passage counts once. The comparison is a fixed guess, the median of the
  training figures, weighted as the fit weights them so that each passage counts
  once there too. Skill is one minus the model's error over the guess's error,
  and it must reach 0.30.

Counting by passage, not by spell, stops one long passage from carrying the check,
or from setting what the fixed guess is.
A target that fails says so in words: "needs 8 passages with motion and sea, has
5", or "the model doesn't yet predict recorded passages better than a fixed guess
(skill 0.12, needs 0.30, from 9 passages)". A target that fails its check gives no
figure, only the reason.

### 4. One fit for each wave model, in memory

A fit made on GFS-Wave says nothing about the Open-Meteo blend, so the fit is kept
for each wave model by the model's id. A worker owns it, as the tide calibration
worker owns its fit: the worker is the only writer, publishes the set behind an
atomic pointer, and is never written to the database, since the fit is worked out
from data that is. Start-up asks for one fit once the recompute is done and does
not wait for it, so the first sea pass starts at once. After that it fits only
when something it learns from has changed: a sea pass that stored hindcast hours or
fetch rows, or a change to a passage's track (a trim, split, merge, or an import
once its points and passages are stored). A pass that stored nothing leaves the
fit alone, a rating doesn't ask for one, and asks that arrive while a fit runs
collapse into one more. A fit logs one line for the fit, which counts the spells it
set aside for each reason, and one for each wave model with each group's passages,
spells, skill or gate. The worker keeps the text of its last log and logs only when
the text differs, so a failure that repeats is logged once.

The fit only reads. It never works a passage's figures out:

- **The key.** Each passage's judgement is cached in memory under what it read: its
  range and `figures_gen` (a new column that a trigger bumps on every write to
  `stats_json`, a clear as much as a store), the count and fetch times of the
  hindcast rows in the passage's own hours and cells, the count of fetch rows in
  its own box, the shore's state at each of its positions (the squares' stamp, or
  why there is none), and whether the wave plugin can be asked. One cheap query
  lists the passages with their `figures_gen`; the stats are parsed only when that
  or the range has changed. A fit with nothing changed judges no passage and logs
  nothing. A merge or split changes the list of passages, and the entries of the
  IDs that are gone are dropped.
- **Waiting.** A passage whose stats are missing or old is waiting. It trains
  nothing, and the status says "2 passages are waiting for their figures to be
  worked out", or "not learned yet; 3 passages are waiting..." when none was read.
  The start-up recompute, or the first read of the passage, stores the figures, and
  a store asks for a fit. A passage whose recompute fails keeps its last error in
  memory, and the status gives that error in place of "waiting". `comfort_model`
  gains `waiting_passages`.
- **The shore.** The fit reads the Offline coastline's squares through a loader of
  its own made for each fit, never a line of coastline. A sea pass that finds the
  coastline turned on or off, or a square downloaded again, asks for a fit though it
  stored nothing, since the same rows then judge the spells differently.
- **Reading the figures once.** `PassageFiguresFor` reads the current figures
  outside any guard. Readers that find one passage stale together work it out once
  (a singleflight), and the store is refused when `figures_gen` has moved since the
  figures were read, so an import that clears a passage while it is recomputed
  leaves it cleared and waiting. This replaces the lock the fit and the fetch sweep
  took between them: no walk takes a lock.

When the fit has no usable spell, its status names what held back the most, such as
"1,400 of 1,500 recorded spells are set aside, most often because their wind wave
isn't checked against fetch (the Offline coastline is off)". It doesn't say that
no passage has motion and sea together when the spells were there and set aside.

If the record can't be read, the fit stops, the old one is replaced by a status
that says so, and nothing is fitted on part of it. That covers the stored hindcast
rows and the stored fetch figures: a failed read of those would otherwise set
every spell aside as "not worked out yet", which reads as the sweep being behind.
The fetch sweep, which walks the passages too, leaves an unreadable one out and
goes on; the fit is the one that stops. A passage with no current figures is not an
unreadable one: it waits, as above.

### 5. The forecast keeps what the model needs

The wave plugin's `fetch_waves` now names its model (`model`) and how the logbook
words it (`model_label`) every time, not only after a fallback. A forecast hour
keeps the swell and the wind wave apart, each with height, period and direction,
and the model's id and label. Two things on an hour are reasons and never a calm:

- A plugin that doesn't name its model says to update the wave plugin.
- An hour with neither system present, and a combined wave of 0.1 m or more or none
  given, says the plugin gave no swell and wind-wave split. An hour with a system
  is judged from its systems whatever the combined wave says, as in training.

Nothing renders either yet. Plan verdicts, Mate's answers and change marks read the
combined wave as before.

### 6. The prediction interface

`predict` takes a wave model (its id and its label), a heading and the two systems,
and answers for sway, snap and pounding with a figure or a reason. A model with no
fit says which models the fit was learned on and which one the forecast used, in the
forecast model's label. The forecast and the hindcast name their model the same way,
and the plugin words both from one table, so an unknown model is an error in either
export. The stored hindcast keeps the model's id and not its label, so the sentence
names the models the fit was learned on by id. A figure carries a flag when
any input is steeper than any the record held, so a later reader can say the figure
is beyond the record. The interface is tested and not yet wired to a plan.

### 7. The Felt row says what the boat has learned

The passage page's Felt row gains one server-worded line, for the wave model with
the most passages behind its sway: "Comfort model: sway learned from 12 passages
(skill 0.42); snap and pounding not yet: needs 5 passages with 10 Hz motion, has
1." Before the first fit it says it isn't worked out yet, and with no usable spell
it says nothing is learned yet.

The status is the boat's, not the passage's, so `GET /api/passages/:id` returns it
as a top-level `comfort_model` object beside `passage`, not inside the Felt block.
It holds the sentence (`summary`, worked out once for each fit and the same for
every passage), an `error` when the record couldn't be read, and one entry for each
wave model with how sway, snap and pounding stand: whether it is learned, the
passages and spells behind it, the skill once learned, and the reason when not.
The page shows the sentence beside the bands summary.

## What it is not

- Not a forecast of anything the operator sees on a plan. That follows.
- Not stored. A restart refits in the time the logbook takes to read.
- Not a physics model. It is a fit of four numbers per system to one boat.

## When it can't run

- Offline coastline off or incomplete: wind waves can't be checked, so those
  spells don't train, and the line says how many passages it has.
- The wave plugin is the old one, or missing: the forecast says to update it, and
  the fit still learns from the stored hindcast. Its status adds that new sea hours
  are waiting for the wave plugin, and the note goes once a sweep can ask again.
  Only a store that can't be read stops the fit.
- A passage can't be read: the line says the model couldn't be worked out, with
  the reason, until the next fit.

## Follow-ups

- The leg chip on plan rows, and fetch capping of forecast legs.
- Helm height changes need no refit for snap; the sway bands still come from the
  operator's ratings.
