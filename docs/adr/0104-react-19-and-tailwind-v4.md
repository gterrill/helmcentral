# ADR 0104: React 19 and Tailwind v4, for shadcn's Chat Components

## Status

Accepted.

## Context

Mate's answer arrived as one whole `message` event, after a wait with only a
status line ("Thinking…") on screen. Streaming that answer in as it's
written (ADR 0105) meant replacing the hand-rolled thread in
`assistant-thread.tsx` with shadcn's chat components: `MessageScroller`,
`Message`, `Bubble`, `Marker`. Evaluating those components against this
codebase found they don't drop in as they ship:

- `MessageScroller` is built on `@shadcn/react` (peer `react >=19`), and its
  compiled output passes `ref` as a plain prop rather than the second
  argument to `forwardRef`. React 18 strips an unrecognised `ref` prop
  silently, so on 18 the scroller mounts but never actually attaches to its
  DOM nodes: no error, just a scroller that can't scroll.
- All four components are written in Tailwind v4 syntax:
  `wrap-break-word`, `data-autoscrolling:`, `inset-s-*`, `scroll-fade-b`,
  `ring-3`. None of these compile under Tailwind 3.4.19, which the project
  was still on.

Hand-porting the four files to v3 syntax and a React 18-safe ref pattern was
the alternative, and it was rejected: it would mean maintaining a permanent
fork of vendor code every time shadcn shipped a fix, for a set of components
this project wants to keep pulling `shadcn add` updates for. Upgrading the
two toolchain pieces once, cleanly, was judged cheaper than forking four
files forever.

The two upgrades were done as their own commits (`d41137f` React 19,
`b9f7c26` Tailwind v4, CSS-first) ahead of adding the components themselves
(ADR 0105), so each could be verified in isolation against the existing
dashboard before any new component touched the tree.

### The wall's browser had to clear the CSS floor first

Tailwind v4's generated CSS needs `@layer`, `@property` and `color-mix()`:
Safari 16.4 territory. The kiosk wall runs WPE WebKit, and the last recorded
version in this repo was 2.38.5 (Safari 16.0), which would blank the wall
outright if the CSS floor moved out from under it without checking first.
The live version was confirmed at WPE 2.44.1 (UA `Version/17.0`) before
Tailwind v4 work started, comfortably past the floor.

`frontend/public/kiosk-probe.html` (the page `/kiosk` loads to self-report
whether the browser can render the dashboard at all) gained checks for
`@layer`, `@property` and `color-mix()` alongside its existing `:has()` and
lookbehind-regex checks, so a future browser regression on the wall reports
itself the same explicit way a missing `:has()` support already did, rather
than as a silently blank display.

## Decision

### React 18.3 to 19.2

`react`, `react-dom` and their `@types` packages moved to 19. An `overrides`
entry pins `react-is` to `^19.2`, because `recharts` 2.15.4 bundles its own
`react-is` 18.3.1, which misreads a React 19 element's internal shape
without the override forcing the newer copy.

The survey found no removed API in use anywhere in the app, and every
React-peer dependency already declared support for 19. Two call sites
changed as straightforward tidy-ups rather than because anything broke:
`ui/sidebar.tsx`'s three `React.ElementRef` uses became `React.ComponentRef`
(the former is deprecated in the 19 types), and `App.tsx`'s
`{...({ popover: 'manual' } as any)}` cast came out because `@types/react`
19 declares the `popover` attribute directly. A few code comments that
blamed "React 19" for unmounting the root on an uncaught render error were
corrected to say React in general, which has done this since 16.

`forwardRef` components were left exactly as they were. React 19 still
supports the pattern; only the four new shadcn files (function components
that take `ref` as an ordinary prop, the newer style `@shadcn/react` itself
uses) needed the version bump at all.

Console-spy assertions in five test files were flagged ahead of time as
exact-count checks that could trip on a new dev-only warning React 19
introduces (`anchor-watch-map-ui.test.tsx`, `route-planner-map-ui.test.tsx`,
`map-place-labels.test.tsx`, `use-vessel-identity.test.ts`); none of them
did.

### Tailwind 3.4 to v4, CSS-first

`tailwindcss@4` and `@tailwindcss/vite` replaced the PostCSS pipeline
(`postcss.config.js` and `autoprefixer` removed; v4 ships its own
vendor-prefixing and content scanning). `tailwind-merge` moved to `^3` for
the matching major.

