import { describe, expect, it } from 'vitest'

import { localDaysSince } from '@/lib/boat-nights'
import { buildTimeline, type TimedSegment } from '@/lib/passages'
import { cruisePaceSpans } from '@/lib/replay-clock'
import { ReplayController, type FrameInput, type ReplayConfig } from '@/lib/replay-controller'
import { PASSAGE_LABEL_S, passageLabelBox, passageLabelText, passageLabelView, passageLabelsOf } from '@/lib/replay-passage-label'
import { stayLabelBox, staysOf, type StayPassage } from '@/lib/replay-stay'
import { STAY_S, buildReplayTime } from '@/lib/replay-time'

const BRISBANE = 'Australia/Brisbane' // UTC+10, no daylight saving
const HOUR = 3600
const DAY = 86400
// 2026-10-24 00:00 in Brisbane.
const MIDNIGHT = Date.UTC(2026, 9, 23, 14, 0) / 1000

describe('passageLabelText', () => {
  it('is the day and the route', () => {
    expect(passageLabelText(3, 'Funnel Bay', 'Thomas Island')).toBe('Day 3 · Funnel Bay → Thomas Island')
  })
  it('gives what is there when an end has no name', () => {
    expect(passageLabelText(3, 'Funnel Bay', '')).toBe('Day 3 · from Funnel Bay')
    expect(passageLabelText(3, '  ', 'Thomas Island')).toBe('Day 3 · to Thomas Island')
  })
  it('is the day alone when neither has a name', () => {
    expect(passageLabelText(1, '', ' ')).toBe('Day 1')
  })
})

describe('passageLabelsOf', () => {
  const entry = (id: string, start: number) => ({ id, start, end: start + 100, segments: [] as TimedSegment[] })
  const named = (id: string, from: string, to: string, timezone: string | null = BRISBANE): StayPassage => ({ id, from_name: from, to_name: to, timezone })

  it('counts days by the boat\'s own dates from the first departure, across the boat\'s midnight, not UTC\'s', () => {
    // Brisbane 22:00 on the 24th is 12:00 UTC on the 24th. The next leaves at 01:00 on the 25th, which is 15:00 UTC on the 24th: the same UTC date, the next day on the boat.
    const timeline = buildTimeline([entry('a', MIDNIGHT + 22 * HOUR), entry('b', MIDNIGHT + 25 * HOUR), entry('c', MIDNIGHT + 25 * HOUR + 2 * DAY)])
    const labels = passageLabelsOf(timeline, [named('a', 'Funnel Bay', 'X'), named('b', 'X', 'Y'), named('c', '', '')])
        expect(labels.map((l) => l.text)).toEqual(['Day 1 · Funnel Bay → X', 'Day 2 · X → Y', 'Day 4'])
  })

  it('gives a passage that sets out at the boat\'s midnight the new day', () => {
    const timeline = buildTimeline([entry('a', MIDNIGHT + 9 * HOUR), entry('b', MIDNIGHT + DAY)])
    expect(passageLabelsOf(timeline, [named('a', '', ''), named('b', '', '')]).map((l) => l.text)).toEqual(['Day 1', 'Day 2'])
  })

  it('counts several passages on one day as that day', () => {
    const timeline = buildTimeline([entry('a', MIDNIGHT + 7 * HOUR), entry('b', MIDNIGHT + 14 * HOUR)])
    expect(passageLabelsOf(timeline, [named('a', '', ''), named('b', '', '')]).map((l) => l.text)).toEqual(['Day 1', 'Day 1'])
  })

  it('counts days the way the cruise page counts nights: the same dates', () => {
    expect(localDaysSince(MIDNIGHT + 22 * HOUR, MIDNIGHT + 25 * HOUR, 10)).toBe(1)
    expect(localDaysSince(MIDNIGHT + 22 * HOUR, MIDNIGHT + 22 * HOUR, 10)).toBe(0)
  })

  it('has none for a single passage', () => {
    expect(passageLabelsOf(buildTimeline([entry('a', MIDNIGHT)]), [named('a', 'A', 'B')])).toEqual([])
  })

  it('has none when the boat\'s zone is not known, rather than guess at days', () => {
    const timeline = buildTimeline([entry('a', MIDNIGHT), entry('b', MIDNIGHT + DAY)])
    expect(passageLabelsOf(timeline, [named('a', '', '', null), named('b', '', '')])).toEqual([])
  })
})

