import { describe, expect, it } from 'vitest'

import { buildTimeline, type TimedSegment } from '@/lib/passages'
import type { Camera } from '@/lib/replay-camera'
import { advanceClock, ReplayController, ReplayPlayer, sailedToAt, type FrameInput, type ReplayConfig } from '@/lib/replay-controller'
import { INTRO_MS } from '@/lib/replay-intro'
import { paceSpans, resolveShots, type CameraShot } from '@/lib/replay-clock'
import { resolveSigns, type ReplaySign } from '@/lib/replay-signs'

// Two passages: 1000 s heading east, then 500 s heading north; a night between.
const east: TimedSegment = Array.from({ length: 11 }, (_, i) => ({ t: 10000 + i * 100, lat: -20, lon: 149 + i * 0.01 })) as TimedSegment
const north: TimedSegment = Array.from({ length: 6 }, (_, i) => ({ t: 50000 + i * 100, lat: -20 + i * 0.01, lon: 149.1 })) as TimedSegment
const timeline = buildTimeline([
  { id: 'p1', start: 10000, end: 11000, segments: [east] },
  { id: 'p2', start: 50000, end: 50500, segments: [north] },
])
const LENGTH = 15

const shot = (id: string, t: number, extra: Partial<CameraShot> = {}): CameraShot => ({
  id, t_ms: t * 1000, mode: 'follow', zoom: 14, pitch: 60, bearing_offset_deg: 0, orbit_deg_per_s: 0, pace: 1, ...extra,
})
const sign = (id: string, t: number, extra: Partial<ReplaySign> = {}): ReplaySign => ({
  id, t_ms: t * 1000, title: id, note: '', document_id: null, photo_url: null, visibility: 'passing', show_s: 4, starts_leg: false, ...extra,
})

function config(shots: CameraShot[] = [], signs: ReplaySign[] = []): ReplayConfig {
  const resolved = resolveShots(shots, timeline)
  return { timeline, lengthS: LENGTH, spans: paceSpans(resolved), resolved, signs: resolveSigns(signs, timeline) }
}
// A controller with its replay, stepped the way a caller with no render would.
function stepper(cfg: ReplayConfig) {
  const c = new ReplayController()
  return {
    c,
    frameAt: (i: FrameInput) => c.step(cfg, i),
    cameraAt: (e: Parameters<ReplayController['cameraAt']>[0], ms: number) => c.cameraAt(e, ms),
    startIntro: c.startIntro.bind(c),
    endIntro: c.endIntro.bind(c),
    introAt: c.introAt.bind(c),
    get introActive() { return c.introActive },
  }
}
const overview: Camera = { center: [149.05, -20], zoom: 10, pitch: 0, bearing: 0 }
const input = (clock: number, extra: Partial<FrameInput> = {}): FrameInput => ({
  clock, nowMs: 1000, playing: false, resume: 0, reduced: false, wantHeading: true, ...extra,
})
const env = { flat: false, overview, current: overview }
// The frame at a clock, then the camera the map would ask for against it.
const cam = (c: ReturnType<typeof stepper>, clock: number, extra: Partial<FrameInput> = {}) => {
  const i = input(clock, extra)
  c.frameAt(i)
  return c.cameraAt(env, i.nowMs)
}

describe('the replay clock', () => {
  it('runs the timeline in the chosen length and stops at the end', () => {
    const p = new ReplayPlayer(1500, 15, () => [])
    p.play(0)
    expect(p.tick(5000)).toBeCloseTo(500)
    expect(p.tick(10_000)).toBeCloseTo(1000)
    expect(p.tick(15_000)).toBe(1500)
    expect(p.playing).toBe(false)
    expect(p.tick(20_000)).toBe(1500)
  })

  it('a slowed stretch takes longer to watch', () => {
    const spans = [{ from: 750, pace: 0.5 }]
    // 7.5 s to reach 750, then 5 s more covers only 250.
    expect(advanceClock(1500, 15, spans, 750, 5)).toBeCloseTo(1000)
  })

  it('pause holds the clock; a seek clamps to the replay', () => {
    const p = new ReplayPlayer(1500, 15, () => [])
    p.play(0)
    p.tick(1000)
    p.pause()
    expect(p.tick(9000)).toBeCloseTo(100)
    p.seek(99_999)
    expect(p.clock).toBe(1500)
    p.seek(-4)
    expect(p.clock).toBe(0)
  })

  it('time passed before play is not counted', () => {
    const p = new ReplayPlayer(1500, 15, () => [])
    p.play(50_000)
    expect(p.tick(51_000)).toBeCloseTo(100)
  })
})

