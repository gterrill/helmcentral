# ADR 0013: Multi-Page Dashboard

## Status
Accepted

## Context

ADR 0012 gave the dashboard one drag-and-drop-configurable widget layout for the vessel. Operators need different widgets by activity: anchor watch and rode/scope at anchor, alternator and engine widgets underway. The request referenced N2KView's "displays" and Grafana engine/power dashboards as examples. Supporting these activities with one layout required repeated editing or leaving less relevant widgets visible.

This remains a single-vessel, single-user embedded dashboard (per ADR 0012) — the change here is from one layout to several *named* layouts ("pages"), manually switched by the operator, not from single-user to multi-user.

## Decision

### Named pages replace the single layout

The persisted unit changes from "the layout" to "a page": `{id, name, widgets, created_at, updated_at}`. This is a near-exact structural mirror of `routeData` (ADR 0006's "saved routes" — a named, user-created record with full CRUD), not a novel pattern: `backend/dashboard_pages.go` uses the same map + `sync.RWMutex` + atomic-JSON-file-write shape as `backend/routes.go`, down to reusing `cacheFilePath`/`writeJSONFileAtomic`. The existing widget-catalog validation (`validateDashboardWidgets`, `validDashboardWidgetIDs`, `dashboardLayoutItem`) relocates unchanged from the deleted `dashboard_layout.go` into `dashboard_pages.go` — a single page's `widgets` array is validated exactly as the old singleton layout was.

One deliberate divergence from the routes.go precedent: pages are sorted by `CreatedAt` **ascending**, not the newest-first ordering routes use. Pages are stable tabs an operator returns to repeatedly, not a recency feed — reordering them every time a page is edited would be disorienting.

A server-side invariant not present in the routes.go precedent: `DELETE /api/dashboard-pages/:id` returns `400` if it's the only remaining page. The dashboard must always have at least one page once any has ever existed; this is enforced in the handler rather than trusted to the frontend disabling a button, since the frontend is not the only possible client.

### One-time migration, not a compatibility shim

The old singleton (`data/dashboard-layout.json`) held a real, hand-tuned 11-widget layout — this could not simply be discarded. `loadDashboardPages()` tries the new pages file first; only if it doesn't exist yet does it fall back to reading the legacy file, wrap its widgets into a single page named "Anchored", and persist that as the new pages file going forward. The legacy file itself is never modified or deleted — it's read at most once, as a migration source, then ignored on every subsequent boot once the new file exists. There is no dual-write period and no backward-compatible endpoint kept around: `dashboard_layout.go` and its `GET`/`PUT /api/dashboard-layout` routes are deleted outright, matching ADR 0012's own framing of this as a single-vessel app with no external clients to keep compatible.

Naming the migrated page "Anchored" is a judgment call, not a hard requirement — it reflects that the layout being migrated was tuned for at-anchor use, and renaming a page is a first-class supported action if that guess doesn't fit.

A separate, smaller case — zero pages ever having existed (a fresh install, no legacy file either) — is handled in the backend, in `loadDashboardPages()` itself: it synthesizes one page named "Anchored" from a `defaultDashboardLayout` constant (the same 3-column arrangement `DEFAULT_DASHBOARD_LAYOUT` held in `App.tsx` under ADR 0012) and persists it before the server answers its first request. That constant's role shifts from "fallback rendered when nothing is saved" (ADR 0012) to "seed data for the synthesized first page" — its content is unchanged, only its owner moves. An earlier version of this decision had `App.tsx` auto-create the first page client-side, in a `useEffect` guarded against React StrictMode's double-invocation by a bootstrapped-ref; moving the synthesis server-side removes that guard entirely, since `pages` is now never observably empty on a fresh install — there's no client-visible "loaded but empty" gap left to bootstrap against.

### Manual switching only, in the dashboard header

Pages are switched via an explicit UI control, not automatically by vessel state — `navigationState` (anchored/moored/underway) already exists and is read elsewhere in the app (tide-tile display, anchor-watch auto-arm), but wiring it into page switching was explicitly ruled out: auto-switching mid-edit, or on a false-positive state read, would be more surprising than helpful. The operator decides when to switch, the same model as N2KView displays or a Grafana dashboard list.

"Dashboard" remains one entry in the left sidebar — pages are a concept *within* the dashboard view, not a navigation-level concept alongside Forecast/Tides/Routes/etc. The switcher (`DashboardPageSwitcher`) is a `Popover`-based control (there is no `tabs` primitive in `components/ui/`, and tabs would consume header width proportional to page count in an already-packed header row), placed immediately before the existing `LayoutModeToggle`. Its trigger button matches `LayoutModeToggle`'s pill styling for visual consistency. Inside: select, inline rename (pencil icon swaps the row into a text input; the input selects its existing text on focus so typing immediately replaces it, rather than inserting at the cursor), and delete (hidden once only one page remains), plus a "New Page" footer button — deliberately avoiding a modal/dialog, since none exists yet in this project's UI primitives and the interaction is simple enough not to need one.

Layout edit mode (`layoutEditing`) remains a single global toggle, unaffected by which page is active — it applies to whichever page is currently showing, with no per-page edit-mode state to manage.

### Widget rendering is unchanged

`App.tsx`'s `renderWidget` switch (mapping a widget id to its rendered tile, closing over the same ~15 already-fetched data hooks) needed zero changes. Pages only change *which* widget ids are in the active page's `widgets` array — every widget still renders against the same live vessel data regardless of which page is showing, so switching pages is instant and doesn't re-fetch anything.

### Mutation pattern: local patch, not refetch

`useDashboardPages()` initially mirrored `useRoutes()`: every `createPage`/`updatePage`/`deletePage` call refetched the full page list. Every widget drag/resize therefore triggered a `GET` of all pages even though `updatePage` had already returned the updated page. The hook now patches local state from each mutation's response: `createPage` appends the returned page, preserving the server's oldest-first order; `updatePage` replaces the matching entry by id; `deletePage` filters it out. `refetch` remains exposed and handles the mount-time `GET`; only post-mutation refetches were removed.

### Render-cost cleanup: memoized tiles, deduped localStorage hook, stale active-page reconciliation

Three follow-on cleanups, made once the page-switching machinery above was in place and its actual cost became visible:

- Every dashboard tile component (`AlternatorTile`, `BatteryPowerTile`, `RodeScopeTile`, etc.) is now wrapped in `React.memo`. `App.tsx` re-renders on every polled-data tick (vessel, electrical, tanks, wind, ...), and before memoization every mounted tile re-rendered on each of those ticks regardless of whether its own props changed — switching dashboard pages made this worse by mounting a fresh set of tiles whose props were otherwise stable. `WindTile` already had this from an earlier pass; this extends the same treatment to the rest of the catalog.
- `useActiveDashboardPageId`, `useDashboardRouteId`, and the (now-removed) inline duplicate in each shared the identical "read/write a single id to `localStorage`, mirrored in `useState`" body. This is factored into one `useLocalStorageId(key)` hook that both now call.
- `useActiveDashboardPageId` now takes `pages` and reconciles the stored id against it: if the stored id doesn't match any current page (e.g. it pointed at a page that's since been deleted), it resolves to `pages[0]` and writes that back, rather than leaving `App.tsx` to fall back to `pages[0]` itself on every render via `pages.find(...) ?? pages[0]`. While `pages` hasn't loaded yet (empty array), the raw stored id passes through unresolved and no write-back happens, so an empty list during initial load isn't mistaken for "every page was deleted."

