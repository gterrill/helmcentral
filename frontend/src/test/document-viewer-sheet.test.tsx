import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { DocumentViewerSheet } from '@/components/documents/document-viewer-sheet'
import type { DocumentRecord } from '@/hooks/use-documents'
import { testRouter } from '@/test/helpers/router'

// The full-screen alarm banner reads the live alarm list; seed it here.
const mockAlarms = vi.hoisted(() => ({ list: [] as unknown[] }))
vi.mock('@/hooks/use-alarms', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/hooks/use-alarms')>()),
  useAlarmListComparedOn: () => mockAlarms.list,
  acknowledgeAlarm: vi.fn(),
}))
vi.mock('@/hooks/use-forecast-warnings', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/hooks/use-forecast-warnings')>()),
  useForecastWarnings: () => ({ activeWarning: null }),
}))

// The viewer fetches its own metadata for a document the caller does not
// already hold. Two review findings: a failed fetch used to leave the sheet
// on "Loading..." forever, and a slow first response could land after a
// second open and show the first document under the second's name.

function doc(id: string, name: string, mime = 'text/plain'): DocumentRecord {
  return { id, filename: name, title: name, mime, kind: 'file', size_bytes: 10, created_at: '2026-01-01T00:00:00Z', pinned: false } as unknown as DocumentRecord
}

const fetchMock = vi.fn()
beforeEach(() => { fetchMock.mockReset(); vi.stubGlobal('fetch', fetchMock) })
afterEach(() => { vi.unstubAllGlobals() })

const noNotes = { getNote: vi.fn(), patchNote: vi.fn() }

function deferred<T>() {
  let resolve!: (v: T) => void
  const promise = new Promise<T>((r) => { resolve = r })
  return { promise, resolve }
}

