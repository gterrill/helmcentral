import { useState } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { EquipmentEditor } from '@/components/inventory/equipment-editor'
import type { EquipmentItem, EquipmentDocument, InventoryZone } from '@/hooks/use-inventory'

// ADR 0127: the photo row runs every picked file through downscaleImage
// before it ever reaches the network (canvas/createImageBitmap aren't
// implemented by jsdom - image-downscale.test.ts covers that function's
// OWN logic against mocked browser APIs; this file only needs it to be a
// harmless pass-through so the rest of the upload flow can be exercised).
vi.mock('@/lib/image-downscale', () => ({
  downscaleImage: vi.fn(async (file: Blob) => file),
}))

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
    photo_ids: [],
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
let uploadedPhotoOrder: string[]
let failingPhotoUploadNames: Set<string>
// Release-fixes code-review finding: a 409 ("already in Documents") can
// never succeed on Retry - it's the same bytes every time - so it needs its
// own fixture, distinct from failingPhotoUploadNames' plain 500s which DO
// belong on the retry queue.
let conflictPhotoUploadNames: Map<string, string>
const fetchMock = vi.fn()

// Mirrors backend/inventory_store.go's normalizeAliases: trim every alias,
// drop blanks, dedupe case-insensitively (folding only the comparison key,
// keeping first-seen casing) - see that function's own comment.
function normalizeAliasesLikeServer(aliases: string[]): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  for (const alias of aliases) {
    const trimmed = alias.trim()
    if (trimmed === '') continue
    const key = trimmed.toLowerCase()
    if (seen.has(key)) continue
    seen.add(key)
    out.push(trimmed)
  }
  return out
}

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
    // Generic, not just eq-1: once a draft's create POST assigns 'eq-new'
    // (below), the useEquipmentItem hook immediately GETs that new id -
    // currentItem is the single record these fixtures track either way.
    if (u.match(/\/api\/inventory\/equipment\/[^/]+$/) && method === 'GET') {
      return Promise.resolve({ ok: true, json: async () => ({ item: currentItem, documents: currentDocuments }) })
    }
    if (u.match(/\/api\/inventory\/equipment\/eq-1$/) && method === 'PUT') {
      const body = JSON.parse(String(init?.body))
      // Same normalisation the real handler applies (backend/inventory_
      // handlers.go / inventory_store.go) before it ever echoes the item
      // back - trimmed strings, deduped aliases. A fixture that skipped
      // this would go green against a shape the real server never sends.
      currentItem = {
        ...currentItem,
        ...body,
        name: String(body.name).trim(),
        manufacturer: String(body.manufacturer).trim(),
        model: String(body.model).trim(),
        serial: String(body.serial).trim(),
        location_detail: String(body.location_detail).trim(),
        install_date: String(body.install_date).trim(),
        aliases: normalizeAliasesLikeServer(body.aliases ?? []),
      }
      return Promise.resolve({ ok: true, json: async () => ({ item: currentItem }) })
    }
    if (u.match(/\/api\/inventory\/equipment\/eq-1$/) && method === 'DELETE') {
      return Promise.resolve({ ok: true, status: 204, json: async () => ({}) })
    }
    if (u.match(/\/api\/inventory\/equipment$/) && method === 'POST') {
      const body = JSON.parse(String(init?.body))
      currentItem = { ...makeItem(body), id: 'eq-new' }
      return Promise.resolve({ ok: true, status: 201, json: async () => ({ item: currentItem }) })
    }
    // ADR 0127's photo routes - a plain in-memory stand-in for
    // AddEquipmentPhoto/SetEquipmentPhotoOrder/RemoveEquipmentPhoto
    // (backend/inventory_store.go), tracked on whichever record (eq-1 or a
    // freshly created eq-new) the URL names.
    const photoPost = u.match(/\/api\/inventory\/equipment\/([^/]+)\/photos$/)
    if (photoPost && method === 'POST') {
      const targetId = photoPost[1]
      const form = init?.body as FormData
      const file = form.get('file') as File
      uploadedPhotoOrder.push(file.name)
      if (conflictPhotoUploadNames.has(file.name)) {
        return Promise.resolve({ ok: false, status: 409, json: async () => ({ error: conflictPhotoUploadNames.get(file.name) }) })
      }
      if (failingPhotoUploadNames.has(file.name)) {
        return Promise.resolve({ ok: false, status: 500, json: async () => ({ error: `upload failed: ${file.name}` }) })
      }
      const photoId = `photo-${uploadedPhotoOrder.length}`
      if (targetId === currentItem.id) {
        currentItem = { ...currentItem, photo_ids: [...currentItem.photo_ids, photoId] }
      }
      return Promise.resolve({ ok: true, status: 201, json: async () => ({ item: currentItem }) })
    }
    const photoPut = u.match(/\/api\/inventory\/equipment\/([^/]+)\/photos$/)
    if (photoPut && method === 'PUT') {
      const body = JSON.parse(String(init?.body)) as { document_ids: string[] }
      currentItem = { ...currentItem, photo_ids: body.document_ids }
      return Promise.resolve({ ok: true, json: async () => ({ item: currentItem }) })
    }
    const photoDelete = u.match(/\/api\/inventory\/equipment\/[^/]+\/photos\/([^/]+)$/)
    if (photoDelete && method === 'DELETE') {
      currentItem = { ...currentItem, photo_ids: currentItem.photo_ids.filter((p) => p !== photoDelete[1]) }
      return Promise.resolve({ ok: true, json: async () => ({ item: currentItem }) })
    }
    return Promise.resolve({ ok: false, json: async () => ({ error: 'not found' }) })
  })
  vi.stubGlobal('fetch', fetchMock)
}

