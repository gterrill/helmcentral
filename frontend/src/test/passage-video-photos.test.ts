import { afterEach, describe, expect, it, vi } from 'vitest'

import { loadPhotos, photoDrawWidth, type PhotoDeps } from '@/lib/passage-video/photos'
import { VIDEO_SIZES } from '@/lib/passage-video/schedule'
import type { ReplaySign } from '@/lib/replay-signs'

const sign = (id: string, photo = true): ReplaySign => ({
  id, t_ms: 0, title: id, note: '', document_id: photo ? 'd' : null, photo_url: photo ? `/p/${id}` : null, visibility: 'passing', show_s: 4, starts_leg: false, photo_moment: null,
})

afterEach(() => { vi.unstubAllGlobals() })

describe('photoDrawWidth', () => {
  it('is the photo in the biggest card a moment makes, to the end of the slow zoom, in the video\'s pixels', () => {
    // Landscape: the frame is 1280 by 720 in the card's pixels, and 70 percent of the 720 high is 504, 2.25 times the 224 high card. The 240 wide photo is then 540, made 1.5 times bigger for the video and 6 percent more by the zoom.
    expect(photoDrawWidth(VIDEO_SIZES.landscape)).toBe(Math.ceil(240 * 2.25 * 1.5 * 1.06))
    // Portrait: 720 wide, and 70 percent of it is 504, 1.96875 times the card's 256.
    expect(photoDrawWidth(VIDEO_SIZES.portrait)).toBe(Math.ceil(240 * 1.96875 * 1.5 * 1.06))
  })

  it('is less than the width of the frame, which the old rule gave it', () => {
    expect(photoDrawWidth(VIDEO_SIZES.landscape)).toBeLessThan(1068)
    expect(photoDrawWidth(VIDEO_SIZES.portrait)).toBeLessThan(VIDEO_SIZES.portrait.width)
  })
})

describe('loadPhotos', () => {
  const deps = (): PhotoDeps & { widths: number[] } => {
    const widths: number[] = []
    return {
      widths,
      fetchPhoto: async () => new Blob(['x']),
      makeBitmap: async (_b, w) => { widths.push(w); return { close: vi.fn() } as unknown as ImageBitmap },
    }
  }

  it('decodes each photo no wider than it will be drawn', async () => {
    const d = deps()
    const set = await loadPhotos([sign('a'), sign('b'), sign('c', false)], 1068, undefined, d)
    expect([...set.bitmaps.keys()].sort()).toEqual(['a', 'b'])
    expect(d.widths).toEqual([1068, 1068])
  })

  it('asks the browser to decode it at that width, keeping its shape', async () => {
    const createImageBitmap = vi.fn(async () => ({ close() {} }))
    vi.stubGlobal('createImageBitmap', createImageBitmap)
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: true, blob: async () => new Blob(['x']) })))
    const set = await loadPhotos([sign('a')], 1068)
    expect(set.bitmaps.size).toBe(1)
    expect(createImageBitmap).toHaveBeenCalledWith(expect.any(Blob), { resizeWidth: 1068, resizeQuality: 'high' })
  })

  it('notes a photo that would not load, and carries on with the rest', async () => {
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    const d = deps()
    d.fetchPhoto = async (url) => { if (url.endsWith('/a')) throw new Error('HTTP 404'); return new Blob(['x']) }
    const set = await loadPhotos([sign('a'), sign('b')], 1068, undefined, d)
    expect([...set.failed]).toEqual(['a'])
    expect([...set.bitmaps.keys()]).toEqual(['b'])
  })
})
