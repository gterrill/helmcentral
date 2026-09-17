import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { act, render, screen, fireEvent, waitFor, within } from '@testing-library/react'

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
    draft: null,
    error: null,
    abort: vi.fn(),
    // ADR 0105: every test below either doesn't care about rejoining a run
    // (the default, a no-op 204-shaped resolution) or overrides this
    // directly - AssistantThread calls this on mount/switch regardless of
    // what a given test is checking.
    attach: vi.fn().mockResolvedValue(null),
    isStreamingConversation: vi.fn().mockReturnValue(false),
    ...overrides,
  }
}

// FakeXHR (ADR 0106 F2): AssistantThread's composer now stages attachments
// through use-document-uploads.ts, which uploads via XMLHttpRequest (not
// fetch) so it can report real progress. Mirrors
// use-document-uploads.test.ts's own fake - see that file for why a fake is
// needed at all rather than mocking fetch.
class FakeXHR {
  static instances: FakeXHR[] = []

  status = 0
  responseText = ''
  upload: { onprogress: ((e: { lengthComputable: boolean; loaded: number; total: number }) => void) | null } = {
    onprogress: null,
  }
  onload: (() => void) | null = null
  onerror: (() => void) | null = null

  constructor() {
    FakeXHR.instances.push(this)
  }

  open() {}
  send() {}
  abort() {}
}

