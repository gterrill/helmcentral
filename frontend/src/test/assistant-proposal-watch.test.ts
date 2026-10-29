import { describe, expect, it } from 'vitest'

import { fieldLabel } from '@/components/assistant-proposal-views'

// ADR 0227: Mate proposes a watch. The card names its fields the way the
// Watches page does.
describe('watch proposal field labels', () => {
  it('reads the schedule, sections and thresholds in the operator’s words', () => {
    expect(fieldLabel('schedule', 'watch')).toBe('Runs')
    expect(fieldLabel('sections', 'watch')).toBe('Reports on')
    expect(fieldLabel('look_out_for', 'watch')).toBe('Looks out for')
    expect(fieldLabel('tank_change_points', 'watch')).toBe('Tank change that counts (points)')
    expect(fieldLabel('pressure_fall_hpa', 'watch')).toBe('Pressure fall that counts (hPa)')
  })

  it('leaves other types alone', () => {
    expect(fieldLabel('schedule', 'location')).toBe('Schedule')
  })
})
