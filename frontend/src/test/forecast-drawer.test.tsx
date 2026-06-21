import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'

import { ForecastDrawer, formatRefreshAge } from '@/components/forecast-drawer'

const HOUR_LABELS = [
  '12AM', '1AM', '2AM', '3AM', '4AM', '5AM', '6AM', '7AM', '8AM', '9AM', '10AM', '11AM',
  '12PM', '1PM', '2PM', '3PM', '4PM', '5PM', '6PM', '7PM', '8PM', '9PM', '10PM', '11PM',
]

function buildHourlyWind(count = 24) {
  return Array.from({ length: count }, (_, idx) => ({
    label: HOUR_LABELS[idx % HOUR_LABELS.length],
    windSpeed: 10 + idx,
    windGust: 15 + idx,
    windDirection: 'NE',
    windDirectionDeg: 45,
  }))
}

function buildHourlyWave(count = 24) {
  return Array.from({ length: count }, (_, idx) => ({
    label: HOUR_LABELS[idx % HOUR_LABELS.length],
    waveHeightM: 1 + idx * 0.05,
    wavePeriodS: 6,
    waveDirectionDeg: 90,
    windWaveHeightM: 0.4 + idx * 0.02,
    swellWaveHeightM: 0.8 + idx * 0.03,
  }))
}

function buildHourlyPrecip(count = 24) {
  return Array.from({ length: count }, (_, idx) => ({
    label: HOUR_LABELS[idx % HOUR_LABELS.length],
    precipChancePct: (idx * 4) % 101,
    precipIntensityMm: idx % 6 === 0 ? 1.5 + idx * 0.1 : 0,
  }))
}

function buildHourlyUV(count = 24) {
  return Array.from({ length: count }, (_, idx) => ({
    label: HOUR_LABELS[idx % HOUR_LABELS.length],
    uvIndex: Math.max(0, Math.round(6 - Math.abs(idx - 12) * 0.6)),
  }))
}

function buildDay(overrides: Record<string, unknown> = {}) {
  return {
    date: 'Jun 14',
    dayName: 'Sunday',
    condition: 'Clear',
    high: 76,
    low: 62,
    windSpeed: 10,
    windGust: 14,
    windDirection: 'NE',
    windSummary: 'Winds 10 to 19 kts, with gusts up to 24 kts.',
    waveSummary: 'Significant wave height 1.0 to 1.2 m from the E, with a period around 6 sec.',
    precipitationSummary: 'Slight chance of rain after 5PM.',
    precipitation: 5,
    sunriseTime: '6:32AM',
    sunsetTime: '5:47PM',
    moonPhase: 'waningCrescent',
    hourlyWind: buildHourlyWind(),
    hourlyWave: buildHourlyWave(),
    hourlyPrecip: buildHourlyPrecip(),
    hourlyUV: buildHourlyUV(),
    ...overrides,
  }
}

