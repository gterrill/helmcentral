import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, render, screen, waitFor } from '@testing-library/react'
import { DocumentViewerSheet } from '@/components/documents/document-viewer-sheet'
import type { DocumentRecord } from '@/hooks/use-documents'

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
})
