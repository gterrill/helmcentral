import { describe, expect, it } from 'vitest'

import { lowWaterClearanceReasonLabel, underKeelPhrase, underKeelRangePhrase } from '@/lib/low-water-clearance'

// The clearance itself is worked out on the server now (backend/anchor_low_water_test.go
// holds the rules); these are the words the page puts to it.
describe('lowWaterClearanceReasonLabel', () => {
  it('names what is missing in the operator\'s words', () => {
    expect(lowWaterClearanceReasonLabel('no_depth')).toBe('No depth')
    expect(lowWaterClearanceReasonLabel('no_draft')).toBe('No draft from the boat')
    expect(lowWaterClearanceReasonLabel('no_tide_station')).toBe('No tide station')
    expect(lowWaterClearanceReasonLabel('tide_unreachable')).toBe('Tide unavailable')
    expect(lowWaterClearanceReasonLabel('no_tide')).toBe('Tide unavailable')
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

  it('rounds once and prints the rounded figure, as the verdict is judged', () => {
    expect(underKeelPhrase(-0.25)).toBe('keel 0.2 m into the bottom')
    expect(underKeelPhrase(0.25)).toBe('0.3 m under keel')
  })

  it('does not print a negative zero for a clearance that rounds to 0.0', () => {
    expect(underKeelPhrase(-0.04)).toBe('0.0 m under keel')
  })
})

describe('underKeelRangePhrase', () => {
  const length = { format: (m: number) => m.toFixed(1), unit: 'm' }

  it('keeps the bottom the verdict was judged on even when the half width reads 0.0', () => {
    // 0.48 m centre, 0.04 m half width: shows 0.5 m, but the bottom is 0.4 m.
    expect(underKeelRangePhrase(0.48, 0.04, true, length)).toBe('0.5 m under keel, could be 0.4 m')
  })

  it('leaves the bottom off when it reads the same as the centre', () => {
    expect(underKeelRangePhrase(0.8, 0.03, true, length)).toBe('0.8 m under keel')
  })

  it('shows the range and the bottom when the half width is worth printing', () => {
    expect(underKeelRangePhrase(0.8, 0.4, true, length)).toBe('0.8 m ±0.4 under keel, could be 0.4 m')
  })
})
