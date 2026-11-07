import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { AddToJobsButton } from '@/components/inventory/add-to-jobs-button'
import { AlarmsDrawer } from '@/components/alarms-drawer'
import { ChecklistRunner } from '@/components/documents/checklist-runner'
import { WatchRunCard } from '@/components/watches/watch-run-list'
import { useChecklistRun } from '@/hooks/use-checklist-run'
import type { WatchRun } from '@/lib/watches'
import type { ActiveAlarm } from '@/hooks/use-alarms'

// ADR 0270: "Add to jobs" on a watch report finding, an alarm card and a
// checklist line. Each opens the Add job form with the title, the link back
// and where it came from already filled in; nothing is saved until Save.

vi.mock('@/hooks/use-checklist-run')
vi.mock('@/hooks/use-alarm-rules', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/hooks/use-alarm-rules')>()),
  useAlarmLog: () => ({ entries: [], refresh: vi.fn().mockResolvedValue(undefined) }),
}))

let posted: { url: string; body: Record<string, unknown> } | undefined
const fetchMock = vi.fn()

beforeEach(() => {
  posted = undefined
  fetchMock.mockReset()
  fetchMock.mockImplementation(async (url: string, init?: RequestInit) => {
    const u = String(url)
    const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
    if (u.includes('/api/inventory/open-jobs') && init?.method === 'POST') {
      posted = { url: u, body: JSON.parse(String(init.body)) }
      return json({ job: { id: 'j1', photo_ids: [], ...posted.body } }, 201)
    }
    if (u.includes('/api/inventory/equipment')) return json({ items: [] })
    return json({})
  })
  vi.stubGlobal('fetch', fetchMock)
})

afterEach(() => { vi.unstubAllGlobals() })

async function saveFromDialog(titleExpected: string) {
  const dialog = await screen.findByRole('dialog')
  expect(within(dialog).getByLabelText('Title')).toHaveValue(titleExpected)
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(posted).toBeDefined())
  return posted!.body
}

describe('AddToJobsButton', () => {
  it('opens the form filled in and saves the source and the link back', async () => {
    render(<AddToJobsButton what="the bilge pump" draft={{ title: 'Bilge pump cycling', source: 'alarm', source_link: '/alarms', source_label: 'Bilge high' }} />)
    fireEvent.click(screen.getByRole('button', { name: 'Add the bilge pump to jobs' }))
    const body = await saveFromDialog('Bilge pump cycling')
    expect(body).toMatchObject({ title: 'Bilge pump cycling', source: 'alarm', source_link: '/alarms', source_label: 'Bilge high', equipment_id: null })
    expect(posted!.url).toMatch(/today=\d{4}-\d{2}-\d{2}/)
  })

  it('keeps a long finding whole in the notes and a title that fits', async () => {
    const long = `${'Port battery sagged overnight. '.repeat(10)}End.`
    render(<AddToJobsButton what="a finding" draft={{ title: long, source: 'watch_finding' }} />)
    fireEvent.click(screen.getByRole('button', { name: 'Add a finding to jobs' }))
    const dialog = await screen.findByRole('dialog')
    const title = (within(dialog).getByLabelText('Title') as HTMLInputElement).value
    expect(title.length).toBeLessThanOrEqual(200)
    expect(title.endsWith('…')).toBe(true)
    expect(within(dialog).getByLabelText('Notes')).toHaveValue(long)
  })
})

describe('Add to jobs on a watch report', () => {
  const run = {
    id: 'run-9', watch_id: 'w-1', started_at: '2026-11-06T06:00:00Z', finished_at: '2026-11-06T06:01:00Z', outcome: 'ran',
    report_kind: 'sections', findings: [{ section: 'power', rule: 'overnight_drain', text: 'House bank fell 18% overnight', by: 'rules' }],
    review: null, review_problem: null, cost_usd: null, cost_known: false, notified: [],
  } as unknown as WatchRun

  it('adds a finding with a link back to that report', async () => {
    render(<WatchRunCard run={run} />)
    fireEvent.click(screen.getByRole('button', { name: /add house bank fell 18% overnight to jobs/i }))
    const body = await saveFromDialog('House bank fell 18% overnight')
    expect(body).toMatchObject({ source: 'watch_finding', source_link: '/watches/w-1?run=run-9' })
    expect(String(body.source_label)).not.toBe('')
  })
})

describe('Add to jobs on an alarm card', () => {
  const alarm: ActiveAlarm = {
    rule_id: 'rule-1', label: 'Bilge high', path: 'notifications.bilge', phase: 'active', state: 'alarm', value: 0,
    message: 'Bilge water level is high', silenced: false, can_silence: true, can_acknowledge: true,
  } as ActiveAlarm

  it('adds the alarm with its words in the notes and a link to the alarms page', async () => {
    render(
      <AlarmsDrawer alarms={[alarm]} onAcknowledge={vi.fn()} onSilence={vi.fn()} rules={[]} loading={false} error={null}
        createRule={vi.fn()} updateRule={vi.fn()} deleteRule={vi.fn()} collisionTuningUrl={null} forecastWarnings={null} />,
    )
    fireEvent.click(screen.getByRole('button', { name: /add bilge high to jobs/i }))
    const body = await saveFromDialog('Bilge high')
    expect(body).toMatchObject({ source: 'alarm', source_link: '/alarms', source_label: 'Bilge high', notes: 'Bilge water level is high' })
  })
})

describe('Add to jobs on a checklist line', () => {
  function hook(closed = false) {
    return {
      run: {
        id: 'run-1', document_id: 'note-1', started_at: '2026-09-20T14:22:00Z', total: 2, checked_count: 0, changed: [],
        ...(closed ? { completed_at: '2026-09-20T15:00:00Z' } : {}),
        items: [
          { item_key: 'k1', occurrence: 0, text: 'Check bilge', depth: 0, checked: false },
          { item_key: 'k2', occurrence: 0, text: 'Start blower', depth: 0, checked: false },
        ],
      },
      resumed: false, loading: false, error: null, tick: vi.fn(), complete: vi.fn(), abandon: vi.fn(),
    } as unknown as ReturnType<typeof useChecklistRun>
  }

  it('adds a line with a link to its checklist, without ticking it', async () => {
    const h = hook()
    vi.mocked(useChecklistRun).mockReturnValue(h)
    render(<ChecklistRunner noteId="note-1" noteTitle="Shutdown" onExit={vi.fn()} />)
    fireEvent.click(screen.getByRole('button', { name: /add check bilge to jobs/i }))
    expect(h.tick).not.toHaveBeenCalled()
    const body = await saveFromDialog('Check bilge')
    expect(body).toMatchObject({ source: 'checklist_line', source_label: 'Shutdown' })
    expect(String(body.source_link)).toContain('note-1')
  })

  it('offers it only while the run is open', () => {
    vi.mocked(useChecklistRun).mockReturnValue(hook(true))
    render(<ChecklistRunner noteId="note-1" noteTitle="Shutdown" onExit={vi.fn()} />)
    expect(screen.queryByRole('button', { name: /to jobs/i })).not.toBeInTheDocument()
  })
})
