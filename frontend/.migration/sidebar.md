# sidebar

2026-07-06, transformation engine (sidebar.tsx is not itself a shadcn
registry component with a golden pair fetchable by name; it is a hand-rolled
composition, migrated per `universal-patterns.md`'s Slot -> useRender worked
example and `wrapper-shapes.md`'s Button guidance). Verdict: all 5 internal
`asChild` sites migrated; the file's own radix dependency
(`@radix-ui/react-slot`) is fully removed.

## Changed

- `src/components/ui/sidebar.tsx`:
  - Import: dropped `import { Slot } from "@radix-ui/react-slot"`. Added
    `import { Button as ButtonPrimitive } from "@base-ui/react/button"`,
    `import { mergeProps } from "@base-ui/react/merge-props"`,
    `import { useRender } from "@base-ui/react/use-render"`.
  - `TooltipProvider delayDuration={0}` -> `delay={0}` and
    `TooltipTrigger asChild` -> `render={button}` were fixed in the
    tooltip.tsx commit (this file is tooltip's sole consumer); not repeated
    here, see `.migration/tooltip.md`.
  - Five internal `asChild ? Slot : <tag>` sites converted, split by
    rendered element per `universal-patterns.md`'s rule ("This pattern
    [useRender + mergeProps] is ONLY for non-button polymorphic components
    ... button.tsx migrates to the real @base-ui/react/button primitive"),
    extended in this file to every polymorphic part that renders a
    `<button>` by default, not only the top-level `Button` wrapper:
    - `SidebarGroupLabel` (renders `<div>`): `useRender` + `mergeProps`,
      `useRender.ComponentProps<"div">`.
    - `SidebarGroupAction` (renders `<button>`): switched to
      `<ButtonPrimitive>` directly (real Base UI Button primitive, which
      natively accepts `render`), not `useRender`.
    - `SidebarMenuButton` (renders `<button>`, has extra `isActive`/
      `tooltip`/`variant`/`size` props): switched to `<ButtonPrimitive>`.
      Its `React.ComponentProps<"button"> & { asChild?: boolean }` base type
      became `React.ComponentProps<typeof ButtonPrimitive>` (drops the
      manual `asChild` field, gains `render` for free from the primitive).
    - `SidebarMenuAction` (renders `<button>`, has extra `showOnHover`):
      switched to `<ButtonPrimitive>`.
    - `SidebarMenuSubButton` (renders `<a>`, a genuinely non-button
      polymorphic part): `useRender` + `mergeProps`,
      `useRender.ComponentProps<"a">`.
  - Both `useRender`/`mergeProps` conversions cast the internal literal
    props object to `React.ComponentPropsWithRef<TagName>` (not
    `React.ComponentProps<TagName>`) before passing to `mergeProps`, per a
    type mismatch discovered during this migration: `mergeProps`'s
    `InputProps<T>` expects `React.ComponentPropsWithRef<T>` (`ref` typed as
    `Ref<T> | RefObject<T> | null`), while `React.ComponentProps<T>` types
    `ref` as the wider/older `LegacyRef<T>` (`string | ...`), which fails
    `mergeProps`'s structural check. This refines
    `universal-patterns.md`'s worked example, which shows a plain
    `as React.ComponentProps<"a">` cast for a component with no `ref` prop
    in its literal (breadcrumb link) — sidebar's parts all forward a real
    `ref`, so the wider `ComponentPropsWithRef` cast is required here. Noted
    as a gap for the reference doc.
  - Leftover scan: `grep -n "radix-ui\|@radix-ui\|Slot\|asChild" src/components/ui/sidebar.tsx`
    -> no matches. Clean.

## Left alone

- `SidebarInput` (wraps the already-migrated `Input`, no radix), `SidebarSeparator`
  (wraps the already-migrated `Separator`, no radix), `SidebarRail` (plain
  `<button>`, no `asChild`, never used Slot) — all untouched, correctly.
- `SidebarMenuSkeleton`, `SidebarMenuBadge`, `SidebarHeader`, `SidebarFooter`,
  `SidebarContent`, `SidebarGroup`, `SidebarGroupContent`, `SidebarMenu`,
  `SidebarMenuItem`, `SidebarMenuSub`, `SidebarMenuSubItem`, `SidebarInset` —
  plain `<div>`/`<ul>`/`<li>`/`<main>` wrappers with no `asChild`/Slot
  usage, untouched.

## Behavior changes

- None expected. `render={<X/>}` and `useRender`+`mergeProps` reproduce the
  Slot-merge-onto-child behavior; no consumer in this project (`App.tsx`)
  passes `asChild` to any of the five migrated parts (confirmed via grep
  before migrating), so there is no live call-site break, only an internal
  implementation swap.
- `SidebarMenuButton`/`SidebarGroupAction`/`SidebarMenuAction` now render via
  the real Base UI `Button` primitive, which additionally exposes
  `nativeButton` and `focusableWhenDisabled` (both default appropriately for
  a `<button>` target, not exercised by this project today).

## Verify by hand

- Toggle the sidebar between expanded/collapsed states and confirm
  `SidebarMenuButton`s still show hover/active/focus-visible styling
  identically, and that the collapsed-state tooltip (via `TooltipContent`)
  still appears per the already-verified tooltip migration.
- Confirm `SidebarGroupLabel`/`SidebarGroupAction`/`SidebarMenuAction` render
  and respond to clicks/hover with no console errors (no consumer currently
  exercises `render=`, so this is purely a smoke check of the default,
  non-polymorphic path).
- Click through the sidebar's nav items (`SidebarMenuButton` with
  `isActive`) and confirm the active-state background/text color still
  applies via `data-active`.
