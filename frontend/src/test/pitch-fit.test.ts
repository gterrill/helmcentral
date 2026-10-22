import { describe, expect, it } from 'vitest'

import { fitZoomForPitch, projectAtPitch } from '@/lib/pitch-fit'

const view = { width: 800, height: 400, padding: { top: 48, bottom: 80, left: 48, right: 48 } }
const bounds: [number, number, number, number] = [148, -21, 149, -20]
const centre = { lon: 148.5, lat: -20.5 }

describe('projectAtPitch', () => {
  it('puts the centre on the middle of the screen at any pitch', () => {
    for (const pitch of [0, 50, 70]) {
      const p = projectAtPitch({ ...centre, zoom: 8, pitch, width: 800, height: 400 }, centre)
      expect(p?.x).toBeCloseTo(400, 6)
      expect(p?.y).toBeCloseTo(200, 6)
    }
  })

  it('pushes the far edge up the screen and the near edge down, more than a flat map would', () => {
    const cam = { ...centre, zoom: 8, pitch: 0, width: 800, height: 400 }
    const flatNorth = projectAtPitch(cam, { lon: 148.5, lat: -20 })!
    const tiltedNorth = projectAtPitch({ ...cam, pitch: 60 }, { lon: 148.5, lat: -20 })!
    expect(tiltedNorth.y).toBeLessThan(200)
    // Foreshortening: the tilted far point sits nearer the middle than flat.
    expect(Math.abs(200 - tiltedNorth.y)).toBeLessThan(Math.abs(200 - flatNorth.y))
  })

  it('returns null for a point beyond the horizon', () => {
    expect(projectAtPitch({ ...centre, zoom: 2, pitch: 85, width: 800, height: 400 }, { lon: 148.5, lat: -80 })).toBeNull()
  })
})

describe('fitZoomForPitch', () => {
  it('leaves a flat camera alone', () => {
    expect(fitZoomForPitch({ bounds, centre, zoom: 8, pitch: 0, ...view })).toBe(8)
  })

  it('backs off a zoom that fitted flat but loses the ends when tilted', () => {
    // Zoom that just fits the box flat in the padded viewport.
    const tight = fitZoomForPitch({ bounds, centre, zoom: 20, pitch: 0, ...view })
    expect(tight).toBe(20)
    const flatFit = 8.6
    const tilted = fitZoomForPitch({ bounds, centre, zoom: flatFit, pitch: 60, ...view })
    expect(tilted).toBeLessThan(flatFit)
  })

  it('returns a zoom at which every corner is inside the padded viewport', () => {
    const zoom = fitZoomForPitch({ bounds, centre, zoom: 12, pitch: 60, ...view })
    const corners = [[bounds[0], bounds[1]], [bounds[2], bounds[1]], [bounds[2], bounds[3]], [bounds[0], bounds[3]]]
    for (const [lon, lat] of corners) {
      const p = projectAtPitch({ ...centre, zoom, pitch: 60, width: view.width, height: view.height }, { lon, lat })!
      expect(p.x).toBeGreaterThanOrEqual(view.padding.left - 1e-6)
      expect(p.x).toBeLessThanOrEqual(view.width - view.padding.right + 1e-6)
      expect(p.y).toBeGreaterThanOrEqual(view.padding.top - 1e-6)
      expect(p.y).toBeLessThanOrEqual(view.height - view.padding.bottom + 1e-6)
    }
  })

  it('does not back off a zoom that already fits', () => {
    expect(fitZoomForPitch({ bounds, centre, zoom: 5, pitch: 60, ...view })).toBe(5)
  })
})
