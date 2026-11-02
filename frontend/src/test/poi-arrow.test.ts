import { describe, expect, it } from 'vitest'

import { poiArrowGeometry } from '@/lib/poi-arrow'

const pane = { width: 400, height: 300 }
const nums = (s: string) => (s.match(/-?\d+(\.\d+)?/g) ?? []).map(Number)

describe('poiArrowGeometry', () => {
  it('returns null when the boat and place are too close for an arc', () => {
    expect(poiArrowGeometry({ x: 100, y: 100 }, { x: 150, y: 130 }, pane)).toBeNull()
    expect(poiArrowGeometry({ x: 100, y: 100 }, { x: 100, y: 100 }, pane)).toBeNull()
  })

  it('draws from clear of the boat marker to clear of the place ring', () => {
    const g = poiArrowGeometry({ x: 100, y: 150 }, { x: 300, y: 150 }, pane)!
    expect(g).not.toBeNull()
    const [sx, sy, , , ex, ey] = nums(g.path)
    expect(Math.hypot(sx - 100, sy - 150)).toBeGreaterThanOrEqual(21)
    expect(Math.hypot(sx - 100, sy - 150)).toBeLessThanOrEqual(24)
    expect(Math.hypot(ex - 300, ey - 150)).toBeGreaterThanOrEqual(23)
    expect(Math.hypot(ex - 300, ey - 150)).toBeLessThanOrEqual(26)
  })

  it('bows toward the pane centre, on either side of the chord', () => {
    // Chord along the top edge: bow downward (toward the centre).
    const top = poiArrowGeometry({ x: 100, y: 20 }, { x: 300, y: 20 }, pane)!
    expect(nums(top.path)[3]).toBeGreaterThan(20)
    // Chord along the bottom edge: bow upward.
    const bottom = poiArrowGeometry({ x: 100, y: 280 }, { x: 300, y: 280 }, pane)!
    expect(nums(bottom.path)[3]).toBeLessThan(280)
    // Reversing the direction of travel still bows toward the centre.
    const reversed = poiArrowGeometry({ x: 300, y: 20 }, { x: 100, y: 20 }, pane)!
    expect(nums(reversed.path)[3]).toBeGreaterThan(20)
  })

  it('keeps the same side when the boat jitters across the pane centre', () => {
    // After a framing jump the boat sits at the pane centre, so the chord runs
    // through it and either side is as near the centre as the other.
    const above = poiArrowGeometry({ x: 100, y: 149.9 }, { x: 300, y: 150 }, pane)!
    const below = poiArrowGeometry({ x: 100, y: 150.1 }, { x: 300, y: 150 }, pane)!
    expect(Math.sign(nums(above.path)[3] - 150)).toBe(Math.sign(nums(below.path)[3] - 150))
  })

  it('bows inward when the usual side would leave the pane', () => {
    // Rightward along the bottom edge the usual side is down, off the pane.
    const bottom = poiArrowGeometry({ x: 100, y: 290 }, { x: 300, y: 290 }, pane)!
    expect(nums(bottom.path)[3]).toBeLessThan(290)
    // Leftward along the top edge the usual side is up, off the pane.
    const top = poiArrowGeometry({ x: 300, y: 10 }, { x: 100, y: 10 }, pane)!
    expect(nums(top.path)[3]).toBeGreaterThan(10)
  })

  it('bows by a fifth of the chord, capped at 60 px', () => {
    const short = poiArrowGeometry({ x: 100, y: 20 }, { x: 200, y: 20 }, pane)!
    expect(nums(short.path)[3]).toBe(40)
    const long = poiArrowGeometry({ x: 10, y: 20 }, { x: 790, y: 20 }, { width: 800, height: 300 })!
    expect(nums(long.path)[3]).toBe(80)
  })

  it('leaves the arrow longer than its head just above the minimum chord', () => {
    const g = poiArrowGeometry({ x: 100, y: 150 }, { x: 165, y: 150 }, pane)!
    expect(g).not.toBeNull()
    const [sx, sy, , , ex, ey] = nums(g.path)
    expect(Math.hypot(ex - sx, ey - sy)).toBeGreaterThanOrEqual(10)
  })

  it('gives whole-pixel coordinates only', () => {
    const g = poiArrowGeometry({ x: 101.37, y: 150.81 }, { x: 301.62, y: 177.2 }, pane)!
    for (const n of [...nums(g.path), ...nums(g.head)]) expect(Number.isInteger(n)).toBe(true)
  })

  it('points the arrowhead at the place, with its tip at the end of the path', () => {
    const g = poiArrowGeometry({ x: 100, y: 150 }, { x: 300, y: 150 }, pane)!
    const [, , , , ex, ey] = nums(g.path)
    const head = nums(g.head)
    expect(head).toHaveLength(6)
    expect([head[0], head[1]]).toEqual([ex, ey])
    // The base of the head sits back toward the boat.
    expect(head[2]).toBeLessThan(ex)
    expect(head[4]).toBeLessThan(ex)
  })

  it('produces an identical result for a sub-pixel boat movement', () => {
    const a = poiArrowGeometry({ x: 100.1, y: 150.1 }, { x: 300, y: 150 }, pane)
    const b = poiArrowGeometry({ x: 100.2, y: 150.05 }, { x: 300, y: 150 }, pane)
    expect(a).toEqual(b)
  })
})
