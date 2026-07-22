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
})

test('renders fallback state when controller list is empty', () => {
  render(
    <SolarTile
      currentW={null}
      todayKWh={null}
      yesterdayKWh={null}
      peakTodayW={null}
      controllers={[]}
    />,
  )

  expect(screen.getByText('No controller data')).toBeInTheDocument()
  expect(screen.getAllByText('—').length).toBeGreaterThan(0)
})
