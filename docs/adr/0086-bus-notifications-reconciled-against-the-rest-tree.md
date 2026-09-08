# ADR 0086: Bus Notifications Reconciled Against the REST Tree

## Status
Accepted

## Context

Helmcentral's SignalK model (`signalKSnapshot`, `backend/signalk_snapshot.go`)
is fed only by the delta stream (`backend/signalk_stream.go`): subscription
`context: vessels.*`, `path: *`. Bus alarms shown in the Active Alarms drawer
are rebuilt from that snapshot on every read (`signalKNotifications` in
`alarm_notifications.go`, `signalKCollisionNotifications` in
`collision_ais.go`, merged in `activeAlarms`, `alarm_service.go`). Nothing
ever re-reads the server, so a notification the server has forgotten stays
live in Helmcentral until that exact path happens to change again, however
long that takes.

Three confirmed ways that happens, against the boat's own signalk-server
2.24.0:

**The server restarts.** The boat log for 2026-09-08 shows the stream
reconnecting at 02:11Z and 02:12Z with "connection refused". signalk-server's
`NotificationManager` (`src/api/notifications/notificationManager.ts`) keeps
alerts in an in-memory Map, and the delta cache starts empty on restart, so
nothing replays the old notifications and nothing clears Helmcentral's
copies. Acknowledging one afterwards answers `signalk returned status 400:
{"state":"FAILED","statusCode":400,"message":"Alarm not found!"}`. That
manager throws exactly that string for an id it no longer holds.

**The stream drops the final value inside a debounce window.**
signalk-server implements `policy: instant` with `minPeriod` using Bacon's
`debounceImmediate(minPeriod)` (`src/subscriptionmanager.ts`): the first
value in the window passes through, and every later one is dropped, not
deferred. A path that raises and clears inside the same one-second window
loses the clear outright. `policy: fixed` with `period` instead buffers the
window with `bufferWithTime(period)` and delivers the latest value per
(context, `$source`, path) once it closes, so the final state in any window
is never lost. Measured against the boat, fixed costs more frames than
instant (210/s against 159/s; `subscribe=all` alone was 333/s), accepted for
the correctness it buys.

**The server forgets a cleared alert 60 to 120 seconds later.**
`NotificationManager.clean()` runs every 60 seconds and deletes any alert
that has stayed `normal` since the previous run. The v1 REST tree keeps the
path with `state: "normal"`; only the id vanishes from
`/signalk/v2/api/notifications`.

