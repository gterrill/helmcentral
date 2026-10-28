# ADR 0227: Mate routines

## Status

Accepted (2026-10-29). Builds on [ADR 0120](0120-mate-on-is-the-consent.md)
(turning Mate on is the consent to send anything to the model),
[ADR 0160](0160-mate-watches-readings.md) (the hand-off that starts a turn from
the server), [ADR 0224](0224-alarm-rules-live-in-the-database.md) and
[ADR 0225](0225-mate-proposes-alarm-rules.md) (a record type created by a
proposal, with its table in `helmcentral.sqlite`) and
[ADR 0172](0172-mate-tools-are-one-record-each.md) (the tool registry). It is
cycle 6a of the agent-first plan: item 6 of
`docs/research/agent-first-helmcentral.md`, without push notifications, which
are 6b.

## Context

Mate only ever answered a question. A skipper who wanted the boat looked at
every hour, or a summary at 06:00 while at anchor, had to ask each time. The
pieces to do it unattended already existed: a server-side turn launcher, a read
tool that reports the whole boat (`get_vessel_status`), and records that Mate
proposes and the operator applies. What was missing is a clock, a place for the
findings, and a limit on what an unattended run may spend.

## Decision

**A routine is a record.** The `routine` type (table `mate_routines`, beside the
alarm rules) holds a name, a cadence (`hourly` or `daily`), a boat-local
`daily_time` for a daily one, a free-text `focus` in the operator's words, and
an `enabled` flag. Mate creates, changes and deletes routines only through
`propose_changes`, so the operator's **Apply** tap is the consent to each one.
`list_routines` lets Mate read what exists first. At most 8 routines can exist,
which keeps the worst-case spend easy to reason about. The scheduler's own
columns (`last_run_at`, `last_outcome`, `last_note`, `last_finding_at`) are
written without touching `updated_at`, so a run never makes a waiting proposal
stale.

**Each routine owns a conversation, and it stays while the routine does.** Creating the routine also creates a Mate
conversation titled with its name, in the same transaction: the `conversations`
table is in the same database file, so both are saved or neither is, and there
is no half-created routine to repair. `conversations.routine_id` points back, so
the list can badge it and spend can be summed. Deleting the conversation is refused (409) while its routine exists, with
"This conversation belongs to the routine ... Delete the routine in Mate
settings first." Once the routine is deleted, its conversation, which keeps its
findings and its label, deletes like any other.

**Spend has its own ledger.** `mate_routine_runs` gets a row for every run that
reached the model: routine, start time, cost, whether the cost is known, and the
outcome. The row is written first in the same transaction as the routine's
last-run update, so a routine deleted mid-run still leaves its cost. The run's
cost comes from a meter carried on the run's context and fed as each OpenRouter
response and tool charge arrives, so a run that fails, times out or is stopped
after spending still counts. What a failed request cost depends on how far it
got:

- **Never reached the model:** a refused request (non-2xx), or one that failed to
  connect (the name did not resolve, connection refused, TLS handshake failed),
  cost nothing and is recorded as $0.
- **A response had started:** the stream was cut, the time ran out, the operator
  pressed Stop. The first chunk carries the generation's id, and the real cost
  is read from OpenRouter's generation endpoint when the run is recorded,
  retrying for about a minute because OpenRouter can lag. The same applies to
  `search_web` sub-requests, and a failed tool round still drains what its
  searches spent into the meter.
- **Nothing to look up:** a timeout before any response, a response with no
  generation id, a lookup that never succeeds, or a crash. Only then is the cost
  unknown, and unknown is never treated as $0: routines are paused for 24 hours
  from that run, each due run is skipped with "a run failed and its cost is not
  known, so routines are paused until <time>", and Settings says so.

If the ledger row cannot be saved, the scheduler stops every routine run until
Helmcentral restarts, logs it, and GET /api/assistant/routines reports
"Routines stopped: the run log could not be saved (...)". The same endpoint
reports "Routines are not running: ..." when the settings cannot be read (an
unreadable limit, for example), and the panel shows either. Neither leaves notes
on the routines themselves. The ledger is independent of the conversations, so
deleting one cannot lower the total. The 24-hour figure in Settings and the limit
the scheduler enforces both read it.