describe('passageLabelView', () => {
  const label = { id: 'a', text: 'Day 1' }
  it('is up for two seconds from the start', () => {
    expect(passageLabelView(label, -0.01)).toBeNull()
    expect(passageLabelView(label, 0)).toMatchObject({ elapsed: 0 })
    expect(passageLabelView(label, PASSAGE_LABEL_S - 0.01)).not.toBeNull()
    expect(passageLabelView(label, PASSAGE_LABEL_S)).toBeNull()
    expect(PASSAGE_LABEL_S).toBe(2)
  })
})

describe('passageLabelBox', () => {
  const frame = { w: 1280, h: 720 }
  it('sits low and to the left, on the line the stay\'s label is on', () => {
    const b = passageLabelBox(frame, 1)
    expect(b.x).toBe(24)
    expect(b.y).toBe(stayLabelBox(frame, 0.75).y)
    expect(b.fontPx).toBe(stayLabelBox(frame, 0.75).fontPx)
    expect(b.alpha).toBe(1)
  })
  it('fades in and out over its two seconds', () => {
    expect(passageLabelBox(frame, 0).alpha).toBe(0)
    expect(passageLabelBox(frame, 0.15).alpha).toBeCloseTo(0.5)
    expect(passageLabelBox(frame, PASSAGE_LABEL_S - 0.15).alpha).toBeCloseTo(0.5)
    expect(passageLabelBox(frame, PASSAGE_LABEL_S).alpha).toBe(0)
  })
  it('keeps clear of the overview map in the bottom-left corner of a narrow map', () => {
    const narrow = { w: 400, h: 600 }
    const b = passageLabelBox(narrow, 1, 'bottom-left')
    expect(b.x).toBeGreaterThanOrEqual(8 + 176)
    expect(b.x + b.maxW).toBeLessThanOrEqual(narrow.w - 24)
    expect(b.maxW).toBeGreaterThan(0)
  })
  it('keeps clear of it in the bottom-right corner, and does not move for a top one', () => {
    const narrow = { w: 400, h: 600 }
    const right = passageLabelBox(narrow, 1, 'bottom-right')
    expect(right.x + right.maxW).toBeLessThanOrEqual(narrow.w - 8 - 176)
    expect(passageLabelBox(narrow, 1, 'top-left')).toEqual(passageLabelBox(narrow, 1, null))
  })
})

// ── in the controller's frame ────────────────────────────────────────────

const T1 = MIDNIGHT + 8 * HOUR
const T2 = T1 + 1.5 * DAY
const T3 = T2 + 1.5 * DAY
const seg = (t: number, lat: number): TimedSegment => Array.from({ length: 6 }, (_, i) => ({ t: t + i * 100, lat, lon: 149 + i * 0.01 })) as TimedSegment
const timeline = buildTimeline([
  { id: 'p1', start: T1, end: T1 + 500, segments: [seg(T1, -20)] },
  { id: 'p2', start: T2, end: T2 + 500, segments: [seg(T2, -20.2)] },
  { id: 'p3', start: T3, end: T3 + 500, segments: [seg(T3, -20.4)] },
])
const passages: StayPassage[] = [
  { id: 'p1', from_name: 'Funnel Bay', to_name: 'Thomas Island', timezone: BRISBANE },
  { id: 'p2', from_name: 'Thomas Island', to_name: '', timezone: BRISBANE },
  { id: 'p3', from_name: '', to_name: '', timezone: BRISBANE },
]
const stays = staysOf(timeline, passages)
const passageLabels = passageLabelsOf(timeline, passages)
const pieces = timeline.entries.map((e, i) => ({ key: e.id, passageId: e.id, segments: e.segments, colourIndex: i, from: -Infinity, to: Infinity }))
const time = buildReplayTime({ total: timeline.total, lengthS: 30, spans: cruisePaceSpans([], timeline, 30), moments: [], stays })
const config: ReplayConfig = { timeline, time, stays, passageLabels, resolved: [], signs: [], pieces, isDarkTheme: false }
const input = (wall: number): FrameInput => ({ wall, nowMs: 1000, playing: true, resume: 0, reduced: false, wantHeading: true, view: { zoom: 12 } })
const frame = (wall: number) => new ReplayController().step(config, input(wall))
const startOf = (id: string) => time.wallAtClock(timeline.entries.find((e) => e.id === id)!.offset)
const stayHold = (i: number) => time.holds.filter((h) => h.kind === 'stay')[i]

