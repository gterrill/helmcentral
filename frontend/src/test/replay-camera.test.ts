import { describe, expect, it } from 'vitest'

import {
  blendCameras,
  CameraDirector,
  CameraGate,
  easeInOut,
  outsideCentral,
  sameCamera,
  shotStartState,
  TRANSITION_S,
  zoomBucket,
  type BoatState,
  type Camera,
  type CameraContext,
  type CameraResult,
  type FrameInput,
} from '@/lib/replay-camera'
import { paceSpans, resolveShots, wallAtTimeline, type CameraShot } from '@/lib/replay-clock'
import { buildTimeline, type TimedSegment } from '@/lib/passages'

const overview: Camera = { center: [149.5, -22], zoom: 8, pitch: 50, bearing: 0 }
const timeline = buildTimeline([{ id: 'p', start: 10_000, end: 11_000, segments: [] }])
const boatAtClock = (c: number): BoatState => ({ lat: -22, lon: 149 + c / 1000, heading: 90 })

const shot = (id: string, clock: number, extra: Partial<CameraShot> = {}): CameraShot => ({
  id, t_ms: (10_000 + clock) * 1000, mode: 'follow', zoom: 14, pitch: 60, bearing_offset_deg: 0, orbit_deg_per_s: 0, pace: 1, ...extra,
})

function rig(shots: CameraShot[], opts: { flat?: boolean; lengthS?: number } = {}) {
  const resolved = resolveShots(shots, timeline)
  let lengthS = opts.lengthS ?? 100
  const ctx: CameraContext = {
    overview: opts.flat ? { ...overview, pitch: 0 } : overview,
    flat: !!opts.flat,
    shotStart: (rs) => boatAtClock(rs.clock),
    elapsedAt: (rs, c) => wallAtTimeline(1000, lengthS, paceSpans(resolved), c) - wallAtTimeline(1000, lengthS, paceSpans(resolved), rs.clock),
  }
  const director = new CameraDirector()
  let now = 0
  const input = (clock: number, over: Partial<FrameInput> = {}): FrameInput => ({
    resolved, clock, playing: false, nowMs: now, reduced: false, resume: 0, boat: boatAtClock(clock), current: overview, ...over,
  })
  return {
    ctx, resolved, director,
    seek: (clock: number, over: Partial<FrameInput> = {}) => director.frame(ctx, input(clock, { ...over, playing: false }))!,
    /** One frame of playing, `dtMs` after the last, at timeline `clock`. */
    play: (clock: number, dtMs: number, over: Partial<FrameInput> = {}) => { now += dtMs; return director.frame(ctx, input(clock, { ...over, playing: true }))! },
    setLength: (n: number) => { lengthS = n },
  }
}

