# ADR 0105: Mate Streams Its Answer

## Status

Accepted. Amends ADR 0093 §5 and its "Token streaming in v1" rejection
(both recorded token-by-token streaming as a deliberate follow-up, not
built there). Built on ADR 0104's React 19 / Tailwind v4 migration, which
this feature is the reason for.

## Context

Mate already streamed progress: `status` events while a round of tool calls
ran, then the whole finished reply as one `message` event. ADR 0093 §5
called this out plainly: "only whole-message and status events exist
today, and streaming the answer text itself is a follow-up", and named the
blocker: the OpenRouter client didn't consume a streamed response itself,
only a single JSON body.

A long answer (a two-anchorage comparison, a passage estimate with its
caveats) could take ten or more seconds of actual model output. For all of
that time the operator watched a status line ("Working out the answer…")
with no growing text under it, then the entire reply appeared at once. The
model was writing the whole time; the operator just couldn't see it.

Building this needed two things that didn't exist yet on the frontend side:
a React version shadcn's `MessageScroller` could actually mount on, and a
Tailwind version its generated CSS would compile under. Both landed first,
as their own commits, in ADR 0104.

## Decision

### The backend always streams, and rebuilds the same response shape

`openrouter_client.go`'s `openRouterChatCompletion` now always sets
`Stream: true` and parses OpenRouter's SSE body with a `bufio.Reader`
reading whole lines (never `bufio.Scanner`, whose 64 KB line cap a long
tool-call argument string could exceed). Content fragments and tool-call
argument fragments (keyed by `index`, since a model can stream several tool
calls in interleaved pieces) are accumulated exactly the way the previous
non-streaming path already assembled a full response, so every downstream
consumer of `openRouterChatResponse` (the tolerant `openRouterContent`/
`openRouterArguments` decoding, the cost summation) is unchanged. Streaming
changed how the bytes arrive, not the shape anything after the client sees.

A chunk carrying an `error` field, or the stream ending without `[DONE]` or
a `finish_reason`, fails the call outright rather than returning whatever
partial content had arrived, the same fail-fast rule every other upstream
read in this codebase follows.

### `delta` and `retract`, alongside the existing `status`/`message`/`error`

`assistant_run.go`'s `run` passes an `onContent` callback into the
completion call. On each content fragment, it holds back the trailing bytes
that could be the start of a still-unrecognised tool-call marker, emits a
`delta {"text": "..."}` event carrying only the safe prefix beyond what has
already been sent to the browser for the current round, stops emitting
once a marker is found, and flushes the held-back tail once the round ends
clean (the hold-back window is sized below).

A round's text is never forwarded blindly. ADR 0093 §2 already
established that text alongside tool calls is the model thinking aloud, not
the answer: a round that ends in tool calls was never going to keep its
streamed text as the final reply. `retract {}` is the event that makes that
visible: `run` emits it before that round's tool-call statuses, whenever
any `delta` was actually shown for the round, telling the browser to
discard the draft text and fall back to the ordinary status/tool-activity
display. A round with no tool calls (a final answer) never emits
`retract`; its accumulated text becomes the `message` event content
instead.

### The hold-back window, sized to the longest text tool-call marker

ADR 0103 added `assistantTextToolCallMarker`, a scan for the substrings
that mark a model's own text-based tool-call dialect (DeepSeek's
`<｜DSML｜`, Anthropic-style `<invoke name="`, and others) so that markup is
rejected as an answer rather than shown to the operator as garbled prose.
That rejection ran once, at the end of a round, against the whole
accumulated text; streaming breaks that, because by the time a marker is
recognisable in the accumulated text, some of its bytes may already have
been forwarded as earlier `delta` fragments.

`assistantSafeStreamLen` closes that gap: at every point, only
`len(text) - (assistantTextToolCallMarkerMaxLen - 1)` leading bytes of the
round's accumulated-so-far text are provably free of any marker, given that
the whole text has already been scanned and found clean up to that point.
The trailing `assistantTextToolCallMarkerMaxLen - 1` bytes (one byte short
of the longest known marker) stay held back, because they could be the
start of a marker still waiting on more input. `onContent` emits only that
provably-safe prefix on each fragment, and once
`assistantTextToolCallMarker` does find a marker in the accumulated text,
no further `delta` is emitted for the rest of that round at all; the
existing end-of-round rejection still runs and still fails the whole call.
The proof is in `assistantSafeStreamLen`'s own doc comment
(`assistant_run.go`); the practical effect is that ADR 0103's markup can
never reach the screen a fragment at a time, however OpenRouter happens to
split it across chunk boundaries.

### `message` stays authoritative; voice reads only from it

