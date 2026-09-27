import { afterEach, describe, expect, it, vi } from 'vitest'
import { addMonthsISO, todayISO } from '@/lib/local-date'

// ADR 0138's 2026-09-27 amendment: calendar dates (a maintenance rule's
// "today") are the operator's own LOCAL date, never UTC. The boat this
// shipped for runs at UTC+10 - before about 10am local, the UTC date is
// still YESTERDAY, so `new Date().toISOString().slice(0, 10)` (the bug)
// would compute every due/overdue decision against the wrong day for
// roughly the first ten hours of every single day.

// Assigning process.env.TZ inside a vitest worker does not change the
// timezone Date already resolved, so these tests pass only on a machine
// that is already east of UTC. They pin the local calendar fields instead,
// which holds on any machine, CI's UTC runners included.

afterEach(() => {
  vi.restoreAllMocks()
  vi.useRealTimers()
})

describe('todayISO', () => {
  it('uses the operator\'s own local date, not UTC', () => {
    // 2026-06-14T20:00:00Z is 2026-06-15 06:00 at UTC+10: still yesterday
    // in UTC, already tomorrow at the helm.
    const date = new Date('2026-06-14T20:00:00Z')
    vi.spyOn(date, 'getFullYear').mockReturnValue(2026)
    vi.spyOn(date, 'getMonth').mockReturnValue(5)
    vi.spyOn(date, 'getDate').mockReturnValue(15)

    expect(date.toISOString().slice(0, 10)).toBe('2026-06-14')
    expect(todayISO(date)).toBe('2026-06-15')
  })

  it('reads the system clock when given no date', () => {
    vi.useFakeTimers()
    // The local-time constructor, so the calendar day is the 15th in
    // whatever timezone runs the test.
    vi.setSystemTime(new Date(2026, 5, 15, 6, 0, 0))

    expect(todayISO()).toBe('2026-06-15')
  })

  it('accepts an explicit Date rather than always reading the system clock', () => {
    expect(todayISO(new Date(2026, 5, 15, 3, 0, 0))).toBe('2026-06-15')
  })

  it('zero-pads a single-digit month and day', () => {
    expect(todayISO(new Date(2026, 0, 5, 12, 0, 0))).toBe('2026-01-05')
  })
})

describe('addMonthsISO', () => {
  it('adds whole months', () => {
    expect(addMonthsISO('2026-05-20', 12)).toBe('2027-05-20')
  })

  it('rolls over the year boundary', () => {
    expect(addMonthsISO('2026-11-15', 3)).toBe('2027-02-15')
  })

  it('normalises an out-of-range day the same way a calendar would', () => {
    // Jan 31 + 1 month has no Feb 31 - it spills into March, matching the
    // backend's own time.AddDate semantics (maintenance_handlers.go).
    expect(addMonthsISO('2026-01-31', 1)).toBe('2026-03-03')
  })
})
