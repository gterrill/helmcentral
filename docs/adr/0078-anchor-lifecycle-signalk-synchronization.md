# ADR 0078: Publish anchor lifecycle and decouple departure detection

## Status

Accepted

## Context

Helmcentral's anchor watch was local-only. Auto-state 0.6.3 latches `anchored`
on an anchor-position event and ignores movement until an explicit null event
releases it. An absent path is not that event. Live observation found anchored
being republished while both engines ran at about 1,828 RPM and SOG was near
10 knots. Publishing an explicit null immediately produced `motoring`.

Automatic watch closure previously required `navigation.state == motoring`.
Connecting Drop to Auto-state without changing that condition would create a
cycle: the watch could not close until the state that its closure releases had
already changed. Stationary-only GNSS heuristics could also freeze the position
needed to detect departure while the state remained anchored.

## Decision

- POST publishes the stored, bow-corrected anchor coordinates as
  `navigation.anchor.position`; repositioning follows the same path. PATCH
  of planning/watch settings does not publish deployment events.
- DELETE always publishes explicit null, including when already inactive.
  This makes an explicit retry capable of repairing the upstream latch.
- Reuse the authenticated delta transport, not notification-specific clear
  confirmation. Require a successful REST read with the expected coordinates
  or explicit null; a 404 is not confirmation. No direct `navigation.state`
  writes: Auto-state remains the classifier.
- Serialize local lifecycle writes while keeping network I/O outside the
  telemetry-state mutex. Persist Drop locally first, retain its safety watch
  on publish failure, and return 502 rather than pretend synchronization worked.
  Confirm Raise before deleting the local record. Removal failure is explicit.
  This is not a distributed transaction; errors describe the partial result.
- Auto-close uses finite positive main-engine RPM and valid non-critical
  position outside radius + 4.572 m continuously for five seconds, not the
  navigation-state label. Preserve browser scope and the existing feature toggle.
  One failed attempt is surfaced; no retry loop while eligibility stays true.
- Running main-engine evidence suppresses only stationary-only GNSS heuristics.
  Quality, age and recovery checks remain. Do not rewrite the reported vessel
  state or bypass the GNSS gate to force auto-close.
- No startup replay, silent fallback, plugin changes, or dependence on the
  SignalK alarm-transport toggle. Drag notifications remain separate.

## Consequences

The navigation-state dependency is removed, not the underlying SignalK telemetry
or publication dependency. A missing source prevents automatic closure. An
upstream outage can leave a saved local watch out of sync, but the operation
reports failure and the watch remains protective. Operator-facing behavior is
documented in `docs/features/anchor-watch.md`.

Backend tests exercise authenticated WebSocket deltas, exact position/null
confirmation, persistence and upstream failures; frontend tests cover direct
RPM evidence, hysteresis, invalid fixes, single requests and visible failures.