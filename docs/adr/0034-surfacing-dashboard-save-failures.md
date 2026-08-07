# ADR 0034: Surfacing Dashboard Save Failures

## Status
Accepted

Applies the AGENTS.md Fallback Policy ("prefer fail-fast; do not add graceful fallback behavior that masks upstream data/source problems") to the dashboard-page mutation path introduced by ADR 0013 and extended by ADR 0031.

Introduces the app's first toast primitive. Any later feature needing transient feedback should use it rather than inventing a second mechanism.

## Context

`useDashboardPages` exposed three mutations — `createPage`, `updatePage`, `deletePage` — each ending a failed request with `if (!res.ok) return null` (or `false`). The caller learned nothing, and every one of the ~8 call sites in `App.tsx` invoked them as `void updatePage(...)`, discarding even that. A rejected save was indistinguishable from a successful one.

This was not theoretical. `commit` in `dashboard-bento-grid.tsx` rebuilt the widget list from `react-grid-layout`'s `LayoutItem`, which carries only `{i,x,y,w,h}`, silently dropping the `embed` field that ADR 0031 decision 3 put on the layout item. Every drag or resize on a page holding an embed therefore PATCHed a widget array that `validateEmbedWidget` correctly rejected with `embed widget requires embed config: embed:<token>`, and **no layout change on that page persisted at all** — not merely the embed's.

What the operator saw was a widget sliding back to the bottom of the grid after a reload. The grid still showed the drag because that is RGL's local state; only a reload revealed the server had never accepted it. Diagnosing it took a full session of reading the save path, when the server had been naming the exact problem in every response body the whole time.

The backend was right at every step. Both defects were on the client: one dropping data, one hiding the complaint.

## Decision

### 1. Report from inside the hook, not at the call sites

`toast.error(...)` fires in `createPage`, `updatePage` and `deletePage` on `!res.ok`.

The alternative — throwing, or returning a result object, and having each caller report — was rejected on coverage grounds. It requires editing ~8 call sites, and more importantly it makes silence the default for the *ninth*: a new `void updatePage(...)` written later would reintroduce exactly this bug. Reporting at the single point every mutation already funnels through makes the guarantee structural rather than a convention to remember.

The cost is that a data hook now imports a UI library, which is accepted at app level.

### 2. Mutation failures do **not** set the hook's `error` state

`error` stays reserved for the initial `fetchPages` load failure.

This is the load-bearing distinction, and it is not stylistic: `App.tsx` gates the entire dashboard render on `!pagesError`. Routing a save failure through the same state would blank the whole screen because a drag was rejected. A failed load means there is nothing to show; a failed mutation means the last known-good state is still on screen and still correct. They are different severities and need different surfaces.

**Convention for other hooks:** fatal-on-load → `error` state; recoverable mutation → toast.

### 3. The server's message is shown, not a generic one

The backend returns `{"error": "<message>"}` with 400. That string becomes the toast description under a short human prefix (`Could not save dashboard`), because `embed widget requires embed config` is the sentence that would have collapsed the debugging session above into one glance.

Parsing must never throw — a non-JSON or empty body (a 500 from a proxy, say) falls back to `HTTP <status>`. An error handler that can itself fail is worse than the silence it replaces.

### 4. Return values are unchanged

`null` / `false` on failure, as before, so no call site changes and this ADR is additive to ADR 0013's API.

### 5. `sonner`, adapted away from `next-themes`

The shadcn `sonner` registry item reads the theme via `useTheme()` from `next-themes`. This app has no theme provider — `useDarkMode` toggles a `dark` class on `<html>` directly — so that hook would have reported the wrong theme permanently while adding a dependency that exists to solve a problem this app does not have.

`Toaster` therefore takes `isDarkTheme` as a prop, matching how `App` already passes it to `VesselStatusBar`, `EmbedTile` and others, and `next-themes` is not a dependency. This is a deliberate divergence from upstream shadcn: re-running `shadcn add sonner` will reintroduce the `next-themes` import and must be re-adapted.

## Consequences

**Positive:**

- No dashboard mutation can fail silently, by construction rather than by discipline.
- Backend validation messages reach the operator. `validateDashboardWidgets` already produced precise, human-readable strings; nothing consumed them until now.
- A toast primitive exists for the rest of the app.

**Tradeoffs:**

- Toasts are transient. A failure that occurs while the operator is looking away is missed, and there is no persistent log or error surface to consult afterwards. Acceptable for a single-user helm dashboard where the action and the feedback are seconds apart; it would not be for an unattended one.
- `useDashboardPages` can no longer be unit-tested without mocking `sonner`, and cannot be reused in a non-toast context without refactoring.
- The prefixes (`Could not save dashboard`) are hardcoded English in the hook. The app has no i18n, so this matches everything around it.
- Only the dashboard-pages hook is covered. Other fetch paths — settings, routes, sat charts — retain their own error handling and were deliberately left alone; converging them is a larger pass.

## Related

- ADR 0013 — multi-page dashboard, `dashboard-pages.json`, the mutation API this covers
- ADR 0031 — the generic Embed widget, whose per-item `embed` config the dropped-field bug lost
- `AGENTS.md` — Fallback Policy and Exception Rule
- `frontend/src/hooks/use-dashboard-pages.ts` — `readErrorMessage`, the three mutations
- `frontend/src/components/ui/sonner.tsx` — the adapted `Toaster`
- `frontend/src/lib/dashboard-widgets.ts` — `mergeLayoutGeometry`, the fix for the bug this ADR's absence hid
- `backend/dashboard_pages.go` — `validateDashboardWidgets`, `validateEmbedWidget`
