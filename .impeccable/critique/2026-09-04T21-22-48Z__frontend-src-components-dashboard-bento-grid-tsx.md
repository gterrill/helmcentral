---
target: frontend/src/components/dashboard-bento-grid.tsx
total_score: 21
max_score: 40
na_heuristics: 
p0_count: 2
p1_count: 3
target_identity: "file:/Users/gavinator/Work/pikorua/helmcentral/frontend/src/components/dashboard-bento-grid.tsx"
target_fingerprint: "sha256:48150297d687cd2a2a5dd90ed983020dc11e85061e753c76c8e958c9e5cfc3cd"
target_path: /Users/gavinator/Work/pikorua/helmcentral/frontend/src/components/dashboard-bento-grid.tsx
timestamp: 2026-09-04T21-22-48Z
slug: frontend-src-components-dashboard-bento-grid-tsx
---
Method: dual-agent (A: design review, isolated · B: detector evidence, isolated). Neither saw the other's output.

# Critique: the dashboard board

`frontend/src/components/dashboard-bento-grid.tsx` · Operate mode

## Design Health Score

| # | Heuristic | Score | Key issue |
|---|---|---|---|
| 1 | Visibility of System Status | 2 | `Tile` has a stale mechanism; 2 of 24 tile users pass `stale=`. Depth, wind, position, tanks can never go stale. |
| 2 | Match System / Real World | 3 | Excellent marine vocabulary undercut by CZone rendering `BANK 0 CIRCUIT 7`, `VENUS-0`. |
| 3 | User Control and Freedom | 2 | No undo. Page delete is one unconfirmed click, 3.5px from rename. |
| 4 | Consistency and Standards | 1 | Four interactive colours across four controls; raw hex in two tiles; the wind hero is non-tabular sans. |
| 5 | Error Prevention | 2 | Raise confirms, autopilot holds-to-confirm. 14 breaker toggles are single-tap. |
| 6 | Recognition Rather Than Recall | 2 | Tank state is colour-only and thresholds invert by tank kind. |
| 7 | Flexibility and Efficiency | 3 | Real power in pages, grid, gauges. Zero keyboard path; layout mode vanishes below `lg`. |
| 8 | Aesthetic and Minimalist Design | 2 | Anchor Watch: overlay panel over 40% of the map, controls clipped, AIS labels overprinting. |
| 9 | Error Recovery | 2 | CZone's stale-data message is exemplary. The drag alarm says only `ALARM`, never how far or past what. |
| 10 | Help and Documentation | 2 | The only in-context help is a pointer-only tooltip on a touchscreen. |
| **Total** | | **21/40** | **Acceptable — significant improvements needed** |

## Design Specificity Verdict

**LLM assessment:** the data is boat-specific; the design is a competent generic dashboard with marine labels on it, plus two or three tiles where someone thought properly about the boat.

Genuinely authored: the anchor overlay's DISTANCE / BEARING / RADIUS / DEPTH / CURRENT / SCOPE; Depth & Tide projecting `Est. low 3.7 m` from live depth against the tide curve; Battery's Time Remaining and Charge Rate pair; the Live badge requiring both halves of the pipe to be healthy.

Category-interchangeable: the board is stock bento-editor idiom (circular `X`, `Copy`, `GripVertical`, dashed outlines) that does not know it is arranging instruments. No tile can be more important than another, by construction. `DEPTH & TIDE` and `CZONE` are typographically identical.

