# ADR 0076: Shared dashboard page order

Status: Accepted

## Context

Dashboard pages were sorted by creation time. Operators could name pages for
vessel modes but could not put those modes in their preferred navigation order.
Page definitions are shared across devices; a browser-local ordering preference
would make navigation inconsistent between the helm and a phone.

## Decision

Store a zero-based `position` on each page in the existing dashboard pages
file. Both listing and persistence use position, creation time, then ID. On
load, normalize positions to consecutive integers: older files without positions
retain creation order with ID as the deterministic timestamp tie-breaker.
New pages append after the greatest position, and ordinary page patches preserve
position. Deletion does not change the relative order of surviving pages.

`PUT /api/dashboard-pages/order` accepts `{"page_ids":[...]}` and requires every
current page ID exactly once. Missing, duplicate, unknown, or stale membership
is rejected. The endpoint holds the page mutex, stages copied records, writes
the complete file atomically, and only then publishes the new state in memory.
A persistence failure changes neither the in-memory order nor the previous file.
The operation uses the same `tierWrite` authorization as other page mutations.
Concurrent valid reorder requests are serialized; the last successful write wins.

The existing page switcher exposes a reorder mode with keyboard- and
touch-operable movement buttons. The client waits for confirmation before
changing its page array, blocks overlapping reorder requests, and surfaces
failures without treating them as initial dashboard-load errors. The sidebar
and switcher consume that same ordered array. Read-only users can navigate but
do not see page-management controls.

Reordering preserves active page identity. If the first page changes, its
canonical `/` alias changes too; URL reconciliation replaces the current history
entry rather than inventing a navigation. Initial browser-local page selection
and explicit `/dashboard/<id>` links keep their existing behavior.

## Consequences

- Supersedes the deferral of page reordering in ADR 0013.
- No drag-and-drop library or configuration-file editing is required.
- All devices share persisted order; already-open devices see changes on reload
  or refetch, not through a new live synchronization channel.
- A concurrent create/delete makes an old reorder request fail explicitly;
  the server never silently drops or adds IDs to make the request succeed.
- Operator steps are in [Reorder dashboard pages](../how-to/reorder-dashboard-pages.md).