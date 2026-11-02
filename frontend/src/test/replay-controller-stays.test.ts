import { describe, expect, it } from 'vitest'

import { cruisePaceSpans } from '@/lib/replay-clock'
import { ReplayController, ReplayPlayer, type FrameInput, type ReplayConfig } from '@/lib/replay-controller'
import { buildTimeline, positionAt, type TimedSegment } from '@/lib/passages'
import { staySunTime, staysOf } from '@/lib/replay-stay'
import { STAY_S, buildReplayTime } from '@/lib/replay-time'
import { sunPosition } from '@/lib/sun'

// Two passages, a day and a half apart: 1000 s heading east, then 500 s heading north.
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
const time = buildReplayTime({ total: timeline.total, lengthS: 15, spans: cruisePaceSpans([], timeline, 15), moments: [], stays })
const config: ReplayConfig = { timeline, time, stays, resolved: [], signs: [], pieces: PIECES, isDarkTheme: false }
const input = (wall: number, extra: Partial<FrameInput> = {}): FrameInput => ({
  wall, nowMs: 1000, playing: true, resume: 0, reduced: false, wantHeading: true, view: { zoom: 12 }, ...extra,
})
const stayStart = time.holds.find((h) => h.kind === 'stay')!.startWall
const frame = (wall: number) => new ReplayController().step(config, input(wall))

describe('a stay in the controller\'s frame', () => {
  it('says which stay it is, between which passages, when, and how far through', () => {
    const f = frame(stayStart + STAY_S / 2)
    expect(f.stay!.stay).toMatchObject({ index: 0, fromId: 'p1', toId: 'p2', arrival: T1 + 1000, departure: T2, label: '2 nights · Blue Pearl Bay' })
    expect(f.stay!.progress).toBeCloseTo(0.5)
  })

  it('has no stay before it or after it', () => {
    expect(frame(stayStart - 0.1).stay).toBeNull()
    expect(frame(stayStart + STAY_S + 0.1).stay).toBeNull()
    expect(frame(0.5).stay).toBeNull()
  })

  it('holds the timeline at the join and the boat where the passage before it ended', () => {
    const early = frame(stayStart + 0.1)
    const late = frame(stayStart + STAY_S - 0.1)
    expect(early.boat).toEqual(late.boat)
    expect(early.boat).toEqual(positionAt([east], T1 + 1000))
    expect(early.here!.entry.id).toBe('p1')
    // And next to it, the first frame past the stay has the boat at the next departure.
    const after = frame(stayStart + STAY_S + 0.001)
    expect(after.here!.entry.id).toBe('p2')
    expect(after.boat!.lat).toBeCloseTo(-20.5, 3)
    expect(after.boat!.lon).toBeCloseTo(149.5, 3)
  })

  it('shows the whole of the passage before it as sailed, and none of the next', () => {
    const f = frame(stayStart + 0.5)
    expect(f.sailedTo!.get('p1')!.at).toBe(T1 + 1000)
    expect(f.sailedTo!.get('p2')!.at).toBe(T2)
  })

  it('runs the light on the stay\'s own clock, not on the track\'s', () => {
    const start = frame(stayStart + 0.001)
    const end = frame(stayStart + STAY_S - 0.001)
    const lit = (t: number) => sunPosition(t * 1000, -20, 149.1)
    // The sun the light is made from, at the arrival and at the departure.
    expect(start.stay!.sunAt).toBeCloseTo(staySunTime(T1 + 1000, T2, 0.001 / STAY_S), 3)
    expect(end.stay!.sunAt).toBeCloseTo(staySunTime(T1 + 1000, T2, 1 - 0.001 / STAY_S), 3)
    expect(start.light).not.toBeNull()
    // A day and a half on, the light is not the same: the night between shows.
    const mid = frame(stayStart + STAY_S / 2)
    expect(mid.light!.key).not.toBe(start.light!.key)
    expect(lit(T2).altitudeDeg).not.toBeCloseTo(lit(T1 + 1000).altitudeDeg, 0)
  })

  it('is the same stay when asked again, whatever came before', () => {
    const c = new ReplayController()
    c.step(config, input(1))
    const a = c.step(config, input(stayStart + 0.7))
    const b = new ReplayController().step(config, input(stayStart + 0.7))
    expect(a.stay).toEqual(b.stay)
    expect(a.boat).toEqual(b.boat)
  })

  it('holds the camera still, as a photo moment does', () => {
    const c = new ReplayController()
    const f = c.step(config, input(stayStart + 0.5))
    expect(f.stay).not.toBeNull()
    const cam = c.cameraAt({ flat: false, overview: { center: [149.5, -20], zoom: 10, pitch: 0, bearing: 0 }, current: { center: [149.5, -20], zoom: 10, pitch: 0, bearing: 0 } }, 1000)
    // No shots: nothing to place, and no throw.
    expect(cam).toBeNull()
  })
})

