import { describe, expect, it } from 'vitest'

import { prepareVideoReplay, type PassageVideoInput } from '@/lib/passage-video/setup'
import { buildTimeline, type PassageDetail, type TimedSegment } from '@/lib/passages'
import { TRANSITION_S, CameraDirector, blendCameras, easeInOut, type Camera, type CameraContext } from '@/lib/replay-camera'
import { cruisePaceSpans, resolveShots, type CameraShot } from '@/lib/replay-clock'
import { ReplayController, type FrameInput, type ReplayConfig } from '@/lib/replay-controller'
import { cruiseCamera, cruiseFramingFor, passageBoundsOf, type CruiseFraming, type FitMap } from '@/lib/replay-framing'
import { planReplay } from '@/lib/replay-plan'
import { staysOf } from '@/lib/replay-stay'
import { STAY_S, buildReplayTime } from '@/lib/replay-time'

const A: Camera = { center: [148.25, -20.05], zoom: 11, pitch: 50, bearing: 0 }
const B: Camera = { center: [149.3, -20.5], zoom: 11.5, pitch: 50, bearing: 0 }
const C: Camera = { center: [149.9, -20.9], zoom: 12, pitch: 50, bearing: 0 }
const O: Camera = { center: [148.8, -20.3], zoom: 9, pitch: 50, bearing: 0 }
const framing: CruiseFraming = { overview: O, passages: new Map([['p1', A], ['p2', B], ['p3', C]]) }
const env = { flat: false, overview: O, passages: framing.passages, current: O }

const DAY = 86400
const T1 = Date.UTC(2026, 9, 24, 6, 0) / 1000
const seg = (t: number, lat: number, lon: number): TimedSegment => Array.from({ length: 11 }, (_, i) => ({ t: t + i * 100, lat, lon: lon + i * 0.01 })) as TimedSegment
const names = (ids: string[]) => ids.map((id) => ({ id, from_name: '', to_name: '', timezone: 'Australia/Brisbane' }))
const shot = (id: string, at: number, extra: Partial<CameraShot> = {}): CameraShot => ({
  id, t_ms: at * 1000, mode: 'follow', zoom: 15, pitch: 55, bearing_offset_deg: 0, orbit_deg_per_s: 0, pace: 1, ...extra,
})

/** Passages of 1000 s starting at the given real times. */
function cruise(starts: number[], shots: CameraShot[] = []) {
  const ids = starts.map((_, i) => `p${i + 1}`)
  const segs = starts.map((t, i) => seg(t, -20 - i * 0.2, 148 + i * 0.5))
  const timeline = buildTimeline(starts.map((t, i) => ({ id: ids[i], start: t, end: t + 1000, segments: [segs[i]] })))
  const stays = staysOf(timeline, names(ids))
  const resolved = resolveShots(shots, timeline)
  const time = buildReplayTime({ total: timeline.total, lengthS: 30, spans: cruisePaceSpans(resolved, timeline, 30), moments: [], stays })
  const pieces = ids.map((id, i) => ({ key: id, passageId: id, segments: [segs[i]], colourIndex: i, from: -Infinity, to: Infinity }))
  const config: ReplayConfig = { timeline, time, stays, resolved, signs: [], pieces, isDarkTheme: false }
  return { config, timeline, time, stays }
}
const input = (wall: number, extra: Partial<FrameInput> = {}): FrameInput => ({
  wall, nowMs: 1000 + wall * 1000, playing: true, resume: 0, reduced: false, wantHeading: true, view: { zoom: 12 }, ...extra,
})
/** Steps the controller through `walls` in order, as a playing replay would, and gives the camera at each. */
function play(config: ReplayConfig, walls: number[], extra: Partial<FrameInput> = {}) {
  const c = new ReplayController()
  return walls.map((wall) => {
    c.step(config, input(wall, extra))
    return c.cameraAt(env, 1000 + wall * 1000)
  })
}
const range = (from: number, to: number, step: number) => Array.from({ length: Math.round((to - from) / step) + 1 }, (_, i) => from + i * step)
const close = (a: Camera, b: Camera, digits = 5) => {
  expect(a.zoom).toBeCloseTo(b.zoom, digits)
  expect(a.center[0]).toBeCloseTo(b.center[0], digits)
  expect(a.center[1]).toBeCloseTo(b.center[1], digits)
}

