import { describe, expect, it, vi, afterEach } from 'vitest'

import { formatClock, formatDate, formatWeekday, isSameLocalDate } from '@/lib/vessel-time'

afterEach(() => {
  vi.restoreAllMocks()
})

// The wall display's browser sits on UTC while the boat is at UTC+10 (live
// finding: the dashboard clock read 08:31 PM at 06:31 AM boat time). These
// pin formatClock/formatDate to an explicit vessel zone rather than the
// browser's, so the assertions hold regardless of the machine running the
// suite — which happens to be Australia/Lindeman, itself UTC+10, so a test
// that only omitted timeZone would not actually prove anything.

describe('formatClock', () => {
  it('renders vessel-local time for a zone east of UTC, not the browser zone', () => {
    const instant = new Date('2026-09-12T20:31:00Z')

    const { timePart, meridiem } = formatClock(instant, 'Etc/GMT-10')

    expect(`${timePart} ${meridiem}`).toBe('06:31:00 AM')
  })

  it('throws for an unknown zone name rather than reading another zone\'s clock', () => {
    const instant = new Date('2026-09-12T20:31:00Z')

    expect(() => formatClock(instant, 'Not/AZone')).toThrow(RangeError)
    // Nothing was cached for the bad name: asking again throws again.
    expect(() => formatClock(instant, 'Not/AZone')).toThrow(RangeError)
  })

  it('reads the browser zone and says so once while the boat has not reported a zone', () => {
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const instant = new Date('2026-09-12T20:31:00Z')

    expect(() => formatClock(instant)).not.toThrow()
    formatClock(instant)
    formatDate(instant)
    expect(warnSpy.mock.calls.filter((c) => String(c[0]).includes('no zone yet'))).toHaveLength(1)
    warnSpy.mockRestore()
  })
})

describe('formatDate', () => {
  it('crosses the vessel-local midnight boundary independently of the browser zone', () => {
    // 2026-09-12T20:31Z is already Sep 13 local at UTC+10, but still Sep 12 at UTC.
    const instant = new Date('2026-09-12T20:31:00Z')

    expect(formatDate(instant, { timeZone: 'Etc/GMT-10' })).toContain('Sep 13, 2026')
    expect(formatDate(instant, { timeZone: 'UTC' })).toContain('Sep 12, 2026')
  })

  it('throws for an unknown zone name', () => {
    expect(() => formatDate(new Date('2026-09-12T20:31:00Z'), { timeZone: 'Not/AZone' })).toThrow(RangeError)
  })
})

// ADR 0125: the clock tile's ETA line prefixes a 3-letter weekday when the
// ETA isn't today in vessel-local time (HelmCast did this).
describe('formatWeekday', () => {
  it('names the weekday in the given vessel-local zone, independent of the browser zone', () => {
    // 2026-09-12T20:31Z is Sep 12 (Saturday) at UTC, but already Sep 13
    // (Sunday) at UTC+10.
    const instant = new Date('2026-09-12T20:31:00Z')

    expect(formatWeekday(instant, 'UTC')).toBe('Sat')
    expect(formatWeekday(instant, 'Etc/GMT-10')).toBe('Sun')
  })
})

describe('isSameLocalDate', () => {
  it('is true for two instants on the same calendar date in the given zone', () => {
    const a = new Date('2026-09-12T01:00:00Z')
    const b = new Date('2026-09-12T23:00:00Z')
    expect(isSameLocalDate(a, b, 'UTC')).toBe(true)
  })

  it('is false once the zone-local calendar date differs', () => {
    const a = new Date('2026-09-12T23:00:00Z')
    const b = new Date('2026-09-13T01:00:00Z')
    expect(isSameLocalDate(a, b, 'UTC')).toBe(false)
  })

  it('crosses the vessel-local midnight boundary independently of the browser zone', () => {
    // Same UTC instants as the "false" case above, but at UTC+10 both
    // already fall on Sep 13 - the zone the comparison runs in changes the
    // answer, exactly like formatDate's own boundary-crossing test above.
    const a = new Date('2026-09-12T23:00:00Z')
    const b = new Date('2026-09-13T01:00:00Z')
    expect(isSameLocalDate(a, b, 'Etc/GMT-10')).toBe(true)
  })
})
