import { describe, it, expect } from 'vitest'
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
  const now = Date.now()
  return {
    station: STATION,
    extremes: [
      { time: new Date(now - 60 * 60 * 1000).toISOString(), heightM: 0.4, high: false },
      { time: new Date(now + 5 * 60 * 60 * 1000).toISOString(), heightM: 1.8, high: true },
      { time: new Date(now + 11 * 60 * 60 * 1000).toISOString(), heightM: 0.3, high: false },
    ],
    currentHeightM: 0.9,
    direction: 'Rising',
    ...overrides,
  }
}

describe('TideChart', () => {
  it('shows the no-data message when there are no extremes', () => {
    render(<TideChart chart={buildChart({ extremes: [] })} isImperial={false} />)

    expect(screen.getByText('No tide data available for this station')).toBeInTheDocument()
  })

  it('does not throw when transitioning from no-data (early return) to a loaded chart', () => {
    const { rerender } = render(<TideChart chart={buildChart({ extremes: [] })} isImperial={false} />)

    expect(screen.getByText('No tide data available for this station')).toBeInTheDocument()

    expect(() => {
      rerender(<TideChart chart={buildChart()} isImperial={false} />)
    }).not.toThrow()

    expect(screen.queryByText('No tide data available for this station')).not.toBeInTheDocument()
  })
})
