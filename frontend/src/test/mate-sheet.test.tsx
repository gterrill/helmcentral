import { describe, it, expect, afterEach, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'

import { MateSheet } from '@/components/mate-sheet'

// ADR 0093 voice phase: the Mate sheet owns its own conversations/chat hooks
// (same fetch-router pattern as assistant-drawer.test.tsx) - a voice
// question opens it with `initialQuestion`, which it sends at once with
// `spoken: true` and the current `screen`; opened without one, it just
// shows whatever thread is already active.
//
// Read-aloud (also ADR 0093 voice phase) speaks the `## Spoken summary`
// section of a reply that arrived from a spoken question - jsdom has no
// speechSynthesis, so a fake is installed the same way
// use-speech-output.test.ts installs one.

class FakeUtterance {
  text: string
  onstart: (() => void) | null = null
  onend: (() => void) | null = null
  onerror: (() => void) | null = null
  constructor(text?: string) { this.text = text ?? '' }
}

class FakeSpeechSynthesis {
  spoken: FakeUtterance[] = []
  getVoices() { return [] }
  speak(utterance: FakeUtterance) { this.spoken.push(utterance) }
  cancel() {}
}

let fakeSynth: FakeSpeechSynthesis

function installFakeSpeechSynthesis() {
  fakeSynth = new FakeSpeechSynthesis()
  vi.stubGlobal('speechSynthesis', fakeSynth)
  vi.stubGlobal('SpeechSynthesisUtterance', FakeUtterance)
}

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

    render(<MateSheet open={false} onOpenChange={vi.fn()} screen={{ panel: 'forecast' }} canWrite readAloud={false} onOpenPanel={vi.fn()} />)

    expect(screen.queryByRole('heading', { name: 'Mate' })).not.toBeInTheDocument()
  })

  it('opens with no initial question and just shows the thread and composer', async () => {
    vi.stubGlobal('fetch', buildFetch())

    render(<MateSheet open onOpenChange={vi.fn()} screen={{ panel: 'forecast' }} canWrite readAloud={false} onOpenPanel={vi.fn()} />)

    expect(await screen.findByRole('heading', { name: 'Mate' })).toBeInTheDocument()
    expect(screen.getByPlaceholderText('Ask Mate about the next couple of days…')).toBeInTheDocument()
  })

  // ADR 0094: the sheet is one thread plus the composer, and it keeps
  // appending to the current conversation until the operator presses New -
  // no time-based expiry. These two header buttons are how the operator
  // starts a fresh thread or hands the current one off to the full page.
  it('New conversation creates a fresh thread and focuses the composer', async () => {
    vi.stubGlobal('fetch', buildFetch())

    render(
      <MateSheet
        open
        onOpenChange={vi.fn()}
        initialQuestion="How does tomorrow look?"
        screen={{ panel: 'forecast' }}
        canWrite
        readAloud={false}
        onOpenPanel={vi.fn()}
      />,
    )

    // The initial question's optimistic bubble lands first - the sheet has
    // an active thread with content before New is pressed.
    await screen.findByText('How does tomorrow look?')

    fireEvent.click(screen.getByRole('button', { name: 'New conversation' }))

    await waitFor(() => expect(screen.queryByText('How does tomorrow look?')).not.toBeInTheDocument())
    const textarea = await screen.findByPlaceholderText('Ask Mate about the next couple of days…')
    await waitFor(() => expect(textarea).toHaveFocus())
  })

  it('Open in Mate hands the active conversation to the panel and closes the sheet', async () => {
    const onOpenChange = vi.fn()
    const onOpenPanel = vi.fn()
    vi.stubGlobal('fetch', buildFetch((conversationId) => sseMessageResponse(
      { id: 'm1', conversation_id: conversationId, seq: 1, role: 'assistant', content: 'Fine tomorrow.', created_at: '' },
      { id: conversationId, title: 'How does tomorrow look?', created_at: '', updated_at: '' },
    )))

    render(
      <MateSheet
        open
        onOpenChange={onOpenChange}
        initialQuestion="How does tomorrow look?"
        screen={{ panel: 'forecast' }}
        canWrite
        readAloud={false}
        onOpenPanel={onOpenPanel}
      />,
    )

    await screen.findByText('Fine tomorrow.')

    fireEvent.click(screen.getByRole('button', { name: 'Open in Mate' }))

    expect(onOpenPanel).toHaveBeenCalledWith('new-1')
    expect(onOpenChange).toHaveBeenCalledWith(false)
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
        readAloud={false}
        onOpenPanel={vi.fn()}
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
        readAloud={false}
        onOpenPanel={vi.fn()}
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

    render(<MateSheet open onOpenChange={onOpenChange} screen={{ panel: 'forecast' }} canWrite readAloud={false} onOpenPanel={vi.fn()} />)
    await screen.findByRole('heading', { name: 'Mate' })

    fireEvent.click(screen.getByRole('button', { name: 'Close' }))

    // The Base UI dialog's onOpenChange also carries an event-details object
    // as a second argument - only the boolean matters to MateSheet's caller.
    await waitFor(() => expect(onOpenChange.mock.calls[0]?.[0]).toBe(false))
  })
})

