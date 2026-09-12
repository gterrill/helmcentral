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
    errorMessage: null,
    select: vi.fn(),
    create: vi.fn(),
    remove: vi.fn(),
    appendLocal: vi.fn(),
    refresh: vi.fn().mockResolvedValue(undefined),
    reload: vi.fn().mockResolvedValue(undefined),
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
    expect(screen.getByPlaceholderText('Ask about a passage, an anchorage, or how a panel works…')).toBeInTheDocument()
  })

  it('renders messages with the assistant reply footer, cost first and mechanics in a tooltip', () => {
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
        assistantMessage({ toolRounds: 1 }),
      ],
    })

    render(<AssistantThread canWrite conversations={conversations} chat={buildChat()} />)

    expect(screen.getByText('Tongue Bay or Blue Pearl Bay first?')).toBeInTheDocument()
    expect(screen.getByText('Blue Pearl Bay first, on the flood.')).toBeInTheDocument()
    // Cost leads, to 3 decimal places, then the tool-round count.
    const footer = screen.getByText('$0.018 · 1 tool round')
    expect(footer).toBeInTheDocument()
    // Model and token count move to a title tooltip rather than the visible line.
    expect(footer).toHaveAttribute('title', 'anthropic/claude-sonnet-4.5 · 1,200 tokens')
  })

  it('omits the tool-round part of the footer when tool_rounds was not reported', () => {
    const conversations = buildConversations({ messages: [assistantMessage()] })

    render(<AssistantThread canWrite conversations={conversations} chat={buildChat()} />)

    expect(screen.getByText('$0.018')).toBeInTheDocument()
  })

  it('pluralises multiple tool rounds', () => {
    const conversations = buildConversations({ messages: [assistantMessage({ toolRounds: 2 })] })

    render(<AssistantThread canWrite conversations={conversations} chat={buildChat()} />)

    expect(screen.getByText('$0.018 · 2 tool rounds')).toBeInTheDocument()
  })

  it('renders -- for every footer field the server did not report', () => {
    const conversations = buildConversations({
      messages: [
        assistantMessage({ model: undefined, promptTokens: undefined, completionTokens: undefined, costUsd: undefined, toolRounds: undefined }),
      ],
    })

    render(<AssistantThread canWrite conversations={conversations} chat={buildChat()} />)

    const footer = screen.getByText('--')
    expect(footer).toHaveAttribute('title', '-- · -- tokens')
  })

  // fix(frontend): put cost first in the Mate reply footer and give the
  // operator's bubble a figure (impeccable critique 2026-09-12, Wertheimer
  // 1923 figure-ground) - bg-muted on bg-background was a 2% step, so the
  // operator's own words barely registered as a bubble at all.
  it('gives the user bubble a border and the secondary surface token, so it reads as a figure', () => {
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
      ],
    })

    render(<AssistantThread canWrite conversations={conversations} chat={buildChat()} />)

    const bubble = screen.getByText('Tongue Bay or Blue Pearl Bay first?')
    expect(bubble.className).toEqual(expect.stringContaining('border'))
    expect(bubble.className).toEqual(expect.stringContaining('border-border'))
    expect(bubble.className).toEqual(expect.stringContaining('bg-secondary'))
    expect(bubble.className).not.toEqual(expect.stringContaining('bg-muted'))
  })

  it('Enter sends the composer content through the passed chat.send and appends it locally', async () => {
    const send = vi.fn().mockResolvedValue(null)
    const conversations = buildConversations()

    render(<AssistantThread canWrite conversations={conversations} chat={buildChat({ send })} />)

    const textarea = screen.getByPlaceholderText('Ask about a passage, an anchorage, or how a panel works…') as HTMLTextAreaElement
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

    const textarea = screen.getByPlaceholderText('Ask about a passage, an anchorage, or how a panel works…') as HTMLTextAreaElement
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

    const textarea = screen.getByPlaceholderText('Ask about a passage, an anchorage, or how a panel works…')
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

  // [P1, impeccable critique 2026-09-12] useAssistantChat.abort() already
  // existed (wired only to unmount and a superseding send) but there was no
  // way for the operator to cancel a question in flight themselves.
  it('shows a Stop button while sending, which calls abort and leaves a Stopped. line', () => {
    const abort = vi.fn()
    const { rerender } = render(
      <AssistantThread
        canWrite
        conversations={buildConversations()}
        chat={buildChat({ sending: true, statusText: 'Fetching wind forecast for Tongue Bay…', abort })}
      />,
    )

    const stopButton = screen.getByRole('button', { name: 'Stop asking' })
    fireEvent.click(stopButton)
    expect(abort).toHaveBeenCalledTimes(1)

    // The hook's own abort() sets sending false and leaves error null - the
    // rerender below is what that looks like from the caller's side.
    rerender(
      <AssistantThread
        canWrite
        conversations={buildConversations()}
        chat={buildChat({ sending: false, statusText: null, error: null, abort })}
      />,
    )

    expect(screen.queryByRole('button', { name: 'Stop asking' })).not.toBeInTheDocument()
    expect(screen.getByText('Stopped.')).toBeInTheDocument()
  })

  it('does not show the Stop button or the Stopped. line while idle', () => {
    render(<AssistantThread canWrite conversations={buildConversations()} chat={buildChat()} />)

    expect(screen.queryByRole('button', { name: 'Stop asking' })).not.toBeInTheDocument()
    expect(screen.queryByText('Stopped.')).not.toBeInTheDocument()
  })

  it('clears the Stopped. line once a new question is sent', async () => {
    const send = vi.fn().mockResolvedValue(null)
    const abort = vi.fn()
    const conversations = buildConversations()

    const { rerender } = render(
      <AssistantThread canWrite conversations={conversations} chat={buildChat({ sending: true, send, abort })} />,
    )
    fireEvent.click(screen.getByRole('button', { name: 'Stop asking' }))
    rerender(<AssistantThread canWrite conversations={conversations} chat={buildChat({ sending: false, send, abort })} />)
    expect(screen.getByText('Stopped.')).toBeInTheDocument()

    const textarea = screen.getByPlaceholderText('Ask about a passage, an anchorage, or how a panel works…')
    fireEvent.change(textarea, { target: { value: 'A follow-up' } })
    fireEvent.keyDown(textarea, { key: 'Enter' })

    await waitFor(() => expect(send).toHaveBeenCalledWith('c1', 'A follow-up'))
    expect(screen.queryByText('Stopped.')).not.toBeInTheDocument()
  })

  it('surfaces a chat error as-is - it already carries server-provided text', () => {
    render(
      <AssistantThread canWrite conversations={buildConversations()} chat={buildChat({ error: 'upstream 401' })} />,
    )

    expect(screen.getByText('upstream 401')).toBeInTheDocument()
  })

  // fix(frontend): make a failed Mate load recoverable - the raw `HTTP 502`
  // string used to reach this row unchanged; it now goes through
  // describeLoadError() before AssistantThread ever sees it.
  it('surfaces a conversations load error as its friendly errorMessage, never the raw error', () => {
    render(
      <AssistantThread
        canWrite
        conversations={buildConversations({
          error: 'HTTP 502',
          errorMessage:
            "Mate's conversations could not be loaded. The server answered 502. This is usually the boat's link dropping for a moment.",
        })}
        chat={buildChat()}
      />,
    )

    expect(screen.getByText(/Mate's conversations could not be loaded/)).toBeInTheDocument()
    expect(screen.queryByText('HTTP 502')).not.toBeInTheDocument()
  })

  it('disables the composer and shows the read-only hint when canWrite is false', () => {
    render(<AssistantThread canWrite={false} conversations={buildConversations()} chat={buildChat()} />)

    expect(screen.getByPlaceholderText('Ask about a passage, an anchorage, or how a panel works…')).toBeDisabled()
    expect(screen.getByText('Read-only session')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Send' })).toBeDisabled()
  })
})
