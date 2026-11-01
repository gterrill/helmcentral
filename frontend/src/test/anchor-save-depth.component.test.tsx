import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AnchorSaveDepth } from '@/components/anchor-save-depth'
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

const env = vi.hoisted(() => ({ units: 'metric', timeZone: 'Australia/Brisbane' as string | null }))
vi.mock('@/hooks/use-vessel-state', () => ({
  useVesselStateSelector: (select: (state: { timeZone: string | null }) => unknown) => select({ timeZone: env.timeZone }),
}))
vi.mock('@/hooks/use-app-config', () => ({ useAppConfig: () => ({ ui: { distanceUnits: env.units } }) }))
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

const watch = {
  active: true,
  anchorLat: -20,
  anchorLon: 149,
  setAt: '2026-11-01T20:00:00Z',
  placeName: 'Butterfly Bay',
  datumDepthM: 4.2 as number | null,
  datumDepthReason: '',
  planningDepthSource: 'sounder' as 'sounder' | 'operator' | null,
}

const cove: SavedPlace = {
  id: 'p1', name: 'Butterfly Cove', lat: -20.0005, lon: 149, note: '',
  depth_m: 4, depth_source: 'charted', depth_date: '2026-10-30', created_at: '', updated_at: '',
}

beforeEach(() => {
  env.units = 'metric'
  env.timeZone = 'Australia/Brisbane'
  hook.places = []
  hook.loading = false
  hook.error = null
  hook.add = vi.fn().mockResolvedValue(undefined)
  hook.change = vi.fn().mockResolvedValue(undefined)
})

