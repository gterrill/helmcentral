import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { InboxTargetDialog, type InboxTargetKind } from '@/components/passages/inbox-target-dialog'

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
const open = (kind: InboxTargetKind, over: { count?: number; lastId?: string | null; onPick?: () => void } = {}) =>
  render(<InboxTargetDialog kind={kind} count={over.count ?? 1} lastId={over.lastId ?? null} onPick={over.onPick ?? vi.fn()} onClose={vi.fn()} />)

beforeEach(() => {
  // jsdom has no Web Animations; the scroll area asks for them after a pause.
  Element.prototype.getAnimations = () => []
  env.equipment = [gear('e1', 'Bilge pump', { manufacturer: 'Rule' }), gear('e2', 'Anchor windlass', { manufacturer: 'Lewmar' }), gear('e3', 'Spare impeller', { status: 'stored' })]
  env.jobs = [job('j1', 'Leaking hose', 'Port engine'), job('j2', 'Chafe on the main sheet')]
  env.equipmentError = null
  env.equipmentFilters = []
  env.jobFilters = []
})

describe('InboxTargetDialog for equipment', () => {
  it('lists the equipment and waits for a choice', () => {
    const onPick = vi.fn()
    open('equipment', { count: 3, onPick })
    expect(screen.getByText('Add 3 photos to equipment')).toBeInTheDocument()
    expect(screen.getAllByRole('radio')).toHaveLength(3)
    const add = screen.getByRole('button', { name: 'Add here' })
    expect(add).toBeDisabled()
    fireEvent.click(screen.getByRole('radio', { name: /Anchor windlass/ }))
    expect(add).toBeEnabled()
    fireEvent.click(add)
    expect(onPick).toHaveBeenCalledWith('e2', 'Anchor windlass')
  })

  it('asks the box to search once typing pauses, so it finds what the Equipment screen finds', async () => {
    open('equipment')
    expect(env.equipmentFilters.at(-1)).toEqual({ q: '' })
    const search = screen.getByRole('searchbox', { name: 'Search equipment' })
    fireEvent.change(search, { target: { value: 'lew' } })
    fireEvent.change(search, { target: { value: ' lewmar ' } })
    expect(env.equipmentFilters.some((f) => (f as { q: string }).q !== '')).toBe(false)
    await waitFor(() => expect(env.equipmentFilters.at(-1)).toEqual({ q: 'lewmar' }))
    expect(env.equipmentFilters.some((f) => (f as { q: string }).q === 'lew')).toBe(false)
  })

  it('is one named group of native radios, so the arrow keys move the choice and Tab enters it once', () => {
    open('equipment')
    const radios = screen.getAllByRole('radio') as HTMLInputElement[]
    expect(screen.getByRole('group', { name: 'equipment item' })).toContainElement(radios[0])
    for (const r of radios) {
      expect(r.tagName).toBe('INPUT')
      expect(r.name).toBe(radios[0].name)
    }
    expect(radios[0].name).not.toBe('')
  })

  it('says so when nothing matches', () => {
    env.equipment = []
    open('equipment')
    expect(screen.getByText('No equipment matches.')).toBeInTheDocument()
  })

  it('drops the choice when a new search no longer lists it, so Add here never files to a hidden item', () => {
    const onPick = vi.fn()
    const { rerender } = open('equipment', { onPick })
    fireEvent.click(screen.getByRole('radio', { name: /Bilge pump/ }))
    expect(screen.getByRole('button', { name: 'Add here' })).toBeEnabled()
    env.equipment = [gear('e2', 'Anchor windlass')]
    rerender(<InboxTargetDialog kind="equipment" count={1} lastId={null} onPick={onPick} onClose={vi.fn()} />)
    const add = screen.getByRole('button', { name: 'Add here' })
    expect(add).toBeDisabled()
    expect(screen.queryAllByRole('radio', { checked: true })).toHaveLength(0)
    fireEvent.click(add)
    expect(onPick).not.toHaveBeenCalled()
  })

  it('lets long names shrink inside the dialog instead of widening it', () => {
    env.equipment = [gear('e1', 'A very long equipment name '.repeat(8), { manufacturer: 'Maker '.repeat(20) })]
    open('equipment')
    expect(screen.getByText(/A very long equipment name/)).toHaveClass('truncate')
    expect(screen.getByText(/Maker/)).toHaveClass('truncate')
    expect(screen.getByRole('group', { name: 'equipment item' })).toHaveClass('min-w-0')
    const column = screen.getByRole('searchbox').parentElement as HTMLElement
    expect(column).toHaveClass('min-w-0')
    expect(column.querySelector('[data-slot="scroll-area"]')).toHaveClass('min-w-0')
  })

  it('highlights the last item used while it is listed, and ignores one that has gone', () => {
    const { unmount } = open('equipment', { lastId: 'e1' })
    expect(screen.getByRole('radio', { name: /Bilge pump/ })).toBeChecked()
    expect(screen.getByRole('button', { name: 'Add here' })).toBeEnabled()
    unmount()
    open('equipment', { lastId: 'deleted' })
    expect(screen.queryAllByRole('radio', { checked: true })).toHaveLength(0)
    expect(screen.getByRole('button', { name: 'Add here' })).toBeDisabled()
  })

  it('says so when the list cannot be read', () => {
    env.equipmentError = 'boom'
    open('equipment')
    expect(screen.getByRole('alert')).toHaveTextContent('boom')
  })

  it('says one photo in the singular', () => {
    open('equipment')
    expect(screen.getByText('Add 1 photo to equipment')).toBeInTheDocument()
  })
})

describe('InboxTargetDialog for jobs', () => {
  it('lists every open job, including those on stored equipment, with the equipment each is about', () => {
    const onPick = vi.fn()
    open('job', { count: 2, onPick })
    expect(env.jobFilters[0]).toEqual({ includeStored: true })
    expect(screen.getByText('Add 2 photos to a job')).toBeInTheDocument()
    expect(screen.getByRole('radio', { name: 'Leaking hose' })).toHaveAccessibleDescription('Port engine')
    fireEvent.click(screen.getByRole('radio', { name: /Chafe on the main sheet/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Add here' }))
    expect(onPick).toHaveBeenCalledWith('j2', 'Chafe on the main sheet')
  })

  it('searches the title and the equipment name here, as the box has no job search', () => {
    open('job')
    fireEvent.change(screen.getByRole('searchbox', { name: 'Search jobs' }), { target: { value: 'port eng' } })
    expect(screen.getAllByRole('radio')).toHaveLength(1)
    fireEvent.change(screen.getByRole('searchbox', { name: 'Search jobs' }), { target: { value: 'nothing here' } })
    expect(screen.getByText('No open job matches.')).toBeInTheDocument()
  })

  it('highlights the last job used while it is still open', () => {
    open('job', { lastId: 'j2' })
    expect(screen.getByRole('radio', { name: /Chafe on the main sheet/ })).toBeChecked()
  })
})