describe('camera director', () => {
  it('has no opinion without shots', () => {
    const r = rig([])
    expect(r.director.frame(r.ctx, { resolved: [], clock: 3, playing: false, nowMs: 0, reduced: false, resume: 0, boat: boatAtClock(3), current: overview })).toBeNull()
  })

  it('before the first shot it holds the overview framing', () => {
    const r = rig([shot('a', 200)])
    expect(r.seek(50)).toEqual({ camera: overview, shotKey: 'before', mode: 'before' })
  })

  it('an overview shot frames the whole replayed track', () => {
    const r = rig([shot('a', 100, { mode: 'overview' })])
    const out = r.seek(500)
    expect(out.camera).toEqual(overview)
    expect(out.shotKey).toBe('a')
    expect(out.mode).toBe('overview')
  })

  it('follow centres on the boat with the shot zoom and pitch, bearing from the heading', () => {
    const { camera } = rig([shot('a', 100)]).seek(500)
    expect(camera.center).toEqual([149.5, -22])
    expect([camera.zoom, camera.pitch, camera.bearing]).toEqual([14, 60, 90])
  })

  it('follow adds the bearing offset and wraps it', () => {
    expect(rig([shot('a', 100, { bearing_offset_deg: 90 })]).seek(500).camera.bearing).toBe(180)
    expect(rig([shot('a', 100, { bearing_offset_deg: -180 })]).seek(500).camera.bearing).toBe(270)
  })

  it('follow takes the live heading, and 0 when there is none', () => {
    const r = rig([shot('a', 100)])
    expect(r.seek(500, { boat: { lat: -22, lon: 149.5, heading: 200 } }).camera.bearing).toBe(200)
    expect(r.seek(500, { boat: { lat: -22, lon: 149.5, heading: null } }).camera.bearing).toBe(0)
  })

  it('orbit starts from the heading at the shot and turns by degrees per second watched', () => {
    // 1000 s of timeline in 100 s: 10 timeline s per wall s.
    const r = rig([shot('a', 100, { mode: 'orbit', orbit_deg_per_s: 6 })])
    expect(r.seek(100).camera.bearing).toBeCloseTo(90)
    expect(r.seek(300).camera.bearing).toBeCloseTo(210)
    expect(r.seek(300).camera.center).toEqual([149.3, -22])
    const back = rig([shot('a', 100, { mode: 'orbit', orbit_deg_per_s: -6 })])
    expect(back.seek(300).camera.bearing).toBeCloseTo(330)
  })

  it('orbit spin while playing counts real watching time, whatever the replay length', () => {
    const spin = (lengthS: number) => {
      const r = rig([shot('a', 0, { mode: 'orbit', orbit_deg_per_s: 5 })], { lengthS })
      r.play(0, 16)
      let out = r.play(0, 16)
      for (let i = 0; i < 100; i++) out = r.play(1, 200) // 20 s of watching
      return out.camera.bearing
    }
    expect(spin(100)).toBeCloseTo(spin(30))
    expect(spin(100)).toBeCloseTo(90 + 100 + 0.016 * 5, 0)
  })

  it('stands still while the replay is held for a photo moment, then goes on from where it was', () => {
    const r = rig([shot('a', 0, { mode: 'orbit', orbit_deg_per_s: 10 })])
    r.play(0, 16)
    for (let i = 0; i < 30; i++) r.play(1, 100) // past the ease into the shot
    const before = r.play(1, 100).camera.bearing
    let held = before
    for (let i = 0; i < 20; i++) held = r.play(1, 100, { hold: true }).camera.bearing // two seconds held
    expect(held).toBeCloseTo(before, 6)
    // A tenth of a second after the hold is a tenth of a second of turning, not two seconds of it.
    expect(r.play(1, 100).camera.bearing).toBeCloseTo(before + 1, 3)
  })

  it('does not run the ease into a shot while held', () => {
    const r = rig([shot('a', 0)])
    r.play(0, 16)
    const early = r.play(0, 500)
    const held = r.play(0, 1000, { hold: true })
    expect(held.camera).toEqual(early.camera)
  })

  it('a replay speed change or a pace edit mid-shot leaves the bearing continuous', () => {
    const r = rig([shot('a', 0, { mode: 'orbit', orbit_deg_per_s: 10 })])
    r.play(0, 16)
    let out = r.play(100, 16)
    for (let i = 0; i < 6; i++) out = r.play(100, 500) // past the ease
    const before = out.camera.bearing
    r.setLength(15)
    out = r.play(300, 16)
    expect(Math.abs(out.camera.bearing - before - 0.16)).toBeLessThan(0.01)
    // A pace added earlier on the timeline changes the clock mapping, not the spin.
    const r2 = rig([shot('a', 0, { mode: 'orbit', orbit_deg_per_s: 10 }), shot('z', 900, { pace: 0.25 })])
    r2.play(0, 16)
    for (let i = 0; i < 6; i++) r2.play(50, 500)
    const b1 = r2.play(50, 500).camera.bearing
    const b2 = r2.play(60, 500).camera.bearing
    expect(b2 - b1).toBeCloseTo(5, 1)
  })

  it('a seek places the orbit where continuous play would have', () => {
    const r = rig([shot('a', 0, { mode: 'orbit', orbit_deg_per_s: 6 })])
    r.seek(0)
    expect(r.seek(500).camera.bearing).toBeCloseTo((90 + 6 * 50) % 360)
  })

  it('the last shot lasts until the next one starts', () => {
    const r = rig([shot('a', 100, { zoom: 12 }), shot('b', 600, { zoom: 16 })])
    expect(r.seek(599).camera.zoom).toBe(12)
    expect(r.seek(600).camera.zoom).toBe(16)
    expect(r.seek(1000).shotKey).toBe('b')
  })

  it('a flat map keeps zoom and centre only', () => {
    const r = rig([shot('a', 100, { bearing_offset_deg: 90 })], { flat: true })
    const { camera } = r.seek(500)
    expect([camera.pitch, camera.bearing, camera.zoom]).toEqual([0, 0, 14])
    expect(camera.center).toEqual([149.5, -22])
    const o = rig([shot('a', 100, { mode: 'orbit', orbit_deg_per_s: 9 })], { flat: true })
    expect(o.seek(700).camera.bearing).toBe(0)
  })

  it('eases into a shot from the map\'s own camera over 1.5 s of real time', () => {
    const r = rig([shot('a', 100, { zoom: 14 })])
    r.seek(50)
    const first = r.play(100, 16, { current: { ...overview, zoom: 10 } })
    expect(first.camera.zoom).toBeCloseTo(10, 1)
    let out = first
    for (let t = 0; t < TRANSITION_S / 2 * 1000; t += 16) out = r.play(100, 16, { current: { ...overview, zoom: 10 } })
    expect(out.camera.zoom).toBeGreaterThan(11.5)
    expect(out.camera.zoom).toBeLessThan(12.5)
    for (let t = 0; t < 1000; t += 16) out = r.play(100, 16)
    expect(out.camera.zoom).toBe(14)
  })

  it('after the operator moved the map the ease starts from where they left it, with no snap', () => {
    const r = rig([shot('a', 100, { zoom: 12 }), shot('b', 500, { zoom: 16 })])
    r.play(100, 16)
    for (let i = 0; i < 200; i++) r.play(300, 16)
    // The operator had zoomed to 9 and panned away; the next shot begins.
    const theirs: Camera = { center: [140, -10], zoom: 9, pitch: 30, bearing: 200 }
    const first = r.play(500, 16, { current: theirs })
    expect(first.shotKey).toBe('b')
    expect(first.camera.zoom).toBeCloseTo(9, 1)
    expect(first.camera.center[0]).toBeCloseTo(140, 0)
  })

  it('handing control back eases from the map as it is', () => {
    const r = rig([shot('a', 100, { zoom: 14 })])
    for (let i = 0; i < 200; i++) r.play(300, 16)
    const theirs: Camera = { center: [140, -10], zoom: 9, pitch: 30, bearing: 200 }
    const out = r.play(300, 16, { current: theirs, resume: 1 })
    expect(out.camera.zoom).toBeCloseTo(9, 1)
  })

  it('does not ease when the camera is placed by a seek', () => {
    expect(rig([shot('a', 100, { zoom: 14 })]).seek(100).camera.zoom).toBe(14)
  })

  it('reads the shot start once per activation, not every frame', () => {
    const r = rig([shot('a', 0, { mode: 'orbit', orbit_deg_per_s: 1 })])
    let calls = 0
    const ctx = { ...r.ctx, shotStart: (rs: Parameters<CameraContext['shotStart']>[0]) => { calls++; return r.ctx.shotStart(rs) } }
    const d = new CameraDirector()
    const base = { resolved: r.resolved, playing: true, reduced: false, resume: 0, boat: boatAtClock(5), current: overview }
    for (let i = 0; i < 30; i++) d.frame(ctx, { ...base, clock: 5, nowMs: i * 16 })
    expect(calls).toBe(1)
  })

  it('a shot whose passage has no position there gives no camera instead of failing', () => {
    const r = rig([shot('a', 100)])
    const ctx = { ...r.ctx, shotStart: () => null }
    expect(new CameraDirector().frame(ctx, { resolved: r.resolved, clock: 500, playing: true, nowMs: 0, reduced: false, resume: 0, boat: boatAtClock(500), current: overview })).toBeNull()
  })

  it('blends bearings the short way round north', () => {
    const a: Camera = { center: [0, 0], zoom: 10, pitch: 40, bearing: 350 }
    const b: Camera = { center: [0, 0], zoom: 10, pitch: 40, bearing: 10 }
    expect(blendCameras(a, b, 0.5).bearing).toBeCloseTo(0)
    expect(blendCameras(b, a, 0.25).bearing).toBeCloseTo(5)
  })

  it('easeInOut runs 0 to 1 with a flat start and end', () => {
    expect([easeInOut(0), easeInOut(0.5), easeInOut(1), easeInOut(-1), easeInOut(2)]).toEqual([0, 0.5, 1, 0, 1])
    expect(easeInOut(0.1)).toBeLessThan(0.1)
  })

  describe('reduced motion', () => {
    it('cuts to the shot framing: orbit holds its starting bearing, no easing', () => {
      const r = rig([shot('a', 100, { mode: 'orbit', orbit_deg_per_s: 6, zoom: 14 })])
      const out = r.seek(700, { reduced: true })
      expect(out.camera.bearing).toBe(90)
      expect(out.camera.center).toEqual([149.7, -22])
      expect(r.play(700, 16, { reduced: true, current: { ...overview, zoom: 9 } }).camera.zoom).toBe(14)
    })
  })
})

