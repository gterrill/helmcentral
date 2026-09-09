# ADR 0087: Forecast Warnings Are Alarms

## Status
Accepted

Extends ADR 0019 (pluggable forecast warnings), ADR 0055 (host-derived vessel paths), ADR 0070 (heavy-weather indicators) and ADR 0083 (ages on the gauge-values stream).

## Context

An official marine warning in force for the vessel's zone, fetched by the forecast-warnings plugin of ADR 0019, reached the operator one way: a dismissible banner on the dashboard, refreshed by a ten-minute browser poll. That banner had no severity, no occurrence log and no off-boat delivery. Its dismissal lived in one browser's localStorage, so the helm dismissing it did nothing for the phone. A dead provider cleared it silently, because the poll simply stopped returning bulletins.

A gale warning issued while everyone is ashore is the case the notification transports of ADR 0038 exist for, and it never went near them.

The alarm banner's own comment already named the problem: dismissal stored per browser is fine for an informational bulletin and wrong for a live condition. Two `role="alert"` banners stacked at the top of the dashboard with different acknowledgement semantics was the thing to remove.

ADR 0070 deliberately kept the wave indicators display-only, and that decision stands. Those are heuristics over a model. An official warning in force is a categorical fact from the met service, closer in kind to a bus notification than to a guess, and it belongs with the rest of the things that demand attention.

## Decision

### 1. Two derived paths, absent until fetched

The plugin's bundle is reduced host-side to two paths under `helmcentral.environment.`, both unitless:

| Path | Value |
| --- | --- |
| `forecastWindWarningLevel` | 0 none, 1 strong wind or small craft or watch, 2 gale, 3 storm or hurricane |
| `forecastSurfWarning` | 0 or 1 |

Neither is 0 by default. Both are absent until a fetch has landed and absent again once the last one is older than thirty minutes. A quiet day fetched an hour ago is a genuine 0 and reads as present, the same way `squashZoneIndex` reads 0 with a steady barometer. The age is reported past the thirty-minute point even though the value is not, so a staleness rule has something to watch climb.

The wind ladder is a first-match table over the plugin's free-text `warning_type`, checked case-insensitively: `watch` ranks 1 before anything else is tested, so a Gale Watch reads as the advisory it is; `hurricane` and `storm` rank 3; `gale` 2; `strong wind`, `small craft`, `wind advisory` and `brisk wind` 1. The level is the maximum across every wind section in the bundle. Surf is 1 when any surf bulletin has a section. Other categories map to neither path.

A wind type the ladder does not recognise ranks 1 and is logged by name. This is the floor of the ladder, not a masking fallback. The plugin has already asserted that the bulletin is active and that it is a wind warning. Discarding it because the host's vocabulary is incomplete would be the masking.

### 2. A background fetcher with its own clock

Nothing on the backend fetched warnings unless a browser asked. A goroutine now polls the configured provider every ten minutes from the vessel's snapshot position, shaped after the anchor-drag watcher rather than the context-free tide updater, so it stops with the stream on shutdown. The plugin adapter's own ninety-minute cache bounds the FTP traffic to the Bureau.

The slot the fetcher writes holds the mapped reading, not the bundle: wind level, surf flag and the time of the fetch. `computeDerivedPaths` runs every second and does no string work on the path.

The adapter's stale-on-error branch returns no error, `Cached` true and an old `CachedAt` when the upstream is unreachable. To the fetcher that is indistinguishable from a healthy cache hit unless checked, and a dead Bureau would read as a quiet sea for as long as the cache lived. A cached bundle older than the provider's own TTL can only come from that branch and is treated as a failed fetch. Failures keep the previous reading with its old timestamp so the path ages out on its own; they are logged once per distinct error with a recovery line when the run ends, not once per tick.

### 3. A derived path reports its own age

`derivedAwareAlarmReader` stamped every derived sample with the SignalK stream's last-message time. That was close enough for the barometer paths, which ride the stream, and wrong the moment a path was computed from anything else. A stale rule on the forecast path would have read as fresh for as long as the boat's other instruments kept talking, which is exactly the failure the rule exists to catch.

The reader now takes each path's age from `computeDerivedPaths`, the same figure ADR 0083 already carries on the gauge-values stream, and only falls back to the stream's last message for a path whose age is unknown. This changes what a stale rule means on every derived path: newest ring-buffer sample for the barometer, oldest contributing input for fuel, last successful fetch for the forecast. That is the intended reading in all three cases.

### 4. Four seeded rules, shipped enabled

