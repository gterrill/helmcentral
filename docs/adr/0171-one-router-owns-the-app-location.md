# ADR 0171: One router owns the app location

## Status

Accepted (2026-10-13). Replaces the navigation plumbing in `App.tsx` that
[ADR 0074](0074-deep-links.md) introduced and that ADRs 0106, 0110, 0112, 0115, 0123, 0127
and 0156 each extended by a field. The URL scheme itself is unchanged.

## Context

`App.tsx` knew where the app was in sixteen separate `useState`s: the panel, the Settings
section and import run, the Mate conversation, the Documents folder, Details page and
manual section, the wall-displays slug, and eight Inventory fields (section, equipment id,
bin, location, plan view, plan deck, decks list, deck id). Two more, the New item draft and
its preset, had no URL at all. Each deep link or page added another one.

The same location was translated three times. `applyAppLocation` turned a URL into state.
A sync effect turned state back into a URL and decided push or replace from a ref, a
first-write flag and a canonical-path check. The popstate handler did the first translation
again, and when a guard blocked a Back press it rebuilt the URL with a second, hand-copied
builder of the sync effect's object literal. The two builders drifted: the copy in the
popstate handler left out the open manual section.

Three guards sat on top. `requestNavigate` compared panels, `requestBackFromDocumentDetails`
and `requestWithinInventory` covered moves that stay in a panel, and the popstate handler
had to work out which of them applied. Each had a pair of `onDirtyChange` flags and a
clearing effect for when the page that reported them went away, plus a save ref per panel
and an inline `AlertDialog` of about ninety lines in `App`.

Because every field was independent, every move had to clear its siblings by hand. Opening
a bin cleared the location page, the decks list and the deck. Opening an item cleared four
fields before setting its own. A missed line meant the bar and the screen disagreed, which
the code comments record as fixed more than once.

## Decision

1. **`AppLocation` is a union.** Each panel with views of its own carries a `route`, a
   single union of that panel's views. Settings has a section or an import run. Documents has
   the listing (folder, manual section, viewer deep link) or a document's Details page.
   Wall displays has the index or one display's editor. Inventory has a section index,
   an equipment record, a New item draft, the Plan view, a bin, a location, the Decks list
   or one deck. Going to a view replaces the whole slice, so there is nothing left over to
   clear. The dashboard carries its page id, Mate its conversation, and the wall display
   (`/display/<slug>`) its slug and parsed options. A New item draft formats to the Equipment
   index, as it always did. No URL string changed; `app-location.test.ts` asserts the same
   paths and only its object-shape expectations were rewritten.
2. **`lib/app-router.ts` holds the location.** `createAppRouter(history)` has no React in
   it. It owns the one location value and a `subscribe`/`getSnapshot` pair for
   `useSyncExternalStore`, parses the address it starts at, and listens for Back/Forward.
   `navigate(target, { replace?, force? })` is the only way to change the location, and it
   writes the address by the rules `App` used to apply: replace for a correction, for the
   first write, when the first page changes underneath, or when the bar being left was not
   canonical; push otherwise. The router also keeps the last location of each panel, so the
   sidebar returns a panel to where it was left (and Mate always to an empty chat). Two
   history adapters exist: the browser's (`window.history` and `popstate`) and an in-memory
   one for tests.
3. **One formatter.** Every URL the router writes comes from `formatAppLocation`. When a
   guard holds a Back press, the router writes the page still on screen back as a new entry
   with the same formatter as any other write. The rollback now includes the manual
   section it used to drop.
