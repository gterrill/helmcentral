# ADR 0012: Configurable Bento Dashboard with Drag-and-Drop Layout

## Status
Accepted. The single-global-layout persistence described here (`dashboard-layout.json`, `GET`/`PUT /api/dashboard-layout`) was superseded by ADR 0013 (Multi-Page Dashboard), which replaces it with multiple named pages. The grid-rendering mechanics, widget catalog, and validation described below are unchanged and still apply per-page.

Amended by ADR 0032 (Responsive Dashboard Below the Grid Breakpoint), which replaces the "plain reflowed stack" below `lg` with a two-column CSS grid derived from the same persisted layout. Both decisions below survive intact: the legacy non-responsive RGL API, and one global layout per page. ADR 0032 persists nothing additional — it only changes how the existing coordinates are *rendered* on narrow screens.

## Context

The dashboard presented a fixed 3-column grid arrangement hardcoded in `App.tsx`, with 13 widgets statically positioned. Operators requested the ability to rearrange, show, or hide widgets without a code deployment, to adapt the layout to changing vessel states and operational needs (e.g., hiding widgets relevant only at anchor when underway, or making room for real-time data that matters in the moment).

This is a single-vessel, single-user embedded dashboard (helm-station touchscreen) with no multi-tenant auth or per-user persistence concerns. The layout is therefore stored as a single global server-persisted configuration, not per-device or per-user.

The previous conditional logic (Anchor Watch + Rode & Scope visible only at anchor; Alternator visible only underway) was based on engine/anchor state. This coupling meant layout decisions were baked into the navigation state machine rather than under operator control.

## Decision

### React Grid Layout with legacy (non-responsive) API

The implementation uses `react-grid-layout`'s `WidthProvider(GridLayout)` (accessed via `./legacy` subpath export, since v2 restructured the export surface) instead of the `ResponsiveGridLayout` variant. Persisting a distinct layout per CSS media-query breakpoint would either require storing multiple server-side layout configurations (violating the "one global layout" constraint) or synthesizing mobile breakpoints in code anyway.

