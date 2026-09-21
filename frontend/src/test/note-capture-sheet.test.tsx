import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { act, render, screen, fireEvent, waitFor } from '@testing-library/react'
import { useState } from 'react'

import { NoteCaptureSheet } from '@/components/documents/note-capture-sheet'
import { useNotes } from '@/hooks/use-notes'
import type { NoteRecord } from '@/hooks/use-notes'

// ADR 0119: the one capture sheet, opened either from App.tsx's global
// header action/Alt+N or from Documents' New → Note menu item - this file
// is about what the sheet itself does with whatever useNotes reports, the
// same App-level mocking convention documents-panel.test.tsx and the
// deleted notes-panel.tsx used for the identical hook. useSpeechInput is
// deliberately left real, for the same reason it was in the deleted
// notes-panel.test.tsx: the mic-visibility assertions below are exactly
// about how the real hook's unsupportedReason drives what the sheet renders.
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

function note(overrides: Partial<NoteRecord> = {}): NoteRecord {
  return {
    id: 'note-1',
    sha256: 'abc123',
    folder_id: null,
    filename: 'Ring Dave.md',
    title: 'Ring Dave',
    notes: '',
    mime: 'text/markdown',
    size_bytes: 32,
    page_count: 0,
    summary: '',
    status: 'indexed',
    stage: 'done',
    indexed_with: '',
    error: '',
    index_model: '',
    index_cost_usd: 0,
    tags: [],
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    indexed_at: '2026-01-01T00:00:00Z',
    kind: 'note',
    note_type: 'contact',
    note_type_source: 'auto',
    pinned: false,
    sort_index: 0,
    ...overrides,
  }
}

function stubSecureContext(value: boolean | undefined) {
  Object.defineProperty(window, 'isSecureContext', { value, configurable: true })
}

beforeEach(() => {
  vi.clearAllMocks()
  mockedUseNotes.mockReturnValue(makeNotesMock())
})

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  Object.defineProperty(window, 'isSecureContext', { value: undefined, configurable: true })
})

