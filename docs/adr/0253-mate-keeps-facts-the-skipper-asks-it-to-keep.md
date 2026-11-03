# ADR 0253: Mate keeps facts the skipper asks it to keep

## Status

Accepted (2026-11-03). Extends [ADR 0146](0146-mate-proposes-the-operator-applies.md) (Mate proposes and the
operator applies) and [ADR 0158](0158-mate-changes-records-through-changesets.md) (the changeset that carries
every proposal). Builds on [ADR 0161](0161-mate-recalls-earlier-conversations.md) (recall by search) and
[ADR 0227](0227-watches.md) (the review of a watch report).

## Context

The morning report asked about the overnight power draw two days running, although the skipper had answered in
the "Ask Mate about this run" chat. The review saw the report, the rules' findings and its own earlier added
findings. It saw neither its written review nor any reply, and it had no conversation tools. Nothing linked a
chat to the run it began from.

The same gap sits under every chat. ADR 0161 left recall to Mate's habit of calling `search_conversations`, and
a model that thinks it knows the answer does not search. The standing notes are a single block the skipper
edits by hand, so a fact Mate learns has nowhere to go that Mate itself can reach.

## Decision

### The next review reads the last one

- **The previous review's text goes into the next review**, in every case. The prompt says not to repeat a
  question from it unless the figures have changed.
- **A chat opened from a run is linked to it.** `conversations` gains a `watch_run_id` column, added when the
  store opens, and the chat's creation request carries the run id the screen already names.
- **The skipper's messages in chats linked to the previous run go into the next review, and no further.**
  They are labelled as the skipper's words and as record, not instructions. Replies to an older run are not
  carried, so the review never rests on a stale answer.
- **Only user messages travel.** Mate's own wrong conclusion in that chat must not become the next review's
  premise. The first user message of a linked chat is the question "Ask Mate about this run" sent for the
  skipper, not their words, so it is left out. The text is capped at 2,000 characters, the newest kept. A deleted chat has no rows and gives
  nothing. A failure to read the replies is logged and the review still gets the previous review's text.

### A list of facts, not notes and not search

A fact Mate should never lose is a short statement, so the store is a list of them: `mate_memories`, with an
id, the text and two times, in `helmcentral.sqlite`, created with the assistant's tables.

- **Why not the standing notes.** They are one block of text written by hand. A fact Mate learns would have to
  edit the skipper's paragraph, and a bad edit could not be told from the skipper's own words.
- **Why not search.** Search finds what the model decides to look for. A kept fact is in the prompt every time.
- **All facts go into every chat prompt and every review prompt**, in the stable part of the prompt, where
  they cost little once cached and change only when a fact is kept, edited or removed. Each is dated.
- **Current state is never kept.** "We're at anchor" goes stale, and the boat can measure it. The prompt and
  the tool description say so.
- **The limits are 50 facts of 300 characters on one line.** A save past either fails with a plain message.
  When the list is full, the call fails and says that someone who can open Settings must remove one. Mate never
  drops one itself.

### The save guard is on the server

`remember_fact` is the one tool. It extends ADR 0146: a change still waits for the operator's tap, except the
one the operator has already asked for in words.

- **Saved at once** only when the skipper's own latest message in the turn asks to remember and the fact is in
  their words. The ask is a phrase list (`remember that`, `keep in mind`, `make a note that` and a few more) run
  on the stored message text, so an attached document or a watch report can never ask. A question or a negation
  ("do you remember", "I can't remember") is not an ask. The fact must also be theirs: its content words are
  mostly in the message, every number in it appears exactly, and a "no" or "not" in one is in the other. A unit
  of one or two letters after a number (L, kg) is not counted, so "400 litres" matches "400 L". Anything else,
  such as Mate's own conclusion or a document's text, becomes a card. The check errs towards a card.
- **Otherwise a proposal.** The tool builds a changeset of one `memory` operation and returns it as any
  proposal, and the skipper taps **Remember this**, or **Replace fact** for a replacement. This reuses the card,
  the Apply route and the stale check, with the button named for what it does.
- **A replacement is always a proposal.** The tool takes the id of the fact it would replace, and the card
  shows the old and the new text. Only the tap replaces it, even straight after "remember that". Nothing
  inside the server decides that two facts contradict. Mate does, from the list in its prompt, and the guard
  keeps its mistake from becoming a silent overwrite.
- **A saved fact commits with its card.** The tool checks the fact and the limit with a dry run and answers
  with an applied proposal, but writes nothing. The fact is inserted, under the id the card names, in the
  transaction that saves the assistant message. A run that fails or is stopped saves neither, so no fact
  outlives a run that left no card to undo it. A fact that cannot commit then, because the list filled or the
  same fact was kept earlier in the turn, never costs the reply: the message saves and that card is stored
  `stale` with a plain reason ("Not kept. ..."), shown as Not kept with no Undo. A database error still fails
  the save.
- **Undo is the server's.** `POST /api/assistant/proposals/:id/undo` removes the fact and stores the proposal as
  `undone`, which is final, so a reload shows the same card with no Undo. It refuses, with a plain reason, when
  the fact was rewritten since (its text or `updated_at` differs from what the card saved). A fact already
  removed counts as undone. The proposals table gains the `undone` status; a table made before it is rebuilt
  when the store opens, keeping every row.
- **Unattended reviews read the list and cannot save.** `remember_fact` is in no review's tool list. A review
  prompt lists the facts without their ids, since a review cannot replace one. The chat prompt keeps the ids,
  which `replaces` needs.
- **A list that can't be read is said.** The prompt tells Mate the facts could not be read, in a chat and in
  both kinds of review, rather than reading as an empty list. The failure is also logged.
- **Days are in the boat's time.** The prompt, the outside assistants' context and the record's `kept_on` use
  one helper. The record also carries `kept_at_iso`, the instant in UTC.

### Managing and outside assistants

- Settings → Mate lists the facts under the standing notes, with **Edit**, **Remove** and **Add fact**, on
  routes at read tier for the list and write tier for changes.
- The boat context for outside assistants lists the facts as a read-only section. The section is drawn from no
  switchable tool, so no token gate hides it, and no outside tool changes a fact.

## Consequences

- A fact the skipper states once reaches every later chat and review, without a search.
- Mate's own conclusion can be offered, never kept, without the skipper's tap.
- Two facts that contradict reach the skipper as one card, not as a quiet overwrite.
- A reply reaches only the next review. A question still unanswered after that is asked again, as it should be.
- Fifty facts at 300 characters add at most about 15,000 characters to each prompt.
- Rejected: storing replies as facts automatically, which would keep Mate's own mistakes; a similarity check on
  the server to detect contradiction, which would be a guess the skipper cannot see.
