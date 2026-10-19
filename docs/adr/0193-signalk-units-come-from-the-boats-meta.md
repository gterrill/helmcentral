# ADR 0193: SignalK units come from the boat's meta

## Status

Accepted (2026-10-20). Extends [ADR 0141](0141-one-business-database.md)
(storage) and [ADR 0184](0184-one-transaction-convention-for-the-record-store.md) (RunTx).

## Context

Alarm cards and notifications convert a reading into the operator's units from
the unit its path declares. That unit is SignalK `meta.units`, and
`unitForAlarmPath` read it off the path's node in the snapshot tree. The stream
client never put it there: it dialled `/signalk/v1/stream?subscribe=none`
without `sendMeta`, so the server sent no meta at all, and `signalKUpdate` had
no field to receive it either. Against the live boat `GET /api/signalk/paths`
reported units on 8 of 387 paths, all derived `helmcentral.*` paths whose units
come from `derivedPathUnits`. Every real path resolved to "", so alarms showed
the raw SI figure with no unit.

The bug pre-dated this work and the tests missed it because they wrote `meta`
straight into the tree. That is the shape the REST tree has, not the shape the
stream produces, so the lookup was tested against data the running process could
never hold.

## Decision

- **Ask for meta.** The stream URL carries `sendMeta=all`. Meta arrives as
  updates of their own, with no `values` and no `$source`:
  `{"updates":[{"timestamp":..., "meta":[{"path":"...","value":{"units":"ratio",...}}]}]}`.
  `signalKUpdate` gains `Meta`.
- **Units live in a map, never in the tree.** `applyDelta` reads only
  `meta.units` from each entry and keeps it in one snapshot-owned map, self
  vessel only, updated under the snapshot's write lock. Meta is not a reading:
  it does not touch `pathSeen`, and it does not enter the snapshot tree.
  Merging it there was tried first and rejected: a meta path through a scalar
  (`name`) replaced the scalar, meta for paths nothing publishes grew
  value-less nodes without bound, and every deep copy of the tree carried the
  whole meta payload. A tree that holds only readings also means no walker has
  to know about `meta`. An empty path, a radar target path or a path over the
  segment cap is dropped. The map is capped at `signalKContextMaxDistinctPaths`.
- **One map, no rollback.** A declaration updates the in-memory map at once
  and, if it changed the unit, marks the path pending. Memory is never put
  back: a failed write does not change what readers see. An earlier design
  kept a live map beside a remembered one and reverted the remembered unit on
  a failed write; it could lose a newer declaration (A, then B fails, then C),
  so it was dropped.
- **Keyed by vessel.** Rows are `(vessel, path, units, declared_at)`, where
  vessel is the self context id from the stream's hello frame. Units are
  loaded and served only for the current self vessel; when the self context
  changes the map is reloaded for the new vessel, so a path the current boat
  never declared reads "" even if another boat declared it. The table is new
  with this change and has no installed base, so a table without the vessel
  column is dropped and recreated at start-up.
- **Batched writes.** The stream goroutine never touches SQLite. Pending
  entries are written in one `RunTx` by a background goroutine: the first
  pending declaration arms a 2 s timer, and everything pending by then goes in
  one transaction. The first burst after subscribing (about 600 declarations)
  is therefore one transaction. An entry leaves the pending set only when a
  write that carried its current value succeeds. If a write fails it is logged
  once for that flush, memory keeps the declared unit, the entries stay pending
  and the next flush, 2 s later, retries them.
- **Shutdown.** `main` has no signal handling, so the flusher's flush when its
  context ends does not run in production. Pending units are written within
  about 2 s of the declaration; a restart inside that window loses them until
  the device declares them again, which SignalK does on every subscribe
  (`sendMeta=all`).
- **One lookup.** Every reader of a unit resolves it the same way:
  `meta.units` on the tree node (the REST tree carries it), else the self
  vessel's declared unit, else "". `Sample`, the unit of a path with meta but
  no value, the path picker's list, the Mate path samples and
  `unitForAlarmPath` all go through it. A live view reads the map inside the
  read lock it already holds (a second read lock inside the first would
  deadlock behind a queued writer); a view that copies the tree also copies the
  map once when it is built, so it never takes the snapshot lock per path.
- **No guessing.** A path that never declared a unit stays "". Inferring one
  from the path's name was rejected: a wrong unit makes an alarm say a false
  number, and the fallback policy prefers an honest blank.

## Consequences

- Alarm cards and notifications for SignalK paths show their unit and
  converted value, where before they showed the raw figure.
- The unit of a device that is offline comes from what it last declared. If the
  device changes unit while Helmcentral is down, the first declaration after
  reconnecting overwrites the old one.
- The first burst after subscribing now carries about 600 meta frames beside
  the values (measured 2026-10-20: 3,085 frames, 947 KB in about 8 s, largest
  frame 1.3 KB). The 4 MiB read limit is per frame and is not approached.
- A change of unit is durable about 2 s after it is declared. A restart or
  crash inside that window loses it until the device declares it again on the
  next connect.
- Each map holds at most `signalKContextMaxDistinctPaths` paths.
- The czone switch reader skips `meta` in SignalK's REST tree, which always
  carried it beside a node's children and was being read as a switch.
