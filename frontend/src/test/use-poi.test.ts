import { act, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { usePoi } from '@/hooks/use-poi'

function jsonResponse(status: number, body: unknown): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
  } as Response
}

const samplePayload = {
  center: { lat: -20.27, lon: 148.94 },
  radius_nm: 5,
  categories: ['anchorage'],
  provider: 'osm-overpass',
  cached: false,
  fetched_at: '2026-09-11T00:00:00Z',
  features: [
    {
      id: 'a1', category: 'anchorage', name: 'Cid Harbour', lat: -20.28, lon: 148.95,
      distance_m: 1200, bearing_deg: 45, detail: 'A sheltered bay.', source_url: 'https://osm.org/a1',
    },
  ],
  truncated: [],
  unsupported: [],
}

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('usePoi', () => {
  it('fetches on mount with the right query params and maps the response', async () => {
    const mockFetch = vi.fn().mockResolvedValue(jsonResponse(200, samplePayload))
    vi.stubGlobal('fetch', mockFetch)

    const { result } = renderHook(() => usePoi(5, ['anchorage', 'fuel'], 50))

    await act(async () => { await Promise.resolve(); await Promise.resolve() })

    const url = String(mockFetch.mock.calls[0][0])
    expect(url).toContain('/api/poi?')
    expect(url).toContain('radius_nm=5')
    expect(url).toContain('categories=anchorage%2Cfuel')
    expect(url).toContain('limit=50')

    expect(result.current.loading).toBe(false)
    expect(result.current.error).toBeNull()
    expect(result.current.center).toEqual({ lat: -20.27, lon: 148.94 })
    expect(result.current.provider).toBe('osm-overpass')
    expect(result.current.fetchedAt).toBe('2026-09-11T00:00:00Z')
    expect(result.current.features).toEqual([{
      id: 'a1', category: 'anchorage', name: 'Cid Harbour', lat: -20.28, lon: 148.95,
      distanceM: 1200, bearingDeg: 45, detail: 'A sheltered bay.', sourceUrl: 'https://osm.org/a1',
    }])
  })

  it('polls again after 60 seconds', async () => {
    vi.useFakeTimers()
    const mockFetch = vi.fn().mockResolvedValue(jsonResponse(200, samplePayload))
    vi.stubGlobal('fetch', mockFetch)

    renderHook(() => usePoi(5, ['anchorage'], 50))
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })
    expect(mockFetch).toHaveBeenCalledTimes(1)

    await act(async () => { await vi.advanceTimersByTimeAsync(60_000) })
    expect(mockFetch).toHaveBeenCalledTimes(2)
  })

  it('keeps the last good features and reports the error on a failed fetch', async () => {
    vi.useFakeTimers()
    const mockFetch = vi.fn()
      .mockResolvedValueOnce(jsonResponse(200, samplePayload))
      .mockResolvedValueOnce(jsonResponse(502, { error: 'POI provider unavailable' }))
    vi.stubGlobal('fetch', mockFetch)

    const { result } = renderHook(() => usePoi(5, ['anchorage'], 50))
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })
    expect(result.current.features).toHaveLength(1)
    const fetchedAtAfterFirst = result.current.fetchedAt

    await act(async () => { await vi.advanceTimersByTimeAsync(60_000) })

    // Never invents or clears data on a failed poll (no masking fallback).
    expect(result.current.features).toHaveLength(1)
    expect(result.current.fetchedAt).toBe(fetchedAtAfterFirst)
    expect(result.current.error).not.toBeNull()
    expect(result.current.retryAt).not.toBeNull()
  })

  it('backs off 502/503 starting at 60s and doubling up to 10 minutes', async () => {
    vi.useFakeTimers()
    const mockFetch = vi.fn().mockResolvedValue(jsonResponse(503, { error: 'unavailable' }))
    vi.stubGlobal('fetch', mockFetch)

    renderHook(() => usePoi(5, ['anchorage'], 50))
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })
    expect(mockFetch).toHaveBeenCalledTimes(1)

    // 60s -> second attempt
    await act(async () => { await vi.advanceTimersByTimeAsync(60_000) })
    expect(mockFetch).toHaveBeenCalledTimes(2)
    // Not yet due at 60s again (backoff has doubled to 120s).
    await act(async () => { await vi.advanceTimersByTimeAsync(60_000) })
    expect(mockFetch).toHaveBeenCalledTimes(2)
    await act(async () => { await vi.advanceTimersByTimeAsync(60_000) })
    expect(mockFetch).toHaveBeenCalledTimes(3)

    // Keep failing and advancing far enough that the doubling must have
    // capped at 10 minutes rather than growing unbounded.
    await act(async () => { await vi.advanceTimersByTimeAsync(10 * 60_000) })
    const callsAfterLongWait = mockFetch.mock.calls.length
    await act(async () => { await vi.advanceTimersByTimeAsync(10 * 60_000) })
    // Exactly one more attempt in the next 10-minute window once capped.
    expect(mockFetch.mock.calls.length).toBe(callsAfterLongWait + 1)
  })

  it('resets the backoff after a successful poll', async () => {
    vi.useFakeTimers()
    const mockFetch = vi.fn()
      .mockResolvedValueOnce(jsonResponse(502, { error: 'unavailable' }))
      .mockResolvedValueOnce(jsonResponse(502, { error: 'unavailable' }))
      .mockResolvedValue(jsonResponse(200, samplePayload))
    vi.stubGlobal('fetch', mockFetch)

    renderHook(() => usePoi(5, ['anchorage'], 50))
    await act(async () => { await vi.advanceTimersByTimeAsync(0) }) // fail #1 (60s scheduled)
    await act(async () => { await vi.advanceTimersByTimeAsync(60_000) }) // fail #2 (120s scheduled)
    await act(async () => { await vi.advanceTimersByTimeAsync(120_000) }) // success
    expect(mockFetch).toHaveBeenCalledTimes(3)

    // Backoff should be back to 60s, not still growing.
    await act(async () => { await vi.advanceTimersByTimeAsync(60_000) })
    expect(mockFetch).toHaveBeenCalledTimes(4)
  })

  it('aborts the in-flight fetch on unmount', async () => {
    let capturedSignal: AbortSignal | undefined
    const mockFetch = vi.fn((_url: string, init?: RequestInit) => {
      capturedSignal = init?.signal ?? undefined
      return new Promise<Response>(() => {}) // never resolves
    })
    vi.stubGlobal('fetch', mockFetch)

    const { unmount } = renderHook(() => usePoi(5, ['anchorage'], 50))
    await act(async () => { await Promise.resolve() })
    expect(capturedSignal?.aborted).toBe(false)

    unmount()
    expect(capturedSignal?.aborted).toBe(true)
  })

  it('never fabricates features before the first response arrives', () => {
    vi.stubGlobal('fetch', vi.fn(() => new Promise<Response>(() => {})))
    const { result } = renderHook(() => usePoi(5, ['anchorage'], 50))
    expect(result.current.features).toEqual([])
    expect(result.current.loading).toBe(true)
  })
})
