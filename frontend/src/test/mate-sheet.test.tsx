import { describe, it, expect, afterEach, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'

import { MateSheet } from '@/components/mate-sheet'

// ADR 0093 voice phase: the Mate sheet owns its own conversations/chat hooks
// (same fetch-router pattern as assistant-drawer.test.tsx) - a voice
// question opens it with `initialQuestion`, which it sends at once with
// `spoken: true` and the current `screen`; opened without one, it just
// shows whatever thread is already active.

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

function buildFetch(onSendMessage?: (conversationId: string, body: Record<string, unknown>) => FetchLike) {
  const conversations: ConversationRecord[] = []
  const messagesByConversation = new Map<string, Array<Record<string, unknown>>>()
  let counter = 0

  return vi.fn(async (url: string, init?: RequestInit): Promise<FetchLike> => {
    const method = init?.method ?? 'GET'

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
      const body = init?.body ? (JSON.parse(init.body as string) as Record<string, unknown>) : {}
      if (!onSendMessage) throw new Error('onSendMessage not configured for this test')
      return onSendMessage(id, body)
    }

    const conversationMatch = url.match(/\/api\/assistant\/conversations\/([^/]+)$/)
    if (conversationMatch && method === 'GET') {
      const id = decodeURIComponent(conversationMatch[1])
      const conversation = conversations.find((c) => c.id === id) ?? { id, title: 'Untitled', created_at: '', updated_at: '' }
      return { ok: true, json: async () => ({ conversation, messages: messagesByConversation.get(id) ?? [] }) }
    }

    throw new Error(`Unhandled fetch in test: ${method} ${url}`)
  })
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

describe('MateSheet', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('does not render the thread while closed', () => {
    vi.stubGlobal('fetch', buildFetch())

    render(<MateSheet open={false} onOpenChange={vi.fn()} screen={{ panel: 'forecast' }} canWrite />)

    expect(screen.queryByRole('heading', { name: 'Mate' })).not.toBeInTheDocument()
  })

  it('opens with no initial question and just shows the thread and composer', async () => {
    vi.stubGlobal('fetch', buildFetch())

    render(<MateSheet open onOpenChange={vi.fn()} screen={{ panel: 'forecast' }} canWrite />)

    expect(await screen.findByRole('heading', { name: 'Mate' })).toBeInTheDocument()
    expect(screen.getByPlaceholderText('Ask Mate about the next couple of days…')).toBeInTheDocument()
  })

  it('sends the initial question at once with spoken:true and the screen', async () => {
    let sentBody: Record<string, unknown> | null = null
    vi.stubGlobal('fetch', buildFetch((conversationId, body) => {
      sentBody = body
      return sseMessageResponse(
        {
          id: 'm1',
          conversation_id: conversationId,
          seq: 1,
          role: 'assistant',
          content: '## Spoken summary\n\nFine tomorrow, light winds.',
          created_at: '2026-09-12T00:00:00Z',
        },
        { id: conversationId, title: 'How does tomorrow look?', created_at: '2026-09-12T00:00:00Z', updated_at: '2026-09-12T00:00:01Z' },
      )
    }))

    render(
      <MateSheet
        open
        onOpenChange={vi.fn()}
        initialQuestion="How does tomorrow look?"
        screen={{ panel: 'forecast' }}
        canWrite
      />,
    )

    await waitFor(() => expect(sentBody).toEqual({
      content: 'How does tomorrow look?',
      spoken: true,
      screen: { panel: 'forecast' },
    }))
    expect(await screen.findByText(/Fine tomorrow, light winds\./)).toBeInTheDocument()
    // The question itself lands as the optimistic user bubble.
    expect(screen.getByText('How does tomorrow look?')).toBeInTheDocument()
  })

  it('does not send when canWrite is false', async () => {
    let sent = false
    vi.stubGlobal('fetch', buildFetch((conversationId) => {
      sent = true
      return sseMessageResponse({ id: 'm1', conversation_id: conversationId, seq: 1, role: 'assistant', content: 'x', created_at: '' }, { id: conversationId, title: '', created_at: '', updated_at: '' })
    }))

    render(
      <MateSheet
        open
        onOpenChange={vi.fn()}
        initialQuestion="How does tomorrow look?"
        screen={{ panel: 'forecast' }}
        canWrite={false}
      />,
    )

    await screen.findByRole('heading', { name: 'Mate' })
    expect(sent).toBe(false)
  })

  // The Sheet primitive (Base UI dialog) wires up Escape and an overlay
  // click itself - this only needs to prove MateSheet relays that dismissal
  // back through the onOpenChange prop App.tsx uses to close it. The
  // built-in close button is the one guaranteed, stable way to trigger that
  // dismissal from a test without fighting jsdom's native keydown routing.
  it('closes via onOpenChange(false) when the sheet is dismissed', async () => {
    const onOpenChange = vi.fn()
    vi.stubGlobal('fetch', buildFetch())

    render(<MateSheet open onOpenChange={onOpenChange} screen={{ panel: 'forecast' }} canWrite />)
    await screen.findByRole('heading', { name: 'Mate' })

    fireEvent.click(screen.getByRole('button', { name: 'Close' }))

    // The Base UI dialog's onOpenChange also carries an event-details object
    // as a second argument - only the boolean matters to MateSheet's caller.
    await waitFor(() => expect(onOpenChange.mock.calls[0]?.[0]).toBe(false))
  })
})
