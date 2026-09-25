import { describe, expect, it } from 'vitest'

import type { TideToday } from '@/hooks/use-tide-today'
import {
  computeLowWaterClearance,
  lowWaterClearanceReasonLabel,
  underKeelPhrase,
  TIDE_STALE_AFTER_MINUTES,
  type LowWaterClearanceInput,
} from '@/lib/low-water-clearance'

// Mirrors the unexported constant of the same name in low-water-clearance.ts
// (and in rode-plan.ts) — used only to build test fixtures in feet from a
// figure in metres, never to duplicate the module's own arithmetic.
const METERS_PER_FOOT = 3.28084

// Fixed instant the fixtures below are built around: the tide reading was
// taken 15 minutes before this (well inside the 30 minute staleness
// threshold) and the next low is still 20 minutes ahead of it (not yet
// passed). Individual tests override datetime/low_tide_time/now to probe
// each edge on its own.
const NOW = new Date('2026-09-26T14:00:00Z')

function makeTide(overrides: Partial<TideToday> = {}): TideToday {
  return {
    datetime: '2026-09-26T13:45:00Z',
    current_tide_height_ft: 3,
    tide_direction: 'Falling',
    high_tide_time: new Date(0).toISOString(),
    high_tide_height_ft: 5,
    low_tide_time: '2026-09-26T14:20:00Z',
    low_tide_height_ft: 1,
    station_name: 'Test Station',
    provider: 'test',
    ...overrides,
  }
}

const baseInput: LowWaterClearanceInput = {
  depthM: 4,
  tide: makeTide(),
  draftM: 1.2,
  marginM: 0.5,
  now: NOW,
}

describe('computeLowWaterClearance', () => {
  it('reports ok when clearance at low water clears the margin', () => {
    // Fall = (3 - 1) / 3.28084 = 0.6096 m. depthAtLow = 4 - 0.6096 = 3.3904.
    // clearance = 3.3904 - 1.2 = 2.1904, well above the 0.5 m margin.
    const result = computeLowWaterClearance(baseInput)
    expect(result.status).toBe('ok')
    if (result.status === 'unknown') throw new Error('expected a resolved result')
    expect(result.depthAtLowM).toBeCloseTo(3.3904, 3)
    expect(result.clearanceM).toBeCloseTo(2.1904, 3)
    expect(result.lowTideTime).toBe('2026-09-26T14:20:00Z')
  })

  it('reports too_shallow when clearance at low water is under the margin', () => {
    const result = computeLowWaterClearance({
      ...baseInput,
      depthM: 2,
      draftM: 1.2,
      marginM: 0.5,
    })
    // Fall = 0.6096 m. depthAtLow = 2 - 0.6096 = 1.3904. clearance = 0.1904,
    // under the 0.5 m margin.
    expect(result.status).toBe('too_shallow')
    if (result.status === 'unknown') throw new Error('expected a resolved result')
    expect(result.clearanceM).toBeCloseTo(0.1904, 3)
  })

  it('treats a clearance exactly at the margin as ok, not too_shallow', () => {
    // Pick depth so clearance lands exactly on marginM: fallM = (3-1)/3.28084.
    const fallM = (3 - 1) / METERS_PER_FOOT
    const marginM = 0.5
    const draftM = 1.2
    const depthM = marginM + draftM + fallM
    const result = computeLowWaterClearance({ ...baseInput, depthM, draftM, marginM })
    expect(result.status).toBe('ok')
    if (result.status === 'unknown') throw new Error('expected a resolved result')
    expect(result.clearanceM).toBeCloseTo(marginM, 6)
  })

  it('clamps fall to zero when the current tide is already below the next low forecast', () => {
    const tide = makeTide({ current_tide_height_ft: 0.5, low_tide_height_ft: 1 })
    const result = computeLowWaterClearance({ ...baseInput, tide, depthM: 4, draftM: 1.2, marginM: 0.5 })
    expect(result.status).toBe('ok')
    if (result.status === 'unknown') throw new Error('expected a resolved result')
    // No fall applied: depthAtLowM stays at the live depth.
    expect(result.depthAtLowM).toBeCloseTo(4, 6)
    expect(result.clearanceM).toBeCloseTo(4 - 1.2, 6)
  })

  it('is unknown with no_depth when depth is null', () => {
    const result = computeLowWaterClearance({ ...baseInput, depthM: null })
    expect(result).toEqual({ status: 'unknown', reason: 'no_depth' })
  })

  it('is unknown with no_depth when depth carries the -1 sentinel', () => {
    const result = computeLowWaterClearance({ ...baseInput, depthM: -1 })
    expect(result).toEqual({ status: 'unknown', reason: 'no_depth' })
  })

  it('is unknown with no_draft when draft is null', () => {
    const result = computeLowWaterClearance({ ...baseInput, draftM: null })
    expect(result).toEqual({ status: 'unknown', reason: 'no_draft' })
  })

  it('is unknown with no_draft when draft carries the -1 sentinel', () => {
    const result = computeLowWaterClearance({ ...baseInput, draftM: -1 })
    expect(result).toEqual({ status: 'unknown', reason: 'no_draft' })
  })

  it('is unknown with no_tide when tide is null', () => {
    const result = computeLowWaterClearance({ ...baseInput, tide: null })
    expect(result).toEqual({ status: 'unknown', reason: 'no_tide' })
  })

  it('is unknown with no_tide when the current tide height carries the -1 sentinel', () => {
    const tide = makeTide({ current_tide_height_ft: -1 })
    const result = computeLowWaterClearance({ ...baseInput, tide })
    expect(result).toEqual({ status: 'unknown', reason: 'no_tide' })
  })

  it('is unknown with no_tide when the next low height carries the -1 sentinel', () => {
    const tide = makeTide({ low_tide_height_ft: -1 })
    const result = computeLowWaterClearance({ ...baseInput, tide })
    expect(result).toEqual({ status: 'unknown', reason: 'no_tide' })
  })

  it('never silently substitutes a zero tide fall when tide data is missing', () => {
    // No masking fallback (AGENTS.md): a missing tide must read as unknown,
    // never quietly compute as if the fall were zero.
    const result = computeLowWaterClearance({ ...baseInput, tide: null })
    expect(result.status).toBe('unknown')
  })

  it('computes a result for a negative next low forecast height (a spring low below chart datum)', () => {
    // Real, below-datum data — the exact case this warning exists for.
    // Fall = (3 - (-0.4)) / 3.28084 = 1.036329 m. depthAtLow = 4 - 1.036329.
    const tide = makeTide({ low_tide_height_ft: -0.4 })
    const result = computeLowWaterClearance({ ...baseInput, tide })
    expect(result.status).not.toBe('unknown')
    if (result.status === 'unknown') throw new Error('expected a resolved result')
    expect(result.depthAtLowM).toBeCloseTo(4 - 3.4 / METERS_PER_FOOT, 3)
  })

  it('computes a result for a negative current tide height', () => {
    // current = -0.4 ft, next low = -0.9 ft (also below datum). Fall = 0.5 ft.
    const tide = makeTide({ current_tide_height_ft: -0.4, low_tide_height_ft: -0.9 })
    const result = computeLowWaterClearance({ ...baseInput, tide })
    expect(result.status).not.toBe('unknown')
    if (result.status === 'unknown') throw new Error('expected a resolved result')
    expect(result.depthAtLowM).toBeCloseTo(4 - 0.5 / METERS_PER_FOOT, 3)
  })
})

