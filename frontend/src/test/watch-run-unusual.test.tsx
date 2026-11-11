import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'

import { WatchRunCard } from '@/components/watches/watch-run-list'
import { findingsToLookAt, UnusualSavedError, type WatchRun, type WatchSectionsReport, type WatchUnusualStretch } from '@/lib/watches'

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

function run(rep: WatchSectionsReport | null, answers?: string[]): WatchRun {
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
    unusual_answers: answers,
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

/** The stretch behind a line, which the operator can answer. */
const STRETCH: WatchUnusualStretch = {
  stretch_id: 'engines:0@2026-11-10T02:10:00Z',
  subsystem: 'engines',
  subject: '0',
  engine: 'Port engine',
  from: '2026-11-10T02:10:00Z',
  to: '2026-11-10T02:40:00Z',
}

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

  describe('answers', () => {
    const withStretch = (answers?: string[]) => run(report({ unusual: { lines: [LINE], stretches: [STRETCH], judged_minutes: 300 } }), answers)

    it('shows Real, Normal and Don\'t know on a stretch, named for its engine', () => {
      render(<WatchRunCard run={withStretch()} onAnswerUnusual={vi.fn()} />)
      const group = screen.getByRole('group', { name: 'Your answer for the Port engine stretch' })
      expect(within(group).getByRole('button', { name: 'Real: Port engine stretch' })).toHaveTextContent('Real')
      expect(within(group).getByRole('button', { name: 'Normal: Port engine stretch' })).toHaveTextContent('Normal')
      expect(within(group).getByRole('button', { name: "Don't know: Port engine stretch" })).toHaveTextContent("Don't know")
      expect(screen.queryByTestId('unusual-answer')).toBeNull()
    })

    it('sends the answer with the stretch, and shows what the server then says', async () => {
      const onAnswerUnusual = vi.fn().mockResolvedValue(undefined)
      const { rerender } = render(<WatchRunCard run={withStretch()} onAnswerUnusual={onAnswerUnusual} />)
      fireEvent.click(screen.getByRole('button', { name: 'Real: Port engine stretch' }))
      await waitFor(() => expect(onAnswerUnusual).toHaveBeenCalledWith(expect.objectContaining({ id: 'r1', watch_id: 'w1' }), 0, 'real'))
      rerender(<WatchRunCard run={withStretch(['real'])} onAnswerUnusual={onAnswerUnusual} />)
      expect(screen.getByTestId('unusual-answer')).toHaveTextContent('You said: real')
      expect(screen.getByRole('button', { name: 'Real: Port engine stretch' })).toHaveAttribute('aria-pressed', 'true')

      fireEvent.click(screen.getByRole('button', { name: "Don't know: Port engine stretch" }))
      await waitFor(() => expect(onAnswerUnusual).toHaveBeenLastCalledWith(expect.objectContaining({ id: 'r1' }), 0, 'unknown'))
      rerender(<WatchRunCard run={withStretch(['unknown'])} onAnswerUnusual={onAnswerUnusual} />)
      expect(screen.getByTestId('unusual-answer')).toHaveTextContent("You said: don't know")
    })

    it('does not send an answer again, or clear it, when the chosen button is pressed', async () => {
      const onAnswerUnusual = vi.fn().mockResolvedValue(undefined)
      render(<WatchRunCard run={withStretch(['normal'])} onAnswerUnusual={onAnswerUnusual} />)
      expect(screen.getByTestId('unusual-answer')).toHaveTextContent('You said: normal')
      fireEvent.click(screen.getByRole('button', { name: 'Normal: Port engine stretch' }))
      await Promise.resolve()
      expect(onAnswerUnusual).not.toHaveBeenCalled()
    })

    it('says so when the answer could not be saved, and keeps what was said before', async () => {
      const onAnswerUnusual = vi.fn().mockRejectedValue(new Error('the answers about unusual stretches could not be read'))
      render(<WatchRunCard run={withStretch(['real'])} onAnswerUnusual={onAnswerUnusual} />)
      fireEvent.click(screen.getByRole('button', { name: 'Normal: Port engine stretch' }))
      expect(await screen.findByRole('alert')).toHaveTextContent('the answers about unusual stretches could not be read')
      expect(screen.getByTestId('unusual-answer')).toHaveTextContent('You said: real')
    })

    it('says the answer was saved, not lost, when only the page could not refresh', async () => {
      const onAnswerUnusual = vi.fn().mockRejectedValue(new UnusualSavedError('offline'))
      render(<WatchRunCard run={withStretch(['real'])} onAnswerUnusual={onAnswerUnusual} />)
      fireEvent.click(screen.getByRole('button', { name: 'Normal: Port engine stretch' }))
      expect(await screen.findByRole('status')).toHaveTextContent('Saved. The page could not refresh: offline')
      expect(screen.queryByRole('alert')).toBeNull()
      expect(screen.getByTestId('unusual-answer')).toHaveTextContent('You said: normal')
      expect(screen.getByRole('button', { name: 'Normal: Port engine stretch' })).toHaveAttribute('aria-pressed', 'true')
    })

    it('shows the answer but no buttons where the page cannot write', () => {
      render(<WatchRunCard run={withStretch(['normal'])} />)
      expect(screen.getByTestId('unusual-answer')).toHaveTextContent('You said: normal')
      expect(screen.queryByRole('button', { name: /stretch/ })).toBeNull()
    })

    it('says why, in place of the buttons, when the answers could not be read', () => {
      const r = { ...withStretch(), unusual_answers_problem: 'Your answers could not be read: parsing answers.json' }
      render(<WatchRunCard run={r} onAnswerUnusual={vi.fn()} />)
      expect(screen.getByText(LINE)).toBeInTheDocument()
      expect(screen.getByTestId('unusual-answers-problem')).toHaveTextContent('Your answers could not be read: parsing answers.json')
      expect(screen.queryByRole('group')).toBeNull()
    })

    it('shows no buttons on a line from an old report', () => {
      render(<WatchRunCard run={run(report({ unusual: { lines: [LINE], judged_minutes: 300 } }))} onAnswerUnusual={vi.fn()} />)
      expect(screen.getByText(LINE)).toBeInTheDocument()
      expect(screen.queryByRole('group')).toBeNull()
    })
  })
})
