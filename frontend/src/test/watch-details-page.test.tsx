// The device reading the page is a long way from the boat: nothing on screen may be on its clock.
process.env.TZ = 'Pacific/Honolulu'

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'

import { SaveBarSlot } from '@/components/patterns/save-bar-slot'
import { WatchDetailsPage, type WatchDetailsPageProps } from '@/components/watches/watch-details-page'
import { invalidateWatches } from '@/hooks/use-watches'
import { resetQueryStore } from '@/lib/api-query'
import { WATCH_TEMPLATES, type WatchDetail, type WatchRecord, type WatchRun, type WatchSchedulePreview } from '@/lib/watches'

function record(overrides: Partial<WatchRecord> = {}): WatchRecord {
  return {
    id: 'w1',
    name: 'Overnight check',
    kind: 'scheduled',
    schedule: '0 22-23,0-6 * * *',
    sections: ['power', 'alarms'],
    thresholds: { alarms_raised: 2, instrument_issues: 1, tank_change_points: 10, pressure_fall_hpa: 3 },
    look_out_for: 'the fridge',
    enabled: true,
    updated_at: '2026-10-29T00:00:00Z',
    schedule_sentence: 'Every hour from 22:00 to 06:00',
    last_run: null,
    has_finding: false,
    ...overrides,
  }
}

function run(id: string, overrides: Partial<WatchRun> = {}): WatchRun {
  return {
    id,
    watch_id: 'w1',
    started_at: '2026-10-28T17:00:00Z',
    finished_at: '2026-10-28T17:00:30Z',
    boat_time: 'Thu 29 Oct 06:00 UTC+13',
    timezone: 'UTC+13',
    outcome: 'ran',
    report_kind: 'sections',
    report: {
      kind: 'sections',
      generated_at: '2026-10-29T06:00:00Z',
      boat_time: 'Thu 29 Oct 06:00 UTC+13',
      since: 'Wed 28 Oct 22:00 UTC+13',
      sections: [
        { name: 'power', title: 'Power', lines: ['House bank 84% at 13.1 V'] },
        { name: 'weather', title: 'Weather', unavailable: 'pressure history still filling' },
      ],
    },
    findings: [
      { section: 'alarms', rule: 'alarms_raised', text: '2 alarms raised', by: 'rules' },
      { section: 'power', rule: 'mate', text: 'Fridge draw is climbing', by: 'mate' },
    ],
    review: 'The **fridge** draw rose overnight.',
    review_problem: null,
    cost_usd: 0.0123,
    cost_known: true,
    ...overrides,
  }
}

const PREVIEW_OK: WatchSchedulePreview = {
  valid: true,
  sentence: 'Every hour from 22:00 to 06:00',
  boat_time_known: true,
  next: [
    { at: '2026-10-29T09:00:00Z', local: 'Thu 29 Oct 22:00 NZDT' },
    { at: '2026-10-29T10:00:00Z', local: 'Thu 29 Oct 23:00 NZDT' },
    { at: '2026-10-29T11:00:00Z', local: 'Fri 30 Oct 00:00 NZDT' },
  ],
}

interface Calls { method: string; url: string; body: unknown }
let calls: Calls[]
let detail: WatchDetail
let preview: (schedule: string) => WatchSchedulePreview
let olderRuns: WatchRun[]

beforeEach(() => {
  calls = []
  detail = { watch: record(), runs: [run('r1')], has_older: false }
  preview = () => PREVIEW_OK
  olderRuns = []
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    const method = init?.method ?? 'GET'
    const body = init?.body ? JSON.parse(String(init.body)) : undefined
    calls.push({ method, url: String(url), body })
    const ok = (data: unknown, status = 200) => ({ ok: true, status, json: async () => data })
    if (String(url).includes('/schedule-preview')) return ok(preview((body as { schedule: string }).schedule))
    if (method === 'GET' && String(url).includes('before=')) return ok({ watch: detail.watch, runs: olderRuns, has_older: false })
    if (method === 'GET') return ok(detail)
    if (method === 'PATCH') return ok({ ...detail.watch, ...(body as object) })
    if (method === 'POST' && String(url).endsWith('/api/watches')) return ok({ ...record(), id: 'new-1' }, 201)
    if (method === 'POST' && String(url).endsWith('/run')) return ok(run('r-new'))
    if (method === 'DELETE') return { ok: true, status: 204, json: async () => undefined }
    return { ok: false, status: 404, json: async () => ({ error: 'not found' }) }
  }))
})

