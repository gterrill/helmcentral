---
name: Helmcentral
description: A dense boat dashboard with distinct colours for readings, controls and alarms.
colors:
  background: "hsl(0 0% 98%)"
  foreground: "hsl(0 0% 0%)"
  card: "hsl(0 0% 100%)"
  card-foreground: "hsl(0 0% 9.5%)"
  primary: "hsl(226 52% 50%)"
  primary-foreground: "hsl(214 96% 97%)"
  secondary: "hsl(0 0% 92%)"
  secondary-foreground: "hsl(0 0% 13%)"
  muted: "hsl(0 0% 96%)"
  muted-foreground: "hsl(0 0% 32%)"
  accent: "hsl(216 60% 91%)"
  accent-foreground: "hsl(237 64% 21%)"
  border: "hsl(0 0% 90%)"
  input: "hsl(0 0% 92%)"
  ring: "hsl(226 52% 50%)"
  destructive: "hsl(359 75% 50%)"
  destructive-foreground: "hsl(214 96% 97%)"
  gauge-primary: "hsl(39 100% 39%)"
  gauge-secondary: "hsl(175 42% 32%)"
  chart-wind: "hsl(226 72% 53%)"
  chart-gust: "hsl(38 92% 50%)"
  chart-wave: "hsl(173 80% 40%)"
  chart-swell: "hsl(258 90% 66%)"
  chart-temp: "hsl(32 95% 44%)"
  chart-precip: "hsl(217 91% 60%)"
  chart-uv: "hsl(48 96% 53%)"
  chart-grid: "hsl(214 20% 39%)"
typography:
  display:
    fontFamily: "Geist Mono, monospace"
    fontSize: "2.25rem"
    fontWeight: 400
    lineHeight: 1
    letterSpacing: "normal"
    fontFeature: "tabular-nums"
  headline:
    fontFamily: "Geist Mono, monospace"
    fontSize: "1.125rem"
    fontWeight: 400
    lineHeight: 1
    letterSpacing: "normal"
    fontFeature: "tabular-nums"
  title:
    fontFamily: "Geist Sans, sans-serif"
    fontSize: "1rem"
    fontWeight: 600
    lineHeight: 1
    letterSpacing: "normal"
  body:
    fontFamily: "Geist Sans, sans-serif"
    fontSize: "0.875rem"
    fontWeight: 400
    lineHeight: 1.5
    letterSpacing: "normal"
  label:
    fontFamily: "Geist Sans, sans-serif"
    fontSize: "0.625rem"
    fontWeight: 400
    lineHeight: 1
    letterSpacing: "0.16em"
  tile-title:
    fontFamily: "Geist Mono, monospace"
    fontSize: "0.75rem"
    fontWeight: 400
    lineHeight: 1
    letterSpacing: "0.22em"
rounded:
  sm: "4px"
  md: "6px"
  lg: "8px"
  xl: "16px"
  board: "0.75rem"
  full: "9999px"
spacing:
  micro: "0.5rem"
  tight: "0.75rem"
  base: "1rem"
  loose: "1.5rem"
components:
  button-primary:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.primary-foreground}"
    rounded: "{rounded.md}"
    padding: "0.5rem 1rem"
    height: "2.5rem"
    typography: "{typography.body}"
  button-primary-hover:
    backgroundColor: "hsl(226 52% 50% / 0.9)"
    textColor: "{colors.primary-foreground}"
  button-secondary:
    backgroundColor: "{colors.secondary}"
    textColor: "{colors.secondary-foreground}"
    rounded: "{rounded.md}"
    padding: "0.5rem 1rem"
    height: "2.5rem"
  button-outline:
    backgroundColor: "{colors.card}"
    textColor: "{colors.foreground}"
    rounded: "{rounded.md}"
    padding: "0.5rem 1rem"
    height: "2.5rem"
  button-ghost:
    backgroundColor: "transparent"
    textColor: "{colors.foreground}"
    rounded: "{rounded.md}"
    padding: "0.5rem 1rem"
    height: "2.5rem"
  input:
    backgroundColor: "{colors.background}"
    textColor: "{colors.foreground}"
    rounded: "{rounded.md}"
    padding: "0.5rem 0.75rem"
    height: "2.5rem"
  tile:
    backgroundColor: "{colors.card}"
    textColor: "{colors.card-foreground}"
    rounded: "{rounded.xl}"
    padding: "1rem 0.75rem"
  tile-title:
    textColor: "{colors.muted-foreground}"
    typography: "{typography.tile-title}"
  alarm-banner:
    backgroundColor: "hsl(359 75% 50% / 0.1)"
    textColor: "{colors.destructive}"
    rounded: "{rounded.lg}"
    padding: "0.75rem 1rem"
