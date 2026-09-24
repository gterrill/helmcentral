import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { downscaleAll, downscaleImage } from '@/lib/image-downscale'

// Lets every currently-queued microtask (a resolved mock promise's `.then`
// continuation, an async function resuming past its own `await`) run before
// the test's next assertion - a real macrotask boundary via setTimeout(0),
// not fake timers, so it works the same whether one or several microtask
// hops separate "resolve this mock promise" from "the next downscale() call
// happens".
async function flushMicrotasks() {
  await new Promise((resolve) => setTimeout(resolve, 0))
}

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

// Item 4 of the pre-release review: bin-quick-add.tsx's addFiles and
// equipment-editor.tsx's addLocalPhotos/uploadPhotosToSavedItem each ran
// Promise.all(files.map(downscaleImage)) - every picked photo decoded into
// memory at once, which risks a phone's tab running out of memory on a
// bin's worth of full-resolution camera photos. downscaleAll replaces all
// three call sites with a small worker pool - these tests pin its own
// concurrency cap and ordering, independent of downscaleImage's real
// canvas/createImageBitmap work (already covered above), via its optional
// third `downscale` parameter.
describe('downscaleAll', () => {
  it('never runs more than `concurrency` downscales at once', async () => {
    const files = Array.from({ length: 5 }, (_, i) => new File([`f${i}`], `f${i}.jpg`))
    const started: number[] = []
    const finish: Array<() => void> = []
    const downscale = vi.fn((file: File) => {
      started.push(Number(file.name.match(/\d+/)![0]))
      return new Promise<Blob>((resolve) => {
        finish.push(() => resolve(new Blob([file.name])))
      })
    })

    const resultPromise = downscaleAll(files, 2, downscale)

    // Only `concurrency` (2) workers start up front - the other 3 files
    // haven't been touched yet.
    expect(started).toEqual([0, 1])

    finish[0]()
    await flushMicrotasks()
    expect(started).toEqual([0, 1, 2])

    finish[1]()
    await flushMicrotasks()
    expect(started).toEqual([0, 1, 2, 3])

    finish[2]()
    await flushMicrotasks()
    expect(started).toEqual([0, 1, 2, 3, 4])

    finish[3]()
    finish[4]()
    await resultPromise
  })

  it('keeps results in the original file order regardless of completion order', async () => {
    const files = Array.from({ length: 4 }, (_, i) => new File([`f${i}`], `f${i}.jpg`))
    const finish: Record<number, (blob: Blob) => void> = {}
    const downscale = vi.fn((file: File) => {
      const index = Number(file.name.match(/\d+/)![0])
      return new Promise<Blob>((resolve) => { finish[index] = resolve })
    })

    const resultPromise = downscaleAll(files, 4, downscale)
    await flushMicrotasks()

    // Resolve deliberately out of order - last file first.
    finish[3](new Blob(['out-3']))
    finish[0](new Blob(['out-0']))
    finish[2](new Blob(['out-2']))
    finish[1](new Blob(['out-1']))

    const outcomes = await resultPromise
    expect(outcomes.map((o) => o.file.name)).toEqual(['f0.jpg', 'f1.jpg', 'f2.jpg', 'f3.jpg'])
    expect(outcomes.map((o) => (o.result.ok ? o.result.blob : null))).toEqual([
      expect.any(Blob), expect.any(Blob), expect.any(Blob), expect.any(Blob),
    ])
  })

  it('reports a per-file failure without losing the other files\' results', async () => {
    const files = [new File(['a'], 'a.jpg'), new File(['b'], 'b.jpg'), new File(['c'], 'c.jpg')]
    const downscale = vi.fn(async (file: File) => {
      if (file.name === 'b.jpg') throw new Error('could not decode b.jpg')
      return new Blob([file.name])
    })

    const outcomes = await downscaleAll(files, 3, downscale)

    expect(outcomes[0].result).toEqual({ ok: true, blob: expect.any(Blob) })
    expect(outcomes[1].result).toEqual({ ok: false, error: 'could not decode b.jpg' })
    expect(outcomes[2].result).toEqual({ ok: true, blob: expect.any(Blob) })
  })

  it('defaults to downscaleImage when no downscale function is given', async () => {
    stubImageBitmap(800, 600)
    stubCanvas({}, new Blob(['jpeg']))

    const outcomes = await downscaleAll([new File(['x'], 'x.jpg')])
    expect(outcomes).toHaveLength(1)
    expect(outcomes[0].result.ok).toBe(true)
  })
})
