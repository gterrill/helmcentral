import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'

describe('useGshhgCoastline', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.resetModules()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('fetches the coastline GeoJSON on mount', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ type: 'FeatureCollection', features: [] }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { useGshhgCoastline } = await import('@/hooks/use-gshhg-coastline')
    const { result } = renderHook(() => useGshhgCoastline())

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(fetchMock).toHaveBeenCalledWith('/api/gshhg-coastline')
    expect(result.current.data).toEqual({ type: 'FeatureCollection', features: [] })
    expect(result.current.error).toBeNull()
  })

  it('sets an error message when the fetch fails', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: false, status: 500 })
    vi.stubGlobal('fetch', fetchMock)

    const { useGshhgCoastline } = await import('@/hooks/use-gshhg-coastline')
    const { result } = renderHook(() => useGshhgCoastline())

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.error).toContain('500')
    expect(result.current.data).toBeNull()
  })

  it('does not refetch on a second mount once the data is cached', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ type: 'FeatureCollection', features: [] }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { useGshhgCoastline } = await import('@/hooks/use-gshhg-coastline')

    const first = renderHook(() => useGshhgCoastline())
    await waitFor(() => expect(first.result.current.loading).toBe(false))

    const second = renderHook(() => useGshhgCoastline())
    await waitFor(() => expect(second.result.current.loading).toBe(false))

    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(second.result.current.data).toEqual({ type: 'FeatureCollection', features: [] })
  })
})
