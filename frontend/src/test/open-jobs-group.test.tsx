import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { OpenJobsGroup } from '@/components/inventory/open-jobs-group'
import type { OpenJob } from '@/hooks/use-open-jobs'
import { openJobAgeDays } from '@/hooks/use-open-jobs'
import { stubApi } from '@/test/helpers/stub-api'

// ADR 0269: the Open jobs group at the top of Inventory, Maintenance and on
// an item's page. A list of known problems with an age and a before-date,
// Done (the service log form) and Dropped (one tap, optional reason).

function makeJob(overrides: Partial<OpenJob> = {}): OpenJob {
  return {
    id: 'job-1',
    equipment_id: 'eq-1',
    equipment_name: 'Shower pump',
    title: 'Leaks at the outlet',
    notes: '',
    opened_on: '2026-10-26',
    before_on: '',
    status: 'open',
    closed_on: '',
    drop_reason: '',
    log_entry_id: null,
    source: 'manual',
    source_link: '',
    source_label: '',
    created_at: '2026-10-26T00:00:00Z',
    updated_at: '2026-10-26T00:00:00Z',
    photo_ids: [],
    ...overrides,
  }
}

const JOBS = 'GET /api/inventory/open-jobs'
const EQUIPMENT = 'GET /api/inventory/equipment'

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(2026, 10, 7, 12))
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('openJobAgeDays', () => {
  it('counts whole days from the day it opened', () => {
    expect(openJobAgeDays(makeJob({ opened_on: '2026-10-26' }), '2026-11-07')).toBe(12)
    expect(openJobAgeDays(makeJob({ opened_on: '2026-11-07' }), '2026-11-07')).toBe(0)
  })

  it('stops counting on the day a closed job was closed', () => {
    expect(openJobAgeDays(makeJob({ status: 'done', opened_on: '2026-10-26', closed_on: '2026-11-01' }), '2026-11-07')).toBe(6)
  })

  it('is never negative', () => {
    expect(openJobAgeDays(makeJob({ opened_on: '2026-11-09' }), '2026-11-07')).toBe(0)
  })
})

