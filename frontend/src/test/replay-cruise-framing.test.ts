import { describe, expect, it } from 'vitest'

import { VIDEO_SIZES } from '@/lib/passage-video/schedule'
import { buildTimeline, isMultiPassage, type TimedSegment } from '@/lib/passages'
import { blendCameras, easeInOut, type Camera } from '@/lib/replay-camera'
import { cruisePaceSpans, resolveShots, type CameraShot } from '@/lib/replay-clock'
import { ReplayController, type FrameInput, type ReplayConfig } from '@/lib/replay-controller'
import { cruiseCamera, cruiseFramingFor, introLanding, overviewCameraFor, type CruiseFraming, type FitMap } from '@/lib/replay-framing'
import { staysOf } from '@/lib/replay-stay'
import { STAY_S, buildReplayTime } from '@/lib/replay-time'

// ── the framing ──────────────────────────────────────────────────────────

function mapOfSize(width: number, height: number): FitMap {
  return {
    getContainer: () => ({ clientWidth: width, clientHeight: height }),
    cameraForBounds: (b, o) => {
      const spanLon = b[1][0] - b[0][0]
      const spanLat = b[1][1] - b[0][1]
      const w = width - o.padding.left - o.padding.right
      const h = height - o.padding.top - o.padding.bottom
      const zoom = Math.min(Math.log2((w / 512) * (360 / spanLon)), Math.log2((h / 512) * (180 / spanLat)), o.maxZoom)
      return { center: { lng: (b[0][0] + b[1][0]) / 2, lat: (b[0][1] + b[1][1]) / 2 }, zoom }
    },
  }
}
const whole: [number, number, number, number] = [148, -20.6, 149.6, -20]
const first: [number, number, number, number] = [148, -20.1, 148.5, -20]
const second: [number, number, number, number] = [149, -20.6, 149.6, -20.4]
const passageBounds = [{ id: 'p1', bounds: first }, { id: 'p2', bounds: second }]

describe('cruiseFramingFor', () => {
  it('frames the whole cruise and each passage on its own, as a single passage is framed', () => {
    const map = mapOfSize(1920, 1080)
    const f = cruiseFramingFor(map, whole, passageBounds, 50)!
    expect(f.overview).toEqual(overviewCameraFor(map, whole, 50))
    expect(f.passages.get('p1')).toEqual(overviewCameraFor(map, first, 50))
    expect(f.passages.get('p2')).toEqual(overviewCameraFor(map, second, 50))
    // Closer in on a passage than on the whole cruise.
    expect(f.passages.get('p1')!.zoom).toBeGreaterThan(f.overview.zoom)
    expect(f.passages.get('p1')!.pitch).toBe(50)
  })

  it('is flat for a flat map', () => {
    const f = cruiseFramingFor(mapOfSize(1920, 1080), whole, passageBounds, 0)!
    expect([f.overview.pitch, f.passages.get('p1')!.pitch, f.passages.get('p2')!.pitch]).toEqual([0, 0, 0])
  })

  it('refits every framing to the portrait frame', () => {
    const land = cruiseFramingFor(mapOfSize(VIDEO_SIZES.landscape.width, VIDEO_SIZES.landscape.height), whole, passageBounds, 50)!
    const port = cruiseFramingFor(mapOfSize(VIDEO_SIZES.portrait.width, VIDEO_SIZES.portrait.height), whole, passageBounds, 50)!
    expect(port.overview.zoom).toBeLessThan(land.overview.zoom)
    expect(port.passages.get('p1')!.zoom).toBeLessThan(land.passages.get('p1')!.zoom)
    expect(port.passages.get('p2')!.zoom).toBeLessThan(land.passages.get('p2')!.zoom)
    // And the passages are as much closer than the cruise in either.
    expect(port.passages.get('p1')!.center).toEqual(land.passages.get('p1')!.center)
  })

  it('leaves out a passage that cannot be framed, and frames the rest', () => {
    const f = cruiseFramingFor(mapOfSize(1920, 1080), whole, [{ id: 'p1', bounds: null }, { id: 'p2', bounds: second }], 50)!
    expect([...f.passages.keys()]).toEqual(['p2'])
    expect(f.overview).toEqual(overviewCameraFor(mapOfSize(1920, 1080), whole, 50))
  })

  it('says so when the whole cruise cannot be framed', () => {
    const none: FitMap = { getContainer: () => ({ clientWidth: 0, clientHeight: 0 }), cameraForBounds: () => undefined }
    expect(cruiseFramingFor(none, whole, passageBounds, 50)).toBeNull()
  })
})

