# ADR 0133: Anchor adjust is explicit, not a map gesture

## Status

Accepted for Phase 1 (this cycle): the map becomes view-only, a radius
stepper ships as an interim control, and the swing circle fits the screen on
open. A full Adjust sheet (radius plus repositioning by "Place from bow") is
the agreed direction for a later release and is recorded here too, but only
Phase 1 is built.

Amends [ADR 0059](0059-always-on-anchor-map-drop-raise-and-scope-method.md),
[ADR 0064](0064-anchor-map-follows-the-session.md) and
[ADR 0078](0078-anchor-lifecycle-signalk-synchronization.md) — see
Consequences for what each keeps and what changes.
[ADR 0089](0089-kiosk-feed-is-a-page-flag.md)'s kiosk-is-display-only stance
is unaffected: the kiosk was never offered edit gestures and still isn't.

## Context

An impeccable critique of the anchor-watch map (dual-agent design and
detector review, Playwright at 390×844 touch, 2026-09-25) scored it 13/40
with three P0s:

- A tap on the anchor marker could fire a radius PATCH whenever the swing
  circle rendered under about 25px on screen — the touch-edge hit test ran
  straight through the marker at the zoom the map opened at.
- Tap-to-commit ignored the tap point: the map committed whatever
  ghost-anchor position or live radius a previous drag had left in state on
  the *next* touchstart, anywhere on the map — not on the tap that actually
  triggered the commit.
- The swing circle was routinely invisible at the zoom the map opened at:
  the old `zoomForRadius` capped at 14, and a 40 m radius projects to about
  8px there — smaller than the 32px anchor marker sitting on top of it.

All three trace back to the same design choice: editing the anchor and its
radius was a *mode*, entered by an ordinary map gesture (tap the marker,
mousedown/touchstart on the circle edge) and committed by another ordinary
gesture (tap elsewhere, double-click, Enter). On a chart the operator is also
panning, zooming, and tapping to read range and bearing to hazards and AIS
traffic — "ordinary gesture" and "edit command" cannot be reliably told
apart on the same surface, not with a wider hit tolerance and not with a
longer press. Editing has to move off the map-gesture surface entirely.

## Decision

### Phase 1: the map is view-only, full stop

