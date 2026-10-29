import { describe, expect, it } from 'vitest'

import { buildTimeline } from '@/lib/passages'
import { moveByCapture, placeByCapture } from '@/lib/sign-placement'

// 22 Oct 2026, 09:00-15:00 UTC+10 is 23:00 (21st) to 05:00 UTC.
const start = Date.UTC(2026, 9, 21, 23, 0) / 1000
// A track covering the whole span, since a sign is placed only where the
// replay has a position.
const track = [{ t: start, lat: -20, lon: 148 }, { t: start + 6 * 3600, lat: -20.5, lon: 148.5 }]
const timeline = buildTimeline([{ id: 'p', start, end: start + 6 * 3600, segments: [track] }])
const cursor = start + 3600
const base = { year: 2026, month: 10, day: 22, hour: 11, minute: 30, second: 0 }

describe('placeByCapture', () => {
  it('puts the sign at the capture time when it falls inside a replayed passage, reading it in the boat zone', () => {
    const r = placeByCapture({ ...base, offsetMinutes: null }, 10, timeline, cursor)
    expect(r.at).toBe(Date.UTC(2026, 9, 22, 1, 30) / 1000)
    expect(r.placedByPhoto).toBe(true)
    expect(r.message).toBe('Placed at the time the photo was taken: 22 Oct 11:30 UTC+10 (the photo has no time zone, so boat time is used).')
  })

  it('uses the photo\'s own zone when it has one', () => {
    const r = placeByCapture({ ...base, hour: 9, minute: 30, offsetMinutes: 480 }, 10, timeline, cursor)
    expect(r.at).toBe(Date.UTC(2026, 9, 22, 1, 30) / 1000)
    expect(r.message).toBe('Placed at the time the photo was taken: 22 Oct 09:30 UTC+08:00.')
  })

  it('otherwise sits at the cursor and says why', () => {
    const outside = placeByCapture({ ...base, day: 25, offsetMinutes: null }, 10, timeline, cursor)
    expect(outside.at).toBe(cursor)
    expect(outside.placedByPhoto).toBe(false)
    expect(outside.message).toBe('The photo was taken 25 Oct 11:30 UTC+10, outside this replay, so the sign is at the current position.')
    expect(placeByCapture(null, 10, timeline, cursor).message).toBe('The photo has no capture time, so the sign is at the current position.')
  })

  it('says the time could not be read when it could not', () => {
    const r = placeByCapture(new Error('The photo\'s EXIF block is damaged'), 10, timeline, cursor)
    expect(r.at).toBe(cursor)
    expect(r.message).toBe('Could not read when the photo was taken (The photo\'s EXIF block is damaged), so the sign is at the current position.')
  })
})

describe('a passage whose time zone is not known', () => {
  it('does not place a photo with no zone of its own by its time, and says why', () => {
    const r = placeByCapture({ ...base, offsetMinutes: null }, null, timeline, cursor)
    expect(r.at).toBe(cursor)
    expect(r.placedByPhoto).toBe(false)
    expect(r.message).toBe("This passage's time zone isn't known, so the photo's time can't be matched to the track, so the sign is at the current position.")
  })

  it('leaves an already placed sign where it is', () => {
    const r = moveByCapture({ ...base, offsetMinutes: null }, null, timeline)
    expect(r.at).toBeNull()
    expect(r.message).toBe("This passage's time zone isn't known, so the photo's time can't be matched to the track, so the sign was not moved.")
  })

  it('still places a photo that carries its own zone', () => {
    const r = placeByCapture({ ...base, hour: 9, minute: 30, offsetMinutes: 480 }, null, timeline, cursor)
    expect(r.at).toBe(Date.UTC(2026, 9, 22, 1, 30) / 1000)
    expect(r.placedByPhoto).toBe(true)
  })
})
