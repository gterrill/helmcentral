# ADR 0164: Sensor-health cards name the sensor, and solar chargers are not batteries

## Status

Accepted (2026-10-07). Extends the wording rules of
[ADR 0157](0157-alarm-cards-say-what-happened-not-where-it-came-from.md) to the
three sensor-health alarms of the anomaly detector (the silent-source check is
[ADR 0147](0147-silent-source-key-off.md)), and narrows the impossible-reading
check.

## Context

The Impossible sensor reading card on the boat read:

    Impossible sensor reading
    Now 1. Clears below 0.5.
    electrical.batteries.1.voltage
    [IGNORE ELECTRICAL.BATTERIES.1.VOLTAGE]

The operator could not tell what the reading was, why it was impossible, or
what it belonged to. It was also a false positive. SignalK files four Victron
BlueSolar MPPT chargers (`YachtDevices.36` to `39`) under
`electrical.batteries.1`, publishing PGN 127508 at about 76 V. That is the
solar array's input voltage, not a battery, and the 70 V sanity limit for a
12, 24 or 48 V bank correctly calls it impossible for a battery.

Reading the sources tree on the boat showed that the NMEA 2000 device class
cannot tell a charger from a battery. Every Victron device on the bus reports
`deviceClass` "Electrical Generation": the solar chargers, the Skylla charger,
the Quattro, both SmartShunts, the Batrium BMS and the system battery monitor.
Only `deviceFunction` differs: 160 for the chargers, 170 for the battery
monitors, 153 for the Quattro.

## Decision

1. **A battery instance published only by chargers is not range-checked.**
   `electrical.batteries.<id>` is skipped by the impossible-reading check when
   every source that has published it is an NMEA 2000 device of class
   "Electrical Generation" with device function 160 (charger). The device
   class alone is not used, because on this bus it matches every battery
   monitor too and would switch the check off for the whole bank.
   Instance 0, which the chargers share with the BMS and the Quattro, is still
   checked.
2. **Unknown means checked.** The set of publishers of each instance is
   remembered by the snapshot (the tree leaf only keeps the last writer), and
   the device details come from `GET /signalk/v1/api/sources`, refreshed every
   ten minutes. If the sources tree has not been read, or one publisher is
   missing from it, the instance stays in the check as a battery. Nothing is
   skipped on a guess.
3. **One naming function.** A sensor's name is resolved in a fixed order, each
   step real data: the operator's name from Vessel settings (battery instance
   names, or the engine's name with "engine" after it); the bus's own
   `electrical.batteries.<id>.name`; the `installationDescription1` of the
   source that publishes it; that source's product name; then "Battery <id>" or
   "Engine <id>". Where several devices share an instance, battery monitors are
   asked before other devices and chargers last. The quantity is one plain
   phrase per reading: voltage, current, state of charge, coolant temperature,
   oil pressure, boost pressure, engine load, rpm, gear oil pressure, gear oil
   temperature.
4. **Operator names live in Vessel settings.** `vessel.batteries` holds an
   instance and a name per entry. An empty name is not stored. Settings lists
   the instances the boat publishes, shows what the bus calls each as the hint,
   and marks charger instances.
5. **The server writes the card.** Each sensor-health alarm carries one entry
   per failing sensor: the raw identifier (kept for the Ignore action), the
   name, the quantity, and the line the card, banner, Mate and every
   notification show. `sensors` is the set at raise and `live_sensors` is read
   fresh on every list, the split `evidence` and `live_evidence` already make.
   The notification message becomes the rule's title followed by the lines. The
   count sentence ("Now 1. Clears below 0.5.") is not shown for these alarms,
   and no path or `$source` id is.
   - Impossible: `Port Engine Starter Battery voltage 75.7 V · above the 70 V
     any 12, 24 or 48 V bank reaches`. Each physical limit has a phrase for each
     side. Figures are in the operator's units.
   - Frozen: `Port engine oil pressure has not changed in 15 minutes while rpm
     varied`.
   - Silent source: `PORT START BATT has stopped sending`. A source that
     reports a single battery instance takes that battery's name.
   Lines that would read identically (four chargers with one product name) are
   numbered "(1 of 4)".
6. **Ignore stays on identifiers.** The button reads "Ignore <name> <quantity>"
   in sentence case and submits the raw path or `$source` id. The Ignored
   sensors list in Settings shows the same names.

## Consequences

- The false alarm on the boat is gone without touching the 70 V limit, which is
  still right for a battery.
- A charger-only instance that is a real battery on someone else's boat (a
  charger that is the only publisher of its own bank) would not be checked.
  Chargers do not measure a bank on their own, so this was judged acceptable.
- Pressures print in millibars, the unit the alarm table already uses for
  pascals, so an oil pressure limit reads as thousands of millibars. A kPa or
  psi alarm unit is a separate change.
- Frozen and silent-source names depend on the operator naming engines and
  batteries; until then the bus's own names and "Engine <id>" are used.
- The engine part of a sensor's name comes only from engines ticked in Vessel
  settings.
