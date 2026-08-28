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
      await result.current.setAnchorHere(-21.1, 149.2, { planningDepthM: 5, planningTideHeightFt: 2 })
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

// The planning depth / planning tide pair (ADR 0063) — a contemporaneous
// pair, carried by POST since a drop must never round-trip to a tide
// provider before a watch exists.
describe('useAnchorWatch planning-depth capture', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ active: true, lat: -21.1, lon: 149.2 }),
    }))
  })

  it('setAnchorHere posts the depth and tide pair when both are known', async () => {
    const fetchMock = vi.mocked(fetch)
    const { result } = renderHook(() => useAnchorWatch(-21.1, 149.2, null, 3600))
    await act(async () => { await Promise.resolve() })
    fetchMock.mockClear()

    await act(async () => {
      await result.current.setAnchorHere(-21.1, 149.2, { planningDepthM: 6.4, planningTideHeightFt: 1.8 })
    })

    const [, init] = fetchMock.mock.calls[0]
    const body = JSON.parse(init!.body as string)
    expect(body.planning_depth_m).toBe(6.4)
    expect(body.planning_tide_height_ft).toBe(1.8)
  })

  it('setAnchorHere posts -1 sentinels when there is no reading, rather than omitting the fields', async () => {
    const fetchMock = vi.mocked(fetch)
    const { result } = renderHook(() => useAnchorWatch(-21.1, 149.2, null, 3600))
    await act(async () => { await Promise.resolve() })
    fetchMock.mockClear()

    await act(async () => {
      await result.current.setAnchorHere(-21.1, 149.2, { planningDepthM: null, planningTideHeightFt: null })
    })

    const [, init] = fetchMock.mock.calls[0]
    const body = JSON.parse(init!.body as string)
    expect(body.planning_depth_m).toBe(-1)
    expect(body.planning_tide_height_ft).toBe(-1)
  })

  it('updatePosition omits the depth/tide pair entirely, letting the backend carry it forward', async () => {
    const fetchMock = vi.mocked(fetch)
    const { result } = renderHook(() => useAnchorWatch(-21.1, 149.2, null, 3600))
    await act(async () => { await Promise.resolve() })
    fetchMock.mockClear()

    await act(async () => {
      await result.current.updatePosition(-21.1, 149.2)
    })

    const [, init] = fetchMock.mock.calls[0]
    const body = JSON.parse(init!.body as string)
    expect(body).not.toHaveProperty('planning_depth_m')
    expect(body).not.toHaveProperty('planning_tide_height_ft')
  })
})

// updatePlanningDepth PATCHes the planning-depth pair — mirrors
// updateRodeAndConditions exactly (no optimistic update, replaces state with
// the server echo).
describe('useAnchorWatch updatePlanningDepth', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ active: true, lat: -21.1, lon: 149.2, planning_depth_m: 8, planning_tide_height_ft: 2.1 }),
    }))
  })

  it('PATCHes planning_depth_m and planning_tide_height_ft together', async () => {
    const fetchMock = vi.mocked(fetch)
    const { result } = renderHook(() => useAnchorWatch(-21.1, 149.2, null, 3600))
    await act(async () => { await Promise.resolve() })
    fetchMock.mockClear()

    await act(async () => {
      await result.current.updatePlanningDepth(8, 2.1)
    })

    expect(fetchMock).toHaveBeenCalledWith('/api/anchor-watch', expect.objectContaining({ method: 'PATCH' }))
    const [, init] = fetchMock.mock.calls[0]
    const body = JSON.parse(init!.body as string)
    expect(body).toEqual({ planning_depth_m: 8, planning_tide_height_ft: 2.1 })
  })

  it('replaces state with the server echo rather than updating optimistically', async () => {
    const { result } = renderHook(() => useAnchorWatch(-21.1, 149.2, null, 3600))
    await act(async () => { await Promise.resolve() })

    await act(async () => {
      await result.current.updatePlanningDepth(8, 2.1)
    })

    expect(result.current.planningDepthM).toBe(8)
    expect(result.current.planningTideHeightFt).toBe(2.1)
  })
})
