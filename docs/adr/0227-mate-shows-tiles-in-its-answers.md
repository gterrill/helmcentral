# ADR 0227: Mate shows tiles in its answers

## Status

Accepted (2026-10-29). Extends [ADR 0191](0191-mate-cards-come-from-tool-results.md)
(cards come from tool results) with a `tile` kind, and registers its tool under
[ADR 0172](0172-mate-tools-are-one-record-each.md). This step enables one tile,
Depth & Tide. Wind, Tanks, Battery & Power, Solar and Position are designed for
and follow as one table row each.

## Context

Mate already answers with pictures. A weather window is a grid, a telemetry
chart is a plot, and ADR 0221 added a wind rose that Mate built as a card of its
own, because no dashboard tile could show where the wind had come from over a
week. Asked "what's our depth?", though, Mate answers in a sentence, and the
operator then goes to the dashboard to see the tile they already know.

The tiles already render live data. Each one reads the app-wide stores (vessel
state, the tide poll, the depth trend poll) through a `Live` wrapper, and draws
it with a pure component that takes plain props. The picture exists. What Mate
lacks is a way to put it under an answer without writing a second renderer.

Two facts shape how. A card sits in the history, and the prose beside it was
written about the figures Mate read. ADR 0191 found this for `passage_window`:
a card that recomputes on reopen can say "go" beside text that says "no-go".
And only some tiles can be shown at all. The gauge tiles read `gauge-values`,
which streams only the paths a dashboard page is bound to, so on a chat page
they would draw blank. The generator, CZone switches and autopilot tiles carry
controls that would reach the boat outside the changeset consent of
[ADR 0158](0158-mate-changes-records-through-changesets.md).

## Decision

**`show_tile(tile)`, a read tool with a table.** `mateTileTable` in
`assistant_tile_card.go` maps a tile id to its title, the function that reads its
figures and a note on the snapshot's units. The tool's `tile` enum is built from
the table, and a tile outside it is refused with the tiles on offer. The design
allows six built-in tiles (depth-tide, wind, tanks, battery-power, solar,
position). Only `depth-tide` is in the table in this step.

**The server reads the figures once.** For Depth & Tide it reads what
`LiveDepthTideTile` reads, from the sources the live app reads:

- depth, its age and the navigation state from `fetchSignalKVesselState`, the
  reader the vessel-state stream uses (ADR 0178);
- the tide from `buildTideToday`, the builder behind `/api/tide-today`, which
  uses the configured station;
- the three hour depth trend from `buildDepthTrend`, the builder behind
  `/api/depth-trend`, thinned to 48 points with the newest kept.

Both builders were lifted out of their HTTP handlers so the handler and the tool
share one body. A reading that is missing is `null` in the snapshot and is named
in the result's `missing` list: "depth: no depth reading", "tide: no tide station
is set up". A tide failure is mapped to a plain sentence for the model and the raw
error goes to the log. A current tide height under zero counts as missing, because
the tile draws a dash for it. The trend list is `[]` when empty, never `null`. Nothing is filled from an older reading. A depth older
than the stale limit is kept, because the tile marks it stale itself, and the
list says so.

**Clock times are boat time.** The `display` figures give the next high and low in
the zone the app derives from the vessel's position. With no usable position they
give the instants as UTC and no clock time, and the note says so.

**The card and the model get the same snapshot.** The stored payload is
`{tile, captured_at, snapshot}`. The tool result carries that snapshot byte for
byte, with `display` figures in the operator's units, `missing`, a note on units
and an instruction. Mate's words and the frozen card quote the same numbers. The
card goes through the existing card sink, so there is no new SSE event.

**Live while newest, frozen after.** `AssistantThread` finds the newest assistant
message, and it counts only if the reply arrived during this viewing of the
conversation (`liveAnswerIds` in the Mate session). A chat reopened from the Chats
list, or a page reload, reads its replies back from the server, so every tile in it
is frozen. Its tile cards render the live tile and a small "Live" marker, with no
timer of their own, since the stores already update. The moment a newer turn starts
(a question sent after it, or a reply being worked on) it is frozen, so the tile
never moves under words that no longer describe it. Every older reply renders the
same tile component on the stored snapshot, labelled with the time it was read
("14:03"). The dashboard already splits each tile into a pure component and a
`Live` wrapper, so the frozen card renders the pure component with the snapshot as
props. No store is overridden, nothing is fetched, and the live and frozen cards
cannot drift apart, because they are the same markup. A tile that lacks the split
gets it when it joins the table.

**Read-only, sized like the dashboard.** The card passes no `onOpen`, so nothing
on it is a button. Its box is the tile's default dashboard height (rows times the
grid row height and gaps, 320 px for Depth & Tide) at the full width of the
thread, because `Tile` fills its parent. The renderer loads lazily, as the other
cards do, so the tile code stays out of the kiosk chunk. The tile still follows
the Settings units for distance, so a card from an old reply shows its stored
metres in whichever unit the operator uses now.

**Prompt.** Answer in words first and quote the key figure, then let the card
follow. Call `show_tile` when the picture adds something ("what's our depth?"),
not for a question about whether a sensor works ("is the sounder working?"),
which is answered from the readings.

**Typed, with a golden fixture.** The Go struct is `mateTilePayload`. The
`message_source.json` fixture gains a tile card, and the strict TypeScript decoder
reads the snapshot field by field. A malformed card becomes an error card.

## Alternatives considered

- **Always live.** Rejected. An old reply would show today's depth beside
  yesterday's sentence about it, the contradiction ADR 0191 found in
  `passage_window`.
- **Freeze on the client only.** Rejected. A snapshot kept in the browser is lost
  on reload and never reaches the model, so the words and the card could
  disagree.
- **Collapse an old card to a stub.** Rejected. The history loses the picture the
  answer was written beside.
- **Attach tiles on the server by rule,** such as a depth tile on every depth
  question. Rejected: the model knows whether the picture adds anything, and the
  prompt rule keeps that judgement with it.
- **Allow any tile.** Rejected, for the reasons in the context. Gauge tiles would
  draw blank, and the controllable tiles would offer a way to act on the boat that
  skips the changeset consent.
- **Rebuild the wind rose as a tile.** Rejected. The rose counts logged history,
  and InfluxDB is optional, so it cannot be a tile that every boat can place.

## Consequences

- A new tile is a row in `mateTileTable`, a snapshot decoder in `mate-cards.ts`, a
  branch in `AssistantTileCard` and its default size. Its pure component must take
  its figures as props.
- A tile card stores about 48 trend points and one tide record, a few kilobytes,
  under the 12,000 character cap for a tool result.
- A reply whose card was stored as the newest turns frozen the moment a newer
  assistant reply arrives, and reopening an old conversation shows every tile
  card frozen except the last reply's.
- The tide time in a frozen card is shown in the browser's zone, as on the
  dashboard.
