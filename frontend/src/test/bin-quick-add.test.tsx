import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { BinQuickAdd } from '@/components/inventory/bin-quick-add'

// ADR 0127 (the plan's A5b): mirrors equipment-editor.test.tsx's own
// downscale-and-fetch mocking approach exactly (that file's own comment
// explains why canvas/createImageBitmap are stubbed away rather than run
// for real against jsdom).
vi.mock('@/lib/image-downscale', () => ({
  downscaleImage: vi.fn(async (file: Blob) => file),
}))

let uploadedPhotoOrder: string[]
let failingPhotoUploadNames: Set<string>
const fetchMock = vi.fn()

beforeEach(() => {
  uploadedPhotoOrder = []
  failingPhotoUploadNames = new Set()
  fetchMock.mockReset()
  fetchMock.mockImplementation((url: string, init?: RequestInit) => {
    const u = String(url)
    const method = init?.method ?? 'GET'

    if (u.endsWith('/api/inventory/equipment') && method === 'POST') {
      const body = JSON.parse(String(init?.body))
      return Promise.resolve({
        ok: true,
        status: 201,
        json: async () => ({ item: { id: 'eq-new', photo_ids: [], ...body } }),
      })
    }
    const photoPost = u.match(/\/api\/inventory\/equipment\/([^/]+)\/photos$/)
    if (photoPost && method === 'POST') {
      const form = init?.body as FormData
      const file = form.get('file') as File
      uploadedPhotoOrder.push(file.name)
      if (failingPhotoUploadNames.has(file.name)) {
        return Promise.resolve({ ok: false, status: 500, json: async () => ({ error: `upload failed: ${file.name}` }) })
      }
      return Promise.resolve({ ok: true, status: 201, json: async () => ({ item: { id: photoPost[1], photo_ids: ['p1'] } }) })
    }
    return Promise.resolve({ ok: false, json: async () => ({ error: 'not found' }) })
  })
  vi.stubGlobal('fetch', fetchMock)
  vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:mock-url')
  vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})
})

