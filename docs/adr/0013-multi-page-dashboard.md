# ADR 0013: Multi-Page Dashboard

## Status
Accepted

## Context

ADR 0012 gave the dashboard a single, drag-and-drop-configurable widget layout, but still only one layout for the whole vessel. In practice, the widgets an operator wants visible differ sharply by activity — anchored vs. underway wants a different mix entirely (e.g. anchor watch and rode/scope matter at anchor; alternator and engine-adjacent widgets matter underway), the same idea behind N2KView's "displays" and Grafana engine/power dashboards referenced when this was requested. Forcing one layout to cover every situation means constant re-editing or a layout that's a compromise for all of them.

This remains a single-vessel, single-user embedded dashboard (per ADR 0012) — the change here is from one layout to several *named* layouts ("pages"), manually switched by the operator, not from single-user to multi-user.

## Decision

### Named pages replace the single layout

The persisted unit changes from "the layout" to "a page": `{id, name, widgets, created_at, updated_at}`. This is a near-exact structural mirror of `routeData` (ADR 0006's "saved routes" — a named, user-created record with full CRUD), not a novel pattern: `backend/dashboard_pages.go` uses the same map + `sync.RWMutex` + atomic-JSON-file-write shape as `backend/routes.go`, down to reusing `cacheFilePath`/`writeJSONFileAtomic`. The existing widget-catalog validation (`validateDashboardWidgets`, `validDashboardWidgetIDs`, `dashboardLayoutItem`) relocates unchanged from the deleted `dashboard_layout.go` into `dashboard_pages.go` — a single page's `widgets` array is validated exactly as the old singleton layout was.

One deliberate divergence from the routes.go precedent: pages are sorted by `CreatedAt` **ascending**, not the newest-first ordering routes use. Pages are stable tabs an operator returns to repeatedly, not a recency feed — reordering them every time a page is edited would be disorienting.

A server-side invariant not present in the routes.go precedent: `DELETE /api/dashboard-pages/:id` returns `400` if it's the only remaining page. The dashboard must always have at least one page once any has ever existed; this is enforced in the handler rather than trusted to the frontend disabling a button, since the frontend is not the only possible client.

### One-time migration, not a compatibility shim

The old singleton (`data/dashboard-layout.json`) held a real, hand-tuned 11-widget layout — this could not simply be discarded. `loadDashboardPages()` tries the new pages file first; only if it doesn't exist yet does it fall back to reading the legacy file, wrap its widgets into a single page named "Anchored", and persist that as the new pages file going forward. The legacy file itself is never modified or deleted — it's read at most once, as a migration source, then ignored on every subsequent boot once the new file exists. There is no dual-write period and no backward-compatible endpoint kept around: `dashboard_layout.go` and its `GET`/`PUT /api/dashboard-layout` routes are deleted outright, matching ADR 0012's own framing of this as a single-vessel app with no external clients to keep compatible.

Naming the migrated page "Anchored" is a judgment call, not a hard requirement — it reflects that the layout being migrated was tuned for at-anchor use, and renaming a page is a first-class supported action if that guess doesn't fit.

A separate, smaller case — zero pages ever having existed (a fresh install, no legacy file either) — is handled in the frontend, not the backend: `App.tsx` auto-creates a page named "Anchored" seeded from the existing `DEFAULT_DASHBOARD_LAYOUT` constant the first time it observes an empty page list. That constant's role shifts from "fallback rendered when nothing is saved" (ADR 0012) to "seed data for the auto-created first page" — its content is unchanged.

### Manual switching only, in the dashboard header

Pages are switched via an explicit UI control, not automatically by vessel state — `navigationState` (anchored/moored/underway) already exists and is read elsewhere in the app (tide-tile display, anchor-watch auto-arm), but wiring it into page switching was explicitly ruled out: auto-switching mid-edit, or on a false-positive state read, would be more surprising than helpful. The operator decides when to switch, the same model as N2KView displays or a Grafana dashboard list.

"Dashboard" remains one entry in the left sidebar — pages are a concept *within* the dashboard view, not a navigation-level concept alongside Forecast/Tides/Routes/etc. The switcher (`DashboardPageSwitcher`) is a `Popover`-based control (there is no `tabs` primitive in `components/ui/`, and tabs would consume header width proportional to page count in an already-packed header row), placed immediately before the existing `LayoutModeToggle`. Its trigger button matches `LayoutModeToggle`'s pill styling for visual consistency. Inside: select, inline rename (pencil icon swaps the row into a text input; the input selects its existing text on focus so typing immediately replaces it, rather than inserting at the cursor), and delete (hidden once only one page remains), plus a "New Page" footer button — deliberately avoiding a modal/dialog, since none exists yet in this project's UI primitives and the interaction is simple enough not to need one.

Layout edit mode (`layoutEditing`) remains a single global toggle, unaffected by which page is active — it applies to whichever page is currently showing, with no per-page edit-mode state to manage.

### Widget rendering is unchanged

`App.tsx`'s `renderWidget` switch (mapping a widget id to its rendered tile, closing over the same ~15 already-fetched data hooks) needed zero changes. Pages only change *which* widget ids are in the active page's `widgets` array — every widget still renders against the same live vessel data regardless of which page is showing, so switching pages is instant and doesn't re-fetch anything.

### Mutation pattern change: refetch, not optimistic update

`useDashboardPages()` mirrors `useRoutes()`'s shape rather than the old `useDashboardLayout()`'s: every `createPage`/`updatePage`/`deletePage` call refetches the full page list afterward instead of patching local state optimistically. This means every widget drag/resize now round-trips a `GET` of all pages, not just a `PUT` of one layout — a deliberate, acceptable cost given this app's low page count, chosen for consistency with the routes precedent over introducing a second, different hook shape.

## Consequences

Positive:
- Operators can define as many named tile arrangements as they want (Anchored, Underway, or anything else), each independently laid out, without one layout being a compromise across activities.
- The existing hand-tuned layout is preserved automatically on upgrade, not lost or reset.
- No new widget types, no new data-fetching, no changes to how any individual tile renders — this is purely a change to how widget-placement configuration is organized and switched.
- The last-page-delete guard means the dashboard can never be driven into a zero-page, empty-shell state through normal use.

Negative / explicitly deferred:
- No automatic or suggested page switching based on vessel state, even though the signal (`navigationState`) already exists — purely manual, by design.
- Every layout-editing action (drag, resize, add, remove widget) now costs a full page-list refetch rather than an optimistic local update, unlike the old single-layout hook.
- The frontend's `DASHBOARD_WIDGET_IDS` and the backend's `validDashboardWidgetIDs` remain two independently hand-maintained lists of valid widget ids (a pre-existing issue from ADR 0012, not introduced or fixed here).
- No reordering of pages beyond creation order, and no per-page icon/color — pages are name + widgets only, matching the minimal scope of what was requested.

## Related

- ADR 0012: Configurable Bento Dashboard with Drag-and-Drop Layout — the single-layout system this replaces; its widget catalog, validation, and grid-rendering mechanics are unchanged and reused as-is.
- ADR 0006: Manual Route Planning with Smart Helpers — the named-record CRUD + `data/` persistence pattern (`routes.go`/`useRoutes()`) this feature's backend and hook are modeled directly on.
