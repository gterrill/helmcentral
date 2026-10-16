# ADR 0189: Mate's stream is one typed event set

## Status

Accepted (2026-10-17). Builds on
[ADR 0093](0093-onboard-assistant-over-openrouter.md),
[ADR 0105](0105-mate-streams-its-answer.md) and
[ADR 0183](0183-one-mate-session-on-the-client.md). The wire bytes and the
event names are unchanged.

## Context

A Mate run reaches the browser as server-sent events: `status`, `delta`,
`retract`, `message` and `error`. The set existed only by convention. On the
Go side the runner emitted `(event string, payload any)` through four small
builders returning `map[string]string`, and the handler marshalled `message`
and `error` by hand with their own anonymous maps. On the browser side
`use-assistant-chat.ts` and `use-mate-answer-watcher.ts` each ran an
`if/else` over event names and cast `JSON.parse` output with `as`.

Nothing tied the two ends together. A renamed field or a new event compiled on
both sides, and the browser ignored whatever it did not recognise: an event
added on the server was dropped without a trace, and a payload of the wrong
shape produced an `undefined` somewhere downstream.

## Decision

**The set is closed, as structs on the Go side.** `mateEvent` is an interface
with a marker method, implemented by six types: `mateStatusEvent`,
`mateWaitingEvent` (also a `status` on the wire), `mateDeltaEvent`,
`mateRetractEvent`, `mateMessageEvent` and `mateErrorEvent`. The emitter and
the run log take a `mateEvent`, never a name and an `any`. Everything lives in
package `main`, so the language does not stop another type implementing the
marker; the set is closed by convention, enforced by the fixture test (every
shape has a golden frame) and by the browser decoder, which rejects anything
outside the set. A status carries either `text` (always present, even when
empty) or `kind: "waiting"`, never both, which is why they are two types. The
JSON tags and field order reproduce the bytes the maps produced.

**Golden fixtures are the contract between the languages.**
`backend/testdata/mate_events/` holds one SSE frame per shape: `status_text`,
`status_waiting`, `delta`, `retract`, `message` and `error`. Go owns the bytes:
its test builds each event, pushes it through the run log and the stream
writer, and compares the output with the file; `go test -run TestMateEventFixtures -update`
rewrites them. The `message` fixture is built from a real stored reply that
carries a form draft, trimmed, so the optional fields are present.
`message_source.json` is that reply. The frontend test decodes the same files,
so a change on one side that the other cannot read fails a test rather than a
reply.

**The browser decodes in one place and fails fast.** `lib/mate-events.ts`
exports the `MateEvent` union and `readMateEvents`, which wraps the existing
SSE framing and validates each frame. It throws `MateEventError`, carrying the
event name and the reason, for an unknown event name, invalid JSON, a field of
the wrong type, a status with both `text` and `kind` or with neither, and a
`message` whose `message` or `conversation` is not an object with a string
`id`. Both hooks switch on `event.type` with an exhaustive `never` check, so a
new variant fails the type check until it is handled. On a `MateEventError` the
chat hook logs the detail, shows a plain sentence in its existing error state
and clears the draft; the background answer watcher logs it and raises an
error toast with a Reload action. Both say the reply could not be shown and
that reloading will show it, because the server run carries on and stores the
answer; a decode failure never cancels it. Skipping an unknown event would have hidden exactly the breakage
this change is meant to expose. Aborts are not errors and are not caught
here. If the callback throws, the reader cancels the underlying stream.

**Validation of `message` stops at the top level.** The reply and the
conversation are checked to be objects with a string `id`. Their deeper shape
stays with `mapMessage` and `mapConversation`, which already own it and change
whenever a message gains a field; repeating that shape in the decoder would
give it two places to drift.

## Alternatives considered

- **Adopting TanStack AI or AG-UI wholesale.** The agentic loop and its tools
  stay on the server in Go, which is the point of Mate being on the boat; a
  TypeScript client library would model a server it does not talk to. The
  library is 0.x and has broken its wire format between releases. `retract` and
  the `waiting` status have no AG-UI equivalent, so they would travel as custom
  events anyway.
- **Generating the TypeScript union from the Go structs.** Five events do not
  justify a generator or a schema file; the fixtures check the same thing.

## Consequences

- Adding an event means a Go type, a fixture, a `MateEvent` variant and a
  decoder case; the exhaustive switches show every consumer that needs to
  handle it.
- When Mate gains structured tool calls on the stream, the new events take
  AG-UI's names (for example `TOOL_CALL_START`) so a later move to that
  protocol is a rename of fields, not a redesign.
- A server that sends an event this client does not know now produces a
  "couldn't be shown, reload to see it" message on an out-of-date open page,
  where it used to produce a silent gap. The stored answer is there after the
  reload.
- Wire output is unchanged, so no stored state or setting is affected.
