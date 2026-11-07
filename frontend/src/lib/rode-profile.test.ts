import { describe, expect, it } from 'vitest'
import {
  DIAGRAM_HULL_PX,
  DIAGRAM_LEFT,
  DIAGRAM_SEABED_PX,
  DIAGRAM_WIDTH,
  hullDraftPx,
  rodeDiagramLayout,
  rodeProfile,
} from '@/lib/rode-profile'

const DEEP_DRAFT_PX = 7

describe('rodeProfile', () => {
  it('lays the excess rode on the seabed and hangs the rest as a catenary (known case)', () => {
    // h = 5, a = 10: sqrt(h² + 2ah) = sqrt(125); sqrt(125)/10 = 1.118 and
    // asinh(1.118) = ln(φ²) = 0.9624, so the span is 9.624 m and y(span) = h.
    const p = rodeProfile({ rodeM: 30, heightM: 5, parameterM: 10 })!
    const suspended = Math.sqrt(125)
    expect(p.anchorLifting).toBe(false)
    expect(p.lyingM).toBeCloseTo(30 - suspended, 6)
    expect(p.spanM).toBeCloseTo(9.6242, 3)
    expect(p.reachM).toBeCloseTo(30 - suspended + 9.6242, 3)
    expect(p.touchdown).toEqual({ x: p.lyingM, y: 0 })
    expect(p.points[0]).toEqual({ x: p.lyingM, y: 0 })
    const last = p.points[p.points.length - 1]
    expect(last.x).toBeCloseTo(p.reachM, 6)
    expect(last.y).toBeCloseTo(5, 6)
  })

  it('keeps every point on y = a(cosh(s/a) - 1) and rising', () => {
    const p = rodeProfile({ rodeM: 30, heightM: 5, parameterM: 10 })!
    for (const pt of p.points) {
      expect(pt.y).toBeCloseTo(10 * (Math.cosh((pt.x - p.lyingM) / 10) - 1), 6)
    }
    for (let i = 1; i < p.points.length; i += 1) {
      expect(p.points[i].y).toBeGreaterThanOrEqual(p.points[i - 1].y)
    }
  })

  it('has nothing on the seabed exactly at the boundary L = sqrt(h² + 2ah)', () => {
    const L = Math.sqrt(125)
    const p = rodeProfile({ rodeM: L, heightM: 5, parameterM: 10 })!
    expect(p.anchorLifting).toBe(false)
    expect(p.lyingM).toBeCloseTo(0, 9)
  })

  it('does not flag a rode a few ulps either side of the boundary as lifting', () => {
    const L = Math.sqrt(125)
    for (const rodeM of [L * (1 - 4e-16), L * (1 - 1e-12), L, L * (1 + 4e-16)]) {
      const p = rodeProfile({ rodeM, heightM: 5, parameterM: 10 })!
      expect(p.anchorLifting).toBe(false)
      expect(p.lyingM).toBeGreaterThanOrEqual(0)
    }
    // A real shortfall (0.1%) is still flagged.
    expect(rodeProfile({ rodeM: L * 0.999, heightM: 5, parameterM: 10 })!.anchorLifting).toBe(true)
  })

  it('keeps every point finite when the rode only just exceeds the depth to the hawse', () => {
    for (const [rodeM, heightM] of [[5 + 1e-9, 5], [500 + 1e-9, 500], [27 + 1e-12, 27]]) {
      for (const parameterM of [10, 0.5]) {
        const p = rodeProfile({ rodeM, heightM, parameterM })!
        expect(p).not.toBeNull()
        for (const pt of p.points) {
          expect(Number.isFinite(pt.x)).toBe(true)
          expect(Number.isFinite(pt.y)).toBe(true)
        }
        expect(Number.isFinite(p.reachM)).toBe(true)
        expect(p.points[p.points.length - 1].y).toBeCloseTo(heightM, 6)
      }
    }
  })

  it('returns null, not a made-up shape, when the arithmetic does not come out finite', () => {
    // A parameter this small makes the span overflow: nothing sensible to draw.
    expect(rodeProfile({ rodeM: 30, heightM: 5, parameterM: Number.MIN_VALUE })).toBeNull()
  })

  it('flags a lifting anchor when the rode is short for the load, and draws the shape that just reaches the anchor', () => {
    // a becomes (L² - h²) / 2h = (100 - 25) / 10 = 7.5, not the 10 asked for.
    const p = rodeProfile({ rodeM: 10, heightM: 5, parameterM: 10 })!
    expect(p.anchorLifting).toBe(true)
    expect(p.lyingM).toBe(0)
    // Drawn with a = (L² - h²) / 2h = 7.5, not the 10 asked for.
    expect(p.spanM).toBeCloseTo(7.5 * Math.asinh(10 / 7.5), 9)
    expect(p.points[0]).toEqual({ x: 0, y: 0 })
    const last = p.points[p.points.length - 1]
    expect(last.y).toBeCloseTo(5, 6)
    expect(last.x).toBeCloseTo(p.reachM, 6)
  })

  it('returns null when the rode cannot reach the seabed', () => {
    expect(rodeProfile({ rodeM: 5, heightM: 5, parameterM: 10 })).toBeNull()
  })

  it('returns null for unusable inputs instead of inventing a shape', () => {
    expect(rodeProfile({ rodeM: 30, heightM: 0, parameterM: 10 })).toBeNull()
    expect(rodeProfile({ rodeM: Number.NaN, heightM: 5, parameterM: 10 })).toBeNull()
    expect(rodeProfile({ rodeM: 30, heightM: 5, parameterM: 0 })).toBeNull()
    expect(rodeProfile({ rodeM: 30, heightM: 5, parameterM: -3 })).toBeNull()
  })
})

