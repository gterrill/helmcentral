# ADR 0302: A Plan Leg Reads Its Comfort Hour By Hour Over The Shore It Sails By

## Status

Accepted (2026-11-23). Reads the prediction interface of
[ADR 0301](0301-the-comfort-model-is-fitted-per-wave-model-in-variance-space-and-gated-by-leave-one-passage-out.md),
applies the fetch cap and the enclosed-swell flag of
[ADR 0300](0300-the-logbook-caps-the-wind-wave-by-fetch-and-flags-enclosed-swell.md)
to forecast hours, and bands the sway with the edges of
[ADR 0298](0298-the-logbook-says-how-a-passage-felt-in-words-learned-from-the-operators-ratings.md).
The plan assessment it joins is
[ADR 0286](0286-passage-plan-verdicts-and-suggested-departures.md) and its
successors, which this ADR leaves as they are.

## Context

The model can now say how much a boat sways, snaps and pounds in a given sea. The
plan page still says only whether a leg is inside the passage limits. A leg can be
Go and still be a miserable ride, and the operator learns that today by sailing it.

Two things stand between the model and a plan. The logbook corrected a sheltered
wind wave from stored rays, one row for each recorded position, and a forecast hour
has no stored rays. And the model reads a spell's sea, while a leg is a sequence of
hours at positions that move.

## Decision

### 1. One walk over a leg's hours

The loop that `cell` ran over a leg's hourly stamps is now `walkLegHours`. It takes
the real span of the leg (departure to ETA) and the span to walk, up to but not
including its end, and yields each stamp with the leg's position and heading then.
`cell` calls it as before, asking for one hour past its last stamp, so every verdict
and every passage window cell is unchanged, and the existing goldens guard that.
Comfort calls it from the departure to the arrival. A stamp covers the hour from
itself, cut to the leg: a departure at 8.15 AM gives an 8 AM stamp worth 45 minutes.

A leg the boat is under way on is walked from now. Its position still comes from the
fraction of the real departure-to-ETA span, so the hours that are left are read where
the boat will be and not where it started. A leg past its ETA says "The leg is under
way and its ETA has passed." The wind changes and the comfort take the leg's span
from one place, the departure and arrival the verdict was made at, kept as instants
and never read back from the row's text, and say the ETA has passed in the same words.

### 2. Comfort is worked out once, at the chosen departure, for the plan page only

*Amended by [ADR 0303](0303-a-suggested-departure-is-ranked-by-comfort-then-current.md):
for a suggested departure, comfort is now worked out for each candidate of the best
verdict, and the chip is the chosen candidate's. A leg judges a departure every 15
minutes, about 40 to 50, not "up to five".*

The assessment judges up to five candidate departures a leg. Comfort runs after the
verdicts, once for each leg, at the departure the verdict was made at. Only the plan
page's endpoint asks for it (`planAssessOptions.Comfort`). A verdicts-only run, a
slip price, a change mark, `get_plans` and Mate never read it, so nothing that was
judged yesterday is judged differently today. `planLegAssessment.comfort` is null
when it was not asked for, when the caller gave no comfort readers at all, and when
the leg has no verdict and is not under way. A caller that asks for comfort with
readers that are not all set, or whose model or bands cannot be read (the passage
store is not running, say), gets "Comfort not judged yet:" and the reason on each
leg that would have had comfort, never a quiet null and never "not learned". The
model, bands, helm height and coastline loader are read once for the request, not
for each leg.

### 3. The fetch cap reads live rays, not stored ones

The sweep stores rays for recorded positions. A leg's hours lie where no spell has
been, so each hour casts its own rays. For each hour:

- The position is snapped to 0.001 degree, about 110 m, so the rays start near the
  real position and never across a channel or a headland from it. The first
  implementation snapped to 0.01 degree, which could put a point on the far side of
  the land.
- The coastline loader of the request plans the squares within 15 nm and stamps
  them.
- What the shore says along a bearing from that snapped position, for that stamp,
  comes from a memo. When the memo has no answer, the request builds the shore
  geometry for the position from the squares' lines, casts the ray over it and keeps
  the answer in the memo.
- The wind wave is checked with `checkWindWave` and the swell with the enclosed
  check, exactly as ADR 0300 words them. The refactor is mechanical: they take a
  `windInput` (kept, speed, direction, why missing, and whether it was taken over a
  spell or an hour) and the logbook builds one from the spell, so a recorded spell is
  judged as before. A leg's wind is the forecast's, and its reasons say "this hour".
  A position charted ashore is a flag the check sets, not words read back from a note.

