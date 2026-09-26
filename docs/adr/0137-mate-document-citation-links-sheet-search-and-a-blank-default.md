# ADR 0137: Mate cites documents as icon links, the sheet gets its own search, and Mate opens blank

## Status

Accepted. Amends [ADR 0094](0094-mate-voice-and-app-wide-help.md)'s claim
that the sheet "keeps appending to that conversation - the most recently
updated one": a plain open of either the Mate panel or the Mate sheet no
longer auto-selects any existing conversation at all (see §3 below). ADR
0094's own "New conversation"/"Open in Mate" wiring is otherwise unchanged.

## Context

Three separate rough edges in Mate's UI, shipped together as one cycle:

1. A document Mate found via `search_documents` or `read_document` was cited
   as free text in parentheses - "Two Furuno NMEA 2000 power taps are fitted
   (Operations/Equipment List, navigation and electronics)." - which the
   operator could read but never act on. There was no way to tap it open.
2. The Mate sheet (the quick channel opened from any page's header) had no
   way to reach a conversation other than whichever one was already active.
   The full `/mate` panel has always had a list column with its own inline
   "Search conversations" box; the sheet, by design (ADR 0093), has no list
   column at all.
3. A plain open of either surface - clicking the sidebar's Mate entry, or
   the header's "Ask Mate" button - auto-selected whatever conversation the
   server considered newest. Combined with "New conversation" persisting an
   empty row immediately (see §3 below), the practical result was that
   opening Mate often landed on a conversation the operator never chose:
   sometimes a real earlier thread, sometimes an empty one left over from a
   "New conversation" press that was never followed by an actual question.

## Decision

### 1. A citation is a document id in a real link, resolved and iconified client-side

The system prompt (`assistant_prompt.go`) now tells Mate to cite a document
or note it found via `search_documents`/`read_document` (or one attached
directly to the message) as a markdown link of the exact shape
`[Title](/documents?document=<document_id>)`, adding `" › Section"` to the
title when a specific heading or page is the source - e.g.
`[Equipment List › Navigation](/documents?document=abc123)`. This is the
same URL `documentViewerHref` already builds for an attachment chip
(`lib/document-citation.ts`, moved out of `assistant-thread.tsx` so both
share one definition), so a citation opens exactly what an attachment chip's
click already does - no new route.

Both tools already had a `document_id`; `search_documents`' `Title` and a
new `Title` field on `read_document`'s result are guaranteed non-blank now
(`assistantCitationTitle`, `assistant_tools.go`: the stored title, falling
back to the filename) - the same fallback `documentDisplayName` already
applies on the Documents panel - so Mate always has a real name to put in
the link text.

The markdown renderer (`assistant-markdown-impl.tsx`) recognises that exact
link shape (`parseDocumentCitationHref`) and swaps its visible text for a
small icon: the link's own text becomes the tooltip and accessible name
instead. The icon (`citationIconKind`, `lib/document-citation.ts`) is
resolved from the document's actual mime/kind - PDF, spreadsheet,
image, note, or a generic file - via one `GET /api/documents/:id` per
distinct cited id, shared through a small in-memory cache
(`use-document-citation.ts`) so the same id cited more than once in a
reply, or across a whole conversation, only fetches once. This same lookup
is what marks an unknown or deleted id: a 404 (or any other failure)
renders a muted, broken-file icon with "Document not found" rather than
disappearing - AGENTS.md's fallback policy applied to a UI affordance.
Kind was deliberately NOT threaded through the link itself (a `?kind=`
param would work but goes stale the moment a document's mime changes, and
duplicates data the store already owns); the one-request-per-id lookup was
judged cheap enough not to need it, and is exactly the "single lookup, not
an N+1 per row" shape the backend's own `search_documents` folder-path
resolution already established.

Tap/click always navigates, in-app, same tab (a plain `<a href>`, matching
the existing attachment-chip pattern) - the tooltip is a hover/focus bonus,
not the only way to learn what a citation points to, since the same text
is already the icon's `aria-label`.

Old conversations, with the free-text parenthetical form, render exactly as
they always have - there is no migration, and no attempt to retroactively
parse a mention back into a link.

### 2. The sheet gets a command-palette style search overlay, sharing the panel's own filter

`conversation-search-overlay.tsx` is a `Dialog`-based overlay: a search box,
and below it either the 8 most recent conversations (an empty query) or
every title match, navigable with arrow keys and Enter, closed with Esc (the
`Dialog` primitive's own handling - the overlay adds no Escape listener of
its own). It filters with the exact same predicate the `/mate` panel's
inline "Search conversations" box already used
(`filterConversationsByQuery`, moved to `lib/assistant-conversation-search.ts`
so both surfaces share it rather than growing a second search), over
`conversations.conversations` the caller already has loaded - no separate
fetch. There is no backend search over conversation message text, only
titles, so that's as far as either surface's search goes; building one was
out of scope for a two-line predicate this cycle didn't otherwise need.

A dedicated `command`/`cmdk` component was considered and rejected: this
project's `components/ui/dialog.tsx` (and several others `shadcn add command`
would have overwritten) has been hand-tuned to this app's `base-ui`
z-index stacking order (dialog/sheet/popover/tooltip all coordinate through
specific `z-*` values, documented on `tooltip.tsx`), and the upstream
`command` component ships a materially different `Dialog` with its own
z-index scheme. Reusing the existing `Dialog` + `Input` + a hand-rolled
roving highlight (the same combobox pattern shadcn's own `Command`
implements internally) cost about a hundred lines and touched nothing
already shipped.

