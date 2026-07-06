# breadcrumb

2026-07-06, transformation engine (legacy `default` style, no `base-default`
registry counterpart; scaffolded via `npx shadcn@latest add breadcrumb`,
which emits `@radix-ui/react-slot` for `BreadcrumbLink`'s `asChild` prop —
hand-transformed to `@base-ui/react`'s `useRender`/`mergeProps` pattern,
matching the same transform already applied to `SidebarGroupLabel`/
`SidebarMenuSubButton` in `sidebar.tsx`). Verdict: only one of the six parts
(`BreadcrumbLink`) needed a primitive transform; the rest are plain semantic
elements (`nav`/`ol`/`li`/`span`) with zero Radix/Base UI dependency, same
shape as `field.tsx`'s finding.

## Changed

- `src/components/ui/breadcrumb.tsx`:
  - `BreadcrumbLink`: `import { Slot } from "@radix-ui/react-slot"` + `const
    Comp = asChild ? Slot : "a"` -> `useRender({ defaultTagName: "a", render,
    ref, props: mergeProps(...) })`, replacing the boolean `asChild` prop with
    Base UI's `render` prop (element or render-function), per this project's
    established `asChild` -> `render` convention.
  - Everything else (`Breadcrumb`, `BreadcrumbList`, `BreadcrumbItem`,
    `BreadcrumbPage`, `BreadcrumbSeparator`, `BreadcrumbEllipsis`) — no
    transform needed, confirmed zero Radix/Base UI imports in the original
    scaffold for these parts.
  - Removed the transient `@radix-ui/react-slot` dependency the CLI installed
    (`npm uninstall @radix-ui/react-slot`), confirmed no other file in
    `src/components/ui/` still needs it (`grep -rln "radix-ui\|@radix-ui"
    src/components/ui/*.tsx` -> no matches).
  - Leftover scan: `grep -n "radix-ui\|@radix-ui"
    src/components/ui/breadcrumb.tsx` -> no matches. Clean.

## Left alone

- None beyond the single-part transform above.

## Behavior changes

- None. `useRender`'s `render` prop is a strict superset of `asChild`'s
  capability (element or render-function vs. element only) — no current
  consumer passes either, so this is a pure primitive swap with no observable
  difference.

## Verify by hand

- Confirm the page-header breadcrumb (added alongside this component) renders
  correctly: "Dashboard" as a clickable link-styled span when a panel is
  active, current panel name as plain (non-link) text, with a chevron
  separator between them — both in light and dark mode.
