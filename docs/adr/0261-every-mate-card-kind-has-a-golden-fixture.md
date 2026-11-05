# ADR 0261: Every Mate Card Kind Has Golden Fixtures

## Status

Accepted (2026-11-05). Extends ADR 0191.

## Context

A Mate card is written twice. Go builds the payload from a tool result and the
store saves it. The browser reads the stored JSON with a hand-written decoder in
`frontend/src/lib/mate-cards.ts`.

That decoder reads the keys it names and ignores the rest. It rejects a wrong
type or an unknown enum value, and it does not notice a key it never asked for.
So a field Go adds, or renames, can vanish from the card without an error, and
a field renamed on a strictly read key shows the operator "This card couldn't
be shown".

Two kinds, marina and anchorage, already shared a fixture. The other four
(passage window, telemetry chart, wind rose, tile) had browser fixtures typed
by hand, which prove only that the decoder agrees with itself.

## Decision

Every card kind has golden fixtures in `backend/testdata/mate_cards`. Go writes
them from the real builders, and the browser decodes every one.

- **Go writes them.** `mateCardGoldens` in `assistant_mate_card_golden_test.go`
  lists each fixture beside the builder that produces it. A builder runs the
  real tool (`assess_passage_window`, `show_telemetry_chart`, `show_wind_rose`,
  `show_tile`, `marina_details`, `anchorage_details`) on small neutral input,
  and `TestMateCardGoldens` compares the card byte for byte with its file.
- **The fixture is the stored form.** It holds what the thread reads back: the
  `kind` and `payload`, and for a passage window the row `id` and the
  `decision`. Those two are saved by the store, so the decided passage-window
  fixtures run the card through the store and its decision calls.
- **`go test -run MateCardGoldens -update` rewrites them.** The flag is shared
  with the event fixtures of ADR 0189. A payload change shows up as a fixture
  change in the same commit.
- **Every key must be read.** `mate-card-fixtures.test.ts` decodes each file
  through a recording proxy and fails on any key the decoder never touched. A
  short list of keys the card leaves unread on purpose stays in that file: the
  boat-time labels Go writes for Mate to quote beside the instants the card
  reads, and `depth.known`, which the card derives.
- **Real values are asserted.** The same file checks what each kind decodes to:
  verdicts, limits, sectors, stays, notes, decisions, views and missing readings.

## What keeps a fixture honest

- **No stale files.** `TestEveryMateCardKindHasAGoldenFixture` fails on any file in the
  directory that no registered builder writes, and on a registered fixture that is
  missing. Deleting a golden test leaves its file unwritten, and the check names it.
- **No unseen kind.** The same test reads the backend source for the kind of
  every `mateCard` built and wants a builder in the registry that produces a card
  of that kind. The scan reads typed, positional and elided literals (inside
  `[]mateCard` and maps), string literals and string constants, and refuses what
  it cannot read: a Kind that is neither, a literal with no Kind, and a card passed
  to `cards.put` that was not built in the call. A card built by a helper and
  handed to `put` as a variable makes the test fail, and the fix is to build it
  in the call or extend the scan.
- **The browser agrees on the list.** The browser test wants a fixture for every key
  of `DECODERS` (exported as `MATE_CARD_KINDS`) and none for a kind it cannot decode.
- **The types agree.** `DECODERS` must satisfy `Record<kind, decoder>` for every
  kind but `error`, and `AssistantCard` ends its `switch` in a `never` check. A
  kind added to the `MateCard` union without a decoder or a `case` stops the type check.

A kind that reaches `AssistantCard` with no `case` anyway, from a stale bundle,
renders an error card. It never throws, so one card cannot blank the thread.

## What it covers, and what it does not

Fixtures exercise the branches Go can produce on small input: verdicts, sectors,
factors, stays, notes and missing readings in a passage window; time and xy views, named
sources and comparisons in a telemetry chart; present and absent strongest gust
in a wind rose; full and missing tile snapshots; a marina with no berth count,
an exceeded limit and truncated geometry; an anchorage with and without a depth.

Not covered, because Go cannot produce them in a test without much more machinery
or the decoder keeps them for old cards:

- The `wind_against_tide` limiting factor (needs a tide provider).
- A `not_checked` entry for `daylight`, `gust` or `wave_direction`.
- A marina with saved documents (needs a document store).
- Anchorage error branches (forecast, tide and feature errors), restricted-area
  and seabed shapes, and a saved-place depth.
- A telemetry chart saved before panels, which carries `axis` and no `panel`.
- A wind rose with no mean speed.

A fixture proves the decoder accepts what Go writes and reads every key. It does
not prove the card renders well, and the hand-built browser fixtures stay for
testing one field at a time. The proxy check also cannot see a key that appears
only in some array elements when another element reads it.

## Rejected: one registry entry per kind

A single table holding decoder, lazy import and renderer would have made a new
kind one edit. It would not have removed any code. The decoders belong in
`lib/mate-cards.ts`, which stores and tests read without React, and the renderers
belong with the components. A table spanning both either pulls React into the
decoder module or makes it import the components. Per-kind props (`canWrite` for a
passage window, `live` for a tile, the tile's own fallback) would also have needed
a props type for every entry to keep the card narrowed. The type checks above
make a missed place a build error instead. The repeated `Suspense` wrappers
collapsed into one `CardSuspense` in `assistant-card.tsx`, and each card still
loads in its own chunk.
