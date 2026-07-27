# ADR 0030: Selectable Max-Gust Windows and a Keyed `max_gust_kts` Contract

## Status
Accepted

Extends ADR 0020, which introduced the in-memory telemetry ring buffer and the Influx-vs-in-memory dispatch. This ADR raises that buffer's cap for wind gust specifically, and amends the dispatch pattern ADR 0020 §4 documents — preserving its semantics exactly while changing how they are evaluated.

## Context

The wind tile carried two max-gust readouts fixed at 10 minutes and 1 hour. The pair was hardcoded the whole way down: `max_gust_10m_kts` / `max_gust_1h_kts` in the `vessel-state` response, matching fields in `use-vessel-state`, matching props on `WindTile`.

Which window is useful is a matter of what the operator is doing. Deciding whether to leave an anchorage wants the last few days' worth of context; trimming sail to the current squall wants the last ten minutes. A fixed pair serves one of those at a time, and the operator cannot say which.

Two of the four wanted windows did not exist server-side. 30 minutes was simply never computed. 24 hours was **unreachable in memory**: `telemetryHistoryCapacity` was 4320 samples — ~6h at the 5s poll cadence — a figure ADR 0020 chose for depth-trend's 3h window and which `windGustHistory` inherited by sharing the same constant.

Widening the ladder from two windows to four, and the buffer from ~6h to 24h, also turned two pre-existing inefficiencies from negligible into significant. Both are addressed here, because this change is what made them bite.

## Decision

1. **Four nested windows — 10m, 30m, 1h, 24h — selected per card and persisted client-side.** Each MAX GUST card is a real `<button>` that cycles forward through the ladder, wrapping. The two cards are independent; both may sit on the same window. Defaults are 10m and 1h, preserving exactly what the tile showed before.

   Selection persists to `localStorage` under `windTile.gustWindow.left` / `.right`. This is a display preference belonging to whoever is looking at the screen, not vessel configuration — putting it in `settings.yaml` would round-trip it through `POST /api/settings` and make two people looking at two tablets fight over one value.

2. **One `max_gust_kts` object keyed by window replaces both old fields.** The response carries `{"10m": …, "30m": …, "1h": …, "24h": …}` rather than four `max_gust_*_kts` scalars, which would grow a new field per window forever.

   The old fields are **removed, not deprecated in place**. Keeping them alongside would leave two sources for the same number and an open question about which is authoritative — the speculative-compatibility trap this codebase avoids elsewhere. This is a private API between this backend and this frontend, both updated in the same commit; there is no third consumer to strand.

3. **`windGustHistory` gets its own capacity; `depthHistory` keeps the old one.** `windGustHistoryCapacity` is 17280 samples (~24h at 5s). `telemetryHistoryCapacity` stays 4320 and is now documented as sized for depth-trend's 3h window, which is all it ever needed.

   Raising the shared constant was rejected: it would have tripled `depthHistory` for no reason, since nothing reads depth beyond 3h.

4. **The monotonic clamp generalizes across the ladder.** The previous code clamped the 10m no-data sentinel to 0 and forced `1h >= 10m`. The same walk now runs the full ladder: the shortest window's sentinel clamps to 0, then each longer window is clamped to at least the previous one. A longer window's maximum cannot be less than a shorter window's, and a display that showed 24h below 10m would read as a bug to the operator whether or not it came from a sentinel.

5. **Exactly one source is consulted per request.** ADR 0020 §4 documents the dispatch as:

   ```go
   points := inMemoryDepthTrend(window)
   if influxTelemetryConfigured() {
       points = queryInfluxDepthTrend(window)
   }
   ```

   Applied to wind gust, that computes the in-memory result on every request and discards it whenever Influx is configured. At two windows over a 6h buffer the waste was small enough to overlook; at four windows over 24h it was 2.64 MB of garbage per `/api/vessel-state` request on precisely the deployments that had gone to the trouble of running Influx. `computeMaxGustKtsFor` is now an if/else.

   **ADR 0020's reasoning is untouched.** Selection still branches on *configuration state* only, never on query error or emptiness; a configured-but-failing Influx still surfaces its own `-1` sentinel rather than quietly falling back to the in-memory buffer. Only the evaluation strategy changed, from "compute both, keep one" to "compute one".

   The accompanying test proves non-invocation rather than comparing output — both paths can legitimately return the same numbers, so matching output would prove nothing. It holds `windGustHistory`'s mutex write-locked and asserts `computeMaxGustKtsFor` still returns; had the in-memory branch run, it would have blocked on `RLock`.

