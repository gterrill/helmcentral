---
target: the forecast drawer
total_score: 21
max_score: 40
na_heuristics: 
p0_count: 2
p1_count: 2
target_identity: "file:/Users/gavinator/Work/pikorua/helmcentral/frontend/src/components/forecast-drawer.tsx"
target_fingerprint: "sha256:f435150b443fb5349a1b90fbda767474ee148c3335528f245175d83ba96e4317"
target_path: /Users/gavinator/Work/pikorua/helmcentral/frontend/src/components/forecast-drawer.tsx
timestamp: 2026-09-05T01-34-05Z
slug: frontend-src-components-forecast-drawer-tsx
---
`Method: dual-agent (A: design review · B: detector + browser evidence)`

# Critique — Forecast Panel
`frontend/src/components/forecast-drawer.tsx` · Mode: **Operate** · 2287 lines

## Design Health Score

| # | Heuristic | Score | Key Issue |
|---|---|---|---|
| 1 | Visibility of System Status | 2 | Freshness is 12px grey text in 2 of 6 card headers; no stale escalation at any age; three unaggregated provider clocks |
| 2 | Match System / Real World | 3 | Excellent domain language; undercut by "1:190" steepness ratios never explained and two derived numbers dressed as readouts |
| 3 | User Control and Freedom | 2 | Unguarded `behavior:'smooth'` on every day click (`:673`); nothing collapses; selection silently resets to day 0 (`:655`) |
| 4 | Consistency and Standards | 2 | Abandons `Tile` chrome for gradients + shadow; `--chart-wind` (hue 226) collides with `--primary` (hue 226); `tabular-nums` used once in 2287 lines |
| 5 | Error Prevention | 2 | Nothing prevents reading a derived Visibility as measured, or a day-old cache as current |
| 6 | Recognition Rather Than Recall | 2 | Comparing two days is pure memory: click, scroll 1200px, memorise, scroll back |
| 7 | Flexibility and Efficiency | 2 | 10 sequential tab stops across the day strip, no roving tabindex, no arrow-key day switching |
| 8 | Aesthetic and Minimalist Design | 2 | Gradients and shadows on an explicitly flat board; decorative span meter; L/H markers on a 1°C range |
| 9 | Error Recovery | 1 | An outage renders smaller and fainter than the success prose beside it. Inverts "Fail loudly" |
| 10 | Help and Documentation | 3 | Per-chart key lines are genuinely good ("A full feather is 10kt, a half feather 5kt") |
| **Total** | | **21/40** | **Acceptable — significant improvements needed** |

## Design Specificity Verdict

**LLM assessment.** The words are authored for a boat. The chrome is a weather app.

The prose layer is unfakeably specific: "Roughly one wave in seven reaches the significant height" (`:1886`), "Seas outrunning the wind that could have built them" (`:1138`), wind barbs at 10kt/5kt feathers, a 500mb trace with a quintile band, all sourced to *Surviving the Storm* with page citations in the comments. No consumer weather app contains any of it.

Then the frame it sits in. `ForecastPanel` (`:319-366`) is a bespoke replacement for the shared `Tile` that deviates on five axes at once: gradient body instead of flat (`:343`), coloured drop shadow instead of border, `rounded-2xl` instead of `rounded-xl`, a Geist Sans `text-foreground/70` title instead of the mono uppercase tracked title with its hairline rule, and — the expensive one — no `stale` prop at all. This is the one panel in the app that opted out of the board chrome, and it lost the stale machinery on the way out.

The panel also argues against itself in its own comments. `:1333` and `:1437` state the thesis "size follows consequence — wind owns the display slot" and implement it correctly in the hour tiles and day cards. Then the details card (`:1467`) gives `text-4xl` amber to temperature and puts wind in a 14px grey chip.

**Deterministic scan.** The CLI detector returned `[]`, exit 0 — and that result is worthless. Assessment B verified it by writing a synthetic file containing `bg-[#ff0000]`, `text-[8px]`, `dark:bg-black` and `text-white/50`: as `.tsx` it scored 0 findings; the identical violations as `.html` scored 4. The regex path is a no-op for `.tsx`. Treat the file scan as not exercised.

The browser overlay is the working path, and it ran: 167 anti-patterns page-wide, 150 enumerable. After filtering, three findings are real and two of those are detector wins over the human review:

