import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { act, render, screen, fireEvent, waitFor, within } from '@testing-library/react'

import { AssistantThread } from '@/components/assistant-thread'
import type { AssistantMessage } from '@/hooks/use-assistant-conversations'
import type { useAssistantChat } from '@/hooks/use-assistant-chat'
import type { useAssistantConversations } from '@/hooks/use-assistant-conversations'
import { useNotes } from '@/hooks/use-notes'
import { type useMateTelemetryWatch } from '@/hooks/use-mate-telemetry-watch'
import { MATE_WAITING_PHRASES } from '@/lib/mate-waiting-phrases'

// The thread creates a note directly (no sheet) when the operator saves an
// answer - Mate is often itself a sheet, so opening the capture sheet over
// it would stack two, and capture is meant to cost nothing anyway.
vi.mock('@/hooks/use-notes')
const mockedUseNotes = vi.mocked(useNotes)
type NotesMock = ReturnType<typeof useNotes>
function makeNotesMock(overrides: Partial<NotesMock> = {}): NotesMock {
  return {
    notes: [],
    loading: false,
    error: null,
    refresh: vi.fn(),
    getNote: vi.fn(),
    createNote: vi.fn(),
    patchNote: vi.fn(),
    ...overrides,
  }
}

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
    startNew: vi.fn(),
    remove: vi.fn(),
    appendLocal: vi.fn(),
    updateProposal: vi.fn(),
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

    expect(screen.getByText('What Mate can check')).toBeInTheDocument()
    expect(screen.getByText('Other vessels nearby')).toBeInTheDocument()
    expect(screen.getByPlaceholderText('Ask Mate')).toBeInTheDocument()
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

    const textarea = screen.getByPlaceholderText('Ask Mate') as HTMLTextAreaElement
    fireEvent.change(textarea, { target: { value: 'What about the wind tomorrow?' } })
    fireEvent.keyDown(textarea, { key: 'Enter' })

    await waitFor(() => expect(send).toHaveBeenCalledWith('c1', 'What about the wind tomorrow?', { onMessage: expect.any(Function) }))
    expect(conversations.appendLocal).toHaveBeenCalledWith(
      expect.objectContaining({ role: 'user', content: 'What about the wind tomorrow?' }),
    )
    // The composer clears once the send is issued.
    await waitFor(() => expect(textarea.value).toBe(''))
  })

  it('reports typed-but-unsent text as a draft, and not once it is sent or cleared', async () => {
    const onHasDraftChange = vi.fn()
    const send = vi.fn().mockResolvedValue(null)
    render(
      <AssistantThread canWrite conversations={buildConversations()} chat={buildChat({ send })} onHasDraftChange={onHasDraftChange} />,
    )
    expect(onHasDraftChange).toHaveBeenLastCalledWith(false)

    const textarea = screen.getByPlaceholderText('Ask Mate')
    fireEvent.change(textarea, { target: { value: 'What about the wind' } })
    expect(onHasDraftChange).toHaveBeenLastCalledWith(true)

    fireEvent.change(textarea, { target: { value: '   ' } })
    expect(onHasDraftChange).toHaveBeenLastCalledWith(false)

    fireEvent.change(textarea, { target: { value: 'Sent one' } })
    fireEvent.keyDown(textarea, { key: 'Enter' })
    await waitFor(() => expect(onHasDraftChange).toHaveBeenLastCalledWith(false))
  })

  it('Shift+Enter does not send', () => {
    const send = vi.fn()
    render(<AssistantThread canWrite conversations={buildConversations()} chat={buildChat({ send })} />)

    const textarea = screen.getByPlaceholderText('Ask Mate') as HTMLTextAreaElement
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

    const textarea = screen.getByPlaceholderText('Ask Mate')
    fireEvent.change(textarea, { target: { value: 'A fresh question' } })
    fireEvent.keyDown(textarea, { key: 'Enter' })

    await waitFor(() => expect(create).toHaveBeenCalled())
    await waitFor(() => expect(send).toHaveBeenCalledWith('new-1', 'A fresh question', { onMessage: expect.any(Function) }))
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

    const textarea = screen.getByPlaceholderText('Ask Mate')
    fireEvent.change(textarea, { target: { value: 'A follow-up' } })
    fireEvent.keyDown(textarea, { key: 'Enter' })

    await waitFor(() => expect(send).toHaveBeenCalledWith('c1', 'A follow-up', { onMessage: expect.any(Function) }))
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

    expect(screen.getByPlaceholderText('Ask Mate')).toBeDisabled()
    expect(screen.getByText('Read-only session')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Send' })).toBeDisabled()
  })

  describe('waiting phrase', () => {
    afterEach(() => {
      vi.useRealTimers()
    })

    const shownPhrase = () => {
      const status = screen.getByRole('status')
      return MATE_WAITING_PHRASES.find((phrase) => status.textContent?.includes(phrase)) ?? null
    }

    it('shows a nautical phrase while waiting with no tool status, and rotates it without repeating', () => {
      vi.useFakeTimers()
      render(<AssistantThread canWrite conversations={buildConversations()} chat={buildChat({ sending: true, statusText: null })} />)

      let previous = shownPhrase()
      expect(previous).not.toBeNull()
      for (let i = 0; i < 6; i++) {
        act(() => {
          vi.advanceTimersByTime(3000)
        })
        const next = shownPhrase()
        expect(next).not.toBeNull()
        expect(next).not.toBe(previous)
        previous = next
      }
    })

    it('shows the tool status as given, with no phrase, when statusText is set', () => {
      vi.useFakeTimers()
      render(
        <AssistantThread
          canWrite
          conversations={buildConversations()}
          chat={buildChat({ sending: true, statusText: 'Looking up Tongue Bay…' })}
        />,
      )

      expect(screen.getByRole('status')).toHaveTextContent('Looking up Tongue Bay…')
      expect(shownPhrase()).toBeNull()
      act(() => {
        vi.advanceTimersByTime(9000)
      })
      expect(screen.getByRole('status')).toHaveTextContent('Looking up Tongue Bay…')
      expect(shownPhrase()).toBeNull()
    })
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

      expect(attach).toHaveBeenCalledWith('c1', undefined, expect.any(Function))
    })

    it('calls chat.attach again when the active conversation switches to a different id', () => {
      const attach = vi.fn().mockResolvedValue(null)
      const chat = buildChat({ attach })
      const { rerender } = render(
        <AssistantThread canWrite conversations={buildConversations({ activeId: 'c1' })} chat={chat} />,
      )
      expect(attach).toHaveBeenCalledWith('c1', undefined, expect.any(Function))

      rerender(<AssistantThread canWrite conversations={buildConversations({ activeId: 'c2' })} chat={chat} />)
      expect(attach).toHaveBeenCalledWith('c2', undefined, expect.any(Function))
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

      expect(attach).toHaveBeenCalledWith('c2', undefined, expect.any(Function))
    })

    it("does not append a reply into a conversation other than the one it answers", async () => {
      let resolveSend!: (message: AssistantMessage | null) => void
      const send = vi.fn((_id: string, _text: string, options?: { onMessage?: (m: AssistantMessage) => void }) =>
        new Promise<AssistantMessage | null>((resolve) => {
          resolveSend = (message) => {
            if (message) options?.onMessage?.(message)
            resolve(message)
          }
        }))
      const chat = buildChat({ send })
      const first = buildConversations({ activeId: 'c1' })
      const { rerender } = render(<AssistantThread canWrite conversations={first} chat={chat} />)

      const textarea = screen.getByPlaceholderText('Ask Mate')
      fireEvent.change(textarea, { target: { value: 'Refuge Cove or Waterloo Bay?' } })
      fireEvent.keyDown(textarea, { key: 'Enter' })
      await waitFor(() => expect(send).toHaveBeenCalledWith('c1', 'Refuge Cove or Waterloo Bay?', { onMessage: expect.any(Function) }))

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
      const attach = vi.fn(async (_id: string, _c?: unknown, onMessage?: (m: AssistantMessage) => void) => {
        onMessage?.(reply)
        return reply
      })
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

    it('reports a draft while an attachment is staged and clears it on unmount', () => {
      const onHasDraftChange = vi.fn()
      const { unmount } = render(
        <AssistantThread canWrite conversations={buildConversations()} chat={buildChat()} onHasDraftChange={onHasDraftChange} />,
      )
      expect(onHasDraftChange).toHaveBeenLastCalledWith(false)

      fireEvent.change(screen.getByTestId('composer-file-input'), {
        target: { files: [new File(['hello'], 'manual.pdf', { type: 'application/pdf' })] },
      })
      expect(onHasDraftChange).toHaveBeenLastCalledWith(true)

      unmount()
      expect(onHasDraftChange).toHaveBeenLastCalledWith(false)
    })

    it('disables Send while an attachment is uploading, with a readable reason, and enables it once indexed', () => {
      render(<AssistantThread canWrite conversations={buildConversations()} chat={buildChat()} />)

      const textarea = screen.getByPlaceholderText('Ask Mate')
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

      const textarea = screen.getByPlaceholderText('Ask Mate')
      fireEvent.change(textarea, { target: { value: 'What is the impeller part number?' } })
      fireEvent.click(screen.getByRole('button', { name: 'Send' }))

      await waitFor(() =>
        expect(send).toHaveBeenCalledWith('c1', 'What is the impeller part number?', { attachments: ['doc-1'], onMessage: expect.any(Function) }),
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

      await waitFor(() => expect(send).toHaveBeenCalledWith('c1', '', { attachments: ['doc-1'], onMessage: expect.any(Function) }))
    })

    // Existing thread tests (above) call send with exactly two arguments for
    // a text-only question - this pins that a staged-but-empty composer
    // still omits the attachments option entirely rather than sending `{}`
    // or `{ attachments: [] }`, the same conditional-inclusion rule
    // spoken/screen already follow in use-assistant-chat.ts.
    it('omits the attachments option entirely when nothing is staged', async () => {
      const send = vi.fn().mockResolvedValue(null)
      render(<AssistantThread canWrite conversations={buildConversations()} chat={buildChat({ send })} />)

      const textarea = screen.getByPlaceholderText('Ask Mate')
      fireEvent.change(textarea, { target: { value: 'Plain question, no attachment' } })
      fireEvent.click(screen.getByRole('button', { name: 'Send' }))

      await waitFor(() => expect(send).toHaveBeenCalledWith('c1', 'Plain question, no attachment', { onMessage: expect.any(Function) }))
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

      const textarea = screen.getByPlaceholderText('Ask Mate')
      fireEvent.change(textarea, { target: { value: 'What is the impeller part number?' } })
      fireEvent.click(screen.getByRole('button', { name: 'Send' }))

      await waitFor(() =>
        expect(send).toHaveBeenCalledWith('c1', 'What is the impeller part number?', { attachments: ['doc-1'], onMessage: expect.any(Function) }),
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
      // ADR 0106 F1: the chip is a real link into the Documents panel's
      // viewer for this document, not inert text.
      const chip = screen.getByText('manual.pdf')
      expect(chip.closest('a')).toHaveAttribute('href', '/documents?document=doc-1')
    })
  })

  // [shadcn 2026-06 chat composer] attach and send now live inside the same
  // bordered InputGroup panel as the textarea, rather than a button row
  // below it - matching upstream's InputGroup + block-end addon composer
  // shape from the 2026-06 chat components changelog. These pin the new
  // structure ahead of the JSX rebuild.
  describe('composer layout (shadcn 2026-06 chat composer)', () => {
    beforeEach(() => {
      FakeXHR.instances = []
      vi.stubGlobal('XMLHttpRequest', FakeXHR)
    })

    afterEach(() => {
      vi.unstubAllGlobals()
    })

    it('puts the textarea, the attach button and the send button inside one input-group panel', () => {
      render(<AssistantThread canWrite conversations={buildConversations()} chat={buildChat()} />)

      const textarea = screen.getByPlaceholderText('Ask Mate')
      const group = textarea.closest('[data-slot="input-group"]')
      expect(group).not.toBeNull()

      const sendButton = screen.getByRole('button', { name: 'Send' })
      const attachButton = screen.getByRole('button', { name: 'Attach files' })
      expect(group as HTMLElement).toContainElement(sendButton)
      expect(group as HTMLElement).toContainElement(attachButton)
    })

    // ADR 0122: dictation inside the composer - DictateButton appends to
    // `content`, never sends, and lives in the same block-end addon as
    // Paperclip/Send.
    describe('dictation (ADR 0122)', () => {
      interface FakeResultAlternative { transcript: string }
      interface FakeResult extends Array<FakeResultAlternative> { isFinal: boolean }

      class FakeSpeechRecognition {
        lang = ''
        continuous = false
        interimResults = false
        onresult: ((event: { resultIndex: number; results: ArrayLike<FakeResult> }) => void) | null = null
        onerror: ((event: { error: string }) => void) | null = null
        onend: (() => void) | null = null
        started = false
        aborted = false
        stopped = false

        constructor() {
          fakeInstances.push(this)
        }

        start() { this.started = true }
        stop() { this.stopped = true }
        abort() { this.aborted = true }

        emitResult(transcript: string, isFinal: boolean) {
          const result: FakeResult = Object.assign([{ transcript }], { isFinal })
          this.onresult?.({ resultIndex: 0, results: [result] })
        }
      }

      let fakeInstances: FakeSpeechRecognition[] = []
      const currentRecognition = () => fakeInstances[fakeInstances.length - 1]

      beforeEach(() => {
        fakeInstances = []
        vi.stubGlobal('SpeechRecognition', FakeSpeechRecognition)
      })

      afterEach(() => {
        vi.unstubAllGlobals()
      })

      it('shows a Dictate button in the same input-group panel as Send, that never sends by itself', () => {
        const send = vi.fn()
        render(<AssistantThread canWrite conversations={buildConversations()} chat={buildChat({ send })} />)

        const textarea = screen.getByPlaceholderText('Ask Mate')
        const group = textarea.closest('[data-slot="input-group"]')
        const dictateButton = screen.getByRole('button', { name: 'Dictate' })
        expect(group as HTMLElement).toContainElement(dictateButton)

        fireEvent.click(dictateButton)
        act(() => { currentRecognition().emitResult('what about the wind tomorrow', true) })

        expect(send).not.toHaveBeenCalled()
        expect(textarea).toHaveValue('what about the wind tomorrow')
      })

      it('is disabled when canWrite is false', () => {
        render(<AssistantThread canWrite={false} conversations={buildConversations()} chat={buildChat()} />)

        expect(screen.getByRole('button', { name: 'Dictate' })).toBeDisabled()
      })

      it('is absent entirely when there is no speech API', () => {
        vi.unstubAllGlobals()
        render(<AssistantThread canWrite conversations={buildConversations()} chat={buildChat()} />)

        expect(screen.queryByRole('button', { name: /dictate/i })).not.toBeInTheDocument()
      })

      // Code review: sending (Enter or the Send button) used to leave an
      // active dictation running - it kept listening after the composer was
      // cleared, so a word recognised after Send landed in the now-empty
      // box as if it were the start of a fresh, unsent message. Send has to
      // cancel dictation itself, before it clears the composer.
      it('cancels dictation at send time, so a result recognised after Send cannot land in the emptied composer', async () => {
        const send = vi.fn().mockResolvedValue(null)
        render(<AssistantThread canWrite conversations={buildConversations()} chat={buildChat({ send })} />)

        const textarea = screen.getByPlaceholderText('Ask Mate') as HTMLTextAreaElement
        fireEvent.change(textarea, { target: { value: 'What about the wind tomorrow?' } })
        fireEvent.click(screen.getByRole('button', { name: 'Dictate' }))
        expect(currentRecognition().started).toBe(true)

        fireEvent.keyDown(textarea, { key: 'Enter' })

        await waitFor(() => expect(send).toHaveBeenCalledWith('c1', 'What about the wind tomorrow?', { onMessage: expect.any(Function) }))
        expect(currentRecognition().aborted).toBe(true)

        act(() => { currentRecognition().emitResult('leftover words', true) })
        expect(textarea.value).toBe('')
      })
    })

    it('puts the staged-attachment chips inside the same input-group panel', () => {
      render(<AssistantThread canWrite conversations={buildConversations()} chat={buildChat()} />)

      const fileInput = screen.getByTestId('composer-file-input')
      fireEvent.change(fileInput, {
        target: { files: [new File(['hello'], 'manual.pdf', { type: 'application/pdf' })] },
      })

      const chips = screen.getByTestId('composer-attachments')
      const textarea = screen.getByPlaceholderText('Ask Mate')
      const group = textarea.closest('[data-slot="input-group"]')
      expect(group).not.toBeNull()
      expect(group as HTMLElement).toContainElement(chips)
    })
  })
})

// "Save as note" on an assistant message: the deliberately small answer to
// what a draft_note tool would have done (plan phase 6, cut). Mate returns
// something worth keeping - a procedure it just walked through, a figure it
// worked out - and this keeps it without retyping, without a write-capable
// tool, and without Mate ever writing anything itself. The operator's tap
// is still what creates the note.
describe('AssistantThread: save an answer as a note', () => {
  it('creates a note from that message and confirms it', async () => {
    const createNote = vi.fn().mockResolvedValue({
      document: { id: 'n-1', title: 'Blue Pearl Bay first, on the flood.' },
      body: 'Blue Pearl Bay first, on the flood.',
    })
    mockedUseNotes.mockReturnValue(makeNotesMock({ createNote }))

    const conversations = buildConversations({ messages: [assistantMessage()] })
    render(<AssistantThread canWrite conversations={conversations} chat={buildChat()} />)

    fireEvent.click(screen.getByRole('button', { name: 'Save as note' }))

    await waitFor(() => {
      expect(createNote).toHaveBeenCalledWith({ body: 'Blue Pearl Bay first, on the flood.' })
    })
  })

  // Every other write affordance in this app is gated the same way, and a
  // read-only viewer being offered a button that 403s is worse than no
  // button at all.
  it('is absent without write permission', () => {
    mockedUseNotes.mockReturnValue(makeNotesMock())
    const conversations = buildConversations({ messages: [assistantMessage()] })

    render(<AssistantThread canWrite={false} conversations={conversations} chat={buildChat()} />)

    expect(screen.queryByRole('button', { name: 'Save as note' })).not.toBeInTheDocument()
  })

  // A user's own message is already theirs - they typed it. The affordance
  // is for Mate's answers.
  it('is not offered on the operator\'s own messages', () => {
    mockedUseNotes.mockReturnValue(makeNotesMock())
    const conversations = buildConversations({
      messages: [assistantMessage({ id: 'u1', role: 'user', content: 'where should we anchor?' })],
    })

    render(<AssistantThread canWrite conversations={conversations} chat={buildChat()} />)

    expect(screen.queryByRole('button', { name: 'Save as note' })).not.toBeInTheDocument()
  })

  describe('change proposals (ADR 0146, ADR 0158)', () => {
    const proposedMessage = (status: 'pending' | 'applied' | 'dismissed' | 'stale') =>
      assistantMessage({
        proposals: [
          {
            id: 'p1',
            messageId: 'm1',
            status,
            staleReason: status === 'stale' ? 'a rule changed since Mate proposed this' : undefined,
            ops: [{ type: 'maintenance_rule', action: 'update', description: 'Acknowledge Generator · Belts: parts on order', href: '/inventory/maintenance' }],
          },
        ],
      })

    it('renders the card under the reply from the stored status, so it survives a reload', () => {
      const applied = render(
        <AssistantThread canWrite conversations={buildConversations({ messages: [proposedMessage('applied')] })} chat={buildChat()} />,
      )
      expect(screen.getByTestId('assistant-proposal-card')).toHaveAttribute('data-status', 'applied')
      expect(screen.getByRole('link', { name: 'Acknowledge Generator · Belts: parts on order' })).toHaveAttribute(
        'href',
        '/inventory/maintenance',
      )
      applied.unmount()

      render(
        <AssistantThread canWrite conversations={buildConversations({ messages: [proposedMessage('pending')] })} chat={buildChat()} />,
      )
      expect(screen.getByTestId('assistant-proposal-card')).toHaveAttribute('data-status', 'pending')
      expect(screen.getByRole('button', { name: 'Apply' })).toBeInTheDocument()
    })

    it('renders a stored stale proposal with its reason and no Apply after a reload', () => {
      render(
        <AssistantThread canWrite conversations={buildConversations({ messages: [proposedMessage('stale')] })} chat={buildChat()} />,
      )

      expect(screen.getByTestId('assistant-proposal-card')).toHaveAttribute('data-status', 'stale')
      expect(screen.getByRole('alert')).toHaveTextContent('a rule changed since Mate proposed this')
      expect(screen.queryByRole('button', { name: 'Apply' })).not.toBeInTheDocument()
    })

    it('hides Apply and Dismiss for a read-tier viewer but still shows the proposal', () => {
      render(
        <AssistantThread canWrite={false} conversations={buildConversations({ messages: [proposedMessage('pending')] })} chat={buildChat()} />,
      )

      expect(screen.getByText('Acknowledge Generator · Belts: parts on order')).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: 'Apply' })).not.toBeInTheDocument()
    })

    it('shows no card on a reply with no proposals', () => {
      render(<AssistantThread canWrite conversations={buildConversations({ messages: [assistantMessage()] })} chat={buildChat()} />)

      expect(screen.queryByTestId('assistant-proposal-card')).not.toBeInTheDocument()
    })
  })
})

