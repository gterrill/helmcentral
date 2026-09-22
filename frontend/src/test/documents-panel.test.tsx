import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, within, act, waitFor } from '@testing-library/react'

import { DocumentsPanel } from '@/components/documents-panel'
import { useAssistantStatus } from '@/hooks/use-assistant-status'
import { useDocuments } from '@/hooks/use-documents'
import { useDocumentUploads } from '@/hooks/use-document-uploads'
import { useManuals } from '@/hooks/use-manuals'
import { useNotes } from '@/hooks/use-notes'
import type { ManualTreeNode } from '@/hooks/use-manuals'
import type { NoteRecord } from '@/hooks/use-notes'

// ADR 0106 F1: DocumentsPanel is tested against mocked hooks (App-level test
// convention - see e.g. app-sidebar-navigation.test.tsx), not real fetch:
// use-documents.test.ts and use-document-uploads.test.ts already exercise
// the real request/response wiring. This file is about what the panel does
// with whatever the hooks report - rendering, navigation, selection,
// confirmations - not the network.
//
// Revision "one panel, not three" (2026-09-20) folds the deleted
// notes-panel.tsx/manuals-panel.tsx coverage in here too, so useNotes and
// useManuals are now mocked the identical way those two files mocked them -
// findManualTreeNode is left real (a pure helper the component calls
// directly), same reasoning as the deleted files' own comment on it.

vi.mock('@/hooks/use-documents')
vi.mock('@/hooks/use-document-uploads')
vi.mock('@/hooks/use-notes')
// Ask Mate about a selection (ask-mate-selection.tsx) gates on this same
// status hook AssistantDrawer already uses - mocked the identical way the
// other network-backed hooks above are, rather than letting a real fetch to
// /api/assistant/status run (and fail) on every test in this file.
vi.mock('@/hooks/use-assistant-status')
vi.mock('@/hooks/use-manuals', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/hooks/use-manuals')>()
  return { ...actual, useManuals: vi.fn() }
})
// note-editor.tsx is the lazy-loaded Plate-based WYSIWYG editor (ADR 0117) -
// its own round-trip/golden-file coverage lives in note-editor.test.tsx. This
// file only needs to prove the viewer's Edit toggle reaches it and that Save
// goes through use-notes.ts's patchNote, so it stands in a lightweight fake
// exposing exactly that contract.
vi.mock('@/components/note-editor', () => ({
  NoteEditor: ({ value, onSave }: { value: string; onSave: (body: string) => void | Promise<void> }) => (
    <div>
      <p>editor: {value}</p>
      <button type="button" onClick={() => { void onSave(`${value} edited`) }}>Save</button>
    </div>
  ),
}))
// checklist-runner.tsx has its own full coverage against a mocked
// use-checklist-run (checklist-runner.test.tsx) - this file only needs to
// prove the viewer's Start checklist toggle reaches it with the right
// note, and that Back exits the runner mode without abandoning the run.
vi.mock('@/components/documents/checklist-runner', () => ({
  ChecklistRunner: ({ noteId, noteTitle, onExit }: { noteId: string; noteTitle: string; onExit: () => void }) => (
    <div>
      <p>checklist runner: {noteId} ({noteTitle})</p>
      <button type="button" onClick={onExit}>Back</button>
    </div>
  ),
}))

const mockedUseDocuments = vi.mocked(useDocuments)
const mockedUseDocumentUploads = vi.mocked(useDocumentUploads)
const mockedUseManuals = vi.mocked(useManuals)
const mockedUseNotes = vi.mocked(useNotes)
const mockedUseAssistantStatus = vi.mocked(useAssistantStatus)

type DocumentsMock = ReturnType<typeof useDocuments>
type UploadsMock = ReturnType<typeof useDocumentUploads>

function makeDocumentsMock(overrides: Partial<DocumentsMock> = {}): DocumentsMock {
  return {
    path: [],
    folders: [],
    documents: [],
    loading: false,
    error: null,
    refresh: vi.fn(),
    tags: [],
    selectedTag: null,
    setSelectedTag: vi.fn(),
    searchResults: null,
    searching: false,
    searchError: null,
    semanticProblem: null,
    search: vi.fn(),
    clearSearch: vi.fn(),
    embeddingsStatus: null,
    createFolder: vi.fn(),
    renameFolder: vi.fn(),
    moveFolder: vi.fn(),
    deleteFolder: vi.fn(),
    patchDocument: vi.fn(),
    deleteDocument: vi.fn(),
    reindexDocument: vi.fn(),
    moveDocuments: vi.fn(),
    ...overrides,
  }
}

function makeUploadsMock(overrides: Partial<UploadsMock> = {}): UploadsMock {
  return {
    items: [],
    add: vi.fn(),
    remove: vi.fn(),
    clear: vi.fn(),
    ready: true,
    error: null,
    ...overrides,
  }
}

function doc(overrides: Partial<import('@/hooks/use-documents').DocumentRecord> = {}) {
  return {
    id: 'doc-1',
    sha256: 'abc123',
    folder_id: null,
    filename: 'manual.pdf',
    title: '',
    notes: '',
    mime: 'application/pdf',
    size_bytes: 123456,
    page_count: 5,
    summary: '',
    status: 'indexed' as const,
    stage: 'done',
    indexed_with: 'local',
    error: '',
    index_model: '',
    index_cost_usd: 0,
    tags: [],
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    indexed_at: '2026-01-01T00:00:00Z',
    kind: 'file',
    note_type: '',
    note_type_source: '',
    pinned: false,
    sort_index: 0,
    ...overrides,
  }
}

function embeddingsStatus(overrides: Partial<import('@/hooks/use-documents').DocumentEmbeddingsStatus> = {}) {
  return {
    enabled: true,
    model: 'openai/text-embedding-3-small',
    dimensions: 512,
    counts: { chunks_total: 10, chunks_embedded: 10, chunks_stale: 0, chunks_pending: 0, chunks_pending_auto: 0, chars_pending: 0 },
    backfill: { running: false, chunks_embedded: 0, started_at: '' },
    ...overrides,
  }
}

type ManualsMock = ReturnType<typeof useManuals>
type NotesMock = ReturnType<typeof useNotes>

function makeManualsMock(overrides: Partial<ManualsMock> = {}): ManualsMock {
  return {
    manuals: [],
    manualsLoading: false,
    manualsError: null,
    refreshManuals: vi.fn(),
    tree: null,
    treeLoading: false,
    treeError: null,
    refreshTree: vi.fn(),
    createManual: vi.fn(),
    flagManual: vi.fn(),
    clearManual: vi.fn(),
    reorder: vi.fn(),
    ...overrides,
  }
}

function makeNotesMock(overrides: Partial<NotesMock> = {}): NotesMock {
  return {
    notes: [],
    loading: false,
    error: null,
    refresh: vi.fn(),
    // A note's body now comes from GET /api/notes/:id, not from the
    // chunk-reassembling /text endpoint, so the default has to resolve -
    // an un-stubbed getNote would make every viewer test fail on an
    // undefined detail rather than on what it is actually asserting.
    getNote: vi.fn().mockResolvedValue({ document: note(), body: 'Open the seacock first.' }),
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
    filename: 'Genset start-up.md',
    title: 'Genset start-up',
    notes: '',
    mime: 'text/markdown',
    size_bytes: 256,
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
    note_type: 'procedure',
    note_type_source: 'auto',
    pinned: false,
    sort_index: 0,
    ...overrides,
  }
}

