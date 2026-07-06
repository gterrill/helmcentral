# tooltip

2026-07-06, transformation engine (legacy `default` style, no `base-default`
registry counterpart; hand-transform against `overlays.md` and
`wrapper-shapes.md`, preserving the project's classes/tokens). Verdict:
migrated to the Portal > Positioner > Popup model; one dead-CSS cleanup
folded in (see below), one consumer call site fixed in the same commit
since it's the sole consumer.

## Changed

- `src/components/ui/tooltip.tsx`:
  - Import: `import * as TooltipPrimitive from "@radix-ui/react-tooltip"` ->
    `import { Tooltip as TooltipPrimitive } from "@base-ui/react/tooltip"`.
  - `TooltipProvider`, `Tooltip` (Root), `TooltipTrigger` are still bare
    re-exports; no shape change needed for those parts per the mapping
    tables (Provider/Root/Trigger prop names are stable, only their VALUES
    differ at call sites — see sidebar.tsx below).
  - `TooltipContent` restructured from a single `Content` part into
    `Portal > Positioner > Popup`, per `overlays.md`'s tooltip section and
    `universal-patterns.md`'s Portal/positioning model. `side`, `sideOffset`
    (default kept at 4, which was already the project's non-default value —
    radix default is 0, base UI default is 4, this wrapper was already
    passing 4 explicitly so no visual change), `align`, `alignOffset` are
    now destructured and forwarded explicitly to `Positioner` per the
    "Positioner props: Pick means FORWARD" checklist in
    `universal-patterns.md` (declare -> destructure -> forward, all three).
  - Positioner gets `className="isolate z-50"` and Popup keeps `z-50` per
    `wrapper-shapes.md`'s convention ("Tooltip: Popup keeps z-50, Positioner
    gets isolate z-50").
  - CSS var: `origin-[--radix-tooltip-content-transform-origin]` ->
    `origin-[--transform-origin]` per `class-mapping.md`.
  - Animation classes: replaced the radix `animate-in`/`animate-out`/
    `fade-in-0`/`zoom-in-95`/`data-[state=closed]:*` idiom with
    `transition-[opacity,transform] data-starting-style:opacity-0
    data-starting-style:scale-95 data-ending-style:opacity-0
    data-ending-style:scale-95` per `class-mapping.md`'s "Animation idiom"
    section and `universal-patterns.md`'s data-attribute table
    (`data-[state=open]:animate-in` -> `data-starting-style:*` /
    `data-ending-style:*`, transition-based not keyframe-based).
  - DROPPED (not ported) the per-side `data-[side=bottom]:slide-in-from-top-2`
    etc. classes. Investigated first: this project has NO `tailwindcss-animate`
    plugin registered in `tailwind.config.ts` (`plugins: []`), so
    `animate-in`, `fade-in-0`, `zoom-in-95`, and all `slide-in-from-*`
    utilities were already dead/no-op classes in the pre-migration radix
    version — Tailwind has no utility generator for them. Porting dead
    classes forward under new (also-unrecognized) names would not be a
    faithful migration, so they were dropped rather than translated. The
    opacity/scale transition that WAS added is a real, working replacement
    using only plain Tailwind arbitrary values + Base UI's own
    `data-starting-style`/`data-ending-style` attributes (no plugin needed).
  - Leftover scan: `grep -n "radix-ui\|@radix-ui" src/components/ui/tooltip.tsx`
    -> no matches. Clean.
- `src/components/ui/sidebar.tsx` (sole consumer, fixed in the same commit
  since tooltip.tsx cannot typecheck otherwise):
  - Line 138: `<TooltipProvider delayDuration={0}>` ->
    `<TooltipProvider delay={0}>` (rename per `overlays.md`/`consumer-props.md`:
    `delayDuration` -> `delay`). Value `0` preserved exactly (sidebar tooltips
    should show instantly on hover in collapsed mode, same as before).
  - Line 590: `<TooltipTrigger asChild>{button}</TooltipTrigger>` ->
    `<TooltipTrigger render={button} />` (universal `asChild` -> `render`
    rename).

## Left alone

- None beyond the sidebar.tsx call-site fixes above, which were required
  for this component's own typecheck (sidebar.tsx has no direct radix
  import of its own; its full migration report is separate).

## Behavior changes

- Enter/exit animation is now a real CSS transition (opacity + scale) where
  before, due to the missing Tailwind plugin, the tooltip had NO enter/exit
  animation at all (the animate-in/out classes were silently inert). This
  is a behavior IMPROVEMENT (a previously-broken animation now works), but
  is flagged here since it changes the visual feel: tooltips now fade/scale
  in over the default Tailwind transition duration instead of popping
  instantly.
- Default `sideOffset` is 4 on both sides for this wrapper (no change,
  since the wrapper already set `sideOffset = 4` explicitly, matching Base
  UI's default and overriding radix's default of 0).

## Verify by hand

- Hover over a sidebar icon button while the sidebar is collapsed: confirm
  the tooltip appears instantly (`delay={0}` on the Provider) to the right
  of the trigger, with the new fade/scale-in transition, and disappears
  cleanly on mouse-out.
- Confirm the tooltip's `hidden` prop (used when the sidebar is expanded or
  on mobile, `hidden={state !== "collapsed" || isMobile}`) still fully
  suppresses the tooltip in those states — this is a plain HTML `hidden`
  attribute passthrough on both the old and new primitive, unchanged.
- Tab-focus a sidebar menu button and confirm the tooltip also appears on
  keyboard focus (Base UI Trigger supports focus-triggered opens same as
  radix).
