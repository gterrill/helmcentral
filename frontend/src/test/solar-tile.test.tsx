import { fireEvent, render, screen, within } from '@testing-library/react'
import { expect, test } from 'vitest'

import { SolarTile } from '@/components/solar-tile'
import type { SolarController } from '@/hooks/use-solar-state'

const base = {
  currentW: 1280,
  todayKWh: 5.43,
  yesterdayKWh: 4.98,
  peakTodayW: 1560,
  lastUpdateAgeS: 2,
}

function ctl(id: string, label: string, over: Partial<SolarController> = {}): SolarController {
  return {
    id,
    label,
    currentW: 400,
    todayKWh: 1.5,
    yesterdayKWh: 1.4,
    mode: 'bulk',
    error: null,
    lastUpdateAgeS: 1,
    ...over,
  }
}

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
        },
      ]}
    />,
  )

  expect(screen.getByText('Solar')).toBeInTheDocument()
  expect(screen.getByText('Now')).toBeInTheDocument()
  expect(screen.getByText('Peak Today')).toBeInTheDocument()
  expect(screen.getByTestId('solar-segment-0')).toBeInTheDocument()
  expect(screen.getByTestId('solar-segment-1')).toBeInTheDocument()
  expect(screen.getByTestId('solar-array-status')).toHaveTextContent('2 arrays · no faults')
  expect(screen.queryByTestId('tile-stale-badge')).not.toBeInTheDocument()
})

test('sizes each segment by its controller yield today', () => {
  render(<SolarTile {...base} controllers={[ctl('0', 'Port', { todayKWh: 3 }), ctl('1', 'Starboard', { todayKWh: 1 })]} />)
  expect(screen.getByTestId('solar-segment-0')).toHaveStyle({ flexGrow: '3' })
  expect(screen.getByTestId('solar-segment-1')).toHaveStyle({ flexGrow: '1' })
})

test('shows equal segments when no controller has a yield yet', () => {
  render(<SolarTile {...base} controllers={[ctl('0', 'Port', { todayKWh: 0 }), ctl('1', 'Starboard', { todayKWh: null })]} />)
  expect(screen.getByTestId('solar-segment-0')).toHaveStyle({ flexGrow: '1' })
  expect(screen.getByTestId('solar-segment-1')).toHaveStyle({ flexGrow: '1' })
})

test('uses the singular for one array', () => {
  render(<SolarTile {...base} controllers={[ctl('0', 'Port')]} />)
  expect(screen.getByTestId('solar-array-status')).toHaveTextContent('1 array · no faults')
})

test('names the first problem under the bar and counts the rest', () => {
  render(
    <SolarTile
      {...base}
      controllers={[
        ctl('290', 'Array 290', { error: '38' }),
        ctl('291', 'Array 291', { lastUpdateAgeS: 300 }),
        ctl('288', 'Array 288'),
      ]}
    />,
  )
  const status = screen.getByTestId('solar-array-status')
  expect(status).toHaveTextContent('Array 290 · error 38 +1 more')
  expect(status).not.toHaveTextContent('no faults')
})

test('does not call a switched-off array a fault, since MPPTs report off every night', () => {
  render(<SolarTile {...base} controllers={[ctl('289', 'Array 289', { mode: 'Off' }), ctl('288', 'Array 288', { mode: 'off' })]} />)
  expect(screen.getByTestId('solar-array-status')).toHaveTextContent('2 arrays · no faults')
  expect(screen.getByTestId('solar-segment-289').className).not.toContain('bg-red-500')
})

test('sizes a faulted array by its real yield so far', () => {
  render(<SolarTile {...base} controllers={[ctl('0', 'Port', { todayKWh: 3, error: '38' }), ctl('1', 'Starboard', { todayKWh: 0.5 })]} />)
  expect(screen.getByTestId('solar-segment-0')).toHaveStyle({ flexGrow: '3' })
  expect(screen.getByTestId('solar-segment-0').className).toContain('bg-red-500')
})

test('the bar button is named with its visible status', () => {
  render(<SolarTile {...base} controllers={[ctl('290', 'Array 290', { error: '38' })]} />)
  expect(screen.getByRole('button', { name: 'Show each array. Array 290 · error 38' })).toBeInTheDocument()
})

test('a controller error leads the line ahead of a silent array listed before it', () => {
  render(<SolarTile {...base} controllers={[ctl('291', 'Array 291', { lastUpdateAgeS: 300 }), ctl('290', 'Array 290', { error: '38' })]} />)
  expect(screen.getByTestId('solar-array-status')).toHaveTextContent('Array 290 · error 38 +1 more')
})

test('opens a table of each array from the bar', async () => {
  render(
    <SolarTile
      {...base}
      controllers={[
        ctl('0', 'Port', { currentW: 420, todayKWh: 1.82, yesterdayKWh: 1.64, mode: 'bulk' }),
        ctl('1', 'Starboard', { currentW: 470, todayKWh: 1.9, yesterdayKWh: 1.75, mode: 'absorption', error: '38' }),
      ]}
    />,
  )
  expect(screen.queryByText('bulk')).not.toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: /^Show each array/ }))
  const portRow = (await screen.findByText('bulk')).closest('tr') as HTMLElement
  expect(portRow).toHaveTextContent('Port')
  expect(portRow).toHaveTextContent('420')
  expect(portRow).toHaveTextContent('1.82')
  expect(portRow).toHaveTextContent('1.64')
  expect(within(screen.getByRole('table')).getByText('absorption')).toBeInTheDocument()
  expect(screen.getByText('Error 38')).toBeInTheDocument()
  expect(screen.queryByText(/Share/)).not.toBeInTheDocument()
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

test('flags a stale feed instead of presenting its last reading as current', async () => {
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

  fireEvent.click(screen.getByRole('button', { name: /^Show each array/ }))
  const row = (await screen.findByRole('table')).querySelector('tbody tr') as HTMLElement
  expect(row).not.toHaveTextContent('3.9')
  expect(within(row).getAllByText('—')).toHaveLength(4)
})

test('greys a single stale controller while the rest keep reporting', async () => {
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
        },
      ]}
    />,
  )

  // Tile as a whole is live, so no tile-level badge.
  expect(screen.queryByTestId('tile-stale-badge')).not.toBeInTheDocument()
  expect(screen.getByTestId('solar-array-status')).toHaveTextContent('Starboard · no update for 1h 10m')

  fireEvent.click(screen.getByRole('button', { name: /^Show each array/ }))
  await screen.findByRole('table')

  // The one dead controller is called out individually.
  expect(screen.getByTestId('controller-stale-289')).toHaveTextContent('1h 10m')
  expect(screen.queryByTestId('controller-stale-288')).not.toBeInTheDocument()

  // The dead controller contributes no number of its own.
  const rows = screen.getAllByRole('row')
  expect(rows[2]).toHaveTextContent('Starboard')
  expect(within(rows[2]).getAllByText('—')).toHaveLength(4)
  expect(rows[1]).toHaveTextContent('890')
})

test('a narrow tile gives each figure its own row instead of two by two', () => {
  const { container } = render(<SolarTile {...base} controllers={[ctl('0', 'Port')]} />)
  expect(container.querySelector('.\\@container')).not.toBeNull()
  const figures = screen.getByText('Now').closest('div')!.parentElement!
  expect(figures.className).toContain('grid-cols-2')
  expect(figures.className).toContain('@max-2xs:grid-cols-1')
  expect(screen.getByText('Yesterday').closest('div')!.className).toContain('@max-2xs:flex')
})