describe('the sailed track', () => {
  it('is null at the start', () => {
    expect(sailedToAt(timeline, 0, 'p1', { lat: 0, lon: 0 })).toBeNull()
  })

  it('has earlier passages whole, later ones not begun, and the current one ending at the boat', () => {
    const boat = { lat: -20, lon: 149.1 }
    const m = sailedToAt(timeline, 1200, 'p2', boat)!
    expect(m.get('p1')).toEqual({ at: 11000, end: undefined })
    expect(m.get('p2')).toEqual({ at: 50200, end: boat })
    const early = sailedToAt(timeline, 300, 'p1', boat)!
    expect(early.get('p2')!.at).toBe(50000)
  })
})

describe('frameAt', () => {
  it('puts the boat where the track is at the clock', () => {
    const c = stepper(config())
    const f = c.frameAt(input(500))
    expect(f.here?.entry.id).toBe('p1')
    expect(f.boat!.lon).toBeCloseTo(149.05)
    expect(f.heading).toBeCloseTo(90, 0)
  })

  it('shows signs as pins before the start, and as cards while replaying', () => {
    const c = stepper(config([], [sign('a', 10500)]))
    const before = c.frameAt(input(0))
    expect(before.pins.map((p) => p.sign.id)).toEqual(['a'])
    expect(before.cards).toEqual([])
    // Sign at clock 500 = 7.5 s into the watching; card up from 0.5 s before, for 4 s.
    const during = c.frameAt(input(500))
    expect(during.cards.map((r) => r.sign.id)).toEqual(['a'])
    expect(during.pins).toEqual([])
    expect(c.frameAt(input(500, { signsOpen: true })).pins.map((p) => p.sign.id)).toEqual(['a'])
    expect(c.frameAt(input(900)).cards).toEqual([])
  })

  it('frames the overview before the first shot and follows the boat in a follow shot', () => {
    const c = stepper(config([shot('s1', 10400)]))
    expect(cam(c, 100)?.camera).toEqual(overview)
    const f = cam(c, 500)
    expect(f?.mode).toBe('follow')
    expect(f?.camera.center[0]).toBeCloseTo(149.05)
    expect(f?.camera.zoom).toBe(14)
    expect(f?.camera.pitch).toBe(60)
    expect(f?.camera.bearing).toBeCloseTo(90, 0)
  })

  it('an overview shot frames the whole track, an orbit holds its start heading when paused at the start', () => {
    const c = stepper(config([shot('o', 10300, { mode: 'overview' }), shot('orb', 10600, { mode: 'orbit', orbit_deg_per_s: 10 })]))
    expect(cam(c, 350)?.camera).toEqual(overview)
    const orb = cam(c, 600)!
    expect(orb.mode).toBe('orbit')
    expect(orb.camera.bearing).toBeCloseTo(90, 0)
  })

  it('does not step the smoothed heading twice for the same key', () => {
    const c = stepper(config())
    const a = c.frameAt(input(500, { playing: true, nowMs: 1000 }))
    const b = c.frameAt(input(500, { playing: true, nowMs: 1400 }))
    expect(b.heading).toBe(a.heading)
  })

  it('without a boat there is no camera', () => {
    const c = stepper({ ...config(), timeline: buildTimeline([]) })
    c.frameAt(input(0))
    expect(c.cameraAt(env, 1000)).toBeNull()
  })

  it('a frame changes nothing until it is committed', () => {
    const cfg = config([shot('s1', 10200)])
    const c = new ReplayController()
    // Frames worked out and thrown away leave no trace: the heading is not smoothed and no shot is placed.
    c.frameAt(cfg, input(500, { playing: true, nowMs: 1000 }))
    c.frameAt(cfg, input(520, { playing: true, nowMs: 1016 }))
    expect(c.cameraAt(env, 1016)).toBeNull()
    const kept = c.frameAt(cfg, input(500, { playing: true, nowMs: 2000 }))
    const fresh = new ReplayController().frameAt(cfg, input(500, { playing: true, nowMs: 2000 }))
    expect(kept.heading).toBe(fresh.heading)
    c.commit(kept)
    expect(c.cameraAt(env, 2000)?.mode).toBe('follow')
  })

  it('refuses a frame it did not work out', () => {
    const other = new ReplayController().frameAt(config(), input(0))
    expect(() => new ReplayController().commit(other)).toThrow(/did not come from this controller/)
  })

  describe('is deterministic', () => {
    const cfg = config([shot('s1', 10200), shot('orb', 10600, { mode: 'orbit', orbit_deg_per_s: 20 }), shot('ov', 50100, { mode: 'overview' })], [sign('a', 10500), sign('b', 50200)])
    const clocks = [0, 120, 340, 500, 640, 900, 1000, 1100, 1300, 1500]
    // Paused frames: a seek places everything from the clock alone.
    const pausedFrame = (c: ReturnType<typeof stepper>, clock: number) => {
      const f = c.frameAt(input(clock))
      return JSON.stringify({ f: { ...f, sailedTo: f.sailedTo ? [...f.sailedTo] : null } })
    }

    it('gives the same frame for the same inputs', () => {
      const a = stepper(cfg)
      const b = stepper(cfg)
      for (const clock of clocks) expect(pausedFrame(a, clock)).toBe(pausedFrame(b, clock))
    })

    it('paused frames do not depend on the order they are asked for', () => {
      const forward = stepper(cfg)
      const shuffled = stepper(cfg)
      const expected = new Map(clocks.map((clock) => [clock, pausedFrame(forward, clock)]))
      for (const clock of [900, 0, 1500, 340, 1100, 120, 640, 1000, 500, 1300]) expect(pausedFrame(shuffled, clock)).toBe(expected.get(clock))
    })

    it('a stepped playback gives the same frames every time it is run from a fresh controller', () => {
      const run = () => {
        const c = stepper(cfg)
        const out: string[] = []
        for (let i = 0; i <= 90; i++) {
          const extra = { playing: true, nowMs: 5000 + i * 33.3333, resume: 1 }
          const f = c.frameAt(input(300 + i * 5, extra))
          out.push(JSON.stringify(c.cameraAt(env, extra.nowMs)) + String(f.heading))
        }
        return out
      }
      expect(run()).toEqual(run())
    })
  })
})

