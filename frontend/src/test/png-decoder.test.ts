import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { deflateSync } from 'node:zlib'

import { describe, expect, it } from 'vitest'

import { decodePng, unfilter } from '@/export-player/png'
import { clampSeaPixels } from '@/lib/terrain-sea'

// Real Terrarium tiles from AWS, and the pixels an independent decoder (Python's
// zlib and its own filter code) read from them, as hashes. A canvas is never
// used to read elevation: see export-player/png.ts.
const dir = join(__dirname, 'fixtures/terrarium')
const expected = JSON.parse(readFileSync(join(dir, 'expected.json'), 'utf8')) as Record<string, { width: number; height: number; rgbaSha256: string; clampedSha256: string; seaPixels: number }>
const sha = (b: Uint8ClampedArray | Uint8Array) => createHash('sha256').update(b).digest('hex')

describe('decoding real Terrarium tiles', () => {
  for (const [file, want] of Object.entries(expected)) {
    it(`reads ${file} exactly, and flattens the sea as the independent decoder did`, async () => {
      const px = await decodePng(new Uint8Array(readFileSync(join(dir, file))))
      expect([px.width, px.height]).toEqual([want.width, want.height])
      expect(sha(px.data)).toBe(want.rgbaSha256)
      expect(clampSeaPixels(px.data)).toBe(want.seaPixels)
      expect(sha(px.data)).toBe(want.clampedSha256)
    })
  }
})

// A PNG written here, with a chosen filter on every row, so each of the five
// filter types is undone against pixels we know.
function crc32(buf: Uint8Array) {
  let c = ~0
  for (const b of buf) { c ^= b; for (let k = 0; k < 8; k++) c = (c >>> 1) ^ (0xedb88320 & -(c & 1)) }
  return ~c >>> 0
}
const chunk = (type: string, body: Uint8Array) => {
  const out = new Uint8Array(12 + body.length)
  const v = new DataView(out.buffer)
  v.setUint32(0, body.length)
  out.set([...type].map((ch) => ch.charCodeAt(0)), 4)
  out.set(body, 8)
  v.setUint32(8 + body.length, crc32(out.subarray(4, 8 + body.length)))
  return out
}
const paeth = (a: number, b: number, c: number) => {
  const p = a + b - c, pa = Math.abs(p - a), pb = Math.abs(p - b), pc = Math.abs(p - c)
  return pa <= pb && pa <= pc ? a : pb <= pc ? b : c
}

function encode(pixels: Uint8Array, width: number, height: number, bpp: 3 | 4, filters: number[], idatSplit = 1): Uint8Array {
  const stride = width * bpp
  const raw = new Uint8Array(height * (stride + 1))
  for (let y = 0; y < height; y++) {
    const f = filters[y % filters.length]
    raw[y * (stride + 1)] = f
    for (let i = 0; i < stride; i++) {
      const x = pixels[y * stride + i]
      const a = i >= bpp ? pixels[y * stride + i - bpp] : 0
      const b = y > 0 ? pixels[(y - 1) * stride + i] : 0
      const c = y > 0 && i >= bpp ? pixels[(y - 1) * stride + i - bpp] : 0
      const pred = [0, a, b, (a + b) >> 1, paeth(a, b, c)][f]
      raw[y * (stride + 1) + 1 + i] = (x - pred) & 255
    }
  }
  const ihdr = new Uint8Array(13)
  const v = new DataView(ihdr.buffer)
  v.setUint32(0, width); v.setUint32(4, height)
  ihdr[8] = 8; ihdr[9] = bpp === 3 ? 2 : 6
  const z = new Uint8Array(deflateSync(raw))
  const size = Math.ceil(z.length / idatSplit)
  const idats: Uint8Array[] = []
  for (let p = 0; p < z.length; p += size) idats.push(chunk('IDAT', z.subarray(p, p + size)))
  const parts = [new Uint8Array([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]), chunk('IHDR', ihdr), ...idats, chunk('IEND', new Uint8Array(0))]
  const out = new Uint8Array(parts.reduce((n, p) => n + p.length, 0))
  let at = 0
  for (const p of parts) { out.set(p, at); at += p.length }
  return out
}

function noise(n: number, seed: number) {
  const out = new Uint8Array(n)
  let s = seed
  for (let i = 0; i < n; i++) { s = (s * 1103515245 + 12345) >>> 0; out[i] = (s >>> 16) & 255 }
  return out
}

describe('the five PNG row filters', () => {
  const names = ['None', 'Sub', 'Up', 'Average', 'Paeth']
  for (const bpp of [3, 4] as const) {
    names.forEach((name, f) => {
      it(`undoes ${name} on 8-bit ${bpp === 3 ? 'RGB' : 'RGBA'}`, async () => {
        const w = 17, h = 9
        const pixels = noise(w * h * bpp, 7 + f)
        const px = await decodePng(encode(pixels, w, h, bpp, [f]))
        const want = new Uint8ClampedArray(w * h * 4)
        for (let i = 0; i < w * h; i++) {
          want.set(pixels.subarray(i * bpp, i * bpp + 3), i * 4)
          want[i * 4 + 3] = bpp === 4 ? pixels[i * bpp + 3] : 255
        }
        expect([...px.data]).toEqual([...want])
      })
    })
  }

  it('undoes a different filter on each row, and image data split across IDAT chunks', async () => {
    const w = 11, h = 10
    const pixels = noise(w * h * 3, 99)
    const px = await decodePng(encode(pixels, w, h, 3, [0, 1, 2, 3, 4, 4, 3, 2, 1, 0], 4))
    expect(px.data[0]).toBe(pixels[0])
    expect(px.data[(h * w - 1) * 4 + 2]).toBe(pixels[h * w * 3 - 1])
    expect(sha(px.data)).toBe(sha(await decodePng(encode(pixels, w, h, 3, [0]))
      .then((p) => p.data)))
  })

  it('refuses a filter byte that is not a filter', () => {
    expect(() => unfilter(new Uint8Array([9, 1, 2, 3]), 1, 1, 3)).toThrow(/filter 9/)
  })

  it('refuses what is not a Terrarium tile, loudly', async () => {
    await expect(decodePng(new Uint8Array([1, 2, 3]))).rejects.toThrow(/Not a PNG/)
    const palette = encode(noise(12, 1), 2, 2, 3, [0])
    palette[24] = 4 // colour type: grey with alpha
    await expect(decodePng(palette)).rejects.toThrow(/not a Terrarium tile/)
  })
})
