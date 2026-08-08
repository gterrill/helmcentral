import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { renderHook, act } from '@testing-library/react'

import { useWeatherForecast } from '@/hooks/use-weather-forecast'

describe('useWeatherForecast', () => {
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
      json: async () => [],
    })
    vi.stubGlobal('fetch', fetchMock)

    renderHook(() => useWeatherForecast())

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

  it('respects an explicit refresh interval override', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => [],
    })
    vi.stubGlobal('fetch', fetchMock)

    renderHook(() => useWeatherForecast(120))

    await act(async () => {
      await Promise.resolve()
    })
    expect(fetchMock).toHaveBeenCalledTimes(1)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(119 * 1000)
    })
    expect(fetchMock).toHaveBeenCalledTimes(1)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000)
    })
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('parses forecast metadata envelope fields', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        provider: 'open-meteo',
        days: [
          {
            day_key: '2026-06-14',
            date: 'Jun 14',
            day_name: 'Sunday',
            condition: 'Clear',
            high_temp_f: 76,
            low_temp_f: 62,
            wind_speed_kts: 10,
            wind_gust_kts: 14,
            wind_direction: 'NE',
            precipitation_pct: 5,
          },
        ],
        hourly_today: [
          {
            label: 'Now',
            condition: 'Mostly Sunny',
            temperature_f: 72,
            kind: 'forecast',
          },
          {
            label: '5:09PM',
            condition: 'Sunset',
            temperature_f: -1,
            kind: 'sunset',
          },
        ],
        summary: 'Mostly Sunny conditions will continue through today.',
        cached: true,
        updated_at: '2026-06-14T12:30:00Z',
        ttl_seconds: 3600,
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useWeatherForecast())

    await act(async () => {
      await Promise.resolve()
    })

    expect(result.current.forecast).toHaveLength(1)
    expect(result.current.forecast[0].dayKey).toBe('2026-06-14')
    expect(result.current.hourlyToday).toHaveLength(2)
    expect(result.current.hourlyToday[1].kind).toBe('sunset')
    expect(result.current.summary).toBe('Mostly Sunny conditions will continue through today.')
    expect(result.current.provider).toBe('open-meteo')
    expect(result.current.isCached).toBe(true)
    expect(result.current.updatedAt).toBe('2026-06-14T12:30:00Z')
    expect(result.current.ttlSeconds).toBe(3600)
  })

  it('falls back to an ISO date-derived dayKey when day_key is missing', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => [
        {
          date: 'Jun 14',
          day_name: 'Sunday',
          condition: 'Clear',
          high_temp_f: 76,
          low_temp_f: 62,
        },
      ],
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useWeatherForecast())

    await act(async () => {
      await Promise.resolve()
    })

    expect(result.current.forecast).toHaveLength(1)
    expect(result.current.forecast[0].dayKey).toMatch(/^\d{4}-\d{2}-\d{2}$/)
    expect(result.current.provider).toBeNull()
  })
  // The backend sends -1 when the provider reported no precipitation data at
  // all, which is different from a genuine 0% chance. Collapsing the two is
  // what showed a confident "0% precip" during actual drizzle, so the hook
  // must surface absence as null and let the UI say "unavailable".
  it('maps an absent precipitation chance to null and keeps a real 0 as 0', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        provider: 'weatherkit',
        days: [
          {
            day_key: '2026-08-09',
            date: 'Aug 9',
            day_name: 'Sunday',
            condition: 'Drizzle',
            precipitation_pct: -1,
            hourly_precip: [
              { label: '6AM', hour_of_day: 6, precipitation_chance_pct: -1, precipitation_intensity_mm: 0 },
              { label: '7AM', hour_of_day: 7, precipitation_chance_pct: 0, precipitation_intensity_mm: 0 },
              { label: '8AM', hour_of_day: 8, precipitation_chance_pct: 65, precipitation_intensity_mm: 1.2 },
            ],
          },
          {
            day_key: '2026-08-10',
            date: 'Aug 10',
            day_name: 'Monday',
            condition: 'Clear',
            precipitation_pct: 0,
          },
        ],
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useWeatherForecast())

    await act(async () => {
      await Promise.resolve()
    })

    expect(result.current.forecast[0].precipitation).toBeNull()
    expect(result.current.forecast[1].precipitation).toBe(0)

    const hourly = result.current.forecast[0].hourlyPrecip
    expect(hourly[0].precipChancePct).toBeNull()
    expect(hourly[1].precipChancePct).toBe(0)
    expect(hourly[2].precipChancePct).toBe(65)
  })

  // A field the backend omitted entirely is also "no data", not 0%.
  it('maps a missing precipitation_pct field to null rather than 0', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        provider: 'weatherkit',
        days: [{ day_key: '2026-08-09', date: 'Aug 9', day_name: 'Sunday', condition: 'Drizzle' }],
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useWeatherForecast())

    await act(async () => {
      await Promise.resolve()
    })

    expect(result.current.forecast[0].precipitation).toBeNull()
  })
})
