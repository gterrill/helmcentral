import { afterEach, describe, expect, it, vi } from 'vitest'
import { act, renderHook, waitFor } from '@testing-library/react'
import { invalidateQueries } from '@/lib/api-query'
import { useBarometerTrend } from '@/hooks/use-barometer-trend'

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('useBarometerTrend', () => {
  it('maps the pascal readings and the 3 hour change', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        points: [
          { time: '2026-11-12T10:00:00Z', pressure_pa: 101300 },
          { time: '2026-11-12T10:05:00Z', pressure_pa: 101250.5 },
        ],
        current_pa: 101250.5,
        change_pa: -49.5,
        change_span_s: 2400,
      }),
    })
    vi.stubGlobal('fetch', fetchMock)
    const { result } = renderHook(() => useBarometerTrend(0))
    await waitFor(() => expect(result.current.points).toHaveLength(2))
    expect(result.current.points[1]).toEqual({ time: '2026-11-12T10:05:00Z', pressurePa: 101250.5 })
    expect(result.current.currentPa).toBe(101250.5)
    expect(result.current.changePa).toBe(-49.5)
    expect(result.current.changeSpanS).toBe(2400)
    expect(String(fetchMock.mock.calls[0][0])).toContain('/api/barometer-trend')
  })

  it('keeps a boat with no barometer empty, never a stand-in value', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ points: [], current_pa: null, change_pa: null, change_span_s: null }),
    }))
    const { result } = renderHook(() => useBarometerTrend(0))
    await new Promise((r) => setTimeout(r, 20))
    expect(result.current).toEqual({ points: [], currentPa: null, changePa: null, changeSpanS: null, failed: false })
  })

  it('drops a change that arrives without its span, so the two are never half-present', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        points: [{ time: '2026-11-12T10:00:00Z', pressure_pa: 101300 }],
        current_pa: 101300,
        change_pa: -49.5,
        change_span_s: null,
      }),
    }))
    const { result } = renderHook(() => useBarometerTrend(0))
    await waitFor(() => expect(result.current.currentPa).toBe(101300))
    expect(result.current.changePa).toBeNull()
    expect(result.current.changeSpanS).toBeNull()
  })

  it('reports a failed request as failed, not as a boat with no barometer', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 500, json: async () => ({}) }))
    const { result } = renderHook(() => useBarometerTrend(0))
    await waitFor(() => expect(result.current.failed).toBe(true))
    expect(result.current).toEqual({ points: [], currentPa: null, changePa: null, changeSpanS: null, failed: true })
  })

  it('reports an answer without a points list as failed, not as a boat with no barometer', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) }))
    const { result } = renderHook(() => useBarometerTrend(0))
    await waitFor(() => expect(result.current.failed).toBe(true))
  })

  it('drops the last good reading when a later request fails, rather than present it as current', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce({
      ok: true,
      json: async () => ({
        points: [{ time: '2026-11-12T10:00:00Z', pressure_pa: 101300 }],
        current_pa: 101300,
        change_pa: null,
        change_span_s: null,
      }),
    })
    vi.stubGlobal('fetch', fetchMock)
    const { result } = renderHook(() => useBarometerTrend(0))
    await waitFor(() => expect(result.current.currentPa).toBe(101300))

    fetchMock.mockResolvedValue({ ok: false, status: 500, json: async () => ({}) })
    await act(async () => { await invalidateQueries(['/api/barometer-trend']) })
    await waitFor(() => expect(result.current.currentPa).toBeNull())
    expect(result.current.points).toEqual([])
    expect(result.current.failed).toBe(true)
  })
})
