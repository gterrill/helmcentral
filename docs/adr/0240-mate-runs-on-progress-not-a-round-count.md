# 0240. Mate runs on progress, not a round count

Status: accepted

Amends 0197: an answer's tool calls are stored with its run, not in
`message_tool_calls`.

## Context

Mate's loop stopped after a fixed eight rounds of tool calls and gave the whole
reply a single three-minute clock. A question about a three-leg passage spent
its rounds on place searches, route estimates, wind and tides, reached the cap
at 2 minutes 13 seconds, and was forced into a final answer with no tools. That
last completion was still streaming when the three minutes ran out. The run
failed with `read openrouter stream: context deadline exceeded`, nothing was
stored, and the conversation showed only the question. The one record was a log
line, because tool calls are kept only for runs that produce an answer.

Two things were wrong. A passage with several legs needs more than eight rounds,
so the cap was too small for the work. And the final answer inherited whatever
time the lookups left, so a run that used its time on lookups could not finish
writing.

## Decision

**The budget is progress.** A round counts as progress when it makes at least one
call that is new to the run (the same tool with the same arguments, compared as
canonical JSON, has not been made before) and that call succeeds. A retry of a
failed call changes nothing and is not new. Two rounds in a row without progress
is a stall, and the loop withdraws the tools and asks for the answer.

**Two ceilings remain** as a safety net against a model that invents new
arguments for ever: 30 rounds, and 10 minutes for the tool phase. The field run
took about 17 seconds a round, so 30 rounds is about 8.5 minutes and the clock
covers a slow provider. The per-tool failure limit and the per-round call limit
are unchanged.

**The final answer has its own clock.** When any of the three limits ends the
tool phase, the tools are withdrawn and the final completion starts a fresh
2 minutes (capped by the per-completion timeout). The tool phase runs on its own
context with a 10 minute deadline, so no tool round starts after it, and a round
in flight when it ends is abandoned and recorded. The final answer derives from
the caller's context, so cancelling still stops everything at once. The longest a
run can last is 14 minutes: the 10 minute tool phase, 2 minutes for a round that was already writing when it ended, and 2 minutes for the forced final answer. A model's natural answer that is already streaming
text when the tool deadline passes is not cut off and redone: its completion
runs on to the tool deadline plus the final reserve. A round that has not
started on text at the deadline is abandoned, and one that began text and then
asked for tools has its text withdrawn and its tools left unrun. The lookups
sent to a forced final say why they stopped (nothing new, the round ceiling or
the time ceiling), not that tool calls ran out.

**Every run is recorded.** `assistant_runs`, `assistant_run_rounds` and
`assistant_run_calls` hold, per run, the question it followed, the answer (null
when none), the model, start time, duration, stop reason (answered, stalled,
round ceiling, time ceiling, cancelled, error), the error and the budget it had.
Per round they hold the start offset, completion latency, tokens, cost, whether
it was the forced final and whether it made progress. Per call they hold the
name, the full arguments and result (each cut at 64 kB), duration, result size,
error and, for `find_places`, whether the place search cache answered. A tool
reports the cache outcome through its result, not through the context.

The run owns its tool calls. They used to live in `message_tool_calls`, which
belongs to an answer message and cannot exist for a run with no answer. That
table is gone: an answer's calls are found by joining `assistant_runs` on
`answer_message_id`, so feedback, ratings and review read the same rows the run
details do. The run details show a call's arguments cut short and leave its
result out; the stored call keeps both.

One pruner applies the retention: a failed run is kept 30 days, an answered
unrated run 7 days, and a run behind a rated answer for as long as the rating
stands. The failed run is the only record of what went wrong and is shown to the
operator, so it outlives the rest.

**The conversation returns the runs.** The conversation lists a run with no
answer in full and an answered run by its id and answer only. The thread merges
the two into one timeline and shows a run with no answer as a plain row after its
question, never as an assistant message. The row carries the headline only ("it
ran out of time after 11 rounds"); the raw error is in the run's details, and a
run the operator stopped keeps its quiet row. Every run offers a Run details view
that reads its full trace on demand. The words for how a run stopped live in the
frontend; the server sends the stop reason.

**A setting turns on a verbose log.** `assistant.verbose_run_log` writes one line
per round (elapsed, latency, tokens, tool count, progress, stalls, time left) and
a stop-reason line at the end of every run. A failed run always logs its stop
reason and duration. It is a stored setting, not an environment variable.

**How a run is classed.** A deadline error is a time ceiling only when the tool
phase or the final reserve has actually run out. One reply that exceeds the
per-completion timeout first is an error ("a reply from the model took too
long"). A panic is recorded as an error with the crash text, never as answered.
A call refused for the per-round call cap did not run, so asking for it again is
new. When the tool phase ends mid-round, the calls that finished are kept for
the final answer, with the cards and proposals they produced. Reading a thread
whose run the operator has just stopped waits for that run to store its trace.
An answer under 7 days old with no run on record can still be rated; it has no
lookups to show. An answered run is pruned by the age of its answer, the same age the rating rule uses.
On first open, tool calls that earlier versions kept in `message_tool_calls` are
moved into runs (one per answer, the legacy limits as its budget) before that
table is dropped, so no recorded call is lost and no entry is needed in the
release notes.

**Unattended reviews can still file.** The forced final answer of a watch or
alarm review keeps its submit tool and no other, so a review that stalled files
with what it has. An alarm review's outer deadline is the unattended budget's
whole ceiling, so the final reserve happens. Each stored round says what it was
(lookups, a draft sent back, the answer, the final answer, cut off by time,
failed), a run's round count means rounds of lookups, and a round's cost
includes what its tools spent, so the rounds add up to the answer's cost. When
the tool phase ends by time the stop reason is the time ceiling, whatever else
is also true.

**Further rules.** The round ceiling, the draft-check guard and the logs count
rounds of lookups, so a correction or a round cut off by time does not use one
up. A round that is still running when the tool phase ends is abandoned: the
run stops waiting for it, keeps the calls that had finished, and discards what
a straggler returns later. A review's deadline starts after its input is built.
In the forced final a submit that failed or was withheld is not filed: the model
gets one more turn with the error, and if that fails the run fails with the
submit error. An answer whose run was cleared by the retention rule is marked, and
only a marked answer is refused a rating; a failed run is taken from the model
that answered its rounds. A run recorded by an earlier version shows only the
limits it had.

## Rejected

- **Classify the task up front and give each class a budget.** It needs a second
  model call or a heuristic on every question, and the class is a guess about
  work the model has not started. Progress is observed, not predicted.
- **Let the model ask for more rounds.** The model that is stuck is the one that
  would ask. It moves the decision to the party least able to make it.
- **Raise the fixed cap.** A larger number only moves where the next long
  question fails, and a stalled loop would then burn all of it.

## Consequences

- Watch and alarm reviews use the same runner with nobody waiting on them, so
  they get a tighter budget through the runner options: 8 rounds and a 3 minute
  tool phase, with the same stall rule and final reserve. They have no
  conversation and store no trace; the verbose log applies to them.
- The end-of-watch follow-up waits for three runs at the new ceiling, about
  44 minutes at most.
- A run that fails after its answer was stored (the trace could not be saved) logs
  the failure and keeps the answer.
