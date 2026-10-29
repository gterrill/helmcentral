import { describe, it, expect, afterEach, vi } from 'vitest'
import { render, screen, fireEvent, waitFor, within, act } from '@testing-library/react'

import { MateSheet } from '@/components/mate-sheet'
import { todayISO } from '@/lib/local-date'
import { controllableSseStream, flushMicrotasks, recordingBody } from './helpers/fake-sse'

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

function buildFetch(
  onSendMessage?: (conversationId: string, body: Record<string, unknown>) => FetchLike,
  initialConversations: ConversationRecord[] = [],
  onCreateConversation?: () => void,
  // ADR 0105: AssistantThread's rejoin effect calls chat.attach() on mount
  // for whatever conversation ends up active, which GETs .../run - every
  // test in this file that reaches a selected conversation hits this route
  // whether or not it cares about rejoining anything. Undefined means "no
  // run in flight", the ordinary case: a plain 204, same as the real
  // backend answers with nothing to rejoin.
  onAttach?: (conversationId: string) => FetchLike,
) {
  const conversations: ConversationRecord[] = [...initialConversations]
  const messagesByConversation = new Map<string, Array<Record<string, unknown>>>()
  let counter = 0
  const store = (conversationId: string, message: Record<string, unknown>) => {
    messagesByConversation.set(conversationId, [...(messagesByConversation.get(conversationId) ?? []), message])
  }
  const recorded = (response: FetchLike, conversationId: string): FetchLike =>
    response.body ? { ...response, body: recordingBody(response.body, (message) => store(conversationId, message)) } : response

  return vi.fn(async (url: string, init?: RequestInit): Promise<FetchLike> => {
    const method = init?.method ?? 'GET'

    if (url.endsWith('/api/assistant/conversations') && method === 'GET') {
      return { ok: true, json: async () => ({ conversations }) }
    }

    if (url.endsWith('/api/assistant/conversations') && method === 'POST') {
      counter += 1
      onCreateConversation?.()
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
      counter += 1
      store(id, { id: `u${counter}`, conversation_id: id, seq: 0, role: 'user', content: body.content, created_at: '' })
      return recorded(onSendMessage(id, body), id)
    }

    // ADR 0160: the thread asks whether a watch is running; none is here.
    if (/\/api\/assistant\/conversations\/[^/]+\/watch$/.test(url) && method === 'GET') {
      return { ok: true, status: 204 }
    }

    const cancelMatch = url.match(/\/api\/assistant\/conversations\/([^/]+)\/run\/cancel$/)
    if (cancelMatch && method === 'POST') {
      return { ok: true, status: 204 }
    }

    const runMatch = url.match(/\/api\/assistant\/conversations\/([^/]+)\/run$/)
    if (runMatch && method === 'GET') {
      const id = decodeURIComponent(runMatch[1])
      if (onAttach) {
        // A rejoined run's frames arrive after the conversation read has
        // landed; an instant answer here would let that read's setMessages([])
        // wipe the frame, an ordering the real server does not produce.
        await new Promise((resolve) => setTimeout(resolve, 20))
        return recorded(onAttach(id), id)
      }
      return { ok: true, status: 204 }
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
    expect(screen.getByPlaceholderText('Ask Mate')).toBeInTheDocument()
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
    const textarea = await screen.findByPlaceholderText('Ask Mate')
    await waitFor(() => expect(textarea).toHaveFocus())
  })

  it('creates a fresh thread before sending an initial question when requested', async () => {
    let postedCreateCalls = 0
    let sentBody: Record<string, unknown> | null = null
    vi.stubGlobal('fetch', buildFetch(
      (conversationId, body) => {
        sentBody = body
        return sseMessageResponse(
          {
            id: 'm1',
            conversation_id: conversationId,
            seq: 1,
            role: 'assistant',
            content: 'Fresh thread answer.',
            created_at: '',
          },
          { id: conversationId, title: 'Fresh thread', created_at: '', updated_at: '' },
        )
      },
      [],
      () => {
        postedCreateCalls += 1
      },
    ))

    render(
      <MateSheet
        open
        onOpenChange={vi.fn()}
        initialQuestion="Why did the logs fail?"
        screen={{ panel: 'forecast' }}
        canWrite
        readAloud={false}
        newConversation
        onOpenPanel={vi.fn()}
      />,
    )

    await waitFor(() => expect(postedCreateCalls).toBeGreaterThan(0))
    // From the Logs page or a text selection: read, not heard.
    await waitFor(() => expect(sentBody).toEqual({ content: 'Why did the logs fail?', today: todayISO(), screen: { panel: 'forecast' } }))
  })

  it('Open the Mate page hands the active conversation to the panel and closes the sheet', async () => {
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

    fireEvent.click(screen.getByRole('button', { name: 'Open the Mate page' }))

    expect(onOpenPanel).toHaveBeenCalledWith('new-1')
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })

  it('sends a voice question at once with spoken:true and the screen', async () => {
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
        spoken
        screen={{ panel: 'forecast' }}
        canWrite
        readAloud={false}
        onOpenPanel={vi.fn()}
      />,
    )

    await waitFor(() => expect(sentBody).toEqual({
      content: 'How does tomorrow look?',
      today: todayISO(),
      spoken: true,
      screen: { panel: 'forecast' },
    }))
    expect(await screen.findByText(/Fine tomorrow, light winds\./)).toBeInTheDocument()
    // The question itself lands as the optimistic user bubble.
    expect(screen.getByText('How does tomorrow look?')).toBeInTheDocument()
  })

  // A typed follow-up is asked from the same page as the first question:
  // Mate needs to know what is on screen for it too (ADR 0167's "this").
  it('sends the screen with a question typed into the composer', async () => {
    let sentBody: Record<string, unknown> | null = null
    vi.stubGlobal('fetch', buildFetch((conversationId, body) => {
      sentBody = body
      return sseMessageResponse(
        { id: 'm1', conversation_id: conversationId, seq: 1, role: 'assistant', content: 'Replace the membrane.', created_at: '' },
        { id: conversationId, title: 'Membrane', created_at: '', updated_at: '' },
      )
    }))
    const screenContext = { panel: 'documents', document_id: 'd7', document_title: 'Watermaker manual' }

    render(<MateSheet open onOpenChange={vi.fn()} screen={screenContext} canWrite readAloud={false} onOpenPanel={vi.fn()} />)

    const textarea = await screen.findByPlaceholderText('Ask Mate')
    fireEvent.change(textarea, { target: { value: 'What does this say about the membrane?' } })
    fireEvent.keyDown(textarea, { key: 'Enter' })

    await waitFor(() => expect(sentBody).toEqual({
      content: 'What does this say about the membrane?',
      today: todayISO(),
      screen: screenContext,
    }))
    expect(await screen.findByText('Replace the membrane.')).toBeInTheDocument()
  })

  // ADR 0226: the alarm an Ask Mate tap names belongs to that first question.
  // Once it is answered the tool result is in the history, so the context is
  // handed back to be dropped, and a New conversation or a search pick drops
  // it too.
  describe('alarm context', () => {
    const answer = (conversationId: string) => sseMessageResponse(
      { id: 'm1', conversation_id: conversationId, seq: 1, role: 'assistant', content: 'It tripped at 49 %.', created_at: '' },
      { id: conversationId, title: 'House Bank Low', created_at: '', updated_at: '' },
    )

    it('is released once the seeded question has been answered', async () => {
      vi.stubGlobal('fetch', buildFetch((id) => answer(id)))
      const onContextDone = vi.fn()
      render(
        <MateSheet open onOpenChange={vi.fn()} initialQuestion="Why did House Bank Low go off?" newConversation
          screen={{ panel: 'alarms', alarm_id: 'r1', alarm_title: 'House Bank Low' }} canWrite readAloud={false}
          onOpenPanel={vi.fn()} onContextDone={onContextDone} />,
      )
      expect(await screen.findByText(/It tripped at 49/)).toBeInTheDocument()
      await waitFor(() => expect(onContextDone).toHaveBeenCalledTimes(1))
    })

    it('is released by New conversation', async () => {
      vi.stubGlobal('fetch', buildFetch())
      const onContextDone = vi.fn()
      render(<MateSheet open onOpenChange={vi.fn()} screen={{ panel: 'alarms' }} canWrite readAloud={false}
        onOpenPanel={vi.fn()} onContextDone={onContextDone} />)
      fireEvent.click(await screen.findByRole('button', { name: 'New conversation' }))
      expect(onContextDone).toHaveBeenCalled()
    })

    it('is released by picking another conversation from search', async () => {
      vi.stubGlobal('fetch', buildFetch(undefined, [{ id: 'c2', title: 'Hook Reef anchorages', created_at: '', updated_at: '' }]))
      const onContextDone = vi.fn()
      render(<MateSheet open onOpenChange={vi.fn()} screen={{ panel: 'alarms' }} canWrite readAloud={false}
        onOpenPanel={vi.fn()} onContextDone={onContextDone} />)
      fireEvent.click(await screen.findByRole('button', { name: 'Search conversations' }))
      await screen.findByRole('textbox', { name: 'Search conversations' })
      fireEvent.click(screen.getByText('Hook Reef anchorages'))
      await waitFor(() => expect(onContextDone).toHaveBeenCalled())
    })
  })

  // A question handed to the sheet while it is still answering (Hey Mate,
  // Help, a selection) is not sent, and not lost either: it waits in the
  // composer for the operator to send once the answer is in.
  it('puts a question that arrives while Mate is still answering into the composer, with the reason', async () => {
    const stream = controllableSseStream()
    let posts = 0
    vi.stubGlobal('fetch', buildFetch(() => {
      posts += 1
      return { ok: true, body: stream.stream }
    }))
    const props = { onOpenChange: vi.fn(), screen: { panel: 'forecast' }, canWrite: true, readAloud: false, onOpenPanel: vi.fn() }

    const { rerender } = render(<MateSheet open {...props} />)
    const textarea = await screen.findByPlaceholderText('Ask Mate') as HTMLTextAreaElement
    fireEvent.change(textarea, { target: { value: 'What about tomorrow?' } })
    fireEvent.keyDown(textarea, { key: 'Enter' })
    await waitFor(() => expect(posts).toBe(1))

    rerender(<MateSheet open {...props} initialQuestion="And the swell?" spoken />)

    await waitFor(() => expect(textarea.value).toBe('And the swell?'))
    expect(screen.getByText('Mate is still answering the last question.')).toBeInTheDocument()
    expect(posts).toBe(1)

    await act(async () => {
      stream.push(`data: ${JSON.stringify({
        message: { id: 'm1', conversation_id: 'new-1', seq: 1, role: 'assistant', content: 'Fine tomorrow.', created_at: '' },
        conversation: { id: 'new-1', title: 'What about tomorrow?', created_at: '', updated_at: '' },
      })}\n\n`)
      stream.close()
      await flushMicrotasks()
    })

    await waitFor(() => expect(screen.queryByText('Mate is still answering the last question.')).not.toBeInTheDocument())
    expect(textarea.value).toBe('And the swell?')
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
  // [P0, impeccable critique 2026-09-12] jsdom does not run layout, so this
  // cannot measure the composer's on-screen pixel position the way the
  // critique did (textarea top at 1261px, off a 1000px-tall viewport). What
  // it can check is the thing that actually produces that bug: the wrapper
  // around AssistantThread used to be a plain block `<div>`, so
  // AssistantThread's own `flex-1 min-h-0 flex-col` chain had no flex parent
  // to size against, and the message list's `overflow-y-auto` never got a
  // bounded height to scroll within - the thread just grew forever and
  // pushed the composer below the fold. This asserts the unbroken
  // flex/min-h-0 chain from the dialog down to the scrolling message list,
  // which is what has to hold for real layout to bound that height.
  //
  // ADR 0105 rebuilt AssistantThread on shadcn's MessageScroller, whose own
  // root is `size-full min-h-0` (a percentage box, not `flex-1`) - it only
  // resolves to a real height because AssistantThread wraps it in a
  // `flex-1 min-h-0` container of its own. "assistant-thread-scroll" now
  // names MessageScroller's Viewport (the element that actually carries
  // `overflow-y-auto`), rather than a hand-rolled scrolling `<div>`; the
  // three assertions below check the same intent as before - bounded height,
  // scrolls - against that new element.
  it('keeps an unbroken flex/min-h-0 chain from the dialog to the scrolling message list', async () => {
    vi.stubGlobal('fetch', buildFetch())

    render(<MateSheet open onOpenChange={vi.fn()} screen={{ panel: 'forecast' }} canWrite readAloud={false} onOpenPanel={vi.fn()} />)

    const dialog = await screen.findByRole('dialog')

    const region = within(dialog).getByTestId('mate-sheet-thread-region')
    expect(region.className).toEqual(expect.stringContaining('flex'))
    expect(region.className).toEqual(expect.stringContaining('min-h-0'))
    expect(region.className).toEqual(expect.stringContaining('flex-1'))
    expect(region.className).toEqual(expect.stringContaining('flex-col'))

    const threadRoot = within(region).getByTestId('assistant-thread-root')
    expect(threadRoot.className).toEqual(expect.stringContaining('flex'))
    expect(threadRoot.className).toEqual(expect.stringContaining('min-h-0'))
    expect(threadRoot.className).toEqual(expect.stringContaining('flex-1'))
    expect(threadRoot.className).toEqual(expect.stringContaining('flex-col'))

    // The MessageScroller primitive itself, between threadRoot and the
    // Viewport: it needs its own flex-1/min-h-0 to turn its `size-full`
    // percentage box into a real, bounded height.
    const messageScroller = within(threadRoot).getByTestId('assistant-thread-message-scroller')
    expect(messageScroller.className).toEqual(expect.stringContaining('flex-1'))
    expect(messageScroller.className).toEqual(expect.stringContaining('min-h-0'))

    const scrollList = within(threadRoot).getByTestId('assistant-thread-scroll')
    expect(scrollList.className).toEqual(expect.stringContaining('flex-1'))
    expect(scrollList.className).toEqual(expect.stringContaining('min-h-0'))
    expect(scrollList.className).toEqual(expect.stringContaining('overflow-y-auto'))
  })

  // [P1, impeccable critique 2026-09-12] the primitive's close control used
  // to be a bare 16 px `X` with no Button wrapper, well under AGENTS.md's
  // 40 px control floor (a moving boat, wet hands). It has to have an
  // accessible name of "Close" (the sr-only text) and carry the same
  // `size="icon"` classes (h-10 w-10 = 40x40) every other icon button in
  // this sheet's header already uses.
  it('gives the close button the 40 px control floor', async () => {
    vi.stubGlobal('fetch', buildFetch())

    render(<MateSheet open onOpenChange={vi.fn()} screen={{ panel: 'forecast' }} canWrite readAloud={false} onOpenPanel={vi.fn()} />)

    const closeButton = await screen.findByRole('button', { name: 'Close' })
    expect(closeButton.className).toEqual(expect.stringContaining('h-10'))
    expect(closeButton.className).toEqual(expect.stringContaining('w-10'))
  })

  // [P1, impeccable critique 2026-09-12] the primitive's overlay used to be
  // a flat 80% black scrim (`bg-black/80`), which dims a live, unacknowledged
  // alarm on the page behind the sheet right when the skipper is heads-down
  // answering a question. A theme-aware token keeps the same fade without
  // going to opaque black.
  // [P2, impeccable critique 2026-09-12] all three header actions used to be
  // aria-label only, with no visible label and no title tooltip - a first
  // timer (persona Jordan) has nothing to go on for three unlabelled icons.
  // [P3, impeccable critique 2026-09-12] SheetTitle's own default is
  // text-lg (18px); DESIGN.md's Title token is 1rem (text-base).
  it('sets the sheet title to the Title token size, not the primitive default', async () => {
    vi.stubGlobal('fetch', buildFetch())

    render(<MateSheet open onOpenChange={vi.fn()} screen={{ panel: 'forecast' }} canWrite readAloud={false} onOpenPanel={vi.fn()} />)

    const title = await screen.findByRole('heading', { name: 'Mate' })
    expect(title.className).toEqual(expect.stringContaining('text-base'))
  })

  it('gives every header action a title tooltip, including read-aloud once it is showing', async () => {
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
        spoken
        screen={{ panel: 'forecast' }}
        canWrite
        readAloud
        onOpenPanel={vi.fn()}
      />,
    )

    expect(await screen.findByRole('button', { name: 'New conversation' })).toHaveAttribute('title', 'New conversation')
    expect(screen.getByRole('button', { name: 'Search conversations' })).toHaveAttribute('title', 'Search conversations')
    expect(screen.getByRole('button', { name: 'Open the Mate page' })).toHaveAttribute('title', 'Open the Mate page')

    await waitFor(() => expect(fakeSynth.spoken).toHaveLength(1))
    fakeSynth.spoken[0].onstart?.()
    expect(await screen.findByRole('button', { name: 'Stop reading' })).toHaveAttribute('title', 'Stop reading')
  })

  it('uses a light theme-aware scrim, not the primitive default 80% black', async () => {
    vi.stubGlobal('fetch', buildFetch())

    render(<MateSheet open onOpenChange={vi.fn()} screen={{ panel: 'forecast' }} canWrite readAloud={false} onOpenPanel={vi.fn()} />)
    await screen.findByRole('dialog')

    // Base UI's Dialog Backdrop renders with role="presentation" and
    // data-open once mounted open - it shares the bare role with an inert
    // background placeholder Base UI also renders, which has no class of
    // its own, so data-open is what picks out the real backdrop.
    const overlay = document.querySelector('[role="presentation"][data-open]')
    expect(overlay).not.toBeNull()
    expect(overlay!.className).not.toEqual(expect.stringContaining('bg-black'))
    expect(overlay!.className).toEqual(expect.stringContaining('bg-background/60'))
  })

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

  // fix(frontend): say which thread the Mate sheet is in and what Mate can
  // do (impeccable critique 2026-09-12, weak scent) - "Mate" alone doesn't
  // say which of several open conversations the sheet is showing. Mate UI
  // cycle ("Mate opens on an empty chat"): a plain open no longer
  // auto-selects an existing conversation, so this now picks one through
  // the search overlay - the sheet's only way to reach a conversation other
  // than whichever one is already current - rather than relying on the
  // sheet happening to already have one active.
  it('names the active conversation under "Mate" in the header, once picked from search', async () => {
    vi.stubGlobal(
      'fetch',
      buildFetch(undefined, [
        { id: 'c1', title: 'Hamilton Island to Gloucester Island, 14 Sep', created_at: '', updated_at: '' },
      ]),
    )

    render(<MateSheet open onOpenChange={vi.fn()} screen={{ panel: 'forecast' }} canWrite readAloud={false} onOpenPanel={vi.fn()} />)

    fireEvent.click(await screen.findByRole('button', { name: 'Search conversations' }))
    fireEvent.click(await screen.findByText('Hamilton Island to Gloucester Island, 14 Sep'))

    // The overlay's own row is gone once it closes, so this is now the
    // sheet's header naming the picked conversation, not the search result.
    await waitFor(() => expect(screen.queryByRole('textbox', { name: 'Search conversations' })).not.toBeInTheDocument())
    expect(screen.getByText('Hamilton Island to Gloucester Island, 14 Sep')).toBeInTheDocument()
  })

  it('shows -- under "Mate" when there is no active thread', async () => {
    vi.stubGlobal('fetch', buildFetch())

    render(<MateSheet open onOpenChange={vi.fn()} screen={{ panel: 'forecast' }} canWrite readAloud={false} onOpenPanel={vi.fn()} />)

    await screen.findByRole('heading', { name: 'Mate' })
    expect(screen.getByText('--')).toBeInTheDocument()
  })

  // Mate UI cycle ("Mate opens on an empty chat"): the header must read "--"
  // even when OTHER conversations exist to pick from - the old behaviour
  // silently auto-selected the newest of them, which is exactly the bug
  // this cycle removes.
  it('shows -- under "Mate" on a plain open even when other conversations exist', async () => {
    vi.stubGlobal(
      'fetch',
      buildFetch(undefined, [
        { id: 'c1', title: 'Hamilton Island to Gloucester Island', created_at: '2026-09-14T00:00:00Z', updated_at: '2026-09-14T00:00:00Z' },
        { id: 'c2', title: 'Hook Reef anchorages', created_at: '2026-09-10T00:00:00Z', updated_at: '2026-09-10T00:00:00Z' },
      ]),
    )

    render(<MateSheet open onOpenChange={vi.fn()} screen={{ panel: 'forecast' }} canWrite readAloud={false} onOpenPanel={vi.fn()} />)

    await screen.findByRole('heading', { name: 'Mate' })
    expect(screen.getByText('--')).toBeInTheDocument()
    // The blank-thread hint (assistant-thread.tsx's "What Mate can check" help) shows -
    // not either conversation's own content, confirming nothing loaded.
    expect(screen.getByText('What Mate can check')).toBeInTheDocument()
  })

  // mate-answer-toast plan: App.tsx needs to know which conversation the
  // sheet is currently showing, to keep the App-level answer watcher from
  // opening a background stream (and toasting) for a reply the sheet is
  // already displaying. Mirrors assistant-drawer.tsx's own prop of the same
  // name and the same "wait on loading" guard. Mate UI cycle: reports null
  // first (the new blank-by-default state), then the picked id once the
  // operator chooses one through search.
  it('reports the active conversation as it settles, once loading is done', async () => {
    const onActiveConversationChange = vi.fn()
    vi.stubGlobal(
      'fetch',
      buildFetch(undefined, [
        { id: 'c1', title: 'Hamilton Island to Gloucester Island, 14 Sep', created_at: '', updated_at: '' },
      ]),
    )

    render(
      <MateSheet
        open
        onOpenChange={vi.fn()}
        screen={{ panel: 'forecast' }}
        canWrite
        readAloud={false}
        onOpenPanel={vi.fn()}
        onActiveConversationChange={onActiveConversationChange}
      />,
    )

    await screen.findByRole('heading', { name: 'Mate' })
    await waitFor(() => expect(onActiveConversationChange).toHaveBeenCalledWith(null))

    fireEvent.click(screen.getByRole('button', { name: 'Search conversations' }))
    fireEvent.click(await screen.findByText('Hamilton Island to Gloucester Island, 14 Sep'))

    await waitFor(() => expect(onActiveConversationChange).toHaveBeenCalledWith('c1'))
  })

  // Mate UI cycle: search the Mate sheet's conversations. The sheet has no
  // list column of its own (that's the whole reason for the sheet existing
  // alongside the full panel) - Search is the sheet's only way to reach a
  // conversation that isn't already its current one.
  describe('search', () => {
    it('opens the search overlay, and picking a conversation loads it into the sheet', async () => {
      vi.stubGlobal(
        'fetch',
        buildFetch(undefined, [
          { id: 'c1', title: 'Hamilton Island to Gloucester Island', created_at: '2026-09-14T00:00:00Z', updated_at: '2026-09-14T00:00:00Z' },
          { id: 'c2', title: 'Hook Reef anchorages', created_at: '2026-09-10T00:00:00Z', updated_at: '2026-09-10T00:00:00Z' },
        ]),
      )

      render(<MateSheet open onOpenChange={vi.fn()} screen={{ panel: 'forecast' }} canWrite readAloud={false} onOpenPanel={vi.fn()} />)

      fireEvent.click(await screen.findByRole('button', { name: 'Search conversations' }))
      const searchBox = await screen.findByRole('textbox', { name: 'Search conversations' })
      fireEvent.click(screen.getByText('Hook Reef anchorages'))

      // The overlay closes (its own search box unmounts - the sheet's own
      // dialog stays open behind it) and the sheet's header now names the
      // picked conversation, so it actually loaded rather than just closing.
      await waitFor(() => expect(searchBox).not.toBeInTheDocument())
      expect(await screen.findByText('Hook Reef anchorages')).toBeInTheDocument()
    })

    it('Esc closes the search overlay without picking anything', async () => {
      vi.stubGlobal(
        'fetch',
        buildFetch(undefined, [
          { id: 'c1', title: 'Hamilton Island to Gloucester Island', created_at: '', updated_at: '' },
        ]),
      )

      render(<MateSheet open onOpenChange={vi.fn()} screen={{ panel: 'forecast' }} canWrite readAloud={false} onOpenPanel={vi.fn()} />)

      fireEvent.click(await screen.findByRole('button', { name: 'Search conversations' }))
      const searchBox = await screen.findByRole('textbox', { name: 'Search conversations' })
      fireEvent.keyDown(searchBox, { key: 'Escape' })

      await waitFor(() => expect(searchBox).not.toBeInTheDocument())
      // The sheet itself is unaffected - only its search overlay closed.
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })
  })

  // Code-review finding: "opening the Mate sheet ALWAYS shows a fresh empty
  // chat". The sheet never truly unmounts once opened (App.tsx's
  // mateSheetHasOpenedRef keeps it mounted, only `open` toggles) - without
  // resetting on each open, whatever conversation the operator last picked
  // (or sent a message into) stayed active across every later close/reopen,
  // for the rest of the browser session.
  describe('reopening', () => {
    it('starts on a fresh, blank chat again, even though a conversation was active before closing', async () => {
      vi.stubGlobal(
        'fetch',
        buildFetch(undefined, [
          { id: 'c1', title: 'Hamilton Island to Gloucester Island', created_at: '2026-09-14T00:00:00Z', updated_at: '2026-09-14T00:00:00Z' },
        ]),
      )

      const { rerender } = render(
        <MateSheet open onOpenChange={vi.fn()} screen={{ panel: 'forecast' }} canWrite readAloud={false} onOpenPanel={vi.fn()} />,
      )

      fireEvent.click(await screen.findByRole('button', { name: 'Search conversations' }))
      fireEvent.click(await screen.findByText('Hamilton Island to Gloucester Island'))
      // findByText alone would still match the (now-closed) search
      // overlay's own row during its closing transition - wait for that to
      // clear first, then confirm the header itself names the conversation.
      await waitFor(() => expect(screen.queryByRole('textbox', { name: 'Search conversations' })).not.toBeInTheDocument())
      expect(screen.getByText('Hamilton Island to Gloucester Island')).toBeInTheDocument()

      rerender(<MateSheet open={false} onOpenChange={vi.fn()} screen={{ panel: 'forecast' }} canWrite readAloud={false} onOpenPanel={vi.fn()} />)
      rerender(<MateSheet open onOpenChange={vi.fn()} screen={{ panel: 'forecast' }} canWrite readAloud={false} onOpenPanel={vi.fn()} />)

      await waitFor(() => expect(screen.getByText('--')).toBeInTheDocument())
      expect(screen.queryByText('Hamilton Island to Gloucester Island')).not.toBeInTheDocument()
      expect(screen.getByText('What Mate can check')).toBeInTheDocument()
    })

    // ADR 0105 ("the answer outlives the page") applied to the sheet's own
    // close/reopen, not just a full remount: the sheet's chat instance keeps
    // running its fetch/SSE reader in the background regardless of whether
    // `open` is true, so a blind reset-on-every-open would wipe the
    // optimistic question bubble and strand the in-flight draft with nothing
    // to explain it, and - worse - the finished reply would never land in
    // `messages` at all (AssistantThread's handleSend only appends it when
    // the conversation it was sent to is still the active one).
    it('does not reset a conversation whose reply is still streaming', async () => {
      const stream = controllableSseStream()
      vi.stubGlobal('fetch', buildFetch(() => ({ ok: true, body: stream.stream })))

      const { rerender } = render(
        <MateSheet open onOpenChange={vi.fn()} screen={{ panel: 'forecast' }} canWrite readAloud={false} onOpenPanel={vi.fn()} />,
      )

      const textarea = await screen.findByPlaceholderText('Ask Mate')
      fireEvent.change(textarea, { target: { value: 'What about tomorrow?' } })
      fireEvent.keyDown(textarea, { key: 'Enter' })

      expect(await screen.findByText('What about tomorrow?')).toBeInTheDocument()

      await act(async () => {
        stream.push('event: delta\ndata: {"text":"Partial answer arriving..."}\n\n')
        await flushMicrotasks()
      })
      expect(await screen.findByText(/Partial answer arriving/)).toBeInTheDocument()

      rerender(<MateSheet open={false} onOpenChange={vi.fn()} screen={{ panel: 'forecast' }} canWrite readAloud={false} onOpenPanel={vi.fn()} />)
      rerender(<MateSheet open onOpenChange={vi.fn()} screen={{ panel: 'forecast' }} canWrite readAloud={false} onOpenPanel={vi.fn()} />)

      // Both the question and the still-arriving draft survive the close/reopen.
      expect(screen.getByText('What about tomorrow?')).toBeInTheDocument()
      expect(screen.getByText(/Partial answer arriving/)).toBeInTheDocument()

      await act(async () => {
        stream.push(`data: ${JSON.stringify({
          message: { id: 'm1', conversation_id: 'new-1', seq: 1, role: 'assistant', content: 'Fine tomorrow, settled.', created_at: '' },
          conversation: { id: 'new-1', title: 'What about tomorrow?', created_at: '', updated_at: '' },
        })}\n\n`)
        stream.close()
        await flushMicrotasks()
      })

      // The finished reply actually lands - it was not dropped because
      // reopening had quietly changed which conversation is "active".
      await waitFor(() => expect(screen.getByText('Fine tomorrow, settled.')).toBeInTheDocument())
    })
  })
})

