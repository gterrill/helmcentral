# ADR 0037: SignalK Delta Stream Ingestion

## Status
Accepted

Extends ADR 0001 (server-owned trail sampling) rather than superseding it: the backend still owns sampling and clients still never talk to SignalK directly. Only the transport between Helmcentral and SignalK changes.

## Context

Every telemetry handler made a **live, blocking SignalK HTTP GET per request**. There was no cached snapshot anywhere in the backend — `fetchSignalKVesselState`, `fetchSignalKElectricalState`, `fetchSignalKSolarState`, `fetchSignalKTanksState`, `fetchSignalKNearbyVessels`, `fetchSignalKVesselNameMap` and `fetchSignalKSelfName` each built a fresh `http.Client`, fetched a full subtree, `json.Unmarshal`ed it into `map[string]any`, and discarded it after a handful of fixed lookups.

With one browser tab open at defaults the browser issued roughly 8 requests per 10s, each triggering its own upstream fetch, on top of `startTrackPoller`'s own 5s cycle doing four more sequential SignalK calls. End-to-end latency was 5–15s.

The deeper limit was structural: ingestion was a **hardcoded path list**. `lookupNumber(payload, "environment", "depth", "belowTransducer", "value")` and its ~40 siblings enumerate by hand every value Helmcentral can ever see. Nothing could be alarmed on, charted, or bound to a widget unless a developer had already wired that specific path. Configurable alarms (ADR 0038) and bindable gauge widgets (ADR 0039) are both blocked on this, not on their own feature work.

SignalK offers a delta WebSocket at `/signalk/v1/stream`, previously unused. It delivers every path the server knows about, at source cadence, with no hardcoded list.

## Decision

1. **Consume the delta stream over WebSocket**, using `github.com/coder/websocket` — pure Go, no transitive dependencies, `context`-native.

   **Pinned to v1.8.13.** v1.8.14+ declares `go 1.23`, and `go get @latest` silently rewrites the module's `go` directive to match. ADR 0011 already recorded a deliberate decision to hold the toolchain at 1.22 (it is why `modernc.org/sqlite` is pinned to v1.34.4), and five files plus the production `Dockerfile`'s `golang:1.22-alpine` stage encode that. v1.8.13 declares `go 1.19` and needs no bump. Any future `go get -u` on this module will attempt the same upgrade.

2. **Reassemble deltas into the nested tree shape the REST API returns.** Deltas carry flat dotted paths; `environment.depth.belowTransducer = 1.7` is stored as `{"environment":{"depth":{"belowTransducer":{"value":1.7}}}}`, with `timestamp` and `$source` as siblings of `value` when the update provides them. An empty path merges its object's keys at top level without a `value` wrapper, which is how `name` arrives.

   This is not merely convenient. `parseGNSSPositionValidation` (ADR 0004) consumes the **whole payload map**, and `applyGNSSHeuristics` is stateful across calls — it expects one coherent snapshot per call. Field-level delta application would break it; a rebuilt tree does not. The same reassembly preserves all ~40 `lookup*` call sites and every `fetchSignalK*` signature, so the 13 `fetchSignalKVesselState` call sites across 10 files were not touched.

3. **`signalKSnapshot` holds the trees**, guarded by an embedded `sync.RWMutex` — the codebase's established background-writer/handler-reader idiom, the same shape as `telemetryRingBuffer` (ADR 0020) and `anchor.go`'s `vesselTrail`. `treeFor` returns a **deep copy**: handlers read while the stream writes, so returning the live map would race.

4. **The self context comes from the hello frame.** Deltas carry the vessel's real context (`vessels.urn:mrn:signalk:uuid:…`), never the literal `vessels.self`. The server's opening frame is the only place that context is named, so it is captured before the no-updates skip. Servers that report `self` without the `vessels.` prefix are normalized.

5. **Subscribe with `?subscribe=all`, not `self`.** Nearby-vessels reads other vessels' contexts, which a self-only subscription never delivers. `vesselsTree()` re-keys those contexts by bare vessel id to match `GET /signalk/v1/api/vessels`.

6. **The stream is the only ingestion path. There is no REST fallback and no toggle.**

   `AGENTS.md`'s Fallback Policy prohibits masking upstream problems, and the cleanest way to honour it is to have nothing to fall back *to*. `signalKSelfPayload` returns an error whenever the snapshot is empty; `fetchSignalKVesselState` routes that into `criticalVesselState`, freezing position at the last trusted fix so a stream outage cannot read as a jump and trip the anchor alarm.

7. **REST survives in exactly one place: probing.** Connection *setup* has to reach a server that is not the configured one and has no stream open — discovery names every host it finds on the LAN, and the settings screen verifies a new address before saving it (ADR 0028). Answering either from the snapshot would report the currently configured vessel, so discovery would label every result with the same boat name and a broken address would validate clean and take the dashboard offline on save. `backend/signalk_probe.go` holds `probeSignalKTree`/`probeSignalKReachable`/`probeSignalKVesselName` for that, and nothing else may use them.

   The tracks plugin endpoint (`/signalk/v1/api/tracks`, read by `fetchSignalKAISTrails`) also stays REST: it serves historical track geometry, which deltas do not carry at all.

8. **Per-path staleness is mandatory.** Under REST, an absent key meant no data. Under deltas a value persists indefinitely until superseded, so a dead sensor would otherwise stay frozen at its last good reading forever. `pathSeen` records receive time per `context|path`; `stale(context, path, maxAge, now)` takes `now` as a parameter so callers drive it deterministically.

