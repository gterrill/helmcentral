import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { useAisTrails, useSelfTrail } from '@/hooks/use-server-trails'

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

    // `reactStrictMode: true` (Testing Library's own option), not a
    // `<StrictMode>` wrapper composed via the `wrapper` option: under React
    // 19, StrictMode only double-invokes effects when it is the outermost
    // element handed to the root's render() call. A wrapper composes it one
    // function-component layer down (wrapper -> StrictMode -> TestComponent),
    // and that layer is enough for React 19 to skip the mount/cleanup/remount
    // replay entirely — the effect then only ever runs once, silently, with
    // no error. `reactStrictMode` avoids the extra layer by having Testing
    // Library apply StrictMode itself, last, around the whole tree.
    const { result } = renderHook(() => useSelfTrail(5000), { reactStrictMode: true })

    await waitFor(() => expect(mockFetch).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(result.current.length).toBeGreaterThan(0))

    expect(result.current.length).toBe(N)
  })

  it('does not start a poll while one is in flight', async () => {
    vi.useFakeTimers()

    const first = deferred<Response>()
    const mockFetch = vi.fn()
    mockFetch.mockReturnValueOnce(first.promise)
    mockFetch.mockResolvedValue(makeResponse({}))
    vi.stubGlobal('fetch', mockFetch)

    renderHook(() => useSelfTrail(5000))

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

    const { unmount } = renderHook(() => useSelfTrail(5000))
    
    unmount()

    const N = 3
    const points = Array.from({ length: N }, (_, i) => ({
      lat: i,
      lon: i,
      timestamp: new Date(Date.UTC(2024, 0, 1, 0, 0, i)).toISOString(),
    }))
    first.resolve(makeResponse({ self: points }))

    await flushMicrotasks()

    const reader = renderHook(() => useSelfTrail(5000, false))
    expect(reader.result.current).toEqual([])
  })
})

// Item B: nothing on screen reads the trails on most pages
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

    renderHook(() => useSelfTrail(5000, false))
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

    const { result, rerender } = renderHook(({ enabled }) => useSelfTrail(5000, enabled), {
      initialProps: { enabled: false },
    })
    await act(async () => { await vi.advanceTimersByTimeAsync(20000) })
    expect(mockFetch).not.toHaveBeenCalled()

    rerender({ enabled: true })
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })
    expect(mockFetch).toHaveBeenCalledTimes(1)
    expect(String(mockFetch.mock.calls[0][0])).toBe('/api/tracks')
    expect(result.current).toHaveLength(1)

    // The next poll must ask only for what's new since the first response's
    // latest timestamp, not refetch from scratch.
    await act(async () => { await vi.advanceTimersByTimeAsync(5000) })
    expect(mockFetch).toHaveBeenCalledTimes(2)
    const secondUrl = String(mockFetch.mock.calls[1][0])
    expect(secondUrl).toBe('/api/tracks?since=2024-01-01T00%3A00%3A00.000Z')
    expect(result.current).toHaveLength(2)
  })

  it('stops polling once enabled flips back false, keeping the trail already collected', async () => {
    vi.useFakeTimers()
    const mockFetch = vi.fn().mockResolvedValue(makeResponse({
      self: [{ lat: 1, lon: 2, timestamp: '2024-01-01T00:00:00.000Z' }],
    }))
    vi.stubGlobal('fetch', mockFetch)

    const { result, rerender } = renderHook(({ enabled }) => useSelfTrail(5000, enabled), {
      initialProps: { enabled: true },
    })
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })
    expect(mockFetch).toHaveBeenCalledTimes(1)
    expect(result.current).toHaveLength(1)

    rerender({ enabled: false })
    await act(async () => { await vi.advanceTimersByTimeAsync(20000) })

    expect(mockFetch).toHaveBeenCalledTimes(1)
    // Pausing must not wipe what's already been collected.
    expect(result.current).toHaveLength(1)
  })
})

// One shared poller (ADR 0215 phase 5 review): an anchor watch tile and a
// Nearby map on one page share a poll, and a map that comes back after a page
// switch or a wall rotation asks only for what is new.
describe('useServerTrails shared poller', () => {
  it('two readers make one poll', async () => {
    vi.useFakeTimers()
    const mockFetch = vi.fn().mockResolvedValue(makeResponse({ self: [] }))
    vi.stubGlobal('fetch', mockFetch)

    renderHook(() => useSelfTrail(5000))
    renderHook(() => useSelfTrail(5000))
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })
    expect(mockFetch).toHaveBeenCalledTimes(1)

    await act(async () => { await vi.advanceTimersByTimeAsync(5000) })
    expect(mockFetch).toHaveBeenCalledTimes(2)
  })

  it('a remount keeps the trail and asks only for what is new', async () => {
    vi.useFakeTimers()
    const mockFetch = vi.fn().mockResolvedValue(makeResponse({
      self: [{ lat: 1, lon: 2, timestamp: '2024-01-01T00:00:00.000Z' }],
    }))
    vi.stubGlobal('fetch', mockFetch)

    const first = renderHook(() => useSelfTrail(5000))
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })
    first.unmount()

    const second = renderHook(() => useSelfTrail(5000))
    expect(second.result.current).toHaveLength(1)
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })

    expect(String(mockFetch.mock.calls[1][0])).toBe('/api/tracks?since=2024-01-01T00%3A00%3A00.000Z')
  })
})

