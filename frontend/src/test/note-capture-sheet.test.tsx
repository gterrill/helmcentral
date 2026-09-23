import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { act, render, screen, fireEvent, waitFor } from '@testing-library/react'
import { useState } from 'react'

import { NoteCaptureSheet } from '@/components/documents/note-capture-sheet'
import { useNotes } from '@/hooks/use-notes'
import type { NoteRecord } from '@/hooks/use-notes'

// ADR 0119/0121: the one capture sheet, opened only from Documents' New →
// Note menu - this file is about what the sheet itself does with whatever
// useNotes reports, the same App-level mocking convention documents-
// panel.test.tsx and the deleted notes-panel.tsx used for the identical
// hook. useSpeechInput is deliberately left real, for the same reason it
// was in the deleted notes-panel.test.tsx: the mic-visibility assertions
// below are exactly about how the real hook's unsupportedReason drives what
// the sheet renders.
//
// ADR 0124 replaced the plain textarea with the ADR 0117 editor
// (NoteEditorBody, lazy over note-editor-impl.tsx - see note-editor.tsx),
// so every test below that needs body content in place has to (a) await
// the lazy chunk with findBy* before touching anything, matching note-
// editor.test.tsx's own convention, and (b) get text into a contentEditable
// surface some other way than fireEvent.change - jsdom does not run a real
// browser's input pipeline against contentEditable, so typing into it is
// unreliable. Every test here drives content through the fake dictation
// recognizer (already used for the mic tests below) or, where dictation
// itself isn't what's under test, through the editor's own "Markdown
// source" toggle and its real textarea.
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

// The fake SpeechRecognition harness every dictation-driven test below uses
// to get text into the editor - identical shape to dictation.test.tsx's own
// (useDictation composes useSpeechInput directly, so both files fake the
// same underlying API).
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

// Awaits the lazy editor body and returns its contentEditable surface -
// every test that needs to reach into the body (to dictate into it, toggle
// source mode, or fire a keydown on it) starts here.
async function findEditorBody() {
  return screen.findByRole('textbox', { name: 'Note body' })
}

// Dictates `text` as a single final result into the (already-mounted)
// editor and waits for it to land - the one way these tests put real
// content into the body (see this file's own header comment).
async function dictateInto(text: string) {
  fireEvent.click(screen.getByRole('button', { name: 'Dictate' }))
  act(() => { current().emitResult(text, true) })
  await waitFor(() => expect(screen.getByRole('button', { name: 'Stop dictation' })).toBeInTheDocument())
}

beforeEach(() => {
  vi.clearAllMocks()
  instances = []
  mockedUseNotes.mockReturnValue(makeNotesMock())
  vi.stubGlobal('SpeechRecognition', FakeSpeechRecognition)
  stubSecureContext(true)
})

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  Object.defineProperty(window, 'isSecureContext', { value: undefined, configurable: true })
})