describe('AssistantThread: a watch Mate is running (ADR 0160)', () => {
  function buildWatch(
    overrides: Partial<ReturnType<typeof useMateTelemetryWatch>> = {},
  ): ReturnType<typeof useMateTelemetryWatch> {
    return {
      watch: {
        id: 'w1',
        subject: 'Port engine load and Starboard engine load',
        labels: ['Port engine load', 'Starboard engine load'],
        minutes: 5,
        startedAt: '2026-10-04T04:00:00Z',
        endsAt: '2026-10-04T04:05:00Z',
        // Deliberately not 04:05Z on any likely device clock: the chip
        // must show the server's vessel-local time, not its own.
        endsAtLocal: '15:35',
        status: 'watching',
      },
      ended: 0,
      error: null,
      refresh: vi.fn().mockResolvedValue(undefined),
      stop: vi.fn().mockResolvedValue(undefined),
      ...overrides,
    }
  }

  // The end time is the vessel-local clock Mate states, formatted by the
  // server, never this device's clock.
  it('shows what is being watched, when it ends, and a Stop that ends it', () => {
    const watch = buildWatch()
    render(<AssistantThread canWrite conversations={buildConversations()} chat={buildChat()} watch={watch} />)

    const chip = screen.getByTestId('mate-watch-chip')
    expect(chip).toHaveTextContent('Watching Port engine load and Starboard engine load · ends 15:35')
    fireEvent.click(within(chip).getByRole('button', { name: 'Stop watching' }))
    expect(watch.stop).toHaveBeenCalledTimes(1)
  })

  it('offers no Stop to a read-only session', () => {
    render(<AssistantThread canWrite={false} conversations={buildConversations()} chat={buildChat()} watch={buildWatch()} />)

    expect(screen.getByTestId('mate-watch-chip')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Stop watching' })).not.toBeInTheDocument()
  })

  it('says Mate is reading the report once the watch has ended, with no Stop', () => {
    const base = buildWatch()
    const watch = buildWatch({ watch: { ...base.watch!, status: 'reporting' } })
    render(<AssistantThread canWrite conversations={buildConversations()} chat={buildChat()} watch={watch} />)

    expect(screen.getByTestId('mate-watch-chip')).toHaveTextContent('Watch finished. Mate is reading it.')
    expect(screen.queryByRole('button', { name: 'Stop watching' })).not.toBeInTheDocument()
  })

  it('shows no chip with no watch running', () => {
    render(<AssistantThread canWrite conversations={buildConversations()} chat={buildChat()} watch={buildWatch({ watch: null })} />)

    expect(screen.queryByTestId('mate-watch-chip')).not.toBeInTheDocument()
  })

  it('shows a watch report row by its headline only, never the report Mate reads', () => {
    const report = assistantMessage({
      id: 'm2',
      role: 'watch',
      content: 'Watch finished: Port engine load and Starboard engine load (5 min)\n[Automatic watch report]\n{"watch_id":"w1"}',
      model: undefined,
      costUsd: undefined,
    })
    render(<AssistantThread canWrite conversations={buildConversations({ messages: [report] })} chat={buildChat()} />)

    expect(screen.getByText('Watch finished: Port engine load and Starboard engine load (5 min)')).toBeInTheDocument()
    expect(screen.queryByText(/watch_id/)).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Save as note/ })).not.toBeInTheDocument()
  })

  it('rejoins Mate\'s follow-up and reloads the thread when the watch ends', async () => {
    const reply = assistantMessage({ id: 'm9', content: 'Port load spiked twice.' })
    const attach = vi.fn()
      .mockResolvedValueOnce(null)
      .mockImplementationOnce(async (_id: string, _c?: unknown, onMessage?: (m: AssistantMessage) => void) => {
        onMessage?.(reply)
        return reply
      })
    const chat = buildChat({ attach })
    const conversations = buildConversations()
    const { rerender } = render(<AssistantThread canWrite conversations={conversations} chat={chat} watch={buildWatch()} />)
    await waitFor(() => expect(attach).toHaveBeenCalledTimes(1)) // the mount-time rejoin

    rerender(<AssistantThread canWrite conversations={conversations} chat={chat} watch={buildWatch({ watch: null, ended: 1 })} />)

    await waitFor(() => expect(attach).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(conversations.appendLocal).toHaveBeenCalledWith(reply))
    expect(conversations.refresh).toHaveBeenCalled()
  })
})

