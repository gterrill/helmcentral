# Alarms

A rule can name any path the SignalK server publishes. Helmcentral subscribes
to the delta stream rather than a hardcoded list of paths, so adding a rule
for a newly published path needs no code change or release.

Helmcentral uses SignalK's own notification vocabulary, with the severities
`normal`, `alert`, `warn`, `alarm` and `emergency` kept verbatim. Alarms raised
elsewhere on the bus, by a Victron GX, an N2K device or another plugin, appear
with those severities unchanged. Helmcentral's rule hits are written back to
`notifications.*`, where a buzzer plugin or an MFD can react to them.

## Three decisions worth spelling out

**Dwell and hysteresis are mandatory.** A rule has to hold for its dwell before
it fires, and the value has to return past a deadband before it clears.
Both are required to limit repeated alarms when readings fluctuate around a
threshold.

The deadband is also why acknowledging does not clear an alarm. Acknowledge
says you have seen it; it stops the sound and the visual alert, and the alarm
stays on the board until the value has recovered. Every alarm card shows its
clearing condition in your selected units: "Falling 1.1 mb/hr. Clears once the
fall eases to 0.7 mb/hr." An
alarm raised by something else on the bus, which has no rule behind it, says
so and clears when its source clears it.

**Missing paths do not satisfy thresholds.** This prevents a freshly booted
boat from firing every rule at once while the bus comes up. The
same rule applies in reverse: a live alarm does not clear when its path goes
stale, so a sensor outage cannot clear an active alarm.

**Failed deliveries are queued for retry.** Retries use backoff from 30 seconds
to 30 minutes, for up to 24 hours. After that, deliveries are discarded to avoid
presenting old alarms as current. Heartbeats are never queued, since delayed
heartbeats could incorrectly indicate that the system is still running.

## Built in

Anchor drag and the stream watchdog are built in and use the same logging,
acknowledgement and delivery as your own rules.

The watchdog raises an alarm if the SignalK connection fails, since frozen
readings can otherwise look like stable conditions. A periodic heartbeat sent
off the boat also allows an external monitor to detect when the whole system
stops responding.

## Getting told

Five transports, none of which needs a paid subscription:

| Transport | Notes |
| --- | --- |
| **ntfy** | Self-hostable, or the free public server. No account needed. |
| **SMTP** | Your own mail server or provider. |
| **Webhook** | Anything that accepts an HTTP POST. |
| **SignalK `notifications.*`** | Needs no internet at all. |
| **Web push** | Alarms on your phone's lock screen with no app to install. Needs Helmcentral served over https. |

`POST /api/alarm-transports/test` probes every enabled transport so you can
check delivery before relying on it.

Helmcentral reads `notifications.*` to pick up alarms from other producers,
so it has to recognise its
own output coming back. It does that by path: anything under `helmcentral.` is
its own, and so is the path of any enabled rule. These are not shown as
duplicate external alarms. A notification under its own namespace that no
rule here is holding, left behind by a restart or by another Helmcentral
install pointed at the same SignalK, is cleared from the bus automatically.
For that reason a development copy of Helmcentral should keep the SignalK
transport switched off if it talks to the boat's server.

## The rules list

Rules are grouped by what they watch, using the first part of the SignalK path:
`electrical`, `environment`, `propulsion`, `tanks`, `radar` and so on, in
alphabetical order. No manual categorisation is needed. A rule whose alarm is live is marked as
firing. Each rule shows its threshold and the point it clears at in your units,
and deleting one asks first.

## Gauge bands are alarm rules

A coloured band on a gauge set to an alarm severity defines an alarm rule.
Dragging engine temperature into the red on the dial raises an alarm through
the configured transports. You do not need to re-enter the threshold on a
separate screen or convert it to different units.

Rules derived this way appear in the alarms list alongside hand-written ones,
marked as coming from a gauge, and are edited on the gauge rather than in the
list.

## Values Helmcentral works out for itself

A rule can name any path SignalK publishes. Rates and relationships between
readings may need to be computed from instrument data before rules can use them.

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
| `helmcentral.fuel.volume` | m3 | Fuel aboard, summed across every tank that reports both a level and a capacity. Empty when no tank reports both. |
| `helmcentral.fuel.timeToEmpty` | s | Fuel aboard divided by the current total burn. Empty while stopped, with the engines off, or with no fuel volume to divide. |
| `helmcentral.fuel.rangeAtCurrentBurn` | m | Fuel aboard times the boat's current distance per unit fuel. Empty under the same conditions as fuel economy or fuel volume, whichever is absent. |

These need the boat to be publishing `environment.outside.pressure` and, for
the squash-zone index, true wind speed and direction. If these inputs are
missing, the value stays absent and does not satisfy a rule.

The barometer paths need half an hour of history before they report anything,
and clear when Helmcentral restarts. This avoids deriving a weather trend from
too few readings.

The fuel-economy and fuel paths go absent for a second reason as well: if any
reading they are built from (a fuel tank's level or capacity, an engine's fuel
rate, or speed over ground) has stopped updating for more than two minutes,
the derived path reports nothing rather than a number computed from a source
that is no longer telling the truth. A rule bound to one of these paths never
fires on stale arithmetic; it simply sees no value, the same as if the path
had never been published at all.

### Why a squash zone gets its own path

A squash zone is a high and a low close enough together to accelerate the wind
between them. Pressure and wind direction can both hold steady while the wind
builds, so a barometer-only rule would not detect this pattern.

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

**They arrive switched off.** These are one crew's thresholds, published in 1999,
and they have not been calibrated against your boat or your cruising ground.
Turn on the ones that suit where you sail, and adjust them once you have watched your own
barometer for a season.

For example, in the tropics the barometer
swings about 1.5 mb either way every day on its own, so any departure from that
rhythm is significant. Outside the tropics the same rule can raise false alarms.

Once created, these rules can be edited, retuned or deleted like any other. A
rule you delete stays deleted; the set is not re-created on the next restart.

### One thing the rules cannot know

A rate of fall means different things depending on which way you are going. A
system moving at 15 knots closes 190 miles a day on a boat running with it and
528 on one heading into it, and the barometer moves accordingly. The same
1 mb/hr is a different situation in each case.

At anchor this does not apply. Underway, account for your course relative to
the weather system when interpreting an alarm; the thresholds do not adjust
for it.