// ── the camera ───────────────────────────────────────────────────────────

const A: Camera = { center: [148.25, -20.05], zoom: 11, pitch: 50, bearing: 0 }
const B: Camera = { center: [149.3, -20.5], zoom: 11.5, pitch: 50, bearing: 0 }
const O: Camera = { center: [148.8, -20.3], zoom: 9, pitch: 50, bearing: 0 }
const framing: CruiseFraming = { overview: O, passages: new Map([['p1', A], ['p2', B]]) }
const stay = (progress: number) => ({ fromId: 'p1', toId: 'p2', progress })

describe('cruiseCamera', () => {
  it('frames the passage the boat is on', () => {
    expect(cruiseCamera(framing, { passageId: 'p1', stay: null }, false)).toEqual({ camera: A, key: 'passage:p1' })
    expect(cruiseCamera(framing, { passageId: 'p2', stay: null }, false)).toEqual({ camera: B, key: 'passage:p2' })
  })

  it('splits a stay in thirds: out to the whole cruise, held wide, back in on the next passage', () => {
    // In reduced motion each third has a key of its own, which shows where the thirds fall.
    const phase = (p: number) => cruiseCamera(framing, { passageId: 'p1', stay: stay(p) }, true)!.key.split(':').pop()
    expect([0, 0.33].map(phase)).toEqual(['out', 'out'])
    expect([0.34, 0.66].map(phase)).toEqual(['wide', 'wide'])
    expect([0.67, 1].map(phase)).toEqual(['in', 'in'])
  })

  it('starts the stay on the passage it arrived on, and ends it on the one that follows', () => {
    expect(cruiseCamera(framing, { passageId: 'p1', stay: stay(0) }, false)!.camera).toEqual(A)
    expect(cruiseCamera(framing, { passageId: 'p1', stay: stay(1) }, false)!.camera).toEqual(B)
  })

  it('eases out in the first third, so it is whole cruise by the end of it', () => {
    const mid = cruiseCamera(framing, { passageId: 'p1', stay: stay(1 / 6) }, false)!.camera
    expect(mid).toEqual(blendCameras(A, O, easeInOut(0.5)))
    expect(mid.zoom).toBeLessThan(A.zoom)
    expect(mid.zoom).toBeGreaterThan(O.zoom)
    const nearly = cruiseCamera(framing, { passageId: 'p1', stay: stay(1 / 3 - 1e-6) }, false)!.camera
    expect(nearly.zoom).toBeCloseTo(O.zoom, 3)
  })

  it('holds the whole cruise for the middle third', () => {
    for (const p of [0.34, 0.5, 0.66]) expect(cruiseCamera(framing, { passageId: 'p1', stay: stay(p) }, false)!.camera).toEqual(O)
  })

  it('eases in on the next passage in the last third', () => {
    const mid = cruiseCamera(framing, { passageId: 'p1', stay: stay(5 / 6) }, false)!.camera
    const want = blendCameras(O, B, easeInOut(0.5))
    expect(mid.zoom).toBeCloseTo(want.zoom, 6)
    expect(mid.center[0]).toBeCloseTo(want.center[0], 6)
    expect(mid.center[1]).toBeCloseTo(want.center[1], 6)
    expect(cruiseCamera(framing, { passageId: 'p1', stay: stay(2 / 3 + 1e-6) }, false)!.camera.zoom).toBeCloseTo(O.zoom, 3)
  })

  it('has one key for the whole stay, so a person who takes the map keeps it until the next stay', () => {
    const keys = [0, 0.2, 0.5, 0.9].map((p) => cruiseCamera(framing, { passageId: 'p1', stay: stay(p) }, false)!.key)
    expect(new Set(keys)).toEqual(new Set(['stay:p1>p2']))
  })

  it('cuts instead of easing in reduced motion: each third is a framing of its own, with its own key', () => {
    const out = cruiseCamera(framing, { passageId: 'p1', stay: stay(0.2) }, true)!
    const wide = cruiseCamera(framing, { passageId: 'p1', stay: stay(0.5) }, true)!
    const into = cruiseCamera(framing, { passageId: 'p1', stay: stay(0.8) }, true)!
    expect([out.camera, wide.camera, into.camera]).toEqual([A, O, B])
    expect(new Set([out.key, wide.key, into.key]).size).toBe(3)
  })

  it('frames the whole cruise for a passage the framing lacks', () => {
    expect(cruiseCamera(framing, { passageId: 'gone', stay: null }, false)).toEqual({ camera: O, key: 'passage:gone' })
    expect(cruiseCamera(framing, { passageId: 'p1', stay: { fromId: 'p1', toId: 'gone', progress: 1 } }, false)!.camera).toEqual(O)
  })
})

