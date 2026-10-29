import { describe, expect, it } from 'vitest'

import {
  zoneClocksDiffer,
  zoneOffsetLabel,
  zoneOffsetMinutes,
  zonedDateTimeParts,
  zonedDayStart,
} from '@/lib/vessel-time'

describe('zones named by the server', () => {
  it('reads the offset with daylight saving at the instant', () => {
    expect(zoneOffsetMinutes('Australia/Sydney', Date.UTC(2026, 0, 15))).toBe(660)
    expect(zoneOffsetMinutes('Australia/Sydney', Date.UTC(2026, 6, 15))).toBe(600)
    expect(zoneOffsetMinutes('Australia/Adelaide', Date.UTC(2026, 6, 15))).toBe(570)
    expect(zoneOffsetMinutes('UTC', 0)).toBe(0)
    expect(zoneOffsetMinutes('Etc/GMT+5', 0)).toBe(-300)
  })

  it('labels the offset the way the operator says it', () => {
    expect(zoneOffsetLabel('Australia/Brisbane', Date.UTC(2026, 9, 29))).toBe('UTC+10')
    expect(zoneOffsetLabel('Australia/Adelaide', Date.UTC(2026, 6, 15))).toBe('UTC+9:30')
    expect(zoneOffsetLabel('America/New_York', Date.UTC(2026, 0, 15))).toBe('UTC-5')
    expect(zoneOffsetLabel('UTC', 0)).toBe('UTC')
  })

  it('finds the instant a day begins, across a daylight-saving change', () => {
    expect(zonedDayStart('2026-10-27', 'Australia/Brisbane').toISOString()).toBe('2026-10-26T14:00:00.000Z')
    expect(zonedDayStart('2026-10-27', 'America/New_York').toISOString()).toBe('2026-10-27T04:00:00.000Z')
    // Sydney moves to daylight saving at 02:00 on 4 Oct 2026; the day still starts at midnight.
    expect(zonedDayStart('2026-10-04', 'Australia/Sydney').toISOString()).toBe('2026-10-03T14:00:00.000Z')
    expect(zonedDayStart('2026-10-05', 'Australia/Sydney').toISOString()).toBe('2026-10-04T13:00:00.000Z')
  })

  it('compares clocks by offset, not by zone name', () => {
    const at = Date.UTC(2026, 9, 29)
    expect(zoneClocksDiffer('Australia/Brisbane', 'Etc/GMT-10', at)).toBe(false)
    expect(zoneClocksDiffer('Australia/Sydney', 'Australia/Brisbane', at)).toBe(true)
  })

  it('splits an instant into date, time and zone', () => {
    const t = Date.UTC(2026, 2, 13, 23, 30) / 1000
    expect(zonedDateTimeParts(t, 'Australia/Brisbane')).toEqual({ date: '14 Mar', time: '09:30', zone: 'UTC+10' })
    expect(zonedDateTimeParts(t, 'UTC')).toEqual({ date: '13 Mar', time: '23:30', zone: 'UTC' })
  })
})
