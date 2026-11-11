import { describe, expect, it } from 'vitest'

import { dHash, greyFromRgba, hammingHex, laplacianVariance, resizeGrey } from '@/lib/image-fingerprint'

const grid = (w: number, h: number, f: (x: number, y: number) => number) => {
  const g = new Uint8Array(w * h)
  for (let y = 0; y < h; y++) for (let x = 0; x < w; x++) g[y * w + x] = f(x, y)
  return g
}

describe('greyFromRgba', () => {
  it('weights the channels as luma and ignores alpha', () => {
    const rgba = new Uint8ClampedArray([255, 0, 0, 255, 0, 255, 0, 255, 0, 0, 255, 0, 255, 255, 255, 255])
    const grey = greyFromRgba(rgba)
    expect(grey[0]).toBe(76)
    expect(grey[1]).toBe(150)
    expect(grey[2]).toBe(29)
    expect(grey[3]).toBe(255)
  })
})

describe('resizeGrey', () => {
  it('averages the cells it folds together', () => {
    const out = resizeGrey(new Uint8Array([0, 100, 200, 100]), 2, 2, 1, 1)
    expect(out[0]).toBe(100)
  })
  it('keeps a flat picture flat at any size', () => {
    const out = resizeGrey(grid(37, 23, () => 90), 37, 23, 9, 8)
    expect([...out].every((v) => v === 90)).toBe(true)
  })
})

describe('dHash', () => {
  const ramp = grid(64, 64, (x) => x * 3)
  it('is 16 hex characters', () => {
    expect(dHash(ramp, 64, 64)).toMatch(/^[0-9a-f]{16}$/)
  })
  it('does not change with brightness or contrast', () => {
    const bright = grid(64, 64, (x) => x * 3 + 40)
    const flat = grid(64, 64, (x) => 20 + x)
    expect(dHash(bright, 64, 64)).toBe(dHash(ramp, 64, 64))
    expect(dHash(flat, 64, 64)).toBe(dHash(ramp, 64, 64))
  })
  it('stays close for a slightly shifted frame and far for another scene', () => {
    const scene = grid(96, 96, (x, y) => ((x >> 3) * 37 + (y >> 3) * 91) % 256)
    const shifted = grid(96, 96, (x, y) => ((((x + 2) >> 3) * 37) + (y >> 3) * 91) % 256)
    const other = grid(96, 96, (x, y) => ((x >> 2) * 151 + (y >> 4) * 13 + 77) % 256)
    const a = dHash(scene, 96, 96)
    expect(hammingHex(a, dHash(shifted, 96, 96))).toBeLessThan(hammingHex(a, dHash(other, 96, 96)))
  })
  it('left-to-right ramp and its mirror are opposites', () => {
    const mirror = grid(64, 64, (x) => (63 - x) * 3)
    expect(hammingHex(dHash(ramp, 64, 64), dHash(mirror, 64, 64))).toBe(64)
  })
})

describe('hammingHex', () => {
  it('counts the bits that differ', () => {
    expect(hammingHex('0000000000000000', '0000000000000000')).toBe(0)
    expect(hammingHex('0000000000000000', '000000000000000f')).toBe(4)
    expect(hammingHex('ffffffffffffffff', '0000000000000000')).toBe(64)
  })
  it('refuses fingerprints of different lengths', () => {
    expect(() => hammingHex('00', '0000')).toThrow(/length/)
  })
})

describe('laplacianVariance', () => {
  it('is zero for a flat picture', () => {
    expect(laplacianVariance(grid(32, 32, () => 128), 32, 32)).toBe(0)
  })
  it('is higher for crisp edges than for a soft ramp', () => {
    const crisp = grid(32, 32, (x, y) => ((x + y) % 2 === 0 ? 0 : 255))
    const soft = grid(32, 32, (x) => x * 4)
    expect(laplacianVariance(crisp, 32, 32)).toBeGreaterThan(laplacianVariance(soft, 32, 32) + 1000)
  })
  it('a picture too small to have an interior has no score to give', () => {
    expect(() => laplacianVariance(new Uint8Array(4), 2, 2)).toThrow(/small/)
  })
})
