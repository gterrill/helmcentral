import { describe, it, expect } from 'vitest'
import { haversineMeters, bearingDeg, destinationPoint } from '@/lib/geo'

describe('haversineMeters', () => {
  it('returns ~111.2km for one degree of latitude', () => {
    const distance = haversineMeters(0, 0, 1, 0)
    expect(distance).toBeGreaterThan(111000)
    expect(distance).toBeLessThan(111300)
  })

  it('returns 0 for identical points', () => {
    expect(haversineMeters(45, -73, 45, -73)).toBe(0)
  })
})

describe('bearingDeg', () => {
  it('returns 0 for due north', () => {
    expect(bearingDeg(0, 0, 1, 0)).toBeCloseTo(0, 1)
  })

  it('returns 90 for due east', () => {
    expect(bearingDeg(0, 0, 0, 1)).toBeCloseTo(90, 1)
  })

  it('returns 180 for due south', () => {
    expect(bearingDeg(0, 0, -1, 0)).toBeCloseTo(180, 1)
  })

  it('returns 270 for due west', () => {
    expect(bearingDeg(0, 0, 0, -1)).toBeCloseTo(270, 1)
  })
})

describe('destinationPoint', () => {
  it('round-trips with haversineMeters and bearingDeg', () => {
    const [lat, lon] = destinationPoint(37.8, -122.4, Math.PI / 2, 5000)
    const distance = haversineMeters(37.8, -122.4, lat, lon)
    const bearing = bearingDeg(37.8, -122.4, lat, lon)
    expect(distance).toBeCloseTo(5000, 0)
    expect(bearing).toBeCloseTo(90, 0)
  })
})