// ── the controller ───────────────────────────────────────────────────────

const T1 = Date.UTC(2026, 9, 24, 6, 0) / 1000
const T2 = T1 + 1.5 * 86400
const east: TimedSegment = Array.from({ length: 11 }, (_, i) => ({ t: T1 + i * 100, lat: -20, lon: 148 + i * 0.01 })) as TimedSegment
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
  { id: 'p1', from_name: 'A', to_name: 'Bay', timezone: 'Australia/Brisbane' },
  { id: 'p2', from_name: 'B', to_name: 'C', timezone: 'Australia/Brisbane' },
])
const shot = (id: string, at: number, extra: Partial<CameraShot> = {}): CameraShot => ({
  id, t_ms: at * 1000, mode: 'follow', zoom: 15, pitch: 55, bearing_offset_deg: 0, orbit_deg_per_s: 0, pace: 1, ...extra,
})
const configWith = (shots: CameraShot[]): ReplayConfig => {
  const resolved = resolveShots(shots, timeline)
  const time = buildReplayTime({ total: timeline.total, lengthS: 15, spans: cruisePaceSpans(resolved, timeline, 15), moments: [], stays })
  return { timeline, time, stays, resolved, signs: [], pieces: PIECES, isDarkTheme: false }
}
const input = (wall: number, extra: Partial<FrameInput> = {}): FrameInput => ({ wall, nowMs: 1000, playing: true, resume: 0, reduced: false, wantHeading: true, view: { zoom: 12 }, ...extra })
const env = { flat: false, overview: O, passages: framing.passages, current: O }
const cameraAt = (config: ReplayConfig, wall: number, extra: Partial<FrameInput> = {}) => {
  const c = new ReplayController()
  c.step(config, input(wall, extra))
  return c.cameraAt(env, 1000)
}
const stayStart = (config: ReplayConfig) => config.time.holds.find((h) => h.kind === 'stay')!.startWall

