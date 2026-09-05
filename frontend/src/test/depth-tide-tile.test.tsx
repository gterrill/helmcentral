import { render, screen, fireEvent } from '@testing-library/react'
import { expect, test, vi } from 'vitest'

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
      lastUpdateAgeS={null}
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
      lastUpdateAgeS={null}
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
      lastUpdateAgeS={null}
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
      lastUpdateAgeS={null}
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
      lastUpdateAgeS={null}
    />,
  )

  expect(screen.queryByText('Est. low')).not.toBeInTheDocument()
})

// ── clickable-tile semantics ────────────────────────────────────────────
// Previously a bare <div onClick> with cursor-pointer styling: no role, no
// keyboard access, no focus ring. A touchscreen mouse-substitute is not a
// keyboard user's only way in.

test('becomes a real, clickable button when onOpen is provided', () => {
  const onOpen = vi.fn()
  render(
    <DepthTideTile
      depth={4}
      isImperialDistance={false}
      navigationState="anchored"
      depthTrend={emptyTrend}
      tide={baseTide}
      lastUpdateAgeS={null}
      onOpen={onOpen}
    />,
  )

  const trigger = screen.getByRole('button')
  expect(trigger.tagName).toBe('BUTTON')
  expect(trigger.className).toMatch(/focus-visible:ring/)

  fireEvent.click(trigger)
  expect(onOpen).toHaveBeenCalledTimes(1)
})

test('stays a plain, non-interactive wrapper when there is nothing to open', () => {
  render(
    <DepthTideTile
      depth={4}
      isImperialDistance={false}
      navigationState="anchored"
      depthTrend={emptyTrend}
      tide={baseTide}
      lastUpdateAgeS={null}
    />,
  )

  expect(screen.queryByRole('button')).not.toBeInTheDocument()
})

// ── staleness ────────────────────────────────────────────────────────────
// Depth is the number you run aground on. A dead transducer must not leave
// a confident reading on screen forever.

test('flags the tile stale once the feed age passes the threshold', () => {
  render(
    <DepthTideTile
      depth={4}
      isImperialDistance={false}
      navigationState="anchored"
      depthTrend={emptyTrend}
      tide={baseTide}
      lastUpdateAgeS={5940}
    />,
  )

  const badge = screen.getByTestId('tile-stale-badge')
  expect(badge).toHaveTextContent('1h 39m')
})

test('does not flag the tile stale when no update age is known', () => {
  render(
    <DepthTideTile
      depth={4}
      isImperialDistance={false}
      navigationState="anchored"
      depthTrend={emptyTrend}
      tide={baseTide}
      lastUpdateAgeS={null}
    />,
  )

  expect(screen.queryByTestId('tile-stale-badge')).not.toBeInTheDocument()
})