describe('the opening move', () => {
  const from: Camera = { center: [149.05, -20], zoom: 1.5, pitch: 0, bearing: 0 }
  const to: Camera = { center: [149.05, -20], zoom: 10, pitch: 50, bearing: 0 }

  it('has nothing to say until it is started', () => {
    const c = stepper(config())
    expect(c.introAt(0)).toBeNull()
    expect(c.introActive).toBe(false)
  })

  it('runs from the start camera to the end camera over its length, landing on the last frame', () => {
    const c = stepper(config())
    c.startIntro(from, to, 10_000)
    expect(c.introActive).toBe(true)
    expect(c.introAt(10_000)!.camera.zoom).toBeCloseTo(1.5)
    const mid = c.introAt(10_000 + INTRO_MS / 2)!
    expect(mid.done).toBe(false)
    expect(mid.camera.zoom).toBeGreaterThan(1.5)
    expect(mid.camera.zoom).toBeLessThan(10)
    const end = c.introAt(10_000 + INTRO_MS)!
    expect(end.done).toBe(true)
    expect(end.camera).toEqual(to)
    c.endIntro()
    expect(c.introAt(20_000)).toBeNull()
    expect(c.introActive).toBe(false)
  })

  it('is a function of the time asked, not of the order it is asked in', () => {
    const c = stepper(config())
    c.startIntro(from, to, 0)
    const a = c.introAt(1200)
    c.introAt(3000)
    expect(c.introAt(1200)).toEqual(a)
  })

  it('a length of 0 lands at once', () => {
    const c = stepper(config())
    c.startIntro(from, to, 5000, 0)
    const f = c.introAt(5000)!
    expect(f.done).toBe(true)
    expect(f.camera).toEqual(to)
  })
})
