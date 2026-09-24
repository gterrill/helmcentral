import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { BinPage } from '@/components/inventory/bin-page'
import type { EquipmentItem, InventoryZone } from '@/hooks/use-inventory'

// ADR 0127 (the plan's A4): BinQuickAdd and TagRow are each already
// covered by their own test file (bin-quick-add.test.tsx, tag-row.test.tsx)
// - mocked here the same way inventory-panel.test.tsx mocks EquipmentIndex/
// EquipmentEditor, so this file only exercises BinPage's own job: resolving
// a code against the zone/bin tree, the not-found/create flow, and the
// contents list + photo stack.
vi.mock('@/components/inventory/bin-quick-add', () => ({
  // An uncontrolled input stands in for the real form's draft: its typed
  // value survives a re-render and is cleared only by a remount.
  BinQuickAdd: () => <div data-testid="bin-quick-add"><input aria-label="Quick add draft" /></div>,
}))
vi.mock('@/components/inventory/tag-row', () => ({
  TagRow: (props: { path: string }) => <div data-testid="tag-row">{props.path}</div>,
}))

function makeItem(overrides: Partial<EquipmentItem> = {}): EquipmentItem {
  return {
    id: 'eq-1',
    name: 'Gaffer tape',
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
let items: EquipmentItem[]
const fetchMock = vi.fn()

beforeEach(() => {
  zones = [
    {
      id: 'z1', name: 'Lazarette', sort_index: 0,
      bins: [{ id: 'b1', zone_id: 'z1', code: 'LAZ-02', name: 'Adhesives', sort_index: 0 }],
    },
  ]
  items = []
  fetchMock.mockReset()
  fetchMock.mockImplementation((url: string, init?: RequestInit) => {
    const u = String(url)
    const method = init?.method ?? 'GET'

    if (u.endsWith('/api/inventory/zones') && method === 'GET') {
      return Promise.resolve({ ok: true, json: async () => ({ zones }) })
    }
    if (u.includes('/api/inventory/equipment?bin=') && method === 'GET') {
      return Promise.resolve({ ok: true, json: async () => ({ items }) })
    }
    if (u.endsWith('/api/inventory/bins') && method === 'POST') {
      const body = JSON.parse(String(init?.body)) as { zone_id: string; code: string; name: string }
      const bin = { id: 'b2', zone_id: body.zone_id, code: body.code, name: body.name, sort_index: 1 }
      zones = zones.map((z) => (z.id === body.zone_id ? { ...z, bins: [...z.bins, bin] } : z))
      return Promise.resolve({ ok: true, status: 201, json: async () => ({ bin }) })
    }
    return Promise.resolve({ ok: false, json: async () => ({ error: 'not found' }) })
  })
  vi.stubGlobal('fetch', fetchMock)
})

describe('BinPage', () => {
  it('resolves a bin code case-insensitively', async () => {
    render(<BinPage code="laz-02" onClose={vi.fn()} onOpenEquipment={vi.fn()} onNewEquipment={vi.fn()} />)
    await screen.findByText('LAZ-02')
    expect(screen.queryByText(/No bin/)).not.toBeInTheDocument()
  })

  it('shows the not-found state for an unknown code', async () => {
    render(<BinPage code="NOPE" onClose={vi.fn()} onOpenEquipment={vi.fn()} onNewEquipment={vi.fn()} />)
    await screen.findByText('No bin')
    expect(screen.getByText('NOPE')).toBeInTheDocument()
  })

  // Review finding: a failed zones fetch used to be indistinguishable from
  // "no such bin" - useInventoryZones' own error was never read, so a
  // network failure landed on the same "No bin NOPE" + Create state a
  // genuinely unknown code does, offering to create a duplicate of a bin
  // that may well already exist.
  it('shows the zones fetch error instead of the not-found/create state', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (String(url).endsWith('/api/inventory/zones')) {
        return Promise.resolve({ ok: false, status: 500, json: async () => ({ error: 'zones unavailable' }) })
      }
      return Promise.resolve({ ok: false, json: async () => ({ error: 'not found' }) })
    })

    render(<BinPage code="LAZ-02" onClose={vi.fn()} onOpenEquipment={vi.fn()} onNewEquipment={vi.fn()} />)

    await screen.findByText('zones unavailable')
    expect(screen.queryByText(/No bin/)).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Create bin/ })).not.toBeInTheDocument()
  })

  it('does not show "No bin" while zones are still loading', async () => {
    let resolveZones!: (value: { ok: boolean; json: () => Promise<unknown> }) => void
    fetchMock.mockImplementation((url: string) => {
      if (String(url).endsWith('/api/inventory/zones')) {
        return new Promise((resolve) => { resolveZones = resolve })
      }
      return Promise.resolve({ ok: false, json: async () => ({ error: 'not found' }) })
    })

    render(<BinPage code="LAZ-02" onClose={vi.fn()} onOpenEquipment={vi.fn()} onNewEquipment={vi.fn()} />)

    expect(screen.queryByText(/No bin/)).not.toBeInTheDocument()
    expect(screen.getByText('Loading...')).toBeInTheDocument()

    resolveZones({ ok: true, json: async () => ({ zones }) })
    await screen.findByText('LAZ-02')
  })

  it('offers Create bin for an unknown code, and creating it shows the empty bin', async () => {
    render(<BinPage code="NOPE" onClose={vi.fn()} onOpenEquipment={vi.fn()} onNewEquipment={vi.fn()} />)
    await screen.findByText('No bin')

    fireEvent.click(screen.getByRole('button', { name: 'Create bin NOPE' }))
    fireEvent.click(screen.getByRole('button', { name: 'Create' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/api/inventory/bins'),
      expect.objectContaining({ method: 'POST' }),
    ))
    await screen.findByText('NOPE')
    await screen.findByText('0 items')
  })

  it('renders three images in order with the 1 / 3 counter for an item with three photos', async () => {
    items = [makeItem({ photo_ids: ['p1', 'p2', 'p3'] })]
    const { container } = render(<BinPage code="LAZ-02" onClose={vi.fn()} onOpenEquipment={vi.fn()} onNewEquipment={vi.fn()} />)
    await waitFor(() => expect(screen.getAllByText('Gaffer tape').length).toBeGreaterThan(0))

    // Decorative (alt="") since the item's own name is right underneath -
    // that removes them from the accessibility tree's "img" role, so this
    // reaches into the DOM directly rather than via getByRole.
    const images = Array.from(container.querySelectorAll('img'))
    expect(images.map((img) => img.src)).toEqual([
      expect.stringContaining('/api/documents/p1/content'),
      expect.stringContaining('/api/documents/p2/content'),
      expect.stringContaining('/api/documents/p3/content'),
    ])
    expect(screen.getByText('1 / 3')).toBeInTheDocument()
  })

  it('shows "No photo" for an item with no photo', async () => {
    items = [makeItem({ photo_ids: [] })]
    render(<BinPage code="LAZ-02" onClose={vi.fn()} onOpenEquipment={vi.fn()} onNewEquipment={vi.fn()} />)
    await screen.findByText('No photo')
  })

  it('renders the created bin through the normal path with real callbacks (not dead buttons)', async () => {
    items = [makeItem({ id: 'eq-2', zone_id: 'z1', bin_id: 'b2' })]
    const onOpenEquipment = vi.fn()
    const onNewEquipment = vi.fn()
    render(<BinPage code="NOPE" onClose={vi.fn()} onOpenEquipment={onOpenEquipment} onNewEquipment={onNewEquipment} />)
    await screen.findByText('No bin')

    fireEvent.click(screen.getByRole('button', { name: 'Create bin NOPE' }))
    fireEvent.click(screen.getByRole('button', { name: 'Create' }))

    await screen.findByText('NOPE')
    await waitFor(() => expect(screen.getAllByText('Gaffer tape').length).toBeGreaterThan(0))

    fireEvent.click(screen.getByRole('button', { name: 'Full item' }))
    expect(onNewEquipment).toHaveBeenCalledWith({ zoneId: 'z1', binId: 'b2' })

    fireEvent.click(screen.getAllByRole('button', { name: 'Open' })[0])
    expect(onOpenEquipment).toHaveBeenCalledWith('eq-2')
  })

  it("builds the Tag row's path with formatAppLocation, encoded like the app's own URLs", async () => {
    zones = [
      {
        id: 'z1', name: 'Lazarette', sort_index: 0,
        bins: [{ id: 'b1', zone_id: 'z1', code: 'LAZ 02', name: '', sort_index: 0 }],
      },
    ]
    render(<BinPage code="LAZ 02" onClose={vi.fn()} onOpenEquipment={vi.fn()} onNewEquipment={vi.fn()} />)
    await screen.findByText('LAZ 02')
    expect(screen.getByTestId('tag-row')).toHaveTextContent('/inventory/bins/LAZ%2002')
  })

  it('pre-sets the bin and zone when Full item is pressed', async () => {
    const onNewEquipment = vi.fn()
    render(<BinPage code="LAZ-02" onClose={vi.fn()} onOpenEquipment={vi.fn()} onNewEquipment={onNewEquipment} />)
    await screen.findByText('LAZ-02')

    fireEvent.click(screen.getByRole('button', { name: 'Full item' }))

    expect(onNewEquipment).toHaveBeenCalledWith({ zoneId: 'z1', binId: 'b1' })
  })

  // Final pre-release review finding: the quick-add form was not reset when
  // the bin changed, so Back/Forward from one bin to another carried a typed
  // name and staged photos across, and Save then filed them in the wrong bin.
  it("starts a fresh quick-add draft when the bin changes", async () => {
    zones = [
      {
        id: "z1", name: "Lazarette", sort_index: 0,
        bins: [
          { id: "b1", zone_id: "z1", code: "LAZ-02", name: "Adhesives", sort_index: 0 },
          { id: "b2", zone_id: "z1", code: "LAZ-03", name: "Fasteners", sort_index: 1 },
        ],
      },
    ]
    const props = { onClose: vi.fn(), onOpenEquipment: vi.fn(), onNewEquipment: vi.fn() }
    const { rerender } = render(<BinPage code="LAZ-02" {...props} />)
    const draft = await screen.findByLabelText("Quick add draft")
    fireEvent.change(draft, { target: { value: "Gaffer tape" } })

    rerender(<BinPage code="LAZ-03" {...props} />)
    await screen.findByText("Fasteners")

    expect((screen.getByLabelText("Quick add draft") as HTMLInputElement).value).toBe("")
  })
})
