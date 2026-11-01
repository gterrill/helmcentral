import { afterEach, describe, expect, it, vi } from 'vitest'

import { loadPhotos, photoDrawWidth, type PhotoDeps } from '@/lib/passage-video/photos'
import { VIDEO_SIZES } from '@/lib/passage-video/schedule'
import type { ReplaySign } from '@/lib/replay-signs'

const sign = (id: string, photo = true): ReplaySign => ({
  id, t_ms: 0, title: id, note: '', document_id: photo ? 'd' : null, photo_url: photo ? `/p/${id}` : null, visibility: 'passing', show_s: 4, starts_leg: false, photo_moment: null,
})

afterEach(() => { vi.unstubAllGlobals() })

describe('photoDrawWidth', () => {
  it('is the width of the moment at full size, with room for its slow zoom', () => {
    // 70 percent of 1080 high is 756, as wide as 4:3 makes 1008; the slow zoom is 6 percent more.
    expect(photoDrawWidth(VIDEO_SIZES.landscape)).toBe(Math.ceil(1008 * 1.06))
    // Portrait is limited by its width: 70 percent of 1080.
    expect(photoDrawWidth(VIDEO_SIZES.portrait)).toBe(Math.ceil(756 * 1.06))
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
