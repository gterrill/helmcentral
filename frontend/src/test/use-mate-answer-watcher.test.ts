import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'

import { useMateAnswerWatcher } from '@/hooks/use-mate-answer-watcher'
import { registerMateWatch, removeMateWatch, getMateWatchSnapshot } from '@/lib/mate-watch-store'

// mate-answer-toast plan (on top of ADR 0105's "the answer outlives the
// page"): this hook is mounted once in App.tsx, outside the kiosk path, and
// opens GET .../run for every conversation the watch store knows about that
// isn't the one currently on screen - see the hook's own doc comment for the
// full behaviour. Mocked the same way existing tests mock sonner (grep
// `vi.mock('sonner'`), with both the plain callable form (the "message"
// toast) and `.error` (the failure toast) since this hook uses both.
const { toastMock, toastErrorMock } = vi.hoisted(() => ({
  toastMock: vi.fn(),
  toastErrorMock: vi.fn(),
}))

vi.mock('sonner', () => ({
  toast: Object.assign(toastMock, { error: toastErrorMock }),
}))

function sseStream(chunks: string[]): ReadableStream<Uint8Array> {
  const encoder = new TextEncoder()
  return new ReadableStream({
    start(controller) {
      for (const chunk of chunks) controller.enqueue(encoder.encode(chunk))
      controller.close()
    },
  })
}

// A stream that never closes on its own and records whether it was
// cancelled - used to prove "becomes viewed mid-stream" actually aborts the
// in-flight fetch rather than just ignoring its eventual result.
function neverEndingStream(onCancel: () => void): ReadableStream<Uint8Array> {
  return new ReadableStream({
    start() {},
    cancel() { onCancel() },
  })
}

async function flushMicrotasks(times = 8) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

const conversationApi = {
  id: 'c1',
  title: 'Hook Reef anchorages',
  created_at: '2026-09-11T00:00:00Z',
  updated_at: '2026-09-11T00:00:01Z',
}

const messageApi = {
  id: 'm2',
  conversation_id: 'c1',
  seq: 2,
  role: 'assistant' as const,
  content: 'Blue Pearl Bay first.',
  created_at: '2026-09-11T00:05:00Z',
}

function clearWatchStore() {
  for (const entry of getMateWatchSnapshot()) removeMateWatch(entry.conversationId)
}

