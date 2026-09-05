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

The deadband is also why acknowledging does not clear an alarm. Acknowledge
says you have seen it; it stops the sound and the visual alert, and the alarm
stays on the board until the value has actually recovered. So that you are not
left guessing, every alarm card says what it is waiting for, in the units you
think in: "Falling 1.1 mb/hr. Clears once the fall eases to 0.7 mb/hr." An
alarm raised by something else on the bus, which has no rule behind it, says
so and clears when its source clears it.

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

Writing to `notifications.*` has a consequence worth knowing. Helmcentral reads
the same tree to pick up alarms from other producers, so it has to recognise its
own output coming back. It does that by path: anything under `helmcentral.` is
its own, and so is the path of any enabled rule. Those it never shows as a
second, foreign-looking alarm. A notification under its own namespace that no
rule here is holding, left behind by a restart or by another Helmcentral
install pointed at the same SignalK, is cleared from the bus automatically.
For that reason a development copy of Helmcentral should keep the SignalK
transport switched off if it talks to the boat's server.

## The rules list

Rules are grouped by what they watch, using the first part of the SignalK path:
`electrical`, `environment`, `propulsion`, `tanks`, `radar` and so on, in
alphabetical order. There is nothing to categorise by hand; the path already
says which system a rule belongs to. A rule whose alarm is live is marked as
firing. Each rule shows its threshold and the point it clears at in your units,
and deleting one asks first.

## Gauge bands are alarm rules

A coloured band on a gauge, set into one of the alarm severities, is not
decoration. It is the rule. Dragging engine temperature into the red on the dial
raises a real alarm with real delivery, rather than colouring a picture and
leaving you to re-enter the same threshold on a separate screen in different
units.

Rules derived this way appear in the alarms list alongside hand-written ones,
marked as coming from a gauge, and are edited on the gauge rather than in the
list.

## Values Helmcentral works out for itself

A rule can name any path SignalK publishes, but some of the most useful things
to alarm on are not published by anything. They are rates, or relationships
between two readings, and an instrument reports neither.

Helmcentral computes these and publishes them under `helmcentral.`, where they
behave like any other path. They appear in the path picker with their units,
they bind to gauges and rules the same way, and they report nothing rather than
zero when there is not enough history to answer.

| Path | Units | What it is |
| --- | --- | --- |
| `helmcentral.environment.pressureRate` | Pa/s | The barometer's rate of change over the last three hours. 100 Pa/hr is 1 mb/hr. |
| `helmcentral.environment.pressureChange3h` | Pa | The plain three-hour tendency, the figure marine forecasts quote. |
| `helmcentral.environment.squashZoneIndex` | none | 1 when the wind has climbed 10 knots in three hours while the barometer stayed within 1 mb and the direction held. Bind it with "above 0.5". |
| `helmcentral.propulsion.fuelEconomy` | m/m³ | The whole boat's distance per unit fuel, rather than one engine's. |

These need the boat to be publishing `environment.outside.pressure` and, for
the squash-zone index, true wind speed and direction. Nothing breaks if it is
not: the value stays absent, and an absent value never satisfies a rule.

The barometer paths need half an hour of history before they report anything,
and clear when Helmcentral restarts. A slope drawn through two readings a
minute apart can imply any weather at all.

### Why a squash zone gets its own path

A squash zone is a high and a low close enough together to accelerate the wind
between them. It is worth a rule of its own because it is the one pattern the
barometer cannot warn you about: pressure and wind direction both hold steady
while the wind builds. The instrument everybody watches does nothing.

*Surviving the Storm* calls squash zones the cause of most heavy-weather
trouble yachts encounter, and singles out the western South Pacific around New
Zealand and Australia as getting more than its share. See
[Forecast](forecast.md) for where these thresholds come from.

## The heavy-weather rule set

Helmcentral ships five rules using thresholds from that book, created once on
first run:

| Rule | Fires when | Severity |
| --- | --- | --- |
| Barometer falling | Falling faster than 1 mb/hr | warn |
| Barometer plummeting | Falling faster than 2 mb/hr | alarm |
| Barometer down 3mb in three hours | Three-hour tendency past -3 mb | warn |
| Squash zone | The signature above holds | warn |
| Tropical barometer anomaly | Three-hour tendency past -1.5 mb | alert |

**They arrive switched off.** These are one crew's numbers, published in 1999,
and they have not been calibrated against your boat or your cruising ground.
Enabling them unasked would claim a confidence nobody has earned. Turn on the
ones that suit where you sail, and adjust them once you have watched your own
barometer for a season.

The tropical rule is the clearest example of why. In the tropics the barometer
swings about 1.5 mb either way every day on its own, so any departure from that
rhythm means something. Anywhere else the same rule will cry wolf.

They are ordinary rules once created. Edit them, retune them, delete them. A
rule you delete stays deleted; the set is not re-created on the next restart.

### One thing the rules cannot know

A rate of fall means different things depending on which way you are going. A
system moving at 15 knots closes 190 miles a day on a boat running with it and
528 on one heading into it, and the barometer moves accordingly. The same
1 mb/hr is a different situation in each case.

At anchor this does not apply. Underway it does, and no threshold can account
for it, so it is worth holding in your head rather than expecting the alarm to
have allowed for it.
