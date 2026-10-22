import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { ANCHOR_TRAIL_MAX_POINTS, useAnchorTrail } from '@/hooks/use-anchor-trail'

function makeResponse(body: unknown): Response {
  return { ok: true, json: async () => body } as Response
}

const pt = (i: number) => ({ lat: 1, lon: 2 + i / 1e6, timestamp: new Date(Date.UTC(2026, 0, 1, 0, 0, i)).toISOString() })

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('useAnchorTrail', () => {
  it('polls the anchor trail endpoint and advances since', async () => {
    vi.useFakeTimers()
    const mockFetch = vi.fn()
      .mockResolvedValueOnce(makeResponse({ points: [pt(0), pt(5)], trail_started_at: 'A' }))
      .mockResolvedValue(makeResponse({ points: [pt(10)], trail_started_at: 'A' }))
    vi.stubGlobal('fetch', mockFetch)
    const { result } = renderHook(() => useAnchorTrail(5000))
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })
    expect(String(mockFetch.mock.calls[0][0])).toBe('/api/anchor-watch/trails/self')
    expect(result.current.getAnchorTrail()).toHaveLength(2)
    await act(async () => { await vi.advanceTimersByTimeAsync(5000) })
    expect(String(mockFetch.mock.calls[1][0])).toContain('since=')
    expect(result.current.getAnchorTrail()).toHaveLength(3)
  })

  it('drops its points and refetches in full when the trail restarts', async () => {
    vi.useFakeTimers()
    const mockFetch = vi.fn()
      .mockResolvedValueOnce(makeResponse({ points: [pt(0), pt(5)], trail_started_at: 'A' }))
      // incremental poll: the server restarted the trail, so this answer is partial
      .mockResolvedValueOnce(makeResponse({ points: [pt(20)], trail_started_at: 'B' }))
      // the full refetch
      .mockResolvedValueOnce(makeResponse({ points: [pt(15), pt(20)], trail_started_at: 'B' }))
    vi.stubGlobal('fetch', mockFetch)
    const { result } = renderHook(() => useAnchorTrail(5000))
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })
    await act(async () => { await vi.advanceTimersByTimeAsync(5000) })
    expect(mockFetch).toHaveBeenCalledTimes(3)
    expect(String(mockFetch.mock.calls[2][0])).toBe('/api/anchor-watch/trails/self')
    const trail = result.current.getAnchorTrail()
    expect(trail).toHaveLength(2)
    expect(trail[0].timestampMs).toBe(new Date(pt(15).timestamp).getTime())
  })

  it('empties the trail when the watch is raised', async () => {
    vi.useFakeTimers()
    const mockFetch = vi.fn()
      .mockResolvedValueOnce(makeResponse({ points: [pt(0)], trail_started_at: 'A' }))
      .mockResolvedValue(makeResponse({ points: [], trail_started_at: null }))
    vi.stubGlobal('fetch', mockFetch)
    const { result } = renderHook(() => useAnchorTrail(5000))
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })
    expect(result.current.getAnchorTrail()).toHaveLength(1)
    await act(async () => { await vi.advanceTimersByTimeAsync(5000) })
    expect(result.current.getAnchorTrail()).toHaveLength(0)
  })

  it('keeps the newest points up to the backend bound', async () => {
    const big = Array.from({ length: ANCHOR_TRAIL_MAX_POINTS + 10 }, (_, i) => pt(i))
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(makeResponse({ points: big, trail_started_at: 'A' })))
    const { result } = renderHook(() => useAnchorTrail(5000))
    await waitFor(() => expect(result.current.getAnchorTrail().length).toBeGreaterThan(0))
        expect(result.current.getAnchorTrail()).toHaveLength(ANCHOR_TRAIL_MAX_POINTS)
  })

  it('does not poll while disabled', async () => {
    const mockFetch = vi.fn().mockResolvedValue(makeResponse({ points: [], trail_started_at: null }))
    vi.stubGlobal('fetch', mockFetch)
    renderHook(() => useAnchorTrail(5000, false))
    await new Promise((r) => setTimeout(r, 10))
    expect(mockFetch).not.toHaveBeenCalled()
  })
})
