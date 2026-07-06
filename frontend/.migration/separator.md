# separator

2026-07-06, transformation engine (legacy `default` style, no `base-default`
registry counterpart; hand-transform preserving the project's own classes).
Verdict: direct 1:1 migration, cleanest primitive in the coverage matrix.

## Changed

- `src/components/ui/separator.tsx`:
  - Import: `import * as SeparatorPrimitive from "@radix-ui/react-separator"`
    -> `import { Separator as SeparatorPrimitive } from "@base-ui/react/separator"`.
    Single-part primitive, so per the universal-patterns rule ("Single-part
    primitives are callable") the radix `SeparatorPrimitive.Root` collapses
    to a directly-callable `SeparatorPrimitive`.
  - `React.ElementRef<typeof SeparatorPrimitive.Root>` / `ComponentPropsWithoutRef<...Root>`
    -> `React.ComponentRef<typeof SeparatorPrimitive>` / `ComponentProps<typeof SeparatorPrimitive>`
    (no more `.Root` sub-path).
  - Dropped the `decorative` prop (and its `= true` default) entirely: Base
    UI's separator has no equivalent, it is always semantic (`role="separator"`
    per `class-mapping.md` / `form-controls.md`'s "no CSS variables, decorative
    dropped" note). No consumer in the project passed `decorative`, confirmed
    via grep, so this is a no-op for current call sites.
  - `displayName` set to a literal `"Separator"` since
    `SeparatorPrimitive.Root.displayName` no longer exists (no `.Root`).
  - Classes (`shrink-0 bg-border`, orientation-conditional sizing) untouched.
  - Leftover scan: `grep -n "radix-ui\|@radix-ui" src/components/ui/separator.tsx`
    -> no matches. Clean.

## Left alone

- None. Single-file wrapper.

## Behavior changes

- None observed. `orientation` prop and `data-orientation` attribute are
  identical on both sides (confirmed against `display-misc.md`'s separator
  section: same values, same default, same CSS-var story — none on either
  side).
- If any future consumer passes `decorative={false}` (opting OUT of the
  semantic role, i.e. wanting `aria-hidden`-style behavior), there is no
  direct prop; per `display-misc.md` the workaround is a plain
  `<div aria-hidden="true">` instead of this wrapper. No current consumer
  needs this, so not flagged as a live break, just documented for future use.

## Verify by hand

- Visually confirm the sidebar's `SidebarSeparator` (the only consumer)
  still renders as a 1px horizontal/vertical rule with the same `bg-border`
  color and spacing in both expanded and collapsed sidebar states.
