import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, within } from '@testing-library/react'

import { WatchesIndex, watchesFooter, type WatchesIndexProps } from '@/components/watches/watches-index'
import type { WatchRecord } from '@/lib/watches'

function watch(overrides: Partial<WatchRecord>): WatchRecord {
  return {
    id: 'w1',
    name: 'Morning report',
    kind: 'scheduled',
    schedule: '0 6 * * *',
    sections: ['power'],
    thresholds: { alarms_raised: 1, instrument_issues: 1, tank_change_points: 10, pressure_fall_hpa: 3 },
    look_out_for: '',
    enabled: true,
    updated_at: '2026-10-29T00:00:00Z',
    schedule_sentence: 'Every day at 06:00 boat time',
    last_run: null,
    has_finding: false,
    ...overrides,
  }
}

function renderIndex(props: Partial<WatchesIndexProps> = {}) {
  const onOpen = vi.fn()
  const onNew = vi.fn()
  render(
    <WatchesIndex
      watches={[]}
      max={20}
      cost24h={{ usd: 0, unknown_runs: 0 }}
      storageBytes={2048}
      problem={null}
      loading={false}
      error={null}
      canWrite
      onRetry={() => {}}
      onOpen={onOpen}
      onNew={onNew}
      {...props}
    />,
  )
  return { onOpen, onNew }
}

describe('WatchesIndex', () => {
  it('starts empty with three templates that open the new-watch form pre-filled', () => {
    const { onNew } = renderIndex()
    expect(screen.getByText('No watches yet')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Morning report\s*Daily 06:00/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Overnight check\s*Hourly 22:00/ })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /At anchor\s*Every 2 hours/ }))
    expect(onNew).toHaveBeenCalledWith(expect.objectContaining({ id: 'anchor' }))
  })

  it('offers New watch with no template', () => {
    const { onNew } = renderIndex()
    fireEvent.click(screen.getByRole('button', { name: 'New watch' }))
    expect(onNew).toHaveBeenCalledWith(null)
  })

  it('lists the name, the schedule sentence, the last run and whether it found something', () => {
    renderIndex({
      watches: [
        watch({
          id: 'a',
          name: 'Overnight check',
          schedule_sentence: 'Every hour from 22:00 to 06:00',
          has_finding: true,
          last_run: { id: 'r', started_at: new Date(Date.now() - 2 * 3600_000).toISOString(), outcome: 'ran', finding_count: 2, review_problem: null },
        }),
        watch({ id: 'b', name: 'Quiet', last_run: { id: 'r2', started_at: new Date().toISOString(), outcome: 'ran', finding_count: 0, review_problem: null } }),
      ],
    })
    const row = screen.getByText('Overnight check').closest('tr')!
    expect(within(row).getByText('Every hour from 22:00 to 06:00')).toBeInTheDocument()
    expect(within(row).getByText('2 h ago')).toBeInTheDocument()
    expect(within(row).getByText('Found something')).toBeInTheDocument()
    const quiet = screen.getByText('Quiet').closest('tr')!
    expect(within(quiet).getByText('Nothing found')).toBeInTheDocument()
  })

  it('labels a one-off while it runs and shows a watch that never ran as not yet', () => {
    renderIndex({
      watches: [
        watch({ id: 'o', name: 'Engine load', kind: 'one_off', schedule: undefined, schedule_sentence: 'One-off · 10 minutes', running: true }),
        watch({ id: 'n', name: 'Fresh' }),
      ],
    })
    const oneOff = screen.getByText('Engine load').closest('tr')!
    expect(within(oneOff).getByText('One-off · 10 minutes')).toBeInTheDocument()
    expect(within(oneOff).getByText('Running')).toBeInTheDocument()
    expect(within(screen.getByText('Fresh').closest('tr')!).getByText('Not yet')).toBeInTheDocument()
  })

  it('opens a watch from its row', () => {
    const { onOpen } = renderIndex({ watches: [watch({ id: 'w9', name: 'Open me' })] })
    fireEvent.click(screen.getByText('Open me'))
    expect(onOpen).toHaveBeenCalledWith('w9')
  })

  it('shows why the scheduler stopped, in the server’s words, as an alert', () => {
    renderIndex({ problem: 'Watches stopped: the run history could not be saved (disk full). Restart Helmcentral after fixing it.' })
    expect(screen.getByRole('alert')).toHaveTextContent('Watches stopped: the run history could not be saved (disk full).')
  })

  it('does not offer New watch or templates to a read-only viewer', () => {
    renderIndex({ canWrite: false })
    expect(screen.queryByRole('button', { name: 'New watch' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Morning report/ })).not.toBeInTheDocument()
  })
})

describe('WatchesIndex limit', () => {
  it('assumes no limit until the server has said what it is', () => {
    renderIndex({ max: null, watches: [watch({ id: 'a' })] })
    expect(screen.getByRole('button', { name: 'New watch' })).toBeEnabled()
    expect(screen.queryByText(/limit of/)).not.toBeInTheDocument()
  })

  it('stops offering New watch at the limit the server gave', () => {
    renderIndex({ max: 1, watches: [watch({ id: 'a' })] })
    expect(screen.getByRole('button', { name: 'New watch' })).toBeDisabled()
    expect(screen.getByText('You have reached the limit of 1 scheduled watches.')).toBeInTheDocument()
  })
})

describe('watchesFooter', () => {
  it('gives the space used and the cost of the last day', () => {
    expect(watchesFooter(2048, { usd: 0.1234, unknown_runs: 0 })).toEqual([
      'Watch history uses 2.0 KB.',
      'Watches cost $0.12 in the last 24 hours.',
    ])
  })

  it('says nothing about cost when Mate reviews have cost nothing', () => {
    expect(watchesFooter(10, { usd: 0, unknown_runs: 0 })).toEqual(['Watch history uses 10 B.'])
  })

  it('says the cost is not known rather than guessing', () => {
    expect(watchesFooter(10, { usd: 0, unknown_runs: 2 })[1]).toBe('Watch cost in the last 24 hours: cost not known.')
    expect(watchesFooter(10, { usd: 0.5, unknown_runs: 1 })[1]).toContain('Cost not known for 1 run.')
  })
})
