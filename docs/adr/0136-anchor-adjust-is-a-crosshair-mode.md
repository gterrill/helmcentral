# ADR 0136: Anchor Adjust is a fixed-crosshair mode, not a sheet with map gestures

## Status

Accepted. Amends [ADR 0133](0133-anchor-adjust-is-explicit.md): the Adjust
sheet ADR 0133 described (a bottom sheet/side panel with a "Place from bow"
primary method and a draggable map handle for fine tuning) is replaced by the
design below before any of it shipped. [ADR 0089](0089-kiosk-feed-is-a-page-flag.md)'s
kiosk-is-display-only stance is unaffected: the kiosk never gets Adjust,
gesture-based or otherwise.

## Context

An impeccable critique of the anchor-watch map
(`.impeccable/critique/2026-09-25T08-50-10Z__frontend-src-components-anchor-watch-map-tsx.md`,
13/40, three P0s) drove ADR 0133's Phase 1 (view-only map, an interim −/+
stepper). Its own Phase 2 — the sheet-plus-drag-handle design — went through
a `/impeccable shape` round before being built, and came out the other side
redesigned: a fixed-crosshair mode instead of a sheet, replacing the drag
handle outright.

The operator's own reasoning: on a chart already carrying AIS traffic, a
swing circle and a trail, an edge-of-circle drag handle is a small, fiddly
target on a boat that is moving, and it asks the operator to find and hold a
specific pixel rather than just looking at the chart and adjusting what they
see. Panning the chart under a fixed centre point is a gesture every operator
already has muscle memory for from every other map on the boat; a
purpose-built handle is one more thing to learn only Adjust mode needs.

## Decision

### The mode: camera centred on the anchor, pan moves the draft, pinch/wheel sizes it

Adjust replaces the ordinary chart view with one where the anchor sits
permanently at the centre of the screen — a fixed crosshair — and the swing
ring is a fixed-size screen overlay (35% of the map container's short side as
its radius, 70% as its diameter), not a shape drawn at a map coordinate.
Panning the chart moves the world under the crosshair, so wherever the
crosshair sits over is the draft position. Pinching or spinning the scroll
wheel changes the map's zoom, so the ring — fixed in screen pixels — comes to
represent a different ground radius; the readout snaps that to a whole
display unit and that snapped number, not the raw zoom, is both what's shown
and what Set eventually commits (avoids the readout jittering by fractions of
a metre as a gesture settles at an off zoom).

