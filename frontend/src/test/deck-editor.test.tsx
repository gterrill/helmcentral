import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createRef } from 'react'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { DeckEditor, type DeckEditorHandle } from '@/components/inventory/deck-editor'
import type { InventoryDeck, InventoryZone } from '@/hooks/use-inventory'
import type { LayoutDraft } from '@/lib/deck-layout'
import { stubImageSize } from './stub-image'

// The drawing surface has its own tests (deck-plan-editor.test.tsx); here a
// stand-in lets these tests change the draft without driving pointer events.
vi.mock('@/components/inventory/deck-plan-editor', () => ({
  DeckPlanEditor: (props: { layout: LayoutDraft; onChange: (l: LayoutDraft) => void }) => (
    <div data-testid="plan-editor">
      <button
        type="button"
        onClick={() => props.onChange({
          polygons: { ...props.layout.polygons, z3: [[0.1, 0.1], [0.5, 0.1], [0.3, 0.5]] },
          pins: { ...props.layout.pins },
        })}
      >
        Stub: outline Lazarette
      </button>
      <span data-testid="outlined">{Object.keys(props.layout.polygons).sort().join(',')}</span>
    </div>
  ),
  prefetchDeckPlanEditor: () => {},
}))

let decks: InventoryDeck[]
let zones: InventoryZone[]
const fetchMock = vi.fn()
let headerHost: HTMLElement

const tri: Array<[number, number]> = [[0.1, 0.1], [0.5, 0.1], [0.3, 0.5]]

const calls = (method: string, suffix: string) =>
  fetchMock.mock.calls.filter(([url, init]) => String(url).endsWith(suffix) && ((init as RequestInit | undefined)?.method ?? 'GET') === method)

function renderEditor(props: Partial<React.ComponentProps<typeof DeckEditor>> = {}) {
  return render(<DeckEditor id="d1" onBack={vi.fn()} onDeleted={vi.fn()} {...props} />)
}

beforeEach(() => {
  headerHost = document.createElement('header')
  const slot = document.createElement('div')
  slot.id = 'save-bar-slot'
  headerHost.appendChild(slot)
  document.body.appendChild(headerHost)

  stubImageSize(2000, 1000)
  decks = [
    { id: 'd1', name: 'Main deck', sort_index: 0, plan_document_id: 'doc1' },
    { id: 'd2', name: 'Flybridge', sort_index: 1, plan_document_id: null },
  ]
  zones = [
    { id: 'z1', name: 'Salon', sort_index: 0, bins: [], deck_id: 'd1', polygon: tri },
    { id: 'z2', name: 'Galley', sort_index: 1, bins: [], deck_id: 'd1', polygon: tri },
    { id: 'z3', name: 'Lazarette', sort_index: 2, bins: [], deck_id: null, polygon: null },
  ]
  fetchMock.mockReset()
  fetchMock.mockImplementation((url: string, init?: RequestInit) => {
    const u = String(url)
    const method = init?.method ?? 'GET'
    if (u.endsWith('/api/inventory/zones') && method === 'GET') return Promise.resolve({ ok: true, json: async () => ({ zones }) })
    if (u.endsWith('/api/inventory/decks') && method === 'GET') return Promise.resolve({ ok: true, json: async () => ({ decks }) })
    if (u.endsWith('/api/inventory/decks/d1') && method === 'PUT') {
      const body = JSON.parse(String(init?.body)) as { name: string }
      decks = decks.map((d) => (d.id === 'd1' ? { ...d, name: body.name } : d))
      return Promise.resolve({ ok: true, json: async () => ({ deck: decks[0] }) })
    }
    if (u.endsWith('/api/inventory/decks/d1/layout') && method === 'PUT') {
      const body = JSON.parse(String(init?.body)) as { zones: Array<{ id: string; polygon: Array<[number, number]> }> }
      zones = zones.map((z) => {
        const listed = body.zones.find((l) => l.id === z.id)
        return listed ? { ...z, deck_id: 'd1', polygon: listed.polygon } : z.deck_id === 'd1' ? { ...z, deck_id: null, polygon: null } : z
      })
      return Promise.resolve({ ok: true, json: async () => ({ zones }) })
    }
    if (u.endsWith('/api/inventory/decks/d1/plan') && method === 'POST') {
      decks = decks.map((d) => (d.id === 'd1' ? { ...d, plan_document_id: 'doc2' } : d))
      return Promise.resolve({ ok: true, json: async () => ({ deck: decks[0] }) })
    }
    if (u.endsWith('/api/inventory/decks/d2/plan') && method === 'POST') {
      return Promise.resolve({ ok: false, status: 400, json: async () => ({ error: 'a deck plan must be a JPEG or PNG image' }) })
    }
    if (u.endsWith('/api/inventory/decks/d1') && method === 'DELETE') {
      return Promise.resolve({ ok: true, json: async () => ({ zones_cleared: 2 }) })
    }
    return Promise.resolve({ ok: false, json: async () => ({ error: 'not found' }) })
  })
  vi.stubGlobal('fetch', fetchMock)
  vi.stubGlobal('createImageBitmap', vi.fn().mockResolvedValue({ width: 2000, height: 1000, close: () => {} }))
})