### 3. A plain open never auto-selects a conversation, and "New conversation" persists nothing until sent

`useAssistantConversations`' mount/`reload()` logic used to fall back to the
first (i.e. newest) conversation in the freshly fetched list whenever no
explicit id was requested or the requested one no longer existed. That is
gone: with no explicit id - the ordinary "click Mate" case, or the sheet's
plain "Ask Mate" open with no voice question - nothing is selected at all,
and the thread shows blank (`AssistantThread`'s own empty-state prompt).
`reload()` inherits the same rule, so reopening the sheet after a load
failure repopulates the list without disturbing whatever the operator
already had open (or hadn't).

Separately, both "New conversation" buttons (the panel's and the sheet's)
used to call `create()`, which `POST`s and persists a brand-new conversation
row immediately - before the operator had typed a word. Closing either
surface right after left an empty, permanent row in the list forever: the
"empty persisted draft" bug this decision removes. Both buttons now call a
new `startNew()` - a local reset (clears the active id and thread) with no
request at all. `create()` still exists and is still what actually persists
a conversation, called lazily by `AssistantThread`'s send handler (and by
the sheet's own spoken-question flow) at the moment the first real message
is posted - unchanged from before this decision.

An explicit id - a URL naming a conversation (`/mate/<id>`), "Open the Mate
page" handing the sheet's thread to the panel, or the operator picking one
from the list or the new search - still opens it, and still wins when it
matches something in the freshly fetched list. A URL naming a conversation
that has since been deleted now lands on the same blank state a plain open
shows, rather than silently substituting an unrelated "newest" thread in its
place.

#### 3a. Closing the gap: neither surface remembers a picked conversation past its own visit

The above was not, on its own, "always blank" - it only covered the very
first mount. Two things kept a picked conversation alive past that:

- `App.tsx`'s `matePanelConversationId` is app-level state, not panel state:
  it survives `AssistantDrawer` unmounting and remounting as the operator
  leaves the Mate panel and comes back to it (the sidebar's Mate entry
  reused the SAME id, so a remount reopened the same thread), and it does
  not change at all when the panel is already showing (re-clicking "Mate"
  while already on it is a same-instance no-op).
- The Mate sheet never unmounts once opened at all (`mateSheetHasOpenedRef`),
  so its own `useAssistantConversations` instance simply kept whatever was
  active in memory across any number of closes and reopens.

Both are fixed at the point that actually decides what shows, not by adding
special cases at each call site:

- The sidebar's Mate entry now clears `matePanelConversationId` in the same
  click that sets `activePanel`, and `useAssistantConversations`' re-select
  effect (§3 above) was generalized to react to `initialId` moving to null,
  not only to a new non-null value - so both "the panel remounts with
  `initialConversationId={null}`" and "the panel is already showing and the
  prop merely drops to null" now reset it the same way, via `startNew()`.
- The sheet gained its own reset-on-open effect: every time `open` becomes
  true with no `initialQuestion` (a plain "Ask Mate", not a voice question
  or Help's "Ask Mate", both of which are left entirely alone - see the
  effect's own comment in `mate-sheet.tsx` for why coordinating the two
  would race), it calls `startNew()`, guarded by one exception (next).

**The exception**: a reply the sheet's own chat is still streaming into the
active conversation is never reset out from under itself. The sheet's chat
instance keeps its fetch/SSE reader running in the background regardless of
`open` (it never unmounts), so a blind reset-on-every-open would wipe the
optimistic question bubble and the in-flight draft on a mid-answer
close/reopen, and - the worse failure - the FINISHED reply would never land
in `messages` at all, because `AssistantThread`'s own append-on-completion
only fires when the conversation it was sent to is still the active one.
`chat.isStreamingConversation(activeId)` (the same check `AssistantThread`'s
own rejoin effect already used) is the guard: reopening mid-stream leaves
everything exactly as it was, and the reply lands normally once it finishes.

## Consequences

- A citation is now a real, tappable affordance instead of prose the
  operator could only read. The icon depends on one extra request per
  distinct cited document id per session (cached), which is a deliberate,
  bounded cost over threading a `kind` field through every link.
- The sheet's search reuses the panel's own filter rather than duplicating
  it; a future change to how conversations are matched (e.g. adding message
  text once a backend search exists for it) only has one place to change.
- Mate now genuinely opens blank every time - the panel and the sheet both -
  matching what the feature was always meant to feel like: a fresh page, not
  a resumed one, unless the operator explicitly asks for an earlier thread.
  The list can no longer accumulate empty rows from an idle "New
  conversation" press. The one deliberate exception - a reply already
  streaming survives a mid-answer close/reopen - means "always blank" is not
  quite absolute; it is scoped to when there is nothing in flight to lose.
- `useAssistantConversations`' hook tests and several component tests that
  relied on the old auto-select-newest behaviour as scaffolding (rather than
  as the thing under test) were rewritten to select explicitly first - see
  `use-assistant-conversations.test.ts` and `mate-sheet.test.tsx`.
