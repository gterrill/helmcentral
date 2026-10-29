# ADR 0227: Watches

## Status

Accepted (2026-10-29). Supersedes the in-memory timed watch of
[ADR 0160](0160-mate-runs-timed-watches.md) (its sampling, report and chat
follow-up stay; its "a restart cancels it" does not). Builds on
[ADR 0120](0120-turning-mate-on-is-the-consent.md) (turning Mate on is the consent to
send anything to the model), [ADR 0222](0222-mate-gives-a-vessel-status-snapshot.md)
(the boat's status builders), [ADR 0224](0224-alarm-rules-live-in-the-database.md)
and [ADR 0225](0225-mate-proposes-alarm-rules.md) (a record type created by a
proposal, with its table in `helmcentral.sqlite`) and
[ADR 0172](0172-mate-tools-are-one-record-each.md) (the tool registry). It is
cycle 6 of the agent-first plan, redesigned after a first draft of the same
cycle (called routines, never released) was reviewed with the operator.

## Context

The first draft let Mate run on a timer. Each routine owned a conversation and
a Mate turn per run, behind a separate "let routines run on their own" switch,
a daily spending limit, and a pause for 24 hours whenever a failed run's cost
could not be read. It worked, and the operator rejected the shape:

- What a skipper wants at 06:00 is a **report**: figures, in a fixed order,
  that look the same every day. A model deciding what to mention each time
  gives a different page every day and spends money to do it.
- The judgement of whether something is wrong belongs to rules the operator
  can read and change, the way alarms are. Mate's part is to read the result
  in context, not to decide what counts.
- A conversation per routine put findings in a chat list, where nobody looks
  for them, and made deleting a chat refuse.
- The spending limit and the unattended switch asked for a second consent
  after Mate was already on ([ADR 0120](0120-turning-mate-on-is-the-consent.md)), and
  a pause on an unreadable cost turned a billing lookup into an outage of the
  whole feature.

So the feature became Helmcentral's own: **watches**, as in the officer of the
watch. Helmcentral builds the report and applies the rules. Mate is one of the
things that reads it, and the thing that starts one on request.

## Decision

**A watch is a record.** The `watch` record type (table `watches`, beside the
alarm rules) holds a name, a schedule, the report sections it covers, the
thresholds that turn a figure into a finding, a free-text "what to look out
for" (up to 500 characters) and an enabled flag. Mate creates, changes and
deletes watches only through `propose_changes`, so the operator's **Apply**
tap is the consent to each one. The operator can do the same directly through
`POST`, `PATCH` and `DELETE` on `/api/watches`; the validation is one function
both paths call. At most 20 scheduled watches can exist.

**The schedule is cron, on the boat's clock.** A schedule is a standard
five-field cron string, parsed with `robfig/cron` as a parser only: our own
one-minute ticker decides what is due, so the scheduler stays injectable and
testable. The Watches page offers presets (hourly, every 2, 3, 4, 6 or 12
hours, daily at a time, chosen weekdays at a time, an hours window such as
22:00 to 06:00, or custom cron) and writes the cron they stand for. The backend
turns any cron string back into a sentence ("Every day at 06:00 boat time",
"Every hour from 22:00 to 06:00", "Custom: ...") and the next three run times,
through `POST /api/watches/schedule-preview`, so the page shows what a
schedule means before it is saved.

A schedule may not run more often than every 15 minutes. Mate reviews every
run, and a schedule of every minute would be a bill, not a feature.

A run is due at the most recent cron instant after the latest of the watch's
last run, its last schedule change and its creation, so moving the time never
runs a watch twice in a day. After downtime a due watch runs once and missed
runs are never made up. Time of day is read on the boat's clock, from its
position, and never as UTC. Without a position the boat's time is not known, so
no run is made. A skip is recorded with "No boat position, so the boat's time
is not known", at most once per missed instant. Because the zone is unknown, an
instant counts as missed only when it has passed in every time zone from
UTC-12 to UTC+14, so the note is never left early on any boat's account. The
skip is not a run: the run is still owed when the position returns.

**The report is deterministic.** Each run builds the sections the watch chose:
power, anchor, alarms, tanks, weather and instruments. The figures come from
the same builders `get_vessel_status` uses ([ADR 0222](0222-mate-gives-a-vessel-status-snapshot.md)),
the alarm log for what was raised, and the previous run's report for what to
compare with. A section that cannot report says why ("Unavailable: ...") and is
never filled from an older reading. "Since the last run" is the watch's
previous run; the first report covers the last 24 hours and says so.

**Findings are decided by rules.** The defaults are the same for every boat and
editable per watch:

- Alarms: any alarm raised since the last run.
- Instruments: any frozen, impossible or silent sensor.
- Power: the overnight-draw observation of ADR 0222.
- Anchor: the boat outside the anchor watch radius. With no anchor watch set
  the section says "No anchor watch set" and there is no finding.
- Tanks: a level that changed by 10 percentage points or more since the last
  run.
- Weather: pressure down 3 hPa or more in 3 hours, or the planned stay's
  forecast wind reaching the lowest no-go wind the operator has set for a
  passage ([ADR 0194](0194-passage-limits-are-no-go-only.md)). A stay has no
  course, so the lowest limit across the sectors is the cautious reading.
  Without any limit the forecast is shown and not judged.

The editable thresholds are the alarm count, the sensor-problem count, the tank
change in points and the pressure fall in hPa, each validated to a sensible
range. The other rules are on whenever their section is.

**Mate reviews every run.** When Mate is ready ([ADR 0120](0120-turning-mate-on-is-the-consent.md):
no per-watch switch, no extra consent, no spending limit), one unattended Mate
turn reads the report, the findings and the "what to look out for" text. It is
not stored in any conversation. The decision between reusing `beginAssistantTurn`
and driving the runner directly went to the runner: `beginAssistantTurn` is
built around a stored conversation, a run registry entry and an SSE stream, and
a review needs none of them. `assistantRunner` (the same agentic loop an
operator's question uses) is given a dedicated short system prompt (the boat
lines that matter, the operator's units, boat time, the voice of an alarm card,
and the rules below) and a list of read-only tools. The rules: judge in
context; add findings, never remove one; ask where unsure; never guess a cause;
never stand in for an alarm; keep it short.

The model ends its turn by calling `submit_watch_review` with its review and
any findings it adds, so what it says is parsed from arguments and never read
out of prose. A reply that finishes without the call is recorded as a problem,
not as a review. The tools offered are `get_vessel_status`, the alarm, history
and logbook reads, the forecast and tide reads, and `search_web` only while the
operator's web search setting is on. `propose_changes`, `start_watch`, the form
tools, `run_watch_now` and the chart tools are never offered, and are refused if
called. When Mate is not ready the report is still recorded, with the reason
there is no review.

**Cost is shown, never a gate.** The cost meter and the failure classification
of the first draft stay, now only to show a run's cost. A request OpenRouter
refused, or one that never connected, cost nothing. A response that had started
and was cut off is priced from OpenRouter's generation endpoint, retrying for
about a minute. A cost that still cannot be read is shown as "cost not known".
Nothing pauses and nothing is skipped over it. The Watches page shows each
run's cost and the last 24 hours' total.

**A run that cannot be saved stops the scheduler.** If the history cannot be
written, the scheduler logs it loudly, stops running watches until Helmcentral
restarts, and `GET /api/watches` reports "Watches stopped: the run history could
not be saved (...). Restart Helmcentral after fixing it." Running on would
spend model time on runs nobody could read back.

**One-off watches are watches.** The timed watch of [ADR 0160](0160-mate-runs-timed-watches.md)
becomes a saved watch of kind `one_off`. The sampling still happens in memory in
the same registry, with the same limits (1 to 30 minutes, up to 6 paths, one a
second, one per chat). What changes is that it is kept:

- It is written to the `watches` table before it starts, so a watch that cannot
  be saved does not run.
- Its samples are appended to `watch_samples` every two minutes, only the ones
  taken since the last save, by a writer goroutine of its own. A slow or stuck
  write never delays a tick; the queue keeps every batch.
- Its finished report is stored as a run and still goes to the chat that asked,
  which answers it as before.
- After a restart, a watch that has not ended resumes from its saved samples
  and shows the time Helmcentral was down as a gap. A watch whose time ran out
  while Helmcentral was down is reported from the samples it had, and the
  report says so.
- Stopping one from the chat leaves a skipped run saying so. A watch whose
  sampling crashed is stopped the same way, so a restart does not pick it up.
- Deleting one from the Watches page while it is still sampling stops it first:
  no report goes to the chat, and its samples and runs go with it.
- **A report is made once.** The end is recorded, with a `reported_at` mark,
  before the report goes to the chat. If that cannot be saved the row is
  switched off and marked in a second attempt. If even that fails, the failure
  is logged loudly and the process remembers it has reported the watch. A
  restart never reports a watch that has `reported_at`.

**Switching a watch back on restarts its clock.** A new schedule restarts the
schedule clock, and so does switching a disabled watch on again, from the Watches
page or through Mate. A watch never runs for an instant that passed while it was
off. A scheduled run that has waited behind another run (Run now, or an earlier
watch) checks again that it is still owed, so one instant is never run twice.
Run now is not cancelled when the operator leaves the page or stops the chat
turn; the run and its cost lookup have time limits of their own.

**Storage.** `watches` holds definitions. `watch_runs` holds one row per run:
the report, the findings (each with its section, rule, text and whether the
rules or Mate reached it), Mate's review or the reason there is none, the
cost, whether it is known, and the model. The first draft's tables
(`mate_routines`, `mate_routine_runs`) and its `conversations.routine_id`
column were never released and carry nothing worth keeping, so the new schema
drops the tables and the code no longer reads the column.

**Retention.** Every run is kept for a year. Once a day, runs older than that
that found nothing are dropped; a run with findings stays for good. A run can
be deleted by hand. There is no archive. A run is about 5 KB, so an hourly
watch grows about 45 MB a year. `GET /api/watches` reports the space the watch
tables use, counted from the text they hold, so the page can say so.

**Mate's tools.** `list_watches` (the scheduled watches, with ids and versions
for a proposal), `get_watch_runs` (the latest runs of a watch, or one run by id),
`run_watch_now` (runs a watch now, with no tap, because it only reads, and
returns the run) and the `watch` record type for `propose_changes`. A normal
turn is told to list watches and then propose one when asked for regular
checks, and to say that findings and the review appear on the Watches page.
"Ask Mate about this run" carries the run's id in the screen context the way an
alarm card carries `alarm_id` ([ADR 0226](0226-ask-mate-about-an-alarm.md)),
and the prompt tells Mate to read it with `get_watch_runs` first.

