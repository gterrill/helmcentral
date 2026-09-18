import { describe, it, expect, vi, afterEach } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'

import { useDocuments } from '@/hooks/use-documents'

// ADR 0106 F1: use-documents.ts owns folder browsing, search, tags and every
// document/folder write for the Documents panel. Fetch is stubbed with a
// small router (by method + URL) rather than a sequence of mockResolvedValueOnce
// calls, because a single hook action can trigger more than one request (a
// write, then its own refetch) and the exact call count/order is not what
// these tests want to pin - callFor() below finds whichever call matches.

interface Call {
  url: string
  method: string
  body: unknown
}

function folderPayload(overrides: Record<string, unknown> = {}) {
  return {
    path: [],
    folders: [],
    documents: [],
    ...overrides,
  }
}

function docPayload(overrides: Record<string, unknown> = {}) {
  return {
    id: 'doc-1',
    sha256: 'abc123',
    folder_id: null,
    filename: 'manual.pdf',
    title: '',
    notes: '',
    mime: 'application/pdf',
    size_bytes: 1024,
    page_count: 3,
    summary: '',
    status: 'pending',
    stage: 'extract',
    tags: [],
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    indexed_at: null,
    ...overrides,
  }
}

/** Routes a fetch mock by exact pathname + method, recording every call
 * (queryable by callFor) so assertions can inspect query params without
 * caring what order the hook happens to issue its other requests in. */
function routedFetch(routes: Record<string, (url: URL, init?: RequestInit) => unknown>) {
  const calls: Call[] = []
  const fn = vi.fn(async (url: string, init?: RequestInit) => {
    const method = init?.method ?? 'GET'
    calls.push({ url, method, body: init?.body ? JSON.parse(init.body as string) : undefined })
    const parsed = new URL(url, 'http://test.local')
    const handler = routes[`${method} ${parsed.pathname}`]
    if (!handler) {
      throw new Error(`unhandled fetch: ${method} ${url}`)
    }
    const result = handler(parsed, init)
    return result
  })
  return { fn, calls }
}

function ok(body: unknown, status = 200) {
  return { ok: true, status, json: async () => body }
}

