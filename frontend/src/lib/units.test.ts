import { describe, expect, it } from 'vitest'
import { fromDisplayDistance, toDisplayDistance } from '@/lib/units'

describe('toDisplayDistance and fromDisplayDistance', () => {
  it('leave metres alone and convert to feet and back', () => {
    expect(toDisplayDistance(10, false)).toBe(10)
    expect(toDisplayDistance(10, true)).toBeCloseTo(32.8084, 6)
    expect(fromDisplayDistance(10, false)).toBe(10)
    expect(fromDisplayDistance(toDisplayDistance(10, true), true)).toBeCloseTo(10, 9)
  })
})
