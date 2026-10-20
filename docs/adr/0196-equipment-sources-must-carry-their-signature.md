# ADR 0196: Equipment sources must carry their signature

## Status

Accepted (2026-10-21). Amends
[ADR 0187](0187-equipment-items-own-their-signalk-source.md), whose Discovery
offered any instance with any path, and
[ADR 0190](0190-first-run-setup-status.md), whose Engines step counted any
assigned engine as done.

## Context

On the boat, NMEA 2000 publishes exhaust temperature under `propulsion.0` and
`propulsion.1`, while revolutions, fuel rate, run time, oil pressure and the
rest sit under `propulsion.port` and `propulsion.starboard`. With the engines
off the second pair is absent from the live tree, so Discovery offered only the
first, the operator assigned both Cummins items to them, and Setup said
"Engines: done". Mate's passage estimate, engines-running detection, the chart
engine roles, the anomaly detector, sensor names and hour meters all read paths
that never had data, silently.

## Decision

1. **A signature per kind.** One table (`source_signatures.go`) names the
   reading each kind needs; any one listed leaf is enough:

   | Kind | Leaves |
   | --- | --- |
   | engine | `revolutions` |
   | battery | `voltage` |
   | alternator | `current` |
   | solar | `panelPower`, `power` |
   | tank | `currentLevel` |
   | generator | `state`, `runtime` |
   | charger | `current` |
   | inverter | `ac.output.power`, `acout.power`, `acOutputPower` |

   Engine is `revolutions` because that is what makes an engine an engine;
   `fuel.rate` and `runTime` support features but do not define one. The others
   are the one or two readings the tiles and checks start from, checked against
   the real instances on the boat (an inverter instance with only
   `operatingState` is not an inverter).
2. **Helmcentral keeps its own record of what the boat has reported.** A
   candidate is offered when a signature path is live or has been seen reporting.
   The record is two SQLite tables in helmcentral.sqlite, written through RunTx
   like the remembered units of ADR 0193: `signalk_source_seen(vessel, path,
   first_seen, last_seen)` keyed by vessel and path, and
   `signalk_source_tracking(vessel, tracking_since, seeded_at, seeded_from)`.
   Only paths under the equipment source shapes are recorded (about 270 on the
   boat). A goroutine looks at the live tree once a minute, a path counts as seen
   if it reported in the last ten minutes (dated by when it arrived), a new path
   is written at once and a known one at most every five minutes, in one batch;
   a failed write stays pending, is retried on the next pass and is logged once
   per failing streak. The stream goroutine never touches it. Rows carry
   `last_seen`, so an engine that is off appears as not reporting now with the
   day it was last seen. A new install has nothing in the record until an engine
   has run, so Discovery says since when Helmcentral has been watching.
   **InfluxDB is an optional one-time seed, never read per request.** When a
   vessel has not been seeded and InfluxDB is configured, one query reads the
   last time each equipment path was logged in the last 90 days (an anchored
   regex pushed down to storage, under a second on the boat); the result is
   merged under what is known and the seed is recorded (when, and from when). A
   failed query is logged and retried every 15 minutes; an unconfigured InfluxDB
   is logged once and left alone. Without a seed, tracking from live data alone
   is enough.
3. **One candidate list.** Discovery, the Roles "From the boat" groups, the
   tile picker and the Source select on an item's page all read
   `GET /api/vessel/discovery`; there is no second implementation.
4. **The record says how much it covers.** Discovery carries `tracking` (since,
   seeded, covers) so the pages are honest about a young record: "Helmcentral has
   been watching the boat since 18 Oct. A source that is switched off is listed
   once it has reported since then."
5. **Existing assignments are judged, but only on a record that can bear it.**
   A row an item owns has a `signature` of carried, missing or unknown. Missing
   means the source has been seen carrying other readings and never its
   signature (`propulsion.0` reported exhaust temperature and never RPM) **and**
   the record covers enough time: it was seeded from history, or Helmcentral has
   watched this vessel for at least seven days (`sourceRecordMinTracking`; an
   engine that is off for a few days must not look like one that never reports
   RPM). Unknown means anything less: the record is younger than that, or the
   source has never been seen at all (laid up). Silence is not evidence against a
   mapping. Missing shows an inline warning in Discovery, under the role and on
   the item's page, and reopens the Setup step (Engines, House bank, or Charging
   and power sources, judging only the items the roles chose) with the item, the
   source and the fix. Unknown never reopens a step: it stays done and says it
   couldn't check yet, since when Helmcentral has been watching. This is
   consistent with ADR 0190's rule that a SignalK dropout does not reopen a
   finished setup: a source that was ever seen reporting its signature is
   carried for good.
6. **Mate says which mapping is silent.** `estimate_passage` returns an error
   naming the engine item and path when an engine's fuel rate or RPM has no
   points in the window. It blames the mapping only when the source is known
   to be missing its signature; otherwise it says the engine logged nothing in
   the window. It keeps "no underway history" for a genuinely empty
   join. `assess_passage_window` accepts both `passage_hours` and `speed_kn`
   on a leg (the hours win, and the result notes the speed was ignored); the
   model sent both three times and retried identically, so no card appeared.

## Alternatives rejected

- **Reading InfluxDB per request** (the first version of this ADR). It made an
  optional store a requirement for a core setup feature, needed a single-flight
  cache and a failure mode to explain on every page, and its retention can be
  shorter than the question needs.
- **Judging a source missing from the first day.** An engine that has not run
  since Helmcentral started watching would be flagged as wrong.
- **Hiding instances with fewer than N paths.** Counts say nothing about which
  readings are present; `propulsion.0` has several.
- **Requiring every supporting reading** (fuel rate, run time). Some real
  engines lack them; the missing one is a feature gap, not a wrong instance.
- **Live tree only.** It reproduces the original failure: the right engines
  are absent exactly when the operator is setting up in port.
- **Rewriting stored assignments automatically.** `propulsion.0` to
  `propulsion.port` is a guess; the operator chooses.

## Consequences

A fresh install lists a switched-off engine only after it has been seen running
(or after the seed), and flags nothing for a week. The charger-input flag on a
battery that is not reporting now still comes from the live source record the
sensor namer keeps for the running process; the registry stores paths, not
`$source`, so after a restart a battery input that is off reads as a plain
battery until it publishes again.