describe('1. a stay pulls out from the camera that is on screen', () => {
  it('starts from a follow shot that runs to the end of the passage, not from the passage\'s own framing', () => {
    const { config, time } = cruise([T1, T1 + 3 * DAY], [shot('f', T1 + 100)])
    const s = time.holds.find((h) => h.kind === 'stay')!.startWall
    const before = play(config, range(0, s - 0.05, 0.05)).pop()!
    expect(before.shotKey).toBe('f')
    const [first, a, b] = play(config, [...range(0, s - 0.05, 0.05), s + 0.001, s + STAY_S / 6, s + STAY_S / 3 - 0.001]).slice(-3)
    // Zoom 15 in the follow shot against 11 on the passage: the pull-out begins at 15.
    expect(first!.camera.zoom).toBeCloseTo(15, 1)
    expect(first!.shotKey).toBe('stay:p1>p2')
    const want = blendCameras(before.camera, O, easeInOut(0.5))
    expect(a!.camera.zoom).toBeCloseTo(want.zoom, 1)
    expect(a!.camera.zoom).toBeGreaterThan(O.zoom)
    expect(b!.camera.zoom).toBeCloseTo(O.zoom, 0)
  })

  it('starts from the passage\'s own framing when no shot was in force', () => {
    const { config, time } = cruise([T1, T1 + 3 * DAY])
    const s = time.holds.find((h) => h.kind === 'stay')!.startWall
    const first = play(config, [...range(0, s - 0.05, 0.05), s + 0.001]).pop()!
    close(first!.camera, A, 2)
  })

  it('is the same pull-out as a pure stay camera given that camera', () => {
    const from: Camera = { center: [148.4, -20.1], zoom: 15, pitch: 55, bearing: 30 }
    expect(cruiseCamera(framing, { passageId: 'p1', stay: { fromId: 'p1', toId: 'p2', progress: 0 } }, false, from)!.camera).toEqual(from)
    expect(cruiseCamera(framing, { passageId: 'p1', stay: { fromId: 'p1', toId: 'p2', progress: 0.5 } }, false, from)!.camera).toEqual(O)
  })
})

describe('2. every change of framing is eased', () => {
  it('eases from one passage to the next when no stay comes between (they ran back to back)', () => {
    // The second sets out 30 s after the first ends: under a minute, so no stay.
    const { config, time, stays } = cruise([T1, T1 + 1030])
    expect(stays).toHaveLength(0)
    const boundary = time.wallAtClock(config.timeline.entries[1].offset)
    const out = play(config, range(boundary - 0.5, boundary + 2, 0.05))
    const at = (dt: number) => out[Math.round((dt + 0.5) / 0.05)]!
    close(at(-0.05).camera, A, 3)
    // Just over the boundary it is still near the first passage, halfway through it is between, and by the end of the ease it is on the second.
    expect(at(0.05).camera.zoom).toBeLessThan(11.1)
    expect(at(0.75).camera.zoom).toBeGreaterThan(11.1)
    expect(at(0.75).camera.zoom).toBeLessThan(11.4)
    close(at(TRANSITION_S + 0.05).camera, B, 3)
  })

  it('eases from a shot that ends at a passage boundary to the next passage\'s framing', () => {
    const { config, time } = cruise([T1, T1 + 1030], [shot('f', T1 + 100)])
    const boundary = time.wallAtClock(config.timeline.entries[1].offset)
    // Long enough from the start that the shot is settled before the boundary.
    const walls = [...range(0, boundary - 2, 0.1), ...Array.from({ length: 81 }, (_, k) => boundary + (k - 40) * 0.05)]
    const out = play(config, walls)
    const at = (dt: number) => out[walls.length - 81 + Math.round(dt / 0.05) + 40]!
    expect(at(-0.05).shotKey).toBe('f')
    // The shot's zoom, then toward the second passage's rather than straight to it.
    expect(at(0.05).camera.zoom).toBeGreaterThan(14)
    expect(at(0.75).camera.zoom).toBeLessThan(14.5)
    expect(at(0.75).camera.zoom).toBeGreaterThan(B.zoom)
    close(at(TRANSITION_S + 0.05).camera, B, 3)
  })

  it('cuts when the replay is scrubbed, not playing', () => {
    const { config, time } = cruise([T1, T1 + 1030])
    const boundary = time.wallAtClock(config.timeline.entries[1].offset)
    const c = new ReplayController()
    c.step(config, input(boundary - 1, { playing: false }))
    c.cameraAt(env, 1)
    c.step(config, input(boundary + 1, { playing: false }))
    close(c.cameraAt(env, 2)!.camera, B)
  })
})