---

# Design System: Helmcentral

## Overview

The default dashboard uses flat, near-greyscale surfaces: white tiles on an
almost-white page, hairline borders, and no decorative gradients or
illustrations. Amber and teal identify instrument readings, blue identifies
interactive controls, and red identifies alarms.

Tiles use a dense layout with a small, letter-spaced mono title, a divider to
the edge, and tabular figures to keep values aligned as they update. Small
labels need sufficient contrast for a helm screen read at arm's length in
direct sun. Use the muted foreground token for secondary text; do not reduce
the opacity of small text.

The optional `[data-skin="instrument"]` dark board redefines the tokens used by
each component, without per-component branching. Its instrument chrome uses a
bezel gradient, an inset rim and a needle glow to resemble an MFD at night.
Red and amber retain their alert meanings in both skins.

**Key Characteristics:**

- Near-monochrome surfaces; colour reserved for readings, chrome and alarms.
- Geist Mono for every number and every instrument label; Geist Sans for prose.
- Flat by default. Depth belongs to the instrument skin and to overlays.
- Dense grid, hairline borders, uppercase tracked micro-labels at 10px.
- Semantic tokens only. Themes swap values, components never branch.

## Colors

Near-greyscale surfaces with separate accent colours for readings, interactive
controls and alarms.

### Primary

- **Signal Blue** (`hsl(226 52% 50%)`): interactive chrome and nothing else.
  Buttons, toggles, selects, focus rings, the active state of a control. It is
  also the dial band and the layout placeholder at low opacity. It never carries
  a measurement.
- **Pale Blue Ink** (`hsl(214 96% 97%)`): the foreground that sits on Signal
  Blue and on alarm red.

### Secondary

- **Deep Amber** (`hsl(39 100% 39%)`): the primary instrument readout token
  (`--gauge-primary`). Depth, battery state of charge, wind, AC and DC draw:
  the main numeral in a tile usually uses this token. Darkened for text contrast
  on a white card.
- **Muted Teal** (`hsl(175 42% 32%)`): the secondary readout token
  (`--gauge-secondary`). The second-rank numbers in a tile, and the tide and
  depth figures that pair against amber.

### Tertiary

Chart series use one hue per measured quantity,
so a colour means the same thing across every graph: wind
(`hsl(226 72% 53%)`), gust (`hsl(38 92% 50%)`), wave (`hsl(173 80% 40%)`),
swell (`hsl(258 90% 66%)`), temperature (`hsl(32 95% 44%)`), precipitation
(`hsl(217 91% 60%)`), UV (`hsl(48 96% 53%)`), and a grid line
(`hsl(214 20% 39%)`).

Four have a darkened `-label` variant (`--chart-temp-label`,
`--chart-precip-label`, `--chart-wave-label`, `--chart-gust-label`). A plotted
line requires 3:1 contrast; an axis label requires 4.5:1. Those four colours
needed darker variants to meet text contrast on a white card without changing
the plotted line colours.

### Neutral

- **Paper** (`hsl(0 0% 98%)`) and **Card White** (`hsl(0 0% 100%)`): the page
  ground and the tile surface. The tile is lighter than the page it sits on;
  that one step is most of the depth in the light theme.
- **Ink** (`hsl(0 0% 0%)`) and **Card Ink** (`hsl(0 0% 9.5%)`): body and tile
  text.
- **Muted Ink** (`hsl(0 0% 32%)`): labels, units, captions and axes. Use this
  token to de-emphasise text.
- **Hairline** (`hsl(0 0% 90%)`): borders, dividers, tick minors.
- **Alarm Red** (`hsl(359 75% 50%)`): raised alarms and destructive
  confirmation only. Brightened to `hsl(359 100% 70%)` on dark grounds so it
  stays unmistakable, never re-hued.

### Named Rules

**Colour proportions.** Follow 60-30-10. Surfaces are `bg-background` and
`bg-card` (60%). Structure is `text-muted-foreground` and `border-border` (30%).
The remaining 10% is `text-primary` for chrome, `text-gauge-primary` /
`text-gauge-secondary` for readouts, and raw palette colours (`amber-*`, `red-*`,
`emerald-*`) for alert semantics only. No colour is ever decorative.

