import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { OpenJobDialog, OpenJobDoneDialog } from '@/components/inventory/open-job-dialogs'
import { MaintenanceSection } from '@/components/inventory/maintenance-section'
import { completeOpenJob, type OpenJob } from '@/hooks/use-open-jobs'
import { useMaintenanceRules, type MaintenanceLogEntry } from '@/hooks/use-maintenance'
import { stubApi } from '@/test/helpers/stub-api'
import { testRouter } from './helpers/router'

// Fixes from the review of the Open jobs PR (ADR 0269): Done refreshes the
// rules, the Done dialog reports unsaved work only once touched, a failed
// photo upload keeps the photos already linked, and lines with no item are
// listed in a boat-wide Service log.

function makeJob(overrides: Partial<OpenJob> = {}): OpenJob {
  return {
    id: 'job-1', equipment_id: 'eq-1', equipment_name: 'Shower pump', title: 'Leaks at the outlet', notes: '',
    opened_on: '2026-10-26', before_on: '', status: 'open', closed_on: '', drop_reason: '', log_entry_id: null,
    source: 'manual', source_link: '', source_label: '', created_at: '', updated_at: '', photo_ids: [],
    ...overrides,
  }
}

function makeEntry(overrides: Partial<MaintenanceLogEntry> = {}): MaintenanceLogEntry {
  return {
    id: 'log-1', equipment_id: null, rule_id: null, performed_at: '2026-11-05', hours: null, kind: 'repair',
    description: 'Renewed the flares', who: '', cost: null, currency: '', created_at: '', updated_at: '', parts: [], photo_ids: [],
    ...overrides,
  }
}

const JOBS = 'GET /api/inventory/open-jobs'
const EQUIPMENT = 'GET /api/inventory/equipment'
const RULES = 'GET /api/inventory/maintenance/rules'
const NO_ITEM_LOG = 'GET /api/inventory/maintenance/log?no_item=true'

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(2026, 10, 7, 12))
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('Done on an open job', () => {
  it('refreshes the maintenance rules, since the line it writes can move one', async () => {
    let ruleReads = 0
    stubApi({
      [RULES]: () => {
        ruleReads += 1
        return { rules: [], removed: [], schedule_errors: [] }
      },
      'POST /api/inventory/open-jobs/job-1/done': { job: makeJob({ status: 'done' }), entry: makeEntry() },
    })
    function Harness() {
      useMaintenanceRules({})
      return null
    }
    render(<Harness />)
    await waitFor(() => expect(ruleReads).toBe(1))

    await completeOpenJob('job-1', { performed_at: '2026-11-07', kind: 'repair', hours: null, description: '', who: '', cost: null, currency: '', parts: [] })
    await waitFor(() => expect(ruleReads).toBe(2))
  })

  it('sends the operators date, which the server checks the line against', async () => {
    let url = ''
    stubApi({
      'POST /api/inventory/open-jobs/job-1/done': (req: { url: string }) => {
        url = req.url
        return { job: makeJob({ status: 'done' }), entry: makeEntry() }
      },
    })
    await completeOpenJob('job-1', { performed_at: '2026-11-07', kind: 'repair', hours: null, description: '', who: '', cost: null, currency: '', parts: [] })
    expect(url).toContain('today=2026-11-07')
  })
})

describe('the Done dialog and unsaved work', () => {
  it('reports unsaved work only once the form is changed', async () => {
    stubApi({ [EQUIPMENT]: { items: [] } })
    const { wrapper, router } = testRouter('/inventory')
    render(<OpenJobDoneDialog job={makeJob()} onClose={vi.fn()} />, { wrapper })
    const dialog = await screen.findByRole('dialog')
    expect(router.hasGuards()).toBe(false)

    fireEvent.change(within(dialog).getByLabelText('Notes'), { target: { value: 'New fitting' } })
    expect(router.hasGuards()).toBe(true)
  })
})

