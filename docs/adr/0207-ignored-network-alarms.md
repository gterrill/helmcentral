# ADR 0207: The Operator Can Stop Showing a Network Alarm

## Status

Accepted (2026-10-24)

## Context

Other devices on the boat raise alarms through the SignalK notifications tree,
and `signalKNotifications` (`backend/alarm_notifications.go`) shows every live
one (ADR 0038). Some of them are not actionable. The plotter's
`perpendicularPassed` and `arrivalCircleEntered`, raised from APB sentences
(ADR 0144), are true for the whole of a leg: the boat has passed the waypoint,
the plotter says so, and nothing at the helm can make it stop. They sit on the
board, hold the banner up and ride along in every push notification until the
route ends. An operator who has learned to look past them also learns to look
past the board.

Silencing and acknowledging do not help. Both are SignalK actions, so they
change what every other device on the network does with the notification, and
an acknowledged alarm still occupies the board.

The sensor ignore list already lets the operator tell the
sensor-health checks to skip a sender, but it is a different thing: it filters
what Helmcentral's own checks look at, not what other devices raise. It was also
kept in `alarm-rules.json`, the rule file, with no date.

## Decision

1. **Ignore is per alarm kind, whichever device raises it.** The kind is the
   notification path relative to the boat (`perpendicularPassed`,
   `navigation.arrivalCircleEntered`). Ignoring "Waypoint Passed" ignores it
   from the plotter and from anything else that raises the same path. The
   plotter's two route alarms (`perpendicularPassed`, `arrivalCircleEntered`)
   are one kind whether raised with or without the `navigation.` prefix, and
   are stored as the bare name.
2. **It is hidden everywhere Helmcentral shows alarms.** The filter sits in
   `signalKNotifications`, beside the owned-path, stale-sentence and departed-
   beacon drops. Both consumers go through it: `activeAlarms()` (the board, the
   banner, the SSE stream, the heartbeat and Mate's `get_active_alarms`) and
   `busNotificationWatcher.check` (push and the other transports, and the alarm
   history). A live alarm disappears on the next read. Nothing is acknowledged
   or silenced on the SignalK server, so other devices keep their own alarm.
3. **Ignoring a raised alarm looks like it clearing.** The watcher sees the
   notification leave the list and reports it the usual way: the log row closes
   and the transports send their "over" message. A genuine clear of an ignored
   kind that was showing as an emergency is reported the same way.
4. **Restoring is a fresh raise.** The watcher forgot the alarm when it was
   hidden, so if the condition is still live after a restore it serves the
   normal dwell and raises again: a new notification and a new history row.
5. **Safety alarms can never be ignored.** One function,
   `alarmKindIgnorable(kind, state)` (`backend/alarm_ignored_alarms.go`), is the
   only place the list lives. It refuses: the SignalK emergency kinds (`mob`,
   `fire`, `sinking`, `flooding`, `collision`, `grounding`, `listing`, `adrift`,
   `piracy`, `abandon`), `navigation.anchor`, `navigation.closestApproach`
   (which includes the Collision Watch fault), received distress of any kind,
   radar guard zones, and any alarm in the `emergency` state whatever its kind.
   The server computes it and sends it on the alarm as `ignorable`; the card
   offers the action from that flag alone and shows nothing otherwise. `POST`
   answers 400 for a kind the function refuses. A kind that somehow reached the
   list is still never hidden, and an ignored kind that arrives in the
   emergency state is shown.
6. **The action is on the network alarm card only.** "Stop showing" sits beside
   Acknowledge, with a confirmation that says the alarm stays off the board and
   out of notifications until restored in Settings. Banner, push and Mate have
   no such action. Mate can read the ignored titles in the result of
   `get_active_alarms` so it can say why it has not heard of an alarm, and has
   no tool to add or remove one.
7. **Restore is in Settings → Alarms.** An "Ignored alarms" list shows each
   kind by the title the card used (the server's presenter), the date it was
   ignored and a Restore button. The ignored-sensors list shows its date too.
8. **Both ignore lists live in `helmcentral.sqlite`.** `ignored_alarms (kind,
   ignored_at)` and `ignored_sensors (identifier, ignored_at)`, Unix seconds,
   next to the alarm log. They are held in memory and written through, so the
   anomaly tick and every alarm list read them without touching the database.
   On boot, a non-empty `ignored_sensors` in `alarm-rules.json` is inserted into
   the table (dated at the move, `INSERT OR IGNORE`), then the file is rewritten
   without the field. The insert commits first, so a crash between the two steps
   repeats harmlessly. A failure stops the boot. The legacy field lives only on
   the struct the move reads and can be deleted in a later release.
9. **Routes mirror the sensor ones.** `GET`/`POST /api/alarms/ignored-alarms`
   and `DELETE /api/alarms/ignored-alarms/:kind`, read and write tier (ADR
   0177), both writes idempotent.

Separately, in the same release, the two plotter alarms are reworded for the
watchkeeper (ADR 0157): "Waypoint Passed" and "Arriving at Waypoint", each with
one plain sentence. The APB notification carries no waypoint name or figures,
so the body states only what the flag states.

## Rejected

- **Per device.** The same kind from a second plotter or a plugin would come
  straight back, and the operator thinks in "that alarm", not "that box".
- **Per occurrence.** It would have to be repeated at every leg, which is the
  problem being solved.
- **Keeping the lists in `alarm-rules.json`.** It is the rule file, edited by
  hand and rewritten on every rule save; a dated, queryable list belongs with
  the alarm log. Moving sensors at the same time avoids having two homes for the
  same idea.
- **A greyed-out button on safety alarms.** A control that can never work is
  noise at 02:00. The card shows nothing, and the server refuses anyway.
- **Letting Mate ignore alarms.** Hiding an alarm is a decision about what the
  crew will be told; it stays with the person at the screen.
- **Keeping ignored alarms in the history.** The history is read as "what
  reached us". A hidden kind that still logged would contradict the board and
  clutter the log with the very alarm the operator asked not to see.
- **Acknowledging on the SignalK server.** It silences the plotter and every
  other client, and an acknowledged alarm still shows.

## Consequences

- A plotter alarm the operator ignored is invisible in Helmcentral for as long
  as it stays on the list, including its history; the plotter itself still
  shows it.
- Ignoring a raised alarm sends one "alarm over" message, as a clear would.
- The first boot after upgrading moves the sensor list and dates every entry at
  that boot. Nothing for the operator to do.

## Related

- [ADR 0038](0038-alarms.md): alarms and SignalK notifications.
- [ADR 0144](0144-stale-nmea-notifications-expire.md): the APB alarms go stale.
- [ADR 0157](0157-alarm-cards-say-what-happened-not-where-it-came-from.md): alarm wording.
- [ADR 0141](0141-one-business-database.md): one SQLite file.