describe('the player in a stay', () => {
  it('plays through it, and skips to its end', () => {
    const p = new ReplayPlayer(time)
    p.wall = stayStart + 0.3
    expect(p.hold).toMatchObject({ kind: 'stay', id: 'stay:p1>p2' })
    p.skipMoment()
    expect(p.wall).toBeCloseTo(stayStart + STAY_S)
    expect(p.hold).toBeNull()
  })

  it('lands a seek past the join after the stay', () => {
    const p = new ReplayPlayer(time)
    p.seek(timeline.entries[1].offset)
    expect(p.wall).toBeCloseTo(stayStart + STAY_S)
    expect(p.hold).toBeNull()
  })

  it('keeps its place in a stay when the timing changes under it', () => {
    const p = new ReplayPlayer(time)
    p.wall = stayStart + 0.6
    const longer = buildReplayTime({ total: timeline.total, lengthS: 30, spans: cruisePaceSpans([], timeline, 30), moments: [], stays })
    p.setTime(longer, 0)
    expect(p.hold).toMatchObject({ kind: 'stay', id: 'stay:p1>p2' })
    expect(p.hold!.elapsed).toBeCloseTo(0.6)
  })

  it('does not step over a stay when the timing changes while the boat stands at its join, after it', () => {
    const p = new ReplayPlayer(time)
    p.seek(timeline.entries[1].offset)
    const longer = buildReplayTime({ total: timeline.total, lengthS: 30, spans: cruisePaceSpans([], timeline, 30), moments: [], stays })
    p.setTime(longer, 0)
    expect(p.clock).toBeCloseTo(timeline.entries[1].offset)
    expect(p.hold).toBeNull()
  })
})

describe('a stay keeps its place when the passages shown change under it', () => {
  // Four passages of 100 s a day apart. One cruise shows p1 to p3; hiding the first shows p2 to p4. Both are 300 s long.
  const DAY = 86400
  const seg = (t: number): TimedSegment => [{ t, lat: -20, lon: 149 }, { t: t + 100, lat: -20, lon: 149.01 }] as TimedSegment
  const starts = { p1: T1, p2: T1 + DAY, p3: T1 + 2 * DAY, p4: T1 + 3 * DAY }
  const entry = (id: keyof typeof starts) => ({ id, start: starts[id], end: starts[id] + 100, segments: [seg(starts[id])] })
  const named = (id: string, to: string) => ({ id, from_name: '', to_name: to, timezone: 'Australia/Brisbane' })
  const names = [named('p1', 'North Bay'), named('p2', 'Blue Pearl Bay'), named('p3', 'Dumbell Island'), named('p4', 'Hamilton')]
  const make = (ids: (keyof typeof starts)[]) => {
    const tl = buildTimeline(ids.map(entry))
    const st = staysOf(tl, names)
    return { tl, st, time: buildReplayTime({ total: tl.total, lengthS: 15, spans: cruisePaceSpans([], tl, 15), moments: [], stays: st }) }
  }

  it('names a stay for the two passages either side of it', () => {
    expect(make(['p1', 'p2', 'p3']).st.map((s) => s.id)).toEqual(['stay:p1>p2', 'stay:p2>p3'])
    expect(make(['p2', 'p3', 'p4']).st.map((s) => s.id)).toEqual(['stay:p2>p3', 'stay:p3>p4'])
  })

  it('resumes in the same stay, at the same anchorage, when the others change', () => {
    const before = make(['p1', 'p2', 'p3'])
    const after = make(['p2', 'p3', 'p4'])
    expect(after.time.totalWall).toBeCloseTo(before.time.totalWall)
    const p = new ReplayPlayer(before.time)
    p.wall = before.time.holds.find((h) => h.id === 'stay:p2>p3')!.startWall + 0.5
    p.setTime(after.time, 0)
    expect(p.hold).toMatchObject({ kind: 'stay', id: 'stay:p2>p3' })
    expect(p.hold!.elapsed).toBeCloseTo(0.5)
  })

  it('does not resume in another anchorage\'s stay when its own is gone', () => {
    const before = make(['p1', 'p2', 'p3'])
    const after = make(['p2', 'p3', 'p4'])
    const p = new ReplayPlayer(before.time)
    p.wall = before.time.holds.find((h) => h.id === 'stay:p1>p2')!.startWall + 0.5
    p.setTime(after.time, 0)
    // It may land at the start of the stay now at that place on the timeline, but never part-way through one.
    expect(p.hold?.elapsed ?? 0).toBeCloseTo(0)
  })
})
