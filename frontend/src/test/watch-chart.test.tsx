import { describe, expect, it } from 'vitest'
import { render, screen, within } from '@testing-library/react'

import { WatchChart } from '@/components/watches/watch-chart'
import { WatchRunCard } from '@/components/watches/watch-run-list'
import type { WatchChartData, WatchRun } from '@/lib/watches'

function chart(over: Partial<WatchChartData> = {}): WatchChartData {
  const from = Date.parse('2026-10-29T00:00:00Z')
  const points = (f: (i: number) => number): [number, number][] => Array.from({ length: 25 }, (_, i) => [from + i * 30 * 60_000, f(i)])
  return {
    finding_index: 0,
    title: 'Pressure, last 12 hours',
    unit: 'hPa',
    timezone: 'UTC+12',
    utc_offset_seconds: 43200,
    window: { from: '2026-10-29T00:00:00Z', to: '2026-10-29T12:00:00Z' },
    series: [{ label: 'Pressure', unit: 'hPa', points: points((i) => 1015 - i * 0.4) }],
    threshold: { label: '3 h fall limit', value: 1008 },
    ...over,
  }
}

function run(over: Partial<WatchRun> = {}): WatchRun {
  return {
    id: 'r1',
    watch_id: 'w1',
    started_at: '2026-10-29T12:00:00Z',
    finished_at: '2026-10-29T12:00:30Z',
    boat_time: 'Thu 29 Oct 12:00 UTC+12',
    timezone: 'UTC+12',
    outcome: 'ran',
    report_kind: 'sections',
    findings: [
      { section: 'weather', rule: 'pressure_fall', text: 'Pressure is down 4 hPa in 3 hours.', by: 'rules' },
      { section: 'tanks', rule: 'tank_change', text: 'Diesel is down 14 points.', by: 'rules', chart_problem: 'No history to chart: nothing was recorded for Diesel in this window' },
      { section: 'power', rule: 'x', text: 'No chart wanted.', by: 'rules' },
    ],
    charts: [chart()],
    notify_notes: [],
    review: 'Fine.',
    review_problem: null,
    cost_usd: 0,
    cost_known: true,
    notified: [],
    ...over,
  }
}

describe('WatchChart', () => {
  it('names the chart, its window on the boat clock, its unit and its threshold', () => {
    render(<WatchChart chart={chart()} />)
    const card = screen.getByTestId('watch-chart')
    expect(within(card).getByText('Pressure, last 12 hours')).toBeInTheDocument()
    expect(within(card).getByText('Pressure (hPa)')).toBeInTheDocument()
    expect(within(card).getByText('3 h fall limit')).toBeInTheDocument()
    // The window is shown on the boat's clock: 00:00 UTC is 12:00 at UTC+12.
    expect(within(card).getByText(/12:00 .* 00:00/)).toBeInTheDocument()
    expect(within(card).getByText('UTC+12')).toBeInTheDocument()
    expect(within(card).getByRole('img', { name: /Pressure, last 12 hours/ })).toBeInTheDocument()
  })

  it('stacks one panel for each unit', () => {
    const c = chart({ series: [chart().series[0], { label: 'Current', unit: 'A', points: chart().series[0].points }], threshold: undefined })
    render(<WatchChart chart={c} />)
    expect(screen.getAllByTestId('watch-chart-panel')).toHaveLength(2)
  })

  it('says it cannot draw a series with fewer than two points instead of drawing one', () => {
    render(<WatchChart chart={chart({ series: [{ label: 'Pressure', unit: 'hPa', points: [[1, 1]] }] })} />)
    expect(screen.getByRole('alert')).toHaveTextContent('Cannot draw this chart: Pressure has 1 reading')
    expect(screen.queryByTestId('watch-chart-panel')).toBeNull()
  })
})

describe('WatchRunCard charts', () => {
  const props = { canWrite: false, onDelete: () => {} }

  it('puts a chart under its finding and the reason a chart is missing as a muted line', () => {
    render(<WatchRunCard run={run()} {...props} />)
    const first = screen.getByText('Pressure is down 4 hPa in 3 hours.').closest('li')!
    expect(within(first).getByTestId('watch-chart')).toBeInTheDocument()
    const second = screen.getByText('Diesel is down 14 points.').closest('li')!
    expect(within(second).queryByTestId('watch-chart')).toBeNull()
    const problem = within(second).getByText('No history to chart: nothing was recorded for Diesel in this window')
    expect(problem.className).toContain('text-muted-foreground')
    const third = screen.getByText('No chart wanted.').closest('li')!
    expect(within(third).queryByTestId('watch-chart')).toBeNull()
    expect(screen.getAllByTestId('watch-chart')).toHaveLength(1)
  })

  it('shows a delivery note such as a message sent without its chart', () => {
    render(<WatchRunCard run={run({ notified: ['ntfy'], notify_notes: ['ntfy: sent without the chart (the server answered 400: attachments not allowed)'] })} {...props} />)
    expect(screen.getByText('ntfy: sent without the chart (the server answered 400: attachments not allowed)')).toBeInTheDocument()
  })

  it('reads a run saved before charts existed', () => {
    const old = run()
    delete (old as Partial<WatchRun>).charts
    delete (old as Partial<WatchRun>).notify_notes
    render(<WatchRunCard run={old} {...props} />)
    expect(screen.queryByTestId('watch-chart')).toBeNull()
  })
})
