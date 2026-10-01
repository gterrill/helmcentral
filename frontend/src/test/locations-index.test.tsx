import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { LocationsIndex } from '@/components/inventory/locations-index'
import type { InventoryZone } from '@/hooks/use-inventory'

let zones: InventoryZone[]
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
  fetchMock.mockReset()
  fetchMock.mockImplementation((url: string, init?: RequestInit) => {
    const u = String(url)
    const method = init?.method ?? 'GET'
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
  it('lists each location with its bin count in two columns', async () => {
    render(<LocationsIndex onOpenLocation={vi.fn()} />)
    await screen.findByText('Engine room (stbd)')

    const headers = screen.getAllByRole('columnheader').map((h) => h.textContent?.trim())
    expect(headers).toEqual(['Name', 'Bins'])

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