describe('adding photos to a job', () => {
  it('shows the photos that were added and names the one that failed', async () => {
    let uploads = 0
    stubApi({
      [EQUIPMENT]: { items: [] },
      'POST /api/inventory/open-jobs/job-1/photos': () => {
        uploads += 1
        if (uploads === 2) return new Response(JSON.stringify({ error: 'only JPEG or PNG photos are accepted' }), { status: 400, headers: { 'Content-Type': 'application/json' } })
        return { job: makeJob({ photo_ids: ['doc-a'] }) }
      },
    })
    render(<OpenJobDialog open job={makeJob()} onClose={vi.fn()} />)
    const dialog = await screen.findByRole('dialog')
    const a = new File(['a'], 'a.jpg', { type: 'image/jpeg' })
    const b = new File(['b'], 'b.gif', { type: 'image/gif' })
    fireEvent.change(within(dialog).getByLabelText('Add from library'), { target: { files: [a, b] } })

    expect(await within(dialog).findByText(/b\.gif/)).toBeInTheDocument()
    expect(within(dialog).getByText(/only JPEG or PNG/)).toBeInTheDocument()
    await waitFor(() => expect(dialog.querySelectorAll('img')).toHaveLength(1))
  })
})

describe('the boat-wide Service log', () => {
  function stubMaintenance(extra: Record<string, unknown> = {}) {
    return stubApi({
      [JOBS]: { jobs: [] },
      [RULES]: { rules: [], removed: [], schedule_errors: [] },
      [EQUIPMENT]: { items: [] },
      [NO_ITEM_LOG]: { entries: [makeEntry(), makeEntry({ id: 'log-2', description: 'Calendar rule done', rule_id: 'rule-9', performed_at: '2026-10-01' })] },
      ...extra,
    })
  }

  it('lists the lines that have no item, newest first as the server sends them', async () => {
    const fetchMock = stubMaintenance()
    render(<MaintenanceSection onOpenEquipment={vi.fn()} />)
    fireEvent.click(await screen.findByRole('button', { name: /service log/i }))
    expect(await screen.findByText('Renewed the flares')).toBeInTheDocument()
    expect(screen.getByText('Calendar rule done')).toBeInTheDocument()
    expect(fetchMock.mock.calls.some(([u]) => String(u).includes('/api/inventory/maintenance/log?no_item=true'))).toBe(true)
  })

  it('edits a line and deletes one', async () => {
    let put: { url: string; body: unknown } | undefined
    let deleted = ''
    stubMaintenance({
      'PUT /api/inventory/maintenance/log/log-1': (req: { url: string; body: unknown }) => {
        put = req
        return { entry: makeEntry({ description: 'Renewed the flares and EPIRB' }) }
      },
      'DELETE /api/inventory/maintenance/log/log-2': (req: { url: string }) => {
        deleted = req.url
        return undefined
      },
    })
    render(<MaintenanceSection onOpenEquipment={vi.fn()} />)
    fireEvent.click(await screen.findByRole('button', { name: /service log/i }))

    fireEvent.click(await screen.findByRole('button', { name: /renewed the flares/i }))
    let dialog = await screen.findByRole('dialog')
    fireEvent.change(within(dialog).getByLabelText('Description'), { target: { value: 'Renewed the flares and EPIRB' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(put).toBeDefined())
    expect(put!.body).toMatchObject({ description: 'Renewed the flares and EPIRB', performed_at: '2026-11-05', kind: 'repair' })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Done' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: /calendar rule done/i }))
    dialog = await screen.findByRole('dialog')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(deleted).toContain('/api/inventory/maintenance/log/log-2'))
  })

  it('is left out when every line belongs to an item', async () => {
    stubMaintenance({ [NO_ITEM_LOG]: { entries: [] } })
    render(<MaintenanceSection onOpenEquipment={vi.fn()} />)
    await screen.findByText(/no maintenance rules yet/i)
    await waitFor(() => expect(screen.queryByRole('button', { name: /service log/i })).not.toBeInTheDocument())
  })
})
