import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { EquipmentIndex } from '@/components/inventory/equipment-index'
import type { EquipmentItem, InventoryZone } from '@/hooks/use-inventory'

function makeItem(overrides: Partial<EquipmentItem>): EquipmentItem {
  return {
    id: 'eq-1',
    name: 'Generator',
    category: 'mechanical',
    system: 'electrical',
    manufacturer: 'Onan',
    model: '13.5 kW',
    serial: '',
    quantity: 1,
    status: 'deployed',
    zone_id: 'z1',
    bin_id: null,
    zone_name: 'Engine room (stbd)',
    bin_code: '',
    location_detail: '',
    install_date: '',
    hour_meter_path: '',
    profile_id: '',
    aliases: [],
    verified_aboard: false,
    notes: '',
    link_count: 0,
    created_at: '',
    updated_at: '',
    photo_ids: [],
    exclusive_photo_ids: [],
    ...overrides,
  }
}

const zones: InventoryZone[] = [
  { id: 'z1', name: 'Engine room (stbd)', sort_index: 0, bins: [] },
  { id: 'z2', name: 'Lazarette', sort_index: 1, bins: [{ id: 'b1', zone_id: 'z2', code: 'LAZ-01', name: '', sort_index: 0 }] },
]

const fetchMock = vi.fn()

beforeEach(() => {
  fetchMock.mockReset()
  fetchMock.mockImplementation((url: string) => {
    if (String(url).includes('/api/inventory/zones')) {
      return Promise.resolve({ ok: true, json: async () => ({ zones }) })
    }
    if (String(url).includes('/api/inventory/equipment')) {
      return Promise.resolve({ ok: true, json: async () => ({ items: [] }) })
    }
    return Promise.resolve({ ok: false, json: async () => ({ error: 'not found' }) })
  })
  vi.stubGlobal('fetch', fetchMock)
})

describe('EquipmentIndex', () => {
  it('groups items by system in enum order, not alphabetically', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (String(url).includes('/api/inventory/zones')) return Promise.resolve({ ok: true, json: async () => ({ zones }) })
      if (String(url).includes('/api/inventory/equipment')) {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            items: [
              makeItem({ id: 'eq-bilge', name: 'Bilge pump', system: 'bilge' }),
              makeItem({ id: 'eq-electrical', name: 'Generator', system: 'electrical' }),
            ],
          }),
        })
      }
      return Promise.resolve({ ok: false, json: async () => ({}) })
    })

    render(<EquipmentIndex onOpenItem={vi.fn()} onNewItem={vi.fn()} />)

    await screen.findByText('Generator')

    // 'electrical' precedes 'bilge' in EQUIPMENT_SYSTEMS (backend/frontend
    // enum order), even though alphabetically 'bilge' would come first.
    const headings = screen.getAllByText(/^(Electrical|Bilge)$/).map((el) => el.textContent)
    expect(headings.indexOf('Electrical')).toBeLessThan(headings.indexOf('Bilge'))
  })

  it('shows an empty state inviting New item when there are no results', async () => {
    render(<EquipmentIndex onOpenItem={vi.fn()} onNewItem={vi.fn()} />)

    await waitFor(() => expect(screen.getByText(/no equipment/i)).toBeInTheDocument())
    expect(screen.getAllByRole('button', { name: 'New item' }).length).toBeGreaterThan(0)
  })

  it('opens the editor on a row click', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (String(url).includes('/api/inventory/zones')) return Promise.resolve({ ok: true, json: async () => ({ zones }) })
      if (String(url).includes('/api/inventory/equipment')) {
        return Promise.resolve({ ok: true, json: async () => ({ items: [makeItem({})] }) })
      }
      return Promise.resolve({ ok: false, json: async () => ({}) })
    })
    const onOpenItem = vi.fn()
    render(<EquipmentIndex onOpenItem={onOpenItem} onNewItem={vi.fn()} />)

    fireEvent.click(await screen.findByRole('button', { name: 'Generator' }))
    expect(onOpenItem).toHaveBeenCalledWith('eq-1')
  })

  it('calls onNewItem from the toolbar button', async () => {
    const onNewItem = vi.fn()
    render(<EquipmentIndex onOpenItem={vi.fn()} onNewItem={onNewItem} />)

    await waitFor(() => expect(fetchMock).toHaveBeenCalled())
    fireEvent.click(screen.getAllByRole('button', { name: 'New item' })[0])
    expect(onNewItem).toHaveBeenCalled()
  })

  it('shows the zone/bin location and document count for a row', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (String(url).includes('/api/inventory/zones')) return Promise.resolve({ ok: true, json: async () => ({ zones }) })
      if (String(url).includes('/api/inventory/equipment')) {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            items: [makeItem({ zone_name: 'Lazarette', bin_code: 'LAZ-01', link_count: 3 })],
          }),
        })
      }
      return Promise.resolve({ ok: false, json: async () => ({}) })
    })
    render(<EquipmentIndex onOpenItem={vi.fn()} onNewItem={vi.fn()} />)

    await screen.findByText('Generator')
    expect(screen.getByText('Lazarette / LAZ-01')).toBeInTheDocument()
    expect(screen.getByText('3')).toBeInTheDocument()
  })

  it('sends the search box text as the q query parameter', async () => {
    render(<EquipmentIndex onOpenItem={vi.fn()} onNewItem={vi.fn()} />)
    await waitFor(() => expect(fetchMock).toHaveBeenCalled())
    fetchMock.mockClear()

    fireEvent.change(screen.getByLabelText('Search equipment'), { target: { value: 'genset' } })

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(([url]) => String(url).includes('/api/inventory/equipment?'))
      expect(call).toBeDefined()
      expect(String(call?.[0])).toContain('q=genset')
    })
  })

  it('sends the category filter as a query parameter', async () => {
    render(<EquipmentIndex onOpenItem={vi.fn()} onNewItem={vi.fn()} />)
    await waitFor(() => expect(fetchMock).toHaveBeenCalled())
    fetchMock.mockClear()

    fireEvent.click(screen.getByRole('combobox', { name: 'Filter by category' }))
    const option = await screen.findByRole('option', { name: 'Mechanical' })
    fireEvent.pointerDown(option)
    fireEvent.pointerUp(option)
    fireEvent.click(option)

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(([url]) => String(url).includes('/api/inventory/equipment?'))
      expect(call).toBeDefined()
      expect(String(call?.[0])).toContain('category=mechanical')
    })
  })
})
