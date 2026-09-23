import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, test } from 'vitest'

import {
  POI_CATEGORIES,
  POI_CATEGORY_IDS,
  POI_MAP_MAX_ZOOM,
  POI_MAP_MIN_ZOOM,
  fitCameraToPoints,
  formatBearing,
  poiCategoryById,
  topPoi,
  zoomForRangeNm,
  type PoiFeature,
} from '@/lib/poi'

/**
 * Go's JSON decoder and validatePoiMapWidget both key off poiCategoryIDs in
 * backend/poi_providers.go. A category id present in one catalog but not the
 * other is a 400 an operator only discovers by trying to save the widget.
 * Reads the backend source directly off disk (the same technique
 * dashboard-widgets.test.ts uses for validDashboardWidgetIDs) so the two
 * lists can never drift apart without failing the suite immediately.
 */
describe('POI_CATEGORIES backend parity', () => {
  test('matches backend/poi_providers.go poiCategoryIDs exactly, in order', () => {
    const testDir = dirname(fileURLToPath(import.meta.url))
    const source = readFileSync(resolve(testDir, '../../../backend/poi_providers.go'), 'utf8')

    const start = source.indexOf('var poiCategoryIDs = []string{')
    expect(start, 'poiCategoryIDs not found in backend/poi_providers.go').toBeGreaterThan(-1)
    const end = source.indexOf('}', start)
    const block = source.slice(start, end)

    const backendIds = [...block.matchAll(/"([a-z-]+)"/g)].map((m) => m[1])

    expect(backendIds.length, 'no category ids parsed out of poiCategoryIDs - regex or shape changed').toBeGreaterThan(0)
    expect(POI_CATEGORY_IDS).toEqual(backendIds)
  })

  test('every category has a distinct id, label and icon', () => {
    expect(new Set(POI_CATEGORIES.map((c) => c.id)).size).toBe(POI_CATEGORIES.length)
    for (const category of POI_CATEGORIES) {
      expect(category.label.trim()).not.toBe('')
      expect(category.icon).toBeDefined()
    }
  })
})

describe('poiCategoryById', () => {
  test('finds a known category', () => {
    expect(poiCategoryById('anchorage')?.label).toBe('Anchorage')
  })

  test('returns undefined for an unknown id', () => {
    expect(poiCategoryById('moon-base')).toBeUndefined()
  })
})

describe('zoomForRangeNm', () => {
  test('clamps to the minimum for a very large range', () => {
    expect(zoomForRangeNm(1000, -25, 400)).toBe(POI_MAP_MIN_ZOOM)
  })

  test('clamps to the maximum for a tiny range', () => {
    expect(zoomForRangeNm(0.01, -25, 400)).toBe(POI_MAP_MAX_ZOOM)
  })

  test('a larger range never zooms in more than a smaller one', () => {
    const near = zoomForRangeNm(2, -25, 400)
    const far = zoomForRangeNm(20, -25, 400)
    expect(far).toBeLessThanOrEqual(near)
  })

  test('a taller map fits the same range at a higher (more zoomed-in) level', () => {
    const short = zoomForRangeNm(5, -25, 200)
    const tall = zoomForRangeNm(5, -25, 800)
    expect(tall).toBeGreaterThan(short)
  })

  test('stays within bounds for an ordinary input', () => {
    const zoom = zoomForRangeNm(5, -25, 400)
    expect(zoom).toBeGreaterThanOrEqual(POI_MAP_MIN_ZOOM)
    expect(zoom).toBeLessThanOrEqual(POI_MAP_MAX_ZOOM)
  })

  test('degenerate inputs fall back to the minimum rather than NaN', () => {
    expect(zoomForRangeNm(0, -25, 400)).toBe(POI_MAP_MIN_ZOOM)
    expect(zoomForRangeNm(5, -25, 0)).toBe(POI_MAP_MIN_ZOOM)
  })
})