afterEach(() => {
  vi.unstubAllGlobals()
  resetQueryStore()
})

function renderPage(props: Partial<WatchDetailsPageProps> = {}) {
  const handlers = { onOpenIndex: vi.fn(), onCreated: vi.fn(), onDeleted: vi.fn(), onAskMate: vi.fn() }
  render(
    <div className="relative">
      <SaveBarSlot />
      <WatchDetailsPage mode={{ kind: 'existing', id: 'w1' }} canWrite {...handlers} {...props} />
    </div>,
  )
  return handlers
}

const sent = (method: string) => calls.filter((c) => c.method === method && !c.url.includes('schedule-preview'))

describe('WatchDetailsPage settings', () => {
  it('shows the watch with the server’s schedule sentence and next three run times', async () => {
    renderPage()
    expect(await screen.findByDisplayValue('Overnight check')).toBeInTheDocument()
    const previewBox = await screen.findByTestId('watch-schedule-preview')
    await waitFor(() => expect(within(previewBox).getByText('Every hour from 22:00 to 06:00')).toBeInTheDocument())
    expect(within(previewBox).getByText('Thu 29 Oct 22:00 NZDT')).toBeInTheDocument()
    expect(within(previewBox).getAllByRole('listitem')).toHaveLength(3)
    expect(screen.getByRole('checkbox', { name: 'Power' })).toBeChecked()
    expect(screen.getByRole('checkbox', { name: 'Tanks' })).not.toBeChecked()
    expect(screen.getByLabelText('Alarms raised that count')).toHaveValue(2)
    expect(screen.getByText(/the watermaker runs overnight; tell me if the fridge draw climbs/)).toBeInTheDocument()
  })

  it('reads the stored cron back into its preset and Custom when no preset writes it', async () => {
    detail = { ...detail, watch: record({ schedule: '0 */5 * * *' }) }
    renderPage()
    expect(await screen.findByLabelText('Cron')).toHaveValue('0 */5 * * *')
  })

  it('saves only what changed and nothing is sent before Save', async () => {
    renderPage()
    const name = await screen.findByDisplayValue('Overnight check')
    expect(screen.queryByRole('button', { name: 'Save' })).not.toBeInTheDocument()
    fireEvent.change(name, { target: { value: 'Night watch' } })
    await waitFor(() => expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled(), { timeout: 3000 })
    expect(sent('PATCH')).toHaveLength(0)
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(sent('PATCH')).toHaveLength(1))
    expect(sent('PATCH')[0].body).toMatchObject({
      name: 'Night watch',
      schedule: '0 22-23,0-6 * * *',
      sections: ['power', 'alarms'],
      thresholds: { alarms_raised: 2, instrument_issues: 1, tank_change_points: 10, pressure_fall_hpa: 3 },
    })
  })

  it('blocks Save and says why when the server refuses the schedule', async () => {
    preview = () => ({ valid: false, error: '"* * * * *" runs more often than every 15 minutes', boat_time_known: true, next: [] })
    renderPage()
    fireEvent.change(await screen.findByDisplayValue('Overnight check'), { target: { value: 'Renamed' } })
    expect(await screen.findByText(/runs more often than every 15 minutes/, undefined, { timeout: 3000 })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
  })

  it('shows that the boat time is unknown rather than any UTC time', async () => {
    preview = () => ({
      valid: true,
      sentence: 'Every day at 06:00 boat time',
      boat_time_known: false,
      next_problem: "No boat position, so the boat's time is not known",
      next: [],
    })
    renderPage()
    expect(await screen.findByText("No boat position, so the boat's time is not known", undefined, { timeout: 3000 })).toBeInTheDocument()
    expect(screen.getByText('Every day at 06:00 boat time')).toBeInTheDocument()
  })

  it('needs at least one section', async () => {
    renderPage()
    await screen.findByDisplayValue('Overnight check')
    fireEvent.click(screen.getByRole('checkbox', { name: 'Power' }))
    fireEvent.click(screen.getByRole('checkbox', { name: 'Alarms' }))
    expect(screen.getByText('Pick at least one section.')).toBeInTheDocument()
    await waitFor(() => expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled())
  })

  it('validates the thresholds the way the server does', async () => {
    renderPage()
    await screen.findByDisplayValue('Overnight check')
    fireEvent.change(screen.getByLabelText('Alarms raised that count'), { target: { value: '0' } })
    expect(screen.getByText('A whole number from 1 to 100.')).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Alarms raised that count'), { target: { value: '3' } })
    fireEvent.click(screen.getByRole('checkbox', { name: 'Weather' }))
    fireEvent.change(screen.getByLabelText('Pressure fall that counts (hPa)'), { target: { value: '31' } })
    expect(screen.getByText('From 0.5 to 30 hPa.')).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Pressure fall that counts (hPa)'), { target: { value: '0.5' } })
    expect(screen.queryByText('From 0.5 to 30 hPa.')).not.toBeInTheDocument()
  })

  it('counts the look-out text against its limit', async () => {
    renderPage()
    await screen.findByDisplayValue('Overnight check')
    fireEvent.change(screen.getByLabelText('What to look out for'), { target: { value: 'x'.repeat(501) } })
    expect(screen.getByText('Keep this to 500 characters.')).toBeInTheDocument()
  })

  it('writes the cron of a preset chosen from the picker', async () => {
    renderPage()
    await screen.findByDisplayValue('Overnight check')
    fireEvent.click(screen.getByRole('combobox', { name: 'Schedule' }))
    const option = await screen.findByRole('option', { name: 'Every hour' })
    fireEvent.pointerDown(option)
    fireEvent.pointerUp(option)
    fireEvent.click(option)
    await waitFor(() => expect(calls.some((c) => c.url.includes('schedule-preview') && (c.body as { schedule: string }).schedule === '0 * * * *')).toBe(true), { timeout: 3000 })
  })
})

