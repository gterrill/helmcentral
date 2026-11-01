import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useSignMoments } from '@/hooks/use-sign-moments'
import type { ReplaySign, ResolvedSign } from '@/lib/replay-signs'

const sign = (id: string, extra: Partial<ReplaySign> = {}): ReplaySign => ({
  id, t_ms: 0, title: id, note: '', document_id: `d-${id}`, photo_url: `/api/documents/d-${id}/content`, visibility: 'passing', show_s: 4, starts_leg: false, photo_moment: null, ...extra,
})
const placed = (id: string, clock: number, extra: Partial<ReplaySign> = {}): ResolvedSign => ({ sign: sign(id, extra), clock, at: 0, entryId: 'p1', lat: 0, lon: 0 })

let loads: string[] = []
beforeEach(() => {
  loads = []
  // A photo whose address says it is broken fails to load; any other loads.
  vi.stubGlobal('Image', class {
    onerror: (() => void) | null = null
    onload: (() => void) | null = null
    set src(v: string) {
      loads.push(v)
      queueMicrotask(() => (v.includes('broken') ? this.onerror?.() : this.onload?.()))
    }
  })
})
afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks() })

describe('useSignMoments', () => {
  it('lists the signs whose photos hold the replay', () => {
    const { result } = renderHook(() => useSignMoments([placed('a', 100), placed('b', 300)]))
    expect(result.current).toEqual([{ clock: 100, id: 'a' }, { clock: 300, id: 'b' }])
  })

  it('leaves a moment out once its photo has failed to load, and says so', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const signs = [placed('a', 100), placed('bad', 300, { photo_url: '/api/documents/broken/content' })]
    const { result } = renderHook(() => useSignMoments(signs))
    expect(result.current.map((m) => m.id)).toEqual(['a', 'bad'])
    await act(async () => { await Promise.resolve() })
    expect(result.current.map((m) => m.id)).toEqual(['a'])
    expect(warn).toHaveBeenCalledTimes(1)
    expect(warn).toHaveBeenCalledWith(expect.stringContaining('bad'))
  })

  it('tries again when the sign has a different photo', async () => {
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    const { result, rerender } = renderHook(({ signs }) => useSignMoments(signs), {
      initialProps: { signs: [placed('s', 100, { photo_url: '/api/documents/broken/content' })] },
    })
    await act(async () => { await Promise.resolve() })
    expect(result.current).toEqual([])
    rerender({ signs: [placed('s', 100, { photo_url: '/api/documents/fixed/content' })] })
    expect(result.current).toEqual([{ clock: 100, id: 's' }])
  })

  it('does not load a photo that will not be used', () => {
    renderHook(() => useSignMoments([placed('off', 100, { photo_moment: false }), placed('none', 200, { photo_url: null, document_id: null })]))
    expect(loads).toEqual([])
  })

  it('keeps the same list while only other things about the signs change', () => {
    const { result, rerender } = renderHook(({ signs }) => useSignMoments(signs), { initialProps: { signs: [placed('a', 100)] } })
    const first = result.current
    // A note edited: new sign objects, the same moments.
    rerender({ signs: [placed('a', 100, { note: 'edited' })] })
    expect(result.current).toBe(first)
    // Moved, or switched off: a different list.
    rerender({ signs: [placed('a', 150)] })
    expect(result.current).not.toBe(first)
    expect(result.current).toEqual([{ clock: 150, id: 'a' }])
    rerender({ signs: [placed('a', 150, { photo_moment: false })] })
    expect(result.current).toEqual([])
  })

  it('does not load the same photo again for an unrelated change', () => {
    const { rerender } = renderHook(({ signs }) => useSignMoments(signs), { initialProps: { signs: [placed('a', 100)] } })
    rerender({ signs: [placed('a', 100, { note: 'edited' })] })
    expect(loads).toHaveLength(1)
  })
})
