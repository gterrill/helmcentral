# ADR 0143: Anchor Adjust's radius is a toolbar, decoupled from zoom

## Status

Accepted. Amends [ADR 0136](0136-anchor-adjust-is-a-crosshair-mode.md): the
fixed-screen-size ring that read the alarm radius off the camera's own zoom
is gone, along with the bottom bar it lived in. The crosshair, the faded
original anchor and its "moved 8 m · 045°" label, and the whole commit/Undo
flow (`hooks/use-anchor-adjust-commit.ts`, `buildAdjustCommitTargets`,
radius-only Set) are unaffected and stand as ADR 0136 described them.

## Context

ADR 0136 shipped and went straight to unusable. Tying the ring's ground
radius to the camera's zoom meant the one gesture every operator reaches for
to get a better look at the chart — pinch or scroll to zoom — silently
changed the number Set was about to commit. There was no way to zoom in and
check where the crosshair actually sat over the chart without also moving the
alarm radius. Separately, the bottom bar carrying the hero readout, two
recommendation chips and Cancel/Set took a fixed slice of the map's own
height, which on a phone (the full-screen takeover) left less chart than the
mode's whole point — undivided attention while positioning the anchor —
should have cost it.

## Decision

### Radius is plain state; zoom is free

The draft radius (`AnchorWatchDrawer`'s own `adjustDraft.radiusM`) is now
ordinary React state, stepped directly by a small toolbar's own −/+ (and the
map's own keyboard +/-, which now calls the same step handler instead of
easing the camera). Nothing about the camera's zoom is read to produce it and
nothing clamps zoom to keep it in range any more —
`AnchorWatchMap` never calls `setMinZoom`/`setMaxZoom` during Adjust. The
operator can pinch or scroll exactly as they would anywhere else on the map,
including past whatever zoom level the alarm radius happens to represent, and
the radius does not move. The only camera command Adjust issues is a
one-time recentre on the anchor at entry, so the crosshair starts on it. The
zoom is left where the operator had it: a first cut refitted the camera to
the draft circle, and the operator rejected that, because opening Adjust
should not throw away the view they had set up.

### The draft circle is real map geometry, not a screen-space overlay

ADR 0136's ring was a fixed-pixel DOM overlay whose ground meaning changed
with zoom by construction — that was the whole mechanism this ADR removes.
Its replacement (`adjust-draft-circle`, a `Source`/`Layer` pair inside the
`<Map>`) is the same kind of GeoJSON polygon the alarm circle itself already
uses (`generateCircleGeoJSON`), centred on the draft position and sized to
the draft radius, recomputed on every pan and every radius step. Because it's
genuine map geometry, it scales with the basemap under a pinch or a scroll
exactly the way the alarm circle, the AIS markers and everything else on this
map already do — there is no separate "keep it in sync with zoom" step to get
wrong, because nothing about it depends on zoom in the first place.

The fixed centre crosshair stays exactly as ADR 0136 described it (a DOM
overlay, always the viewport's centre, which is by construction also the
centre the draft circle is drawn around), and so does the faded original
anchor, the dashed reference line back to it, and the "moved 8 m · 045°"
label — none of that was zoom-coupled to begin with.

### The corner toolbar: −, a readout, +, under the Adjust icon

The −/+ and the bare radius readout move into `AnchorWatchMap`'s own
top-right control column, in a small horizontal toolbar (− readout +)
(`components/anchor-adjust-radius-toolbar.tsx`) that slides out directly
under the Adjust icon once Adjust opens — a short CSS transition off
Tailwind's `starting:` variant (`@starting-style`, Baseline 2024), which
`motion-reduce:` disables per `prefers-reduced-motion`. Hold-to-repeat is
unchanged (`hooks/use-press-repeat.ts`); the step size is unchanged
(`radiusStepM`: 1 m metric, a round 5 ft imperial). The toolbar reads its
disabled state from the host's own `alarmRadiusBounds` check
(`adjustRadiusAtMin`/`adjustRadiusAtMax`/`adjustRadiusMaxDisabledReason`
props) — bounds themselves stay entirely the host's business
(`AnchorWatchDrawer`), never threaded into the map component as a whole
object the way `adjustRadiusBounds` used to be.

