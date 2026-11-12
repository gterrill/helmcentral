import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { InboxEquipmentDialog, InboxJobDialog } from '@/components/passages/inbox-target-dialog'

const env = vi.hoisted(() => ({
  equipment: [] as unknown[],
  jobs: [] as unknown[],
  equipmentError: null as string | null,
  equipmentFilters: [] as unknown[],
  jobFilters: [] as unknown[],
}))

vi.mock('@/hooks/use-inventory', async (orig) => ({
  ...(await orig<typeof import('@/hooks/use-inventory')>()),
  useEquipment: (filter: unknown) => (env.equipmentFilters.push(filter), { items: env.equipment, loading: false, error: env.equipmentError, refresh: vi.fn() }),
}))
vi.mock('@/hooks/use-open-jobs', async (orig) => ({
  ...(await orig<typeof import('@/hooks/use-open-jobs')>()),
  useOpenJobs: (filter: unknown) => (env.jobFilters.push(filter), { jobs: env.jobs, loading: false, error: null, refresh: vi.fn() }),
}))

const gear = (id: string, name: string, extra: Record<string, unknown> = {}) => ({ id, name, manufacturer: '', model: '', aliases: [], status: 'deployed', zone_name: '', ...extra })
const job = (id: string, title: string, equipment_name = '') => ({ id, title, equipment_name, status: 'open' })

beforeEach(() => {
  env.equipment = [gear('e1', 'Bilge pump', { manufacturer: 'Rule' }), gear('e2', 'Anchor windlass', { manufacturer: 'Lewmar' }), gear('e3', 'Spare impeller', { status: 'stored' })]
  env.jobs = [job('j1', 'Leaking hose', 'Port engine'), job('j2', 'Chafe on the main sheet')]
  env.equipmentError = null
  env.equipmentFilters = []
  env.jobFilters = []
})

describe('InboxEquipmentDialog', () => {
  it('lists the equipment and waits for a choice', () => {
    const onPick = vi.fn()
    render(<InboxEquipmentDialog count={3} lastId={null} onPick={onPick} onClose={vi.fn()} />)
    expect(screen.getByText('Add 3 photos to equipment')).toBeInTheDocument()
    expect(screen.getAllByRole('radio')).toHaveLength(3)
    const add = screen.getByRole('button', { name: 'Add here' })
    expect(add).toBeDisabled()
    fireEvent.click(screen.getByRole('radio', { name: /Anchor windlass/ }))
    expect(add).toBeEnabled()
    fireEvent.click(add)
    expect(onPick).toHaveBeenCalledWith('e2', 'Anchor windlass')
  })

  it('asks for the whole list and narrows it by what is typed', () => {
    render(<InboxEquipmentDialog count={1} lastId={null} onPick={vi.fn()} onClose={vi.fn()} />)
    expect(env.equipmentFilters[0]).toEqual({})
    fireEvent.change(screen.getByRole('searchbox', { name: 'Search equipment' }), { target: { value: 'lewmar' } })
    expect(screen.getAllByRole('radio')).toHaveLength(1)
    expect(screen.getByRole('radio', { name: /Anchor windlass/ })).toBeInTheDocument()
    fireEvent.change(screen.getByRole('searchbox', { name: 'Search equipment' }), { target: { value: 'zzz' } })
    expect(screen.getByText('No equipment matches.')).toBeInTheDocument()
  })

  it('highlights the last item used while it still exists, and ignores one that has gone', () => {
    const { unmount } = render(<InboxEquipmentDialog count={1} lastId="e1" onPick={vi.fn()} onClose={vi.fn()} />)
    expect(screen.getByRole('radio', { name: /Bilge pump/ })).toBeChecked()
    expect(screen.getByRole('button', { name: 'Add here' })).toBeEnabled()
    unmount()
    render(<InboxEquipmentDialog count={1} lastId="deleted" onPick={vi.fn()} onClose={vi.fn()} />)
    expect(screen.queryAllByRole('radio', { checked: true })).toHaveLength(0)
    expect(screen.getByRole('button', { name: 'Add here' })).toBeDisabled()
  })

  it('says so when the list cannot be read', () => {
    env.equipmentError = 'boom'
    render(<InboxEquipmentDialog count={1} lastId={null} onPick={vi.fn()} onClose={vi.fn()} />)
    expect(screen.getByRole('alert')).toHaveTextContent('boom')
  })

  it('says one photo in the singular', () => {
    render(<InboxEquipmentDialog count={1} lastId={null} onPick={vi.fn()} onClose={vi.fn()} />)
    expect(screen.getByText('Add 1 photo to equipment')).toBeInTheDocument()
  })
})

describe('InboxJobDialog', () => {
  it('lists only the open jobs, with the equipment each is about', () => {
    const onPick = vi.fn()
    render(<InboxJobDialog count={2} lastId={null} onPick={onPick} onClose={vi.fn()} />)
    expect(env.jobFilters[0]).toEqual({})
    expect(screen.getByText('Add 2 photos to a job')).toBeInTheDocument()
    expect(screen.getByRole('radio', { name: /Leaking hose/ })).toHaveTextContent('Port engine')
    fireEvent.click(screen.getByRole('radio', { name: /Chafe on the main sheet/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Add here' }))
    expect(onPick).toHaveBeenCalledWith('j2', 'Chafe on the main sheet')
  })

  it('searches the title and the equipment name', () => {
    render(<InboxJobDialog count={1} lastId={null} onPick={vi.fn()} onClose={vi.fn()} />)
    fireEvent.change(screen.getByRole('searchbox', { name: 'Search jobs' }), { target: { value: 'port eng' } })
    expect(screen.getAllByRole('radio')).toHaveLength(1)
    fireEvent.change(screen.getByRole('searchbox', { name: 'Search jobs' }), { target: { value: 'nothing here' } })
    expect(screen.getByText('No open job matches.')).toBeInTheDocument()
  })

  it('highlights the last job used while it is still open', () => {
    render(<InboxJobDialog count={1} lastId="j2" onPick={vi.fn()} onClose={vi.fn()} />)
    expect(screen.getByRole('radio', { name: /Chafe on the main sheet/ })).toBeChecked()
  })
})