describe('ForecastDrawer refresh age', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-06-14T12:30:00Z'))
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('formats relative refresh age labels', () => {
    expect(formatRefreshAge('2026-06-14T12:30:00Z', new Date('2026-06-14T12:30:20Z').getTime())).toBe('just now')
    expect(formatRefreshAge('2026-06-14T12:29:00Z', new Date('2026-06-14T12:30:00Z').getTime())).toBe('1 min ago')
    expect(formatRefreshAge('2026-06-14T10:00:00Z', new Date('2026-06-14T12:30:00Z').getTime())).toBe('2 hours ago')
  })

  it('updates summary metrics when a day card is selected', () => {
    render(
      <ForecastDrawer
        forecast={[
          buildDay(),
          buildDay({
            date: 'Jun 15',
            dayName: 'Monday',
            condition: 'Cloudy',
            high: 68,
            low: 58,
            windSpeed: 22,
            windGust: 30,
            windDirection: 'SW',
            precipitation: 85,
          }),
        ]}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    expect(screen.getByTestId('forecast-selected-wind')).toHaveTextContent('10.0 kts')

    fireEvent.click(screen.getByRole('button', { name: /Select forecast day Monday Jun 15/i }))

    expect(screen.getByTestId('forecast-selected-wind')).toHaveTextContent('22.0 kts')
    expect(screen.getByTestId('forecast-selected-gust')).toHaveTextContent('30.0 kts')
  })

  it('renders the hourly strip with summary and sunset marker', () => {
    render(
      <ForecastDrawer
        forecast={[buildDay({ condition: 'Mostly Sunny' })]}
        hourlyToday={[
          { label: 'Now', condition: 'Mostly Sunny', temperatureF: 72, windSpeedKts: 11, windGustKts: 18, windDirection: 'NE', windDirectionDeg: 45, kind: 'forecast' },
          { label: '11AM', condition: 'Mostly Sunny', temperatureF: 72, windSpeedKts: 12, windGustKts: 19, windDirection: 'NE', windDirectionDeg: 50, kind: 'forecast' },
          { label: '5:09PM', condition: 'Sunset', temperatureF: -1, windSpeedKts: -1, windGustKts: -1, windDirection: '—', windDirectionDeg: -1, kind: 'sunset' },
        ]}
        summary="Sunny conditions will continue all day."
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    expect(screen.getByText('Sunny conditions will continue all day.')).toBeInTheDocument()
    expect(screen.getByText('Now')).toBeInTheDocument()
    expect(screen.getByText('11AM')).toBeInTheDocument()
    expect(screen.getByText('5:09PM')).toBeInTheDocument()
    expect(screen.getAllByText('Sunset').length).toBeGreaterThan(0)
    expect(screen.getByText('11kts NE')).toBeInTheDocument()
    expect(screen.getByText('12kts NE')).toBeInTheDocument()
  })

  it('renders up to 10 day tabs', () => {
    const days = Array.from({ length: 12 }, (_, idx) =>
      buildDay({
        date: `Jun ${14 + idx}`,
        dayName: 'Sunday',
        precipitation: idx,
      }),
    )

    render(<ForecastDrawer forecast={days} loading={false} error={null} unit="metric" />)

    const tabs = screen.getAllByRole('button', { name: /Select forecast day/i })
    expect(tabs).toHaveLength(10)
  })

  it('renders wind and wave graphs for the selected day', () => {
    render(<ForecastDrawer forecast={[buildDay()]} loading={false} error={null} unit="metric" />)

    expect(screen.getByTestId('forecast-wind-chart')).toBeInTheDocument()
    expect(screen.getByTestId('forecast-wave-chart')).toBeInTheDocument()
    expect(screen.queryByTestId('forecast-wave-unavailable')).not.toBeInTheDocument()
  })

  it('shows the wind summary sentence and direction barbs for the selected day', () => {
    render(<ForecastDrawer forecast={[buildDay()]} loading={false} error={null} unit="metric" />)

    expect(screen.getByText('Winds 10 to 19 kts, with gusts up to 24 kts.')).toBeInTheDocument()
    expect(screen.getAllByTestId('forecast-wind-barb').length).toBeGreaterThan(0)
  })

  it('shows the wave summary sentence, direction arrows and period for the selected day', () => {
    render(<ForecastDrawer forecast={[buildDay()]} loading={false} error={null} unit="metric" />)

    expect(screen.getByText('Significant wave height 1.0 to 1.2 m from the E, with a period around 6 sec.')).toBeInTheDocument()
    expect(screen.getAllByTestId('forecast-wave-arrow').length).toBeGreaterThan(0)
    expect(screen.getAllByText('6.0s').length).toBeGreaterThan(0)
  })

  it('shows sunrise, sunset and moon phase for the selected day', () => {
    render(<ForecastDrawer forecast={[buildDay()]} loading={false} error={null} unit="metric" />)

    expect(screen.getByText('6:32AM')).toBeInTheDocument()
    expect(screen.getByText('5:47PM')).toBeInTheDocument()
    expect(screen.getByText('Waning Crescent')).toBeInTheDocument()
  })

  it('omits the wind summary when not provided', () => {
    render(
      <ForecastDrawer
        forecast={[buildDay({ windSummary: null })]}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    expect(screen.queryByText(/winds will be/)).not.toBeInTheDocument()
  })

  it('shows an unavailable message when a day has no wave forecast', () => {
    render(
      <ForecastDrawer
        forecast={[buildDay({ hourlyWave: [] })]}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    expect(screen.getByTestId('forecast-wind-chart')).toBeInTheDocument()
    expect(screen.queryByTestId('forecast-wave-chart')).not.toBeInTheDocument()
    expect(screen.getByTestId('forecast-wave-unavailable')).toBeInTheDocument()
  })

  it('renders the precipitation graph with bars for the selected day', () => {
    render(<ForecastDrawer forecast={[buildDay()]} loading={false} error={null} unit="metric" />)

    expect(screen.getByTestId('forecast-precip-chart')).toBeInTheDocument()
    expect(screen.getAllByTestId('forecast-precip-bar').length).toBeGreaterThan(0)
  })

  it('shows the precipitation summary sentence for the selected day', () => {
    render(<ForecastDrawer forecast={[buildDay()]} loading={false} error={null} unit="metric" />)

    expect(screen.getByText('Slight chance of rain after 5PM.')).toBeInTheDocument()
  })

  it('shows an unavailable message when a day has no precipitation forecast', () => {
    render(
      <ForecastDrawer
        forecast={[buildDay({ hourlyPrecip: [] })]}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    expect(screen.getByTestId('forecast-wind-chart')).toBeInTheDocument()
    expect(screen.queryByTestId('forecast-precip-chart')).not.toBeInTheDocument()
    expect(screen.getByTestId('forecast-precip-unavailable')).toBeInTheDocument()
  })

  it('renders the UV index graph with a sun protection recommendation', () => {
    render(<ForecastDrawer forecast={[buildDay()]} loading={false} error={null} unit="metric" />)

    expect(screen.getByTestId('forecast-uv-chart')).toBeInTheDocument()
    expect(screen.getByText('Sun protection recommended from 7AM to 5PM.')).toBeInTheDocument()
  })

  it('shows an unavailable message when a day has no UV forecast', () => {
    render(
      <ForecastDrawer
        forecast={[buildDay({ hourlyUV: [] })]}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    expect(screen.getByTestId('forecast-precip-chart')).toBeInTheDocument()
    expect(screen.queryByTestId('forecast-uv-chart')).not.toBeInTheDocument()
    expect(screen.getByTestId('forecast-uv-unavailable')).toBeInTheDocument()
  })

  it('shows no sun protection needed when UV stays low all day', () => {
    render(
      <ForecastDrawer
        forecast={[buildDay({ hourlyUV: buildHourlyUV().map((entry) => ({ ...entry, uvIndex: 1 })) })]}
        loading={false}
        error={null}
        unit="metric"
      />,
    )

    expect(screen.getByText('No sun protection needed.')).toBeInTheDocument()
  })
})