Live evidence captured 2026-09-08 ~05:20Z: the server's `GET
/signalk/v1/api/vessels/urn:mrn:imo:mmsi:503135940/notifications` (JARRICKI)
reports `navigation.closestApproach` as `state: "normal"`, message
"Watching", timestamp `2026-09-08T03:43:01Z`, while Helmcentral on the boat
still listed that vessel's CPA alarm as `warn`, acknowledged. Nothing had
re-read the server between the two states.

REST behaviour confirmed against the boat: `GET
/signalk/v1/api/vessels/self/notifications` returns 200 with the
notifications subtree (each leaf `{"meta":{...},"value":{...},"$source":
"...","timestamp":"...",` and sometimes `"pgn"`/`"sentence"`); the same GET
for an unknown vessel id returns 404. Both captures are kept as fixtures at
`backend/testdata/signalk_notifications/self.json` and `other_vessel.json`.

## Decision

**Reconcile the notifications subtree against the REST tree every 30
seconds**, rather than trusting the delta stream to eventually correct
itself. `signalKSnapshot.reconcileNotifications` (`signalk_snapshot.go`)
replaces the notifications held for one context with the server's answer to
`GET .../notifications`, leaf by leaf, and a `notificationSyncer`
(`notification_sync.go`) drives it once per tick: the self context always,
plus every other known vessel context currently holding a live notification
(collision alarms live on their target's own context, not self).

**A leaf a delta updated at or after the read began wins over the server's
answer.** The syncer records `readStartedAt` before calling GET; a merge rule
keeps our copy untouched whenever `pathSeen` for that leaf is at or after
that timestamp, and only takes the server's word otherwise. This is what
makes the reconcile race-free without a lock spanning the network call: a
delta that lands mid-read is guaranteed newer than anything the read can
possibly say, so there is no window where a slow REST response can stomp a
value the stream has already corrected.

**A 404 means the context has no notifications at all**, not an error to
retry or ignore. Every notification for that context is dropped. This is
deliberately not "if the count differs, investigate"; a vessel dropping off
AIS should leave no orphaned collision alarm behind either.

**A failed bus action forces an early resync.** `alarmActionHandler`
(`alarm_service.go`) already distinguishes a refusal (409, the server was
never asked, e.g. an emergency) from an upstream failure (502, the server
was asked and something went wrong, which is the same shape as "Alarm not
found!"). On the 502 path it now calls `notificationSyncer.invalidate()`
before returning, so the next tick re-reads immediately instead of leaving a
stale card up for the rest of the interval. The error returned to the
caller is unchanged either way. This never substitutes a success for a
failure; it only makes the next correction arrive sooner.

**The stream subscription changed from `instant`+`minPeriod` to
`fixed`+`period`** (`signalk_stream.go`), for the reason in Context above:
instant+minPeriod silently drops a clear that lands in the same window as a
raise. The server source shows that directly; whether it is what took the
JARRICKI clear, or the restart did, the boat log cannot say, and it does not
matter, because both are covered. The two fixes are complementary. Fixed
policy prevents one specific way a clear goes missing; the REST reconcile
catches every other way a notification goes stale, including ones no stream
policy can fix (a restart, the server's own 60-120s forgetting).

**The reconcile runs after the rule engine evaluates and before
`globalBusNotificationWatcher.check`, in the same tick.** A correction that
clears the last live leaf for a path is therefore visible to the watcher
immediately: the alarm-log row closes and a "cleared" push goes out on the
same tick the correction happened, not up to 30 seconds later on some
future tick.

## Rejected

**Resetting the snapshot's notifications on every stream reconnect.** This
looked like the obvious fix for the restart case, but it fires on every
reconnect, not only ones where the server actually forgot something. The
bus watcher would see every live alarm vanish and reappear, closing log rows
and paging "cleared" then "raised" for conditions that never actually
cleared. A brief network blip would look like a night of alarms clearing and
re-raising.

**Reconciling by the v2 notifications id list
(`/signalk/v2/api/notifications`) instead of the v1 per-vessel REST tree.**
The v2 list is exactly what `clean()` prunes, so it lags the true state by
60 to 120 seconds even when nothing else is wrong, and it only carries
notifications that were ever assigned an id: a producer that writes the
tree directly without going through the Notifications API is invisible to
it. The v1 tree used here has no such gap.

**Filtering stale notifications at the alarm layer instead of correcting the
snapshot.** `activeAlarms`, `signalKNotifications` and
`signalKCollisionNotifications` are not the only consumers of the snapshot;
patching each read site would leave the underlying tree wrong for whatever
reads it next. Fixing `signalKSnapshot` itself means every current and
future consumer sees the same corrected state.

## Consequences

- Worst case, a notification the server has forgotten stays visible for up
  to 30 seconds after the server itself cleared it, down from indefinitely.
- One log line per actual correction (`notification sync: <context>
  notifications.<path> corrected against the server ...`); a persistently
  wrong copy is corrected once, not repeated every tick, so this cannot
  flood the log.
- Roughly a third more stream frames (measured 210/s against 159/s) from the
  fixed+period subscription change.
- An acknowledge or silence issued inside that 30-second window can still
  show the server's own "Alarm not found" error before the card disappears
  on the next correction. The resync on failure (above) shortens that
  window to about a second rather than removing it, since the action itself
  still has to reach the server and fail first.
