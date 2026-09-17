import { describe, it, expect, afterEach, vi } from 'vitest'
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react'

import { AssistantDrawer } from '@/components/assistant-drawer'

// ADR 0093: the assistant panel owns its own hooks (status, conversations,
// chat), each of which calls fetch directly - so these tests drive it the
// way App-level tests drive fetch-backed components: a small router stubbed
// onto the global fetch, rather than mocking three hook modules and losing
// the wiring between them.

interface ConversationRecord {
  id: string
  title: string
  created_at: string
  updated_at: string
}

interface FetchLike {
  ok: boolean
  status?: number
  json?: () => Promise<unknown>
  body?: ReadableStream<Uint8Array> | null
}

interface AssistantServerOptions {
  status: { enabled: boolean; configured: boolean; model: string; problem?: string }
  conversations?: ConversationRecord[]
  onSendMessage?: (conversationId: string, content: string) => FetchLike
}

function buildAssistantFetch(opts: AssistantServerOptions) {
  const conversations: ConversationRecord[] = opts.conversations ? [...opts.conversations] : []
  const messagesByConversation = new Map<string, Array<Record<string, unknown>>>()
  let counter = 0

  const fetchMock = vi.fn(async (url: string, init?: RequestInit): Promise<FetchLike> => {
    const method = init?.method ?? 'GET'

    if (url.endsWith('/api/assistant/status')) {
      return { ok: true, json: async () => opts.status }
    }

    if (url.endsWith('/api/assistant/conversations') && method === 'GET') {
      return { ok: true, json: async () => ({ conversations }) }
    }

    if (url.endsWith('/api/assistant/conversations') && method === 'POST') {
      counter += 1
      const id = `new-${counter}`
      const now = new Date().toISOString()
      const conversation: ConversationRecord = { id, title: 'New conversation', created_at: now, updated_at: now }
      conversations.unshift(conversation)
      messagesByConversation.set(id, [])
      return { ok: true, status: 201, json: async () => conversation }
    }

    const messagesMatch = url.match(/\/api\/assistant\/conversations\/([^/]+)\/messages$/)
    if (messagesMatch && method === 'POST') {
      const id = decodeURIComponent(messagesMatch[1])
      const parsedBody = init?.body ? (JSON.parse(init.body as string) as { content: string }) : { content: '' }
      if (!opts.onSendMessage) throw new Error('onSendMessage not configured for this test')
      return opts.onSendMessage(id, parsedBody.content)
    }

    const conversationMatch = url.match(/\/api\/assistant\/conversations\/([^/]+)$/)
    if (conversationMatch && method === 'GET') {
      const id = decodeURIComponent(conversationMatch[1])
      const conversation = conversations.find((c) => c.id === id) ?? { id, title: 'Untitled', created_at: '', updated_at: '' }
      return { ok: true, json: async () => ({ conversation, messages: messagesByConversation.get(id) ?? [] }) }
    }

    if (conversationMatch && method === 'DELETE') {
      const id = decodeURIComponent(conversationMatch[1])
      const idx = conversations.findIndex((c) => c.id === id)
      if (idx >= 0) conversations.splice(idx, 1)
      messagesByConversation.delete(id)
      return { ok: true, status: 204 }
    }

    // ADR 0105: AssistantThread's rejoin effect GETs .../run for whatever
    // conversation becomes active - 204 (no run in flight) is the ordinary
    // answer here, since none of this file's scenarios leave a run actually
    // going once send() has resolved.
    if (/\/api\/assistant\/conversations\/[^/]+\/run$/.test(url) && method === 'GET') {
      return { ok: true, status: 204 }
    }
    if (/\/api\/assistant\/conversations\/[^/]+\/run\/cancel$/.test(url) && method === 'POST') {
      return { ok: true, status: 204 }
    }

    throw new Error(`Unhandled fetch in test: ${method} ${url}`)
  })

  return fetchMock
}

function sseMessageResponse(message: Record<string, unknown>, conversation: Record<string, unknown>): FetchLike {
  const encoder = new TextEncoder()
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(encoder.encode(`data: ${JSON.stringify({ message, conversation })}\n\n`))
      controller.close()
    },
  })
  return { ok: true, body }
}

function controllableStream() {
  let streamController!: ReadableStreamDefaultController<Uint8Array>
  const stream = new ReadableStream<Uint8Array>({ start(controller) { streamController = controller } })
  const encoder = new TextEncoder()
  return {
    body: stream,
    push: (chunk: string) => streamController.enqueue(encoder.encode(chunk)),
    close: () => streamController.close(),
  }
}

async function flush(times = 8) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