describe('the passage label in the frame', () => {
  it('is named for each passage, from the day it set out', () => {
    // 08:00 on the 24th, 20:00 on the 25th, 08:00 on the 27th.
    expect(passageLabels.map((l) => l.text)).toEqual(['Day 1 \u00b7 Funnel Bay \u2192 Thomas Island', 'Day 2 \u00b7 from Thomas Island', 'Day 4'])
  })

  it('is up for two seconds as the first passage begins, and not before the replay moves off the start', () => {
    expect(startOf('p1')).toBe(0)
    expect(frame(0).passageLabel).toBeNull()
    expect(frame(0.5).passageLabel).toMatchObject({ elapsed: 0.5, label: { id: 'p1' } })
    expect(frame(PASSAGE_LABEL_S - 0.01).passageLabel).not.toBeNull()
    expect(frame(PASSAGE_LABEL_S + 0.01).passageLabel).toBeNull()
  })

  it('is up again for the next passage once the stay is over', () => {
    const start = startOf('p2')
    expect(start).toBeCloseTo(stayHold(0).startWall + STAY_S)
    expect(frame(start - 0.01).passageLabel).toBeNull()
    expect(frame(start + 1).passageLabel).toMatchObject({ elapsed: 1, label: { id: 'p2' } })
    expect(frame(start + PASSAGE_LABEL_S + 0.01).passageLabel).toBeNull()
  })

  it('holds nothing up: the clock moves while it is up', () => {
    const start = startOf('p2')
    const a = frame(start + 0.5)
    const b = frame(start + 1.5)
    expect(a.passageLabel).not.toBeNull()
    expect(b.passageLabel).not.toBeNull()
    expect(a.hold).toBeNull()
    expect(time.at(start + 1.5).clock).toBeGreaterThan(time.at(start + 0.5).clock)
  })

  it('is never up with the stay\'s label, nor at the edges of their fades', () => {
    for (const [i, id] of [[0, 'p2'], [1, 'p3']] as const) {
      const stayEnd = stayHold(i).startWall + STAY_S
      expect(startOf(id)).toBeGreaterThanOrEqual(stayEnd)
      // Through the stay no passage label, and through the passage label no stay.
      for (let w = stayHold(i).startWall; w < stayEnd; w += 0.05) expect(frame(w).passageLabel).toBeNull()
      for (let w = startOf(id); w < startOf(id) + PASSAGE_LABEL_S; w += 0.05) expect(frame(w).stay).toBeNull()
      // At the seam the stay's label has faded to nothing, and the next one starts from nothing.
      expect(stayLabelBox({ w: 1000, h: 600 }, STAY_S).alpha).toBe(0)
      expect(passageLabelBox({ w: 1000, h: 600 }, 0).alpha).toBe(0)
    }
  })

  it('is not there in a replay of one passage', () => {
    const one = buildTimeline([{ id: 'p1', start: T1, end: T1 + 500, segments: [seg(T1, -20)] }])
    const cfg: ReplayConfig = { timeline: one, time: buildReplayTime({ total: one.total, lengthS: 15, spans: [], moments: [] }), passageLabels: passageLabelsOf(one, passages), resolved: [], signs: [], pieces: [pieces[0]], isDarkTheme: false }
    expect(new ReplayController().step(cfg, input(0.5)).passageLabel).toBeNull()
  })
})
