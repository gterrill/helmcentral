import { describe, expect, test } from 'vitest'

import {
  CLUSTER_FUEL_MIN_W,
  CLUSTER_WIDGET_CONSTRAINTS,
  GRID_MARGIN,
  GRID_ROW_HEIGHT,
  POI_MAP_SPLIT_MIN_W,
  POI_MAP_WIDGET_CONSTRAINTS,
  WIDGET_CONSTRAINTS,
  gridPixelHeight,
} from '@/components/dashboard-bento-grid'
import { NARROW_STRIP_FOLD_PX } from '@/lib/displays'

/**
 * A widget's minimum row count is the only thing standing between an operator
 * and a tile they cannot shrink, and it is a number written by hand next to a
 * comment describing a canvas size. The engine cluster's said "460x300" and
 * outlived two rebuilds of the cluster: the canvas came out at 228px tall and
 * the floor stayed at nine rows, so the tile could not be resized down at all.
 */
describe('grid row maths', () => {
  test('counts the margins between rows, not just the rows', () => {
    expect(gridPixelHeight(1)).toBe(GRID_ROW_HEIGHT)
    expect(gridPixelHeight(2)).toBe(2 * GRID_ROW_HEIGHT + GRID_MARGIN)
    expect(gridPixelHeight(9)).toBe(9 * GRID_ROW_HEIGHT + 8 * GRID_MARGIN)
  })
})

describe('the engine cluster resize floor', () => {
  /**
   * Measured in the browser at scale 1, which is the tallest the tile ever
   * gets: useFitScale only ever shrinks the canvas, so a wider column costs no
   * extra height. 32px of tile header and top padding, 228.4px of canvas,
   * 8px of bottom padding.
   */
  const MEASURED_TILE_HEIGHT = 32 + 228.4 + 8

  test('holds the whole tile', () => {
    expect(gridPixelHeight(CLUSTER_WIDGET_CONSTRAINTS.minH))
      .toBeGreaterThanOrEqual(MEASURED_TILE_HEIGHT)
  })

  test('holds it with no more than a row to spare', () => {
    const slack = gridPixelHeight(CLUSTER_WIDGET_CONSTRAINTS.minH) - MEASURED_TILE_HEIGHT
    expect(slack).toBeLessThan(GRID_ROW_HEIGHT + GRID_MARGIN)
  })
})

/**
 * A fuel rail widens the design the canvas scales against by a fifth, so the
 * floor that was fine for a bare cluster leaves a railed one scaled to nothing.
 * The height is untouched, since the rail is exactly as tall as the body.
 */
describe('the fuel rail', () => {
  test('costs the cluster no extra rows', () => {
    expect(gridPixelHeight(CLUSTER_WIDGET_CONSTRAINTS.minH))
      .toBeGreaterThanOrEqual(32 + 228.4 + 8)
  })

  test('needs a wider column than a bare cluster', () => {
    expect(CLUSTER_FUEL_MIN_W).toBeGreaterThan(CLUSTER_WIDGET_CONSTRAINTS.minW)
  })
})

/**
 * The four wall-display tiles (ADR 0092). Each minH is small enough to fit
 * inside the narrow strip's seven-row, 344px fold budget on its own - a tile sized
 * past that could never appear on the wall at all, only ever on the ordinary
 * dashboard, which would defeat the point of building it for the wall.
 */
describe('the wall-display tiles', () => {
  test.each([
    ['clock', { minW: 2, minH: 6 }],
    ['current-conditions', { minW: 3, minH: 6 }],
    ['forecast-days', { minW: 3, minH: 4 }],
    ['sea-state', { minW: 4, minH: 5 }],
  ] as const)('%s has the constraint the wall-display layout was authored against', (id, expected) => {
    expect(WIDGET_CONSTRAINTS[id]).toEqual(expected)
  })

  test.each(['clock', 'current-conditions', 'forecast-days', 'sea-state'] as const)(
    '%s fits inside the narrow-strip fold on its own',
    (id) => {
      const minH = WIDGET_CONSTRAINTS[id]!.minH!
      expect(gridPixelHeight(minH)).toBeLessThanOrEqual(NARROW_STRIP_FOLD_PX)
    },
  )
})

/**
 * The Nearby widget (ADR 0091 phase 3b). Split layout adds a ranked list
 * beside the map, so it needs a wider floor than the bare map-only layout —
 * the same reasoning the cluster fuel rail's own wider floor follows.
 */
describe('the poi-map widget', () => {
  test('has a floor small enough for map-only, small AIS/POI markers still readable', () => {
    expect(POI_MAP_WIDGET_CONSTRAINTS).toEqual({ minW: 4, minH: 6 })
  })

  test('the split-layout floor is wider than the bare map floor', () => {
    expect(POI_MAP_SPLIT_MIN_W).toBeGreaterThan(POI_MAP_WIDGET_CONSTRAINTS.minW)
  })
})
