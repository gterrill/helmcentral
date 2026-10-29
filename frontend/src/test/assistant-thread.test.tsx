import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { act, render, screen, fireEvent, waitFor, within } from '@testing-library/react'

import { AssistantThread } from '@/components/assistant-thread'
import type { AssistantMessage } from '@/hooks/use-assistant-conversations'
import { useNotes } from '@/hooks/use-notes'
import { type useMateTelemetryWatch } from '@/hooks/use-mate-telemetry-watch'
import { MATE_WAITING_PHRASES } from '@/lib/mate-waiting-phrases'
import type { MateSession } from '@/hooks/use-mate-session'
import { fakeMateSession } from './helpers/fake-mate-session'

// The thread creates a note directly (no sheet) when the operator saves an
// answer - Mate is often itself a sheet, so opening the capture sheet over
// it would stack two, and capture is meant to cost nothing anyway.
vi.mock('@/hooks/use-notes')
// Spy on the scroller's scrollToMessage while keeping the real primitive.
const { scrollToMessageSpy } = vi.hoisted(() => ({ scrollToMessageSpy: vi.fn() }))
vi.mock('@/components/ui/message-scroller', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/components/ui/message-scroller')>()
  return {
    ...actual,
    useMessageScroller: () => {
      const real = actual.useMessageScroller()
      return {
        ...real,
        scrollToMessage: (...args: Parameters<typeof real.scrollToMessage>) => {
          scrollToMessageSpy(...args)
          return real.scrollToMessage(...args)
        },
      }
    },
  }
})
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

