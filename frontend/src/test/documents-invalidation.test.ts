import { describe, it, expect, vi, afterEach } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'

import { useDocumentFolderTree } from '@/hooks/use-document-folder-tree'
import { useDocumentUploads } from '@/hooks/use-document-uploads'
import { useDocument, useDocuments } from '@/hooks/use-documents'
import { useManuals } from '@/hooks/use-manuals'
import { useNotes } from '@/hooks/use-notes'
import { jsonResponse, stubApi } from '@/test/helpers/stub-api'

// ADR 0173: a write in one hook refreshes the other hooks that show what it
// changed, through the shared store. The Documents panel used to do this by
// hand (treeChanged, documents.refresh, onFolderCreated).

const embeddings = {
  enabled: false,
  model: '',
  dimensions: 0,
  counts: { chunks_total: 0, chunks_embedded: 0, chunks_stale: 0, chunks_pending: 0, chunks_pending_auto: 0, chars_pending: 0 },
  backfill: { running: false, chunks_embedded: 0, started_at: '' },
}

function routes(extra: Record<string, unknown> = {}) {
  return {
    'GET /api/document-folders': { path: [], folders: [], documents: [] },
    'GET /api/document-folders/tree': { folders: [] },
    'GET /api/documents/tags': [],
    'GET /api/documents/embeddings': embeddings,
    ...extra,
  }
}

const count = (fn: ReturnType<typeof stubApi>, key: string) =>
  fn.mock.calls.filter(([url, init]) => `${(init as RequestInit | undefined)?.method ?? 'GET'} ${url}` === key).length

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('document writes refresh the other readers', () => {
  it('createFolder reloads the folder tree and every other open listing', async () => {
    const fetchMock = stubApi(routes({
      'POST /api/document-folders': { id: 'f9', name: '2026', parent_id: null },
    }))
    const panel = renderHook(() => useDocuments(null))
    const picker = renderHook(() => useDocuments(null))
    const tree = renderHook(() => useDocumentFolderTree())
    await waitFor(() => expect(tree.result.current.loading).toBe(false))
    await waitFor(() => expect(panel.result.current.loading).toBe(false))

    const listings = count(fetchMock, 'GET /api/document-folders')
    const trees = count(fetchMock, 'GET /api/document-folders/tree')
    await act(async () => { await picker.result.current.createFolder('2026', null) })

    await waitFor(() => expect(count(fetchMock, 'GET /api/document-folders')).toBeGreaterThan(listings))
    await waitFor(() => expect(count(fetchMock, 'GET /api/document-folders/tree')).toBeGreaterThan(trees))
  })

  it('renameFolder and deleteDocument reload the folder tree', async () => {
    const fetchMock = stubApi(routes({
      'PATCH /api/document-folders/f1': { id: 'f1', name: 'Handbooks', parent_id: null },
      'DELETE /api/documents/d1': () => jsonResponse(undefined, 204),
    }))
    const docs = renderHook(() => useDocuments(null))
    const tree = renderHook(() => useDocumentFolderTree())
    await waitFor(() => expect(tree.result.current.loading).toBe(false))
    await waitFor(() => expect(docs.result.current.loading).toBe(false))

    let trees = count(fetchMock, 'GET /api/document-folders/tree')
    await act(async () => { await docs.result.current.renameFolder('f1', 'Handbooks') })
    await waitFor(() => expect(count(fetchMock, 'GET /api/document-folders/tree')).toBeGreaterThan(trees))

    trees = count(fetchMock, 'GET /api/document-folders/tree')
    await act(async () => { await docs.result.current.deleteDocument('d1') })
    await waitFor(() => expect(count(fetchMock, 'GET /api/document-folders/tree')).toBeGreaterThan(trees))
  })

  it('moving or retitling an open document refreshes the listings, tag counts and folder tree', async () => {
    const doc = { id: 'd1', title: 'Manual', filename: 'm.pdf', mime: 'application/pdf', kind: 'file', status: 'ready', tags: [], notes: '', folder_id: null }
    const fetchMock = stubApi(routes({
      'GET /api/documents/d1': doc,
      'POST /api/documents/move': () => jsonResponse(undefined, 204),
      'PATCH /api/documents/d1': { ...doc, title: 'Manual 2' },
    }))
    const details = renderHook(() => useDocument('d1'))
    const tree = renderHook(() => useDocumentFolderTree())
    const docs = renderHook(() => useDocuments(null))
    await waitFor(() => expect(details.result.current.loading).toBe(false))
    await waitFor(() => expect(tree.result.current.loading).toBe(false))
    await waitFor(() => expect(docs.result.current.loading).toBe(false))

    let listings = count(fetchMock, 'GET /api/document-folders')
    let trees = count(fetchMock, 'GET /api/document-folders/tree')
    await act(async () => { await details.result.current.move('f1') })
    await waitFor(() => expect(count(fetchMock, 'GET /api/document-folders')).toBeGreaterThan(listings))
    await waitFor(() => expect(count(fetchMock, 'GET /api/document-folders/tree')).toBeGreaterThan(trees))

    listings = count(fetchMock, 'GET /api/document-folders')
    const tags = count(fetchMock, 'GET /api/documents/tags')
    await act(async () => { await details.result.current.patch({ title: 'Manual 2' }) })
    await waitFor(() => expect(count(fetchMock, 'GET /api/document-folders')).toBeGreaterThan(listings))
    await waitFor(() => expect(count(fetchMock, 'GET /api/documents/tags')).toBeGreaterThan(tags))

    listings = count(fetchMock, 'GET /api/document-folders')
    await act(async () => { await details.result.current.patch({ notes: 'only notes' }) })
    expect(count(fetchMock, 'GET /api/document-folders')).toBe(listings)
  })

  it('a document landing from an upload reloads the listing and the folder counts', async () => {
    class UploadXHR {
      static last: UploadXHR
      status = 0
      responseText = ''
      upload = { onprogress: null }
      onload: (() => void) | null = null
      onerror: (() => void) | null = null
      constructor() { UploadXHR.last = this }
      open() {}
      send() {}
      abort() {}
    }
    const fetchMock = stubApi(routes())
    vi.stubGlobal('XMLHttpRequest', UploadXHR)
    const docs = renderHook(() => useDocuments(null))
    const tree = renderHook(() => useDocumentFolderTree())
    const uploads = renderHook(() => useDocumentUploads(null))
    await waitFor(() => expect(tree.result.current.loading).toBe(false))
    await waitFor(() => expect(docs.result.current.loading).toBe(false))

    const listings = count(fetchMock, 'GET /api/document-folders')
    const trees = count(fetchMock, 'GET /api/document-folders/tree')
    act(() => { uploads.result.current.add([new File(['hello'], 'manual.pdf', { type: 'application/pdf' })]) })
    act(() => {
      UploadXHR.last.status = 201
      UploadXHR.last.responseText = JSON.stringify({ document: { id: 'doc-1', filename: 'manual.pdf', status: 'indexed' }, duplicate: false })
      UploadXHR.last.onload?.()
    })

    await waitFor(() => expect(count(fetchMock, 'GET /api/document-folders')).toBeGreaterThan(listings))
    await waitFor(() => expect(count(fetchMock, 'GET /api/document-folders/tree')).toBeGreaterThan(trees))
  })
})