## Consequences

Positive:
- Operators can define as many named tile arrangements as they want (Anchored, Underway, or anything else), each independently laid out, without one layout being a compromise across activities.
- The existing hand-tuned layout is preserved automatically on upgrade, not lost or reset.
- No new widget types, no new data-fetching — this is purely a change to how widget-placement configuration is organized and switched (individual tiles gained `React.memo` as a render-cost cleanup, but render *inputs* are unchanged).
- The last-page-delete guard means the dashboard can never be driven into a zero-page, empty-shell state through normal use.
- Layout-editing actions (drag, resize, add, remove widget) patch local state directly from each mutation's response, matching the old single-layout hook's cost rather than the full-refetch cost initially incurred by mirroring `useRoutes()`.
- Fresh installs get their first page synthesized server-side before the first request is answered, so the frontend never has to distinguish "still loading" from "genuinely empty" for bootstrap purposes.

Negative / explicitly deferred:
- No automatic or suggested page switching based on vessel state, even though the signal (`navigationState`) already exists — purely manual, by design.
- The frontend's `DASHBOARD_WIDGET_IDS` and the backend's `validDashboardWidgetIDs` remain two independently hand-maintained lists of valid widget ids (a pre-existing issue from ADR 0012, not introduced or fixed here).
- No reordering of pages beyond creation order, and no per-page icon/color — pages are name + widgets only, matching the minimal scope of what was requested.

## Related

- ADR 0012: Configurable Bento Dashboard with Drag-and-Drop Layout — the single-layout system this replaces; its widget catalog, validation, and grid-rendering mechanics are unchanged and reused as-is.
- ADR 0006: Manual Route Planning with Smart Helpers — the named-record CRUD + `data/` persistence pattern (`routes.go`/`useRoutes()`) this feature's backend and hook are modeled directly on.