describe('BinQuickAdd', () => {
  it('creates the item, then uploads the picked photo', async () => {
    const onCreated = vi.fn()
    render(<BinQuickAdd zoneId="z1" binId="b1" onCreated={onCreated} />)

    const file = new File(['a'], 'a.jpg', { type: 'image/jpeg' })
    fireEvent.change(screen.getByLabelText('Take photo'), { target: { files: [file] } })
    await waitFor(() => expect(screen.getByText('Remove')).toBeInTheDocument())

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Gaffer tape' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(([url, init]) =>
        String(url).endsWith('/api/inventory/equipment') && (init as RequestInit | undefined)?.method === 'POST')
      expect(call).toBeDefined()
      const body = JSON.parse(String((call?.[1] as RequestInit).body))
      expect(body).toMatchObject({ name: 'Gaffer tape', category: 'general', status: 'stored', zone_id: 'z1', bin_id: 'b1' })
    })
    await waitFor(() => expect(uploadedPhotoOrder).toEqual(['a.jpg']))
    await waitFor(() => expect(onCreated).toHaveBeenCalled())
    // The form clears, ready for the next item.
    expect(screen.getByLabelText('Name')).toHaveValue('')
  })

  // Release-fixes code-review finding: a 409 refusal ("already in Documents
  // as ...") means the exact same bytes can never be linked as a NEW photo -
  // re-sending them on Retry can only get the identical refusal, so it must
  // not go on the retry queue the way a genuine (transient) failure does.
  it('shows a 409 duplicate-photo refusal as a plain notice with no Retry', async () => {
    fetchMock.mockImplementation((url: string, init?: RequestInit) => {
      const u = String(url)
      const method = init?.method ?? 'GET'
      if (u.endsWith('/api/inventory/equipment') && method === 'POST') {
        const body = JSON.parse(String(init?.body))
        return Promise.resolve({ ok: true, status: 201, json: async () => ({ item: { id: 'eq-new', photo_ids: [], ...body } }) })
      }
      const photoPost = u.match(/\/api\/inventory\/equipment\/([^/]+)\/photos$/)
      if (photoPost && method === 'POST') {
        const form = init?.body as FormData
        const file = form.get('file') as File
        uploadedPhotoOrder.push(file.name)
        return Promise.resolve({ ok: false, status: 409, json: async () => ({ error: 'This image is already in Documents as "Fuel receipt"' }) })
      }
      return Promise.resolve({ ok: false, json: async () => ({ error: 'not found' }) })
    })

    render(<BinQuickAdd zoneId="z1" binId="b1" onCreated={vi.fn()} />)

    const file = new File(['a'], 'a.jpg', { type: 'image/jpeg' })
    fireEvent.change(screen.getByLabelText('Take photo'), { target: { files: [file] } })
    await waitFor(() => expect(screen.getByText('Remove')).toBeInTheDocument())

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Gaffer tape' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    await screen.findByText('This image is already in Documents as "Fuel receipt"')
    expect(screen.queryByRole('button', { name: 'Retry' })).not.toBeInTheDocument()
  })

  it('leaves the item saved and offers Retry when a photo upload fails', async () => {
    failingPhotoUploadNames.add('a.jpg')
    render(<BinQuickAdd zoneId="z1" binId="b1" onCreated={vi.fn()} />)

    const file = new File(['a'], 'a.jpg', { type: 'image/jpeg' })
    fireEvent.change(screen.getByLabelText('Take photo'), { target: { files: [file] } })
    await waitFor(() => expect(screen.getByText('Remove')).toBeInTheDocument())

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Gaffer tape' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    await screen.findByText("Saved Gaffer tape, but 1 photo didn't upload: upload failed: a.jpg")
    expect(screen.getByRole('button', { name: 'Retry' })).toBeInTheDocument()

    failingPhotoUploadNames.clear()
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))

    await waitFor(() => expect(uploadedPhotoOrder).toEqual(['a.jpg', 'a.jpg']))
    await waitFor(() => expect(screen.queryByText(/didn't upload/)).not.toBeInTheDocument())
  })

  // Release-fixes code-review finding: reports whether the form holds
  // anything a navigation away would clear - App.tsx routes the bin page's
  // "Full item" through the same unsaved-work guard when this is true.
  it('reports hasWork as a name or photos are staged, and clears it after Save', async () => {
    const onHasWorkChange = vi.fn()
    render(<BinQuickAdd zoneId="z1" binId="b1" onCreated={vi.fn()} onHasWorkChange={onHasWorkChange} />)
    expect(onHasWorkChange).toHaveBeenLastCalledWith(false)

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Gaffer tape' } })
    expect(onHasWorkChange).toHaveBeenLastCalledWith(true)

    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(screen.getByLabelText('Name')).toHaveValue(''))
    expect(onHasWorkChange).toHaveBeenLastCalledWith(false)
  })

  it('reports hasWork from a staged photo alone, with no name typed', async () => {
    const onHasWorkChange = vi.fn()
    render(<BinQuickAdd zoneId="z1" binId="b1" onCreated={vi.fn()} onHasWorkChange={onHasWorkChange} />)
    onHasWorkChange.mockClear()

    const file = new File(['a'], 'a.jpg', { type: 'image/jpeg' })
    fireEvent.change(screen.getByLabelText('Take photo'), { target: { files: [file] } })

    await waitFor(() => expect(onHasWorkChange).toHaveBeenLastCalledWith(true))
  })

  it('blocks Save when the name is blank', async () => {
    render(<BinQuickAdd zoneId="z1" binId="b1" onCreated={vi.fn()} />)

    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    expect(fetchMock).not.toHaveBeenCalled()
  })

  // Review finding: Enter in the Name field calls handleSave directly, with
  // no in-flight guard - two Enters pressed before the first create's POST
  // has a chance to resolve (and re-render `saving` into the DOM) both ran
  // the whole create flow, same as pressing Enter then clicking Save before
  // the button had disabled itself.
  it('Enter pressed twice in quick succession creates exactly one item', async () => {
    const onCreated = vi.fn()
    render(<BinQuickAdd zoneId="z1" binId="b1" onCreated={onCreated} />)

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Gaffer tape' } })
    const nameField = screen.getByLabelText('Name')
    // Deliberately no await between these two - reproduces two Enters
    // landing before React has re-rendered `saving` into the closures
    // either keydown handler reads.
    fireEvent.keyDown(nameField, { key: 'Enter' })
    fireEvent.keyDown(nameField, { key: 'Enter' })

    await waitFor(() => expect(onCreated).toHaveBeenCalled())

    const createCalls = fetchMock.mock.calls.filter(([url, init]) =>
      String(url).endsWith('/api/inventory/equipment') && (init as RequestInit | undefined)?.method === 'POST')
    expect(createCalls).toHaveLength(1)
  })
})
