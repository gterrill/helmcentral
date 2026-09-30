# ADR 0147: The Silent Source Check Ignores Key-Off and Plugin Outputs

## Status

Accepted

## Context

The silent sensor source check (`silentSources`,
`backend/anomaly_sensor_health.go`, ADR 0132) reports every `$source` that
set up a regular reporting pattern and then went quiet. Two kinds of source
trip it in normal use.

**Devices that turn off with the engines.** Turning an engine's key off powers
down its engine computer, and anything else on that ignition circuit (alternator
regulators, DC-DC chargers), within seconds. The check read that as several
devices failing at once.

**SignalK server plugins.** Plugins publish their own status and computed
values (route maths, logger status, upload status). They report when something
happens, not on a schedule, and they are software rather than devices on the
network.

The doc comment on `silentSources` already said engine-bound sources were
excluded. The code never did it: `computeAnomalyReading` passed only the
operator's ignored set.

Engine rpm cannot separate a key-off from a fault, because the engine
computer is the rpm source and loses power before it reports 0 rpm; its last
reading is idle. Speed over ground cannot either, because a sailing vessel
runs with its engines off.

## Decision

The check classifies sources from what they publish and how SignalK describes
them. There is no configuration and no source name in the code.

1. **Plugin outputs are not watched.** SignalK gives hardware inputs a dotted
   `$source` and, for bus inputs, a `source.type` (`NMEA2000`, `NMEA0183`).
   A plugin publishes under its bare id with no `source` object. `applyDelta`
   records on `sourceSeenEntry` whether any update carried a `source.type`
   (`BusTyped`); a source with no dot in its id and no bus type is a plugin.
   A dotted source with no type, such as a Victron GX service, stays watched.
2. **Engine-bound sources are not reported when their silence reads as a
   key-off.** `applyDelta` sets `EngineBound` on the first update that carries
   a `propulsion.<id>.*` path. A source's connection is its `$source` up to
   the first dot (`n2k` for `n2k.engine.port`). An engine-bound source's
   silence counts as a key-off only when its connection is alive: some
   non-engine-bound source on the same connection published within 120 s, or
   the connection carries no non-engine-bound source at all (a dedicated
   engine connection). When the connection is not alive, a bus gateway has
   most likely failed, and the engine source is reported like any other.
3. **Devices that went quiet with an engine are not reported.** A watched,
   non-engine source that is quiet now is left out when its last update falls
   within `silentSourceKeyOffWindow` (60 s) of the last update of any
   engine-bound source whose silence reads as a key-off, whether or not that
   source has itself passed its own quiet threshold (a slow-cadence engine
   source would otherwise leave its companions reported for minutes after
   every key-off). While the engine source is live its last update is about
   now, so it never matches a device that is already quiet and a device that
   dies while motoring is raised. One that goes quiet more than a minute from
   key-off is raised too. A companion of an engine whose connection is down
   is not excused.

`silentSources` stays a pure function over `[]sourceHealth`, and keeps its
`excluded` parameter. It now sits on a new `quietSources`, which returns every
quiet source regardless of kind. The detector's `valid` closure keeps using
that wider set to stop trusting a path whose source has gone quiet, so an
engine computer that stops reporting still invalidates the engine readings
and the differentials built on them. Only the operator-facing count narrows.

## Rejected

- **A "motoring" condition on the alarm rule.** Rpm and speed cannot tell
  key-off from a fault (see Context), so the condition would either miss the
  shutdown or fire on every sail.
- **Treating last rpm as 0 at key-off.** The last reading is idle rpm, not 0.
- **Speed over ground.** A vessel under sail is moving with the engines off.
- **Skipping every engine-bound source unconditionally.** A gateway dying
  while the engines run takes the engine sources and every device behind it
  quiet together, and the unconditional skip plus the key-off window would
  hide all of it. The connection-alive test keeps that failure visible.
- **A list of plugin ids or device names.** It would be a vessel-specific
  configuration the operator must keep current.

## Consequences

- An engine computer that drops out while the engine is running, on a
  connection that still carries live non-engine sources, is not raised by
  this check, and neither is a dedicated engine connection going quiet. It shows as the engine tiles going to dashes, and the frozen
  and engine-differential checks stop trusting that engine's paths. A
  dedicated "engine data lost while running" alarm is a possible follow-up.
- A device that fails on its own within a minute of a key-off is missed until
  the engine next runs. The engine source is then live again and the device's
  absence is raised.
- A source that stays quiet for days is still counted until the operator
  ignores it. Ignoring remains the answer for equipment switched off by hand,
  such as a chartplotter.

## Related

- [ADR 0132](0132-anomaly-detection-cycle-1.md): the sensor-health checks this
  narrows.
