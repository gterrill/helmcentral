import { describe, it, expect, vi, afterEach } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'

import { findManualTreeNode, useManuals } from '@/hooks/use-manuals'

// ADR 0116 / plan §7: use-manuals.ts owns the Manuals panel's data layer -
// the library-wide list (for the none/one/several state and the switcher)
// and the currently-open manual's own ordered tree. Same routedFetch idiom
// as use-notes.test.ts/use-documents.test.ts.

interface Call {
  url: string
  method: string
  body: unknown
}

function manualPayload(overrides: Record<string, unknown> = {}) {
  return {
    id: 'manual-1',
    name: 'Operations Manual',
    document_count: 3,
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

function treePayload(overrides: Record<string, unknown> = {}) {
  return {
    type: 'folder',
    id: 'manual-1',
    name: 'Operations Manual',
    sort_index: 0,
    updated_at: '2026-01-01T00:00:00Z',
    children: [
      { type: 'document', id: 'note-1', name: 'Genset start-up', sort_index: 0, updated_at: '2026-01-01T00:00:00Z', kind: 'note', note_type: 'procedure', mime: 'text/markdown' },
    ],
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

function noContent() {
  return { ok: true, status: 204, json: async () => ({}) }
}

function failed(status: number, error: string) {
  return { ok: false, status, json: async () => ({ error }) }
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('findManualTreeNode', () => {
  const tree = treePayload({
    children: [
      {
        type: 'folder', id: 'folder-a', name: 'Engine room', sort_index: 0, updated_at: '2026-01-01T00:00:00Z',
        children: [
          { type: 'document', id: 'note-2', name: 'Genset shutdown', sort_index: 0, updated_at: '2026-01-01T00:00:00Z', kind: 'note', note_type: 'procedure' },
        ],
      },
    ],
  }) as unknown as import('@/hooks/use-manuals').ManualTreeNode

  it('finds the root node by its own id', () => {
    expect(findManualTreeNode(tree, 'manual-1')?.id).toBe('manual-1')
  })

  it('finds a nested document node several levels down', () => {
    expect(findManualTreeNode(tree, 'note-2')?.name).toBe('Genset shutdown')
  })

  it('returns null for an id not present anywhere in the subtree', () => {
    expect(findManualTreeNode(tree, 'nope')).toBeNull()
  })
})

describe('useManuals: library list', () => {
  it('fetches the library-wide manuals list on mount, even with nothing open', async () => {
    const { fn } = routedFetch({
      'GET /api/manuals': () => ok({ manuals: [manualPayload(), manualPayload({ id: 'manual-2', name: 'Crew Training' })] }),
    })
    vi.stubGlobal('fetch', fn)

    const { result } = renderHook(() => useManuals(null))

    await waitFor(() => expect(result.current.manualsLoading).toBe(false))
    expect(result.current.manuals).toHaveLength(2)
    expect(result.current.manuals[0].name).toBe('Operations Manual')
    // No manual is open, so no tree request should ever have been issued.
    expect(fn.mock.calls.some((c) => String(c[0]).includes('/tree'))).toBe(false)
  })

  it('treats an empty library as a normal state, not an error', async () => {
    const { fn } = routedFetch({
      'GET /api/manuals': () => ok({ manuals: [] }),
    })
    vi.stubGlobal('fetch', fn)

    const { result } = renderHook(() => useManuals(null))
    await waitFor(() => expect(result.current.manualsLoading).toBe(false))
    expect(result.current.manuals).toEqual([])
    expect(result.current.manualsError).toBeNull()
  })
})

describe('useManuals: the open manual\'s tree', () => {
  it('fetches the tree for the given manualId and orders children as the server sent them', async () => {
    const { fn } = routedFetch({
      'GET /api/manuals': () => ok({ manuals: [manualPayload()] }),
      'GET /api/manuals/manual-1/tree': () => ok(treePayload()),
    })
    vi.stubGlobal('fetch', fn)

    const { result } = renderHook(() => useManuals('manual-1'))

    await waitFor(() => expect(result.current.treeLoading).toBe(false))
    expect(result.current.tree?.children).toHaveLength(1)
    expect(result.current.tree?.children?.[0].id).toBe('note-1')
  })

  it('re-fetches the tree when manualId changes, and clears it when it goes back to null', async () => {
    const { fn } = routedFetch({
      'GET /api/manuals': () => ok({ manuals: [manualPayload(), manualPayload({ id: 'manual-2', name: 'Crew Training' })] }),
      'GET /api/manuals/manual-1/tree': () => ok(treePayload()),
      'GET /api/manuals/manual-2/tree': () => ok(treePayload({ id: 'manual-2', name: 'Crew Training', children: [] })),
    })
    vi.stubGlobal('fetch', fn)

    const { result, rerender } = renderHook(({ id }) => useManuals(id), { initialProps: { id: 'manual-1' as string | null } })
    await waitFor(() => expect(result.current.tree?.id).toBe('manual-1'))

    rerender({ id: 'manual-2' })
    await waitFor(() => expect(result.current.tree?.id).toBe('manual-2'))

    rerender({ id: null })
    await waitFor(() => expect(result.current.tree).toBeNull())
  })
})

describe('useManuals: writes', () => {
  it('createManual POSTs {name} and refreshes the library list', async () => {
    const { fn, calls } = routedFetch({
      'GET /api/manuals': () => ok({ manuals: [manualPayload()] }),
      'POST /api/manuals': () => ok(manualPayload(), 201),
    })
    vi.stubGlobal('fetch', fn)

    const { result } = renderHook(() => useManuals(null))
    await waitFor(() => expect(result.current.manualsLoading).toBe(false))

    let created
    await act(async () => {
      created = await result.current.createManual('Operations Manual')
    })

    expect(created).toMatchObject({ name: 'Operations Manual' })
    const postCall = calls.find((c) => c.method === 'POST')
    expect(postCall?.body).toEqual({ name: 'Operations Manual' })
    expect(calls.filter((c) => c.method === 'GET' && c.url === '/api/manuals').length).toBeGreaterThanOrEqual(2)
  })

  it('flagManual POSTs {folder_id} rather than {name}', async () => {
    const { fn, calls } = routedFetch({
      'GET /api/manuals': () => ok({ manuals: [] }),
      'POST /api/manuals': () => ok(manualPayload(), 201),
    })
    vi.stubGlobal('fetch', fn)

    const { result } = renderHook(() => useManuals(null))
    await waitFor(() => expect(result.current.manualsLoading).toBe(false))

    await act(async () => {
      await result.current.flagManual('folder-9')
    })

    const postCall = calls.find((c) => c.method === 'POST')
    expect(postCall?.body).toEqual({ folder_id: 'folder-9' })
  })

  it('clearManual DELETEs and refreshes, and clears the tree if that manual was open', async () => {
    const { fn, calls } = routedFetch({
      'GET /api/manuals': () => ok({ manuals: [manualPayload()] }),
      'GET /api/manuals/manual-1/tree': () => ok(treePayload()),
      'DELETE /api/manuals/manual-1': () => noContent(),
    })
    vi.stubGlobal('fetch', fn)

    const { result } = renderHook(() => useManuals('manual-1'))
    await waitFor(() => expect(result.current.tree).not.toBeNull())

    await act(async () => {
      await result.current.clearManual('manual-1')
    })

    expect(calls.some((c) => c.method === 'DELETE' && c.url === '/api/manuals/manual-1')).toBe(true)
    expect(result.current.tree).toBeNull()
  })

  it('reorder POSTs {parent_id, items} to this manual\'s own reorder endpoint and applies the returned tree directly', async () => {
    const { fn, calls } = routedFetch({
      'GET /api/manuals': () => ok({ manuals: [manualPayload()] }),
      'GET /api/manuals/manual-1/tree': () => ok(treePayload()),
      'POST /api/manuals/manual-1/reorder': () => ok(treePayload({ children: [
        { type: 'document', id: 'note-1', name: 'Genset start-up', sort_index: 1, updated_at: '2026-01-01T00:00:00Z', kind: 'note', note_type: 'procedure', mime: 'text/markdown' },
      ] })),
    })
    vi.stubGlobal('fetch', fn)

    const { result } = renderHook(() => useManuals('manual-1'))
    await waitFor(() => expect(result.current.tree).not.toBeNull())

    await act(async () => {
      await result.current.reorder('manual-1', [{ kind: 'document', id: 'note-1', sort_index: 1 }])
    })

    const reorderCall = calls.find((c) => c.method === 'POST' && c.url === '/api/manuals/manual-1/reorder')
    expect(reorderCall?.body).toEqual({ parent_id: 'manual-1', items: [{ kind: 'document', id: 'note-1', sort_index: 1 }] })
    // Applied straight from the response - no second GET .../tree fired
    // after the reorder resolves.
    expect(result.current.tree?.children?.[0].sort_index).toBe(1)
    expect(calls.filter((c) => c.method === 'GET' && c.url === '/api/manuals/manual-1/tree').length).toBe(1)
  })

  // AGENTS.md fallback policy: the server's own message surfaces verbatim.
  it('surfaces the server\'s message on a failed createManual (e.g. a duplicate name)', async () => {
    const { fn } = routedFetch({
      'GET /api/manuals': () => ok({ manuals: [] }),
      'POST /api/manuals': () => failed(409, 'folder name already exists'),
    })
    vi.stubGlobal('fetch', fn)

    const { result } = renderHook(() => useManuals(null))
    await waitFor(() => expect(result.current.manualsLoading).toBe(false))

    await expect(result.current.createManual('Operations Manual')).rejects.toThrow('folder name already exists')
  })
})
