import { describe, expect, it } from 'vitest'

import { buildTimeline } from '@/lib/passages'
import { midnightsSince } from '@/lib/boat-nights'
import { CARD_FADE_S } from '@/lib/replay-signs'
import {
  MIN_STAY_S, staySunTime, stayDurationText, stayLabelBox, stayLabelText, stayView, staysOf, type StayPassage,
} from '@/lib/replay-stay'
import { STAY_S, buildReplayTime } from '@/lib/replay-time'

const BRISBANE = 'Australia/Brisbane' // UTC+10, no daylight saving
const HOUR = 3600
const DAY = 86400
// 2026-10-24 00:00 in Brisbane.
const MIDNIGHT = Date.UTC(2026, 9, 23, 14, 0) / 1000

const passage = (id: string, extra: Partial<StayPassage> = {}): StayPassage => ({ id, from_name: '', to_name: '', timezone: BRISBANE, ...extra })
// Brisbane is UTC+10 all year.
const OFFSET = 10

describe('staySunTime', () => {
  const arrival = MIDNIGHT + 16 * HOUR // 16:00
  it('runs the true clock from the arrival to the departure for a stay of a day or less', () => {
    const departure = arrival + 15 * HOUR // 07:00 next morning
    expect(staySunTime(arrival, departure, 0)).toBe(arrival)
    expect(staySunTime(arrival, departure, 0.5)).toBe(arrival + 7.5 * HOUR)
    expect(staySunTime(arrival, departure, 1)).toBe(departure)
  })

  it('runs through the last day before the departure for a longer one: one evening, one night, one morning', () => {
    const departure = arrival + 5 * DAY + 15 * HOUR
    expect(staySunTime(arrival, departure, 0)).toBe(departure - DAY)
    expect(staySunTime(arrival, departure, 0.5)).toBe(departure - DAY / 2)
    // It ends on the departure's own light.
    expect(staySunTime(arrival, departure, 1)).toBe(departure)
  })

  it('never covers more than a day of the sun, however long the stay', () => {
    for (const days of [1.5, 2, 7, 30]) {
      const departure = arrival + days * DAY
      expect(staySunTime(arrival, departure, 1) - staySunTime(arrival, departure, 0)).toBeLessThanOrEqual(DAY)
    }
  })

  it('moves forward and keeps to the range', () => {
    const departure = arrival + 3 * DAY
    let last = -Infinity
    for (const p of [-1, 0, 0.25, 0.5, 0.75, 1, 2]) {
      const t = staySunTime(arrival, departure, p)
      expect(t).toBeGreaterThanOrEqual(last)
      last = t
    }
    expect(staySunTime(arrival, departure, -1)).toBe(staySunTime(arrival, departure, 0))
    expect(staySunTime(arrival, departure, 2)).toBe(departure)
  })
})

describe('stayDurationText', () => {
  it('counts nights by the boat\'s own midnights', () => {
    // 22:00 to 07:00 next day: one night, though it is nine hours.
    expect(stayDurationText(MIDNIGHT + 22 * HOUR, MIDNIGHT + 31 * HOUR, OFFSET)).toBe('1 night')
    // 10:00 on the 24th to 10:00 on the 26th.
    expect(stayDurationText(MIDNIGHT + 10 * HOUR, MIDNIGHT + 2 * DAY + 10 * HOUR, OFFSET)).toBe('2 nights')
    // 23:30 to 00:30 across the boat's midnight, which is not midnight in UTC.
    expect(stayDurationText(MIDNIGHT + 23.5 * HOUR, MIDNIGHT + 24.5 * HOUR, OFFSET)).toBe('1 night')
  })

  it('reads the time like the rest of the app under a night', () => {
    expect(stayDurationText(MIDNIGHT + 9 * HOUR, MIDNIGHT + 13 * HOUR, OFFSET)).toBe('4 h 00 m')
    expect(stayDurationText(MIDNIGHT + 9 * HOUR, MIDNIGHT + 12 * HOUR + 5 * 60, OFFSET)).toBe('3 h 05 m')
    expect(stayDurationText(MIDNIGHT + 9 * HOUR, MIDNIGHT + 9 * HOUR + 42 * 60, OFFSET)).toBe('42 m')
    expect(stayDurationText(MIDNIGHT + 9 * HOUR, MIDNIGHT + 9 * HOUR + MIN_STAY_S, OFFSET)).toBe('1 m')
  })

  it('gives the time alone when the boat\'s zone is not known, rather than guess at nights', () => {
    expect(stayDurationText(MIDNIGHT + 22 * HOUR, MIDNIGHT + 31 * HOUR, null)).toBe('9 h 00 m')
  })

  it('counts the nights the cruise page counts, on the same inputs, in a zone that is not UTC', () => {
    const cases: [number, number][] = [
      [MIDNIGHT + 8 * HOUR, MIDNIGHT + 17 * HOUR], // the same day on the boat, though UTC changes date
      [MIDNIGHT + 23.5 * HOUR, MIDNIGHT + 24.5 * HOUR], // across the boat's midnight, within one UTC day
      [MIDNIGHT + 14 * HOUR, MIDNIGHT + 38 * HOUR],
      [MIDNIGHT + 22 * HOUR, MIDNIGHT + 4 * DAY + 7 * HOUR],
      [MIDNIGHT + 24 * HOUR, MIDNIGHT + 2 * DAY], // from midnight to midnight: an instant on midnight counts none of its own
    ]
    for (const [arrival, departure] of cases) {
      // The cruise page counts its nights with this function (lib/passage-export uses the same one).
      const nights = midnightsSince(arrival, departure, OFFSET)
      const text = stayDurationText(arrival, departure, OFFSET)
      if (nights >= 1) expect(text).toBe(nights === 1 ? '1 night' : `${nights} nights`)
      else expect(text).toMatch(/\d m$/)
    }
  })
})