// Code review: the mic used to have no relationship to the lazy editor
// body's own readiness at all - a final arriving before NoteEditorBody's
// imperative handle had attached hit editorRef.current?.insertDictation on
// null and was silently dropped (nothing dictated in that window ever
// landed anywhere). `editorReady` now tracks the handle's own attach/detach
// through a callback ref, and DictateButton is disabled until it's true.
//
// This has to run before any other test in this file mounts the lazy
// editor body: React.lazy() memoises its resolved promise on the module-
// level lazy() object itself (note-editor.tsx), so once any earlier test
// has resolved it, every later mount in the same file renders synchronously
// and never suspends again - see markdown-lazy.test.tsx's identical
// comment on the same mechanism.
describe('NoteCaptureSheet mic readiness (does not race the lazy editor body)', () => {
  it('disables Dictate while the lazy editor body has not resolved yet, and enables it once mounted', async () => {
    render(<NoteCaptureSheet open onOpenChange={vi.fn()} />)

    // Nothing has been awaited since render() - the lazy chunk's dynamic
    // import cannot have resolved yet (same reasoning as markdown-lazy.
    // test.tsx's "never calls react-markdown synchronously" case: a promise
    // can only settle on a later microtask, never within the synchronous
    // call stack that scheduled it), so the body is still its own "Loading
    // editor…" Suspense fallback and NoteEditorBody's imperative handle
    // has not attached.
    expect(screen.getByText('Loading editor…')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Dictate' })).toBeDisabled()

    await findEditorBody()

    expect(screen.getByRole('button', { name: 'Dictate' })).toBeEnabled()
  })
})

describe('NoteCaptureSheet capture', () => {
  it('defaults the type select to Auto and sends no type field when left alone', async () => {
    const createNote = vi.fn().mockResolvedValue({ document: note(), body: 'Ring Dave about the mooring' })
    mockedUseNotes.mockReturnValue(makeNotesMock({ createNote }))
    const onOpenChange = vi.fn()

    render(<NoteCaptureSheet open onOpenChange={onOpenChange} />)
    await findEditorBody()
    expect(screen.getByRole('combobox', { name: 'Note type' })).toHaveTextContent('Auto')

    await dictateInto('Ring Dave about the mooring')
    fireEvent.click(screen.getByRole('button', { name: 'Capture' }))

    await waitFor(() => expect(createNote).toHaveBeenCalledWith({ body: 'Ring Dave about the mooring' }))
    expect(createNote).not.toHaveBeenCalledWith(expect.objectContaining({ type: expect.anything() }))
  })

  it('posts the dictated body and closes the sheet on success', async () => {
    const createNote = vi.fn().mockResolvedValue({ document: note(), body: 'x' })
    mockedUseNotes.mockReturnValue(makeNotesMock({ createNote }))
    const onOpenChange = vi.fn()
    const onCaptured = vi.fn()

    render(<NoteCaptureSheet open onOpenChange={onOpenChange} onCaptured={onCaptured} />)
    await findEditorBody()
    await dictateInto('x')
    fireEvent.click(screen.getByRole('button', { name: 'Capture' }))

    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
    expect(onCaptured).toHaveBeenCalledWith('note-1')
  })

  it('submits on Ctrl+Enter (Cmd+Enter on a Mac) as well as the button', async () => {
    const createNote = vi.fn().mockResolvedValue({ document: note(), body: 'x' })
    mockedUseNotes.mockReturnValue(makeNotesMock({ createNote }))

    render(<NoteCaptureSheet open onOpenChange={vi.fn()} />)
    const body = await findEditorBody()
    await dictateInto('x')
    fireEvent.keyDown(body, { key: 'Enter', ctrlKey: true })

    await waitFor(() => expect(createNote).toHaveBeenCalledWith({ body: 'x' }))
  })

  it('does not post an empty capture, and stays disabled through a whitespace-only Markdown source edit', async () => {
    const createNote = vi.fn()
    mockedUseNotes.mockReturnValue(makeNotesMock({ createNote }))

    render(<NoteCaptureSheet open onOpenChange={vi.fn()} />)
    await findEditorBody()
    expect(screen.getByRole('button', { name: 'Capture' })).toBeDisabled()

    fireEvent.click(screen.getByRole('button', { name: 'Markdown source' }))
    const source = await screen.findByRole('textbox', { name: 'Note markdown source' })
    fireEvent.change(source, { target: { value: '   ' } })

    expect(screen.getByRole('button', { name: 'Capture' })).toBeDisabled()
  })

  // Code review: handleBodyChange used to read editorRef.current?.getMarkdown()
  // from INSIDE the onChange it was itself called by - NoteEditorBody's
  // imperative handle only picks up a just-typed character on the render
  // that follows setSourceText, which hasn't happened yet at that point, so
  // Capture stayed disabled for one keystroke's worth of stale state.
  // onChange now hands the fresh markdown straight to the caller instead of
  // making it read a handle.
  it('enables Capture immediately when typing in Markdown source mode, and disables it again when cleared', async () => {
    render(<NoteCaptureSheet open onOpenChange={vi.fn()} />)
    await findEditorBody()

    fireEvent.click(screen.getByRole('button', { name: 'Markdown source' }))
    const source = await screen.findByRole('textbox', { name: 'Note markdown source' })
    expect(screen.getByRole('button', { name: 'Capture' })).toBeDisabled()

    fireEvent.change(source, { target: { value: 'x' } })
    expect(screen.getByRole('button', { name: 'Capture' })).toBeEnabled()

    fireEvent.change(source, { target: { value: '' } })
    expect(screen.getByRole('button', { name: 'Capture' })).toBeDisabled()
  })

  // Picking an explicit type sends it, which createNoteHandler
  // (backend/notes_handlers.go) records as note_type_source: 'operator' -
  // SetNoteTypeIfNotOperator then refuses to overwrite it. ADR 0119.
  it('picking an explicit type sends it', async () => {
    const createNote = vi.fn().mockResolvedValue({ document: note(), body: 'Open the seacock first' })
    mockedUseNotes.mockReturnValue(makeNotesMock({ createNote }))

    render(<NoteCaptureSheet open onOpenChange={vi.fn()} />)
    await findEditorBody()

    fireEvent.click(screen.getByRole('combobox', { name: 'Note type' }))
    const option = await screen.findByRole('option', { name: 'Procedure' })
    fireEvent.pointerDown(option)
    fireEvent.pointerUp(option)
    fireEvent.click(option)

    await dictateInto('Open the seacock first')
    fireEvent.click(screen.getByRole('button', { name: 'Capture' }))

    await waitFor(() => expect(createNote).toHaveBeenCalledWith({ body: 'Open the seacock first', type: 'procedure' }))
  })

  it("shows the server's error and keeps the body in the editor when capture fails", async () => {
    const createNote = vi.fn().mockRejectedValue(new Error('note body is empty'))
    mockedUseNotes.mockReturnValue(makeNotesMock({ createNote }))

    render(<NoteCaptureSheet open onOpenChange={vi.fn()} />)
    await findEditorBody()
    await dictateInto('x')
    fireEvent.click(screen.getByRole('button', { name: 'Capture' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('note body is empty')

    fireEvent.click(screen.getByRole('button', { name: 'Markdown source' }))
    // Trailing newline: serializeNoteMarkdown's own remark-stringify output,
    // same as every fixture in note-editor-markdown.test.ts's corpus.
    expect(await screen.findByRole('textbox', { name: 'Note markdown source' })).toHaveValue('x\n')
  })
})

// ADR 0122: dictation, moved (ADR 0124) from the InputGroup below a plain
// textarea to a plain row under the editor, whose mic inserts at the
// caret rather than appending to a string. "Dictate"/"Stop dictation" and
// the visibility/disabled rules themselves are unchanged from before that
// move.
describe('NoteCaptureSheet mic visibility', () => {
  it('hides the mic entirely when the browser has no SpeechRecognition API at all', async () => {
    vi.unstubAllGlobals()
    render(<NoteCaptureSheet open onOpenChange={vi.fn()} />)
    await findEditorBody()
    expect(screen.queryByRole('button', { name: /dictate/i })).not.toBeInTheDocument()
  })

  it('shows the mic, disabled, with an explicit sentence, when the API exists but the page is not a secure context', async () => {
    stubSecureContext(false)

    render(<NoteCaptureSheet open onOpenChange={vi.fn()} />)
    await findEditorBody()

    const mic = screen.getByRole('button', { name: 'Dictate' })
    expect(mic).toBeDisabled()
    expect(screen.getByRole('alert')).toHaveTextContent('Voice input needs the app opened over https')
  })

  it('shows an enabled mic when the API exists and the page is a secure context', async () => {
    render(<NoteCaptureSheet open onOpenChange={vi.fn()} />)
    await findEditorBody()

    expect(screen.getByRole('button', { name: 'Dictate' })).toBeEnabled()
  })
})

describe('NoteCaptureSheet dictation (ADR 0122/0124)', () => {
  it('dictating inserts into the body rather than replacing it, and never submits by itself', async () => {
    const createNote = vi.fn()
    mockedUseNotes.mockReturnValue(makeNotesMock({ createNote }))

    render(<NoteCaptureSheet open onOpenChange={vi.fn()} />)
    await findEditorBody()

    fireEvent.click(screen.getByRole('button', { name: 'Dictate' }))
    act(() => { current().emitResult('fuel return is the inboard valve', true) })
    act(() => { current().emitResult('the one with the scratched handle', true) })

    fireEvent.click(screen.getByRole('button', { name: 'Markdown source' }))
    expect(await screen.findByRole('textbox', { name: 'Note markdown source' }))
      .toHaveValue('fuel return is the inboard valve the one with the scratched handle\n')
    expect(createNote).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole('button', { name: 'Markdown source' }))
    fireEvent.click(screen.getByRole('button', { name: 'Stop dictation' }))
    expect(current().stopped).toBe(true)
    expect(current().aborted).toBe(false)
  })

  it('shows the interim transcript as a visible, aria-live line while listening, and never posts it', async () => {
    const createNote = vi.fn().mockResolvedValue({ document: note(), body: 'x' })
    mockedUseNotes.mockReturnValue(makeNotesMock({ createNote }))

    render(<NoteCaptureSheet open onOpenChange={vi.fn()} />)
    await findEditorBody()

    fireEvent.click(screen.getByRole('button', { name: 'Dictate' }))
    expect(screen.getByText('Listening…')).toBeInTheDocument()

    act(() => { current().emitResult('fuel return is the', false) })
    expect(screen.getByText('fuel return is the')).toBeInTheDocument()

    // An interim result never reaches the document (ADR 0124) - Capture
    // stays disabled, and finishing the phrase without a final result
    // posts nothing.
    expect(screen.getByRole('button', { name: 'Capture' })).toBeDisabled()
  })

  it('Escape while dictating cancels dictation without closing the sheet', async () => {
    render(<NoteCaptureSheet open onOpenChange={vi.fn()} />)
    const body = await findEditorBody()

    fireEvent.click(screen.getByRole('button', { name: 'Dictate' }))
    fireEvent.keyDown(body, { key: 'Escape' })

    expect(current().aborted).toBe(true)
    expect(screen.getByRole('textbox', { name: 'Note body' })).toBeInTheDocument()
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
    await findEditorBody()
    await dictateInto('x')
    expect(current().started).toBe(true)

    fireEvent.click(screen.getByRole('button', { name: 'Capture' }))

    await waitFor(() => expect(current().aborted).toBe(true))
  })

  it('cancels dictation when the sheet closes via Escape/overlay/its own close button, not just on submit', async () => {
    render(<NoteCaptureSheet open onOpenChange={vi.fn()} />)
    await findEditorBody()

    fireEvent.click(screen.getByRole('button', { name: 'Dictate' }))
    expect(current().started).toBe(true)

    fireEvent.click(screen.getByRole('button', { name: 'Close' }))

    expect(current().aborted).toBe(true)
  })

  it('does not let a result from a dictation session running before close land after the sheet reopens', async () => {
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
    await findEditorBody()
    fireEvent.click(screen.getByRole('button', { name: 'Dictate' }))
    const staleRecognition = current()

    fireEvent.click(screen.getByRole('button', { name: 'Close' }))
    fireEvent.click(screen.getByRole('button', { name: 'reopen' }))
    await findEditorBody()

    // A result from the recognizer session that was running before the
    // sheet closed must not be able to land in the freshly reopened body -
    // cancelling on close detaches its handlers, so this is inert.
    act(() => { staleRecognition.emitResult('leftover words', true) })

    expect(screen.getByRole('button', { name: 'Capture' })).toBeDisabled()
  })
})

// The New → Note submenu (Documents' toolbar) can open this sheet with a
// kind already chosen, so the caret path lands on an editor that's already
// typed rather than making the operator set it twice. `Auto` remains the
// default whenever nothing is passed, which is what keeps the one-click
// path free of a capture-time decision (ADR 0119).
describe('NoteCaptureSheet initialType', () => {
  it('starts on the kind it was opened with, and sends it', async () => {
    const createNote = vi.fn().mockResolvedValue({ document: note({ id: 'n-9' }), body: 'x' })
    mockedUseNotes.mockReturnValue(makeNotesMock({ createNote }))

    render(<NoteCaptureSheet open onOpenChange={vi.fn()} initialType="quirk" />)
    await findEditorBody()

    expect(screen.getByRole('combobox', { name: 'Note type' })).toHaveTextContent('Quirk')

    await dictateInto('Seacock is stiff')
    fireEvent.click(screen.getByRole('button', { name: 'Capture' }))

    await waitFor(() => {
      expect(createNote).toHaveBeenCalledWith({ body: 'Seacock is stiff', type: 'quirk' })
    })
  })

  it('still defaults to Auto, sending no type field, when opened with nothing', async () => {
    const createNote = vi.fn().mockResolvedValue({ document: note({ id: 'n-10' }), body: 'x' })
    mockedUseNotes.mockReturnValue(makeNotesMock({ createNote }))

    render(<NoteCaptureSheet open onOpenChange={vi.fn()} />)
    await findEditorBody()

    expect(screen.getByRole('combobox', { name: 'Note type' })).toHaveTextContent('Auto')

    await dictateInto('Ring Dave')
    fireEvent.click(screen.getByRole('button', { name: 'Capture' }))

    await waitFor(() => expect(createNote).toHaveBeenCalledWith({ body: 'Ring Dave' }))
  })
})
