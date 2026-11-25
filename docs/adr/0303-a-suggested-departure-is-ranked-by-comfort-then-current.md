# ADR 0303: A Suggested Departure Is Ranked By Comfort, Then Current

## Status

Accepted (2026-11-23). Amends
[ADR 0302](0302-a-plan-leg-reads-its-comfort-hour-by-hour-over-the-shore-it-sails-by.md)
§2, which worked comfort out "once for each leg, never for each candidate", and
corrects its "up to five candidate departures": a leg judges a departure every 15
minutes across the day's daylight, about 40 to 50. Amends
[ADR 0286](0286-passage-plan-verdicts-and-suggested-departures.md), which walked the
ranking "and stops at the first Go". Both ADRs carry a note pointing here.

## Context

The plan page suggests a departure when the operator sets none. It takes the best
verdict inside the day's daylight, Go before Marginal, and then the fairest
current. ADR 0302 added the comfort chip, but only for the departure already
chosen. A day could hold a Go at 6.30 AM with the fairest stream and a lively ride,
and another Go two hours later that is Comfortable throughout. The page suggested
the first and then said, truthfully, that it would be Lively.

The comfort walk is cheap enough to run for every candidate. Positions repeat by
start minute, so a leg has at most four sets of positions, and the rays the walk
needs are shared through the request's snapped positions and the memo of ADR 0302.
`BenchmarkRankComfort` ranks 48 candidates of a Whitsundays leg in about 70 ms cold
and under 1 ms warm on a laptop, building 16 geometries cold and none warm and
holding 32 memo entries. `BenchmarkRankComfortThreeLegs` ranks the three legs of
`BenchmarkLegComfortThreeLegs` in about 150 ms cold and about 2 ms warm, holding 88
entries of the 32,768 the memo allows. A forecast run that moves the bearings costs
about as much as a cold one, since its rays are not in the memo.

## Decision

### 1. What ranks

Ranking applies when all of these hold:

- the departure is suggested, because the operator set no time;
- the request asked for comfort (`planAssessOptions.Comfort`);
- the comfort readers are wired (`planAssessDeps.comfort` is not nil).

A verdicts-only run, a slip price, a change mark, `get_plans` and a Mate answer
never rank. With comfort off the assessment judges the candidates as before, stops
at the first Go, and leaves `depart_basis` null.

With comfort on and able to rank (§4), every candidate is judged for its verdict, and the candidates
whose verdict is the best present are the tier: Go when any is Go, otherwise
Marginal when any is. Only the tier is ranked. Because every candidate in it has
the same state, the verdict never changes. When there is no tier, because every
candidate is a No-Go or the leg has a gap, today's choice stands (the No-Go closest
to its limit, the gap and beyond-the-forecast order of `departureOrder`), and
`depart_basis` stays null.

The verdict phase therefore does more work with comfort on. The tier needs the
verdict of every cell, so no candidate is skipped after the first Go, and every daylight
candidate is judged. What the budget of §5 bounds is the comfort walk, which
starts after the verdicts are in.

### 2. The key

Each candidate in the tier is judged hour by hour with the code the chip uses
(`legComfortInput`), and ranked by its judged hours:

1. The worst band, calmest first: Comfortable, Lively, Lively above every rated
   passage (the "rougher than any recorded passage" case of ADR 0302 §6), Rough.
2. The whole hours spent in that band, after the sliver folding of ADR 0302 §5,
   rounded up, so any time in a band other than Comfortable counts at least 1 h.
   Rounding to the nearest hour let a few minutes either side of the half hour
   decide the pick. Fewer is better. A Comfortable leg counts none, since
   Comfortable is Comfortable however long. The basis words these same whole
   hours, never minutes.
3. A candidate with no hour in seas steeper than any recorded passage goes before
   one that has such an hour.
4. The current. The first candidate that no later one beats wins, so two candidates
   that match on 1 to 3 keep the fairer stream first.

