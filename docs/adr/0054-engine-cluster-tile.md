# ADR 0054: Engine Cluster Tile, and the Ticked Dial It Needed

## Status
Accepted

The fifth multi-instance widget, following ADR 0031, 0039, 0049 and 0052. Extends ADR 0053 (engine profiles), whose bundled profile this ADR also corrects.

## Context

A marine MFD shows an engine as one instrument: a big ticked ring with the primary reading inside it and satellite cards in the corners. Helmcentral's radial gauge could not approach it — no tick marks, no scale numbers, the readout below the arc rather than in it, and a gauge group is a uniform grid with no notion of a hero element.

But the layout already existed here. `wind-tile.tsx` is exactly that shape: a ring with four corner cards whose inner corners are mask-cut along a circle that follows the ring, on a fixed canvas that scales down to whatever column it lands in. The work was to generalise it, not invent it.

### Reading the vessel first changed the design

This was planned against a live path list rather than an assumption, and three things came back that would otherwise have been found after shipping:

- **`propulsion.port.coolantTemperature` and `.oilTemperature` do not exist.** The engine temperature is `.temperature`; the only oil temperature is `.transmission.oilTemperature`, which is the **gearbox**.
- **Exhaust temperature is published under `propulsion.0` / `propulsion.1`** — orphan nodes carrying nothing else, both reading an identical 304.749 K. Nothing on the bus says which maps to which engine.
- **Not one path carries `units` metadata**, so ADR 0039's `quantityForSIUnit` inference has nothing to work from and every hand-added gauge starts as "Unitless".

Without units metadata, profiles supply the units that would otherwise need to be configured by hand (ADR 0053).

## Decision

### 1. The bundled Cummins profile was wrong, and is fixed here

ADR 0053 shipped `coolantTemperature`, `oilTemperature` and `exhaustTemperature` as suffixes. None matched a path under the engine node. The profile applied without error but produced three permanently dashed gauges because the configured paths were absent.

Corrected to `temperature`, `transmission.oilTemperature` (relabelled from "Oil Temp" to **Gearbox** to distinguish it from engine oil), with exhaust dropped because no suffix under the engine node can reach it. `fuel.rate`, `engineLoad` and `transmission.oilPressure` added.

A new test asserts every bundled suffix resolves against a captured path list from the real vessel. The fixture is captured, not assumed — the failure mode this is guarding against is precisely a plausible-looking suffix nobody checked.

### 2. Full-suffix matching, because `transmission.oilTemperature` has a dot in it

`mergeGaugeSettingsBySuffix` matched on the last dotted segment, so `transmission.oilTemperature` would bind to a bare `oilTemperature` gauge — the wrong sensor, silently. Matching now uses the whole declared suffix, longest first.

The suffix list is a **required parameter** rather than derived from the composed path. Nothing in `propulsion.port.transmission.oilTemperature` says where the instance ends and the suffix begins, so a derived guess cannot be correct; making the caller supply it means it cannot be forgotten.

### 3. `DialRing`, shared rather than forked

The ticked arc is `components/ui/dial-ring.tsx`, used by both the cluster and the gauge's new `instrument` ring style. `wind-compass.tsx` already had the idiom working against theme tokens — polar helper, major/minor tick radii, `hsl(var(--muted-foreground))` strokes — including its 280-unit viewBox, which the radial gauge's 100×74 box is far too coarse to imitate.

Zones draw as **rim segments** rather than the translucent band across the arc that ADR 0039 used. A red band has to be readable at a glance, which is most of what the reference designs get right.

`children` renders centred in the ring, which is how both the gauge's inside-readout and the cluster's fuel-flow stack get there without either knowing about the other.

### 4. The gauge upgrade is opt-in and invisible by default

`ringStyle`, `readout` and `labelDivisor` are optional on `GaugeWidgetConfig`; absent means exactly today's rendering. Every gauge configured before this ADR is untouched, and a test asserts the plain arc still draws no ticks.

### 5. The instrument skin redefines tokens, not components

> **Scope superseded by ADR 0060.** The skin is now selected per dashboard page, not per tile. Everything below about *how* it works is unchanged and is what made that move a one-line change.

A real MFD is dark whatever time it is, so the skin is independent of the app theme.

`[data-skin="instrument"]` redefines the **same token names** every component already consumes — `--card`, `--foreground`, `--primary`, `--gauge-primary` and the rest. `Tile`, `DialRing` and every readout therefore re-skin with no per-component branching and no new props threaded anywhere. Tokens, not hex, so AGENTS.md holds.

Alert red and amber are deliberately **not** redefined. A warning has to look like a warning in either skin, and a skin that could recolour an alarm state would be a skin that could hide one. (ADR 0060 §3 refines this: at page scope the skin owns the ground under the alarm, so it now sets `--destructive` to the dark-theme red. The rule is that a red must keep reading as an alarm, and one too dim for its own background fails it.)

The vocabulary later had to grow past colour. Recreating a helm-cluster reference meant a bezel with a lit rim and an outer bloom, a wide gradient band in place of the thin value arc, brighter and heavier ticks, and a needle that glows. None of that is expressible as an HSL triplet, and none of it belonged in a component branch on `config.skin`.

So the `--dial-*` group was added: colours as triplets, radii and widths as lengths, and the bezel, its shadow and the hub wash as composite gradient and shadow recipes built out of those triplets. Composite tokens are a real extension of a vocabulary that was triplets only, but every colour inside one is still a token or a triplet, so AGENTS.md's no-hex rule holds.

