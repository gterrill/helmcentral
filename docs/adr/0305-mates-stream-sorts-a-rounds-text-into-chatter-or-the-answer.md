# ADR 0305: Mate's Stream Sorts A Round's Text Into Chatter Or The Answer

## Status

Accepted (2026-11-24). Replaces §2's rule in
[ADR 0093](0093-onboard-assistant-over-openrouter.md) that a tool failure is
shown as a status line, and replaces the `status` and `retract` events of
[ADR 0105](0105-mate-streams-its-answer.md), keeping its hold-back window.
Amends the event set of [ADR 0189](0189-mates-stream-is-one-typed-event-set.md).
Streams what [ADR 0240](0240-mate-runs-on-progress-not-a-round-count.md)'s trace
now stores. Builds the contract the activity view reads; the view itself comes
in a later change, and until then the thread reads the new events as it read the
old.

## Context

Until now a Mate round put every word it wrote on the stream as `delta` text. A
round that went on to ask for tools had that text taken back with `retract`
(ADR 0105). On the screen it looked like this: "Let me check the tide at the
bar" appears in the answer, stays for a second, and vanishes when the lookup
starts. The words were never the answer, and the operator watched them arrive as
if they were.

Two other things were thin. `status` carried a line of text and no round or
time, so nothing could say how long a lookup took or what came before it. And
`retract` said that text was gone without saying why, or where it went.

The run now records what the model thought and said while it worked: each
round's visible reasoning, its chatter, the triggers that sent a draft back, and
how long the run went before the answer began. The stream has to carry the same
things, live, with times, so the activity view can show a run as steps.

## Decision

**One round is one `round` event, then its output, then `round_end`.** The
events, each with a Go struct, a golden frame and a decoder:

- `round {round, at_ms, final}` opens a round. `final` marks the round that has
  the tools withdrawn to get an answer.
- `reasoning {round, text}` carries the reasoning the model chose to show, up
  to 32 KiB a round, the same cap the stored copy has. Past it the stream sends
  no more, and `round_end` says how much was written.
- `chatter {round, text}` carries text written alongside tool calls.
- `lookup {round, seq, at_ms, text, subject, skipped}` says a tool call is
  starting. `text` is the operator's wording (`describeAssistantToolCall`),
  never a tool's name; `subject` is what the call looks at, with no verb
  (`describeAssistantToolSubject`), for a line that lists several lookups. A
  call that was not run says why in `skipped`, `"withheld"` or `"round_full"`,
  and gets no `lookup_done`.
- `lookup_done {round, seq, at_ms, outcome, reason}` closes a lookup that ran.
  Every lookup announced without `skipped` gets one before its round ends.
  `outcome` is `"done"`, `"failed"`, `"cut"` or `"stopped"`, worked out in one
  place (`assistantCallOutcome`) from the error the call is recorded with.
  `reason` is the failure's one-line cause without the tool's name, and empty
  for every other outcome; it is where ADR 0093's failure line went. A call
  abandoned when the tool phase ended is `"cut"`, not a failure, and so is a
  call that stopped with a context error because the tool phase's own time ran
  out. A call stopped by the operator is `"stopped"`. A saved run's calls carry
  the same `outcome` and `reason`, plus `text` and `subject`. The `outcome` is
  stored when the run is saved; the rest is worked out when the run is read.
  (Amended 2026-11-26: a cut or stopped call is recorded with its exact
  sentinel text, so a tool's own error that ends in a context error, such as
  its HTTP client timing out, is `"failed"` with that cause. Only a call saved
  before outcomes were stored, which has none, is still sorted by that ending.)
- `answer_start {round, at_ms}` says the round's text is the answer. Its `at_ms`
  is when the text began, so it equals the trace's thought time.
- `delta {text}` keeps its shape and bytes and now carries answer text only.
- `answer_withdrawn {round, at_ms, reason}` says text already shown as the
  answer is chatter after all. `reason` is `"lookups"` when the round went on to
  ask for tools, `"cut_off"` when the tool phase ended while it wrote, and
  `"failed"` when the round's completion failed or its text could not be used.
- `draft_sent_back {round, at_ms, triggers, draft}` says the draft failed
  Helmcentral's check. `draft` is the whole of it, including the bytes the
  stream was still holding back, so the browser can show it as a step.
- `round_end {round, at_ms, reasoning, reasoning_bytes, prompt_tokens,
  completion_tokens, cached_tokens, cost_usd}` closes the round. `reasoning` is
  `"text"`, `"private"` (the model thought but shares nothing readable) or
  `"none"`, and is empty for a round that failed before the model's output
  began. `reasoning_bytes` is the size of all the reasoning text the model
  wrote, which is more than the `reasoning` events carried when it passed the cap.
- `clock {at_ms}` opens every subscription; see below.

`message` gains `run {id, thought_ms, lookups}`, naming the stored trace of the
answer, and `error` gains `run_id` for a failed run. Both are absent when the
trace could not be stored. `status` and `retract` are gone, and the decoder
rejects them as it rejects any unknown event.

**A round's text is held, then sorted into a lane.** The text of a round waits
until the round shows what it is:

- It is chatter when the model begins a tool call. Whatever was held goes out as
  `chatter`, and so does the rest.
- It is the answer when it is promoted: as soon as there is something to show
  on the final round, which has no tools to call; when the held text reaches a
  paragraph break or about 400 bytes with no tool call begun; or at the round's
  end when no tools were asked for and the draft was accepted. `answer_start`
  goes out first, then the held text as `delta`. Whatever the rule, a round is
  promoted only once some of its text has cleared the hold-back window below
  and has something in it besides blanks, so an answer never opens with nothing
  to show. A final round that writes only the start of a text tool-call marker
  is not promoted, and so has nothing to withdraw when the marker completes.
