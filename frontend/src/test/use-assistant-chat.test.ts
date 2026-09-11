import { describe, it, expect, afterEach, vi } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useAssistantChat } from '@/hooks/use-assistant-chat'
import type { AssistantMessage } from '@/hooks/use-assistant-conversations'

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
})
