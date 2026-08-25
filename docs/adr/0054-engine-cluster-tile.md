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

The last point quietly raises the stakes for ADR 0053: on this vessel a profile is the only thing that gets units right.

## Decision

### 1. The bundled Cummins profile was wrong, and is fixed here

ADR 0053 shipped `coolantTemperature`, `oilTemperature` and `exhaustTemperature` as suffixes. All three match nothing. The profile applied cleanly and produced three permanently dashed gauges — the structural dash working exactly as intended, about a mistake in a file we wrote.

Corrected to `temperature`, `transmission.oilTemperature` (relabelled **Gearbox**, since calling gearbox oil "Oil Temp" was wrong twice over), with exhaust dropped because no suffix under the engine node can reach it. `fuel.rate`, `engineLoad` and `transmission.oilPressure` added.

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

A real MFD is dark whatever time it is, so the skin is opt-in per tile and independent of the app theme.

`[data-skin="instrument"]` redefines the **same token names** every component already consumes — `--card`, `--foreground`, `--primary`, `--gauge-primary` and the rest. `Tile`, `DialRing` and every readout therefore re-skin with no per-component branching and no new props threaded anywhere. Tokens, not hex, so AGENTS.md holds.

Alert red and amber are deliberately **not** redefined. A warning has to look like a warning in either skin, and a skin that could recolour an alarm state would be a skin that could hide one.

### 6. Every slot is a `GaugeWidgetConfig`

Ring, centre and every corner row reuse the gauge config verbatim — the decision that made ADR 0049 cheap, applied a third time. Zones, units, scales, the `GaugeFields` form and the backend's `validateGaugeConfig` all work per slot with nothing new.

Corners hold **rows**, so one card stacks the three temperatures while the others carry a single hero reading. A card with several rows steps its type down rather than shrinking the card.

### 7. The two walkers, for the fifth and third time

`gaugeBoundPaths()` walks ring, centre and every corner row, or the whole cluster renders dashes with nothing in any log. `zoneDerivedAlarmRules()` walks them too, or a cluster's red bands colour without alarming — precisely the split ADR 0050 closed.

Cluster slot indices are flat and stable — ring, centre, then corner rows in order — so derived alarm ids survive a restart.

## Consequences

- An engine reads as one instrument, and a second engine is Duplicate plus one find/replace (ADR 0049). Verified against the live vessel at 698/699 RPM.
- The mask cuts a real bite out of each card, so corner content is padded away from the ring-facing side and aligned to the outer corner. Laid out edge to edge it loses its unit — found by screenshotting, not by a test, and worth remembering as the failure mode this layout has.
- `wind-tile.tsx` now imports `computeCornerMasks` and `useFitScale` from `lib/cluster-canvas.ts`. Its own tests were the regression net for that extraction and passed unchanged. The corner **card** was deliberately not extracted: AGENTS.md's no-shared-KPI-primitive rule still governs card layout, and the instrument-skinned card looks nothing like the wind one.
- Exhaust temperature has to be typed by hand per cluster. Mapping `propulsion.0` to port would bake in an ordering nothing on the bus confirms, and both nodes currently read identically.
- Five widget kinds now carry per-instance config on the layout item. The pattern has absorbed embed, gauge, group, lamps and cluster without strain.
- The dial has no needle. The value arc plus the centred number reads clearly at helm distance, and a needle is a lot of geometry for a second encoding of the same number.

## Verification

`go test -short ./...` and 799 frontend tests pass; `tsc` clean, no lint errors.

Verified against the live vessel, which is the point of having read its paths first — every number below was predicted from the raw SI before the tile existed:

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
