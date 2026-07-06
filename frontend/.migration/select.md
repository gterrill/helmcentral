# select

2026-07-06, transformation engine (legacy `default` style, no `base-default`
registry counterpart; scaffolded via `npx shadcn@latest add select`, which
emits Radix-based source per `@radix-ui/react-select` — hand-transformed
against `@base-ui/react/select`'s `.d.ts` files, confirmed directly rather
than assumed, same methodology as `sheet.tsx`/`tooltip.tsx`). Verdict:
migrated to the Root > Trigger > Value/Icon plus Portal > Positioner > Popup
> List > Item model; scroll buttons dropped (not needed, every dropdown in
this app has 2-4 items); one REAL dead-CSS bug found and fixed during
verification (`bg-accent`/`text-accent-foreground` reference color tokens
that don't exist in this project's Tailwind config at all — not just a
presence/value-pair syntax issue), caught by the mandated compiled-CSS grep
step, not by visual inspection.

## Changed

- `src/components/ui/select.tsx`:
  - Import: `import * as SelectPrimitive from "@radix-ui/react-select"` ->
    `import { Select as SelectPrimitive } from "@base-ui/react/select"`.
  - Structural parts, confirmed against
    `node_modules/@base-ui/react/select/index.parts.d.ts`:
    - `Select`/`SelectGroup`/`SelectValue` — same `Root`/`Group`/`Value`
      names as Radix, `value`/`onValueChange` prop names unchanged (per
      `SelectRoot.d.ts`).
    - `SelectTrigger` — same `Trigger` name; unchanged structurally, still
      renders children + a trailing `Select.Icon`.
    - `SelectPrimitive.Icon asChild><ChevronDown /></SelectPrimitive.Icon>`
      -> `<SelectPrimitive.Icon><ChevronDown /></SelectPrimitive.Icon>`
      (Base UI's `Select.Icon` renders its own `<span>` wrapper directly, no
      `asChild` prop exists on it per `SelectIcon.d.ts` — it always renders
      an element, so the child icon nests inside rather than replacing it).
    - `SelectContent` (Portal > Content > Viewport, scroll buttons) split
      into `SelectPopup` (Portal > Positioner > Popup > List), matching the
      Portal/Positioner/Popup shape already established in this project's
      `tooltip.tsx`. `Select.List` (per
      `node_modules/@base-ui/react/select/list/SelectList.d.ts`) replaces
      Radix's `Viewport` as the direct children container inside `Popup`.
    - `SelectLabel` -> `SelectGroupLabel` (renamed part, per
      `SelectGroupLabel.d.ts` — Base UI calls this `GroupLabel`, not
      `Label`, to avoid clashing with the standalone `label.tsx` component).
    - `SelectItem`/`SelectItemIndicator`/`SelectItemText`/`SelectSeparator`
      — same names, same nesting shape as the Radix scaffold.
    - DROPPED `SelectScrollUpButton`/`SelectScrollDownButton` entirely (not
      ported under new names) — every dropdown in `signalk-settings-panel.tsx`
      has 2-4 static options (Units, Tide Provider, Hull Type, Theme), never
      needs scroll affordances. Base UI's equivalent parts
      (`ScrollUpArrow`/`ScrollDownArrow`) exist but were intentionally not
      wired up.
    - DROPPED the `position="popper"` prop and its associated
      `data-[side=...]:translate-*` / `[--radix-select-trigger-height]` /
      `[--radix-select-trigger-width]` classes — Base UI's `Positioner`
      always handles anchor-relative placement itself (no `position` prop
      exists on `SelectPositioner` per its `.d.ts`); this concept doesn't
      carry over, it's not a rename.
  - State-attribute rewrite (confirmed against each part's `.d.ts` state
    interface rather than assumed, per the project's established gotcha —
    Base UI uses presence attributes, Tailwind 3.4 requires bracket syntax
    for those):
    - `SelectTrigger`/`SelectValue`: `data-[placeholder]:text-muted-foreground`
      — kept the SAME bracket syntax the Radix scaffold already used (Radix's
      `data-placeholder` was already presence-only, so no rewrite was
      needed there). Moved the class from `Trigger` onto `Value` specifically:
      checked `SelectTriggerState`/`SelectValueState` in `SelectTrigger.d.ts`/
      `SelectValue.d.ts` and found BOTH parts carry a `placeholder: boolean`
      state field, meaning both receive `data-placeholder` at runtime — since
      either works, `Value` was chosen (matches shadcn's upstream Base UI
      registry convention of styling the value text itself, and keeps
      `Trigger`'s own class list focused on the button chrome, not content
      state).
    - `SelectItem`: `data-[disabled]:pointer-events-none
      data-[disabled]:opacity-50` — unchanged bracket syntax (Radix's
      `data-disabled` was already presence-only). `focus:bg-accent
      focus:text-accent-foreground` -> `data-[highlighted]:bg-accent
      data-[highlighted]:text-accent-foreground` initially (matching the
      brief's mapping table), THEN discovered via the mandated compiled-CSS
      grep step that this produced ZERO CSS output — not a bracket-syntax
      problem, but because `bg-accent`/`text-accent-foreground` reference
      Tailwind color tokens (`accent`/`accent-foreground`) that this
      project's `tailwind.config.ts` never defines at the top level (only
      `sidebar.accent`/`sidebar.accent-foreground` exist, nested under the
      `sidebar` key) — confirmed via
      `grep -n "accent" tailwind.config.ts src/index.css` and via isolated
      Tailwind CLI repro (`bg-red-500` compiled fine with the same
      `data-[highlighted]:` variant, `bg-accent` did not, in the same file).
      This was DEAD CSS already in the pre-migration Radix scaffold too
      (never rendered a highlight), same class of issue as the `bg-secondary`
      dead class found during the `sheet.tsx` migration. Fixed by swapping to
      `data-[highlighted]:bg-muted` (dropping the foreground swap — no
      `-foreground` pairing is used with `muted` elsewhere in this project,
      e.g. `button.tsx`'s ghost/outline variants use bare `hover:bg-muted`),
      matching this project's actual convention for a neutral hover/highlight
      background rather than porting a token that doesn't exist here.
    - `SelectPopup`/`SelectPositioner`: replaced
      `data-[state=open]:animate-in data-[state=closed]:animate-out
      data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0
      data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95` with
      `transition-[opacity,transform] data-[starting-style]:opacity-0
      data-[starting-style]:scale-95 data-[ending-style]:opacity-0
      data-[ending-style]:scale-95`, same idiom as `tooltip.tsx`.
    - DROPPED the per-side `data-[side=bottom]:slide-in-from-top-2` etc.
      classes entirely, not ported under new names — this project has no
      `tailwindcss-animate` plugin (`tailwind.config.ts` has `plugins: []`),
      so these were already dead/no-op in the Radix scaffold too, same
      reasoning as `tooltip.tsx`/`sheet.tsx`.
    - CSS var: `origin-[--radix-select-content-transform-origin]` ->
      `origin-[--transform-origin]`, and
      `max-h-[--radix-select-content-available-height]` ->
      `max-h-[--available-height]` — both confirmed as the real CSS custom
      properties Base UI's Select positioner sets, via
      `grep -rn "available-height\|transform-origin"
      node_modules/@base-ui/react/select/positioner/SelectPositionerCssVars.js`
      (`--available-height`, `--transform-origin`), not guessed.
    - `Positioner` gets `className="isolate z-50"`, `Popup` keeps `z-50`,
      matching the `tooltip.tsx` convention noted in this project's own
      migration log.
    - `Positioner` also gets `alignItemWithTrigger={false}` — Base UI
      defaults this to `true` (aligns the currently-*selected* item's text
      with the trigger, shifting the whole popup up/down depending on that
      item's position in the list). Found via visual verification, not
      inspection: in this app's dense field-stacked layout, selecting a
      non-first item (e.g. Tide Provider's `bom`, the 2nd of 2 options)
      shifted the popup upward far enough to overlap the `FieldLabel` text
      of the field itself. Disabling it makes the popup always drop straight
      below the trigger — matching the old Radix scaffold's behavior (Radix
      has no such feature) and avoiding the overlap in this layout.
  - `React.ElementRef<X>`/`ComponentPropsWithoutRef<X>` ->
    `React.ComponentRef<X>`/`React.ComponentProps<X>` throughout (mechanical,
    matches the pattern used for other already-migrated wrappers).
  - `displayName` set to literal strings (Base UI parts carry no
    `.displayName` static).
  - Leftover scan: `grep -n "radix-ui\|@radix-ui" src/components/ui/select.tsx`
    -> no matches. Clean.
- `src/components/signalk-settings-panel.tsx` (consumer, migrated in the
  same effort): all four `SettingsSelect` call sites (Units, Tide Provider,
  Hull Type, Theme) replaced with `Field`/`FieldLabel` +
  `Select`/`SelectTrigger`/`SelectValue`/`SelectPopup`/`SelectItem`, same
  `tideProviders.map(...)` data flow for Tide Provider. Each `onValueChange`
  callback wraps the setter (`(value) => value && setX(value)`) because Base
  UI's `Select.Root` types `onValueChange`'s value parameter as nullable
  (`T | null`) to support a "no selection" state that this app's fixed-option
  dropdowns never actually produce, unlike the setters' own state types
  which are non-nullable string unions.

## Left alone

- `SelectGroup`/`SelectGroupLabel`/`SelectSeparator` exported but unused by
  `signalk-settings-panel.tsx` (no dropdown in this panel groups its
  options) — kept in the primitive for future consumers, matching how the
  Radix scaffold also shipped unused-by-this-panel parts.

## Behavior changes

- Popup enter/exit animation is now a real CSS transition (opacity + scale)
  where before, due to the missing `tailwindcss-animate` plugin, it had NO
  animation at all — same behavior IMPROVEMENT already documented for
  `tooltip.tsx`/`sheet.tsx`.
- Highlighted (keyboard/pointer-active) select items now show a visible
  `bg-muted` background — this is a behavior FIX, not just a token rename:
  the pre-migration Radix version's `focus:bg-accent` never fired either
  (Radix's `SelectItem` doesn't receive real DOM focus during arrow-key
  navigation, it tracks a separate highlighted index), so this project's
  select dropdowns have NEVER shown a highlight state, on either primitive,
  until this fix. Flagged as a user-visible improvement introduced
  incidentally by this migration's verification step.

## Verify by hand

- Settings panel, Boat And UI section: open the Units select, confirm
  Metric/Imperial render, arrow-key through them and confirm the active
  option now visibly highlights (`bg-muted`) — this is new, verify it
  doesn't look jarring against the popup's `bg-popover` background.
- Settings panel, Tide section: open Tide Provider, confirm it's populated
  from `useTideProviders()` exactly as before, and confirm selecting `bom`
  still reveals the Auto-update tide station Switch immediately below.
- Settings panel, Anchor section: open Hull Type, confirm the 2-column span
  (`md:col-span-2` on the `Field`) still renders correctly at desktop width.
- Settings panel, Appearance section: open Theme, confirm all three options
  render with their em-dash subtitles and selecting one calls
  `onThemeChange` up to `App.tsx` exactly as before.
- Confirm the select popup closes on outside click, Escape, and item
  selection, and that the trigger regains focus after closing (Base UI
  default `finalFocus` behavior).
