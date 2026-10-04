import { describe, expect, test } from 'vitest'

import {
  CLUSTER_FUEL_MIN_W,
  CLUSTER_WIDGET_CONSTRAINTS,
  GRID_MARGIN,
  GRID_ROW_HEIGHT,
  POI_MAP_SPLIT_MIN_W,
  POI_MAP_WIDGET_CONSTRAINTS,
  WIDGET_CONSTRAINTS,
  gaugeWidgetConstraints,
  raiseGaugeHeight,
  gridPixelHeight,
} from '@/components/dashboard-bento-grid'
import { NARROW_STRIP_FOLD_PX } from '@/lib/displays'
import { TILE_INSET_H, rowsForHeight } from '@/lib/grid-metrics'

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
   * Measured in the browser at scale 1, which is the shortest the tile can
   * be: the canvas only grows past this, so a wider column costs no extra
   * height. 32px of tile header and top padding, 228.4px of canvas,
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
 * The three wall-display tiles (ADR 0092, ADR 0125 - forecast-days and
 * sea-state merged into forecast-conditions). Each minH is small enough to
 * fit inside the narrow strip's seven-row, 344px fold budget on its own - a
 * tile sized past that could never appear on the wall at all, only ever on
 * the ordinary dashboard, which would defeat the point of building it for
 * the wall.
 */
describe('the wall-display tiles', () => {
  test.each([
    ['clock', { minW: 2, minH: 6 }],
    ['current-conditions', { minW: 3, minH: 6 }],
    ['forecast-conditions', { minW: 4, minH: 6 }],
  ] as const)('%s has the constraint the wall-display layout was authored against', (id, expected) => {
    expect(WIDGET_CONSTRAINTS[id]).toEqual(expected)
  })

  test.each(['clock', 'current-conditions', 'forecast-conditions'] as const)(
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

/**
 * A standalone gauge tile used to be held at four rows whatever it showed. The
 * short displays are one readout tall: 8px tile inset, 24px card padding-top
 * and 8px card padding-bottom, plus the readout. A standalone gauge has no
 * inner frame, so nothing else adds to that. Radial and trend gauges draw a
 * dial or a chart and do need the four rows.
 */
describe('the standalone gauge resize floor', () => {
  const CHROME_H = TILE_INSET_H + 24 + 8
  const NUMERIC_H = CHROME_H + 36 // the 2.25rem floor of the readout, leading-none
  const BAR_H = NUMERIC_H + 8 + 8 // mt-2 and the 8px bar

  test.each([
    ['numeric', NUMERIC_H, 2],
    ['lamp', NUMERIC_H, 2],
    ['bar', BAR_H, 3],
  ] as const)('a %s gauge gets %i rows and they hold it', (display, measured, rows) => {
    const { minW, minH } = gaugeWidgetConstraints(display)
    expect(minW).toBe(2)
    expect(minH).toBe(rows)
    expect(gridPixelHeight(minH)).toBeGreaterThanOrEqual(measured)
    expect(rowsForHeight(measured)).toBe(rows)
    expect(gridPixelHeight(minH) - measured).toBeLessThan(GRID_ROW_HEIGHT + GRID_MARGIN)
  })

  test('a gauge with no display is drawn as numeric, so it gets two rows', () => {
    expect(gaugeWidgetConstraints(undefined)).toEqual({ minW: 2, minH: 2 })
    expect(gaugeWidgetConstraints('mystery' as never)).toEqual({ minW: 2, minH: 2 })
  })

  test.each(['radial', 'trend'] as const)('a %s gauge keeps four rows', (display) => {
    expect(gaugeWidgetConstraints(display)).toEqual({ minW: 2, minH: 4 })
  })
})

describe('raiseGaugeHeight', () => {
  test.each([
    ['radial', 2, 4], ['trend', 3, 4], ['bar', 2, 3], ['numeric', 2, 2], ['lamp', 1, 2],
  ] as const)('a %s gauge at h=%i is raised to %i', (display, h, expected) => {
    expect(raiseGaugeHeight(h, display)).toBe(expected)
  })

  test('never lowers a taller tile', () => {
    expect(raiseGaugeHeight(6, 'numeric')).toBe(6)
    expect(raiseGaugeHeight(5, 'radial')).toBe(5)
  })
})
