# ADR 0166: Mate reads logged telemetry for history questions

## Status

Accepted (2026-10-07). Builds on [ADR 0131](0131-mate-diagnoses-missing-and-stale-telemetry.md), which gave Mate
`check_signalk_paths`, `get_last_recorded` and `get_path_history`.

## Context

The skipper asked Mate: "Based on the last few passages, when do the alternators cut off charging?"
Mate made five tool rounds, all of them `search_conversations`, `search_documents` (folder "Passages")
and `read_document`, and answered from an incident report. It never called `get_path_history`,
`get_last_recorded` or `check_signalk_paths`, although InfluxDB holds the alternator currents, battery
state of charge and voltages for every passage.

Two causes:

1. The only prompt guidance pointing at InfluxDB (ADR 0131) is about telemetry that is missing, stale,
   frozen or unavailable. Nothing said that a question about how a reading behaved over time, or on past
   runs, belongs to the same tools.
2. Mate had no way to turn "the last few passages" into time windows. `get_path_history` needs an exact
   path and a start and end.

## Decision

- **A prompt rule, fixed wording, next to the ADR 0131 block.** For questions about what a reading did
  over time, past behaviour, trends, when something started or stopped, or "on the last few passages",
  the logged telemetry is the primary source: find paths with `check_signalk_paths` or
  `get_last_recorded`, get windows from `list_passages`, then call `get_path_history` per window.
  Documents and past conversations are secondary context.
- **A new tool, `list_passages`.** It reads `navigation.speedOverGround` from InfluxDB in 5-minute means
  and returns recent periods underway, most recent first: start, end, duration, approximate distance
  (integrated from the logged speed), mean and max speed. Arguments are `days_back` (default 14, at most
  60) and `limit` (default 5, at most 20).
- **Detection constants.** Underway is 3 kn or more, the same threshold the anchor auto-raise uses
  ([ADR 0099](0099-server-side-anchor-auto-raise.md)), so the two features agree. Gaps (slow buckets or
  missing data) up to 30 minutes stay inside one passage, which keeps a lock, a bridge or a wake from
  splitting a run. Runs under 15 minutes are dropped as berth shifting. 5-minute buckets keep a 60-day
  query near 17,000 points, inside the 6 second query timeout, and are fine enough for those two limits.
- **Fail fast.** With InfluxDB not configured or the query failing, the tool returns the error, as
  `get_path_history` does. With no speed logged at all in the range, the result says
  `no_speed_data` explicitly instead of an empty list, so Mate never reads "no data" as "no passages".
- `get_path_history`'s description points at `list_passages` for passage windows. Its bucket width
  already follows the span, so a short passage window gives fine buckets.

## Consequences

- A question about a reading across passages now takes `list_passages` plus one `get_path_history` per
  path and window. Mate may need several rounds; the existing tool-round cap applies.
- Passage boundaries are inferred from speed alone. A boat drifting faster than 3 kn on a current, or
  motoring slowly through a long no-wake zone, is misjudged. The result states the threshold so Mate can
  say how it found the windows.
- Distance is an integral of 5-minute mean speeds and is approximate.
- The operator needs the history log set up, as for the ADR 0131 tools.

## Alternatives considered

- **Reading passages from documents.** Rejected. That is what went wrong: a written incident report is
  one person's account of one event, and it answered a question about all recent passages.
- **A generic raw Flux query tool.** Rejected. It lets the model write queries against the whole bucket,
  with injection and cost risk, to solve what is a window-finding problem. Fixed tools with validated
  arguments stay in force.
- **Prompt rule alone.** Rejected. Without a way to get windows, Mate would still guess at timestamps.
- **A dedicated `get_passage_telemetry` that returns several paths per passage.** Deferred. It would
  save rounds but fixes the path list in code; the two-step shape reuses tools that already exist.
