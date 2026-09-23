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

  // Bottom-half nowcast (ADR 0126): with no `nextHour` prop at all, the tile
  // falls straight to the hourly/daily fallback cascade (lib/nowcast.ts) and
  // never draws the strip - see nowcast.test.ts for the cascade's own rules.
  test('falls back to the hourly rain line when no nowcast is supplied', () => {
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

    expect(screen.getByText(/rain likely from 3pm \(60%\)/i)).toBeInTheDocument()
    expect(screen.queryByTestId('nowcast-strip')).not.toBeInTheDocument()
  })

  test('shows a dash when there is no nowcast and no hourly/daily precip data at all, never a fabricated no-rain', () => {
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

    expect(screen.getByText('—', { selector: 'p' })).toBeInTheDocument()
    expect(screen.queryByTestId('nowcast-strip')).not.toBeInTheDocument()
  })

  test('draws the nowcast strip and says "Rain expected now" when the current minute already has signal', () => {
    const now = new Date()
    render(
      <CurrentConditionsTile
        depth={5}
        depthLastUpdateAgeS={0}
        windSpeedApparentKts={10}
        maxGustKts={NO_GUSTS}
        weather={weather()}
        forecast={[day()]}
        nextHour={{
          stepMinutes: 15,
          source: 'nowcast',
          points: [
            { time: now, chancePct: 60, mmPerH: 2.1 },
            { time: new Date(now.getTime() + 15 * 60000), chancePct: 70, mmPerH: 3 },
          ],
        }}
        distanceUnits="metric"
      />,
    )

    expect(screen.getByTestId('nowcast-strip')).toBeInTheDocument()
    expect(screen.getByText(/rain expected now/i)).toBeInTheDocument()
    // A genuine nowcast never gets the "hourly forecast" caption.
    expect(screen.queryByTestId('nowcast-hourly-caption')).not.toBeInTheDocument()
    expect(screen.queryByText(/hourly forecast/i)).not.toBeInTheDocument()
  })

  // The strip's plot SVG uses preserveAspectRatio="none" so its bars fill
  // the tile's full width - correct for rects/lines, but an SVG <text>
  // sharing that non-uniformly-scaled viewBox would get its glyphs squashed
  // horizontally. The tick labels must render as plain (undistorted) text
  // outside the SVG, not as an SVG <text> element.
  test('renders the nowcast strip tick labels as plain text outside the stretched SVG, not distorted inside it', () => {
    const now = new Date()
    render(
      <CurrentConditionsTile
        depth={5}
        depthLastUpdateAgeS={0}
        windSpeedApparentKts={10}
        maxGustKts={NO_GUSTS}
        weather={weather()}
        forecast={[day()]}
        nextHour={{
          stepMinutes: 15,
          source: 'nowcast',
          points: [{ time: now, chancePct: 60, mmPerH: 2.1 }],
        }}
        distanceUnits="metric"
      />,
    )

    const strip = screen.getByTestId('nowcast-strip')
    expect(strip.querySelector('text')).toBeNull()
    expect(screen.getByText('Now')).toBeInTheDocument()
    expect(screen.getByText('20m')).toBeInTheDocument()
    expect(screen.getByText('40m')).toBeInTheDocument()
    expect(screen.getByText('60m')).toBeInTheDocument()
  })

  test('says "Rain expected in N minutes" when the nowcast\'s first signal is a later bucket', () => {
    const now = new Date()
    render(
      <CurrentConditionsTile
        depth={5}
        depthLastUpdateAgeS={0}
        windSpeedApparentKts={10}
        maxGustKts={NO_GUSTS}
        weather={weather()}
        forecast={[day()]}
        nextHour={{
          stepMinutes: 15,
          source: 'nowcast',
          points: [
            { time: now, chancePct: 0, mmPerH: 0 },
            { time: new Date(now.getTime() + 30 * 60000), chancePct: 55, mmPerH: 1.8 },
          ],
        }}
        distanceUnits="metric"
      />,
    )

    expect(screen.getByTestId('nowcast-strip')).toBeInTheDocument()
    expect(screen.getByText(/rain expected in 30 minutes/i)).toBeInTheDocument()
  })

  // ADR 0126 addendum: Open-Meteo's minutely_15 is interpolated from the
  // hourly model outside its two native-resolution regions - the tile must
  // still draw the strip (operator's decision: keep drawing it) but caption
  // it honestly, both on the strip itself and in the status line.
  test('source "hourly": still draws the strip, but captions the line and the strip itself', () => {
    const now = new Date()
    render(
      <CurrentConditionsTile
        depth={5}
        depthLastUpdateAgeS={0}
        windSpeedApparentKts={10}
        maxGustKts={NO_GUSTS}
        weather={weather()}
        forecast={[day()]}
        nextHour={{
          stepMinutes: 15,
          source: 'hourly',
          points: [
            { time: now, chancePct: 0, mmPerH: 0 },
            { time: new Date(now.getTime() + 30 * 60000), chancePct: 55, mmPerH: 1.8 },
          ],
        }}
        distanceUnits="metric"
      />,
    )

    expect(screen.getByTestId('nowcast-strip')).toBeInTheDocument()
    expect(screen.getByTestId('nowcast-hourly-caption')).toBeInTheDocument()
    expect(screen.getByText(/light rain expected in 30 minutes \(hourly forecast\)/i)).toBeInTheDocument()
  })

  test('does not draw the strip when the nowcast is entirely dry, falling back to the hourly/daily line instead', () => {
    const now = new Date()
    render(
      <CurrentConditionsTile
        depth={5}
        depthLastUpdateAgeS={0}
        windSpeedApparentKts={10}
        maxGustKts={NO_GUSTS}
        weather={weather()}
        forecast={[day({ hourlyPrecip: [] })]}
        nextHour={{
          stepMinutes: 15,
          source: 'nowcast',
          points: [
            { time: now, chancePct: 0, mmPerH: 0 },
            { time: new Date(now.getTime() + 15 * 60000), chancePct: 0, mmPerH: 0 },
          ],
        }}
        distanceUnits="metric"
      />,
    )

    expect(screen.queryByTestId('nowcast-strip')).not.toBeInTheDocument()
    expect(screen.getByText('—', { selector: 'p' })).toBeInTheDocument()
  })
})
