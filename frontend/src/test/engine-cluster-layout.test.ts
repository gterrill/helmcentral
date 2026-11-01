import { describe, expect, test } from 'vitest'

import {
  CARD_H, CARD_TOPS, CLUSTER_H, CLUSTER_INNER_TOP, CLUSTER_SIZES, CLUSTER_W, STACKED_MARGIN, STRIP_H, STRIP_MARGIN, clusterLayout, fuelArc, fuelFeet, fuelPoint, fuelTitleLeft, gapArcPath, mirrorX,
} from '@/lib/engine-cluster-layout'

/** Pulls every "x y" pair out of an SVG path built from M/L/Q commands. */
function points(d: string): Array<[number, number]> {
  return [...d.matchAll(/(-?\d+(?:\.\d+)?) (-?\d+(?:\.\d+)?)/g)].map((m) => [Number(m[1]), Number(m[2])])
}

// Nothing rides the card's top edge any more, so the card starts just above
// the first readout card rather than leaving a band for tabs.
describe('engine cluster layout: the visible card', () => {
  test('starts no more than 12px above the first readout card', () => {
    const gap = CARD_TOPS[0] + CLUSTER_INNER_TOP
    expect(gap).toBeGreaterThanOrEqual(0)
    expect(gap).toBeLessThanOrEqual(12)
  })
})

describe('engine cluster layout: the starboard tile is the port tile reflected', () => {
  test('mirrorX reflects about the vertical centre line and leaves port alone', () => {
    expect(mirrorX('port')(100)).toBe(100)
    expect(mirrorX('stbd')(100)).toBe(CLUSTER_W - 100)
  })

  test('readout cards sit on the inner edge: right of the dial on port, left on starboard', () => {
    const port = clusterLayout('port')
    const stbd = clusterLayout('stbd')
    for (const r of port.readouts) expect(r.left).toBeGreaterThan(CLUSTER_W / 2)
    for (const r of stbd.readouts) expect(r.left + r.width).toBeLessThan(CLUSTER_W / 2)
  })

  test('every card outline is the other side reflected', () => {
    const port = clusterLayout('port')
    const stbd = clusterLayout('stbd')
    port.readouts.forEach((r, i) => {
      const a = points(r.path)
      const b = points(stbd.readouts[i].path)
      expect(b).toHaveLength(a.length)
      a.forEach(([x, y], k) => {
        expect(b[k][0]).toBeCloseTo(CLUSTER_W - x, 0)
        expect(b[k][1]).toBeCloseTo(y, 0)
      })
    })
  })

  test('all four bullets share one width so their scales line up', () => {
    const { bulletWidth, readouts } = clusterLayout('port')
    expect(bulletWidth).toBeGreaterThan(100)
    for (const r of readouts) expect(r.width).toBeGreaterThanOrEqual(bulletWidth)
  })

  test('the fuel fan and its title sit on the outer edge', () => {
    expect(fuelPoint('port', 256, 0.5)[0]).toBeLessThan(CLUSTER_W / 2)
    expect(fuelPoint('stbd', 256, 0.5)[0]).toBeGreaterThan(CLUSTER_W / 2)
    expect(fuelTitleLeft('port')).toBeLessThan(fuelTitleLeft('stbd'))
    const port = points(fuelArc('port', 256, 0.6))
    const stbd = points(fuelArc('stbd', 256, 0.6))
    port.forEach(([x, y], k) => {
      expect(stbd[k][0]).toBeCloseTo(CLUSTER_W - x, 0)
      expect(stbd[k][1]).toBeCloseTo(y, 0)
    })
  })

  test('an empty fuel arc draws nothing', () => {
    expect(fuelArc('port', 256, 0)).toBe('')
  })

  test('the dial gap arcs do not mirror: coolant stays left, oil stays right', () => {
    expect(gapArcPath('left', 0, 1)).not.toBe('')
    const left = points(gapArcPath('left', 0, 1))
    const right = points(gapArcPath('right', 0, 1))
    expect(Math.max(...left.map((p) => p[0]))).toBeLessThan(CLUSTER_W / 2)
    expect(Math.min(...right.map((p) => p[0]))).toBeGreaterThan(CLUSTER_W / 2)
  })
})

// The feet are closer together than one caption is wide, so each caption must
// run away from its neighbour or the pair prints as "FWDAFT".
describe('engine cluster layout: the FWD and AFT captions', () => {
  test.each(['port', 'stbd'] as const)('on %s share one baseline and run apart', (side) => {
    const [fwd, aft] = fuelFeet(side)
    expect(fwd.y).toBe(aft.y)
    // The caption nearer the card edge grows towards it, the inner one towards the dial.
    const aftIsLeft = aft.x < fwd.x
    expect(aft.anchor).toBe(aftIsLeft ? 'end' : 'start')
    expect(fwd.anchor).toBe(aftIsLeft ? 'start' : 'end')
  })
})

