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
    expect(nextTideExtreme(baseTide).isHigh).toBe(true)
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
})
