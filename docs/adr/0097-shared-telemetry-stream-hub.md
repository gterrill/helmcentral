# ADR 0097: Shared Telemetry Stream Hub

## Status
Accepted, extends ADR 0037.

## Context

ADR 0037 put nine SSE event types on `/api/stream`, each on its own interval,
and noted what it left unfinished: "each SSE connection holds a goroutine
rebuilding payloads on a timer, and every builder re-reads `settings.yaml`
from disk." A backend performance audit against the boat on 2026-09-15
measured what that cost:

- `telemetryStream` built its own emitters per connection. Every second, per
  client, `buildVesselStatePayload` ran four serial Influx `max()` queries
  (one a raw 24 h scan) on a new client and TCP connection. Solar-state ran
  four more every ten seconds. `settings.yaml` was parsed three to five times
  a second per client.
- The gate meant to skip unchanged payloads never skipped anything. Every
  payload carried a `datetime` stamped at build time, and every age was
  rounded to 0.1 s against the build clock, so no two frames matched. The
  stream ran at about 9.6 MB/h per client, uncompressed. gauge-values alone
  was 55 KB/min, most of it the per-path `ages` map.
- Events meant to fire every second fired about every two. The scheduler set
  `nextDue = now + interval`, which landed just after the next tick.
- `/api/stream` and `/api/logs/stream` were on the gzip skip list.

## Decision

### One hub, one build per interval

`telemetryHub` (`vessel_state_stream.go`) replaces the per-connection
emitters. It builds each event once per interval and fans the result out to
every subscriber.

1. **Fixed-grid scheduling.** `nextDue` advances from its own previous value,
   so tick jitter can't push the schedule forward. A slot missed outright
   (the process paused longer than an interval) skips to the next slot
   instead of firing a burst.
2. **Runs only while subscribed.** The first subscriber starts the hub and
   gets an immediate build pass. The last one leaving stops it, and
   `Unsubscribe` waits for in-flight builds to finish. With no browser open
   the hub does no work.
3. **A stuck subscriber is dropped, not waited on.** Each subscriber has a
   32-frame buffer. When it fills, the hub closes that subscriber so its
   EventSource reconnects. The drop happens inside a build goroutine, so it
   must not wait for the hub to stop: that would deadlock against the
   WaitGroup and leave the event's build lock held for good. Only
   `Unsubscribe`, on the handler's goroutine, waits.
4. **A fresh run rebuilds everything.** Starting the hub waits for the
   previous run to exit, then clears every `nextDue` and the frame cache, so a
   kiosk reload that reconnects within milliseconds gets every event on the
   first pass.
5. **Influx figures come from a background ticker.** The gust ladder and the
   four solar figures refresh every 30 s on one long-lived Influx client.
   Builders read the stored result. A result older than three ticks (90 s)
   reads as unavailable (`-1`), the same as a failed query, so a stuck ticker
   or a dead Influx can't pass old numbers off as current.
6. **Settings are cached** by file mtime and size and invalidated by
   `writeSettings`. Callers get a deep copy because some of them mutate the
   map.

### The change gate compares a normalised copy

The payload itself can't change. `frontend/src/lib/staleness.ts` takes ages
computed by the backend against the vessel clock because the wall kiosk's own
clock isn't trustworthy, so every frame still carries the real `datetime` and
real ages. What changes is what the gate compares.

1. A gated event has a `gateKey` function. The hub sends the real payload
   when the key differs from the last key sent. `heartbeat` has no gate and
   always sends, since its job is to prove liveness on a quiet boat.
   `autopilot` and `alarms` carry no clock or age fields and still compare
   the raw payload.
2. `telemetry_gate.go` builds the key from a decoded copy:
   - `datetime` is dropped wherever it appears.
   - Ages are banded by `bandAge`. Anything from 0 to 120 s is one "fresh"
     value. 120 s mirrors `STALE_AFTER_SECONDS`, and the UI shows no age
     below it. Above 120 s the band is the whole minute, and past a day the
     whole day, matching what `formatDataAge` displays. So a label change and
     a key change happen together, and crossing into stale always sends. The
     `-1` "no timestamp" sentinel passes through unchanged.
   - Banded fields: any key ending `_age_s` at any depth, every value in
     gauge-values' `ages` map, and radar per-target `age_seconds`. Nearby
     vessels' `age_seconds` stays exact because every row shows it ("12s
     ago"), and that event only builds every five seconds.
   - solar-state's `trend_24h_total` is left out of the key. Its last bucket's
     time follows the query's stop bound, so it moves on every Influx refresh,
     and nothing in the frontend reads the field.
   - Position, depth, wind and other readings are not touched. A real change
     in any of them sends.
3. The frame cache that seeds a new subscriber updates on every build,
   whether or not it was sent. The last-sent keys live in a separate map. A
   new client therefore starts from this tick's real ages, not the ones from
   the last send.

### gzip on both streams

`/api/stream` and `/api/logs/stream` came off the gzip skip list. The worry
was Echo's 1 KB `MinLength` holding small frames back. The vendored
`echo/middleware/compress.go` (v4.11.4) settles it: `gzipResponseWriter.Flush()`
marks the minimum as met, writes what's buffered and flushes the connection.
Both handlers flush after every write, so frames go out immediately.
`compression_test.go` checks this through the real middleware: a gzip client
gets `Content-Encoding: gzip` and decodes an event while the handler is still
running. The assistant's SSE route stays skipped.

On the dev stack the compressed stream was about 9% of its raw size.

### Rejected

**Sending an `updated_at` epoch and ageing it in the browser.** It's the
obvious fix, since the timestamp only changes when data arrives. It would
also make every age depend on the kiosk's clock, which is exactly what
server-side ages were built to avoid.

**Rounding instrument readings.** Coarser position or wind values would cut
more bytes, but a small real change in depth or wind could then never reach
the screen. The churn came from clocks and ages, not readings.

## Consequences

- Backend CPU and Influx query volume no longer grow with the number of
  connected clients.
- On a quiet boat, gated events go silent after their first frame until
  something real changes. That's intended. A manual "is the stream flowing"
  check against static data should watch `heartbeat`, not vessel-state.
  `TestTelemetryStreamHonoursPerEmitterIntervals` now uses a synthetic
  always-changing event for the same reason.
- Under way, vessel-state and gauge-values still send most seconds because
  the readings really change. There, gzip is what cuts the bytes.
- Each gated build costs one extra JSON decode and encode, paid once per
  interval by the hub rather than per client.
- The kiosk's WPE WebKit has not yet been checked against a gzipped
  EventSource on the boat. Check it after the next deploy.

## Related

- ADR 0037 (Signal K delta stream ingestion): introduced `/api/stream` and
  named the per-connection and settings costs this ADR removes.
- ADR 0068 (tiles mark a stale source): the `last_update_age_s` / `*_age_s`
  contract the gate bands without changing.
- ADR 0083 (ages ride the gauge-values stream): the `ages` map the gate
  bands by value because its keys are Signal K paths.
