# ADR 0060: The Skin Belongs to the Page, Not the Tile

## Status
Accepted

Supersedes the scope decision in ADR 0054 §5. The rest of §5, which is what made this change cheap, still stands.

## Context

ADR 0054 §5 shipped the instrument skin as a per-tile setting: `EngineClusterConfig.skin`, read by the engine cluster tile, which wrapped itself in a div carrying `data-skin="instrument"`. The reasoning was that a real MFD is dark whatever time it is, so the look should not be tied to the app theme.

That part was right. The scope was wrong, and living with it made three things obvious.

**A page is already a mode.** The pages on this boat are called Anchored, Underway and Cluster preview. They exist because what you want to look at depends on what the boat is doing. Night-helm-dark is a property of that situation, not of one widget in it. Building a night page meant setting the skin on every cluster by hand and remembering to set it on anything added later.

**Two tiles on one page could disagree.** Nothing prevented it and nothing flagged it. The only place that was ever useful was the preview page, where a skinned and an unskinned cluster sat side by side on purpose.

**Nineteen other tile kinds could not have the look at all.** The skin field existed on exactly one widget config. Lamps, gauges, tanks and battery all consume the same tokens and all rendered in the app theme regardless.

And the seam. A dark cluster on a light page reads as a mis-styled widget rather than as an instrument, which is the opposite of what the skin was for.

## Decision

### 1. The attribute moves to the grid container, and nothing else changes

`data-skin` now sits on the dashboard grid's root element and is driven by `DashboardPage.skin`. Every tile beneath it re-skins.

The entire rendering change is that one attribute moving up the tree, and this is ADR 0054 §5 paying off rather than luck. `[data-skin="instrument"]` is an attribute-only selector that redefines the same token names every component already consumes, and no component branches on the skin value. A selector like that has no opinion about how much of the tree it covers, so widening the scope is a DOM move and not a refactor.

The header, breadcrumb, page switcher and alarm banner stay on the app theme. Taking the skin to the document root instead would have made it a third theme competing with the dark-mode toggle, and left the toggle doing nothing visible on a skinned page. The cost of stopping at the grid is a visible seam where the board meets the header, which was checked by eye rather than assumed acceptable.

Portalled content is out of reach either way. Dialogs, popovers and tooltips all attach to `body`, and custom properties inherit down the DOM tree, so the cluster config dialog was unskinned before this change and is unskinned after it. Only the document-root option would have reached them, and that is not the option taken.

### 2. There is no per-tile override

Custom-property inheritance makes an override look free: nest a `data-skin="default"` inside the skinned subtree and the inner value wins. It is not free. That block has to re-declare the light theme's values, and a second block under `.dark` has to re-declare the dark theme's, because there is no way to say "revert to whatever the theme says" for a custom property. Two hand-maintained token lists that must track `:root` and `.dark` forever.

This codebase has already been bitten once by exactly that: `--card-foreground` was left out of the skin, fell through to the light theme, and rendered the tile's config gear in near-black on a near-black card, where it looked absent rather than broken. Building two more lists that can drift the same way to serve one comparison view is a bad trade. The comparison view becomes two pages.

### 3. `--destructive` joins the skin, which refines §5's alarm rule rather than reversing it

ADR 0054 §5 deliberately left alert red and amber out of the skin, on the grounds that a skin which could recolour an alarm state would be a skin that could hide one. That still holds.

But omitting a token does not freeze it, it delegates it. At tile scope the omission was harmless, because alarm text sat on cards inside a page whose theme still governed the ground. At page scope the ground is the skin's, and `--destructive` falls through to `:root`'s `359 75% 50%`, which is the red tuned for a light background, now on a dark board. `.dark` carries `359 100% 70%` precisely because a dark ground needs the brighter one.

So the skin now sets the dark-mode pair. Same hue, still unmistakably an alarm, brightened only for the ground it now sits on. The rule was never "the skin may not name a red", it was "the skin may not make a red stop reading as an alarm", and a red that is too dim to see against its own background breaks that rule rather than obeying it.

### 4. What did not need widening, and one gap left open

The audit of what falls through at page scope came back much smaller than expected, for a reason worth recording: **all eight `--sidebar-*` tokens are `var()` indirections** to `--card`, `--foreground`, `--primary`, `--border` and `--secondary`. Indirections resolve at use time, so they follow the skin with no declaration of their own. `--radius` and the two font tokens are not colour and are correct as inherited.

The six `--chart-*` tokens are a real gap and are left open. Only the forecast drawer and the tide chart read them, and neither renders on the bento grid, so nothing is wrong today. If a charting tile is ever added to a page it will draw light-theme series colours on a dark board. The coverage test names them in its allowlist with that reason attached, so the gap is recorded where someone will trip over it rather than in prose nobody rereads.

### 5. The coverage test now guards the whole vocabulary

ADR 0054's guard asserted that every `*-foreground` token in `:root` was redefined in the skin. That caught the specific bug that motivated it and nothing else.

It now asserts that **every token in `:root` whose value is not a `var()` indirection** is redefined in the skin, minus an allowlist where each entry carries its reason. That is what turned an audit into a test. `--destructive` and `--destructive-foreground` were found by writing the widened test and watching it fail, not by reading the stylesheet.

### 6. Board chrome is a token, because a component must not branch on the skin

The skinned page paints its own ground, which needs padding and a corner radius that the unskinned page must not have. The obvious implementation is a conditional class on the skin value, and it is the one thing this whole design exists to avoid.

So `--board-pad` and `--board-radius` join the chrome vocabulary that ADR 0054 §5 started with `--dial-bezel-overhang`, inert at `0px` in `:root` and set by the skin. The container's `bg-background` needs no guard at all: it is exactly what `body` already paints, in both themes, so it is a no-op until a skin redefines `--background` above it.

### 7. No migration, and the field-by-field rebuild that nearly ate the setting

The backend does not use `DisallowUnknownFields`, so removing `Skin` from the cluster config makes the stale `cluster.skin` still sitting in the saved pages file ignored on load and never written back. One operator, no installed base, no migration code.

The one genuinely dangerous line was in the page PATCH handler, which rebuilds the page from the current one field by field before applying the patch. A new field omitted there is not a compile error and not a validation failure. It is wiped on the next layout drag, silently, because dragging a tile sends a widgets-only patch. That got its own test before the field was added: set a skin, patch widgets only, assert the skin survived.

## Consequences

The instrument look is now available to every tile kind rather than to clusters only, which was never a design goal and is the main thing that makes the change worth doing.

Raw Tailwind palette classes do not follow the skin and cannot be made to. There are 44 of them across 15 grid tiles, 16 tuned for a light ground. AGENTS.md reserves the raw palette for alert semantics, so they are correct as written and outside the token system on purpose. The bound on the exposure is dark mode: the skin's ground is `222 47% 8%` against `.dark`'s black, so anything already legible in dark mode is legible here, and the only tiles at risk are ones nobody ever looked at in dark. They were screenshotted under the skin rather than rewritten pre-emptively.

The Cluster preview page loses its side-by-side. That was the one place a per-tile skin earned its keep, and it is now two pages.

The skin control leaves the cluster config dialog and lands in the page switcher popover, which until now did nothing but create, rename, delete and select. It is the first page-level property this app has, and if a second one arrives the popover is the wrong shape for it and a real page-settings dialog will be worth building.
