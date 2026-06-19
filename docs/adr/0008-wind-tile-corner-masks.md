# ADR 0008: Wind Tile Corner-Mask Gauges and Animated Heading Arrow

## Status
Accepted

## Context
The operator produced a Claude Design mockup (`frontend/wind-compass-mockup/WindCompass.tsx`) showing a redesigned Wind tile: 4 gauge cards arranged around a central compass, each card's inner corner cut with a CSS radial-gradient mask centered on the compass so it traces a concave arc wrapping the compass with an even gap, plus a CSS-animated heading arrow. The existing `WindCompass` (SVG-based, `frontend/src/components/wind-compass.tsx`) and `WindMetricCard` (`frontend/src/App.tsx`) visuals were to be kept as-is — only those two specific techniques were to be adopted, not the mockup's bespoke fonts/colors/metallic compass styling.

## Decision

### Corner masks need a fixed pixel relationship
A mask's `radial-gradient(circle at X Y, ...)` is defined in each card's own local coordinate box, so it only stays aligned with the compass if the offset from card to compass is a known constant. The original desktop layout was fluid (grid `w-full`, cards capped at `max-w-[...]`), so the gap between card and compass grew or shrank with viewport width — a hardcoded mask radius would drift out of alignment.

Solved by giving each breakpoint's gauge cluster (mobile, desktop) a fixed-size design canvas, generalized as a `WindCanvasConfig` (`width`/`height`/`compassBox`/`topCardW`/`bottomCardW`/`cardH`/`gap`) and `computeWindMasks()` in `App.tsx`, producing one `radial-gradient` per corner: transparent up to `compassRadius + gap - 1`, opaque from `compassRadius + gap + 1` (mirrors the mockup's own math, generalized to arbitrary dimensions). `compassRadius` is derived from the compass's actual outer SVG ring (`RO = 130` inside a `280`-wide viewBox), not naively half the box width.

### Scale-to-fit instead of two fixed breakpoint sizes
The desktop block initially used one constant size for the entire `md+` range, to keep things simple. Live testing then showed the page's `lg:grid-cols-3` layout squeezes the wind tile's column to as little as ~279px right at the `lg` breakpoint (1024px viewport) — a naively fixed-width canvas overflowed there.

Fixed with `useFitScale()`: a `ResizeObserver`-backed hook that scales the fixed-size canvas down (never up — `Math.min(1, measured / design)`) to whatever width its column actually has. The scale is applied as `transform: scale()` on an absolutely-positioned inner wrapper, which preserves the mask math exactly, since masks are computed in pre-transform local coordinates and uniform scaling afterward doesn't break their alignment.

This needed a `ResizeObserver` polyfill stub added to `frontend/src/test/setup.ts` — jsdom doesn't implement it, and 6 tests that mount `<App>` started failing with `ReferenceError: ResizeObserver is not defined` once the hook landed.

### Mobile needed its own, larger canvas — not the desktop one
Reusing the desktop's compact canvas for mobile (which has no competing grid columns) would have shrunk the compass well below its previous mobile size — confirmed visually, this looked like a regression. Mobile (`WIND_MOBILE_CFG`) and desktop (`WIND_DESKTOP_CFG`) each get their own `WindCanvasConfig`, rendered through one shared `WindGaugeCluster` component (extracted to avoid duplicating the per-corner card/compass markup twice).

Mobile's design width was tuned up from 340 to 420 after the operator reported wasted space on either side at typical phone widths — the cluster wasn't filling the available column even though `useFitScale` would have let it grow that far; the design's own reference width was the smaller of the two limits.

### Compass-to-canvas ratio is what avoids clipping the gauge titles
With mobile's compass sized close to its original ~220px, the cut intruded into the bottom row's title text ("MAX GUST 10M" / "MAX GUST 1HR" got clipped) — the bottom cards anchor with their top edge nearest the compass, which is exactly where `WindMetricCard`'s title sits. Fixed by keeping each canvas's `compassBox`/`height` ratio close to ~0.64–0.65 (matching the original mockup's own 360/560 proportions) rather than independently maximizing compass size. This was confirmed empirically via screenshots at 320–767px, not derived purely from a formula.

### Heading arrow: shortest-path unwrap + CSS-property-driven transform
`windAngleApparentDeg` is a raw 0–360° value; naively transitioning between e.g. 350° and 10° would animate the long way around (340°) since CSS interpolates the numeric value, not the shortest angular path. Fixed with a `useRef`-based unwrap in `wind-compass.tsx`: each new angle is converted to `prevUnwrapped + shortestDelta`, so the value fed into the rotation keeps accumulating instead of wrapping back to 0–360, and the CSS transition always takes the shorter path.

The arrow's rotation was originally driven by the SVG `transform` *attribute* (matching the existing code and the mockup's conceptual approach), with a `transition` set via inline `style`. Live testing (Playwright against headless Chromium) showed `getComputedStyle(g).transform` stayed `"none"` throughout — the browser does not reliably animate a `transition: transform` declaration when it's the attribute, not the CSS property, that's changing. Fixed by driving rotation via the CSS `transform` property directly (`style={{ transform: 'rotate(${deg}deg)', transformOrigin: '${CX}px ${CY}px' }}`) instead of the SVG attribute. Confirmed working via a burst-screenshot capture mid-transition showing the needle sweeping through intermediate angles.

Easing was deliberately changed from the mockup's bouncy `cubic-bezier(.34,1.2,.44,1)` (overshoot) to a plain `ease-out` over 650ms — an overshoot would briefly show an incorrect heading on what's read as a precision instrument. Vessel state polls every 10s with no other smoothing, so 650ms reads as smooth without ever lagging behind the next reading.

### Border continuity on the cut edge
Masking a bordered card removes the curved edge's border along with its background — the straight edges keep their `border` (the global `* { @apply border-border; }` reset), but the cut arc had none, leaving an inconsistent outline. Fixed by adding a second background layer per corner: a thin radial-gradient "ring" in the card's border color (`hsl(var(--border))`), positioned just outside the mask's cut radius (in the always-visible zone), 1px wide to match the existing border width. It survives masking and reads as a continuous border tracing both the straight edges and the arc.

## Consequences
Positive:
- The 4 gauge cards visually wrap the compass with a precise, consistent gap on both breakpoints, matching the mockup's intent, without adopting any of its bespoke styling.
- `WindGaugeCluster` / `computeWindMasks` / `useFitScale` are generic enough that a future third breakpoint size, if ever needed, is a new `WindCanvasConfig`, not new markup.
- Both the corner masks and the arrow animation degrade gracefully: `useFitScale` only ever shrinks, never overflows a narrow column; the angle unwrap has no failure mode for `null` wind data (it resets cleanly when the arrow unmounts).

Negative / explicitly deferred:
- The desktop and mobile canvas dimensions (`WIND_DESKTOP_CFG` / `WIND_MOBILE_CFG`) are hand-tuned constants verified empirically via screenshots, not derived from a formula guaranteed to hold if `WindMetricCard`'s padding or font sizes change later — such a change would need re-verifying the corner cut doesn't clip title text again.
- `frontend/wind-compass-mockup/` is kept in the repo as the original design reference; it is not imported or built into the app.

## Related
- `frontend/wind-compass-mockup/WindCompass.tsx` — the original Claude Design mockup this was derived from.