// AssistantThread (ADR 0093) is a view over the Mate session its caller
// hands it - AssistantDrawer (the panel) and MateSheet (the voice
// quick-channel) each pass their own. These tests drive it with a fake
// session; what a session does with a question is use-mate-session.test.ts.

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
    render(<AssistantThread canWrite session={fakeMateSession()} />)

    expect(screen.getByText('What Mate can check')).toBeInTheDocument()
    expect(screen.getByText('Other vessels nearby')).toBeInTheDocument()
    expect(screen.getByPlaceholderText('Ask Mate')).toBeInTheDocument()
  })

  it('renders messages with the assistant reply footer, cost first and mechanics in a tooltip', async () => {
    const session = fakeMateSession({
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

    render(<AssistantThread canWrite session={session} />)

    expect(screen.getByText('Tongue Bay or Blue Pearl Bay first?')).toBeInTheDocument()
    // Assistant content renders through the now-lazy AssistantMarkdown.
    expect(await screen.findByText('Blue Pearl Bay first, on the flood.')).toBeInTheDocument()
    // Cost leads, to 3 decimal places, then the model, then the tool-round count.
    const footer = screen.getByText('$0.018 · anthropic/claude-sonnet-4.5 · 1 tool round')
    expect(footer).toBeInTheDocument()
    // The tooltip still carries the model+token details for quick hover inspection.
    expect(footer).toHaveAttribute('title', 'anthropic/claude-sonnet-4.5 · 1,200 tokens')
  })

  it('shows how many prompt tokens came from the cache, only when there were some', () => {
    const cached = fakeMateSession({ messages: [assistantMessage({ cachedTokens: 9820 })] })
    const { unmount } = render(<AssistantThread canWrite session={cached} />)
    const footer = screen.getByText('$0.018 · anthropic/claude-sonnet-4.5 · 9,820 cached')
    expect(footer).toHaveAttribute('title', 'anthropic/claude-sonnet-4.5 · 1,200 tokens · 9,820 cached')
    unmount()

    const none = fakeMateSession({ messages: [assistantMessage({ cachedTokens: 0 })] })
    render(<AssistantThread canWrite session={none} />)
    expect(screen.getByText('$0.018 · anthropic/claude-sonnet-4.5')).toBeInTheDocument()
  })

  it('omits the tool-round part of the footer when tool_rounds was not reported', () => {
    const session = fakeMateSession({ messages: [assistantMessage()] })

    render(<AssistantThread canWrite session={session} />)

    expect(screen.getByText('$0.018 · anthropic/claude-sonnet-4.5')).toBeInTheDocument()
  })

  it('pluralises multiple tool rounds', () => {
    const session = fakeMateSession({ messages: [assistantMessage({ toolRounds: 2 })] })

    render(<AssistantThread canWrite session={session} />)

    expect(screen.getByText('$0.018 · anthropic/claude-sonnet-4.5 · 2 tool rounds')).toBeInTheDocument()
  })

  it('renders -- for every footer field the server did not report', () => {
    const session = fakeMateSession({
      messages: [
        assistantMessage({ model: undefined, promptTokens: undefined, completionTokens: undefined, costUsd: undefined, toolRounds: undefined }),
      ],
    })

    render(<AssistantThread canWrite session={session} />)

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
    const session = fakeMateSession({
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

    render(<AssistantThread canWrite session={session} />)

    const content = screen.getByText('Tongue Bay or Blue Pearl Bay first?')
    const bubble = content.closest('[data-slot="bubble"]')
    expect(bubble).not.toBeNull()
    expect(bubble).toHaveAttribute('data-variant', 'secondary')
    // whitespace-pre-wrap still lives directly on the content node.
    expect(content.className).toEqual(expect.stringContaining('whitespace-pre-wrap'))
  })

  it('Enter sends the composer content through the session and clears the composer', async () => {
    const session = fakeMateSession()

    render(<AssistantThread canWrite session={session} />)

    const textarea = screen.getByPlaceholderText('Ask Mate') as HTMLTextAreaElement
    fireEvent.change(textarea, { target: { value: 'What about the wind tomorrow?' } })
    fireEvent.keyDown(textarea, { key: 'Enter' })

    await waitFor(() => expect(session.send).toHaveBeenCalledWith('What about the wind tomorrow?', { attachments: [] }))
    // The composer clears once the send is issued.
    await waitFor(() => expect(textarea.value).toBe(''))
  })

  it('puts a refused question back in the composer', async () => {
    const session = fakeMateSession({ send: vi.fn().mockResolvedValue({ answered: false, message: null, refused: 'busy' }) })

    render(<AssistantThread canWrite session={session} />)

    const textarea = screen.getByPlaceholderText('Ask Mate') as HTMLTextAreaElement
    fireEvent.change(textarea, { target: { value: 'And the wind?' } })
    fireEvent.keyDown(textarea, { key: 'Enter' })

    await waitFor(() => expect(session.send).toHaveBeenCalled())
    await waitFor(() => expect(textarea.value).toBe('And the wind?'))
  })

  it('reports typed-but-unsent text as a draft, and not once it is sent or cleared', async () => {
    const onHasDraftChange = vi.fn()
    render(
      <AssistantThread canWrite session={fakeMateSession()} onHasDraftChange={onHasDraftChange} />,
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
    const session = fakeMateSession()
    render(<AssistantThread canWrite session={session} />)

    const textarea = screen.getByPlaceholderText('Ask Mate') as HTMLTextAreaElement
    fireEvent.change(textarea, { target: { value: 'Draft in progress' } })
    fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: true })

    expect(session.send).not.toHaveBeenCalled()
    expect(textarea.value).toBe('Draft in progress')
  })

  it('does not send while an answer is still streaming', () => {
    const session = fakeMateSession({ status: { sending: true } })
    render(<AssistantThread canWrite session={session} />)

    const textarea = screen.getByPlaceholderText('Ask Mate')
    fireEvent.change(textarea, { target: { value: 'And the wind?' } })
    fireEvent.keyDown(textarea, { key: 'Enter' })

    expect(session.send).not.toHaveBeenCalled()
    expect(screen.getByRole('button', { name: 'Send' })).toBeDisabled()
  })

  it('shows the status line while sending', () => {
    render(
      <AssistantThread
        canWrite
        session={fakeMateSession({ status: { sending: true, statusText: 'Fetching wind forecast for Tongue Bay…' } })}
      />,
    )

    expect(screen.getByText('Fetching wind forecast for Tongue Bay…')).toBeInTheDocument()
  })

  // [P1, impeccable critique 2026-09-12] there was no way for the operator
  // to cancel a question in flight themselves.
  it('shows a Stop button while sending, which stops the session', () => {
    const session = fakeMateSession({ status: { sending: true, statusText: 'Fetching wind forecast for Tongue Bay…' } })
    render(<AssistantThread canWrite session={session} />)

    fireEvent.click(screen.getByRole('button', { name: 'Stop asking' }))
    expect(session.stop).toHaveBeenCalledTimes(1)
  })

  it('says Stopped. once the session has stopped an answer, and no Stop button', () => {
    render(<AssistantThread canWrite session={fakeMateSession({ status: { stopped: true } })} />)

    expect(screen.queryByRole('button', { name: 'Stop asking' })).not.toBeInTheDocument()
    expect(screen.getByText('Stopped.')).toBeInTheDocument()
  })

  it('does not show the Stop button or the Stopped. line while idle', () => {
    render(<AssistantThread canWrite session={fakeMateSession()} />)

    expect(screen.queryByRole('button', { name: 'Stop asking' })).not.toBeInTheDocument()
    expect(screen.queryByText('Stopped.')).not.toBeInTheDocument()
  })

  it('surfaces a chat error as-is - it already carries server-provided text', () => {
    render(
      <AssistantThread canWrite session={fakeMateSession({ status: { error: 'upstream 401' } })} />,
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
        session={fakeMateSession({
          errorMessage:
            "Mate's conversations could not be loaded. The server answered 502. This is usually the boat's link dropping for a moment.",
        })}
      />,
    )

    expect(screen.getByText(/Mate's conversations could not be loaded/)).toBeInTheDocument()
    expect(screen.queryByText('HTTP 502')).not.toBeInTheDocument()
  })

  it('disables the composer and shows the read-only hint when canWrite is false', () => {
    render(<AssistantThread canWrite={false} session={fakeMateSession()} />)

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
      render(<AssistantThread canWrite session={fakeMateSession({ status: { sending: true, statusText: null } })} />)

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
          session={fakeMateSession({ status: { sending: true, statusText: 'Looking up Tongue Bay…' } })}
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
      const session = fakeMateSession({
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
          session={{ ...session, status: { ...session.status, sending: true, statusText: 'Thinking…', draft: 'A bowline forms a fixed loop' } }}
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
      const session = fakeMateSession({ messages: [] })

      const { rerender } = render(
        <AssistantThread
          canWrite
          session={{ ...session, status: { ...session.status, sending: true, statusText: 'Working out the answer…', draft: 'Let me check the wind' } }}
        />,
      )

      expect(screen.queryByText('Working out the answer…')).not.toBeInTheDocument()

      // A retract clears the draft (use-assistant-chat.ts sets it back to
      // null) while the run is still going - the loop moved on to tool
      // calls.
      rerender(
        <AssistantThread
          canWrite
          session={{ ...session, status: { ...session.status, sending: true, statusText: 'Fetching wind forecast…', draft: null } }}
        />,
      )

      expect(screen.getByText('Fetching wind forecast…')).toBeInTheDocument()
      expect(screen.queryByText(/Let me check the wind/)).not.toBeInTheDocument()
    })

    it('does not render a footer on the in-progress draft item', () => {
      render(
        <AssistantThread
          canWrite
          session={fakeMateSession({ status: { sending: true, draft: 'Partial answer, still writing' } })}
        />,
      )

      const draftText = screen.getByText('Partial answer, still writing')
      const message = draftText.closest('[data-slot="message"]')
      expect(message).not.toBeNull()
      expect(within(message as HTMLElement).queryByText(/·/)).not.toBeInTheDocument()
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
        <AssistantThread canWrite session={fakeMateSession()} onHasDraftChange={onHasDraftChange} />,
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
      render(<AssistantThread canWrite session={fakeMateSession()} />)

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

    it('sends the staged attachments through the session, alongside the typed question', async () => {
      const session = fakeMateSession()
      render(<AssistantThread canWrite session={session} />)

      const fileInput = screen.getByTestId('composer-file-input')
      fireEvent.change(fileInput, { target: { files: [new File(['hello'], 'manual.pdf', { type: 'application/pdf' })] } })
      resolveUpload(FakeXHR.instances[0], { documentId: 'doc-1' })

      const textarea = screen.getByPlaceholderText('Ask Mate')
      fireEvent.change(textarea, { target: { value: 'What is the impeller part number?' } })
      fireEvent.click(screen.getByRole('button', { name: 'Send' }))

      await waitFor(() =>
        expect(session.send).toHaveBeenCalledWith('What is the impeller part number?', {
          attachments: [{ documentId: 'doc-1', filename: 'manual.pdf' }],
        }),
      )
    })

    // The session says the answer arrived even when the operator has moved
    // to another conversation (its `message` is then null): the chips that
    // went with the question are spent either way, and would otherwise ride
    // along with the next question there.
    it('clears the sent chips when the answer lands after the operator moved to another conversation', async () => {
      let deliver!: () => void
      const send = vi.fn(() => new Promise((resolve) => {
        deliver = () => resolve({ answered: true, message: null })
      }))
      const session = fakeMateSession({ activeId: 'c1', send } as Partial<MateSession>)
      const { rerender } = render(<AssistantThread canWrite session={session} />)

      fireEvent.change(screen.getByTestId('composer-file-input'), { target: { files: [new File(['hello'], 'manual.pdf', { type: 'application/pdf' })] } })
      resolveUpload(FakeXHR.instances[0], { documentId: 'doc-1' })
      expect(await screen.findByText('manual.pdf')).toBeInTheDocument()
      fireEvent.change(screen.getByPlaceholderText('Ask Mate'), { target: { value: 'Part number?' } })
      fireEvent.click(screen.getByRole('button', { name: 'Send' }))
      await waitFor(() => expect(send).toHaveBeenCalled())

      rerender(<AssistantThread canWrite session={{ ...session, activeId: 'c2' }} />)
      await act(async () => { deliver() })

      await waitFor(() => expect(screen.queryByText('manual.pdf')).not.toBeInTheDocument())
    })

    it('keeps a chip staged while the answer was streaming and drops only the sent one', async () => {
      let deliver!: () => void
      const send = vi.fn(() => new Promise((resolve) => {
        deliver = () => resolve({ answered: true, message: assistantMessage({ id: 'late-2' }) })
      }))
      render(<AssistantThread canWrite session={fakeMateSession({ send } as Partial<MateSession>)} />)

      fireEvent.change(screen.getByTestId('composer-file-input'), { target: { files: [new File(['hello'], 'manual.pdf', { type: 'application/pdf' })] } })
      resolveUpload(FakeXHR.instances[0], { documentId: 'doc-1' })
      expect(await screen.findByText('manual.pdf')).toBeInTheDocument()
      fireEvent.change(screen.getByPlaceholderText('Ask Mate'), { target: { value: 'Part number?' } })
      fireEvent.click(screen.getByRole('button', { name: 'Send' }))
      await waitFor(() => expect(send).toHaveBeenCalled())

      // A second file is staged for the next question while this one streams.
      fireEvent.change(screen.getByTestId('composer-file-input'), { target: { files: [new File(['world'], 'wiring.pdf', { type: 'application/pdf' })] } })
      resolveUpload(FakeXHR.instances[1], { documentId: 'doc-2' })
      expect(await screen.findByText('wiring.pdf')).toBeInTheDocument()

      await act(async () => { deliver() })

      await waitFor(() => expect(screen.queryByText('manual.pdf')).not.toBeInTheDocument())
      expect(screen.getByText('wiring.pdf')).toBeInTheDocument()
    })

    it('keeps the chips staged when no answer came, so the operator can retry', async () => {
      const session = fakeMateSession()
      render(<AssistantThread canWrite session={session} />)

      fireEvent.change(screen.getByTestId('composer-file-input'), { target: { files: [new File(['hello'], 'manual.pdf', { type: 'application/pdf' })] } })
      resolveUpload(FakeXHR.instances[0], { documentId: 'doc-1' })
      fireEvent.click(screen.getByRole('button', { name: 'Send' }))

      await waitFor(() => expect(session.send).toHaveBeenCalled())
      expect(screen.getByText('manual.pdf')).toBeInTheDocument()
    })

    it('sends with an attachment and no text typed', async () => {
      const session = fakeMateSession()
      render(<AssistantThread canWrite session={session} />)

      const fileInput = screen.getByTestId('composer-file-input')
      fireEvent.change(fileInput, { target: { files: [new File(['hello'], 'manual.pdf', { type: 'application/pdf' })] } })
      resolveUpload(FakeXHR.instances[0], { documentId: 'doc-1' })

      expect(screen.getByRole('button', { name: 'Send' })).not.toBeDisabled()
      fireEvent.click(screen.getByRole('button', { name: 'Send' }))

      await waitFor(() => expect(session.send).toHaveBeenCalledWith('', { attachments: [{ documentId: 'doc-1', filename: 'manual.pdf' }] }))
    })

    // ADR 0106 F2 follow-up: uploads dedupe by sha256 server-side, so
    // attaching the same file twice (or two files with identical content)
    // can resolve to the same document id. use-document-uploads.ts collapses
    // the second chip rather than staging a duplicate - this pins that the
    // composer only ever sends one copy of that id, not [X, X], which the
    // backend rejects with 400 "duplicate attachment".
    it('sends one attachment when the same document is staged twice', async () => {
      const session = fakeMateSession()
      render(<AssistantThread canWrite session={session} />)

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
        expect(session.send).toHaveBeenCalledWith('What is the impeller part number?', {
          attachments: [{ documentId: 'doc-1', filename: 'manual.pdf' }],
        }),
      )
    })

    it('shows a staged chip and removes it on request', () => {
      render(<AssistantThread canWrite session={fakeMateSession()} />)

      const fileInput = screen.getByTestId('composer-file-input')
      fireEvent.change(fileInput, { target: { files: [new File(['hello'], 'manual.pdf', { type: 'application/pdf' })] } })

      expect(screen.getByText('manual.pdf')).toBeInTheDocument()
      fireEvent.click(screen.getByRole('button', { name: 'Remove manual.pdf' }))
      expect(screen.queryByText('manual.pdf')).not.toBeInTheDocument()
    })

    it('uploads a file dropped onto the composer', () => {
      render(<AssistantThread canWrite session={fakeMateSession()} />)

      const dropzone = screen.getByTestId('composer-dropzone')
      const file = new File(['hello'], 'manual.pdf', { type: 'application/pdf' })
      fireEvent.drop(dropzone, { dataTransfer: { files: [file] } })

      expect(screen.getByText('manual.pdf')).toBeInTheDocument()
    })

    it("renders a past user message's attachment filenames as chips", () => {
      const session = fakeMateSession({
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

      render(<AssistantThread canWrite session={session} />)

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
      render(<AssistantThread canWrite session={fakeMateSession()} />)

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
        const session = fakeMateSession()
        render(<AssistantThread canWrite session={session} />)

        const textarea = screen.getByPlaceholderText('Ask Mate')
        const group = textarea.closest('[data-slot="input-group"]')
        const dictateButton = screen.getByRole('button', { name: 'Dictate' })
        expect(group as HTMLElement).toContainElement(dictateButton)

        fireEvent.click(dictateButton)
        act(() => { currentRecognition().emitResult('what about the wind tomorrow', true) })

        expect(session.send).not.toHaveBeenCalled()
        expect(textarea).toHaveValue('what about the wind tomorrow')
      })

      it('is disabled when canWrite is false', () => {
        render(<AssistantThread canWrite={false} session={fakeMateSession()} />)

        expect(screen.getByRole('button', { name: 'Dictate' })).toBeDisabled()
      })

      it('is absent entirely when there is no speech API', () => {
        vi.unstubAllGlobals()
        render(<AssistantThread canWrite session={fakeMateSession()} />)

        expect(screen.queryByRole('button', { name: /dictate/i })).not.toBeInTheDocument()
      })

      // Code review: sending (Enter or the Send button) used to leave an
      // active dictation running - it kept listening after the composer was
      // cleared, so a word recognised after Send landed in the now-empty
      // box as if it were the start of a fresh, unsent message. Send has to
      // cancel dictation itself, before it clears the composer.
      it('cancels dictation at send time, so a result recognised after Send cannot land in the emptied composer', async () => {
        const session = fakeMateSession()
        render(<AssistantThread canWrite session={session} />)

        const textarea = screen.getByPlaceholderText('Ask Mate') as HTMLTextAreaElement
        fireEvent.change(textarea, { target: { value: 'What about the wind tomorrow?' } })
        fireEvent.click(screen.getByRole('button', { name: 'Dictate' }))
        expect(currentRecognition().started).toBe(true)

        fireEvent.keyDown(textarea, { key: 'Enter' })

        await waitFor(() => expect(session.send).toHaveBeenCalledWith('What about the wind tomorrow?', { attachments: [] }))
        expect(currentRecognition().aborted).toBe(true)

        act(() => { currentRecognition().emitResult('leftover words', true) })
        expect(textarea.value).toBe('')
      })
    })

    it('puts the staged-attachment chips inside the same input-group panel', () => {
      render(<AssistantThread canWrite session={fakeMateSession()} />)

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

    const session = fakeMateSession({ messages: [assistantMessage()] })
    render(<AssistantThread canWrite session={session} />)

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
    const session = fakeMateSession({ messages: [assistantMessage()] })

    render(<AssistantThread canWrite={false} session={session} />)

    expect(screen.queryByRole('button', { name: 'Save as note' })).not.toBeInTheDocument()
  })

  // A user's own message is already theirs - they typed it. The affordance
  // is for Mate's answers.
  it('is not offered on the operator\'s own messages', () => {
    mockedUseNotes.mockReturnValue(makeNotesMock())
    const session = fakeMateSession({
      messages: [assistantMessage({ id: 'u1', role: 'user', content: 'where should we anchor?' })],
    })

    render(<AssistantThread canWrite session={session} />)

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
        <AssistantThread canWrite session={fakeMateSession({ messages: [proposedMessage('applied')] })} />,
      )
      expect(screen.getByTestId('assistant-proposal-card')).toHaveAttribute('data-status', 'applied')
      expect(screen.getByRole('link', { name: 'Acknowledge Generator · Belts: parts on order' })).toHaveAttribute(
        'href',
        '/inventory/maintenance',
      )
      applied.unmount()

      render(
        <AssistantThread canWrite session={fakeMateSession({ messages: [proposedMessage('pending')] })} />,
      )
      expect(screen.getByTestId('assistant-proposal-card')).toHaveAttribute('data-status', 'pending')
      expect(screen.getByRole('button', { name: 'Apply' })).toBeInTheDocument()
    })

    it('renders a stored stale proposal with its reason and no Apply after a reload', () => {
      render(
        <AssistantThread canWrite session={fakeMateSession({ messages: [proposedMessage('stale')] })} />,
      )

      expect(screen.getByTestId('assistant-proposal-card')).toHaveAttribute('data-status', 'stale')
      expect(screen.getByRole('alert')).toHaveTextContent('a rule changed since Mate proposed this')
      expect(screen.queryByRole('button', { name: 'Apply' })).not.toBeInTheDocument()
    })

    it('hides Apply and Dismiss for a read-tier viewer but still shows the proposal', () => {
      render(
        <AssistantThread canWrite={false} session={fakeMateSession({ messages: [proposedMessage('pending')] })} />,
      )

      expect(screen.getByText('Acknowledge Generator · Belts: parts on order')).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: 'Apply' })).not.toBeInTheDocument()
    })

    it('shows no card on a reply with no proposals', () => {
      render(<AssistantThread canWrite session={fakeMateSession({ messages: [assistantMessage()] })} />)

      expect(screen.queryByTestId('assistant-proposal-card')).not.toBeInTheDocument()
    })
  })
})

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

