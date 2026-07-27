# ADR 0032: Responsive Dashboard Below the Grid Breakpoint

## Status

Accepted. Amends ADR 0012 (Configurable Bento Dashboard), which is otherwise unchanged — see its Status block.

## Context

The UI targets a helm-station touchscreen at ~1600×1000. Below `lg` (1024px) it did not adapt so much as give up:

- `DashboardBentoGrid` fell back to a `flex flex-col` stack. At 768px — iPad portrait, a common second screen on board — that produced a single ~900px-wide column, which wastes the screen rather than fitting it.
- The header was `h-16` with no wrapping and no responsive hiding, packing SidebarTrigger, Separator, Breadcrumb, DashboardPageSwitcher, LayoutModeToggle and VesselStatusBar into roughly 550px of content. Neither of its two child flex containers carried `min-w-0`, so at 320px they refused to shrink and pushed the right-hand cluster off-screen — a standing violation of the AGENTS.md rule against viewport overflows.
- `EmbedTile`'s `h-full` iframe sat inside a percentage-height chain whose ancestors were auto-height in the stack, so it collapsed to its ~150px intrinsic default rather than the height the operator had sized it to.

The trigger was a question about adopting a community `tailwind-responsive-design` skill. We did not. No canonical skill of that name exists; the similarly-named ones are general Tailwind reference material, most of them leading with v4 CSS-first `@theme` while this repo is on 3.4.17, and all of them aimed at viewport breakpoints — which is the wrong axis, since the dashboard is laid out by `react-grid-layout`, not by Tailwind. Applied naively such a skill would have recommended `ResponsiveGridLayout`, exactly what ADR 0012 rejected.

## Decision

### Narrow layouts are derived, never persisted

Below `lg`, the same persisted 12-column layout is reflowed into a CSS grid:

| Aspect | Rule |
|---|---|
| Columns | `grid-cols-1`, `sm:grid-cols-2` (two columns from 640px) |
| Order | sort by `(y, x)` — unchanged from the previous stack |
| Span | `w >= 6` (half the 12-column grid) gets `sm:col-span-2`; everything else one column |
| Height | `minHeight: h * 32 + (h - 1) * 16` — RGL's own row maths, as a **floor** |

Nothing extra is written. ADR 0012's one-global-layout constraint is confirmed, not violated: the narrow view is a pure function of the coordinates already stored.

`minHeight` rather than `height` is the load-bearing choice. It honours the operator's sizing intent while letting a tile whose text wraps at phone width grow instead of clipping — and it gives `EmbedTile`'s percentage chain a resolvable ancestor, fixing the iframe collapse as a side effect.

### RGL is not reused at a reduced column count

Considered and rejected. RGL rows are fixed height, so a `h=6` tile sized against a 400px desktop column overflows its absolutely-positioned box at 320px where text wraps more, and RGL v2 legacy has no auto-height rows — an active regression, not a neutral trade. At one or two columns there is no relative `x` left to preserve that `(y, x)` document order doesn't already capture. It would also keep drag/resize machinery mounted on touch devices, fighting native scroll.

A second *persisted* narrow layout was rejected outright: real schema surface, real validation surface, and a "which one did I just edit?" failure mode, to serve a device that cannot express 12-column intent.

### Edit mode stays desktop-only

A narrow-view drag expresses "put this third", which has no non-arbitrary mapping back to `(x, y, w, h)`. The decisive argument is fail-fast: `validateDashboardWidgets` in `backend/dashboard_pages.go` checks only positivity and a 1000 max — it does **not** enforce `X + W <= 12`. A bad write-back from a narrow view would therefore be persisted silently and corrupt the desktop layout with no server-side tripwire.

`LayoutModeToggle` is consequently absent below `lg` rather than present-but-inert, and `layoutEditing` in `App.tsx` is **derived** (`layoutEditingRequested && canEditLayout`) rather than stored — otherwise narrowing the window while editing strands the dashboard in a non-interactive state with no visible control to leave it.

### Header collapse

`min-w-0` on both header halves, with the breadcrumb as the designated slack absorber (`flex-1 min-w-0` + `truncate`) so a long panel label truncates instead of displacing the clock. Below `sm`: the separator and the parent breadcrumb crumb are hidden, the page-switcher trigger goes icon-only (retaining its `aria-label` — create/rename/delete live only in that popover and the sidebar drawer only navigates), and the clock drops its date line and seconds, keeping `HH:MM` at `text-[1.35rem]`.

The clock stays in the header rather than moving to a tile: it is the most-glanced element, and the panel views replace the dashboard entirely, so a tile would hide it exactly when navigating. Date and seconds are conditionally rendered, not CSS-hidden — the seconds span re-renders every second, and there is no reason to pay for a widget nobody can see.

