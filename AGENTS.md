# Agent Instructions

This file mirrors AGENTS.md. AGENTS.md is the canonical repo instruction file.

## Release Tags

- Use SemVer tags only (for example `v0.3.5`).
- Do not create date-based or ad-hoc tags.
- If tagging is requested, determine the next SemVer from existing tags.

## Fallback Policy

- Prefer fail-fast behavior for correctness and diagnostics paths.
- Do not add graceful fallback behavior that masks upstream data/source problems.
- If a required upstream source is missing (for example a required Furuno source), surface the issue explicitly and stop.

## Exception Rule

- Add fallback behavior only when explicitly requested.
- When fallback is approved, gate it behind a clear feature flag and emit explicit logs/telemetry that indicate fallback was used.

## Test-First Policy

- Prefer test-first development (TDD) for behavior changes and bug fixes.
- Write or update a failing test that reproduces the expected behavior before implementing the fix.
- Implement the minimal change required to make the test pass.
- Run relevant tests after the change and include the test result summary in your response.

## Documentation Location Policy

- Architectural decisions, design decisions, and feature specifications must be documented in the ADR folder: https://vscode.dev/github/gterrill/helmcentral/blob/main/docs/adr
- Do not keep durable feature-spec or architecture notes in backend/README.md or other component READMEs.
- When adding or changing behavior that affects architecture or feature contracts, create or update an ADR in docs/adr and link it from relevant README files.

## High-Density Tailwind UI/UX Specification

Build an inclusive, quietly dense, highly glanceable dashboard interface using Tailwind CSS. Treat the design as a mature, mission-critical cockpit, not a generic web app.

### Strict Tailwind Layout Resiliency

- Prevent viewport overflows. Never let text or elements stretch parent containers. Use `min-w-0` on flex items and `minmax(0, 1fr)` patterns in CSS grids to allow elements to shrink gracefully when screen real estate tightens.
- Enforce structural grid consistency. Use strict, matching spacing tokens across all widgets to preserve Gestalt grouping principles (e.g., wrap the parent layout in `grid gap-4 p-4` or `gap-6 p-6`). Individual metric containers must share identical inner padding (e.g., `p-4` or `p-6`).
- Handle truncation boundaries explicitly. When dealing with variable string lengths (like labels or data units), handle text overflow using `truncate` or `line-clamp-1`. Elements must never wrap to a second line and break the vertical grid unless intentionally designed as a historical graph or log.

### Telemetry & Color Mapping (60-30-10 Rule)

- Base canvas (60%): use the project's semantic surface tokens — `bg-background` for the page and `bg-card` for widget surfaces — rather than a raw Tailwind palette. These are backed by HSL CSS variables in `src/index.css` and already adapt across `.dark` without extra classes.
- Structural text/borders (30%): use `text-muted-foreground` for system labels (e.g., `text-muted-foreground font-medium text-xs tracking-wider uppercase`) and `border-border` for structural lines, again for automatic theme adaptation.
- Accent telemetry (10%): use `text-primary` / `text-secondary` (the app's blue/neutral interactive-chrome accent — buttons, toggles, selects, focus rings, and other UI chrome) for normal high-contrast chrome accents. For hero-number instrument readouts (KPI/gauge values — depth, tide, wind, battery, AC/DC draw, etc.), use the dedicated `text-gauge-primary` / `text-gauge-secondary` tokens (amber/teal) instead. Reserve raw palette colors (`amber-*`, `red-*`, `emerald-*`) strictly for alert semantics — warning/critical/healthy state — matching existing helpers like `tempClass()` / `scopeBadgeClass()`. Do not use any of these for standard text or decoration.
- Theming: prefer semantic tokens (`bg-background`, `bg-card`, `text-foreground`, `text-muted-foreground`, `border-border`, `text-primary`, `text-secondary`, `text-gauge-primary`, `text-gauge-secondary`) over raw palette classes — they adapt automatically across light/dark. Only reach for an explicit `dark:` class when a color is intentionally non-token, such as an alert state (e.g. `dark:bg-red-950`).

### Micro-Hierarchies for Glanceability

- Standardize KPI stacking. Place the muted uppercase identifier text label on top, followed by a significantly larger, high-contrast, bold data readout using the project's display font and tabular figures (e.g., `font-display text-2xl font-bold text-gauge-primary tabular-nums tracking-tight`, scaling up to `text-4xl` for hero metrics).
- Follow the density scale. Use `gap-4 p-4` (or `gap-6 p-6`) for the outer dashboard grid, but tighter `gap-2` and `p-2`-`p-3` for nested KPI sub-cards within a widget — matching the density already used inside `components/ui/tile.tsx`-based widgets.
- Keep trend presentation secondary. For inline trends or historical data vectors (like depth logs or voltage trends), prioritize clean canvas usage. Keep graphs simple and secondary to the primary real-time digital readouts.
- Do not introduce new primitives. There is no shared `MetricTile`/`StatCard` component yet — each widget (e.g. `alternator-tile.tsx`, `rode-scope-tile.tsx`) builds its own KPI-stack layout inside the shared `components/ui/tile.tsx` wrapper. Follow that existing bespoke-within-`Tile` pattern rather than inventing a new shared component.

### Micro-Typography Scale

Text below `text-xs` (12px) only ever needs three sizes — use exactly these, nothing else:

- `text-[11px]` — secondary inline values (unit suffixes, sub-readouts next to a KPI).
- `text-[10px]` — standard uppercase micro-labels: axis labels, chart legends, KPI identifier labels. This is the default for anything not covered by the other two tiers.
- `text-[9px]` — dense map/marker annotation only (vessel tags, badges). This is the legibility floor; never go smaller (no `text-[8px]` or below).
- Do not stack low-opacity color modifiers (e.g. `text-white/50`, `text-white/60`) on text at or below `text-[11px]` — reduced contrast on already-tiny glyphs is illegible in daylight glare. On themed surfaces, use `text-muted-foreground` for de-emphasis instead of opacity. On non-themed overlays (e.g. map HUDs), don't go below `/80` opacity for text this small.
- SVG `<text>` chart labels follow the same rules as DOM text: use a `fontSize` from the scale above (as a bare string, e.g. `fontSize="10"`), and set `fill` from a theme token (`hsl(var(--muted-foreground))` for axis/legend labels, `hsl(var(--primary))` for accent/emphasis labels) rather than a hardcoded `rgba()`/hex value — otherwise the chart silently stops adapting to dark mode and drifts from the DOM styling.

### Anti-Slop Constraints

- No hex improvisation. Never write raw arbitrary color values (e.g., `bg-[#f4f3ef]`, `text-[#334455]`). Use the project's semantic tokens (see Telemetry & Color Mapping above) or, for alert semantics only, the standard Tailwind palette scale. Arbitrary values for type size/tracking fine-tuning (e.g., `tracking-[0.16em]`) remain expected, but the sizes themselves must come from the Micro-Typography Scale above — this rule targets color escapes and undisciplined sizing, not fine-tuning in general.
- Maintain zero-state integrity. Do not use fake marketing metrics or placeholder strings. Use structural dashes (`--`), realistic operational defaults (`0.0`), or clear state toggles (`ON` / `OFF`).