describe('useMateAnswerWatcher', () => {
  beforeEach(() => {
    clearWatchStore()
    toastMock.mockClear()
    toastErrorMock.mockClear()
  })

  afterEach(() => {
    clearWatchStore()
    vi.unstubAllGlobals()
  })

  it('opens GET .../run for a watched, unviewed conversation and toasts "Mate answered" with the title, and Open navigates', async () => {
    registerMateWatch('c1', 1000)
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      body: sseStream([`data: ${JSON.stringify({ message: messageApi, conversation: conversationApi })}\n\n`]),
    })
    vi.stubGlobal('fetch', fetchMock)
    const onOpen = vi.fn()

    renderHook(() => useMateAnswerWatcher(new Set(), onOpen))

    await waitFor(() => expect(toastMock).toHaveBeenCalledTimes(1))
    expect(toastMock).toHaveBeenCalledWith('Mate answered', expect.objectContaining({
      description: 'Hook Reef anchorages',
    }))
    expect(String(fetchMock.mock.calls[0][0])).toContain('/api/assistant/conversations/c1/run')

    const options = toastMock.mock.calls[0][1] as { action: { label: string; onClick: () => void } }
    expect(options.action.label).toBe('Open')
    options.action.onClick()
    expect(onOpen).toHaveBeenCalledWith('c1')

    // Handled: removed from the watch list.
    await waitFor(() => expect(getMateWatchSnapshot()).toEqual([]))
  })

  it('toasts "Mate couldn\'t answer" with the first line of the error, and the same Open action', async () => {
    registerMateWatch('c1', 1000)
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      body: sseStream(['event: error\ndata: {"error":"upstream 401: invalid API key\\nmore detail"}\n\n']),
    }))
    const onOpen = vi.fn()

    renderHook(() => useMateAnswerWatcher(new Set(), onOpen))

    await waitFor(() => expect(toastErrorMock).toHaveBeenCalledTimes(1))
    expect(toastErrorMock).toHaveBeenCalledWith("Mate couldn't answer", expect.objectContaining({
      description: 'upstream 401: invalid API key',
    }))

    const options = toastErrorMock.mock.calls[0][1] as { action: { label: string; onClick: () => void } }
    options.action.onClick()
    expect(onOpen).toHaveBeenCalledWith('c1')
    expect(toastMock).not.toHaveBeenCalled()
  })

  it('shows no toast for a "stopped" error - that is operator-initiated', async () => {
    registerMateWatch('c1', 1000)
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      body: sseStream(['event: error\ndata: {"error":"stopped"}\n\n']),
    }))

    renderHook(() => useMateAnswerWatcher(new Set(), vi.fn()))

    await waitFor(() => expect(getMateWatchSnapshot()).toEqual([]))
    expect(toastMock).not.toHaveBeenCalled()
    expect(toastErrorMock).not.toHaveBeenCalled()
  })

  it('204 with a newer assistant message toasts "Mate answered"', async () => {
    registerMateWatch('c1', new Date('2026-09-11T00:00:00Z').getTime())
    const fetchMock = vi.fn(async (url: string) => {
      if (url.includes('/run')) return { ok: true, status: 204 }
      return {
        ok: true,
        status: 200,
        json: async () => ({
          conversation: conversationApi,
          messages: [
            { id: 'm1', conversation_id: 'c1', seq: 1, role: 'user', created_at: '2026-09-11T00:00:00Z' },
            { id: 'm2', conversation_id: 'c1', seq: 2, role: 'assistant', created_at: '2026-09-11T00:05:00Z' },
          ],
        }),
      }
    })
    vi.stubGlobal('fetch', fetchMock)
    const onOpen = vi.fn()

    renderHook(() => useMateAnswerWatcher(new Set(), onOpen))

    await waitFor(() => expect(toastMock).toHaveBeenCalledTimes(1))
    expect(toastMock).toHaveBeenCalledWith('Mate answered', expect.objectContaining({
      description: 'Hook Reef anchorages',
    }))
  })

  it('204 with no newer message shows no toast', async () => {
    // The question was sent after the only assistant reply already on
    // record - nothing happened since this tab started watching.
    registerMateWatch('c1', new Date('2026-09-11T01:00:00Z').getTime())
    vi.stubGlobal('fetch', vi.fn(async (url: string) => {
      if (url.includes('/run')) return { ok: true, status: 204 }
      return {
        ok: true,
        status: 200,
        json: async () => ({
          conversation: conversationApi,
          messages: [
            { id: 'm1', conversation_id: 'c1', seq: 1, role: 'user', created_at: '2026-09-11T00:00:00Z' },
            { id: 'm2', conversation_id: 'c1', seq: 2, role: 'assistant', created_at: '2026-09-11T00:05:00Z' },
          ],
        }),
      }
    }))

    renderHook(() => useMateAnswerWatcher(new Set(), vi.fn()))

    await waitFor(() => expect(getMateWatchSnapshot()).toEqual([]))
    expect(toastMock).not.toHaveBeenCalled()
  })

  it('a conversation that is being viewed opens no stream and shows no toast', async () => {
    registerMateWatch('c1', 1000)
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 204 })
    vi.stubGlobal('fetch', fetchMock)

    renderHook(() => useMateAnswerWatcher(new Set(['c1']), vi.fn()))

    await flushMicrotasks()
    expect(fetchMock).not.toHaveBeenCalled()
    expect(toastMock).not.toHaveBeenCalled()
    expect(toastErrorMock).not.toHaveBeenCalled()
    // Still watched: nothing has happened to it yet, it's just not being
    // streamed while on screen.
    expect(getMateWatchSnapshot().some((e) => e.conversationId === 'c1')).toBe(true)
  })

  it('a conversation that becomes viewed mid-stream has its stream closed, with no toast', async () => {
    registerMateWatch('c1', 1000)
    let cancelled = false
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      body: neverEndingStream(() => { cancelled = true }),
    }))

    const { rerender } = renderHook(
      ({ viewed }: { viewed: ReadonlySet<string> }) => useMateAnswerWatcher(viewed, vi.fn()),
      { initialProps: { viewed: new Set<string>() } },
    )

    await flushMicrotasks()

    await act(async () => {
      rerender({ viewed: new Set(['c1']) })
      await flushMicrotasks()
    })

    expect(cancelled).toBe(true)
    expect(toastMock).not.toHaveBeenCalled()
    expect(toastErrorMock).not.toHaveBeenCalled()
    await waitFor(() => expect(getMateWatchSnapshot()).toEqual([]))
  })

  it('cleans up its own stream on unmount without a second watcher instance racing it', async () => {
    registerMateWatch('c1', 1000)
    let cancelled = false
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      body: neverEndingStream(() => { cancelled = true }),
    }))

    const { unmount } = renderHook(() => useMateAnswerWatcher(new Set(), vi.fn()))
    await flushMicrotasks()

    unmount()

    expect(cancelled).toBe(true)
  })

  it('does not open a second stream for a conversation already being watched (StrictMode double mount)', async () => {
    registerMateWatch('c1', 1000)
    // A real fetch() hands back a fresh Response (and body stream) on every
    // call - mockResolvedValue would instead reuse one ReadableStream
    // across both of StrictMode's mount/cleanup/mount fetch() calls, which
    // throws "already locked" on the second getReader() and masks the
    // thing this test actually checks.
    const fetchMock = vi.fn().mockImplementation(async () => ({
      ok: true,
      status: 200,
      body: sseStream([`data: ${JSON.stringify({ message: messageApi, conversation: conversationApi })}\n\n`]),
    }))
    vi.stubGlobal('fetch', fetchMock)

    renderHook(() => useMateAnswerWatcher(new Set(), vi.fn()), { reactStrictMode: true })

    await waitFor(() => expect(toastMock).toHaveBeenCalledTimes(1))
    // Exactly one toast even though StrictMode's mount/cleanup/mount replay
    // may have issued the request twice.
    expect(toastMock).toHaveBeenCalledTimes(1)
  })

  it('opens no stream and shows no toast while disabled (App.tsx\'s kiosk gate)', async () => {
    registerMateWatch('c1', 1000)
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 204 })
    vi.stubGlobal('fetch', fetchMock)

    renderHook(() => useMateAnswerWatcher(new Set(), vi.fn(), false))

    await flushMicrotasks()
    expect(fetchMock).not.toHaveBeenCalled()
    expect(toastMock).not.toHaveBeenCalled()
  })

  // Extra correctness check beyond the required list above (not itself in
  // the spec's test list, but implied by "If the operator is viewing it
  // when the answer lands... just drop it from the list"): a conversation
  // that's viewed continuously until its answer finishes has no stream
  // opened for it at all, so leaving afterward must not treat the already-
  // seen answer as newly missed.
  it('does not toast for an answer that already arrived while viewed, once the operator later leaves', async () => {
    registerMateWatch('c1', new Date('2026-09-11T00:00:00Z').getTime())
    const fetchMock = vi.fn(async (url: string) => {
      if (url.includes('/run')) return { ok: true, status: 204 }
      return {
        ok: true,
        status: 200,
        json: async () => ({
          conversation: conversationApi,
          // The reply arrived at 00:05, well after the question was sent -
          // it would read as "newer" against the original send time.
          messages: [
            { id: 'm1', conversation_id: 'c1', seq: 1, role: 'user', created_at: '2026-09-11T00:00:00Z' },
            { id: 'm2', conversation_id: 'c1', seq: 2, role: 'assistant', created_at: '2026-09-11T00:05:00Z' },
          ],
        }),
      }
    })
    vi.stubGlobal('fetch', fetchMock)

    const { rerender } = renderHook(
      ({ viewed }: { viewed: ReadonlySet<string> }) => useMateAnswerWatcher(viewed, vi.fn()),
      { initialProps: { viewed: new Set<string>(['c1']) } },
    )
    await flushMicrotasks()
    expect(fetchMock).not.toHaveBeenCalled()

    // The operator leaves well after the reply would have finished.
    await act(async () => {
      rerender({ viewed: new Set<string>() })
      await flushMicrotasks()
    })

    await waitFor(() => expect(getMateWatchSnapshot()).toEqual([]))
    expect(toastMock).not.toHaveBeenCalled()
  })
})
