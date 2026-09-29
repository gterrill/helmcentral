# ADR 0144: Stale NMEA 0183 Notifications Expire

## Status

Accepted

## Context

`signalKNotifications` (`backend/alarm_notifications.go`) surfaces every live
notification on the boat's instrument-network tree as an alarm, reconciled
against the server's REST tree every 30 seconds by `notification_sync.go`
(ADR 0086). That reconcile catches a notification the server itself has
forgotten - a restart, a debounced clear the stream dropped, its own 60-120s
cleanup sweep. It does nothing for a notification the server has **not**
forgotten, because nothing about it looks wrong to the server at all.

signalk-server's NMEA 0183 parser raises `notifications.arrivalCircleEntered`
("WP arrival circle entered!") and `notifications.perpendicularPassed` from
APB sentences, and only clears one when a *later* APB sentence arrives with
the flag unset. These two have no timeout of their own: the parser re-asserts
the same notification on every repeat of the sentence, typically once a
second while the plotter is actively steering a route, and stopping the route
on the plotter (or losing the NMEA 0183 feed entirely) simply stops the
sentence arriving. Nothing ever tells signalk-server the condition ended,
because from its point of view nothing changed - the state just stopped being
updated.

Live evidence captured off the boat, 2026-09-29:

```json
GET /signalk/v1/api/vessels/self/notifications/arrivalCircleEntered
{"meta":{},"value":{"method":[],"state":"alarm","message":"WP arrival circle entered!","id":"489f8f89-...","status":{"silenced":false,"acknowledged":true,"canSilence":true,"canAcknowledge":true,"canClear":false}},"$source":"notificationApi.AI","timestamp":"2026-09-28T02:43:31.332Z","sentence":"APB"}
```

The APB stream behind it stopped at `2026-09-28T00:52:49Z`. More than a day
later the notification was still `state: "alarm"` on the server, `canClear:
false`, and the 30-second REST reconcile had nothing to correct: the server's
own copy agreed with Helmcentral's the whole time. Every consumer downstream
of `signalKNotifications` - the alarms list, `worst`, the bus watcher's
raise/clear/dispatch, the alarm log - kept the alarm open indefinitely.

The leaf itself carries the one piece of evidence that scopes a fix to
exactly the right notifications: a `sentence` field naming the NMEA 0183
sentence that produced it (`"APB"`). Victron, N2K devices, other SignalK
plugins and Helmcentral's own rule alarms never carry `sentence` - they are
event-driven, raised once and cleared once, not a message repeating on a
fixed cadence.