describe('AssistantThread: a watch Mate is running (ADR 0160)', () => {
  // The end time is the vessel-local clock Mate states, formatted by the
  // server, never this device's clock.
  it('shows what is being watched, when it ends, and a Stop that ends it', () => {
    const watch = buildWatch()
    render(<AssistantThread canWrite session={fakeMateSession()} watch={watch} />)

    const chip = screen.getByTestId('mate-watch-chip')
    expect(chip).toHaveTextContent('Watching Port engine load and Starboard engine load · ends 15:35')
    fireEvent.click(within(chip).getByRole('button', { name: 'Stop watching' }))
    expect(watch.stop).toHaveBeenCalledTimes(1)
  })

  it('offers no Stop to a read-only session', () => {
    render(<AssistantThread canWrite={false} session={fakeMateSession()} watch={buildWatch()} />)

    expect(screen.getByTestId('mate-watch-chip')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Stop watching' })).not.toBeInTheDocument()
  })

  it('says Mate is reading the report once the watch has ended, with no Stop', () => {
    const base = buildWatch()
    const watch = buildWatch({ watch: { ...base.watch!, status: 'reporting' } })
    render(<AssistantThread canWrite session={fakeMateSession()} watch={watch} />)

    expect(screen.getByTestId('mate-watch-chip')).toHaveTextContent('Watch finished. Mate is reading it.')
    expect(screen.queryByRole('button', { name: 'Stop watching' })).not.toBeInTheDocument()
  })

  it('shows no chip with no watch running', () => {
    render(<AssistantThread canWrite session={fakeMateSession()} watch={buildWatch({ watch: null })} />)

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
    render(<AssistantThread canWrite session={fakeMateSession({ messages: [report] })} />)

    expect(screen.getByText('Watch finished: Port engine load and Starboard engine load (5 min)')).toBeInTheDocument()
    expect(screen.queryByText(/watch_id/)).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Save as note/ })).not.toBeInTheDocument()
  })

  it('asks the session to rejoin Mate\'s follow-up when the watch ends', async () => {
    const session = fakeMateSession()
    const { rerender } = render(<AssistantThread canWrite session={session} watch={buildWatch()} />)
    expect(session.rejoin).not.toHaveBeenCalled()

    rerender(<AssistantThread canWrite session={session} watch={buildWatch({ watch: null, ended: 1 })} />)

    await waitFor(() => expect(session.rejoin).toHaveBeenCalledTimes(1))
  })

  it('leaves an answer already streaming alone when the watch ends', () => {
    const session = fakeMateSession({ isAnswering: vi.fn().mockReturnValue(true), status: { sending: true } })
    const { rerender } = render(<AssistantThread canWrite session={session} watch={buildWatch()} />)

    rerender(<AssistantThread canWrite session={session} watch={buildWatch({ watch: null, ended: 1 })} />)

    expect(session.rejoin).not.toHaveBeenCalled()
  })
})

