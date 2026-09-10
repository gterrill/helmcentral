import { render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { CurrentConditionsTile } from '@/components/current-conditions-tile'
import type { WeatherToday } from '@/hooks/use-weather-today'
import type { WeatherForecastDay } from '@/hooks/use-weather-forecast'

function weather(overrides: Partial<WeatherToday> = {}): WeatherToday {
  return {
    datetime: '2026-06-14T14:00:00Z',
    temperature_f: 77,
    condition: 'Clear',
    wind_speed_kts: 12,
    wind_gust_kts: 16,
    wind_direction: 'ENE',
    precipitation_pct: 10,
    provider: 'weatherkit',
    cached: false,
    updated_at: '2026-06-14T14:00:00Z',
    ttl_seconds: 900,
    ...overrides,
  }
}

function day(overrides: Partial<WeatherForecastDay> = {}): WeatherForecastDay {
  return {
    dayKey: '2026-06-14',
    date: 'Jun 14',
    dayName: 'Sunday',
    condition: 'Clear',
    high: 82,
    low: 68,
    windSpeed: 12,
    windGust: 18,
    windDirection: 'ENE',
    windSummary: null,
    precipitationSummary: null,
    precipitation: null,
    humidityPct: null,
    visibilityNm: null,
    sunriseTime: null,
    sunsetTime: null,
    moonPhase: null,
    hourlyWind: [],
    hourlyPrecip: [],
    hourlyUV: [],
    hourlyCloud: [],
    ...overrides,
  }
}

const NO_GUSTS = { '10m': null, '30m': null, '1h': null, '24h': null }

beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date('2026-06-14T14:00:00Z'))
})

afterEach(() => {
  vi.useRealTimers()
})

describe('CurrentConditionsTile', () => {
  test('renders depth, wind and temperature readouts', () => {
    render(
      <CurrentConditionsTile
        depth={12.3}
        depthLastUpdateAgeS={5}
        windSpeedApparentKts={14}
        maxGustKts={{ ...NO_GUSTS, '1h': 19 }}
        weather={weather()}
        forecast={[day()]}
        distanceUnits="metric"
      />,
    )

    expect(screen.getByText('12.3')).toBeInTheDocument()
    expect(screen.getByText('14')).toBeInTheDocument()
    expect(screen.getByText('25')).toBeInTheDocument() // 77F -> 25C
  })

  test('converts depth and shows feet units when imperial', () => {
    render(
      <CurrentConditionsTile
        depth={10}
        depthLastUpdateAgeS={5}
        windSpeedApparentKts={null}
        maxGustKts={NO_GUSTS}
        weather={weather()}
        forecast={[day()]}
        distanceUnits="imperial"
      />,
    )

    // 10m -> 32.8ft
    expect(screen.getByText('32.8')).toBeInTheDocument()
  })

  test('shows structural dashes when depth and wind are null', () => {
    render(
      <CurrentConditionsTile
        depth={null}
        depthLastUpdateAgeS={null}
        windSpeedApparentKts={null}
        maxGustKts={NO_GUSTS}
        weather={weather({ temperature_f: -1 })}
        forecast={[]}
        distanceUnits="metric"
      />,
    )

    expect(screen.getAllByText('—').length).toBeGreaterThanOrEqual(3)
  })

  test('marks the tile stale when the depth feed has gone quiet', () => {
    render(
      <CurrentConditionsTile
        depth={12}
        depthLastUpdateAgeS={999}
        windSpeedApparentKts={10}
        maxGustKts={NO_GUSTS}
        weather={weather()}
        forecast={[day()]}
        distanceUnits="metric"
      />,
    )

    expect(screen.getByTestId('tile-stale-badge')).toBeInTheDocument()
  })

  test('passes the forecast gust max and the observed 1h gust as bullet-gauge markers', () => {
    render(
      <CurrentConditionsTile
        depth={5}
        depthLastUpdateAgeS={0}
        windSpeedApparentKts={14}
        maxGustKts={{ ...NO_GUSTS, '1h': 19 }}
        weather={weather()}
        forecast={[day({ hourlyWind: [{ label: '2PM', hourOfDay: 14, windSpeed: 12, windGust: 18, windDirection: 'ENE', windDirectionDeg: 70 }] })]}
        distanceUnits="metric"
      />,
    )

    expect(screen.getByLabelText(/fcst gust: 18/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/obs: 19/i)).toBeInTheDocument()
  })

  test('shows a rain line when the forecast crosses the threshold', () => {
    render(
      <CurrentConditionsTile
        depth={5}
        depthLastUpdateAgeS={0}
        windSpeedApparentKts={10}
        maxGustKts={NO_GUSTS}
        weather={weather()}
        forecast={[
          day({
            hourlyPrecip: [
              { label: '3PM', hourOfDay: 15, precipChancePct: 60, precipIntensityMm: 1 },
            ],
          }),
        ]}
        distanceUnits="metric"
      />,
    )

    expect(screen.getByText(/rain likely from 3PM/i)).toBeInTheDocument()
    expect(screen.getByText(/60%/)).toBeInTheDocument()
  })

  test('shows "no rain expected" when the forecast has data but stays under threshold', () => {
    render(
      <CurrentConditionsTile
        depth={5}
        depthLastUpdateAgeS={0}
        windSpeedApparentKts={10}
        maxGustKts={NO_GUSTS}
        weather={weather()}
        forecast={[
          day({ hourlyPrecip: [{ label: '3PM', hourOfDay: 15, precipChancePct: 5, precipIntensityMm: 0 }] }),
        ]}
        distanceUnits="metric"
      />,
    )

    expect(screen.getByText(/no rain expected/i)).toBeInTheDocument()
  })

  test('shows a dash for the rain line when the forecast has no precip data at all, never a fabricated no-rain', () => {
    render(
      <CurrentConditionsTile
        depth={5}
        depthLastUpdateAgeS={0}
        windSpeedApparentKts={10}
        maxGustKts={NO_GUSTS}
        weather={weather()}
        forecast={[day({ hourlyPrecip: [] })]}
        distanceUnits="metric"
      />,
    )

    expect(screen.queryByText(/no rain expected/i)).not.toBeInTheDocument()
    expect(screen.queryByText(/rain likely/i)).not.toBeInTheDocument()
  })
})
