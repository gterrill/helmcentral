import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { renderHook, act } from '@testing-library/react'

import { useUpperAir } from '@/hooks/use-upper-air'

describe('useUpperAir', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('maps the outlook for each day', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        provider: 'open-meteo-upper',
        days: [
          {
            day_key: '2026-09-12', date: 'Sep 12', day_name: 'Saturday',
            outlook: {
              present: true, height_500_m: 5835, thickness_m: 5645, peak_wind_500_kts: 33.9,
              height_percentile: 0.07, tendency_24h_m: -23.1, trough_support: true,
            },
          },
        ],
      }),
    }))

    const { result } = renderHook(() => useUpperAir())
    await act(async () => { await Promise.resolve() })

    expect(result.current.provider).toBe('open-meteo-upper')
    const outlook = result.current.days[0].outlook
    expect(outlook.present).toBe(true)
    expect(outlook.height500M).toBe(5835)
    expect(outlook.troughSupport).toBe(true)
    expect(outlook.tendency24hM).toBeCloseTo(-23.1, 2)
  })

  // A boat with no upper-air plugin is a normal setup, not a broken one, so
  // this must not surface as an error the page has to apologise for.
  it('treats no provider as empty rather than as a failure', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ provider: '', days: [] }),
    }))

    const { result } = renderHook(() => useUpperAir())
    await act(async () => { await Promise.resolve() })

    expect(result.current.error).toBeNull()
    expect(result.current.days).toEqual([])
    expect(result.current.provider).toBeNull()
  })

  it('maps a day the provider had no data for as absent, never as zero height', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ days: [{ day_key: '2026-09-12', outlook: { present: false } }] }),
    }))

    const { result } = renderHook(() => useUpperAir())
    await act(async () => { await Promise.resolve() })

    expect(result.current.days[0].outlook.present).toBe(false)
    expect(result.current.days[0].outlook.troughSupport).toBe(false)
  })

  it('refreshes on the model cadence rather than hourly', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ days: [] }) })
    vi.stubGlobal('fetch', fetchMock)

    renderHook(() => useUpperAir())
    await act(async () => { await Promise.resolve() })
    expect(fetchMock).toHaveBeenCalledTimes(1)

    // The global models behind this run four times a day.
    await act(async () => { await vi.advanceTimersByTimeAsync(6 * 60 * 60 * 1000) })
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })
})
