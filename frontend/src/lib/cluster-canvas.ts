import { useEffect, useRef, useState, type CSSProperties } from 'react'

/**
 * Fixed-canvas geometry for a ring-with-corner-cards tile (ADR 0054).
 *
 * Extracted from wind-tile.tsx, which established it: the cards and the ring
 * sit in a fixed-size canvas rather than stretching with their column, so each
 * card's inner corner can be mask-cut along a circle that lines up with the
 * ring. The engine cluster is the same layout with a different ring, so the
 * maths lives here and both import it.
 */

export type ClusterCanvasConfig = {
  width: number
  height: number
  /** Side of the square the ring is drawn in. */
  ringBox: number
  /**
   * Distance from the canvas top to the ring box. Defaults to centring it.
   *
   * Explicit because a layout that aligns cards to the arc cannot also derive
   * the ring's position from the canvas height — each would depend on the
   * other.
   */
  ringTop?: number
  topCardW: number
  bottomCardW: number
  cardH: number
  /** Per-row overrides; both default to cardH. */
  topCardH?: number
  bottomCardH?: number
  /** Distance from the canvas top to the lower card row. Defaults to flush bottom. */
  bottomCardTop?: number
  gap: number
  /**
   * The ring's own radius as a fraction of half its box. WindCompass draws at
   * 130 in a 280 viewBox; DialRing's rim sits slightly further out.
   */
  radiusRatio?: number
}

// Width of the card's straight-edge border (Tailwind's bare `border` utility), so the
// drawn ring along the mask's cut edge reads as a continuous border, not just 3 sides.
const BORDER_WIDTH = 1

/**
 * How far past its own box a ring may paint before it meets the cards' cut
 * line. A dial with a bezel is a solid circle rather than an arc on nothing,
 * so its caller has to know this to give the disc room and to centre it.
 *
 * Independent of ringTop, which is what lets a layout derive its ring position
 * from the disc size without the two defining each other.
 */
export function bezelOverhangFor(ringBox: number, gap: number, radiusRatio = 130 / 140): number {
  return Math.max(0, (ringBox / 2) * radiusRatio + gap - 1 - ringBox / 2)
}

/**
 * The engine cluster's own canvas (ADR 0054).
 *
 * Here rather than in the tile because it is not only the tile that needs it:
 * the bento grid derives the cluster's minimum row count from this height, and
 * the last copy of that number was written by hand and went stale.
 */
const RING_BOX = 218
const CARD_W = 196
const CARD_H = 92
const GAP = 14

/**
 * The bezel makes the dial a solid disc, so the canvas has to contain the
 * whole circle. It used to stop where the 250-degree sweep ended and let the
 * empty lower part of the ring box hang off the bottom, which cost nothing
 * while nothing was drawn there and put the dial through the tile's edge the
 * moment a bezel was.
 *
 * So the composition now closes on the circle: the disc is exactly as tall as
 * the block of boxes and centred on it, which is also what makes all four the
 * same size instead of the lower pair being stretched to reach the arc.
 */
const OVERHANG = bezelOverhangFor(RING_BOX, GAP)
const DISC = RING_BOX + 2 * OVERHANG
const RING_TOP = OVERHANG
const CARD_BLOCK_H = DISC

export const CLUSTER_CANVAS: ClusterCanvasConfig = {
  width: 520,
  height: CARD_BLOCK_H,
  ringBox: RING_BOX,
  ringTop: RING_TOP,
  topCardW: CARD_W,
  bottomCardW: CARD_W,
  cardH: CARD_H,
  bottomCardTop: CARD_BLOCK_H - CARD_H,
  gap: GAP,
}

/**
 * How wide the cluster's design is once a fuel rail is attached (ADR 0061).
 *
 * The rail is a sibling column, so CLUSTER_CANVAS keeps its 520 and the corner
 * masks keep every offset they were computed against. Growing the canvas itself
 * would re-centre the ring and move all four cards, including on clusters that
 * have no rail at all.
 */
export const CLUSTER_RAIL = { width: 112, gap: 14 }

export function clusterDesignWidth(hasRail: boolean): number {
  return CLUSTER_CANVAS.width + (hasRail ? CLUSTER_RAIL.width + CLUSTER_RAIL.gap : 0)
}

export function computeCornerMasks(cfg: ClusterCanvasConfig) {
  const { width, height, ringBox, topCardW, bottomCardW, cardH, gap, radiusRatio = 130 / 140 } = cfg
  const topCardH = cfg.topCardH ?? cardH
  const bottomCardH = cfg.bottomCardH ?? cardH
  const bottomCardTop = cfg.bottomCardTop ?? height - bottomCardH
  const ringTop = cfg.ringTop ?? (height - ringBox) / 2

  const ringRadius = (ringBox / 2) * radiusRatio
  const inner = ringRadius + gap - 1
  const outer = ringRadius + gap + 1
  const ringOuter = outer + BORDER_WIDTH
  const centerX = width / 2
  const centerY = ringTop + ringBox / 2

  const mask = (cardLeft: number, cardTop: number): CSSProperties => {
    const x = centerX - cardLeft
    const y = centerY - cardTop
    const maskImg = `radial-gradient(circle at ${x}px ${y}px, transparent ${inner}px, #000 ${outer}px)`
    // A thin ring drawn just outside the mask's cut line, in the card's border color,
    // so the curved edge gets a stroke to match the card's straight-edge border.
    const ringImg = `radial-gradient(circle at ${x}px ${y}px, transparent ${outer}px, hsl(var(--border)) ${outer}px, hsl(var(--border)) ${ringOuter}px, transparent ${ringOuter}px)`
    return { maskImage: maskImg, WebkitMaskImage: maskImg, backgroundImage: ringImg }
  }

  return {
    tl: mask(0, 0),
    tr: mask(width - topCardW, 0),
    bl: mask(0, bottomCardTop),
    br: mask(width - bottomCardW, bottomCardTop),
    geometry: {
      ringTop, topCardH, bottomCardH, bottomCardTop,
      // How far past its own box the ring may paint before it meets the cards'
      // cut line. A dial bezel wants exactly this, and deriving it here keeps
      // it tracking `gap` and `radiusRatio` instead of being matched by eye.
      bezelOverhang: bezelOverhangFor(ringBox, gap, radiusRatio),
    },
  }
}

/** Scales a fixed-size design down (never up) to fit whatever width its column ends up with. */
export function useFitScale(designWidth: number) {
  const ref = useRef<HTMLDivElement>(null)
  const [scale, setScale] = useState(1)

  useEffect(() => {
    const el = ref.current
    if (!el) return
    const observer = new ResizeObserver(([entry]) => {
      setScale(Math.min(1, entry.contentRect.width / designWidth))
    })
    observer.observe(el)
    return () => observer.disconnect()
  }, [designWidth])

  return [ref, scale] as const
}