- A round that was chatter and then turns out to be the accepted answer (a tool
  call began and the round still ended with none to run) opens the answer afresh.
  `answer_start` goes out and the whole text follows as `delta`, from its first
  byte. The chatter already sent stays in the timeline as chatter.
- A promoted answer stops streaming when a tool call begins. The rest of the
  round's text is held and goes out as chatter when the round ends asking for
  tools.

Every lane sends only what `assistantSafeStreamLen` has cleared, so the
hold-back window of ADR 0105 keeps the start of a text tool-call marker out of
chatter and answer alike. Once a marker turns up in a round's text, no more of
it streams; an answer that only quotes one goes out whole when the round ends
clean, as before.

**Taking text back names where it goes.**

- A round that was promoted and then asked for tools sends `answer_withdrawn`
  with `"lookups"`, followed by a `chatter` event holding the part the stream was
  still holding. The browser already has the rest, and moves it into the round's
  chatter. A round that was never promoted sends no withdrawal: its text was
  chatter all along.
- A round cut off by the tool phase ending does the same with `"cut_off"`, or
  files its text as chatter if it was not promoted.
- A round that fails, mid-stream or by writing a tool call as text, does the same
  with `"failed"`. The text it held goes out as chatter, up to any tool-call
  marker, and the trace records the same text, so the two agree.
- A final round that files its result with the submit tool asked for a tool, so
  its text is not an answer. It settles as any round that asked for tools does,
  with `"lookups"`, whether the filing worked or not.
- A draft that fails the check sends `draft_sent_back` whether or not any of it
  was shown. It is sent back once, so a run has at most one. The trace keeps the
  whole draft, and records as chatter any part of it that already went out as
  chatter.

**Reasoning and chatter arrive coalesced, with no timers.** A fragment joins the
event waiting for it. The event goes out when the next fragment arrives more
than 100 ms after the waiting one began, before any event of another kind, and
at the round's end. A reasoning stream of hundreds of tokens becomes a handful
of events, and nothing wakes on a clock.

**Every `at_ms` counts from the run's start, and each subscriber is told where
the server is.** The start is taken just before the runner's first request to the
model, not when the question arrived, so the time is the model's own work. Time
spent before that, setting up the question, is in no `at_ms`, no offset in the
trace and no thought time. `streamAssistantRun` writes a `clock {at_ms}` ahead of
the first event on every subscription, so a browser that joins late or reloads
can place each time on its own clock. The browser takes its zero when the clock
arrives, so a subscriber waits only until the first of three things: the run has
started, the log holds an event, or the run is done. A subscriber who attaches
earlier gets the clock when the start is set, before the round event, the first
to carry an `at_ms`. A run that ends without ever starting, or whose log already
holds an event before a start, sends a clock of 0 (the runner appends nothing
before it has taken the start in a live run). The clock is
built for each subscriber and never stored in the run's log, so two subscribers
read identical events under their own clocks. The trace takes the same instant,
so the two never disagree.

**An unattended review sends nothing.** A watch or alarm review has nobody
reading its stream, and its trace is not stored. Its runner emits no events at
all, so its reasoning and chatter cannot reach a stream by any route.

**The thread reads the new events as it read the old.** Until the activity view
exists, a `lookup`'s text is the status line, a `round` clears it so the waiting
phrase shows, `delta` builds the draft, and `answer_withdrawn` and
`draft_sent_back` remove the draft. The line shows only the lookup Mate is doing
now: a skipped lookup does not change it. A `lookup_done`, whether the lookup
succeeded, failed or was cut off, takes that lookup off the line, which then
shows the latest lookup still running, or the waiting phrase when none is. The
activity view is where failures, skips and cuts show. `reasoning`, `chatter`,
`answer_start`, `round_end` and `clock` are decoded and set aside. The answer watcher that raises a toast
ignores every progress event.

## What the operator does not see

The reasoning and chatter on the stream are the same text the trace stores, and
the trace is not part of the conversation. Neither reaches the messages table or
the history the next turn sends. A model's private reasoning, an encrypted block
it keeps to itself, never reaches the stream; the round says only that the model
thought.

## Alternatives considered

- **Streaming every word as the answer and taking it back, as before.** It is
  the flicker this ADR exists to remove. The operator chose to hold the first
  paragraph or about 400 bytes instead, accepting that the answer starts a
  moment later.
- **A timer to flush coalesced text.** It would add a goroutine per round and an
  ordering problem against the events that follow. Flushing on the next
  fragment, the next event and the round's end bounds the delay by the stream's
  own cadence.
- **Carrying the withdrawn text in `answer_withdrawn`.** The browser already
  holds up to 400 bytes or more of it. Resending it would double the bytes for a
  rare event; only the held tail travels, as chatter.
- **Keeping `status` for lookups beside the new events.** Two ways to say one
  thing would drift. `lookup` and `lookup_done` carry the round, the sequence
  and the time `status` never had.

## Consequences

- The first words of an answer appear after the first paragraph or about 400
  bytes, not at the first token. A short answer appears whole when its round
  ends. The final round streams at once.
- A model that opens with a long preamble and then calls a tool still shows
  that preamble as the answer for a moment before `answer_withdrawn` moves it.
  It is rarer than the old flicker, since only text past the hold is shown, and
  the withdrawal says why.
- The run's replay log holds reasoning and chatter for as long as the run is in
  flight (ADR 0105). A long reasoning stream makes the log larger than before;
  the log is dropped when the run ends.
- Server and browser must be deployed together. A page open across the upgrade
  meets an event it does not know and shows that the reply couldn't be shown,
  with the stored answer there after a reload (ADR 0189).
- Adding an event still means a Go type, a golden frame, a union member and a
  decoder case. The golden test now fails when a frame has no decoder test.
