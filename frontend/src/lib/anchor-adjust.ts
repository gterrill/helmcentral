/**
 * Shared arithmetic for the Adjust mode (part 2 of the anchor-adjust-sheet
 * plan), landed here in part 1 so its seams exist before the crosshair/pinch
 * UI does.
 */

import { bearingDeg, haversineMeters } from '@/lib/geo'

/** No alarm radius may go below this, regardless of chain onboard or LOA. */
export const MIN_ALARM_RADIUS_M = 5

export interface AlarmRadiusBoundsInput {
  /** Settings > Anchor Watch's Chain Onboard — always set, default 150 m. */
  chainOnboardM: number
  /**
   * The operator's boat length, already resolved the way
   * lib/rode-plan.ts's resolveLoaM resolves it (settings.anchor.loa_m wins,
   * else SignalK design.length.overall). null when neither source has a
   * usable figure.
   */
  loaM: number | null
}

export interface AlarmRadiusBounds {
  minM: number
  maxM: number
  /**
   * Set when maxM had to fall back to chain onboard alone because LOA
   * couldn't be resolved — the UI names this in the ceiling's own caption
   * ("without boat length") rather than silently presenting a ceiling that
   * doesn't actually account for the hull. null when LOA was included.
   */
  maxReason: string | null
}

/**
 * The Adjust mode's radius ceiling (ADR 0133's amendment): the swing an
 * anchorage can physically produce is bounded by how much chain is onboard
 * plus the boat's own length — past that, the alarm radius is either
 * fantasy chain or a circle that doesn't cover where the bow can reach.
 * min is a flat floor (a radius below it can't out-run ordinary GPS scatter
 * at anchor), independent of chain or LOA.
 */
export function alarmRadiusBounds({ chainOnboardM, loaM }: AlarmRadiusBoundsInput): AlarmRadiusBounds {
  if (loaM === null) {
    return { minM: MIN_ALARM_RADIUS_M, maxM: chainOnboardM, maxReason: 'without boat length' }
  }
  return { minM: MIN_ALARM_RADIUS_M, maxM: chainOnboardM + loaM, maxReason: null }
}

/** Clamps a radius into an AlarmRadiusBounds, both inclusive. */
export function clampRadiusM(radiusM: number, bounds: AlarmRadiusBounds): number {
  return Math.min(bounds.maxM, Math.max(bounds.minM, radiusM))
}

// ── Adjust mode geometry (part 2) ───────────────────────────────────────────
//
// The fixed-crosshair Adjust mode (impeccable shape round, 2026-09-26):
// the camera centres on the anchor, the swing ring is a fixed-size screen
// overlay, and panning/pinching the map moves the draft anchor and radius
// underneath it. Every function below is pure geometry with no map instance
// or DOM access, so the crosshair/pinch UI (anchor-watch-map.tsx) can be
// driven from real MapLibre events while these stay unit-testable on their
// own.

/**
 * MapLibre GL tiles the world in 512px tiles, not the 256px tiles most
 * "meters per pixel" formulas found online assume (Leaflet, Google Maps).
 * This is the same derivation and the same constant lib/anchor-view.ts's
 * fitRadiusZoom already uses and has shipped — duplicated rather than
 * imported so this module stays a standalone, dependency-free geometry
 * layer, but deliberately kept numerically identical: an anchor radius that
 * fits inside fitRadiusZoom's fit also fits inside this module's ring at the
 * same zoom.
 */
const MAPLIBRE_METERS_PER_PIXEL_AT_ZOOM_0 = 78271.51696402048

/** Metres represented by one screen pixel at `latDeg`, at MapLibre zoom `zoom`. */
export function metersPerPixel(latDeg: number, zoom: number): number {
  const latRad = (latDeg * Math.PI) / 180
  return (MAPLIBRE_METERS_PER_PIXEL_AT_ZOOM_0 * Math.cos(latRad)) / 2 ** zoom
}

/**
 * The swing ring's fixed size (operator decision, 2026-09-26): 35% of the
 * map container's short side as its radius, so the ring's diameter — 70% of
 * the short side — sits comfortably inside the viewport at every aspect
 * ratio without ever being clipped.
 */
export const ADJUST_RING_FRACTION = 0.35

export function ringRadiusPx(shortSidePx: number): number {
  return shortSidePx * ADJUST_RING_FRACTION
}

/** MapLibre never zooms in past this during Adjust — small radii accept overzoomed, blurry imagery rather than shrinking the ring (operator decision). */
export const ADJUST_MAX_ZOOM = 22

