import { describe, it, expect, afterEach, vi } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'

import { __resetDocumentCitationCacheForTests, useDocumentCitation } from '@/hooks/use-document-citation'

// Mate UI cycle: document sources as icons. One citation icon per rendered
// link needs to know a document's title/mime/kind (for its tooltip and which
// icon to show) and whether it still exists (an unknown/deleted id must be
// visibly marked, not silently hidden) - both from the SAME id already
// carried in the citation link, so this is a plain GET /api/documents/:id,
// not a second search. The module-level cache is what keeps repeated
// citations of the same document (across one reply, or across many) from
// each firing their own request - AGENTS.md's "no N+1 storm" concern, the
// same shape as the backend's own per-call folderPaths cache
// (assistant_tools.go's documentFolderPathString).

describe('useDocumentCitation', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    __resetDocumentCitationCacheForTests()
  })

  it('starts loading, then resolves to the document title/mime/kind', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ id: 'doc-1', title: 'Yanmar 4JH Service Manual', filename: 'yanmar.pdf', mime: 'application/pdf', kind: 'file' }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useDocumentCitation('doc-1'))

    expect(result.current.status).toBe('loading')

    await waitFor(() => expect(result.current.status).toBe('ok'))
    expect(result.current.title).toBe('Yanmar 4JH Service Manual')
    expect(result.current.mime).toBe('application/pdf')
    expect(result.current.kind).toBe('file')
  })

  it('falls back to the filename when the document has no title', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ id: 'doc-2', title: '', filename: 'receipt.pdf', mime: 'application/pdf', kind: 'file' }),
    }))

    const { result } = renderHook(() => useDocumentCitation('doc-2'))
    await waitFor(() => expect(result.current.status).toBe('ok'))
    expect(result.current.title).toBe('receipt.pdf')
  })

  it('resolves to not-found on a 404, without throwing', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 404, json: async () => ({ error: 'document not found' }) }))

    const { result } = renderHook(() => useDocumentCitation('missing-doc'))
    await waitFor(() => expect(result.current.status).toBe('not-found'))
  })

  it('resolves to error on a network failure, rather than hanging forever', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('Failed to fetch')))

    const { result } = renderHook(() => useDocumentCitation('doc-3'))
    await waitFor(() => expect(result.current.status).toBe('error'))
  })

  it('shares one fetch across every hook instance citing the same document id', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ id: 'doc-4', title: 'Shared', filename: 'shared.pdf', mime: 'application/pdf', kind: 'file' }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const first = renderHook(() => useDocumentCitation('doc-4'))
    const second = renderHook(() => useDocumentCitation('doc-4'))

    await waitFor(() => expect(first.result.current.status).toBe('ok'))
    await waitFor(() => expect(second.result.current.status).toBe('ok'))

    const calls = fetchMock.mock.calls.filter((call: unknown[]) => String(call[0]).includes('doc-4'))
    expect(calls).toHaveLength(1)
  })

  it('returns idle for a null id (the broken-id sentinel case), issuing no fetch', () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useDocumentCitation(null))
    expect(result.current.status).toBe('idle')
    expect(fetchMock).not.toHaveBeenCalled()
  })
})
