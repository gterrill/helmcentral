# ADR 0134: A corrupt anchor watch file fails only the anchor watch

## Status

Accepted

## Context

The anchor watch and its placemarks moved to `data/` so they survive a
Docker redeploy (the persisted volume; `cache/` does not survive one).
Alongside that move, `loadAnchorWatch` stopped treating a corrupt
`anchor_watch.json` the same as a missing one — a decode failure now returns
an error instead of silently starting with no watch, on the reasoning that
the anchor alarm going silently absent is worse than saying so.

`main.go` wired that error the same way every other store's load failure is
wired: `log.Fatalf`. That is the right shape for a store this backend cannot
run without — the secrets store, the session store, the alarm rules. It is
the wrong shape here. A code review before this branch's release caught it:
one damaged `anchor_watch.json` — a truncated write, a bad upgrade, disk
corruption — would take the whole backend down, and with it every alarm
this boat has: bilge, battery, engine temperature, the drag watch on every
other vessel's anchor, all of it, over a file that describes nothing but
where one boat's own hook is set. The operator's call: a broken anchor watch
must fail on its own, loudly, without taking anything else with it.

Two more gaps came out of the same review. `json.Unmarshal` treats both `{}`
and a bare `null` as "leave the target at its zero value, no error" for a
non-pointer struct target, so either one would have decoded into a
`anchorWatchData` sitting at exactly 0,0 (Gulf of Guinea, "null island") and
been installed as a genuinely active watch — silently disabled in practice,
since a distance check against 0,0 from wherever the boat actually is will
always read as dragging, or never, depending on which side of that comparison
the bug lands on, neither of which is an alarm anyone would trust. And a
zero-length file — the shape an atomic write leaves behind if it is
interrupted after create/truncate but before the bytes land — was going
through the same "corrupt" path as a genuinely damaged one, where
`loadAlarmRules` already treats it as "no rules yet" (`alarm_rules.go`'s
`if len(data) > 0` guard).

## Decision

### The anchor watch gets its own explicit error state, not log.Fatalf

`main.go` no longer calls `log.Fatalf` when `loadAnchorWatch` fails. It logs
loudly, naming the file, and calls `recordAnchorWatchLoadFailure`
(`backend/anchor_watch_load_error.go`), which:

- Sets a package-level error string that `GET /api/anchor-watch` reports as
  an `error` field — naming the file path and the parse/validation error —
  alongside `active: false`. `anchorWatchState` is always nil when this error
  is set (`loadAnchorWatch` never installs a watch on failure), so the two
  can never disagree about whether a watch is active, and the endpoint never
  returns an invented or empty watch in the error's place.
- Raises a warning through the existing alarm/notification mechanism, the
  same static, non-rule-driven shape the collision-profile syncer's refusal
  and the stream watchdog already use for their own warnings
  (`collision_profile.go`'s `collisionProfileEvent`,
  `alarm_watchdog.go`'s `watchdogRule`) — not a new, bespoke path only this
  feature knows about. The path is `notifications.helmcentral.anchorWatch`,
  owned outright by the existing `helmcentral.` derived-namespace rule
  (`alarm_ownership.go`), so it needs no rule of its own to show up in the
  alarm list.

The rest of the backend starts normally. Every other alarm this process
runs is unaffected by a damaged anchor watch file.

### The bad file is left alone; recovery is manual

Nothing here rewrites or deletes the bad file automatically. Two existing
operator actions already do, and both now also clear the warning:

- **Dropping a new anchor** (`setAnchorWatch`) always writes
  `anchor_watch.json` atomically, whatever it held before. A fresh Drop is
  the ordinary recovery path, and it is also the only one the UI exposes
  when the tile shows no active watch — which is exactly the state a load
  failure leaves it in.
- **An explicit Raise** (`raiseAnchorWatch`) already removes
  `anchor_watch.json` unconditionally, regardless of whether an in-memory
  watch exists. `DELETE /api/anchor-watch` works the same whether the file
  was a good watch or a bad one.

Both call `clearAnchorWatchLoadFailure`, which is a no-op — critically, it
never calls `recordAlarmEvent` — when there was nothing to clear, so an
ordinary Drop or Raise with nothing ever wrong does not write a spurious
"resolved" entry into the alarm log on every use.

### A file that parses but isn't a real watch is corrupt too

`loadAnchorWatch` now validates a successfully-decoded record
(`anchorWatchData.validateLoaded`) before installing it:

- Lat and lon both exactly 0 — the shape `{}` and a bare `null` both decode
  to — is rejected as "no anchor position recorded".
- A radius at or below zero is rejected: a watch that can never trip is not
  a tight one, it is a disabled one wearing an active watch's clothes.

Either failure goes through the same path as a genuine parse error: an
explicit error state, never an active watch at 0,0.

### A zero-length file still means "no watch"

`loadAnchorWatch` returns early, with no error, for a zero-length file —
matching `loadAlarmRules`' own treatment of the same interrupted-write shape.
An empty file is not evidence of a damaged watch; it is evidence of a write
that never got the chance to happen.

## Rejected

### Retrying the load, or trying to repair the file

A truncated or corrupted write is not something the backend can safely infer
a repair for — guessing at a position from a partial JSON payload is exactly
the kind of fabricated data the fallback policy exists to prevent. An
operator dropping the anchor again is a real, current position; anything
this code could reconstruct from the wreckage of the old file would not be.

### Deleting the bad file automatically on load failure

Keeping the bad file on disk costs nothing and preserves it for whoever
next looks at why the watch went missing — an operator, or a later debugging
session. Overwriting or removing it automatically would erase that evidence
for a marginal convenience the two existing recovery paths (Drop, Raise)
already provide.

## Consequences

- A damaged `anchor_watch.json` is now a contained, visible problem: the
  Anchor Watch tile and page show it plainly, every other alarm keeps
  running, and the fix is the same Drop the operator would do anyway to
  re-anchor.
- `GET /api/anchor-watch`'s error field carries raw Go error text, including
  a file path, to the frontend. That is a deliberate exception to the
  no-implementation-detail rule for user-facing copy: this is a diagnostic
  aimed at whoever has to explain a damaged file, not a routine operator
  string, and it matches how the app already surfaces other raw failure text
  (e.g. a failed Drop/Raise's SignalK error).
- Anything that reads `notifications.helmcentral.anchorWatch` off the bus
  should not be surprised to see it — it behaves like every other
  Helmcentral-owned static warning already on that namespace.

## Related

- [ADR 0078](0078-anchor-lifecycle-signalk-synchronization.md) — the Raise
  publish/remove/clear sequence this ADR's recovery paths rely on is
  unchanged.
- [ADR 0099](0099-server-side-anchor-auto-raise.md) — `raiseAnchorWatch` is
  the one path both a manual Raise and the auto-raise watcher go through;
  this ADR's file-removal-clears-the-warning behaviour applies to both.
