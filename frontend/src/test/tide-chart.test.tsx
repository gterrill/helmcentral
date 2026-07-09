import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { render, screen } from '@testing-library/react'

import { TideChart } from '@/components/tide-chart'
import type { TideChart as TideChartData } from '@/hooks/use-tide-chart'

const STATION = {
  stationId: 'station-1',
  name: 'Test Harbor',
  state: 'CA',
  lat: 37.0,
  lon: -122.0,
  timezone: 'America/Los_Angeles',
}

function buildChart(overrides: Partial<TideChartData> = {}): TideChartData {
  return {
    station: STATION,
    extremes: [],
    currentHeightM: 0.9,
    direction: 'Rising',
    ...overrides,
  }
}

// All bounds constructed via the local-time Date constructor (not ISO 'Z'
// strings) so the window math and the "12AM/6AM/..." tick labels line up
// regardless of the machine's timezone the tests run under - exactly how
// ForecastTideSection will derive windowStart/windowEnd from the local clock.
function todayWindow() {
  return {
    windowStart: new Date(2026, 5, 14, 0, 0, 0),
    windowEnd: new Date(2026, 5, 15, 0, 0, 0),
  }
}

function tomorrowWindow() {
  return {
    windowStart: new Date(2026, 5, 15, 0, 0, 0),
    windowEnd: new Date(2026, 5, 16, 0, 0, 0),
  }
}

describe('TideChart', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date(2026, 5, 14, 12, 0, 0))
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('shows the unavailable message when there are no extremes at all', () => {
    const { windowStart, windowEnd } = todayWindow()
    render(
      <TideChart
        chart={buildChart({ extremes: [] })}
        isImperial={false}
        windowStart={windowStart}
        windowEnd={windowEnd}
      />,
    )

    expect(screen.getByTestId('forecast-tide-unavailable')).toBeInTheDocument()
    expect(screen.getByText('Tide forecast unavailable for this day')).toBeInTheDocument()
  })

  it('shows the unavailable message when extremes exist but none fall inside the given window', () => {
    const { windowStart, windowEnd } = todayWindow()
    render(
      <TideChart
        chart={buildChart({
          extremes: [
            { time: new Date(2026, 5, 20, 4, 0, 0).toISOString(), heightM: 1.8, high: true },
            { time: new Date(2026, 5, 20, 10, 0, 0).toISOString(), heightM: 0.3, high: false },
          ],
        })}
        isImperial={false}
        windowStart={windowStart}
        windowEnd={windowEnd}
      />,
    )

    expect(screen.getByTestId('forecast-tide-unavailable')).toBeInTheDocument()
  })

  it('does not throw when transitioning from no-data (early return) to a loaded chart', () => {
    const { windowStart, windowEnd } = todayWindow()
    const { rerender } = render(
      <TideChart chart={buildChart({ extremes: [] })} isImperial={false} windowStart={windowStart} windowEnd={windowEnd} />,
    )

    expect(screen.getByTestId('forecast-tide-unavailable')).toBeInTheDocument()

    expect(() => {
      rerender(
        <TideChart
          chart={buildChart({
            extremes: [
              { time: new Date(2026, 5, 14, 5, 0, 0).toISOString(), heightM: 1.8, high: true },
              { time: new Date(2026, 5, 14, 18, 0, 0).toISOString(), heightM: 0.3, high: false },
            ],
          })}
          isImperial={false}
          windowStart={windowStart}
          windowEnd={windowEnd}
        />,
      )
    }).not.toThrow()

    expect(screen.queryByTestId('forecast-tide-unavailable')).not.toBeInTheDocument()
  })

  it('shows the Now marker when the window contains the current time', () => {
    const { windowStart, windowEnd } = todayWindow()
    render(
      <TideChart
        chart={buildChart({
          extremes: [
            { time: new Date(2026, 5, 14, 5, 0, 0).toISOString(), heightM: 1.8, high: true },
            { time: new Date(2026, 5, 14, 18, 0, 0).toISOString(), heightM: 0.3, high: false },
          ],
        })}
        isImperial={false}
        windowStart={windowStart}
        windowEnd={windowEnd}
      />,
    )

    expect(screen.getByText('Now')).toBeInTheDocument()
  })

  it('hides the Now marker when the window does not contain the current time', () => {
    const { windowStart, windowEnd } = tomorrowWindow()
    render(
      <TideChart
        chart={buildChart({
          extremes: [
            { time: new Date(2026, 5, 15, 5, 0, 0).toISOString(), heightM: 1.8, high: true },
            { time: new Date(2026, 5, 15, 18, 0, 0).toISOString(), heightM: 0.3, high: false },
          ],
        })}
        isImperial={false}
        windowStart={windowStart}
        windowEnd={windowEnd}
      />,
    )

    expect(screen.queryByText('Now')).not.toBeInTheDocument()
  })

  it('shows 12AM/6AM/12PM/6PM hour ticks across the window', () => {
    const { windowStart, windowEnd } = todayWindow()
    render(
      <TideChart
        chart={buildChart({
          extremes: [
            { time: new Date(2026, 5, 14, 5, 0, 0).toISOString(), heightM: 1.8, high: true },
            { time: new Date(2026, 5, 14, 18, 0, 0).toISOString(), heightM: 0.3, high: false },
          ],
        })}
        isImperial={false}
        windowStart={windowStart}
        windowEnd={windowEnd}
      />,
    )

    // Ticks run 0/6/12/18/24h from window start, so "12AM" legitimately
    // appears twice (the window's start and end midnights).
    expect(screen.getAllByText('12AM').length).toBeGreaterThanOrEqual(2)
    expect(screen.getByText('6AM')).toBeInTheDocument()
    expect(screen.getByText('12PM')).toBeInTheDocument()
    expect(screen.getByText('6PM')).toBeInTheDocument()
  })
})