Nothing about what gets persisted or what the `message` event carries
changed. The stored row, and the reply the frontend resolves `chat.send()`
with, are still built from the round's final, complete content once the
loop is done, never from the accumulated draft. ADR 0094's read-aloud path
(`extractSpokenSummary`, called from `mate-sheet.tsx`'s `chat.send()`
resolution) reads that same final content exactly as before; it has no
visibility into `draft` at all, so a streamed fragment that happened to
contain something that looked like a `## Spoken summary` heading (possible
mid-stream, before the hold-back window or a later correction settles the
text) can never be read aloud ahead of the real answer.

### Frontend: a ref-buffered draft, flushed once per frame

`use-assistant-chat.ts` accumulates `delta` text into a ref
(`draftBufferRef`), not directly into React state, and schedules at most
one `requestAnimationFrame` callback to copy that buffer into a new
`draft: string | null` state value. Several `delta` events arriving within
one frame (which happens routinely, since OpenRouter's chunks are much
smaller than a frame's worth of reading time) collapse into one state
update and one markdown re-parse, rather than one of each per token.
`retract`, `message`, an `error` frame, `abort()`, and the start of a new
`send()` all clear the draft and cancel any pending animation frame the
same way, so a stale flush can never land after the state it would have
written to has already moved on.

`assistant-thread.tsx` renders the draft as its own assistant item (message
id `draft`) while it holds text, with no footer: there is no cost or
model figure to show until the real `message` arrives with one. Only the
status text (`Marker role="status"`, the spinning `Loader2` icon, and the
shimmering status line) hides while the draft is showing, since once real
answer text is on screen there is nothing left for that line to usefully
say. The marker itself and its Stop button stay on screen the whole time a
reply is in flight, because a streaming answer can still be cut off
mid-sentence. A `retract` clears the draft back to `null` while the run is
still going, and the status text reappears exactly as it would have if no
draft had streamed at all; the operator sees the same tool-activity status
they always did, just briefly preceded by text that turned out to be
thinking aloud rather than the answer.

### Live-captured fixtures

Per house rule ([[feedback_verify_fixtures_against_live_data]]), the SSE
decoding in `openrouter_client_test.go` is checked against real captured
traffic, not an assumed shape: `backend/testdata/openrouter_stream_text.txt`
and `openrouter_stream_toolcall.txt` are one streamed plain answer and one
streamed tool-call round, captured live against the operator's configured
model and OpenRouter key. The capture itself
(`openrouter_stream_fixtures_live_test.go`) is opt-in behind
`HELMCENTRAL_CAPTURE_OPENROUTER_FIXTURES=1` on top of the usual `-short`
skip, since unlike a read against a free OSM mirror, every run spends the
operator's real OpenRouter balance: `go test -short ./...`, this repo's
normal command, never runs it.

### The answer outlives the page

Streaming fixed the blank-panel problem: the operator now watches the
answer being written. It did nothing for a second problem, reported by the
operator in plain terms: ask a question, get bored waiting, switch to the
Forecast panel, come back, and the answer had stopped. Until this change,
`postAssistantMessageHandler` handed `c.Request().Context()` straight into
`assistantRunner.run`, so the moment the browser's fetch for that POST went
away, so did the OpenRouter completion it was driving. Navigating to another
panel unmounted the Mate thread, which aborted the fetch, which cancelled
the run.

An iPad locking its screen makes this worse than an ordinary navigation:
iOS can suspend or kill a backgrounded tab's network activity outright, and
no code running inside the page can prevent that or even find out about it
in time. If a reply's lifetime is tied to the fetch that asked for it,
whether it finishes at all becomes a race against how long the operator's
tab happens to stay alive. The fix has to put the run somewhere the browser
cannot reach: the server.

`assistant_run_registry.go` adds an in-memory registry, one `assistantRun`
per conversation with a reply in flight, replacing the old
`assistantRunsInFlight` sync.Map guard (which only ever answered "is one
already running", nothing about its progress). Each `assistantRun` holds an
append-only log of the same `status`/`delta`/`retract`/`message`/`error`
events the SSE stream already carried, a `done` flag, and a `changed`
channel that is closed and replaced on every append. A subscriber reads
from wherever it left off in the log and waits on `changed` for the next
entry. Nothing about appending to that log ever touches a subscriber's
connection, so a slow or abandoned subscriber can only ever block its own
read; it can never slow down, or lock up, the goroutine actually driving
the run.

`postAssistantMessageHandler` now starts that goroutine against
`context.Background()`, not the request's own context. `assistantRunTimeout`
still bounds the run, applied exactly where it always was, inside
`assistant_run.go`'s `run`. The handler subscribes to the run's log from
the start and streams it to the client the same way it always did; if that
client goes away mid-reply, only the handler's own subscription ends. The
goroutine keeps writing to the log, and the assistant row is still
persisted once the model finishes, in the same order as before: the row is
saved, then the `message` event is appended, so a page that finds no run in
flight can still read the finished answer straight off `GET` the
conversation.

`GET /api/assistant/conversations/:id/run` is how a page picks a reply back
up: replay whatever is already in the log, then keep streaming live events
the same way, or answer 204 when nothing is running. It sends the same
`: keepalive` comment every 15 seconds that `logsStreamHandler` already
does, so a long tool round sitting behind a proxy that buffers idle
connections does not look dead.

Stop stopped meaning "stop" once an aborted fetch no longer touched the
server at all. `POST /api/assistant/conversations/:id/run/cancel` is the
actual thing now: it cancels the run's own context, the run ends with no
assistant row persisted (the same outcome Stop always produced), and every
subscriber, the tab that pressed Stop and any other tab watching the same
conversation, sees a plain `error` event saying the run was stopped, rather
than whatever raw cancellation error happens to fall out of the OpenRouter
client underneath it. It answers 204 whether or not a run was actually in
flight, so a double click, or a Stop that lands just as the reply was
already finishing on its own, does nothing surprising either way.

The cancel answers only once the conversation is free. A run can take a
moment to unwind its OpenRouter call after its context is cancelled, and
the conversation stays locked until it has, so a cancel that answered
straight away let the operator ask again into a 409 and lose the question.
The page clears the status line and draft the moment Stop is pressed, but
keeps the composer locked until the cancel has answered, and shows the
error if the cancel itself fails.

Switching threads while one is still answering leaves that answer running
on the server. The thread rejoins the newly opened conversation, which
closes the other conversation's local stream and clears its status and
draft, so the new thread never shows another conversation's run and Stop
only ever cancels the run on screen. A reply that lands after the operator
has moved on is not written into the thread they moved to; it is already
stored, and it shows when they go back.

This reverses a rule ADR 0093 shipped without ever stating it as a
decision: because the run had no context to execute on besides the
request's own, a client disconnect cancelled the run, simply because
nothing else was there to keep it going. That was never a deliberate
choice, only what fell out of not yet needing anything more than a single
HTTP response. It stopped being good enough the day an operator asked a
real question, walked away, and came back to find it had given up.

A reply that finishes off screen is only half fixed by keeping the run
alive; the operator still has to go looking for it. `use-assistant-chat.ts`'s
`send` registers the conversation it just posted to in a small module-level
store (`lib/mate-watch-store.ts`), keyed off nothing but the conversation id
and the send time - deliberately outside React state, so the hook can call
it without threading a setter down through every caller. A single watcher
hook, mounted once in `App.tsx` and never on the kiosk route, opens
`GET .../run` for whatever watched conversation isn't the one on screen: the
Mate panel's active thread while the panel is showing, or the sheet's while
it's open. A `message` event toasts "Mate answered" with the conversation's
title and an Open action that navigates to `/mate/<id>`; an `error` event
toasts "Mate couldn't answer" with the error's first line and the same
action, except `"stopped"` (the Stop button's own event, per the section
above), which the operator caused on purpose and gets no toast for. A 204 -
the run had already finished before the watcher attached - falls back to
reading the conversation directly: a newest message that is an assistant
reply newer than the question's send time toasts the same way; anything
older is dropped without one. Either way the conversation comes off the
watch list once handled, and a conversation that becomes the one on screen
while its stream is still open has that stream closed with no toast instead,
since the thread itself is about to show whatever it was going to say. The
toast is the plain Sonner one already used elsewhere in the shell: default
duration, no alarm styling, and never read aloud.

## Consequences

- The dashboard's entry chunk is unaffected: Mate's thread, its markdown
  renderer, and the new `@shadcn/react`-based components all stay behind
  the same lazy imports `markdown-lazy.test.tsx` and
  `check-entry-chunk.mjs` already guarded, since the kiosk never mounts
  Mate at all.
- A reply's total cost, token count and tool-round footer are unchanged in
  shape and timing: they still only exist once `message` arrives, exactly
  as before this ADR.
- The status text and the streamed draft are mutually exclusive on screen,
  but the Stop button is not: a round that flips from streaming text back
  to tool activity (a retract) briefly shows the marker with just the Stop
  button and no status line yet, rather than a cross-fade between the two;
  this was accepted as simple and correct over a more elaborate transition
  that wasn't asked for.
- `openRouterChatCompletion`'s signature changed (it now takes an
  `onContent func(string)` callback); its only caller was already
  `assistant_run.go`, so no other package needed updating.

## Related

- ADR 0093 (onboard assistant over OpenRouter): §2's "thinking aloud"
  reasoning, which `retract` implements for the streamed case; §5, which
  this ADR amends.
- ADR 0094 (Mate voice and app-wide help): the read-aloud path this ADR
  confirms still reads only from the authoritative `message`, never the
  draft.
- ADR 0103 (a dead tool must not eat the answer): the text tool-call
  markers and the end-of-round rejection this ADR's hold-back window keeps
  a fragment of that markup from ever reaching the screen.
- ADR 0104 (React 19 and Tailwind v4): the toolchain migration this feature
  needed before `MessageScroller`/`Message`/`Bubble`/`Marker` could be
  added at all.
