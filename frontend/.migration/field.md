# field

2026-07-06, transformation engine (legacy `default` style, no `base-default`
registry counterpart; scaffolded via `npx shadcn@latest add field`).
Verdict: no primitive transform needed at all — `field.tsx` has zero
Radix/Base UI imports of its own, only composing this project's already-
migrated `Label` (`label.tsx`) and `Separator` (`separator.tsx`, untouched,
already Base UI-backed from the original migration). Only change: density
overrides to match this app's tight "quietly dense cockpit" convention
(AGENTS.md) instead of shipping shadcn's spacious defaults.

## Changed

- `src/components/ui/field.tsx`:
  - Confirmed on read: imports already resolve to this project's correct
    aliases (`@/components/ui/label`, `@/components/ui/separator`) as
    scaffolded — no import-path fix needed, contrary to what the original
    plan speculated before the CLI scaffold was inspected directly.
  - `FieldSet`'s base gap: `gap-6` -> `gap-2`, matching the settings panel's
    existing `mt-3`/`gap-2` grid density instead of shadcn's default
    spacious `FieldGroup`/`FieldSet` spacing.
  - `FieldGroup`'s base gap: `gap-7` -> `gap-2` (same rationale; `FieldGroup`
    itself ends up unused by the settings panel per the plan's Step 2 — the
    panel keeps its existing manual `grid grid-cols-1 gap-2 md:grid-cols-2`
    wrappers directly around `Field`/`FieldLabel` pairs instead — but the
    override is applied here for consistency in case a future consumer uses
    `FieldGroup` directly).
  - `FieldLegend`'s `variant="label"` styling: was `text-sm` (inheriting the
    shared `mb-3 font-medium` base) -> now additionally carries
    `text-xs font-semibold uppercase tracking-[0.16em] text-muted-foreground`
    on the `data-[variant=label]` selector, matching the exact section-header
    class string already used verbatim in `forecast-drawer.tsx`,
    `sat-charts-drawer.tsx`, and `route-planner-drawer.tsx` (confirmed via
    `grep -rn "text-xs font-semibold uppercase tracking-\[0.16em\]" src`
    before changing, per the brief's instruction to verify the convention
    first). `variant="legend"` (the default, unused by this migration) left
    at its original `text-base` sizing.

## Left alone

- `Field`'s `fieldVariants` (orientation logic: vertical/horizontal/
  responsive), `FieldContent`, `FieldTitle`, `FieldDescription`,
  `FieldSeparator`, `FieldError` — none of these are consumed by the
  settings panel rework, left at shadcn defaults untouched.
- `FieldLabel`'s own classes (delegates to `Label` with `data-slot`
  overrides) — unchanged.

## Behavior changes

- None functionally; purely visual density tightening. Every section legend
  in `signalk-settings-panel.tsx` now renders at the same size/weight/
  tracking as the pre-existing bare `<h3>` section headers it replaces, so
  visually the settings panel is unchanged from before this migration aside
  from the underlying markup (`<legend>` inside a `<fieldset>` instead of
  `<h3>` inside a `<div>` — an accessibility improvement, since `<fieldset>`/
  `<legend>` is the semantically correct grouping for a block of form
  controls).

## Verify by hand

- Settings panel: confirm every section header ("SignalK Connection", "Boat
  And UI", "Tide", "Labels", "Anchor", "Anchor Watch Options", "Appearance")
  renders identically in size/weight/letter-spacing/color to how the old
  `<h3>` elements looked, and that inter-field vertical spacing within each
  section still reads as tightly packed as before (no extra whitespace from
  shadcn's un-overridden `gap-6`/`gap-7`/`mb-3` defaults).
- Inspect DOM: confirm each section is a real `<fieldset>` with a `<legend>`
  child (screen-reader / accessibility improvement over the previous
  `<div>`/`<h3>` pairing).