describe('the camera of a replay of several passages', () => {
  const none = configWith([])

  it('frames the passage the boat is on', () => {
    expect(cameraAt(none, 1)).toMatchObject({ camera: A, shotKey: 'passage:p1', mode: 'overview' })
    expect(cameraAt(none, stayStart(none) + STAY_S + 1)).toMatchObject({ camera: B, shotKey: 'passage:p2' })
  })

  it('pulls out through the stay and comes back in on the next passage', () => {
    const s = stayStart(none)
    expect(cameraAt(none, s + 0.001)!.camera).toEqual(blendCameras(A, O, easeInOut(0.001 / STAY_S * 3)))
    expect(cameraAt(none, s + STAY_S / 2)!.camera).toEqual(O)
    expect(cameraAt(none, s + STAY_S - 0.001)!.camera.zoom).toBeCloseTo(B.zoom, 2)
    expect(cameraAt(none, s + STAY_S / 2)!.shotKey).toBe('stay:p1>p2')
  })

  it('cuts in reduced motion', () => {
    const s = stayStart(none)
    expect(cameraAt(none, s + 0.1, { reduced: true })!.camera).toEqual(A)
    expect(cameraAt(none, s + STAY_S / 2, { reduced: true })!.camera).toEqual(O)
    expect(cameraAt(none, s + STAY_S - 0.1, { reduced: true })!.camera).toEqual(B)
  })

  it('does not change a replay of one passage: no shots, no camera', () => {
    const one = buildTimeline([{ id: 'p1', start: T1, end: T1 + 1000, segments: [east] }])
    const config: ReplayConfig = { timeline: one, time: buildReplayTime({ total: one.total, lengthS: 15, spans: [], moments: [] }), resolved: [], signs: [], pieces: [PIECES[0]], isDarkTheme: false }
    const c = new ReplayController()
    c.step(config, input(5))
    expect(c.cameraAt({ flat: false, overview: O, current: O }, 1000)).toBeNull()
  })

  describe('and the saved camera shots', () => {
    const inFirst = shot('s1', T1 + 200)
    const inSecond = shot('s2', T2 + 100)

    it('win where they apply: on the passage they were saved in', () => {
      const config = configWith([inFirst])
      const r = cameraAt(config, config.time.wallAtClock(300))
      expect(r!.shotKey).toBe('s1')
      expect(r!.mode).toBe('follow')
    })

    it('are not carried into the next passage: it is framed on its own, until its own shot', () => {
      const config = configWith([inFirst])
      const after = cameraAt(config, stayStart(config) + STAY_S + 1)
      expect(after).toMatchObject({ camera: B, shotKey: 'passage:p2' })
    })

    it('give way to the stay, which has a camera of its own', () => {
      const config = configWith([inFirst])
      expect(cameraAt(config, stayStart(config) + STAY_S / 2)).toMatchObject({ camera: O, shotKey: 'stay:p1>p2' })
    })

    it('frame the passage before them until they begin', () => {
      const config = configWith([inSecond])
      expect(cameraAt(config, 1)).toMatchObject({ camera: A, shotKey: 'passage:p1' })
      const r = cameraAt(config, config.time.wallAtClock(timeline.entries[1].offset + 300))
      expect(r!.shotKey).toBe('s2')
    })

    it('belong to the next passage when one is saved at its very start, and wait out the stay for it', () => {
      const atDeparture = shot('s3', T2)
      const config = configWith([inFirst, atDeparture])
      expect(cameraAt(config, stayStart(config) + 0.2)!.shotKey).toBe('stay:p1>p2')
      expect(cameraAt(config, stayStart(config) + STAY_S + 0.5)!.shotKey).toBe('s3')
    })
  })
})

describe('isMultiPassage and introLanding', () => {
  const one = buildTimeline([{ id: 'p1', start: T1, end: T1 + 1000, segments: [east] }])

  it('says whether a timeline is of several passages', () => {
    expect(isMultiPassage(timeline)).toBe(true)
    expect(isMultiPassage(one)).toBe(false)
    expect(isMultiPassage(buildTimeline([]))).toBe(false)
  })

  it('lands the fly-in on the first passage of a cruise, and on the whole track for one passage', () => {
    expect(introLanding(framing, timeline)).toBe(A)
    expect(introLanding(framing, one)).toBe(O)
  })

  it('lands on the whole track when the map gave no passage framings', () => {
    expect(introLanding({ overview: O }, timeline)).toBe(O)
  })
})
