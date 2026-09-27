import { afterEach, describe, expect, it, vi } from 'vitest'
import { addMonthsISO, todayISO } from '@/lib/local-date'

// ADR 0138's 2026-09-27 amendment: calendar dates (a maintenance rule's
// "today") are the operator's own LOCAL date, never UTC. The boat this
// shipped for runs at UTC+10 - before about 10am local, the UTC date is
// still YESTERDAY, so `new Date().toISOString().slice(0, 10)` (the bug)
// would compute every due/overdue decision against the wrong day for
// roughly the first ten hours of every single day.

const originalTZ = process.env.TZ

afterEach(() => {
  process.env.TZ = originalTZ
  vi.useRealTimers()
})

describe('todayISO', () => {
  it('uses the operator\'s own local date, not UTC', () => {
    process.env.TZ = 'Australia/Brisbane' // UTC+10, no daylight saving
    vi.useFakeTimers()
    // 2026-06-14T20:00:00Z UTC is 2026-06-15T06:00:00+10:00 local - the
    // exact "still yesterday in UTC, already tomorrow at the helm" case.
    vi.setSystemTime(new Date('2026-06-14T20:00:00Z'))

    expect(todayISO()).toBe('2026-06-15')
  })

  it('still agrees with UTC when the local date and the UTC date are the same', () => {
    process.env.TZ = 'Australia/Brisbane'
    vi.useFakeTimers()
    // 2026-06-15T04:00:00Z UTC is 2026-06-15T14:00:00+10:00 local - both
    // read the same calendar day.
    vi.setSystemTime(new Date('2026-06-15T04:00:00Z'))

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
