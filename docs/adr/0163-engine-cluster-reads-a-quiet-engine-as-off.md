# ADR 0163: The Engine Cluster Reads a Quiet Engine as Off

## Status

Accepted

## Context

Turning an engine's key off powers down its engine computer, so its readings
stop; the last rpm is idle, never 0. The Engine Cluster tile read that as a
dead feed: an amber `STALE 2H 28M` marker on the title and on every reading,
and an amber border, for an engine that was simply switched off.

ADR 0147 already decides, for the silent sensor source check, whether an
engine-bound `$source` going quiet reads as a key-off (`engineKeyOff`): its
connection is alive because some non-engine source on it published within the
quiet window, or the connection carries no other source at all. Otherwise the
whole connection is dead and it is a data loss.

## Decision

1. **The tile uses ADR 0147's reading.** `engineStates`
   (`backend/engine_state.go`) classifies each propulsion id as `running` (a
   source for it is reporting, using the same per-source quiet threshold as
   `quietSources`, now `sourceQuietThreshold`), `off` (all its sources are
   quiet and `engineKeyOff` holds for the newest) or `lost` (quiet and its
   connection is quiet too). It calls `engineKeyOff` rather than restating it.
2. **Which source belongs to which engine** comes from deltas.
   `sourceSeenEntry` gains `EngineIDs`, the `<id>` of every
   `propulsion.<id>.*` path the source has published (capped at 8).
3. **Channel.** The existing `gauge-values` event gains an `engines` map,
   `{ "<id>": { "state", "last_update" } }`. `last_update` is sent only for
   `off` and `lost`; a running engine's would change every tick and defeat the
   event's change gate. An engine with no known source has no entry, and
   while the SignalK stream itself is down (the silent-source check's
   10 s rule) no engine is classified, so the tile keeps today's behaviour.
   Unlike `quietSources` there is no "watched" gate (30 updates over 5
   minutes): after a restart a dead engine's retained values arrive as one
   update and the engine is plainly off.
4. **Tile.** For `off`, the cluster's own `propulsion.<id>.*` readings read
   as absent (`--`) with no age, the title pill shows a muted `OFF` badge
   (tooltip "Off since HH:MM", local time) and the border stays neutral. Other
   bound paths, such as the fuel rail's tank, are unaffected. `lost` and
   `running` are unchanged. The engine id is taken from the ring's path.

## Consequences

An engine computer dropping out while the engine runs, on an otherwise healthy
bus, shows `OFF`. ADR 0147 accepted the same limitation for the alarm. Other
tiles bound to engine paths (gauges, groups, lamps) still show `STALE`; only
the cluster reads the engine state.

## Rejected

- **A second copy of the rule in the frontend.** The frontend has no source
  health; the decision belongs next to `engineKeyOff`.
- **Treating last rpm as 0.** It is idle, not 0 (see ADR 0147).
- **Per-reading OFF badges.** A whole engine off is one fact; the title pill
  carries it and the readings show `--`.