function failed(status: number, error: string) {
  return { ok: false, status, json: async () => ({ error }) }
}

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('useDocuments', () => {
  it('fetches the root folder listing on mount', async () => {
    const { fn, calls } = routedFetch({
      'GET /api/document-folders': () => ok(folderPayload({ documents: [docPayload({ status: 'indexed' })] })),
      'GET /api/documents/tags': () => ok([]),
    })
    vi.stubGlobal('fetch', fn)

    const { result } = renderHook(() => useDocuments(null))

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.documents).toHaveLength(1)
    expect(calls.some((c) => c.method === 'GET' && c.url === '/api/document-folders')).toBe(true)
  })

  it('fetches a subfolder listing with its parent id, and exposes the breadcrumb path', async () => {
    const { fn } = routedFetch({
      'GET /api/document-folders': (url) => {
        expect(url.searchParams.get('parent')).toBe('f1')
        return ok(folderPayload({ path: [{ id: 'f1', name: 'Manuals', parent_id: null }] }))
      },
      'GET /api/documents/tags': () => ok([]),
    })
    vi.stubGlobal('fetch', fn)

    const { result } = renderHook(() => useDocuments('f1'))

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.path).toEqual([{ id: 'f1', name: 'Manuals', parent_id: null }])
  })

  it('loads tag counts on mount', async () => {
    const { fn } = routedFetch({
      'GET /api/document-folders': () => ok(folderPayload()),
      'GET /api/documents/tags': () => ok([{ tag: 'engine', count: 3 }]),
    })
    vi.stubGlobal('fetch', fn)

    const { result } = renderHook(() => useDocuments(null))

    await waitFor(() => expect(result.current.tags).toEqual([{ tag: 'engine', count: 3 }]))
  })

  it('search sends q, recursive=true and the current folder', async () => {
    const { fn, calls } = routedFetch({
      'GET /api/document-folders': () => ok(folderPayload()),
      'GET /api/documents/tags': () => ok([]),
      'GET /api/documents': (url) => {
        expect(url.searchParams.get('q')).toBe('impeller')
        expect(url.searchParams.get('recursive')).toBe('true')
        expect(url.searchParams.get('folder')).toBe('f1')
        return ok({ results: [{ document_id: 'doc-1', filename: 'manual.pdf', title: '', status: 'indexed', page: 2, snippet: 'the \x02impeller\x03 kit', folder_id: 'f1' }] })
      },
    })
    vi.stubGlobal('fetch', fn)

    const { result } = renderHook(() => useDocuments('f1'))
    await waitFor(() => expect(result.current.loading).toBe(false))

    await act(async () => {
      await result.current.search('impeller')
    })

    expect(result.current.searchResults).toHaveLength(1)
    expect(result.current.searchResults?.[0].snippet).toContain('impeller')
    expect(calls.some((c) => c.url.startsWith('/api/documents?'))).toBe(true)
  })

  it('search scopes to the root when allFolders is requested from within a subfolder', async () => {
    const { fn } = routedFetch({
      'GET /api/document-folders': () => ok(folderPayload()),
      'GET /api/documents/tags': () => ok([]),
      'GET /api/documents': (url) => {
        expect(url.searchParams.get('folder')).toBe('root')
        return ok({ results: [] })
      },
    })
    vi.stubGlobal('fetch', fn)

    const { result } = renderHook(() => useDocuments('f1'))
    await waitFor(() => expect(result.current.loading).toBe(false))

    await act(async () => {
      await result.current.search('impeller', { allFolders: true })
    })
  })

  it('clearing the query to empty clears results with no request', async () => {
    const { fn, calls } = routedFetch({
      'GET /api/document-folders': () => ok(folderPayload()),
      'GET /api/documents/tags': () => ok([]),
      'GET /api/documents': () => ok({ results: [] }),
    })
    vi.stubGlobal('fetch', fn)

    const { result } = renderHook(() => useDocuments(null))
    await waitFor(() => expect(result.current.loading).toBe(false))

    await act(async () => {
      await result.current.search('impeller')
    })
    calls.length = 0

    await act(async () => {
      await result.current.search('   ')
    })

    expect(result.current.searchResults).toBeNull()
    expect(calls).toHaveLength(0)
  })

  it('selecting a tag filters the folder document list through the flat endpoint', async () => {
    const { fn } = routedFetch({
      'GET /api/document-folders': () => ok(folderPayload({ documents: [docPayload({ id: 'doc-unfiltered' })] })),
      'GET /api/documents/tags': () => ok([{ tag: 'engine', count: 1 }]),
      'GET /api/documents': (url) => {
        expect(url.searchParams.get('tag')).toBe('engine')
        expect(url.searchParams.get('folder')).toBe('root')
        return ok({ documents: [docPayload({ id: 'doc-tagged' })] })
      },
    })
    vi.stubGlobal('fetch', fn)

    const { result } = renderHook(() => useDocuments(null))
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.documents[0].id).toBe('doc-unfiltered')

    act(() => {
      result.current.setSelectedTag('engine')
    })

    await waitFor(() => expect(result.current.documents[0]?.id).toBe('doc-tagged'))
  })

  it('polls the folder listing every 3s while a document is pending, and stops once none are', async () => {
    let indexed = false
    const { fn, calls } = routedFetch({
      'GET /api/document-folders': () =>
        ok(folderPayload({ documents: [docPayload({ status: indexed ? 'indexed' : 'pending' })] })),
      'GET /api/documents/tags': () => ok([]),
    })
    vi.stubGlobal('fetch', fn)

    // Fake timers from the very start (not switched on only after mount, as
    // waitFor-based tests elsewhere in this file do it): the poll interval
    // is armed by an effect that runs as soon as the mount fetch resolves,
    // so it must already be the fake setInterval when that happens, or this
    // test's own advanceTimersByTimeAsync below would never reach it -
    // mirrors use-document-uploads.test.ts's own timer-first ordering.
    vi.useFakeTimers()
    const { result } = renderHook(() => useDocuments(null))
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(result.current.loading).toBe(false)
    expect(result.current.documents[0].status).toBe('pending')

    calls.length = 0
    indexed = true
    await act(async () => {
      await vi.advanceTimersByTimeAsync(3000)
    })
    expect(result.current.documents[0].status).toBe('indexed')

    calls.length = 0
    await act(async () => {
      await vi.advanceTimersByTimeAsync(9000)
    })
    expect(calls.filter((c) => c.url.startsWith('/api/document-folders'))).toHaveLength(0)
  })

  it('patchDocument PATCHes and refreshes the listing', async () => {
    const { fn, calls } = routedFetch({
      'GET /api/document-folders': () => ok(folderPayload()),
      'GET /api/documents/tags': () => ok([]),
      'PATCH /api/documents/doc-1': () => ok(docPayload({ title: 'Impeller kit' })),
    })
    vi.stubGlobal('fetch', fn)

    const { result } = renderHook(() => useDocuments(null))
    await waitFor(() => expect(result.current.loading).toBe(false))

    let returned
    await act(async () => {
      returned = await result.current.patchDocument('doc-1', { title: 'Impeller kit' })
    })

    expect(returned).toMatchObject({ title: 'Impeller kit' })
    const patchCall = calls.find((c) => c.method === 'PATCH')
    expect(patchCall?.body).toEqual({ title: 'Impeller kit' })
  })

  it('deleteDocument DELETEs and refreshes', async () => {
    const { fn, calls } = routedFetch({
      'GET /api/document-folders': () => ok(folderPayload()),
      'GET /api/documents/tags': () => ok([]),
      'DELETE /api/documents/doc-1': () => ({ ok: true, status: 204, json: async () => ({}) }),
    })
    vi.stubGlobal('fetch', fn)

    const { result } = renderHook(() => useDocuments(null))
    await waitFor(() => expect(result.current.loading).toBe(false))

    await act(async () => {
      await result.current.deleteDocument('doc-1')
    })

    expect(calls.some((c) => c.method === 'DELETE' && c.url === '/api/documents/doc-1')).toBe(true)
  })

  it('reindexDocument POSTs and returns the enrich/problem outcome', async () => {
    const { fn } = routedFetch({
      'GET /api/document-folders': () => ok(folderPayload()),
      'GET /api/documents/tags': () => ok([]),
      'POST /api/documents/doc-1/reindex': () => ok({ document: docPayload({ status: 'pending' }), enrich: false, problem: 'Set up the assistant in Settings.' }),
    })
    vi.stubGlobal('fetch', fn)

    const { result } = renderHook(() => useDocuments(null))
    await waitFor(() => expect(result.current.loading).toBe(false))

    let outcome
    await act(async () => {
      outcome = await result.current.reindexDocument('doc-1')
    })

    expect(outcome).toMatchObject({ enrich: false, problem: 'Set up the assistant in Settings.' })
  })

  it('moveDocuments POSTs the ids and target folder', async () => {
    const { fn, calls } = routedFetch({
      'GET /api/document-folders': () => ok(folderPayload()),
      'GET /api/documents/tags': () => ok([]),
      'POST /api/documents/move': () => ({ ok: true, status: 204, json: async () => ({}) }),
    })
    vi.stubGlobal('fetch', fn)

    const { result } = renderHook(() => useDocuments(null))
    await waitFor(() => expect(result.current.loading).toBe(false))

    await act(async () => {
      await result.current.moveDocuments(['doc-1', 'doc-2'], 'f1')
    })

    const moveCall = calls.find((c) => c.method === 'POST' && c.url === '/api/documents/move')
    expect(moveCall?.body).toEqual({ ids: ['doc-1', 'doc-2'], folder_id: 'f1' })
  })

  it('createFolder POSTs and refreshes', async () => {
    const { fn, calls } = routedFetch({
      'GET /api/document-folders': () => ok(folderPayload()),
      'GET /api/documents/tags': () => ok([]),
      'POST /api/document-folders': () => ok({ id: 'f2', name: 'Receipts', parent_id: null }, 201),
    })
    vi.stubGlobal('fetch', fn)

    const { result } = renderHook(() => useDocuments(null))
    await waitFor(() => expect(result.current.loading).toBe(false))

    let created
    await act(async () => {
      created = await result.current.createFolder('Receipts', null)
    })

    expect(created).toMatchObject({ id: 'f2', name: 'Receipts' })
    const createCall = calls.find((c) => c.method === 'POST' && c.url === '/api/document-folders')
    expect(createCall?.body).toEqual({ name: 'Receipts', parent_id: null })
  })

  it('renameFolder PATCHes the name', async () => {
    const { fn, calls } = routedFetch({
      'GET /api/document-folders': () => ok(folderPayload()),
      'GET /api/documents/tags': () => ok([]),
      'PATCH /api/document-folders/f1': () => ok({ id: 'f1', name: 'Engine manuals', parent_id: null }),
    })
    vi.stubGlobal('fetch', fn)

    const { result } = renderHook(() => useDocuments(null))
    await waitFor(() => expect(result.current.loading).toBe(false))

    await act(async () => {
      await result.current.renameFolder('f1', 'Engine manuals')
    })

    const patchCall = calls.find((c) => c.method === 'PATCH')
    expect(patchCall?.body).toEqual({ name: 'Engine manuals' })
  })

  // AGENTS.md fallback policy: a 409 (a cycle, a taken name, a non-empty
  // folder) is the server's own message, surfaced verbatim to the caller
  // rather than masked behind a generic "something went wrong".
  it('surfaces the server\'s 409 message when deleting a non-empty folder', async () => {
    const { fn } = routedFetch({
      'GET /api/document-folders': () => ok(folderPayload()),
      'GET /api/documents/tags': () => ok([]),
      'DELETE /api/document-folders/f1': () => failed(409, 'folder is not empty'),
    })
    vi.stubGlobal('fetch', fn)

    const { result } = renderHook(() => useDocuments(null))
    await waitFor(() => expect(result.current.loading).toBe(false))

    await expect(result.current.deleteFolder('f1')).rejects.toThrow('folder is not empty')
  })

  it('surfaces the server\'s 409 message when moving a folder into its own descendant', async () => {
    const { fn } = routedFetch({
      'GET /api/document-folders': () => ok(folderPayload()),
      'GET /api/documents/tags': () => ok([]),
      'PATCH /api/document-folders/f1': () => failed(409, 'folder move would create a cycle'),
    })
    vi.stubGlobal('fetch', fn)

    const { result } = renderHook(() => useDocuments(null))
    await waitFor(() => expect(result.current.loading).toBe(false))

    await expect(result.current.moveFolder('f1', 'f2')).rejects.toThrow('folder move would create a cycle')
  })

  // Review finding: refresh() had no ordering guard, so an older in-flight
  // call whose fetch happens to resolve AFTER a newer call's could silently
  // overwrite the newer, more-correct state - e.g. a 3s poll tick that was
  // already in flight when a post-delete refresh() fires, landing last and
  // resurrecting the just-deleted row.
  it('an older in-flight refresh does not overwrite a newer one\'s state', async () => {
    let resolveFirst!: (v: unknown) => void
    let resolveSecond!: (v: unknown) => void
    let call = 0
    const { fn } = routedFetch({
      'GET /api/document-folders': () => {
        call += 1
        if (call === 1) return new Promise((resolve) => { resolveFirst = resolve })
        if (call === 2) return new Promise((resolve) => { resolveSecond = resolve })
        return ok(folderPayload())
      },
      'GET /api/documents/tags': () => ok([]),
    })
    vi.stubGlobal('fetch', fn)

    const { result } = renderHook(() => useDocuments(null))
    await waitFor(() => expect(call).toBeGreaterThanOrEqual(1)) // the mount's own refresh, now in flight

    act(() => { void result.current.refresh() }) // e.g. a post-delete refresh, fired while the mount's refresh is still pending
    await waitFor(() => expect(call).toBe(2))

    // resolve the NEWER call first, with its own data...
    await act(async () => { resolveSecond(ok(folderPayload({ documents: [docPayload({ id: 'doc-new' })] }))) })
    await waitFor(() => expect(result.current.documents.map((d) => d.id)).toEqual(['doc-new']))

    // ...then the STALE older call resolves, with different data. It must
    // not be applied over what the newer call already set.
    await act(async () => { resolveFirst(ok(folderPayload({ documents: [docPayload({ id: 'doc-stale' })] }))) })

    expect(result.current.documents.map((d) => d.id)).toEqual(['doc-new'])
  })

  it('an older in-flight search does not overwrite a newer one\'s results', async () => {
    let resolveFirst!: (v: unknown) => void
    let resolveSecond!: (v: unknown) => void
    let call = 0
    const { fn } = routedFetch({
      'GET /api/document-folders': () => ok(folderPayload()),
      'GET /api/documents/tags': () => ok([]),
      'GET /api/documents': () => {
        call += 1
        if (call === 1) return new Promise((resolve) => { resolveFirst = resolve })
        if (call === 2) return new Promise((resolve) => { resolveSecond = resolve })
        return ok({ results: [] })
      },
    })
    vi.stubGlobal('fetch', fn)

    const { result } = renderHook(() => useDocuments(null))
    await waitFor(() => expect(result.current.loading).toBe(false))

    act(() => { void result.current.search('impeller') })
    await waitFor(() => expect(call).toBe(1))
    act(() => { void result.current.search('bilge pump') })
    await waitFor(() => expect(call).toBe(2))

    // resolve the NEWER search first...
    await act(async () => {
      resolveSecond(ok({ results: [{ document_id: 'doc-new', filename: 'a.pdf', title: '', status: 'indexed', page: 1, snippet: '', folder_id: null }] }))
    })
    await waitFor(() => expect(result.current.searchResults?.map((r) => r.document_id)).toEqual(['doc-new']))

    // ...then the STALE older search resolves. It must not clobber it.
    await act(async () => {
      resolveFirst(ok({ results: [{ document_id: 'doc-stale', filename: 'b.pdf', title: '', status: 'indexed', page: 1, snippet: '', folder_id: null }] }))
    })

    expect(result.current.searchResults?.map((r) => r.document_id)).toEqual(['doc-new'])
  })

  // Enrichment writes suggested tags after a document lands, so the poll that
  // watches a pending document has to refresh the tag filter too. Otherwise
  // the filter row keeps showing the tags from before the document was read.
  it('refreshes the tag filter while polling a pending document', async () => {
    let indexed = false
    const { fn } = routedFetch({
      'GET /api/document-folders': () =>
        ok(folderPayload({ documents: [docPayload({ status: indexed ? 'indexed' : 'pending' })] })),
      'GET /api/documents/tags': () =>
        ok(indexed ? [{ tag: 'Receipt', count: 1 }, { tag: 'Johnson', count: 1 }] : [{ tag: 'Receipt', count: 1 }]),
    })
    vi.stubGlobal('fetch', fn)

    vi.useFakeTimers()
    const { result } = renderHook(() => useDocuments(null))
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(result.current.tags.map((t) => t.tag)).toEqual(['Receipt'])

    indexed = true
    await act(async () => {
      await vi.advanceTimersByTimeAsync(3000)
    })
    expect(result.current.tags.map((t) => t.tag)).toEqual(['Receipt', 'Johnson'])
  })

})
