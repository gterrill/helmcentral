# ADR 0257: The Mate Sheet Request Has One Owner

## Status

Accepted (2026-11-05). Moves where the rules in ADR 0183, 0226 and 0227 live. It
changes none of them.

## Context

The Mate sheet opens over whatever panel is showing, from a header button, an
alarm card, a watch run, Help or a voice command. Each open says what it is for.
`AppShell` held that as seven separate pieces of state: the open flag, the
question, the new-conversation, draft and spoken flags, the alarm and the watch
run. Three places had to agree about them. `openMate` set all seven, the sheet's
close handler cleared them by hand, and a third callback cleared just the two
contexts once a question was sent. Add a new kind of context and each of the
three needed a line, plus the screen context's memo dependencies and a prop on
the sheet.

Each of those rules once cost a bug, and the comments say so: a stale question
re-sent on reopen, and a draft left unsent that tied the next, unrelated chat to
its watch run. Rules that live in three places drift.

## Decision

`useMateSheetRequest` owns the request and its life. It takes the screen the
operator is on and returns:

- **`ask(question?, options?)`:** Opens the sheet. The request it builds replaces
  the previous one whole, so nothing from the last open survives.
- **`sheetProps`:** Everything the sheet takes about its request: `open`,
  `onOpenChange`, `requestId`, `initialQuestion`, `newConversation`, `draft`,
  `spoken`, `screen` and `onContextDone`. `AppShell` spreads it.
- **`open` and `hasOpened`:** `AppShell` needs the first to tell the answer
  watcher which conversation is on screen. The second is the lazy-mount latch.

Closing the sheet resets the whole request, and "Open the Mate page" closes it the
same way. The sheet's "context done" call drops only the alarm and the watch run,
once their question is sent or the operator starts or picks another chat. The
sheet stays open and its question stays.

## Which request a send belongs to

Every `ask` takes a new request id, and the sheet passes it back with "context
done". The hook ignores a call whose id is no longer current. Without that, run A's
send could finish after the operator closed the sheet and opened a draft about run
B, drop B's context, and leave B's chat unlinked. The sheet also puts the id in the
key that stops one open sending its question twice, so asking the same words again
while the sheet is open sends them again.

## Adding a kind of context

A new kind is one entry in the hook's table of context layers, which pairs its
name with the function that layers it over the screen. The ask options, the stored
context and the screen all derive from that table, so nothing else changes.
Callers that name the options type (`MateAskOptions`) pick the new kind up too.

## What stays in the shell

- **The sheet's active conversation:** `mateSheetConversationId` mirrors the
  panel's, and the answer watcher reads both. It is about which thread is on
  screen, not what the sheet was opened for.
- **The sheet's unsent-draft flag:** The unsaved-work guard reads it with the
  panel's.
- **The Documents deep-link latch:** It watches the operator leave Documents and
  has nothing to do with the sheet.
- **The Help sheet's latch:** It is the same pattern for a different sheet.

## Rejected

- **A reducer shared with the panel:** The panel opens a conversation by id and
  has no question, flags or context. A shared shape would be mostly empty fields.
- **Moving the screen context into `useMateSession`:** The session sends whatever
  screen it is given (ADR 0183). The sheet's context has a different lifetime from
  the session's, so it stays outside.

## Limits

The lazy-mount latch is state set in the same update as `open`, so the sheet mounts
on the render that first shows it. Each caller's prop type is a `Pick` of
`MateAskOptions`, so a panel only sees the options it uses.
