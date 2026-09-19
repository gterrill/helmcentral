# Agent Instructions

## Release Tags

- Use SemVer tags only (for example `v0.3.5`).
- Do not create date-based or ad-hoc tags.
- If tagging is requested, determine the next SemVer from existing tags.

## Checks Before Commit And Release

- Run `/code-review medium` and `/security-review' on the working tree before committing a batch of changes.
- Run `/code-review high` on the commits since the last tag before creating a release tag. For large migrations (framework or major dependency upgrades), use `/code-review ultra` instead.
- Check each finding against the code before acting on it. `high` and above report uncertain findings.
- Findings applied with `--fix` still have to follow the Fallback and Test-First policies below.

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
- **Apply TDD only to functional code and business logic** (new features, bug fixes, algorithmic changes).
- **Do NOT use TDD for:**
  - Build/tooling changes (`Makefile`, `Dockerfile`, CI/CD pipelines, config files).
  - Dependency upgrades and package manager lockfile updates.
  - Pure refactoring that doesn't change public interfaces.

## Documentation Location Policy

Two audiences, two trees. Operator-facing documentation exists to explain what
Helmcentral does and how to use it. ADRs exist to record why it was built that
way. Mixing them produces feature pages that read like engineering history and
decision records that read like manuals, and both get worse.

### The user-facing tree

`docs/` follows the [Diátaxis](https://diataxis.fr/) split, indexed by
`docs/index.md`:

| Content | Location |
| --- | --- |
| What a feature does, what it gives the operator, where it stops | `docs/features/` |
| Steps to accomplish a specific task | `docs/how-to/` |
| Fields, formats, environment variables, state paths | `docs/reference/` |
| Guided first run (none yet; create only when something needs it) | `docs/tutorials/` |

A feature normally gets one `features/` page as its entry point, and grows
`how-to/` and `reference/` pages as it needs them. Do not create empty
directories or placeholder pages in advance.

#### Target Audience & Persona

- **Audience:** Vessel skippers, navigators, and boat owners monitoring live vessel systems at the helm or remotely over tailscale.
- **Voice:** Pragmatic, professional marine systems guide. Focus on operational utility, situational awareness, and helm workflows.
- **Perspective:** Focus on *what the system does for the boat and operator*, never on *how the code or build system was implemented*.

#### User Facing Writing Rules

1. **No Implementation Details in User Docs:** Never mention Go, WebAssembly (WASM), Web Components, WebSocket lifecycles, JSON schemas, or internal data pipelines in user/operator documentation.
2. **Marine Terminology First:**
   - Use "vessel telemetry," "live instrument data," or "NMEA network feeds" instead of "Signal K paths / tree."
   - Use "tile" for a component placed on a dashboard page. One word, every time. Never "widget," and never a synonym picked per sentence: this bullet used to offer four alternatives and no default, which is how the manual ended up carrying both "Widgets" and "Tile state" as peer headings for the same object. "Gauge," "dial" and "lamp" remain correct for the specific kinds of tile that are those things.
   - Use "operating modes" or "helm profiles" (e.g., Underway, At Anchor, Passage, Refueling) instead of "UI pages / dashboard grids."
   - Use "alarm thresholds" or "system warnings" instead of "boolean state triggers."
3. **Operational Context First:** Start every feature doc with 1–2 sentences explaining the physical onboard benefit (e.g., preventing engine overheat, monitoring battery health at anchor, passage navigation).
4. **Actionable Steps:** Focus how-to instructions strictly on what the user clicks, selects, or toggles on the display.

### The engineering tree

`docs/adr/` holds architecture decision records and is **not part of the
Diátaxis tree.** It is the internal record: context, the decision, what was
rejected, and what a later ADR reversed. Wrong turns belong here and only here.

### The rule that keeps them apart

- **User-facing pages do not cite ADR numbers.** If a `features/`, `how-to/` or
  `reference/` page only makes sense once the reader has followed a decision
  record, the page is not finished. State the behaviour and the reason for it in
  the page's own words.
- **ADRs do not carry operator instructions.** An ADR may describe what an
  operator will see; the steps for doing it live in `docs/how-to/`.
- Component READMEs (`backend/README.md`, `frontend/README.md`) hold neither.
  No durable feature specs or architecture notes there.

### When behaviour changes

A change affecting architecture or a feature contract needs both: create or
update the ADR in `docs/adr/`, **and** update the affected page under `docs/`.
An ADR alone leaves the operator-facing docs silently wrong.

## Tiles And Widgets

The operator has one word for the things on a dashboard page, and it is
**tile**. That word appears in every UI string, every `aria-label`, every page
under `docs/`, and in Mate's answers. Nothing an operator can read says
"widget".

The code keeps both words, because they name different things:

- A **widget** is the config record: an id, its geometry, and its settings.
  It is what the backend validates against `validDashboardWidgetIDs`, what
  the `widgets` array persists, and what `dashboard-widgets.ts` types. It has
  no appearance and the operator never sees it.
- A **tile** is the rendered surface: what `components/ui/tile.tsx` draws,
  what `*-tile.tsx` implements, and what the operator places, drags, resizes,
  promotes to hero and removes.

So `widgetDisplayName(w)` returning a string that ends up inside "Remove Depth
tile" is correct, not a leftover. A record has a display name; the surface it
produces is a tile.

Two rules follow. Do not introduce "widget" into anything the operator
perceives. Do not rename the persisted `widgets` field, the backend's
`validDashboardWidgetIDs`, or the config types to "tile" either: they are the
record layer, renaming them buys nothing and costs a coordinated change
against stored state.

`docs/adr/` is exempt from all of this. Thirty-four ADRs say "widget" because
that was the word when they were written, and they are the historical record.
Do not rewrite them.

## Modern Web Guidance

This project's Baseline target is Baseline 2024.

## High-Density Tailwind UI/UX Specification

Build an inclusive, quietly dense, highly glanceable dashboard interface using Tailwind CSS. Treat the design as a mature, mission-critical cockpit, not a generic web app.

### Strict Tailwind Layout Resiliency

- Prevent viewport overflows. Never let text or elements stretch parent containers. Use `min-w-0` on flex items and `minmax(0, 1fr)` patterns in CSS grids to allow elements to shrink gracefully when screen real estate tightens.
- Enforce structural grid consistency. Use strict, matching spacing tokens across all tiles to preserve Gestalt grouping principles (e.g., wrap the parent layout in `grid gap-4 p-4` or `gap-6 p-6`). Individual metric containers must share identical inner padding (e.g., `p-4` or `p-6`).
- Handle truncation boundaries explicitly. When dealing with variable string lengths (like labels or data units), handle text overflow using `truncate` or `line-clamp-1`. Elements must never wrap to a second line and break the vertical grid unless intentionally designed as a historical graph or log.

### Telemetry & Color Mapping (60-30-10 Rule)

- Base canvas (60%): use the project's semantic surface tokens — `bg-background` for the page and `bg-card` for tile surfaces — rather than a raw Tailwind palette. These are backed by HSL CSS variables in `src/index.css` and already adapt across `.dark` without extra classes. Tailwind is CSS-first (v4, no `tailwind.config.ts`): `src/index.css`'s `@theme inline` block maps every one of these tokens to its `bg-*`/`text-*`/`border-*` utility, so a new semantic colour is wired up there, not in a JS config file.
- Structural text/borders (30%): use `text-muted-foreground` for system labels (e.g., `text-muted-foreground font-medium text-xs tracking-wider uppercase`) and `border-border` for structural lines, again for automatic theme adaptation.
- Accent telemetry (10%): use `text-primary` / `text-secondary` (the app's blue/neutral interactive-chrome accent — buttons, toggles, selects, focus rings, and other UI chrome) for normal high-contrast chrome accents. For hero-number instrument readouts (KPI/gauge values — depth, tide, wind, battery, AC/DC draw, etc.), use the dedicated `text-gauge-primary` / `text-gauge-secondary` tokens (amber/teal) instead. Reserve raw palette colors (`amber-*`, `red-*`, `emerald-*`) strictly for alert semantics — warning/critical/healthy state — matching existing helpers like `tempClass()` (`alternator-tile.tsx`) / `scopeBadgeClass()` (`anchor-rode-planner.tsx`). Do not use any of these for standard text or decoration.
- Theming: prefer semantic tokens (`bg-background`, `bg-card`, `text-foreground`, `text-muted-foreground`, `border-border`, `text-primary`, `text-secondary`, `text-gauge-primary`, `text-gauge-secondary`) over raw palette classes — they adapt automatically across light/dark. Only reach for an explicit `dark:` class when a color is intentionally non-token, such as an alert state (e.g. `dark:bg-red-950`).

### Micro-Hierarchies for Glanceability

- Standardize KPI stacking. Place the muted uppercase identifier text label on top, followed by a significantly larger, high-contrast, bold data readout using the project's display font and tabular figures (e.g., `font-display text-2xl font-bold text-gauge-primary tabular-nums tracking-tight`, scaling up to `text-4xl` for hero metrics).
- Follow the density scale. Use `gap-4 p-4` (or `gap-6 p-6`) for the outer dashboard grid, but tighter `gap-2` and `p-2`-`p-3` for nested KPI sub-cards within a tile, matching the density already used inside `components/ui/tile.tsx`-based tiles.
- Keep trend presentation secondary. For inline trends or historical data vectors (like depth logs or voltage trends), prioritize clean canvas usage. Keep graphs simple and secondary to the primary real-time digital readouts.
- Do not introduce new primitives. There is no shared `MetricTile`/`StatCard` component yet. Each tile (e.g. `alternator-tile.tsx`, `depth-tide-tile.tsx`) builds its own KPI-stack layout inside the shared `components/ui/tile.tsx` wrapper. Follow that existing bespoke-within-`Tile` pattern rather than inventing a new shared component.

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

## Papercuts

- Maintain ~/papercuts.md, a global log shared by all sessions of anything that slowed down development. When you lose time to one mid-session, append date · symptom · fix · project. Check this file first when tooling fails mysteriously.