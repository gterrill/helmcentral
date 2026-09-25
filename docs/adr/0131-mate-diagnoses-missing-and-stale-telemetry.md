# ADR 0131: Mate Diagnoses Missing and Stale Telemetry

## Status

Accepted (2026-09-25). Adds three read-only tools to the onboard assistant's
tool-calling loop ([ADR 0093](0093-onboard-assistant-over-openrouter.md)):
`check_signalk_paths`, `get_last_recorded` and `get_path_history`.

## Context

An operator asked Mate: "The exhaust temperature and tank levels are no
longer available from SignalK. When did they stop receiving updates?" Mate
answered that it had no access to historical logs or per-path timestamps and
told the operator to go look at the SignalK admin console - a real gap, since
Helmcentral already keeps exactly the two records that answer this: the live
delta-stream snapshot (`signalk_snapshot.go`, `signalk_paths.go`) and, when
configured, InfluxDB via the `signalk-to-influxdb-v2` plugin
([ADR 0051](0051-trend-gauges-on-influxdb.md)).

Investigating this specific case (fleet notes, not part of this change)
found that every SignalK source named `YachtDevices.*` stopped publishing at
2026-09-21T00:34Z and never came back - a gateway or wiring fault upstream of
SignalK, invisible to Helmcentral's own health checks because SignalK itself
never reported the connection as down, it just stopped hearing from that one
gateway. Tanks and exhaust temperature both came from that gateway
(`tanks.fuel.{2,4,5,7}.currentLevel`, `tanks.freshWater.{0,3}.currentLevel`,
`tanks.blackWater.{1,6}.currentLevel`, each `.capacity`, and
`propulsion.<id>.exhaustTemperature` among others), while `venus.com.
victronenergy.*`, `Vesper_Cortex.*` and `WLN10.GP` kept reporting throughout -
a single dead gateway, not a SignalK outage, which is exactly the kind of
fault a per-source view catches and an "is SignalK connected" check does not.

Two separate facts were missing from Mate's reach, not one:

1. **Live freshness and per-source health.** The snapshot already carries a
   node's own declared timestamp and `$source` per path
   (`signalk_paths.go`'s `pathAge`, `derived_paths.go`'s
   `derivedInputMaxAge`), and the tile layer already uses both to decide
   what reads as stale. None of that was exposed to Mate; it had no tool
   that could even ask "what is SignalK's connection doing right now" beyond
   the live vessel-state fields already baked into every prompt.
2. **When a path or source stopped, for a path no longer live at all.** The
   snapshot only ever holds what the CURRENT PROCESS has received over the
   delta stream since it last started. `signalk_stream.go`'s own comment
   already establishes that SignalK replays its retained model on reconnect
   - but that replay only carries a path forward if the path's source is
   still sending something; a source that went silent before this backend's
   last restart leaves nothing to replay, and the path is not merely stale
   in the live tree, it is **absent from it entirely**. Verified against the
   production box (2026-09-25): `GET /api/signalk/paths` today returns zero
   `tanks.*` or `exhaust` paths at all, even though `GET /api/telemetry/
   history?path=tanks.fuel.2.currentLevel&window=7d` still returns InfluxDB
   history up to 2026-09-21T01:00Z. The live view and InfluxDB disagree
   about whether the path exists at all, and only InfluxDB is right about
   the past.

Both facts already exist somewhere in this codebase; nothing new needed to
be measured or recorded. The gap was entirely that Mate had no tool that
could read either one.

## Decision

### Three tools, matching the two facts above one-to-one plus the join between them

