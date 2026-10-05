import { forwardRef, useImperativeHandle } from 'react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react'

import { MateSummaryNoteDialog } from '@/components/mate-summary-note-dialog'
import { AssistantThread } from '@/components/assistant-thread'
import type { useAssistantChat } from '@/hooks/use-assistant-chat'
import type { AssistantMessage, useAssistantConversations } from '@/hooks/use-assistant-conversations'

// The Plate editor is covered by note-editor.test.tsx; here it is a textarea
// that reports edits the way the real body does.
vi.mock('@/components/note-editor', () => ({
  NoteEditorBody: forwardRef<
    { getMarkdown: () => string; insertDictation: () => void; focus: () => void },
    { value: string; onMarkdownChange?: (markdown: string) => void }
  >(function NoteEditorBody({ value, onMarkdownChange }, ref) {
    useImperativeHandle(ref, () => ({ getMarkdown: () => value, insertDictation: () => {}, focus: () => {} }), [value])
    return <textarea aria-label="Note body" defaultValue={value} onChange={(e) => onMarkdownChange?.(e.target.value)} />
  }),
}))

const draftResponse = {
  title: 'Reverso polisher sampling',
  body: '## What we established\n\n- No sampling port.\n',
  type: 'quirk',
  equipment: [
    { id: 'eq-1', name: 'Reverso fuel polisher' },
    { id: 'eq-2', name: 'Port Racor', linked: true },
  ],
}

function jsonResponse(body: unknown, status = 200) {
  return Promise.resolve(new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } }))
}

let fetchMock: ReturnType<typeof vi.fn>

// The thread's notes hook also fetches /api/notes on mount; only the summary
// calls are scripted.
function summaryCalls() {
  return fetchMock.mock.calls.filter(([url]) => String(url).includes('/summary-'))
}
function scriptSummary(...responses: Promise<Response>[]) {
  fetchMock.mockImplementation((url: string) => {
    if (String(url).includes('/summary-')) return responses.shift() ?? jsonResponse({ error: 'unscripted' }, 500)
    return jsonResponse({ notes: [], documents: [] })
  })
}

beforeEach(() => {
  fetchMock = vi.fn()
  vi.stubGlobal('fetch', fetchMock)
})
afterEach(() => vi.unstubAllGlobals())

function renderDialog(onSaved = vi.fn()) {
  render(<MateSummaryNoteDialog open onOpenChange={vi.fn()} conversationId="c1" onSaved={onSaved} />)
  return onSaved
}

