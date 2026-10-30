# ADR 0235: Charts on alarm notifications

## Status

Accepted (2026-10-31). Builds on [ADR 0228](0228-charts-in-watch-reports.md),
which drew charts for watch reports and rejected them for alarms "for later",
and on [ADR 0224](0224-alarm-rules-live-in-the-database.md) (alarm rules) and
[ADR 0214](0214-tank-levels-drive-the-tiles-and-the-alarms.md) (tank alarms).

## Context

An alarm that says "Engine Room Hot, 71 °C" leaves the watchkeeper asking
whether it jumped or crept. The curve answers that, and the phone is where the
question is asked. ADR 0228 left alarms out because an alarm card is read in
seconds and has its own presenter. The card in the app is still out of scope.
The notification is different: it is an email or a push on a phone, where an
image costs nothing to look at, and the renderer and the three transports that
carry a picture already exist.

The hard part is that an alarm is not a report. A report can wait 30 seconds for
history. An alarm has to leave the boat now, and a slow or missing InfluxDB must
never be the reason it does not.

## Decision

**A raised alarm carries one chart of the reading that raised it, when
Helmcentral knows that reading.** It is the same chart as in a watch report
(`watchChart`), made by the same code (`resolveWatchChart`) from a spec the alarm
builds:

- **Rule alarms** whose rule has a path and tests it with above, below, equal or
  not equal: the path over the last 6 hours, converted to the operator's unit
  with the same table the rule's threshold is shown in, with the threshold as
  the dashed line labelled "Warning limit" or "Alarm limit" after the rule's state. A tank's lines
are "Warning level" and "Alarm level", the same words a watch report's tank
chart uses.
  A rule that follows a role (the house bank's state of charge) takes its unit
  from the role, so the series spec gained a `Role`.
- **Tank alarms** (rules whose id starts `tank:`): the level as a percent, read
  with the tank level helper so a sender that publishes 0 to 100 and one that
  publishes a ratio both draw correctly. The warn level is the first dashed line
  and the alarm level a second one, so the chart gained an optional
  `alarm_threshold`.
- **Gauge zone alarms** are ordinary threshold rules on a path, so they chart the
  same way.

**Built-in checks.** None of the four gets a chart, and the list is closed, not
pending:

| Check | Chart | Why |
| --- | --- | --- |
| Anchor drag | No | The distance from the drop point is worked out from two positions on each tick. No path records it, so there is no history to read. |
| Anchor watch state (the watch could not load) | No | A fault in a stored setting, with no reading. |
| Stream watchdog | No | The alarm is that readings stopped. There is no curve to draw. |
| Collision profile | No | A setting out of line, with no reading. |

**No chart for:** alarms raised elsewhere on the network (SignalK notifications,
collision, radar, plugin alarms), because the path of those is a notification and
not a reading; rules that watch for silence; any clear; an alarm easing back to
a lesser state; and the reminder that a live alarm is still unacknowledged.
Those repeat something the operator has seen. A raise carries a chart, and so
does a live alarm that gets worse (a tank going from warn to alarm), because
that is a more urgent message than the first and the operator wants the curve
again. "Worse" is the new state outranking the rule's own state.

**The chart read never delays the alarm.** Queueing an alarm fixes its deadline
at 3 seconds on the real clock, and the queue carries it, so time spent waiting
behind another delivery counts. The read is `resolveWatchChart` under a context
with that deadline. The dispatcher runs it in its own goroutine and waits on the
result and on the deadline, so a reader that ignores its context still cannot
hold the alarm. (An alarm chart never reads the barometer buffer, which is the
reader that ignores its context, but the wait costs nothing.) If the deadline
passes, or the read fails, or there is no history (InfluxDB is optional, a window
can hold one reading, a path can have no display unit), the alarm goes out at
once without the chart.

**Channels that cannot show a chart are sent first.** The dispatcher is one
worker, so reading history before any send would make web push and SignalK wait
up to 3 seconds for a picture they never show. They are sent first. The history
read starts at the same moment, not after them, so a slow web push does not eat
the chart's time. Then the dispatcher waits for the read, up to the deadline,
and sends ntfy, the webhook and email. Those three share one drawing of the
picture, made once after the read. A retry from the queue draws it again from the
stored data.

**Left out, and says why.** This is the exception ADR 0228 recorded and the
operator approved on 2026-10-30, carried over unchanged. An alarm that went out
without its chart carries the line "Chart left out: <reason>" in the email
(plain and HTML parts) and in the ntfy message, and a `problem` field in place of
`png_base64` on the webhook's `charts` entry. The reason is logged. Nothing is
interpolated or filled from an older reading, and there is no flag, since the
message and the log both say it happened. The reason does not repeat the "No
history to chart" prefix watch findings use, because it reads after "Chart left
out".

**Stored as data.** The chart, or the left-out entry (a title and a problem, no
series), is stored on the queued message in a `charts` field, so a retry sends
the same chart and does not read history again, however late it goes. The PNG is
drawn when the alarm is sent (and again on a retry), as for reports. A left-out entry already has its
reason, so rendering passes it through without trying to draw it.

**Delivery** reuses the report paths:

- **Email** with a chart is `multipart/related`: the plain text and an HTML
  version in a `multipart/alternative`, then the PNG inline under the body. The
  watch report's assembler is now shared (`assembleChartEmail`). An alarm without
  a chart is the plain message it was, byte for byte, apart from the
  left-out line when there is one.
- **ntfy** gets the chart as an attachment through the same PUT path, with the
  same handling when the server refuses attachments: log it and send the text.
- **Webhook** gets `charts`, one entry, with `png_base64` or `problem`. An alarm
  with no chart has no `charts` key.
- **Web push** is unchanged, and so is the alarm card in the app.

## Rejected

- **Charting every raise from the path the alarm card shows.** Bus alarms'
  paths are notification paths, `notifications.*`, which hold no readings.
- **Reading history when the alarm is raised, in the evaluator.** The evaluator
  tick must not wait on InfluxDB. The read happens in the dispatcher, which
  already exists to take slow work off that tick.
- **Making the picture first and queueing the PNG.** Tens of kilobytes against a
  few, and a retry would send an image of the same moment either way.
- **A shorter window for a short condition.** One window, 6 hours, so every
  alarm chart reads the same.
- **A chart on the unacknowledged reminder.** It repeats the raise.

## Consequences

- A raise on a charted rule spends up to 3 seconds in the dispatcher worker when
  InfluxDB is slow. Alarms behind it in the queue wait for that, which is the
  price of one worker keeping raise and clear in order. A boat with a dead
  InfluxDB pays it once the connection times out, not on every alarm if the
  failure is quick.
- The queued message grows by a few kilobytes of points per charted alarm.
- A webhook receiver sees a new optional `charts` field on alarms.
- Anchor drag has no chart. If its distance is ever recorded as a path, it can
  join the list by giving the check a spec.

## Follow-ups left out on purpose

- **Neutral names.** The chart types and files still say `watch`. Renaming them
  to `chart` would touch the same files as two branches in flight, so it waits.
- **One slot for charts on the message.** A watch report keeps its charts on
  `Watch` and an alarm on `Charts`; `chartList` hides the difference. Merging
  them is a change to the stored report format and is not worth it yet.
- **A cache for a flapping rule.** A rule that raises and clears many times
  reads history each time it raises. One alarm in a few minutes does not need
  a cache, and a stale chart would be worse than a slow one.
- **The channel list as a map.** `alarmChartChannels` is a small map where a
  field on each transport would do. It is three names and reads clearly.