- `low-contrast` x2 — `#c78100` at 3.1:1 and 3.0:1 against near-white. That is `--gauge-primary` amber on the hour tile label (`:1356`) and the "Sunset" headline (`:1361`). Both fail 4.5:1 AA, on the exact surface whose binding viewing condition is direct sunlight at arm's length.
- `ai-color-palette` x3 on the `ForecastPanel` headers (`:343`). The "cyan gradient" read is a false positive on hue — the source is `linear-gradient(180deg, hsl(var(--card)/0.96), hsl(var(--muted)/0.92))`, all tokens. But it is a true positive on the thing that matters: there is a gradient here, on a board whose design system opens with "no gradients."
- `text-2xs` + `/80` opacity stacking at `:1383` — found independently by both assessments.

The other 147 are noise: 112 `undersized-ui-text` are all 10px `text-2xs`, which the project explicitly sanctions as one of three legal steps, so 75% of the headline count is a rule-threshold conflict rather than a defect. 3 `layout-transition` are the sidebar and `body`, not this file. 17 of 18 `nested-cards` are the day-tab strip's own structure.

Clean on the mechanical checks: zero raw arbitrary colours, zero hardcoded SVG fills (all 63 rendered `<svg text>` nodes use `hsl(var())`), zero off-scale font sizes, zero `dark:` variants. The token discipline in this file is genuinely good.

**Visual overlays.** Injection succeeded and the overlay ran in the page, but the tab was closed and the live server stopped before reporting — there is no overlay left visible in the browser now.

## Overall Impression

This is a panel with excellent judgment and a broken finish. Someone read Dashew, understood that a forecast read as its headline number understates what you'll meet, and built the interpretation layer to say so. Then wrapped it in chrome borrowed from the category it was trying not to be, and skipped the one state the whole product is organised around.

The single biggest opportunity: this panel already computes its own answer and then buries it. `extendedIntro` (`:773-809`) produces "Upper air supports a surface low developing on Sun 13 and Mon 14" — and renders it in the same size, weight and colour as "Mostly Sunny conditions will continue all day." Give the verdict the weight, demote the five charts to what you open when you disagree with it, and most of the cognitive-load failures below dissolve.

## What's Working

1. **The four-outcome upper-air intro has correct epistemics** (`:773-809`). No provider installed → silence. Checked and clear → a sentence. A day flagged beyond the visible strip → point at the trace, not at a day the reader can't click. It refuses to let "we didn't look" and "we looked and it's fine" render the same, which is exactly the failure this product names as fatal.

2. **Constant cross-day chart framing, plus the refusal to invent a threshold** (`:1011-1034`, `:1476-1481`). Every day's wind is framed against the window's max so a 6kt day reads calm against the 20kt one coming; wind gets no colour escalation because no sourced threshold exists. The second is rarer and harder than the feature it declined to build.

3. **Chart accessibility is better than most production dashboards.** All five charts carry `role="img"`, `tabIndex={0}`, a real descriptive `aria-label`, an explicit `focus-visible` ring, and a prose key line. 16 interactive elements in the panel, zero without an accessible name.

## Priority Issues

### [P0] The panel has no stale state, in the app whose defining risk is silent staleness
`grep grayscale forecast-drawer.tsx` returns nothing. `components/ui/tile.tsx:74` implements the system's stale treatment with a comment calling it "the system's loudest state, not its quietest" — and `ForecastPanel` never takes the prop. Freshness appears only as `formatRefreshAge` in `text-xs text-muted-foreground` at the right edge of two card headers (`:1530`, `:1859`). That function renders "27 hours ago" in exactly the same grey as "just now". The hourly strip and the 10-day strip carry no age at all.

**Why it matters:** PRODUCT.md names the defining risk as "a frozen dashboard looks exactly like a calm night." This is the panel most likely to be read immediately before an anchoring or departure decision, over the unreliable internet the product assumes, and a three-day-old cached forecast is pixel-identical to a live one.

**Fix:** Add `stale` + `ageLabel` to `ForecastPanel` (`:319-366`), computed as `age > 3 x ttlSeconds`. Grayscale the panel body and put the amber outlined age badge in the header slot at `:356` — that slot currently holds the decorative span meter, which encodes nothing the text 8px to its right doesn't already say. Wire `waveUpdatedAt`/`waveTtlSeconds` the same way.
**Suggested command:** `/impeccable harden`