describe('fitCameraToPoints', () => {
  test('a single point has nothing to fit against, so it gets the maximum zoom', () => {
    const result = fitCameraToPoints([{ lat: -20.27, lon: 148.94 }], 400, 400, 30)
    expect(result.zoom).toBe(POI_MAP_MAX_ZOOM)
    // The Mercator round trip (lat -> y -> lat) isn't bit-exact.
    expect(result.center.lat).toBeCloseTo(-20.27, 9)
    expect(result.center.lon).toBeCloseTo(148.94, 9)
  })

  test('several coincident points behave the same as a single point', () => {
    const result = fitCameraToPoints(
      [{ lat: -20.27, lon: 148.94 }, { lat: -20.27, lon: 148.94 }],
      400, 400, 30,
    )
    expect(result.zoom).toBe(POI_MAP_MAX_ZOOM)
  })

  test('centres on the midpoint of the bounding box, not an average weighted toward one point', () => {
    // Same latitude, so the Mercator midpoint is exact and easy to check by hand.
    const result = fitCameraToPoints(
      [{ lat: 0, lon: 0 }, { lat: 0, lon: 2 }, { lat: 0, lon: 2 }, { lat: 0, lon: 2 }],
      1000, 1000, 0,
    )
    expect(result.center.lat).toBeCloseTo(0, 6)
    expect(result.center.lon).toBeCloseTo(1, 6)
  })

  test('a wider spread of points never zooms in more than a tighter one', () => {
    const tight = fitCameraToPoints([{ lat: -20.27, lon: 148.94 }, { lat: -20.28, lon: 148.95 }], 400, 400, 20)
    const wide = fitCameraToPoints([{ lat: -20.0, lon: 148.0 }, { lat: -21.0, lon: 150.0 }], 400, 400, 20)
    expect(wide.zoom).toBeLessThanOrEqual(tight.zoom)
  })

  test('a wider viewport never forces a lower zoom for the same points', () => {
    // Spread mostly east-west, so viewport width is the binding constraint.
    const points = [{ lat: -20.27, lon: 148.0 }, { lat: -20.27, lon: 149.0 }]
    const narrow = fitCameraToPoints(points, 300, 800, 20)
    const wide = fitCameraToPoints(points, 900, 800, 20)
    expect(wide.zoom).toBeGreaterThanOrEqual(narrow.zoom)
  })

  test('more padding never zooms in more than less padding, for the same points and viewport', () => {
    const points = [{ lat: -20.27, lon: 148.94 }, { lat: -20.3, lon: 149.0 }]
    const noPadding = fitCameraToPoints(points, 400, 400, 0)
    const padded = fitCameraToPoints(points, 400, 400, 60)
    expect(padded.zoom).toBeLessThanOrEqual(noPadding.zoom)
  })

  test('clamps to the minimum zoom for a very wide spread', () => {
    const result = fitCameraToPoints([{ lat: -40, lon: 100 }, { lat: 10, lon: 170 }], 400, 400, 20)
    expect(result.zoom).toBe(POI_MAP_MIN_ZOOM)
  })

  test('stays within bounds for an ordinary input', () => {
    const points = [{ lat: -20.27, lon: 148.94 }, { lat: -20.3, lon: 149.0 }, { lat: -20.25, lon: 148.9 }]
    const result = fitCameraToPoints(points, 400, 400, 30)
    expect(result.zoom).toBeGreaterThanOrEqual(POI_MAP_MIN_ZOOM)
    expect(result.zoom).toBeLessThanOrEqual(POI_MAP_MAX_ZOOM)
  })
})

function feature(overrides: Partial<PoiFeature>): PoiFeature {
  return {
    id: 'a', category: 'anchorage', name: 'Cid Harbour', lat: -20.27, lon: 148.94,
    distanceM: 1000, bearingDeg: 90, detail: '', sourceUrl: '',
    ...overrides,
  }
}

describe('topPoi', () => {
  test('returns up to n features, already in server order', () => {
    const features = [
      feature({ id: '1', name: 'A' }),
      feature({ id: '2', name: 'B' }),
      feature({ id: '3', name: 'C' }),
    ]
    expect(topPoi(features, 2).map((f) => f.id)).toEqual(['1', '2'])
  })

  test('defaults to five', () => {
    const features = Array.from({ length: 8 }, (_, i) => feature({ id: String(i), name: `F${i}` }))
    expect(topPoi(features)).toHaveLength(5)
  })

  test('collapses a repeated name to one entry, keeping the closer (first) one', () => {
    const features = [
      feature({ id: 'near', name: 'Cid Harbour', distanceM: 500 }),
      feature({ id: 'far', name: 'cid harbour', distanceM: 5000 }),
    ]
    const result = topPoi(features, 5)
    expect(result).toHaveLength(1)
    expect(result[0].id).toBe('near')
  })

  test('never collapses two unnamed features', () => {
    const features = [
      feature({ id: '1', name: '' }),
      feature({ id: '2', name: '' }),
    ]
    expect(topPoi(features, 5)).toHaveLength(2)
  })
})

describe('formatBearing', () => {
  test.each([
    [0, '000° N'],
    [90, '090° E'],
    [180, '180° S'],
    [270, '270° W'],
    [45, '045° NE'],
  ])('formats %d degrees as %s', (deg, expected) => {
    expect(formatBearing(deg)).toBe(expected)
  })

  test('normalises an out-of-range or negative value', () => {
    expect(formatBearing(-10)).toBe('350° N')
    expect(formatBearing(370)).toBe('010° N')
  })
})
