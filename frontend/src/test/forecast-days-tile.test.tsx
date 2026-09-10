import { render, screen } from '@testing-library/react'
import { describe, expect, test } from 'vitest'

import { ForecastDaysTile } from '@/components/forecast-days-tile'
import type { WeatherForecastDay } from '@/hooks/use-weather-forecast'

function day(dayName: string, overrides: Partial<WeatherForecastDay> = {}): WeatherForecastDay {
  return {
    dayKey: dayName,
    date: dayName,
    dayName,
    condition: 'Clear',
    high: 82,
    low: 68,
    windSpeed: 10,
    windGust: 14,
    windDirection: 'E',
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

const SEVEN_DAYS = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'].map((n, i) =>
  day(n, { high: 80 + i, low: 60 + i }),
)

describe('ForecastDaysTile', () => {
  test('renders tomorrow onward, skipping today', () => {
    render(<ForecastDaysTile days={SEVEN_DAYS} units="imperial" />)

    expect(screen.queryByText('SUN')).not.toBeInTheDocument()
    expect(screen.getByText('MON')).toBeInTheDocument()
    expect(screen.getByText('FRI')).toBeInTheDocument()
  })

  test('renders at most five days', () => {
    render(<ForecastDaysTile days={SEVEN_DAYS} units="imperial" />)

    // tomorrow(Mon) through 5 days out = Mon..Fri, Saturday excluded.
    expect(screen.queryByText('SAT')).not.toBeInTheDocument()
  })

  test('shows imperial highs and lows in Fahrenheit', () => {
    render(<ForecastDaysTile days={[day('Sunday'), day('Monday', { high: 82, low: 68 })]} units="imperial" />)

    expect(screen.getByText('82°')).toBeInTheDocument()
    expect(screen.getByText('68°')).toBeInTheDocument()
  })

  test('converts highs and lows to Celsius when metric', () => {
    render(<ForecastDaysTile days={[day('Sunday'), day('Monday', { high: 82, low: 68 })]} units="metric" />)

    // 82F -> 28C, 68F -> 20C
    expect(screen.getByText('28°')).toBeInTheDocument()
    expect(screen.getByText('20°')).toBeInTheDocument()
  })

  test('renders dash cards for missing days rather than hiding them, always showing five slots', () => {
    render(<ForecastDaysTile days={[day('Sunday'), day('Monday')]} units="imperial" />)

    expect(screen.getAllByText('--').length).toBeGreaterThan(0)
    expect(screen.getAllByTestId('forecast-days-card')).toHaveLength(5)
  })

  test('shows a dash when a day has the -1 sentinel for high or low', () => {
    render(<ForecastDaysTile days={[day('Sunday'), day('Monday', { high: -1, low: -1 })]} units="imperial" />)

    const card = screen.getAllByTestId('forecast-days-card')[0]
    expect(card).toHaveTextContent('--')
  })
})