### [P0] Humidity and Visibility are invented numbers presented as instrument readouts
```
:697  const humidityPct   = ... Math.round(45 + (precipitationPct * 0.4))
:698  const visibilityNm  = ... Math.max(1, 12 - (precipitationPct * 0.06))
```
Rendered at `:1500-1501` as "Humidity 63%" and "Visibility 9.4 nm", in the same row, same styling, same authority as the measured Wind and Gusts. They are arithmetic on chance-of-precipitation.

**Why it matters:** Visibility is a navigation-safety number. A skipper who reads "Visibility 9.4 nm" will believe the forecast said 9.4 nm. Nobody measured or forecast it. This is a direct hit on the "no invented placeholder metrics" don't and on the fallback policy in `AGENTS.md`. The file already gets this exactly right one field over, where `precipitation: number | null` carefully distinguishes "no data" from a real 0% (`:34`).

**Fix:** Delete both, and their two chips. If the provider ships real fields, plumb those through; if not, show nothing.
**Suggested command:** `/impeccable harden`

### [P1] The only control in the panel is broken three ways
- **It isn't sticky.** `:1402` sets `sticky top-0 z-10`, and `selectDay` computes `scrollMarginTop` from the row's height on that assumption — but `:343` puts `overflow-hidden` on the parent `<section>`, establishing a scrollport that never scrolls. Verified live: at the Wind and Wave charts the day strip is entirely out of the viewport. The `scrollMarginTop` compensation then adds a phantom gap on every selection, correcting for a header that isn't there.
- **Selection is colour-only and nearly invisible.** `border-primary/50 bg-primary/5` — a 5% blue tint, on a screen read in direct sun.
- **Screen readers get nothing.** Ten buttons named "Select forecast day Wednesday Sep 9", no `aria-pressed`, no wind, no gust, no condition, no trough flag. The TROUGH badge (`:1420`) is a bare `<span>` with `aria-label` and no role — most AT drops it, silencing the panel's single most safety-relevant marker.

**Why it matters:** The day strip drives four charts sitting 800-2000px below it. Changing days while reading the wave chart means scrolling up half a screen. WCAG 1.4.1 and 4.1.2 both fail, and the entire ten-day comparison is unreachable non-visually.

**Fix:** Drop `overflow-hidden` from `:343` (border-radius already clips backgrounds). Add `aria-pressed`, fold the card's numbers into the `aria-label`, give the TROUGH span `role="img"`, and add a non-colour selected indicator — a left rule or filled corner mark.
**Suggested command:** `/impeccable audit`

### [P1] The chrome and the digits both break the board's binding rules
Beyond the gradients already noted: `tabular-nums` appears once in 2287 lines, on an hour label rather than a value. Every wind speed, temperature, gust, precip percentage and 500mb height in this panel renders on proportional figures — the Still Digits Rule exists precisely so digits don't dance as values update, and scrubbing a chart is exactly when they do. Alongside: the two amber readouts at 3.0:1 and 3.1:1; `text-gauge-secondary/80` on 10px text (`:1383`); gauge tokens carrying chrome (borders, shadows, the span meter) in violation of the Two Accents Rule; and the sole Retry button at `h-9` (36px) against the stated non-negotiable 40px floor.

**Why it matters:** Each is individually small; together they are why the panel reads as a different product from the board it sits on. The contrast pair and the touch target fail the one accessibility requirement PRODUCT.md actually commits to — daylight legibility on a helm screen.

**Fix:** Add `tabular-nums` alongside every existing `font-display`/`leading-none` readout. Replace the panel gradients and shadow with the flat card + hairline rule idiom and the mono uppercase title. Move the amber readouts to a token that clears 4.5:1 on the card. Raise Retry to `h-10`.
**Suggested command:** `/impeccable polish`

### [P2] Failure is rendered quieter than success, and the charts don't survive sunlight
`ChartUnavailableMessage` renders `text-xs text-muted-foreground` centred — 12px grey — while the success prose beside it is `text-base text-foreground/80`, 16px near-black. "Wave forecast unavailable for this day" is literally smaller and fainter than "Significant wave height 0.5 to 0.7 m from the ENE", and offers no retry: the only Retry fires when the whole forecast is empty, not when a feed dies. Meanwhile `HOURLY_CHART_TOP/BOTTOM` (`:474-475`) give every chart a 90px-tall plot across the full panel width — 2.25px per knot on a 0-40kt frame, with 12px wind barbs and a 2.5px calm-wind circle.

