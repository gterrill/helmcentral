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
2. **Logged history counts as well as the live tree.** A candidate is offered
   when a signature path is live or was logged to InfluxDB in the last 90 days.
   One query reads the last time every signature path was logged (an anchored
   measurement regex, which pushes down to storage; about half a second on the
   boat over 90 days), and the result is reused for a minute. Rows carry
   `last_logged`, so an engine that is off appears as not reporting now with
   the day it was last seen.
3. **One candidate list.** Discovery, the Roles "From the boat" groups, the
   tile picker and the Source select on an item's page all read
   `GET /api/vessel/discovery`; there is no second implementation.
4. **Unavailable history is said, not hidden.** If InfluxDB can't be read or
   isn't configured, the response carries `history_error` and the pages show
   that only sources reporting now are listed. History is never replaced by an
   empty result.
5. **Existing assignments are judged.** A row an item owns has a `signature` of
   carried, missing or unknown. Missing means the source is alive in the log
   (something under it was recorded in the last 90 days) but never carried its
   signature, as `propulsion.0` logged exhaust temperature and never RPM.
   Unknown means history could not be read, or the source logged nothing at all
   (laid up, or a short retention): silence is not evidence against a mapping.
   The history query therefore reads the last time of every path under the
   equipment sources, not only the signature paths (about 270 paths on the
   boat, under a second). It is loaded single-flight, a failure is reused for
   30 seconds, and a failure streak is logged once. Missing shows an inline warning in
   Discovery, under the role and on the item's page, and reopens the Setup step
   (Engines, House bank, or Charging and power sources) with the item, the
   source and the fix. Unknown never reopens a step: it stays done and says it
   couldn't check. Logged history is what keeps this consistent with ADR 0190's
   rule that a SignalK dropout does not reopen a finished setup: a dropped
   source that has ever reported its signature is carried.
6. **Mate says which mapping is silent.** `estimate_passage` returns an error
   naming the engine item and path when an engine's fuel rate or RPM has no
   points in the window. It blames the mapping only when the source is known
   to be missing its signature; otherwise it says the engine logged nothing in
   the window. It keeps "no underway history" for a genuinely empty
   join. `assess_passage_window` accepts both `passage_hours` and `speed_kn`
   on a leg (the hours win, and the result notes the speed was ignored); the
   model sent both three times and retried identically, so no card appeared.

## Alternatives rejected

- **Hiding instances with fewer than N paths.** Counts say nothing about which
  readings are present; `propulsion.0` has several.
- **Requiring every supporting reading** (fuel rate, run time). Some real
  engines lack them; the missing one is a feature gap, not a wrong instance.
- **Live tree only.** It reproduces the original failure: the right engines
  are absent exactly when the operator is setting up in port.
- **Falling back to the live list when history is down, silently.** It would
  present an incomplete list as complete.
- **Rewriting stored assignments automatically.** `propulsion.0` to
  `propulsion.port` is a guess; the operator chooses.

## Consequences

An item on a source with no logged signature is flagged for as long as the
history window holds nothing for it. A boat whose InfluxDB is not configured
sees the history notice on those pages and has checks reported as unknown.
