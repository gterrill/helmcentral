# ADR 0243: An Anchorage Shelter Claim Needs The Shoreline Check

## Status

Accepted (2026-11-01). Extends the draft check from ADR 0234 and leaves the
`anchorage_shelter` tool unchanged.

## Context

Asked about a passage to Thomas Island South (20°32.676'S 149°07.136'E), Mate
said the southern tip was exposed to the north-east wind forecast for that night
and the next day. It told the operator to look for a nook on the north side
instead. The bay faces south and the wind was northerly, so the bay is sheltered
and the north side is where the wind arrives. The advice was backwards.

The run called `find_places`, `estimate_passage`, `get_wind_forecast` twice,
`get_tides` and `plan_tidal_departure`. It never called `anchorage_shelter`,
though the prompt says to call it whenever Mate chooses or assesses an anchorage
against a forecast. The answer even admitted it was "general knowledge of the
coastline". This is the same failure as ADR 0234: the prompt asks for a tool
call and the model skips it.

## Decision

The draft check gets a third trigger, "anchorage shelter". It sends the draft
back when a sentence both judges shelter or exposure and names a wind or a
direction, and `anchorage_shelter` was not called this run. The correction tells
the model to call the tool with the position from `find_places` and to base the
judgement on its sheltered, partly sheltered and exposed arcs against the forecast
wind, or to remove the statements.

- **What counts as a judgement:** exposed, exposure to, sheltered from, shelter
  from or against, protected from, lee shore, roadstead, and "well", "poorly",
  "badly", "fully" or "partly" sheltered or protected. Bare "sheltered" does not
  count, so "the sheltered marina" passes.
- **What counts as a direction:** a compass point in words or capitals (NE, north,
  northerly), or a word like wind, breeze, trades or swell, in the same sentence.
- **Independent of the passage triggers:** it is gated on the install having an
  Offline coastline reader, which is the one condition `anchorage_shelter` has
  for being offered. It does not read the coastline store or the on/off setting
  per draft. If the coastline is off, the tool errors and Mate says so. It still fires when `assess_passage_window` is absent or was
  called, since that tool says nothing about an anchorage.
- **Sent back once per run, not once per trigger:** the run loop allows a single
  send-back. A draft that hits more than one trigger gets one message carrying
  every correction, so the second answer can fix them all. If the second answer
  still fails, it is accepted and the log names what it still had.

## Alternatives rejected

- **A stronger prompt rule.** The rule is already there and was ignored.
- **Running `anchorage_shelter` ourselves whenever `find_places` returns a
  place.** It would spend a shoreline read on every place lookup, and an install
  without the Offline coastline would fail on questions that never asked about
  shelter.
- **One send-back per trigger.** It would let a draft cost two extra rounds and
  loosen the limit ADR 0234 set.

## Consequences

- A draft that mentions shelter and a direction costs one more round if the tool
  wasn't called. Marina descriptions that name a wind in the same sentence as
  "protected from" are sent back too, and the tool answers them from the
  chart, so the cost is one round.
- Any `anchorage_shelter` call in the run satisfies the guard, even one that
  errored or was for another place. The guard checks that the tool ran, not that
  it ran for the anchorage under discussion.
- The correction covers both an anchorage and the boat's own position, and
  tells the model to drop the statements when they are about open water.
- The phrase list will miss some wordings. It does not check that the final
  answer agrees with the tool, only that the tool ran.
