// The device reading the page is a long way from the boat: nothing on screen may be on its clock.
process.env.TZ = 'Pacific/Honolulu'

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'

import { SaveBarSlot } from '@/components/patterns/save-bar-slot'
import { WatchDetailsPage, type WatchDetailsPageProps } from '@/components/watches/watch-details-page'
import { WatchRunList } from '@/components/watches/watch-run-list'
import { AppRouterContext } from '@/hooks/use-app-location'
import { invalidateWatches } from '@/hooks/use-watches'
import { resetQueryStore } from '@/lib/api-query'
import { createAppRouter, memoryHistory } from '@/lib/app-router'
import { clearPendingSetupFocus, peekPendingSetupFocus } from '@/lib/setup-status'
import { runTimeLabel, WATCH_TEMPLATES, type WatchDetail, type WatchFinding, type WatchRecord, type WatchReportSection, type WatchRun, type WatchSchedulePreview } from '@/lib/watches'

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
    notify: ['ntfy'],
    send: 'findings',
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
    notified: [],
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
let channels: { offered: string[]; enabled: string[] }
let channelsGate: Promise<void> | null
let linkedRuns: Record<string, WatchRun>

beforeEach(() => {
  calls = []
  detail = { watch: record(), runs: [run('r1')], has_older: false }
  preview = () => PREVIEW_OK
  olderRuns = []
  channels = { offered: ['ntfy', 'smtp', 'webhook', 'webpush'], enabled: ['ntfy', 'webpush'] }
  channelsGate = null
  linkedRuns = {}
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    const method = init?.method ?? 'GET'
    const body = init?.body ? JSON.parse(String(init.body)) : undefined
    calls.push({ method, url: String(url), body })
    const ok = (data: unknown, status = 200) => ({ ok: true, status, json: async () => data })
    if (String(url).includes('/schedule-preview')) return ok(preview((body as { schedule: string }).schedule))
    if (method === 'GET' && String(url).endsWith('/api/watches/channels')) {
      if (channelsGate) await channelsGate
      return ok(channels)
    }
    if (method === 'GET' && String(url).match(/\/api\/watches\/[^/]+\/runs\/[^/?]+$/)) {
      const id = String(url).split('/').pop()
      const found = linkedRuns[id ?? '']
      return found ? ok(found) : { ok: false, status: 404, json: async () => ({ error: 'no such run' }) }
    }
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
      <WatchDetailsPage mode={{ kind: 'existing', id: 'w1' }} canWrite canAdmin {...handlers} {...props} />
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

  it('lines a section note and its threshold up under the section name, ticked or not', async () => {
    renderPage()
    await screen.findByDisplayValue('Overnight check')
    expect(screen.getByRole('checkbox', { name: 'Power' })).toHaveAccessibleDescription(/house bank drains/)
    expect(screen.getByRole('checkbox', { name: 'Weather' })).not.toBeChecked()
    expect(screen.getByLabelText('Pressure fall that counts (hPa)').closest('.pl-7')).not.toBeNull()
    expect(screen.getByRole('checkbox', { name: 'Alarms' })).toBeChecked()
    expect(screen.getByLabelText('Alarms raised that count').closest('.pl-7')).not.toBeNull()
  })

  it('drops a stored section that has no row, so it cannot block Save or stand in for a ticked one', async () => {
    detail = { ...detail, watch: record({ sections: ['power', 'engine'] }) }
    renderPage()
    await screen.findByDisplayValue('Overnight check')
    fireEvent.click(screen.getByRole('checkbox', { name: 'Power' }))
    expect(screen.getByText('Pick at least one section.')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('checkbox', { name: 'Tanks' }))
    await waitFor(() => expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled())
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(sent('PATCH')).toHaveLength(1))
    expect(sent('PATCH')[0].body).toMatchObject({ sections: ['tanks'] })
  })

  it('keeps a bad threshold on screen after its section is unticked, so Save never blocks on something hidden', async () => {
    renderPage()
    await screen.findByDisplayValue('Overnight check')
    fireEvent.change(screen.getByLabelText('Alarms raised that count'), { target: { value: '0' } })
    fireEvent.click(screen.getByRole('checkbox', { name: 'Alarms' }))
    expect(screen.getByText('A whole number from 1 to 100.')).toBeInTheDocument()
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
    await waitFor(() => expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled())
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
    expect(await screen.findByText(/runs more often than every 15 minutes/)).toBeInTheDocument()
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
    expect(await screen.findByText("No boat position, so the boat's time is not known")).toBeInTheDocument()
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
    await waitFor(() => expect(calls.some((c) => c.url.includes('schedule-preview') && (c.body as { schedule: string }).schedule === '0 * * * *')).toBe(true))
  })
})

describe('WatchDetailsPage save bar', () => {
  it('says why Save is off, since the field at fault can be a long way down the page', async () => {
    renderPage()
    fireEvent.change(await screen.findByLabelText('Alarms raised that count'), { target: { value: '' } })
    await waitFor(() => expect(document.querySelector('[data-slot="save-bar"]')).toHaveTextContent('Fix the highlighted fields first.'))
  })
})

describe('WatchDetailsPage save bar and the schedule check', () => {
  const bar = () => document.querySelector('[data-slot="save-bar"]')

  it('does not send the skipper to a highlighted field when the schedule check itself failed', async () => {
    preview = () => { throw new Error('offline') }
    renderPage()
    fireEvent.change(await screen.findByDisplayValue('Overnight check'), { target: { value: 'Renamed' } })
    await waitFor(() => expect(bar()).toHaveTextContent('Save waits until the schedule can be checked.'))
    expect(bar()).not.toHaveTextContent('Fix the highlighted fields first.')
  })

  it('still asks for the highlighted field when the schedule is refused', async () => {
    preview = () => ({ valid: false, error: '"* * * * *" runs more often than every 15 minutes', boat_time_known: true, next: [] })
    renderPage()
    fireEvent.change(await screen.findByDisplayValue('Overnight check'), { target: { value: 'Renamed' } })
    await waitFor(() => expect(bar()).toHaveTextContent('Fix the highlighted fields first.'))
  })
})