`@tailwindcss/upgrade` ran first on a clean tree, then its diff was reviewed
by hand rather than trusted outright, because two of its renames are lossy
outside plain Tailwind class strings: `bg-[--x]` → `bg-(--x)` and
`theme(spacing.4)` → `--spacing(4)` don't fire inside a TS template literal
or a hand-built class string the way `lib/severity.ts` and
`assistant-markdown-impl.tsx` build theirs, so those two files were checked
by hand for the same renames the automated pass made everywhere else
(`shadow-sm`→`shadow-xs`, `rounded-sm`→`rounded-xs`, `outline-none`→
`outline-hidden`, `backdrop-blur`→`backdrop-blur-sm`, `flex-shrink-0`→
`shrink-0`, `!p-0`→`p-0!`, and 14 sites of `bg-[--x]`→`bg-(--x)`).

`tailwind.config.ts` is gone. `src/index.css` now opens with
`@import "tailwindcss"`, keeps `@custom-variant dark (&:is(.dark *))`
unchanged, and adds `@import "shadcn/tailwind.css"`: the source of the
`scroll-fade-*` and `shimmer` utilities the chat components use (ADR 0105),
imported in this commit specifically so that one didn't have to touch
`index.css` again.

Tokens stay HSL channel triples. The plain `:root {`, `.dark {` and
`[data-skin="instrument"] {` blocks, and every `hsl(var(--x))` reference
across the TypeScript source (157 call sites) and several tests that slice
`index.css` by those exact selector strings (`forecast-drawer`,
`engine-cluster-tile`, `dial-ring`, `fuel-rail`), were left exactly as they
were. Moving to Tailwind v4's own preferred `oklch()` tokens was explicitly
out of scope for this change (see Context in the streaming ADR). This
migration's job was the toolchain, not the colour space. An `@theme inline`
block maps every one of these HSL-channel custom properties to the
`--color-*` name Tailwind's utilities expect (`--color-primary:
hsl(var(--primary))`, and the same for `gauge-*`, `chart-*` and
`sidebar-*`), plus `--font-display`, `--font-sans`, and `--text-2xs` with
its own line height. `inline` matters here: without it, Tailwind would
synthesize its own separate `--color-primary` custom property holding the
same indirection, a second name for the same value that anything still
reading `--primary` directly (charts, SVG fills) would silently miss.

### The pixel-parity approach: same-moment dual-tree captures

The acceptance bar was zero visible change on every existing page. That was
checked by rendering the old (pre-migration) and new trees side by side at
the same moment, across 25 routes and four viewports (dark mode and the
instrument skin included), rather than screenshotting one tree, merging,
and screenshotting the other later; a same-moment capture rules out a
live-data widget (wind, tide, engine RPM) having simply moved between the
two screenshots and being misread as a CSS regression.

Three things needed deliberate holding in place to pass that bar:

- **Radius mapping.** v3's `rounded-sm` became v4's `rounded-xs`, and v3's
  bare `rounded` became v4's `rounded-sm` (the automated upgrade's own
  renames, applied everywhere they appeared). For the rendered pixels to
  match, `--radius-xs` in `@theme` is `calc(var(--radius) - 4px)` (v3's old
  `rounded-sm`), `--radius-sm` is a flat `0.25rem` (v3's old bare
  `rounded`), and `--radius-md`/`--radius-lg` stay `calc(var(--radius) -
  2px)`/`var(--radius)` as before. `--radius-xl` is `var(--radius) * 2` and
  `--radius-2xl` is `var(--radius) * 3`, both new names v3 never had, added
  so a `rounded-xl`/`rounded-2xl` class (tiles and cards already used
  `rounded-xl`) keeps resolving.
- **Text line heights.** v4 gives `text-xs` through `text-4xl` a unitless
  line-height ratio by default; v3's were absolute. Anywhere text set its
  own size and inherited v3's absolute line height (log rows, alarm rows,
  tile readouts) got visibly taller under v4's ratio. `@theme` restores
  v3's absolute values explicitly (`--text-xs--line-height: 1rem` through
  `--text-4xl--line-height: 2.5rem`), so nothing downstream had to change
  to get the old rendering back.
- **`leading-none` now wins.** v3 let a responsive text-size utility
  override a `leading-none` set earlier; v4 honours `leading-none`
  regardless of what comes after it in the cascade. The battery and wind
  tiles, which relied on the old override behaviour, now pin the line
  height they have always visually rendered with directly, rather than
  depending on the override.

One incidental fix fell out of the same pass: `field.tsx`'s
`FieldDescription` carried a `nth-last-2:-mt-1` utility that had never
actually compiled under v3 (the selector syntax is v4-only) and would have
started applying under v4, pulling the security page's description text up
4px from where it had always rendered. It was removed rather than kept
working, since the 4px shift was never an intended part of that page's
layout.

### Compat rules kept in `@layer base`

A few v3 behaviours the app depends on needed an explicit rule to survive
v4's stricter cascade-layer semantics, since native `@layer` now makes
unlayered CSS beat layered utilities where v3's emulated layers didn't:
`* { border-color: var(--color-border) }` (covers roughly 160 bare `border`
utilities that never named a colour), a `muted-foreground` placeholder
colour, and `button:not(:disabled), [role="button"]:not(:disabled) {
cursor: pointer }`. Three existing rule blocks that need to keep beating
utility classes (the `.maplibregl-ctrl-attrib*` overrides,
`dashboard-bento-grid.css`, and `.anchor-watch-toast`) moved into `@layer
components` for the same reason.

### `hover:` now only fires on hover-capable devices

Tailwind v4 changed `hover:` to be scoped to `@media (hover: hover)`
automatically; v3's `hover:` fired on any pointer, including a touch tap.
This is accepted as a genuine improvement rather than worked around: on the
iPad and the touch wall, a `hover:` style used to visibly "stick" after a
tap until the next touch moved focus elsewhere, and v4's behaviour removes
that for every `hover:` class in the app with no per-component change
needed.

### `components.json` describes a Base UI, Tailwind v4 project

`"tailwind": { "config": "", "css": "src/index.css", ... }` points the CLI
at the CSS file directly rather than a JS config that no longer exists.
`"style": "base-nova"` and `"iconLibrary": "lucide"` match what this project
already uses (Base UI primitives, lucide icons), and `ui`/`lib`/`hooks`
aliases were added so `shadcn add` resolves imports against this project's
actual directory layout. `npx shadcn@latest info` confirms the CLI resolves
Base UI and Tailwind v4 correctly against this config.

## Known, accepted gap

Bubble's `secondary`, `muted` and `tinted` variants (ADR 0105) style a
`button`/`a` bubble's hover state with `color-mix()`/`oklch(from …)`
expressions that read `var(--secondary)` etc. directly, which, under this
project's HSL-channel-triple tokens (not the raw OKLCH values these CSS
functions expect), don't compute a sensible colour. This is accepted rather
than fixed: Helmcentral never renders a `Bubble` as a `button` or `a` (every
bubble in `assistant-thread.tsx` is a plain content block), so the broken
expression never actually executes in this app. Moving the token system to
OKLCH to make it correct in the general case was explicitly ruled out of
scope for this migration (see the streaming ADR's Context) and remains a
possible future change, not a defect this ADR needed to close.

## Consequences

- Every dependency accepting `react-is` 18 or 19 keeps working; the
  `overrides` entry only forces the one nested copy (`recharts`) that would
  otherwise misread a React 19 element.
- `tailwind.config.ts` no longer exists. Anything that used to read theme
  values from it at build time (the removed file's own consumers, plus
  `forecast-drawer.test.tsx`'s former regex read of it) now reads the
  `@theme` block in `index.css` instead.
- The CSS floor for this app is now Safari 16.4 (`@layer`, `@property`,
  `color-mix()`), tighter than the Baseline-2024 lazy-code floor of Safari
  16.4 already recorded in ADR 0046: the two floors happen to coincide, so
  this migration didn't move the floor, it just made Tailwind's own CSS
  meet a floor that was already the project's stated target.
- `hover:` styles across the whole app now only engage on a device that
  actually has a hover-capable pointer, with no code change required
  per-component.
- shadcn's own chat components (`message-scroller.tsx`, `message.tsx`,
  `bubble.tsx`, `marker.tsx`, ADR 0105) can now be added with `shadcn add`
  and kept unmodified, rather than hand-forked to older syntax; that was the
  originating reason for this migration.

## Related

- ADR 0046 (Vite 8 / Baseline 2024 CSS floor): the Safari 16.4 floor this
  migration's own CSS requirement happens to match.
- ADR 0089 (kiosk feed is a page flag): `kiosk-probe.html`'s existing
  `:has()`/lookbehind checks, which the new `@layer`/`@property`/
  `color-mix()` checks sit alongside.
- ADR 0105 (Mate streams its answer): the feature this migration exists to
  unblock, and the source of the Bubble hover-style gap noted above.
