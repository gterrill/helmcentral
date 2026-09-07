# ADR 0079: Prominent connection loss and bounded stream silence

## Status

Accepted

## Context

A phone on limited-connectivity Wi-Fi displayed old anchor-watch state while
the boat backend and its HTTPS endpoint both held the correct active watch.
Toggling Wi-Fi restored the display. The existing small header badge hid its
text on mobile, and an EventSource left silently OPEN had no receive deadline.
An optimistic connected status and treating `open` as recovery compounded it.

## Decision

- Show a persistent, non-dismissible connection warning above panel content
  on every authenticated dashboard page. Keep all warning text visible on
  mobile and distinguish retained data from live evidence.
- Extend the existing singleton stream manager, not a second polling service.
  Start unconfirmed, mark explicit errors/offline as reconnecting, and clear
  only on a received telemetry event or named heartbeat.
- Publish a changing named heartbeat every 15 seconds independently of
  telemetry change gating. SSE comments are invisible to EventSource clients
  and cannot serve as the client watchdog signal.
- After 45 seconds without received events, replace even an OPEN or stuck
  CONNECTING source through the existing bounded-backoff retry mechanism.
  Check wall-clock elapsed time on visibility return, pageshow and online.
  Online is only a hint, never proof of server reachability.
- Remove old dispatch listeners before replacement; clean up browser listeners
  and watchdog timers when the final subscriber releases the shared stream.
- Keep authentication and upstream SignalK status separate. No automatic
  logout, alarm dismissal or live vessel-state mutation on connection loss.

## Consequences

The initial connection briefly carries the warning until the first event.
Silent outages take up to 45 seconds to detect while the browser is running;
suspended pages check promptly on return. Existing servers still emitting
telemetry regularly work, but the new named heartbeat permits quiet streams.
The backend auto-close migration remains a separate, deferred change.