The geometry (`lib/anchor-adjust.ts`): MapLibre GL tiles the world in 512px
tiles, so `metersPerPixel(latDeg, zoom)` uses
`78271.51696402048 * cos(lat) / 2^zoom` — the same constant `lib/anchor-view.ts`'s
`fitRadiusZoom` already derived and shipped, not the 156543.03392 constant
most "zoom to metres" formulas online assume (that one is correct for a
256px-tile convention and computes a zoom one level too high fed straight
into MapLibre's own 512px zoom). `zoomForRingRadius`/`radiusForZoom` are
exact inverses of each other, verified by a round-trip test.

Rotation and pitch are locked for the session (this map already runs with
`dragRotate={false}`/`touchPitch={false}` at all times; Adjust additionally
disables MapLibre's own touch-pinch rotate handler and restores it on exit,
since that gesture isn't already covered by those two props). Max zoom is
22, MapLibre's own default — small radii are accepted overzoomed and blurry
rather than shrinking the ring below its fixed size (see Rejected below).

### The radius ceiling: chain onboard plus boat length

The Adjust mode's own bounds (`alarmRadiusBounds`): a flat 5 m floor
regardless of chain or LOA (below it, a radius can't outrun ordinary GPS
scatter at anchor), and a ceiling of Settings → Anchor Watch's **Chain
Onboard** (always set) plus the resolved LOA (`lib/rode-plan.ts`'s
`resolveLoaM`: an explicit `settings.anchor.loa_m` override, else SignalK's
`design.length.overall`). With no LOA resolved, the ceiling is chain onboard
alone, and the bar's own + button — disabled at the ceiling — names why:
"Chain onboard + boat length" when LOA is included, "Chain onboard, without
boat length" when it fell back. The rule: the swing an anchorage can
physically produce is bounded by how much chain is paid out plus the hull's
own length past the anchor; a ceiling that ignored either would let the
operator set an alarm radius that's either fantasy chain or a circle that
doesn't cover where the bow can actually reach.

These bounds are enforced twice, deliberately: `setMinZoom`/`setMaxZoom` on
the MapLibre instance stop a pinch or scroll gesture physically at the limit
(so the ring can never even be dragged past it), and `clampRadiusM` clamps
the keyboard's and the bar's own +/-/chip paths the same way, since those
don't go through a zoom gesture at all in the no-WebGL2 case.

### Reference layers: the original anchor stays visible, faded

The committed anchor and its alarm circle stay on screen throughout Adjust,
faded down (lower fill/line opacity) rather than removed, plus a thin dashed
line from that original point to the crosshair labelled "moved 8 m · 045°"
(true bearing, zero-padded to three digits; hidden under 1 m of movement,
since that's GPS/rounding noise, not a deliberate reposition). Nothing here
needed a second captured "original" value — the committed `anchorLat`/
`anchorLon`/`radiusMeters` props are frozen for the whole session by
construction, since nothing writes to the server until Set.

### Full screen on phones, in place at `lg` and up

Below the `lg` breakpoint, Adjust takes over the whole viewport: the page's
own header, the rode planner sidebar and the Drop/Raise row all hide, leaving
just the map and the bottom bar in a fixed full-screen layer. At `lg` and up,
the layout is untouched — the map stays where it is in the drawer, and the
bar replaces the Drop/Raise row in place. The same `AnchorWatchMap` instance
stays mounted across the transition (only its wrapping container's CSS
changes), so entering/leaving Adjust never reloads map tiles or replays the
zoom-fit dance; MapLibre's own resize tracking (backed by a `resize()` call
on entry as a deliberate belt-and-suspenders) picks up the container's new
size.

### The bar: radius, two chips, and a second tap when it matters

A 48px-target bottom bar shows the draft radius as a hero readout (`RADIUS
24 m`), −/+ that step 1 m (5 ft under imperial — a rounder number to land on
than a literal 1 m-in-feet conversion) with hold-to-repeat
(`hooks/use-press-repeat.ts`: 400 ms initial delay, 80 ms repeat), and two
recommendation chips — "Rode + LOA" (rode deployed from the watch record plus
resolved LOA) and "Planner swing" (`computeSwingRadiusM`, extracted from
`anchor-rode-planner.tsx` into `lib/rode-plan.ts` so the chip and the
planner's own swing circle share one calculation) — each disabled with its
own short reason when an input is missing, never a silent substitute value.
Tapping an enabled chip applies its value straight to the draft radius, the
same absolute-target path the +/- buttons and the map's own keyboard handler
share (`AnchorWatchMap`'s exposed `setAdjustRadius`).

The committed radius, at the moment Adjust opened, may already sit above the
current chain+LOA ceiling — possible from before that ceiling existed (the
old −/+ stepper had none). Rather than silently opening on a draft the
operator never chose, the starting draft is the clamped value, and the bar
shows its own line naming what happened: "Was 180 m, above the maximum"
(code-review finding — the with-map path's own entry effect used to report
the raw, unclamped radius as the starting draft while the ring on screen
already showed the clamped one, so the two disagreed about what Set was
about to commit).

When the boat's live distance from the draft anchor exceeds the draft radius
plus the drag buffer (the same `DRAG_BUFFER_METERS` `useAnchorWatch` itself
uses for its dragging state), the bar shows "Alarm would sound now" and Set
requires a second tap: the first changes its own label to "Set anyway" rather
than committing, and the state clears itself the moment the condition does
(a boat position or a radius that no longer trips it shouldn't leave a stale
confirmation armed). This check needs a genuine live boat position to mean
anything: with no GPS fix, `AnchorWatchDrawer`'s own `vesselLat`/`vesselLon`
props are App.tsx's fallback to the anchor point itself (distance ~0, so the
warning would wrongly never fire) or the backend's -1/-1 "no fix" sentinel
(thousands of kilometres away, so it would wrongly always fire) — neither is
the boat. A `hasGpsFix` prop (checked against the raw vessel lat/lon being
null, `gnss_critical_alert`, and the -1/-1 sentinel specifically) gates the
warning off entirely in that case, and the bar instead shows a muted line,
"No GPS fix: can't check the boat against this circle" — Set then needs only
the ordinary single tap, since there is nothing to confirm (code-review
finding).

### One write, an atomic PATCH, a 10 second undo

Nothing is written until Set. Set — and the Undo toast's own re-send of the
pre-Adjust values — both go through `useAnchorWatch`'s `adjustAnchor`, one
atomic `PATCH` of `lat`/`lon`/`radius_meters` together (the backend's
`patchAnchorWatch`, landed as part of this same cycle: lat/lon optional but
must arrive together, published to SignalK outside the anchor-watch mutex,
`set_at` preserved). `hooks/use-anchor-adjust-commit.ts` owns the whole flow:
a successful Set exits Adjust and raises a 10 second toast offering Undo; a
failed Set keeps the draft and the Adjust UI exactly as they were and shows
Retry instead.

**A radius-only Set omits lat/lon entirely, not just unchanged values.**
`lib/anchor-adjust.ts`'s `buildAdjustCommitTargets` compares the draft
position against the committed one with a 0.5 m haversine tolerance
(comfortably tighter than ordinary GPS scatter at anchor, loose enough that
the zoom/pixel round-trip on a crosshair the operator never actually panned
can't itself trip it) and sends `radius_meters` alone when the draft is
still within it — which is every bar-only interaction (+/-, a chip) and the
entire no-WebGL2 path, since neither can move the crosshair at all. This
matters because the backend treats any `lat`/`lon` on the PATCH as a genuine
reposition: it resets the post-anchor self trail and requires SignalK to
confirm the new position, 502ing outright if SignalK is unreachable — a
radius-only change no longer risks failing over a system it never touched
(code-review finding). Undo is built from the same moved/unmoved decision as
its Set, so it never restores a position Set itself never sent.

**Undo can overwrite another client's change.** If a second browser (or the
server's own auto-raise) changes the watch during that 10 second window,
tapping Undo still PATCHes the pre-Adjust values over whatever is there now —
the same last-write-wins behaviour every other PATCH against this resource
already has. This is a known, accepted risk, not an oversight: a lock or a
compare-and-swap across two independent browser tabs is more machinery than a
10 second window on a single-operator boat justifies.

### Keyboard, scoped to the map container

Arrows pan 1 m (5 m with Shift), +/− step the radius, Enter is Set, Escape is
Cancel — all read from the map container's own `onKeyDown`, never `window`.
A `window`-level handler would fire from a keystroke typed anywhere else on
the page (the bar's own inputs, an entirely different part of the drawer);
scoping to the container is what lets Adjust own its own keys without
stealing everyone else's. Focus moves to the container on entry and back to
the Move icon (or, in the no-WebGL2 path, the header's own text button) on
exit.

### Depth replaces distance as the page's own hero (ADR 0133's amendment, restated)

Unchanged from ADR 0133's own amendment and restated here only because
Adjust's bottom bar now occupies the space the old Distance KPI and radius
stepper used to: the header above the map shows live depth and tide context,
not distance from the anchor.

## Rejected

### An edge-of-circle drag handle (ADR 0133's own original Phase 2 direction)

Superseded by the fixed-crosshair pan/pinch design above. A drag handle asks
the operator to find and hold a specific point on a moving chart; panning the
whole chart under a fixed centre uses a gesture every map on the boat already
trained into muscle memory.

### A separate sheet/panel surface

ADR 0133 planned a non-modal bottom sheet or side panel holding the draft
controls, with the map still requiring a drag handle underneath it. Once the
drag handle was gone, a separate sheet added a second surface with nothing
left to justify its own existence: the bar at the bottom of the same map
serves the identical purpose with one fewer container.

### Tap-to-place

A single tap committing a new anchor position was rejected the same way ADR
0133 rejected it: too easy to fire by accident while panning or tapping to
read range and bearing, and it carries no feedback about where the tap
actually landed relative to the boat's swing before it's already applied.

### Long-press

Same failure mode as ADR 0133's rejection of a long-press commit for the old
gesture-based editing: an idle finger resting on the glass while the boat
rolls can hold a press by accident, and a genuine long-press can't be told
apart from "the operator is about to do something else."

### "Place from bow" (deferred, not rejected)

ADR 0133 named "Place from bow" (rode deployed plus bearing, defaulting to
current heading) as the primary repositioning method. The operator's own
call this round: "most skippers couldn't tell you the bearing" reliably
enough to make it the primary method. Panning to the position they can see
on the chart is more reliable than a bearing they'd have to estimate. Not
rejected outright — a `dropLat`/`dropLon`-based "Reset to drop point" stays a
plausible later addition — just not built this cycle.

### Shrinking the ring at small radii

An alternative considered and rejected: below some radius, shrink the ring's
own screen size rather than letting the map overzoom. Rejected because a
shrinking ring changes the meaning of "the ring's size" mid-session — the
whole point of a fixed-size ring is that easing zoom, not resizing the ring,
is what represents a radius change. Overzoomed, blurry imagery at a 5 m
radius is an acceptable trade for keeping that invariant.

## Consequences

- `AnchorWatchMap`'s Adjust icon changes from Lucide `Crosshair` to Lucide
  `Move`: `Crosshair` was already this same control stack's recentre icon: reusing
  it for Adjust would give two different actions the same glyph.
- The metrics panel (Distance/Bearing/Radius/Current/Scope) and the ordinary
  control stack (fullscreen, zoom, satellite, radar echo, recentre) both hide
  while Adjust is open — their numbers describe the *committed* watch, and
  showing them alongside a draft that says something different would
  contradict the bar and the crosshair's own moved-distance label.
- The rode planner sidebar hides entirely while Adjust is open: on phones the
  fixed full-screen layer already covers it, and at `lg` and up a map whose
  whole point is undivided attention while positioning the anchor doesn't
  want a sidebar competing for width.
- `patchAnchorWatch`'s new `lat`/`lon` support (this cycle) makes the anchor
  reposition able to publish to SignalK the same way `setAnchorHere`'s POST
  already does, outside the anchor-watch mutex, so a slow SignalK publish
  cannot block the watch's own poll.

## Related

- [ADR 0133](0133-anchor-adjust-is-explicit.md) — Phase 1 (view-only map, the
  interim stepper, depth-as-hero) stands; this ADR replaces only its Phase 2
  design, before any of it was built.
- [ADR 0089](0089-kiosk-feed-is-a-page-flag.md) — kiosk stays display-only;
  unaffected by this change.
- [ADR 0059](0059-always-on-anchor-map-drop-raise-and-scope-method.md) — the
  always-on map, Drop/Raise and the scope-method setting this mode sits on
  top of are unaffected.
