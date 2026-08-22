# ADR 0047: Retire the Rode & Scope tile; add a Rode Planner to the Anchor Watch panel

## Status

Accepted.

## Context

The Rode & Scope dashboard tile (`rode-scope-tile.tsx`) was built to answer "how much
chain should I put out?", but it was framed as a *monitor*: it divided `rodeDeployedM`
by hawse depth and graded the result against a catenary recommendation. That framing
only works if the boat knows how much chain is actually deployed — and this boat's
Lewmar windlass has no chain counter, so `rodeDeployedM` sits at whatever was last typed
by hand (usually 0). The tile therefore showed a permanent red **"Scope Insufficient"**
badge, not because scope was actually insufficient, but because the input was unknown.
A red alarm-coloured badge that is red for a missing-input reason, not a real condition,
trains the operator to ignore red — which is worse than not showing the badge at all.

The useful moment for this calculation is **before the anchor goes down**, not after:
you want to know how much chain to pay out and how big the swing circle will be, so you
can choose a spot with room to swing. That belongs next to the anchor watch, not on the
dashboard grid where it can only ever look backward at a number nobody entered.

## Decision

- **The tile is removed entirely**: the component, its `rode-scope` widget id, the
  backend's `validDashboardWidgetIDs` allowlist entry, and the entry in
  `defaultDashboardLayout`.
- **A retired-widget-id migration replaces it.** `validateDashboardWidgets` rejects a
  PATCH outright if it contains *any* unknown widget id, so an existing install with
  `rode-scope` already saved in a page's layout would be permanently unable to save any
  further layout change — dragging an unrelated widget would 400. `loadDashboardPages`
  now strips ids in a named `retiredDashboardWidgetIDs` map on every load and rewrites
  the file when something was actually removed, so the stale id can never reach
  `validateDashboardWidgets` again. This is the general mechanism for retiring a builtin
  widget going forward — add the id to `retiredDashboardWidgetIDs`, nothing else.
- **A collapsible "Rode Planner" sidebar is added to the Anchor Watch panel**, reachable
  and useful both before and after the anchor is set:
  - Plans against **tide-corrected maximum expected depth** (sounder depth plus the rise
    to the next high, clamped so a falling tide never reduces the planning depth), with
    an explicit, visible fallback to sounder-only depth when no tide station is
    configured — never a silent substitution.
  - Planning wind is **editable, seeded from `maxGustKts['1h']`** (falling back to
    apparent wind, then blank) — you plan for the forecast gust, not the breeze at the
    moment you happen to drop the hook.
  - Outputs a headline "pay out N m" figure, scope ratio, and swing radius from
    `lib/catenary.ts`'s existing (now covered) `calculateCatenary`.
  - Can **write the alarm radius** directly: `recommendedRodeM + bowOffsetM + loaM`.
  - **The pass/fail scope badge renders only when `rodeDeployedM > 0`.** This is the
    direct fix for the reported problem: with no deployed rode recorded, there is no
    comparison to make, so no badge — not an "unknown" badge, no badge at all.
- **Sea state / seabed type follow a strict no-masking-fallback persistence rule.**
  `PATCH /api/anchor-watch` 404s when no watch is active (`backend/anchor.go`), so the
  planner guards the call site and simply holds edits locally while inactive — it never
  fires the PATCH and swallows the resulting error. Wind and depth overrides are pure
  planning inputs and are never sent to the server at all.

### Mounting a second sidebar without breaking the left nav

The planner reuses the vendored `components/ui/sidebar.tsx` primitives, but three facts
about that implementation make the obvious approach (`<SidebarProvider><Sidebar
side="right">...`) actively dangerous:

1. Default `Sidebar` (`collapsible="offcanvas" | "icon"`) renders `fixed inset-y-0 h-svh`
   — it pins to the *viewport*, not its container. Mounted inside the anchor watch panel
   it would overlay the whole app.
2. `SidebarProvider` persists to a single **module-level** `localStorage` key
   (`sidebar.open`) on every `setOpen`, even when controlled. A second provider would
   clobber the left nav's persisted open/closed state.
3. `SidebarProvider` unconditionally binds ⌘/Ctrl+B. A second provider means one keypress
   toggles both sidebars.

The fix is **`<Sidebar side="right" collapsible="none">`**, which short-circuits to a
plain in-flow `flex h-full w-[--sidebar-width] flex-col` column with no fixed
positioning and no provider state consulted for layout. It still calls `useSidebar()`
internally, which resolves fine against the app's single, already-mounted
`SidebarProvider` (the anchor watch panel renders inside it) — so **no second provider
is created**, and `SidebarTrigger` (which calls the left nav's `toggleSidebar()`) is
never used. Collapse/expand state is owned entirely by the planner component itself:
local `useState` seeded from its own `localStorage` key
(`anchorWatch.rodePlanner.open`), completely independent of `sidebar.open`.

Whoever adds the second calculation method (a user-requested "ratio method",
`(depth + bowRollerHeight) × 5` or `× 7` in a blow) should follow this same
`collapsible="none"` + self-owned-collapse pattern — do not reach for a second
`SidebarProvider` to get there.

### Extension seam for a second method

`lib/rode-plan.ts` exports a `RodeMethod` type
(`(input: RodePlanInput) => RodeMethodResult | null`) and one implementation,
`catenaryMethod`. The planner renders the one-element array of results directly rather
than through a registry — a registry with one entry would be guessing at requirements
the second method hasn't stated yet. Adding the ratio method is: write a second
`RodeMethod` function, add it to the array the component maps over into
`SidebarGroup`s.

### Swing radius refuses to compute when LOA is unset

The swing circle is `rode + bow offset + LOA`. `anchor.loa_m` defaults to `0`
(`config/app-config.ts`) and is genuinely unset on real installs — it was `0` on
the development boat when this was written, a ~60 ft power cat.

Computing the swing radius with `loaM = 0` produces a number that looks
authoritative but is short by a boat length, and "Apply as alarm radius" would
then *shrink* an existing, correct alarm circle — on the development boat, from
75 m to 39 m. A too-small anchor alarm is a safety defect, not a cosmetic one.

So when `loaM <= 0` the planner emits no swing figure, shows an amber prompt to
set LOA in Settings, and disables "Apply as alarm radius". This is the same
reasoning as the badge rule above and follows the repo's fallback policy: a
missing required input is surfaced, never silently defaulted to a value that
makes the output wrong.

## Consequences

- Dashboard widget count drops from 17 to 16 (`README.md`, `dashboard-widgets.ts`,
  `dashboard-bento-grid.tsx`).
- `lib/catenary.ts`'s `calculateCatenary` — previously untested — now has
  characterization coverage via `test/rode-plan.test.ts`, pinning its existing output so
  future edits to the math can't silently drift.
- A future widget retirement follows the same `retiredDashboardWidgetIDs` mechanism
  rather than inventing a new one.
