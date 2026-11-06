# ADR 0267: The Operator Can Ask For Coastline Squares

## Status

Accepted (2026-11-06). Extends [ADR 0203](0203-offline-coastline-and-anchorage-shelter.md).
Nothing it decided about the ring, the edit check or Mate changes.

## Context

The offline coastline held only the squares within 200 nm of the boat, plus
whatever the boat had held before. An operator planning a passage had no way to
fill the coast ahead while the boat was still on a good link. Mate could name
the missing square, but the only way to get it was to sail into range.

## Decision

The operator picks squares on the coverage map in Settings → Offline coastline,
and the worker downloads them ahead of the top-up around the boat.

### Picking

- **Select squares** puts the map in select mode and **Done** leaves it. A tap
  toggles one 1° square and a drag selects a block. The map does not pan in
  select mode, on a desktop or a touch screen.
- Selection works only when the map spans about 30° or less. Beyond that the
  page says "Zoom in to select squares". The grid of not-held squares is drawn
  only then, so the page never draws a thousand cells.
- Selection is off while the switch is off, with "Turn on the offline coastline
  to download squares". A request never turns the switch on, and the server
  refuses a request or a removal while it is off.
- Squares at the poles or beside the 180° meridian cannot be selected. The
  server refuses them with the same limits as the boat's ring.
- A selection may mix held and missing squares. The action bar shows
  **Download N** and **Remove M**, each behind its own confirm.

### The server

- Requested squares live in a `coastline_requests` table in the coastline
  store, so a restart or a switch turned off and on loses none of them. A row
  carries the request number and the square's place in the selection.
- `POST /api/offline-coastline/request` stores a request. With `dry_run` it
  counts what it would do and stores nothing, so the confirm can state how many
  all-land squares it will skip before the operator agrees.
  `POST /api/offline-coastline/remove` deletes held squares. Both are admin
  tier, like Update and Clear.
- One request adds at most 100 squares, counting only the ones to download.
  Held squares, all-land squares and squares already waiting do not count. The
  server refuses the whole request over the cap and stores nothing.
- The worker takes an operator-requested update first, then a requested
  square, then the nearest wanted square in the ring. Requests run in the order
  made. Within one, the square nearest the boat goes first, or the selection
  order with no position fix. A requested square needs no fix.
- A downloaded square drops its request in the same transaction that saves it.
  From then on it is an ordinary held square: kept until removed or cleared,
  checked weekly, counted by Update, and backed off after a failure like any
  other. A failed requested square keeps its request and its failure row, so
  the retry waits and the next request goes ahead.
- **Clear coverage** deletes the requests as well, and its confirm says so.
  Turning the switch off keeps them and they resume when it is turned on.
- **Remove** deletes held squares and cancels waiting requests, with the failed
  row a request left behind, and drops a queued update for them. A square
  inside the 200 nm ring downloads again by the top-up, and the confirm says so.
  A plugin call in flight for a removed square is discarded, and no other call
  is: a per-square mark, not the generation Clear bumps, so removing one square
  never loses an unrelated download.
- **Refusals are worded for the operator.** The server text for the cap, the
  pole limit and an empty selection is what the confirm shows, so it reads as
  a sentence ("Select fewer.") and not as an error code.

### Status

Each square in the status carries `requested`. The status adds
`requested_waiting` and `requested_failed`, and the state reads "downloading"
while a requested square is due. The page shows
"Requested: 9 waiting, 1 failed (reason)", with "(offline coastline is off)"
added while the switch is off. Requested squares have their own shade on the
map. A failed one is red with its reason, like any failed square. The map never
refits after a request.

## Alternatives rejected

- **Put requests in the in-memory queue.** A restart would lose them, and the
  switch turning off empties that queue.
- **Raise the ring's radius.** It pays for squares the operator does not want,
  at every position, and still cannot reach a coast across an ocean.
- **Let Mate request squares.** Out of scope. Mate keeps naming the missing
  square and points to Settings → Offline coastline.
- **Send the all-land mask to the browser** to count skipped squares. It is an
  8 KB file the page would carry for one number. The dry run asks the server.

## When it cannot help

- With no signal, or with the map server refusing the boat, requests wait. The
  page says why through the same line it uses for the ring.
- A request gives no size or time estimate. Nothing records a per-square size,
  so the confirm leaves it out.
