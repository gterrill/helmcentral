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
    part_number: '',
    required_quantity: null,
    status: 'deployed',
    zone_id: 'z1',
    bin_id: null,
    zone_name: 'Engine room (stbd)',
    bin_code: '',
    location_detail: '',
    install_date: '',
    signalk_source: '', capacity_ah: 0, cells: 0, warn_voltage: 0, high_voltage: 0, source_kind: '', hour_meter_source: '',
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

const NO_DEPENDANTS = { item_id: 'eq-1', tiles: [], roles: [], alarm_rules: [], features: [] }

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

    fireEvent.click(await screen.findByText('Generator'))
    expect(onOpenItem).toHaveBeenCalledWith('eq-1')
  })

  it('opens the editor from the row\'s own action menu, without double-firing the row click', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (String(url).includes('/api/inventory/zones')) return Promise.resolve({ ok: true, json: async () => ({ zones }) })
      if (String(url).includes('/api/inventory/equipment')) {
        return Promise.resolve({ ok: true, json: async () => ({ items: [makeItem({})] }) })
      }
      return Promise.resolve({ ok: false, json: async () => ({}) })
    })
    const onOpenItem = vi.fn()
    render(<EquipmentIndex onOpenItem={onOpenItem} onNewItem={vi.fn()} />)

    await screen.findByText('Generator')
    fireEvent.click(screen.getByRole('button', { name: 'Actions for Generator' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Open' }))

    expect(onOpenItem).toHaveBeenCalledTimes(1)
    expect(onOpenItem).toHaveBeenCalledWith('eq-1')
    // Let the menu finish closing before the test ends: unmounting mid-close
    // left Base UI in a state where the NEXT test's first trigger click was
    // swallowed (no open-change at all) - the flake in "deletes an item...".
    await waitFor(() => expect(screen.queryByRole('menu')).not.toBeInTheDocument())
  })

  it('deletes an item from the row action menu, after confirming', async () => {
    // The list read that follows the delete is held, to see what the dialog
    // does while it is outstanding.
    let listReadCount = 0
    let releaseSecondRead: () => void = () => {}
    const secondRead = new Promise<void>((resolve) => { releaseSecondRead = resolve })
    fetchMock.mockImplementation(async (url: string, init?: RequestInit) => {
      if (String(url).includes('/api/inventory/zones')) return { ok: true, json: async () => ({ zones }) }
      if (init?.method === 'DELETE') return { ok: true, status: 204, json: async () => ({}) }
      if (String(url).endsWith('/dependants')) return { ok: true, json: async () => NO_DEPENDANTS }
      if (String(url).includes('/api/inventory/equipment')) {
        if (++listReadCount === 2) await secondRead
        return { ok: true, json: async () => ({ items: listReadCount === 1 ? [makeItem({})] : [] }) }
      }
      return { ok: false, json: async () => ({}) }
    })
    render(<EquipmentIndex onOpenItem={vi.fn()} onNewItem={vi.fn()} />)

    await screen.findByText('Generator')
    fireEvent.click(screen.getByRole('button', { name: 'Actions for Generator' }))
    // The zones request landing re-renders the table; that used to remount
    // the row menu and drop the click (fixed in IndexTable, see its test).
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Delete' }))

    fireEvent.click(await screen.findByRole('button', { name: 'Delete' }))

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        expect.stringMatching(/\/api\/inventory\/equipment\/eq-1$/),
        expect.objectContaining({ method: 'DELETE' }),
      )
    })
    // The dialog stays up, row still listed, until the fresh list has landed.
    await waitFor(() => expect(listReadCount).toBe(2))
    expect(screen.getByRole('alertdialog')).toBeInTheDocument()
    expect(screen.getByText('Generator')).toBeInTheDocument()
    releaseSecondRead()
    await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument())
    expect(screen.queryByText('Generator')).not.toBeInTheDocument()
    // One read after the delete, not an invalidate and a refetch.
    await new Promise((r) => setTimeout(r, 30))
    expect(listReadCount).toBe(2)
  })

  it('says what uses the item before a delete, and deletes only once that is known', async () => {
    fetchMock.mockImplementation(async (url: string, init?: RequestInit) => {
      if (String(url).includes('/api/inventory/zones')) return { ok: true, json: async () => ({ zones }) }
      if (init?.method === 'DELETE') return { ok: true, status: 204, json: async () => ({}) }
      if (String(url).endsWith('/eq-1/dependants')) {
        return { ok: true, json: async () => ({
          ...NO_DEPENDANTS,
          tiles: [{ page_id: 'p1', page_name: 'Underway', tile_id: 'cluster:a', title: 'Port' }],
          roles: [{ role: 'house_bank', label: 'House bank' }],
        }) }
      }
      if (String(url).includes('/api/inventory/equipment')) return { ok: true, json: async () => ({ items: [makeItem({})] }) }
      return { ok: false, json: async () => ({}) }
    })
    render(<EquipmentIndex onOpenItem={vi.fn()} onNewItem={vi.fn()} />)

    await screen.findByText('Generator')
    fireEvent.click(screen.getByRole('button', { name: 'Actions for Generator' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Delete' }))

    const notice = await screen.findByTestId('equipment-dependants')
    expect(notice).toHaveTextContent('Used by 1 tile (Underway: Port), the House bank role.')
    expect(notice).toHaveTextContent('Tiles stay and show Equipment removed')
  })

  it('will not delete while what uses the item cannot be checked', async () => {
    fetchMock.mockImplementation(async (url: string) => {
      if (String(url).includes('/api/inventory/zones')) return { ok: true, json: async () => ({ zones }) }
      if (String(url).endsWith('/eq-1/dependants')) return { ok: false, status: 500, json: async () => ({ error: 'store down' }) }
      if (String(url).includes('/api/inventory/equipment')) return { ok: true, json: async () => ({ items: [makeItem({})] }) }
      return { ok: false, json: async () => ({}) }
    })
    render(<EquipmentIndex onOpenItem={vi.fn()} onNewItem={vi.fn()} />)

    await screen.findByText('Generator')
    fireEvent.click(screen.getByRole('button', { name: 'Actions for Generator' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Delete' }))

    expect(await screen.findByText(/Could not check what uses this item: store down/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Delete' })).toBeDisabled()
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

  // category is derived from the hour meter now, so the index neither shows
  // nor filters on it.
  it('has no category / "Serviced by" column or filter', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (String(url).includes('/api/inventory/zones')) return Promise.resolve({ ok: true, json: async () => ({ zones }) })
      if (String(url).includes('/api/inventory/equipment')) {
        return Promise.resolve({
          ok: true,
          json: async () => ({ items: [makeItem({ id: 'a', name: 'Generator', category: 'mechanical' }), makeItem({ id: 'b', name: 'Flares', category: 'general' })] }),
        })
      }
      return Promise.resolve({ ok: false, json: async () => ({}) })
    })
    render(<EquipmentIndex onOpenItem={vi.fn()} onNewItem={vi.fn()} />)
    await screen.findByText('Generator')

    expect(screen.queryByRole('columnheader', { name: /serviced by|category/i })).not.toBeInTheDocument()
    expect(screen.queryByRole('combobox', { name: /serviced by|category/i })).not.toBeInTheDocument()
    expect(screen.queryByText('Running hours')).not.toBeInTheDocument()
    expect(screen.queryByText('Calendar')).not.toBeInTheDocument()
    expect(fetchMock.mock.calls.some(([url]) => String(url).includes('category='))).toBe(false)
  })
})