### Two breakpoint values, one mechanism

`frontend/src/lib/breakpoints.ts` holds `BREAKPOINTS` (Tailwind stock 640/768/1024) and `useMinWidth(px)`, matchMedia-only, seeded from `matchMedia(...).matches` in a lazy `useState` initialiser.

768 (sidebar) and 1024 (grid) are deliberately **kept distinct** — they answer different questions and both answers are right: an off-canvas sheet at 900px is worse than an icon rail, and 12 columns at 800px is ~62px per column. What was wrong before was not the values but that there was no shared constant and the two hooks used different *mechanisms*: `useIsMobile` read `window.innerWidth` inside its `change` handler, so it never reacted to a matchMedia-only change, while `useIsDesktopGrid` seeded from `innerWidth` and corrected in an effect, guaranteeing a stack→grid flip on every desktop first paint. Both are fixed by construction.

`useIsMobile`'s name and path are unchanged so `ui/sidebar.tsx` stays untouched and the shadcn upgrade path survives.

### `md`–`lg` is the tightest band, and old `md:` step-ups were backwards

Two columns arrive at `sm` (640), but the sidebar stops being an overlay sheet and becomes a fixed ~256px rail at `md` (768). So 768–1023 is the *narrowest* the content area ever gets — ~240px per tile, tighter than a phone's ~350px single column.

Several tiles had `md:` step-ups written when `md` meant "more room" (`md:text-7xl` on the SOC readout). Under the narrow grid that assumption inverts, and those step-ups pushed values outside their card borders. Where a tile must adapt in this band, the pattern is `md:` **down**, `lg:` back up — see `battery-power-tile.tsx` and `depth-tide-tile.tsx`.

This is the clearest argument yet for container queries: a viewport breakpoint cannot see that the sidebar took 256px. Deferring them (below) means accepting this awkward triple-step in exchange for not expanding scope.

## Consequences

- iPad portrait gets a usable two-up layout instead of one ~900px column.
- Phone users still cannot rearrange the dashboard. Unchanged from ADR 0012, now for a stated fail-fast reason rather than a density claim.
- `sm:`/`md:` prefixes added to tiles are **viewport-scoped and will be semantically wrong** if container queries land — they should be re-expressed as `@sm:`/`@lg:` at that point. This is the main reason the tile pass was kept confined to `ui/tile.tsx` plus the two tiles that demonstrably overflowed.
- Horizontal overflow is **screenshot-verified, not unit-verified**. jsdom has no layout engine, so `scrollWidth > clientWidth` cannot be asserted in Vitest; a test that appeared to check it would be theatre. `.claude/skills/run-dashboard/SKILL.md` now carries a four-viewport matrix (390 / 430 / 768 / 1600), with 768 called out as the case that catches overflow.
- `frontend/src/test/setup.ts` previously stubbed `matchMedia` to always-false with a no-op `addEventListener`. Every test therefore rendered the mobile stack and **no breakpoint transition was testable at all**. It now defaults to 1280 via `setViewportWidth`, so the RGL grid is exercised in tests for the first time.

## Deferred

`@tailwindcss/container-queries` (Tailwind 3.4-compatible). It is the right long-term answer — a tile at `w=2` and one at `w=8` share a viewport but need different layouts, and the `md`–`lg` squeeze above is a viewport breakpoint failing to see its own container. It is deferred because on phone every tile is full-bleed, so container width ≈ viewport width and `sm:`/`md:` do the identical job; pulling it in would expand this change into a rewrite of 17 tiles for no phone benefit. Its own ADR when taken up.

Two dead `@container/…` classes (v4-era shadcn markup that compiled to nothing on 3.4 with `plugins: []`) were deleted from `ui/card.tsx` and `ui/field.tsx` rather than left as false evidence that container queries were already in play.

`wind-tile.tsx` is a **known, pre-existing** violation of the AGENTS.md `text-[9px]` legibility floor. `WIND_MOBILE_CFG` is a 475px design canvas scaled by `Math.min(1, availableWidth / 475)`; at 390px the tile gets ~335px, so `scale ≈ 0.71` and an authored `text-[10px]` label renders at ~7.1px. Reaching 9px needs `scale >= 0.9`, i.e. ~427px of tile width, which no phone provides. The fix is either a third, narrower canvas config or a scale clamp with horizontal scroll — real design work on the primary navigation instrument, constrained by ADR 0030 (state must live above the mobile/desktop split). Not introduced by this change: the scale only drops below 1.0 under ~515px viewport.
