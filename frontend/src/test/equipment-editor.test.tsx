import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { EquipmentEditor } from '@/components/inventory/equipment-editor'
import type { EquipmentItem, EquipmentDocument, InventoryZone } from '@/hooks/use-inventory'

function makeItem(overrides: Partial<EquipmentItem> = {}): EquipmentItem {
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
    zone_id: null,
    bin_id: null,
    zone_name: '',
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
    ...overrides,
  }
}

const zones: InventoryZone[] = [
  {
    id: 'z1', name: 'Engine room (stbd)', sort_index: 0,
    bins: [{ id: 'b1', zone_id: 'z1', code: 'ER-01', name: '', sort_index: 0 }],
  },
  {
    id: 'z2', name: 'Lazarette', sort_index: 1,
    bins: [{ id: 'b2', zone_id: 'z2', code: 'LAZ-01', name: '', sort_index: 0 }],
  },
]

const profiles = [
  {
    schema_version: 1, kind: 'generator', id: 'cummins-onan-13-5kw-60hz', name: 'Onan 13.5kW',
    manufacturer: 'Onan', model: '13.5 kW MDKD', gauges: [],
  },
]

let currentItem: EquipmentItem
let currentDocuments: EquipmentDocument[]
const fetchMock = vi.fn()

function stubFetch() {
  fetchMock.mockImplementation((url: string, init?: RequestInit) => {
    const u = String(url)
    const method = init?.method ?? 'GET'

    if (u.includes('/api/inventory/zones')) {
      return Promise.resolve({ ok: true, json: async () => ({ zones }) })
    }
    if (u.includes('/api/equipment-profiles')) {
      return Promise.resolve({ ok: true, json: async () => ({ profiles, problems: [] }) })
    }
    if (u.includes('/api/signalk/paths')) {
      return Promise.resolve({
        ok: true,
        json: async () => ({ paths: [{ path: 'electrical.generator.0.runTime' }, { path: 'navigation.speedOverGround' }] }),
      })
    }
    if (u.match(/\/api\/inventory\/equipment\/eq-1\/documents$/) && method === 'PUT') {
      const body = JSON.parse(String(init?.body)) as { document_ids: string[] }
      currentDocuments = currentDocuments.filter((d) => body.document_ids.includes(d.document_id))
      return Promise.resolve({ ok: true, status: 204, json: async () => ({}) })
    }
    if (u.match(/\/api\/inventory\/equipment\/eq-1$/) && method === 'GET') {
      return Promise.resolve({ ok: true, json: async () => ({ item: currentItem, documents: currentDocuments }) })
    }
    if (u.match(/\/api\/inventory\/equipment\/eq-1$/) && method === 'PUT') {
      const body = JSON.parse(String(init?.body))
      currentItem = { ...currentItem, ...body }
      return Promise.resolve({ ok: true, json: async () => ({ item: currentItem }) })
    }
    if (u.match(/\/api\/inventory\/equipment\/eq-1$/) && method === 'DELETE') {
      return Promise.resolve({ ok: true, status: 204, json: async () => ({}) })
    }
    if (u.match(/\/api\/inventory\/equipment$/) && method === 'POST') {
      const body = JSON.parse(String(init?.body))
      return Promise.resolve({ ok: true, status: 201, json: async () => ({ item: { ...makeItem(body), id: 'eq-new' } }) })
    }
    return Promise.resolve({ ok: false, json: async () => ({ error: 'not found' }) })
  })
  vi.stubGlobal('fetch', fetchMock)
}

beforeEach(() => {
  fetchMock.mockReset()
  currentItem = makeItem()
  currentDocuments = []
  stubFetch()
})

async function waitForLoaded() {
  await waitFor(() => expect(screen.getByLabelText('Name')).toHaveValue('Generator'))
}

