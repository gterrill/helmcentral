# Alarms

A rule can name **any path the SignalK server publishes**. Ingestion is a
subscription to the delta stream rather than a hardcoded list of paths, so
alarming on something the boat started publishing yesterday needs no code
change and no release.

Helmcentral uses SignalK's own notification vocabulary, with the severities
`normal`, `alert`, `warn`, `alarm` and `emergency` kept verbatim. That is what
makes it work in both directions. Alarms raised elsewhere on the bus, by a
Victron GX, an N2K device or another plugin, show up here untranslated. Rule
hits of its own are written back to `notifications.*`, where a buzzer plugin or
an MFD can react without knowing Helmcentral exists.

## Three decisions worth spelling out

These are the ones people ask about, and they are the reason the alarm system
behaves differently from most.

**Dwell and hysteresis are mandatory.** A rule has to hold for its dwell before
it fires, and the value has to travel back past a deadband before it clears.
There is no way to configure a rule without them. Alarm storms are the most
common reason people switch marine alarms off, and a switched-off alarm is worse
than no alarm at all, because you still think something is watching.

**Absence is not a value.** A missing path does not satisfy a threshold, so a
freshly booted boat does not fire every rule at once while the bus comes up. The
same rule applies in reverse: a live alarm does not clear when its path goes
stale, so a dying sensor cannot silence its own alarm by going quiet.

**Failed deliveries are queued, not dropped.** Retried with backoff from 30
seconds out to 30 minutes, for up to 24 hours, then discarded. Discarded rather
than retried forever because a day-old alarm delivered as though it were current
is its own kind of wrong. The heartbeat is the exception and is never queued,
since a burst of stale "still alive" messages is worse than useless.

## Built in

Anchor drag and the stream watchdog are built in and travel the same path as
your own rules, with the same logging, acknowledgement and delivery.

The watchdog is worth understanding: if the SignalK connection dies, that is
itself an alarm. A dashboard that quietly freezes looks exactly like a calm
night, which is the failure it exists to catch. A periodic heartbeat sent off
the boat makes its absence an alarm too, for the case where the whole box has
gone.

## Getting told

Five transports, none of which needs a paid subscription:

| Transport | Notes |
| --- | --- |
| **ntfy** | Self-hostable, or the free public server. No account needed. |
| **SMTP** | Your own mail server or provider. |
| **Webhook** | Anything that accepts an HTTP POST. |
| **SignalK `notifications.*`** | Needs no internet at all. |
| **Web push** | Alarms on your phone's lock screen with no app to install. Needs Helmcentral served over https. |

`POST /api/alarm-transports/test` probes every enabled transport. Discovering at
3am that the ntfy topic was mistyped is the failure that justifies one button.

## Gauge bands are alarm rules

A coloured band on a gauge, set into one of the alarm severities, is not
decoration. It is the rule. Dragging engine temperature into the red on the dial
raises a real alarm with real delivery, rather than colouring a picture and
leaving you to re-enter the same threshold on a separate screen in different
units.

Rules derived this way appear in the alarms list alongside hand-written ones,
marked as coming from a gauge, and are edited on the gauge rather than in the
list.
