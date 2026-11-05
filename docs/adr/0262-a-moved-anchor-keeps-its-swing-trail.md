# ADR 0262: A Moved Anchor Keeps Its Swing Trail

## Status

Accepted (2026-11-05). Reverses decision 3 of
[ADR 0199](0199-anchor-swing-trail-has-its-own-stored-record.md) for a moved
anchor.

## Context

ADR 0199 restarted the swing trail whenever the anchor position changed, on the
reasoning that the trail measured the boat against one anchor position. It does
not. The table stores the boat's own fixes as absolute positions, and nothing
reads them relative to the anchor. Correcting the mark in Adjust therefore threw
away a true record, often the very swing that told the operator the mark was
wrong. Undo wiped it as well, so backing out of a move could not bring it back.

## Decision

- **A drop restarts the trail.** A POST with no active watch deletes the rows and
  stamps a new start, and a failed reset still fails the drop with a 500.
- **A position PATCH keeps the trail.** That covers Adjust and its Undo.
- **A re-POST with an active watch keeps the trail.** This is the same correction
  by another route, and retrying a drop whose SignalK publish failed lands here.
- **A raise ends the trail**, by the operator or the auto-raise, as before.

`trail_started_at` now changes only on a drop. The frontend poller needs no change:
an unchanged start means it keeps its points and its `since` cursor.

## What goes away

- The move guard, `anchorTrailMoveMu`. A tick taken during a move is still a true
  fix, so it may land in the trail. A drop does not need the guard either: until
  the new watch is installed, the trail endpoint answers empty and the recorder
  skips, because no watch is active. A tick that read an older watch as active is
  rejected in its own transaction, sampled before `started_ms`.
- The rollback for a failed reset during a PATCH. A move no longer touches the
  trail store, so a missing store cannot fail one. The rollback for a failed local
  save stays, and its message no longer mentions the trail.

## Considered options

- **Keep the trail only for short moves.** Rejected: any threshold would wipe a
  true record somewhere, and the operator would have to guess which side of it a
  move fell on.
- **Add a separate control to clear the trail.** Not built. Nobody has asked, and
  raising and dropping again already gives a clean trail.

## Consequences

After a long move the old swing stays on the map around the old position until
the raise. The trail is capped at the newest 10000 points as before, so a move
does not change how much is kept.
