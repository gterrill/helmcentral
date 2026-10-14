import { describe, it, expect, afterEach, vi } from 'vitest'
import { renderHook, waitFor, act } from '@testing-library/react'

import { useDocumentCitation } from '@/hooks/use-document-citation'
import { refetchQuery } from '@/lib/api-query'
import { stubApi, jsonResponse } from '@/test/helpers/stub-api'

// Mate UI cycle: document sources as icons. One citation icon per rendered
// link needs to know a document's title/mime/kind (for its tooltip and which
// icon to show) and whether it still exists (an unknown/deleted id must be
// visibly marked, not silently hidden) - both from the SAME id already
// carried in the citation link, so this is a plain GET /api/documents/:id,
// not a second search. The query store is what keeps repeated
// citations of the same document (across one reply, or across many) from
// each firing their own request - AGENTS.md's "no N+1 storm" concern, the
// same shape as the backend's own per-call folderPaths cache
// (assistant_tools.go's documentFolderPathString).

describe('useDocumentCitation', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
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

  // Follow-up fix: an 'error' result (a dropped link, a 500) used to be
  // cached for the life of the page the same as 'ok'/'not-found' - one
  // transient blip left the icon permanently broken until a full reload,
  // even though the document is fine and a retry moments later would
  // succeed. Only 'ok' and 'not-found' are worth caching (a real answer, or
  // a real "no such document" that isn't going to change); an 'error'
  // result is evicted so the next mount gets a fresh attempt.
  it('evicts an error result so a later remount retries, after a rejected fetch', async () => {
    const fetchMock = vi.fn()
      .mockRejectedValueOnce(new TypeError('Failed to fetch'))
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({ id: 'doc-5', title: 'Engine manual', filename: 'engine.pdf', mime: 'application/pdf', kind: 'file' }),
      })
    vi.stubGlobal('fetch', fetchMock)

    const first = renderHook(() => useDocumentCitation('doc-5'))
    await waitFor(() => expect(first.result.current.status).toBe('error'))
    first.unmount()

    const second = renderHook(() => useDocumentCitation('doc-5'))
    await waitFor(() => expect(second.result.current.status).toBe('ok'))
    expect(second.result.current.title).toBe('Engine manual')

    const calls = fetchMock.mock.calls.filter((call: unknown[]) => String(call[0]).includes('doc-5'))
    expect(calls).toHaveLength(2)
  })

  it('evicts an error result so a later remount retries, after a 500 response', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: false, status: 500, json: async () => ({ error: 'internal error' }) })
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({ id: 'doc-6', title: 'Fuel log', filename: 'fuel.csv', mime: 'text/csv', kind: 'file' }),
      })
    vi.stubGlobal('fetch', fetchMock)

    const first = renderHook(() => useDocumentCitation('doc-6'))
    await waitFor(() => expect(first.result.current.status).toBe('error'))
    first.unmount()

    const second = renderHook(() => useDocumentCitation('doc-6'))
    await waitFor(() => expect(second.result.current.status).toBe('ok'))
    expect(second.result.current.title).toBe('Fuel log')

    const calls = fetchMock.mock.calls.filter((call: unknown[]) => String(call[0]).includes('doc-6'))
    expect(calls).toHaveLength(2)
  })

  // A confirmed 404 shows as not-found, and the next mount looks again: the
  // store revalidates on mount, so a document restored since is picked up.
  it('shows a confirmed not-found, and looks again on the next mount', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: false, status: 404, json: async () => ({ error: 'document not found' }) })
    vi.stubGlobal('fetch', fetchMock)

    const first = renderHook(() => useDocumentCitation('doc-7'))
    await waitFor(() => expect(first.result.current.status).toBe('not-found'))
    first.unmount()

    const second = renderHook(() => useDocumentCitation('doc-7'))
    await waitFor(() => expect(second.result.current.status).toBe('not-found'))

    const calls = fetchMock.mock.calls.filter((call: unknown[]) => String(call[0]).includes('doc-7'))
    expect(calls).toHaveLength(2)
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

  it('keeps a resolved document ok when another reader revalidation fails', async () => {
    let calls = 0
    stubApi({
      'GET /api/documents/doc-8': () => (++calls === 1
        ? { id: 'doc-8', title: 'Manual', filename: 'm.pdf', mime: 'application/pdf', kind: 'file' }
        : jsonResponse({ error: 'bad gateway' }, 502)),
    })
    const { result } = renderHook(() => useDocumentCitation('doc-8'))
    await waitFor(() => expect(result.current.status).toBe('ok'))
    await act(async () => { await refetchQuery(['/api/documents/doc-8'], '/api/documents/doc-8') })
    expect(calls).toBe(2)
    expect(result.current.status).toBe('ok')
    expect(result.current.title).toBe('Manual')
  })

  it('does not request a document again for an icon mounted after it resolved', async () => {
    const fetchMock = stubApi({
      'GET /api/documents/doc-9': { id: 'doc-9', title: 'Log', filename: 'l.csv', mime: 'text/csv', kind: 'file' },
    })
    const first = renderHook(() => useDocumentCitation('doc-9'))
    await waitFor(() => expect(first.result.current.status).toBe('ok'))
    const second = renderHook(() => useDocumentCitation('doc-9'))
    expect(second.result.current.status).toBe('ok')
    expect(second.result.current.title).toBe('Log')
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('returns idle for a null id (the broken-id sentinel case), issuing no fetch', () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useDocumentCitation(null))
    expect(result.current.status).toBe('idle')
    expect(fetchMock).not.toHaveBeenCalled()
  })
})
