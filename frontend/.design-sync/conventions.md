## Conventions

This design system is a Tailwind CSS utility-class system with CSS custom-property tokens (HSL channel values, wrapped in `hsl(var(--token))` by the Tailwind color config) — never inline styles or a CSS-in-JS runtime, except where noted below.

### Styling idiom: Tailwind utility classes over semantic color tokens

Every component composes Tailwind utility classes. Colors are never literal (`bg-blue-500`) — always the semantic family below, which maps to a `hsl(var(--*))` custom property so light/dark and brand values stay centralized:

| Utility family | Token | Use for |
|---|---|---|
| `bg-primary` / `text-primary-foreground` | `--primary` | primary actions (default Button) |
| `bg-secondary` / `text-secondary-foreground` | `--secondary` | secondary actions |
| `bg-destructive` / `text-destructive-foreground` | `--destructive` | destructive actions — note: `Button` has no `variant="destructive"`; apply `className="bg-destructive text-destructive-foreground hover:bg-destructive/90"` directly on a plain `Button` |
| `bg-card` / `text-card-foreground` | `--card` | card surfaces |
| `bg-popover` / `text-popover-foreground` | `--popover` | popovers, dropdowns, tooltips |
| `bg-muted` / `text-muted-foreground` | `--muted` | de-emphasized text/backgrounds |
| `bg-accent` / `text-accent-foreground` | `--accent` | hover/highlighted states |
| `border-border` / `border-input` / `ring-ring` | `--border` / `--input` / `--ring` | borders and focus rings |
| `bg-sidebar` / `text-sidebar-foreground` (+ `-primary`, `-accent`, `-border`, `-ring` variants) | `--sidebar-*` | the Sidebar family only — a separate token namespace from the app's main surface colors |
| `bg-gauge-primary` / `bg-gauge-secondary` | `--gauge-*` | marine gauge/instrument accents (wind, tide, depth visualizations) |

Border radius: `rounded-lg`/`rounded-md`/`rounded-sm` map to `--radius` (not literal pixel values). Typography: Geist Sans (`--font-sans`) is the automatic base/body font (applied via Tailwind's base layer, not an explicit utility class — never add `font-sans` expecting a visible change). `font-display` → `--font-display` (Geist Mono) is the utility to reach for explicitly, used throughout for numeric/gauge readouts (speed, tank levels, battery %, coordinates) — note this is distinct from Tailwind's built-in `font-mono` utility, which is NOT overridden here and falls back to the default system monospace stack. Both font families ship as real `@font-face` files, never assume a system font fallback is the intended look.

### Compound components need their provider/context ancestor

Several families are context-driven — a subpart rendered without its ancestor either crashes or renders meaninglessly:

- **`Sidebar*` (23 parts)**: everything except `SidebarProvider` itself requires `SidebarProvider > Sidebar` as an ancestor (`useSidebar()` context). Default `Sidebar` uses `collapsible="offcanvas"` (responsive, hides on narrow viewports) — for a deliberately-always-visible composition use `collapsible="none"`; `collapsible="icon"` gives the icon-only collapsed rail.
- **`Field*` (10 parts)**: `FieldLabel` wrapping `FieldContent` + `FieldTitle`/`FieldDescription` is the standard "toggle row" pattern (label + control on one line, description below). `FieldSet` + `FieldLegend` + `FieldGroup` is the standard grouped-settings pattern. `FieldSeparator` takes optional text children (e.g. "or") and sits inside a `FieldGroup`.
- **`Card*` (7 parts)**: `CardHeader` > `CardTitle` + `CardDescription` + optional `CardAction` (top-right slot, e.g. an icon button) is the standard header shape; `CardContent` and `CardFooter` follow.
- **Base UI overlay primitives** (`Popover*`, `Select*`, `Sheet*`, `Tooltip*`) are built on `@base-ui/react`, **not Radix** — composition uses a `render={<Element/>}` prop to merge behavior onto a custom trigger/close element, not `asChild`. To force one of these open in a non-interactive context, use `defaultOpen` (uncontrolled); `Select` additionally needs `defaultValue`, and `Tooltip` additionally needs `defaultTriggerId` matching the trigger's `id`.

### Where the truth lives

Read `_ds_bundle.css` (via `styles.css`'s import) for the full compiled token/utility set, and each component's own `<Name>.d.ts` for its real prop surface — the `.d.ts` is extracted directly from the shipped TypeScript, so it's authoritative over anything summarized here.

### Build snippet

```tsx
import { Card, CardHeader, CardTitle, CardDescription, CardContent, CardFooter, Button } from 'helmcentral-dashboard'

function ExampleCard() {
  return (
    <Card className="w-80">
      <CardHeader>
        <CardTitle>Anchor Watch</CardTitle>
        <CardDescription>Monitoring drift radius from the drop point.</CardDescription>
      </CardHeader>
      <CardContent>
        <p className="text-sm text-muted-foreground">Vessel is holding within a 12m radius.</p>
      </CardContent>
      <CardFooter>
        <Button size="sm">View details</Button>
      </CardFooter>
    </Card>
  )
}
```