describe('stayLabelText', () => {
  const t = MIDNIGHT + 9 * HOUR
  it('is the time and the place where the arriving passage ended', () => {
    expect(stayLabelText(passage('a', { to_name: 'Blue Pearl Bay' }), passage('b', { from_name: 'Elsewhere' }), t, t + 2 * DAY, OFFSET)).toBe('2 nights · Blue Pearl Bay')
  })
  it('is the place the next passage set out from, when the first has none', () => {
    expect(stayLabelText(passage('a'), passage('b', { from_name: 'Hayman Island' }), t, t + 4 * HOUR, OFFSET)).toBe('4 h 00 m · Hayman Island')
  })
  it('is the time alone when neither passage names a place', () => {
    expect(stayLabelText(passage('a', { to_name: '  ' }), passage('b'), t, t + 4 * HOUR, OFFSET)).toBe('4 h 00 m')
  })
})

describe('staysOf', () => {
  const seg = (start: number) => [{ t: start, lat: -20, lon: 149 }, { t: start + 100, lat: -20, lon: 149.1 }] as never
  const timeline = buildTimeline([
    { id: 'a', start: MIDNIGHT, end: MIDNIGHT + 100, segments: [seg(MIDNIGHT)] },
    { id: 'b', start: MIDNIGHT + 2 * DAY, end: MIDNIGHT + 2 * DAY + 100, segments: [seg(MIDNIGHT + 2 * DAY)] },
    // Sails again at once: not a stay.
    { id: 'c', start: MIDNIGHT + 2 * DAY + 100 + 10, end: MIDNIGHT + 2 * DAY + 400, segments: [seg(MIDNIGHT + 2 * DAY + 110)] },
  ])
  const passages = [passage('a', { to_name: 'Bay' }), passage('b'), passage('c')]

  it('has one between each pair of passages with a gap, at the start of the second', () => {
    const stays = staysOf(timeline, passages)
    expect(stays).toHaveLength(1)
    expect(stays[0]).toMatchObject({
      id: 'stay:a>b', index: 0, clock: timeline.entries[1].offset, at: MIDNIGHT + 100, fromId: 'a', toId: 'b', arrival: MIDNIGHT + 100, departure: MIDNIGHT + 2 * DAY,
    })
    // 00:01 on the 24th to 00:00 on the 26th: the cruise page's rule gives an instant on midnight none of its own.
    expect(stays[0].label).toBe('1 night · Bay')
  })

  it('has none for a single passage', () => {
    expect(staysOf(buildTimeline([{ id: 'a', start: 0, end: 100, segments: [] }]), [passage('a')])).toEqual([])
  })

  it('says so when a passage of the timeline is not among those given', () => {
    expect(() => staysOf(timeline, [passage('a')])).toThrow(/not in the replay/)
  })
})

describe('stayView', () => {
  const stay = { id: 'stay:0', index: 0, clock: 100, at: 1000, fromId: 'a', toId: 'b', arrival: 1000, departure: 1000 + 3 * HOUR, label: '3 h' }
  it('carries the stay, how far through it is, and the sun time for the light', () => {
    const v = stayView(stay, STAY_S / 2)
    expect(v.stay).toBe(stay)
    expect(v).toMatchObject({ progress: 0.5, elapsed: STAY_S / 2 })
    expect(v.sunAt).toBe(1000 + 1.5 * HOUR)
    expect(stayView(stay, 0).sunAt).toBe(1000)
    expect(stayView(stay, 99).progress).toBe(1)
  })
})

