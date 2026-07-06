# button

2026-07-06, transformation engine (legacy `default` style has no `base-default`
registry counterpart, so this is a hand-transform preserving the project's
exact classes, not a golden-pair replay). Verdict: migrated cleanly to the
real `@base-ui/react/button` primitive; both custom variants (`ghost`) and
sizes (`icon`) survive untouched since they live in the wrapper's own `cva`
config, not in the primitive.

## Changed

- `src/components/ui/button.tsx`:
  - Import swapped: `import { Slot } from '@radix-ui/react-slot'` ->
    `import { Button as ButtonPrimitive } from '@base-ui/react/button'`.
  - Removed the hand-rolled `const Comp = asChild ? Slot : 'button'` idiom.
    Per the runbook's hard rule, `button.tsx` must migrate to the REAL
    `@base-ui/react/button` primitive, not a `useRender` wrapper. Base UI's
    `Button` natively accepts a `render` prop, so `asChild` (boolean) callers
    become `render={<X/>}` callers at any call site that used it.
  - `ButtonProps` now extends `React.ComponentProps<typeof ButtonPrimitive>`
    instead of `React.ButtonHTMLAttributes<HTMLButtonElement>` + a manual
    `asChild?: boolean`. This is what makes `render` (and Base UI's
    `nativeButton`, `focusableWhenDisabled`) available to consumers for free.
  - `buttonVariants` (cva config), including the hand-added `ghost` variant
    and `icon` size, is untouched — those live entirely in the wrapper, not
    in the primitive, so they carried over with zero changes.
  - Leftover scan: `grep -n "radix-ui\|@radix-ui" src/components/ui/button.tsx`
    -> no matches. Clean.

## Left alone

- None. This is a single-file wrapper.

## Behavior changes

- Consumers using `asChild` would need to switch to `render` (Base UI prop
  rename, universal across the whole migration). Confirmed via grep that
  no call site in this project (App.tsx, sidebar.tsx, or elsewhere) uses
  `asChild` on `Button`, so there is no actual call-site breakage today —
  noted here only because the prop surface changed.
- Base UI's `Button` renders `<button>` by default same as before; no
  DOM-element change for the default (non-`render`) case.

## Verify by hand

- Click through the app's various `Button` usages (header actions, panel
  toggles, sidebar trigger) and confirm hover/focus-visible ring styling,
  disabled state dimming, and the `ghost`/`icon` variants render identically
  to before (icon-only sidebar trigger button, ghost "back" button).
- Tab to a button and confirm the focus ring (`focus-visible:ring-2`) still
  appears (Base UI Button forwards standard button semantics, no keyboard
  regression expected).
