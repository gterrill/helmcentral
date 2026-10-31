# ADR 0243: An Anchorage Shelter Claim Needs The Shoreline Check

## Status

Accepted (2026-11-01). Extends the draft check from ADR 0234. The
`anchorage_shelter` tool is unchanged.

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

### The forecast carries the shelter

Mate called `get_wind_forecast` for the anchorage in the Thomas Island run, as it
does for every anchorage, and skipped only `anchorage_shelter`. So the shelter
rides with the forecast. When the install has an Offline coastline,
`get_wind_forecast` estimates the shelter at the same latitude and longitude
(the estimate `anchorage_shelter` makes, shared in one helper) and labels what it
returns, the way `rel_wind` labels rows against `course_deg`:

- **Each row:** `shelter` is the grade of the 10° bearing nearest the direction
  the wind comes from: `sheltered`, `partial`, `exposed` or `reef`. A row with no
  wind direction gets none.
- **Each day:** `shelter_prevailing` is the most common row label, ties going to
  the first seen.
- **The result:** `shelter_summary` (the estimate's one-line summary) and
  `shelter_reading` (the coastline's date). The 36-row bearing table stays with
  `anchorage_shelter`, so the forecast stays small.
- **Open water:** when no bearing is better than exposed, the forecast adds
  nothing. A position with no shoreline has no shelter to report.
- **A coastline that can't be read:** when the coastline is off, still
  downloading or doesn't cover the position, the forecast still returns, with
  `shelter_unavailable` carrying the error text unchanged and a log line. An
  install with no coastline reader at all adds nothing.

Reading the shoreline and estimating a position takes about 16 ms on a warm
database (26 ms the first time), so there is no cache.

The prompt and the tool description say shelter labels come with the forecast and
that every shelter claim comes from them or from `anchorage_shelter`.

### The draft check is the backstop

A model can still skip both, or judge an anchorage it never forecast. The draft
check from ADR 0234 gets a third trigger, "anchorage shelter". It sends the draft
back when a sentence both judges shelter or exposure and names a wind or a
direction, and the run made no `anchorage_shelter` call and no `get_wind_forecast`
call whose result carried shelter. The correction tells the model to call
`anchorage_shelter` with the anchorage's position (from `find_places`, or the
boat's position if it is where the boat is now), or to remove the statements if
they are not about an anchorage.

- **How the check knows what a forecast returned:** the run loop keeps each call's
  result alongside the call, by index, and passes both to `checkDraft`. The check
  reads `shelter_summary` from a `get_wind_forecast` result. A failed call has no
  result, and `shelter_unavailable` and open water leave `shelter_summary` out, so
  none of them satisfies the check.
- **What counts as a judgement:** exposed to or in, exposure to, open to, in the
  lee of, sheltered from, shelter from or against, protected from, lee shore,
  roadstead, and "well", "poorly", "badly", "fully" or "partly" sheltered or
  protected. Bare "sheltered" does not count, so "the sheltered marina" passes.
- **What counts as a direction:** a compass point in words or capitals (NE, north,
  northerly), or a word like wind, breeze, trades or swell, in the same sentence.
- **Independent of the passage triggers:** it is gated on the install having an
  Offline coastline reader, which is the one condition `anchorage_shelter` has for
  being offered. It does not read the coastline store or the on/off setting per
  draft. It still fires when `assess_passage_window` is absent or was called,
  since that tool says nothing about an anchorage.
- **Sent back once per run, not once per trigger:** the run loop allows a single
  send-back. A draft that hits more than one trigger gets one message carrying
  every correction. If the second answer still fails, it is accepted and the log
  names what it still had.

## Alternatives rejected

- **A stronger prompt rule.** The rule is already there and was ignored.
- **The draft check alone.** It costs a model round every time it fires, and it
  only withdraws a wrong claim. Putting the answer in the forecast Mate already
  fetches means the right figure is in front of it the first time.
- **Running `anchorage_shelter` ourselves whenever `find_places` returns a
  place.** It would spend a shoreline read on every place lookup, and an install
  without the Offline coastline would fail on questions that never asked about
  shelter.
- **One send-back per trigger.** It would let a draft cost two extra rounds and
  loosen the limit ADR 0234 set.

## Consequences

- Every forecast for a position near shoreline costs one coastline read, about
  16 ms, and a few hundred bytes of result.
- A draft that mentions shelter and a direction costs one more round if neither
  the tool nor a labelled forecast was used. Marina descriptions that name a wind in the same sentence as
  "protected from" are sent back too, and the tool answers them from the
  chart, so the cost is one round.
- Any `anchorage_shelter` call, or any labelled forecast, in the run satisfies
  the guard, even one that errored or was for another place. The guard checks
  that the shoreline was read, not that it was read for the anchorage under
  discussion.
- The correction covers both an anchorage and the boat's own position, and
  tells the model to drop the statements when they are about open water.
- The phrase list will miss some wordings. It does not check that the final
  answer agrees with the tool, only that the tool ran.
