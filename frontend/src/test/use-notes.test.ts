import { describe, it, expect, vi, afterEach } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'

import { useNotes } from '@/hooks/use-notes'

// ADR 0116 / plan §7: use-notes.ts owns list/get/create/patch for the Notes
// inbox. Same routedFetch idiom as use-documents.test.ts (a small router by
// method + URL, since one hook action can trigger more than one request -
// a write, then its own refresh).

interface Call {
  url: string
  method: string
  body: unknown
}

function notePayload(overrides: Record<string, unknown> = {}) {
  return {
    id: 'note-1',
    sha256: 'abc123',
    folder_id: null,
    filename: 'Genset start-up.md',
    title: 'Genset start-up',
    notes: '',
    mime: 'text/markdown',
    size_bytes: 256,
    page_count: 0,
    summary: '',
    status: 'pending',
    stage: 'extract',
    tags: [],
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    indexed_at: null,
    kind: 'note',
    note_type: 'procedure',
    note_type_source: 'auto',
    pinned: false,
    sort_index: 0,
    ...overrides,
  }
}

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
    return handler(parsed, init)
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
  vi.unstubAllGlobals()
})

describe('useNotes', () => {
  it('fetches the unfiled inbox with ?filed=0 when unfiledOnly is set', async () => {
    const { fn } = routedFetch({
      'GET /api/notes': (url) => {
        expect(url.searchParams.get('filed')).toBe('0')
        return ok({ notes: [notePayload()] })
      },
    })
    vi.stubGlobal('fetch', fn)

    const { result } = renderHook(() => useNotes({ unfiledOnly: true }))

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.notes).toHaveLength(1)
    expect(result.current.notes[0].title).toBe('Genset start-up')
  })

  it('omits the filed param entirely when unfiledOnly is not set', async () => {
    const { fn } = routedFetch({
      'GET /api/notes': (url) => {
        expect(url.searchParams.has('filed')).toBe(false)
        return ok({ notes: [] })
      },
    })
    vi.stubGlobal('fetch', fn)

    const { result } = renderHook(() => useNotes())
    await waitFor(() => expect(result.current.loading).toBe(false))
  })

  it('passes type and tag filters through as query params', async () => {
    const { fn } = routedFetch({
      'GET /api/notes': (url) => {
        expect(url.searchParams.get('type')).toBe('quirk')
        expect(url.searchParams.get('tag')).toBe('engine')
        return ok({ notes: [] })
      },
    })
    vi.stubGlobal('fetch', fn)

    const { result } = renderHook(() => useNotes({ type: 'quirk', tag: 'engine' }))
    await waitFor(() => expect(result.current.loading).toBe(false))
  })

  it('createNote POSTs only what it is given (body-only capture) and refreshes the inbox', async () => {
    const { fn, calls } = routedFetch({
      'GET /api/notes': () => ok({ notes: [notePayload()] }),
      'POST /api/notes': () => ok({ document: notePayload(), body: 'Ring Dave about the mooring' }, 201),
    })
    vi.stubGlobal('fetch', fn)

    const { result } = renderHook(() => useNotes({ unfiledOnly: true }))
    await waitFor(() => expect(result.current.loading).toBe(false))

    let created
    await act(async () => {
      created = await result.current.createNote({ body: 'Ring Dave about the mooring' })
    })

    expect(created).toMatchObject({ body: 'Ring Dave about the mooring' })
    const postCall = calls.find((c) => c.method === 'POST')
    expect(postCall?.body).toEqual({ body: 'Ring Dave about the mooring' })
    // A create is not a no-op for the inbox listing - the new note has to
    // show up without a manual reload.
    expect(calls.filter((c) => c.method === 'GET' && c.url.startsWith('/api/notes')).length).toBeGreaterThanOrEqual(2)
  })

  // Plan §7 / verification: "filing a note from the inbox removes it from
  // the list." Filing is PATCH .../folder_id + refresh() (patchNote's own
  // comment: "a type override or a file-out both change what the inbox
  // shows") - the inbox view is ?filed=0, so the server's own second
  // response (now excluding the just-filed note, since it has a folder_id)
  // is what actually drains the list, not any client-side filtering here.
  it('filing a note (folder_id patch) drops it out of the unfiled-only list after the refresh', async () => {
    let listCall = 0
    const { fn } = routedFetch({
      'GET /api/notes': () => {
        listCall += 1
        // First call: the mount fetch, note still unfiled. Second call:
        // patchNote's own post-write refresh(), server now excludes it -
        // it has a folder_id and this list is ?filed=0.
        return ok({ notes: listCall === 1 ? [notePayload({ id: 'note-1' })] : [] })
      },
      'PATCH /api/notes/note-1': () => ok({ document: notePayload({ id: 'note-1', folder_id: 'folder-9' }), body: 'text' }),
    })
    vi.stubGlobal('fetch', fn)

    const { result } = renderHook(() => useNotes({ unfiledOnly: true }))
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.notes.map((n) => n.id)).toEqual(['note-1'])

    await act(async () => {
      await result.current.patchNote('note-1', { folder_id: 'folder-9' })
    })

    expect(result.current.notes).toEqual([])
  })

  it('patchNote PATCHes the given fields and refreshes, returning the updated note', async () => {
    const { fn, calls } = routedFetch({
      'GET /api/notes': () => ok({ notes: [] }),
      'PATCH /api/notes/note-1': () => ok({ document: notePayload({ note_type: 'contact', note_type_source: 'operator' }), body: 'text' }),
    })
    vi.stubGlobal('fetch', fn)

    const { result } = renderHook(() => useNotes({ unfiledOnly: true }))
    await waitFor(() => expect(result.current.loading).toBe(false))

    let updated
    await act(async () => {
      updated = await result.current.patchNote('note-1', { type: 'contact' })
    })

    expect(updated).toMatchObject({ document: { note_type: 'contact', note_type_source: 'operator' } })
    const patchCall = calls.find((c) => c.method === 'PATCH')
    expect(patchCall?.url).toBe('/api/notes/note-1')
    expect(patchCall?.body).toEqual({ type: 'contact' })
  })

  it('getNote fetches a single note by id, not the list endpoint, and does not touch the list', async () => {
    const { fn, calls } = routedFetch({
      'GET /api/notes': () => ok({ notes: [notePayload({ id: 'note-1' })] }),
      'GET /api/notes/note-2': () => ok({ document: notePayload({ id: 'note-2', title: 'Fuel valves' }), body: 'Port is the return.' }),
    })
    vi.stubGlobal('fetch', fn)

    const { result } = renderHook(() => useNotes({ unfiledOnly: true }))
    await waitFor(() => expect(result.current.loading).toBe(false))

    let detail
    await act(async () => {
      detail = await result.current.getNote('note-2')
    })

    expect(detail).toMatchObject({ document: { id: 'note-2', title: 'Fuel valves' }, body: 'Port is the return.' })
    expect(result.current.notes.map((n) => n.id)).toEqual(['note-1']) // list untouched
    expect(calls.some((c) => c.url === '/api/notes/note-2')).toBe(true)
  })

  // AGENTS.md fallback policy: the server's own message surfaces verbatim -
  // an empty capture is exactly the 400 notes_handlers.go returns for it.
  it('surfaces the server\'s message on a failed create (e.g. an empty body)', async () => {
    const { fn } = routedFetch({
      'GET /api/notes': () => ok({ notes: [] }),
      'POST /api/notes': () => failed(400, 'note body is empty'),
    })
    vi.stubGlobal('fetch', fn)

    const { result } = renderHook(() => useNotes({ unfiledOnly: true }))
    await waitFor(() => expect(result.current.loading).toBe(false))

    await expect(result.current.createNote({ body: '' })).rejects.toThrow('note body is empty')
  })

  // Same ordering-guard shape as use-documents.test.ts's own refresh test:
  // an older in-flight GET /api/notes resolving after a newer one must not
  // clobber the newer, more-correct state.
  it('an older in-flight refresh does not overwrite a newer one\'s state', async () => {
    let resolveFirst!: (v: unknown) => void
    let resolveSecond!: (v: unknown) => void
    let call = 0
    const { fn } = routedFetch({
      'GET /api/notes': () => {
        call += 1
        if (call === 1) return new Promise((resolve) => { resolveFirst = resolve })
        if (call === 2) return new Promise((resolve) => { resolveSecond = resolve })
        return ok({ notes: [] })
      },
    })
    vi.stubGlobal('fetch', fn)

    const { result } = renderHook(() => useNotes({ unfiledOnly: true }))
    await waitFor(() => expect(call).toBeGreaterThanOrEqual(1)) // the mount's own refresh, in flight

    act(() => { void result.current.refresh() })
    await waitFor(() => expect(call).toBe(2))

    await act(async () => { resolveSecond(ok({ notes: [notePayload({ id: 'note-new' })] })) })
    await waitFor(() => expect(result.current.notes.map((n) => n.id)).toEqual(['note-new']))

    await act(async () => { resolveFirst(ok({ notes: [notePayload({ id: 'note-stale' })] })) })
    expect(result.current.notes.map((n) => n.id)).toEqual(['note-new'])
  })

  it('patchNote PATCHes pinned alone, without any of the note-content fields', async () => {
    const { fn, calls } = routedFetch({
      'GET /api/notes': () => ok({ notes: [] }),
      'PATCH /api/notes/note-1': () => ok({ document: notePayload({ pinned: true }), body: 'text' }),
    })
    vi.stubGlobal('fetch', fn)

    const { result } = renderHook(() => useNotes({ unfiledOnly: true }))
    await waitFor(() => expect(result.current.loading).toBe(false))

    let updated
    await act(async () => {
      updated = await result.current.patchNote('note-1', { pinned: true })
    })

    expect(updated).toMatchObject({ document: { pinned: true } })
    const patchCall = calls.find((c) => c.method === 'PATCH')
    expect(patchCall?.body).toEqual({ pinned: true })
  })
})