The memo is wired into the comfort readers once, not reached for as a global. It
keeps answers, not geometries. An answer is one ray result: the distance to the shore
along a bearing and whether a reef stops it, whether the water is open, or that the
position is charted ashore. Each is a few bytes, where a geometry is tens of thousands
of segments. The key is the snapped position, the stamp of its squares, the kind of
ray (wind, calm or swell) and the bearing in tenths of a degree. A square downloaded
again, replaced or removed gives a new stamp, so only the positions that read it miss,
and a stale answer is never read for a changed shore. The memo is a least-recently-used
list of 32,768 answers, a few hundred bytes each, under 10 MB in all. A position
charted ashore is an answer and is kept.

A geometry lives only inside the request. The request builds it the first time one of
the position's rays misses, builds it once for each leg whatever the number of rays
cast over it, and lets it go when the leg is judged, so it holds about one leg's worth.
Requests that miss on the same position at once share one build, and a failed build is
not kept.

A reason the shore could not be read, such as the coastline off, a square missing or
failed, or the position inland, is never kept in the memo. It belongs to the request
that met it, so the next request, with the coastline on, gets its rays. The same goes
for running out of time.

Two more things stop the same shore being read twice. A request remembers each ray it
has read, so a memo that drops an answer in the middle of a request never costs a
second build, and each snapped position's squares are planned once. And the squares'
own lines are kept by the state each square was downloaded in (1,000,000 points, about
16 MB), and joined once for every position of the request that shares them, so a new
position in a square already read does not read it again.

On the Whitsundays fixture, three legs of about ten hours each take about 52 ms cold
and under 1 ms warm on a laptop (`BenchmarkLegComfortThreeLegs`); the target for cold
was 200 ms. A warm run builds no geometry. A new forecast run is not warm, though: its
bearings differ from the last run's, so its rays miss and each position is built again,
at about the cost of a cold run. The model, bands and helm height are read when the
first leg that gets comfort asks for them, so a plan whose legs all skip comfort reads
none.

A shore that can't be read leaves the sea unjudged, never calm: the coastline off,
a square missing or failed, the position inland. Every hour then carries the reason
in the coastline's own words, swell-only hours included. A position charted ashore,
which a berth in a marina can be, is its own gap, "position charted ashore". It is an
honest gap and not a guess at the berth's fetch.

### 4. An hour is judged or it says why not

Before any hour is walked, the leg is read down one fixed ladder. The first rung that
blocks gives the whole leg's text, "Comfort not judged yet:" and every distinct reason
at that rung, and the leg lists no hours. The ladder is one function, so the order
never depends on which hour comes first.

1. The model set: not worked out, couldn't be worked out, nothing learned, or sway not
   learned for any wave model.
