# ADR 0162: Mate summarises a conversation into a note

## Status

Accepted (2026-10-05). Builds on [ADR 0161](0161-mate-recalls-earlier-conversations.md) (Mate searches and
reads earlier conversations) and [ADR 0146](0146-mate-proposes-the-operator-applies.md) (Mate proposes,
the operator applies). The notes themselves are those of [ADR 0116](0116-notes-are-documents-and-folders-are-the-manual.md).

## Context

Recalling a conversation (ADR 0161) fixes Mate repeating itself, but it leaves what the conversation
settled buried in a chat. The skipper who showed Mate that the Reverso fuel polisher's discharge
manifold has no sampling port wants that kept where the rest of the boat's knowledge is: found by
document search, and shown on the equipment record. Raw chat is the wrong thing to keep. It holds
Mate's wrong guesses next to the corrections, and the whole route to the answer.

## Decision

- **A button in the Mate composer**, beside the microphone: **Summarise to note**. It is disabled while
  Mate is answering, before Mate has replied, and in a read-only session. It sits in the shared
  thread, so the full Mate page and the quick Mate sheet both have it.
- **Mate proposes, the operator applies.** Pressing the button opens a review dialog and asks the
  server for a draft. Nothing is written until the operator presses Save. The dialog shows an
  editable title, the body in the note editor, and the equipment the draft is about as ticked
  checkboxes. The server's own message is shown in the dialog when drafting or saving fails.
- **One note per conversation.** The conversation remembers its note (`conversations.summary_note_id`,
  added to existing databases when the store opens). Once one exists the button reads **Update note**
  and Save replaces that note's title and body instead of creating another. If the operator has
  since deleted the note, the conversation reads as having none and the next save creates a fresh
  one: that is plain state, not a masked error.
- **Three sections, each left out when empty.** *What we established* (facts about this boat,
  especially from the operator's photos and observations), *Ruled out* (the option and why), and
  *Procedure that worked* (numbered steps actually followed). There is no open-questions section: a
  note records what was settled. A conversation with nothing in any section is answered with a 422
  and "Nothing in this conversation to keep yet.", never an empty note.
- **The model writes JSON, the server writes the Markdown.** The draft endpoint
  (`POST /api/assistant/conversations/:id/summary-draft`) sends the user and assistant messages, never
  watch reports, to the configured Mate model once, non-streaming, asking for
  `{title, established[], ruled_out[{option, why}], procedure[], equipment[]}`. Each attachment is
  listed with its filename and the title and summary the document store holds for it, which is where a
  photo's analysis lives. The prompt says: only what this conversation established about this boat; no
  general knowledge presented as boat fact; the operator's observations and photos outrank Mate's
  guesses; a correction by the operator wins over what Mate said; invent nothing. The server renders
  the headings itself, so the layout cannot drift, and ends the note with
  `From the Mate conversation [title](/mate/<id>), <date>.` A model failure or a reply that is not
  that JSON is a 502 carrying the reason. There is no fallback text.
- **Equipment links.** The names the model gives are matched against inventory with the same text
  match find_equipment uses (name, manufacturer, model, aliases). A name counts when it matches a
  record's name exactly, or matches exactly one record. A name that matches several records without
  an exact name, or none, is dropped rather than guessed. Suggestions are deduplicated and come back
  as `{id, name, linked}`; `linked` marks those already linked to the existing note.
- **Note type.** Suggested `quirk`, or `procedure` when the procedure has more entries than facts and
  ruled-out items together. On an update the type is left alone.
- **Save is one server call.** `POST /api/assistant/conversations/:id/summary-note` takes
  `{title, body, type, add_equipment_ids, remove_equipment_ids}`. It creates the note through the
  same path as POST /api/notes, or applies the update through the same path as PATCH /api/notes/:id
  (called in process, so the frontmatter rewrite, file swap and reindex are not copied), remembers the
  note on the conversation, then adjusts the links. The note id is remembered before the links, so a
  link failure leaves a note the retry updates rather than a second one. On update, the dialog sends
  only the links it changed, so links the operator made elsewhere stay.
- Both endpoints are write tier: the draft spends the operator's OpenRouter credit, as sending a
  message does. The Mate chat model is used; there is no separate setting.

## Alternatives considered

- **Save straight away.** Rejected. A summary of a conversation can be wrong, and a wrong note found
  later by search is worse than none. The review step is the point.
- **Automatic memory without review.** Rejected, for the reason ADR 0161 deferred it: a store Mate
  writes to unseen cannot be corrected, and a wrong fact saved once is repeated for good.
- **A new note on every run.** Rejected. Summarising twice would leave two notes that disagree and
  both turn up in search. One note per conversation, updated in place.
- **An open-questions section.** Rejected. A note of what is unsettled reads as fact on a later
  search hit and invites Mate to act on it.
- **The browser makes several calls (create, patch, link) itself.** Rejected. Three requests can
  fail part way and strand a note unlinked from its conversation. The server call keeps the order
  that makes a retry safe.
- **A PUT to record the note on the conversation.** Not built. The save call records it, so there is
  nothing for the browser to do afterwards.
- **A separate summary model setting.** Rejected for now. One more setting for a call the operator
  makes occasionally.

## Consequences

- Each Summarise or Update press costs one model call over the whole conversation. Long messages
  are cut at 4000 characters and attachment descriptions at 1500 so a long chat fits.
- Photos reach the summary only through the title and summary the document store already holds. A
  photo not yet analysed contributes its filename alone.
- Updating regenerates the note from the whole conversation, replacing the body. Edits the operator
  made to the note since are in the draft only if they are made again in the dialog.
- The conversation list does not check whether each summary note still exists; opening a
  conversation does.