**One switch and one limit.** The consent is `assistant.routines_enabled`
("Let routines run on their own"), off by default. It extends ADR 0120: Mate
being on lets it answer; this lets it spend with nobody there. The limit,
`assistant.routines_daily_cap_usd`, defaults to $1.00 and must be between $0.05
and $20. Spend is the sum of the ledger over the last rolling 24 hours. A run
that would start at or over the limit is skipped. A stored limit that is not a
number between the minimum and maximum fails the settings read and stops the
scheduler, rather than quietly becoming $1.00. A key with no value counts as
missing and takes the default.

**The scheduler** (`startMateRoutines`) ticks each minute, runs only while the
switch is on, and runs one routine at a time in creation order. A routine is
re-read from the store just before it runs, so one switched off or deleted
during an earlier run is left alone. It is due
when it is hourly and 59 minutes have passed since its last run (a tick early,
so the scheduler's jitter does not push each run a minute later than the last), or daily and the
boat's clock has passed `daily_time` since its last run. An hourly routine that has
never run is due on the first tick after it is made, because the operator has
just asked for it. A daily routine runs at most once per boat-local day, and
only for a time of day after both its last run and its last schedule change
(`schedule_changed_at`, set when it is made and whenever its cadence or time
changes), so moving the time never makes it run twice in a day. A daily routine
that has never run counts from its creation, so it waits for its next time.
After downtime a due routine runs once. Missed runs are not made up.

**A skip is a result.** Whenever a due run cannot happen, the routine records
`skipped` and a plain reason, and the skip counts as that hour's or day's run, so
it is recorded once rather than every minute: Mate is not ready, the daily limit
is reached, no boat position for a daily routine (written only when its last run, or its
creation, is more than 26 hours old, which no time zone can explain, and at
most once a day; before that nothing is written and nothing runs. This note is
not a run: `last_run_at` is untouched, so the run still happens when the position
returns), the conversation was busy for
two minutes with the operator's own turn, or the turn failed. While the switch is
off nothing is recorded, because nothing was due to run.

**The run is a Mate turn.** The scheduler takes the routine's conversation
(waiting briefly for the operator's own turn, before writing anything), appends a
row with role `routine` holding the instruction (the name alone on the first line, which the chat shows as its label, then
schedule, boat time and focus), and starts the turn with the same launcher a posted question and the end
of a watch use. It waits for the run to release the conversation, then reads the
reply. The run is unattended: `propose_changes`, `start_watch` and the form tools
are not offered or dispatched, and a live prompt rule says no one is watching,
the first line is exactly "Nothing to report." or a one-line headline, then at
most three short findings with figures, as questions when the cause is unknown,
never an estimated cause, and that an active alarm is mentioned but not raised
again. The first line becomes `last_note`, and `last_finding_at` is set unless it
is "Nothing to report.". The settings and Mate's readiness are read once per tick and the boat's position
at most once, and every run in the tick uses them. The model reads only the
last three earlier runs of the conversation, plus the current instruction, as history, so an hourly routine does not pay for a day of its own
past each hour. The operator still sees all of it.

**Direct control without Mate.** The Mate settings section shows what routines
spent in the last 24 hours, lists the routines with their last run, and gives
each an on/off switch and Delete through `GET /api/assistant/routines`,
`PATCH` (only `enabled`) and `DELETE`. Rewording or adding a routine stays a Mate
proposal, so there is one writer of a routine's definition.

## Rejected

**Making up missed runs.** After a night with the box off, twelve hourly runs
would spend the day's limit at once to report on hours already gone. One run
reports the boat as it is.

**Reading a daily time as UTC when there is no boat position.** The skipper
means 06:00 on the boat, and UTC could be a day off by the date line. A routine
without a position is skipped, with the reason. (UTC decides only when to write
that note, once a day, never whether to run.)

**Letting a routine push or alert on its own judgement.** A model-written
"alarm" would compete with the deterministic alarms ([ADR 0160](0160-mate-watches-readings.md)
set the same line for watches). A routine says an alarm is active; it does not
raise one. Notifications are 6b, and will deliver a routine's finding, not
create alarm behaviour.

**A consent per routine or per feature.** One switch the operator can find and
turn off is easier to trust than eight. The per-routine on/off switch covers
pausing one, and the limit covers the money.

**Creating the conversation after the commit.** It would leave a window where
the routine exists with no place to write, and a failure there would need a
repair path. The shared database makes the single transaction possible, so it is
used.