### 3. Hours that cannot be judged

A gap never reads as calm.

- A stretch not judged at the start or the end of the leg is ignored, whatever
  the reason: a berth or position charted ashore, an enclosed swell, a wind wave
  not checked against fetch, no split between swell and wind wave, no forecast for
  the hour. Real legs run from one marina to another, so their ends lie ashore or in
  enclosed water, and a rule that left those candidates unranked would almost never
  rank. The candidate is ranked on the judged hours between its ends. A position
  charted ashore says nothing more: a marina berth is ashore for the first hour of
  every candidate, and it says nothing about the sea. The basis says what was left
  out, and how depends on the reasons at that end:
  - When every reason is the shore's (an enclosed swell, a wind wave not checked
    against fetch, a position charted ashore), it adds "Not judged leaving harbour",
    "Not judged entering harbour" or "Not judged leaving and entering harbour".
  - When any reason is something else, such as no forecast figure for the hour, the
    word harbour would be untrue. The basis names the first such reason and the
    whole hours it covers: "Not judged for the last 2 h: the forecast has no figure
    for this hour, so comfort can't be judged."
  The chip still shows those hours as not judged, with their reasons. The ends are
  read on the stretches of ADR 0302 §5, so a sliver of a few minutes folds into its
  neighbour before the ends are found.
- Any other unjudged stretch leaves the candidate unranked: a gap with judged hours
  on both sides, including a position charted ashore in the middle of the leg, or a
  departure left out because its forecast could not be read (§5). So does a
  candidate with no judged hour at all, and one whose walk or comfort ladder (ADR
  0302 §4) blocks it, since the ladder reads the wave models over that candidate's
  own hours.
- A run that ran out of time is never an end to ignore, even for a sliver of a few
  minutes folded into an ashore stretch: any hour with that reason, read before the
  stretches are built, leaves the candidate unranked.
  Only the berth is ignored, never an island the route crosses.
- Unranked candidates come after every ranked one, in the order of the current.
- When some candidates in the tier rank and others do not, the basis ends with a
  sentence that counts them and gives the first reason, such as "3 of 8 times
  not judged: the comfort check ran out of time."
- When no candidate in the tier ranks, the choice is today's, and the basis says
  why. The chip is walked for that departure afresh, under the request's own time,
  as before ranking: hours the ranking walked partway, when its budget ran out,
  never become the chip.

### 4. When comfort cannot rank at all

The choice stays the fairest current's, and `depart_basis` says why:

- a reader is not wired, or the model, bands or helm height cannot be read;
- the bands are not learned. Without bands no hour has a word to rank by.

A comfort ladder block (the model is held back, was learned on another wave model,
or the plugin names no model) is not in this list. It is found over one candidate's
hours, so it leaves that candidate unranked and the ranking goes on (§3). When every
candidate meets it, none ranks and the basis gives the block as its reason; each
candidate is walked once to find that, which is accepted.

### 5. The budget and the order of work

The ranking runs after every leg's verdict (`judgeLegs`) and before the wind
changes, under a fixed 10 second budget, so a slow shore read never delays a
verdict. The budget bounds the comfort walk only; the verdicts of every candidate
come first and are not budgeted (§1). The budget is shared by the legs that rank, not spent in plan order: each
leg gets what is left of it divided by the legs still to rank, as a context of its
own. A leg that finishes early leaves its unused time to the legs after it, and a
slow leg can take no more than its share from them. When a leg's share runs out
partway, candidates not reached are unranked, each saying "the comfort check ran
out of time", and the basis counts them. If nothing in the tier was ranked, the
choice falls back to the current alone with that reason.

No block ends the ranking early. A setup problem is caught before the first
candidate (§4), and every block found later is one candidate's own (§3).

