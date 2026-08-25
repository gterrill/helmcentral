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
    geometry: { ringTop, topCardH, bottomCardH, bottomCardTop },
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
