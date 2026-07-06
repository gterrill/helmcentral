# switch

2026-07-06, transformation engine (legacy `default` style, no `base-default`
registry counterpart; scaffolded via `npx shadcn@latest add switch`, which
emits Radix-based source per `@radix-ui/react-switch` — hand-transformed
against `@base-ui/react/switch`'s `.d.ts` files, confirmed directly rather
than assumed). Verdict: two-part `Root`/`Thumb` shape carried over 1:1
structurally; only the state-attribute idiom changed (presence vs.
value-pair), the exact bug class already caught once in this project's
`sheet.tsx`/`tooltip.tsx` migrations.

## Changed

- `src/components/ui/switch.tsx`:
  - Import: `import * as SwitchPrimitives from "@radix-ui/react-switch"` ->
    `import { Switch as SwitchPrimitives } from "@base-ui/react/switch"`.
  - `SwitchPrimitives.Root`/`SwitchPrimitives.Thumb` part names unchanged
    (confirmed against `node_modules/@base-ui/react/switch/index.parts.d.ts`
    -> `Root`/`Thumb`, same as Radix).
  - State-attribute rewrite (confirmed against
    `node_modules/@base-ui/react/switch/root/SwitchRoot.d.ts`'s
    `SwitchRootState` -> `checked: boolean` surfaces as presence attributes
    `data-checked`/`data-unchecked`, NOT Radix's `data-state="checked"`
    value-pair):
    - Root: `data-[state=checked]:bg-primary data-[state=unchecked]:bg-input`
      -> `data-[checked]:bg-primary data-[unchecked]:bg-input`.
    - Thumb: `data-[state=checked]:translate-x-5
      data-[state=unchecked]:translate-x-0` -> `data-[checked]:translate-x-5
      data-[unchecked]:translate-x-0`.
  - `React.ElementRef<X>`/`ComponentPropsWithoutRef<X>` ->
    `React.ComponentRef<X>`/`React.ComponentProps<X>` (mechanical, matches
    the pattern used for button/separator/tooltip/sheet wrappers already
    migrated in this project).
  - `Switch.displayName = SwitchPrimitives.Root.displayName` -> literal
    string `"Switch"` (Base UI parts carry no `.displayName` static).
  - Leftover scan: `grep -n "radix-ui\|@radix-ui" src/components/ui/switch.tsx`
    -> no matches. Clean.
- `src/components/signalk-settings-panel.tsx` (consumer, migrated in the same
  effort): two native `<input type="checkbox">` toggles (tide-provider's
  "Auto-update tide station" and Anchor Watch Options' "Auto-close anchor
  watch") replaced with `Switch` wrapped in `Field orientation="horizontal"`
  + `FieldLabel`. `onCheckedChange` gives a raw `boolean` directly, simpler
  than the previous `(e) => onChange(e.target.checked)` handlers — call
  sites updated accordingly, no state-shape change (`tideAutoStation` /
  `autoCloseAnchorWatchEnabled` remain plain booleans).

## Left alone

- The overall two-part `border-2 border-transparent` track + circular thumb
  visual design — unchanged, no Radix-specific styling depended on it.

## Behavior changes

- None expected. Base UI's Switch is a controlled/uncontrolled toggle with
  the same `checked`/`onCheckedChange` prop names as Radix's
  `checked`/`onCheckedChange`, confirmed against `SwitchRootProps` in
  `SwitchRoot.d.ts` — this is a drop-in prop-compatible swap for how this
  project already used it (no `defaultChecked`, `disabled`, or `name` props
  were in use pre-migration).

## Verify by hand

- Settings panel, Tide section: select the `bom` tide provider, confirm the
  "Auto-update tide station as vessel moves" Switch appears, toggles
  visually (thumb slides right, track turns primary-colored) and updates
  `tideAutoStation` state (persisted via Save Settings).
- Settings panel, Anchor Watch Options section: toggle "Auto-close anchor
  watch when engines start" and confirm it round-trips through
  `onAutoCloseAnchorWatchToggle` up to `App.tsx` exactly as the old
  checkbox did.
- Tab-focus a Switch and confirm the focus ring (`focus-visible:ring-2`)
  still appears, and disabled state (not currently exercised in this panel)
  would still dim to `opacity-50` per the untouched `disabled:` classes.
