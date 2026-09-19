# ADR 0072: One Tile Can Be the Page's Hero

## Status
Accepted

Follows the precedent ADR 0060 set for skin: a page-level property, edited in the
layout-mode toolbar next to the other layout controls.

## Context

Every tile on the board is `rounded-xl` plus a hairline border plus a mono
tracked title, so `Depth & Tide` and `Switches` are typographically identical.
Nothing on a page can be more important than anything else, by construction.
The one exception is accidental: battery state of charge happens to be
authored at `text-7xl`, so it is the biggest thing on screen on every page
that carries it, whether or not it is the page's main reading. On an anchorage
page, anchor distance may be more important but is displayed smaller. The page
model gave the operator no way to choose which tile to emphasise.

The decision: a page nominates one placed widget as its hero, and that widget
gets a visibly different treatment — its own full-width row above the grid,
more width, and a uniform enlargement — while every other tile on the page
keeps exactly the position it was authored at.

## Decision

### 1. The field lives on the page, mirroring Skin exactly

`DashboardPage.hero` (backend `dashboardPageData.Hero`) is a widget id, or
empty for none. Same shape as `Skin`: typed, validated on POST and PATCH,
round-tripped through GET. The one difference is that Skin's valid set is
fixed (`""`, `default`, `instrument`) while Hero's valid set is the page's own
`Widgets` — a hero name that is not also a widget id on the page is rejected
the same way an unknown skin is, fail closed:

```
if !heroWidgetExists(body.Hero, body.Widgets) {
    return 400 "hero must name a widget on this page: " + body.Hero
}
```

### 2. A dangling hero is repaired, not rejected

Because Hero's validity depends on Widgets, removing a widget can invalidate
whatever Hero was already pointing at. Two different requests can do this,
and they are handled differently:

- **An explicit PATCH that sets `hero` to a value the resulting widget list
  doesn't contain** is the caller asserting something false, and is rejected
  with the same 400 an unknown skin gets.
- **A widgets-only PATCH that happens to drop the widget that was already the
  hero** — this is exactly what the bento grid's "X" remove-widget button
  sends, and it says nothing about hero at all — clears the hero to `""`
  automatically rather than failing. Rejecting it would mean the ordinary act
  of removing a tile fails with an error about an unrelated field the caller
  never mentioned, for a reason invisible from the request they sent. This
  takes the same stance `stripRetiredWidgets` already takes on load: a stale
  reference is corrected on write, not treated as the caller's error.

The rule in code is one line, checked only when the patch didn't itself touch
hero: `if body.Hero == nil && !heroWidgetExists(updated.Hero, updated.Widgets)
{ updated.Hero = "" }`.

### 3. The hero leaves the react-grid-layout array, but not really

The obvious implementation — filter the hero out of the widgets handed to
`react-grid-layout` and render it separately — breaks the "everything else
stays put" requirement. RGL compacts vertically by default, and it does this
on every layout-prop change, not just on a drag. Remove an item and RGL will
shift whatever was below it in that column upward to close the gap, visible
immediately and persisted for real the next time the operator drags anything
else on the page (the drag-stop handler commits RGL's *current* internal
layout for every item it manages, compacted positions included). A hero
promotion would then leave neighbouring tiles at new, uncommitted-until-the-
next-drag positions, and demoting the hero later would not put anything back
because nothing was ever supposed to have moved.

So the hero's entry stays in the array RGL manages, marked `static: true`.
Verified directly against the installed react-grid-layout source
(`node_modules/react-grid-layout/dist/chunk-55DQUWLA.js`): a static item is
excluded from the compaction pass outright (`if (!l.static) { l =
compactItemVertical(...) }`), so it neither moves nor lets the compactor
decide anything moved into its space. Its grid cell renders an invisible
placeholder (`aria-hidden`, no content, no edit affordances) purely so the
rectangle keeps reading as occupied; the tile's real, visible content renders
once, in the new row above. Demoting a hero is then just clearing the field —
nothing about `x`/`y`/`w`/`h` was ever touched, so the widget reappears
exactly where it was.