describe('NoteCaptureSheet capture', () => {
  it('defaults the type select to Auto and sends no type field when left alone', async () => {
    const createNote = vi.fn().mockResolvedValue({ document: note(), body: 'Ring Dave about the mooring' })
    mockedUseNotes.mockReturnValue(makeNotesMock({ createNote }))
    const onOpenChange = vi.fn()

    render(<NoteCaptureSheet open onOpenChange={onOpenChange} />)

    expect(screen.getByRole('combobox', { name: 'Note type' })).toHaveTextContent('Auto')

    fireEvent.change(screen.getByRole('textbox', { name: 'Capture a note' }), { target: { value: 'Ring Dave about the mooring' } })
    fireEvent.click(screen.getByRole('button', { name: 'Capture' }))

    await waitFor(() => expect(createNote).toHaveBeenCalledWith({ body: 'Ring Dave about the mooring' }))
    expect(createNote).not.toHaveBeenCalledWith(expect.objectContaining({ type: expect.anything() }))
  })

  it('posts the typed body, clears the textarea, and closes the sheet on success', async () => {
    const createNote = vi.fn().mockResolvedValue({ document: note(), body: 'x' })
    mockedUseNotes.mockReturnValue(makeNotesMock({ createNote }))
    const onOpenChange = vi.fn()
    const onCaptured = vi.fn()

    render(<NoteCaptureSheet open onOpenChange={onOpenChange} onCaptured={onCaptured} />)

    const textarea = screen.getByRole('textbox', { name: 'Capture a note' })
    fireEvent.change(textarea, { target: { value: 'x' } })
    fireEvent.click(screen.getByRole('button', { name: 'Capture' }))

    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
    expect(onCaptured).toHaveBeenCalledWith('note-1')
  })

  it('submits on Ctrl+Enter (Cmd+Enter on a Mac) as well as the button', async () => {
    const createNote = vi.fn().mockResolvedValue({ document: note(), body: 'x' })
    mockedUseNotes.mockReturnValue(makeNotesMock({ createNote }))

    render(<NoteCaptureSheet open onOpenChange={vi.fn()} />)

    const textarea = screen.getByRole('textbox', { name: 'Capture a note' })
    fireEvent.change(textarea, { target: { value: 'x' } })
    fireEvent.keyDown(textarea, { key: 'Enter', ctrlKey: true })

    await waitFor(() => expect(createNote).toHaveBeenCalledWith({ body: 'x' }))
  })

  it('does not post an empty or whitespace-only capture', () => {
    const createNote = vi.fn()
    mockedUseNotes.mockReturnValue(makeNotesMock({ createNote }))

    render(<NoteCaptureSheet open onOpenChange={vi.fn()} />)
    expect(screen.getByRole('button', { name: 'Capture' })).toBeDisabled()

    fireEvent.change(screen.getByRole('textbox', { name: 'Capture a note' }), { target: { value: '   ' } })
    expect(screen.getByRole('button', { name: 'Capture' })).toBeDisabled()
  })

  // Picking an explicit type sends it, which createNoteHandler
  // (backend/notes_handlers.go) records as note_type_source: 'operator' -
  // SetNoteTypeIfNotOperator then refuses to overwrite it. ADR 0119.
  it('picking an explicit type sends it', async () => {
    const createNote = vi.fn().mockResolvedValue({ document: note(), body: 'Open the seacock first' })
    mockedUseNotes.mockReturnValue(makeNotesMock({ createNote }))

    render(<NoteCaptureSheet open onOpenChange={vi.fn()} />)

    fireEvent.click(screen.getByRole('combobox', { name: 'Note type' }))
    const option = await screen.findByRole('option', { name: 'Procedure' })
    fireEvent.pointerDown(option)
    fireEvent.pointerUp(option)
    fireEvent.click(option)

    fireEvent.change(screen.getByRole('textbox', { name: 'Capture a note' }), { target: { value: 'Open the seacock first' } })
    fireEvent.click(screen.getByRole('button', { name: 'Capture' }))

    await waitFor(() => expect(createNote).toHaveBeenCalledWith({ body: 'Open the seacock first', type: 'procedure' }))
  })

  it("shows the server's error and keeps the text in the box when capture fails", async () => {
    const createNote = vi.fn().mockRejectedValue(new Error('note body is empty'))
    mockedUseNotes.mockReturnValue(makeNotesMock({ createNote }))

    render(<NoteCaptureSheet open onOpenChange={vi.fn()} />)
    const textarea = screen.getByRole('textbox', { name: 'Capture a note' })
    fireEvent.change(textarea, { target: { value: 'x' } })
    fireEvent.click(screen.getByRole('button', { name: 'Capture' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('note body is empty')
    expect(textarea).toHaveValue('x')
  })
})

// ADR 0122: dictation moved from a separate row below the textarea into the
// field itself (an InputGroup, matching the Mate composer's own mic), with
// "Dictate"/"Stop dictation" replacing this sheet's old "Dictate a
// note"/"Stop dictation" labels, and the interim line replaced by
// DictationStatus's aria-live region. The visibility/disabled rules
// themselves (hidden with no API, disabled with a visible reason on an
// insecure origin) are unchanged from what notes-panel.tsx's deleted
// capture box, and this sheet's own footer mic, both did before.
describe('NoteCaptureSheet mic visibility', () => {
  it('hides the mic entirely when the browser has no SpeechRecognition API at all', () => {
    render(<NoteCaptureSheet open onOpenChange={vi.fn()} />)
    expect(screen.queryByRole('button', { name: /dictate/i })).not.toBeInTheDocument()
  })

  it('shows the mic, disabled, with an explicit sentence, when the API exists but the page is not a secure context', () => {
    class FakeSpeechRecognition {
      start() {}
      stop() {}
      abort() {}
    }
    vi.stubGlobal('SpeechRecognition', FakeSpeechRecognition)
    stubSecureContext(false)

    render(<NoteCaptureSheet open onOpenChange={vi.fn()} />)

    const mic = screen.getByRole('button', { name: 'Dictate' })
    expect(mic).toBeDisabled()
    expect(screen.getByRole('alert')).toHaveTextContent('Voice input needs the app opened over https')
  })

  it('shows an enabled mic when the API exists and the page is a secure context', () => {
    class FakeSpeechRecognition {
      start() {}
      stop() {}
      abort() {}
    }
    vi.stubGlobal('SpeechRecognition', FakeSpeechRecognition)
    stubSecureContext(true)

    render(<NoteCaptureSheet open onOpenChange={vi.fn()} />)

    expect(screen.getByRole('button', { name: 'Dictate' })).toBeEnabled()
  })
})

describe('NoteCaptureSheet dictation (ADR 0122)', () => {
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
      instances.push(this)
    }

    start() { this.started = true }
    stop() { this.stopped = true }
    abort() { this.aborted = true }

    emitResult(transcript: string, isFinal: boolean) {
      const result: FakeResult = Object.assign([{ transcript }], { isFinal })
      this.onresult?.({ resultIndex: 0, results: [result] })
    }
  }

  let instances: FakeSpeechRecognition[] = []
  const current = () => instances[instances.length - 1]

  beforeEach(() => {
    instances = []
    vi.stubGlobal('SpeechRecognition', FakeSpeechRecognition)
    stubSecureContext(true)
  })

  it('dictating appends to the textarea rather than replacing it, and never submits by itself', () => {
    const createNote = vi.fn()
    mockedUseNotes.mockReturnValue(makeNotesMock({ createNote }))

    render(<NoteCaptureSheet open onOpenChange={vi.fn()} />)
    const textarea = screen.getByRole('textbox', { name: 'Capture a note' })

    fireEvent.click(screen.getByRole('button', { name: 'Dictate' }))
    act(() => { current().emitResult('fuel return is the inboard valve', true) })

    expect(textarea).toHaveValue('fuel return is the inboard valve')
    expect(createNote).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole('button', { name: 'Stop dictation' }))
    expect(current().stopped).toBe(true)
    expect(current().aborted).toBe(false)
  })

  it('shows the interim transcript as a visible, aria-live line while listening', () => {
    render(<NoteCaptureSheet open onOpenChange={vi.fn()} />)

    fireEvent.click(screen.getByRole('button', { name: 'Dictate' }))
    expect(screen.getByText('Listening…')).toBeInTheDocument()

    act(() => { current().emitResult('fuel return is the', false) })

    expect(screen.getByText('fuel return is the')).toBeInTheDocument()
  })

  it('Escape while dictating cancels dictation without closing the sheet', () => {
    render(<NoteCaptureSheet open onOpenChange={vi.fn()} />)
    const textarea = screen.getByRole('textbox', { name: 'Capture a note' })

    fireEvent.click(screen.getByRole('button', { name: 'Dictate' }))
    fireEvent.keyDown(textarea, { key: 'Escape' })

    expect(current().aborted).toBe(true)
    expect(screen.getByRole('textbox', { name: 'Capture a note' })).toBeInTheDocument()
  })

  // Code review: the sheet is one mounted instance for the app's lifetime
  // (ADR 0119/0121) - closing it (by any path) or capturing successfully
  // must not leave a dictation session listening into hidden state and
  // holding the voice arbiter's claim, which would block "Hey Mate"
  // indefinitely.
  it('cancels dictation on a successful Capture', async () => {
    const createNote = vi.fn().mockResolvedValue({ document: note(), body: 'x' })
    mockedUseNotes.mockReturnValue(makeNotesMock({ createNote }))

    render(<NoteCaptureSheet open onOpenChange={vi.fn()} />)
    fireEvent.change(screen.getByRole('textbox', { name: 'Capture a note' }), { target: { value: 'x' } })

    fireEvent.click(screen.getByRole('button', { name: 'Dictate' }))
    expect(current().started).toBe(true)

    fireEvent.click(screen.getByRole('button', { name: 'Capture' }))

    await waitFor(() => expect(current().aborted).toBe(true))
  })

  it('cancels dictation when the sheet closes via Escape/overlay/its own close button, not just on submit', () => {
    render(<NoteCaptureSheet open onOpenChange={vi.fn()} />)

    fireEvent.click(screen.getByRole('button', { name: 'Dictate' }))
    expect(current().started).toBe(true)

    fireEvent.click(screen.getByRole('button', { name: 'Close' }))

    expect(current().aborted).toBe(true)
  })

  it('does not let a result from a dictation session running before close land after the sheet reopens', () => {
    function Wrapper() {
      const [open, setOpen] = useState(true)
      return (
        <>
          <button type="button" onClick={() => setOpen(true)}>reopen</button>
          <NoteCaptureSheet open={open} onOpenChange={setOpen} />
        </>
      )
    }

    render(<Wrapper />)
    fireEvent.click(screen.getByRole('button', { name: 'Dictate' }))
    const staleRecognition = current()

    fireEvent.click(screen.getByRole('button', { name: 'Close' }))
    fireEvent.click(screen.getByRole('button', { name: 'reopen' }))

    // A result from the recognizer session that was running before the
    // sheet closed must not be able to land in the freshly reopened field -
    // cancelling on close detaches its handlers, so this is inert.
    act(() => { staleRecognition.emitResult('leftover words', true) })

    expect(screen.getByRole('textbox', { name: 'Capture a note' })).toHaveValue('')
  })
})