describe('engine cluster layout: stacked layout width', () => {
  test('is unchanged at the design width and has no footer column', () => {
    const layout = clusterLayout('port')
    expect(layout.shift).toBe(0)
    expect(layout.footer).toEqual([])
    const wide = clusterLayout('port', 'stacked', CLUSTER_W + 100)
    // Extra width goes to the readout cards, which still end 40px from the edge.
    expect(wide.readouts[0].left + wide.readouts[0].width).toBeCloseTo(CLUSTER_W + 100 - STACKED_MARGIN - 16, 0)
    expect(wide.bulletWidth).toBeGreaterThan(layout.bulletWidth)
    expect(wide.shift).toBe(0)
    expect(clusterLayout('stbd', 'stacked', CLUSTER_W + 100).shift).toBe(100)
  })

  test('stretching leaves the footer bullets alone so the alternator card takes the extra', () => {
    const base = clusterLayout('port')
    expect(clusterLayout('port', 'stacked', CLUSTER_W + 300).footerBulletWidth).toBe(base.footerBulletWidth)
    expect(clusterLayout('stbd', 'stacked', CLUSTER_W + 300).footerBulletWidth).toBe(base.footerBulletWidth)
  })
})

describe('engine cluster layout: the strip', () => {
  const STRIP_W = CLUSTER_SIZES.strip.width
  const maxX = (d: string) => Math.max(...points(d).map((p) => p[0]))
  const minX = (d: string) => Math.min(...points(d).map((p) => p[0]))

  test('is just the dial block high, with the stacked card\'s margin above and below the readout column', () => {
    const top = CARD_TOPS[0] + CLUSTER_INNER_TOP
    const bottom = STRIP_H - (CARD_TOPS[3] + CARD_H + CLUSTER_INNER_TOP)
    expect(top).toBeGreaterThanOrEqual(10)
    expect(top).toBeLessThanOrEqual(12)
    expect(bottom).toBeGreaterThanOrEqual(10)
    expect(bottom).toBeLessThanOrEqual(12)
    expect(STRIP_H).toBeLessThan(CLUSTER_H - 100)
  })

  test('puts one footer card beside each readout card, with the same top and height', () => {
    for (const side of ['port', 'stbd'] as const) {
      const { footer } = clusterLayout(side, 'strip', STRIP_W)
      expect(footer.map((f) => f.top)).toEqual([...CARD_TOPS])
      for (const f of footer) expect(f.height).toBe(CARD_H)
    }
  })

  test('port runs fan, dial, readouts, footer; starboard is that reflected', () => {
    const port = clusterLayout('port', 'strip', STRIP_W)
    const stbd = clusterLayout('stbd', 'strip', STRIP_W)
    port.footer.forEach((f, i) => {
      expect(f.left).toBeGreaterThanOrEqual(port.readouts[i].left + port.readouts[i].width)
      expect(f.left + f.width).toBeCloseTo(STRIP_W - STRIP_MARGIN, 5)
      expect(stbd.footer[i].left).toBeCloseTo(STRIP_W - (f.left + f.width), 5)
      expect(stbd.footer[i].width).toBeCloseTo(f.width, 5)
    })
    expect(maxX(port.fuelCard) + port.shift).toBeLessThan(STRIP_W / 2)
    expect(minX(stbd.fuelCard) + stbd.shift).toBeGreaterThan(STRIP_W / 2)
  })

  test('keeps 12px from both edges, at the design width and stretched', () => {
    for (const width of [STRIP_W, STRIP_W + 240]) {
      const port = clusterLayout('port', 'strip', width)
      const stbd = clusterLayout('stbd', 'strip', width)
      expect(minX(port.fuelCard) + port.shift).toBeCloseTo(STRIP_MARGIN, 0)
      expect(maxX(stbd.fuelCard) + stbd.shift).toBeCloseTo(width - STRIP_MARGIN, 0)
      const last = (l: typeof port) => l.footer[0]
      expect(last(port).left + last(port).width).toBeCloseTo(width - STRIP_MARGIN, 5)
      expect(last(stbd).left).toBeCloseTo(STRIP_MARGIN, 5)
    }
  })

  test('stretching widens the readout cards and the footer cards, not the dial or fan', () => {
    const base = clusterLayout('port', 'strip', STRIP_W)
    const wide = clusterLayout('port', 'strip', STRIP_W + 240)
    expect(wide.readouts[0].width).toBeGreaterThan(base.readouts[0].width)
    expect(wide.footer[0].width).toBeGreaterThan(base.footer[0].width)
    expect(wide.footer[0].width - base.footer[0].width + (wide.readouts[0].width - base.readouts[0].width)).toBeCloseTo(240, 5)
    expect(wide.shift).toBe(base.shift)
    expect(wide.fuelCard).toBe(base.fuelCard)
  })

  test('readout outlines on starboard are the port ones reflected about the actual width', () => {
    const width = STRIP_W + 100
    const port = clusterLayout('port', 'strip', width)
    const stbd = clusterLayout('stbd', 'strip', width)
    port.readouts.forEach((r, i) => {
      const a = points(r.path)
      const b = points(stbd.readouts[i].path)
      a.forEach(([x, y], k) => {
        expect(b[k][0]).toBeCloseTo(width - x, 0)
        expect(b[k][1]).toBeCloseTo(y, 0)
      })
    })
  })

  test('the readout cards still clear the dial', () => {
    const { readouts } = clusterLayout('port', 'strip', STRIP_W)
    // Dial centre in the strip is 372: its 204px cut ends at 576.
    for (const r of readouts) expect(r.left).toBeGreaterThanOrEqual(372)
  })

  test('mirrorX reflects about the width it is given', () => {
    expect(mirrorX('stbd', 1000)(100)).toBe(900)
    expect(mirrorX('port', 1000)(100)).toBe(100)
  })
})