// The reply must never leave the DOM between the streamed draft and the
// final message: if it does, the content shrinks by a whole reply, the
// browser clamps scrollTop, and the scroller re-anchors from the wrong spot.
describe('reply hand-off from draft to final message', () => {
  it('keeps the reply text on screen across completion of a streamed reply', async () => {
    const { useAssistantChat: realUseAssistantChat } = await vi.importActual<
      typeof import('@/hooks/use-assistant-chat')
    >('@/hooks/use-assistant-chat')
    const { useState, useCallback } = await import('react')

    const replyText = 'Blue Pearl Bay first, on the flood.'
    const enc = new TextEncoder()
    let push!: (chunk: string) => void
    let close!: () => void
    const stream = new ReadableStream<Uint8Array>({
      start(c) {
        push = (chunk) => c.enqueue(enc.encode(chunk))
        close = () => c.close()
      },
    })
    vi.stubGlobal('fetch', vi.fn().mockImplementation(async (url: string) =>
      url.endsWith('/run') ? { ok: true, status: 204, body: null } : { ok: true, status: 200, body: stream },
    ))
    // rAF fires on a real timer in happy-dom; make it immediate for the draft flush.
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
      queueMicrotask(() => cb(0))
      return 1
    })

    function Harness() {
      const [messages, setMessages] = useState<AssistantMessage[]>([])
      const appendLocal = useCallback((m: AssistantMessage) => setMessages((p) => [...p, m]), [])
      const chat = realUseAssistantChat()
      const conversations = buildConversations({ messages, appendLocal })
      return <AssistantThread canWrite conversations={conversations} chat={chat} />
    }

    const { container } = render(<Harness />)
    const gaps: string[] = []
    let sawReply = false
    const observer = new MutationObserver(() => {
      const has = container.textContent?.includes(replyText) ?? false
      if (has) sawReply = true
      else if (sawReply) gaps.push(container.textContent ?? '')
    })
    observer.observe(container, { childList: true, subtree: true, characterData: true })

    fireEvent.change(screen.getByPlaceholderText('Ask Mate'), { target: { value: 'Which bay?' } })
    fireEvent.keyDown(screen.getByPlaceholderText('Ask Mate'), { key: 'Enter' })

    await waitFor(() => expect(fetch).toHaveBeenCalledWith(expect.stringContaining('/messages'), expect.anything()))
    await act(async () => {
      push(`event: delta\ndata: ${JSON.stringify({ text: replyText })}\n\n`)
    })
    await waitFor(() => expect(container.textContent).toContain(replyText))

    const message = {
      id: 'm2', conversation_id: 'c1', seq: 2, role: 'assistant', content: replyText,
      model: 'm', created_at: '2026-10-05T00:00:00Z',
    }
    const conversation = { id: 'c1', title: 't', created_at: '2026-10-05T00:00:00Z', updated_at: '2026-10-05T00:00:00Z' }
    await act(async () => {
      push(`event: message\ndata: ${JSON.stringify({ message, conversation })}\n\n`)
      close()
    })
    await waitFor(() => expect(screen.queryByText('Stop')).not.toBeInTheDocument())
    await act(async () => { await new Promise((r) => setTimeout(r, 20)) })
    observer.disconnect()

    expect(gaps).toEqual([])
    expect(container.textContent).toContain(replyText)
    vi.unstubAllGlobals()
  })
})