describe('OpenJobsGroup', () => {
  it('lists each open job with its item, age and before date', async () => {
    stubApi({
      [JOBS]: { jobs: [makeJob({ before_on: '2026-12-01' }), makeJob({ id: 'job-2', title: 'Flares expire', equipment_id: null, equipment_name: '', opened_on: '2026-11-05' })] },
    })
    render(<OpenJobsGroup onOpenEquipment={vi.fn()} />)

    expect(await screen.findByText('Leaks at the outlet')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: /open jobs/i })).toBeInTheDocument()
    expect(screen.getByText('Shower pump')).toBeInTheDocument()
    expect(screen.getByText('12 d')).toBeInTheDocument()
    expect(screen.getByText('1 Dec')).toBeInTheDocument()
    expect(screen.getByText('Flares expire')).toBeInTheDocument()
    expect(screen.getByText('2 d')).toBeInTheDocument()
  })

  it('notes a before date that has passed', async () => {
    stubApi({ [JOBS]: { jobs: [makeJob({ before_on: '2026-11-01' })] } })
    render(<OpenJobsGroup onOpenEquipment={vi.fn()} />)
    expect(await screen.findByText('1 Nov (passed)')).toBeInTheDocument()
  })

  it('asks for the open jobs of the chosen system, or of one item', async () => {
    const fetchMock = stubApi({ [JOBS]: { jobs: [] } })
    const { unmount } = render(<OpenJobsGroup system="electrical" onOpenEquipment={vi.fn()} />)
    await waitFor(() => expect(fetchMock).toHaveBeenCalled())
    expect(String(fetchMock.mock.calls[0][0])).toContain('system=electrical')
    unmount()

    fetchMock.mockClear()
    render(<OpenJobsGroup equipmentId="eq-9" onOpenEquipment={vi.fn()} />)
    await waitFor(() => expect(fetchMock).toHaveBeenCalled())
    expect(String(fetchMock.mock.calls[0][0])).toContain('equipment=eq-9')
  })

  it('says there are none and still offers Add job', async () => {
    stubApi({ [JOBS]: { jobs: [] } })
    render(<OpenJobsGroup onOpenEquipment={vi.fn()} />)
    expect(await screen.findByText(/no open jobs/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /add job/i })).toBeInTheDocument()
  })

  it('shows the way back to where a job came from', async () => {
    stubApi({
      [JOBS]: { jobs: [makeJob({ source: 'alarm', source_link: '/alarms?alarm=abc', source_label: 'Bilge high' })] },
    })
    render(<OpenJobsGroup onOpenEquipment={vi.fn()} />)
    const link = await screen.findByRole('link', { name: /bilge high/i })
    expect(link).toHaveAttribute('href', '/alarms?alarm=abc')
  })

  it('says where a job came from when there is no link back', async () => {
    stubApi({ [JOBS]: { jobs: [makeJob({ source: 'mate' })] } })
    render(<OpenJobsGroup onOpenEquipment={vi.fn()} />)
    expect(await screen.findByText(/added by mate/i)).toBeInTheDocument()
  })

  it('opens the item a job is on', async () => {
    stubApi({ [JOBS]: { jobs: [makeJob()] } })
    const onOpenEquipment = vi.fn()
    render(<OpenJobsGroup onOpenEquipment={onOpenEquipment} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Shower pump' }))
    expect(onOpenEquipment).toHaveBeenCalledWith('eq-1')
  })

  it('adds a job on the operators date, on the item when scoped to one', async () => {
    let posted: { url: string; body: unknown } | undefined
    stubApi({
      [JOBS]: { jobs: [] },
      [EQUIPMENT]: { items: [{ id: 'eq-9', name: 'Bilge pump' }] },
      'POST /api/inventory/open-jobs': (req: { url: string; body: unknown }) => {
        posted = req
        return { job: makeJob({ id: 'new', title: 'Cycles too often', equipment_id: 'eq-9' }) }
      },
    })
    render(<OpenJobsGroup equipmentId="eq-9" onOpenEquipment={vi.fn()} />)
    await screen.findByText(/no open jobs/i)

    fireEvent.click(screen.getByRole('button', { name: /add job/i }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.change(within(dialog).getByLabelText('Title'), { target: { value: 'Cycles too often' } })
    fireEvent.change(within(dialog).getByLabelText('Before'), { target: { value: '2026-12-01' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(posted).toBeDefined())
    expect(posted!.url).toContain('today=2026-11-07')
    expect(posted!.body).toMatchObject({ title: 'Cycles too often', equipment_id: 'eq-9', before_on: '2026-12-01' })
  })

  it('will not save a job with no title', async () => {
    const fetchMock = stubApi({ [JOBS]: { jobs: [] }, [EQUIPMENT]: { items: [] } })
    render(<OpenJobsGroup onOpenEquipment={vi.fn()} />)
    await screen.findByText(/no open jobs/i)

    fireEvent.click(screen.getByRole('button', { name: /add job/i }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByRole('button', { name: 'Save' })).toBeDisabled()
    expect(fetchMock.mock.calls.some(([, init]) => (init as RequestInit | undefined)?.method === 'POST')).toBe(false)
  })

  it('shows the servers reason when a save is refused', async () => {
    stubApi({
      [JOBS]: { jobs: [] },
      [EQUIPMENT]: { items: [] },
      'POST /api/inventory/open-jobs': () => new Response(JSON.stringify({ error: 'before_on must be blank or a real date, YYYY-MM-DD' }), { status: 400, headers: { 'Content-Type': 'application/json' } }),
    })
    render(<OpenJobsGroup onOpenEquipment={vi.fn()} />)
    await screen.findByText(/no open jobs/i)
    fireEvent.click(screen.getByRole('button', { name: /add job/i }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.change(within(dialog).getByLabelText('Title'), { target: { value: 'x' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
    expect(await within(dialog).findByText(/must be blank or a real date/)).toBeInTheDocument()
  })

  it('edits a job from its title', async () => {
    let put: { url: string; body: unknown } | undefined
    stubApi({
      [JOBS]: { jobs: [makeJob()] },
      [EQUIPMENT]: { items: [{ id: 'eq-1', name: 'Shower pump' }] },
      'PUT /api/inventory/open-jobs/job-1': (req: { url: string; body: unknown }) => {
        put = req
        return { job: makeJob({ title: 'Leaks at the outlet fitting' }) }
      },
    })
    render(<OpenJobsGroup onOpenEquipment={vi.fn()} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Leaks at the outlet' }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.change(within(dialog).getByLabelText('Title'), { target: { value: 'Leaks at the outlet fitting' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(put).toBeDefined())
    expect(put!.body).toMatchObject({ title: 'Leaks at the outlet fitting', equipment_id: 'eq-1' })
  })

  it('drops a job with a reason', async () => {
    let dropped: { url: string; body: unknown } | undefined
    stubApi({
      [JOBS]: { jobs: [makeJob()] },
      'POST /api/inventory/open-jobs/job-1/drop': (req: { url: string; body: unknown }) => {
        dropped = req
        return { job: makeJob({ status: 'dropped', closed_on: '2026-11-07', drop_reason: 'Fixed itself' }) }
      },
    })
    render(<OpenJobsGroup onOpenEquipment={vi.fn()} />)
    await screen.findByText('Leaks at the outlet')

    fireEvent.click(screen.getByRole('button', { name: /^drop/i }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.change(within(dialog).getByLabelText(/reason/i), { target: { value: 'Fixed itself' } })
    fireEvent.click(within(dialog).getByRole('button', { name: /drop job/i }))

    await waitFor(() => expect(dropped).toBeDefined())
    expect(dropped!.url).toContain('today=2026-11-07')
    expect(dropped!.body).toEqual({ reason: 'Fixed itself' })
  })

  it('drops a job without a reason', async () => {
    let dropped: { body: unknown } | undefined
    stubApi({
      [JOBS]: { jobs: [makeJob()] },
      'POST /api/inventory/open-jobs/job-1/drop': (req: { body: unknown }) => {
        dropped = req
        return { job: makeJob({ status: 'dropped' }) }
      },
    })
    render(<OpenJobsGroup onOpenEquipment={vi.fn()} />)
    await screen.findByText('Leaks at the outlet')
    fireEvent.click(screen.getByRole('button', { name: /^drop/i }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.click(within(dialog).getByRole('button', { name: /drop job/i }))
    await waitFor(() => expect(dropped).toBeDefined())
    expect(dropped!.body).toEqual({ reason: '' })
  })

  it('marks a job done through the service log form', async () => {
    let done: { body: unknown } | undefined
    stubApi({
      [JOBS]: { jobs: [makeJob()] },
      [EQUIPMENT]: { items: [] },
      'POST /api/inventory/open-jobs/job-1/done': (req: { body: unknown }) => {
        done = req
        return {
          job: makeJob({ status: 'done', closed_on: '2026-11-07', log_entry_id: 'log-1' }),
          entry: { id: 'log-1', equipment_id: 'eq-1', rule_id: null, performed_at: '2026-11-07', hours: null, kind: 'repair', description: 'New fitting', who: '', cost: null, currency: '', created_at: '', updated_at: '', parts: [], photo_ids: [] },
        }
      },
    })
    render(<OpenJobsGroup onOpenEquipment={vi.fn()} />)
    await screen.findByText('Leaks at the outlet')

    fireEvent.click(screen.getByRole('button', { name: /^done/i }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByLabelText('Date')).toHaveValue('2026-11-07')
    fireEvent.change(within(dialog).getByLabelText('Notes'), { target: { value: 'New fitting' } })
    fireEvent.click(within(dialog).getByRole('button', { name: /mark done/i }))

    await waitFor(() => expect(done).toBeDefined())
    expect(done!.body).toMatchObject({ performed_at: '2026-11-07', description: 'New fitting', kind: 'repair', hours: null, parts: [] })
    expect(await screen.findByText(/logged/i)).toBeInTheDocument()
  })

  it('leaves out Done, Drop and Add job when the operator cannot write', async () => {
    stubApi({ [JOBS]: { jobs: [makeJob()] } })
    render(<OpenJobsGroup onOpenEquipment={vi.fn()} canWrite={false} />)
    await screen.findByText('Leaks at the outlet')
    expect(screen.queryByRole('button', { name: /add job/i })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /^done/i })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /^drop/i })).not.toBeInTheDocument()
  })

  it('can show the closed jobs, with how each ended', async () => {
    const fetchMock = stubApi({
      [JOBS]: ({ url }: { url: string }) => {
        if (url.includes('status=any')) {
          return {
            jobs: [
              makeJob(),
              makeJob({ id: 'job-3', title: 'Hatch seal', status: 'done', closed_on: '2026-11-01', log_entry_id: 'log-3' }),
              makeJob({ id: 'job-4', title: 'Old radar', status: 'dropped', closed_on: '2026-11-02', drop_reason: 'Sold it' }),
            ],
          }
        }
        return { jobs: [makeJob()] }
      },
    })
    render(<OpenJobsGroup onOpenEquipment={vi.fn()} />)
    await screen.findByText('Leaks at the outlet')
    expect(screen.queryByText('Hatch seal')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: /show closed/i }))
    expect(await screen.findByText('Hatch seal')).toBeInTheDocument()
    expect(screen.getByText(/done 1 Nov/i)).toBeInTheDocument()
    expect(screen.getByText(/dropped 2 Nov/i)).toBeInTheDocument()
    expect(screen.getByText(/sold it/i)).toBeInTheDocument()
    expect(fetchMock.mock.calls.some(([u]) => String(u).includes('status=any'))).toBe(true)
    // A closed job offers no Done or Drop.
    expect(screen.getAllByRole('button', { name: /^done/i })).toHaveLength(1)
  })
})