describe('EquipmentEditor', () => {
  it('sends the edited draft fields in the PUT body on Save', async () => {
    render(<EquipmentEditor id="eq-1" onBack={vi.fn()} onCreated={vi.fn()} onDeleted={vi.fn()} />)
    await waitForLoaded()

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Onan Generator' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(([url, init]) =>
        String(url).endsWith('/api/inventory/equipment/eq-1') && (init as RequestInit | undefined)?.method === 'PUT')
      expect(call).toBeDefined()
      const body = JSON.parse(String((call?.[1] as RequestInit).body))
      expect(body.name).toBe('Onan Generator')
    })
  })

  it('POSTs a new item and calls onCreated with the new id', async () => {
    const onCreated = vi.fn()
    render(<EquipmentEditor id={null} onBack={vi.fn()} onCreated={onCreated} onDeleted={vi.fn()} />)

    await waitFor(() => expect(screen.getByLabelText('Name')).toHaveValue(''))
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Spare impeller' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(onCreated).toHaveBeenCalledWith('eq-new'))
    const postCall = fetchMock.mock.calls.find(([url, init]) =>
      String(url).endsWith('/api/inventory/equipment') && (init as RequestInit | undefined)?.method === 'POST')
    expect(postCall).toBeDefined()
  })

  it('prefills blank manufacturer/model and sets category to mechanical when a profile is chosen', async () => {
    currentItem = makeItem({ manufacturer: '', model: '', category: 'general' })
    render(<EquipmentEditor id="eq-1" onBack={vi.fn()} onCreated={vi.fn()} onDeleted={vi.fn()} />)
    await waitForLoaded()

    fireEvent.click(screen.getByRole('combobox', { name: 'Profile' }))
    const option = await screen.findByRole('option', { name: 'Onan 13.5kW' })
    fireEvent.pointerDown(option)
    fireEvent.pointerUp(option)
    fireEvent.click(option)

    await waitFor(() => expect(screen.getByLabelText('Manufacturer')).toHaveValue('Onan'))
    expect(screen.getByLabelText('Model')).toHaveValue('13.5 kW MDKD')
  })

  it('never overwrites a manufacturer the operator already typed when picking a profile', async () => {
    currentItem = makeItem({ manufacturer: 'Kohler', model: '', category: 'general' })
    render(<EquipmentEditor id="eq-1" onBack={vi.fn()} onCreated={vi.fn()} onDeleted={vi.fn()} />)
    await waitForLoaded()
    expect(screen.getByLabelText('Manufacturer')).toHaveValue('Kohler')

    fireEvent.click(screen.getByRole('combobox', { name: 'Profile' }))
    const option = await screen.findByRole('option', { name: 'Onan 13.5kW' })
    fireEvent.pointerDown(option)
    fireEvent.pointerUp(option)
    fireEvent.click(option)

    await waitFor(() => expect(screen.getByLabelText('Model')).toHaveValue('13.5 kW MDKD'))
    // The operator's own manufacturer survives - only the blank model filled in.
    expect(screen.getByLabelText('Manufacturer')).toHaveValue('Kohler')
  })

  it('constrains the bin select to the chosen zone\'s bins', async () => {
    currentItem = makeItem({ zone_id: 'z1', zone_name: 'Engine room (stbd)' })
    render(<EquipmentEditor id="eq-1" onBack={vi.fn()} onCreated={vi.fn()} onDeleted={vi.fn()} />)
    await waitForLoaded()

    fireEvent.click(screen.getByRole('combobox', { name: 'Bin' }))
    expect(await screen.findByRole('option', { name: 'ER-01' })).toBeInTheDocument()
    expect(screen.queryByRole('option', { name: 'LAZ-01' })).not.toBeInTheDocument()
  })

  it('setting a bin with no zone chosen sets the zone from that bin', async () => {
    currentItem = makeItem({ zone_id: null, bin_id: null })
    render(<EquipmentEditor id="eq-1" onBack={vi.fn()} onCreated={vi.fn()} onDeleted={vi.fn()} />)
    await waitForLoaded()

    fireEvent.click(screen.getByRole('combobox', { name: 'Bin' }))
    const option = await screen.findByRole('option', { name: 'LAZ-01' })
    fireEvent.pointerDown(option)
    fireEvent.pointerUp(option)
    fireEvent.click(option)

    await waitFor(() => expect(screen.getByRole('combobox', { name: 'Zone' })).toHaveTextContent('Lazarette'))
  })

  it('adds and removes alias chips', async () => {
    render(<EquipmentEditor id="eq-1" onBack={vi.fn()} onCreated={vi.fn()} onDeleted={vi.fn()} />)
    await waitForLoaded()

    fireEvent.change(screen.getByLabelText('Add alias'), { target: { value: 'genset' } })
    fireEvent.keyDown(screen.getByLabelText('Add alias'), { key: 'Enter' })
    expect(screen.getByText('genset')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Remove alias genset' }))
    expect(screen.queryByText('genset')).not.toBeInTheDocument()
  })

  it('reports dirty as soon as a field changes and clears it after a successful save', async () => {
    const onDirtyChange = vi.fn()
    render(<EquipmentEditor id="eq-1" onBack={vi.fn()} onCreated={vi.fn()} onDeleted={vi.fn()} onDirtyChange={onDirtyChange} />)
    await waitForLoaded()
    onDirtyChange.mockClear()

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Renamed' } })
    expect(onDirtyChange).toHaveBeenCalledWith(true)

    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(onDirtyChange).toHaveBeenLastCalledWith(false))
  })

  it('does not PUT the documents link set when it was not touched', async () => {
    currentDocuments = [{ document_id: 'd1', title: 'Manual', filename: 'manual.pdf', kind: 'file', note_type: '', source: 'operator' }]
    render(<EquipmentEditor id="eq-1" onBack={vi.fn()} onCreated={vi.fn()} onDeleted={vi.fn()} />)
    await waitForLoaded()
    await screen.findByText('Manual')

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Generator 2' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(fetchMock.mock.calls.some(([url, init]) =>
      String(url).endsWith('/api/inventory/equipment/eq-1') && (init as RequestInit | undefined)?.method === 'PUT')).toBe(true))

    expect(fetchMock.mock.calls.some(([url]) => String(url).endsWith('/documents'))).toBe(false)
  })

  it('PUTs the reduced documents link set once a linked document is removed', async () => {
    currentDocuments = [{ document_id: 'd1', title: 'Manual', filename: 'manual.pdf', kind: 'file', note_type: '', source: 'operator' }]
    render(<EquipmentEditor id="eq-1" onBack={vi.fn()} onCreated={vi.fn()} onDeleted={vi.fn()} />)
    await waitForLoaded()
    await screen.findByText('Manual')

    fireEvent.click(screen.getByRole('button', { name: 'Remove document Manual' }))
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(([url, init]) =>
        String(url).endsWith('/api/inventory/equipment/eq-1/documents') && (init as RequestInit | undefined)?.method === 'PUT')
      expect(call).toBeDefined()
      const body = JSON.parse(String((call?.[1] as RequestInit).body))
      expect(body.document_ids).toEqual([])
    })
  })

  it('asks for confirmation before deleting and calls onDeleted once accepted', async () => {
    const onDeleted = vi.fn()
    render(<EquipmentEditor id="eq-1" onBack={vi.fn()} onCreated={vi.fn()} onDeleted={onDeleted} />)
    await waitForLoaded()

    fireEvent.click(screen.getByRole('button', { name: 'Delete equipment' }))
    expect(screen.getByText('Delete "Generator"?')).toBeInTheDocument()
    expect(fetchMock.mock.calls.some(([, init]) => (init as RequestInit | undefined)?.method === 'DELETE')).toBe(false)

    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))

    await waitFor(() => expect(onDeleted).toHaveBeenCalled())
    expect(fetchMock.mock.calls.some(([, init]) => (init as RequestInit | undefined)?.method === 'DELETE')).toBe(true)
  })
})
