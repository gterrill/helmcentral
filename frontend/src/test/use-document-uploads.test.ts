import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'

import { useDocumentUploads } from '@/hooks/use-document-uploads'

// FakeXHR stands in for the real XMLHttpRequest (ADR 0106 F2): the hook
// uses XHR rather than fetch specifically so upload progress is real
// (fetch can't report it), so these tests drive that transport directly
// rather than mocking fetch, mirroring use-radar-echo-stream.test.ts's
// FakeWebSocket for the same reason - happy-dom's XMLHttpRequest would
// otherwise attempt a real network call.
class FakeXHR {
  static instances: FakeXHR[] = []

  method = ''
  url = ''
  status = 0
  responseText = ''
  aborted = false
  body: FormData | null = null
  upload: { onprogress: ((e: { lengthComputable: boolean; loaded: number; total: number }) => void) | null } = {
    onprogress: null,
  }
  onload: (() => void) | null = null
  onerror: (() => void) | null = null

  constructor() {
    FakeXHR.instances.push(this)
  }

  open(method: string, url: string) {
    this.method = method
    this.url = url
  }

  send(body: FormData) {
    this.body = body
  }

  abort() {
    this.aborted = true
  }

  emitProgress(loaded: number, total: number) {
    this.upload.onprogress?.({ lengthComputable: true, loaded, total })
  }

  emitLoad(status: number, body: unknown) {
    this.status = status
    this.responseText = JSON.stringify(body)
    this.onload?.()
  }

  emitError() {
    this.onerror?.()
  }
}

function documentPayload(overrides: Record<string, unknown> = {}) {
  return {
    id: 'doc-1',
    filename: 'manual.pdf',
    mime: 'application/pdf',
    size_bytes: 1024,
    status: 'pending',
    stage: 'extract',
    tags: [],
    ...overrides,
  }
}