**REST.** `GET /api/watches` (watches with their schedule sentence, latest run
and whether it found something, the last 24 hours' cost, the space used and any
problem), `GET /api/watches/:id` (the watch and its runs, newest first, paged),
`POST`, `PATCH`, `DELETE /api/watches/:id`, `POST /api/watches/:id/run`,
`DELETE /api/watches/:id/runs/:runId` and `POST /api/watches/schedule-preview`.
Reads use the read tier and everything that writes or spends uses the write
tier.

## Rejected

**A conversation per watch.** Findings belong on a page built for them, with
the report beside the review. A chat per watch buried them in a list and made
deleting a chat refuse.

**A switch for running unattended, and a spending limit.** Mate being on is
already the consent ([ADR 0120](0120-turning-mate-on-is-the-consent.md)), and the model
sees only a report Helmcentral built. A limit that skips runs turns a
billing question into silence on the night it matters.

**Pausing on an unknown cost.** An unreadable cost is a display problem. It
stopped every watch for a day over a lookup that OpenRouter sometimes lags on.

**Letting Mate decide what counts.** Whether a tank moved enough is arithmetic
on a number the operator can set. Mate reads the result for what the rules
cannot see, and may add a finding, but cannot remove one.

**An archive.** A year of history with findings kept for good and a delete
button is enough, and an archive is a second place to look.

**Making up missed runs.** After a night with the box off, twelve hourly runs
would spend money to report on hours already gone. One run reports the boat as
it is.

**Reading a time of day as UTC with no boat position.** The skipper means 06:00
on the boat, and UTC can be a day off by the date line. The run waits, with its
reason recorded.

**A push on every finding.** Notifications are the next cycle. They will use
the existing transports in a category of their own, separate from alarms, so
that a watch can never be mistaken for one.

**Reusing `beginAssistantTurn` for the review.** It writes a message into a
conversation and registers a run that an open chat can rejoin. A review has no
chat to rejoin and no place to be written.
