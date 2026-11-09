import { describe, expect, it } from 'vitest'
import { cellRing, localToLonLat, parseSoundedCells, soundedAreaGeoJSON, type SoundedFrame } from '@/lib/sounded-cells'

const frame: SoundedFrame = { originLat: -25.29, originLon: 152.91, cellM: 2.5 }

function inRing(ring: number[][], x: number, y: number): boolean {
  let inside = false
  for (let a = 0, b = ring.length - 1; a < ring.length; b = a++) {
    const [xa, ya] = ring[a]
    const [xb, yb] = ring[b]
    if (ya > y !== yb > y && x < ((xb - xa) * (y - ya)) / (yb - ya) + xa) inside = !inside
  }
  return inside
}

function covered(area: GeoJSON.Feature<GeoJSON.MultiPolygon>, eastM: number, northM: number): boolean {
  const [lon, lat] = localToLonLat(frame, eastM, northM)
  return area.geometry.coordinates.some((poly) => inRing(poly[0], lon, lat))
}

describe('cellRing', () => {
  it('puts cell (2, -3) 5 to 7.5 m east and 7.5 to 5 m south of the anchor', () => {
    const ring = cellRing(frame, [2, -3])
    expect(ring).toHaveLength(5)
    expect(ring[0]).toEqual(ring[4])
    const lats = ring.map((p) => p[1])
    expect((Math.min(...lats) - frame.originLat) * 111320).toBeCloseTo(-7.5, 6)
    expect((Math.max(...lats) - frame.originLat) * 111320).toBeCloseTo(-5, 6)
    const lons = ring.map((p) => p[0])
    const east = (lon: number) => (lon - frame.originLon) * 111320 * Math.cos((frame.originLat * Math.PI) / 180)
    expect(east(Math.min(...lons))).toBeCloseTo(5, 6)
    expect(east(Math.max(...lons))).toBeCloseTo(7.5, 6)
  })
})

describe('soundedAreaGeoJSON', () => {
  it('tints nothing when nothing is sounded', () => {
    const area = soundedAreaGeoJSON(frame, [], 30)
    expect(area.geometry.coordinates).toHaveLength(0)
  })

  it('tints the sounded cells and leaves the rest alone', () => {
    const cells = [
      [0, 0],
      [1, 0],
      [-1, 3],
    ] as const
    const area = soundedAreaGeoJSON(frame, cells, 30)
    expect(covered(area, 1.25, 1.25)).toBe(true) // cell (0,0)
    expect(covered(area, 3.75, 1.25)).toBe(true) // cell (1,0)
    expect(covered(area, -1.25, 8.75)).toBe(true) // cell (-1,3)
    expect(covered(area, 6.25, 1.25)).toBe(false) // cell (2,0) next to the sounded run
    expect(covered(area, 1.25, 3.75)).toBe(false) // the row above
    expect(covered(area, -10, -10)).toBe(false)
  })

  it('cuts a sounded cell at the circle edge without tinting outside it', () => {
    const area = soundedAreaGeoJSON(frame, [[11, 0]], 28) // east 27.5-30 m, mostly outside
    expect(covered(area, 27.7, 0.1)).toBe(true) // inside the circle
    expect(covered(area, 29, 1)).toBe(false) // in the cell, outside the circle
    const inside = soundedAreaGeoJSON(frame, [[10, 0]], 28) // east 25-27.5 m, inside
    expect(covered(inside, 26, 1)).toBe(true)
    expect(covered(inside, 40, 0)).toBe(false)
  })

  it('joins a run of crossed cells into one polygon', () => {
    const area = soundedAreaGeoJSON(frame, [[0, 0], [1, 0], [2, 0]], 30)
    expect(area.geometry.coordinates).toHaveLength(1)
  })
})

describe('seams', () => {
  it('gives neighbouring blocks the same vertices along the edge they share', () => {
    const area = soundedAreaGeoJSON(frame, [[0, 0], [0, 1], [1, 1]], 30)
    const want = localToLonLat(frame, 2.5, 2.5)
    const owners = area.geometry.coordinates.filter((poly) => poly[0].some(([x, y]) => x === want[0] && y === want[1]))
    // The lower block's corner is a vertex of the wider block above it too.
    expect(owners.length).toBeGreaterThanOrEqual(2)
  })
})

describe('filling small gaps', () => {
  it('fills a one-cell hole between crossed cells on opposite sides', () => {
    const ring = [[-1, 0], [1, 0]] as const // east and west of cell (0,0)
    expect(covered(soundedAreaGeoJSON(frame, ring, 30), 1.25, 1.25)).toBe(true)
    const ns = [[0, -1], [0, 1]] as const
    expect(covered(soundedAreaGeoJSON(frame, ns, 30), 1.25, 1.25)).toBe(true)
    const diag = [[-1, -1], [1, 1]] as const
    expect(covered(soundedAreaGeoJSON(frame, diag, 30), 1.25, 1.25)).toBe(true)
    const diag2 = [[-1, 1], [1, -1]] as const
    expect(covered(soundedAreaGeoJSON(frame, diag2, 30), 1.25, 1.25)).toBe(true)
  })

  it('does not grow the band beyond the holes it fills', () => {
    const area = soundedAreaGeoJSON(frame, [[0, 0]], 30)
    expect(covered(area, 1.25, 1.25)).toBe(true)
    expect(covered(area, 3.75, 1.25)).toBe(false)
    expect(covered(area, -1.25, 1.25)).toBe(false)
    expect(covered(area, 3.75, 3.75)).toBe(false)
    const pair = soundedAreaGeoJSON(frame, [[0, 0], [1, 0]], 30)
    expect(covered(pair, 6.25, 1.25)).toBe(false)
    expect(covered(pair, -1.25, 1.25)).toBe(false)
  })
})

describe('parseSoundedCells', () => {
  it('reads the three statuses', () => {
    expect(parseSoundedCells({ status: 'no_watch' })).toEqual({ status: 'no_watch' })
    expect(parseSoundedCells({ status: 'not_ready' })).toEqual({ status: 'not_ready' })
    expect(parseSoundedCells({ status: 'ready', cell_m: 2.5, origin_lat: 1, origin_lon: 2, cells: [[1, 2]] })).toEqual({
      status: 'ready',
      frame: { originLat: 1, originLon: 2, cellM: 2.5 },
      cells: [[1, 2]],
    })
  })
  it('fails on a shape the server never sends', () => {
    expect(() => parseSoundedCells({ status: 'maybe' })).toThrow()
    expect(() => parseSoundedCells({ status: 'ready', cells: [] })).toThrow()
    expect(() => parseSoundedCells({ status: 'ready', cell_m: 2.5, origin_lat: 1, origin_lon: 2, cells: [[1.5, 2]] })).toThrow()
  })
})
