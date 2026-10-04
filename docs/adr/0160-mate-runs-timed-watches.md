# ADR 0160: Mate runs timed watches on live telemetry

## Status

Accepted (2026-10-04). Builds on [ADR 0093](0093-onboard-assistant-over-openrouter.md) (Mate and
its tool loop), [ADR 0105](0105-mate-streams-its-answer.md) (a reply runs server-side and a page
rejoins it) and [ADR 0131](0131-mate-diagnoses-missing-and-stale-telemetry.md) (Mate reads the live snapshot and
InfluxDB for path freshness and history).

## Context

Mate could only look things up when asked. In a real chat the skipper asked it to watch port
against starboard engine load for intermittent spikes, and Mate had to answer that it could not
sit and watch a reading. The diagnostics tools answer "is it live now" and "what did InfluxDB keep",
but neither can see a spike of a second or two: the live check is one instant, and the history is
whatever the logger kept, bucketed to a minute or more.

A conversation turn is bounded (`assistantRunTimeout`, three minutes) and every tool in a round is
expected to return promptly, so a watch cannot be a tool that blocks for five minutes.

## Decision

Mate gets one more tool, `start_watch(paths, minutes, reason)`.

- **Live feed, 1 Hz, in memory.** The backend samples each path from the live SignalK snapshot
  (`nodeAt`, one node copy per path, aged the same way `check_signalk_paths` ages a leaf) once a
  second for the watch's length, and keeps the samples in memory. Up to six paths, 1 to 30 whole
  minutes, a short label per path, and a reason Mate writes for itself that is carried into the
  report. Out-of-range arguments are rejected, not clamped.
- **No source filter.** The live snapshot keeps only the latest value and `$source` per path,
  whichever source wrote it. A filter on one of two publishers would record the other's updates as
  gaps and pass or fail the start check at random. A `source` argument is refused with that reason;
  instead each path reports the sources it saw and how often they took turns
  (`source_switches`), so Mate can say a jump may be one sounder disagreeing with the other.
- **Fail fast at start.** Every path must be live when the watch starts: present, numeric and
  updated within 10 s. Otherwise the tool fails naming the path.
  The 10 s threshold is deliberately tighter than the 120 s staleness the rest of Helmcentral uses:
  at one sample a second, a value that has not changed for ten seconds is a held value, not a
  measurement.
- **Gaps are recorded, never filled.** A tick where a path is missing, stale or not a number is a
  gap with its reason. Statistics and excursions use real samples only.
- **The report.** Per path: tick and sample counts, min, mean, max, standard deviation, first and
  last, gaps, and excursions. An excursion is a run of samples more than five robust standard
  deviations (1.4826 x the MAD of the residuals), or 5% of the series' own range if larger, from a
  centred 61 s rolling median, merged into episodes with start, duration, peak and baseline. With
  exactly two paths in the same known units the same is reported for their per-second difference,
  which is the port against starboard case. Two paths in different units (volts and amps) get no
  difference, and the report says why. Values are SignalK units, times vessel local, the whole report held to
  the tool result budget (`capToolResultJSON`) by trimming listed excursions, then gaps, with the
  counts kept.
- **Summary at the end only.** Nothing is raised during a watch. Alarms already do that.
- **The report goes back into the same conversation.** When the watch ends, the backend appends the
  report as a message with role `watch` and starts one Mate turn through the same run registry and
  turn launcher a posted question uses (`beginAssistantTurn`, extracted from the message handler for
  this). An open chat rejoins that run the way it rejoins any other; a chat opened later finds the
  report and the answer in its history. The model sees the report as a user turn whose text says it
  is automatic and not from the skipper. The chat shows only its first line, "Watch finished: Port
  engine load and Starboard engine load (5 min)". The report is appended first, and the watch then
  gives up its slot (the one-per-conversation and concurrent limits) before the turn is started, so
  neither the report nor the slot waits on a busy conversation. If a question is already running in
  that conversation, the turn waits for it, for about ten minutes at most; after that the report
  stays in the conversation without a turn: the skipper sees it, and Mate reads it with the next
  question. A question asked while the turn waits already has the report in its history, so if Mate
  has answered one, the follow-up turn is not started; the reply to the question that was running
  when the report was appended does not count, since that turn read its history before the report
  existed.