describe('MateSheet read-aloud', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('speaks the extracted spoken summary when a spoken reply arrives', async () => {
    installFakeSpeechSynthesis()
    vi.stubGlobal('fetch', buildFetch((conversationId) => sseMessageResponse(
      {
        id: 'm1',
        conversation_id: conversationId,
        seq: 1,
        role: 'assistant',
        content: '## Passage plan\n\nDetail.\n\n## Spoken summary\n\nFine tomorrow, light winds.',
        created_at: '2026-09-12T00:00:00Z',
      },
      { id: conversationId, title: 'How does tomorrow look?', created_at: '2026-09-12T00:00:00Z', updated_at: '2026-09-12T00:00:01Z' },
    )))

    render(
      <MateSheet
        open
        onOpenChange={vi.fn()}
        initialQuestion="How does tomorrow look?"
        screen={{ panel: 'forecast' }}
        canWrite
        readAloud
        onOpenPanel={vi.fn()}
      />,
    )

    await screen.findByText(/Fine tomorrow, light winds\./)
    await waitFor(() => expect(fakeSynth.spoken).toHaveLength(1))
    expect(fakeSynth.spoken[0].text).toBe('Fine tomorrow, light winds.')
  })

  it('does not speak when readAloud is off', async () => {
    installFakeSpeechSynthesis()
    vi.stubGlobal('fetch', buildFetch((conversationId) => sseMessageResponse(
      { id: 'm1', conversation_id: conversationId, seq: 1, role: 'assistant', content: '## Spoken summary\n\nFine tomorrow.', created_at: '' },
      { id: conversationId, title: '', created_at: '', updated_at: '' },
    )))

    render(
      <MateSheet
        open
        onOpenChange={vi.fn()}
        initialQuestion="How does tomorrow look?"
        screen={{ panel: 'forecast' }}
        canWrite
        readAloud={false}
        onOpenPanel={vi.fn()}
      />,
    )

    await screen.findByText(/Fine tomorrow\./)
    expect(fakeSynth.spoken).toHaveLength(0)
  })

  it('shows a stop button while speaking, which stops the reading', async () => {
    installFakeSpeechSynthesis()
    vi.stubGlobal('fetch', buildFetch((conversationId) => sseMessageResponse(
      { id: 'm1', conversation_id: conversationId, seq: 1, role: 'assistant', content: '## Spoken summary\n\nFine tomorrow.', created_at: '' },
      { id: conversationId, title: '', created_at: '', updated_at: '' },
    )))

    render(
      <MateSheet
        open
        onOpenChange={vi.fn()}
        initialQuestion="How does tomorrow look?"
        screen={{ panel: 'forecast' }}
        canWrite
        readAloud
        onOpenPanel={vi.fn()}
      />,
    )

    await waitFor(() => expect(fakeSynth.spoken).toHaveLength(1))
    expect(screen.queryByRole('button', { name: 'Stop reading' })).not.toBeInTheDocument()

    fakeSynth.spoken[0].onstart?.()
    expect(await screen.findByRole('button', { name: 'Stop reading' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Stop reading' }))
    fakeSynth.spoken[0].onend?.()

    await waitFor(() => expect(screen.queryByRole('button', { name: 'Stop reading' })).not.toBeInTheDocument())
  })

  it('stops speech when the sheet closes', async () => {
    installFakeSpeechSynthesis()
    const cancelSpy = vi.spyOn(fakeSynth, 'cancel')
    vi.stubGlobal('fetch', buildFetch((conversationId) => sseMessageResponse(
      { id: 'm1', conversation_id: conversationId, seq: 1, role: 'assistant', content: '## Spoken summary\n\nFine tomorrow.', created_at: '' },
      { id: conversationId, title: '', created_at: '', updated_at: '' },
    )))

    const { rerender } = render(
      <MateSheet
        open
        onOpenChange={vi.fn()}
        initialQuestion="How does tomorrow look?"
        screen={{ panel: 'forecast' }}
        canWrite
        readAloud
        onOpenPanel={vi.fn()}
      />,
    )
    await waitFor(() => expect(fakeSynth.spoken).toHaveLength(1))
    fakeSynth.spoken[0].onstart?.()

    rerender(
      <MateSheet
        open={false}
        onOpenChange={vi.fn()}
        screen={{ panel: 'forecast' }}
        canWrite
        readAloud
        onOpenPanel={vi.fn()}
      />,
    )

    expect(cancelSpy).toHaveBeenCalled()
  })
})