beforeEach(() => {
  fetchMock.mockReset()
  currentItem = makeItem()
  currentDocuments = []
  uploadedPhotoOrder = []
  failingPhotoUploadNames = new Set()
  conflictPhotoUploadNames = new Map()
  stubFetch()
  vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:mock-url')
  vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})
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

  it('re-seeds the draft from the saved item after a successful save, so server-side trimming does not leave it dirty forever', async () => {
    const onDirtyChange = vi.fn()
    render(<EquipmentEditor id="eq-1" onBack={vi.fn()} onCreated={vi.fn()} onDeleted={vi.fn()} onDirtyChange={onDirtyChange} />)
    await waitForLoaded()
    onDirtyChange.mockClear()

    // The server trims this to "Onan" and echoes that back - the draft has
    // to pick up the trimmed value, not keep comparing against what was typed.
    fireEvent.change(screen.getByLabelText('Manufacturer'), { target: { value: '  Onan  ' } })
    expect(onDirtyChange).toHaveBeenLastCalledWith(true)

    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(onDirtyChange).toHaveBeenLastCalledWith(false))
    expect(screen.getByLabelText('Manufacturer')).toHaveValue('Onan')
  })

  it('keeps a newer edit typed while a save is in flight instead of the server echo overwriting it, and dirty stays true', async () => {
    const onDirtyChange = vi.fn()
    render(<EquipmentEditor id="eq-1" onBack={vi.fn()} onCreated={vi.fn()} onDeleted={vi.fn()} onDirtyChange={onDirtyChange} />)
    await waitForLoaded()

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Onan Generator' } })
    onDirtyChange.mockClear()

    // The PUT this Save sends is held open, so the operator's next edit
    // lands while it's still in flight - the same race as a slow network.
    let resolvePut!: (value: { ok: boolean; json: () => Promise<unknown> }) => void
    fetchMock.mockImplementation((url: string, init?: RequestInit) => {
      const u = String(url)
      const method = init?.method ?? 'GET'
      if (u.match(/\/api\/inventory\/equipment\/eq-1$/) && method === 'PUT') {
        return new Promise((resolve) => { resolvePut = resolve })
      }
      throw new Error(`unexpected fetch in this test: ${method} ${u}`)
    })

    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    // A further, still-unsaved edit, typed before the PUT above resolves.
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Onan Generator 2' } })

    await act(async () => {
      resolvePut({ ok: true, json: async () => ({ item: { ...currentItem, name: 'Onan Generator' } }) })
      await Promise.resolve()
    })

    // The newer edit survives - it was never sent, so the server's echo of
    // the OLDER draft must not stomp it - and the editor keeps reporting it
    // dirty rather than silently losing the warning along with the edit.
    await waitFor(() => expect(screen.getByLabelText('Name')).toHaveValue('Onan Generator 2'))
    expect(onDirtyChange).not.toHaveBeenCalledWith(false)
  })

  it('addAlias dedupes case-insensitively, matching the server\'s normalizeAliases', async () => {
    currentItem = makeItem({ aliases: ['Genset'] })
    render(<EquipmentEditor id="eq-1" onBack={vi.fn()} onCreated={vi.fn()} onDeleted={vi.fn()} />)
    await waitForLoaded()
    expect(screen.getByText('Genset')).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('Add alias'), { target: { value: 'genset' } })
    fireEvent.keyDown(screen.getByLabelText('Add alias'), { key: 'Enter' })

    // Only the one chip - a case-only variant of an existing alias is not
    // a second alias, the same rule the server's own normalizeAliases applies.
    expect(screen.getAllByText(/^genset$/i)).toHaveLength(1)
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
    currentDocuments = [{ document_id: 'd1', title: 'Manual', filename: 'manual.pdf', kind: 'file', note_type: '', source: 'operator', sort_index: 0 }]
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
    currentDocuments = [{ document_id: 'd1', title: 'Manual', filename: 'manual.pdf', kind: 'file', note_type: '', source: 'operator', sort_index: 0 }]
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

  // ── photos (ADR 0127) ────────────────────────────────────────────────

  // Review finding: baselineDocIds (the dirty check's own "what the server
  // has" side) was built from the RAW `documents` list GET returns, which
  // includes photo-tagged links - while docEntries (the Documents tab's own
  // list, and the other side of the same comparison) excludes them, the
  // same filtering the effect just above this component's Documents tab
  // applies. Any item with a photo was therefore permanently dirty: the two
  // sides could never agree, even with nothing actually changed.
  it('is not dirty when opening a saved item that has one photo and nothing else changes', async () => {
    currentItem = makeItem({ photo_ids: ['photo-1'] })
    currentDocuments = [
      { document_id: 'photo-1', title: '', filename: 'a.jpg', kind: 'file', note_type: '', source: 'operator', sort_index: 0 },
    ]
    const onDirtyChange = vi.fn()
    render(<EquipmentEditor id="eq-1" onBack={vi.fn()} onCreated={vi.fn()} onDeleted={vi.fn()} onDirtyChange={onDirtyChange} />)
    await waitForLoaded()
    await screen.findByText('Remove')

    // Settles to NOT dirty - the ordinary transient true along the way
    // (item loaded, draft not yet re-seeded from it - every dirty-check
    // test in this file settles past that same moment) is not what this
    // pins; a permanently mismatched baseline never settles back to false
    // at all.
    await waitFor(() => expect(onDirtyChange).toHaveBeenLastCalledWith(false))
  })

  // Release-fixes code-review finding: removeSavedPhoto's setItem(updated)
  // drops the photo from item.photo_ids, but `documents` (fetched
  // separately, at mount) still carries its link - the Documents tab's own
  // exclusion filter keys off photo_ids, so the instant photo_ids no longer
  // names it, the still-stale `documents` array makes it look like an
  // ordinary linked document. A later Save that touches the link set at all
  // would then PUT it right back as one.
  it('does not let a removed photo reappear in the Documents tab or get sent on the next document save', async () => {
    currentItem = makeItem({ photo_ids: ['p1'] })
    currentDocuments = [
      { document_id: 'p1', title: '', filename: 'cover.jpg', kind: 'file', note_type: '', source: 'operator', sort_index: 0 },
      { document_id: 'd1', title: 'Manual', filename: 'manual.pdf', kind: 'file', note_type: '', source: 'operator', sort_index: 0 },
    ]
    render(<EquipmentEditor id="eq-1" onBack={vi.fn()} onCreated={vi.fn()} onDeleted={vi.fn()} />)
    await waitForLoaded()
    await screen.findByText('Manual')
    // The photo never shows in the Documents tab to begin with.
    expect(screen.queryByText('cover.jpg')).not.toBeInTheDocument()

    fireEvent.click(screen.getByText('Remove'))
    await waitFor(() => expect(fetchMock.mock.calls.some(([url, init]) =>
      String(url).endsWith('/api/inventory/equipment/eq-1/photos/p1') && (init as RequestInit | undefined)?.method === 'DELETE')).toBe(true))

    // Still not in the Documents tab after the removal.
    expect(screen.queryByText('cover.jpg')).not.toBeInTheDocument()

    // Touch the link set (remove the genuinely-linked Manual) and save - the
    // PUT must not resurrect the removed photo alongside it.
    fireEvent.click(screen.getByRole('button', { name: 'Remove document Manual' }))
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(([url, init]) =>
        String(url).endsWith('/api/inventory/equipment/eq-1/documents') && (init as RequestInit | undefined)?.method === 'PUT')
      expect(call).toBeDefined()
      const body = JSON.parse(String((call?.[1] as RequestInit).body)) as { document_ids: string[] }
      expect(body.document_ids).toEqual([])
    })
  })

  it("POSTs a saved item's Take photo pick to /photos", async () => {
    render(<EquipmentEditor id="eq-1" onBack={vi.fn()} onCreated={vi.fn()} onDeleted={vi.fn()} />)
    await waitForLoaded()

    const file = new File(['data'], 'impeller.jpg', { type: 'image/jpeg' })
    fireEvent.change(screen.getByLabelText('Take photo'), { target: { files: [file] } })

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(([url, init]) =>
        String(url).endsWith('/api/inventory/equipment/eq-1/photos') && (init as RequestInit | undefined)?.method === 'POST')
      expect(call).toBeDefined()
    })
  })

  it('PUTs the reordered ids when Make cover is clicked', async () => {
    currentItem = makeItem({ photo_ids: ['p1', 'p2'] })
    render(<EquipmentEditor id="eq-1" onBack={vi.fn()} onCreated={vi.fn()} onDeleted={vi.fn()} />)
    await waitForLoaded()

    // p1 is the cover already - its own "Make cover" is disabled - so the
    // second thumbnail's (p2) is the one that actually does anything.
    fireEvent.click(screen.getAllByText('Make cover')[1])

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(([url, init]) =>
        String(url).endsWith('/api/inventory/equipment/eq-1/photos') && (init as RequestInit | undefined)?.method === 'PUT')
      expect(call).toBeDefined()
      const body = JSON.parse(String((call?.[1] as RequestInit).body)) as { document_ids: string[] }
      expect(body.document_ids).toEqual(['p2', 'p1'])
    })
  })

  it('DELETEs a photo when Remove is clicked', async () => {
    currentItem = makeItem({ photo_ids: ['p1'] })
    render(<EquipmentEditor id="eq-1" onBack={vi.fn()} onCreated={vi.fn()} onDeleted={vi.fn()} />)
    await waitForLoaded()

    fireEvent.click(screen.getByText('Remove'))

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(([url, init]) =>
        String(url).endsWith('/api/inventory/equipment/eq-1/photos/p1') && (init as RequestInit | undefined)?.method === 'DELETE')
      expect(call).toBeDefined()
    })
  })

  // ADR 0127 review finding: every photo write already gets the updated
  // item back from the server (the same shape update() applies via
  // setItem) - Make cover/Remove should use THAT rather than firing a
  // second, redundant GET afterward.
  it('applies the server response directly on Make cover/Remove, without a follow-up GET', async () => {
    currentItem = makeItem({ photo_ids: ['p1', 'p2'] })
    render(<EquipmentEditor id="eq-1" onBack={vi.fn()} onCreated={vi.fn()} onDeleted={vi.fn()} />)
    await waitForLoaded()

    fireEvent.click(screen.getAllByText('Make cover')[1])
    await waitFor(() => {
      const call = fetchMock.mock.calls.find(([url, init]) =>
        String(url).endsWith('/api/inventory/equipment/eq-1/photos') && (init as RequestInit | undefined)?.method === 'PUT')
      expect(call).toBeDefined()
    })
    fetchMock.mockClear()

    fireEvent.click(screen.getAllByText('Remove')[0])
    await waitFor(() => {
      const call = fetchMock.mock.calls.find(([, init]) =>
        (init as RequestInit | undefined)?.method === 'DELETE')
      expect(call).toBeDefined()
    })

    // No bare GET /api/inventory/equipment/eq-1 after either write - the
    // returned {item} is applied directly instead of triggering a refetch.
    const getEq1 = fetchMock.mock.calls.filter(([url, init]) =>
      String(url).endsWith('/api/inventory/equipment/eq-1') && ((init as RequestInit | undefined)?.method ?? 'GET') === 'GET')
    expect(getEq1).toHaveLength(0)
  })

  // ADR 0127 review finding: uploadPhotosToSavedItem `break`s on the first
  // failure, so the remaining files never even get tried and are never
  // offered for Retry - unlike the new-draft path, which tries every file
  // and queues each failure. Also proves bug #5's fix for THIS call site:
  // the strip reflects the two successful uploads rather than staying
  // stuck on a stale refresh().
  it('tries every file when adding several photos to a saved item, queuing only the failure for Retry', async () => {
    currentItem = makeItem({ photo_ids: [] })
    failingPhotoUploadNames.add('b.jpg')
    render(<EquipmentEditor id="eq-1" onBack={vi.fn()} onCreated={vi.fn()} onDeleted={vi.fn()} />)
    await waitForLoaded()

    const fileA = new File(['a'], 'a.jpg', { type: 'image/jpeg' })
    const fileB = new File(['b'], 'b.jpg', { type: 'image/jpeg' })
    const fileC = new File(['c'], 'c.jpg', { type: 'image/jpeg' })
    fireEvent.change(screen.getByLabelText('Add from library'), { target: { files: [fileA, fileB, fileC] } })

    // All three tried, in order - b.jpg failing must not stop c.jpg.
    await waitFor(() => expect(uploadedPhotoOrder).toEqual(['a.jpg', 'b.jpg', 'c.jpg']))
    await screen.findByText("1 of 3 photos didn't upload: upload failed: b.jpg")
    // a.jpg and c.jpg made it onto the item - the strip isn't stuck empty.
    await waitFor(() => expect(screen.getAllByText('Remove')).toHaveLength(2))

    failingPhotoUploadNames.clear()
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))

    await waitFor(() => expect(uploadedPhotoOrder).toEqual(['a.jpg', 'b.jpg', 'c.jpg', 'b.jpg']))
    await waitFor(() => expect(screen.queryByText(/didn't upload/)).not.toBeInTheDocument())
    await waitFor(() => expect(screen.getAllByText('Remove')).toHaveLength(3))
  })

  // ADR 0127 review finding: the photo row was empty after creating an item
  // with photos - useEquipmentItem's own GET for the newly created id (the
  // id-change effect) can land BEFORE the photo uploads that follow it in
  // performSave finish, and nothing ever applied the uploads' own returned
  // item afterward. Deliberately holds that GET open (rather than trusting
  // the mock's natural timing, which doesn't reliably reproduce the race
  // either way) so this proves the strip comes from the uploads' own
  // responses, not from that GET landing to already show the finished
  // state.
  it('shows both uploaded photos even while the id-change GET is still in flight', async () => {
    const onCreated = vi.fn()
    let resolveGetNew!: (value: { ok: boolean; json: () => Promise<unknown> }) => void
    const withoutRace = fetchMock.getMockImplementation()!
    fetchMock.mockImplementation((url: string, init?: RequestInit) => {
      const u = String(url)
      const method = init?.method ?? 'GET'
      if (u.endsWith('/api/inventory/equipment/eq-new') && method === 'GET') {
        return new Promise((resolve) => { resolveGetNew = resolve })
      }
      return withoutRace(url, init)
    })

    render(<DraftHarness onCreatedSpy={onCreated} />)
    await waitFor(() => expect(screen.getByLabelText('Name')).toHaveValue(''))

    const fileA = new File(['a'], 'a.jpg', { type: 'image/jpeg' })
    const fileB = new File(['b'], 'b.jpg', { type: 'image/jpeg' })
    fireEvent.change(screen.getByLabelText('Add from library'), { target: { files: [fileA, fileB] } })
    await waitFor(() => expect(screen.getAllByText('Remove')).toHaveLength(2))

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Spare impeller' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(onCreated).toHaveBeenCalledWith('eq-new'))
    await waitFor(() => expect(uploadedPhotoOrder).toEqual(['a.jpg', 'b.jpg']))

    // The id-change GET is STILL pending here - the strip must already show
    // both photos from the uploads' own returned item, not from that GET.
    await waitFor(() => expect(screen.getAllByText('Remove')).toHaveLength(2))

    resolveGetNew({ ok: true, json: async () => ({ item: currentItem, documents: currentDocuments }) })
  })

  // The precise "GET starts before the uploads, resolves after them" race
  // (setItem's own seqRef bump, so that late reply is discarded) is
  // deterministic only at the hook level, where the id-change GET's start
  // and each upload's own setItem can be sequenced exactly - see
  // use-inventory.test.ts's own "discards a slow GET..." test. This
  // component-level suite proves the strip is right while that GET is
  // in flight (the test above); forcing the GET to start before a mocked
  // photo upload resolves, without an artificial delay that would just be
  // testing jsdom's own scheduling, isn't reliable here.

  // Mirrors how InventoryPanel/App.tsx actually wire onCreated - id starts
  // null and flips to the server-assigned id once Save's create succeeds,
  // WITHOUT unmounting EquipmentEditor (same component instance, only the
  // `id` prop changes) - the exact transition the plan's own Retry
  // scenario depends on (this component's local photo-failure state has
  // to survive it). A bare `render(<EquipmentEditor id={null} .../>)` with
  // a plain vi.fn() onCreated would never actually make that transition
  // happen, which is why this harness exists rather than every draft test
  // reaching for it directly.
  function DraftHarness({ onCreatedSpy }: { onCreatedSpy: (id: string) => void }) {
    const [id, setId] = useState<string | null>(null)
    return (
      <EquipmentEditor
        id={id}
        onBack={vi.fn()}
        onCreated={(newId) => { onCreatedSpy(newId); setId(newId) }}
        onDeleted={vi.fn()}
      />
    )
  }

  it('creates the item then uploads two draft photos in order', async () => {
    const onCreated = vi.fn()
    render(<DraftHarness onCreatedSpy={onCreated} />)
    await waitFor(() => expect(screen.getByLabelText('Name')).toHaveValue(''))

    const fileA = new File(['a'], 'a.jpg', { type: 'image/jpeg' })
    const fileB = new File(['b'], 'b.jpg', { type: 'image/jpeg' })
    fireEvent.change(screen.getByLabelText('Add from library'), { target: { files: [fileA, fileB] } })
    await waitFor(() => expect(screen.getAllByText('Remove')).toHaveLength(2))

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Spare impeller' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(onCreated).toHaveBeenCalledWith('eq-new'))
    await waitFor(() => expect(uploadedPhotoOrder).toEqual(['a.jpg', 'b.jpg']))
  })

  // Release-fixes code-review finding: a 409 refusal ("already in Documents
  // as ...") is the server saying these exact bytes can never be linked as
  // a NEW photo - re-sending the identical bytes on Retry can only get the
  // identical refusal, so it must not be queued for Retry the way a genuine
  // (transient) failure is.
  it('shows a 409 duplicate-photo refusal as a plain notice with no Retry, on a saved item', async () => {
    currentItem = makeItem({ photo_ids: [] })
    conflictPhotoUploadNames.set('a.jpg', 'This image is already in Documents as "Fuel receipt"')
    render(<EquipmentEditor id="eq-1" onBack={vi.fn()} onCreated={vi.fn()} onDeleted={vi.fn()} />)
    await waitForLoaded()

    const file = new File(['a'], 'a.jpg', { type: 'image/jpeg' })
    fireEvent.change(screen.getByLabelText('Add from library'), { target: { files: [file] } })

    await screen.findByText('This image is already in Documents as "Fuel receipt"')
    expect(screen.queryByRole('button', { name: 'Retry' })).not.toBeInTheDocument()
    // Dropped, not left on the strip as a pending/failed photo either.
    expect(screen.queryByText('Remove')).not.toBeInTheDocument()
  })

  it('shows a 409 duplicate-photo refusal as a plain notice with no Retry, on a brand new draft\'s create-then-upload', async () => {
    conflictPhotoUploadNames.set('a.jpg', 'This image is already in Documents as "Fuel receipt"')
    const onCreated = vi.fn()
    render(<DraftHarness onCreatedSpy={onCreated} />)
    await waitFor(() => expect(screen.getByLabelText('Name')).toHaveValue(''))

    const file = new File(['a'], 'a.jpg', { type: 'image/jpeg' })
    fireEvent.change(screen.getByLabelText('Add from library'), { target: { files: [file] } })
    await waitFor(() => expect(screen.getAllByText('Remove')).toHaveLength(1))

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Spare impeller' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(onCreated).toHaveBeenCalledWith('eq-new'))
    await screen.findByText('This image is already in Documents as "Fuel receipt"')
    expect(screen.queryByRole('button', { name: 'Retry' })).not.toBeInTheDocument()
  })

  it('shows Retry after a partial upload failure, and Retry re-sends only that one photo', async () => {
    failingPhotoUploadNames.add('b.jpg')
    const onCreated = vi.fn()
    render(<DraftHarness onCreatedSpy={onCreated} />)
    await waitFor(() => expect(screen.getByLabelText('Name')).toHaveValue(''))

    const fileA = new File(['a'], 'a.jpg', { type: 'image/jpeg' })
    const fileB = new File(['b'], 'b.jpg', { type: 'image/jpeg' })
    fireEvent.change(screen.getByLabelText('Add from library'), { target: { files: [fileA, fileB] } })
    await waitFor(() => expect(screen.getAllByText('Remove')).toHaveLength(2))

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Spare impeller' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(onCreated).toHaveBeenCalledWith('eq-new'))
    await screen.findByText("Saved, but 1 of 2 photos didn't upload: upload failed: b.jpg")
    expect(uploadedPhotoOrder).toEqual(['a.jpg', 'b.jpg'])

    failingPhotoUploadNames.clear() // the retry itself succeeds
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))

    await waitFor(() => expect(uploadedPhotoOrder).toEqual(['a.jpg', 'b.jpg', 'b.jpg']))
    await waitFor(() => expect(screen.queryByText(/didn't upload/)).not.toBeInTheDocument())
  })
})
