import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useRouteActivation } from '@/hooks/use-route-activation'

describe('useRouteActivation', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('reflects an active route, with its name and waypoints, from the initial poll', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        active: true,
        point_index: 2,
        reverse: false,
        route: { name: 'Whitsunday Run', waypoints: [{ lat: -19.2, lon: 146.8, name: 'Start' }, { lat: -20.3, lon: 148.1 }] },
      }),
    }))

    const { result } = renderHook(() => useRouteActivation())

    await act(async () => { await Promise.resolve() })

    expect(result.current.status).toEqual({
      state: 'active',
      route: { name: 'Whitsunday Run', waypoints: [{ lat: -19.2, lon: 146.8, name: 'Start' }, { lat: -20.3, lon: 148.1 }] },
      pointIndex: 2,
      reverse: false,
      destination: null,
    })
  })

  it('carries the destination through alongside an active route', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        active: true,
        point_index: 0,
        reverse: true,
        route: { name: 'R', waypoints: [{ lat: 1, lon: 2 }] },
        destination: { lat: -18.6675, lon: 146.4846, name: 'Hook Island' },
      }),
    }))

    const { result } = renderHook(() => useRouteActivation())

    await act(async () => { await Promise.resolve() })

    expect(result.current.status).toEqual({
      state: 'active',
      route: { name: 'R', waypoints: [{ lat: 1, lon: 2 }] },
      pointIndex: 0,
      reverse: true,
      destination: { lat: -18.6675, lon: 146.4846, name: 'Hook Island' },
    })
  })

  it('reflects inactive state', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ active: false }),
    }))

    const { result } = renderHook(() => useRouteActivation())

    await act(async () => { await Promise.resolve() })

    expect(result.current.status).toEqual({ state: 'inactive', destination: null })
  })

  // ADR 0125: a chartplotter go-to with no route behind it still carries a
  // destination the tile and the clock can build a trip ETA from.
  it('carries the destination through when inactive but the backend reports one', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ active: false, destination: { lat: -18.6675, lon: 146.4846, name: 'Hook Island' } }),
    }))

    const { result } = renderHook(() => useRouteActivation())

    await act(async () => { await Promise.resolve() })

    expect(result.current.status).toEqual({
      state: 'inactive',
      destination: { lat: -18.6675, lon: 146.4846, name: 'Hook Island' },
    })
  })

  it('sets status to unknown, not inactive, when the poll fails', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 502 }))

    const { result } = renderHook(() => useRouteActivation())

    await act(async () => { await Promise.resolve() })

    expect(result.current.status).toEqual({ state: 'unknown', reason: 'error' })
  })

  it('sets status to unknown when fetch itself throws', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('network down')))

    const { result } = renderHook(() => useRouteActivation())

    await act(async () => { await Promise.resolve() })

    expect(result.current.status).toEqual({ state: 'unknown', reason: 'error' })
  })

  it('is unknown/loading until the first poll is answered', () => {
    vi.stubGlobal('fetch', vi.fn().mockReturnValue(new Promise(() => {})))
    const { result } = renderHook(() => useRouteActivation())
    expect(result.current.status).toEqual({ state: 'unknown', reason: 'loading' })
  })

  it('keeps the destination and the reason when the route could not be read', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        active: true,
        route_error: 'Route details unavailable: gone',
        destination: { lat: 1, lon: 2, name: 'Hook Island' },
      }),
    }))
    const { result } = renderHook(() => useRouteActivation())
    await act(async () => { await Promise.resolve() })
    expect(result.current.status).toEqual({
      state: 'inactive',
      destination: { lat: 1, lon: 2, name: 'Hook Island' },
      routeError: 'Route details unavailable: gone',
    })
  })

  it('treats active with no route object as an error, not inactive', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({ active: true }) }))
    const { result } = renderHook(() => useRouteActivation())
    await act(async () => { await Promise.resolve() })
    expect(result.current.status).toEqual({ state: 'unknown', reason: 'error' })
  })

  it('polls again after the interval elapses', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ active: false }) })
    vi.stubGlobal('fetch', fetchMock)

    renderHook(() => useRouteActivation(15))

    await act(async () => { await Promise.resolve() })
    expect(fetchMock).toHaveBeenCalledTimes(1)

    await act(async () => { await vi.advanceTimersByTimeAsync(15000) })
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('offers no way to activate or deactivate a route', () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({ active: false }) }))
    const { result } = renderHook(() => useRouteActivation())
    expect(Object.keys(result.current)).toEqual(['status'])
  })
})