// Two passages end to end, the first with a track and the second not (or after a night at anchor).
const east = (t0: number): TimedSegment => [0, 60, 120, 180, 240, 300].map((dt, i) => ({ t: t0 + dt, lat: 0, lon: 149 + i * 0.001 }))
describe('shotStartState at a boundary between passages', () => {
  const cut = (id: string, t_ms: number): CameraShot => ({ id, t_ms, mode: 'follow', zoom: 14, pitch: 50, bearing_offset_deg: 0, orbit_deg_per_s: 0, pace: 1 })

  it('reads the boat in the passage the shot belongs to when the next one has no track', () => {
    const tl = buildTimeline([
      { id: 'a', start: 1000, end: 1300, segments: [east(1000)] },
      { id: 'b', start: 1300, end: 1500, segments: [] },
    ])
    const [rs] = resolveShots([cut('x', 1300 * 1000)], tl)
    expect(rs.entryId).toBe('a')
    const state = shotStartState(tl, rs)!
    expect(state.lon).toBeCloseTo(149.005)
    expect(state.heading).toBeCloseTo(90, 0)
  })

  it('takes the heading from the first passage when the second starts after a night at anchor', () => {
    const tl = buildTimeline([
      { id: 'a', start: 1000, end: 1300, segments: [east(1000)] },
      { id: 'b', start: 90_000, end: 90_300, segments: [[...east(90_000)].map((p) => ({ ...p, lat: 0.01 * (p.t - 90_000) / 60, lon: 150 }))] },
    ])
    const [rs] = resolveShots([cut('x', 1300 * 1000)], tl)
    expect(shotStartState(tl, rs)!.heading).toBeCloseTo(90, 0)
  })

  it('gives null, not a throw, for a shot in a passage with no track', () => {
    const tl = buildTimeline([{ id: 'a', start: 1000, end: 1300, segments: [] }])
    expect(shotStartState(tl, resolveShots([cut('x', 1100 * 1000)], tl)[0])).toBeNull()
  })
})

