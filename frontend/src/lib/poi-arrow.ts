export interface PixelPoint {
  x: number
  y: number
}

export interface PoiArrowGeometry {
  /** SVG path data of the curve, as `M x y Q cx cy x y`. */
  path: string
  /** SVG polygon points of the arrowhead, tip first. */
  head: string
}

// Below this the arrow has no room: the clearances at the two ends (22 + 24 px)
// leave 18 px at 64, still longer than the 10 px arrowhead.
const MIN_CHORD_PX = 64
// Clearance from the centre of each marker: the arrow starts outside the
// boat's 40 px marker and stops outside the place's highlight ring. This
// assumes the projected point is the centre of the marker's icon.
const START_CLEARANCE_PX = 22
const END_CLEARANCE_PX = 24
const BOW_FRACTION = 0.2
const BOW_MAX_PX = 60
const HEAD_LENGTH_PX = 10
const HEAD_HALF_WIDTH_PX = 5

type Vec = [number, number]

const unit = (from: Vec, to: Vec): Vec => {
  const dx = to[0] - from[0]
  const dy = to[1] - from[1]
  const length = Math.hypot(dx, dy) || 1
  return [dx / length, dy / length]
}

/**
 * The curved arrow from the boat to the highlighted place, in the pixels of
 * the map pane: a quadratic Bezier from the boat to the place, trimmed to
 * start clear of the boat marker and stop clear of the place's ring, with an
 * arrowhead at the end. The curve bows to a fixed side of the direction of
 * travel, so it stays on the same side however the boat jitters (a rule based
 * on the pane centre would flip with GPS noise, since after a framing jump the
 * boat sits at the centre). It takes the other side only when that would leave
 * the control point outside the pane. Null when the two are too close
 * for an arc, which leaves only the ring to show the place.
 *
 * Each end is trimmed by moving straight toward the control point, which
 * keeps the curve a single Bezier from the trimmed start to the trimmed end
 * with the same control point. For this shallow a bow it differs from the
 * exact trim by a pixel or two.
 *
 * Coordinates are whole pixels, so a small movement of the boat gives the
 * same strings and React leaves the DOM alone.
 */
export function poiArrowGeometry(
  boat: PixelPoint,
  place: PixelPoint,
  pane: { width: number; height: number },
): PoiArrowGeometry | null {
  const dx = place.x - boat.x
  const dy = place.y - boat.y
  const length = Math.hypot(dx, dy)
  if (!(length >= MIN_CHORD_PX)) return null

  const normal: Vec = [-dy / length, dx / length]
  const bow = Math.min(length * BOW_FRACTION, BOW_MAX_PX)
  const mid: Vec = [(boat.x + place.x) / 2, (boat.y + place.y) / 2]
  const bowed = (side: number): Vec => [mid[0] + normal[0] * bow * side, mid[1] + normal[1] * bow * side]
  const onPane = ([x, y]: Vec) => x >= 0 && x <= pane.width && y >= 0 && y <= pane.height
  const control = onPane(bowed(1)) ? bowed(1) : bowed(-1)

  const boatPoint: Vec = [boat.x, boat.y]
  const placePoint: Vec = [place.x, place.y]
  const startDir = unit(boatPoint, control)
  const endDir = unit(placePoint, control)
  const start: Vec = [boat.x + startDir[0] * START_CLEARANCE_PX, boat.y + startDir[1] * START_CLEARANCE_PX]
  const end: Vec = [place.x + endDir[0] * END_CLEARANCE_PX, place.y + endDir[1] * END_CLEARANCE_PX]

  const dir = unit(control, placePoint)
  const baseX = end[0] - dir[0] * HEAD_LENGTH_PX
  const baseY = end[1] - dir[1] * HEAD_LENGTH_PX
  const perp: Vec = [-dir[1], dir[0]]

  const r = Math.round
  return {
    path: `M${r(start[0])} ${r(start[1])} Q${r(control[0])} ${r(control[1])} ${r(end[0])} ${r(end[1])}`,
    head: [
      `${r(end[0])},${r(end[1])}`,
      `${r(baseX + perp[0] * HEAD_HALF_WIDTH_PX)},${r(baseY + perp[1] * HEAD_HALF_WIDTH_PX)}`,
      `${r(baseX - perp[0] * HEAD_HALF_WIDTH_PX)},${r(baseY - perp[1] * HEAD_HALF_WIDTH_PX)}`,
    ].join(' '),
  }
}
