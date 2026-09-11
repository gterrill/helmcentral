import { describe, it, expect, afterEach, vi } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'
import { useAssistantConversations } from '@/hooks/use-assistant-conversations'

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

  it('lists conversations on mount, newest updated first as the server returns them', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ conversations: [conversationApi({ id: 'c1', title: 'Hook Reef' })] }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useAssistantConversations())

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(fetchMock).toHaveBeenCalledWith('/api/assistant/conversations')
    expect(result.current.conversations).toEqual([
      { id: 'c1', title: 'Hook Reef', createdAt: '2026-09-01T00:00:00Z', updatedAt: '2026-09-01T00:00:00Z' },
    ])
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
      .mockResolvedValueOnce({ ok: true, json: async () => ({ conversations: [conversationApi()] }) })
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({ conversation: conversationApi(), messages: [messageApi({ content: 'hi there' })] }),
      })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useAssistantConversations())
    await waitFor(() => expect(result.current.loading).toBe(false))

    await act(async () => {
      await result.current.select('c1')
    })

    expect(fetchMock).toHaveBeenLastCalledWith('/api/assistant/conversations/c1')
    expect(result.current.activeId).toBe('c1')
    expect(result.current.messages).toEqual([{
      id: 'm1',
      conversationId: 'c1',
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
      // initial list: two conversations, c1 first (active later)
      .mockResolvedValueOnce({ ok: true, json: async () => ({ conversations: [conversationApi({ id: 'c1' }), conversationApi({ id: 'c2' })] }) })
      // select c1
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

    await act(async () => {
      await result.current.select('c1')
    })
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

    await act(async () => { await result.current.select('c1') })
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
})
