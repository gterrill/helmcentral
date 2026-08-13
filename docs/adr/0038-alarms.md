# ADR 0038: Configurable Alarms on SignalK's Notification Model

## Status
Accepted

Depends on ADR 0037: rules can name any SignalK path only because the delta stream put every path the server publishes into a snapshot. This was not a feature blocked on its own design, it was blocked on ingestion.

## Context

Helmcentral had no alarm subsystem. There was exactly one hardcoded alarm — the anchor drag klaxon in `use-anchor-alarm.ts` — implemented as Web Audio **in a browser tab**. Closing the tab silenced it. There were no user-configurable thresholds for anything: not battery voltage, tank levels, depth, bilge, engine temperature, or wind. No severities, no acknowledgement, no history, and no notification transports at all — no email, SMS, push, or webhook.

Against the products Helmcentral's users are most likely coming from, this is the gap. N2KView's premise is alarms on *any* N2K data with two severities, an alert log, three-stage audible escalation, and email/SMS delivery; Maretron's cloud services exist to carry that off the boat. A dashboard that renders beautifully and cannot tell you the bilge is cycling is a different category of product.

Two failures are invisible from inside the boat and matter more than any threshold:

1. **The data source dies.** Under a delta stream (ADR 0037) values persist until superseded, so a dead sensor holds its last reading and a frozen dashboard looks like a calm boat.
2. **The boat goes off.** Power, internet, or hardware fails and nothing can be sent. Silence is indistinguishable from "nothing is wrong".

## Decision

### 1. Adopt SignalK's notification vocabulary rather than inventing one

Severities are `normal | alert | warn | alarm | emergency` verbatim, and notifications are the objects the spec defines (`state`, `message`, `method`). Nothing in the codebase consumed `notifications.*` before this.

This is the load-bearing decision. It means Helmcentral is both a **consumer** and a **producer** on that tree:

- Alarms raised by anything else on the bus — Victron GX, N2K devices, other SignalK plugins — appear in Helmcentral's alarm list with **no per-source integration and no translation layer**. `signalKNotifications` walks a subtree the stream already carries; that is the entire implementation.
- Helmcentral's own rule hits are written back to `notifications.*`, so a buzzer plugin or an MFD reacts without knowing Helmcentral exists.

Inbound notification ids are namespaced (`notifications:<path>`) so they can never collide with a local rule id and silence the wrong thing.

Two spec rules are honoured directly: clearing writes `null` to the path, and an `emergency` cannot be acknowledged regardless of `canSilence`.

### 2. Rules and transport config live in their own files, never `settings.yaml`

`POST /api/settings` rebuilds its sections wholesale (only `ui` is merge-preserving), so a new top-level key there would be silently destroyed by the next settings save. Rules use `data/alarm-rules.json` and transports `data/alarm-transports.json`, each with its own CRUD, following the `dashboard-pages` precedent. ADR 0031 already rejected the settings route for per-instance config.

Secrets never enter those files. `SMTP_PASSWORD` and `NTFY_TOKEN` are registered in the encrypted store (ADR 0023) and read at send time, and deliberately excluded from `coreEnvSecretKeys` — no WASM plugin needs them, so they never reach `os.Setenv`.

### 3. Dwell and hysteresis are mandatory, not polish

Each rule carries a dwell (how long the condition must hold before firing) and a hysteresis deadband (how far the value must travel back before clearing). Clearing is deliberately **not** the negation of raising.

Without these, a value hovering at a threshold produces an alarm storm, which is the single most common reason people switch marine alarms off entirely — and an alarm system that has been switched off is worth less than none, because it is still trusted. A test drives 20 oscillations across the threshold and asserts exactly one raise.

Recovery before the dwell elapses restarts the timer, so an intermittent condition never accumulates enough time to fire.

### 4. Absence is never treated as a value

An absent path does not satisfy a threshold — treating "no data" as zero would fire every rule on a boat that has just booted. Symmetrically, a live alarm does **not** clear when its path goes absent or stale, because otherwise a dying sensor silences its own alarm.

Staleness is per-rule policy applied by the engine, not by the reader: the reader reports `LastSeen` as a fact and the engine decides what counts as stale. A zero threshold means "do not gate on staleness", not "stale after zero seconds".

### 5. Severities merge worst-wins

Lifted from `escalateValidation` in `gnss_validation.go` rather than reinvented, as is the latch-until-stable recovery idea and the edge-triggered logging that records transitions rather than steady state.

