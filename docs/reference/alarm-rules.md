# Alarm rules reference

The built-in alarm rules Helmcentral ships, and the extra values it computes
for a rule or a gauge to bind to. See [Alarms](../features/alarms.md) for
what a rule is, how severities work, and how transports and dwell/deadband
behave.

## Derived values

A rule can be built on any of these the same way as on a direct instrument
reading. They appear alongside everything else when you're choosing what a
rule or a gauge should watch, under their own path, and report nothing
rather than zero when there is not enough history to answer:

| Path | Units | What it is |
| --- | --- | --- |
| `helmcentral.environment.pressureRate` | Pa/s | The barometer's rate of change over the last three hours. 100 Pa/hr is 1 mb/hr. |
| `helmcentral.environment.pressureChange3h` | Pa | The plain three-hour tendency, the figure marine forecasts quote. |
| `helmcentral.environment.squashZoneIndex` | none | 1 when the wind has climbed 10 knots in three hours while the barometer stayed within 1 mb and the direction held. Bind it with "above 0.5". |
| `helmcentral.environment.pressureChange12h` | Pa | The twelve-hour barometric tendency. |
| `helmcentral.environment.pressureChange24h` | Pa | The twenty-four-hour barometric tendency, the figure the weather-bomb rule below reads. |
| `helmcentral.environment.stormIndex` | none | 1 when the barometer has fallen 4 mb or more in three hours and sits under 1009 mb, 0 otherwise. Bind it with "above 0.5". |
| `helmcentral.environment.severeThunderstormIndex` | none | 1 when the barometer has fallen 4 mb or more in three hours, 8 mb or more in twelve hours, and sits under 1005 mb. Bind it with "above 0.5". |
| `helmcentral.propulsion.fuelEconomy` | m/m³ | The whole boat's distance per unit fuel, rather than one engine's. |
| `helmcentral.fuel.volume` | m3 | Fuel aboard, summed across every tank that reports both a level and a capacity. Empty when no tank reports both. |
| `helmcentral.fuel.timeToEmpty` | s | Fuel aboard divided by the current total burn. Empty while stopped, with the engines off, or with no fuel volume to divide. |
| `helmcentral.fuel.rangeAtCurrentBurn` | m | Fuel aboard times the boat's current distance per unit fuel. Empty under the same conditions as fuel economy or fuel volume, whichever is absent. |
| `helmcentral.environment.forecastWindWarningLevel` | none | The official wind warning in force for the vessel's zone, ranked: 0 none, 1 strong wind or small craft, 2 gale, 3 storm. Absent until the first fetch lands, and again after thirty minutes without one. |
| `helmcentral.environment.forecastSurfWarning` | none | 1 when a hazardous surf warning is in force for the zone, 0 otherwise. Absent under the same conditions as the wind level. |
| `helmcentral.anomaly.sensor.frozenCount` | none | How many engine-correlated readings are stuck while the engine is clearly working. 0 means the check ran and found nothing; needs at least one engine set up in Settings → Vessel. |
| `helmcentral.anomaly.sensor.outOfRangeCount` | none | How many engine or battery readings are outside anything physically possible. Needs no setup. |
| `helmcentral.anomaly.sensor.silentSourceCount` | none | How many previously steady sources on the instrument network have gone quiet. Needs no setup. |
| `helmcentral.anomaly.battery.fullBankCharging` | none | 0, 1 or 2: whether the house bank is being charged past where it needs to be, and how far. Needs a house bank picked in Settings → Vessel. |
| `helmcentral.anomaly.engines.<name>.<reading>Residual` | °C, kPa or none | One engine's gap from its peers for one reading (coolant temperature, oil pressure, boost pressure, engine load, or the transmission's own oil pressure/temperature), less the gap that engine normally runs. Absent until several weeks of history have taught Helmcentral what normal is for your boat. Needs at least two engines set up. |

