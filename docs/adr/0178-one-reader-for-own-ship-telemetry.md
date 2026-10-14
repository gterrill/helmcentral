# ADR 0178: One reader for own-ship telemetry

## Status

Accepted (2026-10-15). Builds on [ADR 0037](0037-signalk-delta-stream-ingestion.md)
and [ADR 0083](0083-ages-ride-the-gauge-values-stream.md); leaves
[ADR 0042](0042-nearby-vessel-staleness.md) and
[ADR 0068](0068-stale-source-marking-on-tiles.md) as they are.

## Context

Since ADR 0037 the delta stream is the only source of vessel data. The
snapshot holds a REST-shaped tree per context, and there is no REST read
path behind it to fall back on. An architecture review proposed this change
on the premise that a REST fallback still existed beside the snapshot. It
does not, and nothing here adds one.

How the backend read our own boat out of that tree had not caught up. About
35 sites deep-copied the self tree (`selfTree()`) or copied one node
(`nodeAt`) and walked it with a family of helpers in `signalk.go`:
`lookupNumber`, `lookupString`, `lookupFirstNumber`, `lookupAnyMap`,
`lookupNumberFromAnyChild`, `sumNumberFromAllChildren`, `lookupMainBattery`
and more, plus `lookupAnyValue` in the alarm reader and `numericNodeValue` in
the anomaly detector. Three problems came with them.

- `lookupNumber` returned -1 on a miss, so -1 travelled from the tree into
  every consumer. Commit 5c6541f had to make the signed electrical fields
  null on the wire because a real -1 A was being read as "unknown", and every
  fallback chain still skipped past a real -1 to the next path.
- Callers appended `"value"` themselves, usually as "try `X.value`, then bare
  `X`", so each one restated SignalK's leaf shape and some got it subtly
  different from their neighbours.