9. **Reuse the existing auth path.** `acquireSignalKToken` supplies a `Bearer` header on the handshake; a 401 triggers `invalidateSignalKToken` and exactly one retry, mirroring the write-path idiom in `route_activation.go`. Reads were previously anonymous; the stream generally is not.

10. **A malformed frame is logged and skipped, not fatal to the connection.** Dropping the session over one corrupt frame would reset every path to unseen for the whole backoff window, blanking the dashboard. Reconnects use exponential backoff and log every attempt with its delay and cause, as the Fallback Policy requires of retry behaviour.

See also ADR 0042 (nearby-vessel staleness filtering), which works around a consequence of decision 8 that this ADR left open: the snapshot never evicts contexts, so a vessel that transmitted once and left stays frozen in `contexts` forever until something downstream filters it out by age.

## Consequences

- Latency drops from the 5s poll floor to source cadence, and upstream load collapses from ~8 fetches/10s/tab plus the poller to a single subscription.
- Ingestion is no longer limited to a hardcoded path list. Every path the server publishes is in the snapshot, which is the precondition ADR 0038 (alarms) and ADR 0039 (bindable widgets) both depend on.
- There is one ingestion path, not two. The REST telemetry reads and their toggle were removed once the stream had been verified against a live vessel, rather than left in place as a permanent fallback.
- Test fixtures moved from HTTP stubs to snapshot seeding. `seedSelfTree`/`seedVesselTrees` install a REST-shaped JSON body directly as snapshot state, so existing fixtures stayed valid — the stream reassembles deltas into exactly that shape, which is decision 2 paying for itself.
- `go.mod` gains a WebSocket dependency, the module's first. The v1.8.13 pin is load-bearing and must survive dependency updates.
- The browser no longer polls for telemetry. `GET /api/stream` pushes five named event types (`vessel-state`, `electrical-state`, `nearby-vessels`, `solar-state`, `tanks-state`) over one shared, ref-counted `EventSource`, each on its own interval and change-gated. SSE rather than a WebSocket for the browser leg: the traffic is one-way, and it survives an authenticating reverse proxy.

  **Correction (2026-08-13):** this ADR originally claimed "`EventSource` reconnects and resumes on its own," and `frontend/src/hooks/use-telemetry-stream.ts` was written with no `error` handler on that assumption. That is only true of a transport-level drop. Per the WHATWG spec, a response carrying a non-200 status or a non-`text/event-stream` content-type is a *permanent* failure: `readyState` goes to `CLOSED` and the browser never retries. That is exactly what the Vite dev proxy, and nginx/the reverse proxy in production, answer with for the several seconds the Go backend takes to restart. The result in production: the stream died silently and stayed dead - one incident measured 57 minutes with zero `/api/stream` connections while REST polling (settings, weather) kept working normally - and every telemetry tile, including the position feed the anchor drag alarm reads (`use-anchor-watch.ts:181`), froze at its last value with nothing on screen to say so.

  Two fixes, both in `use-telemetry-stream.ts`:
  1. **Reconnect by hand on `CLOSED`, ignore `CONNECTING`.** The module now installs an `onerror` handler; on `CLOSED` it discards the dead instance and redials with exponential backoff (1s floor, 30s cap, reset to the floor after 10s of healthy connection) - the same shape as `backend/signalk_stream.go`'s `defaultStreamMinBackoff` / `defaultStreamMaxBackoff` / `streamStableDuration`, so the two legs read consistently in logs and neither drifts out of sync with the other's tuning. Listener registration moved out of `addEventListener` calls captured by each `subscribeTelemetry` closure and into a module-level `Map<event, Set<listener>>` that survives reconnects; each new `EventSource` gets every registered event's listeners re-attached, rather than subscribers being silently orphaned on the now-dead instance a naive "just recreate the EventSource" fix would have left them on.
  2. **Surface the outage instead of masking it**, per the Fallback Policy. `subscribeTelemetryStatus` / `useTelemetryStatus()` broadcast `'connected' | 'reconnecting' | 'disconnected'`. `VesselStatusBar` (`frontend/src/components/vessel-status-bar.tsx`) now reads it alongside the existing SignalK-unreachable check (`source === 'signalk-unreachable'`, surfaced through `useVesselIdentity`'s `signalkConnected`) and drops the "Live" badge to "Reconnecting" or "No Signal" - the same red/badge visual language the SignalK-unreachable case already used, not a new one.
- Weather, tide and place-name still poll, correctly — they are external API data behind long TTL caches, not vessel telemetry.
- Each SSE connection holds a goroutine rebuilding payloads on a timer, and every builder re-reads `settings.yaml` from disk. Fine for a helm browser or two; the settings read wants caching before it serves more.
- The `fetchSignalK*` telemetry functions take no URL or path arguments: they read the snapshot, so passing a server address was meaningless. Callers that existed only to compute one lost their `loadSignalKSettings`/`buildSignalKURL` preamble with it. The write and control paths (`generator.go`, `czone.go`, `route_activation.go`) still take a URL, because they PUT to a real endpoint.

## Verification

Validated against a live SignalK server (a real vessel at anchor), not only stubs: depth, position, wind, heading and GNSS quality updating per read; `vesselsTree` feeding 10+ AIS targets through nearby-vessels; tanks, electrical, generator and solar all populated; `source: "signalk"`; no reconnect churn in the log.
