import { describe, expect, it } from 'vitest'

import { sunPosition } from '@/lib/sun'

describe('sunPosition', () => {
  it('matches the published SunCalc reference: Kyiv, 5 March 2013 at 00:00 UTC', () => {
    // SunCalc's own test values: altitude -0.7000406838781611 rad, azimuth
    // -2.5003175907168385 rad measured from the south.
    const p = sunPosition(Date.UTC(2013, 2, 5), 50.5, 30.5)
    expect(p.altitudeDeg).toBeCloseTo((-0.7000406838781611 * 180) / Math.PI, 3)
    expect(p.azimuthDeg).toBeCloseTo((((-2.5003175907168385 + Math.PI) * 180) / Math.PI), 3)
  })

  it('stands nearly overhead at solar noon on the equator at the March equinox', () => {
    const p = sunPosition(Date.UTC(2024, 2, 20, 12, 0), 0, 0)
    expect(p.altitudeDeg).toBeGreaterThan(85)
  })

  it('rises in the east and sets in the west on the equator', () => {
    const rise = sunPosition(Date.UTC(2024, 2, 20, 6, 0), 0, 0)
    const set = sunPosition(Date.UTC(2024, 2, 20, 18, 0), 0, 0)
    expect(Math.abs(rise.azimuthDeg - 90)).toBeLessThan(5)
    expect(Math.abs(set.azimuthDeg - 270)).toBeLessThan(5)
    expect(Math.abs(rise.altitudeDeg)).toBeLessThan(5)
  })

  it('is below the horizon at local midnight and above it at local noon, in the boat\'s longitude', () => {
    // Fiji, 178 E: local noon is about 00:10 UTC.
    expect(sunPosition(Date.UTC(2024, 5, 21, 0, 10), -17, 178).altitudeDeg).toBeGreaterThan(40)
    expect(sunPosition(Date.UTC(2024, 5, 21, 12, 10), -17, 178).altitudeDeg).toBeLessThan(-30)
  })

  it('faces north at noon for a boat south of the tropics in winter, and south in the northern summer', () => {
    // Southern hemisphere, June: the sun is to the north.
    const south = sunPosition(Date.UTC(2024, 5, 21, 0, 10), -35, 178)
    expect(Math.abs(south.azimuthDeg - 0) < 10 || Math.abs(south.azimuthDeg - 360) < 10).toBe(true)
    const north = sunPosition(Date.UTC(2024, 5, 21, 12, 0), 50, 0)
    expect(Math.abs(north.azimuthDeg - 180)).toBeLessThan(10)
  })
})