**Controls and readouts.** Use blue for controls and amber or teal for readings.
Do not use Signal Blue for a main readout or Deep Amber for a button.

**Alert colours.** Red and amber alert states are not
reskinnable. The instrument skin may brighten red for a dark board; it may not
recolour it, and it defines no alert palette of its own.

## Typography

**Display Font:** Geist Mono (falling back to `monospace`), as
`var(--font-display)`
**Body Font:** Geist Sans (falling back to `sans-serif`), as `var(--font-sans)`

**Usage:** Readouts, units and instrument labels use mono with tabular figures
to keep changing numbers aligned. Prose, form labels and settings text use sans.

### Hierarchy

- **Hero readout** (Geist Mono, 400, `text-4xl` 2.25rem to `text-7xl` 4.5rem,
  `leading-none`, tabular figures): the one number a tile exists to show. Battery
  state of charge, depth, wind speed. Scales down at `md` and back up at `lg` so
  it fills the tile at every board width.
- **Secondary readout** (Geist Mono, 400, `text-lg` to `text-2xl`,
  `leading-none`, tabular figures): the supporting numbers stacked under or
  beside the hero.
- **Tile title** (Geist Mono, 400, `text-xs` 0.75rem, uppercase,
  `tracking-[0.14em]` tightening to `0.22em` at `sm` and up, muted): the label
  across the top of every tile, followed by a hairline rule to the edge.
- **Title** (Geist Sans, 600, 1rem, `leading-none`): dialog and card titles in
  settings and configuration surfaces.
- **Body** (Geist Sans, 400, `text-sm` 0.875rem, 1.5): help text, descriptions,
  settings prose.
- **Micro-label** (Geist Sans, 400, `text-[10px]`, uppercase,
  `tracking-[0.16em]`, muted): axis labels, chart legends and KPI identifiers.

### Named Rules

**Small text sizes.** Below `text-xs` (12px) there are exactly three sizes
and no others. `text-[11px]` for inline secondary values and unit suffixes.
`text-[10px]` for uppercase micro-labels, the default for anything else small.
`text-[9px]` for dense map and marker annotation, and that is the floor. Never
`text-[8px]`.

**Small text contrast.** Do not stack low-opacity modifiers
(`text-white/50`, `text-white/60`) on anything at or below `text-[11px]`.
Reduced contrast on already-tiny glyphs is illegible in daylight glare. Use
`text-muted-foreground` on themed surfaces; on non-themed overlays such as map
HUDs, never below `/80`.

**Number alignment.** Every number that updates carries `tabular-nums` and
`leading-none` to prevent reflow as its value changes.

**SVG text.** Chart `<text>` uses a size from the three-size
scale as a bare string (`fontSize="10"`) and takes `fill` from a token
(`hsl(var(--muted-foreground))` for axis and legend, `hsl(var(--primary))` for
emphasis). A hardcoded `rgba()` or hex silently stops adapting to dark mode.

## Layout

The dashboard is a 12-column drag-and-drop grid (`react-grid-layout`) with a
32px row unit and a 16px margin on both axes, so a tile's height is
`rows × 32 + (rows − 1) × 16`. Below the `sm` breakpoint it collapses to a
two-column CSS grid, where any tile authored at half the board or wider goes
full-bleed rather than being squeezed into a half. Breakpoints are Tailwind
stock and unoverridden: `sm` 640, `md` 768, `lg` 1024.

Spacing has two levels. The outer board uses `gap-4 p-4` (or
`gap-6 p-6`); nested KPI sub-cards inside a tile tighten to `gap-2` and
`p-2`–`p-3`. Every tile at a given level shares identical inner padding to keep
groups visually consistent. Tile padding itself
is `py-4` with `px-3` tightening from `px-4` at phone width, and the title's
letter-spacing gives up before the padding does.

Prevent overflow with `min-w-0` on flex children and
`minmax(0, 1fr)` in grids, and `truncate` or `line-clamp-1` on anything of
variable length. Nothing wraps to a second line and breaks the vertical grid
unless it is a graph or a log.

### Named Rules

**Text overflow.** No string, however long, may widen its parent. An
operator-supplied embed title, a vessel name, a unit label: all of them truncate.

## Elevation & Depth