6. **All windows are computed in one backward walk of the ring buffer.** The previous shape called `inMemoryMaxWindGustKts` once per window, and each call ran `since()`, which scans every slot in the buffer *and* allocates a result slice. Four windows meant four full scans and four allocations per request.

   Because the ladder is **nested** — 10m ⊂ 30m ⊂ 1h ⊂ 24h — walking the ring newest-to-oldest while carrying a running maximum, and snapshotting that maximum each time the walk crosses a window's cutoff, answers every window in a single pass under a single `RLock`.

   Measured on an M1 Pro against a full 17280-sample buffer, all four windows:

   | | Time/op | Allocated/op | Allocs/op |
   | --- | --- | --- | --- |
   | Four `since()` scans | 542 µs | 2.64 MB | 50 |
   | Single backward walk | 89 µs | 544 B | 7 |

   Nesting is a real precondition, so the function sorts a local copy of the windows by duration rather than trusting caller order — the return value is a map keyed by window string, so callers never depended on input order anyway. **This guards order, not nesting**: a genuinely non-nested set (two disjoint windows) would still produce wrong answers undetected. Nesting-by-construction remains a documented precondition, stated here and in the function's comment rather than left implicit.

   A **monotonic deque per window, fed by hierarchically bucketed samples**, was considered and rejected for now. It is the correct design at higher sample rates, and max is associative and idempotent, so bucketing would be *exact* rather than approximate — unlike the mean-bucketing in `inMemoryDepthTrend`. It was measured rather than assumed: a monotonic deque's worst case is strictly decreasing input, which never pops, and a dying breeze over 24h is exactly that shape. At full buffer it retained **100% of the raw samples** — so it buys O(1) queries, not space. Against a single-pass query already at 89 µs on one vessel at 5s with four fixed windows, roughly 150 lines of new stateful structure could not be justified. It becomes the right answer if the poll cadence rises toward 1s, if the window count grows, or if many clients poll concurrently, since it moves work from query time to sample time.

7. **Window selection lives in `WindTile`, not `WindGaugeCluster`.** The tile renders the cluster twice — once for mobile, once for desktop, chosen by `md:hidden` / `hidden md:block`. State inside the cluster would give each viewport its own selection, and they would silently diverge across a breakpoint change.

## Consequences

Positive:
- The operator picks the averaging window per card, and the choice survives a reload.
- Adding a window is a one-line change to two ladders, with no new response fields.
- 24h max gust works with no InfluxDB, keeping ADR 0020's zero-dependency default install intact across the widened ladder.
- The in-memory read path costs 6.1× less time and ~4,900× less allocation per request; Influx-backed deployments no longer pay for it at all.
- Verified end-to-end against live vessel data, not only in tests: cycling through the ladder read 18.8 / 19.3 / 19.3 / 26.6 kts, monotonic across the ladder as intended.

Tradeoffs:
- **The 24h figure is only as deep as backend uptime** when Influx is not configured. A backend restarted an hour ago reports the maximum over that hour under a "24HR" label. This matches how the 1h window has always behaved and is a direct consequence of ADR 0020's in-memory design; an operator wanting history that genuinely survives restarts still needs Influx. Rendering `—` until the buffer fills was considered and rejected as less useful than a real number over a shorter span, but it is a defensible reversal if the label proves misleading in practice.
- **~553 KB resident** for the wind buffer, up from ~138 KB. Immaterial on the target hardware, and it buys the removal of far more per-request allocation than it costs once.
- **Two ladders must be kept in sync by hand** — `gustWindowLadder` in `backend/main.go` and `GUST_WINDOWS` in `frontend/src/lib/gust-windows.ts`. Each carries a comment pointing at the other. Nothing enforces it; a mismatch would surface as a window rendering `—`.
- **The nesting precondition is only partially guarded**, as described in Decision 6.
- **Selection is per-browser**, so the same vessel viewed from a phone and the helm tablet can show different windows. This is deliberate per Decision 1, but it does mean "what does the gust card say" has no single answer.

## Related
- ADR 0020: In-Memory Telemetry History, Optional InfluxDB (extended and amended here)
- ADR 0012: Configurable Bento Dashboard
