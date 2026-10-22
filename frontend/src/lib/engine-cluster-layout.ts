import type { ClusterSide } from '@/lib/dashboard-widgets'

/**
 * Geometry for the Engine Cluster tile: an 800 x 540 card with a 380 px dial,
 * four readout cards cut round it, a fuel fan on the outer edge and a footer.
 *
 * Pure functions of the side, so the mirror is testable without rendering:
 * a starboard tile is the port one reflected about the vertical centre line.
 * Only the dial, its two gap arcs and the footer's contents stay put.
 *
 * Coordinates are the design's inner space, 800 wide and 648 tall; the visible
 * card starts 108 down it (CLUSTER_INNER_TOP), 11px above the first readout
 * card, which is how the dial centre ends up at 320 while the card is 540 high.
 * The band above that once held the status and hours tabs; the hours are in
 * the hub now and the status chip in the tile's title row.
 */

export const CLUSTER_W = 800
// The card ends 12px under the footer: the design's frame padded 20px past it, which the tile's own border now does.
export const CLUSTER_H = 540
/** The inner space starts this far above the visible card. */
export const CLUSTER_INNER_TOP = -108
export const CLUSTER_INNER_H = CLUSTER_H - CLUSTER_INNER_TOP

export const DIAL_CX = 400
export const DIAL_CY = 320
/** The cut the readout cards leave round the dial. */
const CARD_CUT_R = 204

const CARD_X0 = 430
const CARD_X1 = 760
export const CARD_TOPS = [119, 221, 323, 425] as const
export const CARD_H = 96

export const FUEL_R_FWD = 256
export const FUEL_R_AFT = 284
const FUEL_SPAN = 24

const RAD = Math.PI / 180

/** x on the starboard side is the port x reflected about the centre line. */
export function mirrorX(side: ClusterSide): (x: number) => number {
  return side === 'stbd' ? (x) => CLUSTER_W - x : (x) => x
}

const n1 = (v: number) => v.toFixed(1)

/**
 * The outline of one card on the inner side of the dial: rounded corners on the
 * outer edge and a concave edge following the dial's cut circle on the inner
 * one. `maxL` is how far the concave edge reaches into the card, which is where
 * its text has to start.
 */
export function cardPath(x0: number, y0: number, x1: number, y1: number, tf: (x: number) => number) {
  const rr = 22
  const P = (x: number, y: number) => `${n1(tf(x))} ${n1(y)}`
  const L = (y: number) => {
    const dy = y - DIAL_CY
    return Math.abs(dy) < CARD_CUT_R ? Math.max(x0, DIAL_CX + Math.sqrt(CARD_CUT_R * CARD_CUT_R - dy * dy)) : x0
  }
  const tl = L(y0) === x0
  const bl = L(y1) === x0
  let d = tl ? `M${P(x0, y0 + rr)}Q${P(x0, y0)} ${P(x0 + rr, y0)}` : `M${P(L(y0), y0)}`
  d += `L${P(x1 - rr, y0)}Q${P(x1, y0)} ${P(x1, y0 + rr)}L${P(x1, y1 - rr)}Q${P(x1, y1)} ${P(x1 - rr, y1)}`
  d += bl ? `L${P(x0 + rr, y1)}Q${P(x0, y1)} ${P(x0, y1 - rr)}` : `L${P(L(y1), y1)}`
  const ya = bl ? y1 - rr : y1
  const yb = tl ? y0 + rr : y0
  for (let i = 1; i <= 40; i += 1) {
    const y = ya + ((yb - ya) * i) / 40
    d += `L${P(L(y), y)}`
  }
  let maxL = x0
  for (let i = 0; i <= 40; i += 1) maxL = Math.max(maxL, L(y0 + ((y1 - y0) * i) / 40))
  return { d: `${d}Z`, maxL }
}

export interface ReadoutBox {
  /** Card outline. */
  path: string
  /** Where the readout's content sits, in the tile's own x. */
  left: number
  width: number
  top: number
  /** Left edge of the text in port space, which the bullet width derives from. */
  innerEdge: number
}

/** The four readout cards, the shared bullet width, and the fuel card's outline. */
export function clusterLayout(side: ClusterSide) {
  const m = mirrorX(side)
  const cards = CARD_TOPS.map((top) => cardPath(CARD_X0, top, CARD_X1, top + CARD_H, m))
  const textRight = CARD_X1 - 16
  // One width for every bullet, so their scales line up down the column.
  const bulletWidth = Math.min(...cards.map((c) => textRight - (c.maxL + 18)))

  const readouts: ReadoutBox[] = cards.map((card, i) => {
    const a = card.maxL + 18
    const b = textRight
    return {
      path: card.d,
      left: Math.min(m(a), m(b)),
      width: Math.abs(b - a),
      top: CARD_TOPS[i],
      innerEdge: a,
    }
  })

  // The fuel card is the readout column's reflection about the centre line,
  // so on the port side it sits left and on the starboard side right.
  const fuelCard = cardPath(
    2 * DIAL_CX - 370, CARD_TOPS[0], 2 * DIAL_CX - 40, CARD_TOPS[3] + CARD_H,
    (x) => m(2 * DIAL_CX - x),
  ).d

  return { readouts, bulletWidth, fuelCard }
}