The default board is flat. In light and dark themes, tile surfaces differ
slightly from the page, with hairline borders and a subtle `shadow-sm`.
Stronger shadows are reserved for dialogs, popovers, toasts and switch thumbs.

The instrument skin resembles a lit MFD. It uses a radial bezel gradient behind
the dial, an inset rim at 30% opacity, a 60px outer glow, a vignette hub that
separates the readout from its band, and two drop shadows on the fuel rail for
a bright core and a wider halo. These effects are off by default: `--dial-bezel`, `--dial-hub` and
`--fuel-bar-glow` are `none` on `:root`, so an unskinned page renders flat.

### Shadow Vocabulary

- **Tile rest** (`box-shadow: 0 1px 2px 0 rgb(0 0 0 / 0.05)`): subtle separation
  from the page, without making the card appear raised.
- **Instrument bezel** (`inset 0 0 0 3px hsl(var(--dial-rim) / 0.30), inset 0 8px
  40px hsl(230 60% 3% / 0.6), 0 0 60px hsl(var(--dial-glow) / 0.35)`): the lit
  rim and ambient glow of a skinned dial. Skin only.
- **Needle glow** (`0 0 6px hsl(var(--primary))`): the pointer's emission on a
  skinned dial. `0 0 0 transparent` unskinned.
- **Fuel bar bloom** (`drop-shadow(0 0 2px …/0.9) drop-shadow(0 0 10px …/0.5)`):
  a full `filter` value, two stacked shadows for core and halo. Skin only.

### Named Rules

**Default surfaces.** Surfaces on the default board are flat. If something
needs to stand out, it gets a border, a tonal step or a token colour, not a
shadow.

**Instrument depth.** Depth appears only where it reproduces real
instrument hardware, and only inside the instrument skin. Glass, gloss and
drop-shadowed chrome must not be applied to the rest of the app UI.

## Shapes

Corners come off one `--radius: 0.5rem` base and step through
`calc(--radius − 4px)`, `calc(--radius − 2px)`, `--radius`, `×2` and `×3`. In
practice: tiles and cards are `rounded-xl` (16px), controls are `rounded-md`
(6px), small badges are `rounded-sm` (4px), switches and map pills are fully
round. The skinned board adds its own `--board-radius` of 0.75rem, which is
inert (`0px`) when unskinned so an unskinned page's layout is byte-identical.

Borders are the primary separator: a single hairline at `--border`, and inside a
tile a `h-px` rule at 70% border opacity running from the title out to the edge.
The dial has a thin rim (3px track at r130) in the default skin. The instrument
skin widens it into a band (40px at r96)
with major and minor tick rings.

## Components

### Buttons

- **Shape:** softly rounded (`rounded-md`, 6px), never pill, never square.
- **Size:** a 40px floor on both axes (`min-h-10 min-w-10`), with `h-10 px-4` the
  default, `h-9 px-3` small, `h-11 px-8` large, and a 40×40 icon square. The
  minimum supports touch use on a moving boat.
- **Primary:** Signal Blue ground, pale blue ink, `text-sm font-semibold`,
  hovering to 90% opacity of the same blue.
- **Focus:** a 2px ring in `--secondary` with a 2px offset against the
  background, on `focus-visible` only. Focus is always visible, never suppressed.
- **Secondary / Outline / Ghost:** secondary grey ground; or a bordered card-white
  surface hovering to muted; or transparent hovering to muted. Disabled drops to
  50% opacity with pointer events off.

### Tiles

Every widget uses the tile wrapper. It is a `Card`
with `py-4`, `px-3` (`px-4` at `sm`), header and body flush.

- **Header:** a mono uppercase tracked title in muted, optional 14px icon, then
  a hairline rule filling the remaining width, then an optional action slot.
- **Body:** whatever the widget draws, at the tighter nested density.
- **Stale state:** the tile's whole content goes to `grayscale` and an amber
  outlined badge appears beside the title with the age of the last update.
  This distinguishes stale values from live measurements. Do not also reduce
  opacity: faded values are harder to read in sunlight and may look like a dim
  screen rather than a stale feed.
- **Do not** invent a shared `MetricTile` or `StatCard`. Each widget composes its
  own KPI stack inside `Tile`. That is the established pattern.

### KPI Stack

The recurring unit inside a tile: a muted uppercase micro-label on top, then a
much larger mono readout under it in a gauge token, tabular, `leading-none`,
scaling by breakpoint. Supporting values sit beside or below in the same font at
`text-lg` to `text-2xl`, usually in the teal.