4. **Guards are registered by the components that own the state.** `useNavigationGuard`
   takes `{ owner, label, blocks(from, to), save }`, or null when there is nothing to
   protect. `blocks` returns one verdict, `{ kind, detail? }` with `kind` one of `'dirty'`,
   `'stocktake-work'` or `'quick-add-work'`, or null, and is asked with the move itself, so a
   click and a Back press go through the same question and the wording cannot disagree with
   the kind. The guard registers in a layout effect, so it is live before a navigate made in
   the same commit. Settings and the Document Details page register one each. Inventory has
   no guard of its own: the Equipment, location and deck editors register for their drafts
   (and the Equipment editor for photos still queued for Retry), Stocktake for its scans,
   and the bin's quick-add for its staged item, each with its own `save` where there is one
   and each holding only a move that takes it off screen. They share the questions in
   `lib/inventory-navigation-guard.ts` (`inventoryEditorClosedBy`, `inventoryWorkClosedBy`,
   which moved out of `app-location.ts`), and a component unmounting unregisters it, so no
   dirty flag is mirrored upward or cleared by hand. A held move is stored in the router with
   the wording the guard gave at that moment, not read live, which keeps a quick-add save
   that finishes under the dialog from swapping Leave/Stay for Save and Continue.
   `NavigationGuardDialog` renders it with the same copy and buttons as before, and Save and
   Continue calls the guard's own `save`. While it runs, Cancel and Discard are disabled and
   the held move is made once the save resolves unless the operator withdrew it:
   Stay, a newer held move, or a move of their own while the save ran (Back, a page switch,
   a push navigation). A forced navigation the save itself makes (a New item landing on its
   record) and a replace-correction do not count. A second Save and Continue for the same
   move joins the running save. A registered guard also means
   unsaved work, so the update reload and the sidebar's unsaved dot read the registry
   instead of mirrored flags.
5. **Panel route hooks.** `useSettingsRoute`, `useDocumentsRoute` and `useInventoryRoute`
   each return `{ route, go(view), close() }`, with a few helpers where a panel needs them
   (`openFolder` drops the manual section when the folder changes; Inventory keeps whether
   Locations was last on Table or Plan so the crumbs return to it). Their callbacks keep one
   identity across moves, because the panels they are handed to re-sync on prop identity.
   Three small components, `SettingsRoutePanel`, `DocumentsRoutePanel` and
   `InventoryRoutePanel`, wire each panel to its hook (Settings and Documents register their
   panel's guard there), which takes the callback wiring out of `App`.
6. **The router owns the dashboard page id.** `useActiveDashboardPageId` is now a
   reconciler: it reads the page the location names, corrects a deleted or unknown id with a
   replace, and keeps writing to localStorage what a bare `/` opens. On the wall display it
   keeps `persist: false` and the router changes the page in memory only. Rotation does not
   touch history or localStorage (ADR 0110).

## Alternatives rejected

- **React Router or TanStack Router.** The URL scheme is already written, tested and
  deliberately hand-rolled (ADR 0074). The wall kiosk's WebKit parses the entry chunk and
  that chunk is size-checked on every build. The one thing a router library would add is
  navigation blockers, and its blocker API would still need the per-panel question ("does
  this move take my editor off screen?") written by us. Not worth a dependency in the entry
  chunk.
- **A flat bag of optional fields.** It is what `AppLocation` was. It keeps every move
  responsible for clearing siblings, and a field's "only meaningful alongside X" rule lives
  in a comment rather than in the type.
- **Verb methods per view** (`router.openBin(code)`, `router.closeDeck()`). Callers would
  need to know the verb for every view, the router would grow a method per field added, and
  a test of "every view formats and round-trips" would have to enumerate methods instead of
  data. A target location is data; `navigate(target)` stays one function.

## Consequences

- Mate drafts stay unguarded on purpose. They count towards the update reload's "unsaved
  work" check, which is all they are used for; leaving a half-typed message is not worth a
  dialog.
- The wall display variant is parsed and carried but never written to history. Back and
  Forward and the sync rules both skip it, as before.
- A few edges changed with the model, none visible in the tests that guard navigation:
  - Picking the Equipment section in the Inventory nav while an item is open now goes to
    the index. It used to do nothing, with or without the unsaved-changes dialog in front
    of it.
  - A click inside Inventory is held only when it takes the editor or the stocktake/bin
    work off screen, the question Back already asked. It used to be held on any move while
    anything was dirty.
  - The Table/Plan choice is remembered while the Inventory panel is mounted, not across
    visits to other panels, and is not reset by Back onto an unrelated Inventory page.
  - Choosing a folder in Documents drops the open manual section in the same move, where
    the panel used to follow up with a second write.
- Tests: `app-router.test.ts` drives the router against the in-memory history, one test file
  per panel route hook and per guard, and the existing App navigation tests pass unchanged.
