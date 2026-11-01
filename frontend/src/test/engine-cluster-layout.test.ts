import { describe, expect, test } from 'vitest'

import {
  CARD_TOPS, CLUSTER_H, CLUSTER_INNER_H, CLUSTER_INNER_TOP, CLUSTER_W, clusterLayout, fuelArc, fuelFeet, fuelPoint, fuelTitleLeft, gapArcPath, mirrorX,
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

  test('ends where the inner space ends', () => {
    expect(CLUSTER_H - CLUSTER_INNER_TOP).toBe(CLUSTER_INNER_H)
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
