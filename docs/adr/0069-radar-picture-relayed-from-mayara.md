# ADR 0069: The radar picture is relayed from mayara, while targets stay on the plugin

## Status
Accepted

Extends [ADR 0062](0062-marine-radar-targets-from-mayara.md), whose first decision was to take ARPA targets and leave the spokes alone. Targets are unchanged by this and still arrive through the mayara SignalK plugin exactly as 0062's amendment describes.

## Context

ADR 0062 decision 1 said it plainly: "We want collision awareness, not a second plotter, and the boat already has a plotter that renders this radar properly." That reasoning held for targets. What it did not anticipate is the operator wanting the echo picture on the anchor map itself, next to the AIS contacts and the anchor circle, on a screen that is already open.

Target ingestion remains unchanged. The picture requires a separate transport.

### The plugin does not carry spokes

Measured: `GET {signalk}/signalk/v2/api/vessels/self/radars/{id}/spokes` returns **404** on both v1 and v2 paths, while `/targets` and `/capabilities` proxy through fine. The plugin registers as a Radar API provider for REST; the spoke stream is mayara's alone.

### Why the browser does not connect to mayara directly

The first design connected directly to avoid sending a high-bandwidth stream through a backend whose telemetry transport is a 2-second JSON tick. Two findings ruled it out.

**Mixed content.** [ADR 0045](0045-web-push-secure-context-and-pwa-shell.md) makes `tailscale serve` at `https://<machine>.<tailnet>.ts.net` the supported way to get web push, because push needs a secure context and Helmcentral ships no TLS. On that origin a `ws://192.168.50.81:6502` connection is mixed content and the browser refuses it outright. VAPID keys are already configured on this boat. Browser-direct would have meant the overlay silently not existing on the origin the project treats as supported, which is the masking failure AGENTS.md forbids, dressed as a deployment detail.

**The legend cannot come direct either.** WebSockets are exempt from the same-origin policy; REST is not, and mayara has no CORS layer (its `Cargo.toml` pulls `tower-http` with only `set-header` and `trace`). The colour legend and spoke geometry come from `/capabilities`, which is REST. Baking the captured Furuno legend into the bundle would be wrong for every other radar and is exactly the sort of fallback this project refuses. So the backend had to proxy that regardless, which made relaying the socket a smaller step than it first looked.

Relaying also fixes routability. A phone on the tailnet from ashore reaches Helmcentral but not `192.168.50.81:6502`. Direct would have worked from the helm and not from the bunk, while every other feature works from both.

## Decision

### 1. Helmcentral relays the spoke stream, same-origin.

`GET /api/radar/spokes?radar=<id>`, upgraded to a WebSocket, holding one upstream connection to mayara per radar and fanning frames out to every browser client. Ref-counted: the upstream opens on the first client and closes on the last. `github.com/coder/websocket` was already a direct dependency, so this adds nothing to `go.mod`.

Frames are relayed **verbatim**, without protobuf parsing in Go or server-side decimation. Decision 2 records the bandwidth measurements behind this choice.

`GET /api/radar/capabilities?radar=<id>` proxies the legend through the plugin, cached briefly per radar, and returns an explicit error rather than an empty legend when the radar is gone. A radar whose capabilities we cannot read is one whose picture must not be drawn.

### 2. Backpressure drops frames per client rather than buffering or blocking.

Measured on the live DRS4D-NXT: 4.13 to 4.54 Mbit/s, about 2.4 frames per second, 232 KB per frame, every spoke a full 1024 bytes. The relay delivered 4.15 Mbit/s in a subsequent check, within that range.

That rate is proportional to antenna rotation, and this antenna was turning at about 8 rpm. At a normal 24 rpm the same stream would be near 13 Mbit/s. Rather than build server-side decimation on that uncertainty, which would need a Go protobuf dependency, each client gets a small buffered channel and a full buffer means the frame is dropped for that client alone, logged once.

Spokes are independent and the browser accumulates them into a persistent buffer, so a dropped frame costs one sector of one sweep and the next revolution repaints it. A tablet that cannot keep up degrades to a lower frame rate, which on a rotating picture is barely perceptible. Blocking would stall every other client; unbounded buffering is a memory leak on a boat computer.

### 3. The picture is centred on own ship, not on the radar's reported fix.

This corrects a Phase 0 conclusion.

