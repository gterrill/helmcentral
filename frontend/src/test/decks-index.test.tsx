import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { DecksIndex } from '@/components/inventory/decks-index'
import type { InventoryDeck, InventoryZone } from '@/hooks/use-inventory'

let decks: InventoryDeck[]
let zones: InventoryZone[]
const fetchMock = vi.fn()

beforeEach(() => {
  decks = [
    { id: 'd1', name: 'Main deck', sort_index: 0, plan_document_id: 'doc1' },
    { id: 'd2', name: 'Flybridge', sort_index: 1, plan_document_id: null },
  ]
  zones = [
    { id: 'z1', name: 'Salon', sort_index: 0, bins: [], deck_id: 'd1', polygon: [[0, 0], [1, 0], [1, 1]] },
    { id: 'z2', name: 'Galley', sort_index: 1, bins: [], deck_id: 'd1', polygon: [[0, 0], [1, 0], [1, 1]] },
    { id: 'z3', name: 'Lazarette', sort_index: 2, bins: [], deck_id: null, polygon: null },
  ]
  fetchMock.mockReset()
  fetchMock.mockImplementation((url: string, init?: RequestInit) => {
    const u = String(url)
    const method = init?.method ?? 'GET'
    if (u.endsWith('/api/inventory/zones') && method === 'GET') return Promise.resolve({ ok: true, json: async () => ({ zones }) })
    if (u.endsWith('/api/inventory/decks') && method === 'GET') return Promise.resolve({ ok: true, json: async () => ({ decks }) })
    if (u.endsWith('/api/inventory/decks') && method === 'POST') {
      const body = JSON.parse(String(init?.body)) as { name: string }
      if (body.name.toLowerCase() === 'main deck') {
        return Promise.resolve({ ok: false, status: 409, json: async () => ({ error: 'a deck with this name already exists' }) })
      }
      const deck = { id: 'd3', name: body.name, sort_index: 2, plan_document_id: null }
      decks = [...decks, deck]
      return Promise.resolve({ ok: true, json: async () => ({ deck }) })
    }
    return Promise.resolve({ ok: false, json: async () => ({ error: 'not found' }) })
  })
  vi.stubGlobal('fetch', fetchMock)
})

describe('DecksIndex', () => {
  it('lists each deck with whether it has a plan and how many locations are on it', async () => {
    render(<DecksIndex onOpenDeck={vi.fn()} onBack={vi.fn()} />)
    await screen.findByText('Main deck')
    expect(screen.getAllByRole('columnheader').map((h) => h.textContent?.trim())).toEqual(['Name', 'Plan', 'Locations'])
    const main = screen.getByText('Main deck').closest('tr')!
    expect(within(main).getByText('Yes')).toBeInTheDocument()
    expect(within(main).getByText('2')).toBeInTheDocument()
    const fly = screen.getByText('Flybridge').closest('tr')!
    expect(within(fly).getByText('No')).toBeInTheDocument()
    expect(within(fly).getByText('0')).toBeInTheDocument()
  })

  it('opens a deck when its row is clicked', async () => {
    const onOpenDeck = vi.fn()
    render(<DecksIndex onOpenDeck={onOpenDeck} onBack={vi.fn()} />)
    fireEvent.click(await screen.findByText('Flybridge'))
    expect(onOpenDeck).toHaveBeenCalledWith('d2')
  })

  it('goes back to Locations', async () => {
    const onBack = vi.fn()
    render(<DecksIndex onOpenDeck={vi.fn()} onBack={onBack} />)
    await screen.findByText('Main deck')
    fireEvent.click(screen.getByRole('button', { name: 'Back' }))
    expect(onBack).toHaveBeenCalled()
  })

  it('creates a deck by name and opens its page', async () => {
    const onOpenDeck = vi.fn()
    render(<DecksIndex onOpenDeck={onOpenDeck} onBack={vi.fn()} />)
    await screen.findByText('Main deck')
    fireEvent.click(screen.getByRole('button', { name: 'New deck' }))
    fireEvent.change(await screen.findByLabelText('Deck name'), { target: { value: 'Lower deck' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create' }))
    await waitFor(() => expect(onOpenDeck).toHaveBeenCalledWith('d3'))
  })

  it('shows the server message verbatim when a create is refused', async () => {
    const onOpenDeck = vi.fn()
    render(<DecksIndex onOpenDeck={onOpenDeck} onBack={vi.fn()} />)
    await screen.findByText('Main deck')
    fireEvent.click(screen.getByRole('button', { name: 'New deck' }))
    fireEvent.change(await screen.findByLabelText('Deck name'), { target: { value: 'main deck' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create' }))
    await screen.findByText('a deck with this name already exists')
    expect(onOpenDeck).not.toHaveBeenCalled()
  })

  it('explains what a deck is when there are none', async () => {
    decks = []
    render(<DecksIndex onOpenDeck={vi.fn()} onBack={vi.fn()} />)
    await screen.findByText('No decks yet')
    expect(screen.getByText(/picture of a deck/i)).toBeInTheDocument()
  })

  it('hides New deck when read-only', async () => {
    render(<DecksIndex onOpenDeck={vi.fn()} onBack={vi.fn()} canWrite={false} />)
    await screen.findByText('Main deck')
    expect(screen.queryByRole('button', { name: 'New deck' })).not.toBeInTheDocument()
  })
})