/** Safe fallback zoom for input that can't be computed (zero-size container, non-finite radius) — matches the map's own DEFAULT_ZOOM_BEFORE_FIT. */
const ADJUST_FALLBACK_ZOOM = 14

/**
 * The fractional MapLibre zoom at which a fixed screen-space ring of
 * `ringPx` radius, centred at `latDeg`, represents `radiusM` metres on the
 * ground — used both to jump to the current radius on Adjust entry and to
 * ease towards a stepped target radius while keeping the ring's own pixel
 * size fixed. The inverse of radiusForZoom below.
 */
export function zoomForRingRadius(radiusM: number, latDeg: number, ringPx: number): number {
  if (!(radiusM > 0) || !(ringPx > 0)) return ADJUST_FALLBACK_ZOOM
  const targetMetersPerPixel = radiusM / ringPx
  const latRad = (latDeg * Math.PI) / 180
  const zoom = Math.log2((MAPLIBRE_METERS_PER_PIXEL_AT_ZOOM_0 * Math.cos(latRad)) / targetMetersPerPixel)
  return Number.isFinite(zoom) ? zoom : ADJUST_FALLBACK_ZOOM
}

/** The ground radius (m) a fixed `ringPx` ring represents at `zoom` — the inverse of zoomForRingRadius. */
export function radiusForZoom(zoom: number, latDeg: number, ringPx: number): number {
  return ringPx * metersPerPixel(latDeg, zoom)
}

export interface AdjustZoomBounds {
  minZoom: number
  maxZoom: number
}

/**
 * The MapLibre zoom range Adjust mode locks pinch/scroll-wheel zoom to, so
 * the operator physically cannot zoom the ring past alarmRadiusBounds. A
 * larger radius needs a SMALLER zoom (more ground per pixel), so
 * `bounds.maxM` maps to the lower zoom bound and `bounds.minM` to the upper
 * one — computed both ways and sorted rather than assumed, so this holds
 * even if that relationship's direction ever changes. Clamped to
 * [0, ADJUST_MAX_ZOOM].
 */
export function adjustZoomBounds(bounds: AlarmRadiusBounds, latDeg: number, ringPx: number): AdjustZoomBounds {
  const zoomAtMaxRadius = zoomForRingRadius(bounds.maxM, latDeg, ringPx)
  const zoomAtMinRadius = zoomForRingRadius(bounds.minM, latDeg, ringPx)
  return {
    minZoom: Math.max(0, Math.min(zoomAtMaxRadius, zoomAtMinRadius)),
    maxZoom: Math.min(ADJUST_MAX_ZOOM, Math.max(zoomAtMaxRadius, zoomAtMinRadius)),
  }
}

/** Matches rode-plan.ts's own METERS_PER_FOOT — kept local so this stays a standalone geometry module (see the MAPLIBRE_METERS_PER_PIXEL_AT_ZOOM_0 comment above). */
const METERS_PER_FOOT = 3.28084

/** "24 m" / "79 ft" — a whole-unit radius in the operator's display unit. Shared by the bottom bar and the Set/Undo toast (use-anchor-adjust-commit.ts) so both name a radius the same way. */
export function formatRadiusDisplay(radiusM: number, isImperial: boolean): string {
  return isImperial ? `${Math.round(radiusM * METERS_PER_FOOT)} ft` : `${Math.round(radiusM)} m`
}

/**
 * Adjust mode's own +/- step (operator decision: "pick a sensible unit
 * step"). Metric steps a flat 1 m. Imperial steps a round 5 ft (≈1.52 m)
 * rather than converting 1 m into an odd 3.3 ft — a skipper thinking in feet
 * wants to land on 75, 80, 85 ft, not 74.9, 78.1, 81.4.
 */
export const RADIUS_STEP_M = 1
export const RADIUS_STEP_FT = 5

export function radiusStepM(isImperial: boolean): number {
  return isImperial ? RADIUS_STEP_FT / METERS_PER_FOOT : RADIUS_STEP_M
}

/**
 * Rounds a radius to a whole number in the operator's chosen display unit,
 * returned back in metres — the value Adjust mode both displays and, on
 * Set, actually commits. "Snap the displayed/committed value, not the
 * zoom" (operator decision): the continuous pinch/wheel zoom float jitters
 * by fractions of a unit as a gesture settles, and re-deriving a fresh zoom
 * for every render would make the readout visibly jitter; snapping this
 * derived number instead keeps it stable without touching the camera.
 */
