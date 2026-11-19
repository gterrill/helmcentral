import { afterEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { JobsPage } from '@/components/jobs/jobs-page'
import { MaintenanceSection } from '@/components/inventory/maintenance-section'
import { maintenanceRulesQuery } from '@/hooks/use-maintenance'
import { formatAppLocation } from '@/lib/app-location'
import { openJobsQuery } from '@/hooks/use-open-jobs'
import { refetchQuery } from '@/lib/api-query'
import { stubApi } from '@/test/helpers/stub-api'
import { setViewportWidth } from './viewport'
import { makeProfileJob } from './maintenance-fixtures'

// ADR 0293: Jobs, the "what needs doing on the boat" page. Open jobs and the
// services that are overdue or due soon, in one place.

vi.mock('@/hooks/use-vessel-clock', () => ({ useBoatDayKey: () => '2026-11-19' }))

afterEach(() => { vi.unstubAllGlobals() })

const JOB = {
  id: 'job-1', equipment_id: 'eq-1', equipment_name: 'Main engine', title: 'Weeping hose clamp', notes: '',
  opened_on: '2026-11-01', before_on: '', status: 'open', closed_on: '', drop_reason: '', log_entry_id: null,
  source: 'manual', source_link: '', source_label: '', created_at: '', updated_at: '', photo_ids: [],
}

const RULES = [
  makeProfileJob({ id: 'r-ok', description: 'Raw water impeller', status: 'ok', remaining_hours: 120 }),
  makeProfileJob({ id: 'r-soon', description: 'Fuel filter', status: 'due_soon', remaining_hours: 12 }),
  makeProfileJob({ id: 'r-over', description: 'Engine oil', status: 'overdue', remaining_hours: -10 }),
  makeProfileJob({ id: 'r-ack', description: 'Anode check', status: 'overdue', acknowledged: true, ack_reason: 'yard' }),
  makeProfileJob({ id: 'r-never', description: 'Never logged', status: 'never_recorded' }),
]

const CLOSED_JOB = {
  ...JOB, id: 'job-2', title: 'Bilge pump float', status: 'done', closed_on: '2026-11-10',
}

function routes(jobs: unknown[], rules: unknown[]) {
  return {
    'GET /api/inventory/open-jobs': { jobs },
    'GET /api/inventory/maintenance/rules': { rules, removed: [], schedule_errors: [] },
  }
}

describe('JobsPage', () => {
  it('lists open jobs and only the services that are overdue or due soon, overdue first', async () => {
    stubApi(routes([JOB], RULES))
    render(<JobsPage onOpenEquipment={vi.fn()} onOpenServiceSchedule={vi.fn()} />)

    expect(await screen.findByRole('heading', { name: 'Jobs' })).toBeInTheDocument()
    expect(await screen.findByText('Weeping hose clamp')).toBeInTheDocument()
    await screen.findByText('Engine oil')
    expect(screen.getByText('Fuel filter')).toBeInTheDocument()
    expect(screen.queryByText('Raw water impeller')).not.toBeInTheDocument()
    expect(screen.queryByText('Never logged')).not.toBeInTheDocument()

    const order = screen.getAllByText(/^(Engine oil|Anode check|Fuel filter)$/).map((el) => el.textContent)
    expect(order).toEqual(['Engine oil', 'Anode check', 'Fuel filter'])
  })

  it('shows an acknowledged service with its Ack badge, after the unacknowledged ones', async () => {
    stubApi(routes([], RULES))
    render(<JobsPage onOpenEquipment={vi.fn()} onOpenServiceSchedule={vi.fn()} />)
    const row = (await screen.findByText('Anode check')).closest('tr') as HTMLElement
    expect(within(row).getByTitle('yard')).toHaveTextContent('Ack')
  })

  it('has one Add job button, in the page header, and it opens the new job dialog', async () => {
    stubApi(routes([JOB], RULES))
    render(<JobsPage onOpenEquipment={vi.fn()} onOpenServiceSchedule={vi.fn()} />)
    await screen.findByText('Weeping hose clamp')
    const buttons = screen.getAllByRole('button', { name: /add job/i })
    expect(buttons).toHaveLength(1)
    fireEvent.click(buttons[0])
    expect(await screen.findByRole('dialog')).toBeInTheDocument()
  })

  it('hides Add job from a read-only user', async () => {
    stubApi(routes([JOB], RULES))
    render(<JobsPage onOpenEquipment={vi.fn()} onOpenServiceSchedule={vi.fn()} canWrite={false} />)
    await screen.findByText('Weeping hose clamp')
    expect(screen.queryByRole('button', { name: /add job/i })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Complete' })).not.toBeInTheDocument()
  })

  it('offers Complete and Ack on a due service', async () => {
    stubApi(routes([], RULES))
    render(<JobsPage onOpenEquipment={vi.fn()} onOpenServiceSchedule={vi.fn()} />)
    const row = (await screen.findByText('Engine oil')).closest('tr') as HTMLElement
    expect(within(row).getByRole('button', { name: 'Complete' })).toBeInTheDocument()
    expect(within(row).getByRole('button', { name: 'Ack' })).toBeInTheDocument()
  })

  it('links to the Service schedule, at its own address, and leaves a new-tab click to the browser', async () => {
    stubApi(routes([JOB], RULES))
    const open = vi.fn()
    render(<JobsPage onOpenEquipment={vi.fn()} onOpenServiceSchedule={open} />)
    await screen.findByText('Weeping hose clamp')
    const link = screen.getByRole('link', { name: 'Service schedule' })
    expect(link).toHaveAttribute('href', formatAppLocation({ panel: 'inventory', route: { view: 'index', section: 'maintenance' } }, { firstPageId: null }))
    fireEvent.click(link, { ctrlKey: true })
    expect(open).not.toHaveBeenCalled()
    fireEvent.click(link)
    expect(open).toHaveBeenCalledTimes(1)
  })

  it('says so when nothing is open or due, with Add job', async () => {
    stubApi(routes([], [RULES[0], RULES[4]]))
    render(<JobsPage onOpenEquipment={vi.fn()} onOpenServiceSchedule={vi.fn()} />)
    expect(await screen.findByText('Nothing needs doing.')).toBeInTheDocument()
    expect(screen.getByText('Open jobs and services coming due show here.')).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: /add job/i }).length).toBeGreaterThanOrEqual(1)
  })

  it('still offers Show closed when nothing is open or due, so closed jobs can be reached', async () => {
    stubApi({
      'GET /api/inventory/open-jobs': (req: { url: string }) =>
        req.url.includes('status=any') ? { jobs: [CLOSED_JOB] } : { jobs: [] },
      'GET /api/inventory/maintenance/rules': { rules: [RULES[0]], removed: [], schedule_errors: [] },
    })
    render(<JobsPage onOpenEquipment={vi.fn()} onOpenServiceSchedule={vi.fn()} />)
    expect(await screen.findByText('Nothing needs doing.')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Show closed' }))
    const list = await screen.findByRole('list', { name: 'Closed jobs' })
    expect(within(list).getByText('Bilge pump float')).toBeInTheDocument()
    expect(screen.getByText('Nothing needs doing.')).toBeInTheDocument()
  })

  it('lists the items whose profile is missing, so the services of an item that cannot be listed are not silently absent', async () => {
    stubApi({
      'GET /api/inventory/open-jobs': { jobs: [] },
      'GET /api/inventory/maintenance/rules': {
        rules: [], removed: [],
        schedule_errors: [{ equipment_id: 'eq-9', equipment_name: 'Watermaker', profile_id: 'gone', error: 'profile not found' }],
      },
    })
    const onOpenEquipment = vi.fn()
    render(<JobsPage onOpenEquipment={onOpenEquipment} onOpenServiceSchedule={vi.fn()} />)
    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('One item has a profile that is missing or invalid')
    expect(screen.queryByText(/nothing needs doing/i)).not.toBeInTheDocument()
    fireEvent.click(within(alert).getByRole('button', { name: 'Watermaker' }))
    expect(onOpenEquipment).toHaveBeenCalledWith('eq-9')
  })

  it('does not say "No services due." under the alert for an unreadable schedule', async () => {
    stubApi({
      'GET /api/inventory/open-jobs': { jobs: [JOB] },
      'GET /api/inventory/maintenance/rules': {
        rules: [], removed: [],
        schedule_errors: [{ equipment_id: 'eq-9', equipment_name: 'Watermaker', profile_id: 'gone', error: 'profile not found' }],
      },
    })
    render(<JobsPage onOpenEquipment={vi.fn()} onOpenServiceSchedule={vi.fn()} />)
    await screen.findByRole('alert')
    expect(await screen.findByText('Nothing else listed is due.')).toBeInTheDocument()
    expect(screen.queryByText('No services due.')).not.toBeInTheDocument()
  })

  it('says "No services due." when the schedule read cleanly and nothing is due', async () => {
    stubApi(routes([JOB], [RULES[0]]))
    render(<JobsPage onOpenEquipment={vi.fn()} onOpenServiceSchedule={vi.fn()} />)
    expect(await screen.findByText('No services due.')).toBeInTheDocument()
  })

  it('puts a failed re-read of the services in the Services due section, not above Open jobs', async () => {
    let failing = false
    stubApi({
      'GET /api/inventory/open-jobs': { jobs: [JOB] },
      'GET /api/inventory/maintenance/rules': () =>
        failing ? new Response(JSON.stringify({ error: 'boom' }), { status: 500, headers: { 'Content-Type': 'application/json' } })
          : { rules: RULES, removed: [], schedule_errors: [] },
    })
    render(<JobsPage onOpenEquipment={vi.fn()} onOpenServiceSchedule={vi.fn()} />)
    await screen.findByText('Engine oil')
    failing = true
    const query = maintenanceRulesQuery({})
    await refetchQuery(query.queryKey, query.path)
    const note = await screen.findByText(/Couldn't refresh: boom/)
    expect(note.closest('section[aria-labelledby="services-due-heading"]')).not.toBeNull()
    expect(note.closest('section[aria-labelledby="open-jobs-heading"]')).toBeNull()
  })

  it('puts a failed re-read of the open jobs in the Open jobs group, not in Services due', async () => {
    let failing = false
    stubApi({
      'GET /api/inventory/open-jobs': () =>
        failing ? new Response(JSON.stringify({ error: 'boom' }), { status: 500, headers: { 'Content-Type': 'application/json' } })
          : { jobs: [JOB] },
      'GET /api/inventory/maintenance/rules': { rules: RULES, removed: [], schedule_errors: [] },
    })
    render(<JobsPage onOpenEquipment={vi.fn()} onOpenServiceSchedule={vi.fn()} />)
    await screen.findByText('Weeping hose clamp')
    failing = true
    const query = openJobsQuery({})
    await refetchQuery(query.queryKey, query.path)
    const note = await screen.findByText(/Couldn't refresh: boom/)
    expect(note.closest('section[aria-labelledby="open-jobs-heading"]')).not.toBeNull()
    expect(note.closest('section[aria-labelledby="services-due-heading"]')).toBeNull()
  })

  it('says a re-read of the services failed while the old rows stay on screen', async () => {
    let failing = false
    stubApi({
      'GET /api/inventory/open-jobs': { jobs: [] },
      'GET /api/inventory/maintenance/rules': () =>
        failing ? new Response(JSON.stringify({ error: 'boom' }), { status: 500, headers: { 'Content-Type': 'application/json' } })
          : { rules: RULES, removed: [], schedule_errors: [] },
    })
    render(<JobsPage onOpenEquipment={vi.fn()} onOpenServiceSchedule={vi.fn()} />)
    await screen.findByText('Engine oil')
    failing = true
    const query = maintenanceRulesQuery({})
    await refetchQuery(query.queryKey, query.path)
    expect(await screen.findByText(/Couldn't refresh: boom/)).toBeInTheDocument()
    expect(screen.getByText('Engine oil')).toBeInTheDocument()
  })

  it('draws each service as a card on a phone, with its figures and actions', async () => {
    setViewportWidth(390)
    stubApi(routes([], RULES))
    render(<JobsPage onOpenEquipment={vi.fn()} onOpenServiceSchedule={vi.fn()} />)
    const card = (await screen.findByText('Engine oil')).closest('[data-slot="service-card"]') as HTMLElement
    expect(card).not.toBeNull()
    expect(card.closest('table')).toBeNull()
    expect(within(card).getByText('Remaining')).toBeInTheDocument()
    expect(within(card).getByText('Last done')).toBeInTheDocument()
    expect(within(card).getByRole('button', { name: 'Ack' })).toBeInTheDocument()
    fireEvent.click(within(card).getByRole('button', { name: 'Complete' }))
    expect(await screen.findByRole('dialog')).toBeInTheDocument()
  })

  it('does not call it empty when a failed read left the answer unknown', async () => {
    stubApi({
      'GET /api/inventory/open-jobs': () => new Response(JSON.stringify({ error: 'boom' }), { status: 500, headers: { 'Content-Type': 'application/json' } }),
      'GET /api/inventory/maintenance/rules': { rules: [], removed: [], schedule_errors: [] },
    })
    render(<JobsPage onOpenEquipment={vi.fn()} onOpenServiceSchedule={vi.fn()} />)
    await waitFor(() => expect(screen.getByRole('alert')).toBeInTheDocument())
    expect(screen.queryByText(/nothing needs doing/i)).not.toBeInTheDocument()
  })
})

describe('the Service schedule section', () => {
  it('no longer carries the Open jobs group, and reads Service schedule', async () => {
    stubApi(routes([JOB], RULES))
    render(<MaintenanceSection onOpenEquipment={vi.fn()} />)
    expect(await screen.findByRole('heading', { name: 'Service schedule' })).toBeInTheDocument()
    await screen.findByText('Raw water impeller')
    expect(screen.queryByRole('heading', { name: /open jobs/i })).not.toBeInTheDocument()
    expect(screen.queryByText('Weeping hose clamp')).not.toBeInTheDocument()
  })
})
