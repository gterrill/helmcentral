import { describe, expect, it } from 'vitest'

import { buildTimeline, positionAt, type TimedSegment } from '@/lib/passages'
import { cruisePaceSpans } from '@/lib/replay-clock'
import { ReplayController, ReplayPlayer, type FrameInput, type ReplayConfig } from '@/lib/replay-controller'
import { momentsOf, resolveSigns, type ReplaySign } from '@/lib/replay-signs'
import { staysOf } from '@/lib/replay-stay'
import { MOMENT_TOTAL_S, STAY_S, buildReplayTime } from '@/lib/replay-time'

// Two passages with a night between. A sign stands at the very end of the first and another at the very start of the second.
const T1 = Date.UTC(2026, 9, 24, 6, 0) / 1000
const T2 = T1 + 1.5 * 86400
const east: TimedSegment = Array.from({ length: 11 }, (_, i) => ({ t: T1 + i * 100, lat: -20, lon: 149 + i * 0.01 })) as TimedSegment
const north: TimedSegment = Array.from({ length: 6 }, (_, i) => ({ t: T2 + i * 100, lat: -20.5 + i * 0.01, lon: 149.5 })) as TimedSegment
const timeline = buildTimeline([
  { id: 'p1', start: T1, end: T1 + 1000, segments: [east] },
  { id: 'p2', start: T2, end: T2 + 500, segments: [north] },
])
const PIECES = [
  { key: 'p1', passageId: 'p1', segments: [east], colourIndex: 0, from: -Infinity, to: Infinity },
  { key: 'p2', passageId: 'p2', segments: [north], colourIndex: 1, from: -Infinity, to: Infinity },
]
const stays = staysOf(timeline, [
  { id: 'p1', from_name: 'A', to_name: 'Blue Pearl Bay', timezone: 'Australia/Brisbane' },
  { id: 'p2', from_name: 'B', to_name: 'C', timezone: 'Australia/Brisbane' },
])
const sign = (id: string, at: number): ReplaySign => ({
  id, t_ms: at * 1000, title: id, note: '', document_id: 'd', photo_url: '/p', visibility: 'passing', show_s: 4, starts_leg: false, photo_moment: null,
})
const signs = [sign('dep', T2), sign('arr', T1 + 1000)]
const placed = resolveSigns(signs, timeline)
const time = buildReplayTime({ total: timeline.total, lengthS: 15, spans: cruisePaceSpans([], timeline, 15), moments: momentsOf(placed), stays })
const config: ReplayConfig = { timeline, time, stays, resolved: [], signs: placed, pieces: PIECES, isDarkTheme: false }
const input = (wall: number): FrameInput => ({ wall, nowMs: 1000, playing: true, resume: 0, reduced: false, wantHeading: true, view: { zoom: 12 } })
const hold = (id: string) => time.holds.find((h) => h.id === id)!
const frame = (wall: number) => new ReplayController().step(config, input(wall))

describe('signs at the join of two passages', () => {
  it('both stand at the join\'s clock', () => {
    expect(placed.map((r) => [r.sign.id, r.entryId, r.clock])).toEqual([['arr', 'p1', timeline.entries[1].offset], ['dep', 'p2', timeline.entries[1].offset]])
  })

  it('play in the order they happened: the arrival sign, the stay, the departure sign', () => {
    expect(time.holds.map((h) => h.id)).toEqual(['arr', stays[0].id, 'dep'])
    expect(time.holds.map((h) => h.kind)).toEqual(['moment', 'stay', 'moment'])
    expect(hold('arr').startWall + MOMENT_TOTAL_S).toBeCloseTo(hold(stays[0].id).startWall)
    expect(hold(stays[0].id).startWall + STAY_S).toBeCloseTo(hold('dep').startWall)
  })

  it('keeps that order whichever way the signs are given', () => {
    const swapped = buildReplayTime({
      total: timeline.total, lengthS: 15, spans: [], stays,
      moments: [...momentsOf(placed)].reverse(),
    })
    expect(swapped.holds.map((h) => h.id)).toEqual(['arr', stays[0].id, 'dep'])
  })

  it('shows the arrival sign\'s moment at the arrival: the end of the first passage, in its light', () => {
    const f = frame(hold('arr').startWall + 0.5)
    expect(f.moment).toMatchObject({ signId: 'arr' })
    expect(f.here!.entry.id).toBe('p1')
    expect(f.here!.at).toBe(T1 + 1000)
    expect(f.boat).toEqual(positionAt([east], T1 + 1000))
    expect(f.stay).toBeNull()
  })

  it('shows the departure sign\'s moment at the departure: the start of the second passage', () => {
    const f = frame(hold('dep').startWall + 0.5)
    expect(f.moment).toMatchObject({ signId: 'dep' })
    expect(f.here!.entry.id).toBe('p2')
    expect(f.here!.at).toBe(T2)
    expect(f.boat!.lat).toBeCloseTo(-20.5, 3)
    expect(f.boat!.lon).toBeCloseTo(149.5, 3)
  })

  it('lights each at its own time: the day the boat arrived, then the day it left', () => {
    const arrival = frame(hold('arr').startWall + 0.1).light
    const departure = frame(hold('dep').startWall + 0.1).light
    expect(arrival).not.toBeNull()
    expect(departure).not.toBeNull()
    // Six in the morning at Brisbane's longitude is not the same sky at two in the afternoon, a day and a half on.
    expect(arrival!.key).not.toBe(departure!.key)
  })
})

describe('jumping to a sign at the join', () => {
  it('lands a jump to the departure sign on its moment, after the stay', () => {
    const wall = time.wallAtClock(placed[1].clock, { onSign: true, at: placed[1].at })
    expect(wall).toBeCloseTo(hold('dep').startWall)
    expect(time.at(wall).hold).toMatchObject({ kind: 'moment', id: 'dep' })
  })

  it('lands a jump to the arrival sign on its moment, before the stay', () => {
    const wall = time.wallAtClock(placed[0].clock, { onSign: true, at: placed[0].at })
    expect(wall).toBeCloseTo(hold('arr').startWall)
    expect(time.at(wall).hold).toMatchObject({ kind: 'moment', id: 'arr' })
  })

  it('never plays the stay when the player jumps to the departure sign and goes on', () => {
    const p = new ReplayPlayer(time)
    p.seek(placed[1].clock, { onSign: true, at: placed[1].at })
    p.play(0)
    const seen = new Set<string>()
    for (let t = 0; t <= 20_000; t += 100) {
      p.tick(t)
      if (p.hold) seen.add(p.hold.id)
    }
    expect([...seen]).toEqual(['dep'])
  })

  it('without the sign\'s time, a jump lands before every hold at that clock, as it did', () => {
    expect(time.wallAtClock(placed[1].clock, { onSign: true })).toBeCloseTo(hold('arr').startWall)
  })
})
