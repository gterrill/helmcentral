import { describe, expect, it } from 'vitest'

import type { Camera } from '@/lib/replay-camera'
import { INTRO_MS, INTRO_START_ZOOM, defaultEasing, makeIntroPath } from '@/lib/replay-intro'

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
