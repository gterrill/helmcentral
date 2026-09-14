import { createElement, StrictMode } from 'react'
import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { useServerTrails } from '@/hooks/use-server-trails'

function makeResponse(body: unknown): Response {
  return { ok: true, json: async () => body } as Response
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((res) => { resolve = res })
  return { promise, resolve }
}

/** Flushes the microtask queue without depending on fake vs. real timers. */
async function flushMicrotasks() {
  await act(async () => { await new Promise((r) => setTimeout(r, 0)) })
}

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('useServerTrails', () => {
  it('StrictMode double mount appends the initial buffer once', async () => {
    const N = 5
    const points = Array.from({ length: N }, (_, i) => ({
      lat: 10 + i,
      lon: 20 + i,
      timestamp: new Date(Date.UTC(2024, 0, 1, 0, 0, i)).toISOString(),
    }))
    const mockFetch = vi.fn().mockResolvedValue(makeResponse({ self: points, ais: {} }))
    vi.stubGlobal('fetch', mockFetch)

    const { result } = renderHook(() => useServerTrails(5000), {
      wrapper: ({ children }) => createElement(StrictMode, null, children),
    })

    await waitFor(() => expect(mockFetch).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(result.current.getSelfTrail().length).toBeGreaterThan(0))

    expect(result.current.getSelfTrail().length).toBe(N)
  })

  it('does not start a poll while one is in flight', async () => {
    vi.useFakeTimers()

    const first = deferred<Response>()
    const mockFetch = vi.fn()
    mockFetch.mockReturnValueOnce(first.promise)
    mockFetch.mockResolvedValue(makeResponse({}))
    vi.stubGlobal('fetch', mockFetch)

    renderHook(() => useServerTrails(5000))

    // Two interval ticks pass while the first poll is still pending.
    await act(async () => { await vi.advanceTimersByTimeAsync(10000) })
    expect(mockFetch).toHaveBeenCalledTimes(1)

    // Resolve the in-flight poll with a point so `since` advances.
    first.resolve(makeResponse({
      self: [{ lat: 1, lon: 2, timestamp: '2024-01-01T00:00:00.000Z' }],
    }))
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })

    // Now a new interval tick should be free to start a second poll.
    await act(async () => { await vi.advanceTimersByTimeAsync(5000) })
    expect(mockFetch).toHaveBeenCalledTimes(2)

    const secondUrl = String(mockFetch.mock.calls[1][0])
    expect(secondUrl).toContain('since=')
  })

  it('a response resolving after unmount is discarded', async () => {
    const first = deferred<Response>()
    const mockFetch = vi.fn().mockReturnValue(first.promise)
    vi.stubGlobal('fetch', mockFetch)

    const { result, unmount } = renderHook(() => useServerTrails(5000))
    const getSelfTrail = result.current.getSelfTrail

    unmount()

    const N = 3
    const points = Array.from({ length: N }, (_, i) => ({
      lat: i,
      lon: i,
      timestamp: new Date(Date.UTC(2024, 0, 1, 0, 0, i)).toISOString(),
    }))
    first.resolve(makeResponse({ self: points }))

    await flushMicrotasks()

    expect(getSelfTrail()).toEqual([])
  })
})

// Item B: nothing on screen reads getSelfTrail/getAisTrails on most pages
// (a kiosk page with no map, for instance), so polling /api/tracks every 5s
// app-wide is wasted. `enabled` gates the poll; defaults to true so the two
// suites above (which don't pass it) keep polling unconditionally.
describe('useServerTrails enabled gating', () => {
  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('makes no request at all while disabled', async () => {
    vi.useFakeTimers()
    const mockFetch = vi.fn().mockResolvedValue(makeResponse({ self: [] }))
    vi.stubGlobal('fetch', mockFetch)

    renderHook(() => useServerTrails(5000, false))
    await act(async () => { await vi.advanceTimersByTimeAsync(20000) })

    expect(mockFetch).not.toHaveBeenCalled()
  })

  it('starts polling once enabled flips true, and the incremental `since` still advances correctly', async () => {
    vi.useFakeTimers()
    const mockFetch = vi.fn()
      .mockResolvedValueOnce(makeResponse({
        self: [{ lat: 1, lon: 2, timestamp: '2024-01-01T00:00:00.000Z' }],
      }))
      .mockResolvedValue(makeResponse({
        self: [{ lat: 3, lon: 4, timestamp: '2024-01-01T00:00:05.000Z' }],
      }))
    vi.stubGlobal('fetch', mockFetch)

    const { result, rerender } = renderHook(({ enabled }) => useServerTrails(5000, enabled), {
      initialProps: { enabled: false },
    })
    await act(async () => { await vi.advanceTimersByTimeAsync(20000) })
    expect(mockFetch).not.toHaveBeenCalled()

    rerender({ enabled: true })
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })
    expect(mockFetch).toHaveBeenCalledTimes(1)
    expect(String(mockFetch.mock.calls[0][0])).toBe('/api/tracks')
    expect(result.current.getSelfTrail()).toHaveLength(1)

    // The next poll must ask only for what's new since the first response's
    // latest timestamp, not refetch from scratch.
    await act(async () => { await vi.advanceTimersByTimeAsync(5000) })
    expect(mockFetch).toHaveBeenCalledTimes(2)
    const secondUrl = String(mockFetch.mock.calls[1][0])
    expect(secondUrl).toBe('/api/tracks?since=2024-01-01T00%3A00%3A00.000Z')
    expect(result.current.getSelfTrail()).toHaveLength(2)
  })

  it('stops polling once enabled flips back false, keeping the trail already collected', async () => {
    vi.useFakeTimers()
    const mockFetch = vi.fn().mockResolvedValue(makeResponse({
      self: [{ lat: 1, lon: 2, timestamp: '2024-01-01T00:00:00.000Z' }],
    }))
    vi.stubGlobal('fetch', mockFetch)

    const { result, rerender } = renderHook(({ enabled }) => useServerTrails(5000, enabled), {
      initialProps: { enabled: true },
    })
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })
    expect(mockFetch).toHaveBeenCalledTimes(1)
    expect(result.current.getSelfTrail()).toHaveLength(1)

    rerender({ enabled: false })
    await act(async () => { await vi.advanceTimersByTimeAsync(20000) })

    expect(mockFetch).toHaveBeenCalledTimes(1)
    // Pausing must not wipe what's already been collected.
    expect(result.current.getSelfTrail()).toHaveLength(1)
  })
})