const assistantMessage = (overrides: Partial<Record<string, unknown>> = {}) => ({
  id: 'm1',
  conversation_id: 'new-1',
  seq: 1,
  role: 'assistant',
  content: 'Blue Pearl Bay first, on the flood.',
  model: 'anthropic/claude-sonnet-4.5',
  prompt_tokens: 1000,
  completion_tokens: 200,
  cost_usd: 0.0184,
  created_at: '2026-09-11T00:00:00Z',
  ...overrides,
})

describe('AssistantDrawer', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('shows the problem card when unconfigured, and its button opens settings', async () => {
    const onOpenSettings = vi.fn()
    vi.stubGlobal('fetch', buildAssistantFetch({
      status: { enabled: false, configured: false, model: '', problem: 'Set up an OpenRouter key in Settings → Assistant.' },
    }))

    render(<AssistantDrawer canWrite onOpenSettings={onOpenSettings} />)

    expect(await screen.findByText('Set up an OpenRouter key in Settings → Assistant.')).toBeInTheDocument()
    expect(screen.queryByPlaceholderText('Ask about a passage, an anchorage, or how a panel works…')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Open Mate settings' }))
    expect(onOpenSettings).toHaveBeenCalledTimes(1)
  })

  it('shows the conversation list and composer once configured', async () => {
    vi.stubGlobal('fetch', buildAssistantFetch({
      status: { enabled: true, configured: true, model: 'anthropic/claude-sonnet-4.5' },
      conversations: [{ id: 'c1', title: 'Hook Reef anchorages', created_at: '2026-09-01T00:00:00Z', updated_at: '2026-09-01T00:00:00Z' }],
    }))

    render(<AssistantDrawer canWrite onOpenSettings={vi.fn()} />)

    expect(await screen.findByText('Hook Reef anchorages')).toBeInTheDocument()
    expect(screen.getByPlaceholderText('Ask about a passage, an anchorage, or how a panel works…')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'New conversation' })).toBeInTheDocument()
    // Empty-thread hint shows the Whitsundays example question.
    expect(screen.getByText(/Tongue Bay or Blue Pearl Bay/)).toBeInTheDocument()
  })

  it('shows a search box above the list and filters conversations by query', async () => {
    vi.stubGlobal('fetch', buildAssistantFetch({
      status: { enabled: true, configured: true, model: 'anthropic/claude-sonnet-4.5' },
      conversations: [
        { id: 'c1', title: 'Gloucester Island Anchorages', created_at: '2026-09-11T00:00:00Z', updated_at: '2026-09-11T00:00:00Z' },
        { id: 'c2', title: 'Hamilton Island Weather', created_at: '2026-09-10T00:00:00Z', updated_at: '2026-09-10T00:00:00Z' },
      ],
    }))

    render(<AssistantDrawer canWrite onOpenSettings={vi.fn()} />)

    const search = await screen.findByPlaceholderText('Search conversations')
    expect(search).toBeInTheDocument()

    fireEvent.change(search, { target: { value: 'gloucester' } })

    expect(screen.getByText('Gloucester Island Anchorages')).toBeInTheDocument()
    expect(screen.queryByText('Hamilton Island Weather')).not.toBeInTheDocument()
  })

  it('disables the composer and shows the read-only hint when canWrite is false', async () => {
    vi.stubGlobal('fetch', buildAssistantFetch({
      status: { enabled: true, configured: true, model: 'anthropic/claude-sonnet-4.5' },
    }))

    render(<AssistantDrawer canWrite={false} onOpenSettings={vi.fn()} />)

    const textarea = await screen.findByPlaceholderText('Ask about a passage, an anchorage, or how a panel works…')
    expect(textarea).toBeDisabled()
    expect(screen.getByText('Read-only session')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Send' })).toBeDisabled()
  })

  it('Shift+Enter does not send; Enter sends and appends the assistant reply with its cost footer', async () => {
    vi.stubGlobal('fetch', buildAssistantFetch({
      status: { enabled: true, configured: true, model: 'anthropic/claude-sonnet-4.5' },
      onSendMessage: (conversationId, content) => {
        expect(content).toBe('Tongue Bay or Blue Pearl Bay first?')
        return sseMessageResponse(
          assistantMessage({ conversation_id: conversationId }),
          { id: conversationId, title: 'Tongue Bay or Blue Pearl Bay first?', created_at: '2026-09-11T00:00:00Z', updated_at: '2026-09-11T00:00:01Z' },
        )
      },
    }))

    render(<AssistantDrawer canWrite onOpenSettings={vi.fn()} />)

    const textarea = await screen.findByPlaceholderText('Ask about a passage, an anchorage, or how a panel works…') as HTMLTextAreaElement
    fireEvent.change(textarea, { target: { value: 'Tongue Bay or Blue Pearl Bay first?' } })

    fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: true })
    // Shift+Enter must not have sent anything - the composer still holds the draft.
    expect(textarea.value).toBe('Tongue Bay or Blue Pearl Bay first?')
    expect(screen.queryByText('Blue Pearl Bay first, on the flood.')).not.toBeInTheDocument()

    await act(async () => {
      fireEvent.keyDown(textarea, { key: 'Enter' })
      await flush(20)
    })

    await waitFor(() => expect(screen.getByText('Blue Pearl Bay first, on the flood.')).toBeInTheDocument())
    // Cost leads the visible footer; model/tokens moved to its title tooltip.
    expect(screen.getByTitle('anthropic/claude-sonnet-4.5 · 1,200 tokens')).toHaveTextContent('$0.018')
    // The optimistic user bubble is also in the thread.
    expect(screen.getByText('Tongue Bay or Blue Pearl Bay first?')).toBeInTheDocument()
    // The composer clears after a successful send.
    expect(textarea.value).toBe('')
  })

  it('shows the status line while a status frame is pending, then resolves', async () => {
    const stream = controllableStream()
    vi.stubGlobal('fetch', buildAssistantFetch({
      status: { enabled: true, configured: true, model: 'anthropic/claude-sonnet-4.5' },
      onSendMessage: () => ({ ok: true, body: stream.body }),
    }))

    render(<AssistantDrawer canWrite onOpenSettings={vi.fn()} />)

    const textarea = await screen.findByPlaceholderText('Ask about a passage, an anchorage, or how a panel works…')
    fireEvent.change(textarea, { target: { value: 'What about the wind tomorrow?' } })

    act(() => { fireEvent.keyDown(textarea, { key: 'Enter' }) })

    await act(async () => {
      stream.push('event: status\ndata: {"text":"Fetching wind forecast for Tongue Bay…"}\n\n')
      await flush()
    })

    expect(await screen.findByText('Fetching wind forecast for Tongue Bay…')).toBeInTheDocument()

    await act(async () => {
      stream.push(`data: ${JSON.stringify({
        message: assistantMessage({ content: 'Settled.' }),
        conversation: { id: 'new-1', title: 'What about the wind tomorrow?', created_at: '2026-09-11T00:00:00Z', updated_at: '2026-09-11T00:00:01Z' },
      })}\n\n`)
      stream.close()
      await flush()
    })

    await waitFor(() => expect(screen.getByText('Settled.')).toBeInTheDocument())
    expect(screen.queryByText('Fetching wind forecast for Tongue Bay…')).not.toBeInTheDocument()
  })

  // ADR 0094: "Open in Mate" hands the drawer a conversation id from the
  // sheet, and the drawer's own hook instance has to open that thread on
  // mount rather than the newest one in the list.
  it('opens the requested initialConversationId instead of the newest conversation', async () => {
    vi.stubGlobal('fetch', buildAssistantFetch({
      status: { enabled: true, configured: true, model: 'anthropic/claude-sonnet-4.5' },
      conversations: [
        { id: 'c1', title: 'Newest', created_at: '2026-09-11T00:00:00Z', updated_at: '2026-09-11T00:00:00Z' },
        { id: 'c2', title: 'Older, requested', created_at: '2026-09-01T00:00:00Z', updated_at: '2026-09-01T00:00:00Z' },
      ],
    }))

    render(<AssistantDrawer canWrite onOpenSettings={vi.fn()} initialConversationId="c2" />)

    expect(await screen.findByText('Older, requested')).toBeInTheDocument()
    // The list still shows both; c2 is the one selected as active.
    const button = screen.getByText('Older, requested').closest('button')
    await waitFor(() => expect(button?.parentElement).toHaveClass('bg-primary/10'))
  })

  it('renders -- for every footer field the server did not report', async () => {
    vi.stubGlobal('fetch', buildAssistantFetch({
      status: { enabled: true, configured: true, model: 'anthropic/claude-sonnet-4.5' },
      onSendMessage: (conversationId) => sseMessageResponse(
        {
          id: 'm2',
          conversation_id: conversationId,
          seq: 1,
          role: 'assistant',
          content: 'No cost reported.',
          created_at: '2026-09-11T00:00:00Z',
        },
        { id: conversationId, title: 'No cost reported.', created_at: '2026-09-11T00:00:00Z', updated_at: '2026-09-11T00:00:01Z' },
      ),
    }))

    render(<AssistantDrawer canWrite onOpenSettings={vi.fn()} />)

    const textarea = await screen.findByPlaceholderText('Ask about a passage, an anchorage, or how a panel works…')
    fireEvent.change(textarea, { target: { value: 'Any cost info?' } })

    await act(async () => {
      fireEvent.keyDown(textarea, { key: 'Enter' })
      await flush(20)
    })

    await waitFor(() => expect(screen.getByText('No cost reported.')).toBeInTheDocument())
    expect(screen.getByTitle('-- · -- tokens')).toHaveTextContent('--')
  })
})
