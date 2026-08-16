# ADR 0041: Built-In Autopilot Widget

## Status
Accepted

Depends on ADR 0037 (delta-stream ingestion) for live state, and follows ADR 0012's "widget id + position" model as a fixed single-instance builtin, the same category as `generator` and `czone-switches` rather than the multi-instance `embed:`/`gauge:` pattern of ADR 0031/0039.

## Context

[halos-org/skip](https://github.com/halos-org/skip) ships an autopilot widget; Helmcentral had none, and an autopilot control at the helm is one of the more consequential things a boat dashboard can offer — consequential because a wrong command, or a wrong *belief* about the pilot's state, changes who is steering the boat.

Helmcentral already had every other piece this needed: a delta-stream snapshot (ADR 0037), authenticated SignalK writes (`generatorPut`, route activation), a widget registry, and a bento grid. This was mostly assembly, with the assembly itself needing to be careful.

## Decision

### 1. Target the SignalK v2 Autopilot API only — no legacy fallback

`/signalk/v2/api/vessels/self/autopilots/*` (SK server 2.x plus an autopilot provider plugin) is the only interface implemented. The legacy `steering.autopilot.*` PUT convention is deliberately not implemented and never silently substituted: v2 providers advertise `availableActions`, and that advertisement is precisely what lets the tile grey out what a given pilot cannot do instead of guessing from silence. A v1-only fallback would either have to guess at capability (unsafe) or expose every control unconditionally (worse). Where v2 is absent, `GET /api/autopilot` answers `{"present": false}` and the tile says so in plain text — it does not degrade to a v1 write path.

This project's fallback policy (`AGENTS.md`) forbids masking an upstream absence with a compatibility path; a missing v2 API is exactly that kind of absence, not a shape to paper over.

### 2. One honest limitation, stated rather than hidden: the exact v2 JSON shapes are unverified against a live provider

No SK 2.x server with an autopilot provider plugin was reachable from this environment. The endpoint paths and methods below come from the community-documented v2 Autopilot API and were implemented and tested against an `httptest` stub built to that documented shape — not against real hardware or `signalk-autopilot`/Raymarine/Garmin providers.

Rather than quietly hoping the shapes are right, the design deliberately minimizes how much of that guess is load-bearing:

- **`GET /api/autopilot` proxies SignalK's own JSON verbatim**, adding only `present` and `id`. It does not re-model a typed status shape this codebase can't verify — if a real provider's field names differ from the docs, the proxy still relays them faithfully rather than dropping or mis-mapping them.
- **The safety-critical live state (`engaged`, `state`, `mode`, `target`, `available_actions`, `stale`) comes from the existing delta-stream snapshot**, not from a guessed REST shape — see decision 4.
- Every endpoint **path, method, and body envelope** (the part that commands steering gear) is covered by `backend/autopilot_test.go` against a recording stub, so a wrong assumption there fails loudly in CI rather than silently at the helm.

This is recorded here, not glossed over, because it is the one place this ADR's confidence is lower than the rest of the codebase's verified-against-a-live-vessel norm (see ADR 0039's Verification section for contrast). **Before this widget is trusted against real steering gear, it needs the manual smoke test in this ADR's Verification section, against a real SK 2.x autopilot provider.**

### 3. Body envelope is per-endpoint, not uniform

`signalkRequestJSON` (`backend/route_activation.go`) sends a raw JSON body, unlike `putSignalKValue` (`backend/czone.go`) which always wraps in `{"value": ...}`. The v2 Autopilot API needs the envelope on `state`/`mode`/`target`/`target/adjust`/`dodge` PUTs but not on the bodyless `engage`/`disengage`/`tack`/`gybe` POSTs — so `autopilotControlHandler` takes the pre-wrapped body as a parameter per call site rather than applying one rule uniformly. Both shapes are covered in `autopilot_test.go`.

### 4. Live state rides the existing delta stream — no second ingestion path

`buildAutopilotPayload` reads `steering.autopilot.{engaged,state,mode,target,availableActions}` from the same snapshot every other tile reads (ADR 0037), and is registered as a 1-second `autopilot` emitter in `vessel_state_stream.go`, next to `vessel-state`. This is the same reasoning ADR 0039 recorded for gauges: the stream already carries this data, so pushing it costs nothing extra, and there is exactly one place vessel state can disagree with itself. The frontend's `use-autopilot.ts` subscribes to this event exactly as `use-gauge-values.ts` does — no polling, no second REST call for live values.

`GET /api/autopilot` is used for exactly one thing: a **capability probe** issued once on mount (and again only if a pilot appears later). The set of modes a pilot supports lives in SignalK's v2 status under `options.modes` and is *not* carried on the delta stream, so it cannot come from the SSE payload like everything else. It is static per pilot, so probing once is not polling. A failed probe leaves the mode list empty and records the reason; the tile then omits the mode selector and shows that reason, rather than offering a guessed `compass`/`wind`/`gps` set the pilot may not support.

### 5. Absence is not a value

Mirrors the alarm engine's rule (ADR 0038 §4) and the gauge tiles' treatment of an unbound path: if `steering.autopilot.engaged` has never appeared on the stream, the payload is `{"present": false}` — never a synthesized `engaged: false`. A disengaged-pilot claim is a factual statement about steering gear; when there is no autopilot at all, making that claim would be worse than saying nothing. `GET /api/autopilot` follows the same rule when `GET .../autopilots` (the v2 provider list) comes back empty.

### 6. No optimistic UI

The tile's displayed `engaged`/`state`/`mode`/`target` come *only* from the `autopilot` SSE payload. Calling `engage()`/`disengage()`/`tack()`/`gybe()`/`adjustHeading()` never mutates that displayed state directly — `use-autopilot.ts` tracks in-flight commands in a `pending` set (following `use-czone-switches.ts`'s precedent) but, unlike that hook, does not flip local state before the server confirms it. A command that fails — SK unreachable, provider rejects it, token expired twice — must not leave the tile claiming the boat is under autopilot when it is not. This is the same reasoning as server-owned drag detection in ADR 0038: the tile reports what the pilot *reports*, never what was *requested*.

### 7. Hold-to-confirm on state-changing actions only

Engage, disengage, tack and gybe change who is steering the boat, so each requires an ~800ms hold (`HoldToConfirmButton` in `autopilot-tile.tsx`) — releasing early cancels silently rather than firing on release, so a brushed tile can't trigger one. The ±1°/±10° heading nudges are deliberately exempt: they are the controls used constantly underway, and a confirm gesture on them would make the widget useless for its main job. Mode switching takes the same hold, for the same reason: changing from `compass` to `wind` changes what the pilot steers to, which is a change of who is effectively in command of the course.

Tack and gybe are offered as **four separate controls** (tack port/starboard, gybe port/starboard), each gated on its own advertised action id. An earlier revision showed a single directional pair relabelled from the pilot's current `mode` — `gybe` in a wind-referenced mode, `tack` otherwise. That was rejected: a provider may advertise one manoeuvre and not the other, and inferring which one the crew meant from `mode` is a guess about an action that swings the boom. The two extra buttons are cheap; guessing wrong is not.

Dodge is the one control that steers the boat without a hold. It exists to avoid something in the water *now*, and requiring an 800ms press with a pot buoy coming up is the wrong trade — it is a ±5° nudge that does not disturb the target course, and `Cancel` returns to it.

### 8. `availableActions` gates every control, unconditionally

An action the provider does not currently advertise renders **disabled, not hidden** — a missing button reads as a bug, a greyed one reads as "this pilot can't do that right now." This applies uniformly, including to engage/disengage/heading-adjust, even though those are logically closer to "core" operations than tack/gybe: the point of reading capability from the live stream instead of hard-coding it is that the pilot's own provider is the only thing that actually knows what it can do at this instant (e.g. adjusting target while in standby, or gybing in `compass` mode, may be meaningless for a given provider) — this codebase does not get to assume otherwise.

### 9. Stale state is visible

`buildAutopilotPayload` computes `stale` server-side from `snapshot.stale(context, "steering.autopilot.engaged", 5s, now)` — the same staleness primitive the watchdog and alarm reader already use — and includes it as a boolean *in* the payload rather than as a side channel. That matters for the SSE dedup at `vessel_state_stream.go:96`: because `stale` is part of the JSON being compared, the moment steering data actually goes quiet the payload's content changes (`false` → `true`) and a fresh event fires precisely then, even though `engaged`/`target` haven't moved. Without embedding it in the payload, an idle-but-disconnected pilot with an unchanging last-known value would never re-trigger an event at all. The frontend greys the tile and disables every command while `stale` is true — a last-known `engaged: true` is still shown (not hidden), but nothing further can be sent on the strength of it.

### 10. The pilot id is resolved server-side and cached, never carried by the frontend

`resolveAutopilotID` checks an optional `ui.autopilot_id` setting first (the two-pilot case, or a provider whose `isDefault` flag can't be trusted), otherwise resolves `GET .../autopilots/_providers/_default` and caches the result for 30s. The frontend never sees or sends a pilot id — every Helmcentral endpoint is pilot-agnostic by design, so a future two-pilot UI only needs a settings field, not a new API shape.

### 11. Reused rather than rewritten

- `signalkRequestJSON` (raw-body v2 requests) — used as-is.
- The generator's acquire-token/retry-once/`invalidateSignalKToken()` pattern — rather than copying it a third time, `signalkRequestJSONWithAuth` (already extracted for route activation) was itself split into `signalkRequestJSONWithAuthBody` (returns status+body) and a thin `signalkRequestJSONWithAuth` wrapper (collapses non-2xx to an error) — see `route_activation.go`. Autopilot's GET proxies need the body; its control endpoints don't. Existing callers and their tests (`TestSignalkRequestJSONWithAuth_RetriesOnAuthFailure`) are unchanged.
- `resolveSignalKURL()` — reused for settings/URL resolution, not reimplemented.
- The SSE emitter table and its existing change-suppression (`vessel_state_stream.go:96`) — autopilot is one more row, at 1s.
- Frontend: `use-autopilot.ts` follows `use-czone-switches.ts`'s `pending` set and `use-gauge-values.ts`'s `subscribeTelemetry` (via the shared `useTelemetryEvent` hook) rather than inventing either pattern again.

### 12. Off the default layout

Not added to `defaultDashboardLayout` in `dashboard_pages.go`. Most boats have no v2 autopilot provider yet, and a tile that permanently reads "no autopilot detected" on every fresh install is noise for the majority who don't have one. Users add it from the widget picker, same as any other optional widget.

## Consequences

- Widget id lists again grow by one in the two hand-maintained places ADR 0039 already flagged (`DASHBOARD_WIDGET_IDS` / `validDashboardWidgetIDs`) — unaddressed here, same as there.
- `resolveAutopilotID`'s 30s cache means a provider failover to a new default pilot can take up to 30s to be picked up (or is instant if `ui.autopilot_id` is set explicitly).
- The tile now covers the full v2 control surface — engage/disengage, mode, ±1°/±10°, tack and gybe both ways, and dodge — so there are no longer backend endpoints without a way to reach them.
- Mode switching depends on a REST probe that the rest of the tile does not need (decision 4). On a pilot whose provider omits `options.modes`, the selector simply does not appear; the tile stays fully usable without it.
- `state` (`PUT /api/autopilot/state`) remains implemented and tested with no tile control. It is redundant with engage/disengage for every provider seen in the v2 documentation, and adding a second way to do the same thing to a safety control is not obviously an improvement.
- This widget is not wired to the alarm engine. `notifications.steering.autopilot.*` (`waypointAdvance`, `xte`, heading) already flows into the alarm list for free via the existing `notifications.*` consumer (ADR 0038) — that is the right amount of alarm integration for this change.
- See decision 2: the exact SignalK v2 response field names are implemented against documentation, not a live provider, and carry the residual risk that entails until the verification step below has been run.

## Verification

1. `cd backend && go vet ./... && go test -short ./...` — all backend tests, including the httptest v2 stub in `autopilot_test.go` (id resolution and its cache, every endpoint's method/path/body shape, SK 4xx/5xx surfaced verbatim rather than flattened to 502, the token-expiry retry firing exactly once) and the stream/absence tests in `vessel_state_stream_test.go`.
2. `cd frontend && npm test && npm run lint` — `use-autopilot.test.ts` (no optimistic state, pending tracking, error surfacing) and `autopilot-tile.test.tsx` (availableActions gating renders disabled not hidden, hold-to-confirm does not fire on a short tap, heading nudges fire immediately, stale greys and locks the tile).
3. **Outstanding, not yet run** (no SK 2.x autopilot provider was reachable in this environment): against SK 2.x with a real provider plugin (`signalk-autopilot`, or a Garmin/Raymarine provider), ideally in simulation —
   - Confirm `GET .../autopilots` and `.../autopilots/_providers/_default`'s actual field names match what `resolveAutopilotID`/`getAutopilotHandler` expect; adjust the parsing in `autopilot.go` if not.
   - Add the widget from the picker, confirm it persists across reload.
   - Engage, adjust ±1°/±10°, confirm the target updates on the delta stream and the tile follows the *pilot's reported* value, never the requested one.
   - Point at an SK server with no autopilot provider and confirm the tile states that plainly.
   - Kill the SignalK connection mid-engage and confirm the tile greys within 5s rather than freezing on a stale heading.