describe('stayLabelBox', () => {
  const frame = { w: 1280, h: 720 }
  it('sits low and in the middle, small, inside the frame', () => {
    const b = stayLabelBox(frame, STAY_S / 2)
    expect(b.cx).toBe(640)
    expect(b.y).toBeGreaterThan(frame.h / 2)
    expect(b.y).toBeLessThan(frame.h)
    expect(b.fontPx).toBeLessThanOrEqual(14)
    expect(b.maxW).toBeLessThan(frame.w)
  })
  it('fades in and out over the stay, and is whole between', () => {
    expect(stayLabelBox(frame, 0).alpha).toBe(0)
    expect(stayLabelBox(frame, CARD_FADE_S / 2).alpha).toBeCloseTo(0.5)
    expect(stayLabelBox(frame, STAY_S / 2).alpha).toBe(1)
    expect(stayLabelBox(frame, STAY_S - CARD_FADE_S / 2).alpha).toBeCloseTo(0.5)
    expect(stayLabelBox(frame, STAY_S).alpha).toBe(0)
  })
  it('is in the frame\'s own pixels, so a taller frame puts it lower', () => {
    expect(stayLabelBox({ w: 1080, h: 1920 }, 0.75).y).toBeGreaterThan(stayLabelBox(frame, 0.75).y)
  })
})

describe('stayLabelBox and the overview map', () => {
  const narrow = { w: 400, h: 600 }

  it('is centred and as wide as the map allows when no overview map is shown', () => {
    const b = stayLabelBox(narrow, 0.75, null)
    expect(b.cx).toBe(200)
    expect(b.maxW).toBe(352)
  })

  it('keeps clear of an overview map in the bottom-left corner of a narrow map', () => {
    const b = stayLabelBox(narrow, 0.75, 'bottom-left')
    const left = b.cx - b.maxW / 2
    // The overview map is 176 px wide, 8 px in from the side.
    expect(left).toBeGreaterThanOrEqual(8 + 176)
    expect(b.cx + b.maxW / 2).toBeLessThanOrEqual(narrow.w - 24)
    expect(b.maxW).toBeGreaterThan(0)
  })

  it('keeps clear of one in the bottom-right corner', () => {
    const b = stayLabelBox(narrow, 0.75, 'bottom-right')
    expect(b.cx + b.maxW / 2).toBeLessThanOrEqual(narrow.w - 8 - 176)
    expect(b.cx - b.maxW / 2).toBeGreaterThanOrEqual(24)
  })

  it('does not move for an overview map up in a top corner', () => {
    expect(stayLabelBox(narrow, 0.75, 'top-left')).toEqual(stayLabelBox(narrow, 0.75, null))
    expect(stayLabelBox(narrow, 0.75, 'top-right')).toEqual(stayLabelBox(narrow, 0.75, null))
  })

  it('has no room left on a map too narrow for both', () => {
    expect(stayLabelBox({ w: 200, h: 400 }, 0.75, 'bottom-left').maxW).toBe(0)
  })
})

describe('a stay in the time map', () => {
  // A cruise of two passages, 1000 s of sailing in 10 s, with a stay at the join and a photo moment either side of it.
  const time = (stays = [{ clock: 500, id: 'stay:0' }], moments: { clock: number; id: string }[] = []) => buildReplayTime({ total: 1000, lengthS: 10, spans: [], moments, stays })

  it('holds the timeline still for the length of a stay', () => {
    const t = time()
    expect(STAY_S).toBe(1.5)
    expect(t.totalWall).toBeCloseTo(10 + STAY_S)
    expect(t.holds.map((h) => h.kind)).toEqual(['stay'])
    const start = t.holds[0].startWall
    expect(start).toBeCloseTo(5)
    expect(t.at(start + 0.5)).toMatchObject({ clock: 500, hold: { kind: 'stay', id: 'stay:0', durationS: STAY_S } })
    expect(t.at(start + 0.5).hold!.elapsed).toBeCloseTo(0.5)
    expect(t.at(start + STAY_S).hold).toBeNull()
    expect(t.at(start + STAY_S).clock).toBeCloseTo(500)
    expect(t.at(start + STAY_S + 1).clock).toBeCloseTo(600)
  })

  it('lands a plain seek after the stay, and a jump to a sign before any hold there', () => {
    const t = time([{ clock: 500, id: 'stay:0' }], [{ clock: 500, id: 'sign' }])
    // The sign's moment comes first, then the stay.
    expect(t.holds.map((h) => h.id)).toEqual(['sign', 'stay:0'])
    expect(t.wallAtClock(500)).toBeCloseTo(5 + 2.3 + STAY_S)
    expect(t.wallAtClock(500, { onSign: true })).toBeCloseTo(5)
    expect(t.wallAtClock(400)).toBeCloseTo(4)
    expect(t.wallAtClock(1000)).toBeCloseTo(t.totalWall)
  })

  it('ends the hold it is in, and only that one', () => {
    const t = time()
    expect(t.holdEnd(5.5)).toBeCloseTo(5 + STAY_S)
    expect(t.holdEnd(3)).toBe(3)
    expect(t.holdEnd(8)).toBe(8)
  })

  it('does not change a replay with no stays', () => {
    const none = buildReplayTime({ total: 1000, lengthS: 10, spans: [], moments: [] })
    expect(none.holds).toEqual([])
    expect(none.totalWall).toBeCloseTo(10)
    expect(none.at(3)).toEqual({ clock: 300, hold: null })
  })
})