describe('MateSummaryNoteDialog', () => {
  it('fetches the draft on open and shows the title, body and equipment ticked', async () => {
    scriptSummary(jsonResponse(draftResponse))
    renderDialog()

    expect(screen.getByText('Writing the summary…')).toBeInTheDocument()
    expect(await screen.findByDisplayValue('Reverso polisher sampling')).toBeInTheDocument()
    expect(summaryCalls()[0][0]).toContain('/api/assistant/conversations/c1/summary-draft')
    expect(summaryCalls()[0][1]).toMatchObject({ method: 'POST' })
    expect(screen.getByLabelText('Note body')).toHaveValue(draftResponse.body)
    expect(screen.getByRole('checkbox', { name: 'Reverso fuel polisher' })).toBeChecked()
    expect(screen.getByRole('button', { name: 'Save' })).toBeInTheDocument()
  })

  it('saves the edited title, body and selected equipment', async () => {
    scriptSummary(jsonResponse(draftResponse), jsonResponse({ note_id: 'n1', created: true }))
    const onSaved = renderDialog()

    const title = await screen.findByDisplayValue('Reverso polisher sampling')
    fireEvent.change(title, { target: { value: 'Polisher: no sampling port' } })
    fireEvent.change(screen.getByLabelText('Note body'), { target: { value: '- edited fact' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(onSaved).toHaveBeenCalledWith('n1', true))
    const [url, init] = summaryCalls()[1]
    expect(url).toContain('/api/assistant/conversations/c1/summary-note')
    expect(JSON.parse(init.body)).toEqual({
      title: 'Polisher: no sampling port',
      body: '- edited fact',
      type: 'quirk',
      add_equipment_ids: ['eq-1'],
      remove_equipment_ids: [],
    })
  })

  it('in update mode reads Update note and unlinks equipment the operator unticks', async () => {
    scriptSummary(jsonResponse({ ...draftResponse, existing_note_id: 'n1' }), jsonResponse({ note_id: 'n1', created: false }))
    const onSaved = renderDialog()

    await screen.findByDisplayValue('Reverso polisher sampling')
    expect(screen.getByRole('checkbox', { name: 'Port Racor' })).toBeChecked()
    fireEvent.click(screen.getByRole('checkbox', { name: 'Port Racor' }))
    fireEvent.click(screen.getByRole('button', { name: 'Update note' }))

    await waitFor(() => expect(onSaved).toHaveBeenCalledWith('n1', false))
    expect(JSON.parse(summaryCalls()[1][1].body)).toMatchObject({
      add_equipment_ids: ['eq-1'],
      remove_equipment_ids: ['eq-2'],
    })
  })

  it('shows the server message inline when the draft fails and offers no Save', async () => {
    scriptSummary(jsonResponse({ error: 'Nothing in this conversation to keep yet.' }, 422))
    renderDialog()

    expect(await screen.findByText('Nothing in this conversation to keep yet.')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Save' })).not.toBeInTheDocument()
  })

  it('shows the server message inline when saving fails and stays open', async () => {
    scriptSummary(jsonResponse(draftResponse), jsonResponse({ error: 'The note was saved but linking it to equipment failed: boom' }, 500))
    const onSaved = renderDialog()

    await screen.findByDisplayValue('Reverso polisher sampling')
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    expect(await screen.findByText(/linking it to equipment failed: boom/)).toBeInTheDocument()
    expect(onSaved).not.toHaveBeenCalled()
  })
})

function buildConversations(overrides: Partial<ReturnType<typeof useAssistantConversations>> = {}): ReturnType<typeof useAssistantConversations> {
  return {
    conversations: [],
    activeId: 'c1',
    messages: [],
    summaryNoteId: null,
    setSummaryNoteId: vi.fn(),
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
    attach: vi.fn().mockResolvedValue(null),
    isStreamingConversation: vi.fn().mockReturnValue(false),
    answerDelivered: vi.fn().mockReturnValue(false),
    deliveredMessageId: null,
    ...overrides,
  }
}

const reply: AssistantMessage = {
  id: 'm2',
  conversationId: 'c1',
  seq: 2,
  role: 'assistant',
  content: 'Try the manifold.',
  createdAt: '2026-10-05T00:00:00Z',
}

describe('Summarise to note button in the composer', () => {
  it('is disabled until Mate has replied', () => {
    render(<AssistantThread canWrite conversations={buildConversations()} chat={buildChat()} />)
    expect(screen.getByRole('button', { name: 'Summarise to note' })).toBeDisabled()
  })

  it('is disabled while sending and for a read-only session', () => {
    const conversations = buildConversations({ messages: [reply] })
    const { unmount } = render(<AssistantThread canWrite conversations={conversations} chat={buildChat({ sending: true })} />)
    expect(screen.getByRole('button', { name: 'Summarise to note' })).toBeDisabled()
    unmount()
    render(<AssistantThread canWrite={false} conversations={conversations} chat={buildChat()} />)
    expect(screen.getByRole('button', { name: 'Summarise to note' })).toBeDisabled()
  })

  it('opens the review dialog and fetches the draft when tapped', async () => {
    scriptSummary(jsonResponse(draftResponse))
    render(<AssistantThread canWrite conversations={buildConversations({ messages: [reply] })} chat={buildChat()} />)

    fireEvent.click(screen.getByRole('button', { name: 'Summarise to note' }))

    expect(await screen.findByDisplayValue('Reverso polisher sampling')).toBeInTheDocument()
    expect(summaryCalls()[0][0]).toContain('/conversations/c1/summary-draft')
  })

  it('reads Update note once the conversation has a note, and remembers the saved note', async () => {
    const setSummaryNoteId = vi.fn()
    scriptSummary(jsonResponse({ ...draftResponse, existing_note_id: 'n1' }), jsonResponse({ note_id: 'n1', created: false }))
    render(
      <AssistantThread
        canWrite
        conversations={buildConversations({ messages: [reply], summaryNoteId: 'n1', setSummaryNoteId })}
        chat={buildChat()}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Update note' }))
    await screen.findByDisplayValue('Reverso polisher sampling')
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Update note' }))

    await waitFor(() => expect(setSummaryNoteId).toHaveBeenCalledWith('n1'))
  })
})
