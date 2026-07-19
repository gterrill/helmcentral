import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { renderHook, act } from '@testing-library/react'

import { useWaveForecast } from '@/hooks/use-wave-forecast'

describe('useWaveForecast', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('uses a 60-minute default refresh interval', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ days: [], sea_temperature_f: null }),
    })
    vi.stubGlobal('fetch', fetchMock)

    renderHook(() => useWaveForecast())

    await act(async () => {
      await Promise.resolve()
    })
    expect(fetchMock).toHaveBeenCalledTimes(1)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(59 * 60 * 1000)
    })
    expect(fetchMock).toHaveBeenCalledTimes(1)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(60 * 1000)
    })
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('maps day and hourly wave fields correctly', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        provider: 'open-meteo-marine',
        days: [
          {
            day_key: '2026-06-14',
            date: 'Jun 14',
            day_name: 'Sunday',
            wave_summary: 'Significant wave height 1.0 to 1.2 m from the E.',
            hourly_wave: [
              {
                label: '12AM',
                hour_of_day: 0,
                wave_height_m: 1.1,
                wave_period_s: 6.2,
                wave_direction_deg: 90,
                wind_wave_height_m: 0.4,
                swell_wave_height_m: 0.8,
              },
            ],
          },
        ],
        sea_temperature_f: 74.5,
        cached: true,
        updated_at: '2026-06-14T12:30:00Z',
        ttl_seconds: 3600,
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useWaveForecast())

    await act(async () => {
      await Promise.resolve()
    })

    expect(result.current.days).toHaveLength(1)
    const day = result.current.days[0]
    expect(day.dayKey).toBe('2026-06-14')
    expect(day.date).toBe('Jun 14')
    expect(day.dayName).toBe('Sunday')
    expect(day.waveSummary).toBe('Significant wave height 1.0 to 1.2 m from the E.')
    expect(day.hourlyWave).toHaveLength(1)
    expect(day.hourlyWave[0]).toEqual({
      label: '12AM',
      hourOfDay: 0,
      waveHeightM: 1.1,
      wavePeriodS: 6.2,
      waveDirectionDeg: 90,
      windWaveHeightM: 0.4,
      swellWaveHeightM: 0.8,
    })
    expect(result.current.seaTemperatureF).toBe(74.5)
    expect(result.current.provider).toBe('open-meteo-marine')
    expect(result.current.isCached).toBe(true)
    expect(result.current.updatedAt).toBe('2026-06-14T12:30:00Z')
    expect(result.current.ttlSeconds).toBe(3600)
    expect(result.current.error).toBeNull()
  })

  it('passes through a null sea_temperature_f as a legitimate no-data value, not an error', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        days: [],
        sea_temperature_f: null,
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useWaveForecast())

    await act(async () => {
      await Promise.resolve()
    })

    expect(result.current.seaTemperatureF).toBeNull()
    expect(result.current.error).toBeNull()
  })

  it('sets an error and keeps stale data on a non-OK response, without clearing days', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          days: [
            {
              day_key: '2026-06-14',
              date: 'Jun 14',
              day_name: 'Sunday',
              wave_summary: 'Calm seas.',
              hourly_wave: [
                {
                  label: '12AM',
                  hour_of_day: 0,
                  wave_height_m: 0.5,
                  wave_period_s: 5,
                  wave_direction_deg: 45,
                  wind_wave_height_m: 0.2,
                  swell_wave_height_m: 0.3,
                },
              ],
            },
          ],
          sea_temperature_f: 70,
        }),
      })
      .mockResolvedValueOnce({
        ok: false,
        status: 502,
        json: async () => ({ error: 'wave provider unavailable' }),
      })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useWaveForecast(60))

    await act(async () => {
      await Promise.resolve()
    })
    expect(result.current.days).toHaveLength(1)
    expect(result.current.error).toBeNull()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(60 * 1000)
    })

    expect(result.current.error).toBeTruthy()
    // Stale data from the previous successful fetch stays visible instead of
    // flickering to empty on a transient refetch failure.
    expect(result.current.days).toHaveLength(1)
  })
})