describe('outsideCentral', () => {
  it('is true once a point leaves the central 60 percent of the view', () => {
    expect(outsideCentral({ x: 500, y: 300 }, 1000, 600)).toBe(false)
    expect(outsideCentral({ x: 205, y: 300 }, 1000, 600)).toBe(false)
    expect(outsideCentral({ x: 190, y: 300 }, 1000, 600)).toBe(true)
    expect(outsideCentral({ x: 500, y: 100 }, 1000, 600)).toBe(true)
    expect(outsideCentral({ x: 900, y: 300 }, 1000, 600)).toBe(true)
  })
})

describe('CameraGate', () => {
  const camera = (zoom: number, bearing = 0): Camera => ({ center: [149, -22], zoom, pitch: 50, bearing })
  const result = (shotKey: string, cam: Camera, mode: CameraResult['mode'] = 'follow'): CameraResult => ({ camera: cam, shotKey, mode })
  const input = (r: CameraResult, over: Partial<Parameters<CameraGate['place']>[0]> = {}) => ({
    result: r, resume: 0, reduced: false, current: camera(8), boatOutside: () => false, ...over,
  })

  it('places the shot\'s camera, and not one the map already has', () => {
    const gate = new CameraGate()
    expect(gate.place(input(result('a', camera(14))))).toEqual(camera(14))
    expect(gate.place(input(result('a', camera(14)), { current: camera(14, 0.001) }))).toBeNull()
  })

  it('stays out of the way once a person has the map, until the next shot', () => {
    const gate = new CameraGate()
    gate.place(input(result('a', camera(14))))
    gate.yield()
    expect(gate.place(input(result('a', camera(15))))).toBeNull()
    expect(gate.place(input(result('b', camera(12))))).toEqual(camera(12))
  })

  it('takes the camera back when Play or a scrub hands control back', () => {
    const gate = new CameraGate()
    gate.place(input(result('a', camera(14))))
    gate.yield()
    expect(gate.place(input(result('a', camera(15)), { resume: 1 }))).toEqual(camera(15))
  })

  it('in reduced motion cuts at a shot start, then only when the boat nears the edge', () => {
    const gate = new CameraGate()
    expect(gate.place(input(result('a', camera(14)), { reduced: true }))).toEqual(camera(14))
    expect(gate.place(input(result('a', camera(15)), { reduced: true }))).toBeNull()
    expect(gate.place(input(result('a', camera(15)), { reduced: true, boatOutside: () => true }))).toEqual(camera(15))
    // An overview does not follow the boat, so there is nothing to re-cut.
    expect(gate.place(input(result('a', camera(15)), { reduced: true, boatOutside: () => true }))).toEqual(camera(15))
    expect(gate.place(input(result('a', camera(16), 'overview'), { reduced: true, boatOutside: () => true }))).toBeNull()
  })
})

describe('sameCamera', () => {
  it('treats bearings across north as the same turn', () => {
    const a: Camera = { center: [1, 2], zoom: 3, pitch: 4, bearing: 359.999 }
    expect(sameCamera(a, { ...a, bearing: 0 })).toBe(true)
    expect(sameCamera(a, { ...a, bearing: 5 })).toBe(false)
  })
})

describe('zoomBucket', () => {
  it('rounds a zoom to the nearest quarter', () => {
    expect(zoomBucket(9.1)).toBe(9)
    expect(zoomBucket(9.13)).toBe(9.25)
    expect(zoomBucket(9.37)).toBe(9.25)
    expect(zoomBucket(9.9)).toBe(10)
  })
})
