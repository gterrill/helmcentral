# ADR 0260: A Tile Is A Draft Until It Is Saved

## Status

Accepted (2026-11-05). Moves the lifecycle that ADRs 0012, 0049, 0054, 0109
and 0188 describe into one module. It changes none of their decisions.

## Context

Six kinds of tile need a dialog before they are valid: gauge, gauge group,
engine cluster, indicator strip, embed and Nearby map. The backend rejects a
blank embed, a gauge with no path and an empty gauge group, so a new tile
cannot reach the page until the operator has filled it in.

`AppShell` held that rule six times. It had six draft states, six add
callbacks that each worked out the lowest row on the page, and five save
callbacks that each either replaced a tile's config in place or appended the
draft. A seventh check listed all six states to learn whether a dialog was
open. A change to placement or to saving meant six edits, and a seventh
kind meant copying the whole set again.

## Decision

`hooks/use-tile-draft.ts` owns the lifecycle, and `AppShell` holds one draft.

- **A new tile exists only as a draft.** Adding a tile writes nothing.
  Cancelling drops the draft and leaves the page as it was.
- **Placement:** A new tile or a copy sits at the left edge, below the lowest
  tile on the page, so it never lands on another.
- **Save:** The draft's config replaces the tile's config when the tile is
  already on the page, and the draft is appended when it is not. Opening a
  saved tile's gear fills the same draft, so editing and creating share one
  save.
- **Duplicate from the dialog:** The edits land on a new tile, and the
  original keeps what it had when the dialog opened. A draft never saved gives
  one tile, not a draft and a copy.
- **Duplicate from the grid:** The copy is written first, because a copy is
  already valid, and its dialog opens on it.

Only one dialog is on screen at a time, so one draft is enough. A dialog can
still finish after it has gone, though. The cluster dialog saves its profile
zones first, and Duplicate in the gauge group dialog waits for the same. Cancel
during that wait, open another tile, and the old dialog's Save or Duplicate
lands on a draft that is no longer its own. So every open gets its own entry,
and a save, a duplicate or a cancel applies only to the entry it began with:

- **Cancelled or replaced meanwhile:** The late finish writes nothing and
  closes nothing. A cancelled tile never appears, and the dialog on screen now
  keeps its edits.
- **A write that fails:** The save waits for the page write. When it fails
  (`updatePage` has already raised the toast), the draft stays open so the
  operator can retry.
- **A write that is out:** A second Save, or the dialog closing itself after
  Save, does nothing until the answer lands. Without that, a double click
  would append the tile twice.
- **No page:** Nothing is written and the draft stays open.

A dialog saves its own kind of tile, and `saveTile` takes the kind with the
config, so a wrong wiring fails to compile. A built-in tile has no kind, and a
mismatch throws rather than writing a config under the wrong key.

## What each kind keeps

The module holds the lifecycle. Each kind keeps its own start, in one table:
its id, footprint and blank config.

| Kind | Footprint | Blank config |
| --- | --- | --- |
| Gauge | 3 x 6 | Numeric, no path |
| Gauge group | 4 x 7 | One blank gauge |
| Engine cluster | Tall, from `CLUSTER_DEFAULT_SIZE` | An RPM ring and an hours centre |
| Indicators | 12 x 3 | One blank lamp, titled Status |
| Embed | 6 x 8 | No title, no URL |
| Nearby map | 12 x 7 | Split layout, every category, AIS on |

Two differences stay in the code rather than behind a flag:

- **A gauge is raised on save** to the height its display needs, new or
  existing, so changing a gauge to radial never leaves it cut off.
- **Only a gauge group has Duplicate in its dialog.**

The embed and Nearby map dialogs close themselves after saving and pass the
tile's id with the config. The shell ignores the id, because the draft already
holds it.

## What did not change

The tile ids, the stored layout, the Add Tile menu, the dialogs and the
requests sent to the backend are the same. The two fixes above are the only
difference an operator can see.

## Rejected

- **A draft per kind inside the module:** It would have moved the six states
  without removing them.
- **One generic save that takes the config key as a string:** A typo would pass
  the compiler. The kind names are the layout keys, so the kind itself is the
  key and a wrong pairing fails to build.
- **Closing the dialog before the write lands:** A failed write on a flaky
  boat network would drop a new cluster's config along with its toast.
