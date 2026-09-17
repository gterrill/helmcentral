import { describe, it, expect, afterEach, beforeEach, vi } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useAssistantChat } from '@/hooks/use-assistant-chat'
import type { AssistantMessage } from '@/hooks/use-assistant-conversations'
import { getMateWatchSnapshot, removeMateWatch } from '@/lib/mate-watch-store'

// ADR 0093: send() posts one message and reads the reply back as SSE
// (status/message/error frames) over the response body's ReadableStream,
// since the browser's EventSource can't POST.

function sseStream(chunks: string[]): ReadableStream<Uint8Array> {
  const encoder = new TextEncoder()
  return new ReadableStream({
    start(controller) {
      for (const chunk of chunks) controller.enqueue(encoder.encode(chunk))
      controller.close()
    },
  })
}

// A stream this test pushes chunks into by hand, so the "statusText updates
// before the reply resolves" assertion doesn't race a real setTimeout/poll
// against however many microtask hops the reader happens to need - every
// hop here is awaited explicitly instead.
function controllableSseStream() {
  let streamController!: ReadableStreamDefaultController<Uint8Array>
  const stream = new ReadableStream<Uint8Array>({
    start(controller) { streamController = controller },
  })
  const encoder = new TextEncoder()
  return {
    stream,
    push: (chunk: string) => streamController.enqueue(encoder.encode(chunk)),
    close: () => streamController.close(),
  }
}

async function flushMicrotasks(times = 8) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

// ADR 0105: delta text is buffered in a ref and flushed to `draft` state at
// most once per requestAnimationFrame, so markdown isn't re-parsed on every
// token. happy-dom's rAF fires on a real timer, which would make these tests
// slow and non-deterministic, so every test that touches `draft` installs
// this fake instead (the same pattern anchor-watch-map-radar-echo.test.tsx
// uses): it records the queued callback rather than ever calling it, and
// `flush()` runs it synchronously on demand.
function installFakeRaf() {
  let queued: FrameRequestCallback | null = null
  let nextId = 1
  vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
    queued = cb
    return nextId++
  })
  vi.stubGlobal('cancelAnimationFrame', () => {
    queued = null
  })
  return {
    flush: () => {
      const cb = queued
      queued = null
      cb?.(0)
    },
    pending: () => queued !== null,
  }
}

// A stream that never closes on its own - the caller must abort it. Used to
// prove a second send() cancels a first one still in flight.
function neverEndingStream(onCancel: () => void): ReadableStream<Uint8Array> {
  const encoder = new TextEncoder()
  return new ReadableStream({
    start(controller) {
      controller.enqueue(encoder.encode('event: status\ndata: {"text":"first request working"}\n\n'))
    },
    cancel() {
      onCancel()
    },
  })
}

const messageApi = {
  id: 'm2',
  conversation_id: 'c1',
  seq: 2,
  role: 'assistant' as const,
  content: 'Blue Pearl Bay first.',
  model: 'anthropic/claude-sonnet-4.5',
  prompt_tokens: 1000,
  completion_tokens: 200,
  cost_usd: 0.0184,
  tool_rounds: 2,
  created_at: '2026-09-11T00:00:00Z',
}

const conversationApi = {
  id: 'c1',
  title: 'Hook Reef anchorages',
  created_at: '2026-09-11T00:00:00Z',
  updated_at: '2026-09-11T00:00:01Z',
}