describe('manual and note writes refresh the documents readers', () => {
  async function mountReaders(fetchMock: ReturnType<typeof stubApi>) {
    const docs = renderHook(() => useDocuments(null))
    const tree = renderHook(() => useDocumentFolderTree())
    await waitFor(() => expect(tree.result.current.loading).toBe(false))
    await waitFor(() => expect(docs.result.current.loading).toBe(false))
    return {
      listings: count(fetchMock, 'GET /api/document-folders'),
      trees: count(fetchMock, 'GET /api/document-folders/tree'),
    }
  }

  async function expectRefreshed(fetchMock: ReturnType<typeof stubApi>, before: { listings: number; trees: number }) {
    await waitFor(() => expect(count(fetchMock, 'GET /api/document-folders/tree')).toBeGreaterThan(before.trees))
    await waitFor(() => expect(count(fetchMock, 'GET /api/document-folders')).toBeGreaterThan(before.listings))
  }

  it('flagManual reloads the folder tree and the listings', async () => {
    const fetchMock = stubApi(routes({
      'GET /api/manuals': { manuals: [] },
      'POST /api/manuals': { id: 'f1', name: 'Engine', document_count: 0, updated_at: '' },
    }))
    const manuals = renderHook(() => useManuals(null))
    const before = await mountReaders(fetchMock)
    await act(async () => { await manuals.result.current.flagManual('f1') })
    await expectRefreshed(fetchMock, before)
  })

  it('clearManual reloads the folder tree and the listings', async () => {
    const fetchMock = stubApi(routes({
      'GET /api/manuals': { manuals: [] },
      'DELETE /api/manuals/f1': () => jsonResponse(undefined, 204),
    }))
    const manuals = renderHook(() => useManuals(null))
    const before = await mountReaders(fetchMock)
    await act(async () => { await manuals.result.current.clearManual('f1') })
    await expectRefreshed(fetchMock, before)
  })

  it('createNote and patchNote reload the folder tree and the listings', async () => {
    const detail = { document: { id: 'n1' }, body: '' }
    const fetchMock = stubApi(routes({
      'GET /api/notes': { notes: [] },
      'POST /api/notes': detail,
      'PATCH /api/notes/n1': detail,
    }))
    const notes = renderHook(() => useNotes({ unfiledOnly: true }))
    const before = await mountReaders(fetchMock)
    await act(async () => { await notes.result.current.createNote({ body: 'hello' }) })
    await expectRefreshed(fetchMock, before)

    const after = {
      listings: count(fetchMock, 'GET /api/document-folders'),
      trees: count(fetchMock, 'GET /api/document-folders/tree'),
    }
    await act(async () => { await notes.result.current.patchNote('n1', { folder_id: 'f1' }) })
    await expectRefreshed(fetchMock, after)
  })
})
