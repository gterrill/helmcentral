import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { StocktakeSection } from '@/components/inventory/stocktake-section'
import type { EquipmentItem, InventoryZone } from '@/hooks/use-inventory'

// ADR 0127 (the plan's Phase B): the manual scan field is what drives every
// test here - "a keyboard-wedge or pasted URL or bin code, ending in
// Enter" is deliberately also what makes this section testable on desktop
// with no NFC hardware (the plan's own words). lib/nfc.ts's own behaviour
// is covered by nfc.test.ts/tag-row.test.tsx, not re-tested here.

function makeItem(overrides: Partial<EquipmentItem> = {}): EquipmentItem {
  return {
    id: 'eq-1',
    name: 'Spare impeller',
    category: 'general',
    system: 'other',
    manufacturer: '',
    model: '',
    serial: '',
    quantity: 1,
    status: 'stored',
    zone_id: 'z1',
    bin_id: 'b1',
    zone_name: 'Lazarette',
    bin_code: 'LAZ-02',
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
    ...overrides,
  }
}

let zones: InventoryZone[]
let binItemsByBinId: Record<string, EquipmentItem[]>
let equipmentById: Record<string, EquipmentItem>
let putCalls: { id: string; body: Record<string, unknown> }[]
const fetchMock = vi.fn()

beforeEach(() => {
  zones = [
    {
      id: 'z1', name: 'Lazarette', sort_index: 0,
      bins: [{ id: 'b1', zone_id: 'z1', code: 'LAZ-02', name: '', sort_index: 0 }],
    },
  ]
  binItemsByBinId = {}
  equipmentById = {}
  putCalls = []
  fetchMock.mockReset()
  fetchMock.mockImplementation((url: string, init?: RequestInit) => {
    const u = String(url)
    const method = init?.method ?? 'GET'

    if (u.endsWith('/api/inventory/zones') && method === 'GET') {
      return Promise.resolve({ ok: true, json: async () => ({ zones }) })
    }
    const binMatch = u.match(/\/api\/inventory\/equipment\?bin=([^&]+)/)
    if (binMatch && method === 'GET') {
      const binId = decodeURIComponent(binMatch[1])
      return Promise.resolve({ ok: true, json: async () => ({ items: binItemsByBinId[binId] ?? [] }) })
    }
    const idMatch = u.match(/\/api\/inventory\/equipment\/([^/?]+)$/)
    if (idMatch && method === 'GET') {
      const item = equipmentById[idMatch[1]]
      if (!item) return Promise.resolve({ ok: false, json: async () => ({ error: 'equipment not found' }) })
      return Promise.resolve({ ok: true, json: async () => ({ item }) })
    }
    if (idMatch && method === 'PUT') {
      const body = JSON.parse(String(init?.body)) as Record<string, unknown>
      putCalls.push({ id: idMatch[1], body })
      const updated = { ...equipmentById[idMatch[1]], ...body } as EquipmentItem
      equipmentById[idMatch[1]] = updated
      return Promise.resolve({ ok: true, json: async () => ({ item: updated }) })
    }
    return Promise.resolve({ ok: false, json: async () => ({ error: 'not found' }) })
  })
  vi.stubGlobal('fetch', fetchMock)
})

async function scan(text: string) {
  const input = screen.getByLabelText('Scan')
  fireEvent.change(input, { target: { value: text } })
  fireEvent.keyDown(input, { key: 'Enter' })
}

