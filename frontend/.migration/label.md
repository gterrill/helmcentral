# label

2026-07-06, transformation engine (legacy `default` style, no `base-default`
registry counterpart; scaffolded via `npx shadcn@latest add label`, which
still emits a `radix-ui` Label import — hand-transformed to a plain semantic
element per `@base-ui/react`'s own gap: it ships no dedicated `label`
sub-package at all, unlike `select`/`switch`/`field`/`fieldset`). Verdict:
Radix/`radix-ui` dependency dropped entirely, no primitive replaces it — a
native `<label>` was already sufficient.

## Changed

- `src/components/ui/label.tsx`:
  - Import: `import { Label as LabelPrimitive } from "radix-ui"` -> removed
    entirely, no primitive import needed.
  - `<LabelPrimitive.Root ref={ref} className={...} {...props} />` -> plain
    `<label ref={ref} data-slot="label" className={...} {...props} />`.
    Radix's `Label.Root` added no behavior beyond a styled native `<label>`
    with peer-disabled data-attribute hooks (confirmed: no click-to-focus
    logic, no ARIA wiring beyond what `<label>` gives for free via `htmlFor`/
    nesting) — dropping it is a like-for-like swap, not a capability loss.
  - `React.ElementRef<typeof LabelPrimitive.Root>` /
    `React.ComponentPropsWithoutRef<typeof LabelPrimitive.Root>` ->
    `HTMLLabelElement` / `React.ComponentProps<"label">`, dropping the
    `VariantProps<typeof labelVariants>` primitive type reference (variants
    themselves unchanged — `labelVariants` cva call kept verbatim, same
    single "true" variant with no options).
  - `Label.displayName = LabelPrimitive.Root.displayName` -> literal string
    `"Label"` (no primitive to read a displayName off).
  - Added `data-slot="label"` to match this project's other post-migration
    primitives (`field.tsx`'s `FieldLabel` and other `data-slot`-tagged
    parts already assume a `data-slot="label"` marker exists on the base
    `Label` component it wraps).
  - Leftover scan: `grep -n "radix-ui\|@radix-ui" src/components/ui/label.tsx`
    -> no matches. Clean.

## Left alone

- `labelVariants` cva definition and its single class string
  (`text-sm font-medium leading-none peer-disabled:cursor-not-allowed
  peer-disabled:opacity-70`) — unchanged, no Radix/Base UI dependency ever
  existed in these classes.

## Behavior changes

- None. A plain `<label>` element has the same default browser behavior
  Radix's `Label.Root` relied on (click focuses the associated control via
  `htmlFor` or DOM nesting); Radix's implementation was a thin styling
  wrapper with no extra JS behavior to lose.

## Verify by hand

- Click any settings panel field label (e.g. "Address" in SignalK
  Connection) and confirm it focuses the paired `Input`/`Select` (native
  `<label htmlFor>` behavior, wired via `FieldLabel`'s `htmlFor` prop at
  each call site).
- Confirm labels next to disabled controls still dim via
  `peer-disabled:opacity-70` (no settings-panel field is currently disabled,
  but the class is exercised by other `peer`-paired consumers of `Input`
  elsewhere in the app).
