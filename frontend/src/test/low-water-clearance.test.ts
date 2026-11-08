import { describe, expect, it } from 'vitest'

import { lowWaterClearanceReasonLabel, underKeelPhrase } from '@/lib/low-water-clearance'

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

  it('does not print a negative zero for a clearance that rounds to 0.0', () => {
    expect(underKeelPhrase(-0.04)).toBe('0.0 m under keel')
  })
})