describe('useAssistantChat', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('surfaces the server error text on a 503 and resolves null', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: false,
      status: 503,
      json: async () => ({ error: 'Assistant is not configured. Set it up in Settings → Assistant.' }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useAssistantChat())

    let reply: unknown
    await act(async () => {
      reply = await result.current.send('c1', 'hello')
    })

    expect(reply).toBeNull()
    expect(result.current.error).toBe('Assistant is not configured. Set it up in Settings → Assistant.')
    expect(result.current.sending).toBe(false)
  })

  it('falls back to HTTP <status> when a non-2xx body is not JSON', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: false,
      status: 500,
      json: async () => { throw new Error('not json') },
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useAssistantChat())

    let reply: unknown
    await act(async () => {
      reply = await result.current.send('c1', 'hello')
    })

    expect(reply).toBeNull()
    expect(result.current.error).toBe('HTTP 500')
  })

  it('updates statusText from status frames then resolves with the mapped message', async () => {
    const { stream, push, close } = controllableSseStream()
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, body: stream })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useAssistantChat())

    let sendPromise!: Promise<AssistantMessage | null>
    act(() => {
      sendPromise = result.current.send('c1', 'Tongue Bay or Blue Pearl Bay?')
    })

    await act(async () => {
      push('event: status\ndata: {"text":"Looking up Tongue Bay…"}\n\n')
      await flushMicrotasks()
    })

    expect(result.current.statusText).toBe('Looking up Tongue Bay…')

    await act(async () => {
      push(`data: ${JSON.stringify({ message: messageApi, conversation: conversationApi })}\n\n`)
      close()
      await flushMicrotasks()
    })

    const reply = await sendPromise
    expect(reply).toEqual({
      id: 'm2',
      conversationId: 'c1',
      seq: 2,
      role: 'assistant',
      content: 'Blue Pearl Bay first.',
      model: 'anthropic/claude-sonnet-4.5',
      promptTokens: 1000,
      completionTokens: 200,
      costUsd: 0.0184,
      toolRounds: 2,
      createdAt: '2026-09-11T00:00:00Z',
    })
    // statusText clears once the send settles.
    expect(result.current.statusText).toBeNull()
    expect(result.current.sending).toBe(false)
  })

  it('sets error and resolves null on an error frame', async () => {
    const body = sseStream(['event: error\ndata: {"error":"upstream 401: invalid API key"}\n\n'])
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, body })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useAssistantChat())

    let reply: unknown
    await act(async () => {
      reply = await result.current.send('c1', 'hello')
    })

    expect(reply).toBeNull()
    expect(result.current.error).toBe('upstream 401: invalid API key')
  })

  it('reports "streaming not supported" when the response has no body', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, body: null })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useAssistantChat())

    let reply: unknown
    await act(async () => {
      reply = await result.current.send('c1', 'hello')
    })

    expect(reply).toBeNull()
    expect(result.current.error).toBe('streaming not supported')
  })

  it('aborts an in-flight send when a second send starts', async () => {
    let cancelled = false
    const firstBody = neverEndingStream(() => { cancelled = true })
    const secondBody = sseStream([`data: ${JSON.stringify({ message: messageApi, conversation: conversationApi })}\n\n`])

    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, body: firstBody })
      .mockResolvedValueOnce({ ok: true, body: secondBody })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useAssistantChat())

    let firstReply: unknown = 'not yet resolved'
    let secondReply: unknown = 'not yet resolved'

    await act(async () => {
      const firstPromise = result.current.send('c1', 'first question').then((r) => { firstReply = r })
      // Give the first request's fetch a tick to be issued before starting the second.
      await Promise.resolve()
      const secondPromise = result.current.send('c1', 'second question').then((r) => { secondReply = r })
      await Promise.all([firstPromise, secondPromise])
    })

    expect(cancelled).toBe(true)
    expect(firstReply).toBeNull()
    expect(secondReply).toMatchObject({ id: 'm2' })

    // The first request's own signal should have been the one aborted.
    const firstSignal = fetchMock.mock.calls[0][1]?.signal as AbortSignal
    expect(firstSignal.aborted).toBe(true)
  })

  // ADR 0093 voice phase: `spoken` and `screen` ride along in the POST body
  // only when the caller actually passes them - an ordinary panel send (no
  // options) must not grow a `spoken: false` or `screen: undefined` key the
  // backend was never asked for.
  it('includes spoken and screen in the request body when given', async () => {
    const body = sseStream([`data: ${JSON.stringify({ message: messageApi, conversation: conversationApi })}\n\n`])
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, body })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useAssistantChat())

    await act(async () => {
      await result.current.send('c1', 'How does tomorrow look?', {
        spoken: true,
        screen: { panel: 'forecast' },
      })
    })

    const sentBody = JSON.parse(fetchMock.mock.calls[0][1]?.body as string)
    expect(sentBody).toEqual({ content: 'How does tomorrow look?', spoken: true, screen: { panel: 'forecast' } })
  })

  it('omits spoken and screen from the request body when no options are given', async () => {
    const body = sseStream([`data: ${JSON.stringify({ message: messageApi, conversation: conversationApi })}\n\n`])
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, body })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useAssistantChat())

    await act(async () => {
      await result.current.send('c1', 'hello')
    })

    const sentBody = JSON.parse(fetchMock.mock.calls[0][1]?.body as string)
    expect(sentBody).toEqual({ content: 'hello' })
  })

  // ADR 0105: delta/retract/message/error frames drive `draft`, rAF-batched.
  describe('draft (ADR 0105 token streaming)', () => {
    it('accumulates delta text into draft, flushed at most once per animation frame', async () => {
      const raf = installFakeRaf()
      const { stream, push, close } = controllableSseStream()
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, body: stream }))

      const { result } = renderHook(() => useAssistantChat())

      let sendPromise!: Promise<unknown>
      act(() => {
        sendPromise = result.current.send('c1', 'A bowline?')
      })

      await act(async () => {
        push('event: delta\ndata: {"text":"A bowline"}\n\n')
        await flushMicrotasks()
      })
      // Buffered in the ref, not yet flushed to state.
      expect(result.current.draft).toBeNull()
      expect(raf.pending()).toBe(true)

      act(() => raf.flush())
      expect(result.current.draft).toBe('A bowline')

      await act(async () => {
        push('event: delta\ndata: {"text":" is a fixed loop."}\n\n')
        await flushMicrotasks()
      })
      // A second delta before the next frame stays buffered - still one flush.
      expect(result.current.draft).toBe('A bowline')

      act(() => raf.flush())
      expect(result.current.draft).toBe('A bowline is a fixed loop.')

      await act(async () => {
        push(`data: ${JSON.stringify({ message: messageApi, conversation: conversationApi })}\n\n`)
        close()
        await flushMicrotasks()
      })
      await sendPromise
      expect(result.current.draft).toBeNull()
    })

    it('retract clears the draft and cancels a pending flush', async () => {
      const raf = installFakeRaf()
      const { stream, push, close } = controllableSseStream()
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, body: stream }))

      const { result } = renderHook(() => useAssistantChat())

      let sendPromise!: Promise<unknown>
      act(() => {
        sendPromise = result.current.send('c1', 'Find Cairns')
      })

      await act(async () => {
        push('event: delta\ndata: {"text":"Let me check that"}\n\n')
        await flushMicrotasks()
      })
      expect(raf.pending()).toBe(true)

      await act(async () => {
        push('event: retract\ndata: {}\n\n')
        await flushMicrotasks()
      })
      expect(result.current.draft).toBeNull()
      expect(raf.pending()).toBe(false)

      await act(async () => {
        push(`data: ${JSON.stringify({ message: messageApi, conversation: conversationApi })}\n\n`)
        close()
        await flushMicrotasks()
      })
      await sendPromise
      expect(result.current.draft).toBeNull()
    })

    it('message clears the draft even when a flush was still pending', async () => {
      const raf = installFakeRaf()
      const { stream, push, close } = controllableSseStream()
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, body: stream }))

      const { result } = renderHook(() => useAssistantChat())

      let sendPromise!: Promise<unknown>
      act(() => {
        sendPromise = result.current.send('c1', 'hello')
      })

      await act(async () => {
        push('event: delta\ndata: {"text":"partial"}\n\n')
        await flushMicrotasks()
      })
      expect(raf.pending()).toBe(true)

      await act(async () => {
        push(`data: ${JSON.stringify({ message: messageApi, conversation: conversationApi })}\n\n`)
        close()
        await flushMicrotasks()
      })
      const reply = await sendPromise
      expect(reply).toMatchObject({ id: 'm2' })
      expect(result.current.draft).toBeNull()
      expect(raf.pending()).toBe(false)
    })

    it('an error frame clears the draft', async () => {
      const raf = installFakeRaf()
      const { stream, push, close } = controllableSseStream()
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, body: stream }))

      const { result } = renderHook(() => useAssistantChat())

      act(() => {
        void result.current.send('c1', 'hello')
      })

      await act(async () => {
        push('event: delta\ndata: {"text":"partial"}\n\n')
        await flushMicrotasks()
      })
      act(() => raf.flush())
      expect(result.current.draft).toBe('partial')

      await act(async () => {
        push('event: error\ndata: {"error":"upstream 401"}\n\n')
        close()
        await flushMicrotasks()
      })
      expect(result.current.error).toBe('upstream 401')
      expect(result.current.draft).toBeNull()
    })

    it('abort() clears the draft and any pending flush', async () => {
      const raf = installFakeRaf()
      const { stream, push } = controllableSseStream()
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, body: stream }))

      const { result } = renderHook(() => useAssistantChat())

      act(() => {
        void result.current.send('c1', 'hello')
      })

      await act(async () => {
        push('event: delta\ndata: {"text":"partial"}\n\n')
        await flushMicrotasks()
      })
      act(() => raf.flush())
      expect(result.current.draft).toBe('partial')

      act(() => { void result.current.abort() })
      expect(result.current.draft).toBeNull()
      expect(raf.pending()).toBe(false)
    })

    it('ignores unknown event types and still resolves the message', async () => {
      installFakeRaf()
      const { stream, push, close } = controllableSseStream()
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, body: stream }))

      const { result } = renderHook(() => useAssistantChat())

      let sendPromise!: Promise<unknown>
      act(() => {
        sendPromise = result.current.send('c1', 'hello')
      })

      await act(async () => {
        push('event: ping\ndata: {"whatever":true}\n\n')
        await flushMicrotasks()
      })
      expect(result.current.draft).toBeNull()
      expect(result.current.error).toBeNull()

      await act(async () => {
        push(`data: ${JSON.stringify({ message: messageApi, conversation: conversationApi })}\n\n`)
        close()
        await flushMicrotasks()
      })
      const reply = await sendPromise
      expect(reply).toMatchObject({ id: 'm2' })
    })
  })

  // ADR 0105 ("the answer outlives the page"): closing this tab's own view
  // of a reply must never be confused with actually stopping it server-side.
  describe('detach vs. Stop (ADR 0105)', () => {
    it('does not call /run/cancel on unmount - only Stop does that', () => {
      const { stream } = controllableSseStream()
      const fetchMock = vi.fn().mockResolvedValue({ ok: true, body: stream })
      vi.stubGlobal('fetch', fetchMock)

      const { result, unmount } = renderHook(() => useAssistantChat())

      act(() => {
        void result.current.send('c1', 'hello')
      })

      unmount()

      const cancelCalls = fetchMock.mock.calls.filter(([url]) => String(url).includes('/run/cancel'))
      expect(cancelCalls).toHaveLength(0)
    })

    it('does not call /run/cancel when a second send supersedes the first', async () => {
      const first = controllableSseStream()
      const second = controllableSseStream()
      const fetchMock = vi.fn()
        .mockResolvedValueOnce({ ok: true, body: first.stream })
        .mockResolvedValueOnce({ ok: true, body: second.stream })
      vi.stubGlobal('fetch', fetchMock)

      const { result } = renderHook(() => useAssistantChat())

      await act(async () => {
        void result.current.send('c1', 'first question')
        await Promise.resolve()
        void result.current.send('c1', 'second question')
      })

      const cancelCalls = fetchMock.mock.calls.filter(([url]) => String(url).includes('/run/cancel'))
      expect(cancelCalls).toHaveLength(0)
    })

    it('abort() (Stop) POSTs /run/cancel for the active conversation before closing the local stream', async () => {
      const { stream } = controllableSseStream()
      const fetchMock = vi.fn().mockResolvedValue({ ok: true, body: stream })
      vi.stubGlobal('fetch', fetchMock)

      const { result } = renderHook(() => useAssistantChat())

      act(() => {
        void result.current.send('c1', 'hello')
      })
      await act(async () => {
        await Promise.resolve()
      })

      act(() => {
        result.current.abort()
      })

      const cancelCall = fetchMock.mock.calls.find(([url]) => String(url).includes('/run/cancel'))
      expect(cancelCall).toBeDefined()
      expect(String(cancelCall?.[0])).toContain('/api/assistant/conversations/c1/run/cancel')
      expect(cancelCall?.[1]).toMatchObject({ method: 'POST' })
    })

    // Code review 2026-09-17: Stop used to fire the cancel and unlock the
    // composer at once, but the server only frees the conversation once the
    // run has torn down, so a question asked in that gap got 409 and was
    // lost. The composer now stays locked until the cancel has answered.
    it('keeps sending true until the cancel request has answered', async () => {
      const { stream } = controllableSseStream()
      let answerCancel!: () => void
      const fetchMock = vi.fn((url: string) => {
        if (String(url).includes('/run/cancel')) {
          return new Promise((resolve) => { answerCancel = () => resolve({ ok: true, status: 204 }) })
        }
        return Promise.resolve({ ok: true, body: stream })
      })
      vi.stubGlobal('fetch', fetchMock)

      const { result } = renderHook(() => useAssistantChat())
      act(() => {
        void result.current.send('c1', 'hello')
      })
      await act(async () => {
        await Promise.resolve()
      })

      act(() => {
        void result.current.abort()
      })
      expect(result.current.sending).toBe(true)
      expect(result.current.statusText).toBeNull()
      expect(result.current.draft).toBeNull()

      await act(async () => {
        answerCancel()
        await flushMicrotasks()
      })
      expect(result.current.sending).toBe(false)
    })

    it('surfaces a failed cancel instead of unlocking as if it worked', async () => {
      const { stream } = controllableSseStream()
      const fetchMock = vi.fn((url: string) => {
        if (String(url).includes('/run/cancel')) {
          return Promise.resolve({ ok: false, status: 503, json: () => Promise.resolve({ error: 'backend unavailable' }) })
        }
        return Promise.resolve({ ok: true, body: stream })
      })
      vi.stubGlobal('fetch', fetchMock)

      const { result } = renderHook(() => useAssistantChat())
      act(() => {
        void result.current.send('c1', 'hello')
      })
      await act(async () => {
        await Promise.resolve()
      })
      await act(async () => {
        await result.current.abort()
      })

      expect(result.current.error).toBe('backend unavailable')
      expect(result.current.sending).toBe(false)
    })
  })

  // Code review 2026-09-17: switching threads while one is streaming has to
  // leave that stream's status and draft behind, or the new thread shows
  // (and Stop cancels) the other conversation's run.
  describe('streaming ownership across conversations', () => {
    it('reports which conversation the local stream belongs to', async () => {
      const { stream } = controllableSseStream()
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, body: stream }))

      const { result } = renderHook(() => useAssistantChat())
      act(() => {
        void result.current.send('c1', 'hello')
      })

      expect(result.current.isStreamingConversation('c1')).toBe(true)
      expect(result.current.isStreamingConversation('c2')).toBe(false)
    })

    it('attach() to another conversation clears the superseded stream state even when nothing is running there', async () => {
      const raf = installFakeRaf()
      const first = controllableSseStream()
      const fetchMock = vi.fn((url: string) => {
        if (String(url).endsWith('/c2/run')) return Promise.resolve({ ok: true, status: 204 })
        return Promise.resolve({ ok: true, body: first.stream })
      })
      vi.stubGlobal('fetch', fetchMock)

      const { result } = renderHook(() => useAssistantChat())
      let sendPromise!: Promise<AssistantMessage | null>
      act(() => {
        sendPromise = result.current.send('c1', 'hello')
      })
      await act(async () => {
        first.push('event: status\ndata: {"text":"Looking up Deal Island…"}\n\n')
        first.push('event: delta\ndata: {"text":"Refuge Cove is"}\n\n')
        await flushMicrotasks()
        raf.flush()
      })
      expect(result.current.draft).toBe('Refuge Cove is')

      await act(async () => {
        await result.current.attach('c2')
      })

      expect(result.current.sending).toBe(false)
      expect(result.current.statusText).toBeNull()
      expect(result.current.draft).toBeNull()
      expect(result.current.isStreamingConversation('c1')).toBe(false)
      await expect(sendPromise).resolves.toBeNull()
      expect(fetchMock.mock.calls.some(([url]) => String(url).includes('/run/cancel'))).toBe(false)
    })
  })

  describe('attach() (ADR 0105 rejoin)', () => {
    it('resolves null and touches no state when GET /run answers 204 (no run in flight)', async () => {
      const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 204 })
      vi.stubGlobal('fetch', fetchMock)

      const { result } = renderHook(() => useAssistantChat())

      let reply: unknown = 'not yet resolved'
      await act(async () => {
        reply = await result.current.attach('c1')
      })

      expect(reply).toBeNull()
      expect(result.current.sending).toBe(false)
      expect(result.current.draft).toBeNull()
      expect(result.current.statusText).toBeNull()
      expect(result.current.error).toBeNull()
      expect(String(fetchMock.mock.calls[0][0])).toContain('/api/assistant/conversations/c1/run')
      expect(fetchMock.mock.calls[0][0]).not.toEqual(expect.stringContaining('/run/cancel'))
    })

    it('streams a draft from delta events, then resolves the final message, same as send()', async () => {
      const raf = installFakeRaf()
      const { stream, push, close } = controllableSseStream()
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, status: 200, body: stream }))

      const { result } = renderHook(() => useAssistantChat())

      let attachPromise!: Promise<AssistantMessage | null>
      act(() => {
        attachPromise = result.current.attach('c1')
      })

      await act(async () => {
        push('event: status\ndata: {"text":"Thinking…"}\n\n')
        await flushMicrotasks()
      })
      expect(result.current.sending).toBe(true)
      expect(result.current.statusText).toBe('Thinking…')

      await act(async () => {
        push('event: delta\ndata: {"text":"Tongue Bay"}\n\n')
        await flushMicrotasks()
      })
      expect(result.current.draft).toBeNull()
      act(() => raf.flush())
      expect(result.current.draft).toBe('Tongue Bay')

      await act(async () => {
        push(`data: ${JSON.stringify({ message: messageApi, conversation: conversationApi })}\n\n`)
        close()
        await flushMicrotasks()
      })

      const reply = await attachPromise
      expect(reply).toEqual({
        id: 'm2',
        conversationId: 'c1',
        seq: 2,
        role: 'assistant',
        content: 'Blue Pearl Bay first.',
        model: 'anthropic/claude-sonnet-4.5',
        promptTokens: 1000,
        completionTokens: 200,
        costUsd: 0.0184,
        toolRounds: 2,
        createdAt: '2026-09-11T00:00:00Z',
      })
      expect(result.current.draft).toBeNull()
      expect(result.current.sending).toBe(false)
    })

    it('calls onConversation once the message frame resolves, same as send()', async () => {
      const body = sseStream([`data: ${JSON.stringify({ message: messageApi, conversation: conversationApi })}\n\n`])
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, status: 200, body }))

      const onConversation = vi.fn()
      const { result } = renderHook(() => useAssistantChat())

      await act(async () => {
        await result.current.attach('c1', onConversation)
      })

      expect(onConversation).toHaveBeenCalledWith({
        id: 'c1',
        title: 'Hook Reef anchorages',
        createdAt: '2026-09-11T00:00:00Z',
        updatedAt: '2026-09-11T00:00:01Z',
      })
    })

    it('a second attach supersedes the first, the same way a second send does', async () => {
      const first = controllableSseStream()
      const second = sseStream([`data: ${JSON.stringify({ message: messageApi, conversation: conversationApi })}\n\n`])
      const fetchMock = vi.fn()
        .mockResolvedValueOnce({ ok: true, status: 200, body: first.stream })
        .mockResolvedValueOnce({ ok: true, status: 200, body: second })
      vi.stubGlobal('fetch', fetchMock)

      const { result } = renderHook(() => useAssistantChat())

      let firstReply: unknown = 'not yet resolved'
      let secondReply: unknown = 'not yet resolved'
      await act(async () => {
        const firstPromise = result.current.attach('c1').then((r) => { firstReply = r })
        await Promise.resolve()
        const secondPromise = result.current.attach('c1').then((r) => { secondReply = r })
        await Promise.all([firstPromise, secondPromise])
      })

      expect(firstReply).toBeNull()
      expect(secondReply).toMatchObject({ id: 'm2' })
    })
  })

  it('still calls onConversation via options once the message frame resolves', async () => {
    const body = sseStream([`data: ${JSON.stringify({ message: messageApi, conversation: conversationApi })}\n\n`])
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, body })
    vi.stubGlobal('fetch', fetchMock)

    const onConversation = vi.fn()
    const { result } = renderHook(() => useAssistantChat())

    await act(async () => {
      await result.current.send('c1', 'hello', { onConversation })
    })

    expect(onConversation).toHaveBeenCalledWith({
      id: 'c1',
      title: 'Hook Reef anchorages',
      createdAt: '2026-09-11T00:00:00Z',
      updatedAt: '2026-09-11T00:00:01Z',
    })
  })

  // mate-answer-toast plan (on top of ADR 0105 "the answer outlives the
  // page"): send() registers the conversation in the module-level watch
  // store the moment it posts a question, so an App-level watcher can tell
  // a reply finished while the operator was looking at something else and
  // toast it. attach() (rejoining an already-running reply, e.g. on mount)
  // deliberately does not register - only a question this tab itself just
  // asked counts.
  describe('mate-watch-store registration', () => {
    beforeEach(() => {
      for (const entry of getMateWatchSnapshot()) removeMateWatch(entry.conversationId)
    })

    it('registers the conversation when send() starts', async () => {
      const body = sseStream([`data: ${JSON.stringify({ message: messageApi, conversation: conversationApi })}\n\n`])
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, body }))

      const { result } = renderHook(() => useAssistantChat())

      await act(async () => {
        await result.current.send('c1', 'A bowline or a figure-eight?')
      })

      // The run has already resolved by the time send() returns in this
      // test, but registration happens synchronously at the start of
      // send() - watcher.test.ts covers the "still watched after send
      // settles" half of removal being the watcher's job, not send()'s.
      expect(getMateWatchSnapshot().some((entry) => entry.conversationId === 'c1')).toBe(true)
    })

    it('does not register on attach() - only a question this tab actually sent', async () => {
      const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 204 })
      vi.stubGlobal('fetch', fetchMock)

      const { result } = renderHook(() => useAssistantChat())

      await act(async () => {
        await result.current.attach('c1')
      })

      expect(getMateWatchSnapshot().some((entry) => entry.conversationId === 'c1')).toBe(false)
    })
  })
})