**Every default is inert.** `:root` carries a value for each of them that reproduces the flat dial exactly: the bezel and hub paint `none`, the band keeps the thin arc's radius and width, and each colour indirects to the token the dial already read, which is why `.dark` needs no dial tokens of its own. `DialRing` renders the chrome unconditionally and only a skin turns it on, so the light and dark themes are untouched and the radial gauge's `instrument` ring style, which draws the same dial outside any `data-skin` wrapper, is untouched with it.

What made this possible without a variant prop is that SVG2 promoted `r` to a CSS property while `<line>` endpoints never became one. Drawing the track and the value arc as `pathLength="100"` circles with a dash length instead of arc paths puts their radius, width and cap in CSS, so a skin can move the band; the ticks stay in JS and do not need to move, because a band at r96 w40 lands the existing scale numbers in the middle of it and the existing major ticks along its edge.

The arc is positioned with a dash offset rather than a `rotate()`. objectBoundingBox gradient coordinates are in the element's own space, so transforming the circle drags its gradient round with it. The ramp came out brightest at zero instead of at the reading, and it changed the flat dial as well as the skinned one.

The hub is a vignette, not the reference's hard-rimmed disc. The reference draws a 480px dial whose scale numbers sit outside its hub, at 156 of a 240 radius. This one is a 218px tile whose numbers sit at 96 of 140 with the band running underneath them, so anything solid enough to read as a disc buries the scale, and the centre readout does not fit inside one at that radius without dropping the hero number a size or two.

### 5a. Temperatures are telltales, not readouts

Four boxes of one reading each keeps them the same size, which left no room for the three temperatures the old layout stacked into one corner. They became a strip under the hours notch instead, inside the wedge the 250-degree sweep leaves at the bottom of the dial, so they cost the composition nothing.

A telltale is a warning light and uses that vocabulary rather than the readout one: grey when there is no reading, green while the value sits in its normal band, red once it is past an alarm. Amber covers the tiers between, and also the case that turns out to be the common one here. This engine's profile leaves its warn and alarm thresholds null, nobody having filled them in from the manual, so every reading above the normal band lands in no band at all. Red there would assert an overheat the configuration has no threshold to detect.

The symbols are drawn rather than picked from the icon set, because all three readings are temperatures and the set's one thermometer made the row three identical symbols that only a word could tell apart. What separates them at 16px is silhouette, not detail: coolant is upright, the gearbox is round, exhaust is horizontal. That reads before any interior stroke does, which is what lets the labels go.

They are not configurable. The three exist precisely so the row needs no labels, and a chooser would let a saved config break the one thing they are for.

### 6. Every slot is a `GaugeWidgetConfig`

Ring, centre and every corner row reuse the gauge config verbatim — the decision that made ADR 0049 cheap, applied a third time. Zones, units, scales, the `GaugeFields` form and the backend's `validateGaugeConfig` all work per slot with nothing new.

Corners hold **rows**, so one card stacks the three temperatures while the others carry a single hero reading. A card with several rows steps its type down rather than shrinking the card.

### 7. The two walkers, for the fifth and third time

`gaugeBoundPaths()` walks ring, centre and every corner row, or the whole cluster renders dashes with nothing in any log. `zoneDerivedAlarmRules()` walks them too, or a cluster's red bands colour without alarming — precisely the split ADR 0050 closed.

Cluster slot indices are flat and stable — ring, centre, then corner rows in order — so derived alarm ids survive a restart.

## Consequences

- An engine reads as one instrument, and a second engine is Duplicate plus one find/replace (ADR 0049). Verified against the live vessel at 698/699 RPM.
- The mask clips each card's inner corner, so content is padded away from the ring-facing side and aligned to the outer corner. Screenshot inspection found that edge-to-edge content lost its unit label; tests had not caught this.
- `wind-tile.tsx` now imports `computeCornerMasks` and `useFitScale` from `lib/cluster-canvas.ts`. Its own tests were the regression net for that extraction and passed unchanged. The corner **card** was deliberately not extracted: AGENTS.md's no-shared-KPI-primitive rule still governs card layout, and the instrument-skinned card looks nothing like the wind one.
- Exhaust temperature has to be typed by hand per cluster. Mapping `propulsion.0` to port would bake in an ordering nothing on the bus confirms, and both nodes currently read identically.
- Five widget kinds now carry per-instance config on the layout item: embed, gauge, group, lamps and cluster.
- The dial shipped without a needle: the value arc plus the centred number reads clearly at helm distance, and a needle looked like a lot of geometry for a second encoding of the same number. That was wrong and it was reversed shortly after. The arc says how much and the pointer says where, which is the redundancy every mechanical instrument uses and the reason a dial reads faster than a bare number at a glance. `Needle` draws a blade riding the rim rather than a full pointer pivoting at the centre, because a centre-pivoted needle crosses the readout it is meant to accompany.

## Verification

`go test -short ./...` and 799 frontend tests pass; `tsc` clean, no lint errors.

Verified against the live vessel. Each expected value below was calculated from the raw SI readings before the tile was implemented:

| | Port | Starboard |
|---|---|---|
| RPM | 698 | 699 |
| Oil | 22.0 psi | 22.1 psi |
| Boost | 0.0 psi | 0.0 psi |
| Coolant | 71.0 °C | 68.3 °C |
| Gearbox | 37.7 °C | 37.7 °C |
| Fuel | 1.5 L/h | 1.5 L/h |
| Hours | 894 h | 870 h |

Exhaust reads 31.6 °C from the hand-typed `propulsion.0`/`propulsion.1` path — plausible for a wet exhaust at idle, measured after water injection. Checked at 1600×1000 light, 1600×1000 dark and 768×1024 iPad portrait: the skin stays dark in light mode, and the canvas scales into a single column without overflow.