`anchor-watch-map.tsx` no longer has any code path that writes to the watch.
Removed entirely: the circle-edge mousedown/touchstart/touchmove hit-tests,
the double-click commit, the edge-hover cursor, the reposition/radius commit
branches in the map's own click handler, the window-level keydown handler
(Enter confirmed, Escape cancelled), and the ghost-anchor marker, ghost
circle and "Tap map to place anchor" overlay that existed only to support
them. `onAnchorReposition` and `onRadiusChange` are no longer props of
`AnchorWatchMap` at all — not optional, gone — and the drawer stops passing
them down (the dashboard tile already didn't). The anchor marker is now a
plain, non-interactive "Anchor position" element: it swallows its own click
(so a tap on it doesn't fall through to the map's own place-a-pin handler,
the same treatment every other marker on this map gets) and does nothing
else. `dragPan` is unconditionally on — there is no edit mode left that ever
needs to borrow it.

Placemark pinning, AIS selection, recentre, zoom controls and every kiosk
`interactive={false}` behaviour are unchanged; none of them were the defect.

### An interim radius control

The Adjust sheet below is the intended long-term control, but it ships
alone this release, so the Anchor Watch page gets a plain − / + stepper next
to the radius reading: 5 m steps (15 ft under imperial, converted back to
metres before the write), a 5 m floor, 48px targets, one `updateRadius` call
per press, no hold-to-repeat yet. The rode planner's "Apply as alarm radius"
is unchanged in effect, only in wiring — both surfaces route through the
same failure handling below. Repositioning the anchor has no control at all
in Phase 1; there is nothing yet to replace the removed drag-to-reposition
gesture with, and the how-to doc says so rather than implying otherwise.

### A failed radius change is loud

`useAnchorWatch`'s `updateRadius` used to PATCH and silently do nothing on a
non-OK response — the same silent-no-op pattern `updateRodeAndConditions`
and `updatePlanningDepth` also had. All three now go through the existing
`anchorRequest` helper (already used by `updatePosition`/`setAnchorHere`)
and throw on a non-OK response or a network error, leaving state untouched
either way. None of the three catch internally: the caller — the drawer's
stepper, the rode planner's Apply path, the planning-depth field, the
rode/conditions inputs — is what shows `toast.error(message, { action: {
label: 'Retry', onClick } })`, because the caller is the one holding the
value worth retrying and wants a Retry action, not just a description.

### The swing circle fits the screen on open

`lib/anchor-view.ts` replaces `zoomForRadius` with `fitRadiusZoom(radiusM,
latDeg, shortSidePx, fraction = 0.6)`: the zoom at which the circle's
diameter is 60% of the map's shorter side. Getting this right required
settling which metres-per-pixel constant actually matches MapLibre GL's own
zoom: MapLibre tiles the world in 512px tiles
(`Transform.worldSize = tileSize(512) * 2^zoom`, confirmed against its
bundled source), not the 256px tiles the commonly-quoted constant
156543.03392 (earth's circumference ÷ 256) assumes. Feeding that 256px
constant straight into a MapLibre zoom computes a value one zoom level too
high — twice as zoomed in as intended. `fitRadiusZoom` uses the halved,
512px-tile-correct constant (78271.51696402048) instead, and the source
documents why, rather than leaving the next person to rediscover the same
off-by-one.

The fit runs once when the map opens with no stored zoom for the current
anchor session, and again whenever a new session starts (a new anchorage can
carry a very different radius than whatever the map was last zoomed to).
The zoom is persisted the same way the pan centre already was (ADR 0064):
tagged with the session id it was made in, and ignored on read if the tag
doesn't match the session the map is looking at now — a zoom left over from
a previous, differently-sized anchorage is not a value worth restoring.

### Map controls to the 40px floor

The in-map control stack (fullscreen, zoom, satellite, radar echo, recentre)
moves from `h-9 w-9` to `h-10 w-10`, meeting DESIGN.md's 40px touch-target
floor. This stack was already inconsistent with the floor before this
cycle; fixed here since the whole stack was already being touched.

### The direction: an Adjust sheet (not built this cycle)

A later release replaces the interim stepper with a non-modal bottom sheet
(below `lg`) or side panel (`lg` and up), opened by an explicit Adjust
button (map corner and drawer KPI row, so it works without WebGL2 too). It
holds a draft `{lat, lon, radiusM}` written only by an explicit Set — never
applied to the map's live state before the operator confirms it. Radius gets
the same − / + control at 48px, this time with hold-to-repeat, plus chips
for "Rode + LOA" and "Planner swing" (extracting `computeSwingRadiusM` out of
the rode planner). A draft radius smaller than the current distance from the
anchor warns "Alarm would sound now" and requires a second tap on Set.
Committing shows a 10 second undo toast; a failed commit keeps the draft and
offers Retry.

Repositioning arrives with it: "Place from bow" (rode deployed plus
bearing, defaulting to current heading) becomes the primary method, with a
draggable handle on the map for fine tuning once the sheet is open. The
backend gains a `patchAnchorWatch` that accepts `lat`/`lon` (both or
neither) as one atomic write alongside radius, replacing today's split
between a POST for position and a PATCH for radius.

## Rejected

### Widening the edge-hit tolerance, or requiring a long press, instead of removing the gesture

Both keep editing on the same gesture surface as reading the chart, which is
the defect, not a tuning parameter of it. A wider tolerance only moves the
boundary of the same false positive; a long press still fires from an idle
finger resting on the glass while the boat rolls, and still can't be told
apart from "I am about to pan."

### A modal sheet for the Adjust surface

The operator watching an active drag needs the rest of the screen — the
swing circle, AIS traffic, the metric overlay — visible and live while
adjusting, not hidden behind a modal scrim. A non-modal sheet/panel keeps
all of it on screen.

### Tap-to-place for repositioning

Rejected in favour of "Place from bow" as the primary method, with a drag
handle for fine tuning only once the Adjust sheet is open. A bare
tap-to-place carries no information about how the rode actually laid out;
place-from-bow is the same computation the rode planner already trusts
elsewhere.

### Optimistic writes for the Adjust sheet's draft

The draft is held locally and written only on an explicit Set, with an undo
window after it lands — never spectulatively PATCHed while the operator is
still dragging or stepping the radius.

## Consequences

- No further critique of "the map fires a write on gesture X" applies to
  Phase 1's code — there is no gesture left that writes anything. The
  Adjust sheet, when it lands, is a new, deliberately separate surface, not
  a fix bolted onto this one.
- Repositioning the anchor has no operator-facing control between this
  release and the Adjust sheet's arrival. This is a real capability gap, not
  an oversight — the how-to doc says so explicitly.
- The motoring-breadcrumb trail fetch (`/api/tracks/motoring`, shown during
  the old reposition drag) had no other caller and is removed along with
  reposition mode. Restoring the drag-to-fine-tune handle will need it back.
- `lib/rode-plan.ts` does not yet export `computeSwingRadiusM`; the Adjust
  sheet's "Planner swing" chip needs it extracted from the rode planner
  first.
- Backend: `patchAnchorWatch` does not yet accept `lat`/`lon`; the Adjust
  sheet's atomic position-plus-radius write depends on that landing first.
  `updatePosition`'s existing POST path is untouched by this ADR.

## Related

- [ADR 0059](0059-always-on-anchor-map-drop-raise-and-scope-method.md) — the
  map's reposition/radius edit modes this ADR removes were introduced there;
  the always-on map itself, Drop/Raise and the scope-method setting are
  unaffected.
- [ADR 0064](0064-anchor-map-follows-the-session.md) — the session-tagged
  stored pan centre this ADR now tags a stored zoom against the same way.
- [ADR 0078](0078-anchor-lifecycle-signalk-synchronization.md) —
  `updatePosition`'s SignalK publish-on-reposition contract is unchanged;
  only its callers (the map's removed gesture handlers) are gone.
- [ADR 0089](0089-kiosk-feed-is-a-page-flag.md) — kiosk stays display-only;
  unaffected by this change.