### 6. Delivery is self-hosted, and failures are queued rather than dropped

Four transports, none requiring a paid subscription: **ntfy** (self-hostable, or the free public server with no account — the default), **SMTP**, **webhook**, and **SignalK `notifications.*`** which needs no internet at all. Twilio/SMS and a Maretron-style cloud relay are excluded on exactly that ground.

A boat's internet comes and goes, so a failed delivery is queued in the alarm-log database and retried with backoff from 30s to 30m, every attempt logged — the fallback policy requires retry behaviour to be loud, since a silent retry is indistinguishable from one that never happened. Deliveries still failing after 24h are discarded, because eventually delivering a day-old alarm as if it were current is its own kind of wrong. Disabling a transport keeps its queued items so re-enabling still delivers them.

### 7. The watchdog and heartbeat cover what rules cannot

The watchdog raises when the stream disconnects or goes quiet past a threshold (default 120s — measured gaps on a real vessel run to ~10s, so anything under ~30s false-positives). It is edge-triggered and stays silent before the first message ever arrives, so a starting server does not alarm on every boot.

The heartbeat inverts the second failure: a periodic "still alive" makes its **absence** the alarm. Two deliberate asymmetries with ordinary delivery:

- **A failed heartbeat is never queued.** Its entire meaning is timeliness; a burst of stale "still alive" messages on reconnect, each claiming health that was true hours ago, is worse than useless.
- **It skips the SignalK transport**, which is on-boat and so proves nothing about whether the boat can be reached — the only question the heartbeat exists to answer.

### 8. The alarm log stores occurrences, not conditions

One row per raise, closed on clear. A recurring fault therefore shows how often it happens, which is usually the diagnosis; collapsing it to a single entry would hide exactly that. Acknowledge and clear stamp only the newest open occurrence.

## Consequences

- Any path the SignalK server publishes can be alarmed on, without code changes. This is the first feature to use ADR 0037's generic ingestion, and the precondition ADR 0039 (bindable widgets) also depends on.
- Alarms from other producers on the bus appear for free, which is a capability N2KView does not have in the other direction.
- Notification config is a new surface an operator can get wrong silently, so `POST /api/alarm-transports/test` probes every enabled transport. Discovering at 3am that the ntfy topic was mistyped is the failure that justifies one button.
- The alarm-log database is a new SQLite store, opened fail-fast at boot like the others. It carries both history and the delivery queue.
- Each SSE client already costs a goroutine (ADR 0037); the evaluator adds one more at 1s, plus the drainer, watchdog, and heartbeat tickers.
- Anchor drag detection moved to the server (`alarm_anchor.go`) rather than being derived in the React hook. Closing the tab no longer silences it, and it now flows through the normal path: logged, acknowledgeable, and delivered off the boat. Silencing is the server acknowledgement, so a second browser cannot be left ringing. It also fixed the old bug where the klaxon loop was created only on the transition into `dragging`, so un-silencing never restarted it.
- Drag uses asymmetric thresholds like every other rule: it fires past `radius + 4.572m` (the old client buffer, so the firing distance is unchanged) but clears only back inside the radius. A lost GNSS fix never raises a drag — position freezes at its last trusted value during an outage (ADR 0037), and the stream watchdog already reports the outage itself.
- `use-anchor-watch.ts` still derives its own client-side `dragging` state for map and tile styling. The alarm no longer depends on it, but the two can disagree inside the hysteresis band; collapsing them onto the server's answer is worth doing when that component is next touched.
- Rules are evaluated only while Helmcentral runs. There is no persistence of engine state across restarts, so a restart re-raises a still-true condition — which is the safe direction, but means the alarm log can show a duplicate occurrence after a deploy.

## Verification

Every layer was exercised against a live vessel, not only stubs:

- A shallow-water rule fired on real depth (5.56m against an 8m threshold), logged, acknowledged, and cleared on deletion.
- A webhook delivery failed with a 404, was queued and logged, and the drainer retried it 36s later with the next attempt scheduled 30s out.
- The stream watchdog raised (`No SignalK data for 10s`) and cleared on recovery.

Live testing found three bugs the unit tests had not: deleting the last rule left its alarm stuck active forever; `acked_at` serialized as year 1 and read as "acknowledged"; and resetting the watchdog threshold to zero was silently ignored by the running watchdog.