describe('StocktakeSection', () => {
  it('confirms an item recorded in the current bin and PUTs verified_aboard=true', async () => {
    const item = makeItem({ id: 'eq-1', bin_id: 'b1', verified_aboard: false })
    equipmentById['eq-1'] = item
    binItemsByBinId['b1'] = [item]
    render(<StocktakeSection />)
    await waitFor(() => expect(zones.length).toBeGreaterThan(0))

    await scan('https://boat.example/inventory/bins/LAZ-02')
    await screen.findByRole('heading', { name: 'LAZ-02' })

    await scan('https://boat.example/inventory/equipment/eq-1')

    await screen.findByText('Confirmed')
    await waitFor(() => {
      const call = putCalls.find((c) => c.id === 'eq-1')
      expect(call).toBeDefined()
      expect(call?.body.verified_aboard).toBe(true)
    })
  })

  it('offers Move for an item recorded elsewhere, and writes nothing until it is pressed', async () => {
    const item = makeItem({ id: 'eq-2', bin_id: 'b2', bin_code: 'SAL-04', zone_id: 'z2' })
    equipmentById['eq-2'] = item
    render(<StocktakeSection />)
    await waitFor(() => expect(zones.length).toBeGreaterThan(0))

    await scan('https://boat.example/inventory/bins/LAZ-02')
    await screen.findByRole('heading', { name: 'LAZ-02' })

    await scan('https://boat.example/inventory/equipment/eq-2')

    await screen.findByText(/Recorded in SAL-04/)
    expect(putCalls.find((c) => c.id === 'eq-2')).toBeUndefined()

    fireEvent.click(screen.getByRole('button', { name: 'Move to LAZ-02' }))

    await waitFor(() => {
      const call = putCalls.find((c) => c.id === 'eq-2')
      expect(call).toBeDefined()
      expect(call?.body.bin_id).toBe('b1')
      expect(call?.body.zone_id).toBe('z1')
    })
  })

  it('Move targets the bin that was current when the item was scanned, not whatever is current when pressed', async () => {
    zones = [
      {
        id: 'z1', name: 'Lazarette', sort_index: 0,
        bins: [
          { id: 'b1', zone_id: 'z1', code: 'LAZ-02', name: '', sort_index: 0 },
          { id: 'b3', zone_id: 'z1', code: 'FWD-01', name: '', sort_index: 1 },
        ],
      },
    ]
    const item = makeItem({ id: 'eq-2', bin_id: 'b2', bin_code: 'SAL-04', zone_id: 'z2' })
    equipmentById['eq-2'] = item
    render(<StocktakeSection />)
    await waitFor(() => expect(zones.length).toBeGreaterThan(0))

    await scan('https://boat.example/inventory/bins/LAZ-02')
    await screen.findByRole('heading', { name: 'LAZ-02' })

    await scan('https://boat.example/inventory/equipment/eq-2')
    await screen.findByText(/Recorded in SAL-04/)
    expect(screen.getByRole('button', { name: 'Move to LAZ-02' })).toBeInTheDocument()

    // Scanning a DIFFERENT bin afterward must not retarget an already
    // reported card - its own Move button still reads (and writes) LAZ-02.
    await scan('https://boat.example/inventory/bins/FWD-01')
    await screen.findByRole('heading', { name: 'FWD-01' })

    expect(screen.getByRole('button', { name: 'Move to LAZ-02' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Move to LAZ-02' }))

    await waitFor(() => {
      const call = putCalls.find((c) => c.id === 'eq-2')
      expect(call).toBeDefined()
      expect(call?.body.bin_id).toBe('b1')
      expect(call?.body.zone_id).toBe('z1')
    })
  })

  it('Move fetches the item fresh immediately before writing, keeping a name changed server-side since the scan', async () => {
    const item = makeItem({ id: 'eq-2', bin_id: 'b2', bin_code: 'SAL-04', zone_id: 'z2', name: 'Old name' })
    equipmentById['eq-2'] = item
    render(<StocktakeSection />)
    await waitFor(() => expect(zones.length).toBeGreaterThan(0))

    await scan('https://boat.example/inventory/bins/LAZ-02')
    await screen.findByRole('heading', { name: 'LAZ-02' })

    await scan('https://boat.example/inventory/equipment/eq-2')
    await screen.findByText(/Recorded in SAL-04/)

    // The record changes server-side AFTER the scan, before Move is pressed.
    equipmentById['eq-2'] = { ...equipmentById['eq-2'], name: 'New name' }

    fireEvent.click(screen.getByRole('button', { name: 'Move to LAZ-02' }))

    await waitFor(() => {
      const call = putCalls.find((c) => c.id === 'eq-2')
      expect(call).toBeDefined()
      expect(call?.body.name).toBe('New name')
    })
  })

  it('lists an unseen item under "Not seen this pass" and writes nothing for it', async () => {
    const seen = makeItem({ id: 'eq-1', name: 'Spare impeller', bin_id: 'b1' })
    const unseen = makeItem({ id: 'eq-2', name: 'Fuel filter', bin_id: 'b1' })
    equipmentById['eq-1'] = seen
    equipmentById['eq-2'] = unseen
    binItemsByBinId['b1'] = [seen, unseen]
    render(<StocktakeSection />)
    await waitFor(() => expect(zones.length).toBeGreaterThan(0))

    await scan('https://boat.example/inventory/bins/LAZ-02')
    await screen.findByRole('heading', { name: 'LAZ-02' })
    // The bin's own contents load once scanned in - both items appear
    // (photo grid) before either has been confirmed.
    await waitFor(() => expect(screen.getAllByText('Fuel filter').length).toBeGreaterThan(0))

    await scan('https://boat.example/inventory/equipment/eq-1')
    await screen.findByText('Confirmed')

    await screen.findByText('Not seen this pass')
    expect(putCalls.find((c) => c.id === 'eq-2')).toBeUndefined()
  })

  it('resolves a scan with no https:// scheme via its /inventory/ path', async () => {
    render(<StocktakeSection />)
    await waitFor(() => expect(zones.length).toBeGreaterThan(0))

    // A tailnet host pasted or scanned without its scheme - `new URL(...)`
    // throws on this, and the bug being fixed here used to fall back to
    // treating the WHOLE string (host and all) as a bin code.
    await scan('boat.tailnet.ts.net/inventory/bins/LAZ-02')

    await screen.findByRole('heading', { name: 'LAZ-02' })
    expect(screen.queryByText(/Not an inventory tag/)).not.toBeInTheDocument()
  })

  it('reports a schemeless scan with a slash but no /inventory/ path as not an inventory tag', async () => {
    render(<StocktakeSection />)
    await waitFor(() => expect(zones.length).toBeGreaterThan(0))

    await scan('some/other/path')

    await screen.findByText('Not an inventory tag: some/other/path')
  })

  it('reports an unrecognised scan without writing anything', async () => {
    render(<StocktakeSection />)
    await waitFor(() => expect(zones.length).toBeGreaterThan(0))

    await scan('not a url or a known bin code')

    await screen.findByText('Not an inventory tag: not a url or a known bin code')
    expect(putCalls).toHaveLength(0)
  })

  it('wires the bin photo grid\'s Open button to onOpenEquipment', async () => {
    const item = makeItem({ id: 'eq-1', bin_id: 'b1', photo_ids: [] })
    equipmentById['eq-1'] = item
    binItemsByBinId['b1'] = [item]
    const onOpenEquipment = vi.fn()
    render(<StocktakeSection onOpenEquipment={onOpenEquipment} />)
    await waitFor(() => expect(zones.length).toBeGreaterThan(0))

    await scan('https://boat.example/inventory/bins/LAZ-02')
    await waitFor(() => expect(screen.getAllByText('Spare impeller').length).toBeGreaterThan(0))

    fireEvent.click(screen.getByRole('button', { name: 'Open' }))
    expect(onOpenEquipment).toHaveBeenCalledWith('eq-1')
  })

  it('is read-only when canWrite is false: confirms without writing and hides Move', async () => {
    const item = makeItem({ id: 'eq-1', bin_id: 'b1', verified_aboard: false })
    const other = makeItem({ id: 'eq-2', bin_id: 'b2', bin_code: 'SAL-04', zone_id: 'z2' })
    equipmentById['eq-1'] = item
    equipmentById['eq-2'] = other
    render(<StocktakeSection canWrite={false} />)
    await waitFor(() => expect(zones.length).toBeGreaterThan(0))

    expect(screen.getByText(/read-only/i)).toBeInTheDocument()

    await scan('https://boat.example/inventory/bins/LAZ-02')
    await screen.findByRole('heading', { name: 'LAZ-02' })

    await scan('https://boat.example/inventory/equipment/eq-1')
    await screen.findByText('Confirmed')
    expect(putCalls.find((c) => c.id === 'eq-1')).toBeUndefined()

    await scan('https://boat.example/inventory/equipment/eq-2')
    await screen.findByText(/Recorded in SAL-04/)
    expect(screen.queryByRole('button', { name: /Move to/ })).not.toBeInTheDocument()
  })
})
