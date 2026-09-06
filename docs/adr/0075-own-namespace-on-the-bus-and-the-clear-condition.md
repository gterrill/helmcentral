# ADR 0075: Helmcentral Owns Its Namespace on the Bus, and the Card States the Clear Condition

## Status
Accepted

Amends ADR 0038 (alarms): the bus reader no longer treats every live notification as foreign, and the alarm status carries the rule's clear condition. Relates to ADR 0070 (heavy-weather rules), whose seeded set was the vehicle for the incident below.

## Context

On 2026-09-05 the board on the boat showed three acknowledged alarms. Two of them, "Barometer falling" and "Tropical barometer anomaly", had no engine behind them. The box's alarms API listed only bus-notification ids for them, its alarm log had no entry for either path, and the five heavy-weather rules on the box were disabled, as ADR 0070 ships them.

They had come from the laptop. The development backend's local rules file had the heavy-weather set enabled, and its SignalK notify transport was on. Run against the boat's SignalK, its engine fired and published `notifications.helmcentral.environment.pressureRate` onto the boat's bus. The box's bus reader (`signalKNotifications`) then read that back as a notification from some other producer: label set to the path, rule id `notifications:<path>`, no raised time, the rule's own label buried inside the message text.

That reader was written for ADR 0038's purpose, surfacing alarms from a Victron GX or an N2K device with no per-source integration. It had no notion of Helmcentral's own output coming back round. In the ordinary single-instance case the engine's status and its echo were both on the list; in the two-instance case only the echo was.

Two more things made the incident worse than a duplicate row.

**The operator could not tell what would clear the alarm.** Acknowledge stops the sound and the visual alert; the condition stays live until the value travels back past the deadband, which ADR 0038 decided and which is right. But the deadband was a stored field rendered nowhere except the rule editor. The message read `Barometer falling: below -0.027777777777777776 (-0.0283)`, a threshold that had already been crossed and a current value in pascals per second. Acknowledging looked broken because nothing on screen said what it was waiting for.

**Source and timestamp cannot identify who raised a notification.** SignalK's Notifications API rewrites `$source` to `notificationApi.XX` and resets the timestamp when a notification is acknowledged. Both barometer notifications on the bus carried the acknowledge time and the API's source. Whatever ownership test is used, it cannot rest on either field.

## Decision

### 1. Ownership is decided by path

A notification's bare path (the part after `notifications.`) is Helmcentral's if either holds:

- it is under the derived-value namespace `helmcentral.` (ADR 0070). Nothing else publishes there, so a live notification under it is Helmcentral's whether or not the rule that raised it is configured on this instance;
- it equals the path of an enabled rule on this engine, stored or derived from a gauge zone (ADR 0050).

A disabled rule owns nothing. Disabling a rule retracts nothing already on the bus, and another producer may legitimately raise on that path once this engine has stopped watching it.

### 2. Owned paths are reconciled, never displayed

The bus reader and the bus watcher (which logs foreign raises) both skip owned paths. The engine's own status is the only representation of a rule alarm.

A reconciler runs on every evaluator tick. Any owned notification live on the bus with no live engine status behind it is a ghost, and the reconciler publishes a clear for it through the existing SignalK transport. One attempt per path per five minutes, so a clear that never settles on the bus does not hammer it. Success and failure are both logged; a failed clear is never swallowed, per the fallback policy in AGENTS.md.

When the SignalK notify transport is disabled the reconciler logs once per path and writes nothing. That switch is what keeps a development instance off the boat's bus, and the reconciler must respect it or it would defeat the purpose.

### 3. Development instances keep the SignalK transport off

With no transports file the config is the zero value, so a fresh state directory starts with the transport disabled. It was enabled by hand on the laptop at some point. It is now off there, and the rule is: do not enable it on any instance pointed at the boat's SignalK other than the one on the boat.

### 4. The status carries the clear condition

