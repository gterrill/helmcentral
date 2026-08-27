import { describe, expect, test } from 'vitest'

import {
  CLUSTER_FUEL_MIN_W,
  CLUSTER_WIDGET_CONSTRAINTS,
  GRID_MARGIN,
  GRID_ROW_HEIGHT,
  gridPixelHeight,
} from '@/components/dashboard-bento-grid'

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
   * extra height. 54px of tile header and top padding, 228.4px of canvas,
   * 16px of bottom padding.
   */
  const MEASURED_TILE_HEIGHT = 54 + 228.4 + 16

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
      .toBeGreaterThanOrEqual(54 + 228.4 + 16)
  })

  test('needs a wider column than a bare cluster', () => {
    expect(CLUSTER_FUEL_MIN_W).toBeGreaterThan(CLUSTER_WIDGET_CONSTRAINTS.minW)
  })
})