| Rule | Path | Condition | Dwell | State |
| --- | --- | --- | --- | --- |
| Forecast wind warning | `forecastWindWarningLevel` | above 0.5 | none | warn |
| Forecast gale or storm warning | `forecastWindWarningLevel` | above 1.5 | none | alarm |
| Forecast surf warning | `forecastSurfWarning` | above 0.5 | none | alert |
| Forecast warnings unavailable | `forecastWindWarningLevel` | stale after 1800 s | 120 s | alert |

ADR 0070's set shipped disabled because its thresholds were one crew's numbers from a 1999 book, uncalibrated against this boat. There is nothing to calibrate here. The source is the official warning, already filtered to the vessel's zone and to what is currently in force. Shipping it disabled would mean the one warning an operator most needs never reaches the alarm centre, the log or a transport until they find the toggle.

The unavailable rule carries a dwell because a sample that has never been present counts as stale, and without one it would raise at every boot before the first fetch had a chance to land. The three value rules have no dwell and no hysteresis: the values are discrete and change rarely.

The set is seeded once per installation under its own marker, so a rule the operator deletes stays deleted.

### 5. The banner goes, the notice stays

The dashboard banner and its localStorage signature are deleted. The alarm banner and the drawer carry the warning from here, with the acknowledgement server-side so every screen agrees.

The alarm carries a number, and the number is not the information. The drawer card keys on the path and reads "Gale warning in force. Details on the Forecast page." rather than "Now 2. Clears below 1.5." Region, day and the link to the bulletin live on the Forecast page, where the wind notice already was and where a surf notice now joins it, so the card's sentence points at something. The sidebar's amber dot on the Forecast item stays for the same reason.

## What was rejected

**Raising the alarm from the HTTP handler.** It only runs when a browser asks. The whole point is delivery when nobody is looking.

**A boolean path with a new operator.** ADR 0070 already declined to grow the rule engine for a derived path. A ranked integer bound with "above" keeps every operator site untouched and gives the gale escalation for free.

**Publishing 0 when nothing has been fetched.** A boat that has never fetched must read as absent, not as a quiet sea. Absence is never a value.

**Using the adapter's `CachedAt` as the path's age.** Healthy operation against the Bureau serves from a ninety-minute cache, and the path would have looked stale most of the time while everything was fine. The fetch time is the host's own clock.

## Consequences

- A gale warning coming into force reaches the log, web push and every other enabled transport, and shows in the alarm banner on every screen until acknowledged there.
- A boat with no forecast-warnings plugin installed, or no position fix, carries a standing "Forecast warnings unavailable" alert until the operator disables that rule. This is the honest state: the operator believed something was watching, and nothing was.
- During a gale two cards show, the wind warning and the gale warning. Worst-wins puts the gale first.
- A backend restart re-raises a warning still in force as unacknowledged, because engine acknowledgement is in memory (ADR 0038). The banner's dismissal survived restarts but not device changes. The trade is accepted.
- Every derived path's staleness now means its own input's age. The barometer rules see the newest ring-buffer sample rather than the stream's last message, which is what they should have seen all along.
- One more goroutine and one provider call every ten minutes.
- `docs/reference/plugins.md` said the host performs no derivation on warnings. It now performs one, the wind ladder, and the page says so.

## Verification

`go test -short ./...` and 1850 frontend tests pass, with `npm run build` confirming nothing still imports the deleted banner.

The wind ladder is pinned by a table over both reference plugins' vocabularies, including the watch-before-gale ordering and the unknown-type floor. The fetcher is tested with injected provider and position functions: it stores at fetch time, keeps the previous reading on error, treats a stale-served bundle as a failure, waits on a position with a thirty-second retry and honours its interval. The reader's per-path age is tested directly, and an engine-level test drives the seeded rules through a gale, then ages the slot past thirty minutes and checks that the wind alarms hold rather than clear while the unavailable rule raises after its dwell.

End to end, two runs. First, the backend against the boat's live SignalK with a scratch state directory and the warnings fixture plugin, which serves a Strong Wind and a Gale bulletin for a Queensland zone: the wind path read 2 and the surf path 0 in the path picker, the wind and gale rules raised in the same second the first fetch landed, the unavailable rule cleared at that moment, the surf rule stayed quiet, and acknowledging the gale through the API returned 200 and moved it to acknowledged. The SignalK transport is off in a fresh state directory, so nothing was published onto the boat's bus.

Second, the dev stack hot-reloaded the same code against the real Bureau provider while this was being written, and the Bureau had a Strong Wind Warning in force for the Mackay Coast for the following day. The wind path read 1, the wind rule was active at warn, the dashboard showed one banner reading "Strong wind warning in force. Details on the Forecast page." with no forecast banner beneath it, the drawer card carried the same sentence with an Acknowledge button and no Silence, and the Forecast page showed the wind notice with its link at 1600 and 390 wide.
