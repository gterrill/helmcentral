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

describe('useUpperAir trace', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  // The day cards read a scalar per day. The chart reads the shape of the
  // window, which is what Surviving the Storm's method actually works on, so
  // the sub-daily trace has to survive the mapping intact.
  it('maps the sub-daily trace and the window band', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        provider: 'open-meteo-upper',
        days: [],
        series: [
          { time: '2026-09-03T00:00:00Z', day_key: '2026-09-03', height_500_m: 5899, thickness_m: 5709, wind_500_kts: 23.4, temperature_500_c: -8.2 },
          { time: '2026-09-03T06:00:00Z', day_key: '2026-09-03', height_500_m: 5891, thickness_m: 5701, wind_500_kts: 25.1, temperature_500_c: -8.6 },
        ],
        window: { present: true, low_m: 5835, high_m: 5907, low_quintile_m: 5851 },
      }),
    }))

    const { result } = renderHook(() => useUpperAir())
    await act(async () => { await Promise.resolve() })

    expect(result.current.series).toHaveLength(2)
    expect(result.current.series[0].height500M).toBe(5899)
    expect(result.current.series[0].dayKey).toBe('2026-09-03')
    expect(result.current.series[1].wind500Kts).toBeCloseTo(25.1, 2)
    expect(result.current.windowBand.present).toBe(true)
    expect(result.current.windowBand.lowQuintileM).toBe(5851)
  })

  // A provider with no pressure levels, or no plugin at all, sends no series
  // and no window. That has to arrive as "nothing to draw" rather than as a
  // band pinned to sea level.
  it('reports an absent window when the payload carries none', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ provider: '', days: [] }),
    }))

    const { result } = renderHook(() => useUpperAir())
    await act(async () => { await Promise.resolve() })

    expect(result.current.series).toEqual([])
    expect(result.current.windowBand.present).toBe(false)
  })
})