**Why it matters:** The first inverts "Fail loudly" — a dead wave feed is more important than a live one. The second means the panel's primary quantitative content is unreadable under its own stated binding viewing condition.

**Fix:** Give `ChartUnavailableMessage` the amber outlined badge treatment and a retry callback; render `waveUnavailableDueToError` (`:690`) differently from "no data", since the code already distinguishes them internally. Raise the plot band to ~165px and scale the barb geometry proportionally.
**Suggested command:** `/impeccable adapt`

## Persona Red Flags

**Alex (power user, nav station):** 10 sequential Tab stops across the day strip, no arrow-key switching. Every day click costs ~400ms of unrequested `smooth` scroll with no `prefers-reduced-motion` guard (`:673`). Comparing Thursday to Saturday is click, scroll 1200px, memorise, scroll back, repeat. The intro names Mon 14; Mon 14 is the tenth card in a horizontal scroller with no scrollbar, arrow, or fade.

**Sam (screen reader, keyboard, 200% zoom):** Cannot tell which day is selected. Cannot read any day's forecast from the strip. All four charts promise "Use arrow keys to read values" in their labels — but `role="img"` prunes descendants and `ChartTooltipBubble` has no `aria-live`, so arrowing announces nothing. The promise is unkept. At 200% zoom, `min-w-[150px]` day cards and `min-w-[84px]` hour tiles don't reflow: two nested horizontal scrollers, no affordance on either.

**Rae (live-aboard, anchored, direct sun, deciding if tonight is safe):** Tonight isn't on the page — `hourlyToday.slice(0, 12)` at `:653`; read at 11:27am that ends at 9PM. The one sentence she needs is typographically identical to a pleasantry about sunshine. "Visibility 9.4 nm" is `12 - (precipChance x 0.06)` and she'll anchor on it. If the boat dropped its uplink at 0600 the page looks the same at 1400 apart from "8 hours ago" in 12px grey. And on a day nothing tripped, the wave section says nothing at all (`:1891`) — she cannot distinguish "the indicators are clear" from "the indicators weren't run", a distinction the upper-air section three panels down gets exactly right.

## Minor Observations

- Duplicate SVG gradient IDs — `tempAreaGradientId` and `uvAreaGradientId` are each defined twice in the same document (`:1587` and `:1662`). Harmless today, a trap the next time one is edited.
- `forecastChartWidth` falls back to `1000` before measurement (`:713`) — visible reflow on first paint.
- `key={idx}` on the day buttons while a stable `day.dayKey` exists.
- Steepness ratios ("7.7s 1:190") are written beside every arrow and explained nowhere.
- "the shaded zone" (`:2089`) is ambiguous — the chart has both a grey band and a red line.
- L/H temperature markers mark noise as an extreme on a day with a 1°C range.
- `selectedDayIndex` silently resets to 0 when `days.length` shrinks (`:655`).
- Provenance is split three ways (weatherkit 15m, open-meteo-marine 1h, BOM 12h), never aggregated into "what is the oldest thing on this screen."
- Density is off the two-tier scale throughout: `px-2.5`, `py-3.5`, `gap-1.5` where the system specifies `p-4`/`gap-4` outer and `p-2`-`p-3`/`gap-2` nested.

## Questions to Consider

1. What is this panel's one-sentence answer? It already computes one. What if that sentence plus a wind verdict and a wave verdict were the panel, and the five charts were what you open when you disagree?
2. If the boat lost its uplink at 0600, what on this page changes by 1400? The rest of the app grayscales. Why did this panel get an exemption from the one state the product says matters most?
3. You wrote the rule "size follows consequence, wind owns the display slot" and applied it twice. Why is temperature still the 4xl amber hero of the details card?
4. Would a skipper trust "Visibility 9.4 nm" less if it were labelled "estimated from precipitation chance"? They should. And if it isn't worth showing with that label, is it worth showing?
5. The 10-day strip shows a daily mean wind; the charts show hourly peaks. Which one is the forecast? "17 kts SE" on Thursday over a chart peaking at 25 tells the reader two things.
