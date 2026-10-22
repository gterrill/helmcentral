# ADR 0198: Devices That Go Quiet When Shore Power Is Disconnected Are Not Reported

## Status

Accepted

## Context

The silent source check (`silentSources`, `backend/anomaly_sensor_health.go`,
ADRs 0132 and 0147) raised "Instruments Silent" on 2026-10-21 when the operator
disconnected shore power. Both sources were the Victron Skylla-i shore
charger, which loses its power with shore AC:

- `YachtDevices.35`, its NMEA 2000 output through the Yacht Devices gateway
  (last update 22:10:39Z).
- `venus.com.victronenergy.charger.0`, the same charger through the Venus GX
  (last update 22:10:50Z, every value set to null first).

At the same moment `venus.com.victronenergy.system.0` switched
`electrical.venus.acSource` to `battery` and `electrical.venus.0.acin.acSource`
likewise, with `acSourceNumber` 240 (Venus: 0 not available, 1 grid, 2
generator, 3 shore, 240 battery/inverting). The vebus inverter's
`acin.voltage` went to 0.

Disconnecting shore power is an operator action, as turning the key off is
(ADR 0147). The charger cannot say it is being switched off: it is simply
without power.

## Decision

The same shape as the engine key-off rule, driven by the vessel's AC input
source instead of an engine.

1. **Track the AC input source.** `applyDelta` records every reading of an
   `electrical.*.acSource` or `electrical.*.acSourceNumber` path on the self
   vessel, per publishing source and path (`acSourceSeen`,
   `backend/anomaly_shore_power.go`). A reading is battery when the value is
   the string `battery` or the number 240. What is recorded is the time the
   reading *changed*, from the update's declared timestamp, not the time of each
   repeat: the GX republishes the same value every few seconds. No source id is
   named in code; any source publishing those paths counts.
2. **The vessel reads battery** when the most recent change among its AC source
   readings was to battery (a tie goes to non-battery). `acSourceFor` hands
   `silentSources` that time (`BatteryAt`) and the set of publishing sources.
3. **A quiet source is not reported** when the vessel reads battery and the
   source's last update falls within `silentSourceKeyOffWindow` (60 s, either
   side) of `BatteryAt`. The window is symmetric because the N2K view of the
   charger went quiet before the GX did. The comparison is the one the engine
   key-off rule uses (`withinKeyOffWindow`). No device classification is needed, so both views of the
   charger are covered.
4. **Fail fast.** With no AC source publisher, nothing changes. Once the AC
   source returns to grid, shore or generator, a device still quiet is
   reported. A device quiet while on shore, or more than 60 s from the switch,
   is reported. A source that publishes an AC source path is never excused, so
   the GX going quiet is still raised, and when such a source is itself quiet
   nothing on its connection is excused (a GX crash just after unplugging
   reports every `venus.*` service). While it is live, its other services, the
   GX view of the charger among them, are excused.
5. **Only recognised readings count.** Battery is `battery` or 240; grid,
   shore and generator (or 1, 2, 3) are off battery. Null, `NA`, 0 and
   anything unrecognised are ignored, as are readings older than the last
   change.
6. **A first sighting is not a transition.** After a backend restart or a
   SignalK stream reconnect, whose dump restamps retained values, the time of
   the switch is unknown, so nothing is excused until the next change. The
   tracking is cleared on reconnect and when the self vessel changes, and is
   capped at 32 readings (logged when full).

`quietSources` is unchanged, as in ADR 0147: the detectors keep distrusting a
reading whose source has gone quiet. Only the operator-facing list narrows.

## Rejected

- **A permanent Ignore on the charger.** It would hide a charger that fails on
  shore power, the case the alarm exists for, and the operator would have to do
  it per source, twice.
- **Classifying by `electrical.chargers.*.acin` paths.** It names the GX view
  only; the N2K view publishes `electrical.switches.bank.*` and would still
  be reported.
- **The inverter's `acin.voltage` reading 0.** Not every boat has an inverter
  or charger-inverter, and it also reads 0 for a tripped shore breaker or a
  fault on the inverter itself.
- **Hardcoding the GX system service id.** Vessel-specific, and the point of
  the decision is that any source reporting an AC input source will do.

## Consequences

- A boat whose instruments do not report the AC input source gets no benefit
  and still sees the card on disconnecting shore power; Ignore remains the
  answer there.
- A device that fails within a minute of the switch is missed until the next
  time the AC input changes.
- A restart or reconnect while on battery loses the switch time (decision 6), so a charger
  silent since before the restart is raised until shore returns.
- The same charger still appears under two source ids (bus and GX). Joining
  them looks feasible through the N2K product name against the GX service, but
  is not part of this change.
- The GX publishes no name for its charger service, so that source still reads
  "An instrument".

## Related

- [ADR 0147](0147-silent-source-ignores-key-off-and-plugins.md): the key-off
  rule this mirrors.
- [ADR 0132](0132-anomaly-detection-cycle-1.md): the silent source check.
