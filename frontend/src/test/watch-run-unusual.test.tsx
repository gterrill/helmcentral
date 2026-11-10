import { describe, expect, it } from 'vitest'
import { render, screen, within } from '@testing-library/react'

import { WatchRunCard } from '@/components/watches/watch-run-list'
import { findingsToLookAt, type WatchRun, type WatchSectionsReport } from '@/lib/watches'

// ADR 0276: the report's Unusual block is a learned normal's suggestion. It is
// a quiet block under Needs a look, never counted as an issue.

function report(over: Partial<WatchSectionsReport> = {}): WatchSectionsReport {
  return {
    kind: 'sections',
    generated_at: '2026-11-10T09:00:00Z',
    boat_time: 'Tue 10 Nov 7.00 PM AEST',
    since: 'Mon 9 Nov 7.00 PM AEST',
    sections: [
      { name: 'instruments', title: 'Instruments', lines: ['Port engine rpm: 1,800'], not_checked: ['Exhaust temperature: no reading'] },
    ],
    ...over,
  }
}

function run(rep: WatchSectionsReport | null): WatchRun {
  return {
    id: 'r1',
    watch_id: 'w1',
    started_at: '2026-11-10T09:00:00Z',
    finished_at: '2026-11-10T09:00:30Z',
    boat_time: 'Tue 10 Nov 7.00 PM AEST',
    timezone: 'AEST',
    outcome: 'ran',
    report_kind: 'sections',
    report: rep,
    findings: [{ section: 'power', rule: 'overnight_draw', text: 'Drew a lot overnight.', by: 'rules' }],
    notify_notes: [],
    review: 'Fine.',
    review_problem: null,
    cost_usd: 0,
    cost_known: true,
    notified: [],
  } as WatchRun
}

const LINE = 'Port engine oil pressure ran low from 2.10 AM to 2.40 AM: 280 kPa against a usual 340 kPa at 1,800 to 2,000 rpm.'

describe('Unusual block in a watch run', () => {
  it('shows the lines under a quiet Unusual label', () => {
    render(<WatchRunCard run={run(report({ unusual: { lines: [LINE], judged_minutes: 300 } }))} />)
    const block = screen.getByTestId('watch-run-unusual')
    expect(within(block).getByText('Unusual')).toBeInTheDocument()
    expect(within(block).getByText(LINE)).toBeInTheDocument()
    expect(within(block).getAllByRole('listitem')).toHaveLength(1)
  })

  it('comes after Needs a look and before Not checked', () => {
    render(<WatchRunCard run={run(report({ unusual: { lines: [LINE], judged_minutes: 300 } }))} />)
    const findings = screen.getByText('Drew a lot overnight.')
    const unusual = screen.getByTestId('watch-run-unusual')
    const notChecked = screen.getByTestId('watch-run-not-checked')
    expect(findings.compareDocumentPosition(unusual) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(unusual.compareDocumentPosition(notChecked) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  it('shows a note on its own when nothing stood out', () => {
    render(<WatchRunCard run={run(report({ unusual: { note: 'No learned normal: InfluxDB is not set up.', judged_minutes: 0 } }))} />)
    const block = screen.getByTestId('watch-run-unusual')
    expect(within(block).getByText('No learned normal: InfluxDB is not set up.')).toBeInTheDocument()
    expect(within(block).queryByRole('listitem')).toBeNull()
  })

  it('shows the note beneath the lines', () => {
    render(<WatchRunCard run={run(report({ unusual: { lines: [LINE], note: '1 more unusual stretch is not shown.', judged_minutes: 300 } }))} />)
    const block = screen.getByTestId('watch-run-unusual')
    const line = within(block).getByText(LINE)
    const note = within(block).getByText('1 more unusual stretch is not shown.')
    expect(line.compareDocumentPosition(note) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  it('is hidden when the report has no block, or an empty one', () => {
    const { unmount } = render(<WatchRunCard run={run(report())} />)
    expect(screen.queryByTestId('watch-run-unusual')).toBeNull()
    unmount()
    render(<WatchRunCard run={run(report({ unusual: { judged_minutes: 300 } }))} />)
    expect(screen.queryByTestId('watch-run-unusual')).toBeNull()
  })

  it('is not counted among the things to look at', () => {
    const r = run(report({ unusual: { lines: [LINE, LINE], judged_minutes: 300 } }))
    expect(findingsToLookAt(r.findings)).toBe(1)
    render(<WatchRunCard run={r} />)
    expect(screen.getByText('1 to look at')).toBeInTheDocument()
  })
})
