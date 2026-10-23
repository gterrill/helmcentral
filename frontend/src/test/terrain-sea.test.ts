import { describe, expect, it } from 'vitest'

import { clampSeaPixels, TERRAIN_SEA_FLOOR } from '@/lib/terrain-sea'

// The same cases as the boat's own proxy (backend/terrain_proxy.go,
// clampTerrainSeaToZero): Terrarium encodes elevation as R*256 + G + B/256 -
// 32768, so 0 m is (128, 0, 0) and anything with a 24-bit value under
// 128<<16 is below the sea.
const px = (...rgba: number[]) => new Uint8ClampedArray(rgba)

describe('clampSeaPixels', () => {
  it('uses the same floor as the proxy: 128 << 16', () => {
    expect(TERRAIN_SEA_FLOOR).toBe(128 * 65536)
  })

  it('sets a pixel below sea level to exactly 0 m, opaque', () => {
    const data = px(127, 255, 255, 255, 0, 0, 0, 255, 100, 20, 30, 0)
    expect(clampSeaPixels(data)).toBe(3)
    expect([...data]).toEqual([128, 0, 0, 255, 128, 0, 0, 255, 128, 0, 0, 255])
  })

  it('leaves 0 m and everything above it alone', () => {
    const data = px(128, 0, 0, 255, 128, 0, 1, 255, 129, 0, 0, 255, 200, 10, 20, 255)
    const before = [...data]
    expect(clampSeaPixels(data)).toBe(0)
    expect([...data]).toEqual(before)
  })

  it('handles a mixed tile and reports how many pixels it changed', () => {
    const data = px(120, 0, 0, 255, 130, 5, 6, 255, 127, 255, 255, 255, 255, 255, 255, 255)
    expect(clampSeaPixels(data)).toBe(2)
    expect([...data]).toEqual([128, 0, 0, 255, 130, 5, 6, 255, 128, 0, 0, 255, 255, 255, 255, 255])
  })

  it('refuses data that is not whole pixels', () => {
    expect(() => clampSeaPixels(new Uint8ClampedArray(7))).toThrow(/whole pixels/)
  })
})
