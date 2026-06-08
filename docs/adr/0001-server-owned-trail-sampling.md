# ADR 0001: Server-Owned Trail Sampling

## Status
Accepted

## Context
Trail history was initially built in the frontend by accumulating position fixes from polling hooks. That made trail quality depend on which clients were open, created duplicate logic across self and AIS trails, and blurred the ownership boundary between SignalK sampling and UI rendering.

## Decision
The server owns trail sampling.

- The backend polls SignalK on a fixed cadence.
- The backend records self-vessel and nearby AIS trail points into server-side ring buffers.
- Clients do not sample SignalK for trail history.
- Clients consume incremental updates from backend APIs using `since=<timestamp>`.

## Consequences
Positive:
- Trail history is consistent across clients.
- Sampling rate is independent of browser tabs.
- The frontend becomes a consumer of trail state rather than a recorder of trail state.

Negative:
- The backend is responsible for polling cadence and retention.
- Trail fidelity depends on server polling interval and buffer sizing.

## Notes
This decision applies to ongoing trail capture for self and AIS vessels. It does not automatically apply to special-purpose historical context such as pre-anchor motoring approach tracks.
