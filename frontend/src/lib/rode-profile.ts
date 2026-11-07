import { suspendedLengthM } from '@/lib/catenary'

export interface RodeProfileInput {
  /** Rode paid out, bow roller to anchor (m). */
  rodeM: number
  /** Height of the bow roller above the seabed (m): depth plus bow height. */
  heightM: number
  /** Catenary parameter a = H / w (m): horizontal load over submerged weight per metre. */
  parameterM: number
}

export interface RodePoint {
  /** Metres along the seabed from the anchor. */
  x: number
  /** Metres above the seabed. */
  y: number
}

export interface RodeProfile {
  /** Rode lying on the seabed, anchor to touchdown (m). */
  lyingM: number
  /** Horizontal distance of the suspended part (m). */
  spanM: number
  /** Anchor to bow roller, horizontally (m). */
  reachM: number
  /** True when the rode is too short to lie down under the given load, so the anchor is being pulled up. */
  anchorLifting: boolean
  touchdown: RodePoint
  /** The suspended part, touchdown first, bow roller last. */
  points: RodePoint[]
}

const SEGMENTS = 16
/** Relative shortfall below which a rode counts as exactly the boundary length. */
const LIFT_TOLERANCE = 1e-6

/**
 * Side-view shape of a chain rode: the catenary y = a(cosh(s/a) - 1) from
 * touchdown, with any rode beyond the suspended length lying on the bottom.
 * Too short for the load, it is the shape that just reaches the anchor, and the
 * anchor is lifting.
 *
 * null when the rode cannot reach the seabed, an input is unusable, or the
 * arithmetic does not come out finite. No shape is invented for those.
 */
export function rodeProfile({ rodeM, heightM, parameterM }: RodeProfileInput): RodeProfile | null {
  if (!Number.isFinite(rodeM) || !Number.isFinite(heightM) || heightM <= 0) return null
  if (rodeM <= heightM) return null
  if (!Number.isFinite(parameterM) || parameterM <= 0) return null

  // (L - h)(L + h) rather than L² - h²: it stays exact when L is barely over h.
  const touchAtAnchorA = ((rodeM - heightM) * (rodeM + heightM)) / (2 * heightM)
  let a = parameterM
  let suspendedM = suspendedLengthM(heightM, a)
  let anchorLifting = false

  if (rodeM < suspendedM) {
    // Lifting only past float noise: a rode within a few ulps of the boundary
    // length is just touching.
    anchorLifting = rodeM < suspendedM * (1 - LIFT_TOLERANCE)
    a = touchAtAnchorA
    suspendedM = rodeM
  }

  const lyingM = Math.max(0, rodeM - suspendedM)
  const spanM = a * Math.asinh(suspendedM / a)

  const points: RodePoint[] = []
  for (let i = 0; i <= SEGMENTS; i += 1) {
    const s = (spanM * i) / SEGMENTS
    points.push({ x: lyingM + s, y: a * (Math.cosh(s / a) - 1) })
  }
  if (!Number.isFinite(spanM) || points.some((p) => !Number.isFinite(p.x) || !Number.isFinite(p.y))) return null
  // Pin the end to the roller so rounding cannot leave it a hair off the bow.
  points[points.length - 1] = { x: lyingM + spanM, y: heightM }

  return {
    lyingM,
    spanM,
    reachM: lyingM + spanM,
    anchorLifting,
    touchdown: { x: lyingM, y: 0 },
    points,
  }
}

export const DIAGRAM_WIDTH = 320
/** Where the leftmost anchor sits from the left edge. */
export const DIAGRAM_LEFT = 14
export const DIAGRAM_HULL_PX = 84
export const DIAGRAM_SEABED_PX = 12
const DIAGRAM_RIGHT_PAD = 6
const DIAGRAM_HEADROOM = 8
const DIAGRAM_MAX_PLOT_PX = 110
const DIAGRAM_MIN_HEIGHT = 48

/**
 * Fits the side view to its content: one scale on both axes, set by the width
 * the reach needs or by the height cap, cropped to a little headroom above the
 * bow roller and the seabed strip.
 */
export function rodeDiagramLayout({ reachM, heightM }: { reachM: number; heightM: number }): {
  height: number
  /** Pixels per metre, the same on both axes. */
  scale: number
  seabedY: number
} {
  const scale = Math.min(
    (DIAGRAM_WIDTH - DIAGRAM_LEFT - DIAGRAM_HULL_PX - DIAGRAM_RIGHT_PAD) / reachM,
    DIAGRAM_MAX_PLOT_PX / heightM,
  )
  const height = Math.max(DIAGRAM_MIN_HEIGHT, DIAGRAM_HEADROOM + heightM * scale + DIAGRAM_SEABED_PX)
  return { height, scale, seabedY: height - DIAGRAM_SEABED_PX }
}

const HULL_DRAFT_PX = 7

/**
 * How far the drawn hull sits below the water line. A fixed draft would poke
 * through the seabed in shallow water with a long reach, where the scale
 * squeezes the water to a few pixels, so it never takes more than half of it.
 */
export function hullDraftPx(waterDepthPx: number): number {
  return Math.min(HULL_DRAFT_PX, Math.max(0, waterDepthPx) / 2)
}
