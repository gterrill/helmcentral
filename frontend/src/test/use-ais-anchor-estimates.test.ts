import { act, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { useAisAnchorEstimates } from '@/hooks/use-ais-anchor-estimates'

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

const body = (estimates: unknown[]) => ({ ok: true, json: async () => ({ estimates }) }) as Response
const e = () => ({ mmsi: '111', lat: -20, lon: 149, swing_radius_m: 40, drawn_radius_m: 50, swing_arc_deg: 180, points: 30, overlaps_ours: true })

async function tick(ms: number) {
  await act(async () => { await vi.advanceTimersByTimeAsync(ms) })
}

describe('useAisAnchorEstimates', () => {
  it('serves what the endpoint sends', async () => {
    vi.useFakeTimers()
    const mockFetch = vi.fn().mockResolvedValue(body([e()]))
    vi.stubGlobal('fetch', mockFetch)
    const { result } = renderHook(() => useAisAnchorEstimates(1000))
    await tick(0)
    expect(String(mockFetch.mock.calls[0][0])).toBe('/api/anchor-watch/ais-anchors')
    expect(result.current).toEqual([e()])
  })

  it('drops the estimates and logs when a poll fails, rather than drawing stale ones', async () => {
    vi.useFakeTimers()
    const errorLog = vi.spyOn(console, 'error').mockImplementation(() => {})
    vi.stubGlobal('fetch', vi.fn().mockResolvedValueOnce(body([e()])).mockRejectedValue(new Error('down')))
    const { result } = renderHook(() => useAisAnchorEstimates(1000))
    await tick(0)
    expect(result.current).toHaveLength(1)
    await tick(1000)
    expect(result.current).toEqual([])
    expect(errorLog).toHaveBeenCalled()
  })

  it('keeps the same array when a poll brings the same estimates', async () => {
    vi.useFakeTimers()
    vi.stubGlobal('fetch', vi.fn().mockImplementation(async () => body([e()])))
    const { result } = renderHook(() => useAisAnchorEstimates(1000))
    await tick(0)
    const first = result.current
    await tick(1000)
    expect(result.current).toBe(first)
  })

  it('is empty when the endpoint sends no estimates', async () => {
    vi.useFakeTimers()
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(body([])))
    const { result } = renderHook(() => useAisAnchorEstimates(1000))
    await tick(0)
    expect(result.current).toEqual([])
  })

  it('forgets the estimates once the last reader leaves', async () => {
    vi.useFakeTimers()
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(body([e()])))
    const first = renderHook(() => useAisAnchorEstimates(1000))
    await tick(0)
    expect(first.result.current).toHaveLength(1)
    first.unmount()
    const second = renderHook(() => useAisAnchorEstimates(1000, false))
    expect(second.result.current).toEqual([])
  })
})
