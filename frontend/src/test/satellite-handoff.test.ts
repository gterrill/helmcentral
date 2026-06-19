import { describe, expect, it } from 'vitest'
import { computeWorldImageryOpacity } from '@/components/anchor-watch-map'

describe('world imagery zoom fade-in', () => {
  it('is fully opaque at and above the handoff end zoom', () => {
    expect(computeWorldImageryOpacity(10, true)).toBe(1)
    expect(computeWorldImageryOpacity(14, true)).toBe(1)
  })

  it('is fully transparent at and below the handoff start zoom', () => {
    expect(computeWorldImageryOpacity(9, true)).toBe(0)
    expect(computeWorldImageryOpacity(2, true)).toBe(0)
  })

  it('fades linearly between the handoff zooms', () => {
    expect(computeWorldImageryOpacity(9.5, true)).toBeCloseTo(0.5, 5)
  })

  it('returns zero when imagery is disabled regardless of zoom', () => {
    expect(computeWorldImageryOpacity(14, false)).toBe(0)
  })
})