### Cards / Containers

- **Corner:** `rounded-xl` (16px).
- **Background:** card white on the page ground; hairline border; `shadow-sm`.
- **Internal padding:** `px-6 py-6` in settings and dialog contexts, tightened to
  `px-3`/`px-4` and `py-4` when used as a dashboard tile.

### Inputs / Fields

- **Style:** 40px tall, `rounded-md`, hairline `--input` border, background
  ground (not card), `text-base` dropping to `text-sm` at `md`.
- **Focus:** 2px `--ring` ring with a 2px offset. No border-colour-only focus.
- **Disabled:** 50% opacity, `cursor-not-allowed`.

### Switches

44×24 track, fully round, `--input` unchecked and Signal Blue checked, with a
20px background-coloured thumb carrying the only meaningful shadow in the
control set. State comes from `data-[checked]` / `data-[unchecked]`.

### Alarm Banner

Full-width, `rounded-lg`, alarm red border over a 10% red wash, red text, with
an uppercase tracked `text-sm font-semibold` headline and a `text-xs` message
under it. Both truncate. The banner must stand out from ordinary tile content.

### Dial (Signature)

An SVG dial in a 280 viewBox, fully token-driven so a skin repaints it without
touching the component (ADR 0054). Flat: a 3px track at r130, an 8px band at the
same radius, three gradient stops in Signal Blue at 0.55 / 0.775 / 1, muted tick
marks, a 14px label, and a pointer bar spanning r88–r122 in Deep Amber. Skinned:
the band moves in to r96 and widens to 40px so the scale numbers sit inside it
and the ticks along its outer edge, the bezel and hub light up, ticks go near
white at 4px, labels to 17px/600, and the pointer whitens and gains its glow.

### Fuel Rail (Signature)

The same idea as a straight bar (ADR 0061): colour is tokenised so a skin can
light it, but radii and stroke widths stay JS constants in the component,
because the tick radii are derived from the bar radii and a skin free to move
one and not the other could put a bar through its neighbour.

## Do's and Don'ts

### Do:

- **Do** reach for semantic tokens first: `bg-background`, `bg-card`,
  `text-foreground`, `text-muted-foreground`, `border-border`, `text-primary`,
  `text-gauge-primary`, `text-gauge-secondary`. They adapt across light, dark
  and the instrument skin with no `dark:` class.
- **Do** put every updating number in `font-display` with `tabular-nums` and
  `leading-none`.
- **Do** stack a KPI as muted uppercase micro-label over a large gauge-token
  readout, and build it inside `Tile` rather than inventing a shared primitive.
- **Do** keep the two-tier density: `gap-4 p-4` on the board, `gap-2` / `p-2`–`p-3`
  nested, with identical inner padding across tiles at the same level.
- **Do** guard every flexible axis with `min-w-0` / `minmax(0, 1fr)` and truncate
  variable-length strings.
- **Do** give a skin new values for token names that already exist, so components
  never branch on which skin is active.
- **Do** show structural dashes (`--`), real operational defaults (`0.0`) or an
  explicit `ON` / `OFF` in a zero state.

### Don't:

- **Don't** write raw arbitrary colour values (`bg-[#f4f3ef]`, `text-[#334455]`).
  Tokens, or the standard palette scale for alert semantics only. Arbitrary
  tracking values for fine-tuning stay fine; arbitrary colours never are.
- **Don't** put a gauge token on chrome or a chrome token on a reading.
- **Don't** fade small text with opacity. Below 12px, de-emphasis is
  `text-muted-foreground` and nothing else.
- **Don't** go below `text-[9px]`, or use any size below 12px outside the three
  sanctioned steps.
- **Don't** add shadows to the default board to create hierarchy. Border, tonal
  step or token colour instead.
- **Don't** dress the app in a generic SaaS dashboard: rounded pastel cards,
  gradient hero statistics, decorative illustration or purple-on-white.
- **Don't** apply skeuomorphic marine gloss. No brushed steel, teak, leather or
  decorative chrome bezels. MFD-style depth effects are limited to the
  instrument skin.
- **Don't** style forecasts like a consumer weather app: no photographic
  backdrops, animated precipitation or saturated gradient skies behind the
  numbers.
- **Don't** invent placeholder metrics or marketing copy in an empty state.
