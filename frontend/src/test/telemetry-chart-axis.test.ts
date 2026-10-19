import { describe, expect, it } from 'vitest'

import { axisTicks } from '@/lib/telemetry-chart'

describe('axisTicks', () => {
  it('floors at zero when no value is negative', () => {
    const t = axisTicks([0, 12, 68], 'A')
    expect(t[0]).toBe(0)
    expect(t[t.length - 1]).toBeGreaterThanOrEqual(68)
  })

  it('keeps a negative floor for data that goes negative', () => {
    expect(axisTicks([-30, 10], 'A')[0]).toBeLessThan(0)
  })

  it('clamps a percent axis to 0-100', () => {
    const t = axisTicks([48, 97], '%')
    expect(t[0]).toBeGreaterThanOrEqual(0)
    expect(t[t.length - 1]).toBe(100)
    expect(Math.max(...t)).toBeLessThanOrEqual(100)
  })

  it('does not run a percent axis past 100 for data at 100', () => {
    const t = axisTicks([99, 100], '%')
    expect(Math.max(...t)).toBe(100)
    expect(Math.min(...t)).toBeGreaterThanOrEqual(0)
  })

  it('still returns ticks for no data', () => {
    expect(axisTicks([], 'A').length).toBeGreaterThan(1)
  })
})
