import { render, screen } from '@testing-library/react'
import { describe, expect, test } from 'vitest'

import { SeaStateTile } from '@/components/sea-state-tile'
import type { WeatherForecastDay } from '@/hooks/use-weather-forecast'
import type { WaveForecastDay } from '@/hooks/use-wave-forecast'

function day(dayKey: string): WeatherForecastDay {
  return {
    dayKey,
    date: dayKey,
    dayName: 'Sunday',
    condition: 'Clear',
    high: 80,
    low: 65,
    windSpeed: 10,
    windGust: 15,
    windDirection: 'E',
    windSummary: null,
    precipitationSummary: null,
    precipitation: null,
    humidityPct: null,
    visibilityNm: null,
    sunriseTime: null,
    sunsetTime: null,
    moonPhase: null,
    hourlyWind: Array.from({ length: 24 }, (_, h) => ({
      label: `${h}:00`,
      hourOfDay: h,
      windSpeed: 10,
      windGust: 15,
      windDirection: 'E',
      windDirectionDeg: 90,
    })),
    hourlyPrecip: [],
    hourlyUV: [],
    hourlyCloud: [],
  }
}

function waveDay(dayKey: string): WaveForecastDay {
  return {
    dayKey,
    date: dayKey,
    dayName: 'Sunday',
    waveSummary: null,
    hourlyWave: Array.from({ length: 24 }, (_, h) => ({
      label: `${h}:00`,
      hourOfDay: h,
      waveHeightM: 1,
      wavePeriodS: 6,
      waveDirectionDeg: 180,
      windWaveHeightM: 0.5,
      swellWaveHeightM: 0.5,
      windWaveDirectionDeg: 0,
      windWavePeriodS: 5,
      swellWaveDirectionDeg: 0,
      swellWavePeriodS: 8,
      steepnessRatio: 0.02,
      steepnessBand: 'rolling' as const,
    })),
    indicators: { waveFront: false, rapidBuild: false, periodStep: false, crossSea: false },
  }
}

describe('SeaStateTile', () => {
  test('renders the chart when wave data is available', () => {
    render(
      <SeaStateTile
        forecast={[day('2026-06-14')]}
        waveForecastDays={[waveDay('2026-06-14')]}
        waveLoading={false}
        waveError={null}
        units="metric"
      />,
    )

    expect(screen.getByText('Sea State')).toBeInTheDocument()
    expect(document.querySelector('svg.recharts-surface')).toBeInTheDocument()
    expect(screen.queryByTestId('forecast-wave-error')).not.toBeInTheDocument()
  })

  test('shows the unavailable message and no chart when the wave feed errors', () => {
    render(
      <SeaStateTile
        forecast={[day('2026-06-14')]}
        waveForecastDays={[]}
        waveLoading={false}
        waveError="fetch failed"
        units="metric"
      />,
    )

    expect(screen.getByTestId('forecast-wave-error')).toHaveTextContent(/wave data unavailable/i)
    expect(document.querySelector('svg.recharts-surface')).not.toBeInTheDocument()
  })

  test('renders without crashing when the wave feed has not answered yet', () => {
    render(
      <SeaStateTile forecast={[day('2026-06-14')]} waveForecastDays={[]} waveLoading waveError={null} units="metric" />,
    )

    expect(screen.getByText('Sea State')).toBeInTheDocument()
  })
})
