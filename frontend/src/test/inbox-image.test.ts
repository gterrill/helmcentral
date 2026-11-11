import { afterEach, describe, expect, it, vi } from 'vitest'

import { INBOX_MAX_DIMENSION, INBOX_THUMB_DIMENSION, processInboxImage } from '@/lib/inbox-image'

// createImageBitmap and canvas are not in jsdom, so they are stubbed: this pins
// how many times the photo is decoded and what is drawn from that one decode.
describe('processInboxImage', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('decodes the photo once and makes the shrunk copy, the thumbnail and the fingerprint from that', async () => {
    const close = vi.fn()
    const createImageBitmap = vi.fn().mockResolvedValue({ width: 3200, height: 2400, close })
    vi.stubGlobal('createImageBitmap', createImageBitmap)
    const draws: number[][] = []
    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockImplementation((() => ({
      drawImage: (_s: unknown, _x: number, _y: number, w: number, h: number) => { draws.push([w, h]) },
      getImageData: (_x: number, _y: number, w: number, h: number) => ({ data: new Uint8ClampedArray(w * h * 4) }),
    })) as unknown as typeof HTMLCanvasElement.prototype.getContext)
    vi.spyOn(HTMLCanvasElement.prototype, 'toBlob').mockImplementation(function (cb: BlobCallback, _type?: string, quality?: unknown) {
      cb(new Blob([`q${quality}`], { type: 'image/jpeg' }))
    })

    const got = await processInboxImage(new Blob(['original']))

    expect(createImageBitmap).toHaveBeenCalledTimes(1)
    expect(close).toHaveBeenCalledTimes(1)
    expect(INBOX_MAX_DIMENSION).toBe(2560)
    expect(INBOX_THUMB_DIMENSION).toBe(400)
    expect(draws).toContainEqual([2560, 1920])
    expect(draws).toContainEqual([400, 300])
    expect(draws).toContainEqual([256, 192])
    expect(got.blob.size).toBeGreaterThan(0)
    expect(got.thumb).not.toBe(got.blob)
    expect(got.fingerprint).toMatch(/^[0-9a-f]{16}$/)
  })
})
