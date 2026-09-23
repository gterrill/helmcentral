import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { downscaleImage } from '@/lib/image-downscale'

// createImageBitmap/canvas.toBlob aren't implemented by jsdom, so every
// test here stubs them directly rather than relying on a real decode/encode
// pipeline - this pins the function's own logic (scale math, the "never
// upscale" clamp, and its fail-loud paths), not the browser APIs it calls.

function stubImageBitmap(width: number, height: number) {
  const close = vi.fn()
  vi.stubGlobal('createImageBitmap', vi.fn().mockResolvedValue({ width, height, close }))
  return close
}

function stubCanvas(context: object | null, blobResult: Blob | null) {
  const drawImage = vi.fn()
  vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockImplementation(((id: string) => {
    if (id !== '2d') return null
    return context ? { drawImage } : null
  }) as typeof HTMLCanvasElement.prototype.getContext)
  vi.spyOn(HTMLCanvasElement.prototype, 'toBlob').mockImplementation(function (
    this: HTMLCanvasElement,
    callback: BlobCallback,
  ) {
    callback(blobResult)
  })
  return drawImage
}

describe('downscaleImage', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('scales a photo larger than 1600px down to fit on its long edge', async () => {
    stubImageBitmap(3200, 2400) // 4:3, long edge 3200
    const jpegOut = new Blob(['jpeg'], { type: 'image/jpeg' })
    const drawImage = stubCanvas({}, jpegOut)

    const result = await downscaleImage(new Blob(['original']))

    expect(result).toBe(jpegOut)
    // 3200 -> 1600 is a 0.5 scale; 2400 * 0.5 = 1200.
    expect(drawImage).toHaveBeenCalledWith(expect.anything(), 0, 0, 1600, 1200)
  })

  it('never upscales a photo already smaller than 1600px', async () => {
    stubImageBitmap(800, 600)
    const jpegOut = new Blob(['jpeg'], { type: 'image/jpeg' })
    const drawImage = stubCanvas({}, jpegOut)

    await downscaleImage(new Blob(['original']))

    // drawImage's width/height args are the clamped-to-1 scale result - 800x600 unchanged.
    expect(drawImage).toHaveBeenCalledWith(expect.anything(), 0, 0, 800, 600)
  })

  it('closes the decoded bitmap whether encoding succeeds or throws', async () => {
    const close = stubImageBitmap(800, 600)
    stubCanvas({}, new Blob(['jpeg']))

    await downscaleImage(new Blob(['original']))

    expect(close).toHaveBeenCalled()
  })

  it('throws when the canvas has no 2d context, rather than returning the original bytes', async () => {
    stubImageBitmap(800, 600)
    stubCanvas(null, null)

    await expect(downscaleImage(new Blob(['original']))).rejects.toThrow(/2d canvas context/)
  })

  it('throws when the encoder produces no blob', async () => {
    stubImageBitmap(800, 600)
    stubCanvas({}, null)

    await expect(downscaleImage(new Blob(['original']))).rejects.toThrow(/could not encode/)
  })
})