// Stop tapped after the final message frame but before the stream closes: the
// answer is already on screen, so it is a delivered answer, not a stopped one.
describe('Stop after the answer has been delivered', () => {
  it('keeps the answer a success: no Stopped line, chips cleared, thread refreshed', async () => {
    const reply = assistantMessage({ id: 'done-1', content: 'Delivered answer.' })
    let finish!: () => void
    const send = vi.fn((_id: string, _text: string, options?: { onMessage?: (m: AssistantMessage) => void }) =>
      new Promise<AssistantMessage | null>((resolve) => {
        options?.onMessage?.(reply)
        // Stop makes the stream end early, so send() resolves null.
        finish = () => resolve(null)
      }))
    const conversations = buildConversations()
    const chat = buildChat({ send, abort: vi.fn() })
    const { rerender } = render(<AssistantThread canWrite conversations={conversations} chat={chat} />)

    const textarea = screen.getByPlaceholderText('Ask Mate')
    fireEvent.change(textarea, { target: { value: 'Which bay?' } })
    fireEvent.keyDown(textarea, { key: 'Enter' })
    await waitFor(() => expect(conversations.appendLocal).toHaveBeenCalledWith(reply))

    const withReply = { ...conversations, messages: [reply] }
    rerender(<AssistantThread canWrite conversations={withReply} chat={{ ...chat, sending: true }} />)
    fireEvent.click(screen.getByRole('button', { name: /stop/i }))
    rerender(<AssistantThread canWrite conversations={withReply} chat={{ ...chat, sending: false }} />)
    await act(async () => { finish() })

    expect(chat.abort).toHaveBeenCalled()
    await waitFor(() => expect(conversations.refresh).toHaveBeenCalled())
    expect(screen.queryByText('Stopped.')).not.toBeInTheDocument()
  })
})