describe('computeLowWaterClearance — tide_stale', () => {
  it('is unknown with tide_stale when the tide reading is more than 30 minutes old', () => {
    const tide = makeTide({ datetime: '2026-09-26T13:29:00Z' }) // 31 minutes before NOW
    const result = computeLowWaterClearance({ ...baseInput, tide })
    expect(result).toEqual({ status: 'unknown', reason: 'tide_stale' })
  })

  it('treats exactly 30 minutes old as still fresh — only strictly more trips staleness', () => {
    const tide = makeTide({ datetime: '2026-09-26T13:30:00Z' }) // exactly 30 minutes before NOW
    const result = computeLowWaterClearance({ ...baseInput, tide })
    expect(result.status).not.toBe('unknown')
  })

  it('is unknown with tide_stale when the next low is exactly now (already passed)', () => {
    const tide = makeTide({ low_tide_time: NOW.toISOString() })
    const result = computeLowWaterClearance({ ...baseInput, tide })
    expect(result).toEqual({ status: 'unknown', reason: 'tide_stale' })
  })

  it('is unknown with tide_stale when the next low is before now (already passed)', () => {
    const tide = makeTide({ low_tide_time: '2026-09-26T13:00:00Z' })
    const result = computeLowWaterClearance({ ...baseInput, tide })
    expect(result).toEqual({ status: 'unknown', reason: 'tide_stale' })
  })

  it('is unknown with tide_stale when the tide datetime does not parse', () => {
    const tide = makeTide({ datetime: 'not-a-date' })
    const result = computeLowWaterClearance({ ...baseInput, tide })
    expect(result).toEqual({ status: 'unknown', reason: 'tide_stale' })
  })

  it('is unknown with tide_stale when the low tide time does not parse', () => {
    const tide = makeTide({ low_tide_time: 'not-a-date' })
    const result = computeLowWaterClearance({ ...baseInput, tide })
    expect(result).toEqual({ status: 'unknown', reason: 'tide_stale' })
  })

  it('is not stale for a fresh reading with the low still ahead', () => {
    const result = computeLowWaterClearance(baseInput)
    expect(result.status).not.toBe('unknown')
  })

  it('reports no_depth ahead of tide_stale when both are true (missing-input order)', () => {
    const tide = makeTide({ datetime: '2026-09-26T10:00:00Z' }) // stale
    const result = computeLowWaterClearance({ ...baseInput, depthM: null, tide })
    expect(result).toEqual({ status: 'unknown', reason: 'no_depth' })
  })

  it('exports the 30 minute staleness threshold', () => {
    expect(TIDE_STALE_AFTER_MINUTES).toBe(30)
  })
})

describe('lowWaterClearanceReasonLabel', () => {
  it('names what is missing in the operator\'s words', () => {
    expect(lowWaterClearanceReasonLabel('no_depth')).toBe('No depth')
    expect(lowWaterClearanceReasonLabel('no_draft')).toBe('No draft from the boat')
    expect(lowWaterClearanceReasonLabel('no_tide')).toBe('No tide station')
    expect(lowWaterClearanceReasonLabel('tide_stale')).toBe('Tide forecast out of date')
  })
})

describe('underKeelPhrase', () => {
  it('reads as water under the keel when clearance is zero or more', () => {
    expect(underKeelPhrase(0.24)).toBe('0.2 m under keel')
    expect(underKeelPhrase(0)).toBe('0.0 m under keel')
  })

  it('reads as the keel touching when clearance is negative, never "-0.3 m under keel"', () => {
    expect(underKeelPhrase(-0.3)).toBe('keel 0.3 m into the bottom')
  })

  it('does not print a negative zero for a clearance that rounds to 0.0', () => {
    expect(underKeelPhrase(-0.04)).toBe('0.0 m under keel')
  })
})