describe('WatchDetailsPage new watch', () => {
  it('opens pre-filled from a template and saves nothing until Create', async () => {
    const onCreated = vi.fn()
    renderPage({ mode: { kind: 'new', template: WATCH_TEMPLATES[1] }, onCreated })
    expect(await screen.findByDisplayValue('Overnight check')).toBeInTheDocument()
    expect(sent('POST')).toHaveLength(0)
    await waitFor(() => expect(screen.getByRole('button', { name: 'Create watch' })).toBeEnabled())
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

/** The accordion trigger of a run in the history list: the button inside the item's heading. */
function triggerOf(item: HTMLElement): HTMLElement {
  return within(within(item).getByRole('heading')).getByRole('button')
}
const runItem = (id: string) => {
  const item = document.getElementById(`run-${id}`)
  if (!item) throw new Error(`no run ${id} on the page`)
  return item
}
async function deleteRun(id: string) {
  fireEvent.click(within(runItem(id)).getByRole('button', { name: 'Delete run' }))
  const dialog = await screen.findByRole('alertdialog')
  fireEvent.click(within(dialog).getByRole('button', { name: 'Delete run' }))
  await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument())
}

describe('WatchDetailsPage settings aside', () => {
  it('puts the schedule, report sections and notifications in the complementary column, in that order', async () => {
    renderPage()
    const aside = await screen.findByRole('complementary')
    expect(within(aside).getByRole('combobox', { name: 'Schedule' })).toBeInTheDocument()
    expect(within(aside).getByRole('group', { name: 'Report sections' })).toBeInTheDocument()
    expect(await within(aside).findByRole('group', { name: 'Send to' })).toBeInTheDocument()
    const text = aside.textContent ?? ''
    expect(text.indexOf('Report sections')).toBeGreaterThan(text.indexOf('Times are on the boat'))
    expect(text.indexOf('Notifications')).toBeGreaterThan(text.indexOf('What each report covers'))
  })

  it('keeps the run history in the main column and puts the name and what to look out for at the top of the aside', async () => {
    renderPage()
    const aside = await screen.findByRole('complementary')
    await screen.findByTestId('watch-run')
    expect(aside.contains(screen.getByTestId('watch-run'))).toBe(false)
    expect(aside.contains(screen.getByRole('region', { name: 'Run history' }))).toBe(false)
    const name = screen.getByLabelText('Name')
    const lookOut = screen.getByLabelText('What to look out for')
    expect(aside.contains(name)).toBe(true)
    expect(aside.contains(lookOut)).toBe(true)
    expect(aside.contains(screen.getByRole('switch', { name: 'Enabled' }))).toBe(true)
    const schedule = within(aside).getByRole('combobox', { name: 'Schedule' })
    expect(name.compareDocumentPosition(schedule) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(lookOut.compareDocumentPosition(schedule) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  it('shows the reports before the settings in the page order', async () => {
    renderPage()
    const firstRun = await screen.findByTestId('watch-run')
    const name = screen.getByLabelText('Name')
    expect(firstRun.compareDocumentPosition(name) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  it('also uses the aside for a new watch, and keeps the name and what to look out for in the main column', async () => {
    renderPage({ mode: { kind: 'new', template: null } })
    const aside = await screen.findByRole('complementary')
    expect(within(aside).getByRole('group', { name: 'Report sections' })).toBeInTheDocument()
    expect(aside.contains(screen.getByLabelText('Name'))).toBe(false)
    expect(aside.contains(screen.getByLabelText('What to look out for'))).toBe(false)
    expect(screen.queryByRole('region', { name: 'Run history' })).not.toBeInTheDocument()
  })

  it('gives a one-off watch no aside', async () => {
    detail = {
      watch: record({ kind: 'one_off', name: 'Engine load', schedule: undefined, duration_minutes: 10, schedule_sentence: 'One-off · 10 minutes' }),
      runs: [],
      has_older: false,
    }
    renderPage()
    expect(await screen.findByText('One-off watch')).toBeInTheDocument()
    expect(screen.queryByRole('complementary')).not.toBeInTheDocument()
  })
})

describe('WatchDetailsPage run history accordion', () => {
  const newest = () => run('r1', { review: 'Newest review text', review_rest: 'Newest review text' })
  const older = () =>
    run('r2', {
      started_at: '2026-10-27T17:00:00Z',
      boat_time: 'Wed 28 Oct 06:00 UTC+13',
      findings: [{ section: 'alarms', rule: 'alarms_raised', text: 'One older alarm', by: 'rules' }],
      review: 'Older review text',
      review_rest: 'Older review text',
      cost_usd: 0.05,
    })

  it('opens only the newest run, and a closed run still shows its time and findings but not its cost', async () => {
    detail = { ...detail, runs: [newest(), older()] }
    renderPage()
    expect(await screen.findAllByTestId('watch-run')).toHaveLength(2)
    expect(triggerOf(runItem('r1'))).toHaveAttribute('aria-expanded', 'true')
    expect(triggerOf(runItem('r2'))).toHaveAttribute('aria-expanded', 'false')
    expect(await screen.findByText('Newest review text')).toBeInTheDocument()
    expect(screen.queryByText('Older review text')).not.toBeInTheDocument()
    const closed = within(runItem('r2'))
    expect(closed.getByText('Wed 28 Oct 06:00 UTC+13')).toBeInTheDocument()
    expect(closed.getByText('1 to look at')).toBeInTheDocument()
    expect(closed.queryByText('$0.05')).not.toBeInTheDocument()
    expect(closed.queryByText('One older alarm')).not.toBeInTheDocument()
  })

  it('opens an older run on a tap and leaves the newest open', async () => {
    detail = { ...detail, runs: [newest(), older()] }
    renderPage()
    await screen.findByText('Newest review text')
    fireEvent.click(triggerOf(runItem('r2')))
    expect(await screen.findByText('Older review text')).toBeInTheDocument()
    expect(triggerOf(runItem('r2'))).toHaveAttribute('aria-expanded', 'true')
    expect(triggerOf(runItem('r1'))).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByText('Newest review text')).toBeInTheDocument()
  })

  it('hands the open newest run over to a newer one that arrives, and leaves a run opened by hand open', async () => {
    detail = { ...detail, runs: [newest(), older()] }
    renderPage()
    await screen.findByText('Newest review text')
    fireEvent.click(triggerOf(runItem('r2')))
    await screen.findByText('Older review text')
    detail = { ...detail, runs: [run('r3', { started_at: '2026-10-29T17:00:00Z', boat_time: 'Fri 30 Oct 06:00 UTC+13', review: 'Brand new review', review_rest: 'Brand new review' }), newest(), older()] }
    await act(async () => { await invalidateWatches() })
    expect(await screen.findByText('Brand new review')).toBeInTheDocument()
    expect(triggerOf(runItem('r3'))).toHaveAttribute('aria-expanded', 'true')
    expect(triggerOf(runItem('r1'))).toHaveAttribute('aria-expanded', 'false')
    expect(triggerOf(runItem('r2'))).toHaveAttribute('aria-expanded', 'true')
  })

  it('keeps a run opened by hand open after it becomes the newest and a newer one arrives', async () => {
    detail = { ...detail, runs: [newest(), older()] }
    renderPage()
    await screen.findByText('Newest review text')
    fireEvent.click(triggerOf(runItem('r2')))
    await screen.findByText('Older review text')
    detail = { ...detail, runs: [older()] }
    await deleteRun('r1')
    await waitFor(() => expect(screen.getAllByTestId('watch-run')).toHaveLength(1))
    detail = { ...detail, runs: [run('r3', { started_at: '2026-10-29T17:00:00Z', boat_time: 'Fri 30 Oct 06:00 UTC+13', review: 'Brand new review', review_rest: 'Brand new review' }), older()] }
    await act(async () => { await invalidateWatches() })
    expect(await screen.findByText('Brand new review')).toBeInTheDocument()
    expect(triggerOf(runItem('r2'))).toHaveAttribute('aria-expanded', 'true')
  })

  it('opens the next run when the newest is deleted', async () => {
    detail = { ...detail, runs: [newest(), older()] }
    renderPage()
    await screen.findByText('Newest review text')
    detail = { ...detail, runs: [older()] }
    await deleteRun('r1')
    expect(await screen.findByText('Older review text')).toBeInTheDocument()
    expect(triggerOf(runItem('r2'))).toHaveAttribute('aria-expanded', 'true')
  })

  it('leaves the newest closed across a refresh once it was closed by hand', async () => {
    detail = { ...detail, runs: [newest(), older()] }
    renderPage()
    await screen.findByText('Newest review text')
    fireEvent.click(triggerOf(runItem('r1')))
    await waitFor(() => expect(screen.queryByText('Newest review text')).not.toBeInTheDocument())
    detail = { ...detail, runs: [{ ...newest(), review: 'Newest review text, changed', review_rest: 'Newest review text, changed' }, older()] }
    await act(async () => { await invalidateWatches() })
    expect(triggerOf(runItem('r1'))).toHaveAttribute('aria-expanded', 'false')
  })

  it('says on the closed line when a run did not reach anyone or has no review', async () => {
    detail = {
      ...detail,
      runs: [
        run('r1'),
        run('r2', { notify_pending: [{ channel: 'smtp', reason: 'connection refused' }] }),
        run('r3', { not_sent: 'no channel is switched on for this watch (ntfy)' }),
        run('r4', { review: null, review_problem: 'Mate is not ready' }),
      ],
    }
    renderPage()
    await waitFor(() => expect(screen.getAllByTestId('watch-run')).toHaveLength(4))
    expect(triggerOf(runItem('r1'))).not.toHaveTextContent(/Not delivered|Not sent|No review/)
    expect(triggerOf(runItem('r2'))).toHaveTextContent('Not delivered')
    expect(triggerOf(runItem('r3'))).toHaveTextContent('Not sent')
    expect(triggerOf(runItem('r4'))).toHaveTextContent('No review')
  })

  it('names the run the delete dialog is about', async () => {
    detail = { ...detail, runs: [newest(), older()] }
    renderPage()
    await screen.findByText('Newest review text')
    fireEvent.click(screen.getByRole('button', { name: 'Delete run' }))
    const dialog = await screen.findByRole('alertdialog')
    expect(dialog).toHaveTextContent(`Delete the run from ${runTimeLabel(newest())}?`)
  })

  it('follows a linked run being pinned and cleared without a remount', () => {
    const listProps = { hasOlder: false, canWrite: false, loadingOlder: false, olderError: null, onShowOlder: () => {}, onDeleteRun: async () => {} }
    const { rerender } = render(<WatchRunList runs={[newest(), older()]} expandNewest {...listProps} />)
    expect(triggerOf(runItem('r1'))).toHaveAttribute('aria-expanded', 'true')
    rerender(<WatchRunList runs={[newest(), older()]} expandNewest={false} {...listProps} />)
    expect(triggerOf(runItem('r1'))).toHaveAttribute('aria-expanded', 'false')
    rerender(<WatchRunList runs={[newest(), older()]} expandNewest {...listProps} />)
    expect(triggerOf(runItem('r1'))).toHaveAttribute('aria-expanded', 'true')
  })

  it('opens nothing when Show older brings older runs in', async () => {
    detail = { ...detail, runs: [newest()], has_older: true }
    olderRuns = [older()]
    renderPage()
    await screen.findByText('Newest review text')
    fireEvent.click(screen.getByRole('button', { name: 'Show older' }))
    await waitFor(() => expect(screen.getAllByTestId('watch-run')).toHaveLength(2))
    expect(triggerOf(runItem('r2'))).toHaveAttribute('aria-expanded', 'false')
    expect(triggerOf(runItem('r1'))).toHaveAttribute('aria-expanded', 'true')
    expect(screen.queryByText('Older review text')).not.toBeInTheDocument()
  })

  it('starts the list closed when a linked run is pinned above it', async () => {
    Element.prototype.scrollIntoView = vi.fn()
    linkedRuns = { old: run('old', { boat_time: 'Mon 12 Oct 03:00 UTC+13' }) }
    detail = { ...detail, runs: [newest(), older()] }
    renderPage({ linkedRunId: 'old' })
    await screen.findByTestId('watch-linked-run')
    await screen.findByText('Linked run')
    expect(triggerOf(runItem('r1'))).toHaveAttribute('aria-expanded', 'false')
    expect(triggerOf(runItem('r2'))).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByText('Newest review text')).not.toBeInTheDocument()
  })
})

describe('WatchDetailsPage run history', () => {
  it('renders a run with findings, the Mate badge, section figures, unavailable reasons, review and cost', async () => {
    detail = { ...detail, runs: detail.runs.map((r) => ({ ...r, review_rest: r.review ?? undefined })) }
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

  it('says "No review." with the problem as the opening line, and "cost not known" in the footer', async () => {
    detail = {
      ...detail,
      runs: [run('r1', { review: null, review_problem: 'No model is chosen for Mate.', cost_usd: null, cost_known: false, findings: [] })],
    }
    renderPage()
    const card = await screen.findByTestId('watch-run')
    const opening = within(card).getByTestId('watch-run-opening')
    expect(opening).toHaveTextContent('No review. No model is chosen for Mate.')
    expect(opening.className).toContain('text-xl')
    expect(within(card).queryByTestId('watch-run-review')).not.toBeInTheDocument()
    expect(within(within(card).getByTestId('watch-run-footer')).getByText('cost not known')).toBeInTheDocument()
  })

  it('says Mate did not review the run when no reason was given', async () => {
    detail = { ...detail, runs: [run('r1', { review: null, review_problem: null, findings: [] })] }
    renderPage()
    expect(await screen.findByTestId('watch-run-opening')).toHaveTextContent('No review. Mate did not review this run.')
  })

  it('shows a skipped run with its reason and no review', async () => {
    detail = {
      ...detail,
      runs: [run('r1', { outcome: 'skipped', skip_reason: "No boat position, so the boat's time is not known", report: undefined, findings: [], review: null, review_problem: null })],
    }
    renderPage()
    const card = await screen.findByTestId('watch-run')
    expect(within(card).getByText('Skipped')).toBeInTheDocument()
    const opening = within(card).getByTestId('watch-run-opening')
    expect(opening).toHaveTextContent("No boat position, so the boat's time is not known")
    expect(opening.className).toContain('text-xl')
    expect(within(card).queryByTestId('watch-run-review')).not.toBeInTheDocument()
  })

  it('says Skipped. when a skipped run has no reason', async () => {
    detail = { ...detail, runs: [run('r1', { outcome: 'skipped', skip_reason: '', report: undefined, findings: [], review: null })] }
    renderPage()
    expect(await screen.findByTestId('watch-run-opening')).toHaveTextContent('Skipped.')
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
    // Its report goes to the chat that asked for it, so no review is missing.
    expect(screen.queryByTestId('watch-run-opening')).not.toBeInTheDocument()
    expect(screen.queryByText('No review')).not.toBeInTheDocument()
    // A one-off watch is read-only: nothing to save, nothing to run again.
    expect(screen.getByText('One-off watch')).toBeInTheDocument()
    expect(screen.getByText('Thu 29 Oct 05:50 UTC+13')).toBeInTheDocument()
    expect(screen.getByText('Ended Thu 29 Oct 06:00 UTC+13')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Run now' })).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Cron')).not.toBeInTheDocument()
  })

  it('pages older runs in with Show older', async () => {
    detail = { ...detail, has_older: true }
    olderRuns = [run('r0', { started_at: '2026-10-28T06:00:00Z', report: undefined, report_kind: 'sections', findings: [], review: 'Older review', review_rest: 'Older review', cost_known: true, cost_usd: 0 })]
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

describe('WatchDetailsPage report hierarchy', () => {
  const reviewed = (id: string, over: Partial<WatchRun> = {}) =>
    run(id, {
      review: 'Fridge draw is up. Watch **it** tonight.',
      review_opening: 'Fridge draw is up.',
      review_rest: 'Watch **it** tonight.',
      ...over,
    })

  it('opens a run on the opening line, then what needs a look, Mate’s review, the readings and the footer, in that order', async () => {
    detail = { ...detail, runs: [reviewed('r1', { notified: ['ntfy'] })] }
    renderPage()
    const card = await screen.findByTestId('watch-run')
    const opening = within(card).getByTestId('watch-run-opening')
    expect(opening).toHaveTextContent('Fridge draw is up.')
    expect(opening.className).toContain('text-xl')
    expect(opening.className).toContain('font-semibold')
    expect(await within(card).findByText('it', { selector: 'strong' })).toBeInTheDocument()
    const text = card.textContent ?? ''
    const order = ['Since', 'Fridge draw is up.', 'Needs a look', "Mate's review", 'Watch it tonight.', 'Readings', 'House bank 84%', 'Notified: ntfy']
    const at = order.map((s) => text.indexOf(s))
    expect(at.every((i) => i >= 0)).toBe(true)
    expect([...at].sort((a, b) => a - b)).toEqual(at)
    // The opening is said once, not again inside Mate's review.
    expect(text.split('Fridge draw is up.').length - 1).toBe(1)
  })

  it('draws the since line only on a sections report, and the findings without their section label', async () => {
    detail = { ...detail, runs: [reviewed('r1')] }
    renderPage()
    const card = await screen.findByTestId('watch-run')
    expect(within(card).getByText('Since Wed 28 Oct 22:00 UTC+13')).toBeInTheDocument()
    const rule = within(card).getByText('2 alarms raised').closest('li')!
    expect(rule.textContent).toBe('2 alarms raised')
    expect(within(card).getByText('2 alarms raised').className).toContain('text-base')
    const mate = within(card).getByText('Fridge draw is climbing').closest('li')!
    expect(mate.firstElementChild?.textContent).toContain('Mate')
    expect(within(card).getByLabelText('Findings')).toBeInTheDocument()
  })

  it('shows no findings block, and no Mate’s review heading, for a one-sentence review with no findings', async () => {
    detail = { ...detail, runs: [reviewed('r1', { findings: [], review: 'All quiet.', review_opening: 'All quiet.', review_rest: undefined })] }
    renderPage()
    const card = await screen.findByTestId('watch-run')
    expect(within(card).getByTestId('watch-run-opening')).toHaveTextContent('All quiet.')
    expect(within(card).queryByText('Needs a look')).not.toBeInTheDocument()
    expect(within(card).queryByTestId('watch-run-review')).not.toBeInTheDocument()
  })

  it('shows the whole review under Mate’s review, and invents no opening, when the server sent no opening', async () => {
    detail = { ...detail, runs: [run('r1', { review: 'Whole review text only.', review_opening: undefined, review_rest: 'Whole review text only.' })] }
    renderPage()
    const card = await screen.findByTestId('watch-run')
    expect(within(card).queryByTestId('watch-run-opening')).not.toBeInTheDocument()
    expect(within(within(card).getByTestId('watch-run-review')).getByText('Whole review text only.')).toBeInTheDocument()
  })

  it('folds the opening line into a closed run’s summary, with no cost and no Ran badge', async () => {
    detail = { ...detail, runs: [reviewed('r1'), reviewed('r2', { started_at: '2026-10-27T17:00:00Z', review_opening: 'Older opening line.', review: 'Older opening line.', review_rest: undefined })] }
    renderPage()
    await waitFor(() => expect(screen.getAllByTestId('watch-run')).toHaveLength(2))
    const closed = triggerOf(runItem('r2'))
    expect(closed).toHaveTextContent('Older opening line.')
    expect(closed).not.toHaveTextContent('$0.01')
    expect(closed).not.toHaveTextContent('Ran')
    const line = within(closed).getByText('Older opening line.')
    expect(line.className).toContain('truncate')
    expect(line.className).toContain('min-w-0')
    expect(line.className).toContain('text-sm')
    // Open, the opening is in the body and not repeated on the summary.
    expect(triggerOf(runItem('r1'))).not.toHaveTextContent('Fridge draw is up.')
  })

  it('puts the time first and the findings badge next, and a Skipped badge only on a skipped run', async () => {
    detail = { ...detail, runs: [run('r1'), run('r2', { started_at: '2026-10-27T17:00:00Z', outcome: 'skipped', skip_reason: 'No boat position', findings: [], review: null, report: undefined })] }
    renderPage()
    await waitFor(() => expect(screen.getAllByTestId('watch-run')).toHaveLength(2))
    const ran = triggerOf(runItem('r1')).textContent ?? ''
    expect(ran.indexOf('Thu 29 Oct 06:00 UTC+13')).toBe(0)
    expect(ran.indexOf('2 to look at')).toBeGreaterThan(0)
    expect(ran).not.toContain('Skipped')
    expect(triggerOf(runItem('r2'))).toHaveTextContent('Skipped')
  })

  it('shows no second line on a closed run with no opening', async () => {
    detail = { ...detail, runs: [run('r1'), run('r2', { started_at: '2026-10-27T17:00:00Z', review: null, review_problem: 'Mate is not ready' })] }
    renderPage()
    await waitFor(() => expect(screen.getAllByTestId('watch-run')).toHaveLength(2))
    expect(triggerOf(runItem('r2'))).toHaveAttribute('aria-expanded', 'false')
    expect(triggerOf(runItem('r2'))).not.toHaveTextContent('Mate is not ready')
  })

  it('puts the cost and who was told in the footer beside Ask Mate and Delete run', async () => {
    detail = { ...detail, runs: [reviewed('r1', { notified: ['ntfy'], not_sent: undefined })] }
    renderPage()
    const card = await screen.findByTestId('watch-run')
    const footer = within(card).getByTestId('watch-run-footer')
    expect(footer).toHaveTextContent('$0.01')
    expect(footer).toHaveTextContent('Notified: ntfy')
    expect(within(footer).getByRole('button', { name: 'Ask Mate about this run' })).toBeInTheDocument()
    expect(within(footer).getByRole('button', { name: 'Delete run' })).toBeInTheDocument()
  })

  it('keeps every notify line in the footer', async () => {
    detail = {
      ...detail,
      runs: [reviewed('r1', {
        not_sent: 'no channel is switched on',
        notify_notes: ['ntfy: sent without the chart'],
        notify_pending: [{ channel: 'smtp', reason: 'connection refused' }],
        notified: ['ntfy'],
      })],
    }
    renderPage()
    const footer = within(await screen.findByTestId('watch-run')).getByTestId('watch-run-footer')
    for (const s of ['Not sent: no channel is switched on', 'Notified: ntfy', 'ntfy: sent without the chart', 'Not delivered yet to Email: connection refused']) {
      expect(footer).toHaveTextContent(s)
    }
  })

  it('lays the readings out under a Readings heading with a heading on each section', async () => {
    renderPage()
    const card = await screen.findByTestId('watch-run')
    const readings = within(card).getByTestId('watch-sections-report')
    expect(within(card).getByText('Readings')).toBeInTheDocument()
    const power = within(readings).getByText('Power')
    expect(power.className).toContain('font-semibold')
    expect(within(readings).getByText('House bank 84% at 13.1 V').className).toContain('text-sm')
    expect(within(readings).getByText('Unavailable: pressure history still filling')).toBeInTheDocument()
  })

  it('names Tanks and Alarms apart from the other sections so they can sit in their own places', async () => {
    detail = {
      ...detail,
      runs: [run('r1', {
        report: {
          kind: 'sections', generated_at: '', boat_time: '', since: 'Wed 28 Oct 22:00 UTC+13',
          sections: ['power', 'anchor', 'alarms', 'tanks', 'weather', 'instruments', 'extra'].map((name) => ({ name, title: name, lines: [`${name} line`] })),
        },
      })],
    }
    renderPage()
    const readings = within(await screen.findByTestId('watch-run')).getByTestId('watch-sections-report')
    const section = (name: string) => readings.querySelector(`[data-section="${name}"]`) as HTMLElement
    expect(section('tanks').className).toContain('sm:col-start-2')
    expect(section('alarms').className).toContain('sm:col-span-2')
    for (const name of ['power', 'anchor', 'weather', 'instruments', 'extra']) expect(section(name).className).toContain('sm:col-start-1')
    // One column on a phone, in the report's own order.
    const names = [...readings.querySelectorAll('[data-section]')].map((el) => el.getAttribute('data-section'))
    expect(names).toEqual(['power', 'anchor', 'alarms', 'tanks', 'weather', 'instruments', 'extra'])
  })

  it('puts Earlier reports between the newest run and the rest only when there is more than one', async () => {
    detail = { ...detail, runs: [run('r1'), run('r2', { started_at: '2026-10-27T17:00:00Z' })] }
    renderPage()
    const heading = await screen.findByRole('heading', { name: 'Earlier reports' })
    expect(runItem('r1').compareDocumentPosition(heading) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(heading.compareDocumentPosition(runItem('r2')) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  it('calls the list Other reports, before its first run, when a linked run is pinned above it', () => {
    const listProps = { hasOlder: false, canWrite: false, loadingOlder: false, olderError: null, onShowOlder: () => {}, onDeleteRun: async () => {} }
    render(<WatchRunList runs={[run('r1'), run('r2', { started_at: '2026-10-27T17:00:00Z' })]} expandNewest={false} {...listProps} />)
    const heading = screen.getByRole('heading', { name: 'Other reports' })
    expect(screen.queryByRole('heading', { name: 'Earlier reports' })).not.toBeInTheDocument()
    expect(heading.compareDocumentPosition(runItem('r1')) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  it('keeps Earlier reports before the second run when the newest opens', () => {
    const listProps = { hasOlder: false, canWrite: false, loadingOlder: false, olderError: null, onShowOlder: () => {}, onDeleteRun: async () => {} }
    render(<WatchRunList runs={[run('r1'), run('r2', { started_at: '2026-10-27T17:00:00Z' })]} expandNewest {...listProps} />)
    expect(screen.queryByRole('heading', { name: 'Other reports' })).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Earlier reports' })).toBeInTheDocument()
  })

  it('uses two columns from sm up only when the report has a tanks section', async () => {
    const sections = (names: string[]) => ({
      kind: 'sections' as const, generated_at: '', boat_time: '', since: 'Wed 28 Oct 22:00 UTC+13',
      sections: names.map((name) => ({ name, title: name, lines: [`${name} line`] })),
    })
    detail = { ...detail, runs: [run('r1', { report: sections(['power', 'alarms']) })] }
    renderPage()
    let grid = await screen.findByTestId('watch-sections-report')
    expect(grid.className).toContain('grid-cols-1')
    expect(grid.className).not.toContain('sm:grid-cols-2')
    cleanup()
    detail = { ...detail, runs: [run('r1', { report: sections(['power', 'tanks']) })] }
    renderPage()
    grid = await screen.findByTestId('watch-sections-report')
    expect(grid.className).toContain('sm:grid-cols-2')
  })

  it('has no Earlier reports heading with a single run', async () => {
    renderPage()
    await screen.findByTestId('watch-run')
    expect(screen.queryByRole('heading', { name: 'Earlier reports' })).not.toBeInTheDocument()
  })

  it('is not a card inside a card, and says runs with a finding are kept after the list', async () => {
    renderPage()
    const region = await screen.findByRole('region', { name: 'Run history' })
    expect(region.closest('[data-slot="card"]')).toBeNull()
    expect(screen.queryByText('Newest first. Runs with a finding are kept for good.')).not.toBeInTheDocument()
    const kept = within(region).getByText('Runs with a finding are kept for good.')
    expect(kept.className).toContain('text-muted-foreground')
    expect(screen.getByTestId('watch-run').compareDocumentPosition(kept) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  it('puts Run now in the page header, outside the report list, and keeps its status above the list', async () => {
    renderPage()
    await screen.findByTestId('watch-run')
    const button = screen.getByRole('button', { name: 'Run now' })
    expect(screen.getByRole('region', { name: 'Run history' }).contains(button)).toBe(false)
    expect(button.closest('[data-slot="card"]')).toBeNull()
    fireEvent.click(button)
    const status = screen.getByRole('status')
    expect(status).toHaveTextContent('Building the report.')
    expect(screen.getByRole('region', { name: 'Run history' }).contains(status)).toBe(true)
    expect(status.compareDocumentPosition(screen.getByTestId('watch-run')) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    await waitFor(() => expect(screen.getByRole('button', { name: 'Run now' })).toBeEnabled())
  })

  it('turns Run now off while there are unsaved changes', async () => {
    renderPage()
    fireEvent.change(await screen.findByDisplayValue('Overnight check'), { target: { value: 'Renamed' } })
    expect(screen.getByRole('button', { name: 'Run now' })).toBeDisabled()
  })

  it('puts a one-off watch’s details first and its samples under Readings at text-sm', async () => {
    detail = {
      watch: record({ kind: 'one_off', name: 'Engine load', schedule: undefined, duration_minutes: 10, schedule_sentence: 'One-off · 10 minutes' }),
      runs: [run('r1', {
        report_kind: 'samples', review: null, review_problem: null, findings: [],
        report: { reason: 'Engine load', timezone: 'NZDT', started: 'a', ended: 'b', minutes: 10, note: 'Values are SignalK units', series: [{ label: 'Port engine load', units: 'ratio', samples: 600, min: 0.2, mean: 0.5, max: 0.8, last: 0.6, excursion_count: 1 }] },
      })],
      has_older: false,
    }
    renderPage()
    const details = await screen.findByText('One-off watch')
    const card = await screen.findByTestId('watch-run')
    expect(details.compareDocumentPosition(card) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(within(card).getByText('Readings')).toBeInTheDocument()
    expect(within(within(card).getByTestId('watch-samples-report')).getByText('Port engine load').closest('li')!.className).toContain('text-sm')
  })
})

describe('WatchDetailsPage boat time', () => {
  it('shows a run on the boat clock the server gave, never the device\u2019s', async () => {
    renderPage()
    const card = await screen.findByTestId('watch-run')
    expect(within(card).getByRole('heading', { name: /^Thu 29 Oct 06:00 UTC\+13/ })).toBeInTheDocument()
    // 17:00 UTC is Wed 28 Oct 07:00 in Honolulu, the device zone of this test.
    expect(screen.queryByText(/Wed 28 Oct 07:00/)).not.toBeInTheDocument()
  })

  it('says a run with no boat position is on UTC and not boat time', async () => {
    detail = { ...detail, runs: [run('r1', { boat_time: 'Thu 29 Oct 17:00 UTC (boat time not known)', timezone: '' })] }
    renderPage()
    expect(await screen.findByRole('heading', { name: /^Thu 29 Oct 17:00 UTC \(boat time not known\)/ })).toBeInTheDocument()
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
    await waitFor(() => expect(within(box).getByText('Thu 29 Oct 22:00 NZDT')).toBeInTheDocument())
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
    await waitFor(() => expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled())
    detail = { ...detail, watch: record({ name: 'Night watch' }) }
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Save' })).not.toBeInTheDocument())
    expect(screen.queryByText(/changed elsewhere/)).not.toBeInTheDocument()
    expect(screen.getByDisplayValue('Night watch')).toBeInTheDocument()
  })
})

describe('WatchDetailsPage notifications', () => {
  it('offers a checkbox only for the channels switched on, never SignalK, and lists the rest in one line', async () => {
    renderPage()
    const group = await screen.findByRole('group', { name: 'Send to' })
    await waitFor(() => expect(within(group).queryByRole('checkbox', { name: 'Email' })).not.toBeInTheDocument())
    expect(within(group).getByRole('checkbox', { name: 'ntfy' })).toBeChecked()
    expect(within(group).getByRole('checkbox', { name: 'Phone and browser push' })).not.toBeChecked()
    expect(within(group).queryByRole('checkbox', { name: 'Webhook' })).not.toBeInTheDocument()
    expect(within(group).queryByRole('checkbox', { name: /signalk/i })).not.toBeInTheDocument()
    expect(group).toHaveTextContent(/Email and Webhook are not set up for this boat\. Set them up in Alarm settings\./)
    const link = within(group).getByRole('link', { name: 'Alarm settings' })
    expect(link).toHaveAttribute('href', '/settings/alarms')
  })

  it('moves to the alarm settings in the app, without a page load, and asks for the section to be focused', async () => {
    const router = createAppRouter(memoryHistory('/watches/w1'))
    const navigate = vi.spyOn(router, 'navigate')
    render(
      <AppRouterContext.Provider value={router}>
        <WatchDetailsPage mode={{ kind: 'existing', id: 'w1' }} canWrite canAdmin onOpenIndex={vi.fn()} onCreated={vi.fn()} onDeleted={vi.fn()} onAskMate={vi.fn()} />
      </AppRouterContext.Provider>,
    )
    fireEvent.click(await screen.findByRole('link', { name: 'Alarm settings' }))
    expect(navigate).toHaveBeenCalledWith({ panel: 'settings', route: { view: 'section', section: 'alarms' } })
    expect(peekPendingSetupFocus()).toBe('notifications')
    clearPendingSetupFocus()
  })

  it('keeps a request for the section when a guard holds the move, and asks again once the move is made', async () => {
    const router = createAppRouter(memoryHistory('/watches/w1'))
    const release = router.registerGuard(() => ({ owner: 't', label: 'Watch', blocks: () => ({ kind: 'dirty' as const, detail: null }) }))
    render(
      <AppRouterContext.Provider value={router}>
        <WatchDetailsPage mode={{ kind: 'existing', id: 'w1' }} canWrite canAdmin onOpenIndex={vi.fn()} onCreated={vi.fn()} onDeleted={vi.fn()} onAskMate={vi.fn()} />
      </AppRouterContext.Provider>,
    )
    fireEvent.click(await screen.findByRole('link', { name: 'Alarm settings' }))
    expect(router.getSnapshot().pending).not.toBeNull()
    expect(peekPendingSetupFocus()).toBeNull()
    act(() => router.confirmPending())
    expect(peekPendingSetupFocus()).toBe('notifications')
    release()
    clearPendingSetupFocus()
  })

  it('names the place in plain words, with no link, when the user cannot open Settings', async () => {
    renderPage({ canAdmin: false })
    const group = await screen.findByRole('group', { name: 'Send to' })
    expect(await within(group).findByText(/Ask whoever runs this boat's Helmcentral to set them up in Alarm settings\./)).toBeInTheDocument()
    expect(within(group).queryByRole('link')).not.toBeInTheDocument()
  })

  it('keeps the links for an admin when the form is read-only', async () => {
    renderPage({ canWrite: false })
    const group = await screen.findByRole('group', { name: 'Send to' })
    expect(await within(group).findByRole('link', { name: 'Alarm settings' })).toBeInTheDocument()
    expect(group).not.toHaveTextContent(/Ask whoever runs/)
  })

  it('draws no channel rows until Settings has answered', async () => {
    let release: () => void = () => {}
    channelsGate = new Promise<void>((resolve) => { release = resolve })
    renderPage()
    const group = await screen.findByRole('group', { name: 'Send to' })
    expect(within(group).getByText('Checking which channels are on…')).toBeInTheDocument()
    expect(within(group).queryByRole('checkbox')).not.toBeInTheDocument()
    await act(async () => { release(); await Promise.resolve() })
    await waitFor(() => expect(within(group).getByRole('checkbox', { name: 'ntfy' })).toBeInTheDocument())
  })

  it('names a single channel that is not set up in the singular', async () => {
    channels = { offered: ['ntfy', 'smtp', 'webhook', 'webpush'], enabled: ['ntfy', 'smtp', 'webpush'] }
    renderPage()
    await screen.findByRole('group', { name: 'Send to' })
    await waitFor(() => expect(screen.getByRole('group', { name: 'Send to' })).toHaveTextContent(/Webhook is not set up for this boat\. Set it up in Alarm settings\./))
  })

  it('keeps a selected channel that is off, checked, with a warning and a way to turn it on, and lets it go', async () => {
    detail = { ...detail, watch: record({ notify: ['ntfy', 'smtp'] }) }
    renderPage()
    const group = await screen.findByRole('group', { name: 'Send to' })
    const email = await within(group).findByRole('checkbox', { name: 'Email' })
    expect(email).toBeChecked()
    expect(within(group).getByText(/Off for the boat, so nothing goes here\./)).toBeInTheDocument()
    expect(within(group).getByRole('link', { name: 'Turn it on' })).toHaveAttribute('href', '/settings/alarms')
    fireEvent.click(email)
    await waitFor(() => expect(within(group).queryByRole('checkbox', { name: 'Email' })).not.toBeInTheDocument())
    expect(group).toHaveTextContent(/Email and Webhook are not set up for this boat\. Set them up in Alarm settings\./)
  })

  it('says reports stay on the page when no channel is on', async () => {
    channels = { offered: ['ntfy', 'smtp', 'webhook', 'webpush'], enabled: [] }
    detail = { ...detail, watch: record({ notify: [] }) }
    renderPage()
    const group = await screen.findByRole('group', { name: 'Send to' })
    expect(await within(group).findByText(/Nowhere yet\. Reports stay on this page until ntfy, Email, Webhook or Phone and browser push is set up for the boat\./)).toBeInTheDocument()
    expect(within(group).queryByRole('checkbox')).not.toBeInTheDocument()
    expect(within(group).getByRole('link', { name: 'Set up in Alarm settings' })).toHaveAttribute('href', '/settings/alarms')
  })

  it('shows every channel as a plain checkbox and an alert when Settings could not be read', async () => {
    const real = globalThis.fetch
    vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
      if (String(url).endsWith('/api/watches/channels')) return { ok: false, status: 500, json: async () => ({ error: 'boom' }) }
      return (real as typeof fetch)(url, init)
    }))
    renderPage()
    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent(/Could not read which channels are on: .*The choices below are saved, but may not be sent\./)
    const group = screen.getByRole('group', { name: 'Send to' })
    expect(within(group).getAllByRole('checkbox')).toHaveLength(4)
    expect(within(group).queryByText(/not set up/)).not.toBeInTheDocument()
  })

  it('chooses when to send with a radio group that always shows what each choice means', async () => {
    renderPage()
    const group = await screen.findByRole('group', { name: 'When to send' })
    expect(within(group).getByRole('radio', { name: /When it finds something/ })).toBeChecked()
    expect(within(group).getByText(/A problem that stays is not sent again every hour\./)).toBeInTheDocument()
    expect(within(group).getByText('Including the ones that find nothing.')).toBeInTheDocument()
    fireEvent.click(within(group).getByRole('radio', { name: /Every report/ }))
    await waitFor(() => expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled())
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(sent('PATCH')[0].body).toMatchObject({ send: 'every' }))
  })

  it('learns which channels are on from the watches route, which a read-write user can open, and never from the alarm settings', async () => {
    renderPage()
    await screen.findByRole('group', { name: 'Send to' })
    await waitFor(() => expect(calls.some((c) => c.url.endsWith('/api/watches/channels'))).toBe(true))
    expect(calls.some((c) => c.url.includes('/api/alarm-transports'))).toBe(false)
  })

  it('saves the channels and when to send with the rest of the watch', async () => {
    renderPage()
    const group = await screen.findByRole('group', { name: 'Send to' })
    await waitFor(() => expect(within(group).queryByRole('checkbox', { name: 'Email' })).not.toBeInTheDocument())
    fireEvent.click(within(group).getByRole('checkbox', { name: 'Phone and browser push' }))
    fireEvent.click(within(group).getByRole('checkbox', { name: 'ntfy' }))
    await waitFor(() => expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled())
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(sent('PATCH')).toHaveLength(1))
    expect(sent('PATCH')[0].body).toMatchObject({ notify: ['webpush'], send: 'findings' })
  })

  it('shows who a run was sent to, what is still being retried, and why one was not sent', async () => {
    detail = {
      ...detail,
      runs: [
        run('r1', { started_at: '2026-10-28T18:00:00Z', notified: ['ntfy'], notify_pending: [{ channel: 'smtp', reason: 'connection refused' }] }),
        run('r2', { not_sent: 'no channel is switched on for this watch (ntfy)' }),
      ],
    }
    renderPage()
    await waitFor(() => expect(screen.getAllByTestId('watch-run')).toHaveLength(2))
    fireEvent.click(triggerOf(runItem('r2')))
    await waitFor(() => expect(screen.getAllByTestId('watch-run-notified')).toHaveLength(2))
    const notes = screen.getAllByTestId('watch-run-notified')
    expect(notes[0]).toHaveTextContent('Notified: ntfy')
    expect(notes[0]).toHaveTextContent('Not delivered yet to Email: connection refused')
    expect(notes[1]).toHaveTextContent('Not sent: no channel is switched on for this watch (ntfy)')
  })
})

describe('a new watch', () => {
  const newMode = { kind: 'new', template: null } as const

  it('shows the channels that are switched on and sends only for findings', async () => {
    renderPage({ mode: newMode })
    const group = await screen.findByRole('group', { name: 'Send to' })
    await waitFor(() => expect(within(group).getByRole('checkbox', { name: 'ntfy' })).toBeChecked())
    expect(within(group).getByRole('checkbox', { name: 'Phone and browser push' })).toBeChecked()
    expect(within(group).queryByRole('checkbox', { name: 'Email' })).not.toBeInTheDocument()
  })

  it('leaves the channels to the server unless the operator changed them', async () => {
    renderPage({ mode: newMode })
    const group = await screen.findByRole('group', { name: 'Send to' })
    await waitFor(() => expect(within(group).getByRole('checkbox', { name: 'ntfy' })).toBeChecked())
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Dawn check' } })
    await waitFor(() => expect(screen.getByRole('button', { name: 'Create watch' })).toBeEnabled())
    fireEvent.click(screen.getByRole('button', { name: 'Create watch' }))
    await waitFor(() => expect(sent('POST').filter((c) => c.url.endsWith('/api/watches'))).toHaveLength(1))
    const body = sent('POST').find((c) => c.url.endsWith('/api/watches'))?.body as Record<string, unknown>
    expect('notify' in body).toBe(false)
    expect(body.send).toBe('findings')
  })

  it('sends the channels once the operator has chosen them', async () => {
    renderPage({ mode: newMode })
    const group = await screen.findByRole('group', { name: 'Send to' })
    await waitFor(() => expect(within(group).getByRole('checkbox', { name: 'ntfy' })).toBeChecked())
    fireEvent.click(within(group).getByRole('checkbox', { name: 'ntfy' }))
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Dawn check' } })
    await waitFor(() => expect(screen.getByRole('button', { name: 'Create watch' })).toBeEnabled())
    fireEvent.click(screen.getByRole('button', { name: 'Create watch' }))
    await waitFor(() => expect(sent('POST').filter((c) => c.url.endsWith('/api/watches'))).toHaveLength(1))
    expect(sent('POST').find((c) => c.url.endsWith('/api/watches'))?.body).toMatchObject({ notify: ['webpush'] })
  })

  it('a name typed before the channels arrive is kept, is not called a change made elsewhere, and the channels still fill in', async () => {
    let release: () => void = () => {}
    channelsGate = new Promise<void>((resolve) => { release = resolve })
    renderPage({ mode: newMode })
    fireEvent.change(await screen.findByLabelText('Name'), { target: { value: 'Dawn check' } })
    await act(async () => { release(); await Promise.resolve() })
    const group = await screen.findByRole('group', { name: 'Send to' })
    await waitFor(() => expect(within(group).getByRole('checkbox', { name: 'ntfy' })).toBeChecked())
    expect(screen.queryByText(/changed elsewhere/i)).not.toBeInTheDocument()
    expect(screen.getByLabelText('Name')).toHaveValue('Dawn check')
  })
})

describe('WatchDetailsPage report data from the server', () => {
  const sectionsRun = (sections: WatchReportSection[], over: Partial<WatchRun> = {}) =>
    run('r1', {
      report: { kind: 'sections', generated_at: '', boat_time: '', since: 'Wed 28 Oct 22:00 UTC+13', sections },
      ...over,
    })
  const rule = (text: string): WatchFinding => ({ section: 'alarms', rule: 'alarms_raised', text, by: 'rules' })
  const mate = (text: string, extend?: number): WatchFinding => ({ section: 'power', rule: 'mate', text, by: 'mate', ...(extend === undefined ? {} : { extends: extend }) })
  const show = async (r: WatchRun) => {
    detail = { ...detail, runs: [r] }
    const view = renderPage()
    const card = await screen.findByTestId('watch-run')
    return { card, ...view }
  }

  describe('the count', () => {
    it('counts only the findings that do not add to another', async () => {
      await show(run('r1', { findings: [rule('Alarms'), mate('Adds to the first', 0), rule('Tanks'), mate('Alone')] }))
      expect(triggerOf(runItem('r1'))).toHaveTextContent('3 to look at')
      expect(triggerOf(runItem('r1'))).not.toHaveTextContent('findings')
    })

    it('says 1 to look at for a single one, in the amber the findings badge always had', async () => {
      await show(run('r1', { findings: [rule('Alarms'), mate('Adds', 0)] }))
      const badge = within(triggerOf(runItem('r1'))).getByText('1 to look at')
      expect(badge.className).toContain('text-amber-700')
    })

    it('shows no count with no findings', async () => {
      await show(run('r1', { findings: [] }))
      expect(triggerOf(runItem('r1'))).not.toHaveTextContent('to look at')
    })
  })

  describe('Needs a look', () => {
    it('puts each finding that adds to another directly under it, then their charts problems, before the next finding', async () => {
      const { card } = await show(run('r1', {
        findings: [
          rule('Rule one'),
          { ...mate('Mate adds A', 0), chart_problem: 'Chart A could not be drawn' },
          rule('Rule two'),
          mate('Mate adds B', 0),
        ],
      }))
      const groups = within(card).getAllByTestId('watch-finding-group')
      expect(groups).toHaveLength(2)
      expect(groups[0]).toHaveTextContent('Rule one')
      expect(groups[0]).toHaveTextContent('Mate adds A')
      expect(groups[0]).toHaveTextContent('Mate adds B')
      expect(groups[0]).not.toHaveTextContent('Rule two')
      const text = groups[0].textContent ?? ''
      expect(text.indexOf('Rule one')).toBeLessThan(text.indexOf('Mate adds A'))
      expect(text.indexOf('Mate adds A')).toBeLessThan(text.indexOf('Mate adds B'))
      expect(text.indexOf('Mate adds B')).toBeLessThan(text.indexOf('Chart A could not be drawn'))
      expect(groups[1]).toHaveTextContent('Rule two')
      // The parent is the large line, the nested ones the small line with Mate's badge.
      expect(within(groups[0]).getByText('Rule one').className).toContain('text-base')
      expect(within(groups[0]).getByText('Rule one').className).toContain('font-medium')
      const nested = within(groups[0]).getByText('Mate adds A').closest('div')!
      expect(nested.className).toContain('text-sm')
      expect(within(nested).getByText('Mate')).toBeInTheDocument()
      // A divider and a gap between groups, none above the first.
      expect(groups[1].className).toContain('border-t')
      expect(groups[0].className).not.toContain('border-t')
    })

    it('shows a finding whose extends matches no finding as its own item, with the Mate badge', async () => {
      const { card } = await show(run('r1', { findings: [rule('Only rule'), mate('Orphan', 7)] }))
      const groups = within(card).getAllByTestId('watch-finding-group')
      expect(groups).toHaveLength(2)
      const orphan = within(card).getByText('Orphan')
      expect(orphan.closest('[data-testid="watch-finding-group"]')).toBe(groups[1])
      expect(within(groups[1]).getByText('Mate')).toBeInTheDocument()
      expect(orphan.closest('div')!.className).toContain('text-sm')
    })
  })

  describe('Not checked', () => {
    const unavailable: WatchReportSection[] = [
      { name: 'power', title: 'Power', lines: ['House bank 84% at 13.1 V'] },
      { name: 'weather', title: 'Weather', unavailable: 'pressure history still filling' },
      { name: 'tanks', title: 'Tanks', lines: ['Fuel 60 %'], not_checked: ['Tanks: Grey water is not measured', 'Nothing else was skipped'] },
    ]

    it('lists each unavailable section, then its not_checked sentences, in report order, with the part before the colon in medium weight', async () => {
      const { card } = await show(sectionsRun(unavailable))
      const box = within(card).getByTestId('watch-run-not-checked')
      expect(within(box).getByText('Not checked')).toBeInTheDocument()
      const sentences = [...box.querySelectorAll('li')].map((li) => li.textContent)
      expect(sentences).toEqual([
        'Weather: pressure history still filling',
        'Tanks: Grey water is not measured',
        'Nothing else was skipped',
      ])
      expect(within(box).getByText('Weather').className).toContain('font-medium')
      expect(within(box).getByText('Weather').tagName).toBe('SPAN')
      expect(within(box).getByText('Nothing else was skipped').className).toContain('text-sm')
      expect(box.className).toContain('border-dashed')
      expect(box.className).toContain('border-muted-foreground/60')
      expect(box.className).not.toContain('amber')
    })

    it('keeps Unavailable in the section’s own place under Readings as well', async () => {
      const { card } = await show(sectionsRun(unavailable))
      const readings = within(card).getByTestId('watch-sections-report')
      expect(within(readings).getByText('Unavailable: pressure history still filling')).toBeInTheDocument()
    })

    it('sits directly after Needs a look and before Mate’s review, and is not inside a folding block', async () => {
      const { card } = await show(sectionsRun(unavailable, { review_rest: 'Rest of the review.' }))
      const text = card.textContent ?? ''
      const at = ['Needs a look', 'Not checked', "Mate's review", 'Readings'].map((s) => text.indexOf(s))
      expect(at.every((i) => i >= 0)).toBe(true)
      expect([...at].sort((a, b) => a - b)).toEqual(at)
      expect(within(card).getByTestId('watch-run-not-checked').closest('[data-state]')).toBeNull()
    })

    it('shows on an old run, which has only unavailable sections to go on', async () => {
      const { card } = await show(run('r1'))
      expect(within(card).getByTestId('watch-run-not-checked')).toHaveTextContent('Weather: pressure history still filling')
    })

    it('is absent when nothing was left unchecked', async () => {
      const { card } = await show(sectionsRun([{ name: 'power', title: 'Power', lines: ['House bank 84%'] }]))
      expect(within(card).queryByTestId('watch-run-not-checked')).not.toBeInTheDocument()
      expect(within(card).queryByText('Not checked')).not.toBeInTheDocument()
    })
  })

  describe('Mate’s questions', () => {
    const asked = (over: Partial<WatchRun> = {}) =>
      run('r1', {
        review: 'Opening. Rest of the review.',
        review_opening: 'Opening.',
        review_rest: 'Rest of the review.',
        questions: ['Did you run the watermaker?', 'Is the fridge door sticking?'],
        ...over,
      })

    it('lists each question in a row with an Answer button, under the review', async () => {
      const { card } = await show(asked())
      const review = within(card).getByTestId('watch-run-review')
      expect(review).toHaveTextContent('Rest of the review.')
      const question = within(review).getByText('Did you run the watermaker?')
      expect(question.className).toContain('font-medium')
      expect(question.className).toContain('text-sm')
      const row = question.closest('[data-testid="watch-run-question"]') as HTMLElement
      expect(row.className).toContain('bg-muted')
      expect(row.className).toContain('rounded-md')
      expect(within(row).getByRole('button', { name: /^Answer/ }).className).toContain('min-h-11')
      expect(within(review).getAllByRole('button', { name: /^Answer/ })).toHaveLength(2)
      const text = review.textContent ?? ''
      expect(text.indexOf('Rest of the review.')).toBeLessThan(text.indexOf('Did you run the watermaker?'))
    })

    it('shows the section when only questions exist', async () => {
      const { card } = await show(asked({ review_rest: undefined }))
      const review = within(card).getByTestId('watch-run-review')
      expect(review).toHaveTextContent('Is the fridge door sticking?')
    })

    it('shows no review section with neither', async () => {
      const { card } = await show(asked({ review_rest: undefined, questions: [] }))
      expect(within(card).queryByTestId('watch-run-review')).not.toBeInTheDocument()
    })

    it('hands Mate a draft for the question, tied to the run, and nothing is sent', async () => {
      const { card, onAskMate } = await show(asked())
      fireEvent.click(within(within(card).getAllByTestId('watch-run-question')[1]).getByRole('button', { name: /^Answer/ }))
      expect(onAskMate).toHaveBeenCalledTimes(1)
      expect(onAskMate).toHaveBeenCalledWith('About "Is the fridge door sticking?": ', {
        newConversation: true,
        watchRun: { id: 'r1', name: 'Overnight check' },
        draft: true,
      })
      expect(sent('POST').filter((c) => c.url.includes('/api/assistant'))).toHaveLength(0)
    })

    it('shows the questions but no Answer button to a read-only session', async () => {
      detail = { ...detail, runs: [asked()] }
      renderPage({ canWrite: false })
      const card = await screen.findByTestId('watch-run')
      expect(within(card).getByText('Did you run the watermaker?')).toBeInTheDocument()
      expect(within(card).queryByRole('button', { name: /^Answer/ })).not.toBeInTheDocument()
    })

    it('shows the questions but no Answer button when nothing can open Mate', () => {
      const listProps = { hasOlder: false, canWrite: false, loadingOlder: false, olderError: null, onShowOlder: () => {}, onDeleteRun: async () => {} }
      render(<WatchRunList runs={[asked()]} expandNewest {...listProps} />)
      expect(screen.getByText('Did you run the watermaker?')).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: /^Answer/ })).not.toBeInTheDocument()
    })
  })

  describe('Readings rows', () => {
    it('draws rows where the server sent them, and the lines where it did not', async () => {
      const { card } = await show(sectionsRun([
        {
          name: 'power', title: 'Power', lines: ['legacy power line'],
          rows: [
            { label: 'House bank', value: '84 %' },
            { label: 'Solar', value: 'not checked', not_checked: true },
            { label: 'Port alternator', value: '72 V', flagged: true, detail: 'Impossible reading' },
          ],
        },
        { name: 'anchor', title: 'Anchor', lines: ['Anchor watch is off'] },
      ]))
      const readings = within(card).getByTestId('watch-sections-report')
      const power = readings.querySelector('[data-section="power"]') as HTMLElement
      expect(power.querySelector('dl')).not.toBeNull()
      expect(power).not.toHaveTextContent('legacy power line')
      expect(within(power).getByText('House bank').className).toContain('text-muted-foreground')
      const value = within(power).getByText('84 %')
      expect(value.className).toContain('tabular-nums')
      expect(value.className).toContain('text-right')
      expect(within(power).getByText('not checked').className).toContain('font-medium')
      expect(within(power).getByText('72 V').className).toContain('text-amber-700')
      const detail = within(power).getByText('Impossible reading')
      expect(detail.className).toContain('text-xs')
      expect(detail.className).toContain('text-muted-foreground')
      expect(detail.className).toContain('col-span-2')
      const anchor = readings.querySelector('[data-section="anchor"]') as HTMLElement
      expect(anchor.querySelector('dl')).toBeNull()
      expect(within(anchor).getByText('Anchor watch is off')).toBeInTheDocument()
    })

    it('draws an old run’s sections from their lines, with no list of rows', async () => {
      const { card } = await show(run('r1'))
      const readings = within(card).getByTestId('watch-sections-report')
      expect(within(readings).getByText('House bank 84% at 13.1 V')).toBeInTheDocument()
      expect(readings.querySelector('dl')).toBeNull()
      expect(readings.querySelector('table')).toBeNull()
    })

    it('draws the tanks as a table with the change since the last report', async () => {
      const { card } = await show(sectionsRun([{
        name: 'tanks', title: 'Tanks', lines: ['legacy tank line'],
        rows: [
          { label: 'Fresh water STBD', value: '49 %', change: '−11', flagged: true },
          { label: 'Fresh water PORT', value: '80 %', change: 'no change' },
          { label: 'Fuel', value: '60 %', change: '+4' },
        ],
      }]))
      const table = within(card).getByRole('table')
      expect(within(table).getAllByRole('columnheader').map((h) => h.textContent)).toEqual(['Tank', 'Now', 'Since last report'])
      const rows = within(table).getAllByRole('row').slice(1)
      expect(rows).toHaveLength(3)
      const flagged = rows[0]
      expect(flagged.className).toContain('font-semibold')
      expect(within(flagged).getByText('−11').className).toContain('text-amber-700')
      expect(within(flagged).getByText('49 %').className).toContain('tabular-nums')
      expect(within(flagged).getByText('49 %').className).toContain('text-right')
      expect(within(rows[1]).getByText('no change').className).toContain('text-muted-foreground')
      expect(within(rows[2]).getByText('+4').className).not.toContain('amber')
      expect(card).not.toHaveTextContent('legacy tank line')
    })

    it('leaves out the change column when no tank has a change', async () => {
      const { card } = await show(sectionsRun([{
        name: 'tanks', title: 'Tanks',
        rows: [{ label: 'Fresh water STBD', value: '49 %' }, { label: 'Fuel', value: '60 %' }],
      }]))
      const table = within(card).getByRole('table')
      expect(within(table).getAllByRole('columnheader').map((h) => h.textContent)).toEqual(['Tank', 'Now'])
      expect(within(table).getByText('Fuel')).toBeInTheDocument()
    })

    it('draws the alarms as a list under a heading that carries the summary', async () => {
      const { card } = await show(sectionsRun([{
        name: 'alarms', title: 'Alarms', lines: ['legacy alarm line'], summary: '217 raised, none active now',
        rows: [
          { label: 'Collision Risk', value: '211', when: 'yesterday 9.47 AM to 4.39 PM', detail: '27 vessels; most Delphi 13' },
          { label: 'Depth', value: '6' },
        ],
      }]))
      const alarms = within(card).getByTestId('watch-sections-report').querySelector('[data-section="alarms"]') as HTMLElement
      const summary = within(alarms).getByText('217 raised, none active now')
      expect(summary.className).toContain('font-normal')
      expect(summary.className).toContain('text-muted-foreground')
      expect(within(alarms).getByText('Alarms').compareDocumentPosition(summary) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
      const items = within(alarms).getAllByRole('listitem')
      expect(items).toHaveLength(2)
      expect(within(items[0]).getByText('Collision Risk').className).toContain('font-medium')
      expect(within(items[0]).getByText('211')).toBeInTheDocument()
      expect(within(items[0]).getByText('yesterday 9.47 AM to 4.39 PM').className).toContain('text-xs')
      expect(within(items[0]).getByText('27 vessels; most Delphi 13').className).toContain('text-muted-foreground')
      expect(alarms).not.toHaveTextContent('legacy alarm line')
    })
  })
})

describe('a link to a run', () => {
  beforeEach(() => {
    Element.prototype.scrollIntoView = vi.fn()
  })

  it('pins a run older than the first page at the top, labelled, scrolls to it and highlights it for a moment', async () => {
    linkedRuns = { old: run('old', { boat_time: 'Mon 12 Oct 03:00 UTC+13' }) }
    renderPage({ linkedRunId: 'old' })
    const pinned = await screen.findByTestId('watch-linked-run')
    expect(within(pinned).getByText('Linked run')).toBeInTheDocument()
    expect(within(pinned).getByText('Mon 12 Oct 03:00 UTC+13')).toBeInTheDocument()
    expect(calls.some((c) => c.url.endsWith('/api/watches/w1/runs/old'))).toBe(true)
    await waitFor(() => expect(Element.prototype.scrollIntoView).toHaveBeenCalled())
    const card = within(pinned).getByTestId('watch-run')
    expect(card).toHaveAttribute('data-highlighted', 'true')
    expect(card.id).toBe('run-old')
    await waitFor(() => expect(card).toHaveAttribute('data-highlighted', 'false'))
    expect(screen.getAllByTestId('watch-run')).toHaveLength(2)
  })

  it('shows a linked run once even when it is on the first page', async () => {
    linkedRuns = { r1: run('r1') }
    renderPage({ linkedRunId: 'r1' })
    await screen.findByTestId('watch-linked-run')
    await waitFor(() => expect(screen.getAllByTestId('watch-run')).toHaveLength(1))
    expect(document.querySelectorAll('#run-r1')).toHaveLength(1)
  })

  it('says so when the linked run is gone', async () => {
    renderPage({ linkedRunId: 'missing' })
    expect(await screen.findByText(/linked run is no longer there/i)).toBeInTheDocument()
    expect(screen.getAllByTestId('watch-run')).toHaveLength(1)
    // Nothing is pinned, so the newest run is open as it is on any arrival.
    await waitFor(() => expect(triggerOf(runItem('r1'))).toHaveAttribute('aria-expanded', 'true'))
  })
})
