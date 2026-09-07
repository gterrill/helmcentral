# ADR 0080: Alarm Colour Reserved for State

## Status
Accepted

## Context

An evaluation of MV Dirona's 2016 Maretron N2KView main screen (the reference
this dashboard's dial, ribbon and grouping already draw on) scored 59 overall
and found a specific failure worth naming: the screen runs about 60 red or
yellow marks lit on a healthy boat, including a red needle on every gauge.
None of it means anything is wrong. It means the screen always looks that way.
A crew that has to habituate to sixty standing red marks in order to find the
one that matters has lost the thing alarm colour exists to provide, which is
a signal that stands out because it is rare.

Helmcentral does not have sixty of these, but it had reproduced the same
mechanism in miniature, in two places:

1. **The dial's pointer.** `--dial-needle` indirected to `--gauge-primary`,
   Deep Amber, the same token every readout in the flat skin uses for its
   hero number. Deep Amber (`hsl(39 100% 39%)`) and the amber warning fill
   (`hsl(38 92% 50%)`) sit close enough on the same hue that a pointer at
   rest and a pointer sitting in a warn band read alike at a glance. A
   pointer says where the value is; it carries no state of its own, and
   colouring it with a token that also means "warning" put a warning-shaped
   mark on every dial regardless of the reading.

2. **The zone rim.** `DialRing` draws a 7-unit rim segment for every non-normal
   zone, unconditionally, the width unaffected by where the value currently
   sits. A tachometer with a 3000 RPM redline configured shows that redline at
   full width at idle, at cruise, and redlined, because the rim's job was
   only ever to mark where the boundary is. `RadialGauge`'s plain arc has the
   same zone wash at the same fixed width and opacity.

3. **The lamp strip's "on" colour.** `lampFill('on')` returned
   `hsl(var(--primary))`, Signal Blue, DESIGN.md's interactive-chrome token:
   buttons, toggles, the active state of a control. An indicator lamp is not
   a control; painting its healthy state in the control colour borrows a
   token that means something else, and it is not the token this dashboard
   already uses for "this reading is fine" (`severityFill('normal')`, the
   same green a gauge zone in its normal band draws).

None of these is what the N2KView evaluation is actually about: the ribbon
(ADR 0052) already put grouped healthy state where it belongs, and gauge
zones are already the alarm source (ADR 0050). This is the narrower finding
underneath it: red and amber are supposed to mean "look here", and three
places in this codebase spent that meaning on chrome that draws whether or
not anything needs looking at (PFD Learning #16, a near-miss colour pair
close enough to blur at a glance).

## Decision

### 1. The pointer is neutral

`--dial-needle: var(--foreground);` in `:root`: black on the light board,
white in `.dark`. The instrument skin's own `--dial-needle` (a near-white)
is untouched: it was never `--gauge-primary` and was already independent of
this problem. Deep Amber stays exactly what it was for every readout number;
it no longer also draws the one piece of dial chrome shaped like an alarm.

### 2. The zone rim is a marking until it is a state

`DialRing`'s rim segments and `RadialGauge`'s zone wash both take a rest
width and an active width. Rest is a hairline: 2.5 units on the rim's 280-unit
box, `strokeWidth={3}` on the plain arc's 100-unit viewBox. A zone counts as
active when the converted reading actually falls inside
`[min(from, to), max(from, to)]` for that zone; active widens to the values
these already drew at unconditionally: 7 units on the rim, `strokeWidth={8}`
on the arc. `RadialGauge`'s wash keeps its existing opacity (0.4) in both
states, so only the width changes: the colour is always faintly present as a
marking and only asserts itself at full saturation while true.

`DialRing` stamps `data-zone-active="true"` on the segment currently active,
for the same reason the rim carries `data-zone` at all: something has to be
able to find it without parsing an SVG path.

Zone-coloured **ticks and scale numbers** are unchanged. `markingAt()` already
answers "does this position on the scale fall in a non-normal zone", which is
a fact about the scale, not the reading: the redline's location does not
become false when the needle moves away from it, and a crew still needs to
see where 3000 RPM is before the tachometer gets there. What changes here is
only the second encoding, the rim, whose full width used to assert "this is
where you are" every time it only meant "this is where the line is".

### 3. Lamp "on" is the healthy green

`lampFill('on')` returns `severityFill('normal')`. Off and no-data are
unchanged: off stays the muted-foreground grey, no-data stays the border-tint
grey at reduced opacity. This is the smallest of the three changes and the
most direct: it swaps one hardcoded colour for the token this dashboard
already has for "this reading is fine", rather than reusing the button
colour for it.

### Bar and trend washes were left alone

`BarGauge`'s zone rects (`opacity="0.35"`) and `TrendGauge`'s zone rects
(`opacity="0.18"`) already draw at low opacity across their whole configured
range, unconditional on the current reading, and are not touched here. They
were not reproducing the N2KView failure the way the dial's rim and the
plain arc's wash were: a translucent band across a bar or a trend chart reads
as "this is the danger zone on the scale", the same job the dial's ticks and
labels do, not as "the reading is currently there". The dial's rim was the
outlier because it drew at the same full stroke width and full-strength
colour a reading actually inside the zone would earn, with nothing to tell
the two apart. Revisit this only if a bar or trend gauge is found producing
the same false-positive read in practice; nothing here indicates one has.

## Consequences

- A healthy dashboard now looks healthy. A dial with every reading in its
  normal band shows a black or white pointer and hairline rims; nothing
  amber or red is lit until a reading actually earns it.
- The redline's *location* is still always visible, on the ticks and the
  scale numbers, which is the half of the N2KView reference this codebase was
  already getting right and did not need to change.
- `zoneColor()` and `severityFill()` are unchanged; only how their output is
  applied changes on the rim and the plain arc's wash, and only stroke width
  changes, never which colour.
- Bar and trend gauge zone washes stay exactly as they were, deliberately, for
  the reason above.

## Verification

`npx vitest run` and `npx tsc --noEmit` in `frontend/`, plus `npm run lint`.
New coverage: `dial-ring.test.tsx` (hairline outside the zone, full width and
`data-zone-active` inside, a null value never activates), `gauge-tile.test.tsx`
(the plain radial wash at rest and active width), `lamp-strip-tile.test.tsx`
(the "on" lamp's fill equals `severityFill('normal')`).