// The reply must never leave the DOM between the streamed draft and the
// final message: if it does, the content shrinks by a whole reply, the
// browser clamps scrollTop, and the scroller re-anchors from the wrong spot.
// Driven through a real session against a fake server, since the hand-off is
// the session's and the thread's together.
describe('reply hand-off from draft to final message', () => {
  it('keeps the reply text on screen across completion of a streamed reply', async () => {
    const { useMateSession } = await vi.importActual<typeof import('@/hooks/use-mate-session')>('@/hooks/use-mate-session')

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
    const conversation = { id: 'c1', title: 't', created_at: '2026-10-05T00:00:00Z', updated_at: '2026-10-05T00:00:00Z' }
    const stored: Array<Record<string, unknown>> = []
    vi.stubGlobal('fetch', vi.fn().mockImplementation(async (url: string, init?: RequestInit) => {
      if (url.endsWith('/run')) return { ok: true, status: 204, body: null }
      if (url.endsWith('/messages') && init?.method === 'POST') return { ok: true, status: 200, body: stream }
      if (url.endsWith('/api/assistant/conversations')) return { ok: true, json: async () => ({ conversations: [conversation] }) }
      return { ok: true, json: async () => ({ conversation, messages: stored }) }
    }))
    // rAF fires on a real timer in happy-dom; make it immediate for the draft flush.
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
      queueMicrotask(() => cb(0))
      return 1
    })

    function Harness() {
      const session = useMateSession({ initialId: 'c1' })
      return <AssistantThread canWrite session={session} />
    }

    const { container } = render(<Harness />)
    await waitFor(() => expect(fetch).toHaveBeenCalledWith(expect.stringContaining('/run'), expect.anything()))
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

    const question = { id: 'u1', conversation_id: 'c1', seq: 1, role: 'user', content: 'Which bay?', created_at: '2026-10-05T00:00:00Z' }
    const message = {
      id: 'm2', conversation_id: 'c1', seq: 2, role: 'assistant', content: replyText,
      model: 'm', created_at: '2026-10-05T00:00:00Z',
    }
    stored.push(question, message)
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

// After a reply is delivered the view rests with the start of that reply at
// the top of the viewport, unless the operator scrolled during streaming.
describe('scroll to the start of a delivered reply', () => {
  beforeEach(() => {
    scrollToMessageSpy.mockClear()
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
      queueMicrotask(() => cb(0))
      return 1
    })
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  const streaming = (session: MateSession, deliveredMessageId: string | null = null): MateSession =>
    ({ ...session, status: { ...session.status, sending: true, deliveredMessageId } })

  it('scrolls the delivered reply to the top of the viewport', async () => {
    const session = fakeMateSession()
    const { rerender } = render(<AssistantThread canWrite session={streaming(session)} />)
    rerender(<AssistantThread canWrite session={streaming(session, 'm9')} />)
    await waitFor(() => expect(scrollToMessageSpy).toHaveBeenCalledWith('m9', expect.objectContaining({ align: 'start' })))
  })

  it('hands the streamed draft\'s row to the delivered reply instead of swapping rows', async () => {
    const session = fakeMateSession({ messages: [assistantMessage({ id: 'm0', content: 'Earlier answer' })] })
    const drafting: MateSession = {
      ...session,
      status: { ...session.status, sending: true, draft: 'Blue Pearl Bay', draftKey: 'draft-1' },
    }
    const { container, rerender } = render(<AssistantThread canWrite session={drafting} />)
    const draftRow = container.querySelector('[data-message-id="draft"]')
    expect(draftRow).not.toBeNull()

    const delivered: MateSession = {
      ...session,
      messages: [...session.messages, assistantMessage({ id: 'm9', content: 'Blue Pearl Bay first.', clientKey: 'draft-1' })],
      status: { ...session.status, sending: true, draft: null, draftKey: 'draft-1', deliveredMessageId: 'm9' },
    }
    rerender(<AssistantThread canWrite session={delivered} />)

    // The scroller re-anchors when its rows are swapped; the same node must
    // carry the reply so it never sees a swap.
    expect(draftRow?.isConnected).toBe(true)
    expect(draftRow?.getAttribute('data-message-id')).toBe('m9')
    expect(container.querySelector('[data-message-id="m9"]')).toBe(draftRow)
  })

  it('does not scroll for a reply that was already delivered when the thread mounted', async () => {
    render(<AssistantThread canWrite session={fakeMateSession({ status: { deliveredMessageId: 'm9' } })} />)
    await act(async () => { await new Promise((r) => setTimeout(r, 20)) })
    expect(scrollToMessageSpy).not.toHaveBeenCalled()
  })

  it('leaves the view alone when the operator grabbed the scrollbar during streaming', async () => {
    const session = fakeMateSession()
    const { rerender } = render(<AssistantThread canWrite session={streaming(session)} />)
    fireEvent.pointerDown(screen.getByTestId('assistant-thread-scroll'))
    rerender(<AssistantThread canWrite session={streaming(session, 'm9')} />)
    await act(async () => { await new Promise((r) => setTimeout(r, 20)) })
    expect(scrollToMessageSpy).not.toHaveBeenCalled()
  })

  it('leaves the view alone when the operator scrolled during streaming', async () => {
    const session = fakeMateSession()
    const { rerender } = render(<AssistantThread canWrite session={streaming(session)} />)
    fireEvent.wheel(screen.getByTestId('assistant-thread-scroll'))
    rerender(<AssistantThread canWrite session={streaming(session, 'm9')} />)
    await act(async () => { await new Promise((r) => setTimeout(r, 20)) })
    expect(scrollToMessageSpy).not.toHaveBeenCalled()
  })

  it('keeps the operator\'s scroll-away when a watch ends while a reply is already streaming', async () => {
    const session = fakeMateSession({ isAnswering: vi.fn(() => true) })
    const { rerender } = render(<AssistantThread canWrite session={streaming(session)} watch={buildWatch()} />)
    fireEvent.wheel(screen.getByTestId('assistant-thread-scroll'))
    rerender(<AssistantThread canWrite session={streaming(session)} watch={buildWatch({ watch: null, ended: 1 })} />)
    rerender(<AssistantThread canWrite session={streaming(session, 'm9')} watch={buildWatch({ watch: null, ended: 1 })} />)
    await act(async () => { await new Promise((r) => setTimeout(r, 20)) })
    expect(scrollToMessageSpy).not.toHaveBeenCalled()
  })

  it('scrolls a rejoined reply after the operator scrolled before switching conversations', async () => {
    const session = fakeMateSession({ activeId: 'c1' })
    const { rerender } = render(<AssistantThread canWrite session={session} />)
    fireEvent.wheel(screen.getByTestId('assistant-thread-scroll'))
    // Switching threads starts afresh: the earlier scroll says nothing about
    // a reply that arrives through a rejoin, which never sets `sending`.
    const c2 = fakeMateSession({ activeId: 'c2' })
    rerender(<AssistantThread canWrite session={c2} />)
    rerender(<AssistantThread canWrite session={{ ...c2, status: { ...c2.status, deliveredMessageId: 'm9' } }} />)
    await waitFor(() => expect(scrollToMessageSpy).toHaveBeenCalledWith('m9', expect.objectContaining({ align: 'start' })))
  })
})

describe('cards under a reply (ADR 0191)', () => {
  it('shows an error entry as a visible error card', () => {
    const session = fakeMateSession({
      messages: [assistantMessage({ content: 'Here you go', cards: [{ kind: 'error', reason: 'message card 0: unknown kind "mystery"', rawKind: 'mystery' }] })],
    })
    render(<AssistantThread canWrite session={session} />)
    expect(screen.getByText(/This card couldn't be shown: message card 0: unknown kind "mystery"/)).toBeInTheDocument()
  })

  it('renders a passage window card', async () => {
    const { passageWindowCard } = await import('./helpers/passage-window-fixture')
    const session = fakeMateSession({ messages: [assistantMessage({ content: 'Here you go', cards: [passageWindowCard()] })] })
    render(<AssistantThread canWrite session={session} />)
    expect(await screen.findByText('Passage window')).toBeInTheDocument()
  })

  it('renders a message without cards as before', () => {
    const session = fakeMateSession({ messages: [assistantMessage({ content: 'Plain answer' })] })
    render(<AssistantThread canWrite session={session} />)
    expect(screen.getByText('Plain answer')).toBeInTheDocument()
    expect(screen.queryByText(/This card couldn't be shown/)).toBeNull()
    expect(screen.queryByText('Passage window')).toBeNull()
  })
})

describe('AssistantThread: rating an answer', () => {
  it('offers thumbs on an answer delivered live, and saves through the session', async () => {
    mockedUseNotes.mockReturnValue(makeNotesMock())
    const rateAnswer = vi.fn().mockResolvedValue(null)
    const session = fakeMateSession({
      messages: [assistantMessage()],
      liveAnswerIds: new Set(['m1']),
      rateAnswer,
    })

    render(<AssistantThread canWrite session={session} />)
    fireEvent.click(screen.getByRole('button', { name: 'Good answer' }))

    await waitFor(() => expect(rateAnswer).toHaveBeenCalledWith('m1', 'up', ''))
  })

  it('shows no thumbs on an answer read back from an earlier visit', () => {
    mockedUseNotes.mockReturnValue(makeNotesMock())
    const session = fakeMateSession({ messages: [assistantMessage()] })

    render(<AssistantThread canWrite session={session} />)

    expect(screen.queryByRole('button', { name: 'Good answer' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Poor answer' })).not.toBeInTheDocument()
  })

  it('shows no thumbs without write permission', () => {
    mockedUseNotes.mockReturnValue(makeNotesMock())
    const session = fakeMateSession({ messages: [assistantMessage()], liveAnswerIds: new Set(['m1']) })

    render(<AssistantThread canWrite={false} session={session} />)

    expect(screen.queryByRole('button', { name: 'Good answer' })).not.toBeInTheDocument()
  })

  it('shows the held verdict as pressed', () => {
    mockedUseNotes.mockReturnValue(makeNotesMock())
    const session = fakeMateSession({
      messages: [assistantMessage()],
      liveAnswerIds: new Set(['m1']),
      ratings: { m1: { verdict: 'down', note: '' } },
    })

    render(<AssistantThread canWrite session={session} />)

    expect(screen.getByRole('button', { name: 'Poor answer' })).toHaveAttribute('aria-pressed', 'true')
  })

  it('shows a rated answer from an earlier visit with its verdict and note, and lets it be changed', async () => {
    mockedUseNotes.mockReturnValue(makeNotesMock())
    const rateAnswer = vi.fn().mockResolvedValue(null)
    const session = fakeMateSession({
      messages: [assistantMessage()],
      ratings: { m1: { verdict: 'down', note: 'wrong tank' } },
      rateAnswer,
    })

    render(<AssistantThread canWrite session={session} />)

    expect(screen.getByRole('button', { name: 'Poor answer' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('textbox', { name: 'What was wrong' })).toHaveValue('wrong tank')
    fireEvent.click(screen.getByRole('button', { name: 'Good answer' }))
    await waitFor(() => expect(rateAnswer).toHaveBeenCalledWith('m1', 'up', 'wrong tank'))
  })

  it('shows the verdict read-only to someone who cannot write', () => {
    mockedUseNotes.mockReturnValue(makeNotesMock())
    const session = fakeMateSession({
      messages: [assistantMessage()],
      ratings: { m1: { verdict: 'up', note: 'right call' } },
    })

    render(<AssistantThread canWrite={false} session={session} />)

    expect(screen.getByRole('button', { name: 'Good answer' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Good answer' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.queryByRole('textbox', { name: 'What was good' })).not.toBeInTheDocument()
    expect(screen.getByText('right call')).toBeInTheDocument()
  })

  it('keeps the controls on an answer after its rating is taken back, so it can be rated again', () => {
    mockedUseNotes.mockReturnValue(makeNotesMock())
    const rated = fakeMateSession({ messages: [assistantMessage()], ratings: { m1: { verdict: 'down', note: '' } } })
    const { rerender } = render(<AssistantThread canWrite session={rated} />)
    expect(screen.getByRole('button', { name: 'Poor answer' })).toBeInTheDocument()

    rerender(<AssistantThread canWrite session={{ ...rated, ratings: {} }} />)

    expect(screen.getByRole('button', { name: 'Good answer' })).toHaveAttribute('aria-pressed', 'false')
    expect(screen.getByRole('button', { name: 'Poor answer' })).toBeInTheDocument()
  })
})
