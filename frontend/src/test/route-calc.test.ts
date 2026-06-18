import { describe, it, expect } from 'vitest'
import { calculateLegs, calculateRouteTotals } from '@/lib/route-calc'

describe('calculateLegs', () => {
  it('returns no legs for a single waypoint', () => {
    expect(calculateLegs([{ lat: 0, lon: 0 }], 6)).toHaveLength(0)
  })

  it('computes distance, bearing, and ETA for each leg', () => {
    const legs = calculateLegs(
      [
        { lat: 0, lon: 0 },
        { lat: 1, lon: 0 },
        { lat: 1, lon: 1 },
      ],
      6,
    )

    expect(legs).toHaveLength(2)
    expect(legs[0].fromIndex).toBe(0)
    expect(legs[0].toIndex).toBe(1)
    expect(legs[0].bearingDeg).toBeCloseTo(0, 1)
    expect(legs[0].distanceM).toBeGreaterThan(111000)
    expect(legs[0].etaHours).toBeGreaterThan(0)
    expect(legs[1].bearingDeg).toBeCloseTo(90, 1)
  })

  it('returns Infinity ETA when speed is zero', () => {
    const legs = calculateLegs([{ lat: 0, lon: 0 }, { lat: 1, lon: 0 }], 0)
    expect(legs[0].etaHours).toBe(Infinity)
  })
})

describe('calculateRouteTotals', () => {
  it('sums distance and ETA across legs', () => {
    const legs = calculateLegs(
      [
        { lat: 0, lon: 0 },
        { lat: 1, lon: 0 },
        { lat: 2, lon: 0 },
      ],
      6,
    )
    const totals = calculateRouteTotals(legs)

    expect(totals.totalDistanceM).toBeCloseTo(legs[0].distanceM + legs[1].distanceM, 0)
    expect(totals.totalEtaHours).toBeCloseTo(legs[0].etaHours + legs[1].etaHours, 5)
  })

  it('returns zero totals for no legs', () => {
    expect(calculateRouteTotals([])).toEqual({ totalDistanceM: 0, totalEtaHours: 0 })
  })
})
