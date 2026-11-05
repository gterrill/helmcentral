import { act, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { useAisTrails } from '@/hooks/use-ais-anchor-tracks'

function makeResponse(body: unknown): Response {
  return { ok: true, json: async () => body } as Response
}

const NOW = Date.UTC(2026, 10, 5, 12, 0, 0)
const pt = (mmsi: string, secondsAgo: number, newSegment = false) => ({
  lat: -20, lon: 149 + secondsAgo / 1e6, t_ms: NOW - secondsAgo * 1000, ...(newSegment ? { new_segment: true } : {}),
  mmsi,
})
const track = (mmsi: string, points: Array<{ lat: number; lon: number; t_ms: number; new_segment?: boolean }>) => ({
  mmsi,
  points: points.map(({ lat, lon, t_ms, new_segment }) => ({ lat, lon, t_ms, ...(new_segment ? { new_segment } : {}) })),
})
const answer = (cursor: number, tracks: ReturnType<typeof track>[], started: string | null = 'A') =>
  makeResponse({ trail_started_at: started, cursor, tracks })

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

async function tick(ms: number) {
  await act(async () => { await vi.advanceTimersByTimeAsync(ms) })
}

describe('useAisTrails', () => {
  it('keys the points by MMSI with real times and the stored segment flag, and asks from the cursor', async () => {
    vi.useFakeTimers({ now: NOW })
    const mockFetch = vi.fn()
      .mockResolvedValueOnce(answer(7, [track('111', [pt('111', 60, true), pt('111', 30)])]))
      .mockResolvedValue(answer(7, []))
    vi.stubGlobal('fetch', mockFetch)

    const { result } = renderHook(() => useAisTrails(5000))
    await tick(0)
    expect(String(mockFetch.mock.calls[0][0])).toBe('/api/anchor-watch/trails/ais')
    const points = result.current.get('111')!
    expect(points).toHaveLength(2)
    expect(points[0]).toEqual({ lat: -20, lon: 149 + 60 / 1e6, timestampMs: NOW - 60_000, newSegment: true })
    expect(points[1].newSegment).toBe(false)

    await tick(5000)
    expect(String(mockFetch.mock.calls[1][0])).toBe('/api/anchor-watch/trails/ais?since=7')
  })

  it('republishes only when something changed, and gives only the changed targets a new array', async () => {
    vi.useFakeTimers({ now: NOW })
    const mockFetch = vi.fn()
      .mockResolvedValueOnce(answer(2, [track('111', [pt('111', 60, true)]), track('222', [pt('222', 60, true)])]))
      .mockResolvedValueOnce(answer(2, []))
      .mockResolvedValueOnce(answer(3, [track('111', [pt('111', 10)])]))
    vi.stubGlobal('fetch', mockFetch)

    let renders = 0
    const { result } = renderHook(() => { renders += 1; return useAisTrails(5000) })
    await tick(0)
    const first = result.current
    const afterFirst = renders
    const second111 = first.get('111')
    const second222 = first.get('222')

    await tick(5000) // nothing new
    expect(result.current).toBe(first)
    expect(renders).toBe(afterFirst)

    await tick(5000) // 111 reports again
    expect(result.current).not.toBe(first)
    expect(result.current.get('111')).toHaveLength(2)
    expect(result.current.get('111')).not.toBe(second111)
    expect(result.current.get('222')).toBe(second222)
  })

  it('drops its points and refetches in full when the trail restarts', async () => {
    vi.useFakeTimers({ now: NOW })
    const mockFetch = vi.fn()
      .mockResolvedValueOnce(answer(2, [track('111', [pt('111', 60, true), pt('111', 30)])], 'A'))
      // incremental poll: the server restarted the tracks, so this answer is partial
      .mockResolvedValueOnce(answer(1, [track('222', [pt('222', 5, true)])], 'B'))
      // the full refetch
      .mockResolvedValueOnce(answer(1, [track('222', [pt('222', 5, true)])], 'B'))
    vi.stubGlobal('fetch', mockFetch)

    const { result } = renderHook(() => useAisTrails(5000))
    await tick(0)
    await tick(5000)
    expect(mockFetch).toHaveBeenCalledTimes(3)
    expect(String(mockFetch.mock.calls[2][0])).toBe('/api/anchor-watch/trails/ais')
    expect([...result.current.keys()]).toEqual(['222'])
  })

  it('empties when the watch is raised', async () => {
    vi.useFakeTimers({ now: NOW })
    const mockFetch = vi.fn()
      .mockResolvedValueOnce(answer(1, [track('111', [pt('111', 60, true)])]))
      .mockResolvedValue(makeResponse({ trail_started_at: null, cursor: 0, tracks: [] }))
    vi.stubGlobal('fetch', mockFetch)

    const { result } = renderHook(() => useAisTrails(5000))
    await tick(0)
    expect(result.current.size).toBe(1)
    await tick(5000)
    expect(result.current.size).toBe(0)
  })

  it('trims points older than 24 hours from the head when it merges, and caps nothing else', async () => {
    vi.useFakeTimers({ now: NOW })
    const day = 24 * 60 * 60
    const mockFetch = vi.fn()
      .mockResolvedValueOnce(answer(2, [track('111', [pt('111', day - 2, true), pt('111', day - 1)])]))
      .mockResolvedValueOnce(answer(3, [track('111', [pt('111', 0)])]))
    vi.stubGlobal('fetch', mockFetch)

    const { result } = renderHook(() => useAisTrails(5000))
    await tick(0)
    expect(result.current.get('111')).toHaveLength(2)
    await tick(5000)
    // Five seconds on, the two old points are past the 24 hour mark.
    expect(result.current.get('111')).toHaveLength(1)
  })

  it('forgets a target that stopped reporting once all its points are past 24 hours', async () => {
    vi.useFakeTimers({ now: NOW })
    const day = 24 * 60 * 60
    const mockFetch = vi.fn()
      .mockResolvedValueOnce(answer(2, [track('111', [pt('111', 60, true)]), track('222', [pt('222', day - 2, true)])]))
      .mockResolvedValueOnce(answer(3, [track('111', [pt('111', 0)])]))
    vi.stubGlobal('fetch', mockFetch)

    const { result } = renderHook(() => useAisTrails(5000))
    await tick(0)
    expect(result.current.has('222')).toBe(true)
    await tick(5000)
    // 222 sent nothing new, but its only point is now older than 24 hours.
    expect(result.current.has('222')).toBe(false)
    expect(result.current.get('111')).toHaveLength(2)
  })

  it('does not poll while disabled', async () => {
    const mockFetch = vi.fn().mockResolvedValue(answer(0, []))
    vi.stubGlobal('fetch', mockFetch)
    renderHook(() => useAisTrails(5000, false))
    await new Promise((r) => setTimeout(r, 10))
    expect(mockFetch).not.toHaveBeenCalled()
  })

  it('two readers make one poll', async () => {
    vi.useFakeTimers({ now: NOW })
    const mockFetch = vi.fn().mockResolvedValue(answer(0, []))
    vi.stubGlobal('fetch', mockFetch)
    renderHook(() => useAisTrails(5000))
    renderHook(() => useAisTrails(5000))
    await tick(0)
    expect(mockFetch).toHaveBeenCalledTimes(1)
  })
})
