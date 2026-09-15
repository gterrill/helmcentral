# ADR 0099: Server-side anchor auto-raise

## Status

Accepted

Supersedes the auto-close clause of ADR 0078 ("Preserve browser scope and the
existing feature toggle").

## Context

Automatic Raise on departure ran in the browser, in
`use-anchor-watch-auto-close.ts`. Every open tab ran the same rule on its own
copy of the telemetry: any main engine above 0 rpm, the boat more than radius
plus 4.572 m from the anchor, both for five seconds. Each tab that decided the
boat had left sent its own `DELETE /api/anchor-watch`.

On 2026-09-15 the anchor went down from the flybridge at 13:31:14 local.
Forty-four seconds later the flybridge PC, an iPad and the wall kiosk each
sent DELETE within 0.4 s of one another. The engines were running and the
boat was outside the circle, which is what setting the hook looks like to a
GPS. The watch was dropped again at 13:34:56, and a laptop tab raised it at
13:44:28, six minutes after the engines last reported. The server log can't
say whether a person pressed Raise on that laptop or its tab acted on
readings it had held since the boat was motoring. A DELETE looks the same
either way.

That exposes four problems with the browser rule:

- Every open screen is a separate decision-maker, and they race.
- A tab that is asleep, backgrounded or cut off keeps its last rpm and
  position. The server throws away rpm older than 30 s. A tab can't.
- The on/off toggle lived in each browser's localStorage, so turning it off
  on one screen left it on everywhere else.
- It only worked while a dashboard was open, unlike drag detection.

Moving the rule to the server fixes all four. It would not have prevented
the 13:31 raise, though. The engines really were running and the boat really
was outside the circle. The rule also needed a way to tell backing down from
leaving.

## Decision

### The rule

`anchorAutoRaiseWatcher` in `backend/anchor_auto_raise.go` runs on a 5 s
ticker next to the drag watcher. With a watch active and the setting on, it
raises the watch when all of these have held for 15 s straight
(`autoRaiseSustainSeconds`):

1. At least one main engine has rpm above 0, read through `readEngineRPM`,
   which already returns -1 for a reading older than `defaultRPMMaxAge`.
2. The position is usable: `hasUsableVesselPosition` (range check plus the
   -1,-1 and 0,0 sentinels) and no `GNSSCriticalAlert`.
3. Distance from the anchor is more than `radius_meters` plus
   `anchorDragBufferMeters` (4.572 m, the same buffer the drag alarm uses).
4. Speed over ground is at least 3.0 kn (`autoRaiseMinSOGKts`). Unavailable
   SOG is -1 and never qualifies.

If any condition fails on any tick, the 15 s count starts over. A failed
SignalK read, stale rpm or missing SOG counts as not met. The watcher never
fills in a value.

### Speed over ground, not a grace period

Backing down and leaving both put the boat outside the circle with an engine
running. Speed is what differs. Backing down, the boat stops once the rode
comes tight. Leaving, it keeps going and passes 3 kn within seconds. A fixed
grace period after the drop would only guess how long setting the hook
takes. Get it too short and it fires on a slow set. Get it too long and a
quick departure goes unraised. The 15 s window covers the surge of a hard
back-down that briefly passes 3 kn before the chain stops it.

The trade-off is that a boat idling away from the anchorage below 3 kn keeps
its watch up. That is the safe direction to be wrong, because the drag alarm
still fires, and the operator raises the watch by hand.

### One raise path

`raiseAnchorWatch` in `backend/anchor_raise.go` publishes a null
`navigation.anchor.position`, removes the local record, and clears the
watch, the trail and the session placemarks. `deleteAnchorWatch` and the
watcher both call it under `anchorLifecycleMu`, so a manual Raise and an
automatic one leave SignalK, disk and memory in the same state. The error
carries which step failed, so the HTTP handler keeps its separate 502 and
500 responses.

The watcher decides without holding the lock, so a Drop can land between its
decision and the raise. Under the lock, the raise checks that the active
watch still has the `set_at` it evaluated. If a new session has started, it
raises nothing and the count starts again.

A raise is attempted once per unbroken run of the conditions. If it fails,
the watcher logs it and waits until the conditions break and re-form before
trying again. The watch stays up and the boat stays outside the circle,
so the drag alarm already tells the operator. No second notification path
was added.

Each automatic raise logs the rpm, distance, radius and SOG it acted on, so
the log now shows why a watch came up.

### One setting

`anchor.auto_raise_on_motoring` in `settings.yaml`, default true, handled the
same way as `anchor.gps_from_bow_m`. Because a Go bool can't tell "saved
false" from "never saved", `buildSettingsPayload` applies the true default
before overlaying what is on disk. The Settings toggle writes this value.
The old localStorage key is no longer read. With one operator and no
installed base, it gets no migration.

### Telling the screens

A successful raise records `{at, reason}` in memory, and
`GET /api/anchor-watch` returns it as `last_auto_raise` whether or not a
watch is active. Every client already polls that endpoint. When `at`
changes, the client shows the toast "Anchor watch raised automatically:
engines running, under way outside the zone". The first poll after a page
load only records the value, so a raise from before the page opened doesn't
show as new. A restart clears the record, which costs nothing but a toast.

The browser hook, its test, the drawer's countdown and the unused
`autoCloseAnchorWatchOnEngine` config field are gone. The countdown had
nothing honest to show once the timing moved to the server.

## Rejected

- **A grace period after the drop.** It guesses how long setting the hook
  takes. See above.
- **Coordinating the tabs** through a lock or a leader. That fixes the
  race but keeps stale browser readings in charge.
- **Pushing the raise over SSE.** The poll already reaches every screen
  within 5 s while a watch is active, and a raise is rare.

## Consequences

One place decides, on telemetry the server has already checked for age, with
or without a screen open. The simultaneous raise of 13:31 can't happen
again, and a tab left asleep can't raise the watch. Auto-raise now depends
on SignalK carrying speed over ground as well as rpm. If either goes
missing, the watch stays up and is raised by hand.