The delta stream does not carry `sentence` where the REST tree does. The REST
tree puts it on the leaf beside `value`, `$source` and `timestamp`; a delta
carries it only inside the update's `source` object
(`{"sentence":"APB","talker":"AI","type":"NMEA0183","label":"notificationApi"}`,
captured from the boat's server on 2026-09-29). `applyDelta` copied `value`,
`timestamp` and `$source` onto each leaf and nothing from `source`, so a
delta-fed leaf never had a `sentence` until the 30-second REST reconcile
(`reconcileNotifications`) replaced it with the server's copy.

**A first version of this fix judged staleness by the notification leaf's own
`timestamp`, and that was wrong.** A leaf's `timestamp` is not "when the APB
sentence was last seen" - it is "when this leaf was last written," and the
SignalK Notifications API writes it for reasons that have nothing to do with
APB. Acknowledging or silencing a notification from any client - an MFD, a
phone, another Helmcentral - makes the API re-emit that notification's own
leaf: the same `id`, the same original `sentence` and `source`, a freshly
stamped `timestamp`, and no route data anywhere in the update. That is
exactly what produced the captured evidence above: the APB feed actually died
at `00:52:49Z`, but an acknowledge nearly two hours later re-wrote the leaf's
timestamp to `02:43:31Z` with no APB sentence involved at all. A
timestamp-based check would have read that leaf as fresh, kept surfacing the
alarm, and re-raised and re-dispatched it every time anyone touched it - the
opposite of dead. The 30-second REST reconcile (ADR 0086) does the same
thing on its own schedule: every sync re-copies the server's leaf, timestamp
included, whether or not APB has said anything new. The same flaw runs the
other way too: a leaf's own `timestamp` only updates when *that path*
changes, so a live route's `arrivalCircleEntered` sitting untouched between
sentences (SignalK does not necessarily rewrite an unchanging leaf on every
repeat) could look stale after five minutes while APB was still streaming
navigation data the whole time.

## Decision

**Carry `sentence` through the delta path.** `applyDelta` copies
`source.sentence` onto the leaf beside `timestamp` and `$source`, so a leaf
has the same shape whether the delta stream or the REST reconcile wrote it.
This part is unchanged from the first version: the leaf's `sentence` still
says *which* sentence type a notification came from. What changed is where
the clock for that sentence comes from.

**`signalKSnapshot` tracks, per sentence type, when it last saw that sentence
genuinely repeating** (`sentenceSeen`, read through `lastSentenceSeen`).
`applyDelta` sets it to the receive time, because staleness is measured
against Helmcentral's clock and a SignalK host running a little behind must
not age every live sentence by its skew. An update whose own timestamp is
already older than the five-minute window is skipped: Helmcentral's stream
connects with signalk-server's default cached-value replay, so every
reconnect delivers the last APB course values from whenever the route
stopped, and counting those would hold a dead route's alarm up for another
five minutes after each reconnect. A SignalK clock more than five minutes
behind Helmcentral's would make every live APB look like a replay; that is a
clock fault the operator has to fix, not something this check can see past.
It is set whenever an update names a sentence in its
`source` object *and* carries at least one value outside `notifications.`.
That second condition is what excludes an acknowledge/silence re-emit and a
REST reconcile write: both touch only the notification's own leaf, never a
real navigation path, so neither can pose as "APB is still repeating."

**`signalKSnapshot` also tracks when it first applied a delta at all**
(`listenSince`, read through `listeningSince`), set once, on the very first
`applyDelta` call. Construction happens once at process start, before the
stream has necessarily connected - not a good proxy for "how long has this
process actually been listening to the bus."

**A live notification whose leaf names APB or RMB as its `sentence` is stale
when:** that sentence has gone unseen (`lastSentenceSeen`) for
`nmeaNotificationStaleAfter` (five minutes); or, if it has never once been
seen since this process started, once `listeningSince()` itself is more than
five minutes ago. Five minutes is comfortably longer than any real gap
between APB repeats - it reasserts roughly once a second while a route is
active - while short enough that an abandoned route stops paging within a
few minutes. The "never seen, but not for five minutes yet" half is a grace
period: it surfaces a notification the REST reconcile just brought back after
a restart (old sentence, old leaf, but this fresh process has recorded no
sighting of that sentence at all) for long enough to give a real APB sentence
a chance to arrive, and it is also what clears that same stuck notification
once five full minutes pass with nothing repeating - the direct observation
behind this rule: the boat's own feed had been silent far longer than five
minutes, so a restarted Helmcentral clears it within the grace period rather
than waiting out whatever the leaf's stale timestamp happened to claim.

The check runs inside `signalKNotifications` itself, the one function every
live-alarm consumer already calls through (`activeAlarms` for the REST/SSE
list and `worst`, `busNotificationWatcher.check` for raise/clear/dispatch),
so excluding it there is enough for the exclusion to reach all of them
without a second filter anywhere else.

**A notification without `sentence` is never touched by this.** Victron, N2K,
other plugins and Helmcentral's own alarms stay exactly as live as their own
source says, however long they have sat unchanged - there is no repeating
message to time out, and nothing here second-guesses a producer's own
liveness claim for those.

**Only APB and RMB are timed out** (`nmeaRepeatingRouteSentences`). They are
the route sentences a plotter re-sends every second while it steers, so their
silence means the route stopped. A notification from any other sentence, such
as a DSC distress call, is raised once and never repeats; its silence says
nothing, and timing it out would hide an alarm that still stands.

**A stale notification leaving the live list behaves exactly like a clear.**
`busNotificationWatcher` already treats "no longer in the live set" as a
clear for a notification it had raised, so a stale exclusion closes the alarm
log's open occurrence and dispatches a "cleared" event through the same path
any other clear takes. If the plotter's route resumes and a fresh APB
sentence carrying real navigation data arrives, `lastSentenceSeen` advances,
the exclusion lifts, and the notification is treated as newly sighted - it
serves a fresh dwell before raising again, the same as any other notification
reappearing.

**The watcher logs the transition once each way**, not every tick for as long
as the condition holds: one line when a notification is first set aside as
stale (naming the path, the sentence, and either the last time that sentence
was genuinely seen or that it never has been since Helmcentral started
listening), and one when it is no longer excluded. This lives in
`busNotificationWatcher` (`logStaleNMEATransitions`), which already runs once
a second with persistent per-notification state, rather than in
`signalKNotifications` itself, which every alarm list reads several times a
second and has no natural place to remember "already logged this."

## Rejected

**Judging staleness by the notification leaf's own `timestamp`.** This is
what the first version of this fix did, and Context above documents why it
was wrong in both directions: an acknowledge, a silence, or the ordinary
30-second REST reconcile all rewrite a leaf's `timestamp` without a single
new APB sentence being involved, which would have made a genuinely dead alarm
look fresh again on every such touch and kept re-raising and re-dispatching
it - the opposite of what this ADR sets out to do. Measuring the sentence
type's own last-seen time instead of the leaf's is what makes the check
immune to that: nothing about acknowledging or reconciling a notification
touches real navigation data, so nothing about doing so can look like APB
repeating.

**Writing `state: "normal"` back to SignalK once a notification goes stale.**
The server's own `status.canClear` is `false` for both of these - the
Notifications API offers no clear action for a notification the NMEA 0183
parser owns - and writing the path directly is the exact mistake ADR 0038
already reversed once: signalk-server's parser would simply reassert its own
`alarm` state on the next APB sentence (if one ever arrived again), or leave
Helmcentral's forced "normal" permanently disagreeing with a server that
still thinks it holds a live alert. Fighting the parser's own state machine
from outside it buys nothing a local exclusion doesn't already give, at the
cost of a write that can silently stop matching reality.

**Hiding by path name** (`arrivalCircleEntered`, `perpendicularPassed`)
**instead of by the `sentence` field.** A hardcoded path list only ever covers
the two paths known to misbehave today; any other NMEA 0183 sentence-driven
notification signalk-server's parser raises in the future would need its own
entry added by hand. Keying off `sentence` covers every notification with
this shape, known or not, and covers none that don't.

**Leaving it as a known limitation.** The REST reconcile (ADR 0086) already
demonstrates the boat can hit "an externally-raised alarm stays live forever"
in practice, not just in theory, and this specific case - a stale route alarm
outliving the route by more than a day - is a real symptom an operator has
to notice and either ignore or manually silence, indefinitely, once it happens.

## Consequences

- An `arrivalCircleEntered` or `perpendicularPassed` alarm the plotter stops
  feeding clears itself within a few minutes, not indefinitely - and stays
  cleared even if someone acknowledges or silences it on another client
  afterwards, since doing so no longer resets the clock.
- A `sentence`-bearing notification whose sentence is still genuinely
  repeating is completely unaffected, including while its own leaf happens
  to sit unchanged between updates.
- A notification the REST reconcile brings back after a restart (old
  sentence, old leaf) gets a five-minute grace period before its absence from
  `sentenceSeen` counts as evidence of anything, so a Helmcentral restart
  does not instantly hide every NMEA 0183-sourced alarm that was genuinely
  still live a moment before.
- One log line per stale transition, each way, from
  `busNotificationWatcher`; a persistently stale notification does not
  flood the log.
- `backend/signalk_snapshot.go` gains two small pieces of state
  (`sentenceSeen`, `listenSince`) alongside `pathSeen`/`sourceSeen`, capped
  the same way `sourceSeen` is (`signalKSentenceSeenMaxDistinct`) against a
  hostile delta varying `source.sentence` without bound.
- `backend/signalk_snapshot_test.go` covers `lastSentenceSeen`/
  `listeningSince` directly: a non-notification value records a sentence as
  seen, a notification-only update does not, a mixed update still does, a replayed
  update older than the window is not recorded, a live one is recorded at
  receipt,
  and `listeningSince` is set once by the first delta and never moves after.
  `backend/alarm_notifications_test.go` and `backend/alarm_bus_watch_test.go`
  cover the alarm-surfacing behaviour end to end: a still-repeating sentence
  stays surfaced, one that has stopped for over five minutes does not, an
  acknowledge/silence re-emit does not refresh liveness, a never-seen
  sentence surfaces inside the listening grace period and hides once it
  passes, a `sentence`-less notification is never touched regardless of age,
  the bus watcher clears a raised notification once it goes stale, and the
  diagnostic log fires once each way rather than every tick.

## Related

- [ADR 0038](0038-alarms.md): the SignalK notification vocabulary this
  exclusion still surfaces everything else from unchanged, and the reasoning
  against writing to a notification's path directly.
- [ADR 0086](0086-bus-notifications-reconciled-against-the-rest-tree.md): the
  REST reconcile this complements rather than replaces - it catches the
  server forgetting a notification; this catches one the server has not
  forgotten at all.
