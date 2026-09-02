import { describe, expect, it } from 'vitest'

import { haversineMeters } from '@/lib/geo'
import { buildEchoLut, echoCornerCoordinates, getEchoLut } from '@/lib/radar-echo/geometry'

// Assert a haversine distance lands within half a metre of an expected
// value, matching the tolerance the plan calls out for the corner maths.
function expectMetresCloseTo(actualM: number, expectedM: number): void {
  expect(Math.abs(actualM - expectedM)).toBeLessThan(0.5)
}

describe('buildEchoLut', () => {
  // Odd sizePx so there is an exact centre pixel; spokeCount divisible by 4
  // so north/east/south/west land on exact rows rather than a rounded one.
  const sizePx = 65
  const spokeCount = 16
  const binCount = 8
  const lut = buildEchoLut(sizePx, spokeCount, binCount)
  const centre = (sizePx - 1) / 2

  function pixelIndex(px: number, py: number): number {
    return py * sizePx + px
  }

  it('reads the centre pixel as range bin 0', () => {
    const i = pixelIndex(centre, centre)
    expect(lut.inside[i]).toBe(1)
    expect(lut.rangeBin[i]).toBe(0)
  })

  it('reads directly above centre (north, up) as spoke 0', () => {
    const i = pixelIndex(centre, 0)
    expect(lut.inside[i]).toBe(1)
    expect(lut.spokeIndex[i]).toBe(0)
  })

  it('reads directly right of centre (east) as spoke spokeCount/4', () => {
    const i = pixelIndex(sizePx - 1, centre)
    expect(lut.inside[i]).toBe(1)
    expect(lut.spokeIndex[i]).toBe(spokeCount / 4)
  })

  it('reads directly below centre (south) as spoke spokeCount/2', () => {
    const i = pixelIndex(centre, sizePx - 1)
    expect(lut.inside[i]).toBe(1)
    expect(lut.spokeIndex[i]).toBe(spokeCount / 2)
  })

  it('marks a corner pixel outside the inscribed circle', () => {
    const i = pixelIndex(0, 0)
    expect(lut.inside[i]).toBe(0)
  })

  it('clamps range bins to binCount - 1 rather than overrunning the row', () => {
    for (let i = 0; i < lut.rangeBin.length; i++) {
      expect(lut.rangeBin[i]).toBeLessThan(binCount)
    }
  })
})

describe('getEchoLut', () => {
  it('memoises: identical arguments return the identical object', () => {
    const a = getEchoLut(33, 16, 8)
    const b = getEchoLut(33, 16, 8)
    expect(b).toBe(a)
  })

  it('builds a distinct object when any argument differs', () => {
    const base = getEchoLut(41, 16, 8)
    expect(getEchoLut(43, 16, 8)).not.toBe(base)
    expect(getEchoLut(41, 32, 8)).not.toBe(base)
    expect(getEchoLut(41, 16, 16)).not.toBe(base)
  })
})

describe('echoCornerCoordinates', () => {
  const lat = -20.27
  const lon = 148.77
  const halfSideM = 500

  it('returns NW, NE, SE, SW, each halfSideM from centre along its own axis', () => {
    const [nw, ne, se, sw] = echoCornerCoordinates(lat, lon, halfSideM)

    // NW = [lonW, latN]: north offset in latitude, west offset in longitude.
    expectMetresCloseTo(haversineMeters(lat, lon, nw[1], lon), halfSideM)
    expectMetresCloseTo(haversineMeters(lat, lon, lat, nw[0]), halfSideM)

    // NE = [lonE, latN]: shares its north latitude with NW.
    expect(ne[1]).toBe(nw[1])
    expectMetresCloseTo(haversineMeters(lat, lon, lat, ne[0]), halfSideM)

    // SE = [lonE, latS]: shares its east longitude with NE.
    expect(se[0]).toBe(ne[0])
    expectMetresCloseTo(haversineMeters(lat, lon, se[1], lon), halfSideM)

    // SW = [lonW, latS]: shares longitude with NW and latitude with SE.
    expect(sw[0]).toBe(nw[0])
    expect(sw[1]).toBe(se[1])
  })

  it('places NW/NE north of and SE/SW south of centre', () => {
    const [nw, ne, se, sw] = echoCornerCoordinates(lat, lon, halfSideM)
    expect(nw[1]).toBeGreaterThan(lat)
    expect(ne[1]).toBeGreaterThan(lat)
    expect(se[1]).toBeLessThan(lat)
    expect(sw[1]).toBeLessThan(lat)
  })

  it('places NE/SE east of and NW/SW west of centre', () => {
    const [nw, ne, se, sw] = echoCornerCoordinates(lat, lon, halfSideM)
    expect(ne[0]).toBeGreaterThan(lon)
    expect(se[0]).toBeGreaterThan(lon)
    expect(nw[0]).toBeLessThan(lon)
    expect(sw[0]).toBeLessThan(lon)
  })
})
