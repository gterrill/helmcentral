# ADR 0180: One Mate session on the client

## Status

Accepted (2026-10-15). Builds on
[ADR 0093](0093-onboard-assistant-over-openrouter.md),
[ADR 0105](0105-mate-streams-its-answer.md) and
[ADR 0173](0173-one-query-module-under-the-resource-hooks.md); leaves the
server side of Mate unchanged.

## Context

The client asked Mate a question in two places. `AssistantThread`, the thread
and composer that the Mate panel and the Mate sheet both render
([ADR 0104](0104-react-19-and-tailwind-v4.md) rebuilt it on the shadcn chat
components), owned the composer's send: create the conversation on the first
question, append an optimistic question bubble, stream the answer, append the
reply only if the conversation was still open, refresh the list. `MateSheet`
carried a second copy of the same sequence for the question it is opened with
(voice, Help, Settings, a text selection). The thread also owned rejoining an
answer still being written ([ADR 0105](0105-mate-streams-its-answer.md)), on
opening a conversation and when a watch ended
([ADR 0160](0160-mate-runs-timed-watches.md)).

Both took two hook results, `useAssistantConversations` and
`useAssistantChat`, and the thread made about fifty calls across them. Every
caller had to know the protocol between the two hooks, and the two copies had
drifted:

- The sheet's copy never checked whether an answer was already streaming. A
  second question superseded the first stream locally and usually met the
  server's 409, shown as the server's engineering text.
- The sheet sent every question with `spoken: true`, so a question from Help or
  a text selection got an answer written to be read aloud.
- The screen context ([ADR 0093](0093-onboard-assistant-over-openrouter.md)
  voice phase, the open document per
  [ADR 0167](0167-mate-knows-which-document-is-on-screen.md)) went only with
  the sheet's opening question. A follow-up typed into either composer went
  without it, so "what does this say about the membrane?" lost the document it
  was about.
- The optimistic question bubble (`local-<time>`, `seq: -1`) stayed in the
  thread until the conversation was next opened. Nothing replaced it with the
  server's copy.
- The watch-ended path refreshed the conversation list and said it reloaded
  the thread; it did not, so the watch's report row appeared only on reopening.

## Decision

`hooks/use-mate-session.ts` exports `useMateSession({ initialId?, screen? })`,
the only Mate hook a component uses. It returns:

- `conversations`, `activeId`, `messages`, `summaryNoteId`/`setSummaryNoteId`,
  `conversationLoading`, `loading`, `errorMessage`;
- `status: { sending, statusText, draft, error, stopped, deliveredMessageId }`;
- `send(text, { attachments?, spoken?, newConversation? })`, `stop()`,
  `select(id)`, `startNew()`, `remove(id)`, `reload()`, `rejoin()`,
  `isAnswering()`, `updateProposal`, `updateFormDraft`.

`useAssistantConversations` and `useAssistantChat` stay as files but are
imported only by the session. Their types and the proposal and form-draft
mappers stay where they are, since cards, the search overlay and
`lib/*-actions` import them.

`send` owns the whole question: create the conversation when none is open (a
new chat stays blank and unsaved until then,
[ADR 0137](0137-mate-document-citation-links-sheet-search-and-a-blank-default.md)),
or when `newConversation` asks for one, append the optimistic bubble, stream the
answer, append the reply only while its conversation is still open, refresh
the list, and reconcile. It resolves to `{ answered, message, refused? }`:
`answered` says the answer arrived even if the operator has moved on (the
thread drops the attachment chips that went with it), and `message` is the
reply when it arrived in the open conversation (the sheet reads it aloud).

The behaviour fixes:

1. **Screen on every question.** The session takes `screen` as a value or a
   getter and resolves it when a question is sent, so the sheet's typed
   follow-ups carry it too. On the Mate panel itself `screenContextFor`
   gives no screen: "the operator is on the Mate panel" says nothing about
   the question and reads as though it were about Mate. The panel's session
   takes none.
