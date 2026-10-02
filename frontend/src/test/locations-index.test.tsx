import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { LocationsIndex } from '@/components/inventory/locations-index'
import type { InventoryDeck, InventoryZone } from '@/hooks/use-inventory'
import { stubImageSize } from './stub-image'

let zones: InventoryZone[]
let decks: InventoryDeck[]
const fetchMock = vi.fn()

beforeEach(() => {
  zones = [
    {
      id: 'z1', name: 'Engine room (stbd)', sort_index: 0,
      bins: [
        { id: 'b1', zone_id: 'z1', code: 'ER-01', name: '', sort_index: 0 },
        { id: 'b2', zone_id: 'z1', code: 'ER-02', name: '', sort_index: 1 },
      ],
    },
    { id: 'z2', name: 'Lazarette', sort_index: 1, bins: [] },
  ]
  decks = []
  fetchMock.mockReset()
  fetchMock.mockImplementation((url: string, init?: RequestInit) => {
    const u = String(url)
    const method = init?.method ?? 'GET'
    if (u.endsWith('/api/inventory/decks') && method === 'GET') {
      return Promise.resolve({ ok: true, json: async () => ({ decks }) })
    }
    if (u.endsWith('/api/inventory/zones') && method === 'GET') {
      return Promise.resolve({ ok: true, json: async () => ({ zones }) })
    }
    if (u.endsWith('/api/inventory/zones') && method === 'POST') {
      const body = JSON.parse(String(init?.body)) as { name: string }
      if (body.name.toLowerCase() === 'lazarette') {
        return Promise.resolve({ ok: false, status: 409, json: async () => ({ error: 'a location named Lazarette already exists' }) })
      }
      const zone = { id: 'z3', name: body.name, sort_index: 2, bins: [] }
      zones = [...zones, zone]
      return Promise.resolve({ ok: true, json: async () => ({ zone }) })
    }
    return Promise.resolve({ ok: false, json: async () => ({ error: 'not found' }) })
  })
  vi.stubGlobal('fetch', fetchMock)
})

