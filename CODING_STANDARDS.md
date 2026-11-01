# Coding Standards

Rules for UI code and operator-facing text. The mechanical parts are ESLint
rules (`frontend/eslint-rules`); the rest are judgement calls the review
step checks by reading the diff.

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
against stored state. `helm/no-widget-in-ui-text` enforces the UI-text half: it rejects "widget" in JSX text, in the text attributes (`aria-label`, `aria-description`, `title`, `placeholder`, `alt`, `label`, `description` and similar) and in `toast` messages. It does not check text in data objects rendered later, or text returned by the server.

`docs/adr/` is exempt from all of this. Thirty-four ADRs say "widget" because
that was the word when they were written, and they are the historical record.
Do not rewrite them.

## Alarm Wording

An alarm card is read by a watchkeeper at the helm, often at 02:00, asking
what happened, where it is, and whether to do something. Write every alarm
title, body and notification for that reader, not for the code that raised
it. See [ADR 0157](docs/adr/0157-alarm-cards-say-what-happened-not-where-it-came-from.md).

- **Title: the event, in a few words.** "Radar Guard Zone 1", "Anchor
  Dragging", "House Bank Low". It fits one line on a phone. Never a SignalK
  path, device key (`fur6424A`), plugin name or rule id. Name a device only
  when two of the same kind are live, and then by the operator's name for it.
  A rule alarm's title is the operator's own label; leave it alone.
- **Body: the situation.** What, where, how close, which way it is going, in
  that order and in the operator's units: `Target in guard zone 1 · 042°T ·
  1.4 NM · CPA 0.3 NM`. With no figures to give, one plain sentence. Present
  tense, the boat's frame ("45 m from the drop point", not "value now 45").
- **No internal identifiers.** Track ids, `$source`, paths and rule ids stay
  off the card. The one exception is an id the operator needs to find the same
  thing on another screen, and then it is the label that screen shows.
- **Severity is the colour and the state word.** No `!` or `?` in the text.
- **State and clearing go in the badge and footer, never the body.** Say how
  an alarm clears only when Helmcentral knows: a rule's clear point, yes; how
  another instrument clears its own alarm, no. Never explain the latching.
- **Never claim what isn't known.** If a figure can't be read, leave it out
  and keep the one sentence. Do not estimate, convert through a guessed
  variation, or fill from an older reading.
- **One presenter.** Titles and bodies for alarms raised elsewhere on the
  network are set on the server, so the card, the banner, Mate and every
  notification transport say the same thing. A new alarm source gets its own
  written title rather than leaning on the path-to-words fallback.

## CRUD Pattern Library

Equipment, Locations, Profiles, Maintenance, Wall displays and Documents are
record-keeping surfaces (an index, a details/edit page), not dashboard
tiles. Build and extend them from `frontend/src/components/patterns`
(`Page`, `IndexTable`, `IndexFilters`, `DetailsLayout`, `SettingsLayout`,
`FormSection`, `SaveBar`, `EmptyState`, `ConfirmDelete`) rather than reaching for
`components/ui/table` or hand-rolling a toolbar - an ESLint rule enforces
this for `ui/table` already, with a shrinking allowlist for surfaces not yet
migrated. The "Do not introduce new primitives" rule under High-Density
Tailwind UI/UX below is about dashboard tiles specifically (no shared
`MetricTile`/`StatCard`); it does not apply to these CRUD pages, which are
exactly what `components/patterns` exists to share. Every settings section
page (`components/settings/sections`) is built from `SettingsLayout` and
`FormSection`, and the settings page saves through `SaveBar`, with no Save
button of its own. `Page` carries no help
action of its own - section help is the header `?`, which already follows
the active section. See [ADR 0142](docs/adr/0142-crud-pattern-library.md)
for why, and the dev-only `/patterns` gallery for what each pattern looks
like against fixture data.

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

The floor for anything the operator reads as a value is `text-xs` (12px). That covers readouts, unit suffixes, sub-readouts next to a KPI, and any mixed-case text. Below 12px there are exactly two sizes, and neither carries a value:

- `text-[10px]` — uppercase, letter-spaced micro-labels only: axis labels, chart legends, KPI identifier labels. Capitals at 10px stand taller than lowercase at 11px, and a label is recognised by shape rather than read, so it can go smaller than a value.
- `text-[9px]` — dense map/marker annotation only (badges). This is the legibility floor. A marker label the operator reads, such as a boat name with its range and bearing, is a value and takes `text-xs`. `text-[11px]` is retired: mixed-case text at 11px leaves lowercase glyphs about 5.5px tall, which fails at arm's length in glare. ESLint enforces the sizes (`helm/text-size-scale`): a `text-[Npx]` or `text-[Nrem]` class, a JSX `fontSize` or a `fontSize` object property that is not 9px, 10px or 12px and above is an error (0 is allowed). It does not check MapLibre layer sizes, `em` values, or sizes held in variables.
- Do not stack low-opacity color modifiers (e.g. `text-white/50`, `text-white/60`) on text below `text-xs` — reduced contrast on already-tiny glyphs is illegible in daylight glare. On themed surfaces, use `text-muted-foreground` for de-emphasis instead of opacity. On non-themed overlays (e.g. map HUDs), don't go below `/80` opacity for text this small.
- SVG `<text>` chart labels follow the same rules as DOM text: use a `fontSize` from the scale above (as a bare string, e.g. `fontSize="10"`), and set `fill` from a theme token (`hsl(var(--muted-foreground))` for axis/legend labels, `hsl(var(--primary))` for accent/emphasis labels) rather than a hardcoded `rgba()`/hex value — otherwise the chart silently stops adapting to dark mode and drifts from the DOM styling.

### Anti-Slop Constraints

- No hex improvisation. Never write raw arbitrary color values (e.g., `bg-[#f4f3ef]`, `text-[#334455]`). Use the project's semantic tokens (see Telemetry & Color Mapping above) or, for alert semantics only, the standard Tailwind palette scale. Arbitrary values for type size/tracking fine-tuning (e.g., `tracking-[0.16em]`) remain expected, but the sizes themselves must come from the Micro-Typography Scale above — this rule targets color escapes and undisciplined sizing, not fine-tuning in general. Hex, `rgb()`, `hsl()` and `oklch()` colour escapes in class tokens, and hex/`rgb()`/`hsl()` values in SVG `fill`/`stroke`/`color`/`stopColor` and in style-object colour properties, are enforced by `helm/no-arbitrary-color`; colours held in variables or data objects are not checked.
- Maintain zero-state integrity. Do not use fake marketing metrics or placeholder strings. Use structural dashes (`--`), realistic operational defaults (`0.0`), or clear state toggles (`ON` / `OFF`).
