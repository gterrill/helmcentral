# ADR 0155: Mate times departures for a fair tide

## Status

Accepted (2026-10-02). Builds on [ADR 0128](0128-mate-answers-questions-about-nearby-vessels.md)
and [ADR 0145](0145-mate-reads-the-maintenance-list.md) (Mate's tools: read
what the boat already knows, compute in Go, let the model explain), and on the
`get_tides` and `estimate_passage` tools whose station lookup and passage hours
this one reuses.

## Context

A skipper heading south asks Mate when to leave. The useful answer is a time
that lets the stream carry the boat: depart so the ebb or flood that sets
south runs for most of the passage, and avoid the departure that fights it
for hours.

The tide plugins give high and low water times and heights for the nearest
station. None of them gives tidal stream direction or rate, and no upstream
source of stream data has been identified for the places this boat goes. What
can be known is when water level turns. The stream's phase follows that
closely in many places, and its set direction is a fixed fact of the water
that a cruising guide, the tidal stream atlas, or the operator's own notes
already hold.

Left to the prompt alone, the model had to turn a list of high and low times
into "which half of the cycle is the boat in at each hour of a six hour run".
That is half-cycle arithmetic across a passage, and it gets it wrong in
plain ways: it counts the wrong extreme, confuses flood with ebb, or answers
for the departure time rather than the whole run.

## Decision

Mate has a read-only tool, `plan_tidal_departure`. It takes the departure
position, the passage course, the passage duration, the direction the flood
stream sets toward and where that direction came from.

- The stream's phase comes from the nearest station's high and low water
  times, found the same way `get_tides` finds them. Flood runs from low to the
  next high water, ebb from high to the next low, both shifted by an optional
  slack offset for places where the stream turns later or earlier than the
  water level does.
- Within each half-cycle the strength follows a half sine: nothing at slack,
  a peak mid-cycle. Multiplied by the cosine of the angle between the course
  and the flood's set, that gives help along the track, positive when fair.
- Departures every 15 minutes across the window are scored by sampling that
  figure every 5 minutes along the passage: the mean help (-1 to 1) and the
  share of the run that is fair. The tool returns the best three
  distinct departures (local maxima at least two hours apart), the single
  worst, the stream phases each crosses, and the arrival times.
- The set direction is the one input the tool cannot know. Mate supplies it
  from the operator's standing notes first, else from general knowledge, and
  states which in `flood_set_source`; the result echoes it so the answer can
  label it, and the prompt tells Mate to say the direction is to be checked
  against the tidal stream atlas and cruising guide. If Mate does not know
  which way the flood sets in that water it says so and does not call the
  tool.
- Stream rate is not modelled. The result carries a fixed `basis` sentence
  saying so.
- A course within about 15 degrees of square to the stream still gets a
  result, with a note that timing the tide barely changes the help along the
  track.
- The tool fails fast. No tide provider, no station, or extremes that do not
  reach back to the first departure and forward past the arrival of the last
  all return an error that says what is missing. There is no guessing of
  missing extremes and no fallback to a generic cycle.
- The prompt asks Mate to weigh the departure against the forecast wind: where
  the stream sets against the wind during the run the seas steepen and shorten,
  and Mate says so and says when the fair-tide departure lands in it. The rule
  applies only to a passage leaving from the boat within the forecast range.
  The "planning question" case (later season, somewhere else) still reasons
  from general knowledge.

## Alternatives considered

- **Prompt only.** Give Mate the high and low times and ask for a fair-tide
  departure. Rejected: this is the arithmetic the model got wrong. A
  deterministic tool makes the same inputs give the same answer.
- **A tidal stream provider.** A plugin type returning stream direction and
  rate by position and time. It would be right where this guess is rough, but
  it is a cycle of its own (a new provider interface, a catalogue per region)
  and no upstream source has been identified. This tool does not preclude one:
  a provider would replace the supplied set direction and the half-sine.

## Consequences

- Accuracy is limited where slack water is far from high and low water, as in
  many rivers, narrows and the funnels between islands. The operator can
  record a slack offset in the standing notes and Mate passes it through, but
  without one the timing is the water-level phase.
- In deep water with weak streams there is little to gain, and the result
  says as much only through the cross-stream note and the small spread
  between best and worst. Mate's own judgement fills the rest.
- The set direction from general knowledge is not a measurement. It is
  labelled as such every time, which is why the source is a required argument
  and not an inference.
- Stream rate is absent. A recommended departure says which phase to ride,
  not how many knots to expect.
- The tool needs the extremes to bracket the passage, so asking for a
  departure near the start of the day's data or beyond the provider's horizon
  fails with a message naming the gap, rather than answering from a partial
  cycle.