describe('LocationsIndex', () => {
  it('lists each location with its deck and bin count', async () => {
    render(<LocationsIndex onOpenLocation={vi.fn()} />)
    await screen.findByText('Engine room (stbd)')

    const headers = screen.getAllByRole('columnheader').map((h) => h.textContent?.trim())
    expect(headers).toEqual(['Name', 'Deck', 'Bins'])

    const row = screen.getByText('Engine room (stbd)').closest('tr')!
    expect(within(row).getByText('2')).toBeInTheDocument()
    const lazRow = screen.getByText('Lazarette').closest('tr')!
    expect(within(lazRow).getByText('0')).toBeInTheDocument()
  })

  it('opens the location page when a row is clicked', async () => {
    const onOpenLocation = vi.fn()
    render(<LocationsIndex onOpenLocation={onOpenLocation} />)
    fireEvent.click(await screen.findByText('Lazarette'))
    expect(onOpenLocation).toHaveBeenCalledWith('z2')
  })

  it('filters by name from the search box', async () => {
    render(<LocationsIndex onOpenLocation={vi.fn()} />)
    await screen.findByText('Lazarette')
    fireEvent.change(screen.getByLabelText('Search locations'), { target: { value: 'laz' } })
    expect(screen.queryByText('Engine room (stbd)')).not.toBeInTheDocument()
    expect(screen.getByText('Lazarette')).toBeInTheDocument()
  })

  it('shows an empty state explaining what a location is when there are none', async () => {
    zones = []
    render(<LocationsIndex onOpenLocation={vi.fn()} />)
    await screen.findByText('No locations yet')
    expect(screen.getByText(/salon, engine room, lazarette/i)).toBeInTheDocument()
  })

  it('creates a location by name and opens its page', async () => {
    const onOpenLocation = vi.fn()
    render(<LocationsIndex onOpenLocation={onOpenLocation} />)
    await screen.findByText('Lazarette')

    fireEvent.click(screen.getByRole('button', { name: 'New location' }))
    fireEvent.change(await screen.findByLabelText('Location name'), { target: { value: 'Salon' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create' }))

    await waitFor(() => expect(onOpenLocation).toHaveBeenCalledWith('z3'))
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/api/inventory/zones'),
      expect.objectContaining({ method: 'POST', body: JSON.stringify({ name: 'Salon' }) }),
    )
  })

  it('shows the server message verbatim when a create is refused', async () => {
    const onOpenLocation = vi.fn()
    render(<LocationsIndex onOpenLocation={onOpenLocation} />)
    await screen.findByText('Lazarette')

    fireEvent.click(screen.getByRole('button', { name: 'New location' }))
    fireEvent.change(await screen.findByLabelText('Location name'), { target: { value: 'lazarette' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create' }))

    await screen.findByText('a location named Lazarette already exists')
    expect(onOpenLocation).not.toHaveBeenCalled()
  })

  it('hides New location when read-only', async () => {
    render(<LocationsIndex onOpenLocation={vi.fn()} canWrite={false} />)
    await screen.findByText('Lazarette')
    expect(screen.queryByRole('button', { name: 'New location' })).not.toBeInTheDocument()
  })
})

describe('LocationsIndex bin labels', () => {
  it('offers Print bin labels from the page menu and lists every bin', async () => {
    render(<LocationsIndex onOpenLocation={vi.fn()} />)
    await screen.findByText('Lazarette')
    fireEvent.click(screen.getByRole('button', { name: /more|actions/i }))
    fireEvent.click(await screen.findByText('Print bin labels'))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('ER-01')).toBeInTheDocument()
    expect(within(dialog).getByText('ER-02')).toBeInTheDocument()
  })
})

describe('LocationsIndex plan view (ADR 0156)', () => {
  beforeEach(() => {
    stubImageSize(2000, 1000)
    decks = [
      { id: 'd1', name: 'Main deck', sort_index: 0, plan_document_id: 'doc1' },
      { id: 'd2', name: 'Lower deck', sort_index: 1, plan_document_id: 'doc2' },
      { id: 'd3', name: 'Flybridge', sort_index: 2, plan_document_id: null },
    ]
    zones = [
      {
        id: 'z1', name: 'Engine room', sort_index: 0, deck_id: 'd1', polygon: [[0.1, 0.2], [0.5, 0.2], [0.5, 0.6]],
        bins: [{ id: 'b1', zone_id: 'z1', code: 'ER-01', name: '', sort_index: 0, pin: { x: 0.3, y: 0.3 } }],
      },
      { id: 'z2', name: 'Salon', sort_index: 1, deck_id: 'd2', polygon: [[0, 0], [1, 0], [1, 1]], bins: [] },
    ]
  })

  it('names each location\'s deck in the table, and a dash when it is on none', async () => {
    zones = [...zones, { id: 'z3', name: 'Chain locker', sort_index: 2, bins: [] }]
    render(<LocationsIndex onOpenLocation={vi.fn()} view="table" />)
    const engineRow = (await screen.findByText('Engine room')).closest('tr')!
    expect(await within(engineRow).findByText('Main deck')).toBeInTheDocument()
    expect(within(screen.getByText('Salon').closest('tr')!).getByText('Lower deck')).toBeInTheDocument()
    expect(within(screen.getByText('Chain locker').closest('tr')!).getByText('--')).toBeInTheDocument()
  })

  it('shows the decks load error instead of a table with no deck names', async () => {
    const base = fetchMock.getMockImplementation()!
    fetchMock.mockImplementation((url: string, init?: RequestInit) =>
      String(url).endsWith('/api/inventory/decks')
        ? Promise.resolve({ ok: false, status: 500, json: async () => ({ error: 'decks unavailable' }) })
        : base(url, init))
    render(<LocationsIndex onOpenLocation={vi.fn()} view="table" />)
    expect(await screen.findByText(/decks unavailable/)).toBeInTheDocument()
  })

  it('switches between Table and Plan and reports the choice', async () => {
    const onViewChange = vi.fn()
    render(<LocationsIndex onOpenLocation={vi.fn()} view="table" onViewChange={onViewChange} />)
    await screen.findByText('Engine room')
    expect(screen.getByRole('button', { name: 'Table' })).toHaveAttribute('aria-pressed', 'true')
    fireEvent.click(screen.getByRole('button', { name: 'Plan' }))
    expect(onViewChange).toHaveBeenCalledWith('plan')
  })

  it('shows a tab per deck that has a plan image, and the first deck by default', async () => {
    render(<LocationsIndex onOpenLocation={vi.fn()} view="plan" />)
    expect(await screen.findByRole('button', { name: 'Main deck' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: 'Lower deck' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Flybridge' })).not.toBeInTheDocument()
    expect(await screen.findByRole('link', { name: 'Engine room, 1 bin' })).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /Salon/ })).not.toBeInTheDocument()
  })

  it('opens the chosen deck and reports the tab change', async () => {
    const onPlanDeckChange = vi.fn()
    render(<LocationsIndex onOpenLocation={vi.fn()} view="plan" planDeckId="d2" onPlanDeckChange={onPlanDeckChange} />)
    expect(await screen.findByRole('link', { name: 'Salon, 0 bins' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Main deck' }))
    expect(onPlanDeckChange).toHaveBeenCalledWith('d1')
  })

  it('opens a location from its outline and a bin from its pin', async () => {
    const onOpenLocation = vi.fn()
    const onOpenBin = vi.fn()
    render(<LocationsIndex onOpenLocation={onOpenLocation} onOpenBin={onOpenBin} view="plan" />)
    fireEvent.click(await screen.findByRole('link', { name: 'Engine room, 1 bin' }))
    expect(onOpenLocation).toHaveBeenCalledWith('z1')
    fireEvent.click(screen.getByRole('link', { name: 'Bin ER-01' }))
    expect(onOpenBin).toHaveBeenCalledWith('ER-01')
  })

  it('invites adding a deck plan when no deck has one', async () => {
    decks = [{ id: 'd3', name: 'Flybridge', sort_index: 0, plan_document_id: null }]
    const onOpenDecks = vi.fn()
    render(<LocationsIndex onOpenLocation={vi.fn()} view="plan" onOpenDecks={onOpenDecks} />)
    await screen.findByText('No deck plans yet')
    fireEvent.click(screen.getByRole('button', { name: 'Add a deck plan' }))
    expect(onOpenDecks).toHaveBeenCalled()
  })

  it('reaches the Decks list from a Decks button beside the view switch, not the page menu', async () => {
    const onOpenDecks = vi.fn()
    render(<LocationsIndex onOpenLocation={vi.fn()} onOpenDecks={onOpenDecks} />)
    await screen.findByText('Engine room')
    fireEvent.click(screen.getByRole('button', { name: 'Decks' }))
    expect(onOpenDecks).toHaveBeenCalledTimes(1)
    fireEvent.click(screen.getByRole('button', { name: /more|actions/i }))
    await screen.findByText('Print bin labels')
    expect(screen.queryByRole('menuitem', { name: 'Decks' })).not.toBeInTheDocument()
  })

  it('keeps the Decks button in the plan view too', async () => {
    const onOpenDecks = vi.fn()
    render(<LocationsIndex onOpenLocation={vi.fn()} view="plan" onOpenDecks={onOpenDecks} />)
    await screen.findByRole('link', { name: 'Engine room, 1 bin' })
    fireEvent.click(screen.getByRole('button', { name: 'Decks' }))
    expect(onOpenDecks).toHaveBeenCalledTimes(1)
  })

  it('offers Edit deck on the deck shown in the plan, with or without tabs', async () => {
    const onOpenDeck = vi.fn()
    const { unmount } = render(<LocationsIndex onOpenLocation={vi.fn()} view="plan" planDeckId="d2" onOpenDeck={onOpenDeck} />)
    await screen.findByRole('group', { name: 'Decks' })
    fireEvent.click(screen.getByRole('button', { name: 'Edit deck' }))
    expect(onOpenDeck).toHaveBeenCalledWith('d2')
    unmount()

    decks = decks.filter((d) => d.id === 'd1')
    render(<LocationsIndex onOpenLocation={vi.fn()} view="plan" onOpenDeck={onOpenDeck} />)
    await screen.findByRole('link', { name: 'Engine room, 1 bin' })
    expect(screen.queryByRole('group', { name: 'Decks' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Edit deck' }))
    expect(onOpenDeck).toHaveBeenLastCalledWith('d1')
  })

  it('hides Edit deck when read-only', async () => {
    render(<LocationsIndex onOpenLocation={vi.fn()} view="plan" canWrite={false} onOpenDeck={vi.fn()} />)
    await screen.findByRole('link', { name: 'Engine room, 1 bin' })
    expect(screen.queryByRole('button', { name: 'Edit deck' })).not.toBeInTheDocument()
  })
})
