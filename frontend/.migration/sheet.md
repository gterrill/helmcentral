# sheet

2026-07-06, transformation engine (legacy `default` style, no `base-default`
registry counterpart; hand-transform against `overlays.md`'s dialog section,
since Base UI's Dialog is the underlying primitive for shadcn's Sheet).
Verdict: migrated to Backdrop/Popup; one dead-class removal and one
already-dead selector removed (see below), both flagged, not silently kept.

## Changed

- `src/components/ui/sheet.tsx`:
  - Import: `import * as SheetPrimitive from "@radix-ui/react-dialog"` ->
    `import { Dialog as SheetPrimitive } from "@base-ui/react/dialog"`, per
    `overlays.md`'s dialog part mapping (Sheet is a shadcn skin over Radix's
    Dialog/Base UI's Dialog, not a distinct primitive on either side).
  - `SheetOverlay`: `SheetPrimitive.Overlay` -> `SheetPrimitive.Backdrop`
    (part rename per `universal-patterns.md`). Animation classes
    `data-[state=open]:animate-in data-[state=closed]:animate-out
    data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0` ->
    `transition-opacity data-starting-style:opacity-0
    data-ending-style:opacity-0` per `class-mapping.md`'s animation idiom
    section (Backdrop supports `data-starting-style`/`data-ending-style`
    per `overlays.md`'s dialog data-attribute table).
  - `sheetVariants` (cva): base classes' `data-[state=open]:animate-in
    data-[state=closed]:animate-out data-[state=closed]:duration-300
    data-[state=open]:duration-500` -> `data-ending-style:duration-300
    data-starting-style:duration-500` (duration values preserved exactly).
    Per-side slide classes rewritten from
    `data-[state=closed]:slide-out-to-<side> data-[state=open]:slide-in-from-<side>`
    to explicit `data-ending-style:<translate>` /
    `data-starting-style:<translate>` pairs (both directions use the SAME
    translate transform for a given side, since Base UI's transition model
    animates FROM the starting/ending transform TO the resting `transform:
    none`, unlike radix's separate in/out keyframe names).
  - `SheetContent`: `SheetPrimitive.Content` -> `SheetPrimitive.Popup`
    (part rename; per `overlays.md`, dialog/sheet centered-or-sliding modals
    use Popup directly with NO Positioner, confirmed by inspecting
    `node_modules/@base-ui/react/dialog/index.parts.d.ts`, which exports no
    Positioner part for Dialog at all).
  - `SheetPrimitive.Close`'s `data-[state=open]:bg-secondary` class DROPPED
    (see Behavior changes: this class was already dead in the radix
    version).
  - `React.ElementRef<X>` / `ComponentPropsWithoutRef<X>` pairs ->
    `React.ComponentRef<X>` / `ComponentProps<X>` throughout (mechanical,
    no behavior change, matches the pattern used for the button/separator/
    tooltip wrappers already migrated in this project).
  - `displayName` set to literal strings since Base UI parts do not carry a
    `.displayName` static the way radix's forwardRef-wrapped parts did.
  - Leftover scan: `grep -n "radix-ui\|@radix-ui" src/components/ui/sheet.tsx`
    -> no matches. Clean.

## Left alone

- None beyond the dead-class removals documented above.

## Behavior changes

- The per-side slide-in/out animation and the backdrop fade were, like
  tooltip.tsx, ALREADY INERT before this migration: this project has no
  `tailwindcss-animate` plugin registered (`tailwind.config.ts` has
  `plugins: []`), so `animate-in`/`animate-out`/`fade-in-0`/`fade-out-0`/
  `slide-in-from-*`/`slide-out-to-*` were unrecognized, no-op Tailwind
  classes on both the backdrop and the sliding panel. The new
  `data-starting-style`/`data-ending-style` + `transition-transform`/
  `transition-opacity` classes are REAL, WORKING CSS transitions using
  Base UI's own presence-transition attributes (no plugin dependency).
  This is a behavior IMPROVEMENT (previously-broken enter/exit animation
  now animates), flagged because the sheet will now visibly slide and fade
  where before it snapped open/closed instantly.
- Dropped `data-[state=open]:bg-secondary` on the Close button (kept as
  `data-popup-open:bg-secondary` was considered and REJECTED): per
  `overlays.md`'s dialog data-attribute table, `data-open`/`data-closed`
  presence attributes are documented only for Backdrop/Popup/Viewport, and
  `data-popup-open` is documented only for Trigger — Base UI's `Close` part
  gets neither. Radix's own `Dialog.Close` never set `data-state` on itself
  either (only `Content`/`Overlay` did), so `data-[state=open]:bg-secondary`
  on `Close` never matched anything and was dead CSS in the original file
  too. No behavior change for end users (the class never fired), but noting
  it here since dead-code removal during a migration is not "silent
  patching" of a real behavior — there was no real behavior to preserve.

## Verify by hand

- Open the mobile sidebar (narrow viewport) via the sidebar trigger: confirm
  the sheet slides in from the left (default `side="left"` inherited from
  `Sidebar`'s prop) with a visible backdrop fade, and slides back out on
  close (Close button, Escape key, or outside click).
- Confirm the close button (X icon, top-right) still has an accessible
  `sr-only` "Close" label and responds to hover/focus states with the
  existing opacity classes.
- Confirm focus returns to the sidebar trigger button after closing the
  sheet (Base UI Dialog's default `finalFocus` behavior, matching Radix's
  `onCloseAutoFocus` default of returning focus to the trigger).
