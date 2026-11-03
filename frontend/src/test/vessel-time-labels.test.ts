import { describe, expect, it } from 'vitest'

import { formatAxisTime, formatDayTimeIn, formatHourMinute, formatTimeOfDay, formatTimeOrDayTime } from '@/lib/vessel-time'

// The one way the operator reads a time: "1.30 PM". Dot between hour and
// minute, capital AM or PM, always the minutes, no leading zero.
describe('formatTimeOfDay', () => {
  const brisbane = 'Australia/Brisbane'

  it('writes the fresh-water example as 1.30 PM', () => {
    expect(formatTimeOfDay('2026-11-02T03:30:00Z', brisbane)).toBe('1.30 PM')
  })

  it('writes midnight, noon and the single-digit minutes', () => {
    expect(formatTimeOfDay('2026-11-01T14:00:00Z', brisbane)).toBe('12.00 AM')
    expect(formatTimeOfDay('2026-11-02T02:00:00Z', brisbane)).toBe('12.00 PM')
    expect(formatTimeOfDay('2026-11-01T23:05:00Z', brisbane)).toBe('9.05 AM')
    expect(formatTimeOfDay('2026-11-01T14:05:00Z', brisbane)).toBe('12.05 AM')
    expect(formatTimeOfDay('2026-11-02T12:00:00Z', brisbane)).toBe('10.00 PM')
  })

  it('follows daylight saving in the named zone', () => {
    // Sydney goes from 2.00 AM AEST to 3.00 AM AEDT on Sunday 4 October 2026.
    expect(formatTimeOfDay('2026-10-03T15:59:00Z', 'Australia/Sydney')).toBe('1.59 AM')
    expect(formatTimeOfDay('2026-10-03T16:00:00Z', 'Australia/Sydney')).toBe('3.00 AM')
  })

  it('has no 24-hour form and no zone means no time', () => {
    expect(formatTimeOfDay('2026-11-02T12:00:00Z', brisbane)).not.toMatch(/22/)
    expect(formatTimeOfDay('2026-11-02T12:00:00Z', undefined)).toBe('--')
  })
})

describe('combined stamps', () => {
  it('formatDayTimeIn and formatTimeOrDayTime use the same form', () => {
    expect(formatDayTimeIn('2026-11-02T03:30:00Z', 'Australia/Brisbane')).toBe('2 Nov 2026 1.30 PM')
    const now = new Date('2026-11-02T05:00:00Z')
    expect(formatTimeOrDayTime('2026-11-02T03:30:00Z', 'Australia/Brisbane', now)).toBe('1.30 PM')
    expect(formatTimeOrDayTime('2026-11-01T03:30:00Z', 'Australia/Brisbane', now)).toBe('1 Nov 1.30 PM')
  })
})

describe('formatHourMinute', () => {
  it('reads a split clock the same way', () => {
    expect(formatHourMinute(0, 0)).toBe('12.00 AM')
    expect(formatHourMinute(6, 0)).toBe('6.00 AM')
    expect(formatHourMinute(12, 5)).toBe('12.05 PM')
    expect(formatHourMinute(22, 30)).toBe('10.30 PM')
  })
})

describe('formatAxisTime', () => {
  it('is short on the hour and gives the minutes off it', () => {
    expect(formatAxisTime('2026-11-02T03:00:00Z', 'Australia/Brisbane')).toBe('1 PM')
    expect(formatAxisTime('2026-11-02T03:30:00Z', 'Australia/Brisbane')).toBe('1.30 PM')
    expect(formatAxisTime('2026-11-01T14:00:00Z', 'Australia/Brisbane')).toBe('12 AM')
  })
})
