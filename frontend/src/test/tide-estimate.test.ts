import { describe, expect, it } from 'vitest'
import { estimateDepthAtNextTurn, nextTideExtreme, tideExtremesByTime } from '@/lib/tide-estimate'
import type { TideToday } from '@/hooks/use-tide-today'

const baseTide: TideToday = {
  datetime: new Date().toISOString(),
  current_tide_height_ft: 3,
  tide_direction: 'Falling',
  high_tide_time: new Date(Date.now() - 3600_000).toISOString(),
  high_tide_height_ft: 5,
  low_tide_time: new Date(Date.now() + 3600_000).toISOString(),
  low_tide_height_ft: 1,
  station_name: 'Test Station',
  provider: 'test',
}

describe('tideExtremesByTime', () => {
  it('sorts high and low chronologically', () => {
    const extremes = tideExtremesByTime(baseTide)
    expect(extremes[0].isHigh).toBe(true) // high_tide_time is earlier in baseTide
    expect(extremes[1].isHigh).toBe(false)
  })

  it('reorders when the low sorts before the high', () => {
    const swapped: TideToday = {
      ...baseTide,
      high_tide_time: new Date(Date.now() + 3600_000).toISOString(),
      low_tide_time: new Date(Date.now() - 3600_000).toISOString(),
    }
    const extremes = tideExtremesByTime(swapped)
    expect(extremes[0].isHigh).toBe(false)
    expect(extremes[1].isHigh).toBe(true)
  })
})

describe('nextTideExtreme', () => {
  it('is whichever of high/low sorts first', () => {
    expect(nextTideExtreme(baseTide)?.isHigh).toBe(true)
  })
})

// Code-review finding: tideToday now sends the -1 height sentinel alongside
// an empty time string for an extreme the provider has none of - the old
// behaviour (a fabricated "now"/"tomorrow" time with the -1 height) put a
// placeholder straight into this list, which the Depth & Tide tile and the
// Anchor Watch header both then rendered as if it were real ("Low
// <tomorrow>", a fake "High · <now>"). Both helpers now drop a missing
// extreme instead of passing it through.
describe('tideExtremesByTime / nextTideExtreme — missing extremes are dropped', () => {
  it('drops an extreme whose height is the -1 sentinel', () => {
    const tide: TideToday = { ...baseTide, low_tide_height_ft: -1, low_tide_time: '' }
    const extremes = tideExtremesByTime(tide)
    expect(extremes).toHaveLength(1)
    expect(extremes[0].isHigh).toBe(true)
  })

  it('drops an extreme whose time is an empty string even if its height looks real', () => {
    const tide: TideToday = { ...baseTide, low_tide_time: '' }
    const extremes = tideExtremesByTime(tide)
    expect(extremes).toHaveLength(1)
    expect(extremes[0].isHigh).toBe(true)
  })

  it('drops an extreme whose time is unparseable', () => {
    const tide: TideToday = { ...baseTide, high_tide_time: 'not a date' }
    const extremes = tideExtremesByTime(tide)
    expect(extremes).toHaveLength(1)
    expect(extremes[0].isHigh).toBe(false)
  })

  it('keeps a real extreme below -1 ft rather than dropping it (not the sentinel)', () => {
    const tide: TideToday = { ...baseTide, low_tide_height_ft: -1.4 }
    const extremes = tideExtremesByTime(tide)
    expect(extremes).toHaveLength(2)
  })

  it('returns an empty list when both extremes are missing', () => {
    const tide: TideToday = { ...baseTide, high_tide_time: '', high_tide_height_ft: -1, low_tide_time: '', low_tide_height_ft: -1 }
    expect(tideExtremesByTime(tide)).toHaveLength(0)
  })

  it('nextTideExtreme is null when both extremes are missing', () => {
    const tide: TideToday = { ...baseTide, high_tide_time: '', high_tide_height_ft: -1, low_tide_time: '', low_tide_height_ft: -1 }
    expect(nextTideExtreme(tide)).toBeNull()
  })

  it('nextTideExtreme is the one real extreme when the other is missing', () => {
    const tide: TideToday = { ...baseTide, low_tide_time: '', low_tide_height_ft: -1 }
    expect(nextTideExtreme(tide)?.isHigh).toBe(true)
  })
})

describe('estimateDepthAtNextTurn', () => {
  it('projects to the next low when falling, in metres', () => {
    // drop from current (3 ft) to low (1 ft) = 2 ft = 0.6096 m; 4 - 0.6096 = 3.3904 m
    expect(estimateDepthAtNextTurn(4, baseTide)).toBeCloseTo(3.3904, 4)
  })

  it('projects to the next high when rising, in metres', () => {
    const rising: TideToday = { ...baseTide, tide_direction: 'Rising' }
    // rise from current (3 ft) to high (5 ft) = 2 ft = 0.6096 m; 4 + 0.6096 = 4.6096 m
    expect(estimateDepthAtNextTurn(4, rising)).toBeCloseTo(4.6096, 4)
  })

  it('returns null with no live depth', () => {
    expect(estimateDepthAtNextTurn(null, baseTide)).toBeNull()
  })

  it('returns null when the current tide height is the -1 sentinel', () => {
    expect(estimateDepthAtNextTurn(4, { ...baseTide, current_tide_height_ft: -1 })).toBeNull()
  })

  it('returns null when the falling target (low) height is the -1 sentinel', () => {
    expect(estimateDepthAtNextTurn(4, { ...baseTide, low_tide_height_ft: -1 })).toBeNull()
  })

  it('returns null when the rising target (high) height is the -1 sentinel', () => {
    const rising: TideToday = { ...baseTide, tide_direction: 'Rising', high_tide_height_ft: -1 }
    expect(estimateDepthAtNextTurn(4, rising)).toBeNull()
  })

  // Code-review finding: the sentinel is exactly -1, not "any negative
  // number" - a real low below chart datum (e.g. -0.4 ft) is legitimate
  // data and must still produce an estimate, not null.
  it('projects to a negative next low, in metres, rather than treating it as missing', () => {
    const falling: TideToday = { ...baseTide, current_tide_height_ft: 3, low_tide_height_ft: -0.4 }
    // drop from current (3 ft) to low (-0.4 ft) = 3.4 ft = 1.036329... m
    expect(estimateDepthAtNextTurn(4, falling)).toBeCloseTo(4 - 3.4 / 3.28084, 4)
  })

  it('projects from a negative current tide height, rather than treating it as missing', () => {
    const falling: TideToday = { ...baseTide, current_tide_height_ft: -0.4, low_tide_height_ft: -0.9 }
    // drop from current (-0.4 ft) to low (-0.9 ft) = 0.5 ft
    expect(estimateDepthAtNextTurn(4, falling)).toBeCloseTo(4 - 0.5 / 3.28084, 4)
  })
})