### 4. The enlargement is a transform, because the readout size is not this component's to change

The hero treatment needed a larger type
scale, not just more width. That size is baked into each widget's own file as
Tailwind viewport-breakpoint classes (`lg:text-7xl` in `battery-power-tile.tsx`
and similar per widget) — a prop the bento grid has no way to reach, and
`dashboard-bento-grid.tsx`/`.css` are the only files this change owns.

The lever available from grid scope is a CSS transform on the whole rendered
subtree. `.bento-hero-scale` renders at `100% / HERO_SCALE` of its frame and
is then `transform: scale(HERO_SCALE)`'d back up to exactly fill it. The
widget still lays itself out at its own natural, full-width size (no cramped
intermediate box to trip `truncate`/`line-clamp-1` early), and the whole
rendered result — including its own internal text — comes out `HERO_SCALE`
times bigger. `HERO_SCALE` is `1.15`: enough to read as deliberate without
looking like a distorted blow-up. `overflow-hidden` on the frame is a
defensive clamp against sub-pixel rounding in the `calc()`, not a load-bearing
crop.

`zoom` was considered and rejected: it reflows properly (no transform-scale
blur risk) but Firefox did not ship unprefixed support until Firefox 126,
after this project's Baseline 2024 floor (Firefox 111+, per PRODUCT.md).
`transform: scale()` is the option that actually clears Baseline 2024.

The chrome around it follows the Flat Board Rule: a single `border
border-primary/40` (1px, token colour) frame, never a shadow. `rounded-2xl` so
the frame nests around the tile's own `rounded-xl` rather than fighting it.

### 5. Narrow width gets the same row, just without the grid underneath it

Below `lg` the board is already a plain reflowed CSS grid rather than RGL
(ADR 0032), and there is no `(x, y)` concept to preserve there — the sort
order is recomputed every render. The hero is simply filtered out of that
sorted list and rendered in the same `heroRow` used at desktop width, placed
first. No RGL-specific machinery (the `static` trick, the placeholder) is
needed or present in that branch.

### 6. The control sits beside PageSkinSelect, for the reason ADR 0060 already gave

`PageHeroSelect` is a new component, deliberately shaped like `PageSkinSelect`
rather than folded into it or into the page-switcher popover: a page-level
property belongs where the operator already is when deciding what the page
looks like, and that place is the layout-mode toolbar next to Add Widget.

ADR 0060 flagged that a second page-level property would make the toolbar the
wrong shape and a real page-settings surface would be worth building. This is
that second property, and the toolbar was not rebuilt — two selects still fit
next to Add Widget without crowding. A third one arriving should revisit that
call.

**Known limitation, not fixed here:** layout mode — and with it both
`PageSkinSelect` and now `PageHeroSelect` — is gated behind `useMinWidth(lg)`,
so an operator on a helm tablet cannot reach either control. This was already
true of the skin picker and is unchanged by this work; it is a pre-existing
gap in how layout mode is gated, not something specific to the hero feature,
and fixing it is out of scope here.

## Consequences

An operator can choose a page's most prominent tile independently of the
widgets' default font sizes. The `static` placeholder and reverse-scale CSS
add complexity because the change is scoped to the bento grid rather than
nineteen widget files. Both mechanisms remain in `dashboard-bento-grid.tsx`
and `dashboard-bento-grid.css`; other widgets are unchanged.

The dangling-hero repair means a hero can go away silently from an operator's
point of view: remove the widget, and the page quietly has no hero anymore,
with no toast or confirmation calling that out. This was a deliberate trade
against breaking ordinary widget removal; if it proves confusing in practice,
the fix is a toast on the frontend when a remove happens to take the hero
with it, not a change to the backend's repair behaviour.

## Related

ADR 0110 (wall displays are records) excludes the hero from any page assigned
to a wall display, and clears it when a page is assigned. The hero's row is
additional height above the grid while its promoted tile also keeps its grid
slot, so on a page authored against a measured vertical budget it spends that
budget showing one tile twice. Assigning a display clears the hero; setting a
hero on a page that already has a display is refused.