describe('3. a passage with no track', () => {
  const bounds: [number, number, number, number] = [148, -20.6, 149.6, -20]
  const map: FitMap = {
    getContainer: () => ({ clientWidth: 1920, clientHeight: 1080 }),
    cameraForBounds: (b) => ({ center: { lng: (b[0][0] + b[1][0]) / 2, lat: (b[0][1] + b[1][1]) / 2 }, zoom: 10 }),
  }

  it('is left out of the framing, and the rest are framed', () => {
    const f = cruiseFramingFor(map, bounds, [{ id: 'p1', bounds: [148, -20.1, 148.5, -20] }, { id: 'p2', bounds: null }, { id: 'p3', bounds: [149, -20.6, 149.6, -20.4] }], 50)!
    expect(f).not.toBeNull()
    expect([...f.passages.keys()]).toEqual(['p1', 'p3'])
  })

  it('is framed on the whole cruise while it plays, and the stay around it still works', () => {
    const partial: CruiseFraming = { overview: O, passages: new Map([['p1', A], ['p3', C]]) }
    expect(cruiseCamera(partial, { passageId: 'p2', stay: null }, false)).toEqual({ camera: O, key: 'passage:p2' })
    const into = cruiseCamera(partial, { passageId: 'p1', stay: { fromId: 'p1', toId: 'p2', progress: 1 } }, false)!
    expect(into.camera).toEqual(O)
    const out = cruiseCamera(partial, { passageId: 'p2', stay: { fromId: 'p2', toId: 'p3', progress: 0 } }, false)!
    expect(out.camera).toEqual(O)
  })

  it('does not stop a video of a cruise from being set up', () => {
    const base = (id: string, at: number, track: boolean) => ({
      id, start: new Date((T1 + at) * 1000).toISOString(), end: new Date((T1 + at + 1000) * 1000).toISOString(),
      from_name: '', to_name: '', title: '', notes: '', hidden: false, edited: false, cruise_id: null, created_at: '', stats: {}, timezone: 'Australia/Brisbane',
      track: track ? [[[-20, 149], [-20, 149.1]]] : [], track_times: track ? [[T1 + at, T1 + at + 1000]] : [],
      replay_track: track ? [[[-20, 149], [-20, 149.05], [-20, 149.1]]] : [], replay_times: track ? [[T1 + at, T1 + at + 500, T1 + at + 1000]] : [],
    }) as unknown as PassageDetail
    const passages = [base('a', 0, true), base('b', 2 * DAY, false), base('c', 4 * DAY, true)]
    const setup = prepareVideoReplay({ orientation: 'landscape', passages, shots: [], signs: [], hull: null, threeD: true, satellite: false, isDarkTheme: false, lengthS: 15, summary: {} } as unknown as PassageVideoInput)
    expect(setup.passageBounds.map((p) => [p.id, p.bounds === null])).toEqual([['a', false], ['b', true], ['c', false]])
    expect(setup.bounds).toBeTruthy()
  })
})

describe('4. the director keeps its time while another passage\'s shot is out of force', () => {
  const ctx = (): CameraContext => ({ overview: O, flat: false, shotStart: () => ({ lat: -20, lon: 149, heading: 0 }), elapsedAt: () => 0 })
  const resolved = resolveShots([shot('s', T1 + 100)], buildTimeline([{ id: 'p1', start: T1, end: T1 + 1000, segments: [seg(T1, -20, 149)] }]))
  const frame = (d: CameraDirector, nowMs: number, passageId: string) => d.frame(ctx(), {
    resolved, clock: 500, playing: true, nowMs, reduced: false, resume: 0, boat: { lat: -20, lon: 149, heading: 0 }, current: A, passageId,
  })

  it('starts the ease of a shot at 0 even after a long stretch of the passage\'s own framing', () => {
    const d = new CameraDirector()
    // Sixty seconds of frames on another passage, where the shot does not apply.
    for (let t = 0; t <= 60_000; t += 100) expect(frame(d, t, 'p2')).toBeNull()
    // The first frame of the shot, on its own passage a frame later.
    const first = frame(d, 60_100, 'p1')!
    // Eased from the camera the map had (A) by a frame's worth of the 1.5 s, not by half a second.
    const early = blendCameras(A, first.camera, 0)
    expect(first.camera.zoom).toBeCloseTo(blendCameras(A, { center: [149, -20], zoom: 15, pitch: 55, bearing: 0 }, easeInOut(0.1 / TRANSITION_S)).zoom, 3)
    expect(early.zoom).toBe(A.zoom)
  })
})

