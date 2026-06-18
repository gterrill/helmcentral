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

  it('reflects an active route from the initial poll', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ active: true, route_id: 'r1', point_index: 2, reverse: false }),
    }))

    const { result } = renderHook(() => useRouteActivation())

    await act(async () => { await Promise.resolve() })

    expect(result.current.status).toEqual({ state: 'active', routeId: 'r1', pointIndex: 2, reverse: false })
  })

  it('reflects inactive state', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ active: false }),
    }))

    const { result } = renderHook(() => useRouteActivation())

    await act(async () => { await Promise.resolve() })

    expect(result.current.status).toEqual({ state: 'inactive' })
  })

  it('sets status to unknown — not inactive — when the poll fails', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 502 }))

    const { result } = renderHook(() => useRouteActivation())

    await act(async () => { await Promise.resolve() })

    expect(result.current.status).toEqual({ state: 'unknown' })
  })

  it('sets status to unknown when fetch itself throws', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('network down')))

    const { result } = renderHook(() => useRouteActivation())

    await act(async () => { await Promise.resolve() })

    expect(result.current.status).toEqual({ state: 'unknown' })
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

  it('activate POSTs to the activate endpoint and refetches status', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ active: false }) }) // initial poll
      .mockResolvedValueOnce({ ok: true, json: async () => ({ active: true, route_id: 'r1' }) }) // POST activate
      .mockResolvedValueOnce({ ok: true, json: async () => ({ active: true, route_id: 'r1', point_index: 0, reverse: false }) }) // refetch
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useRouteActivation())
    await act(async () => { await Promise.resolve() })
    expect(result.current.status).toEqual({ state: 'inactive' })

    let activateResult: boolean | undefined
    await act(async () => {
      activateResult = await result.current.activate('r1')
    })

    expect(activateResult).toBe(true)
    expect(fetchMock).toHaveBeenCalledWith('/api/routes/r1/activate', expect.objectContaining({ method: 'POST' }))
    expect(result.current.status).toEqual({ state: 'active', routeId: 'r1', pointIndex: 0, reverse: false })
    expect(result.current.activating).toBe(false)
  })

  it('activate surfaces the raw backend error message on failure', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ active: false }) })
      .mockResolvedValueOnce({ ok: false, status: 502, json: async () => ({ error: 'signalk returned status 400: bad geometry' }) })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useRouteActivation())
    await act(async () => { await Promise.resolve() })

    let activateResult: boolean | undefined
    await act(async () => {
      activateResult = await result.current.activate('r1')
    })

    expect(activateResult).toBe(false)
    expect(result.current.activateError).toBe('signalk returned status 400: bad geometry')
  })

  it('deactivate POSTs to the deactivate endpoint and refetches status', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ active: true, route_id: 'r1' }) })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ active: false }) }) // POST deactivate
      .mockResolvedValueOnce({ ok: true, json: async () => ({ active: false }) }) // refetch
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useRouteActivation())
    await act(async () => { await Promise.resolve() })

    let deactivateResult: boolean | undefined
    await act(async () => {
      deactivateResult = await result.current.deactivate()
    })

    expect(deactivateResult).toBe(true)
    expect(fetchMock).toHaveBeenCalledWith('/api/routes/deactivate', expect.objectContaining({ method: 'POST' }))
    expect(result.current.status).toEqual({ state: 'inactive' })
  })

  it('clears a previous activateError at the start of a new attempt', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ active: false }) })
      .mockResolvedValueOnce({ ok: false, status: 502, json: async () => ({ error: 'first failure' }) })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ active: true, route_id: 'r1' }) })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ active: true, route_id: 'r1', point_index: 0, reverse: false }) })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useRouteActivation())
    await act(async () => { await Promise.resolve() })

    await act(async () => { await result.current.activate('r1') })
    expect(result.current.activateError).toBe('first failure')

    await act(async () => { await result.current.activate('r1') })
    expect(result.current.activateError).toBeNull()
  })
})
