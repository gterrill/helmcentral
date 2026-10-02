# Agent Instructions

## Release Tags

- Use SemVer tags only (for example `v0.3.5`).
- Do not create date-based or ad-hoc tags.
- If tagging is requested, determine the next SemVer from existing tags.

## Release Notes

- `CHANGELOG.md` is the release notes. The release workflow publishes the
  tag's section as the GitHub release body and fails the release if the tag
  has no section.
- Add an entry under `## [Unreleased]` in the same commit as any change an
  operator would notice. Refactors, tests and tooling get no entry.
- Any change that makes an operator act on upgrade (moved setting, removed
  environment variable, changed URL, rewritten stored state, anything they
  must re-enter or place again) goes under **Breaking**, and says what to do.
- Before tagging, rename `## [Unreleased]` to `## [X.Y.Z] - YYYY-MM-DD`, add a
  fresh empty `## [Unreleased]` above it, and update the compare links at the
  foot of the file. Check it with `sh packaging/release-notes.sh vX.Y.Z`.
- Entries are written for the operator, under the user-facing writing rules in
  the Documentation Location Policy: no ADR numbers, no implementation detail.

## Checks Before Commit And Release

- Run `/code-review medium` on the working tree before committing a batch of changes.
- Run `/code-review high` on the commits since the last tag before creating a release tag. For large migrations (framework or major dependency upgrades), use `/code-review ultra` instead.
- Run `/security-review` once the changes are committed, before merging a branch or creating a release tag. It diffs `origin/HEAD...`, so it sees committed work only, and reports nothing on an uncommitted working tree.
- `/security-review` takes no arguments and never edits code. It returns a markdown report of HIGH/MEDIUM findings only and filters anything below 8/10 confidence.
- Check each finding against the code before acting on it. `high` and above report uncertain findings.
- Findings applied with `--fix` still have to follow the Fallback and Test-First policies below.

## Shared Working Tree

- This repo is often open in more than one session at once. Before your first
  edit, run `git status`. If the tree already holds changes you did not make,
  say so and ask before editing any file they touch.
- Stage by path. Never `git add -A`, `git add .` or `git commit -a` in this
  repo: they commit other sessions' unfinished work along with your own, and
  the resulting commit cannot be split once it is pushed.
- Never commit, stash, revert or check out a file you did not change. If your
  work genuinely needs a change another session is mid-way through, ask rather
  than resolving it yourself.
- For anything more than a small edit, work in your own checkout:
  `make worktree NAME=<short-name>` creates one alongside this directory with
  `frontend/node_modules` symlinked, so tests run immediately. Only one
  checkout can run the dev stack; use the one that is already up.
- To check a UI change made in a worktree, leave the dev stack alone and run
  the worktree's frontend on the next free port against the same backend:
  `cd frontend && npx vite --port 5175 --strictPort`. Vite proxies `/api` to
  the running backend on :8080, so the page shows the same live data as :5173
  and the two can be compared side by side. Ports 5173 and 5174 belong to the
  dev and e2e stacks; go up from 5175 if another worktree already holds it.
  Stop the server once the change is checked; `make prune-merged` will not
  remove a worktree while it runs.
- Once a branch's PR is merged, `make prune-merged` lists the worktrees and
  local and remote branches it would clear; `CONFIRM=1` deletes them. It
  skips any worktree with uncommitted changes or with a process still
  running inside it, such as a forgotten preview server, and names the pids.

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
   - Use "tile" for a component placed on a dashboard page. One word, every time. Never "widget," and never a synonym picked per sentence: this bullet used to offer four alternatives and no default, which is how the help pages ended up carrying both "Widgets" and "Tile state" as peer headings for the same object. "Gauge," "dial" and "lamp" remain correct for the specific kinds of tile that are those things.
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
Tailwind UI/UX above is about dashboard tiles specifically (no shared
`MetricTile`/`StatCard`); it does not apply to these CRUD pages, which are
exactly what `components/patterns` exists to share. Every settings section
page (`components/settings/sections`) is built from `SettingsLayout` and
`FormSection`, and the settings page saves through `SaveBar`, with no Save
button of its own. `Page` carries no help
action of its own - section help is the header `?`, which already follows
the active section. See [ADR 0142](docs/adr/0142-crud-pattern-library.md)
for why, and the dev-only `/patterns` gallery for what each pattern looks
like against fixture data.

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

The floor for anything the operator reads as a value is `text-xs` (12px). That covers readouts, unit suffixes, sub-readouts next to a KPI, and any mixed-case text. Below 12px there are exactly two sizes, and neither carries a value:

- `text-[10px]` — uppercase, letter-spaced micro-labels only: axis labels, chart legends, KPI identifier labels. Capitals at 10px stand taller than lowercase at 11px, and a label is recognised by shape rather than read, so it can go smaller than a value.
- `text-[9px]` — dense map/marker annotation only (vessel tags, badges). This is the legibility floor; never go smaller (no `text-[8px]` or below).
- `text-[11px]` is retired. Mixed-case text at 11px leaves lowercase glyphs about 5.5px tall, which fails at arm's length in glare. Use `text-xs` for values or `text-[10px]` for uppercase labels.
- Do not stack low-opacity color modifiers (e.g. `text-white/50`, `text-white/60`) on text below `text-xs` — reduced contrast on already-tiny glyphs is illegible in daylight glare. On themed surfaces, use `text-muted-foreground` for de-emphasis instead of opacity. On non-themed overlays (e.g. map HUDs), don't go below `/80` opacity for text this small.
- SVG `<text>` chart labels follow the same rules as DOM text: use a `fontSize` from the scale above (as a bare string, e.g. `fontSize="10"`), and set `fill` from a theme token (`hsl(var(--muted-foreground))` for axis/legend labels, `hsl(var(--primary))` for accent/emphasis labels) rather than a hardcoded `rgba()`/hex value — otherwise the chart silently stops adapting to dark mode and drifts from the DOM styling.

### Anti-Slop Constraints

- No hex improvisation. Never write raw arbitrary color values (e.g., `bg-[#f4f3ef]`, `text-[#334455]`). Use the project's semantic tokens (see Telemetry & Color Mapping above) or, for alert semantics only, the standard Tailwind palette scale. Arbitrary values for type size/tracking fine-tuning (e.g., `tracking-[0.16em]`) remain expected, but the sizes themselves must come from the Micro-Typography Scale above — this rule targets color escapes and undisciplined sizing, not fine-tuning in general.
- Maintain zero-state integrity. Do not use fake marketing metrics or placeholder strings. Use structural dashes (`--`), realistic operational defaults (`0.0`), or clear state toggles (`ON` / `OFF`).

## Papercuts

- Maintain ~/papercuts.md, a global log shared by all sessions of anything that slowed down development. When you lose time to one mid-session, append date · symptom · fix · project. Check this file first when tooling fails mysteriously.