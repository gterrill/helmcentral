# ADR 0254: Helmcentral converts the positions the skipper types

## Status

Accepted (2026-11-03). Extends [ADR 0243](0243-an-anchorage-shelter-claim-needs-the-shoreline-check.md) (shelter
labels and the draft check) and
[ADR 0234](0234-an-answer-about-wind-and-course-needs-the-passage-check.md) (the draft check for passages). Touches
the `save_place` tool of
[ADR 0158](0158-mate-changes-records-through-changesets.md).

## Context

On 2026-11-03 the skipper wrote `Whites Bay (021º40.271' S / 150º17.341' E)`. That is latitude -21.671183,
longitude 150.289017. Mate did the conversion itself and called its tools with latitude -21.67785, about 0.4 nm
south. The shelter assessment and the passage estimate both used the wrong spot, and the next turn reused the
figure from Mate's own earlier answer.

The boat relies on these positions. A model converts degrees and minutes by arithmetic it cannot check, and
it states the result with the same confidence whether it is right or wrong. The tool descriptions even told it
to do the sum: `save_place` carried a worked conversion as an example.

## Decision

Helmcentral reads the positions in the skipper's own messages and hands the model the decimals.

- **A strict reader on one rule** (`assistant_operator_positions.go`). A position is a latitude half and a
  longitude half side by side, each with a hemisphere letter, a degree mark and a minute mark. The letter comes
  before or after, the halves in either order, and the marks are the ones the dashboard's coordinate field accepts.
  That field also takes unmarked `20 32.676 S`; this reader does not, because it sees free text.
- **Why that rule.** The first versions accepted more, and four rounds of review ran them on sentences.
  `Mark 3 21.6712 S 150.2890 E` read as 3° and 21.67 minutes. `Variation 12° E, 5° S` and
  `wind S 12.5 E 15.5 knots` read as positions and would have stayed in the prompt for the rest of the conversation.
  A minute mark alone was not enough: `Mooring 5 10' E 6 20' S` is feet. Each false reading drew another special
  case, and the cases could not be completed while the signal stayed weak. A degree mark and a minute mark together
  are a signal little else in a message carries. They are also the form that needs arithmetic.
- **Decimals are not read.** `-21.67, 150.28` and `21.6712 S` need at most a sign, which a model does not get
  wrong the way it gets a division wrong.
- **It leaves out anything it cannot read exactly.** Minutes or seconds of 60 or more, a latitude past 90, a
  longitude past 180, a lone half and two latitudes all give no position. A run of halves with only separators
  between them must pair off with none left over, and a blank line ends a run. A half that failed its checks, and a
  figure shaped like a half such as `150° E`, `20.5 S` or `40 16' S`, still count in the run and void it on the same
  line: otherwise the longitude of one position pairs with the latitude of the next. A leading letter that is
  followed by a closing letter was a label, as in `Mark E 12°30' N 61°30' W`, and voids it too. A wrong position
  handed over as fact is worse than none, and each refusal costs the skipper only a rewrite.
- **`find_places` comes second.** A dozen instructions and tool descriptions say to take coordinates from
  `find_places`, which would pull the model off the skipper's figure. One sentence in the stable prompt says a
  position the skipper gave for that place comes first, wherever they say so, and the shelter send-back, the last
  thing the model reads, says it directly.
- **Where it goes.** `beginAssistantTurn` reads every skipper row in the conversation, newest first, drops
  repeats and keeps at most eight. The live part of the system prompt lists each as written, with latitude and
  longitude to six places. A turn with no position adds nothing, so its prompt is unchanged byte for byte, and the
  stable part that is cached never changes.
- **The rule it states.** Use these decimals exactly in tool calls, and do not convert a position yourself. A
  newer position replaces an earlier one, including a correction in a form the reader refused, which the list
  cannot show. Where one message gives several, its words decide, since "use X, not Y" and "not Y, use X" put the
  right one in opposite places. The skipper's figure outranks an earlier answer of Mate's, which is how
  the bad figure carried over, and a `find_places` result for the same place. It says nothing about a different
  place.
- **Rows read.** Only the skipper's rows. An assistant row, a watch report and an attached document's text are
  not scanned, so Mate's own figure never becomes the skipper's.
- **The model is told not to do the sum.** The stable prompt says never to convert a position the skipper gives
  in degrees and minutes, and to ask for a rewrite when no converted figure is listed. `save_place` carried a
  worked conversion as an example; its description now says to copy signed decimals as written and to use the
  converted figures otherwise.

## Rejected

- **A conversion tool the model must call.** It works only when the model remembers to call it, and the failure
  here was a model that was sure of its own answer. It also adds a round trip to every turn that has a position.
- **Regular expressions in the draft check.** The check runs after the draft, and it can only send the draft
  back. Reading the position before the model starts is cheaper and gives it the right figure the first time.
- **Reading decimals and bare numbers.** A rule loose enough to catch them turns depths, winds and courses into
  positions.
- **Sharing a grammar with the dashboard's coordinate field.** The Go reader cannot import the TypeScript, and the
  two do different jobs: that field holds one coordinate the skipper meant as one, and this reader searches free
  text. They disagree on purpose where shown above.

## Consequences

- A position in the skipper's message reaches the model as decimals, with no tool round.
- Positions written in a form the reader refuses reach the model unconverted, and the prompt tells it to ask for
  a rewrite. So does a position older than the eight kept.
- A position in an attached document or a web result is not read, and the model may still convert one itself.
  The rule covers only what the skipper gives. The same mistake is possible there, and is left for later.
- A waypoint number straight before a position voids it: `Mark 3 21°40.271' S 150°17.341' E` asks for a rewrite.
  Reading past the number would mean guessing it was not part of the position.
- Nothing yet enforces that the model uses the figure. The draft check does not compare the arguments of a tool
  call with the converted position, so a model could still ignore it.
