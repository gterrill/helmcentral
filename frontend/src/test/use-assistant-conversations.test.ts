import { describe, it, expect, afterEach, vi } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'
import { describeLoadError, useAssistantConversations } from '@/hooks/use-assistant-conversations'

// ADR 0093: conversation list + thread state for the assistant panel. The
// wire shape is snake_case (Conversation/Message); the hook maps it to the
// camelCase fields the rest of the frontend expects, the way
// use-tide-chart.ts / use-weather-forecast.ts do it.

const conversationApi = (overrides: Partial<Record<string, unknown>> = {}) => ({
  id: 'c1',
  title: 'Untitled',
  created_at: '2026-09-01T00:00:00Z',
  updated_at: '2026-09-01T00:00:00Z',
  ...overrides,
})

const messageApi = (overrides: Partial<Record<string, unknown>> = {}) => ({
  id: 'm1',
  conversation_id: 'c1',
  seq: 1,
  role: 'user',
  content: 'hello',
  created_at: '2026-09-01T00:00:00Z',
  ...overrides,
})

describe('useAssistantConversations', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('lists conversations on mount and opens the newest thread', async () => {
    const fetchMock = vi.fn().mockImplementation(async (url: string) => {
      if (url === '/api/assistant/conversations') {
        return { ok: true, json: async () => ({ conversations: [conversationApi({ id: 'c1', title: 'Hook Reef' })] }) }
      }
      if (url === '/api/assistant/conversations/c1') {
        return {
          ok: true,
          json: async () => ({ conversation: conversationApi({ id: 'c1' }), messages: [messageApi({ content: 'hello' })] }),
        }
      }
      throw new Error(`unexpected fetch ${url}`)
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useAssistantConversations())

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(fetchMock).toHaveBeenCalledWith('/api/assistant/conversations')
    expect(result.current.conversations).toEqual([
      { id: 'c1', title: 'Hook Reef', createdAt: '2026-09-01T00:00:00Z', updatedAt: '2026-09-01T00:00:00Z' },
    ])
    await waitFor(() => expect(result.current.activeId).toBe('c1'))
    await waitFor(() => expect(result.current.messages.map((m) => m.content)).toEqual(['hello']))
  })

  it('leaves nothing selected when there are no conversations', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({ ok: true, json: async () => ({ conversations: [] }) }),
    )

    const { result } = renderHook(() => useAssistantConversations())

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.activeId).toBeNull()
    expect(result.current.messages).toEqual([])
  })

  it('create POSTs a new conversation, refreshes the list and selects the new id', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ conversations: [] }) }) // initial list
      .mockResolvedValueOnce({ ok: true, json: async () => conversationApi({ id: 'new-1', title: 'New conversation' }) }) // POST
      .mockResolvedValueOnce({ ok: true, json: async () => ({ conversations: [conversationApi({ id: 'new-1', title: 'New conversation' })] }) }) // refresh
      .mockResolvedValueOnce({ ok: true, json: async () => ({ conversation: conversationApi({ id: 'new-1' }), messages: [] }) }) // select
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useAssistantConversations())
    await waitFor(() => expect(result.current.loading).toBe(false))

    let newId: string | null = null
    await act(async () => {
      newId = await result.current.create()
    })

    expect(newId).toBe('new-1')
    expect(fetchMock).toHaveBeenCalledWith('/api/assistant/conversations', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({}),
    }))
    expect(result.current.activeId).toBe('new-1')
    expect(result.current.conversations.map((c) => c.id)).toEqual(['new-1'])
  })

  it('select loads the conversation thread', async () => {
    const fetchMock = vi.fn()
      // initial list: c1 first, which the mount opens on its own
      .mockResolvedValueOnce({ ok: true, json: async () => ({ conversations: [conversationApi({ id: 'c1' }), conversationApi({ id: 'c2' })] }) })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ conversation: conversationApi({ id: 'c1' }), messages: [] }) })
      // the operator picks c2
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({ conversation: conversationApi({ id: 'c2' }), messages: [messageApi({ conversation_id: 'c2', content: 'hi there' })] }),
      })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useAssistantConversations())
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.activeId).toBe('c1')

    await act(async () => {
      await result.current.select('c2')
    })

    expect(fetchMock).toHaveBeenLastCalledWith('/api/assistant/conversations/c2')
    expect(result.current.activeId).toBe('c2')
    expect(result.current.messages).toEqual([{
      id: 'm1',
      conversationId: 'c2',
      seq: 1,
      role: 'user',
      content: 'hi there',
      model: undefined,
      promptTokens: undefined,
      completionTokens: undefined,
      costUsd: undefined,
      toolRounds: undefined,
      createdAt: '2026-09-01T00:00:00Z',
    }])
  })

  it('remove deletes the active conversation and selects the next one', async () => {
    const fetchMock = vi.fn()
      // initial list: two conversations, c1 first (opened by the mount)
      .mockResolvedValueOnce({ ok: true, json: async () => ({ conversations: [conversationApi({ id: 'c1' }), conversationApi({ id: 'c2' })] }) })
      // the mount opens c1
      .mockResolvedValueOnce({ ok: true, json: async () => ({ conversation: conversationApi({ id: 'c1' }), messages: [messageApi()] }) })
      // DELETE c1
      .mockResolvedValueOnce({ ok: true, status: 204 })
      // refreshed list after delete: only c2 remains
      .mockResolvedValueOnce({ ok: true, json: async () => ({ conversations: [conversationApi({ id: 'c2' })] }) })
      // select c2 (the next conversation)
      .mockResolvedValueOnce({ ok: true, json: async () => ({ conversation: conversationApi({ id: 'c2' }), messages: [] }) })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useAssistantConversations())
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.activeId).toBe('c1')

    await act(async () => {
      await result.current.remove('c1')
    })

    expect(fetchMock).toHaveBeenCalledWith('/api/assistant/conversations/c1', expect.objectContaining({ method: 'DELETE' }))
    expect(result.current.conversations.map((c) => c.id)).toEqual(['c2'])
    expect(result.current.activeId).toBe('c2')
  })

  it('remove of the only conversation clears the active selection', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ conversations: [conversationApi({ id: 'c1' })] }) })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ conversation: conversationApi({ id: 'c1' }), messages: [] }) })
      .mockResolvedValueOnce({ ok: true, status: 204 })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ conversations: [] }) })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useAssistantConversations())
    await waitFor(() => expect(result.current.loading).toBe(false))

    expect(result.current.activeId).toBe('c1')
    await act(async () => { await result.current.remove('c1') })

    expect(result.current.activeId).toBeNull()
    expect(result.current.messages).toEqual([])
  })

  it('appendLocal appends a message to the thread without a network call', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ conversations: [] }) })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useAssistantConversations())
    await waitFor(() => expect(result.current.loading).toBe(false))

    const callCountBefore = fetchMock.mock.calls.length
    act(() => {
      result.current.appendLocal({
        id: 'local-1', conversationId: 'c1', seq: 0, role: 'user', content: 'hi', createdAt: '2026-09-01T00:00:00Z',
      })
    })

    expect(result.current.messages).toHaveLength(1)
    expect(fetchMock.mock.calls.length).toBe(callCountBefore)
  })

  // ADR 0094: "Open in Mate" hands the panel a conversation id the sheet
  // already has active, so the panel's own hook instance needs to open that
  // thread on mount rather than falling back to the newest one.
  it('selects initialId on mount when it is present in the fetched list', async () => {
    const fetchMock = vi.fn().mockImplementation(async (url: string) => {
      if (url === '/api/assistant/conversations') {
        return {
          ok: true,
          json: async () => ({
            conversations: [conversationApi({ id: 'c1', title: 'Newest' }), conversationApi({ id: 'c2', title: 'Older' })],
          }),
        }
      }
      if (url === '/api/assistant/conversations/c2') {
        return { ok: true, json: async () => ({ conversation: conversationApi({ id: 'c2' }), messages: [messageApi({ conversation_id: 'c2', content: 'from c2' })] }) }
      }
      throw new Error(`unexpected fetch ${url}`)
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useAssistantConversations({ initialId: 'c2' }))

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.activeId).toBe('c2')
    expect(result.current.messages.map((m) => m.content)).toEqual(['from c2'])
  })

  it('falls back to the newest conversation when initialId is absent from the fetched list', async () => {
    const fetchMock = vi.fn().mockImplementation(async (url: string) => {
      if (url === '/api/assistant/conversations') {
        return {
          ok: true,
          json: async () => ({
            conversations: [conversationApi({ id: 'c1', title: 'Newest' }), conversationApi({ id: 'c2', title: 'Older' })],
          }),
        }
      }
      if (url === '/api/assistant/conversations/c1') {
        return { ok: true, json: async () => ({ conversation: conversationApi({ id: 'c1' }), messages: [] }) }
      }
      throw new Error(`unexpected fetch ${url}`)
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useAssistantConversations({ initialId: 'deleted-id' }))

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.activeId).toBe('c1')
  })

  it('re-selects when initialId changes to a new non-null value after mount', async () => {
    const fetchMock = vi.fn().mockImplementation(async (url: string) => {
      if (url === '/api/assistant/conversations') {
        return { ok: true, json: async () => ({ conversations: [conversationApi({ id: 'c1' })] }) }
      }
      if (url === '/api/assistant/conversations/c1') {
        return { ok: true, json: async () => ({ conversation: conversationApi({ id: 'c1' }), messages: [] }) }
      }
      if (url === '/api/assistant/conversations/c9') {
        return { ok: true, json: async () => ({ conversation: conversationApi({ id: 'c9' }), messages: [messageApi({ conversation_id: 'c9', content: 'from c9' })] }) }
      }
      throw new Error(`unexpected fetch ${url}`)
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result, rerender } = renderHook(
      ({ initialId }: { initialId: string | null }) => useAssistantConversations({ initialId }),
      { initialProps: { initialId: null as string | null } },
    )

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.activeId).toBe('c1')

    rerender({ initialId: 'c9' })

    await waitFor(() => expect(result.current.activeId).toBe('c9'))
    // activeId and messages land in separate commits — select() flips
    // activeId immediately, then loads messages once its own fetch resolves
    // — so a synchronous check right after the activeId waitFor is racing
    // that second commit rather than verifying it.
    await waitFor(() => {
      expect(result.current.messages.map((m) => m.content)).toEqual(['from c9'])
    })
  })

  it('does not re-select on mount just because initialId happens to be non-null', async () => {
    // Guards against the re-select effect firing a second, redundant GET for
    // the same id the mount effect already selected.
    const fetchMock = vi.fn().mockImplementation(async (url: string) => {
      if (url === '/api/assistant/conversations') {
        return { ok: true, json: async () => ({ conversations: [conversationApi({ id: 'c1' })] }) }
      }
      if (url === '/api/assistant/conversations/c1') {
        return { ok: true, json: async () => ({ conversation: conversationApi({ id: 'c1' }), messages: [] }) }
      }
      throw new Error(`unexpected fetch ${url}`)
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useAssistantConversations({ initialId: 'c1' }))
    await waitFor(() => expect(result.current.loading).toBe(false))

    const selectCalls = fetchMock.mock.calls.filter(([url]) => url === '/api/assistant/conversations/c1')
    expect(selectCalls).toHaveLength(1)
  })

  // fix(frontend): make a failed Mate load recoverable - reload() repeats
  // the initial load (list, then select the initial id if present else the
  // newest) after clearing the stale error, so a dropped link doesn't
  // strand the sheet/panel with no way out short of a full page reload.
  it('reload clears the error and re-selects the newest conversation once the server recovers', async () => {
    let serverIsUp = false
    const fetchMock = vi.fn().mockImplementation(async (url: string) => {
      if (url === '/api/assistant/conversations') {
        if (!serverIsUp) return { ok: false, status: 502 }
        return { ok: true, json: async () => ({ conversations: [conversationApi({ id: 'c1', title: 'Hook Reef' })] }) }
      }
      if (url === '/api/assistant/conversations/c1') {
        return { ok: true, json: async () => ({ conversation: conversationApi({ id: 'c1' }), messages: [messageApi({ content: 'hello' })] }) }
      }
      throw new Error(`unexpected fetch ${url}`)
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useAssistantConversations())
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.error).toBe('HTTP 502')
    expect(result.current.errorMessage).toBe(describeLoadError('HTTP 502'))
    expect(result.current.conversations).toEqual([])
    expect(result.current.activeId).toBeNull()

    serverIsUp = true
    await act(async () => {
      await result.current.reload()
    })

    expect(result.current.error).toBeNull()
    expect(result.current.errorMessage).toBeNull()
    expect(result.current.activeId).toBe('c1')
    expect(result.current.messages.map((m) => m.content)).toEqual(['hello'])
  })

  it('reload selects initialId again when the server recovers, same as the mount rule', async () => {
    let serverIsUp = false
    const fetchMock = vi.fn().mockImplementation(async (url: string) => {
      if (url === '/api/assistant/conversations') {
        if (!serverIsUp) return { ok: false, status: 502 }
        return {
          ok: true,
          json: async () => ({
            conversations: [conversationApi({ id: 'c1', title: 'Newest' }), conversationApi({ id: 'c2', title: 'Requested' })],
          }),
        }
      }
      if (url === '/api/assistant/conversations/c2') {
        return { ok: true, json: async () => ({ conversation: conversationApi({ id: 'c2' }), messages: [] }) }
      }
      throw new Error(`unexpected fetch ${url}`)
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useAssistantConversations({ initialId: 'c2' }))
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.error).toBe('HTTP 502')

    serverIsUp = true
    await act(async () => {
      await result.current.reload()
    })

    expect(result.current.activeId).toBe('c2')
  })

  it('reload sets loading while it runs', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({ conversations: [] }) }))

    const { result } = renderHook(() => useAssistantConversations())
    await waitFor(() => expect(result.current.loading).toBe(false))

    let pending: Promise<void> = Promise.resolve()
    act(() => {
      pending = result.current.reload()
    })
    expect(result.current.loading).toBe(true)

    await act(async () => {
      await pending
    })
    expect(result.current.loading).toBe(false)
  })
})

describe('describeLoadError', () => {
  it.each(['502', '503', '504'])('names a dropped link for HTTP %s', (code) => {
    expect(describeLoadError(`HTTP ${code}`)).toBe(
      `Mate's conversations could not be loaded. The server answered ${code}. This is usually the boat's link dropping for a moment.`,
    )
  })

  it('names the status for any other HTTP code, without the link-dropping guess', () => {
    expect(describeLoadError('HTTP 401')).toBe("Mate's conversations could not be loaded (HTTP 401).")
  })

  it('describes a network failure that never got a status code at all', () => {
    expect(describeLoadError('Failed to fetch')).toBe("Mate's conversations could not be loaded. The server did not answer.")
  })
})