- Ages came from two clocks. The gauges and derived paths used the ADR 0083
  rule (the source's own timestamp, else arrival time) through `pathAge`; the
  diagnostics and Mate's watch reimplemented it beside the node they had in
  hand; the tiles used `freshestTimestampAge` over a subtree; the alarm
  engine used arrival time alone.

## Decision

`backend/telemetry_view.go` defines `telemetryReader`, the one way the
backend reads our own boat:

```go
t := globalSignalKSnapshot.selfTelemetry() // one consistent copy
v, ok := t.Number("environment.depth.belowTransducer") // Value, Age, AgeKnown, Source
s, ok := t.String("navigation.state")
b, ok := t.Bool("electrical.generator.0.manualStart")
p, ok := t.Position()                       // raw navigation.position
smp := t.Sample(path)                       // the leaf at exactly path
ts, ok := t.Timestamp("environment.wind")   // newest source timestamp in a subtree
ids := t.Children("electrical.batteries")   // sorted instances
paths := t.Paths()                          // every leaf, for the path picker
```

- **Paths are plain SignalK paths.** Callers never write `.value`. `Number`,
  `String` and `Bool` accept the leaf at the path, a bare value at the path,
  or a field inside an object leaf on the way (`design.length.overall`).
  `Sample` is strict: it reads only the leaf at exactly the path, which is
  what a gauge or an alarm rule binds.
- **No sentinels inside the backend.** A missing path, or a value of the
  wrong type, is `ok == false`. An age with no timestamp and no arrival time
  is `AgeKnown == false`. A real -1 is a reading.
- **-1 survives only at the payload edge.** `vesselStateData`,
  `electricalStateData`, the solar, tanks and autopilot payloads, and the
  gauge-values ages still carry -1 for "unknown" to the browser. Each
  builder now produces that -1 itself from `ok` or `AgeKnown`. Moving a field
  to null on the wire is a separate decision, field by field, with the
  frontend change beside it.
- **One age rule.** `telemetryAge` is ADR 0083 exactly: the node's newest
  source timestamp when it declares one, arrival time when it does not,
  unknown when there is neither. `pathAge` applies the same function to the
  node at a path. `Timestamp` deliberately does not fall back to arrival
  time, for the tiles that judge a whole subtree by the source's own clock.
  The alarm engine's stale-data check still times arrival alone, as it
  always has; the sample carries `Received` for it.
- **Own ship only.** Other vessels keep their current reads under ADR 0042's
  arrival-time rule: nearby vessels, collision targets (`nodeAtContext`),
  `vesselsTree`, and the notification sync over every context.
- **SignalK paths only.** The reader does not resolve `helmcentral.*`
  derived paths. Derived values are computed from the reader, so resolving
  them inside it would make the reader depend on its own consumers.
  `derivedAwareAlarmReader` still layers them on top.
- **SI out.** Values come back exactly as SignalK stores them. Unit
  conversion and the "at most 2π means radians" heuristic stay with the
  consumers. So do staleness thresholds.
- **Two views.** `selfTelemetry()` copies the self tree once, so every
  value a fetcher reads belongs to the same instant, and costs what
  `selfTree()` did. `liveSelfTelemetry()` copies nothing up front: each read
  resolves its path under the snapshot's read lock and copies only what it
  returns. The per-tick readers that touch a handful of leaves (the collision
  encounter line, Mate's watch, the anomaly detector's inputs, the hour
  meters) use it, so none of them pays for a whole-tree copy.
- **`alarmReader` keeps its interface.** `alarmReader` and `alarmSample` are
  unchanged; `snapshotAlarmReader` now reads through the view, so there is
  one tree walk and one age rule behind every gauge, rule and derived path.
- **A second adapter.** `fakeTelemetry` in `telemetry_view_test.go`
  satisfies the same interface from a map of samples, so a consumer can be
  tested without building a snapshot. The existing snapshot-building test
  helpers are unchanged.

The helpers that only ever read our own tree are deleted: `lookupFirstNumber`,
`lookupNumberOK`, `lookupFirstNumberOK`, `lookupNumberFromAnyChild`,
`sumNumberFromAllChildren`, the map form of `lookupMainBattery`,
`freshestTimestampAge`, `lookupAnyValue`, `numericNodeValue`,
`collectSignalKPathSamples`, `signalKPathSampleAge` and `signalKSelfPayload`.
`lookupString`, `lookupNumber`, `lookupBool` and `lookupAnyMap` remain for
trees that are not our own boat's snapshot: other vessels, the CZone switch
payload fetched over REST, and the connection probe.

## Alternatives rejected

- **Grow `alarmReader` into the reader.** It is the alarm engine's narrow
  numeric view, and keeping it that way was a deliberate choice. It now sits
  on top of the reader instead.
- **Null on the wire now.** Every -1 field would need its frontend reader
  changed in the same commit. That is worth doing one field at a time, not as
  a side effect of a backend refactor.
- **Migrate other-vessel contexts too.** They age on arrival time (ADR 0042)
  and read a different, much smaller set of fields. Folding them in would
  mix two age rules behind one interface.
- **A REST fallback behind the reader.** There is none (ADR 0037), and adding
  one would mask a dead stream as a working boat.

## Consequences

A new own-ship reading is one call with a plain path, and its absence is a
boolean rather than a number that might be real. The payloads the browser
receives are byte-identical: every builder was dumped against the recorded
fixtures before and after the change and the dumps compared equal. Two
behaviours change on purpose, outside those fixtures: a reading of exactly -1
is now a reading, so a fallback chain stops at it; and the current's set is
read from an object-valued leaf (environment.current's own object, or a drift
object carrying a direction) the same way as from separate leaves, where
before only the rate, or nothing, came through. A tick with no position, or half of one, no longer reaches the GNSS
trust gate as a -1,-1 fix: the gate is told there is no fix, holds the last
trusted one, and does not compare the gap against the next fix as a jump.

Two places still read our own tree structurally rather than through the
reader: the notification walkers (`alarm_notifications.go`,
`alarm_reconcile.go`, `alarm_bus_watch.go`), which copy the notifications
branch with `nodeAt` and walk it as alarm state rather than telemetry, and
the anomaly detector's frozen-input tracker, which times arrival with
`lastSeen`. `selfTree()` is now used only by tests.