/** Point on the fuel fan at radius `r` and fraction `t` (0 = foot, 1 = full). */
export function fuelPoint(side: ClusterSide, r: number, t: number): [number, number] {
  const a = (FUEL_SPAN - 2 * FUEL_SPAN * t) * RAD
  return [mirrorX(side)(DIAL_CX - r * Math.cos(a)), DIAL_CY + r * Math.sin(a)]
}

/** A polyline along a fuel arc from the foot to fraction `f`, or '' when empty. */
export function fuelArc(side: ClusterSide, r: number, f: number): string {
  if (!(f > 0)) return ''
  const steps = Math.max(2, Math.round(50 * f))
  let d = ''
  for (let i = 0; i <= steps; i += 1) {
    const [x, y] = fuelPoint(side, r, (f * i) / steps)
    d += `${i ? 'L' : 'M'}${n1(x)} ${n1(y)}`
  }
  return d
}

/** The fan's tick marks, in two paths so majors can be heavier. */
export function fuelTicks(side: ClusterSide) {
  let minor = ''
  let major = ''
  for (let i = 0; i <= 10; i += 1) {
    const t = i / 10
    const isMajor = i % 5 === 0
    const [ax, ay] = fuelPoint(side, isMajor ? 210 : 213, t)
    const [bx, by] = fuelPoint(side, isMajor ? 228 : 223, t)
    const seg = `M${n1(ax)} ${n1(ay)}L${n1(bx)} ${n1(by)}`
    if (isMajor) major += seg
    else minor += seg
  }
  return { minor, major }
}

/** Where the 0 / 50 / 100 scale numbers sit. */
export function fuelScaleLabels(side: ClusterSide) {
  return [0, 50, 100].map((p) => {
    const [x, y] = fuelPoint(side, 240, p / 100)
    return { value: p, x, y }
  })
}

/** Where a tank's percentage label rides: inside the bar's end when it fits, beyond it when not. */
export function fuelLegendPoint(side: ClusterSide, r: number, fraction: number) {
  const length = r * 2 * FUEL_SPAN * RAD
  const off = 22 / length
  const inside = fraction >= 2 * off
  const t = inside ? fraction - off : fraction + off
  const [x, y] = fuelPoint(side, r, t)
  const angle = FUEL_SPAN - 2 * FUEL_SPAN * t
  return { x, y, inside, rotate: side === 'stbd' ? -90 + angle : -90 - angle }
}

/** The 'FWD' / 'AFT' captions under the foot of each arc. */
export function fuelFeet(side: ClusterSide) {
  const [fx, fy] = fuelPoint(side, FUEL_R_FWD, 0)
  const [ax, ay] = fuelPoint(side, FUEL_R_AFT, 0)
  return [
    { label: 'FWD', x: fx, y: fy + 16 },
    { label: 'AFT', x: ax, y: ay + 16 },
  ]
}

/** Centre of the two arcs' feet, which is where the litres total sits. */
export function fuelTotalX(side: ClusterSide): number {
  const [fx] = fuelPoint(side, FUEL_R_FWD, 0)
  const [ax] = fuelPoint(side, FUEL_R_AFT, 0)
  return (fx + ax) / 2
}

/** Left edge of the fuel title block, which sits on the outer edge. */
export function fuelTitleLeft(side: ClusterSide): number {
  return side === 'stbd' ? CLUSTER_W - 56 - 220 : 56
}

/** A point at `deg` clockwise from straight up, `r` from the dial centre. */
export function dialPoint(r: number, deg: number): [number, number] {
  return [DIAL_CX + r * Math.sin(deg * RAD), DIAL_CY - r * Math.cos(deg * RAD)]
}

const GAP_ARC_R = 168
const GAP_ARC_SWEEP = 34

/**
 * One of the two arcs in the gap at the foot of the dial. The left one runs
 * clockwise-down from 222 degrees, the right one up from 138, so each reads
 * low-to-high away from the bottom centre. Neither mirrors with the tile.
 */
export const GAP_ARCS = {
  left: { start: 222, dir: -1, labelDx: -58 },
  right: { start: 138, dir: 1, labelDx: 58 },
} as const

export type GapArcName = keyof typeof GAP_ARCS

/** A polyline along a gap arc between two fractions of its length. */
export function gapArcPath(name: GapArcName, from: number, to: number): string {
  if (!(to > from)) return ''
  const { start, dir } = GAP_ARCS[name]
  let d = ''
  for (let i = 0; i <= 24; i += 1) {
    const [x, y] = dialPoint(GAP_ARC_R, start + dir * GAP_ARC_SWEEP * (from + ((to - from) * i) / 24))
    d += `${i ? 'L' : 'M'}${n1(x)} ${n1(y)}`
  }
  return d
}

/** The short radial bar marking the current value across a gap arc. */
export function gapArcMark(name: GapArcName, fraction: number): string {
  const { start, dir } = GAP_ARCS[name]
  const deg = start + dir * GAP_ARC_SWEEP * fraction
  const [ax, ay] = dialPoint(GAP_ARC_R - 9, deg)
  const [bx, by] = dialPoint(GAP_ARC_R + 9, deg)
  return `M${n1(ax)} ${n1(ay)}L${n1(bx)} ${n1(by)}`
}

/** Where a gap arc's value label sits: centred under the dial on a 437 baseline. */
export function gapArcLabelAt(name: GapArcName): { x: number; y: number } {
  return { x: DIAL_CX + GAP_ARCS[name].labelDx, y: 437 }
}