describe('rodeDiagramLayout', () => {
  it('crops the height to the content in deep water, with no empty band above the bow', () => {
    // 27 m to the hawse, 81 m of rode: reach is about 80 m, so width sets the scale.
    const layout = rodeDiagramLayout({ reachM: 80, heightM: 27 })
    const rollerY = layout.seabedY - 27 * layout.scale
    expect(layout.scale).toBeCloseTo((DIAGRAM_WIDTH - DIAGRAM_LEFT - DIAGRAM_HULL_PX - 6) / 80, 9)
    // Only a little room above the bow roller, and nothing below the seabed strip.
    expect(rollerY).toBeGreaterThan(0)
    expect(rollerY).toBeLessThan(12)
    expect(layout.height).toBeCloseTo(rollerY + 27 * layout.scale + DIAGRAM_SEABED_PX, 9)
    expect(layout.height).toBeLessThan(110)
  })

  it('caps the height of a tall, short-reach picture by shrinking both axes together', () => {
    const layout = rodeDiagramLayout({ reachM: 10, heightM: 9 })
    expect(9 * layout.scale).toBeLessThanOrEqual(110 + 1e-9)
  })

  it('keeps a minimum height in shallow water', () => {
    const layout = rodeDiagramLayout({ reachM: 12, heightM: 1 })
    expect(layout.height).toBeGreaterThanOrEqual(48)
    expect(layout.seabedY).toBeCloseTo(layout.height - DIAGRAM_SEABED_PX, 9)
  })
})

describe('hullDraftPx', () => {
  it('keeps the hull off the seabed in shallow water with a long reach (2 m deep, 1 m bow, 60 m rode)', () => {
    const heightM = 3
    const profile = rodeProfile({ rodeM: 60, heightM, parameterM: 10 })!
    const layout = rodeDiagramLayout({ reachM: profile.reachM, heightM })
    const waterPx = 2 * layout.scale
    const draft = hullDraftPx(waterPx)
    expect(draft).toBeGreaterThan(0)
    // Leaves at least a third of the water under the keel.
    expect(waterPx - draft).toBeGreaterThanOrEqual(waterPx / 3)
  })

  it('draws the full draft in deep water', () => {
    expect(hullDraftPx(80)).toBe(DEEP_DRAFT_PX)
  })
})
