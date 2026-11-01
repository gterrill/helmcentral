import { describe, expect, it } from 'vitest'

import type { Camera } from '@/lib/replay-camera'
import { INTRO_MS, INTRO_START_ZOOM, MERCATOR, defaultEasing, introProjectionAt, introStartCamera, makeIntroPath } from '@/lib/replay-intro'

const introCameraAt = (f: Camera, t: Camera, ms: number) => makeIntroPath(f, t)(ms)

const from: Camera = { center: [-10, 20], zoom: INTRO_START_ZOOM, pitch: 0, bearing: 0 }
const to: Camera = { center: [149.2, -20.5], zoom: 9.3, pitch: 60, bearing: 0 }

describe('the opening move', () => {
  it('starts on the start camera and ends on the target', () => {
    const start = introCameraAt(from, to, 0)
    expect(start.center[0]).toBeCloseTo(-10, 6)
    expect(start.center[1]).toBeCloseTo(20, 6)
    expect(start.zoom).toBeCloseTo(INTRO_START_ZOOM, 9)
    expect(introCameraAt(from, to, INTRO_MS)).toEqual(to)
    expect(introCameraAt(from, to, INTRO_MS * 5)).toEqual(to)
  })

  it('zooms in without ever backing off, and pitches up steadily', () => {
    let zoom = -Infinity
    let pitch = -Infinity
    for (let ms = 0; ms <= INTRO_MS; ms += 40) {
      const c = introCameraAt(from, to, ms)
      expect(c.zoom).toBeGreaterThanOrEqual(zoom)
      expect(c.pitch).toBeGreaterThanOrEqual(pitch)
      zoom = c.zoom
      pitch = c.pitch
    }
  })

  it('uses MapLibre\'s default easing: slow start, fast middle, slow end', () => {
    expect(defaultEasing(0)).toBe(0)
    expect(defaultEasing(1)).toBe(1)
    // CSS "ease" at x = 0.5 is 0.8024.
    expect(defaultEasing(0.5)).toBeCloseTo(0.8024, 3)
    expect(defaultEasing(0.1)).toBeLessThan(0.1)
  })

  it('moves the centre along the way and never leaves the segment between the two', () => {
    const mid = introCameraAt(from, to, INTRO_MS / 2)
    expect(mid.center[0]).toBeGreaterThan(-10)
    expect(mid.center[0]).toBeLessThan(149.2)
  })

  it('turns the short way round north', () => {
    const c = introCameraAt({ ...from, bearing: 350 }, { ...to, bearing: 10 }, INTRO_MS / 2)
    // Through north, and normalised like every other camera.
    expect(c.bearing).toBeGreaterThanOrEqual(0)
    expect(c.bearing).toBeLessThan(10)
  })

  it('is a pure function of the time spent in it', () => {
    const a = introCameraAt(from, to, 1234)
    introCameraAt(from, to, 3000)
    expect(introCameraAt(from, to, 1234)).toEqual(a)
  })
})

describe('the globe', () => {
  const stops = (p: { projection: unknown }) => (p.projection as { type: unknown[] }).type

  it('starts on the globe and blends to the flat map by zoom', () => {
    const t = stops(introProjectionAt(to, 0))
    expect(t.slice(0, 3)).toEqual(['interpolate', ['linear'], ['zoom']])
    expect(t[4]).toBe('vertical-perspective')
    expect(t[6]).toBe('mercator')
  })

  it('has finished the blend before the move lands, and says where it is flat', () => {
    const p = introProjectionAt(to, 100)
    expect(stops(p)[5]).toBe(p.flatAtZoom)
    expect(p.flatAtZoom).toBeLessThanOrEqual(to.zoom)
    expect(p.flatAtZoom).toBe(5.5)
    expect(introProjectionAt({ ...to, zoom: 3 }, 100).flatAtZoom).toBe(3)
  })

  it('has strictly rising zoom stops, whatever the landing zoom', () => {
    for (const zoom of [0.5, 1, 2, 4, 9, 14]) {
      const p = introProjectionAt({ ...to, zoom }, 0)
      if (p.flatAtZoom === null) continue
      const s = stops(p)
      expect(s[5] as number).toBeGreaterThan(s[3] as number)
    }
  })

  it('is the flat map once the move is over, and when there is no globe to leave', () => {
    for (const p of [introProjectionAt(to, INTRO_MS), introProjectionAt(to, INTRO_MS + 500), introProjectionAt({ ...to, zoom: 0 }, 0), introProjectionAt(to, 0, 0)]) {
      expect(p).toEqual({ projection: MERCATOR, flatAtZoom: null })
    }
  })
})

describe('introStartCamera', () => {
  it('starts over the middle of the track, flat, with the whole earth in view', () => {
    expect(introStartCamera([148, -21, 150, -19])).toEqual({ center: [149, -20], zoom: INTRO_START_ZOOM, pitch: 0, bearing: 0 })
  })
  it('keeps the bearing it is given', () => {
    expect(introStartCamera([148, -21, 150, -19], 30).bearing).toBe(30)
  })
})