beforeEach(() => {
  FakeXHR.instances = []
  vi.useFakeTimers()
  vi.stubGlobal('XMLHttpRequest', FakeXHR)
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('useDocumentUploads', () => {
  it('uploads a file and moves it from uploading to pending to indexed via polling', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => documentPayload({ status: 'indexed', stage: 'done' }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useDocumentUploads())

    act(() => {
      result.current.add([new File(['hello'], 'manual.pdf', { type: 'application/pdf' })])
    })

    expect(result.current.items).toHaveLength(1)
    expect(result.current.items[0]).toMatchObject({ status: 'uploading', filename: 'manual.pdf', progress: 0 })
    expect(result.current.ready).toBe(false)

    const xhr = FakeXHR.instances[0]
    expect(xhr.url).toBe('/api/documents')
    act(() => xhr.emitProgress(50, 100))
    expect(result.current.items[0].progress).toBe(50)

    act(() => xhr.emitLoad(201, { document: documentPayload(), duplicate: false }))

    expect(result.current.items[0]).toMatchObject({ status: 'pending', documentId: 'doc-1', duplicate: false })
    expect(result.current.ready).toBe(false)

    // The poll timer only starts once something is pending.
    // advanceTimersByTimeAsync flushes the microtasks the fetch/json/state
    // update chain creates in between firing the timer, so the update is
    // already applied once this resolves - no separate waitFor needed (and
    // waitFor's own internal setTimeout polling would never fire under fake
    // timers without yet another manual advance).
    await act(async () => {
      await vi.advanceTimersByTimeAsync(3000)
    })
    expect(fetchMock).toHaveBeenCalledWith('/api/documents/doc-1')
    expect(result.current.items[0].status).toBe('indexed')
    expect(result.current.ready).toBe(true)
  })

  it('reuses the existing document on a duplicate response without polling', async () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useDocumentUploads())

    act(() => {
      result.current.add([new File(['hello'], 'manual.pdf', { type: 'application/pdf' })])
    })

    const xhr = FakeXHR.instances[0]
    act(() => xhr.emitLoad(200, { document: documentPayload({ status: 'indexed', stage: 'done' }), duplicate: true }))

    expect(result.current.items[0]).toMatchObject({
      status: 'indexed',
      documentId: 'doc-1',
      duplicate: true,
    })
    expect(result.current.ready).toBe(true)

    // Already indexed - never becomes pending, so no poll is needed.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000)
    })
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('rejects a file over the 100 MB limit before uploading, with no chip staged', () => {
    const { result } = renderHook(() => useDocumentUploads())
    const bigFile = new File(['x'], 'huge.pdf', { type: 'application/pdf' })
    Object.defineProperty(bigFile, 'size', { value: 101 * 1024 * 1024 })

    act(() => {
      result.current.add([bigFile])
    })

    expect(result.current.items).toHaveLength(0)
    expect(FakeXHR.instances).toHaveLength(0)
    expect(result.current.error).toMatch(/100 MB/)
  })

  it('rejects an empty file before uploading, with no chip staged', () => {
    const { result } = renderHook(() => useDocumentUploads())
    const emptyFile = new File([], 'empty.txt', { type: 'text/plain' })

    act(() => {
      result.current.add([emptyFile])
    })

    expect(result.current.items).toHaveLength(0)
    expect(FakeXHR.instances).toHaveLength(0)
    expect(result.current.error).toMatch(/empty/)
  })

  it('shows the server error text and drops the chip when the upload itself fails', () => {
    const { result } = renderHook(() => useDocumentUploads())

    act(() => {
      result.current.add([new File(['hello'], 'manual.pdf', { type: 'application/pdf' })])
    })
    expect(result.current.items).toHaveLength(1)

    const xhr = FakeXHR.instances[0]
    act(() => xhr.emitLoad(413, { error: 'upload exceeds the 104857600 byte limit' }))

    expect(result.current.items).toHaveLength(0)
    expect(result.current.error).toBe('upload exceeds the 104857600 byte limit')
  })

  it('refuses to stage more than 10 files at once', () => {
    const { result } = renderHook(() => useDocumentUploads())
    const files = Array.from({ length: 11 }, (_, i) => new File(['x'], `file-${i}.txt`, { type: 'text/plain' }))

    act(() => {
      result.current.add(files)
    })

    expect(result.current.items).toHaveLength(0)
    expect(FakeXHR.instances).toHaveLength(0)
    expect(result.current.error).toMatch(/at most 10/i)
  })

  it('refuses to stage a batch that would push the total over 10', () => {
    const { result } = renderHook(() => useDocumentUploads())
    const first = Array.from({ length: 9 }, (_, i) => new File(['x'], `file-${i}.txt`, { type: 'text/plain' }))

    act(() => {
      result.current.add(first)
    })
    expect(result.current.items).toHaveLength(9)

    act(() => {
      result.current.add([
        new File(['x'], 'a.txt', { type: 'text/plain' }),
        new File(['x'], 'b.txt', { type: 'text/plain' }),
      ])
    })

    // 9 + 2 > 10: the whole second batch is refused, not just the overflow.
    expect(result.current.items).toHaveLength(9)
    expect(result.current.error).toMatch(/at most 10/i)
  })

  it('removes a single staged item and aborts its upload if still in flight', () => {
    const { result } = renderHook(() => useDocumentUploads())

    act(() => {
      result.current.add([new File(['hello'], 'manual.pdf', { type: 'application/pdf' })])
    })
    const key = result.current.items[0].key
    const xhr = FakeXHR.instances[0]

    act(() => {
      result.current.remove(key)
    })

    expect(result.current.items).toHaveLength(0)
    expect(xhr.aborted).toBe(true)
  })

  it('clears every staged item and aborts anything still uploading', () => {
    const { result } = renderHook(() => useDocumentUploads())

    act(() => {
      result.current.add([
        new File(['hello'], 'a.pdf', { type: 'application/pdf' }),
        new File(['hello'], 'b.pdf', { type: 'application/pdf' }),
      ])
    })
    expect(result.current.items).toHaveLength(2)

    act(() => {
      result.current.clear()
    })

    expect(result.current.items).toHaveLength(0)
    expect(FakeXHR.instances.every((x) => x.aborted)).toBe(true)
  })

  it('stops the poll timer once nothing is pending', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => documentPayload({ status: 'indexed', stage: 'done' }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result, unmount } = renderHook(() => useDocumentUploads())

    act(() => {
      result.current.add([new File(['hello'], 'manual.pdf', { type: 'application/pdf' })])
    })
    const xhr = FakeXHR.instances[0]
    act(() => xhr.emitLoad(201, { document: documentPayload(), duplicate: false }))

    await act(async () => {
      await vi.advanceTimersByTimeAsync(3000)
    })
    expect(result.current.items[0].status).toBe('indexed')

    fetchMock.mockClear()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(9000)
    })
    // Nothing pending any more - the interval must have been cleared, not
    // just skipped, so a later render/unmount has nothing left running.
    expect(fetchMock).not.toHaveBeenCalled()
    expect(vi.getTimerCount()).toBe(0)

    unmount()
  })

  it('marks a failed document as attachable, not staged-forever-uploading', async () => {
    const { result } = renderHook(() => useDocumentUploads())

    act(() => {
      result.current.add([new File(['hello'], 'broken.pdf', { type: 'application/pdf' })])
    })
    const xhr = FakeXHR.instances[0]
    act(() =>
      xhr.emitLoad(201, {
        document: documentPayload({ status: 'failed', stage: 'extract', error: 'could not read PDF' }),
        duplicate: false,
      }),
    )

    expect(result.current.items[0]).toMatchObject({
      status: 'failed',
      documentId: 'doc-1',
      error: 'could not read PDF',
    })
    expect(result.current.ready).toBe(true)
  })

  // A poll that fails must not strand the chip `pending` forever - that
  // holds `ready` false, disables Send, and shows only "Waiting for
  // attachments to finish reading…" with no way out short of removing the
  // chip. A failed poll marks the document `failed` instead, same as a
  // failed extract/enrich, so it's attachable (or removable) again.
  it('marks the chip failed with the server error text on a 500 poll response', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: false,
      status: 500,
      text: async () => JSON.stringify({ error: 'indexer crashed' }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useDocumentUploads())

    act(() => {
      result.current.add([new File(['hello'], 'manual.pdf', { type: 'application/pdf' })])
    })
    const xhr = FakeXHR.instances[0]
    act(() => xhr.emitLoad(201, { document: documentPayload(), duplicate: false }))
    expect(result.current.ready).toBe(false)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(3000)
    })

    expect(result.current.items[0]).toMatchObject({ status: 'failed', documentId: 'doc-1', error: 'indexer crashed' })
    expect(result.current.ready).toBe(true)
  })

  it('marks the chip failed with a library message on a 404 poll response', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: false,
      status: 404,
      text: async () => JSON.stringify({ error: 'document not found' }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useDocumentUploads())

    act(() => {
      result.current.add([new File(['hello'], 'manual.pdf', { type: 'application/pdf' })])
    })
    const xhr = FakeXHR.instances[0]
    act(() => xhr.emitLoad(201, { document: documentPayload(), duplicate: false }))

    await act(async () => {
      await vi.advanceTimersByTimeAsync(3000)
    })

    expect(result.current.items[0]).toMatchObject({
      status: 'failed',
      error: 'This document is no longer in the library.',
    })
    expect(result.current.ready).toBe(true)
  })

  it('marks the chip failed with the network error message when the poll fetch rejects, with no unhandled rejection', async () => {
    const unhandled: unknown[] = []
    const onUnhandledRejection = (reason: unknown) => {
      unhandled.push(reason)
    }
    process.on('unhandledRejection', onUnhandledRejection)

    const fetchMock = vi.fn().mockRejectedValue(new Error('network dropped'))
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useDocumentUploads())

    act(() => {
      result.current.add([new File(['hello'], 'manual.pdf', { type: 'application/pdf' })])
    })
    const xhr = FakeXHR.instances[0]
    act(() => xhr.emitLoad(201, { document: documentPayload(), duplicate: false }))

    await act(async () => {
      await vi.advanceTimersByTimeAsync(3000)
    })

    expect(result.current.items[0]).toMatchObject({ status: 'failed', error: 'network dropped' })
    expect(result.current.ready).toBe(true)

    // Give any stray rejection a turn to surface before asserting none did.
    await act(async () => {
      await Promise.resolve()
    })
    process.off('unhandledRejection', onUnhandledRejection)
    expect(unhandled).toHaveLength(0)
  })

  // ADR 0106 F2 follow-up: uploads dedupe by sha256 server-side, so two
  // chips can resolve to the same document id (the same file attached
  // twice, or two files with identical content). The hook already records
  // `duplicate` on the response; this pins that a document id already
  // staged never becomes a second chip.
  it('collapses a second upload that resolves to an already-staged document id, with a readable message', () => {
    const { result } = renderHook(() => useDocumentUploads())

    act(() => {
      result.current.add([new File(['hello'], 'manual.pdf', { type: 'application/pdf' })])
    })
    const first = FakeXHR.instances[0]
    act(() => first.emitLoad(201, { document: documentPayload(), duplicate: false }))
    expect(result.current.items).toHaveLength(1)

    act(() => {
      result.current.add([new File(['hello'], 'manual.pdf', { type: 'application/pdf' })])
    })
    const second = FakeXHR.instances[1]
    act(() => second.emitLoad(200, { document: documentPayload(), duplicate: true }))

    expect(result.current.items).toHaveLength(1)
    expect(result.current.items[0].documentId).toBe('doc-1')
    expect(result.current.error).toBe('"manual.pdf" is already attached.')
  })
})