**The Adjust icon itself no longer disappears when Adjust opens.** ADR 0136
hid the whole control stack, icon included, the moment Adjust went active,
leaving Cancel/Set in the bottom bar as the only way out. The icon now stays
on screen and reachable throughout the session, carries `aria-pressed`, and
switches to an accent background while active; tapping it again calls the
same handler Cancel does. The rest of the control stack (fullscreen, zoom,
satellite, radar echo, recentre) stays on screen too, reversing ADR 0136:
with zoom free, the zoom buttons and imagery toggles are as useful while
positioning the anchor as at any other time. Recentre snaps the crosshair
back onto the committed anchor, which doubles as "undo my pan".

### The data cards stay up and follow the draft

ADR 0136 hid the Distance/Bearing/Radius cards during Adjust because they
described the saved watch and would contradict the draft. The operator wants
them while positioning the anchor, so they stay on screen and describe the
draft instead: distance and bearing run from the crosshair to the boat, and
Radius is the toolbar's value. With no live boat position the saved figures
are already null, and the draft rows show a dash too.

### Cancel and Save float over the map instead of docking a bar under it

The hero readout, the two recommendation chips ("Rode + LOA", "Planner
swing") and Cancel/Set are gone as a single bottom-docked bar. What's left of
that bar's job — the safety notices ("Was 180 m, above the maximum", the
min/max reasons, "Alarm would sound now", "No GPS fix: can't check the boat
against this circle") and the two buttons, Cancel and Save (Set renamed) — moves
into `components/anchor-adjust-actions.tsx`, rendered as a small floating
overlay at the bottom-centre of the map (`pointer-events-auto` on an
otherwise `pointer-events-none` strip, clear of MapLibre's own compact
attribution control) rather than a bar that claims a fixed slice of the
map's height. Both new components take a `variant` prop (`'overlay'` for the
dark map-scrim styling every other on-map control already uses, `'panel'`
for the no-WebGL2 fallback's themed surface tokens), so the exact same
component and the exact same logic serve both hosts — nothing about the
safety checks, the double-tap, or the radius-only-Save wiring changed, only
where the same pieces are drawn.

### The chips are gone

"Rode + LOA" and "Planner swing" (each disabled with its own reason when an
input was missing) are removed outright — the operator's own spec for this
round asked for +/- only. `computeSwingRadiusM` (`lib/rode-plan.ts`) is
unaffected: the Rode Planner sidebar's own swing circle and its "Apply as
alarm radius" button still use it, unchanged and reachable exactly as before
whenever Adjust itself isn't open.

### Removed

`lib/anchor-adjust.ts` lost the zoom<->radius conversions this ADR made
unnecessary: `zoomForRingRadius`/`radiusForZoom` (exact inverses of each
other, the whole reason a round-trip test existed for them),
`adjustZoomBounds`, `ringRadiusPx`/`ADJUST_RING_FRACTION`, and
`ADJUST_MAX_ZOOM`. `metersPerPixel` stays — the map's own keyboard pan
(arrows) still needs a metres-to-pixels conversion at the current zoom, and
it has nothing to do with the radius. `AnchorWatchMap`'s imperative handle
(`setAdjustRadius`/`getAdjustRadiusTarget`, and the `forwardRef` it existed
for) is gone along with the ease-the-zoom mechanism it drove — the radius
toolbar now calls a plain `onAdjustRadiusStep(deltaM)` prop instead of
reaching into the map component to move its camera.
`components/anchor-adjust-bar.tsx` (the old bottom bar) is deleted; its two
replacements are `anchor-adjust-radius-toolbar.tsx` and
`anchor-adjust-actions.tsx`.

### Entering Adjust leaves the page layout alone

ADR 0136 took over the whole screen on phones and, at every size, hid the
depth header, the Drop/Raise row and the Rode Planner sidebar. Each of those
changed the map's size the moment Adjust opened, so the chart jumped under
the operator's finger just as they went to pan it. None of that happens now:
the map keeps its size and position, the planner's handle stays where it
was, and the only change on entry is the crosshair, the draft circle, the
radius toolbar and Cancel/Save. Raising the anchor from the still-visible
row while Adjust is open closes Adjust, as it already did.

Two things follow from keeping that chrome on screen. The planner's "Apply
as alarm radius" is disabled while Adjust is open, with the reason shown:
it writes the radius straight away, and Save would then write the older
draft back over it. And the map buttons now sit inside the container that
owns Adjust's keys, so a key pressed on a button belongs to that button:
Enter on "Increase radius" steps the radius instead of saving. Escape still
cancels from anywhere in the map.

## Rejected

### Keeping the ring but clamping zoom harder

Tightening `setMinZoom`/`setMaxZoom` around the ring's own bounds was the
mechanism ADR 0136 already shipped, and it's what caused the problem: any
clamp tight enough to keep the ring meaningful is also a clamp that stops the
operator zooming in to actually see where the crosshair sits. There is no
zoom range that is simultaneously "free enough to look around" and "narrow
enough that the ring stays put" — the two goals are opposed by construction
once the radius rides on zoom at all.

### A screen-space overlay resized on every zoom tick instead of map geometry

Recomputing a DOM ring's pixel radius on every zoom tick (rather than
deriving the alarm radius from it) was considered, so the ring's screen size
would visibly track a now-independent ground radius as the operator zoomed.
Rejected as needless: a real GeoJSON circle already gets this for free from
MapLibre's own rendering, with no `project()`/resize bookkeeping of its own
to keep correct, and it composites correctly with rotation and pitch changes
(locked during Adjust, but the geometry approach doesn't care either way).

## Consequences

- `AnchorWatchMapProps` drops `adjustRadiusBounds` and `onAdjustDraftChange`
  (which used to report `{lat, lon, radiusM}` together) in favour of
  `adjustRadiusM`/`adjustRadiusAtMin`/`adjustRadiusAtMax`/
  `adjustRadiusMaxDisabledReason` (host-computed, read-only) and
  `onAdjustPositionChange` (position only) plus `onAdjustRadiusStep` (a
  signed delta in metres). The exported `AnchorAdjustDraft` type is gone from
  `anchor-watch-map.tsx`; the drawer keeps its own local `{lat, lon, radiusM}`
  shape for its own state, since the map component no longer has any reason
  to know about a combined draft.
- `AnchorWatchMap` is no longer wrapped in `forwardRef` — nothing outside it
  needs an imperative handle any more.
- The drawer's own bounds-narrowing safety net (if `alarmRadiusBounds`
  shrinks mid-session, e.g. a chain-onboard settings change landing while
  Adjust is open) moves from a `ResizeObserver` callback (which only ever
  fired on an actual container resize) to a plain effect keyed on the bounds
  themselves — strictly more correct, since it now reacts to the actual
  cause rather than a resize that happened to accompany it.
- `handleMoveEnd` no longer skips zoom-tracking outright during Adjust, only
  the localStorage persistence of centre/zoom — marker scale and the
  satellite-imagery fade now track a genuine pinch/scroll during Adjust the
  same as anywhere else on the map, since zoom is a real, operator-driven
  gesture during Adjust now rather than something Adjust's own machinery
  used to command.

## Related

- [ADR 0136](0136-anchor-adjust-is-a-crosshair-mode.md) — the crosshair mode,
  the reference layers, the radius ceiling (chain onboard + LOA), the
  commit/Undo flow and the keyboard scoping this ADR amends only the radius
  mechanism and the on-screen controls of.
- [ADR 0133](0133-anchor-adjust-is-explicit.md) — Phase 1 (view-only map,
  depth-as-hero) stands, unaffected.
