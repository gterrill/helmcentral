import { describe, expect, it, vi } from 'vitest'

import { buildTimeline, type TimedSegment } from '@/lib/passages'
import type { Camera } from '@/lib/replay-camera'
import { ReplayController, ReplayPlayer, sailedToAt, type FrameInput, type ReplayConfig } from '@/lib/replay-controller'
import { MOMENT_TOTAL_S, buildReplayTime } from '@/lib/replay-time'
import { INTRO_MS } from '@/lib/replay-intro'
import { paceSpans, resolveShots, type CameraShot } from '@/lib/replay-clock'
import { momentsOf, photoMomentOn, resolveSigns, type ReplaySign } from '@/lib/replay-signs'

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
  id, t_ms: t * 1000, title: id, note: '', document_id: null, photo_url: null, visibility: 'passing', show_s: 4, starts_leg: false, photo_moment: null, ...extra,
})

// The line as the map draws it: a piece per passage.
const PIECES = [
  { key: 'p1', passageId: 'p1', segments: [east], colourIndex: 0, from: -Infinity, to: Infinity },
  { key: 'p2', passageId: 'p2', segments: [north], colourIndex: 1, from: -Infinity, to: Infinity },
]

function config(shots: CameraShot[] = [], signs: ReplaySign[] = []): ReplayConfig {
  const resolved = resolveShots(shots, timeline)
  const placed = resolveSigns(signs, timeline)
  const moments = momentsOf(placed)
  return { timeline, time: buildReplayTime({ total: timeline.total, lengthS: LENGTH, spans: paceSpans(resolved), moments }), resolved, signs: placed, pieces: PIECES, isDarkTheme: false }
}
// A controller with its replay, stepped the way a caller with no render would.
// A step is written at a clock, or at a watching time (which only matters in a hold).
type Step = Omit<FrameInput, 'wall'> & { clock: number; wall?: number }
const toWall = (cfg: ReplayConfig, i: Step): FrameInput => {
  const { clock, wall, ...rest } = i
  // At a sign's clock a step stands on the sign, at the start of its moment.
  return { ...rest, wall: wall ?? cfg.time.wallAtClock(clock, { onSign: true }) }
}
function stepper(cfg: ReplayConfig) {
  const c = new ReplayController()
  return {
    c,
    frameAt: (i: Step) => c.step(cfg, toWall(cfg, i)),
    cameraAt: (e: Parameters<ReplayController['cameraAt']>[0], ms: number) => c.cameraAt(e, ms),
    startIntro: c.startIntro.bind(c),
    endIntro: c.endIntro.bind(c),
    introAt: c.introAt.bind(c),
    get introActive() { return c.introActive },
  }
}
const overview: Camera = { center: [149.05, -20], zoom: 10, pitch: 0, bearing: 0 }
const input = (clock: number, extra: Partial<Step> = {}): Step => ({
  clock, nowMs: 1000, playing: false, resume: 0, reduced: false, wantHeading: true, view: { zoom: 12 }, ...extra,
})
const env = { flat: false, overview, current: overview }
// The frame at a clock, then the camera the map would ask for against it.
const cam = (c: ReturnType<typeof stepper>, clock: number, extra: Partial<Step> = {}) => {
  const i = input(clock, extra)
  c.frameAt(i)
  return c.cameraAt(env, i.nowMs)
}

const timeOf = (moments: { clock: number; id: string }[] = [], spans: { from: number; pace: number }[] = []) =>
  buildReplayTime({ total: 1500, lengthS: 15, spans, moments })

