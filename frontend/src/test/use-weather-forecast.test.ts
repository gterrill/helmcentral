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
})