- **Consent is checked again at the end.** If Mate has been switched off, lost its key or model, or
  the conversation has been deleted by the time the watch ends, nothing is appended and no turn
  starts, and the server logs why. Mate being on is the consent to send anything to the model
  ([ADR 0120](0120-turning-mate-on-is-the-consent.md)). Deleting the conversation also stops its watch.
- **One watch per conversation, three in all.** A second start in the same conversation is refused
  with the running watch's subject and end time, rather than replacing it. Simpler, and honest about
  what is already running. The process-wide cap keeps a runaway from sampling dozens of paths.
- **Stop.** `DELETE /api/assistant/conversations/:id/watch` (write tier) stops a watch without a
  report. It answers 204 only when it stopped a watch that was sampling; 409 when the watch has
  already ended and is handing over its report, so the chat keeps following it to Mate's answer;
  and 404 when there was nothing to stop, including a watch that has already finished, which the
  chat treats as a natural end and rejoins Mate's follow-up rather than clearing the chip as if it
  had been stopped. `GET` on the same path (read tier) returns the watch for the chat: `watching`,
  `reporting` from the end of sampling until the follow-up turn is registered, then `finished` for
  15 minutes. The chat shows "Watching &lt;labels&gt; · ends HH:MM" with a Stop button, and "Watch
  finished. Mate is reading it." while reporting. `finished` is never shown; it lets a chat that
  never saw the watch running, because it started and ended between two looks, still rejoin the
  follow-up. The end time is formatted by the server (`ends_at_local`) on the same vessel-local
  clock Mate states it in, not the viewing device's clock. The chip names readings by Mate's
  labels, never by path.
- **Mate is told the truth about it.** The prompt says to use `start_watch` when asked to watch or
  monitor something over minutes and never to claim a watch without one running, and the live
  part of the prompt names the watch running in the conversation, if any.
- **Cost.** The follow-up is one ordinary turn on the conversation's own model, billed and shown in
  its footer like any reply.

`start_watch` is the first tool with an effect beyond its own reply. It is still bounded: it starts
nothing that outlives the process, writes nothing to disk, and a retried call is refused by the
one-per-conversation rule rather than starting a second watch.

## Alternatives considered

- **Read InfluxDB after the fact.** Rejected. The logger's resolution and bucketing hide the
  second-long spikes the skipper was asking about, InfluxDB is optional, and "watch this for five
  minutes" is a question about what happens next, not what was kept. `get_path_history` already
  covers the history question.
- **Alert during the watch.** Rejected. Alarm rules and the alarm card exist for that, with
  thresholds the operator set and a presenter written for 02:00. A second, model-driven alerting
  channel would compete with them and could not be held to the same wording.
- **Persist watches across restarts.** Rejected for now. A restart loses the in-memory samples, so
  a resumed watch would report a window with a hole in it, and the single-operator boat gains
  little from the bookkeeping. The tool result and the docs say a restart cancels a watch.
- **A per-path source filter.** Proposed in the design and dropped once it was clear the snapshot
  holds one source per path at a time; see "No source filter" above. Doing it properly needs the
  snapshot to keep values per source, which nothing else needs yet.
- **Replace a running watch on a second start.** Rejected in favour of refusing it: silently
  dropping the first watch's samples would lose what the skipper asked for first.
- **A parallel delivery path for the report** (a notification, a separate stream). Rejected. The run
  registry already gives a reply that outlives the page, reattachment from any device, and
  persistence, and reusing it keeps one way for an answer to arrive.

## Consequences

- The report reaches OpenRouter in the follow-up turn, like any other tool result.
- The answer toast for conversations not on screen covers questions the tab itself asked; it does
  not yet fire for a watch's follow-up. An open chat sees the answer arrive; a closed one shows it
  next time it is opened.
- The follow-up turn uses the date the watch was started with. A watch that crosses local midnight
  answers with the earlier date; at 30 minutes at most, that affects only the maintenance tools'
  idea of "today" for that one turn.
