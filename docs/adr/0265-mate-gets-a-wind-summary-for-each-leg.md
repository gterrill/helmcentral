# ADR 0265: Mate Gets A Wind Summary For Each Leg

## Status

Accepted (2026-11-06). Builds on ADR 0191's `assess_passage_window`. The card
keeps its shape.

## Context

On 2026-11-06 the operator planned Island Head Creek to Long Beach, Great Keppel
for Saturday 7 November, then Pancake Creek and Lady Musgrave. Mate
(deepseek-v4-pro) wrote: "this is a dead-to-windward run the whole way (course
170°, wind N-NE)". Open-Meteo had the wind from 348° to 031° all morning along
the route. On a 160° to 170° course that is astern.

The tool was right. `assess_passage_window` returned that leg as "go" with the
reason "Gusts 16 kn ahead · Island Head Creek → Long Beach, Great Keppel". The
route's first segment runs about 1.4 nm north-east out of the creek, straight
into the wind at 7 AM. The ahead sector has the tightest limit, so that one hour's
gust became the limiting figure, and the tool printed it as the reason for the
whole leg. Mate read "ahead" and generalised. Two things let that through:

- The reason names one hour. The model gets nothing about the other five.
- The gust line leaves out the wind direction, which the wind line has. A reader
  cannot tell a gust from the north from one from the south.

## Decision

### Each leg gets a wind summary

For every cell, `assess_passage_window` adds one line to what the model reads: the
range of wind directions and speeds over the leg, then the hours the leg runs
with the wind in each sector, longest first.

```
Wind from NNW–NNE 6–15 kn: astern 5.5 h, ahead 0.5 h (first 0.5 h)
```

- **Hours:** Each forecast stamp counts for the part of its hour that lies inside
  the leg, so a leg's sector hours add up to its length. The summary uses the
  same stamps the cell judges for its limiting line, taken at the stamp's position
  on the route. A stamp with none of its hour inside the leg (the one before a
  06:40 departure) stands for the conditions at departure or arrival: its wind
  joins the ranges, and its sector reads "ahead at departure" when it has no
  hours of its own.
- **Direction:** The smallest arc of 16-point directions that holds every hour,
  so a wind backing across north reads "NNW–NNE", not the long way round. An arc
  of half a circle or more reads "variable". Hours under 1 kn have no direction,
  so they count in the speed range and as "calm 1.0 h", not as ahead, abeam or
  astern. The cell still judges them by their sector, so verdicts don't move. A
  leg calm throughout reads "Wind calm 0 kn".
- **Gaps are stated.** Hours of the leg with no forecast end the summary as "no
  forecast 2.0 h", and break a run. The cell is `no_data` as before.
- **Ends of the leg.** A sector whose hours would print as 0.0 h is listed like
  an end stamp, "ahead at departure", "at arrival", or "at departure and
  arrival", so a few minutes' change in departure doesn't change the form. A
  sector is a stretch when it holds under half the leg's hours, counting the
  hours with no forecast.
- **A short stretch is named.** A sector under half the leg gets a bracket.
  When its hours form one unbroken run, the bracket says where, from the edges of
  the hours counted: "first 0.5 h", "last 1.0 h" or "1.0–3.0 h in". On a leg
  with several route segments, a run that fills one segment names it instead.
  Hours in separate runs read "in 2 stretches", since one range would be false.
  A leg of one segment is always placed by position, because the segment is the
  leg.
- **The card does not change.** The summary is not part of the card payload, so
  the frontend and its fixtures stay as they were. A leg with no forecast hour
  has no summary: the cell is still `no_data`.
- **The chat cap trims it early.** When the result is too large, the go cells'
  reasons go first, then the go cells' summaries, then the other summaries, then
  the remaining reasons, so the figure that decided a marginal or no-go cell, and
  the summary beside it, outlast everything on a go cell.

### The model is told what the field means

The tool description and the passage-window rule say that `wind` is how the wind
sits over the whole leg, that the leg's angle comes from it and not from `why`
(the deciding hour), that "at departure" or "at arrival" means only that end, and
that "no forecast" hours are unknown.

### The wind and gust lines match

The limiting line puts the speed first and the direction after it, for both:
"Wind 16 kn from N, ahead · Island Head Creek → Long Beach" and "Gusts 16 kn from
N, ahead · …". A gust used to leave the direction out, and the wind line put it
before the speed. The card shows the new text, and the golden fixtures for the
passage window card changed with it. The summary reads "Wind from N 10 kn: …" so
it can't be mistaken for a limiting line.

## Not done

A draft check that compared the answer's wind-angle claims with the summary was
built and dropped. Phrase matching flagged answers that had already been
corrected, missed wording such as "A to B", and was switched off by unrelated
negatives like "no-go". If the summary alone doesn't stop the mistake, revisit it
through the Mate replay and judge loop, not more phrase matching.

## Alternatives rejected

- **Printing the hour's direction in the reason only.** It tells the model the
  gust was from the north but not that the rest of the leg was astern.
- **Judging a leg by its average direction.** One angle for a leg that bends or
  leaves a creek the wrong way hides the stretch the limit applies to.
- **Putting the summary on the card.** The operator reads the card for the verdict
  and the deciding hour. Six more numbers per cell would change the layout for a
  fault in the model's reading.

## Consequences

- The model result grows by one line per cell.
- Hours are counted at the forecast's own resolution along the route, so a
  segment shorter than the distance covered in an hour can be missed entirely or
  counted for the whole hour. The 1.4 nm creek exit appears as the first half
  hour because the leg starts on it.
- The summary and the limiting line always use the same stamps, so a reason such
  as "Gusts … ahead" never names a sector the summary leaves out.