Below the `lg` breakpoint (1024px — matching Tailwind's convention used elsewhere in `App.tsx`), layout mode is completely unavailable. A JS media-query check (`useIsDesktopGrid`) swaps to a plain reflowed stack layout instead of CSS-only hiding, to avoid rendering duplicate widget DOM nodes in memory and keeping tests tractable (no risk of accidentally selecting the wrong `.grid` or `[hidden]` version).

### Data model and persistence

A single global layout is stored at `backend/data/dashboard-layout.json` as:

```json
{
  "widgets": [
    {"id": "wind", "x": 0, "y": 0, "w": 4, "h": 8},
    {"id": "depth-tide", "x": 0, "y": 8, "w": 4, "h": 6}
  ],
  "updated_at": "2024-01-15T10:30:00Z"
}
```

Only *placed* widgets are stored (an array), not all 13 with an enabled/disabled flag — a widget's absence from the layout means it is available via the "Add Widget" picker. This follows the precedent established in ADR 0006: durable user preferences live in `data/`, not `cache/`, since layouts are committed operator choices, not rebuildable ephemeral state. Persistence uses the same atomic-write pattern (`writeJSONFileAtomic`) already established for routes.

### API design and semantics

- `GET /api/dashboard-layout` returns `200 {"widgets": []}` on first run (no file yet), not a `404` — an empty layout is a legitimate first-run state, not an error. Only transport failures or malformed saved state surface as errors.
- `PUT /api/dashboard-layout` accepts `{"widgets": [...]}`, validates all widget IDs against the canonical catalog, rejects duplicates, and persists atomically. The response includes the stored layout with `updated_at` timestamp.
- Validation is strict: unknown widget IDs, duplicate IDs, non-positive dimensions, or out-of-bounds coordinates all return `400 Bad Request` with an error message.

### Frontend fetch-and-save patterns

`useDashboardLayout()` follows the same shape as `useRoutes()` and `useDashboardRouteId()` — a single call in `App.tsx`, with results passed down to child components. Autosave occurs on drag/resize gesture end (`onDragStop`/`onResizeStop` callbacks from `react-grid-layout`), not on every continuous layout change. These are discrete gesture-end events, so no explicit debouncing is needed.

The save is optimistic: the hook updates local state immediately, then sends the PUT; if the PUT fails, a `saveError` is set but the local state remains changed (matching the pattern already used for anchor watch and routes). The "Layout Mode" toggle itself is ephemeral — plain component `useState`, not persisted — so it always starts off after a reload.

### Widget extraction and consolidation

Five widgets that were previously inline JSX in `App.tsx` (Wind, Depth & Tide, Position, Today & Now, Battery & Power) were extracted into standalone component files, each taking raw data props and formatting internally. This matches the pre-existing convention established by `AlternatorTile.tsx` and `GeneratorTile.tsx`, and makes the main app file more readable by decoupling data-flow from presentation.

### Removal of engine/anchor state coupling

The previous conditional rendering (`{!isAlternatorTileVisible && (...)}` wrapper around some widgets) is removed entirely. Anchor Watch, Rode & Scope, and Alternator are now always independently placeable on the grid — each widget already handles its own empty/inactive state (displaying "—" or a muted variant when data is unavailable or conditions don't apply), so no parent-level conditional logic is needed.

### Base UI Popover primitive

A new `components/ui/popover.tsx` wraps the Base UI `Popover` primitive (matching the project's existing convention to use Base UI, not Radix, for all shadcn-style primitives). This powers the "Add Widget" picker that appears in layout mode, displaying unplaced widgets by name and adding them on click.

### Effective widgets and default layout simplification

The hook returns `widgets: null` while fetching, and `widgets: []` once loaded (or if no file exists). App.tsx computes `effectiveWidgets` as:

```typescript
const effectiveWidgets = savedWidgets !== null && savedWidgets.length > 0 ? savedWidgets : DEFAULT_DASHBOARD_LAYOUT
```

This deliberately collapses three states ("not yet loaded", "loaded but empty", "never configured") into a single "show the default layout" behavior. This choice avoids a visible empty-dashboard flash on mount, keeps synchronous rendering tests tractable, and is reasonable for a single-operator embedded dashboard where an intentionally emptied layout is an unlikely scenario. The cost is losing the ability to distinguish "user deliberately hid all widgets" from "first run" — acceptable for this use case.

The default layout (`DEFAULT_DASHBOARD_LAYOUT` constant) recreates the pre-bento 3-column arrangement (3 columns of 4 units each, 12-unit total width) so a fresh install presents exactly the old layout, easing the transition for existing operators.

## Consequences

Positive:
- Operators can rearrange, show, and hide all 13 widgets without a code redeploy.
- Higher information density is available when operators want it — e.g., viewing both Anchor Watch and Alternator simultaneously, which was impossible in the rigid 3-column grid.
- Layout is persisted server-side, consistent across client reconnects (or future multi-device support, if added).
- No dependency on a charting library or complex layout solver; `react-grid-layout` is a battle-tested, lightweight grid utility.
- Extraction of inline widgets into standalone components improves code modularity and test isolation.

Negative / explicitly deferred:
- Layout mode is desktop/tablet-landscape only (below `lg` breakpoint, the grid is unavailable and all widgets render as a stacked list, always read-only). Phone users cannot rearrange their dashboard, but phone screen density makes a dense grid layout impractical anyway.

  *Amended by ADR 0032.* Edit mode remaining desktop-only still holds, and for a firmer reason than density (a narrow-view drag has no non-arbitrary mapping back to 12-column coordinates). But "renders as a stacked list" no longer describes the behaviour, and the density claim was doing too much work: it conflated "can't be edited" with "can't be laid out". Below `lg` the widgets now reflow into a one- or two-column CSS grid derived from the same persisted coordinates.
- A fully-emptied layout is indistinguishable from "never configured" (both show the default), so there is no easy way to detect operator intent to create an empty dashboard. This is acceptable for a single-operator embedded device.
- No per-device or per-user layouts (each vessel has one global layout). Multi-user or multi-device support would require auth/identity infrastructure, explicitly out of scope for this single-station dashboard.
- The "Hot Water" control block inside Battery & Power remains non-functional (pre-existing, unrelated to this feature, carried over unchanged during component extraction).
- Layout constraints are simple bounds on min/max dimensions per widget; there is no collision detection or smart snapping beyond what `react-grid-layout` provides natively (which is adequate for this use case).

## Related

- ADR 0006: Manual Route Planning with Smart Helpers — establishes the precedent for `data/` (vs `cache/`) persistence of user-committed configuration, and the pattern of lifting data-fetching hooks into `App.tsx` and passing results down as props.
- ADR 0004: GNSS Validation Gate for Anchor Watch — the anchor watch data model and fetch hook pattern that this feature's dashboard layout hook mirrors.
