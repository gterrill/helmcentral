import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { SavedPlacesSection } from '@/components/settings/sections/saved-places-section'
import { SETTINGS_SECTIONS } from '@/components/settings/settings-nav'
import { PROPOSAL_REFRESH } from '@/lib/assistant-proposal-actions'
import { SETTINGS_HELP_TARGETS } from '@/lib/help-links'
import type { SavedPlace } from '@/hooks/use-saved-places'

const hook = vi.hoisted(() => ({
  places: [] as unknown[],
  loading: false,
  error: null as string | null,
  refresh: vi.fn(),
  add: vi.fn(),
  change: vi.fn(),
  remove: vi.fn(),
}))

vi.mock('@/hooks/use-saved-places', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/hooks/use-saved-places')>()),
  useSavedPlaces: () => hook,
}))

const thomas: SavedPlace = {
  id: 'p1',
  name: 'Thomas Island South',
  lat: -20.5446,
  lon: 149.118933,
  note: 'Quiet in a southerly',
  created_at: '2026-10-30T09:00:00Z',
  updated_at: '2026-10-30T09:00:00Z',
}

beforeEach(() => {
  hook.places = []
  hook.loading = false
  hook.error = null
  hook.refresh = vi.fn()
  hook.add = vi.fn().mockResolvedValue(undefined)
  hook.change = vi.fn().mockResolvedValue(undefined)
  hook.remove = vi.fn().mockResolvedValue(undefined)
})

describe('Saved places section', () => {
  it('is a settings section with a help target and refreshes after Mate saves one', () => {
    expect(SETTINGS_SECTIONS.find((s) => s.id === 'saved-places')?.label).toBe('Saved places')
    expect(SETTINGS_HELP_TARGETS['saved-places'].page).toBe('features/place-names')
    expect(PROPOSAL_REFRESH.saved_place).toBeTypeOf('function')
  })

  it('tells the operator how a place gets saved when there are none', () => {
    render(<SavedPlacesSection />)
    expect(screen.getByText('No saved places')).toBeInTheDocument()
    expect(screen.getByText(/it will offer to save it/i)).toBeInTheDocument()
  })

  it('lists each place with its position in degrees and decimal minutes', () => {
    hook.places = [thomas]
    render(<SavedPlacesSection />)
    const list = screen.getByRole('list', { name: 'Saved places' })
    expect(within(list).getByText('Thomas Island South')).toBeInTheDocument()
    expect(within(list).getByText(/20°32\.676' S 149°07\.136' E · Quiet in a southerly/)).toBeInTheDocument()
  })

  it('shows a load failure with a retry', () => {
    hook.error = 'HTTP 500'
    render(<SavedPlacesSection />)
    expect(screen.getByRole('alert')).toHaveTextContent('HTTP 500')
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(hook.refresh).toHaveBeenCalled()
  })

  async function openEdit() {
    hook.places = [thomas]
    render(<SavedPlacesSection />)
    fireEvent.click(screen.getByRole('button', { name: 'Actions for Thomas Island South' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: /Edit/ }))
    return screen.findByRole('dialog')
  }

  it('renames and moves a place, sending signed decimal degrees', async () => {
    const dialog = await openEdit()
    expect(within(dialog).getByLabelText('Latitude')).toHaveValue("20°32.676' S")
    fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'Thomas Is South' } })
    fireEvent.change(within(dialog).getByLabelText('Latitude'), { target: { value: "020º33.000' S" } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(hook.change).toHaveBeenCalled())
    const [id, patch] = hook.change.mock.calls[0]
    expect(id).toBe('p1')
    expect(patch.name).toBe('Thomas Is South')
    expect(patch.lat).toBeCloseTo(-20.55, 6)
    expect(patch).not.toHaveProperty('lon')
    expect(patch).not.toHaveProperty('note')
  })

  it('sends only the name when only the name is edited, so the position cannot drift', async () => {
    const dialog = await openEdit()
    fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'Thomas Is South' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(hook.change).toHaveBeenCalled())
    expect(hook.change.mock.calls[0][1]).toEqual({ name: 'Thomas Is South' })
  })

  it('will not save a position it cannot read', async () => {
    const dialog = await openEdit()
    fireEvent.change(within(dialog).getByLabelText('Longitude'), { target: { value: 'east of here' } })
    expect(within(dialog).getByText(/Not a longitude/)).toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: 'Save' })).toBeDisabled()
    expect(hook.change).not.toHaveBeenCalled()
  })

  it("keeps the dialog open and shows the server's reason when a save is refused", async () => {
    hook.change = vi.fn().mockRejectedValue(new Error('name: a saved place called "X" already exists'))
    const dialog = await openEdit()
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('already exists')
  })

  it('deletes a place only after confirming', async () => {
    hook.places = [thomas]
    render(<SavedPlacesSection />)
    fireEvent.click(screen.getByRole('button', { name: 'Actions for Thomas Island South' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: /Delete/ }))
    const dialog = await screen.findByRole('alertdialog')
    expect(hook.remove).not.toHaveBeenCalled()
    fireEvent.click(within(dialog).getByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(hook.remove).toHaveBeenCalledWith('p1'))
  })

  it('adds a place from a name and a position typed as degrees and decimal minutes', async () => {
    render(<SavedPlacesSection />)
    fireEvent.click(screen.getByRole('button', { name: 'Add place' }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByRole('button', { name: 'Save' })).toBeDisabled()
    fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'Brampton Island - West Bay' } })
    fireEvent.change(within(dialog).getByLabelText('Latitude'), { target: { value: "020º48.978' S" } })
    fireEvent.change(within(dialog).getByLabelText('Longitude'), { target: { value: "149º15.691' E" } })
    fireEvent.change(within(dialog).getByLabelText('Note'), { target: { value: 'Good in a southerly' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(hook.add).toHaveBeenCalled())
    const [patch] = hook.add.mock.calls[0]
    expect(patch.name).toBe('Brampton Island - West Bay')
    expect(patch.lat).toBeCloseTo(-20.8163, 4)
    expect(patch.lon).toBeCloseTo(149.261517, 5)
    expect(patch.note).toBe('Good in a southerly')
  })

  it("shows the server's reason in the dialog when the name is already saved", async () => {
    hook.add = vi.fn().mockRejectedValue(new Error('name: a saved place called "Thomas Island South" already exists; change that one instead'))
    render(<SavedPlacesSection />)
    fireEvent.click(screen.getByRole('button', { name: 'Add place' }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'thomas island south' } })
    fireEvent.change(within(dialog).getByLabelText('Latitude'), { target: { value: "20°32.676' S" } })
    fireEvent.change(within(dialog).getByLabelText('Longitude'), { target: { value: "149°07.136' E" } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('already exists')
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  it('offers Edit and Delete on every row', async () => {
    hook.places = [thomas, { ...thomas, id: 'p2', name: 'Brampton West' }]
    render(<SavedPlacesSection />)
    for (const name of ['Thomas Island South', 'Brampton West']) {
      expect(screen.getByRole('button', { name: `Actions for ${name}` })).toBeInTheDocument()
    }
    fireEvent.click(screen.getByRole('button', { name: 'Actions for Brampton West' }))
    expect(await screen.findByRole('menuitem', { name: /Delete/ })).toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: /Edit/ })).toBeInTheDocument()
  })
})