// The Add Note split button (Documents' toolbar) can open this sheet with a
// kind already chosen, so the caret path lands on a textarea that is
// already typed rather than making the operator set it twice. `Auto`
// remains the default whenever nothing is passed, which is what keeps the
// one-click path free of a capture-time decision (plan R1/R3, ADR 0119).
describe('NoteCaptureSheet initialType', () => {
  it('starts on the kind it was opened with, and sends it', async () => {
    const createNote = vi.fn().mockResolvedValue({ document: note({ id: 'n-9' }), body: 'x' })
    mockedUseNotes.mockReturnValue(makeNotesMock({ createNote }))

    render(<NoteCaptureSheet open onOpenChange={vi.fn()} initialType="quirk" />)

    expect(screen.getByRole('combobox', { name: 'Note type' })).toHaveTextContent('Quirk')

    fireEvent.change(screen.getByRole('textbox', { name: 'Capture a note' }), { target: { value: 'Seacock is stiff' } })
    fireEvent.click(screen.getByRole('button', { name: 'Capture' }))

    await waitFor(() => {
      expect(createNote).toHaveBeenCalledWith({ body: 'Seacock is stiff', type: 'quirk' })
    })
  })

  it('still defaults to Auto, sending no type field, when opened with nothing', async () => {
    const createNote = vi.fn().mockResolvedValue({ document: note({ id: 'n-10' }), body: 'x' })
    mockedUseNotes.mockReturnValue(makeNotesMock({ createNote }))

    render(<NoteCaptureSheet open onOpenChange={vi.fn()} />)

    expect(screen.getByRole('combobox', { name: 'Note type' })).toHaveTextContent('Auto')

    fireEvent.change(screen.getByRole('textbox', { name: 'Capture a note' }), { target: { value: 'Ring Dave' } })
    fireEvent.click(screen.getByRole('button', { name: 'Capture' }))

    await waitFor(() => expect(createNote).toHaveBeenCalledWith({ body: 'Ring Dave' }))
  })
})
