import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { renderHook, act } from '@testing-library/react'

import { useOvernightProjection } from '@/hooks/use-overnight-projection'

const validPayload = {
  soc_path: 'electrical.batteries.0.capacity.stateOfCharge',
  sunset: '2026-09-07T07:56:45Z',
  sunrise: '2026-09-07T20:07:50Z',
  basis: 'history',
  night_rate_percent_per_hour: -2.4866,
  nights_used: 4,
  nights_considered: 7,
  reason: null,
  computed_at: '2026-09-06T22:40:25Z',
}

describe('useOvernightProjection', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('fetches on mount and parses a well-formed payload', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => validPayload,
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useOvernightProjection())

    await act(async () => {
      await Promise.resolve()
    })

    expect(fetchMock).toHaveBeenCalledWith('/api/electrical/overnight')
    expect(result.current.error).toBeNull()
    expect(result.current.projection).not.toBeNull()
    expect(result.current.projection?.basis).toBe('history')
    expect(result.current.projection?.nightsUsed).toBe(4)
  })

  it('refetches on the default 15-minute interval', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => validPayload,
    })
    vi.stubGlobal('fetch', fetchMock)

    renderHook(() => useOvernightProjection())

    await act(async () => {
      await Promise.resolve()
    })
    expect(fetchMock).toHaveBeenCalledTimes(1)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(14 * 60 * 1000)
    })
    expect(fetchMock).toHaveBeenCalledTimes(1)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1 * 60 * 1000)
    })
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('respects an explicit refresh interval override', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => validPayload,
    })
    vi.stubGlobal('fetch', fetchMock)

    renderHook(() => useOvernightProjection(60))

    await act(async () => {
      await Promise.resolve()
    })
    expect(fetchMock).toHaveBeenCalledTimes(1)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(60 * 1000)
    })
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('surfaces the body error string and a null projection on a non-OK response', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: false,
      status: 503,
      json: async () => ({ error: 'vessel position unavailable' }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useOvernightProjection())

    await act(async () => {
      await Promise.resolve()
    })

    expect(result.current.projection).toBeNull()
    expect(result.current.error).toBe('vessel position unavailable')
  })

  it('falls back to an HTTP status message when a non-OK response has no error body', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: false,
      status: 500,
      json: async () => {
        throw new Error('not json')
      },
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useOvernightProjection())

    await act(async () => {
      await Promise.resolve()
    })

    expect(result.current.projection).toBeNull()
    expect(result.current.error).toBe('HTTP 500')
  })

  it('reports an unparsable payload rather than guessing at its shape', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ soc_path: 'x' }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useOvernightProjection())

    await act(async () => {
      await Promise.resolve()
    })

    expect(result.current.projection).toBeNull()
    expect(result.current.error).toBe('unexpected overnight payload')
  })

  it('clears the interval on unmount', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => validPayload,
    })
    vi.stubGlobal('fetch', fetchMock)

    const { unmount } = renderHook(() => useOvernightProjection(60))

    await act(async () => {
      await Promise.resolve()
    })
    expect(fetchMock).toHaveBeenCalledTimes(1)

    unmount()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5 * 60 * 1000)
    })
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })
})