**Deterministic scan:** the CLI pass returned zero findings on all 12 files, and that result is worthless. Controls proved it: a deliberately awful `.tsx` (11px text, #ccc on #ddd, 18px tap target, clickable div) also returned zero, while the same markup as `.html` fired five. The detector only regex-matches non-HTML, so it has no real coverage for TSX.

The in-browser pass is the signal: 168 flagged elements carrying 199 findings. After striking false positives:

| Count | Rule | Verdict |
|---|---|---|
| 15 | `low-contrast` (real DOM backgrounds) | Real. 4.1:1 x12 anchor panel, 2.6:1 x2 gust readouts, 3.6:1 x1 Drop button. |
| 25 | `layout-transition` | Real. `transition: width`/`height` animating layout properties. |
| 13 | `buried-raster` | Real. opacity-0 raster on every resize handle. |
| 8 | `text-occlusion` | Real. "KINGFISH" 50% covered; "4.8" 67% covered. |
| 2 | `clipped-overflow-container` | Real. |
| 20 | `low-contrast` over the map | False positive. Detector can't sample canvas; computed a nonsense 1.0:1. |
| 79 | `undersized-ui-text` | False positive. DESIGN.md's Three Sizes Rule sanctions 10px and 9px. |
| 35 | `nested-cards` | False positive. DESIGN.md specifies nested KPI sub-cards as the architecture. |
| 1 | `overused-font` (49% mono) | False positive. Intended on a board of numbers. |
| 1 | `em-dash-overuse` | False positive. 11 of 12 are no-data placeholders. |

Where they converge: both assessments landed on Anchor Watch independently. A called it unusable at tile size; the detector separately found the occlusion geometry and the 4.1:1 panel text. Same for the Drop button: A flagged off-system teal, the detector measured 3.6:1.

What the detector caught that A missed: the 25 layout-property transitions and the 13 opacity-0 rasters.

What no detector could catch: both P0s.

## Overall Impression

The engineering judgment underneath this board is better than the design on top of it. The freshness model, the two-sided Live badge, the null-age reasoning: genuine alarm-fatigue design, written down and argued. Then it is deployed on two tiles out of twenty-four, and the board presenting it has no hierarchy, no colour rule, and a silence path that erases the alarm it silenced.

Biggest opportunity: decide what the board's hero is. The largest thing on screen is `100 %` battery; the number that determines whether the boat moved is 14px mono in a translucent overlay.

## What's Working

**The freshness model is intellectually correct, down to the null case.** `lib/staleness.ts` sets 120s deliberately clear of the refresh interval, computes ages against the vessel clock so a wrong browser clock cannot lie, and treats a null age as not stale with the reasoning written down: absence of evidence is not evidence of staleness, and flagging it forever teaches the operator to ignore the indicator.

**The Live badge refuses to lie about which half of the pipe is broken.** It requires `signalkConnected` and a healthy browser stream, with a distinct amber Reconnecting between. Two failure modes freeze the same tiles and the design acknowledges both.

**Density discipline inside a tile is real.** The KPI stack holds across four tiles with no shared primitive, and the responsive step-downs are measured rather than guessed.

## Priority Issues

### [P0] A silenced anchor drag renders nothing on the board

**What:** `use-anchor-alarm.ts:189-190` derives `isAlarming` as `alarm !== null && !isSilenced`. The tile gates its red strip on `isAlarming`; `alarm-banner.tsx:24` filters `phase !== 'acknowledged'`. Both vanish the instant Silence is pressed, while the vessel is still outside its radius.

**Why it matters:** PRODUCT.md names this exact failure as the one the product is judged against. Silencing a klaxon at 0300 must not erase the reason it rang. Silence is currently indistinguishable from "problem solved."

**Fix:** split audible from visible. Return a third state from the hook; render silenced-but-active as a persistent amber strip reading `DRAGGING — ALARM SILENCED` with live `{distance} m of {radius} m` and an Unsilence button. Stop filtering acknowledged alarms out of the banner. Add `role="alert"` to the tile strip.

**Suggested command:** /impeccable harden

### [P0] Staleness is wired on 2 of 24 tiles

**What:** only `battery-power-tile.tsx` and `solar-tile.tsx` pass `stale=`. Depth, anchor watch, tanks, position, wind, nearby vessels cannot enter the state.

**Why it matters:** depth is the number you run aground on. A dead transducer leaves `4.8 m` on screen forever at full contrast, indistinguishable from a live sounding. The Live badge only catches total stream loss. Principle 1 rests on distinguishing quiet from broken.

**Fix:** thread `lastUpdateAgeS` through every tile whose source can die. Make the treatment additive, not subtractive: `opacity-50 grayscale` reads as "dim screen" in sun. Keep grayscale, replace the fade with an amber left rule. Raise the badge off `text-[9px]`.

**Suggested command:** /impeccable harden

### [P1] Contrast fails on the readouts DESIGN.md says cannot fail

**What:** measured against real DOM backgrounds, no canvas. Anchor panel labels and values at 4.1:1 (12 elements, needs 4.5:1). Gust readouts at 2.6:1 (needs 3:1). Drop button at 3.6:1.

**Why it matters:** DESIGN.md's argument for permitting 9px and 10px type is that contrast is non-negotiable in exchange. That bargain is unpaid on the anchor readout and the primary action.

**Fix:** darken the anchor overlay ground or lift the label token until the panel clears 4.5:1 at its transparent right edge. Move gust readouts onto `--chart-gust-label`, which exists for this reason. The Drop button resolves under the next issue.

**Suggested command:** /impeccable audit

### [P1] Interactive colour has no rule, and the most important button on the board is teal

**What:** Drop is `bg-teal-600` (the readout colour). CZone switches and autopilot engage are `bg-emerald-600`. Generator Start is correctly Signal Blue. `tanks-tile.tsx:43-50` improvises `#dc2626`, `#d97706`, `#047857` inline; `wind-compass.tsx` carries six hardcoded hex values including a navy absent from the palette.

**Why it matters:** the Two Accents Rule is the load-bearing legibility idea of the system. With teal on the primary action, emerald on switches and permanent green on eight tank bars, colour means nothing, which costs the one channel left when something goes wrong in glare.

**Fix:** Drop to the default Button variant; prominence comes from full width, not hue. CZone and autopilot to `bg-primary`. Tanks neutral at rest, alert colours only at threshold. Compass fills to `hsl(var(--...))`, which also fixes it vanishing in the instrument skin.

**Suggested command:** /impeccable colorize

### [P1] Anchor Watch is unusable at tile size

**What:** in a ~440x235 map viewport: a translucent panel covers ~40% of the width and fades where `4.8 m` sits over moored boats; six controls stack down the right edge with the bottom two clipped; AIS labels at 9px white overprint the panel and each other. Three of six rows read `— m`. The detector independently confirmed the occlusion.

**Why it matters:** this is the tile the product exists for, delivering roughly 180px of actual chart. Nothing supports the primary judgment: is the boat where I left it.

**Fix:** promote the answer out of the map: a KPI stack of DISTANCE over `distanceMeters` at `text-5xl font-display text-gauge-primary` with `of {radius} m` beside it. Suppress dashed rows when not anchored. Collapse six map controls to fullscreen plus zoom.

**Suggested command:** /impeccable layout

## Persona Red Flags

**Alex (power user):** the drag handle is a non-focusable div, so he cannot touch his own layout from the keyboard; no shortcut for edit mode, alarm acknowledge or page switch. Layout mode is gated at `lg`, so on an iPad the board he built is read-only. `Trash2` calls `onDelete(page.id)` with no confirm, 3.5px from rename. Tanks defeat triage: 100%, 74% and 56% are the same green.

**Sam (accessibility-dependent):** the wind readout does not exist to a screen reader; the svg holds speed, angle and side as SVG text children behind `aria-label="Wind compass"`. Tank state is colour-only. 24 tiles produce zero headings because `CardTitle` is a div. Writable versus read-only on CZone is communicated by `cursor-not-allowed`, which does not exist on a touchscreen.

**The single-handed operator on watch (from PRODUCT.md):** 0300, wind up, alone at anchor. The screen is white because Day is default and the instrument skin sits behind edit mode, desktop-only. Night vision gone. The anchor tile is one of nine identically-weighted cards. Distance is 14px mono in an overlay while the largest number is battery percentage. Neither anchor nor depth passes `stale`, so he cannot tell if the number is alive. Silence is a 32px button against DESIGN.md's non-negotiable 40px floor. Having silenced it, his board goes calm while the boat is still dragging.

## Minor Observations

- `marine-header.tsx` renders 'VESSEL NAME NOT SET' in the same `text-primary` display type as a real vessel name.
- 25 layout-transition hits animate width/height rather than transform.
- 13 opacity-0 raster backgrounds on resize handles.
- The left column ends in ~400px of dead whitespace while the right runs past the fold; RGL packs per column with no balancing.
- The Grafana windrose drops third-party chrome inside a tile with no quarantine; its 8-band ramp contradicts the closed chart vocabulary.
- `battery-power-tile.tsx` uses alert `text-amber-600` for discharging, the normal state at anchor, three sub-cards from `text-gauge-primary` amber at nearly the same hue.
- `alternator-tile.tsx` uses `text-[9px]` for Amps/Volts labels, the map-annotation floor applied to instrument labels.
- `depth-tide-tile.tsx` wraps the tile in a bare onClick div: no role, no tabIndex, no focus ring.
- [P2] CZone renders 14 toggles with writable and read-only visually identical; split into CONTROLS and INDICATORS and surface `meta.displayName`.
- [P3] `CardTitle` to h2; give the drag handle `role="button" tabIndex={0}` with arrow-key handlers.

## Questions to Consider

1. If a screen with colour on it is a screen telling you something, why is the resting board green?
2. What is the board's hero, and why is it not decided per page?
3. Should staleness be opt-in at all? What if `Tile` required an `ageSeconds` prop typed `number | null | 'no-source'`?
4. Why does the operator ever see `BANK 0 CIRCUIT 7`?
5. If an acknowledged-but-active alarm stayed on the board permanently, would you need the anchor tile's second alarm design at all?
6. The instrument skin is your best asset and it is three gated clicks away, desktop-only. What if it were the default between sunset and sunrise at the vessel's own position?
