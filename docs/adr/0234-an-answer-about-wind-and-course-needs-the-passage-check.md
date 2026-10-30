# ADR 0234: An Answer About Wind And Course Needs The Passage Check

## Status

Accepted (2026-10-31). Builds on ADR 0191's `assess_passage_window` and leaves
its tool and card unchanged.

## Context

On 2026-10-31 the operator planned a southbound trip with legs on 140°, 152° and
173° true and asked which days to go. Mate (deepseek-v4-pro) answered three
times without calling `assess_passage_window`, though the prompt says to call it
for exactly this question. The operator rated the last answer down. It had:

- Called a northerly on a 152° course a "bow-quarter" wind and a "head sea". The
  true wind angle was 152°, which is astern.
- Told the operator to set passage limits in Settings → Mate. They were set:
  ahead 15 kn and 1.3 m, abeam 22 and 2, astern 30 and 3, daylight arrival on.
  Mate copied the sentence from an earlier turn.
- Said "the find_places hit agrees" and quoted fresh leg distances in a turn with
  no tool call at all.

Each of these needs a model to do something it was already told to do. The
prompt rule for the go/no-go card sits in the stable prompt and was ignored.

## Decision

### The run loop sends back an unchecked answer, once

When the model returns its final text, `assistantRunner.run` asks any tool
executor that implements an optional draft-check hook (`checkDraft`) whether the
text is acceptable, given every tool call made so far this run. The passage
guard implements that hook on the tool dependencies; the loop knows nothing
about passages. The guard is off unless `assess_passage_window` is on offer. It
sends the text back when it:

- tells the operator to set passage limits, or says they aren't set, while the
  limits are set (read fresh from settings) and `assess_passage_window` was not
  called, or
- describes the wind or sea relative to the course (head sea, on the bow,
  bow-quarter, on the beam, on the quarter, following sea, astern, dead run)
  when neither `assess_passage_window` nor `get_wind_forecast` with a
  `course_deg` was called. The forecast tool's `rel_wind` and `rel_wave` labels
  are an accepted source of the angle. A failed call counts as a call.

Both tests are case-insensitive phrase matches, kept simple on purpose. The
relative-wind test applies sentence by sentence, and a sentence counts only if it
also names wind or sea (wind, breeze, sea, swell, wave, chop, gust, or a word
like northerly), so "motor astern to set the hook" and "the target is on your
beam" pass. "Following winds/seas" counts only as "a following sea" or "have
following seas", not as a list introduction. A false positive costs one extra
round. Limits that are unset make the first claim true,
so it doesn't fire. Unreadable limits skip that check and log why.

The draft goes back to the model with a user turn labelled as Helmcentral's own
check, not the operator's words. A system message would match the forced final
round's guidance, but that one is folded into the opening system prompt. Nothing
else puts a system message mid-conversation, and some providers reject one after
an assistant turn or move it to the top. There is one message per trigger: only the limits-claim one says the limits are
set, since that trigger fires only when they are, and the relative-wind one says
nothing about them. The message tells the model to call `assess_passage_window`
and judge by its sectors when the question is whether or when to go, and
otherwise to take the angle from `get_wind_forecast`'s `rel_wind` and `rel_wave`
with `course_deg`, or drop the statements.

- The guard fires at most once per run and counts toward the round cap. It never
  fires on the forced final round.
- If the second answer still fails the check, the run accepts it and logs which
  trigger it still had.
- If no round is left to send the draft back in, the run accepts it and logs
  that the round cap was reached.
- Text already streamed to the browser is withdrawn with the existing `retract`
  event, the same one a tool round uses.

### The live prompt states whether the limits are set

The live part of the prompt gets one line, read fresh each turn: "Passage
limits: set (ahead, abeam, astern have a no-go wind)" or "Passage limits: not
set". Unreadable settings are said in the line, never read as unset. It sits in
the live part because the operator can change the limits between two questions,
and the stable part is cached.

### The prompt names the three failures

Next to the go/no-go rule, three sentences: say whether the limits are set only
from the live Passage limits line or the tool this turn, never an earlier
answer, don't give a sector that `assess_passage_window` or the forecast's
relative labels didn't, and don't cite a tool result from a tool not called this turn. The guard enforces the first
two. Nothing enforces the third yet.

### The incident is the first saved Mate case

`backend/testdata/mate_cases/southbound-northerly-no-passage-check.json` holds
the question, the bad answer and why it was wrong. No replay runner reads it
yet. The guard's tests use its text.

## Alternatives rejected

- **Prompt wording alone.** The rule was already in the prompt and the model
  ignored it. More wording is the same request again.
- **Forcing `tool_choice` when the question looks like a passage question.**
  Keyword matching on the question is brittle, and it would force the tool on
  questions about a mooring or a battery that mention a day.
- **Computing the relative wind in every forecast tool.** It gives the model the
  right angle, and the model can still ignore it and do its own arithmetic.
- **Blocking the answer and showing an error.** The operator gets nothing, when
  one more round usually gets the check.

## Consequences

- A guarded answer costs one more model round, and only when the draft
  triggered. Answers that don't mention wind angles or passage limits are
  untouched.
- The phrase list will miss some wordings and catch some innocent ones. An
  innocent hit makes the model answer again, and no more.
- The guard sees only this run's tool calls. An answer that cites an earlier
  turn's check passes only if the tool ran again this turn, which the
  correction asks for.
- The third claim, citing tools that weren't called, is still only a prompt
  rule.