afterEach(() => {
  headerHost.remove()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('DeckEditor', () => {
  it('shows the deck name and the drawing surface', async () => {
    renderEditor()
    expect(await screen.findByDisplayValue('Main deck')).toBeInTheDocument()
    expect(await screen.findByTestId('plan-editor')).toBeInTheDocument()
    expect(screen.getByTestId('outlined')).toHaveTextContent('z1,z2')
  })

  it('says so when the deck does not exist', async () => {
    renderEditor({ id: 'nope' })
    await screen.findByText('This deck could not be found.')
  })

  it('asks for a plan image before offering the drawing surface', async () => {
    renderEditor({ id: 'd2' })
    await screen.findByDisplayValue('Flybridge')
    expect(screen.getByText(/Upload a plan image to start drawing/i)).toBeInTheDocument()
    expect(screen.queryByTestId('plan-editor')).not.toBeInTheDocument()
  })

  it('renames through the Save bar', async () => {
    const onDirtyChange = vi.fn()
    renderEditor({ onDirtyChange })
    fireEvent.change(await screen.findByLabelText('Deck name'), { target: { value: 'Upper deck' } })
    await waitFor(() => expect(onDirtyChange).toHaveBeenLastCalledWith(true))
    fireEvent.click(await screen.findByRole('button', { name: 'Save' }))
    await waitFor(() => expect(calls('PUT', '/api/inventory/decks/d1')).toHaveLength(1))
    expect(JSON.parse(String(calls('PUT', '/api/inventory/decks/d1')[0][1].body))).toEqual({ name: 'Upper deck' })
    expect(calls('PUT', '/api/inventory/decks/d1/layout')).toHaveLength(0)
  })

  it('saves a layout change as one request, then is clean again', async () => {
    const onDirtyChange = vi.fn()
    renderEditor({ onDirtyChange })
    fireEvent.click(await screen.findByRole('button', { name: 'Stub: outline Lazarette' }))
    await waitFor(() => expect(onDirtyChange).toHaveBeenLastCalledWith(true))
    fireEvent.click(await screen.findByRole('button', { name: 'Save' }))
    await waitFor(() => expect(calls('PUT', '/api/inventory/decks/d1/layout')).toHaveLength(1))
    const body = JSON.parse(String(calls('PUT', '/api/inventory/decks/d1/layout')[0][1].body))
    expect(body.zones.map((z: { id: string }) => z.id).sort()).toEqual(['z1', 'z2', 'z3'])
    expect(body.zones.find((z: { id: string }) => z.id === 'z3').polygon).toEqual(tri)
    expect(body.bins).toEqual([])
    await waitFor(() => expect(onDirtyChange).toHaveBeenLastCalledWith(false))
    expect(calls('PUT', '/api/inventory/decks/d1')).toHaveLength(0)
  })

  it('discard returns the drawing to what is saved', async () => {
    renderEditor()
    fireEvent.click(await screen.findByRole('button', { name: 'Stub: outline Lazarette' }))
    expect(screen.getByTestId('outlined')).toHaveTextContent('z1,z2,z3')
    fireEvent.click(await screen.findByRole('button', { name: 'Discard' }))
    expect(screen.getByTestId('outlined')).toHaveTextContent('z1,z2')
  })

  it('shows the server message when a layout save is refused and stays dirty', async () => {
    const onDirtyChange = vi.fn()
    renderEditor({ onDirtyChange })
    fireEvent.click(await screen.findByRole('button', { name: 'Stub: outline Lazarette' }))
    const previous = fetchMock.getMockImplementation()!
    fetchMock.mockImplementation((url: string, init?: RequestInit) => {
      if (String(url).endsWith('/layout') && init?.method === 'PUT') {
        return Promise.resolve({ ok: false, status: 400, json: async () => ({ error: 'a zone outline needs 3 to 64 points, each between 0 and 1' }) })
      }
      return previous(url, init)
    })
    fireEvent.click(await screen.findByRole('button', { name: 'Save' }))
    await screen.findByText('a zone outline needs 3 to 64 points, each between 0 and 1')
    expect(onDirtyChange).toHaveBeenLastCalledWith(true)
  })

  it('exposes save() for the unsaved-changes guard', async () => {
    const ref = createRef<DeckEditorHandle>()
    render(<DeckEditor ref={ref} id="d1" onBack={vi.fn()} onDeleted={vi.fn()} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Stub: outline Lazarette' }))
    await ref.current!.save()
    expect(calls('PUT', '/api/inventory/decks/d1/layout')).toHaveLength(1)
  })

  it('shows the plain viewer, not the drawing surface, when read-only', async () => {
    renderEditor({ canWrite: false })
    await screen.findByDisplayValue('Main deck')
    await screen.findByTestId('deck-plan-svg')
    expect(screen.queryByTestId('plan-editor')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Replace plan/ })).not.toBeInTheDocument()
  })
})

describe('DeckEditor plan image', () => {
  const pick = (file: File) => {
    const input = document.querySelector('input[type="file"]') as HTMLInputElement
    fireEvent.change(input, { target: { files: [file] } })
  }

  beforeEach(() => {
    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue({ drawImage: () => {} } as unknown as RenderingContext)
    vi.spyOn(HTMLCanvasElement.prototype, 'toBlob').mockImplementation(function (cb: BlobCallback) {
      cb(new Blob(['jpeg'], { type: 'image/jpeg' }))
    })
  })

  it('uploads a replacement straight away, downscaled to a JPEG', async () => {
    renderEditor()
    await screen.findByDisplayValue('Main deck')
    pick(new File(['x'], 'plan.png', { type: 'image/png' }))
    await waitFor(() => expect(calls('POST', '/api/inventory/decks/d1/plan')).toHaveLength(1))
    const form = calls('POST', '/api/inventory/decks/d1/plan')[0][1].body as FormData
    expect((form.get('file') as Blob).type).toBe('image/jpeg')
  })

  it('warns when the new image has a different shape from the old plan', async () => {
    renderEditor()
    await screen.findByDisplayValue('Main deck')
    // Old plan is 2000 x 1000 (2:1); the new one is 1500 x 1000 (3:2).
    vi.stubGlobal('createImageBitmap', vi.fn().mockResolvedValue({ width: 1500, height: 1000, close: () => {} }))
    pick(new File(['x'], 'plan.png', { type: 'image/png' }))
    await screen.findByText(/different shape/i, {}, { timeout: 4000 })
  })

  it('does not warn for a shape within 2 percent', async () => {
    renderEditor()
    await screen.findByDisplayValue('Main deck')
    vi.stubGlobal('createImageBitmap', vi.fn().mockResolvedValue({ width: 2020, height: 1000, close: () => {} }))
    pick(new File(['x'], 'plan.png', { type: 'image/png' }))
    await waitFor(() => expect(calls('POST', '/api/inventory/decks/d1/plan')).toHaveLength(1))
    expect(screen.queryByText(/different shape/i)).not.toBeInTheDocument()
  })

  it('shows the server message when the upload is refused', async () => {
    renderEditor({ id: 'd2' })
    await screen.findByDisplayValue('Flybridge')
    pick(new File(['x'], 'plan.pdf', { type: 'application/pdf' }))
    await screen.findByText('a deck plan must be a JPEG or PNG image')
  })
})

describe('DeckEditor when locations will not load', () => {
  it('shows the load error and offers neither Delete nor the drawing surface', async () => {
    const previous = fetchMock.getMockImplementation()!
    fetchMock.mockImplementation((url: string, init?: RequestInit) => {
      if (String(url).endsWith('/api/inventory/zones')) {
        return Promise.resolve({ ok: false, status: 500, json: async () => ({ error: 'zones unavailable' }) })
      }
      return previous(url, init)
    })
    renderEditor()
    await screen.findByDisplayValue('Main deck')
    expect(await screen.findByText('zones unavailable')).toBeInTheDocument()
    expect(screen.queryByTestId('plan-editor')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /More|actions/i })).not.toBeInTheDocument()
  })
})

describe('DeckEditor delete', () => {
  it('names how many locations lose their outline, then deletes', async () => {
    const onDeleted = vi.fn()
    renderEditor({ onDeleted })
    await screen.findByDisplayValue('Main deck')
    fireEvent.click(screen.getByRole('button', { name: /More|actions/i }))
    fireEvent.click(await screen.findByText('Delete deck'))
    await screen.findByText(/2 locations lose their outline/i)
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(onDeleted).toHaveBeenCalled())
    expect(calls('DELETE', '/api/inventory/decks/d1')).toHaveLength(1)
  })
})