2. **Spoken only for voice.** `openMate(question, { newConversation?, spoken? })`
   in `App.tsx` carries the origin. Only `useMateVoice` (push-to-talk and Hey
   Mate) sets `spoken`. The sheet passes it to `send` and reads the answer
   aloud only when it is set and the operator's **Read replies aloud** setting
   is on; that setting keeps its meaning.
3. **Reconciliation.** After an answer lands in the open conversation the
   session re-reads that conversation quietly: no `conversationLoading`, and
   the result is dropped if the selection sequence moved or a message was
   appended locally while the read was in flight. The `local-*` bubble becomes
   the server's copy, keeping its React key (`clientKey`) so it is not
   remounted, and every unchanged message keeps its object so no bubble
   re-renders or re-parses its markdown. `rejoin()` does the same after a watch-ended rejoin, so
   the report row shows.
4. **One question at a time.** `send` refuses while this session already has a
   question in flight or a stream open, resolving `{ refused: 'busy' }` and
   setting `status.error` to "Mate is still answering the last question." A
   `newConversation` question is the exception: it goes to a fresh
   conversation as before, and the earlier run finishes server side with the
   answer toast covering it. The server's 409 on a send shows the same
   sentence instead of its own text. The refusal shows only while that answer
   is still being written, and only under its conversation; "Stopped." is
   scoped the same way. Busy ends when the answer's stream ends: the quiet
   re-read is not awaited and does not count, so a quick follow-up or Hey Mate
   is taken. A refused question is never lost: the thread puts a typed one
   back in the composer, and the sheet puts a question it was opened with
   (voice, Help, Settings, a selection) into the composer for the operator to
   send.

A first question creates its conversation and opens it straight from the
POST's answer, with no read, and the session marks the new id before it
becomes active so the rejoin below does not ask `GET .../run` of a
conversation it is about to send into. The session object, its `status` and
its actions are memoised, so components depend on them directly.

Rejoining moves into the session: it rejoins on every change of open
conversation, skipping a conversation whose own question is already
streaming, and `rejoin()` is what the thread calls when `useMateTelemetryWatch`
reports a watch ended. The watch hook stays separate.

Each surface keeps its own session. The panel (`AssistantDrawer`) makes one
from the router's conversation id, the sheet makes its own and stays mounted
once opened. `AssistantThread` takes `session` (plus `watch`, `canWrite` and
its composer props) and keeps the composer, uploads, dictation, summary note
and cards.

Tests follow the seam: `use-mate-session.test.ts` drives the session against a
fake server, `test/helpers/fake-sse.ts` holds the SSE fakes, and
`test/helpers/fake-mate-session.ts` gives component tests a session to hand
the thread.

## Alternatives rejected

- **Messages in the shared query store.** One cache entry per conversation
  would let the panel and the sheet show the same thread, but the thread is
  local state that streaming appends to frame by frame, and
  [ADR 0173](0173-one-query-module-under-the-resource-hooks.md) deliberately
  left it out of the store. The two surfaces already show different
  conversations at once, and a reply landing in the wrong one is the bug the
  open-conversation guard exists to prevent.
- **A server-pushed sync of every surface.** A stream of conversation changes
  to every open tab would replace reconciliation and the busy check, at the
  cost of a new server channel. Nothing the operator does needs two surfaces
  to agree live; the answer toast already covers an answer finishing out of
  sight.
- **Fixing the two copies in place.** Each fix would have to be made twice and
  kept in step, which is how they drifted.

## Consequences

A Mate surface is a session plus a view. The send protocol, its guards and the
rejoin rules live in one module with one test file, and a component test hands
the thread a fake session instead of rebuilding two hook results.

Every question from the sheet now sends the screen; none from the panel. A
question from Help, Settings or a selection is
no longer answered with a spoken summary, so it is never read aloud even with
**Read replies aloud** on.

Every delivered answer costs one extra read of its conversation.