Judging every candidate also judges ones the early stop at the first Go never
reached. A candidate after a Go whose cell fails is logged and left out, neither
chosen nor ranked, and the leg keeps the verdict of the cells judged. It counts
among the candidates and the unranked in the basis, with the reason "the forecast
couldn't be read for this time". A failure at or
before the first Go fails the leg as it did, so comfort never hides a failure the leg
had before.

A failure that is the request's own time running out (cancelled, or past its
deadline) is not the forecast's. The verdict loop stops there. The candidate that
failed and every one after it count among the unranked with the reason "the comfort
check ran out of time", and nothing is logged for them. A leg with no Go or Marginal time has no tier, so it settles at once
with no choice held.

The departure, its current, the limiting figure and what was not checked are written
only after the ranking, from the chosen cell, so the wind changes and the arrival
nights follow the chosen departure. `judgeLeg` therefore keeps a leg's judged
candidates (`departureChoice`) when ranking is on, and `settle` writes them once
comfort has chosen.

The model, bands and helm height are read once for the request. The `ready()`
reader, shared by the ranking and by the chips, is read before any verdict, and the
assessment ranks only when it gives no problem and the bands are learned. When it
cannot, the legs stop at the first Go as they did, and each says why in its basis,
with no candidate judged for comfort to say so. The leg's chip is the chosen
candidate's judged hours, worded by `comfortOf`, with no second walk, only when the
chosen time was ranked: fully judged under the key. In every other case (nothing
ranked, a budget that ran out) the ranking gives no chip, and the chip
is walked afresh under the request's own time, as before ranking. A leg under way and a leg with an operator's departure keep the single
walk of ADR 0302.

### 6. The wire

`planLegAssessment.depart_basis` is a string, null unless the departure is
suggested, comfort was asked for and the leg had a tier to rank. The server words it
and the page shows it as a muted line under "Suggested 6.40 AM". The time's tooltip
is always the general account of how a suggested time is picked, so the basis is
not said twice and the account is never lost. The field is always on the wire, a
string or null.

- "Chosen for comfort: Comfortable throughout, then the fairest stream of 6 alike."
- "Chosen for comfort: Lively for 1 h instead of 3 h."
- "Chosen for comfort: Comfortable throughout instead of Lively for 1 h."
- "Not chosen for comfort: Comfort not learned yet. Fairest stream."
- "Not chosen for comfort: The comfort check ran out of time. Fairest stream."

"Instead of" names the departure with the fairest stream, which the ranking
replaced, when it was ranked and differs. "Of N alike" says how many ranked
candidates matched on the whole key, when more than one did.

`get_plans` does not rank, since comfort reaches Mate in a later step. It gains
`depart_basis` ("Fairest current, not ranked for comfort.") on a suggested
departure whose verdict is Go or Marginal, so the two answers do not seem to
disagree without saying why. A leg with no such time was chosen closest to its
limit, not by the current, and has no basis.

## What it is not

- Not a verdict. Go, Marginal and No-Go are the passage limits' and unchanged.
- Not a way to leave outside daylight. The candidates are the ones already inside
  the window.
- Not Mate's. Mate still reads `get_plans`, which does not rank.
- Not stored. The ranking is worked out when the page asks.

## When it can't run

- No passage limits, no forecast, or a leg with no verdict: nothing to rank.
- Every departure a No-Go: the least bad one, as before.
- Comfort that can't be judged for the tier: the fairest current's departure, and a
  basis that says why.

## Amendment 2026-11-26: get_plans ranks as the page does

`get_plans` now ranks a suggested departure for comfort, the same way the plan page
does, for every caller. The chat, the outside assistant and the alarm and watch
reviews therefore quote the departure and the `depart_basis` the page shows, where
before Mate could quote the first Go while the page had chosen a calmer hour.

The fixed line "Fairest current, not ranked for comfort." is gone. `get_plans`
passes the leg's own basis through: "Chosen for comfort: ...", or the reason
comfort could not choose. Slips and verdicts-only runs still never rank.

This reverses "Not Mate's" above, and the sentence that keeps `get_plans` out of the
ranking.