describe('WatchDetailsPage new watch', () => {
  it('opens pre-filled from a template and saves nothing until Create', async () => {
    const onCreated = vi.fn()
    renderPage({ mode: { kind: 'new', template: WATCH_TEMPLATES[1] }, onCreated })
    expect(await screen.findByDisplayValue('Overnight check')).toBeInTheDocument()
    expect(sent('POST')).toHaveLength(0)
    await waitFor(() => expect(screen.getByRole('button', { name: 'Create watch' })).toBeEnabled(), { timeout: 3000 })
    fireEvent.click(screen.getByRole('button', { name: 'Create watch' }))
    await waitFor(() => expect(onCreated).toHaveBeenCalledWith('new-1'))
    expect(sent('POST')[0].body).toMatchObject({ name: 'Overnight check', schedule: '0 22-23,0-6 * * *' })
  })

  it('cannot be created without a name', async () => {
    renderPage({ mode: { kind: 'new', template: null } })
    await screen.findByRole('heading', { name: 'New watch' })
    await waitFor(() => expect(screen.getByRole('button', { name: 'Create watch' })).toBeDisabled())
  })
})

describe('WatchDetailsPage run history', () => {
  it('renders a run with findings, the Mate badge, section figures, unavailable reasons, review and cost', async () => {
    renderPage()
    const card = await screen.findByTestId('watch-run')
    expect(within(card).getByText('Thu 29 Oct 06:00 UTC+13')).toBeInTheDocument()
    expect(within(card).getByText('2 alarms raised')).toBeInTheDocument()
    const mateFinding = within(card).getByText('Fridge draw is climbing').closest('li')!
    expect(within(mateFinding).getByText('Mate')).toBeInTheDocument()
    expect(within(card).getByText('House bank 84% at 13.1 V')).toBeInTheDocument()
    expect(within(card).getByText('Unavailable: pressure history still filling')).toBeInTheDocument()
    expect(await within(card).findByText('fridge', { selector: 'strong' })).toBeInTheDocument()
    expect(within(card).getByText('$0.01')).toBeInTheDocument()
  })

  it('shows the review problem as a muted line and "cost not known" when it is', async () => {
    detail = {
      ...detail,
      runs: [run('r1', { review: null, review_problem: 'No model is chosen for Mate.', cost_usd: null, cost_known: false, findings: [] })],
    }
    renderPage()
    const card = await screen.findByTestId('watch-run')
    expect(within(card).getByText('No model is chosen for Mate.')).toBeInTheDocument()
    expect(within(card).getByText('cost not known')).toBeInTheDocument()
  })

  it('shows a skipped run with its reason and no review', async () => {
    detail = {
      ...detail,
      runs: [run('r1', { outcome: 'skipped', skip_reason: "No boat position, so the boat's time is not known", report: undefined, findings: [], review: null, review_problem: null })],
    }
    renderPage()
    const card = await screen.findByTestId('watch-run')
    expect(within(card).getByText('Skipped')).toBeInTheDocument()
    expect(within(card).getByText("No boat position, so the boat's time is not known")).toBeInTheDocument()
    expect(within(card).queryByTestId('watch-run-review')).not.toBeInTheDocument()
  })

  it('renders a one-off run’s samples summary', async () => {
    detail = {
      watch: record({ kind: 'one_off', name: 'Engine load', schedule: undefined, duration_minutes: 10, schedule_sentence: 'One-off · 10 minutes', started_at: '2026-10-28T16:50:00Z', ends_at: '2026-10-28T17:00:00Z', started_boat_time: 'Thu 29 Oct 05:50 UTC+13', ends_boat_time: 'Thu 29 Oct 06:00 UTC+13' }),
      runs: [
        run('r1', {
          report_kind: 'samples',
          review: null,
          review_problem: null,
          findings: [],
          report: {
            reason: 'Engine load',
            timezone: 'NZDT',
            started: 'Thu 29 Oct 05:50:00',
            ended: 'Thu 29 Oct 06:00:00',
            minutes: 10,
            series: [{ label: 'Port engine load', units: 'ratio', samples: 600, min: 0.2, mean: 0.5, max: 0.8, last: 0.6, excursion_count: 1 }],
            note: 'Values are SignalK units',
          },
        }),
      ],
      has_older: false,
    }
    renderPage()
    const report = await screen.findByTestId('watch-samples-report')
    expect(within(report).getByText('Port engine load')).toBeInTheDocument()
    expect(within(report).getByText(/0\.2 to 0\.8 ratio · mean 0\.5 · last 0\.6 · 1 excursion/)).toBeInTheDocument()
    // A one-off watch is read-only: nothing to save, nothing to run again.
    expect(screen.getByText('One-off watch')).toBeInTheDocument()
    expect(screen.getByText('Thu 29 Oct 05:50 UTC+13')).toBeInTheDocument()
    expect(screen.getByText('Ended Thu 29 Oct 06:00 UTC+13')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Run now' })).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Cron')).not.toBeInTheDocument()
  })

  it('pages older runs in with Show older', async () => {
    detail = { ...detail, has_older: true }
    olderRuns = [run('r0', { started_at: '2026-10-28T06:00:00Z', report: undefined, report_kind: 'sections', findings: [], review: 'Older review', cost_known: true, cost_usd: 0 })]
    renderPage()
    await screen.findByTestId('watch-run')
    fireEvent.click(screen.getByRole('button', { name: 'Show older' }))
    await waitFor(() => expect(screen.getAllByTestId('watch-run')).toHaveLength(2))
    expect(calls.some((c) => c.url.includes('limit=20&before=2026-10-28T17%3A00%3A00Z,r1'))).toBe(true)
    expect(screen.queryByRole('button', { name: 'Show older' })).not.toBeInTheDocument()
  })

  it('opens Mate about a run with the question seeded and the run attached', async () => {
    const { onAskMate } = renderPage()
    await screen.findByTestId('watch-run')
    fireEvent.click(screen.getByRole('button', { name: 'Ask Mate about this run' }))
    expect(onAskMate).toHaveBeenCalledWith('What do you make of the Overnight check report from Thu 29 Oct 06:00 UTC+13?', {
      newConversation: true,
      watchRun: { id: 'r1', name: 'Overnight check' },
    })
  })

  it('deletes a run only after confirming', async () => {
    renderPage()
    await screen.findByTestId('watch-run')
    fireEvent.click(screen.getByRole('button', { name: 'Delete run' }))
    expect(sent('DELETE')).toHaveLength(0)
    const dialog = await screen.findByRole('alertdialog')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Delete run' }))
    await waitFor(() => expect(sent('DELETE')).toHaveLength(1))
    expect(sent('DELETE')[0].url).toContain('/api/watches/w1/runs/r1')
  })

  it('runs now, disabled while it runs, and reads the history again', async () => {
    renderPage()
    await screen.findByTestId('watch-run')
    const gets = () => calls.filter((c) => c.method === 'GET' && c.url.includes('/api/watches/w1')).length
    const before = gets()
    fireEvent.click(screen.getByRole('button', { name: 'Run now' }))
    expect(screen.getByRole('button', { name: 'Running...' })).toBeDisabled()
    await waitFor(() => expect(screen.getByRole('button', { name: 'Run now' })).toBeEnabled())
    expect(sent('POST').some((c) => c.url.endsWith('/api/watches/w1/run'))).toBe(true)
    expect(gets()).toBeGreaterThan(before)
  })

  it('deletes the watch after confirming and tells the page', async () => {
    const { onDeleted } = renderPage()
    await screen.findByTestId('watch-run')
    fireEvent.click(screen.getByRole('button', { name: 'More actions' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Delete watch' }))
    const dialog = await screen.findByRole('alertdialog')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Delete watch' }))
    await waitFor(() => expect(onDeleted).toHaveBeenCalled())
    expect(sent('DELETE').some((c) => c.url.endsWith('/api/watches/w1'))).toBe(true)
  })

  it('hides every write for a read-only viewer', async () => {
    renderPage({ canWrite: false })
    await screen.findByTestId('watch-run')
    expect(screen.queryByRole('button', { name: 'Run now' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Delete run' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'More actions' })).not.toBeInTheDocument()
    expect(screen.getByRole('checkbox', { name: 'Power' })).toHaveAttribute('aria-disabled', 'true')
  })
})

describe('WatchDetailsPage boat time', () => {
  it('shows a run on the boat clock the server gave, never the device\u2019s', async () => {
    renderPage()
    const card = await screen.findByTestId('watch-run')
    expect(within(card).getByRole('heading', { name: 'Thu 29 Oct 06:00 UTC+13' })).toBeInTheDocument()
    // 17:00 UTC is Wed 28 Oct 07:00 in Honolulu, the device zone of this test.
    expect(screen.queryByText(/Wed 28 Oct 07:00/)).not.toBeInTheDocument()
  })

  it('says a run with no boat position is on UTC and not boat time', async () => {
    detail = { ...detail, runs: [run('r1', { boat_time: 'Thu 29 Oct 17:00 UTC (boat time not known)', timezone: '' })] }
    renderPage()
    expect(await screen.findByRole('heading', { name: 'Thu 29 Oct 17:00 UTC (boat time not known)' })).toBeInTheDocument()
  })

  it('asks Mate with the same label the card shows', async () => {
    const { onAskMate } = renderPage()
    await screen.findByTestId('watch-run')
    fireEvent.click(screen.getByRole('button', { name: 'Ask Mate about this run' }))
    expect(onAskMate.mock.calls[0][0]).toBe('What do you make of the Overnight check report from Thu 29 Oct 06:00 UTC+13?')
  })
})

describe('WatchDetailsPage read-only viewer', () => {
  it('still sees what the schedule means and when it next runs', async () => {
    renderPage({ canWrite: false })
    const box = await screen.findByTestId('watch-schedule-preview')
    await waitFor(() => expect(within(box).getByText('Thu 29 Oct 22:00 NZDT')).toBeInTheDocument(), { timeout: 3000 })
    expect(within(box).getByText('Every hour from 22:00 to 06:00')).toBeInTheDocument()
  })
})

describe('WatchDetailsPage changed elsewhere', () => {
  it('keeps unsaved edits when the watch changes on the server, and says so', async () => {
    renderPage()
    const name = await screen.findByDisplayValue('Overnight check')
    fireEvent.change(name, { target: { value: 'My edit' } })
    detail = { ...detail, watch: record({ name: 'Renamed by Mate' }) }
    await act(async () => { await invalidateWatches() })
    expect(screen.getByDisplayValue('My edit')).toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('This watch was changed elsewhere. Saving will overwrite those changes.')
  })

  it('takes the server\u2019s version when there are no edits', async () => {
    renderPage()
    await screen.findByDisplayValue('Overnight check')
    detail = { ...detail, watch: record({ name: 'Renamed by Mate' }) }
    await act(async () => { await invalidateWatches() })
    expect(await screen.findByDisplayValue('Renamed by Mate')).toBeInTheDocument()
    expect(screen.queryByText(/changed elsewhere/)).not.toBeInTheDocument()
  })

  it('does not call its own save a change made elsewhere', async () => {
    renderPage()
    fireEvent.change(await screen.findByDisplayValue('Overnight check'), { target: { value: 'Night watch' } })
    await waitFor(() => expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled(), { timeout: 3000 })
    detail = { ...detail, watch: record({ name: 'Night watch' }) }
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Save' })).not.toBeInTheDocument())
    expect(screen.queryByText(/changed elsewhere/)).not.toBeInTheDocument()
    expect(screen.getByDisplayValue('Night watch')).toBeInTheDocument()
  })
})
