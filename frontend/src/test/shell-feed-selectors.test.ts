/**
 * The two things the shell reads of the forecasts itself (ADR 0215 phase 6):
 * sunrise and sunset for the Auto theme, and whether a wind bulletin is in
 * force for the sidebar's Forecast dot. Both read the entry's data whenever
 * it has some, even with a failed poll beside it, as the shell did before:
 * the banner reads the same data through useForecastWarnings, and the dot and
 * the banner must agree.
 */
import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { useForecastWarnings, useHasActiveWindBulletin } from '@/hooks/use-forecast-warnings'
import { useForecastSunTimes } from '@/hooks/use-weather-forecast'
import { invalidateQueries } from '@/lib/api-query'
import { jsonResponse, stubApi } from './helpers/stub-api'
import * as polls from './fixtures/live-poll-payloads'

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('the shell\'s forecast selectors after a failed poll', () => {
  it('keeps sunrise and sunset from the last good forecast', async () => {
    let fail = false
    stubApi({ 'GET /api/weather-forecast': () => (fail ? jsonResponse({ error: 'provider down' }, 503) : polls.WEATHER_FORECAST) })
    const { result } = renderHook(() => useForecastSunTimes(600))
    await waitFor(() => expect(result.current.sunriseTime).not.toBeNull())
    const good = result.current

    fail = true
    await act(async () => { await invalidateQueries(['/api/weather-forecast']) })

    expect(result.current).toEqual(good)
  })

  it('keeps the wind bulletin, as the banner does', async () => {
    let fail = false
    const withWind = {
      ...polls.FORECAST_WARNINGS,
      bulletins: [{ id: 'w1', title: 'Strong Wind Warning', issued_at: '2026-10-27T06:00:00Z', details_url: '', category: 'wind', sections: [{ day: 'Monday', warning_type: 'Strong Wind Warning' }] }],
    }
    stubApi({ 'GET /api/forecast-warnings': () => (fail ? jsonResponse({ error: 'provider down' }, 503) : withWind) })
    const { result } = renderHook(() => ({
      dot: useHasActiveWindBulletin(600),
      banner: useForecastWarnings(600).activeWarning,
    }))
    await waitFor(() => expect(result.current.dot).toBe(true))

    fail = true
    await act(async () => { await invalidateQueries(['/api/forecast-warnings']) })

    expect(result.current.banner).not.toBeNull()
    expect(result.current.dot).toBe(true)
  })
})