2. Each wave model the leg's forecast hours use: a plugin that names no model (update
   the wave plugin), a model with no fit ("learned on GFS-Wave; this forecast comes
   from ..."), or a fit whose sway is held back. Where a forecast seam gives two wave
   models two reasons, both are named.
3. The bands. Not learned is no block: the leg is walked for its sea only and reads
   "Comfort not learned yet" even when every gap is the shore's.

A reason that came back with no words reads "The comfort model is held back, and gave
no reason", so a block always says why. Only a leg past the ladder is walked. For each
hour, in order:

1. No forecast for the hour is a gap.
2. An hour the model can't read is a gap with the reason from ADR 0301: no swell and
   wind-wave split, no wave figure.
3. The shore is read. An enclosed swell, a swell not checked for enclosed water, a
   wind wave not checked against fetch and a position ashore are gaps.
4. The model predicts from the capped figures. The sway is banded by the learned
   edges. Snap becomes sideways g at the helm height now on record, and pounding is
   in g.

A gap is never a band, and every gap carries a reason: a reason that came back with no
words reads "the comfort model gave no figure for this hour". A leg whose
bands are not learned reads "Comfort not learned yet. Roughest sea: roll, swell 0.6 m
4 s on the port bow." and gives no band word. A flat sea reads "Roughest sea: flat.", a
sea steeper than any recorded adds that clause, and each gap follows as a sentence of
its own with its length: "Not judged for 1 h: swell not judged in enclosed water."

The assessment's deadline reaches the comfort. A request past its deadline reads no
ray, warm or not, builds no more shore geometry, and keeps nothing in the memo, and a
request waiting on another's build stops waiting. The hours not reached read "the
comfort check ran out of time", never a word and never silently missing. A build that
another request abandoned for its own deadline is built again by the request that
waited on it.

### 5. Hours read as stretches

Neighbouring hours with the same gap reason, or the same band and, for Lively and
Rough, the same leading word, merge. The leading word is roll or pitch, whichever is
the larger part of the sway. The sea quoted is the system that adds most to that
part's variance (`comfortGroup.lead`), in the capped figures and in words relative
to the heading, in the words an hour's line uses: "swell 0.6 m 4 s on the port bow". A
Comfortable stretch carries no reason and no figures. A Lively or Rough stretch quotes
its roughest hour.

The first and last hours of a leg are clipped to the leg's span, so one can be a few
seconds long. An hour shorter than 10 minutes is not judged as a stretch of its own: its
time joins the stretch beside it when that is of the same kind (a judged hour into a
judged stretch, a gap into a gap), and otherwise it stands alone, so time not judged is
never counted as Comfortable. A length is never worded "0 min" (under a minute
reads "under a minute"). A leg of nothing but such hours keeps them. The hour still
appears in the hour list. A leg under way with no readable ETA says why (no departure
time set, say) and never that its ETA has passed.

The sentence reads "Lively for the first 2 h: roll, swell 0.6 m 4 s on the port bow.
Then Comfortable." A stretch in the middle gives its length, "Then Comfortable for
3 h."; the last runs to the arrival. A gap always gives its length, even the last:
"Then not judged for 1 h: swell not judged in enclosed water." Under an hour is in
minutes, and a leg under way says "for the next". More than three stretches read
"Lively at times; roughest from 2 PM for 2 h: roll, ...", with a closing clause for any
hours not judged, naming every reason. A leg that is Comfortable where judged keeps the steeper-than-recorded
clause when any judged hour had it.

The second line is the greatest snap and pounding of the judged hours, with no band
word: "Snap up to 0.12 g at the helm · pounding up to 0.30 g". Where one is missing it
gives the reason, once if snap and pounding share it. A blank helm height says to
enter it, and pounding still shows.

### 6. Beyond the record is said, both ways

- With no Rough edge learned and a predicted sway above every rated passage, the
  stretch says "Lively, rougher than any recorded passage". It can't say Rough, since
  no passage was rated that bad, and it won't say a smaller word.
- Any judged hour whose sea is steeper than any the model learned from adds "in seas
  steeper than any recorded passage" to its stretch, even when the sway is mild,
  because the fit is an extrapolation there. The hour's detail carries the flag.

### 7. The wire and the page

The server owns every word. `comfort` on each leg holds `text`, `motion` and the
hours, and each hour holds only its time, its band (comfortable, lively, rough or
"", never a colour), whether its sea is beyond the record, and its `line`: the hour
in the logbook's own sea words ("swell 0.6 m 6 s on the port bow", an enclosed swell as its
note), with snap and pounding worded as the Felt row words them. Stretches, the
worst band and the numbers behind the lines stay on the server. It is kept out of
the passage window cell, the model result, the verdict snapshot, the change marks
and the plan report, so no mark moves because the sea does.

The plan table shows a line across the row below the cells, in plain text with the
second line muted, and in a card a cell labelled Comfort at the foot. It uses no
verdict colour, so a Lively word can't be read as a Marginal one. Tapping it opens
the hours as lines, and the row holds whether it is open, as it does for the route.

## What it is not

- Not a verdict. Go, Marginal and No-Go are the passage limits' and unchanged.
- Not a departure ranking. The chosen departure is the one the verdict chose
  (amended by ADR 0303: a suggested departure is now ranked by comfort).
- Not Mate's. Mate reads the leg through `get_plans`, which has no comfort key.
- Not stored. Comfort is worked out when the page asks, from the live model and the
  forecast.

## When it can't run

- No passage limits, no forecast, or a leg with no verdict: no comfort, and the
  line is absent.
- A forecast with no wave split, or from an older wave plugin: the hours say why.
- The coastline off or missing a square: every hour says so, and nothing reads
  Comfortable.
- A model fitted on another wave model: the leg says which it was learned on.
