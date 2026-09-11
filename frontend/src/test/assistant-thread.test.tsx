import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'

import { AssistantThread } from '@/components/assistant-thread'
import type { AssistantMessage } from '@/hooks/use-assistant-conversations'
import type { useAssistantChat } from '@/hooks/use-assistant-chat'
import type { useAssistantConversations } from '@/hooks/use-assistant-conversations'

// AssistantThread (ADR 0093) is a pure view over whatever conversations/chat
// hook results its caller hands it - both AssistantDrawer (the panel) and
// MateSheet (the voice quick-channel) pass their own instances. These tests
// drive it directly with hand-built hook results rather than through fetch,
// mirroring how a component test drives a controlled child.

function buildConversations(
  overrides: Partial<ReturnType<typeof useAssistantConversations>> = {},
): ReturnType<typeof useAssistantConversations> {
  return {
    conversations: [],
    activeId: 'c1',
    messages: [],
    loading: false,
    error: null,
    select: vi.fn(),
    create: vi.fn(),
    remove: vi.fn(),
    appendLocal: vi.fn(),
    refresh: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  }
}

function buildChat(overrides: Partial<ReturnType<typeof useAssistantChat>> = {}): ReturnType<typeof useAssistantChat> {
  return {
    send: vi.fn().mockResolvedValue(null),
    sending: false,
    statusText: null,
    error: null,
    abort: vi.fn(),
    ...overrides,
  }
}

const assistantMessage = (overrides: Partial<AssistantMessage> = {}): AssistantMessage => ({
  id: 'm1',
  conversationId: 'c1',
  seq: 1,
  role: 'assistant',
  content: 'Blue Pearl Bay first, on the flood.',
  model: 'anthropic/claude-sonnet-4.5',
  promptTokens: 1000,
  completionTokens: 200,
  costUsd: 0.0184,
  createdAt: '2026-09-11T00:00:00Z',
  ...overrides,
})

describe('AssistantThread', () => {
  it('shows the empty-thread hint when there are no messages yet', () => {
    render(<AssistantThread canWrite conversations={buildConversations()} chat={buildChat()} />)

    expect(screen.getByText(/Ask Mate: /)).toBeInTheDocument()
    expect(screen.getByPlaceholderText('Ask Mate about the next couple of days…')).toBeInTheDocument()
  })

  it('renders messages with the assistant reply footer', () => {
    const conversations = buildConversations({
      messages: [
        {
          id: 'u1',
          conversationId: 'c1',
          seq: 0,
          role: 'user',
          content: 'Tongue Bay or Blue Pearl Bay first?',
          createdAt: '2026-09-11T00:00:00Z',
        },
        assistantMessage(),
      ],
    })

    render(<AssistantThread canWrite conversations={conversations} chat={buildChat()} />)

    expect(screen.getByText('Tongue Bay or Blue Pearl Bay first?')).toBeInTheDocument()
    expect(screen.getByText('Blue Pearl Bay first, on the flood.')).toBeInTheDocument()
    expect(screen.getByText('anthropic/claude-sonnet-4.5 · 1,200 tokens · $0.0184')).toBeInTheDocument()
  })

  it('Enter sends the composer content through the passed chat.send and appends it locally', async () => {
    const send = vi.fn().mockResolvedValue(null)
    const conversations = buildConversations()

    render(<AssistantThread canWrite conversations={conversations} chat={buildChat({ send })} />)

    const textarea = screen.getByPlaceholderText('Ask Mate about the next couple of days…') as HTMLTextAreaElement
    fireEvent.change(textarea, { target: { value: 'What about the wind tomorrow?' } })
    fireEvent.keyDown(textarea, { key: 'Enter' })

    await waitFor(() => expect(send).toHaveBeenCalledWith('c1', 'What about the wind tomorrow?'))
    expect(conversations.appendLocal).toHaveBeenCalledWith(
      expect.objectContaining({ role: 'user', content: 'What about the wind tomorrow?' }),
    )
    // The composer clears once the send is issued.
    await waitFor(() => expect(textarea.value).toBe(''))
  })

  it('Shift+Enter does not send', () => {
    const send = vi.fn()
    render(<AssistantThread canWrite conversations={buildConversations()} chat={buildChat({ send })} />)

    const textarea = screen.getByPlaceholderText('Ask Mate about the next couple of days…') as HTMLTextAreaElement
    fireEvent.change(textarea, { target: { value: 'Draft in progress' } })
    fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: true })

    expect(send).not.toHaveBeenCalled()
    expect(textarea.value).toBe('Draft in progress')
  })

  it('creates a conversation first when none is active yet, then sends into it', async () => {
    const send = vi.fn().mockResolvedValue(null)
    const create = vi.fn().mockResolvedValue('new-1')
    const conversations = buildConversations({ activeId: null, create })

    render(<AssistantThread canWrite conversations={conversations} chat={buildChat({ send })} />)

    const textarea = screen.getByPlaceholderText('Ask Mate about the next couple of days…')
    fireEvent.change(textarea, { target: { value: 'A fresh question' } })
    fireEvent.keyDown(textarea, { key: 'Enter' })

    await waitFor(() => expect(create).toHaveBeenCalled())
    await waitFor(() => expect(send).toHaveBeenCalledWith('new-1', 'A fresh question'))
  })

  it('shows the status line while sending', () => {
    render(
      <AssistantThread
        canWrite
        conversations={buildConversations()}
        chat={buildChat({ sending: true, statusText: 'Fetching wind forecast for Tongue Bay…' })}
      />,
    )

    expect(screen.getByText('Fetching wind forecast for Tongue Bay…')).toBeInTheDocument()
  })

  it('surfaces a chat or conversations error', () => {
    render(
      <AssistantThread canWrite conversations={buildConversations()} chat={buildChat({ error: 'upstream 401' })} />,
    )

    expect(screen.getByText('upstream 401')).toBeInTheDocument()
  })

  it('disables the composer and shows the read-only hint when canWrite is false', () => {
    render(<AssistantThread canWrite={false} conversations={buildConversations()} chat={buildChat()} />)

    expect(screen.getByPlaceholderText('Ask Mate about the next couple of days…')).toBeDisabled()
    expect(screen.getByText('Read-only session')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Send' })).toBeDisabled()
  })
})
