import { render, screen } from '@testing-library/react'
import { expect, test } from 'vitest'

import { SolarTile } from '@/components/solar-tile'

test('renders aggregate solar KPIs and controller rows', () => {
  render(
    <SolarTile
      currentW={1280}
      todayKWh={5.43}
      yesterdayKWh={4.98}
      peakTodayW={1560}
      lastUpdateAgeS={2}
      controllers={[
        {
          id: '0',
          label: 'Port',
          currentW: 420,
          todayKWh: 1.82,
          yesterdayKWh: 1.64,
          mode: 'bulk',
          error: null,
          lastUpdateAgeS: 1,
          contributionPct: 32.8,
        },
        {
          id: '1',
          label: 'Starboard',
          currentW: 470,
          todayKWh: 1.90,
          yesterdayKWh: 1.75,
          mode: 'absorption',
          error: 'none',
          lastUpdateAgeS: 1,
          contributionPct: 36.7,
        },
      ]}
    />,
  )

  expect(screen.getByText('Solar')).toBeInTheDocument()
  expect(screen.getByText('Now')).toBeInTheDocument()
  expect(screen.getByText('Peak Today')).toBeInTheDocument()
  expect(screen.getByText('Port')).toBeInTheDocument()
  expect(screen.getByText('Starboard')).toBeInTheDocument()
  expect(screen.getByText('bulk')).toBeInTheDocument()
  expect(screen.getByText('absorption')).toBeInTheDocument()
  expect(screen.queryByTestId('tile-stale-badge')).not.toBeInTheDocument()
})

test('labels the per-controller yesterday figure with its unit, matching the sibling Today readout', () => {
  // The tile previously showed "YESTERDAY 0.70 kWh" on the aggregate card and
  // an unlabelled "YDAY: 1404.00" on the per-controller row a few lines
  // below — same quantity, one with a unit and one without, reading as two
  // different things.
  render(
    <SolarTile
      currentW={1280}
      todayKWh={5.43}
      yesterdayKWh={4.98}
      peakTodayW={1560}
      lastUpdateAgeS={2}
      controllers={[
        {
          id: '0',
          label: 'Port',
          currentW: 420,
          todayKWh: 1.82,
          yesterdayKWh: 1.64,
          mode: 'bulk',
          error: null,
          lastUpdateAgeS: 1,
          contributionPct: 32.8,
        },
      ]}
    />,
  )

  const ydayRow = screen.getByText(/Yday:/).closest('p')
  expect(ydayRow).toHaveTextContent('1.64')
  expect(ydayRow).toHaveTextContent('kWh')
})

test('renders fallback state when controller list is empty', () => {
  render(
    <SolarTile
      currentW={null}
      todayKWh={null}
      yesterdayKWh={null}
      peakTodayW={null}
      lastUpdateAgeS={null}
      controllers={[]}
    />,
  )

  expect(screen.getByText('No controller data')).toBeInTheDocument()
  expect(screen.getAllByText('—').length).toBeGreaterThan(0)
})

test('flags a stale feed instead of presenting its last reading as current', () => {
  // The failure this guards: the Victron feed stops just after dawn, and the
  // 0 W it last published sits on the dashboard all morning looking like a
  // measurement.
  render(
    <SolarTile
      currentW={0}
      todayKWh={0}
      yesterdayKWh={7.78}
      peakTodayW={0}
      lastUpdateAgeS={5940}
      controllers={[
        {
          id: '288',
          label: 'Port',
          currentW: 0,
          todayKWh: 0,
          yesterdayKWh: 3.9,
          mode: 'not charging',
          error: null,
          lastUpdateAgeS: 5940,
          contributionPct: null,
        },
      ]}
    />,
  )

  const badge = screen.getByTestId('tile-stale-badge')
  expect(badge).toBeInTheDocument()
  expect(badge).toHaveTextContent('1h 39m')

  // The stale zero must not be rendered as a live number.
  expect(screen.queryByText('0')).not.toBeInTheDocument()
  expect(screen.getAllByText('—').length).toBeGreaterThan(0)
})

test('greys a single stale controller while the rest keep reporting', () => {
  render(
    <SolarTile
      currentW={890}
      todayKWh={4.1}
      yesterdayKWh={7.78}
      peakTodayW={1500}
      lastUpdateAgeS={3}
      controllers={[
        {
          id: '288',
          label: 'Port',
          currentW: 890,
          todayKWh: 4.1,
          yesterdayKWh: 3.9,
          mode: 'bulk',
          error: null,
          lastUpdateAgeS: 3,
          contributionPct: 100,
        },
        {
          id: '289',
          label: 'Starboard',
          currentW: 0,
          todayKWh: 0,
          yesterdayKWh: 3.88,
          mode: 'not charging',
          error: null,
          lastUpdateAgeS: 4200,
          contributionPct: null,
        },
      ]}
    />,
  )

  // Tile as a whole is live, so no tile-level badge.
  expect(screen.queryByTestId('tile-stale-badge')).not.toBeInTheDocument()

  // The one dead controller is called out individually.
  const controllerBadge = screen.getByTestId('controller-stale-289')
  expect(controllerBadge).toHaveTextContent('1h 10m')

  // The live controller keeps its reading; the array total still shows the
  // same 890 W, so both readouts are expected to carry it.
  expect(screen.getAllByText('890')).toHaveLength(2)

  // The dead controller contributes no number of its own.
  expect(screen.queryByTestId('controller-stale-288')).not.toBeInTheDocument()
})
