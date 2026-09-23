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
            is_daylight: true,
          },
          {
            label: '5:09PM',
            condition: 'Sunset',
            temperature_f: -1,
            kind: 'sunset',
            is_daylight: false,
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
    expect(result.current.hourlyToday[0].isDaylight).toBe(true)
    expect(result.current.hourlyToday[1].kind).toBe('sunset')
    expect(result.current.hourlyToday[1].isDaylight).toBe(false)
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

  it('maps the next_hour nowcast envelope into typed points', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        days: [{ day_key: '2026-06-14', date: 'Jun 14', day_name: 'Sunday', condition: 'Clear' }],
        next_hour: {
          start: '2026-06-14T14:00:00Z',
          step_minutes: 15,
          source: 'nowcast',
          points: [
            { time: '2026-06-14T14:00:00Z', chance_pct: 20, mm_per_h: 0 },
            { time: '2026-06-14T14:15:00Z', chance_pct: -1, mm_per_h: 1.6 },
          ],
        },
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useWeatherForecast())

    await act(async () => {
      await Promise.resolve()
    })

    expect(result.current.nextHour).not.toBeNull()
    expect(result.current.nextHour!.stepMinutes).toBe(15)
    expect(result.current.nextHour!.source).toBe('nowcast')
    expect(result.current.nextHour!.points).toHaveLength(2)
    expect(result.current.nextHour!.points[0].time.toISOString()).toBe('2026-06-14T14:00:00.000Z')
    expect(result.current.nextHour!.points[0].chancePct).toBe(20)
    // The backend's -1 sentinel ("not supplied") must map to null, the same
    // absence convention every other precipitation-chance field in this
    // hook already uses - never a fabricated 0%.
    expect(result.current.nextHour!.points[1].chancePct).toBeNull()
    expect(result.current.nextHour!.points[1].mmPerH).toBe(1.6)
  })

  // ADR 0126 addendum: Open-Meteo's minutely_15 is interpolated from the
  // hourly model outside its two native-resolution regions - next_hour.source
  // carries which case applies so the tile can caption it honestly.
  it('maps next_hour.source "hourly" through unchanged', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        days: [{ day_key: '2026-06-14', date: 'Jun 14', day_name: 'Sunday', condition: 'Clear' }],
        next_hour: {
          start: '2026-06-14T14:00:00Z',
          step_minutes: 15,
          source: 'hourly',
          points: [{ time: '2026-06-14T14:00:00Z', chance_pct: 20, mm_per_h: 0 }],
        },
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useWeatherForecast())

    await act(async () => {
      await Promise.resolve()
    })

    expect(result.current.nextHour!.source).toBe('hourly')
  })

  // The backend hard-errors on a missing/unrecognized next_hour_source
  // whenever next_hour has points (mapWasmFetchForecastOutput,
  // backend/wasm_weather_provider.go), so one reaching the frontend at all
  // means the contract broke upstream - the hook must not quietly repair it
  // by guessing "hourly"; it drops the whole nowcast and logs why, falling
  // back to the well-tested hourly/daily cascade instead.
  it('drops a next_hour with a missing/unrecognized source rather than defaulting it, and logs why', async () => {
    const consoleErrorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        days: [{ day_key: '2026-06-14', date: 'Jun 14', day_name: 'Sunday', condition: 'Clear' }],
        next_hour: {
          start: '2026-06-14T14:00:00Z',
          step_minutes: 15,
          points: [{ time: '2026-06-14T14:00:00Z', chance_pct: 20, mm_per_h: 0 }],
        },
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useWeatherForecast())

    await act(async () => {
      await Promise.resolve()
    })

    expect(result.current.nextHour).toBeNull()
    expect(consoleErrorSpy).toHaveBeenCalled()
    consoleErrorSpy.mockRestore()
  })

  // The backend never emits an unusable step (buildWeatherNextHourResponse
  // omits next_hour entirely rather than sending step_minutes 0/missing) -
  // a non-positive or missing step reaching the frontend is malformed, not
  // "unknown cadence, assume 0", since a 0 step silently zero-widths every
  // bar in the strip (lib/nowcast.ts's buildNowcastBars).
  it('drops a next_hour with a non-positive/missing step_minutes rather than coercing it to 0, and logs why', async () => {
    const consoleErrorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        days: [{ day_key: '2026-06-14', date: 'Jun 14', day_name: 'Sunday', condition: 'Clear' }],
        next_hour: {
          start: '2026-06-14T14:00:00Z',
          step_minutes: 0,
          source: 'nowcast',
          points: [{ time: '2026-06-14T14:00:00Z', chance_pct: 20, mm_per_h: 0 }],
        },
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useWeatherForecast())

    await act(async () => {
      await Promise.resolve()
    })

    expect(result.current.nextHour).toBeNull()
    expect(consoleErrorSpy).toHaveBeenCalled()
    consoleErrorSpy.mockRestore()
  })

  // mm_per_h is a plain number on the wire (not sentinel-coded the way
  // chance_pct is) - a point missing it entirely is malformed, not "assume
  // 0 mm/h" (which would misrepresent a data gap as a confirmed-dry point).
  it('drops a next_hour whose point is missing mm_per_h rather than coercing it to 0, and logs why', async () => {
    const consoleErrorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        days: [{ day_key: '2026-06-14', date: 'Jun 14', day_name: 'Sunday', condition: 'Clear' }],
        next_hour: {
          start: '2026-06-14T14:00:00Z',
          step_minutes: 15,
          source: 'nowcast',
          points: [{ time: '2026-06-14T14:00:00Z', chance_pct: 20 }],
        },
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useWeatherForecast())

    await act(async () => {
      await Promise.resolve()
    })

    expect(result.current.nextHour).toBeNull()
    expect(consoleErrorSpy).toHaveBeenCalled()
    consoleErrorSpy.mockRestore()
  })

  // An unparseable point time is a plugin/host bug reaching the wire, not
  // something the frontend should skip and carry on with a gap - the whole
  // nowcast is dropped and the break logged.
  it('drops a next_hour whose point has an unparseable time rather than skipping just that point, and logs why', async () => {
    const consoleErrorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        days: [{ day_key: '2026-06-14', date: 'Jun 14', day_name: 'Sunday', condition: 'Clear' }],
        next_hour: {
          start: '2026-06-14T14:00:00Z',
          step_minutes: 15,
          source: 'nowcast',
          points: [
            { time: '2026-06-14T14:00:00Z', chance_pct: 20, mm_per_h: 0 },
            { time: 'not-a-real-timestamp', chance_pct: 40, mm_per_h: 1 },
          ],
        },
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useWeatherForecast())

    await act(async () => {
      await Promise.resolve()
    })

    expect(result.current.nextHour).toBeNull()
    expect(consoleErrorSpy).toHaveBeenCalled()
    consoleErrorSpy.mockRestore()
  })

  it('maps a missing next_hour field to null, not an empty nowcast', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        days: [{ day_key: '2026-06-14', date: 'Jun 14', day_name: 'Sunday', condition: 'Clear' }],
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useWeatherForecast())

    await act(async () => {
      await Promise.resolve()
    })

    expect(result.current.nextHour).toBeNull()
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

  // Humidity/visibility follow the exact same -1-sentinel convention as
  // precipitation (see backend/weather_providers.go's sentinelHumidityPct/
  // sentinelVisibilityNm) - absence maps to null, and a genuine 0 must
  // survive as 0, not collapse into "unavailable".
  it('maps absent humidity/visibility to null and keeps a real 0 as 0, at both the day and hourly level', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        provider: 'open-meteo',
        days: [
          {
            day_key: '2026-08-09',
            date: 'Aug 9',
            day_name: 'Sunday',
            condition: 'Foggy',
            humidity_pct: -1,
            visibility_nm: 0,
            hourly_cloud: [
              { label: '6AM', hour_of_day: 6, humidity_pct: -1, visibility_nm: -1 },
              { label: '7AM', hour_of_day: 7, humidity_pct: 0, visibility_nm: 0 },
              { label: '8AM', hour_of_day: 8, humidity_pct: 71, visibility_nm: 6.2 },
            ],
          },
          {
            day_key: '2026-08-10',
            date: 'Aug 10',
            day_name: 'Monday',
            condition: 'Clear',
            // humidity_pct/visibility_nm omitted entirely on this day
          },
        ],
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useWeatherForecast())

    await act(async () => {
      await Promise.resolve()
    })

    expect(result.current.forecast[0].humidityPct).toBeNull()
    expect(result.current.forecast[0].visibilityNm).toBe(0)
    expect(result.current.forecast[1].humidityPct).toBeNull()
    expect(result.current.forecast[1].visibilityNm).toBeNull()

    const hourly = result.current.forecast[0].hourlyCloud
    expect(hourly[0].humidityPct).toBeNull()
    expect(hourly[0].visibilityNm).toBeNull()
    expect(hourly[1].humidityPct).toBe(0)
    expect(hourly[1].visibilityNm).toBe(0)
    expect(hourly[2].humidityPct).toBe(71)
    expect(hourly[2].visibilityNm).toBe(6.2)
  })
})
