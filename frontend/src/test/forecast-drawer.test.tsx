import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { act, fireEvent, render, screen } from '@testing-library/react'

import { ForecastDrawer, formatRefreshAge } from '@/components/forecast-drawer'

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

  it('updates age label as time advances', async () => {
    render(
      <ForecastDrawer
        forecast={[
          {
            date: 'Jun 14',
            dayName: 'Sunday',
            condition: 'Clear',
            high: 76,
            low: 62,
            windSpeed: 10,
            windGust: 14,
            windDirection: 'NE',
            precipitation: 5,
          },
        ]}
        loading={false}
        error={null}
        isCached
        updatedAt="2026-06-14T12:30:00Z"
        ttlSeconds={3600}
        unit="metric"
      />,
    )

    expect(screen.getByText(/Age: just now/)).toBeInTheDocument()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(60000)
    })

    expect(screen.getByText(/Age: 1 min ago/)).toBeInTheDocument()
  })

  it('updates summary metrics when a day card is selected', () => {
    render(
      <ForecastDrawer
        forecast={[
          {
            date: 'Jun 14',
            dayName: 'Sunday',
            condition: 'Clear',
            high: 76,
            low: 62,
            windSpeed: 10,
            windGust: 14,
            windDirection: 'NE',
            precipitation: 5,
          },
          {
            date: 'Jun 15',
            dayName: 'Monday',
            condition: 'Cloudy',
            high: 68,
            low: 58,
            windSpeed: 22,
            windGust: 30,
            windDirection: 'SW',
            precipitation: 85,
          },
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
        forecast={[
          {
            date: 'Jun 14',
            dayName: 'Sunday',
            condition: 'Mostly Sunny',
            high: 76,
            low: 62,
            windSpeed: 10,
            windGust: 14,
            windDirection: 'NE',
            precipitation: 5,
          },
        ]}
        hourlyToday={[
          { label: 'Now', condition: 'Mostly Sunny', temperatureF: 72, kind: 'forecast' },
          { label: '11AM', condition: 'Mostly Sunny', temperatureF: 72, kind: 'forecast' },
          { label: '5:09PM', condition: 'Sunset', temperatureF: -1, kind: 'sunset' },
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
    expect(screen.getByText('Sunset')).toBeInTheDocument()
  })
})