The capture found `Spoke.lat`/`lon` populated on all 48,056 spokes. The initial conclusion was that centring on the radar's fix would resolve the antenna-offset caveat in ADR 0062 line 163.

Populated is not live. Sampled three times over 36 seconds while making way:

    radar -20.25242,148.94615   ship -20.31148,149.06041
    radar -20.25242,148.94615   ship -20.31207,149.06025
    radar -20.25242,148.94615   ship -20.31259,149.06005

Byte-identical every time while own ship moved. mayara captures that fix once and does not refresh it. Centring on it put the overlay **13.7 km** away and off the visible map, with the layer, the source, the palette and 26,221 drawn canvas pixels all perfectly correct. Everything was right except where it was.

Own ship is therefore the reference, and the divergence from the radar's fix is logged once per enable, following `logRadarProjectionMismatch`. This retains the metre-scale antenna offset but avoids the kilometre-scale error caused by the frozen fix.

This is the fifth time in this integration that data has been present, well formed, and stale. The others are catalogued at ADR 0062. The pattern is now the single most reliable predictor of where this breaks, and checking that a field is populated is not the same as checking that it moves.

### 4. Draw north-up from `Spoke.bearing`, and derive the spoke count from the wire.

`bearing` is populated on every spoke and is true-north referenced, so the canvas is drawn north-up and never rotated by heading. That retires the risk ADR 0062 line 165 recorded, where a bow-relative bearing would have put every echo wrong by the heading, rotating as the boat swung.

The capabilities advertise `spokesPerRevolution: 8192`. The wire uses **4096**: every observed angle is even, stepping by exactly 2, confirmed in the saved fixture and again live through the relay. The renderer derives its spoke count from the observed step. `useRadarCapabilities` reports what the radar said, uncorrected, so the discrepancy is visible in one place rather than papered over in two.

`Spoke.range` is the range to that spoke's last pixel and is not the radar's range control: live, `range=825` while the control read 463. The canvas sizes from the spoke.

### 5. A reverse look-up table, not forward rasterisation.

For each canvas pixel, look up which spoke row and range bin it reads. Forward ray-writing leaves radial gaps at the rim; wedge polygons are correct but cost tens of thousands of fills per revolution and get more expensive exactly when there is more to see. The look-up table has no gaps by construction, costs the same regardless of content, and its expensive part depends only on the canvas size, spoke count and bin count, so it is computed once and memoised.

The picture is repainted at 8 fps from a persistent buffer that is the accumulator, with per-spoke expiry at 6 seconds and an immediate clear on disconnect and on range change. None of it touches React state.

### 6. The Mayara address is configured, and only the picture needs it.

ADR 0062's amendment removed all mayara host configuration on the grounds that everything came through the plugin. The picture does not, so a **Mayara** settings section carries the address and port. Targets remain configuration-free.

`validateSettingsChange` deliberately does not probe it, pinned by a test: a radar that is switched off must never block an unrelated settings save.

## Consequences

The anchor map now combines the radar picture, AIS contacts, radar targets, the anchor circle and the trail in one view.

**No plugin, no picture, and no targets either.** The coupling ADR 0062 accepted now covers both products.

**The overlay depends on a mayara address that nothing validates.** Enter it wrongly and the toggle reports the stream as unreachable, which is honest but gives no hint that the address is the problem. Deliberate: probing on save would let a switched-off radar block unrelated settings.

**Bandwidth is measured at one rotation speed only.** Everything above rests on about 8 rpm. If the antenna runs at 24, the relay will start dropping frames for slower clients, which is the designed behaviour rather than a fault, but the picture will visibly update less often. Re-measure before concluding anything is broken.

**The dev proxy needed `ws: true` and failed silently without it.** Vite does not forward upgrade requests unless an entry opts in. Before the fix the browser got 0 frames through `:5173` against 19 in the same 8 seconds straight to the backend, with no error anywhere. Production never hits that path, because there the Go server serves the frontend itself, which is what made it easy to miss.

**Alignment against the plotter is unverified.** The echoes trace the coast and ring the boat plausibly, but plausible is not verified, and the antenna offset is now a real quantity rather than a cancelled one. Compare against the MFD at close range before trusting the overlay for anything navigational.

**This is the first canvas, custom layer and image source in the codebase.** There is no in-repo precedent for its rendering cost, and it has not been profiled on the helm tablet. If performance requires adjustment, reduce the frame cap to 4 fps before reducing resolution to preserve picture detail.