describe('the replay clock', () => {
  it('runs the timeline in the chosen length and stops at the end', () => {
    const p = new ReplayPlayer(timeOf())
    p.play(0)
    p.tick(5000)
    expect(p.clock).toBeCloseTo(500)
    p.tick(10_000)
    expect(p.clock).toBeCloseTo(1000)
    p.tick(15_000)
    expect(p.clock).toBe(1500)
    expect(p.playing).toBe(false)
    expect(p.ended).toBe(true)
    p.tick(20_000)
    expect(p.clock).toBe(1500)
  })

  it('a slowed stretch takes longer to watch', () => {
    const t = timeOf([], [{ from: 750, pace: 0.5 }])
    // 7.5 s to reach 750, then 5 s more covers only 250.
    expect(t.at(7.5 + 5).clock).toBeCloseTo(1000)
  })

  it('pause holds the clock; a seek clamps to the replay', () => {
    const p = new ReplayPlayer(timeOf())
    p.play(0)
    p.tick(1000)
    p.pause()
    p.tick(9000)
    expect(p.clock).toBeCloseTo(100)
    p.seek(99_999)
    expect(p.clock).toBe(1500)
    p.seek(-4)
    expect(p.clock).toBe(0)
  })

  it('time passed before play is not counted', () => {
    const p = new ReplayPlayer(timeOf())
    p.play(50_000)
    p.tick(51_000)
    expect(p.clock).toBeCloseTo(100)
  })

  describe('with photo moments', () => {
    const moments = [{ clock: 500, id: 'a' }]

    it('stands still at the sign for the length of the moment, then carries on', () => {
      const p = new ReplayPlayer(timeOf(moments))
      p.play(0)
      p.tick(5000)
      expect(p.clock).toBeCloseTo(500)
      expect(p.moment!.id).toBe('a')
      p.tick(5000 + 1000)
      expect(p.clock).toBeCloseTo(500)
      expect(p.moment!.elapsed).toBeCloseTo(1)
      p.tick(5000 + MOMENT_TOTAL_S * 1000 + 1000)
      expect(p.moment).toBeNull()
      expect(p.clock).toBeCloseTo(600)
    })

    it('adds the hold to the watching: the replay ends a moment later', () => {
      const p = new ReplayPlayer(timeOf(moments))
      p.play(0)
      p.tick(15_000)
      expect(p.ended).toBe(false)
      p.tick(15_000 + MOMENT_TOTAL_S * 1000)
      expect(p.ended).toBe(true)
      expect(p.clock).toBe(1500)
    })

    it('pauses in the middle of a moment when paused, and goes on from there', () => {
      const p = new ReplayPlayer(timeOf(moments))
      p.play(0)
      p.tick(5000 + 500)
      p.pause()
      p.tick(60_000)
      expect(p.moment!.elapsed).toBeCloseTo(0.5)
      p.play(100_000)
      p.tick(100_000 + 500)
      expect(p.moment!.elapsed).toBeCloseTo(1)
    })

    it('ends the moment when told to, and carries on from the sign', () => {
      const p = new ReplayPlayer(timeOf(moments))
      p.play(0)
      p.tick(5000 + 700)
      p.skipMoment()
      expect(p.moment).toBeNull()
      expect(p.clock).toBeCloseTo(500)
      expect(p.playing).toBe(true)
      p.tick(5000 + 700 + 1000)
      expect(p.clock).toBeCloseTo(600)
    })

    it('does nothing when told to end a moment it is not in', () => {
      const p = new ReplayPlayer(timeOf(moments))
      p.seek(200)
      const wall = p.wall
      p.skipMoment()
      expect(p.wall).toBe(wall)
    })

    it('seeks over sailing time: clear of every hold, unless it lands on the sign', () => {
      const p = new ReplayPlayer(timeOf(moments))
      for (const c of [0, 250, 499, 501, 900, 1500]) {
        p.seek(c)
        expect(p.moment).toBeNull()
        expect(p.clock).toBeCloseTo(c)
      }
      // Even at the sign's own clock the slider lands after the hold.
      p.seek(500)
      expect(p.moment).toBeNull()
      expect(p.clock).toBeCloseTo(500)
      // Only a jump to the sign lands on its moment.
      p.seek(500, { onSign: true })
      expect(p.moment!.id).toBe('a')
      expect(p.moment!.elapsed).toBeCloseTo(0)
    })

    it('counts a seek to the end as ended', () => {
      const p = new ReplayPlayer(timeOf([{ clock: 1500, id: 'z' }]))
      p.seek(1500)
      expect(p.ended).toBe(true)
    })

    it('stays past a hold it has seen when the timing changes: at its end, or later at that clock', () => {
      const changed = timeOf(moments, [{ from: 900, pace: 0.5 }])
      // Just as the hold ends.
      const p = new ReplayPlayer(timeOf(moments))
      p.play(0)
      p.tick(5000 + MOMENT_TOTAL_S * 1000)
      expect(p.moment).toBeNull()
      p.setTime(changed, 0)
      expect(p.moment).toBeNull()
      expect(p.clock).toBeCloseTo(500)
      p.play(0)
      p.tick(500)
      expect(p.clock).toBeGreaterThan(500)
      // A skipped moment stays skipped.
      const q = new ReplayPlayer(timeOf(moments))
      q.seek(500, { onSign: true })
      q.skipMoment()
      q.setTime(changed, 0)
      expect(q.moment).toBeNull()
      expect(q.clock).toBeCloseTo(500)
      // A replay that has not reached the hold is still before it.
      const r = new ReplayPlayer(timeOf(moments))
      r.seek(300)
      r.setTime(changed, 0)
      r.play(0)
      r.tick(2500 + 100)
      expect(r.moment).not.toBeNull()
    })

    it('stays at the very start, before a moment standing there, when signs load after the page', () => {
      const p = new ReplayPlayer(timeOf())
      p.setTime(timeOf([{ clock: 0, id: 'first' }]), 0)
      expect(p.wall).toBe(0)
      p.play(0)
      p.tick(500)
      expect(p.moment?.id).toBe('first')
    })

    it('keeps its place, and its place in a moment, when the timing changes under it', () => {
      const p = new ReplayPlayer(timeOf(moments))
      p.play(0)
      p.tick(5000 + 800)
      p.setTime(timeOf(moments, [{ from: 900, pace: 0.5 }]), 5800)
      expect(p.moment!.id).toBe('a')
      expect(p.moment!.elapsed).toBeCloseTo(0.8)
      p.pause()
      p.seek(300)
      p.setTime(timeOf(moments, [{ from: 100, pace: 0.5 }]), 0)
      expect(p.clock).toBeCloseTo(300)
      // The sign goes: the moment with it.
      p.seek(500, { onSign: true })
      p.setTime(timeOf([]), 0)
      expect(p.moment).toBeNull()
      expect(p.clock).toBeCloseTo(500)
    })
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
    c.frameAt(cfg, toWall(cfg, input(500, { playing: true, nowMs: 1000 })))
    c.frameAt(cfg, toWall(cfg, input(520, { playing: true, nowMs: 1016 })))
    expect(c.cameraAt(env, 1016)).toBeNull()
    const kept = c.frameAt(cfg, toWall(cfg, input(500, { playing: true, nowMs: 2000 })))
    const fresh = new ReplayController().frameAt(cfg, toWall(cfg, input(500, { playing: true, nowMs: 2000 })))
    expect(kept.heading).toBe(fresh.heading)
    c.commit(kept)
    expect(c.cameraAt(env, 2000)?.mode).toBe('follow')
  })

  it('refuses a frame it did not work out', () => {
    const other = new ReplayController().frameAt(config(), toWall(config(), input(0)))
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

describe('the light', () => {
  const cfg = config()

  it('follows the sun over the boat at the track\'s own time', () => {
    // Track time is Unix seconds ~10000 s in 1970 on the equator-ish at 149 E: just before 03:00 UTC
    // is just before 13:00 there, so the sun is up.
    const c = stepper(cfg)
    const f = c.frameAt(input(0))
    expect(f.light).not.toBeNull()
    expect(f.light!.light.position[2]).toBeLessThan(90)
    expect(f.light!.light.position[1]).toBeGreaterThanOrEqual(0)
  })

  it('changes with the time of day as the replay moves', () => {
    // 40 000 s on: the sun has gone round to the other side of the day.
    const c = stepper(cfg)
    const early = c.frameAt(input(0))
    const late = c.frameAt(input(1200))
    // Polar angle from straight up: 90 and over is a sun on or under the horizon.
    expect(late.light!.light.position[2]).toBeGreaterThan(early.light!.light.position[2])
    expect(late.light!.key).not.toBe(early.light!.key)
  })

  it('shows the light for a scrub time when paused', () => {
    const a = stepper(cfg).frameAt(input(700))
    const b = stepper(cfg).frameAt(input(700))
    expect(b.light).toEqual(a.light)
  })

  it('hands back the same object while the sun has barely moved, so the map is touched only when it changes', () => {
    const c = stepper(cfg)
    const a = c.frameAt(input(500))
    const b = c.frameAt(input(501))
    expect(b.light).toBe(a.light)
  })

  it('moves the sun, and only the sun, by a development offset', () => {
    const c = stepper(cfg)
    const day = c.frameAt(input(0))
    const shifted = c.frameAt(input(0, { sunOffsetH: 12 }))
    expect(shifted.light!.light.position[2]).toBeGreaterThan(day.light!.light.position[2] + 30)
    expect(shifted.here).toEqual(day.here)
    expect(shifted.boat).toEqual(day.boat)
  })

  it('has no light without a boat', () => {
    const f = stepper({ ...cfg, timeline: buildTimeline([]) }).frameAt(input(0))
    expect(f.light).toBeNull()
  })
})

describe('a photo moment', () => {
  const photo = { document_id: 'd1', photo_url: '/api/documents/d1/content' }
  // The sign is at clock 500 (5 s into the 15 s of sailing).
  const withPhoto = (extra: Partial<ReplaySign> = {}, shots: CameraShot[] = []) => config(shots, [sign('a', 10500, { ...photo, ...extra })])
  const startWall = 5

  it('is on for a sign with a photo unless it was switched off', () => {
    expect(photoMomentOn({ photo_url: '/p', photo_moment: null })).toBe(true)
    expect(photoMomentOn({ photo_url: '/p', photo_moment: true })).toBe(true)
    expect(photoMomentOn({ photo_url: '/p', photo_moment: false })).toBe(false)
    expect(photoMomentOn({ photo_url: null, photo_moment: null })).toBe(false)
    expect(photoMomentOn({ photo_url: null, photo_moment: true })).toBe(false)
  })

  it('has no moment for a sign without a photo, or one switched off', () => {
    expect(withPhoto({ photo_url: null, document_id: null }).time.moments).toHaveLength(0)
    expect(withPhoto({ photo_moment: false }).time.moments).toHaveLength(0)
    expect(withPhoto().time.moments).toHaveLength(1)
  })

  it('holds the clock while it grows, holds and shrinks, then carries on', () => {
    const c = stepper(withPhoto())
    const atSign = c.frameAt(input(500)).boat
    expect(c.frameAt(input(500, { wall: startWall - 0.1, playing: true })).moment).toBeNull()
    for (const [wall, phase] of [[startWall + 0.1, 'grow'], [startWall + 1, 'hold'], [startWall + 2.1, 'shrink']] as const) {
      const f = c.frameAt(input(500, { wall, playing: true }))
      expect(f.moment).toMatchObject({ signId: 'a', phase })
      expect(f.boat).toEqual(atSign)
    }
    expect(c.frameAt(input(500, { wall: startWall + MOMENT_TOTAL_S, playing: true })).moment).toBeNull()
  })

  it('is the moment at its start for a seek onto the sign, and nothing for a seek anywhere else', () => {
    const c = stepper(withPhoto())
    expect(c.frameAt(input(500)).moment).toMatchObject({ phase: 'grow', fill: 0 })
    for (const clock of [0, 100, 499, 501, 900]) expect(c.frameAt(input(clock)).moment).toBeNull()
  })

  it('is not shown before the replay has moved off the start', () => {
    const c = stepper(config([], [sign('a', 10000, { ...photo })]))
    expect(c.frameAt(input(0)).moment).toBeNull()
    expect(c.frameAt(input(0, { wall: 0.2, playing: true })).moment).toMatchObject({ signId: 'a' })
  })

  it('has no grow, shrink or zoom with reduced motion: the photo is full and still the whole moment', () => {
    const c = stepper(withPhoto())
    for (const w of [0.01, 1, MOMENT_TOTAL_S - 0.05]) {
      expect(c.frameAt(input(500, { wall: startWall + w, playing: true, reduced: true })).moment).toMatchObject({ phase: 'hold', fill: 1, scale: 1 })
    }
  })

  it('keeps the sign\'s card up for the whole moment, even for a card that would have gone by then', () => {
    const c = stepper(config([], [sign('a', 10500, { ...photo, show_s: 1 })]))
    expect(c.frameAt(input(500, { wall: startWall + 2, playing: true })).cards.map((r) => r.sign.id)).toEqual(['a'])
  })

  describe('when two other cards are up', () => {
    // Signs at 200 and 300 are up (long enough to be there still) when the boat meets the photo at 500.
    const crowd = (visibility: [ReplaySign['visibility'], ReplaySign['visibility']] = ['passing', 'passing']) => stepper(config([], [
      sign('x', 10200, { show_s: 30, visibility: visibility[0] }),
      sign('y', 10300, { show_s: 30, visibility: visibility[1] }),
      sign('z', 10500, { ...photo, show_s: 1 }),
    ]))
    const ids = (c: ReturnType<typeof stepper>, wall: number) => c.frameAt(input(500, { wall, playing: true })).cards.map((r) => r.sign.id)

    it('are never three: the photo\'s sign comes in when the others have given way, or takes the place of one', () => {
      const c = crowd()
      expect(ids(c, startWall - 1)).toEqual(['x', 'y'])
      const during = ids(c, startWall + 1)
      expect(during.length).toBeLessThanOrEqual(2)
      expect(during).toContain('z')
    })

    it('never takes the place of an always card; with only always cards it takes the last', () => {
      const c = crowd(['always', 'passing'])
      expect(ids(c, startWall + 1).sort()).toEqual(['x', 'z'])
      const all = crowd(['always', 'always'])
      const during = ids(all, startWall + 1)
      expect(during).toHaveLength(2)
      expect(during).toContain('z')
      expect(during).toContain('x')
    })

    it('goes back to the cards the replay shows when the moment is over', () => {
      const c = crowd()
      expect(ids(c, startWall + MOMENT_TOTAL_S + 0.1)).not.toContain('z')
    })
  })

  it('stops the camera\'s motion for the moment: an orbit does not turn', () => {
    const orbit = shot('o', 10300, { mode: 'orbit', orbit_deg_per_s: 20 })
    const c = stepper(withPhoto({}, [orbit]))
    const bearing = (wall: number, nowMs: number) => {
      c.frameAt(input(500, { wall, nowMs, playing: true, resume: 1 }))
      return c.cameraAt(env, nowMs)!.camera.bearing
    }
    c.frameAt(input(500, { wall: startWall - 0.1, nowMs: 5000, playing: true, resume: 1 }))
    const a = bearing(startWall + 0.5, 5600)
    const b = bearing(startWall + 1.5, 6600)
    expect(b).toBeCloseTo(a, 6)
    // And it turns again once the moment is over.
    const later = bearing(startWall + MOMENT_TOTAL_S + 1, 6600 + 2000)
    expect(later).not.toBeCloseTo(a, 0)
  })

  it('is a function of the watching time alone, however it is reached', () => {
    const cfg = withPhoto()
    const one = stepper(cfg).frameAt(input(500, { wall: startWall + 1 })).moment
    const c = stepper(cfg)
    c.frameAt(input(500, { wall: startWall + 2 }))
    expect(c.frameAt(input(500, { wall: startWall + 1 })).moment).toEqual(one)
  })
})

describe('the sailed track', () => {
  it('has no speed frame before the replay has moved off the start', () => {
    expect(stepper(config()).frameAt(input(0)).speed).toBeNull()
  })

  it('colours the track sailed, passage by passage, and draws a wake behind the boat', () => {
    const f = stepper(config()).frameAt(input(500))
    expect(f.speed!.lines.get('p1')![0]!.data.geometry.coordinates.length).toBeGreaterThan(2)
    expect(f.speed!.lines.has('p2')).toBe(true)
    expect(f.speed!.wake).not.toBeNull()
  })

  it('keeps the lines it drew for a passage the replay has left behind, once the frame is taken', () => {
    const c = stepper(config())
    const a = c.frameAt(input(1100))
    const b = c.frameAt(input(1110))
    expect(b.speed!.lines.get('p1')).toBe(a.speed!.lines.get('p1'))
  })

  it('does not keep them from a frame that was worked out and thrown away', () => {
    const cfg = config()
    const c = new ReplayController()
    c.frameAt(cfg, toWall(cfg, input(1100)))
    const kept = c.step(cfg, toWall(cfg, input(1110)))
    const fresh = new ReplayController().frameAt(cfg, toWall(cfg, input(1110)))
    expect(kept.speed!.lines.get('p1')![0]!.data).toEqual(fresh.speed!.lines.get('p1')![0]!.data)
  })

  it('says so, with the count, when a track has too many segments to colour', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const segs = Array.from({ length: 13 }, (_, i) => [{ t: 10000 + i * 70, lat: -20, lon: 149 + i * 0.001 }, { t: 10000 + i * 70 + 60, lat: -20, lon: 149.0005 + i * 0.001 }]) as TimedSegment[]
    const pieces = [{ key: 'p1', passageId: 'p1', segments: segs, colourIndex: 0, from: -Infinity, to: Infinity }]
    const c = stepper({ ...config(), pieces })
    c.frameAt(input(500))
    c.frameAt(input(510))
    expect(warn).toHaveBeenCalledTimes(1)
    expect(warn).toHaveBeenCalledWith(expect.stringContaining('13 track segments'))
    warn.mockRestore()
  })

  it('has none for a track cut into too many segments to colour', () => {
    const segs = Array.from({ length: 13 }, (_, i) => [{ t: 10000 + i * 70, lat: -20, lon: 149 + i * 0.001 }, { t: 10000 + i * 70 + 60, lat: -20, lon: 149.0005 + i * 0.001 }]) as TimedSegment[]
    const pieces = [{ key: 'p1', passageId: 'p1', segments: segs, colourIndex: 0, from: -Infinity, to: Infinity }]
    expect(stepper({ ...config(), pieces }).frameAt(input(500)).speed).toBeNull()
  })

  it('takes its wake from the map\'s zoom', () => {
    const near = stepper(config()).frameAt(input(500, { view: { zoom: 16 } })).speed!.wake!.data.geometry.coordinates
    const far = stepper(config()).frameAt(input(500, { view: { zoom: 8 } })).speed!.wake!.data.geometry.coordinates
    expect(far.length).toBeGreaterThanOrEqual(near.length)
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
    // On the globe from the start, flat by the time it lands.
    expect(c.introAt(10_000)!.projection).not.toEqual({ type: 'mercator' })
    expect(c.introAt(10_000)!.flatAtZoom).toBe(5.5)
    const end = c.introAt(10_000 + INTRO_MS)!
    expect(end.done).toBe(true)
    expect(end.camera).toEqual(to)
    expect(end.projection).toEqual({ type: 'mercator' })
    expect(end.flatAtZoom).toBeNull()
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