export function snapRadiusM(radiusM: number, isImperial: boolean): number {
  if (isImperial) return Math.round(radiusM * METERS_PER_FOOT) / METERS_PER_FOOT
  return Math.round(radiusM)
}

export interface AnchorMoveOffset {
  distanceM: number
  bearingDeg: number
}

/** The draft anchor's displacement from where Adjust mode started — feeds the reference line's "moved 8 m · 045°" label. */
export function anchorMoveOffset(fromLat: number, fromLon: number, toLat: number, toLon: number): AnchorMoveOffset {
  return {
    distanceM: haversineMeters(fromLat, fromLon, toLat, toLon),
    bearingDeg: bearingDeg(fromLat, fromLon, toLat, toLon),
  }
}

/**
 * "moved 8 m · 045°" — true bearing, zero-padded to 3 digits. Null under 1 m
 * moved: below that the offset is GPS/rounding noise rather than a
 * deliberate reposition, and a label reading "moved 0 m · 173°" sitting on
 * screen for the entire time Adjust is open (before the operator has panned
 * at all) would be confusing, not informative.
 */
export function formatMovedLabel(offset: AnchorMoveOffset, isImperial: boolean): string | null {
  if (offset.distanceM < 1) return null
  const distanceLabel = isImperial
    ? `${Math.round(offset.distanceM * METERS_PER_FOOT)} ft`
    : `${Math.round(offset.distanceM)} m`
  const bearingLabel = String(Math.round(offset.bearingDeg) % 360).padStart(3, '0')
  return `moved ${distanceLabel} · ${bearingLabel}°`
}

/**
 * Whether the boat's live position would trip the alarm against the DRAFT
 * anchor/radius Adjust mode currently shows — not the committed watch.
 * Mirrors useAnchorWatch's own dragging predicate
 * (`distance > radius + DRAG_BUFFER_METERS`) exactly, against the draft
 * instead of server state, so "Alarm would sound now" means precisely that.
 */
export function adjustWarningActive(distanceFromDraftM: number | null, draftRadiusM: number, dragBufferM: number): boolean {
  return distanceFromDraftM !== null && distanceFromDraftM > draftRadiusM + dragBufferM
}

// ── Set/Undo commit targets (code-review finding) ──────────────────────────
//
// Set used to send lat/lon unconditionally, even when the operator only
// touched the radius (the bar's own +/-/chips, or the no-WebGL2 path, which
// can *only* change radius). The backend treats any lat/lon on the PATCH as
// a genuine reposition: it resets the post-anchor self trail and requires a
// fresh SignalK publish to confirm, which 502s outright if SignalK happens
// to be unreachable — turning an ordinary radius tweak into a failure that
// has nothing to do with the radius. Below this tolerance, the draft
// position is read as unchanged and lat/lon travel with neither Set nor its
// Undo.

/**
 * Set (and Undo) omit lat/lon when the draft position is within this of the
 * committed one. 0.5 m: comfortably tighter than ordinary GPS scatter at
 * anchor (which runs into the metres), so a genuine drag is never mistaken
 * for jitter, and loose enough that floating-point re-derivation through the
 * zoom/pixel round-trip (a crosshair the operator never actually panned)
 * can't itself trip it.
 */
export const POSITION_UNCHANGED_TOLERANCE_M = 0.5

export interface AdjustCommitTarget {
  lat?: number
  lon?: number
  radiusMeters: number
}

interface AdjustCommitPoint {
  lat: number
  lon: number
  radiusMeters: number
}

/**
 * Builds what Set actually PATCHes and what Undo PATCHes to reverse it, from
 * the watch's committed values and the Adjust session's draft. Both targets
 * are built from the SAME moved/unmoved decision, so Undo never restores a
 * position Set itself never touched — a radius-only Set is undone with a
 * radius-only PATCH too, not a position snap-back to wherever the crosshair
 * happened to sit.
 */
export function buildAdjustCommitTargets(
  committed: AdjustCommitPoint,
  draft: AdjustCommitPoint,
): { set: AdjustCommitTarget; undo: AdjustCommitTarget } {
  const moved = haversineMeters(committed.lat, committed.lon, draft.lat, draft.lon) >= POSITION_UNCHANGED_TOLERANCE_M
  if (!moved) {
    return {
      set: { radiusMeters: draft.radiusMeters },
      undo: { radiusMeters: committed.radiusMeters },
    }
  }
  return {
    set: { lat: draft.lat, lon: draft.lon, radiusMeters: draft.radiusMeters },
    undo: { lat: committed.lat, lon: committed.lon, radiusMeters: committed.radiusMeters },
  }
}
