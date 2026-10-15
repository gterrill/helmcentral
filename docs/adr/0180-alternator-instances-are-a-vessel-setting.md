# ADR 0180: Alternator instances are a Vessel setting

## Status

Accepted (2026-10-15). Follows the precedent of battery names in
[ADR 0164](0164-sensor-health-cards-name-the-sensor.md): the instance ids a boat
publishes are the operator's to map, not ours to guess.

## Context

The Alternators tile read `electrical.alternator.0` as port and
`electrical.alternator.1` as starboard, hardcoded in the electrical state reader.
The Victron GX numbers alternator instances per installation, and renumbered them
on the boat: port is now instance 2 and starboard is 12, while instance 1 remains
in the SignalK tree with null values. The tile showed nothing.

## Decision

1. `vessel.alternators.port_instance` and `vessel.alternators.starboard_instance`
   in settings.yaml (strings; empty means not set) name the
   `electrical.alternator.<id>` instances read as port and starboard.
2. There is no default. An unset side reads as no data (-1 on the wire, `--` on
   the tile), and one set side gives the tile's existing single-alternator layout.
   An unset side never falls back to instance 0 or 1.
3. A save is rejected when an id contains anything but letters, digits, `_` and
   `-` (so no dots or path characters), or when both sides name the same instance.
4. The wire fields stay `alternator_0_*` (port) and `alternator_1_*` (starboard);
   the meaning is unchanged and renaming them would touch every consumer for no
   gain.
5. `GET /api/vessel/candidates` gains `alternators`: each live
   `electrical.alternator.<id>` with its current, which feeds the Port and
   Starboard selects in Settings -> Vessel.
6. The anomaly detector's charge-source evidence labels the configured instances
   "port alternator" and "starboard alternator"; any other instance keeps its
   "alternator <id>" label. Code that walks every `electrical.alternator.*` path
   is left alone.

## Consequences

An upgrade leaves the tile empty until the operator picks the two instances, which
the changelog lists as breaking. If the vessel block cannot be read, the electrical
state logs that error and both alternator sides read as not set; every other
electrical figure is unaffected. The settings page shows the read error itself.