function resolveUpload(
  xhr: FakeXHR,
  overrides: { status?: string; documentId?: string; filename?: string; duplicate?: boolean } = {},
) {
  act(() => {
    xhr.status = 201
    xhr.responseText = JSON.stringify({
      document: {
        id: overrides.documentId ?? 'doc-1',
        filename: overrides.filename ?? 'manual.pdf',
        status: overrides.status ?? 'indexed',
        stage: 'done',
      },
      duplicate: overrides.duplicate ?? false,
    })
    xhr.onload?.()
  })
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

  it('renders messages with the assistant reply footer, cost first and mechanics in a tooltip', async () => {
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
    // Assistant content renders through the now-lazy AssistantMarkdown.
    expect(await screen.findByText('Blue Pearl Bay first, on the flood.')).toBeInTheDocument()
    // Cost leads, to 3 decimal places, then the model, then the tool-round count.
    const footer = screen.getByText('$0.018 · anthropic/claude-sonnet-4.5 · 1 tool round')
    expect(footer).toBeInTheDocument()
    // The tooltip still carries the model+token details for quick hover inspection.
    expect(footer).toHaveAttribute('title', 'anthropic/claude-sonnet-4.5 · 1,200 tokens')
  })

  it('omits the tool-round part of the footer when tool_rounds was not reported', () => {
    const conversations = buildConversations({ messages: [assistantMessage()] })

    render(<AssistantThread canWrite conversations={conversations} chat={buildChat()} />)

    expect(screen.getByText('$0.018 · anthropic/claude-sonnet-4.5')).toBeInTheDocument()
  })

  it('pluralises multiple tool rounds', () => {
    const conversations = buildConversations({ messages: [assistantMessage({ toolRounds: 2 })] })

    render(<AssistantThread canWrite conversations={conversations} chat={buildChat()} />)

    expect(screen.getByText('$0.018 · anthropic/claude-sonnet-4.5 · 2 tool rounds')).toBeInTheDocument()
  })

  it('renders -- for every footer field the server did not report', () => {
    const conversations = buildConversations({
      messages: [
        assistantMessage({ model: undefined, promptTokens: undefined, completionTokens: undefined, costUsd: undefined, toolRounds: undefined }),
      ],
    })

    render(<AssistantThread canWrite conversations={conversations} chat={buildChat()} />)

    const footer = screen.getByText('-- · --')
    expect(footer).toHaveAttribute('title', '-- · -- tokens')
  })

  // fix(frontend): put cost first in the Mate reply footer and give the
  // operator's bubble a figure (impeccable critique 2026-09-12, Wertheimer
  // 1923 figure-ground) - bg-muted on bg-background was a 2% step, so the
  // operator's own words barely registered as a bubble at all. ADR 0105
  // rebuilt the thread on shadcn's Bubble primitive, whose "secondary" vs
  // "muted"/"ghost" visual difference now lives in bubbleVariants rather
  // than in literal utility classes this test can read off the content
  // node directly - so this checks the ancestor Bubble's own `data-variant`
  // instead of raw class strings.
  it('gives the user bubble the secondary variant, so it reads as a figure', () => {
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

    const content = screen.getByText('Tongue Bay or Blue Pearl Bay first?')
    const bubble = content.closest('[data-slot="bubble"]')
    expect(bubble).not.toBeNull()
    expect(bubble).toHaveAttribute('data-variant', 'secondary')
    // whitespace-pre-wrap still lives directly on the content node.
    expect(content.className).toEqual(expect.stringContaining('whitespace-pre-wrap'))
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

  // ADR 0105: the streamed draft renders as its own assistant item inside
  // the message log while a round is still being written, and the status
  // marker (which otherwise reads "Thinking…"/tool activity) steps aside
  // for it - reappearing once a retract clears the draft but the run keeps
  // going (e.g. the round turned into tool calls).
  describe('streamed draft (ADR 0105)', () => {
    it('renders the draft inside the message log while sending, and hides the status marker', () => {
      const conversations = buildConversations({
        messages: [
          {
            id: 'u1',
            conversationId: 'c1',
            seq: 0,
            role: 'user',
            content: 'In one sentence, what is a bowline used for?',
            createdAt: '2026-09-17T00:00:00Z',
          },
        ],
      })

      render(
        <AssistantThread
          canWrite
          conversations={conversations}
          chat={buildChat({ sending: true, statusText: 'Thinking…', draft: 'A bowline forms a fixed loop' })}
        />,
      )

      const log = screen.getByRole('log')
      expect(within(log).getByText(/A bowline forms a fixed loop/)).toBeInTheDocument()
      // The status text steps aside while the draft itself is on screen, but
      // Stop stays: a long answer can still be streaming and spending tokens,
      // and the operator must be able to cut it off mid-sentence.
      expect(screen.queryByText('Thinking…')).not.toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'Stop asking' })).toBeInTheDocument()
    })

    it('shows the status marker again once a retract clears the draft but sending continues', () => {
      const conversations = buildConversations({ messages: [] })

      const { rerender } = render(
        <AssistantThread
          canWrite
          conversations={conversations}
          chat={buildChat({ sending: true, statusText: 'Working out the answer…', draft: 'Let me check the wind' })}
        />,
      )

      expect(screen.queryByText('Working out the answer…')).not.toBeInTheDocument()

      // A retract clears the draft (use-assistant-chat.ts sets it back to
      // null) while the run is still going - the loop moved on to tool
      // calls.
      rerender(
        <AssistantThread
          canWrite
          conversations={conversations}
          chat={buildChat({ sending: true, statusText: 'Fetching wind forecast…', draft: null })}
        />,
      )

      expect(screen.getByText('Fetching wind forecast…')).toBeInTheDocument()
      expect(screen.queryByText(/Let me check the wind/)).not.toBeInTheDocument()
    })

    it('does not render a footer on the in-progress draft item', () => {
      render(
        <AssistantThread
          canWrite
          conversations={buildConversations({ messages: [] })}
          chat={buildChat({ sending: true, draft: 'Partial answer, still writing' })}
        />,
      )

      const draftText = screen.getByText('Partial answer, still writing')
      const message = draftText.closest('[data-slot="message"]')
      expect(message).not.toBeNull()
      expect(within(message as HTMLElement).queryByText(/·/)).not.toBeInTheDocument()
    })
  })

  // ADR 0105 ("the answer outlives the page"): a reply left running when the
  // operator navigated away must be rejoined, not just left for dead, the
  // moment this thread is showing that conversation again - whether that's
  // this component's first mount or the operator switching to a different
  // thread while the panel stays open.
  describe('rejoining a run on mount/switch (ADR 0105)', () => {
    it('calls chat.attach with the active conversation id on mount', () => {
      const attach = vi.fn().mockResolvedValue(null)
      render(<AssistantThread canWrite conversations={buildConversations({ activeId: 'c1' })} chat={buildChat({ attach })} />)

      expect(attach).toHaveBeenCalledWith('c1')
    })

    it('calls chat.attach again when the active conversation switches to a different id', () => {
      const attach = vi.fn().mockResolvedValue(null)
      const chat = buildChat({ attach })
      const { rerender } = render(
        <AssistantThread canWrite conversations={buildConversations({ activeId: 'c1' })} chat={chat} />,
      )
      expect(attach).toHaveBeenCalledWith('c1')

      rerender(<AssistantThread canWrite conversations={buildConversations({ activeId: 'c2' })} chat={chat} />)
      expect(attach).toHaveBeenCalledWith('c2')
    })

    it('does not call chat.attach when there is no active conversation yet', () => {
      const attach = vi.fn().mockResolvedValue(null)
      render(<AssistantThread canWrite conversations={buildConversations({ activeId: null })} chat={buildChat({ attach })} />)

      expect(attach).not.toHaveBeenCalled()
    })

    it('does not call chat.attach while a local send for this conversation is already streaming', () => {
      const attach = vi.fn().mockResolvedValue(null)
      render(
        <AssistantThread
          canWrite
          conversations={buildConversations({ activeId: 'c1' })}
          chat={buildChat({ attach, sending: true, isStreamingConversation: (id: string) => id === 'c1' })}
        />,
      )

      expect(attach).not.toHaveBeenCalled()
    })

    // Code review 2026-09-17: the rejoin used to be skipped whenever
    // anything was streaming, so opening B while A was still answering left
    // B showing A's draft and status, with Stop cancelling A's run.
    it('rejoins the newly opened conversation even while another one is still streaming', () => {
      const attach = vi.fn().mockResolvedValue(null)
      const chat = buildChat({ attach, sending: true, isStreamingConversation: (id: string) => id === 'c1' })
      const { rerender } = render(
        <AssistantThread canWrite conversations={buildConversations({ activeId: 'c1' })} chat={chat} />,
      )
      expect(attach).not.toHaveBeenCalled()

      rerender(<AssistantThread canWrite conversations={buildConversations({ activeId: 'c2' })} chat={chat} />)

      expect(attach).toHaveBeenCalledWith('c2')
    })

    it("does not append a reply into a conversation other than the one it answers", async () => {
      let resolveSend!: (message: AssistantMessage | null) => void
      const send = vi.fn(() => new Promise<AssistantMessage | null>((resolve) => { resolveSend = resolve }))
      const chat = buildChat({ send })
      const first = buildConversations({ activeId: 'c1' })
      const { rerender } = render(<AssistantThread canWrite conversations={first} chat={chat} />)

      const textarea = screen.getByPlaceholderText('Ask about a passage, an anchorage, or how a panel works…')
      fireEvent.change(textarea, { target: { value: 'Refuge Cove or Waterloo Bay?' } })
      fireEvent.keyDown(textarea, { key: 'Enter' })
      await waitFor(() => expect(send).toHaveBeenCalledWith('c1', 'Refuge Cove or Waterloo Bay?'))

      // One conversations hook across renders, as in the app: its
      // appendLocal always writes into whatever thread is active now.
      const second = buildConversations({ activeId: 'c2', appendLocal: first.appendLocal, refresh: first.refresh })
      rerender(<AssistantThread canWrite conversations={second} chat={chat} />)

      await act(async () => {
        resolveSend(assistantMessage({ id: 'late', conversationId: 'c1' }))
      })
      expect(first.appendLocal).not.toHaveBeenCalledWith(expect.objectContaining({ id: 'late' }))
      expect(first.refresh).not.toHaveBeenCalled()
    })

    it('appends the rejoined reply and refreshes the conversation once attach resolves a message', async () => {
      const reply = assistantMessage({ id: 'rejoined-1', content: 'Blue Pearl Bay first, on the flood.' })
      const attach = vi.fn().mockResolvedValue(reply)
      const conversations = buildConversations({ activeId: 'c1' })

      render(<AssistantThread canWrite conversations={conversations} chat={buildChat({ attach })} />)

      await waitFor(() => expect(conversations.appendLocal).toHaveBeenCalledWith(reply))
      await waitFor(() => expect(conversations.refresh).toHaveBeenCalled())
    })
  })

  // ADR 0106 F2: attaching a document to a question, from the composer.
  describe('attachments (ADR 0106 F2)', () => {
    beforeEach(() => {
      FakeXHR.instances = []
      vi.stubGlobal('XMLHttpRequest', FakeXHR)
    })

    afterEach(() => {
      vi.unstubAllGlobals()
    })

    it('disables Send while an attachment is uploading, with a readable reason, and enables it once indexed', () => {
      render(<AssistantThread canWrite conversations={buildConversations()} chat={buildChat()} />)

      const textarea = screen.getByPlaceholderText('Ask about a passage, an anchorage, or how a panel works…')
      fireEvent.change(textarea, { target: { value: 'What does this say about the impeller?' } })
      expect(screen.getByRole('button', { name: 'Send' })).not.toBeDisabled()

      const fileInput = screen.getByTestId('composer-file-input')
      fireEvent.change(fileInput, { target: { files: [new File(['hello'], 'manual.pdf', { type: 'application/pdf' })] } })

      expect(screen.getByText('manual.pdf')).toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'Send' })).toBeDisabled()
      expect(screen.getByText(/Waiting for attachments to finish uploading/)).toBeInTheDocument()

      resolveUpload(FakeXHR.instances[0])

      expect(screen.getByRole('button', { name: 'Send' })).not.toBeDisabled()
      expect(screen.queryByText(/Waiting for attachments to finish uploading/)).not.toBeInTheDocument()
    })

    it('sends the staged attachment ids through chat.send, alongside the typed question', async () => {
      const send = vi.fn().mockResolvedValue(null)
      render(<AssistantThread canWrite conversations={buildConversations()} chat={buildChat({ send })} />)

      const fileInput = screen.getByTestId('composer-file-input')
      fireEvent.change(fileInput, { target: { files: [new File(['hello'], 'manual.pdf', { type: 'application/pdf' })] } })
      resolveUpload(FakeXHR.instances[0], { documentId: 'doc-1' })

      const textarea = screen.getByPlaceholderText('Ask about a passage, an anchorage, or how a panel works…')
      fireEvent.change(textarea, { target: { value: 'What is the impeller part number?' } })
      fireEvent.click(screen.getByRole('button', { name: 'Send' }))

      await waitFor(() =>
        expect(send).toHaveBeenCalledWith('c1', 'What is the impeller part number?', { attachments: ['doc-1'] }),
      )
    })

    it('sends with an attachment and no text typed', async () => {
      const send = vi.fn().mockResolvedValue(null)
      render(<AssistantThread canWrite conversations={buildConversations()} chat={buildChat({ send })} />)

      const fileInput = screen.getByTestId('composer-file-input')
      fireEvent.change(fileInput, { target: { files: [new File(['hello'], 'manual.pdf', { type: 'application/pdf' })] } })
      resolveUpload(FakeXHR.instances[0], { documentId: 'doc-1' })

      expect(screen.getByRole('button', { name: 'Send' })).not.toBeDisabled()
      fireEvent.click(screen.getByRole('button', { name: 'Send' }))

      await waitFor(() => expect(send).toHaveBeenCalledWith('c1', '', { attachments: ['doc-1'] }))
    })

    // Existing thread tests (above) call send with exactly two arguments for
    // a text-only question - this pins that a staged-but-empty composer
    // still omits the attachments option entirely rather than sending `{}`
    // or `{ attachments: [] }`, the same conditional-inclusion rule
    // spoken/screen already follow in use-assistant-chat.ts.
    it('omits the attachments option entirely when nothing is staged', async () => {
      const send = vi.fn().mockResolvedValue(null)
      render(<AssistantThread canWrite conversations={buildConversations()} chat={buildChat({ send })} />)

      const textarea = screen.getByPlaceholderText('Ask about a passage, an anchorage, or how a panel works…')
      fireEvent.change(textarea, { target: { value: 'Plain question, no attachment' } })
      fireEvent.click(screen.getByRole('button', { name: 'Send' }))

      await waitFor(() => expect(send).toHaveBeenCalledWith('c1', 'Plain question, no attachment'))
    })

    // ADR 0106 F2 follow-up: uploads dedupe by sha256 server-side, so
    // attaching the same file twice (or two files with identical content)
    // can resolve to the same document id. use-document-uploads.ts collapses
    // the second chip rather than staging a duplicate - this pins that the
    // composer only ever posts one copy of that id, not [X, X], which the
    // backend rejects with 400 "duplicate attachment".
    it('sends one attachment id when the same document is staged twice', async () => {
      const send = vi.fn().mockResolvedValue(null)
      render(<AssistantThread canWrite conversations={buildConversations()} chat={buildChat({ send })} />)

      const fileInput = screen.getByTestId('composer-file-input')
      fireEvent.change(fileInput, { target: { files: [new File(['hello'], 'manual.pdf', { type: 'application/pdf' })] } })
      resolveUpload(FakeXHR.instances[0], { documentId: 'doc-1' })

      fireEvent.change(fileInput, { target: { files: [new File(['hello'], 'manual.pdf', { type: 'application/pdf' })] } })
      resolveUpload(FakeXHR.instances[1], { documentId: 'doc-1', duplicate: true })

      expect(screen.getAllByText('manual.pdf')).toHaveLength(1)
      expect(screen.getByRole('alert')).toHaveTextContent('"manual.pdf" is already attached.')

      const textarea = screen.getByPlaceholderText('Ask about a passage, an anchorage, or how a panel works…')
      fireEvent.change(textarea, { target: { value: 'What is the impeller part number?' } })
      fireEvent.click(screen.getByRole('button', { name: 'Send' }))

      await waitFor(() =>
        expect(send).toHaveBeenCalledWith('c1', 'What is the impeller part number?', { attachments: ['doc-1'] }),
      )
    })

    it('shows a staged chip and removes it on request', () => {
      render(<AssistantThread canWrite conversations={buildConversations()} chat={buildChat()} />)

      const fileInput = screen.getByTestId('composer-file-input')
      fireEvent.change(fileInput, { target: { files: [new File(['hello'], 'manual.pdf', { type: 'application/pdf' })] } })

      expect(screen.getByText('manual.pdf')).toBeInTheDocument()
      fireEvent.click(screen.getByRole('button', { name: 'Remove manual.pdf' }))
      expect(screen.queryByText('manual.pdf')).not.toBeInTheDocument()
    })

    it('uploads a file dropped onto the composer', () => {
      render(<AssistantThread canWrite conversations={buildConversations()} chat={buildChat()} />)

      const dropzone = screen.getByTestId('composer-dropzone')
      const file = new File(['hello'], 'manual.pdf', { type: 'application/pdf' })
      fireEvent.drop(dropzone, { dataTransfer: { files: [file] } })

      expect(screen.getByText('manual.pdf')).toBeInTheDocument()
    })

    it("renders a past user message's attachment filenames as chips", () => {
      const conversations = buildConversations({
        messages: [
          {
            id: 'u1',
            conversationId: 'c1',
            seq: 0,
            role: 'user',
            content: 'What about the impeller?',
            createdAt: '2026-09-17T00:00:00Z',
            attachments: [{ documentId: 'doc-1', filename: 'manual.pdf' }],
          },
        ],
      })

      render(<AssistantThread canWrite conversations={conversations} chat={buildChat()} />)

      expect(screen.getByText('What about the impeller?')).toBeInTheDocument()
      expect(screen.getByText('manual.pdf')).toBeInTheDocument()
    })
  })
})
