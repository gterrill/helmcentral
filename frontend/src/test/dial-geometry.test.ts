import { describe, expect, test } from 'vitest'

import { markingAt, polar, scaleTicks, sweepMapper, sweepStart } from '@/lib/dial-geometry'

describe('dial geometry shared by every ticked dial', () => {
  test('marks a position only inside a non-normal zone', () => {
    const zones = [{ from: 0, to: 2600, state: 'normal' as const }, { from: 2600, to: 3000, state: 'alarm' as const }]
    expect(markingAt(1000, zones)).toBeNull()
    expect(markingAt(2800, zones)).toBe('alarm')
    expect(markingAt(2800, undefined)).toBeNull()
  })

  test('a 250 degree sweep leaves its gap centred at the bottom', () => {
    expect(sweepStart(250)).toBe(145)
    const { angleFor } = sweepMapper(0, 3000, 250)
    expect(angleFor(0)).toBe(145)
    expect(angleFor(3000)).toBe(395)
  })

  test('maps a value to a clamped fraction', () => {
    const { fractionFor } = sweepMapper(0, 100, 250)
    expect([fractionFor(-5), fractionFor(50), fractionFor(150)]).toEqual([0, 0.5, 1])
  })

  test('places points clockwise from three o\'clock', () => {
    const [x, y] = polar(100, 100, 90, 10)
    expect(x).toBeCloseTo(100)
    expect(y).toBeCloseTo(110)
  })

  test('ticks a 0..3000 scale every 500 with five minors between', () => {
    const { majors, minors } = scaleTicks(0, 3000, 500, 5)
    expect(majors).toHaveLength(7)
    expect(minors).toHaveLength(6 * 4)
  })
})
