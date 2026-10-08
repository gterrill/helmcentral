# ADR 0186: Engine Stumble Detection

## Status

Accepted (2026-10-08). Built and tested against three captured 1 s histories.
Not yet deployed or run on the boat.

## Context

From 2026-10-02 to 2026-10-04 Pikorua's port engine was being starved of fuel
(clogged tank pickup tubes). In the 1 s telemetry the signature is
unmistakable: at a steady throttle, port rpm repeatedly dips 50-140 rpm below
its steady value for one to three seconds and recovers, while engine load
spikes (33% to 50%) as the governor asks for more fuel. Starboard, at the same
time, held within 2 rpm. Nothing told the crew; they lost the engine and ran
42 NM on one.

The twin differential detector ([ADR 0132](0132-anomaly-detection-cycle-1.md))
did not and could not catch this. It compares slow medians of coolant
temperature, oil and boost pressure and load between engines, so a fault that
lives in seconds-long rpm excursions averages away. It also needs engines set
up in Settings -> Vessel and several weeks of learned baseline, and its rules
ship disabled until that baseline exists.

## Decision

A new detector, `anomaly_stumble.go`, in the same shape as the others: it
computes a number, publishes it as the derived path
`helmcentral.anomaly.propulsion.<id>.stumbleCount`, and a seeded `alarmRule`
watches it. State lives in an `engineStumbleTracker` owned by the anomaly
goroutine (`anomalyTrackers.stumble`), next to the frozen-sensor and voltage
trackers, not in the derived-path computation.

Per engine, discovered from `propulsion.<id>.revolutions` with no vessel
setup (as sensor health discovers its inputs, ADR 0132). All numbers are
named constants. SignalK revolutions are Hz and are converted to rpm on entry.

- Each new reading (by its own timestamp, observed once however often the tick
  runs) joins a trailing window of 10 samples. A gap of more than 3 s between
  readings resets the window.
- With at least 6 samples the engine is steady when the median is at least
  800 rpm and the spread (max - min) is at most max(15 rpm, 1% of the median).
- While steady, a sample at or below median - max(40 rpm, 3% of the median)
  starts a dip. It counts only if rpm returns to at least 98.5% of that median
  within 6 s. No recovery means the operator moved the throttle. After a
  counted dip the window is cleared and restarts from the recovery sample.
  Dip samples never enter the window, so the reference median does not sag.
- Sea-state suppression: a dip is discarded when another engine had a dip
  within 3 s either side. Both props unloading on the same wave is the sea,
  not the fuel. A single-engine boat has nothing to compare and gets none.
  Because the other engine's dip is only recognised once it has recovered, a
  dip is held back for 9 s (3 s window plus 6 s recovery) before it can count.
- `stumbleCount` is the number of counted dips in the trailing 10 minutes. The
  evidence names the count, the deepest dip and the steady rpm it fell from
  ("6 RPM dips in 10 min · up to 140 rpm below 1170 rpm"). The count keeps
  publishing after an engine stops reporting and decays to 0, rather than
  going absent: absence holds a raised alarm indefinitely, and the count is a
  statement about the last ten minutes, not a live reading.
- Inputs go through the same checks as the other detectors: the operator's
  ignore list, and `inputValidity` (present, fresh, in range) per reading.

Seeded rule: one per engine, enabled, warn, above 2.5 with a hysteresis of 2
so it fires at a count of 3 and clears at 0, no dwell (the count is already a
ten-minute statement). It is not tied to vessel settings. The detector
goroutine seeds an engine's rule the first tick it sees that engine, under the
marker `anomaly-v1:stumble:<instance>` and the rule id
`helmcentral:anomaly-engine-stumble-<instance>`, both keyed on the SignalK
instance id and never the display name, for the reason
`anomalyEngineSeedMarker` gives. This reuses `seedAnomalySet`; it is the one
seeding path in this file that runs from the tick rather than from startup and
vessel-settings saves, because the engines are not known at either moment. An
engine that appears later gets its rule then, and a rule the operator deletes
stays deleted. The title is the operator's engine name from Settings -> Vessel
when set, else the instance id made readable, giving "Port Engine Stumbling".
The body is the evidence line. Per [ADR 0157](0157-alarm-cards-say-what-happened-not-where-it-came-from.md)
it states no cause: Helmcentral knows the engine dipped, not why.

## Calibration evidence

The algorithm was prototyped offline and calibrated on a month of 1 s history
before being ported. Zero false positives on starboard, and on port before the
fault. It fires on port from 2026-10-02. The three captured histories are kept
as fixtures in `backend/testdata/anomaly/` and replayed by timestamp:

- 2026-10-04 (30 min, fault): port reaches and holds 3 or more, starboard 0.
- 2026-10-02 (30 min, earliest onset): port reaches 3 or more, starboard 0.
- 2026-09-28 (95 min, both engines healthy at 1807 rpm cruise, throttle
  changes at the end): 0 on both throughout.

## Rejected alternatives

- Rolling standard deviation of rpm. Confused by ordinary throttle changes,
  which look like large variance; the steadiness gate plus a recovery
  requirement separates a stumble from a throttle move where a variance
  threshold cannot.
- Fuel-rate residual between engines. The signal is only 5-12% of the rate,
  too small to hold a threshold against load and sea.
- Extending the twin differential detector. It is slow-median by design and
  needs a learned baseline; bending it to seconds-scale events would lose both
  properties.

## Consequences

- Every install with an engine publishing rpm gets a stumble rule, enabled, on
  first sight of that engine. No setup and nothing to learn.
- Nothing is learned per vessel, so there is no per-boat tuning. The
  thresholds are fractions of the engine's own steady rpm; a boat whose
  engines legitimately hunt at a steady throttle would need the rule edited
  or removed.
- Nine seconds of latency before a dip counts, plus the three-in-ten-minutes
  rule, means the warning arrives a few minutes into a developing fault, not
  on the first stumble.
- The stumble paths are not offered in the rule editor's path picker, which
  lists engine paths from configured engines only. The seeded rule is the
  supported use.
- Open: the sea-state filter cannot separate a fault that hits both engines of
  a twin together (shared fuel supply). That is deliberately traded for
  immunity to waves.
- Open: not yet seen on the boat. Check the first underway run for nuisance
  warnings in a seaway.
