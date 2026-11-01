import { describe, expect, it } from 'vitest'

import { lightFor } from '@/lib/replay-light'
import { SKY_DAY } from '@/lib/terrain'

const rgb = (hex: string) => [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16))
const luma = (hex: string) => { const [r, g, b] = rgb(hex); return 0.2126 * r + 0.7152 * g + 0.0722 * b }

describe('lightFor', () => {
  it('is the app\'s own day sky once the sun is well up', () => {
    const day = lightFor({ altitudeDeg: 40, azimuthDeg: 200 })
    expect(day.sky['sky-color']).toBe(SKY_DAY['sky-color'])
    expect(day.sky['horizon-color']).toBe(SKY_DAY['horizon-color'])
    expect(day.light.color).toBe('#ffffff')
  })

  it('goes dark through twilight to night, and no darker below that', () => {
    const noon = lightFor({ altitudeDeg: 40, azimuthDeg: 180 })
    const dusk = lightFor({ altitudeDeg: -6, azimuthDeg: 270 })
    const night = lightFor({ altitudeDeg: -12, azimuthDeg: 300 })
    const deep = lightFor({ altitudeDeg: -45, azimuthDeg: 0 })
    expect(luma(noon.sky['sky-color'])).toBeGreaterThan(luma(dusk.sky['sky-color']))
    expect(luma(dusk.sky['sky-color'])).toBeGreaterThan(luma(night.sky['sky-color']))
    expect(deep.sky).toEqual(night.sky)
    expect(deep.light.intensity).toBe(night.light.intensity)
  })

  it('keeps some light at night, so the boat model is still readable', () => {
    expect(lightFor({ altitudeDeg: -30, azimuthDeg: 0 }).light.intensity).toBeGreaterThanOrEqual(0.1)
  })

  it('turns the horizon warm at sunrise and golden hour, and cool by day', () => {
    for (const alt of [0, 3]) {
      const [r, , b] = rgb(lightFor({ altitudeDeg: alt, azimuthDeg: 90 }).sky['horizon-color'])
      expect(r).toBeGreaterThan(b)
    }
    const [r, , b] = rgb(lightFor({ altitudeDeg: 40, azimuthDeg: 90 }).sky['horizon-color'])
    expect(b).toBeGreaterThanOrEqual(r)
  })

  it('places the map light on the sun: azimuth from north, polar angle from straight up', () => {
    const l = lightFor({ altitudeDeg: 30, azimuthDeg: 120 })
    expect(l.light.anchor).toBe('map')
    expect(l.light.position[1]).toBe(120)
    expect(l.light.position[2]).toBe(60)
    expect(l.hillshade.direction).toBe(120)
  })

  it('lights the relief from a fixed corner when the sun is down', () => {
    const night = lightFor({ altitudeDeg: -20, azimuthDeg: 10 })
    expect(night.hillshade.direction).toBe(335)
    expect(night.hillshade.exaggeration).toBeLessThan(lightFor({ altitudeDeg: 3, azimuthDeg: 10 }).hillshade.exaggeration)
  })

  it('gives the same key, and the same object values, for a sun that has barely moved', () => {
    const a = lightFor({ altitudeDeg: 20.1, azimuthDeg: 100.2 })
    const b = lightFor({ altitudeDeg: 20.2, azimuthDeg: 100.7 })
    expect(b.key).toBe(a.key)
    expect(b).toEqual(a)
  })

  it('gives a new key once the sun has moved enough to see', () => {
    expect(lightFor({ altitudeDeg: 20, azimuthDeg: 100 }).key).not.toBe(lightFor({ altitudeDeg: 21, azimuthDeg: 100 }).key)
    expect(lightFor({ altitudeDeg: 20, azimuthDeg: 100 }).key).not.toBe(lightFor({ altitudeDeg: 20, azimuthDeg: 105 }).key)
  })

  describe('the base map', () => {
    it('is dark blue and heavy at night', () => {
      const night = lightFor({ altitudeDeg: -20, azimuthDeg: 0 })
      expect(night.tint.opacity).toBeGreaterThanOrEqual(0.55)
      expect(night.tint.opacity).toBeLessThanOrEqual(0.65)
      const [r, , b] = rgb(night.tint.colour)
      expect(b).toBeGreaterThan(r)
      expect(luma(night.tint.colour)).toBeLessThan(60)
    })

    it('is blue to purple at about a third in civil twilight', () => {
      const dusk = lightFor({ altitudeDeg: -6, azimuthDeg: 270 })
      expect(dusk.tint.opacity).toBeGreaterThan(0.3)
      expect(dusk.tint.opacity).toBeLessThan(0.4)
      const [r, , b] = rgb(dusk.tint.colour)
      expect(b).toBeGreaterThan(r)
    })

    it('is a light warm amber at sunrise, sunset and golden hour', () => {
      for (const alt of [0, 3, 6]) {
        const g = lightFor({ altitudeDeg: alt, azimuthDeg: 270 })
        expect(g.tint.opacity).toBeGreaterThanOrEqual(0.12)
        expect(g.tint.opacity).toBeLessThanOrEqual(0.18)
        const [r, , b] = rgb(g.tint.colour)
        expect(r).toBeGreaterThan(b)
      }
    })

    it('is nothing by day', () => {
      expect(lightFor({ altitudeDeg: 25, azimuthDeg: 180 }).tint.opacity).toBe(0)
      expect(lightFor({ altitudeDeg: 60, azimuthDeg: 180 }).tint.opacity).toBe(0)
    })

    it('gets lighter as the sun comes up through the dark, and never lighter than day', () => {
      let before = -1
      for (const alt of [-30, -12, -9, -6, -3, 0]) {
        const b = lightFor({ altitudeDeg: alt, azimuthDeg: 90 }).raster['raster-brightness-max']
        expect(b).toBeGreaterThanOrEqual(before)
        expect(b).toBeLessThanOrEqual(1)
        before = b
      }
      expect(lightFor({ altitudeDeg: 40, azimuthDeg: 90 }).raster['raster-brightness-max']).toBe(1)
      expect(lightFor({ altitudeDeg: -30, azimuthDeg: 90 }).raster['raster-brightness-max']).toBeLessThan(0.6)
    })

    it('leaves a daytime picture as it is', () => {
      const day = lightFor({ altitudeDeg: 40, azimuthDeg: 90 })
      expect(day.raster['raster-saturation']).toBe(0)
    })

    it('is covered by the same key as the sky, so the map is touched only when the light is', () => {
      const a = lightFor({ altitudeDeg: -5.1, azimuthDeg: 10 })
      const b = lightFor({ altitudeDeg: -5.2, azimuthDeg: 10.4 })
      expect(b).toEqual(a)
    })
  })
})