describe('AnchorSaveDepth', () => {
  it('renders nothing without a watch', () => {
    const { container } = render(<AnchorSaveDepth watch={{ ...watch, active: false }} />)
    expect(container).toBeEmptyDOMElement()
  })

  it('shows the reason, and no button, when the depth cannot be worked out', () => {
    render(<AnchorSaveDepth watch={{ ...watch, datumDepthM: null, datumDepthReason: 'Set the transducer depth in Settings → Anchor Watch.' }} />)
    expect(screen.getByText('Set the transducer depth in Settings → Anchor Watch.')).toBeInTheDocument()
    expect(screen.queryByRole('button')).toBeNull()
  })

  it('shows a reason rather than guess a date before the boat reports its zone', () => {
    env.timeZone = null
    render(<AnchorSaveDepth watch={watch} />)
    expect(screen.getByText(/time zone/i)).toBeInTheDocument()
    expect(screen.queryByRole('button')).toBeNull()
  })

  it('says so when the saved places cannot be read, and offers no button', () => {
    hook.error = 'boom'
    render(<AnchorSaveDepth watch={watch} />)
    expect(screen.getByText(/saved places could not be read/i)).toBeInTheDocument()
    expect(screen.queryByRole('button')).toBeNull()
  })

  it('saves to the nearest saved place only after confirming, showing the figure it replaces', async () => {
    hook.places = [cove]
    render(<AnchorSaveDepth watch={watch} />)
    expect(screen.getByText('Save 4.2 m at datum')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Save depth to Butterfly Cove' }))
    expect(hook.change).not.toHaveBeenCalled()
    expect(screen.getByText('Replaces 4.0 m (charted 2026-10-30) with 4.2 m (sounded 2026-11-02).')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Confirm' }))
    await waitFor(() => expect(hook.change).toHaveBeenCalledTimes(1))
    expect(hook.change).toHaveBeenCalledWith('p1', { depth_m: 4.2, depth_source: 'sounded', depth_date: '2026-11-02' })
  })

  it('says where the depth came from', () => {
    const { rerender } = render(<AnchorSaveDepth watch={watch} />)
    expect(screen.getByText(/from the sounder/)).toBeInTheDocument()
    rerender(<AnchorSaveDepth watch={{ ...watch, planningDepthSource: 'operator' }} />)
    expect(screen.getByText(/the depth you entered/)).toBeInTheDocument()
  })

  it('saves to the nearest place that has a depth, even when a nearer one has none', async () => {
    const a: SavedPlace = { ...cove, id: 'A', name: 'A', lat: -20.0002, depth_m: null, depth_source: '', depth_date: '' }
    const b: SavedPlace = { ...cove, id: 'B', name: 'B', lat: -20.001, depth_m: 150 }
    hook.places = [a, b]
    render(<AnchorSaveDepth watch={watch} />)
    fireEvent.click(screen.getByRole('button', { name: 'Save depth to B' }))
    expect(screen.getByText(/^Replaces 150\.0 m/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Confirm' }))
    await waitFor(() => expect(hook.change).toHaveBeenCalledTimes(1))
    expect(hook.change.mock.calls[0][0]).toBe('B')
  })

  it('falls back to the nearest place when none in range has a depth', () => {
    hook.places = [{ ...cove, depth_m: null, depth_source: '', depth_date: '' }]
    render(<AnchorSaveDepth watch={watch} />)
    expect(screen.getByRole('button', { name: 'Save depth to Butterfly Cove' })).toBeInTheDocument()
  })

  it('shows and sends the same figure, so imperial display and the saved value agree', async () => {
    env.units = 'imperial'
    hook.places = [cove]
    render(<AnchorSaveDepth watch={{ ...watch, datumDepthM: 4.2349 }} />)
    expect(screen.getByText('Save 13.9 ft at datum')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Save depth to Butterfly Cove' }))
    expect(screen.getByText(/with 13\.9 ft \(sounded/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Confirm' }))
    await waitFor(() => expect(hook.change).toHaveBeenCalledTimes(1))
    expect(hook.change.mock.calls[0][1].depth_m).toBe(4.23)
  })

  it('cuts a long place name to the 80 characters the server allows', () => {
    render(<AnchorSaveDepth watch={{ ...watch, placeName: 'x'.repeat(100) }} />)
    fireEvent.click(screen.getByRole('button', { name: 'Save as a saved place' }))
    expect((screen.getByLabelText('Name') as HTMLInputElement).value).toHaveLength(80)
  })

  it('cancel writes nothing', () => {
    hook.places = [cove]
    render(<AnchorSaveDepth watch={watch} />)
    fireEvent.click(screen.getByRole('button', { name: 'Save depth to Butterfly Cove' }))
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(hook.change).not.toHaveBeenCalled()
    expect(hook.add).not.toHaveBeenCalled()
    expect(screen.getByRole('button', { name: 'Save depth to Butterfly Cove' })).toBeInTheDocument()
  })

  it('with no saved place in range, asks for a name prefilled from the watch and creates one at the anchor', async () => {
    hook.places = [{ ...cove, lat: -20.01 }]
    render(<AnchorSaveDepth watch={watch} />)
    fireEvent.click(screen.getByRole('button', { name: 'Save as a saved place' }))
    const name = screen.getByLabelText('Name')
    expect(name).toHaveValue('Butterfly Bay')
    fireEvent.change(name, { target: { value: 'Butterfly Bay N' } })
    fireEvent.click(screen.getByRole('button', { name: 'Confirm' }))
    await waitFor(() => expect(hook.add).toHaveBeenCalledTimes(1))
    expect(hook.add).toHaveBeenCalledWith({
      name: 'Butterfly Bay N', lat: -20, lon: 149, note: '', depth_m: 4.2, depth_source: 'sounded', depth_date: '2026-11-02',
    })
  })

  it('will not confirm a new place without a name', () => {
    render(<AnchorSaveDepth watch={{ ...watch, placeName: '' }} />)
    fireEvent.click(screen.getByRole('button', { name: 'Save as a saved place' }))
    expect(screen.getByRole('button', { name: 'Confirm' })).toBeDisabled()
  })

  it('shows the server error and stays open when the write fails', async () => {
    hook.places = [cove]
    hook.change = vi.fn().mockRejectedValue(new Error('depth_m is out of range'))
    render(<AnchorSaveDepth watch={watch} />)
    fireEvent.click(screen.getByRole('button', { name: 'Save depth to Butterfly Cove' }))
    fireEvent.click(screen.getByRole('button', { name: 'Confirm' }))
    expect(await screen.findByText('depth_m is out of range')).toBeInTheDocument()
  })

  it('shows feet in imperial units', () => {
    env.units = 'imperial'
    render(<AnchorSaveDepth watch={watch} />)
    expect(screen.getByText('Save 13.8 ft at datum')).toBeInTheDocument()
  })
})
