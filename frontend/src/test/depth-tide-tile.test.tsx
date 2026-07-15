import { render, screen } from '@testing-library/react'
import { expect, test } from 'vitest'

import { DepthTideTile } from '@/components/depth-tide-tile'
import type { DepthTrendData } from '@/hooks/use-depth-trend'
import type { TideToday } from '@/hooks/use-tide-today'

const emptyTrend: DepthTrendData = { points: [], since: 'window' }

const baseTide: TideToday = {
  datetime: new Date().toISOString(),
  current_tide_height_ft: 3,
  tide_direction: 'Falling',
  high_tide_time: new Date(Date.now() - 3600_000).toISOString(),
  high_tide_height_ft: 5,
  low_tide_time: new Date(Date.now() + 3600_000).toISOString(),
  low_tide_height_ft: 1,
  station_name: 'Test Station',
  provider: 'test',
}

test('shows estimated depth at the next low tide, in metric units', () => {
  render(
    <DepthTideTile
      depth={4}
      isImperialDistance={false}
      navigationState="anchored"
      depthTrend={emptyTrend}
      tide={baseTide}
    />,
  )

  // drop from current (3 ft) to low (1 ft) = 2 ft = 0.6096 m; 4 - 0.6096 = 3.3904 m
  expect(screen.getByText('Est. low')).toBeInTheDocument()
  expect(screen.getByText('3.4 m')).toBeInTheDocument()
})

test('shows estimated depth at the next low tide, in imperial units', () => {
  render(
    <DepthTideTile
      depth={4}
      isImperialDistance
      navigationState="anchored"
      depthTrend={emptyTrend}
      tide={baseTide}
    />,
  )

  // depth 13.1 ft - (3 ft - 1 ft) drop = 11.1 ft
  expect(screen.getByText('Est. low')).toBeInTheDocument()
  expect(screen.getByText('11.1 ft')).toBeInTheDocument()
})

const risingTide: TideToday = { ...baseTide, tide_direction: 'Rising' }

test('shows estimated depth at the next high tide when rising, in metric units', () => {
  render(
    <DepthTideTile
      depth={4}
      isImperialDistance={false}
      navigationState="anchored"
      depthTrend={emptyTrend}
      tide={risingTide}
    />,
  )

  // rise from current (3 ft) to high (5 ft) = 2 ft = 0.6096 m; 4 + 0.6096 = 4.6096 m
  expect(screen.getByText('Est. high')).toBeInTheDocument()
  expect(screen.getByText('4.6 m')).toBeInTheDocument()
  expect(screen.queryByText('Est. low')).not.toBeInTheDocument()
})

test('shows estimated depth at the next high tide when rising, in imperial units', () => {
  render(
    <DepthTideTile
      depth={4}
      isImperialDistance
      navigationState="anchored"
      depthTrend={emptyTrend}
      tide={risingTide}
    />,
  )

  // depth 13.1 ft + (5 ft - 3 ft) rise = 15.1 ft
  expect(screen.getByText('Est. high')).toBeInTheDocument()
  expect(screen.getByText('15.1 ft')).toBeInTheDocument()
})

test('hides the estimate when depth or tide data is unavailable', () => {
  render(
    <DepthTideTile
      depth={null}
      isImperialDistance={false}
      navigationState="anchored"
      depthTrend={emptyTrend}
      tide={baseTide}
    />,
  )

  expect(screen.queryByText('Est. low')).not.toBeInTheDocument()
})