- **`check_signalk_paths`** answers "is it live, and what does SignalK look
  like right now": connection status and last message time
  (`signalKSnapshot.status`), a summary of every `$source` label across the
  WHOLE self tree with a path count and how long ago its freshest path
  updated (so a source that has gone quiet stands out even when the
  operator's filter is about something else), and, when a `filter` and/or
  `source` argument narrows it, the matching paths' value/units/source/
  declared timestamp/age/stale flag.
- **`get_last_recorded`** answers "when did X stop", against InfluxDB, for a
  `path_prefix` and/or `source` filter (at least one required) - this is the
  tool that still works once a path is gone from the live tree entirely,
  since InfluxDB remembers what the snapshot has already forgotten.
- **`get_path_history`** answers "what did X actually do around then": one
  exact path's InfluxDB history over an explicit range, bucketed to
  min/mean/max per bucket (a frozen sensor holding one stale value looks
  different from an outright gap - min==max==mean across many buckets says
  "still reporting, but the value never moves," which is its own diagnosis),
  overall min/mean/max, first/last seen, and gap ranges.

The system prompt (`assistant_prompt.go`) tells Mate to reach for
`check_signalk_paths` first on any "is X missing/stale/frozen" question,
then `get_last_recorded`/`get_path_history` for "when did it stop" and "what
did it do before then", to name the specific source it finds rather than
report a vague absence, and to stop sending the operator to the SignalK
admin console for a question these tools can answer directly - only falling
back to that when InfluxDB is not configured and the live snapshot has
already forgotten the path, since that combination is the one gap none of
these three tools can close.

### The node's own declared timestamp, not the snapshot's arrival time

`check_signalk_paths` reuses `pathAge`'s existing preference
(`signalk_paths.go`): a path's own declared SignalK `timestamp` wins over
`pathSeen` (the snapshot's own record of when IT last received something for
that path) whenever the node carries one at all, because a reconnect replay
resets `pathSeen` to the replay's arrival time for every path it touches,
dead ones included, while the node itself still carries the source's
original timestamp. `pathSeen` is consulted only when the node has no
timestamp of its own. Reusing the identical logic (`signalKPathSampleAge` in
`signalk_paths.go` mirrors `pathAge`'s body against a node already in hand
from the tree walk, rather than re-resolving it) means Mate's staleness
answer can never disagree with what a tile bound to the same path already
shows for the same reason.

The staleness threshold itself is `derivedInputMaxAge` (`derived_paths.go`,
120 seconds) - not a new number picked for this tool. That constant already
has to agree with the frontend's `STALE_AFTER_SECONDS`; reusing it here
rather than inventing a fourth copy means Mate's "stale" and the tile's own
stale badge are reading the same rule.

### A fixed aggregation-window allowlist, never the model's own text

`get_path_history`'s per-bucket window (`assistantPathHistoryBucketWidth`)
is chosen purely from the requested span's length, out of a fixed switch
statement (`1m` up to `3h`, doubling roughly by an order of magnitude up to
`12h` past 30 days) - never from anything the model supplies. This is the
same reason `telemetryHistoryWindows` (`telemetry_history_api.go`) is
already an allowlist rather than a duration parse: the window/`every` value
is interpolated into the Flux query text, and a parse would accept
`1h) |> yield(` as far as the grammar is concerned. `get_last_recorded`'s
`lookback_days` is a plain clamped integer (never a string), so it needs no
allowlist of its own - it is interpolated as a bare number, the same way
`queryInfluxPathRange`'s `every` argument already is for a host-chosen fixed
literal.

Every other piece of caller-supplied text that reaches a Flux query -
`path`, `path_prefix`, `source` on all three tools - goes through
`fluxStringLiteral` (`influx.go`), which rejects Flux's own `${...}`
interpolation syntax outright rather than trying to escape it, exactly as
every existing Influx-backed query in this file already does. `get_last_
recorded`'s prefix matching uses Flux's `strings.hasPrefix`, with the prefix
value itself still going through `fluxStringLiteral` first.

### Read-only, and the live view can be honestly wrong

All three tools only ever read. `check_signalk_paths` fails explicitly
(an error, not an empty success) when the snapshot has no self tree at all
yet - SignalK never connected, or this vessel's context is not known - per
AGENTS.md's fallback policy: a lookalike empty result would read as "nothing
matches" rather than "nothing has been asked yet." The two Influx tools
return an explicit "InfluxDB is not configured" error, not an empty list,
when Influx is off - the same contract `queryInfluxPathRange` already gives
`estimate_passage`.

`check_signalk_paths`' own doc comment states plainly that its live view can
be incomplete in a way that looks like completeness: a path entirely absent
from the self tree does not mean the vessel never had that instrument, only
that nothing has arrived for it since this backend process last started.
This is precisely why `get_last_recorded` exists as a separate tool rather
than folding "check InfluxDB too" into `check_signalk_paths` itself - the
prompt tells Mate to check both rather than stopping at the first "not
found."

## Verification

Verified against the production box (2026-09-25, read-only HTTP checks
only): `GET /api/signalk/paths` confirmed zero `tanks.*` or `exhaust` paths
in the live tree today, and `GET /api/telemetry/history?path=tanks.fuel.2.
currentLevel&window=7d` confirmed InfluxDB still holds that path's history,
flat from around 2026-09-20T21:00Z and with no further points after
2026-09-21T01:00Z - consistent with the known YachtDevices outage and
demonstrating the exact "live view says nothing, InfluxDB says something"
split this ADR is built around. The production `settings.yaml` (via `GET
/api/settings`) confirmed InfluxDB is enabled there, bucket `SignalK_Data`,
org `Pikorua` - the same org/bucket shape `loadInfluxSettings` already reads.

The `signalk-to-influxdb2` plugin's own source (`src/influx.ts`, upstream
GitHub) confirms the tag names this ADR's queries filter on: `context`,
`source` and `self` (boolean) per point, `value` as the field - `source`
matching what `get_last_recorded`/`get_path_history` filter on, `context`/
`self` left unfiltered, matching every existing Influx query in this
codebase (`queryInfluxPathRange` and friends have never filtered on context
either).

Not verified: an authenticated query directly against the production
InfluxDB instance (no token was available for this change), so the exact
Flux this ADR ships - `strings.hasPrefix`, `group(columns: ["_measurement",
"source"]) |> last()`, and the per-bucket `aggregateWindow` variants with an
added `source` filter - has not been executed against a live InfluxDB. It is
covered instead by unit tests against the built query TEXT
(`buildInfluxLastRecordedFlux`/`buildInfluxPathStatFlux` in `influx.go`,
extracted specifically so their string construction and escaping can be
tested with no live connection), following the same structure every other
Flux query in this file already uses successfully in production. `go test
-short ./...` and `go vet ./...` pass.