// The reader side (ADR 0215): each slice changes identity only when its own
// trail does, so a map re-renders when points arrive and never on a quiet
// poll, and a reader of one trail is not woken by the other.
describe('trail slices', () => {
  const at = (s: number) => ({ lat: s, lon: s, timestamp: new Date(Date.UTC(2024, 0, 1, 0, 0, s)).toISOString() })

  it('re-renders a reader when a poll brings new points, and not when it brings none', async () => {
    vi.useFakeTimers()
    const mockFetch = vi.fn()
      .mockResolvedValueOnce(makeResponse({ self: [at(0)] }))
      .mockResolvedValueOnce(makeResponse({ self: [] }))
      .mockResolvedValueOnce(makeResponse({ self: [at(5)] }))
    vi.stubGlobal('fetch', mockFetch)

    let renders = 0
    const { result } = renderHook(() => {
      renders += 1
      return useSelfTrail(5000)
    })
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })
    expect(result.current).toHaveLength(1)
    const afterFirst = renders
    const firstSnapshot = result.current

    await act(async () => { await vi.advanceTimersByTimeAsync(5000) })
    expect(mockFetch).toHaveBeenCalledTimes(2)
    expect(renders).toBe(afterFirst)
    expect(result.current).toBe(firstSnapshot)

    await act(async () => { await vi.advanceTimersByTimeAsync(5000) })
    expect(renders).toBeGreaterThan(afterFirst)
    expect(result.current).toHaveLength(2)
  })

  it('keeps the AIS trails identity across a poll that repeats them', async () => {
    vi.useFakeTimers()
    const ais = { ais: { v1: [at(0), at(5)] } }
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(makeResponse(ais)))

    const { result } = renderHook(() => useAisTrails(5000))
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })
    const first = result.current
    expect(first.get('v1')).toHaveLength(2)

    await act(async () => { await vi.advanceTimersByTimeAsync(5000) })
    expect(result.current).toBe(first)
  })

  it('treats an AIS trail with a changed middle point as a change', async () => {
    vi.useFakeTimers()
    const moved = { lat: 99, lon: 99, timestamp: at(3).timestamp }
    const mockFetch = vi.fn()
      .mockResolvedValueOnce(makeResponse({ ais: { v1: [at(0), at(3), at(5)] } }))
      .mockResolvedValueOnce(makeResponse({ ais: { v1: [at(0), moved, at(5)] } }))
    vi.stubGlobal('fetch', mockFetch)

    const { result } = renderHook(() => useAisTrails(5000))
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })
    const first = result.current
    await act(async () => { await vi.advanceTimersByTimeAsync(5000) })
    expect(result.current).not.toBe(first)
  })

  it('does not wake a self reader for an AIS-only change, nor an AIS reader for a self-only change', async () => {
    vi.useFakeTimers()
    const mockFetch = vi.fn()
      .mockResolvedValueOnce(makeResponse({ self: [at(0)], ais: { v1: [at(0)] } }))
      .mockResolvedValueOnce(makeResponse({ ais: { v1: [at(0), at(5)] } }))
      .mockResolvedValueOnce(makeResponse({ self: [at(10)], ais: { v1: [at(0), at(5)] } }))
    vi.stubGlobal('fetch', mockFetch)

    let selfRenders = 0
    let aisRenders = 0
    renderHook(() => { selfRenders += 1; return useSelfTrail(5000) })
    renderHook(() => { aisRenders += 1; return useAisTrails(5000) })
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })
    const selfAfterFirst = selfRenders
    const aisAfterFirst = aisRenders

    // AIS-only change.
    await act(async () => { await vi.advanceTimersByTimeAsync(5000) })
    expect(aisRenders).toBeGreaterThan(aisAfterFirst)
    expect(selfRenders).toBe(selfAfterFirst)

    // Self-only change.
    const aisAfterSecond = aisRenders
    await act(async () => { await vi.advanceTimersByTimeAsync(5000) })
    expect(selfRenders).toBeGreaterThan(selfAfterFirst)
    expect(aisRenders).toBe(aisAfterSecond)
  })
})
