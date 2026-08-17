import { describe, it, expect, beforeEach, vi } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useAnchorWatch } from '@/hooks/use-anchor-watch'

// setAnchorHere is fed the live GPS fix (App.tsx / anchor-watch-tile.tsx), so
// it must ask the backend to apply the bow-offset correction. updatePosition
// is a user-dragged map point that is already meant to be the anchor
// (AnchorWatchMap's onAnchorReposition), so it must NOT — or dragging the
// anchor would shove it `d` metres forward on every reposition.
describe('useAnchorWatch bow-offset request shape', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ active: true, lat: -21.1, lon: 149.2 }),
    }))
  })

  it('setAnchorHere posts apply_bow_offset: true', async () => {
    const fetchMock = vi.mocked(fetch)
    const { result } = renderHook(() => useAnchorWatch(-21.1, 149.2, null, 3600))

    // Let the initial GET /api/anchor-watch on mount resolve and clear.
    await act(async () => { await Promise.resolve() })
    fetchMock.mockClear()

    await act(async () => {
      await result.current.setAnchorHere(-21.1, 149.2)
    })

    expect(fetchMock).toHaveBeenCalledWith('/api/anchor-watch', expect.objectContaining({
      method: 'POST',
    }))
    const [, init] = fetchMock.mock.calls[0]
    const body = JSON.parse(init!.body as string)
    expect(body.apply_bow_offset).toBe(true)
  })

  it('updatePosition does not send apply_bow_offset', async () => {
    const fetchMock = vi.mocked(fetch)
    const { result } = renderHook(() => useAnchorWatch(-21.1, 149.2, null, 3600))

    await act(async () => { await Promise.resolve() })
    fetchMock.mockClear()

    await act(async () => {
      await result.current.updatePosition(-21.1, 149.2)
    })

    expect(fetchMock).toHaveBeenCalledWith('/api/anchor-watch', expect.objectContaining({
      method: 'POST',
    }))
    const [, init] = fetchMock.mock.calls[0]
    const body = JSON.parse(init!.body as string)
    expect(body).not.toHaveProperty('apply_bow_offset')
  })
})