describe('5. the replay ends on the whole cruise', () => {
  const { config, time } = cruise([T1, T1 + 3 * DAY, T1 + 6 * DAY])
  const end = time.totalWall

  it('eases out to the whole cruise in the last moments, and rests there', () => {
    const out = play(config, range(end - 2.5, end, 0.05))
    const at = (t: number) => out[Math.round((t - (end - 2.5)) / 0.05)]!
    // Still on the last passage until the ease begins.
    close(at(end - TRANSITION_S - 0.05).camera, C, 3)
    expect(at(end - TRANSITION_S / 2).camera.zoom).toBeLessThan(C.zoom)
    expect(at(end - TRANSITION_S / 2).camera.zoom).toBeGreaterThan(O.zoom)
    close(at(end).camera, O)
    expect(at(end).shotKey).toBe('end')
  })

  it('cuts in reduced motion, at the end', () => {
    const out = play(config, [end - 1, end], { reduced: true })
    close(out[0]!.camera, C)
    close(out[1]!.camera, O)
  })

  it('leaves a single passage as it was: no shots, no camera', () => {
    const one = cruise([T1])
    const c = new ReplayController()
    c.step(one.config, input(one.time.totalWall))
    expect(c.cameraAt({ flat: false, overview: O, current: O }, 1)).toBeNull()
  })

  it('leaves a shot that is in force at the end in force', () => {
    const withShot = cruise([T1, T1 + 3 * DAY], [shot('late', T1 + 3 * DAY + 100)])
    const out = play(withShot.config, [withShot.time.totalWall])
    expect(out[0]!.shotKey).toBe('late')
  })
})

describe('7. one derivation of each passage\'s bounds', () => {
  const p = (id: string, segments: TimedSegment[]) => ({ id, segments })
  it('is every passage\'s own, for several', () => {
    const list = passageBoundsOf([p('a', [seg(T1, -20, 148)]), p('b', []), p('c', [seg(T1, -21, 149)])])
    expect(list.map((x) => [x.id, x.bounds === null])).toEqual([['a', false], ['b', true], ['c', false]])
    expect(list[0].bounds).toEqual([148, -20, 148.1, -20])
  })
  it('is nothing for a single passage, so the one framing is the whole track', () => {
    expect(passageBoundsOf([p('a', [seg(T1, -20, 148)])])).toEqual([])
    expect(passageBoundsOf([])).toEqual([])
  })
  it('is what the video plans with', () => {
    const detail = (id: string, at: number) => ({
      id, start: new Date((T1 + at) * 1000).toISOString(), end: new Date((T1 + at + 1000) * 1000).toISOString(),
      from_name: '', to_name: '', title: '', notes: '', hidden: false, edited: false, cruise_id: null, created_at: '', stats: {}, timezone: 'Australia/Brisbane',
      track: [[[-20, 149], [-20, 149.1]]], track_times: [[T1 + at, T1 + at + 1000]], replay_track: [[[-20, 149], [-20, 149.1]]], replay_times: [[T1 + at, T1 + at + 1000]],
    }) as unknown as PassageDetail
    expect(planReplay([detail('a', 0)], [], [], null, true).passageBounds).toEqual([])
    const two = planReplay([detail('a', 0), detail('b', 2 * DAY)], [], [], null, true)
    expect(two.passageBounds).toEqual(passageBoundsOf(two.items.map((i) => ({ id: i.id, segments: i.line }))))
    expect(two.passageBounds).toHaveLength(2)
  })
})
