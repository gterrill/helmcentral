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

const config = vi.hoisted(() => ({ distanceUnits: 'metric', timeZone: 'Australia/Brisbane' as string | undefined }))
vi.mock('@/hooks/use-vessel-state', () => ({
  useVesselStateSelector: (select: (state: { timeZone: string | undefined }) => unknown) => select({ timeZone: config.timeZone }),
}))
vi.mock('@/hooks/use-app-config', () => ({ useAppConfig: () => ({ ui: { distanceUnits: config.distanceUnits } }) }))

const thomas: SavedPlace = {
  id: 'p1',
  name: 'Thomas Island South',
  lat: -20.5446,
  lon: 149.118933,
  note: 'Quiet in a southerly',
  depth_m: null,
  depth_source: '',
  depth_date: '',
  created_at: '2026-10-30T09:00:00Z',
  updated_at: '2026-10-30T09:00:00Z',
}

beforeEach(() => {
  config.distanceUnits = 'metric'
  config.timeZone = 'Australia/Brisbane'
  vi.useRealTimers()
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

  describe('depth', () => {
    const withDepth: SavedPlace = { ...thomas, depth_m: 4, depth_source: 'charted', depth_date: '2026-10-01' }

    async function openEditing(place: SavedPlace) {
      hook.places = [place]
      render(<SavedPlacesSection />)
      fireEvent.click(screen.getByRole('button', { name: `Actions for ${place.name}` }))
      fireEvent.click(await screen.findByRole('menuitem', { name: /Edit/ }))
      return screen.findByRole('dialog')
    }

    it('shows a saved depth in the list with where it came from', () => {
      hook.places = [withDepth]
      render(<SavedPlacesSection />)
      expect(screen.getByText(/4\.0 m \(charted 2026-10-01\) · Quiet in a southerly/)).toBeInTheDocument()
    })

    it('adds a depth as charted, dated today, and sends nothing for it when the box is empty', async () => {
      vi.useFakeTimers({ toFake: ['Date'] })
      // 20:00 UTC on the 1st is already the 2nd at the boat, and the date is the boat's.
      vi.setSystemTime(new Date('2026-11-01T20:00:00Z'))
      render(<SavedPlacesSection />)
      fireEvent.click(screen.getByRole('button', { name: 'Add place' }))
      const dialog = await screen.findByRole('dialog')
      fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'Butterfly Bay' } })
      fireEvent.change(within(dialog).getByLabelText('Latitude'), { target: { value: "20°04.590' S" } })
      fireEvent.change(within(dialog).getByLabelText('Longitude'), { target: { value: "148°55.500' E" } })
      fireEvent.change(within(dialog).getByLabelText(/Depth at chart datum/), { target: { value: '4' } })
      fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
      await vi.waitFor(() => expect(hook.add).toHaveBeenCalled())
      expect(hook.add.mock.calls[0][0]).toMatchObject({ depth_m: 4, depth_source: 'charted', depth_date: '2026-11-02' })
    })

    it('will not date a depth before the boat has reported its zone', async () => {
      config.timeZone = undefined
      const dialog = await openEditing(thomas)
      fireEvent.change(within(dialog).getByLabelText(/Depth at chart datum/), { target: { value: '4' } })
      expect(within(dialog).getByRole('alert')).toHaveTextContent(/time zone/)
      expect(within(dialog).getByRole('button', { name: 'Save' })).toBeDisabled()
    })

    it('leaves the depth out of a new place when the box is empty', async () => {
      render(<SavedPlacesSection />)
      fireEvent.click(screen.getByRole('button', { name: 'Add place' }))
      const dialog = await screen.findByRole('dialog')
      fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'Cove' } })
      fireEvent.change(within(dialog).getByLabelText('Latitude'), { target: { value: "20°04.590' S" } })
      fireEvent.change(within(dialog).getByLabelText('Longitude'), { target: { value: "148°55.500' E" } })
      fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
      await waitFor(() => expect(hook.add).toHaveBeenCalled())
      expect(hook.add.mock.calls[0][0]).not.toHaveProperty('depth_m')
    })

    it('shows the old figure before a new one replaces it', async () => {
      const dialog = await openEditing(withDepth)
      expect(within(dialog).getByTestId('saved-place-depth-current')).toHaveTextContent('Now 4.0 m (charted 2026-10-01).')
      fireEvent.change(within(dialog).getByLabelText(/Depth at chart datum/), { target: { value: '5.5' } })
      expect(within(dialog).getByTestId('saved-place-depth-replaces')).toHaveTextContent('replaces 4.0 m (charted 2026-10-01) with 5.5 m')
      fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
      await waitFor(() => expect(hook.change).toHaveBeenCalled())
      const patch = hook.change.mock.calls[0][1]
      expect(patch).toMatchObject({ depth_m: 5.5, depth_source: 'charted' })
      expect(patch.depth_date).toMatch(/^\d{4}-\d{2}-\d{2}$/)
      expect(patch).not.toHaveProperty('name')
    })

    it('clears the depth when the box is emptied, saying what goes', async () => {
      const dialog = await openEditing(withDepth)
      fireEvent.change(within(dialog).getByLabelText(/Depth at chart datum/), { target: { value: '' } })
      expect(within(dialog).getByTestId('saved-place-depth-clears')).toHaveTextContent('clears 4.0 m (charted 2026-10-01)')
      fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
      await waitFor(() => expect(hook.change).toHaveBeenCalled())
      expect(hook.change.mock.calls[0][1]).toEqual({ depth_m: null })
    })

    it('does not send the depth when only the name changes', async () => {
      const dialog = await openEditing(withDepth)
      fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'Thomas Is South' } })
      fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
      await waitFor(() => expect(hook.change).toHaveBeenCalled())
      expect(hook.change.mock.calls[0][1]).toEqual({ name: 'Thomas Is South' })
    })

    it('refuses a depth it cannot read or that is out of bounds', async () => {
      const dialog = await openEditing(thomas)
      const box = within(dialog).getByLabelText(/Depth at chart datum/)
      for (const bad of ['deep', '5000', '-50']) {
        fireEvent.change(box, { target: { value: bad } })
        expect(within(dialog).getByText(/Not a depth/)).toBeInTheDocument()
        expect(within(dialog).getByRole('button', { name: 'Save' })).toBeDisabled()
      }
      fireEvent.change(box, { target: { value: '-0.8' } })
      expect(within(dialog).queryByText(/Not a depth/)).not.toBeInTheDocument()
    })

    it('reads and writes feet when the operator reads imperial', async () => {
      config.distanceUnits = 'imperial'
      const dialog = await openEditing(withDepth)
      expect(within(dialog).getByLabelText(/Depth at chart datum \(ft\)/)).toHaveValue('13.1')
      fireEvent.change(within(dialog).getByLabelText(/Depth at chart datum/), { target: { value: '20' } })
      fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
      await waitFor(() => expect(hook.change).toHaveBeenCalled())
      expect(hook.change.mock.calls[0][1].depth_m).toBeCloseTo(6.096, 3)
    })
  })
})
