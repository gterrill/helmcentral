import { describe, it, expect } from 'vitest'

import { parseClockMinutes, resolveAutoTheme } from '@/lib/auto-theme'

// The backend formats sunrise/sunset in the vessel's own zone as "3:04PM"
// (weather_providers.go), so these tests build instants from a UTC+10 boat
// (Australia/Brisbane has no DST) and check the comparison happens in that zone.
const TZ = 'Australia/Brisbane'
const sun = { sunriseTime: '6:12AM', sunsetTime: '5:48PM', timeZone: TZ }

// Brisbane local hh:mm on a fixed day, as a UTC instant.
function at(hh: number, mm: number): Date {
  return new Date(Date.UTC(2026, 9, 2, hh - 10, mm))
}

describe('parseClockMinutes', () => {
  it('reads the backend format', () => {
    expect(parseClockMinutes('6:12AM')).toBe(6 * 60 + 12)
    expect(parseClockMinutes('5:48PM')).toBe(17 * 60 + 48)
  })

  it('handles noon and midnight', () => {
    expect(parseClockMinutes('12:00AM')).toBe(0)
    expect(parseClockMinutes('12:30PM')).toBe(12 * 60 + 30)
  })

  it('tolerates a space and lower case', () => {
    expect(parseClockMinutes('6:12 am')).toBe(6 * 60 + 12)
  })

  it('returns null for anything else', () => {
    expect(parseClockMinutes(null)).toBeNull()
    expect(parseClockMinutes('')).toBeNull()
    expect(parseClockMinutes('—')).toBeNull()
    expect(parseClockMinutes('18:12')).toBeNull()
    expect(parseClockMinutes('13:00PM')).toBeNull()
    expect(parseClockMinutes('6:75AM')).toBeNull()
  })
})

describe('resolveAutoTheme', () => {
  it('is night before sunrise and names the sunrise time', () => {
    expect(resolveAutoTheme({ ...sun, now: at(5, 59) })).toEqual({ isDark: true, until: '6:12 AM' })
  })

  it('is day from sunrise on', () => {
    expect(resolveAutoTheme({ ...sun, now: at(6, 12) })).toEqual({ isDark: false, until: '5:48 PM' })
    expect(resolveAutoTheme({ ...sun, now: at(12, 0) })?.isDark).toBe(false)
  })

  it('is night from sunset on, until the next sunrise', () => {
    expect(resolveAutoTheme({ ...sun, now: at(17, 47) })?.isDark).toBe(false)
    expect(resolveAutoTheme({ ...sun, now: at(17, 48) })).toEqual({ isDark: true, until: '6:12 AM' })
    expect(resolveAutoTheme({ ...sun, now: at(23, 30) })?.isDark).toBe(true)
  })

  it('compares in the vessel zone, not the browser zone', () => {
    // 20:00 UTC is 06:00 next day in Brisbane: still before sunrise there.
    expect(resolveAutoTheme({ ...sun, now: new Date(Date.UTC(2026, 9, 2, 20, 0)) })?.isDark).toBe(true)
    // 02:00 UTC is noon in Brisbane.
    expect(resolveAutoTheme({ ...sun, now: new Date(Date.UTC(2026, 9, 2, 2, 0)) })?.isDark).toBe(false)
  })

  it('returns null when either time is missing', () => {
    expect(resolveAutoTheme({ ...sun, sunriseTime: null, now: at(12, 0) })).toBeNull()
    expect(resolveAutoTheme({ ...sun, sunsetTime: null, now: at(12, 0) })).toBeNull()
  })

  it('returns null for an unparseable time rather than guessing', () => {
    expect(resolveAutoTheme({ ...sun, sunsetTime: 'soon', now: at(12, 0) })).toBeNull()
  })

  it('returns null when sunset is not after sunrise (no ordinary day to split)', () => {
    expect(resolveAutoTheme({ ...sun, sunriseTime: '6:00PM', sunsetTime: '5:00AM', now: at(12, 0) })).toBeNull()
    expect(resolveAutoTheme({ ...sun, sunriseTime: '6:00AM', sunsetTime: '6:00AM', now: at(12, 0) })).toBeNull()
  })

  it('returns null for a zone Intl does not know', () => {
    expect(resolveAutoTheme({ ...sun, timeZone: 'Not/AZone', now: at(12, 0) })).toBeNull()
  })
})