Each rule alarm's status now reports `op`, `threshold`, `hysteresis`, `clear_value` and `unit`, all omitted when not applicable. `clear_value` is derived from the same function the engine clears on (`alarmClearValueFor`, agreeing with `alarmConditionCleared`), so the two cannot drift: threshold plus hysteresis for a `below` rule, threshold minus hysteresis for `above`, absent for `equal`, `notEqual` and `stale`. `unit` comes from the derived-path table for `helmcentral.*` paths and from SignalK `meta.units` otherwise.

Bus notifications get `unit` the same way, and their `raised_at` and `acked_at` from the alarm log's open occurrence, because the bus timestamp is the acknowledge time.

The message is one sentence in SI with the unit: `Barometer falling: -0.03 Pa/s, clears above -0.02 Pa/s`. It no longer repeats the threshold. A threshold that has already been crossed tells the reader nothing; the clear point does.

### 5. The card leads with the label and says what clears it

The active-alarm card headlines the rule label, then one sentence in the operator's units built from the structured fields (`Falling 1.1 mb/hr. Clears once the fall eases to 0.7 mb/hr.`, or generically `Now 14.9 V. Clears below 14.4 V.`), then the raised and acknowledged times and the path once, in its own case. A bus notification with no rule shows its message and `Clears when the source clears it.` A missing time is omitted, not rendered as a dash.

Pills describe state (`Acknowledged · still live`, `Silenced · still live`). The Silence and Acknowledge buttons follow the server's capability flags exactly as before; a silenced alarm keeps its Acknowledge button, because silencing is not acknowledging (ADR 0038).

Alert, warn, alarm and emergency each get their own hue. The previous pairs, amber-400 against amber-500 and red-500 against red-600, were indistinguishable at arm's length.

The banner says `all acknowledged, still live` for a board where everything has been acknowledged. It used to say `silenced`, the word ADR 0038 reserves for a different state.

### 6. Rules are grouped by the domain already in the path

The rules list groups by the first path segment after any `helmcentral.` prefix (`electrical`, `environment`, `radar`), groups in alphabetical order, rules alphabetical by label within a group. A rule whose alarm is live is marked. Thresholds render in operator units with the clear point and dwell. Deleting a rule asks first, inline.

## Rejected

**Ownership by `$source`.** The publish transport labels its deltas `helmcentral`, and it would have been the obvious key. Acknowledging rewrites the field, so every acknowledged echo would have leaked through as foreign, which is precisely the state the incident was found in.

**A category field on rules, with a filter.** The request was to group rules as "weather" and "engine". The path already carries that: `environment`, `electrical`, `propulsion`, `tanks`, `radar`. A second taxonomy is one more thing to fill in on every rule and one more way for two rules about the same thing to end up in different bins. A filter control is deferred until the list is long enough to need one; it is not, at five rules plus gauge zones.

**Showing the threshold in the message.** `below -0.03 (-0.03)` after rounding, or `below -0.0277 (-0.0283)` before it, is a comparison the reader has to finish in their head, and the alarm exists because it already came out true.

**Hiding acknowledged alarms.** Rejected in ADR 0038 and not reopened. The card now explains why the alarm is still there instead.

## Consequences

- The two ghosts on the boat clear on the first evaluator tick after deployment, without hand intervention.
- A foreign producer that raises on a path an enabled Helmcentral rule watches is hidden from the board. Accepted: for that path the rule is the authority, and two alarms about one value is the state this ADR removes. The bus notification itself is untouched; only this engine's own echoes are cleared.
- A bus notification the alarm log never recorded, one that was already live when the box started, shows no raised time until it next raises.
- Off-boat messages (ntfy, email, webhook, web push), the notification's `message` on the bus and the alarm log all carry the value and clear point in operator units, converted by a table in `backend/alarm_units.go` that mirrors the frontend's `alarm-display.ts` and `quantities.ts`. The two tables must move together; each says so in its header. Added after the initial release, once the SI messages had been seen in the wild.
- Gauge-zone colouring in the gauge tiles keeps its own amber-400 against amber-500 pair. That is a separate concern from alarm text and was not changed here.