describe('DocumentViewerSheet', () => {
  it('shows the server message, not "Loading...", when the document record cannot be read', async () => {
    fetchMock.mockResolvedValue({ ok: false, status: 404, json: async () => ({ error: 'document not found' }) })
    render(<DocumentViewerSheet documentId="x" onDocumentChange={vi.fn()} {...noNotes} />)
    expect(await screen.findByRole('alert')).toHaveTextContent('document not found')
    expect(screen.queryByText('Loading…')).not.toBeInTheDocument()
  })

  it('shows a status-based message when the failure carries no body', async () => {
    fetchMock.mockResolvedValue({ ok: false, status: 500, json: async () => { throw new Error('no body') } })
    render(<DocumentViewerSheet documentId="x" onDocumentChange={vi.fn()} {...noNotes} />)
    expect(await screen.findByRole('alert')).toHaveTextContent('HTTP 500')
  })

  it('opening A then quickly B shows B only, even if A answers last', async () => {
    const a = deferred<unknown>()
    const b = deferred<unknown>()
    fetchMock.mockImplementation((url: string) => {
      const u = String(url)
      if (u.endsWith('/api/documents/A')) return a.promise
      if (u.endsWith('/api/documents/B')) return b.promise
      if (u.endsWith('/text')) return Promise.resolve({ ok: true, json: async () => ({ text: u.includes('/A/') ? 'text of A' : 'text of B' }) })
      return Promise.resolve({ ok: false, status: 404, json: async () => ({}) })
    })
    const { rerender } = render(<DocumentViewerSheet documentId="A" onDocumentChange={vi.fn()} {...noNotes} />)
    rerender(<DocumentViewerSheet documentId="B" onDocumentChange={vi.fn()} {...noNotes} />)

    await act(async () => { b.resolve({ ok: true, json: async () => doc('B', 'Bee.txt') }) })
    await screen.findByText('text of B')
    await act(async () => { a.resolve({ ok: true, json: async () => doc('A', 'Ay.txt') }) })

    expect(screen.getByText('Bee.txt')).toBeInTheDocument()
    expect(screen.queryByText('Ay.txt')).not.toBeInTheDocument()
    expect(screen.queryByText('text of A')).not.toBeInTheDocument()
  })

  it('does not show the previous document while the next one loads', async () => {
    const b = deferred<unknown>()
    fetchMock.mockImplementation((url: string) => {
      const u = String(url)
      if (u.endsWith('/api/documents/B')) return b.promise
      return Promise.resolve({ ok: true, json: async () => ({ text: 'x' }) })
    })
    const known = [doc('A', 'Ay.txt')]
    const { rerender } = render(<DocumentViewerSheet documentId="A" knownDocuments={known} onDocumentChange={vi.fn()} {...noNotes} />)
    await screen.findByText('Ay.txt')
    rerender(<DocumentViewerSheet documentId="B" knownDocuments={known} onDocumentChange={vi.fn()} {...noNotes} />)
    await waitFor(() => expect(screen.queryByText('Ay.txt')).not.toBeInTheDocument())
  })

  it('uses a known document without fetching its record', async () => {
    fetchMock.mockResolvedValue({ ok: true, json: async () => ({ text: 'hello' }) })
    render(<DocumentViewerSheet documentId="A" knownDocuments={[doc('A', 'Ay.txt')]} onDocumentChange={vi.fn()} {...noNotes} />)
    await screen.findByText('Ay.txt')
    expect(fetchMock.mock.calls.some(([u]) => String(u).endsWith('/api/documents/A'))).toBe(false)
  })

  it('shows the server message when the text of a document cannot be read', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (String(url).endsWith('/text')) return Promise.resolve({ ok: false, status: 500, json: async () => ({ error: 'text extraction failed' }) })
      return Promise.resolve({ ok: true, json: async () => doc('A', 'Ay.txt') })
    })
    render(<DocumentViewerSheet documentId="A" onDocumentChange={vi.fn()} {...noNotes} />)
    expect(await screen.findByRole('alert')).toHaveTextContent('text extraction failed')
  })
  describe('full screen', () => {
    const known = [doc('I', 'Network.png', 'image/png'), doc('P', 'Manual.pdf', 'application/pdf'), doc('T', 'Notes.txt')]
    const dialog = () => screen.getByRole('dialog')
    const isFull = () => screen.queryByRole('button', { name: 'Exit full screen' }) !== null
    // Full screen shows the alarm banner, which navigates through the router.
    const { router, wrapper } = testRouter('/')
    const open = (id: string) => render(<DocumentViewerSheet documentId={id} knownDocuments={known} onDocumentChange={vi.fn()} {...noNotes} />, { wrapper })
    const alarm = { rule_id: 'r', label: 'Bilge high', path: 'notifications.bilge', phase: 'active', state: 'alarm', value: 1, message: 'Bilge high', silenced: false, can_silence: false, can_acknowledge: true }

    beforeEach(() => {
      mockAlarms.list = []
      fetchMock.mockResolvedValue({ ok: true, json: async () => ({ text: 'hello' }) })
    })

    it('the toolbar button toggles full screen and says where a click leads', async () => {
      open('P')
      fireEvent.click(await screen.findByRole('button', { name: 'Full screen' }))
      expect(isFull()).toBe(true)
      expect(dialog().className).toContain('sm:max-w-none')
      expect(dialog().className).not.toContain('sm:max-w-2xl')
      fireEvent.click(screen.getByRole('button', { name: 'Exit full screen' }))
      expect(isFull()).toBe(false)
      expect(dialog().className).toContain('sm:max-w-2xl')
      expect(screen.getByRole('button', { name: 'Full screen' })).toBeInTheDocument()
    })

    it('clicking an image enters full screen and keeps the same image node', async () => {
      open('I')
      const img = await screen.findByRole('img', { name: 'Network.png' })
      fireEvent.click(img)
      expect(isFull()).toBe(true)
      expect(screen.getByRole('img', { name: 'Network.png' })).toBe(img)
      expect(img.className).toContain('object-contain')
      fireEvent.click(img)
      expect(isFull()).toBe(true)
      fireEvent.click(screen.getByRole('button', { name: 'Exit full screen' }))
      expect(screen.getByRole('img', { name: 'Network.png' })).toBe(img)
    })

    it('the image is not a button', async () => {
      open('I')
      await screen.findByRole('img', { name: 'Network.png' })
      expect(screen.queryByRole('button', { name: /Network\.png/ })).not.toBeInTheDocument()
    })

    it('shows the toolbar button on a phone only for a picture, or once full screen', async () => {
      const { unmount } = open('P')
      const pdfButton = await screen.findByRole('button', { name: 'Full screen' })
      expect(pdfButton.className.split(' ')).toContain('hidden')
      expect(pdfButton.className.split(' ')).not.toContain('inline-flex')
      expect(pdfButton.className).toContain('sm:inline-flex')
      fireEvent.click(pdfButton)
      expect(screen.getByRole('button', { name: 'Exit full screen' }).className.split(' ')).not.toContain('hidden')
      unmount()
      open('I')
      expect((await screen.findByRole('button', { name: 'Full screen' })).className.split(' ')).not.toContain('hidden')
    })

    it('returns to the drawer when the viewer closes and reopens', async () => {
      const props = { knownDocuments: known, onDocumentChange: vi.fn(), ...noNotes }
      const { rerender } = render(<DocumentViewerSheet documentId="I" {...props} />, { wrapper })
      fireEvent.click(await screen.findByRole('button', { name: 'Full screen' }))
      rerender(<DocumentViewerSheet documentId={null} {...props} />)
      rerender(<DocumentViewerSheet documentId="I" {...props} />)
      await screen.findByRole('button', { name: 'Full screen' })
      expect(isFull()).toBe(false)
    })

    it('stays full screen when another document is opened', async () => {
      const props = { knownDocuments: known, onDocumentChange: vi.fn(), ...noNotes }
      const { rerender } = render(<DocumentViewerSheet documentId="I" {...props} />, { wrapper })
      fireEvent.click(await screen.findByRole('button', { name: 'Full screen' }))
      rerender(<DocumentViewerSheet documentId="P" {...props} />)
      await screen.findByTitle('Manual.pdf')
      expect(isFull()).toBe(true)
    })

    it('keeps the same PDF iframe node when toggling', async () => {
      open('P')
      const frame = await screen.findByTitle('Manual.pdf')
      fireEvent.click(screen.getByRole('button', { name: 'Full screen' }))
      expect(screen.getByTitle('Manual.pdf')).toBe(frame)
      fireEvent.click(screen.getByRole('button', { name: 'Exit full screen' }))
      expect(screen.getByTitle('Manual.pdf')).toBe(frame)
    })

    it('shows a live alarm across the top in full screen only, and View closes the viewer and opens Alarms', async () => {
      mockAlarms.list = [alarm]
      const onDocumentChange = vi.fn()
      render(<DocumentViewerSheet documentId="P" knownDocuments={known} onDocumentChange={onDocumentChange} {...noNotes} />, { wrapper })
      await screen.findByTitle('Manual.pdf')
      expect(screen.queryByRole('alert')).not.toBeInTheDocument()
      fireEvent.click(screen.getByRole('button', { name: 'Full screen' }))
      expect(within(dialog()).getByRole('alert')).toHaveTextContent('Bilge high')
      fireEvent.click(within(dialog()).getByRole('button', { name: /view/i }))
      expect(onDocumentChange).toHaveBeenCalledWith(null)
      expect(router.getSnapshot().location.panel).toBe('alarms')
    })
  })
})