// fix(frontend): make a failed Mate load recoverable - the sheet has no
// conversation list of its own to fall back on, so a failed initial load
// used to render the raw "HTTP 502" string with no way to try again short
// of reloading the whole page.
describe('MateSheet recoverable load failure', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  function buildAlwaysFailingListFetch(status: number) {
    return vi.fn(async (url: string) => {
      if (url.endsWith('/api/assistant/conversations')) return new Response(null, { status }) as unknown as FetchLike
      throw new Error(`unexpected fetch in this test: ${url}`)
    })
  }

  it('shows a plain-English card in place of the thread, not the raw HTTP string', async () => {
    vi.stubGlobal('fetch', buildAlwaysFailingListFetch(502))

    render(<MateSheet open onOpenChange={vi.fn()} screen={{ panel: 'forecast' }} canWrite readAloud={false} onOpenPanel={vi.fn()} />)

    expect(await screen.findByText("Mate's conversations could not be loaded.")).toBeInTheDocument()
    expect(screen.getByText(/The server answered 502\. This is usually the boat's link dropping for a moment\./)).toBeInTheDocument()
    expect(screen.queryByText(/HTTP 502/)).not.toBeInTheDocument()
  })

  it('gives the card a filled Try again and a ghost Open the Mate page action', async () => {
    vi.stubGlobal('fetch', buildAlwaysFailingListFetch(502))

    render(<MateSheet open onOpenChange={vi.fn()} screen={{ panel: 'forecast' }} canWrite readAloud={false} onOpenPanel={vi.fn()} />)

    const headline = await screen.findByText("Mate's conversations could not be loaded.")
    const card = headline.closest('div')
    expect(card).not.toBeNull()

    const tryAgain = within(card!).getByRole('button', { name: 'Try again' })
    const openPage = within(card!).getByRole('button', { name: 'Open the Mate page' })
    expect(tryAgain.className).toEqual(expect.stringContaining('h-10'))
    expect(openPage.className).toEqual(expect.stringContaining('h-10'))
  })

  it('Try again reloads, and the thread replaces the card once it succeeds', async () => {
    let listShouldFail = true
    const fetchMock = vi.fn(async (url: string) => {
      if (url.endsWith('/api/assistant/conversations')) {
        if (listShouldFail) return { ok: false, status: 502 }
        return { ok: true, json: async () => ({ conversations: [] }) }
      }
      throw new Error(`unexpected fetch in this test: ${url}`)
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<MateSheet open onOpenChange={vi.fn()} screen={{ panel: 'forecast' }} canWrite readAloud={false} onOpenPanel={vi.fn()} />)

    const headline = await screen.findByText("Mate's conversations could not be loaded.")
    const card = headline.closest('div')

    listShouldFail = false
    fireEvent.click(within(card!).getByRole('button', { name: 'Try again' }))

    await waitFor(() => expect(screen.queryByText("Mate's conversations could not be loaded.")).not.toBeInTheDocument())
    expect(await screen.findByPlaceholderText('Ask Mate')).toBeInTheDocument()
  })

  it('Open the Mate page on the card hands the panel null and closes the sheet', async () => {
    const onOpenChange = vi.fn()
    const onOpenPanel = vi.fn()
    vi.stubGlobal('fetch', buildAlwaysFailingListFetch(502))

    render(
      <MateSheet
        open
        onOpenChange={onOpenChange}
        screen={{ panel: 'forecast' }}
        canWrite
        readAloud={false}
        onOpenPanel={onOpenPanel}
      />,
    )

    const headline = await screen.findByText("Mate's conversations could not be loaded.")
    const card = headline.closest('div')

    fireEvent.click(within(card!).getByRole('button', { name: 'Open the Mate page' }))

    expect(onOpenPanel).toHaveBeenCalledWith(null)
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })

  it('reloads the conversation list every time the sheet opens', async () => {
    const fetchMock = buildFetch()
    vi.stubGlobal('fetch', fetchMock)

    const listCalls = () => fetchMock.mock.calls.filter(([url]) => (url as string).endsWith('/api/assistant/conversations')).length

    const { rerender } = render(
      <MateSheet open={false} onOpenChange={vi.fn()} screen={{ panel: 'forecast' }} canWrite readAloud={false} onOpenPanel={vi.fn()} />,
    )
    await waitFor(() => expect(listCalls()).toBeGreaterThan(0))
    const callsWhileClosed = listCalls()

    rerender(
      <MateSheet open onOpenChange={vi.fn()} screen={{ panel: 'forecast' }} canWrite readAloud={false} onOpenPanel={vi.fn()} />,
    )

    await waitFor(() => expect(listCalls()).toBeGreaterThan(callsWhileClosed))
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
        spoken
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

  // Help, Settings and a text selection hand the sheet a question the
  // operator typed or picked, not one they spoke.
  it('neither asks for a spoken answer nor reads one aloud for a question that did not come by voice', async () => {
    installFakeSpeechSynthesis()
    let sentBody: Record<string, unknown> | null = null
    vi.stubGlobal('fetch', buildFetch((conversationId, body) => {
      sentBody = body
      return sseMessageResponse(
        { id: 'm1', conversation_id: conversationId, seq: 1, role: 'assistant', content: 'Set the alarm under Settings.\n\n## Spoken summary\n\nUnder Settings.', created_at: '' },
        { id: conversationId, title: '', created_at: '', updated_at: '' },
      )
    }))

    render(
      <MateSheet
        open
        onOpenChange={vi.fn()}
        initialQuestion="How do I set a depth alarm?"
        screen={{ panel: 'settings', section: 'alarms' }}
        canWrite
        readAloud
        onOpenPanel={vi.fn()}
      />,
    )

    await screen.findByText(/Set the alarm under Settings\./)
    expect(sentBody).not.toHaveProperty('spoken')
    expect(fakeSynth.spoken).toHaveLength(0)
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
        spoken
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
        spoken
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
        spoken
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

  // ADR 0105: the sheet's read-aloud effect only ever reads
  // `reply.content` from chat.send()'s resolved value (the `message` frame),
  // and never looks at chat.draft - this pins that down against a
  // regression where a streamed draft happened to carry its own
  // `## Spoken summary` heading (e.g. before the backend's hold-back window
  // trims a partial one) and got read aloud before the real, final summary
  // arrived.
  it('does not speak from an in-progress draft, only from the resolved message', async () => {
    installFakeSpeechSynthesis()
    const stream = controllableSseStream()
    let capturedConversationId = ''
    vi.stubGlobal('fetch', buildFetch((conversationId) => {
      capturedConversationId = conversationId
      return { ok: true, body: stream.stream }
    }))

    render(
      <MateSheet
        open
        onOpenChange={vi.fn()}
        initialQuestion="How does tomorrow look?"
        spoken
        screen={{ panel: 'forecast' }}
        canWrite
        readAloud
        onOpenPanel={vi.fn()}
      />,
    )

    await waitFor(() => expect(capturedConversationId).not.toBe(''))

    await act(async () => {
      stream.push('event: delta\ndata: {"text":"## Spoken summary\\n\\nDraft text that must never be read aloud."}\n\n')
      await flushMicrotasks()
    })
    expect(await screen.findByText(/Draft text that must never be read aloud/)).toBeInTheDocument()
    expect(fakeSynth.spoken).toHaveLength(0)

    await act(async () => {
      stream.push(`data: ${JSON.stringify({
        message: {
          id: 'm1',
          conversation_id: capturedConversationId,
          seq: 1,
          role: 'assistant',
          content: '## Spoken summary\n\nFine tomorrow, light winds.',
          created_at: '2026-09-12T00:00:00Z',
        },
        conversation: {
          id: capturedConversationId,
          title: 'How does tomorrow look?',
          created_at: '2026-09-12T00:00:00Z',
          updated_at: '2026-09-12T00:00:01Z',
        },
      })}\n\n`)
      stream.close()
      await flushMicrotasks()
    })

    await waitFor(() => expect(fakeSynth.spoken).toHaveLength(1))
    expect(fakeSynth.spoken[0].text).toBe('Fine tomorrow, light winds.')
  })

  it('still reads the answer aloud when Stop is tapped after the answer arrived but before the stream closed', async () => {
    installFakeSpeechSynthesis()
    const stream = controllableSseStream()
    let capturedConversationId = ''
    const inner = buildFetch((conversationId) => {
      capturedConversationId = conversationId
      return { ok: true, body: stream.stream }
    })
    vi.stubGlobal('fetch', (url: string, init?: RequestInit) =>
      url.endsWith('/run/cancel') ? Promise.resolve({ ok: true }) : inner(url, init))

    render(
      <MateSheet
        open
        onOpenChange={vi.fn()}
        initialQuestion="How does tomorrow look?"
        spoken
        screen={{ panel: 'forecast' }}
        canWrite
        readAloud
        onOpenPanel={vi.fn()}
      />,
    )
    await waitFor(() => expect(capturedConversationId).not.toBe(''))

    await act(async () => {
      stream.push(`data: ${JSON.stringify({
        message: {
          id: 'm1',
          conversation_id: capturedConversationId,
          seq: 1,
          role: 'assistant',
          content: '## Spoken summary\n\nFine tomorrow, light winds.',
          created_at: '2026-09-12T00:00:00Z',
        },
        conversation: {
          id: capturedConversationId,
          title: 'How does tomorrow look?',
          created_at: '2026-09-12T00:00:00Z',
          updated_at: '2026-09-12T00:00:01Z',
        },
      })}\n\n`)
      await flushMicrotasks()
    })

    // The stream is still open, so Stop is on screen; tapping it ends the
    // stream early and send() resolves null.
    fireEvent.click(await screen.findByRole('button', { name: 'Stop asking' }))

    await waitFor(() => expect(fakeSynth.spoken).toHaveLength(1))
    expect(fakeSynth.spoken[0].text).toBe('Fine tomorrow, light winds.')
    expect(screen.queryByText('Stopped.')).not.toBeInTheDocument()
  })

  // ADR 0105 ("the answer outlives the page"): the sheet only ever reads a
  // reply aloud after a send it made itself (the initialQuestion flow
  // above) - a reply that shows up because AssistantThread rejoined a run
  // still being written elsewhere (GET .../run, via chat.attach()) must
  // never trigger read-aloud, even with "Read replies aloud" switched on,
  // even though the reply still lands in the thread with its
  // "## Spoken summary" section intact. Mate UI cycle ("Mate opens on an
  // empty chat"): a plain open no longer auto-selects c1, so this picks it
  // through the search overlay first (the operator already had this
  // conversation open earlier, resumed it here) to put the sheet into the
  // "an existing conversation is active, and its run is being rejoined"
  // state this test is actually about.
  it('does not speak a reply that arrives via attach (a rejoined run), only one from its own send', async () => {
    installFakeSpeechSynthesis()
    vi.stubGlobal('fetch', buildFetch(
      undefined,
      [{ id: 'c1', title: 'Hook Reef anchorages', created_at: '', updated_at: '' }],
      undefined,
      (conversationId) => sseMessageResponse(
        {
          id: 'm1',
          conversation_id: conversationId,
          seq: 1,
          role: 'assistant',
          content: '## Spoken summary\n\nFine tomorrow, light winds.',
          created_at: '2026-09-12T00:00:00Z',
        },
        { id: conversationId, title: 'Hook Reef anchorages', created_at: '2026-09-12T00:00:00Z', updated_at: '2026-09-12T00:00:01Z' },
      ),
    ))

    render(
      <MateSheet
        open
        onOpenChange={vi.fn()}
        screen={{ panel: 'forecast' }}
        canWrite
        readAloud
        onOpenPanel={vi.fn()}
      />,
    )

    fireEvent.click(await screen.findByRole('button', { name: 'Search conversations' }))
    fireEvent.click(await screen.findByText('Hook Reef anchorages'))

    // The rejoined reply lands in the thread exactly as a sent one would...
    expect(await screen.findByText(/Fine tomorrow, light winds\./)).toBeInTheDocument()
    // ...but since this sheet never sent that question itself, it must
    // never be read aloud.
    expect(fakeSynth.spoken).toHaveLength(0)
  })

  it('neither shows nor speaks a reply when the operator moved to another conversation meanwhile', async () => {
    installFakeSpeechSynthesis()
    const stream = controllableSseStream()
    let sentTo = ''
    vi.stubGlobal('fetch', buildFetch(
      (conversationId) => {
        sentTo = conversationId
        return { ok: true, body: stream.stream }
      },
      [{ id: 'c1', title: 'Hook Reef anchorages', created_at: '', updated_at: '' }],
    ))

    render(
      <MateSheet
        open
        onOpenChange={vi.fn()}
        initialQuestion="How does tomorrow look?"
        spoken
        screen={{ panel: 'forecast' }}
        canWrite
        readAloud
        onOpenPanel={vi.fn()}
      />,
    )
    await waitFor(() => expect(sentTo).not.toBe(''))

    fireEvent.click(await screen.findByRole('button', { name: 'Search conversations' }))
    fireEvent.click(await screen.findByText('Hook Reef anchorages'))

    // Switching threads supersedes the send's stream, so it may already be
    // cancelled; a frame that still slips through must not be appended.
    await act(async () => {
      try { stream.push(`data: ${JSON.stringify({
        message: {
          id: 'm1',
          conversation_id: sentTo,
          seq: 1,
          role: 'assistant',
          content: '## Spoken summary\n\nFine tomorrow, light winds.',
          created_at: '2026-09-12T00:00:00Z',
        },
        conversation: { id: sentTo, title: 'How does tomorrow look?', created_at: '2026-09-12T00:00:00Z', updated_at: '2026-09-12T00:00:01Z' },
      })}\n\n`) } catch { /* stream already closed */ }
      await flushMicrotasks()
    })

    expect(screen.queryByText(/Fine tomorrow, light winds\./)).not.toBeInTheDocument()
    expect(fakeSynth.spoken).toHaveLength(0)
  })
})