These need the boat to be publishing an outside barometer reading and, for
the squash-zone index, true wind speed and direction. If these inputs are
missing, the value stays absent and does not satisfy a rule.

The three-hour barometer paths, the rate, the three-hour tendency and the
storm signature, need thirty minutes of history before they report anything.
The twelve-hour tendency needs eleven and a half hours, and the
twenty-four-hour tendency and the weather bomb rule need twenty-three and a
half hours. The severe-thunderstorm signature needs whatever the twelve-hour
tendency needs, since it reads both windows at once. All of them clear when
Helmcentral restarts, since the history they are built from is kept in
memory rather than on disk.

The fuel-economy and fuel paths go absent for a second reason as well: if any
reading they are built from (a fuel tank's level or capacity, an engine's fuel
rate, or speed over ground) has stopped updating for more than two minutes,
the derived path reports nothing rather than a number computed from a source
that is no longer telling the truth. A rule bound to one of these paths never
fires on stale arithmetic; it simply sees no value, the same as if the path
had never been published at all.

## Heavy-weather rule set

From *Surviving the Storm*. Both arrive switched off; see
[Alarms](../features/alarms.md#the-heavy-weather-rule-set) for why.

| Rule | Fires when | Severity |
| --- | --- | --- |
| Squash zone | The squash-zone signature above holds | warn |
| Tropical barometer anomaly | Three-hour tendency past -1.5 mb | alert |

## The Law of Storms rule set

From R. J. Ellis, "Secret Law of Storms", worldstormcentral.co (Rules for
storms and gales page). Six of the seven arrive switched on; see
[Alarms](../features/alarms.md#the-law-of-storms-barometer-rules) for which
one doesn't and why.

| Rule | Fires when | Severity |
| --- | --- | --- |
| Barometer up 6 mb in three hours | Three-hour tendency past +6 mb | warn |
| Barometer down 6 mb in three hours | Three-hour tendency past -6 mb | warn |
| Barometer up 10 mb in three hours | Three-hour tendency past +10 mb | alarm |
| Barometer down 10 mb in three hours | Three-hour tendency past -10 mb | alarm |
| Storm signature | Barometer down 4 mb in three hours with pressure under 1009 mb | alert |
| Severe thunderstorm signature | Barometer down 4 mb in three hours and 8 mb in twelve hours with pressure under 1005 mb | alarm |
| Weather bomb | Twenty-four-hour tendency past -24 mb | emergency |

## Anomaly detection rule set

See [Anomaly detection](../features/anomaly-detection.md) for what each of
these watches for and what setting it up needs. Impossible-reading and
gone-quiet arrive switched on with no setup at all; the rest arrive as each
one's own setup is completed, and the engine differential pairs arrive
switched off until a steady run has taught Helmcentral your boat's own
normal gap.

| Rule | Fires when | Severity |
| --- | --- | --- |
| Frozen sensor reading | Any frozen-count above 0 | alert |
| Impossible sensor reading | Any out-of-range count above 0 | alert |
| Silent sensor source | Any silent-source count above 0 | warn |
| Charging into a full house bank | Full-bank charging level 1 or higher | warn |
| House bank overcharge risk | Full-bank charging level 2 (switched off until tuned) | alarm |
| Engine running hotter/cooler, oil/boost pressure high/low, load high/low | That reading's residual past its learned band (switched off until a baseline is learned) | alert |

## Forecast warning rule set

Reads the ranked forecast-warning values above. All four arrive switched on;
see [Alarms](../features/alarms.md#the-forecast-warning-rules).

| Rule | Fires when | Severity |
| --- | --- | --- |
| Forecast wind warning | Any wind warning is in force | warn |
| Forecast gale or storm warning | The warning is a gale or worse | alarm |
| Forecast surf warning | A hazardous surf warning is in force | alert |
| Forecast warnings unavailable | No successful fetch for thirty minutes | alert |