function docNode(overrides: Partial<ManualTreeNode> = {}): ManualTreeNode {
  return {
    type: 'document', id: 'n1', name: 'Genset start-up', sort_index: 0, updated_at: '2026-01-01T00:00:00Z',
    kind: 'note', note_type: 'procedure',
    ...overrides,
  }
}

beforeEach(() => {
  vi.clearAllMocks()
  mockedUseDocuments.mockReturnValue(makeDocumentsMock())
  mockedUseDocumentUploads.mockReturnValue(makeUploadsMock())
  mockedUseManuals.mockReturnValue(makeManualsMock())
  mockedUseNotes.mockReturnValue(makeNotesMock())
  // Available by default (enabled/configured, no problem) - the panel's
  // existing coverage isn't about Mate at all, so it shouldn't have to
  // opt into "available" every time; ask-mate-selection.test.tsx and its
  // own wiring cover the unavailable case.
  mockedUseAssistantStatus.mockReturnValue({
    status: { enabled: true, configured: true, model: 'test-model' },
    loading: false,
    error: null,
    refresh: vi.fn(),
  })
})

afterEach(() => {
  vi.restoreAllMocks()
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('DocumentsPanel', () => {
  it('sends the typed query, debounced, scoped to the current folder', () => {
    vi.useFakeTimers()
    const search = vi.fn()
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({ search }))

    render(<DocumentsPanel initialFolderId="f1" />)

    fireEvent.change(screen.getByRole('searchbox', { name: /search documents/i }), { target: { value: 'impeller' } })
    expect(search).not.toHaveBeenCalled()

    act(() => { vi.advanceTimersByTime(250) })

    expect(search).toHaveBeenCalledWith('impeller', { allFolders: false })
  })

  it('searches every folder once "All folders" is switched on', () => {
    vi.useFakeTimers()
    const search = vi.fn()
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({ search }))

    render(<DocumentsPanel initialFolderId="f1" />)

    fireEvent.click(screen.getByRole('switch', { name: /all folders/i }))
    fireEvent.change(screen.getByRole('searchbox', { name: /search documents/i }), { target: { value: 'impeller' } })
    act(() => { vi.advanceTimersByTime(250) })

    expect(search).toHaveBeenCalledWith('impeller', { allFolders: true })
  })

  it('renders \\x02/\\x03 snippet markers as <mark> text, never as injected HTML', () => {
    const results: import('@/hooks/use-documents').DocumentSearchResult[] = [{
      document_id: 'doc-1',
      filename: 'manual.pdf',
      title: '',
      status: 'indexed',
      page: 4,
      snippet: 'the \x02<img src=x onerror=alert(1)>\x03 kit',
      folder_id: null,
    }]
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({ searchResults: results }))

    const { container } = render(<DocumentsPanel />)

    const mark = container.querySelector('mark')
    expect(mark).not.toBeNull()
    expect(mark?.textContent).toBe('<img src=x onerror=alert(1)>')
    // Never actually parsed as markup - no <img> was created from the snippet.
    expect(container.querySelector('img')).toBeNull()
  })

  it('shows each search hit\'s page and folder path', async () => {
    // Search results only ever carry folder_id (backend/documents_search.go's
    // documentSearchResult), never a name or path, so the panel resolves it
    // itself via GET /api/document-folders?parent=<id> (the same endpoint
    // folder browsing already uses) - one real fetch, not routed through the
    // mocked useDocuments hook.
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ path: [{ id: 'f1', name: 'Manuals', parent_id: null }], folders: [], documents: [] }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const results: import('@/hooks/use-documents').DocumentSearchResult[] = [{
      document_id: 'doc-1',
      filename: 'manual.pdf',
      title: '',
      status: 'indexed',
      page: 4,
      snippet: 'the \x02impeller\x03 kit',
      folder_id: 'f1',
    }]
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({ searchResults: results }))

    render(<DocumentsPanel />)

    expect(screen.getByText(/page 4/i)).toBeInTheDocument()
    expect(await screen.findByText(/manuals$/i)).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith('/api/document-folders?parent=f1')
  })

  it('shows "Documents" as the folder path for a root-level search hit', async () => {
    const results: import('@/hooks/use-documents').DocumentSearchResult[] = [{
      document_id: 'doc-1',
      filename: 'manual.pdf',
      title: '',
      status: 'indexed',
      page: 1,
      snippet: 'the \x02impeller\x03 kit',
      folder_id: null,
    }]
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({ searchResults: results }))

    render(<DocumentsPanel />)

    // Scoped to the results list - "Documents" is also the breadcrumb's own
    // root crumb, always on screen regardless of search state. Substring
    // match: the folder label shares one line with "Page N · ".
    const resultsList = screen.getByTestId('documents-search-results')
    expect(within(resultsList).getByText(/documents$/i)).toBeInTheDocument()
  })

  it('navigating into a folder calls onFolderChange with its id', () => {
    const onFolderChange = vi.fn()
    mockedUseDocuments.mockImplementation((folderId) => {
      if (folderId === null) {
        return makeDocumentsMock({ folders: [{ id: 'f1', name: 'Manuals', parent_id: null }] })
      }
      return makeDocumentsMock({ path: [{ id: 'f1', name: 'Manuals', parent_id: null }] })
    })

    render(<DocumentsPanel onFolderChange={onFolderChange} />)

    fireEvent.click(screen.getByRole('button', { name: 'Manuals' }))

    expect(onFolderChange).toHaveBeenCalledWith('f1')
  })

  it('browser Back and Forward move the panel between folders', () => {
    // App.tsx keeps the Suspense key on 'documents' while browsing (it only
    // remounts the panel on a panel switch), so Back/Forward have to reach
    // the panel as a changed `initialFolderId` prop on an already-mounted
    // instance - not a fresh mount - the same way AssistantDrawer picks up
    // a changed `initialConversationId`. `rerender` with a new prop is
    // exactly that.
    mockedUseDocuments.mockImplementation((folderId) => {
      if (folderId === null) {
        return makeDocumentsMock({ folders: [{ id: 'f1', name: 'Manuals', parent_id: null }] })
      }
      return makeDocumentsMock({ path: [{ id: 'f1', name: 'Manuals', parent_id: null }] })
    })
    const onFolderChange = vi.fn()

    const { rerender } = render(<DocumentsPanel initialFolderId={null} onFolderChange={onFolderChange} />)

    fireEvent.click(screen.getByRole('button', { name: 'Manuals' }))
    expect(onFolderChange).toHaveBeenCalledWith('f1')
    expect(mockedUseDocuments).toHaveBeenLastCalledWith('f1')
    expect(within(screen.getByRole('navigation', { name: /breadcrumb/i })).getByText('Manuals')).toBeInTheDocument()

    // App.tsx's onFolderChange handler mirrors the new folder into its own
    // documentsFolderId state (and the URL) synchronously, so the panel's
    // very next render already carries it back down as a prop, not just as
    // the panel's own internal state.
    rerender(<DocumentsPanel initialFolderId="f1" onFolderChange={onFolderChange} />)

    // Back: App.tsx's popstate handler re-seeds documentsFolderId from the
    // URL and hands it back down as a prop, without remounting the panel.
    rerender(<DocumentsPanel initialFolderId={null} onFolderChange={onFolderChange} />)
    expect(mockedUseDocuments).toHaveBeenLastCalledWith(null)
    expect(within(screen.getByRole('navigation', { name: /breadcrumb/i })).queryByText('Manuals')).not.toBeInTheDocument()

    // Forward: back to f1.
    rerender(<DocumentsPanel initialFolderId="f1" onFolderChange={onFolderChange} />)
    expect(mockedUseDocuments).toHaveBeenLastCalledWith('f1')
    expect(within(screen.getByRole('navigation', { name: /breadcrumb/i })).getByText('Manuals')).toBeInTheDocument()
  })

  it('the breadcrumb reflects the folder path reported for the current folder', () => {
    mockedUseDocuments.mockImplementation((folderId) =>
      makeDocumentsMock(
        folderId === 'f1' ? { path: [{ id: 'f1', name: 'Manuals', parent_id: null }] } : {},
      ),
    )

    render(<DocumentsPanel initialFolderId="f1" />)

    const breadcrumb = screen.getByRole('navigation', { name: /breadcrumb/i })
    expect(within(breadcrumb).getByText('Manuals')).toBeInTheDocument()
  })

  it('uploads into the current folder', () => {
    // Held directly, not read back via mockedUseDocumentUploads.mock.results[0]
    // - vi.restoreAllMocks() (afterEach) doesn't clear an automocked module
    //   function's call history between tests in this file, so `results[0]`
    //   would otherwise mean "the first render across the whole suite", not
    //   this test's own.
    const add = vi.fn()
    mockedUseDocumentUploads.mockReturnValue(makeUploadsMock({ add }))

    render(<DocumentsPanel initialFolderId="f1" />)

    // No attachment cap for the panel (review finding, use-document-uploads.ts:173
    // - MAX_ATTACHMENTS is Mate's per-message limit, not a general uploading
    // one), and sequential so a large dropped batch doesn't open one XHR per
    // file at once.
    expect(mockedUseDocumentUploads).toHaveBeenCalledWith('f1', { maxAttachments: Number.POSITIVE_INFINITY, sequential: true })

    const file = new File(['hello'], 'receipt.pdf', { type: 'application/pdf' })
    const input = screen.getByTestId('documents-file-input') as HTMLInputElement
    fireEvent.change(input, { target: { files: [file] } })

    expect(add).toHaveBeenCalledWith([file])
  })

  it('shows a staged upload\'s progress, then refreshes the listing and clears the chip once it lands', () => {
    const refresh = vi.fn()
    const remove = vi.fn()
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({ refresh }))
    mockedUseDocumentUploads.mockReturnValue(makeUploadsMock({
      remove,
      items: [{
        key: 'doc-upload-1',
        filename: 'receipt.pdf',
        size: 1234,
        status: 'uploading',
        progress: 42,
        documentId: null,
        error: null,
        duplicate: false,
      }],
    }))

    const { rerender } = render(<DocumentsPanel initialFolderId="f1" />)

    expect(screen.getByText('receipt.pdf')).toBeInTheDocument()
    expect(screen.getByText('42%')).toBeInTheDocument()
    expect(refresh).not.toHaveBeenCalled()
    expect(remove).not.toHaveBeenCalled()

    mockedUseDocumentUploads.mockReturnValue(makeUploadsMock({
      remove,
      items: [{
        key: 'doc-upload-1',
        filename: 'receipt.pdf',
        size: 1234,
        status: 'indexed',
        progress: 100,
        documentId: 'doc-9',
        error: null,
        duplicate: false,
      }],
    }))
    rerender(<DocumentsPanel initialFolderId="f1" />)

    expect(refresh).toHaveBeenCalled()
    expect(remove).toHaveBeenCalledWith('doc-upload-1')
  })

  it('shows a rejected upload\'s error with no chip and no refresh', () => {
    const refresh = vi.fn()
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({ refresh }))
    mockedUseDocumentUploads.mockReturnValue(makeUploadsMock({
      error: 'upload exceeds the 104857600 byte limit',
      items: [],
    }))

    render(<DocumentsPanel initialFolderId="f1" />)

    expect(screen.getByRole('alert')).toHaveTextContent('upload exceeds the 104857600 byte limit')
    expect(screen.queryByTestId('documents-upload-items')).not.toBeInTheDocument()
    expect(refresh).not.toHaveBeenCalled()
  })

  // Review finding (documents-panel.tsx:779): removing the Cost column
  // dropped one <TableHead> but two placeholder <TableCell />s from the
  // folder row, so a folder row emitted 5 cells against the header's 6 -
  // the kebab menu landed under "Size" and the actions column sat empty.
  it('a folder row has as many cells as the header has columns, with the actions button in the last cell', () => {
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({
      folders: [{ id: 'f1', name: 'Manuals', parent_id: null }],
    }))

    render(<DocumentsPanel />)

    const headerCells = screen.getAllByRole('columnheader')
    const folderRow = screen.getByRole('button', { name: 'Manuals' }).closest('tr')
    expect(folderRow).not.toBeNull()
    const rowCells = within(folderRow!).getAllByRole('cell')
    expect(rowCells).toHaveLength(headerCells.length)
    expect(within(rowCells[rowCells.length - 1]).getByRole('button', { name: /actions for manuals/i })).toBeInTheDocument()
  })

  it('shows a failed document\'s own error', () => {
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({
      documents: [doc({ status: 'failed', error: 'could not read PDF' })],
    }))

    render(<DocumentsPanel />)

    expect(screen.getByText('could not read PDF')).toBeInTheDocument()
  })

  // ADR 0115 §1: the per-row indexing cost moves to the new Details
  // page (document-details-page.test.tsx), which is the only place an
  // operator can already see index_cost_usd's four-decimal figure - the
  // listing itself never needs to justify a receipt at a glance, and a
  // three-decimal column here was rounding a $0.0012 charge down to $0.001.
  it('has no Cost column, and does not render index_cost_usd, in the listing', () => {
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({
      documents: [doc({ index_cost_usd: 0.0123 })],
    }))

    render(<DocumentsPanel />)

    expect(screen.queryByRole('columnheader', { name: /cost/i })).not.toBeInTheDocument()
    expect(screen.queryByText(/0\.0123/)).not.toBeInTheDocument()
    expect(screen.queryByText(/\$0\.012/)).not.toBeInTheDocument()
  })

  // ADR 0115 §2: Details… opens the new full-panel metadata page
  // (document-details-page.test.tsx covers what that page itself does) -
  // this only pins that the row menu hands App.tsx the right id.
  it('the row menu\'s Details item calls onEditDocument with that document\'s id', () => {
    const onEditDocument = vi.fn()
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({
      documents: [doc({ id: 'doc-7', filename: 'receipt.pdf' })],
    }))

    render(<DocumentsPanel onEditDocument={onEditDocument} />)

    fireEvent.click(screen.getByRole('button', { name: /actions for receipt\.pdf/i }))
    fireEvent.click(screen.getByRole('menuitem', { name: /details/i }))

    expect(onEditDocument).toHaveBeenCalledWith('doc-7')
  })

  it('the viewer\'s Details button calls onEditDocument for the open document', () => {
    const onEditDocument = vi.fn()
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({
      documents: [doc({ id: 'doc-7', filename: 'receipt.pdf' })],
    }))

    render(<DocumentsPanel onEditDocument={onEditDocument} />)

    fireEvent.click(screen.getByText('receipt.pdf'))
    fireEvent.click(screen.getByRole('button', { name: 'Details' }))

    expect(onEditDocument).toHaveBeenCalledWith('doc-7')
  })

  // ADR 0116: a filed note is a document with mime: 'text/markdown', and the
  // viewer renders it through NoteMarkdown - the same renderer the Notes
  // panel's own reader uses - rather than the raw <pre> every other text
  // mime still gets. This makes a note readable from Documents too, and
  // exercises text/markdown's frontmatter-stripped body (extractTextFile,
  // backend) rendering as actual markdown rather than as literal text.
  describe('the viewer', () => {
    it('renders a text/markdown document through NoteMarkdown, not a <pre>', async () => {
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({ text: 'Open the **seacock** first.' }),
      }))
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({
        documents: [doc({ id: 'note-1', mime: 'text/markdown', filename: 'genset.md', title: 'Genset start-up' })],
      }))

      const { container } = render(<DocumentsPanel />)
      fireEvent.click(screen.getByRole('button', { name: 'Genset start-up' }))

      await screen.findByText(/Open the/)
      expect(screen.getByText('seacock')).toBeInTheDocument() // ** rendered as emphasis, not literal asterisks
      expect(container.querySelector('pre')).toBeNull()
    })

    it('still renders a plain text/plain document through the raw <pre>', async () => {
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({ text: 'raw log output, unformatted' }),
      }))
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({
        documents: [doc({ id: 'log-1', mime: 'text/plain', filename: 'engine.log', title: 'Engine log' })],
      }))

      render(<DocumentsPanel />)
      fireEvent.click(screen.getByRole('button', { name: 'Engine log' }))

      const pre = await screen.findByText('raw log output, unformatted')
      expect(pre.tagName).toBe('PRE')
    })
  })

  it('delete asks for confirmation before calling deleteDocument', async () => {
    const deleteDocument = vi.fn().mockResolvedValue(undefined)
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({
      documents: [doc({ filename: 'receipt.pdf' })],
      deleteDocument,
    }))

    render(<DocumentsPanel />)

    fireEvent.click(screen.getByRole('button', { name: /actions for receipt\.pdf/i }))
    fireEvent.click(screen.getByRole('menuitem', { name: /delete/i }))

    // Not called yet - the alert dialog is still asking.
    expect(deleteDocument).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))

    await waitFor(() => expect(deleteDocument).toHaveBeenCalledWith('doc-1'))
  })

  // ADR 0106 review finding (documents-panel.tsx:723): Download used to
  // navigate the whole tab with `window.location.href = contentUrlFor(...)`,
  // so a 401 or the endpoint's 500 "document file missing on disk"
  // (documentContentHandler, backend/documents_handlers.go) rendered raw
  // JSON in place of the SPA, with no way back short of a reload. The fix
  // fetches into a blob instead, so a failure surfaces in the panel's own
  // actionError banner (same one rename/move/delete already use) and the
  // app never navigates away.
  it('a failed download shows the error in the panel and never navigates away', async () => {
    const hrefBeforeClick = window.location.href
    const fetchMock = vi.fn().mockResolvedValue({
      ok: false,
      status: 500,
      json: async () => ({ error: 'document file missing on disk' }),
    })
    vi.stubGlobal('fetch', fetchMock)
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({
      documents: [doc({ filename: 'receipt.pdf' })],
    }))

    render(<DocumentsPanel />)

    fireEvent.click(screen.getByRole('button', { name: /actions for receipt\.pdf/i }))
    fireEvent.click(screen.getByRole('menuitem', { name: /download/i }))

    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('document file missing on disk'))

    expect(fetchMock).toHaveBeenCalledWith('/api/documents/doc-1/content?download=1')
    // The whole point: no window.location.href assignment, so the tab never
    // left the SPA for the endpoint's raw JSON error response.
    expect(window.location.href).toBe(hrefBeforeClick)
  })

  it('a successful download fetches the file into a blob, not a page navigation', async () => {
    const blob = new Blob(['%PDF-1.4 fake content'], { type: 'application/pdf' })
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      blob: async () => blob,
    })
    vi.stubGlobal('fetch', fetchMock)
    // Spies on the real URL statics rather than replacing the global URL
    // object outright - happy-dom's own anchor-click navigation internals
    // construct real URLs, and a wholesale stub broke that with an
    // unrelated "URL is not a constructor" error.
    const createObjectURL = vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:mock-url')
    const revokeObjectURL = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({
      documents: [doc({ filename: 'receipt.pdf' })],
    }))

    render(<DocumentsPanel />)

    fireEvent.click(screen.getByRole('button', { name: /actions for receipt\.pdf/i }))
    fireEvent.click(screen.getByRole('menuitem', { name: /download/i }))

    await waitFor(() => expect(createObjectURL).toHaveBeenCalledWith(blob))
    expect(fetchMock).toHaveBeenCalledWith('/api/documents/doc-1/content?download=1')
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:mock-url')
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    // Not asserting window.location.href here: happy-dom's anchor click
    // simulation navigates on any href regardless of the `download`
    // attribute (unlike a real browser, where `download` makes the click
    // save the blob instead of navigating) - the failure-path test above is
    // what actually pins "never navigates", since that path never creates
    // or clicks an anchor at all.
  })

  it('bulk move sends every selected document id', async () => {
    const moveDocuments = vi.fn().mockResolvedValue(undefined)
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({
      documents: [doc({ id: 'doc-1', filename: 'a.pdf' }), doc({ id: 'doc-2', filename: 'b.pdf' })],
      folders: [{ id: 'f1', name: 'Manuals', parent_id: null }],
      moveDocuments,
    }))

    render(<DocumentsPanel />)

    fireEvent.click(screen.getByRole('checkbox', { name: 'Select a.pdf' }))
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select b.pdf' }))

    fireEvent.click(screen.getByRole('button', { name: /move to/i }))
    fireEvent.click(screen.getByRole('button', { name: 'Move here' }))

    await waitFor(() => expect(moveDocuments).toHaveBeenCalledWith(['doc-1', 'doc-2'], null))
  })

  // ADR 0115 §6: a move no longer has to be abandoned to go create
  // the destination folder first - the picker gets its own create-here row.
  describe('creating a folder from the Move dialog', () => {
    // src/test/setup.ts deletes Element.prototype.getAnimations globally so
    // Base UI's Dialog/AlertDialog/Tabs close synchronously under this
    // suite's fireEvent-driven assertions (see that file's own comment).
    // FolderPicker's ScrollArea (@base-ui/react/scroll-area) hits a
    // different, unguarded call to the same API on mount - it schedules a
    // 0ms timeout that unconditionally calls viewport.getAnimations() to
    // recompute thumb geometry after any subtree animation - and unlike the
    // Dialog/Tabs code, it doesn't feature-detect a missing implementation
    // first. That timeout only gets a chance to actually fire (as an
    // unhandled exception, since it's a bare timer callback, not something
    // these tests await) once a test's own await gives the event loop room
    // to run it - which only these two tests below do, by waiting on the
    // create-folder round trip. A local stub, scoped to just this describe
    // block and removed again afterwards, satisfies ScrollArea's mount
    // effect without weakening the global shim the rest of the suite
    // depends on for Dialog/Tabs.
    beforeEach(() => {
      Element.prototype.getAnimations = () => []
    })
    afterEach(() => {
      delete (Element.prototype as { getAnimations?: unknown }).getAnimations
    })

    it('creates the folder and descends into it, ready for Move here', async () => {
      const createFolder = vi.fn().mockResolvedValue({ id: 'f9', name: '2026', parent_id: null })
      // The picker owns its own useDocuments(pickerFolderId) instance
      // (FolderPicker's own doc comment) - keyed by folderId so the
      // breadcrumb reflects wherever createFolder's setPickerFolderId(created.id)
      // just sent it, the same way the panel's own folder navigation does.
      mockedUseDocuments.mockImplementation((folderId) => {
        if (folderId === 'f9') {
          return makeDocumentsMock({ path: [{ id: 'f9', name: '2026', parent_id: null }], createFolder })
        }
        return makeDocumentsMock({ documents: [doc({ filename: 'receipt.pdf' })], createFolder })
      })

      render(<DocumentsPanel />)

      fireEvent.click(screen.getByRole('button', { name: /actions for receipt\.pdf/i }))
      fireEvent.click(screen.getByRole('menuitem', { name: /^move/i }))

      fireEvent.change(screen.getByRole('textbox', { name: /new folder name/i }), { target: { value: '2026' } })
      fireEvent.click(screen.getByRole('button', { name: 'Create folder' }))

      await waitFor(() => expect(createFolder).toHaveBeenCalledWith('2026', null))
      expect(await screen.findByText('2026')).toBeInTheDocument()
    })

    // Review finding: the picker owns its own useDocuments(pickerFolderId)
    // instance, so a folder it creates used to refresh only the picker -
    // cancelling the dialog left the panel's own listing never having heard
    // of it, invisible in the table, with a second attempt from the
    // toolbar's New folder button hitting a 409 for a folder the operator
    // could not see. A single shared mock object stands in for both
    // useDocuments instances here (unlike the test above, which needs two
    // distinct ones to prove the picker's own descend-into-it behaviour) -
    // this test only cares that creating a folder reaches the panel's own
    // `refresh`, which nothing in either component calls on its own without
    // the fix.
    it('creating a folder from the Move dialog also refreshes the panel\'s own listing', async () => {
      const createFolder = vi.fn().mockResolvedValue({ id: 'f9', name: '2026', parent_id: null })
      const refresh = vi.fn()
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({
        documents: [doc({ filename: 'receipt.pdf' })],
        createFolder,
        refresh,
      }))

      render(<DocumentsPanel />)

      fireEvent.click(screen.getByRole('button', { name: /actions for receipt\.pdf/i }))
      fireEvent.click(screen.getByRole('menuitem', { name: /^move/i }))

      fireEvent.change(screen.getByRole('textbox', { name: /new folder name/i }), { target: { value: '2026' } })
      fireEvent.click(screen.getByRole('button', { name: 'Create folder' }))

      await waitFor(() => expect(createFolder).toHaveBeenCalledWith('2026', null))
      await waitFor(() => expect(refresh).toHaveBeenCalled())
    })

    it('a rejected create shows the server\'s message and moves nothing', async () => {
      const createFolder = vi.fn().mockRejectedValue(new Error('folder name already exists'))
      const moveDocuments = vi.fn()
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({
        documents: [doc({ filename: 'receipt.pdf' })],
        createFolder,
        moveDocuments,
      }))

      render(<DocumentsPanel />)

      fireEvent.click(screen.getByRole('button', { name: /actions for receipt\.pdf/i }))
      fireEvent.click(screen.getByRole('menuitem', { name: /^move/i }))

      fireEvent.change(screen.getByRole('textbox', { name: /new folder name/i }), { target: { value: '2026' } })
      fireEvent.click(screen.getByRole('button', { name: 'Create folder' }))

      expect(await screen.findByText('folder name already exists')).toBeInTheDocument()
      expect(moveDocuments).not.toHaveBeenCalled()
    })
  })

  it('bulk delete also asks for confirmation before deleting every selected document', async () => {
    const deleteDocument = vi.fn().mockResolvedValue(undefined)
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({
      documents: [doc({ id: 'doc-1', filename: 'a.pdf' }), doc({ id: 'doc-2', filename: 'b.pdf' })],
      deleteDocument,
    }))

    render(<DocumentsPanel />)

    fireEvent.click(screen.getByRole('checkbox', { name: 'Select a.pdf' }))
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select b.pdf' }))

    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
    // Not called yet - only the bar's own button was clicked, the alert
    // dialog (a second, distinct "Delete" button) is still asking.
    expect(deleteDocument).not.toHaveBeenCalled()

    fireEvent.click(screen.getAllByRole('button', { name: 'Delete' }).at(-1)!)

    await waitFor(() => expect(deleteDocument).toHaveBeenCalledWith('doc-1'))
    expect(deleteDocument).toHaveBeenCalledWith('doc-2')
  })

  it('shows the server\'s 409 message when a folder delete is refused', async () => {
    const deleteFolder = vi.fn().mockRejectedValue(new Error('folder is not empty'))
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({
      folders: [{ id: 'f1', name: 'Manuals', parent_id: null }],
      deleteFolder,
    }))

    render(<DocumentsPanel />)

    fireEvent.click(screen.getByRole('button', { name: /actions for manuals/i }))
    fireEvent.click(screen.getByRole('menuitem', { name: /delete/i }))
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))

    expect(await screen.findByText('folder is not empty')).toBeInTheDocument()
  })

  // ADR 0106 E1c (F1e): a search response's semantic_problem is present
  // only when semantic search was configured and failed to run for THIS
  // query - never when it's simply switched off - so it's a quiet notice
  // beside real, complete keyword results, not an error banner.
  describe('semantic_problem notice', () => {
    const oneResult: import('@/hooks/use-documents').DocumentSearchResult[] = [{
      document_id: 'doc-1',
      filename: 'manual.pdf',
      title: '',
      status: 'indexed',
      page: 1,
      snippet: 'the \x02impeller\x03 kit',
      folder_id: null,
    }]

    it('shows a quiet notice, not an error, when a search response carries semantic_problem', () => {
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({
        searchResults: oneResult,
        semanticProblem: 'embedding the query failed: rate limited',
      }))

      render(<DocumentsPanel />)

      const notice = screen.getByText(/showing keyword results only/i)
      expect(notice).toHaveTextContent('embedding the query failed: rate limited')
      // Not the destructive/error styling this panel already uses for a
      // fatal problem (e.g. a failed document's own error, or searchError)
      // - a quiet warning, matching forecast-drawer.tsx's own amber
      // non-fatal-notice language rather than inventing a new one.
      expect(notice.className).not.toContain('text-destructive')
      expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    })

    it('shows no notice when the search response did not carry a semantic_problem', () => {
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({ searchResults: oneResult, semanticProblem: null }))

      render(<DocumentsPanel />)

      expect(screen.queryByText(/showing keyword results only/i)).not.toBeInTheDocument()
    })

    it('a second, clean search clears a previously shown notice', () => {
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({
        searchResults: oneResult,
        semanticProblem: 'no embedding model reachable',
      }))

      const { rerender } = render(<DocumentsPanel />)
      expect(screen.getByText(/showing keyword results only/i)).toBeInTheDocument()

      // use-documents.test.ts pins the hook's own clearing behaviour
      // (search() overwriting semanticProblem from the fresh response); this
      // only checks the panel actually reacts to that state change.
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({ searchResults: oneResult, semanticProblem: null }))
      rerender(<DocumentsPanel />)

      expect(screen.queryByText(/showing keyword results only/i)).not.toBeInTheDocument()
    })
  })

  // ADR 0120: "turning Mate on is the consent" retired the toolbar's three
  // backlog banners (semantic search indexing, note classification, note
  // enrichment) and the confirm dialogs that went with them. The automatic
  // sweep (backend) does that work itself once Mate is ready; the only
  // state left worth a line here is a failure.
  describe('Mate failure line', () => {
    it('renders nothing before the initial status load resolves', () => {
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({ embeddingsStatus: null }))

      render(<DocumentsPanel />)

      expect(screen.queryByTestId('documents-mate-error')).not.toBeInTheDocument()
    })

    it('renders nothing when semantic search is off', () => {
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({
        embeddingsStatus: embeddingsStatus({ enabled: false, model: '', dimensions: 0, problem: 'No embedding model is configured. Set one in Settings → Assistant.' }),
      }))

      render(<DocumentsPanel />)

      expect(screen.queryByTestId('documents-mate-error')).not.toBeInTheDocument()
    })

    it('renders nothing while the library is healthy, including mid-sweep', () => {
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({
        embeddingsStatus: embeddingsStatus({
          counts: { chunks_total: 20, chunks_embedded: 15, chunks_stale: 0, chunks_pending: 5, chunks_pending_auto: 5, chars_pending: 2000 },
          backfill: { running: true, chunks_embedded: 7, started_at: '2026-09-18T00:00:00Z' },
        }),
      }))

      render(<DocumentsPanel />)

      expect(screen.queryByTestId('documents-mate-error')).not.toBeInTheDocument()
    })

    it('names the error and links to Mate settings when the sweep hit one', () => {
      const onOpenAssistantSettings = vi.fn()
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({
        embeddingsStatus: embeddingsStatus({
          counts: { chunks_total: 20, chunks_embedded: 15, chunks_stale: 0, chunks_pending: 5, chunks_pending_auto: 5, chars_pending: 2000 },
          backfill: { running: true, chunks_embedded: 7, started_at: '2026-09-18T00:00:00Z', last_error: 'OpenRouter: 429 rate limited' },
        }),
      }))

      render(<DocumentsPanel onOpenAssistantSettings={onOpenAssistantSettings} />)

      expect(screen.getByText(/OpenRouter: 429 rate limited/)).toBeInTheDocument()

      fireEvent.click(screen.getByRole('button', { name: /open mate settings/i }))
      expect(onOpenAssistantSettings).toHaveBeenCalledTimes(1)
    })
  })

  // ADR 0121: capture is no longer a global action - DocumentsPanel owns
  // NoteCaptureSheet entirely now, with no onCaptureNote prop for App.tsx
  // (or a test) to reach in from outside. New → Note is the only door in.
  describe('capture: New → Note', () => {
    // One New menu, Folder first, Note carrying its kinds in a submenu.
    // Auto leads that submenu and must pass NO kind, so the backend's own
    // classifier answers rather than the menu guessing on the operator's
    // behalf (ADR 0116).
    it('New → Note → Auto opens the capture sheet on Auto, with no onCaptureNote prop anywhere', async () => {
      render(<DocumentsPanel />)

      fireEvent.click(screen.getByRole('button', { name: 'New' }))
      fireEvent.click(await screen.findByRole('menuitem', { name: 'Note' }))
      fireEvent.click(await screen.findByRole('menuitem', { name: 'Auto' }))

      // A longer timeout than findByRole's default 1s: this is a
      // lazy-loaded chunk (note-capture-sheet.tsx) resolving via dynamic
      // import(), the same flakiness app-sidebar-navigation.test.tsx's
      // deleted equivalent test used to guard against.
      expect(await screen.findByRole('heading', { name: 'Capture a note' }, { timeout: 5000 })).toBeInTheDocument()
      expect(screen.getByRole('combobox', { name: 'Note type' })).toHaveTextContent('Auto')
    })

    it('New → Note → a kind opens the sheet already on that kind', async () => {
      render(<DocumentsPanel />)

      fireEvent.click(screen.getByRole('button', { name: 'New' }))
      fireEvent.click(await screen.findByRole('menuitem', { name: 'Note' }))
      fireEvent.click(await screen.findByRole('menuitem', { name: 'Quirk' }))

      expect(await screen.findByRole('heading', { name: 'Capture a note' }, { timeout: 5000 })).toBeInTheDocument()
      expect(screen.getByRole('combobox', { name: 'Note type' })).toHaveTextContent('Quirk')
    })

    it('offers every kind the classifier can produce, Auto first', async () => {
      render(<DocumentsPanel />)

      fireEvent.click(screen.getByRole('button', { name: 'New' }))
      fireEvent.click(await screen.findByRole('menuitem', { name: 'Note' }))

      for (const label of ['Auto', 'Contact', 'Procedure', 'Spec', 'Quirk', 'Recipe', 'Plain note']) {
        expect(await screen.findByRole('menuitem', { name: label })).toBeInTheDocument()
      }
    })

    it('has no second create control beside New', () => {
      render(<DocumentsPanel />)

      expect(screen.queryByRole('button', { name: 'Add Note' })).not.toBeInTheDocument()
      expect(screen.queryByRole('button', { name: 'Choose note type' })).not.toBeInTheDocument()
    })

    it('New → Folder still opens the ordinary new-folder dialog', () => {
      render(<DocumentsPanel />)

      fireEvent.click(screen.getByRole('button', { name: 'New' }))
      fireEvent.click(screen.getByRole('menuitem', { name: 'Folder' }))

      expect(screen.getByRole('dialog')).toHaveTextContent('New folder')
    })
  })

  // The pre-attentive type icon column (revision §"Note types as a facet")
  // on an ordinary document row, and the client-side facet filter beside
  // the existing tag ToggleGroup - there is no `?type=` on the plain
  // folder-browse/list endpoints, only on /api/notes, so this filters
  // whatever useDocuments already returned rather than issuing a second
  // fetch.
  describe('note type: the pre-attentive icon column', () => {
    it("a kind='note' row shows its note_type icon, not the generic file icon", () => {
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({
        documents: [doc({ id: 'note-1', filename: 'Ring Dave.md', title: 'Ring Dave', mime: 'text/markdown', kind: 'note', note_type: 'contact' })],
      }))

      render(<DocumentsPanel />)

      expect(screen.getByRole('button', { name: /change type: currently contact/i })).toBeInTheDocument()
    })

    // One trigger, not six toggle buttons. Six icon+label buttons sat in
    // the same row as New and Upload and read as actions rather than as a
    // filter; this pins the collapse so it cannot quietly regress.
  })

  // "Unfiled notes" as a saved view (kind='note' AND folder_id IS NULL,
  // GET /api/notes?filed=0) - a Documents filter, not a place: this proves
  // it lists only what useNotes({unfiledOnly:true}) reports and shows a
  // visible count, preserving the deleted notes-panel.tsx's drain-the-inbox
  // loop without owning a sidebar row.
  describe('Unfiled notes saved view', () => {
    it('shows the unfiled count and, once active, lists only those notes', () => {
      mockedUseNotes.mockReturnValue(makeNotesMock({
        notes: [note({ id: 'note-1', title: 'Ring Dave' }), note({ id: 'note-2', title: 'Ring the yard' })],
      }))
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({
        documents: [doc({ id: 'filed-1', filename: 'filed.pdf', title: 'Filed doc' })],
      }))

      render(<DocumentsPanel />)

      const toggle = screen.getByRole('button', { name: /unfiled notes/i })
      expect(toggle).toHaveTextContent('2')
      expect(screen.getByText('Filed doc')).toBeInTheDocument()

      fireEvent.click(toggle)

      expect(screen.getByText('Ring Dave')).toBeInTheDocument()
      expect(screen.getByText('Ring the yard')).toBeInTheDocument()
      expect(screen.queryByText('Filed doc')).not.toBeInTheDocument()
    })

    it('File… on an unfiled row opens the same Move dialog a document row uses', () => {
      mockedUseNotes.mockReturnValue(makeNotesMock({
        notes: [note({ id: 'note-1', title: 'Ring Dave' })],
      }))

      render(<DocumentsPanel />)
      fireEvent.click(screen.getByRole('button', { name: /unfiled notes/i }))
      fireEvent.click(screen.getByRole('button', { name: 'File "Ring Dave"' }))

      expect(screen.getByRole('dialog')).toHaveTextContent('Move')
    })
  })

  // A Manual-kind folder (document_folders.role='manual') renders ordering
  // and Arrange in place of the ordinary table; a plain folder stays the
  // table. Promotion/demotion (POST/DELETE /api/manuals) are reachable from
  // the folder row menu and from inside the manual view itself.
  describe('Manual-kind folders', () => {
    it('a manual folder shows ordering and Arrange; a plain folder shows the ordinary table instead', () => {
      mockedUseManuals.mockReturnValue(makeManualsMock({
        manuals: [{ id: 'm1', name: 'Operations Manual', document_count: 1, updated_at: '2026-01-01T00:00:00Z' }],
        tree: { type: 'folder', id: 'm1', name: 'Operations Manual', sort_index: 0, updated_at: '2026-01-01T00:00:00Z', children: [docNode()] },
      }))

      const { rerender } = render(<DocumentsPanel initialFolderId="m1" />)
      expect(screen.getByRole('button', { name: 'Arrange' })).toBeInTheDocument()
      expect(screen.getByText('Genset start-up')).toBeInTheDocument()

      mockedUseDocuments.mockReturnValue(makeDocumentsMock({
        documents: [doc({ id: 'doc-1', filename: 'receipt.pdf' })],
      }))
      rerender(<DocumentsPanel initialFolderId="f-plain" />)
      expect(screen.queryByRole('button', { name: 'Arrange' })).not.toBeInTheDocument()
      expect(screen.getByRole('table')).toBeInTheDocument()
    })

    it('promoting a top-level folder calls flagManual', async () => {
      const flagManual = vi.fn().mockResolvedValue({ id: 'f1', name: 'Contacts', document_count: 0, updated_at: '2026-01-01T00:00:00Z' })
      mockedUseManuals.mockReturnValue(makeManualsMock({ flagManual }))
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({
        folders: [{ id: 'f1', name: 'Contacts', parent_id: null }],
      }))

      render(<DocumentsPanel />)
      fireEvent.click(screen.getByRole('button', { name: /actions for contacts/i }))
      fireEvent.click(screen.getByRole('menuitem', { name: /treat as a manual/i }))

      await waitFor(() => expect(flagManual).toHaveBeenCalledWith('f1'))
    })

    it('demoting from inside the manual view calls clearManual', async () => {
      const clearManual = vi.fn().mockResolvedValue(undefined)
      mockedUseManuals.mockReturnValue(makeManualsMock({
        manuals: [{ id: 'm1', name: 'Operations Manual', document_count: 0, updated_at: '2026-01-01T00:00:00Z' }],
        tree: { type: 'folder', id: 'm1', name: 'Operations Manual', sort_index: 0, updated_at: '2026-01-01T00:00:00Z', children: [] },
        clearManual,
      }))

      render(<DocumentsPanel initialFolderId="m1" />)
      fireEvent.click(screen.getByRole('button', { name: 'Stop treating as manual' }))

      await waitFor(() => expect(clearManual).toHaveBeenCalledWith('m1'))
    })

    it('demoting from the folder row menu (browsing the root) also calls clearManual', async () => {
      const clearManual = vi.fn().mockResolvedValue(undefined)
      mockedUseManuals.mockReturnValue(makeManualsMock({
        manuals: [{ id: 'm1', name: 'Operations Manual', document_count: 0, updated_at: '2026-01-01T00:00:00Z' }],
        clearManual,
      }))
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({
        folders: [{ id: 'm1', name: 'Operations Manual', parent_id: null }],
      }))

      render(<DocumentsPanel />)
      fireEvent.click(screen.getByRole('button', { name: /actions for operations manual/i }))
      fireEvent.click(screen.getByRole('menuitem', { name: /stop treating as manual/i }))

      await waitFor(() => expect(clearManual).toHaveBeenCalledWith('m1'))
    })

    it('the New folder dialog offers a Manual checkbox at the root, and flags the created folder', async () => {
      const createFolder = vi.fn().mockResolvedValue({ id: 'new-1', name: 'Crew Training', parent_id: null })
      const flagManual = vi.fn().mockResolvedValue({ id: 'new-1', name: 'Crew Training', document_count: 0, updated_at: '2026-01-01T00:00:00Z' })
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({ createFolder }))
      mockedUseManuals.mockReturnValue(makeManualsMock({ flagManual }))

      render(<DocumentsPanel />)
      fireEvent.click(screen.getByRole('button', { name: 'New' }))
      fireEvent.click(screen.getByRole('menuitem', { name: 'Folder' }))

      fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Crew Training' } })
      fireEvent.click(screen.getByRole('checkbox', { name: /manual/i }))
      fireEvent.click(screen.getByRole('button', { name: 'Create' }))

      await waitFor(() => expect(createFolder).toHaveBeenCalledWith('Crew Training', null))
      await waitFor(() => expect(flagManual).toHaveBeenCalledWith('new-1'))
    })

    it('does not offer the Manual checkbox when creating a folder inside another folder', () => {
      render(<DocumentsPanel initialFolderId="parent-1" />)
      fireEvent.click(screen.getByRole('button', { name: 'New' }))
      fireEvent.click(screen.getByRole('menuitem', { name: 'Folder' }))

      expect(screen.queryByRole('checkbox', { name: /manual/i })).not.toBeInTheDocument()
    })
  })

  // "Opening a note in Documents' viewer must reach the editor" - the Edit
  // toggle is the only way NoteEditor is reachable from the general viewer,
  // and Save must go through use-notes.ts's patchNote, not a raw fetch.
  describe('the viewer: editing a note', () => {
    it('a note opens in the editor and saves through patchNote', async () => {
      const patchNote = vi.fn().mockResolvedValue({ document: note(), body: 'Open the seacock first. edited' })
      mockedUseNotes.mockReturnValue(makeNotesMock({ patchNote }))
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({ text: 'Open the seacock first.' }),
      }))
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({
        documents: [doc({ id: 'note-1', mime: 'text/markdown', filename: 'genset.md', title: 'Genset start-up', kind: 'note' })],
      }))

      render(<DocumentsPanel />)
      fireEvent.click(screen.getByRole('button', { name: 'Genset start-up' }))
      await screen.findByText(/Open the/)

      fireEvent.click(screen.getByRole('button', { name: 'Edit' }))
      expect(screen.getByText('editor: Open the seacock first.')).toBeInTheDocument()

      fireEvent.click(screen.getByRole('button', { name: 'Save' }))

      await waitFor(() => expect(patchNote).toHaveBeenCalledWith('note-1', { body: 'Open the seacock first. edited' }))
    })

    // GET /api/documents/:id/text reassembles the INDEXED CHUNKS, and
    // splitMarkdownSections lifts each heading into its own column and
    // drops it from the chunk's text. So a note read through /text comes
    // back with every "#" line gone - fine for search, catastrophic as the
    // seed for an editor whose Save replaces the note's whole body.
    //
    // A note's real bytes come from GET /api/notes/:id (readNoteBody, off
    // disk). This asserts the viewer uses that, by making the two sources
    // disagree and requiring the note's own heading to win.
    it('reads a note from its own bytes, not from the reassembled chunks', async () => {
      const realBody = '# Genset start-up\n\nOpen the seacock first.\n'
      const getNote = vi.fn().mockResolvedValue({
        document: note({ id: 'note-1', title: 'Genset start-up' }),
        body: realBody,
      })
      mockedUseNotes.mockReturnValue(makeNotesMock({ getNote }))
      // What the chunk endpoint would return: heading stripped.
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({ text: 'Open the seacock first.' }),
      }))
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({
        documents: [doc({ id: 'note-1', mime: 'text/markdown', filename: 'genset.md', title: 'Genset start-up', kind: 'note' })],
      }))

      render(<DocumentsPanel />)
      fireEvent.click(screen.getByRole('button', { name: 'Genset start-up' }))

      await waitFor(() => expect(getNote).toHaveBeenCalledWith('note-1'))

      fireEvent.click(await screen.findByRole('button', { name: 'Edit' }))
      expect(screen.getByText(/editor:.*# Genset start-up/)).toBeInTheDocument()
    })

    it('a plain file (kind file) never shows an Edit toggle', async () => {
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({ text: 'raw log output' }),
      }))
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({
        documents: [doc({ id: 'log-1', mime: 'text/plain', filename: 'engine.log', title: 'Engine log', kind: 'file' })],
      }))

      render(<DocumentsPanel />)
      fireEvent.click(screen.getByRole('button', { name: 'Engine log' }))
      await screen.findByText('raw log output')

      expect(screen.queryByRole('button', { name: 'Edit' })).not.toBeInTheDocument()
    })
  })

  // Plan §8: pinning a note is the only operator-facing way documents.pinned
  // (already a real column, already on the wire) ever becomes true - without
  // it, Mate's pinned-notes prompt feature has no way to ever hold anything.
  // Lives in the viewer, the one place "where a note is read".
  describe('the viewer: pinning a note for Mate', () => {
    it('an unpinned note offers "Pin for Mate", which PATCHes pinned:true', async () => {
      const patchNote = vi.fn().mockResolvedValue({ document: note({ pinned: true }), body: 'Open the seacock first.' })
      mockedUseNotes.mockReturnValue(makeNotesMock({ patchNote }))
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({ text: 'Open the seacock first.' }),
      }))
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({
        documents: [doc({ id: 'note-1', mime: 'text/markdown', filename: 'genset.md', title: 'Genset start-up', kind: 'note', pinned: false })],
      }))

      render(<DocumentsPanel />)
      fireEvent.click(screen.getByRole('button', { name: 'Genset start-up' }))
      await screen.findByText(/Open the/)

      fireEvent.click(screen.getByRole('button', { name: 'Pin for Mate' }))

      await waitFor(() => expect(patchNote).toHaveBeenCalledWith('note-1', { pinned: true }))
      await screen.findByRole('button', { name: 'Unpin' })
    })

    it('a pinned note offers "Unpin", which PATCHes pinned:false', async () => {
      const patchNote = vi.fn().mockResolvedValue({ document: note({ pinned: false }), body: 'Open the seacock first.' })
      mockedUseNotes.mockReturnValue(makeNotesMock({ patchNote }))
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({ text: 'Open the seacock first.' }),
      }))
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({
        documents: [doc({ id: 'note-1', mime: 'text/markdown', filename: 'genset.md', title: 'Genset start-up', kind: 'note', pinned: true })],
      }))

      render(<DocumentsPanel />)
      fireEvent.click(screen.getByRole('button', { name: 'Genset start-up' }))
      await screen.findByText(/Open the/)

      fireEvent.click(screen.getByRole('button', { name: 'Unpin' }))

      await waitFor(() => expect(patchNote).toHaveBeenCalledWith('note-1', { pinned: false }))
      await screen.findByRole('button', { name: 'Pin for Mate' })
    })

    it('a plain file (kind file) never shows a pin control', async () => {
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({ text: 'raw log output' }),
      }))
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({
        documents: [doc({ id: 'log-1', mime: 'text/plain', filename: 'engine.log', title: 'Engine log', kind: 'file' })],
      }))

      render(<DocumentsPanel />)
      fireEvent.click(screen.getByRole('button', { name: 'Engine log' }))
      await screen.findByText('raw log output')

      expect(screen.queryByRole('button', { name: 'Pin for Mate' })).not.toBeInTheDocument()
      expect(screen.queryByRole('button', { name: 'Unpin' })).not.toBeInTheDocument()
    })
  })

  // Plan §7 / ADR 0118: "Start checklist" is the general viewer's own
  // route into the checklist runner - a mode of this same Sheet, gated on
  // GET /api/notes/:id's own `checklist` field (use-notes.ts's getNote).
  describe('the viewer: running a checklist', () => {
    it('shows Start checklist for a note with checklist items, and Back returns to reading', async () => {
      const getNote = vi.fn().mockResolvedValue({
        document: note(),
        body: '- [ ] Seacocks open',
        checklist: [{ item_key: 'k1', occurrence: 0, text: 'Seacocks open', depth: 0 }],
      })
      mockedUseNotes.mockReturnValue(makeNotesMock({ getNote }))
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({ text: '- [ ] Seacocks open' }),
      }))
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({
        documents: [doc({ id: 'note-1', mime: 'text/markdown', filename: 'genset.md', title: 'Genset start-up', kind: 'note' })],
      }))

      render(<DocumentsPanel />)
      fireEvent.click(screen.getByRole('button', { name: 'Genset start-up' }))
      await waitFor(() => expect(getNote).toHaveBeenCalledWith('note-1'))

      const startButton = await screen.findByRole('button', { name: /Start checklist/ })
      fireEvent.click(startButton)

      expect(screen.getByText('checklist runner: note-1 (Genset start-up)')).toBeInTheDocument()

      fireEvent.click(screen.getByRole('button', { name: 'Back' }))
      expect(screen.queryByText(/checklist runner:/)).not.toBeInTheDocument()
    })

    it('does not show Start checklist for a note with no checklist items', async () => {
      const getNote = vi.fn().mockResolvedValue({ document: note(), body: 'Ring Dave about the mooring.', checklist: [] })
      mockedUseNotes.mockReturnValue(makeNotesMock({ getNote }))
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({ text: 'Ring Dave about the mooring.' }),
      }))
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({
        documents: [doc({ id: 'note-1', mime: 'text/markdown', filename: 'ring-dave.md', title: 'Ring Dave', kind: 'note' })],
      }))

      render(<DocumentsPanel />)
      fireEvent.click(screen.getByRole('button', { name: 'Ring Dave' }))
      await waitFor(() => expect(getNote).toHaveBeenCalledWith('note-1'))

      expect(screen.queryByRole('button', { name: /Start checklist/ })).not.toBeInTheDocument()
    })

    it('a plain file (kind file) never shows Start checklist', async () => {
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({ text: 'raw log output' }),
      }))
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({
        documents: [doc({ id: 'log-1', mime: 'text/plain', filename: 'engine.log', title: 'Engine log', kind: 'file' })],
      }))

      render(<DocumentsPanel />)
      fireEvent.click(screen.getByRole('button', { name: 'Engine log' }))
      await screen.findByText('raw log output')

      expect(screen.queryByRole('button', { name: /Start checklist/ })).not.toBeInTheDocument()
    })
  })
})
