import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, within, act, waitFor } from '@testing-library/react'
import { forwardRef, useImperativeHandle } from 'react'

import { DocumentsPanel } from '@/components/documents-panel'
import { setViewportWidth } from '@/test/viewport'
import { useAssistantStatus } from '@/hooks/use-assistant-status'
import { useDocuments } from '@/hooks/use-documents'
import { useDocumentFolderTree, type DocumentFolderTreeNode } from '@/hooks/use-document-folder-tree'
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
vi.mock('@/hooks/use-document-folder-tree')
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
  // ADR 0124: note-capture-sheet.tsx now embeds the body-only editor
  // directly (a ref exposing getMarkdown/insertDictation/focus). This
  // file's own "capture: New → Note" tests only need the sheet to OPEN on
  // the right heading and type - never to type or dictate into the body -
  // so the fake implements just enough of the handle contract for
  // note-capture-sheet.tsx not to throw calling it (submit()'s own
  // .trim() included), without pulling in the real Plate editor. Its own
  // round-trip/insertion coverage lives in note-editor.test.tsx and
  // note-editor-dictation.test.ts.
  NoteEditorBody: forwardRef<
    { getMarkdown: () => string; insertDictation: () => void; focus: () => void },
    { value: string }
  >(function NoteEditorBody({ value }, ref) {
    useImperativeHandle(ref, () => ({
      getMarkdown: () => value,
      insertDictation: () => {},
      focus: () => {},
    }), [value])
    return <p>editor body: {value}</p>
  }),
  prefetchNoteEditor: () => {},
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
const mockedUseFolderTree = vi.mocked(useDocumentFolderTree)
const refreshFolderTree = vi.fn()

/** The folder pane's whole-library tree (ADR 0170), as the hook reports it. */
function setFolderTree(folders: DocumentFolderTreeNode[]) {
  mockedUseFolderTree.mockReturnValue({ folders, loading: false, error: null, refresh: refreshFolderTree })
}
function treeFolder(overrides: Partial<DocumentFolderTreeNode> & { id: string; name: string }): DocumentFolderTreeNode {
  const base = { parent_id: null, is_manual: false, document_count: 0, ...overrides }
  return { subtree_document_count: base.document_count, ...base }
}
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
    removeByDocumentIds: vi.fn(),
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

/** Search now lives behind a trigger + overlay (shadcn.io
 * "navbar-search-overlay"), not an always-on input in the filter row - every
 * test that drives a search has to open it first. */
function openSearchOverlay() {
  // The filter row's search field is the overlay's trigger (IndexFilters'
  // onSearchActivate), not a second search.
  fireEvent.click(screen.getByRole('textbox', { name: 'Search' }))
}

beforeEach(() => {
  vi.clearAllMocks()
  // Recent searches (below) persist in real localStorage across tests in
  // this file otherwise - a query run by an earlier test would still be
  // offered as a "recent" in a later, unrelated one.
  try { localStorage.clear() } catch { /* not available in this env */ }
  mockedUseDocuments.mockReturnValue(makeDocumentsMock())
  mockedUseDocumentUploads.mockReturnValue(makeUploadsMock())
  refreshFolderTree.mockReset()
  setFolderTree([])
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
  // Search lives behind a trigger + full-page overlay now (shadcn.io
  // "navbar-search-overlay"). The overlay's results list sits in a
  // ScrollArea (@base-ui/react/scroll-area), which - like FolderPicker's own
  // ScrollArea below - schedules an unguarded 0ms timeout on mount that
  // unconditionally calls viewport.getAnimations() to recompute thumb
  // geometry. src/test/setup.ts deletes Element.prototype.getAnimations
  // globally so Base UI's Dialog/AlertDialog/Tabs close synchronously for
  // this suite's fireEvent-driven assertions elsewhere, but ScrollArea's own
  // mount effect has no such fallback: a local stub, scoped to just this
  // describe block and removed again afterwards, satisfies it without
  // weakening the global shim the rest of the suite depends on.
  describe('search overlay', () => {
    beforeEach(() => {
      Element.prototype.getAnimations = () => []
    })
    afterEach(() => {
      delete (Element.prototype as { getAnimations?: unknown }).getAnimations
    })

  it('sends the typed query, debounced, scoped to the current folder', () => {
    vi.useFakeTimers()
    const search = vi.fn()
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({ search }))

    render(<DocumentsPanel initialFolderId="f1" />)
    openSearchOverlay()

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
    openSearchOverlay()

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

    render(<DocumentsPanel />)
    openSearchOverlay()

    // document.body, not the render container: the overlay is a Dialog,
    // which portals its content to the end of <body> rather than rendering
    // it inline where DocumentsPanel itself mounted.
    const mark = document.body.querySelector('mark')
    expect(mark).not.toBeNull()
    expect(mark?.textContent).toBe('<img src=x onerror=alert(1)>')
    // Never actually parsed as markup - no <img> was created from the snippet.
    expect(document.body.querySelector('img')).toBeNull()
  })

  it('shows each search hit\'s page and folder path', async () => {
    // Search results only ever carry folder_id (backend/documents_search.go's
    // documentSearchResult), never a name or path, so the panel resolves it
    // itself via GET /api/document-folders?parent=<id> (the same endpoint
    // folder browsing already uses) - one real fetch, not routed through the
    // mocked useDocuments hook.
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
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
    openSearchOverlay()

    expect(screen.getByText(/page 4/i)).toBeInTheDocument()
    expect(await screen.findByText(/manuals$/i)).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith('/api/document-folders?parent=f1', expect.anything())
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
    openSearchOverlay()

    // Scoped to the results list - "Documents" is also the breadcrumb's own
    // root crumb, always on screen regardless of search state. Substring
    // match: the folder label shares one line with "Page N · ".
    const resultsList = screen.getByTestId('documents-search-results')
    expect(within(resultsList).getByText(/documents$/i)).toBeInTheDocument()
  })

  it('the Search trigger opens the overlay with an autofocused input', () => {
    render(<DocumentsPanel />)

    expect(screen.queryByRole('searchbox', { name: /search documents/i })).not.toBeInTheDocument()

    openSearchOverlay()

    const input = screen.getByRole('searchbox', { name: /search documents/i })
    expect(input).toBeInTheDocument()
    expect(input).toHaveFocus()
  })

  it('⌘K opens the overlay from anywhere in the panel', () => {
    render(<DocumentsPanel />)

    fireEvent.keyDown(window, { key: 'k', metaKey: true })

    expect(screen.getByRole('searchbox', { name: /search documents/i })).toBeInTheDocument()
  })

  it('Ctrl+K opens the overlay too, for non-Mac keyboards', () => {
    render(<DocumentsPanel />)

    fireEvent.keyDown(window, { key: 'k', ctrlKey: true })

    expect(screen.getByRole('searchbox', { name: /search documents/i })).toBeInTheDocument()
  })

  it('Enter on the active result opens the viewer and closes the overlay', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => doc({ id: 'doc-9', filename: 'manual.pdf', title: 'Impeller manual' }),
    }))
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({
      searchResults: [{
        document_id: 'doc-9',
        filename: 'manual.pdf',
        title: 'Impeller manual',
        status: 'indexed',
        page: 2,
        snippet: 'the \x02impeller\x03 kit',
        folder_id: null,
      }],
    }))

    render(<DocumentsPanel />)
    openSearchOverlay()

    await act(async () => {
      fireEvent.keyDown(screen.getByRole('searchbox', { name: /search documents/i }), { key: 'Enter' })
    })

    // The overlay itself is gone...
    expect(screen.queryByRole('searchbox', { name: /search documents/i })).not.toBeInTheDocument()
    // ...and the viewer opened on that result's document (fetched directly,
    // since it isn't in whatever folder useDocuments happens to report -
    // the same path a Mate attachment chip's link already exercises).
    expect(await screen.findByText('Impeller manual')).toBeInTheDocument()
  })

  it('clicking a result does the same as Enter: opens the viewer and closes the overlay', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => doc({ id: 'doc-9', filename: 'manual.pdf', title: 'Impeller manual' }),
    }))
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({
      searchResults: [{
        document_id: 'doc-9',
        filename: 'manual.pdf',
        title: 'Impeller manual',
        status: 'indexed',
        page: 2,
        snippet: 'the \x02impeller\x03 kit',
        folder_id: null,
      }],
    }))

    render(<DocumentsPanel />)
    openSearchOverlay()

    await act(async () => {
      fireEvent.click(within(screen.getByTestId('documents-search-results')).getByText(/impeller manual/i))
    })

    expect(screen.queryByRole('searchbox', { name: /search documents/i })).not.toBeInTheDocument()
    expect(await screen.findByText('Impeller manual')).toBeInTheDocument()
  })

  it('⌘K does nothing while another dialog (the viewer, the note editor, Mate) is open', () => {
    render(<><DocumentsPanel /><div role="dialog" aria-label="Note editor">editing</div></>)
    fireEvent.keyDown(window, { key: 'k', metaKey: true })
    expect(screen.queryByRole('searchbox', { name: /search documents/i })).not.toBeInTheDocument()
  })

  it('Enter does not open a result belonging to an earlier query while the newest one is still pending', () => {
    vi.useFakeTimers()
    const search = vi.fn()
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({
      search,
      searchResults: [{
        document_id: 'doc-9',
        filename: 'imp.pdf',
        title: 'Imp result',
        status: 'indexed',
        page: 1,
        snippet: 'imp',
        folder_id: null,
      }],
    }))
    render(<DocumentsPanel />)
    openSearchOverlay()
    const input = screen.getByRole('searchbox', { name: /search documents/i })
    fireEvent.change(input, { target: { value: 'imp' } })
    act(() => { vi.advanceTimersByTime(250) })
    fireEvent.change(input, { target: { value: 'impeller' } })
    fireEvent.keyDown(input, { key: 'Enter' })
    // Still open: the "imp" result was not opened in place of "impeller"'s.
    expect(screen.getByRole('searchbox', { name: /search documents/i })).toBeInTheDocument()
  })

  it('shows Searching…, not "No matches.", before the typed query has been searched', () => {
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({ searchResults: [], searching: false }))
    render(<DocumentsPanel />)
    openSearchOverlay()
    fireEvent.change(screen.getByRole('searchbox', { name: /search documents/i }), { target: { value: 'x' } })
    expect(screen.queryByText('No matches.')).not.toBeInTheDocument()
    expect(screen.getByText('Searching…')).toBeInTheDocument()
  })

  it('shows the active tag filter inside the overlay, with a way to clear it', () => {
    const setSelectedTag = vi.fn()
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({ selectedTag: 'engine', setSelectedTag, tags: [{ tag: 'engine', count: 3 }] }))
    render(<DocumentsPanel />)
    openSearchOverlay()
    const dialog = screen.getByRole('dialog')
    fireEvent.click(within(dialog).getByRole('button', { name: /clear tag filter engine/i }))
    expect(setSelectedTag).toHaveBeenCalledWith(null)
  })

  it('shows Searching… rather than the empty-state suggestions while a typed query has no results yet', () => {
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({
      searching: true,
      searchResults: null,
      tags: [{ tag: 'Alternator', count: 2 }],
    }))
    render(<DocumentsPanel />)
    openSearchOverlay()
    fireEvent.change(screen.getByRole('searchbox', { name: /search documents/i }), { target: { value: 'alternator' } })
    expect(screen.getByText('Searching…')).toBeInTheDocument()
    expect(within(screen.getByRole('dialog')).queryByText('Tags')).not.toBeInTheDocument()
  })

  it("offers only the twelve most-used tags as the overlay's suggestions", () => {
    const tags = Array.from({ length: 20 }, (_, i) => ({ tag: `Tag${i}`, count: i + 1 }))
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({ tags }))
    render(<DocumentsPanel />)
    openSearchOverlay()
    const dialog = screen.getByRole('dialog')
    expect(within(dialog).getByRole('button', { name: 'Tag19 (20)' })).toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: 'Tag8 (9)' })).toBeInTheDocument()
    expect(within(dialog).queryByRole('button', { name: 'Tag7 (8)' })).not.toBeInTheDocument()
  })

  it('records the search in force when the overlay closes as a recent, not each pause mid-typing; offers it on reopen to re-run, and Clear removes it', () => {
    vi.useFakeTimers()
    const search = vi.fn()
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({ search }))

    const { unmount } = render(<DocumentsPanel />)
    openSearchOverlay()
    const input = screen.getByRole('searchbox', { name: /search documents/i })
    // A pause mid-word lets the debounce settle on a partial query - that
    // partial must not become a recent search.
    fireEvent.change(input, { target: { value: 'impel' } })
    act(() => { vi.advanceTimersByTime(250) })
    fireEvent.change(input, { target: { value: 'impeller' } })
    act(() => { vi.advanceTimersByTime(250) })
    expect(search).toHaveBeenCalledWith('impeller', { allFolders: false })
    fireEvent.keyDown(input, { key: 'Escape' })

    // The recent is persisted (localStorage), not merely in-memory - proven
    // by unmounting the panel entirely (leaving, the way navigating to a
    // different sidebar panel would) and mounting a fresh instance, rather
    // than reusing this one's own React state.
    unmount()
    render(<DocumentsPanel />)
    openSearchOverlay()
    const recent = screen.getByRole('button', { name: 'impeller' })
    expect(recent).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'impel' })).not.toBeInTheDocument()

    // Clicking it re-runs the same query.
    search.mockClear()
    fireEvent.click(recent)
    act(() => { vi.advanceTimersByTime(250) })
    expect(search).toHaveBeenCalledWith('impeller', { allFolders: false })

    // Clear removes it - a later reopen offers nothing. The re-run query
    // holds the overlay in results mode, so empty the box first.
    fireEvent.change(screen.getByRole('searchbox', { name: /search documents/i }), { target: { value: '' } })
    fireEvent.click(screen.getByRole('button', { name: 'Clear' }))
    expect(screen.queryByRole('button', { name: 'impeller' })).not.toBeInTheDocument()
  })
  })

  it('navigating into a folder calls onFolderChange with its id', () => {
    const onFolderChange = vi.fn()
    setFolderTree([treeFolder({ id: 'f1', name: 'Manuals' })])

    render(<DocumentsPanel onFolderChange={onFolderChange} />)

    fireEvent.click(screen.getByRole('treeitem', { name: /Manuals/ }))

    expect(onFolderChange).toHaveBeenCalledWith('f1')
  })

  it('browser Back and Forward move the panel between folders', () => {
    // App.tsx keeps the Suspense key on 'documents' while browsing (it only
    // remounts the panel on a panel switch), so Back/Forward have to reach
    // the panel as a changed `initialFolderId` prop on an already-mounted
    // instance - not a fresh mount - the same way AssistantDrawer picks up
    // a changed `initialConversationId`. `rerender` with a new prop is
    // exactly that.
    setFolderTree([treeFolder({ id: 'f1', name: 'Manuals' })])
    mockedUseDocuments.mockImplementation((folderId) =>
      makeDocumentsMock(folderId === 'f1' ? { path: [{ id: 'f1', name: 'Manuals', parent_id: null }] } : {}),
    )
    const onFolderChange = vi.fn()

    const { rerender } = render(<DocumentsPanel initialFolderId={null} onFolderChange={onFolderChange} />)

    fireEvent.click(screen.getByRole('treeitem', { name: /Manuals/ }))
    expect(onFolderChange).toHaveBeenCalledWith('f1')
    expect(mockedUseDocuments).toHaveBeenLastCalledWith('f1')
    expect(screen.getByRole('heading', { name: 'Manuals' })).toBeInTheDocument()

    // App.tsx's onFolderChange handler mirrors the new folder into its own
    // documentsFolderId state (and the URL) synchronously, so the panel's
    // very next render already carries it back down as a prop, not just as
    // the panel's own internal state.
    rerender(<DocumentsPanel initialFolderId="f1" onFolderChange={onFolderChange} />)

    // Back: App.tsx's popstate handler re-seeds documentsFolderId from the
    // URL and hands it back down as a prop, without remounting the panel.
    rerender(<DocumentsPanel initialFolderId={null} onFolderChange={onFolderChange} />)
    expect(mockedUseDocuments).toHaveBeenLastCalledWith(null)
    expect(screen.getByRole('heading', { name: 'Documents' })).toBeInTheDocument()
    expect(screen.getByRole('treeitem', { name: 'Documents' })).toHaveAttribute('aria-selected', 'true')

    // Forward: back to f1.
    rerender(<DocumentsPanel initialFolderId="f1" onFolderChange={onFolderChange} />)
    expect(mockedUseDocuments).toHaveBeenLastCalledWith('f1')
    expect(screen.getByRole('heading', { name: 'Manuals' })).toBeInTheDocument()
    expect(screen.getByRole('treeitem', { name: /Manuals/ })).toHaveAttribute('aria-selected', 'true')
  })

  it('a Back/Forward re-sync of folder and section does not report a section change', () => {
    // Returning to {folder A, section S} hands the panel both props at once.
    // Reporting the folder-driven section clear back to the router would push
    // a new entry and cut the forward history.
    setFolderTree([treeFolder({ id: 'f1', name: 'Manuals' })])
    const onSectionChange = vi.fn()
    const { rerender } = render(
      <DocumentsPanel initialFolderId={null} initialSectionId={null} onSectionChange={onSectionChange} />,
    )
    rerender(<DocumentsPanel initialFolderId="f1" initialSectionId="s1" onSectionChange={onSectionChange} />)
    expect(onSectionChange).not.toHaveBeenCalled()
  })

  it('titles the page with the current folder and has no breadcrumb', () => {
    mockedUseDocuments.mockImplementation((folderId) =>
      makeDocumentsMock(
        folderId === 'f1' ? { path: [{ id: 'f1', name: 'Manuals', parent_id: null }] } : {},
      ),
    )

    render(<DocumentsPanel initialFolderId="f1" />)

    expect(screen.queryByRole('navigation', { name: /breadcrumb/i })).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Manuals' })).toBeInTheDocument()
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

  it('shows a staged upload\'s progress, then clears the chip once it lands', () => {
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
  it('lists documents only: folders are in the pane, with no Folders or Documents group rows', () => {
    setFolderTree([treeFolder({ id: 'f1', name: 'Manuals' })])
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({ documents: [doc({ filename: 'receipt.pdf' })] }))

    render(<DocumentsPanel />)

    const table = screen.getByRole('table')
    expect(within(table).queryByText('Manuals')).not.toBeInTheDocument()
    expect(within(table).queryByText('Folders')).not.toBeInTheDocument()
    expect(within(table).getByText('receipt.pdf')).toBeInTheDocument()
    const dataRow = within(table).getByText('receipt.pdf').closest('tr')!
    expect(within(dataRow).getAllByRole('cell').length).toBeGreaterThanOrEqual(screen.getAllByRole('columnheader').length)
  })

  it('shows an operator-facing message for a failed document, keeping the raw error as a title attribute', () => {
    // A raw stored error (an internal on-disk storage path, "panic: ...",
    // a library's own error text) has no next step for the operator - the
    // row shows documentFailureMessage's plain-English sentence instead,
    // with the raw error still reachable as a title attribute rather than
    // hidden outright (the full text also lives on the Details page).
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({
      documents: [doc({
        status: 'failed',
        stage: 'extract',
        mime: 'application/pdf',
        error: 'pdf page 1: panic: pred',
      })],
    }))

    render(<DocumentsPanel />)

    const message = screen.getByText(/Couldn't read the text in this PDF/)
    expect(message).toBeInTheDocument()
    expect(message).toHaveAttribute('title', 'pdf page 1: panic: pred')
    expect(screen.queryByText('pdf page 1: panic: pred')).not.toBeInTheDocument()
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

    // The Sheet's own container has to lay out as a flex column, not just
    // the scroll box inside it. SheetContent's base classes (sheetVariants,
    // ui/sheet.tsx) are `fixed ... p-6` - a block box, not `flex` - so the
    // viewer body's `min-h-0 flex-1 overflow-auto` div has no flex parent to
    // size against: `flex-1`/`min-h-0` are inert outside a flex container,
    // the div's height resolves to its content instead of the sheet's
    // height, and a long note spills past the fixed-height sheet with no
    // scrollbar anywhere. This checks the same unbroken flex/min-h-0 chain
    // mate-sheet.test.tsx and help-sheet.tsx already require of their own
    // SheetContent (`flex h-full ... flex-col`), which the documents viewer
    // never had.
    it('the viewer sheet is a flex column so its scroll body can actually bound its height', async () => {
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({ text: 'Open the **seacock** first.' }),
      }))
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({
        documents: [doc({ id: 'note-1', mime: 'text/markdown', filename: 'genset.md', title: 'Genset start-up' })],
      }))

      render(<DocumentsPanel />)
      fireEvent.click(screen.getByRole('button', { name: 'Genset start-up' }))
      await screen.findByText(/Open the/)

      const dialog = await screen.findByRole('dialog')
      expect(dialog.className).toEqual(expect.stringContaining('flex'))
      expect(dialog.className).toEqual(expect.stringContaining('flex-col'))
      expect(dialog.className).toEqual(expect.stringContaining('h-full'))

      const scrollBox = dialog.querySelector('.overflow-auto')
      expect(scrollBox).not.toBeNull()
      expect(scrollBox!.className).toEqual(expect.stringContaining('min-h-0'))
      expect(scrollBox!.className).toEqual(expect.stringContaining('flex-1'))
      expect(scrollBox!.className).toEqual(expect.stringContaining('overflow-auto'))
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
    setFolderTree([treeFolder({ id: 'f1', name: 'Manuals' })])
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({ deleteFolder }))

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
    // These now open the search overlay too - see the 'search overlay'
    // describe block's own comment on this same ScrollArea/getAnimations
    // stub.
    beforeEach(() => {
      Element.prototype.getAnimations = () => []
    })
    afterEach(() => {
      delete (Element.prototype as { getAnimations?: unknown }).getAnimations
    })

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
      openSearchOverlay()

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
      openSearchOverlay()

      expect(screen.queryByText(/showing keyword results only/i)).not.toBeInTheDocument()
    })

    it('a second, clean search clears a previously shown notice', () => {
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({
        searchResults: oneResult,
        semanticProblem: 'no embedding model reachable',
      }))

      const { rerender } = render(<DocumentsPanel />)
      openSearchOverlay()
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
  describe('capture: More actions → New note', () => {
    // One New menu, Folder first, Note carrying its kinds in a submenu.
    // Auto leads that submenu and must pass NO kind, so the backend's own
    // classifier answers rather than the menu guessing on the operator's
    // behalf (ADR 0116).
    it('New note → Auto opens the capture sheet on Auto, with no onCaptureNote prop anywhere', async () => {
      render(<DocumentsPanel />)

      fireEvent.click(screen.getByRole('button', { name: 'More actions' }))
      fireEvent.click(await screen.findByRole('menuitem', { name: 'New note' }))
      fireEvent.click(await screen.findByRole('menuitem', { name: 'Auto' }))

      // A longer timeout than findByRole's default 1s: this is a
      // lazy-loaded chunk (note-capture-sheet.tsx) resolving via dynamic
      // import(), the same flakiness app-sidebar-navigation.test.tsx's
      // deleted equivalent test used to guard against.
      expect(await screen.findByRole('heading', { name: 'Capture a note' }, { timeout: 5000 })).toBeInTheDocument()
      expect(screen.getByRole('combobox', { name: 'Note type' })).toHaveTextContent('Auto')
    })

    it('New note → a kind opens the sheet already on that kind', async () => {
      render(<DocumentsPanel />)

      fireEvent.click(screen.getByRole('button', { name: 'More actions' }))
      fireEvent.click(await screen.findByRole('menuitem', { name: 'New note' }))
      fireEvent.click(await screen.findByRole('menuitem', { name: 'Quirk' }))

      expect(await screen.findByRole('heading', { name: 'Capture a note' }, { timeout: 5000 })).toBeInTheDocument()
      expect(screen.getByRole('combobox', { name: 'Note type' })).toHaveTextContent('Quirk')
    })

    it('offers every kind the classifier can produce, Auto first', async () => {
      render(<DocumentsPanel />)

      fireEvent.click(screen.getByRole('button', { name: 'More actions' }))
      fireEvent.click(await screen.findByRole('menuitem', { name: 'New note' }))

      for (const label of ['Auto', 'Contact', 'Procedure', 'Spec', 'Quirk', 'Recipe', 'Plain note']) {
        expect(await screen.findByRole('menuitem', { name: label })).toBeInTheDocument()
      }
    })

    it('has no second create control beside the page menu', () => {
      render(<DocumentsPanel />)

      expect(screen.queryByRole('button', { name: 'Add Note' })).not.toBeInTheDocument()
      expect(screen.queryByRole('button', { name: 'Choose note type' })).not.toBeInTheDocument()
    })

    it('New folder still opens the ordinary new-folder dialog', () => {
      render(<DocumentsPanel />)

      fireEvent.click(screen.getByRole('button', { name: 'More actions' }))
      fireEvent.click(screen.getByRole('menuitem', { name: 'New folder' }))

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

    // A file row's icon is a bare h-4 w-4 MimeIcon with no padding, but a
    // note row's own icon is a full h-8 w-8 NoteTypeIconButton (kept at
    // that size everywhere else - it's still a real tap target) - the
    // `sm` size variant it renders at here now carries -mx-2 so its 32px
    // button collapses back to the same 16px layout footprint as the file
    // row's bare icon, and the icon (and title beside it) line up exactly
    // rather than sitting ~8px further right.
    it("a note row's icon carries -mx-2 so it lines up with a file row's bare icon", () => {
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({
        documents: [doc({ id: 'note-1', filename: 'Ring Dave.md', title: 'Ring Dave', mime: 'text/markdown', kind: 'note', note_type: 'contact' })],
      }))

      render(<DocumentsPanel />)

      const button = screen.getByRole('button', { name: /change type: currently contact/i })
      expect(button.className).toContain('-mx-2')
    })
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

      const toggle = screen.getByRole('treeitem', { name: /unfiled notes/i })
      expect(toggle).toHaveTextContent('2')
      expect(screen.queryByRole('button', { name: /unfiled notes/i })).not.toBeInTheDocument()
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
      fireEvent.click(screen.getByRole('treeitem', { name: /unfiled notes/i }))
      fireEvent.click(screen.getByRole('button', { name: 'File "Ring Dave"' }))

      expect(screen.getByRole('dialog')).toHaveTextContent('Move')
    })

    // Unfiled notes view renders NoteTypeIconButton at `lg` (an 11-unit/44px
    // mobile tap target, unlike the dense table's `sm`) - the -mx-2 fix
    // above only applies to `sm`, and must not shrink this one's footprint.
    it("does not shrink the Unfiled notes view's own (lg) icon button", () => {
      mockedUseNotes.mockReturnValue(makeNotesMock({
        notes: [note({ id: 'note-1', title: 'Ring Dave', note_type: 'contact' })],
      }))

      render(<DocumentsPanel />)
      fireEvent.click(screen.getByRole('treeitem', { name: /unfiled notes/i }))

      const button = screen.getByRole('button', { name: /change type: currently contact/i })
      expect(button.className).toContain('h-11 w-11')
      expect(button.className).not.toContain('-mx-2')
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

      setFolderTree([treeFolder({ id: 'm1', name: 'Operations Manual', is_manual: true })])
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
      setFolderTree([treeFolder({ id: 'f1', name: 'Contacts' })])

      render(<DocumentsPanel />)
      fireEvent.click(screen.getByRole('button', { name: /actions for contacts/i }))
      fireEvent.click(screen.getByRole('menuitem', { name: /treat as a manual/i }))

      await waitFor(() => expect(flagManual).toHaveBeenCalledWith('f1'))
    })

    it('offers no "Treat as a manual" on a folder that is not top-level', () => {
      setFolderTree([
        treeFolder({ id: 'f1', name: 'Contacts' }),
        treeFolder({ id: 'f2', name: 'Yards', parent_id: 'f1' }),
      ])
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({ path: [{ id: 'f2', name: 'Yards', parent_id: 'f1' }] }))

      render(<DocumentsPanel initialFolderId="f2" />)
      fireEvent.click(screen.getByRole('button', { name: /actions for yards/i }))

      expect(screen.getByRole('menuitem', { name: /rename/i })).toBeInTheDocument()
      expect(screen.queryByRole('menuitem', { name: /treat as a manual/i })).not.toBeInTheDocument()
    })

    it('demoting from inside the manual view calls clearManual', async () => {
      const clearManual = vi.fn().mockResolvedValue(undefined)
      mockedUseManuals.mockReturnValue(makeManualsMock({
        manuals: [{ id: 'm1', name: 'Operations Manual', document_count: 0, updated_at: '2026-01-01T00:00:00Z' }],
        tree: { type: 'folder', id: 'm1', name: 'Operations Manual', sort_index: 0, updated_at: '2026-01-01T00:00:00Z', children: [] },
        clearManual,
      }))
      setFolderTree([treeFolder({ id: 'm1', name: 'Operations Manual', is_manual: true })])

      render(<DocumentsPanel initialFolderId="m1" />)
      fireEvent.click(screen.getByRole('button', { name: 'Stop treating as manual' }))

      await waitFor(() => expect(clearManual).toHaveBeenCalledWith('m1'))
    })

    it('demoting from the folder pane menu also calls clearManual', async () => {
      const clearManual = vi.fn().mockResolvedValue(undefined)
      mockedUseManuals.mockReturnValue(makeManualsMock({
        manuals: [{ id: 'm1', name: 'Operations Manual', document_count: 0, updated_at: '2026-01-01T00:00:00Z' }],
        clearManual,
      }))
      setFolderTree([treeFolder({ id: 'm1', name: 'Operations Manual', is_manual: true })])

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
      fireEvent.click(screen.getByRole('button', { name: 'More actions' }))
      fireEvent.click(screen.getByRole('menuitem', { name: 'New folder' }))

      fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Crew Training' } })
      fireEvent.click(screen.getByRole('checkbox', { name: /manual/i }))
      fireEvent.click(screen.getByRole('button', { name: 'Create' }))

      await waitFor(() => expect(createFolder).toHaveBeenCalledWith('Crew Training', null))
      await waitFor(() => expect(flagManual).toHaveBeenCalledWith('new-1'))
    })

    it('does not offer the Manual checkbox when creating a folder inside another folder', () => {
      render(<DocumentsPanel initialFolderId="parent-1" />)
      fireEvent.click(screen.getByRole('button', { name: 'More actions' }))
      fireEvent.click(screen.getByRole('menuitem', { name: 'New folder' }))

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

  // Backend chip-list narrowing (GET /api/documents/tags now excludes a
  // suggested tag used on only one document) can leave an already-active
  // filter out of documents.tags - e.g. an operator selected a tag while
  // it had two documents, then removed it from one of them, or Mate
  // re-enriched and the count dropped. selectedTag persists either way, so
  // the active filter must stay visible and clearable even without a chip
  // of its own in the freshly fetched list.
  describe('tag filter', () => {
    it('offers every tag the hook reports in the tag filter', async () => {
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({
        tags: [{ tag: 'engine', count: 3 }, { tag: 'receipts', count: 2 }],
      }))

      render(<DocumentsPanel />)

      fireEvent.click(screen.getByRole('combobox', { name: 'Filter by tag' }))
      expect(await screen.findByRole('option', { name: 'engine (3)' })).toBeInTheDocument()
      expect(screen.getByRole('option', { name: 'receipts (2)' })).toBeInTheDocument()
    })

    it('keeps the active filter visible and clearable when it drops out of the tag list', () => {
      const setSelectedTag = vi.fn()
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({
        // 'solo-suggested' no longer qualifies for the chip list (now used
        // on only one document, suggested source), but it is still the
        // active filter.
        tags: [{ tag: 'engine', count: 3 }],
        selectedTag: 'solo-suggested',
        setSelectedTag,
        documents: [doc({ id: 'doc-1', title: 'Impeller kit' })],
      }))

      render(<DocumentsPanel />)

      // The active filter is still the select's value, and stays clearable.
      const filter = screen.getByRole('combobox', { name: 'Filter by tag' })
      expect(filter).toHaveTextContent('solo-suggested')
      fireEvent.click(screen.getByRole('button', { name: 'Clear all' }))
      expect(setSelectedTag).toHaveBeenCalledWith(null)
    })
  })

  // The selection bar's own "Reindex…", between Move to… and Delete -
  // mirrors the single-document row menu's confirmation (AlertDialog below
  // "Reindex "name"?"), but summed across every selected document: total
  // pages (page_count, min 1 each) and, only for the PDFs/images among
  // them, an OCR cost estimate.
  describe('bulk reindex', () => {
    it('confirms with the summed page count and OCR estimate, then reindexes every selected id in order', async () => {
      const reindexDocument = vi.fn().mockResolvedValue(undefined)
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({
        documents: [
          doc({ id: 'doc-1', filename: 'a.pdf', mime: 'application/pdf', page_count: 5 }),
          doc({ id: 'doc-2', filename: 'b.txt', mime: 'text/plain', page_count: 0 }),
        ],
        reindexDocument,
      }))

      render(<DocumentsPanel />)

      fireEvent.click(screen.getByRole('checkbox', { name: 'Select a.pdf' }))
      fireEvent.click(screen.getByRole('checkbox', { name: 'Select b.txt' }))

      fireEvent.click(screen.getByRole('button', { name: /reindex/i }))

      expect(screen.getByRole('heading', { name: 'Reindex 2 documents?' })).toBeInTheDocument()
      // a.pdf: 5 pages; b.txt: page_count 0 floors to 1. Total 6.
      expect(screen.getByText(/This re-reads 6 page\(s\)/)).toBeInTheDocument()
      // Only a.pdf (a PDF) counts toward OCR: 5 pages * $0.002.
      expect(screen.getByText(/estimated cost: \$0\.010/)).toBeInTheDocument()

      fireEvent.click(screen.getByRole('button', { name: 'Reindex' }))

      await waitFor(() => expect(reindexDocument).toHaveBeenCalledTimes(2))
      expect(reindexDocument).toHaveBeenNthCalledWith(1, 'doc-1')
      expect(reindexDocument).toHaveBeenNthCalledWith(2, 'doc-2')
    })

    it('says nothing about OCR cost when none of the selected documents are PDFs or images', () => {
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({
        documents: [doc({ id: 'doc-1', filename: 'log.txt', mime: 'text/plain', page_count: 0 })],
      }))

      render(<DocumentsPanel />)
      fireEvent.click(screen.getByRole('checkbox', { name: 'Select log.txt' }))
      fireEvent.click(screen.getByRole('button', { name: /reindex/i }))

      expect(screen.getByRole('heading', { name: 'Reindex 1 document?' })).toBeInTheDocument()
      expect(screen.queryByText(/estimated cost/)).not.toBeInTheDocument()
    })

    it('a failure surfaces loudly through the panel\'s own error banner, not silently', async () => {
      const reindexDocument = vi.fn()
        .mockResolvedValueOnce(undefined)
        .mockRejectedValueOnce(new Error('reindex failed: disk full'))
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({
        documents: [
          doc({ id: 'doc-1', filename: 'a.pdf' }),
          doc({ id: 'doc-2', filename: 'b.pdf' }),
        ],
        reindexDocument,
      }))

      render(<DocumentsPanel />)
      fireEvent.click(screen.getByRole('checkbox', { name: 'Select a.pdf' }))
      fireEvent.click(screen.getByRole('checkbox', { name: 'Select b.pdf' }))
      fireEvent.click(screen.getByRole('button', { name: /reindex/i }))
      fireEvent.click(screen.getByRole('button', { name: 'Reindex' }))

      await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('reindex failed: disk full'))
      expect(reindexDocument).toHaveBeenCalledTimes(2)
    })
  })

  // ADR 0142 (Documents migration): the listing is Page + IndexFilters +
  // IndexTable, with bulk selection and ConfirmDelete.
  describe('on the pattern library', () => {
    it('titles the page with the current folder, Upload as the primary action and creation in the overflow', () => {
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({ path: [{ id: 'f1', name: 'Manuals', parent_id: null }] }))
      render(<DocumentsPanel initialFolderId="f1" />)
      expect(screen.getByRole('heading', { name: 'Manuals' })).toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'Upload' })).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: 'New' })).not.toBeInTheDocument()
      expect(screen.queryByRole('button', { name: /search documents/i })).not.toBeInTheDocument()
    })

    it('Upload opens the file picker', () => {
      render(<DocumentsPanel />)
      const input = screen.getByTestId('documents-file-input') as HTMLInputElement
      const click = vi.spyOn(input, 'click')
      fireEvent.click(screen.getByRole('button', { name: 'Upload' }))
      expect(click).toHaveBeenCalled()
    })

    it('clicking a folder in the pane opens it; clicking a document row opens the viewer', async () => {
      setFolderTree([treeFolder({ id: 'f1', name: 'Manuals' })])
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({
        documents: [doc({ id: 'doc-1', filename: 'receipt.pdf', mime: 'text/plain' })],
      }))
      const onFolderChange = vi.fn()
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({ text: 'body' }) }))
      render(<DocumentsPanel onFolderChange={onFolderChange} />)
      fireEvent.click(screen.getByRole('treeitem', { name: /Manuals/ }))
      expect(onFolderChange).toHaveBeenCalledWith('f1')
      expect(onFolderChange).toHaveBeenCalledTimes(1)
      fireEvent.click(screen.getByText('receipt.pdf'))
      expect(await screen.findByRole('dialog')).toBeInTheDocument()
    })

    it('an empty folder shows the empty state', () => {
      render(<DocumentsPanel />)
      expect(screen.getByText('Nothing here yet')).toBeInTheDocument()
      expect(screen.getByText('Upload a file or create a folder.')).toBeInTheDocument()
    })

    it('documents are selectable, with the bulk bar naming the count', () => {
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({
        documents: [doc({ id: 'doc-1', filename: 'a.pdf' }), doc({ id: 'doc-2', filename: 'b.pdf' })],
      }))
      render(<DocumentsPanel />)
      fireEvent.click(screen.getByRole('checkbox', { name: 'Select all' }))
      expect(screen.getByText('2 selected')).toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'Move to…' })).toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'Reindex…' })).toBeInTheDocument()
    })

    it('typing into the search field opens the overlay seeded with that character', () => {
      render(<DocumentsPanel />)
      fireEvent.keyDown(screen.getByRole('textbox', { name: 'Search' }), { key: 'g' })
      expect(screen.getByRole('searchbox', { name: /search documents/i })).toHaveValue('g')
    })

    it('a delete in flight keeps the dialog open and locked, and a refusal shows inside it', async () => {
      let reject!: (e: Error) => void
      const deleteFolder = vi.fn().mockReturnValue(new Promise((_, r) => { reject = r }))
      setFolderTree([treeFolder({ id: 'f1', name: 'Manuals' })])
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({ deleteFolder }))
      render(<DocumentsPanel />)
      fireEvent.click(screen.getByRole('button', { name: /actions for manuals/i }))
      fireEvent.click(screen.getByRole('menuitem', { name: /delete/i }))
      fireEvent.click(screen.getByRole('button', { name: 'Delete' }))

      expect(await screen.findByRole('button', { name: 'Deleting…' })).toBeDisabled()
      expect(screen.getByRole('button', { name: 'Cancel' })).toBeDisabled()

      await act(async () => { reject(new Error('folder is not empty')) })
      const dialog = screen.getByRole('alertdialog')
      expect(within(dialog).getByRole('alert')).toHaveTextContent('folder is not empty')
      expect(within(dialog).getByRole('button', { name: 'Delete' })).toBeEnabled()
    })

    it('filtering away selected documents drops them from the selection: Delete acts on what is visible only', async () => {
      const deleteDocument = vi.fn().mockResolvedValue(undefined)
      const all = [doc({ id: 'doc-1', filename: 'a.pdf' }), doc({ id: 'doc-2', filename: 'b.pdf' }), doc({ id: 'doc-3', filename: 'c.pdf' })]
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({ documents: all, deleteDocument }))
      const { rerender } = render(<DocumentsPanel />)
      fireEvent.click(screen.getByRole('checkbox', { name: 'Select all' }))
      expect(screen.getByText('3 selected')).toBeInTheDocument()

      // The tag filter narrows the listing to a.pdf.
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({ documents: [all[0]], deleteDocument }))
      rerender(<DocumentsPanel />)
      expect(await screen.findByText('1 selected')).toBeInTheDocument()

      fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
      expect(screen.getByRole('alertdialog')).toHaveTextContent('Delete 1 document?')
      fireEvent.click(within(screen.getByRole('alertdialog')).getByRole('button', { name: 'Delete' }))
      await waitFor(() => expect(deleteDocument).toHaveBeenCalledTimes(1))
      expect(deleteDocument).toHaveBeenCalledWith('doc-1')
    })

    it('a partly failed bulk delete resumes with what is left, never retrying one already deleted', async () => {
      let failB = true
      const deleteDocument = vi.fn(async (id: string) => {
        if (id === 'doc-2' && failB) throw new Error('b is locked')
      })
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({
        documents: [doc({ id: 'doc-1', filename: 'a.pdf' }), doc({ id: 'doc-2', filename: 'b.pdf' }), doc({ id: 'doc-3', filename: 'c.pdf' })],
        deleteDocument,
      }))
      render(<DocumentsPanel />)
      fireEvent.click(screen.getByRole('checkbox', { name: 'Select all' }))
      fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
      fireEvent.click(within(screen.getByRole('alertdialog')).getByRole('button', { name: 'Delete' }))

      const dialog = await screen.findByRole('alertdialog')
      await waitFor(() => expect(within(dialog).getByRole('alert')).toHaveTextContent('b is locked'))
      expect(deleteDocument.mock.calls.map(([id]) => id)).toEqual(['doc-1', 'doc-2'])
      // a.pdf is gone from the pending list: the dialog counts what remains.
      expect(dialog).toHaveTextContent('Delete 2 documents?')

      failB = false
      deleteDocument.mockClear()
      fireEvent.click(within(dialog).getByRole('button', { name: 'Delete' }))
      await waitFor(() => expect(deleteDocument.mock.calls.map(([id]) => id)).toEqual(['doc-2', 'doc-3']))
      await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument())
    })

    it('staged uploads list as a labelled ResourceList above the table', () => {
      mockedUseDocumentUploads.mockReturnValue(makeUploadsMock({
        items: [{ key: 'k1', filename: 'receipt.pdf', size: 1, status: 'failed', progress: 0, documentId: null, error: 'too big', duplicate: false }],
      }))
      render(<DocumentsPanel />)
      const list = screen.getByRole('list', { name: 'Uploads' })
      expect(within(list).getByText('receipt.pdf')).toBeInTheDocument()
      expect(within(list).getByText('too big')).toBeInTheDocument()
    })
  })
})

describe('DocumentsPanel listing columns', () => {
  const rowNames = () =>
    screen.getAllByRole('row').slice(1).map((r) => r.textContent ?? '')

  it('has no Status column and no Indexed badge', () => {
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({ documents: [doc()] }))
    render(<DocumentsPanel />)
    expect(screen.queryByRole('columnheader', { name: /status/i })).not.toBeInTheDocument()
    expect(screen.queryByText('Indexed')).not.toBeInTheDocument()
  })

  it('shows Added as day and month this year, and with the year otherwise', () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-10-12T12:00:00'))
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({
      documents: [
        doc({ id: 'a', filename: 'this-year.pdf', created_at: '2026-10-12T12:00:00' }),
        doc({ id: 'b', filename: 'last-year.pdf', created_at: '2025-01-03T12:00:00' }),
      ],
    }))
    render(<DocumentsPanel />)
    expect(screen.getByRole('columnheader', { name: /added/i })).toBeInTheDocument()
    expect(screen.getByText('12 Oct')).toBeInTheDocument()
    expect(screen.getByText('3 Jan 2025')).toBeInTheDocument()
  })

  it('sorts newest added first by default', () => {
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({
      documents: [
        doc({ id: 'a', filename: 'old.pdf', created_at: '2025-01-01T12:00:00' }),
        doc({ id: 'b', filename: 'new.pdf', created_at: '2026-03-01T12:00:00' }),
      ],
    }))
    render(<DocumentsPanel />)
    const rows = rowNames().filter((t) => /\.pdf/.test(t))
    expect(rows[0]).toContain('new.pdf')
    expect(rows[1]).toContain('old.pdf')
  })

  it('sorts Size numerically', () => {
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({
      documents: [
        doc({ id: 'a', filename: 'nine.pdf', size_bytes: 9 * 1024 }),
        doc({ id: 'b', filename: 'ten.pdf', size_bytes: 10 * 1024 * 1024 }),
        doc({ id: 'c', filename: 'hundred.pdf', size_bytes: 100 * 1024 }),
      ],
    }))
    render(<DocumentsPanel />)
    fireEvent.click(within(screen.getByRole('columnheader', { name: /size/i })).getByRole('button'))
    let rows = rowNames()
    const order = (names: string[]) => names.map((n) => rows.findIndex((r) => r.includes(n)))
    expect(order(['nine.pdf', 'hundred.pdf', 'ten.pdf'])).toEqual(
      [...order(['nine.pdf', 'hundred.pdf', 'ten.pdf'])].sort((x, y) => x - y),
    )
    fireEvent.click(within(screen.getByRole('columnheader', { name: /size/i })).getByRole('button'))
    rows = rowNames()
    const idx = order(['ten.pdf', 'hundred.pdf', 'nine.pdf'])
    expect(idx).toEqual([...idx].sort((x, y) => x - y))
  })

  it('remembers the chosen sort', () => {
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({
      documents: [
        doc({ id: 'a', filename: 'b-file.pdf' }),
        doc({ id: 'b', filename: 'A-file.pdf' }),
      ],
    }))
    const { unmount } = render(<DocumentsPanel />)
    fireEvent.click(within(screen.getByRole('columnheader', { name: /name/i })).getByRole('button'))
    unmount()
    render(<DocumentsPanel />)
    const rows = rowNames().filter((t) => /-file\.pdf/.test(t))
    expect(rows[0]).toContain('A-file.pdf')
  })

  it('keeps the latest sort when the listing remounts after Unfiled notes', () => {
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({
      documents: [
        doc({ id: 'a', filename: 'b-file.pdf', created_at: '2026-03-01T12:00:00' }),
        doc({ id: 'b', filename: 'A-file.pdf', created_at: '2025-01-01T12:00:00' }),
      ],
    }))
    setFolderTree([])
    render(<DocumentsPanel />)
    fireEvent.click(within(screen.getByRole('columnheader', { name: /name/i })).getByRole('button'))
    expect(rowNames().filter((t) => /-file\.pdf/.test(t))[0]).toContain('A-file.pdf')

    fireEvent.click(screen.getByRole('treeitem', { name: /Unfiled notes/ }))
    fireEvent.click(screen.getByRole('treeitem', { name: 'Documents' }))
    expect(rowNames().filter((t) => /-file\.pdf/.test(t))[0]).toContain('A-file.pdf')
  })

  it('marks a pending document with Reading and a failed one with Failed in its icon', () => {
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({
      documents: [
        doc({ id: 'a', filename: 'wait.pdf', status: 'pending' }),
        doc({ id: 'b', filename: 'bad.pdf', status: 'failed', error: 'boom' }),
      ],
    }))
    render(<DocumentsPanel />)
    expect(screen.getByText('Reading…')).toBeInTheDocument()
    expect(screen.getByText('Failed', { selector: '.sr-only' })).toBeInTheDocument()
  })

  it('shows at most three tags then a +N badge, with the full list in a title', () => {
    const names = ['one', 'two', 'three', 'four', 'five']
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({
      documents: [doc({ tags: names.map((tag) => ({ tag, source: 'operator' as const })) })],
    }))
    render(<DocumentsPanel />)
    expect(screen.getByText('three')).toBeInTheDocument()
    expect(screen.queryByText('four')).not.toBeInTheDocument()
    const more = screen.getByText('+2')
    expect(more.closest('[title]')).toHaveAttribute('title', 'one, two, three, four, five')
  })

  it('toggles Name between two states only, and restores the second after a remount', () => {
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({
      documents: [doc({ id: 'a', filename: 'b-file.pdf' }), doc({ id: 'b', filename: 'a-file.pdf' })],
    }))
    const click = () => fireEvent.click(within(screen.getByRole('columnheader', { name: /name/i })).getByRole('button'))
    const first = () => rowNames().filter((t) => /-file\.pdf/.test(t))[0]
    const { unmount } = render(<DocumentsPanel />)
    click()
    expect(first()).toContain('a-file.pdf')
    click()
    expect(first()).toContain('b-file.pdf')
    click()
    expect(first()).toContain('a-file.pdf')
    click()
    expect(first()).toContain('b-file.pdf')
    unmount()
    render(<DocumentsPanel />)
    expect(first()).toContain('b-file.pdf')
  })

  it('shows -- when Added is missing or unparseable', () => {
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({ documents: [doc({ created_at: 'nonsense' })] }))
    render(<DocumentsPanel />)
    expect(screen.getByText('--')).toBeInTheDocument()
  })

  it('puts the state in the note icon button accessible name', () => {
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({
      documents: [doc({ kind: 'note', note_type: 'general', status: 'pending', filename: 'n.md' })],
    }))
    render(<DocumentsPanel />)
    expect(screen.getByRole('button', { name: /Change type: currently .*\(Reading…\)/ })).toBeInTheDocument()
  })

  it('keeps the +N badge outside the clipped tag region', () => {
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({
      documents: [doc({ tags: ['a', 'b', 'c', 'd'].map((tag) => ({ tag, source: 'operator' as const })) })],
    }))
    render(<DocumentsPanel />)
    expect(screen.getByText('+1').parentElement!.closest('.overflow-hidden')).toBeNull()
  })
})

describe('DocumentsPanel folder pane', () => {
  // The Move dialog's folder picker mounts a ScrollArea, which calls
  // getAnimations on mount - see the 'search overlay' block's comment.
  beforeEach(() => { Element.prototype.getAnimations = () => [] })
  afterEach(() => { delete (Element.prototype as { getAnimations?: unknown }).getAnimations })

  const tree = [
    treeFolder({ id: 'f1', name: 'Manuals', document_count: 3 }),
    treeFolder({ id: 'f2', name: 'Engine', parent_id: 'f1', document_count: 2 }),
    // A manual is a leaf in the pane, so its count covers the whole manual.
    treeFolder({ id: 'm1', name: 'Operations Manual', is_manual: true, document_count: 1, subtree_document_count: 4 }),
    treeFolder({ id: 'm2', name: 'Section', parent_id: 'm1', document_count: 3 }),
  ]

  it('shows Documents, the folder hierarchy with counts, then Unfiled notes', () => {
    setFolderTree(tree)
    mockedUseNotes.mockReturnValue(makeNotesMock({ notes: [note({ id: 'n1' }), note({ id: 'n2' })] }))
    render(<DocumentsPanel />)

    const pane = screen.getByRole('tree', { name: 'Folders' })
    const names = within(pane).getAllByRole('treeitem').map((el) => el.textContent)
    // Engine is collapsed under Manuals; a manual's sections are never listed.
    expect(names).toEqual(['Documents', 'Manuals3', 'Operations Manual4', 'Unfiled notes2'])
    expect(within(pane).getByRole('treeitem', { name: /Manuals/ })).toHaveAttribute('aria-expanded', 'false')
    fireEvent.click(within(pane).getByRole('treeitem', { name: /Manuals/ }).querySelector('[data-slot="tree-chevron"]')!)
    expect(within(pane).getByRole('treeitem', { name: /Engine/ })).toHaveTextContent('2')
    expect(within(pane).getByRole('treeitem', { name: /Engine/ })).toHaveAttribute('aria-level', '2')
  })

  it('waits for the folder tree before choosing between a listing and a manual', () => {
    mockedUseFolderTree.mockReturnValue({ folders: [], loading: true, error: null, refresh: refreshFolderTree })
    render(<DocumentsPanel initialFolderId="m1" />)

    expect(screen.queryByRole('columnheader', { name: /name/i })).not.toBeInTheDocument()
    expect(screen.getByText('Loading…')).toBeInTheDocument()
  })

  it('says so when the folder tree failed instead of treating a manual as a plain folder', () => {
    mockedUseFolderTree.mockReturnValue({ folders: [], loading: false, error: 'HTTP 500', refresh: refreshFolderTree })
    render(<DocumentsPanel initialFolderId="m1" />)

    expect(screen.queryByRole('columnheader', { name: /name/i })).not.toBeInTheDocument()
    expect(screen.getByText(/Cannot tell whether this folder is a manual \(HTTP 500\)/)).toBeInTheDocument()
  })

  it('opens with the selected folder\'s ancestors expanded', () => {
    setFolderTree(tree)
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({ path: [{ id: 'f1', name: 'Manuals', parent_id: null }, { id: 'f2', name: 'Engine', parent_id: 'f1' }] }))
    render(<DocumentsPanel initialFolderId="f2" />)

    expect(screen.getByRole('treeitem', { name: /Engine/ })).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByRole('treeitem', { name: /Manuals/ })).toHaveAttribute('aria-expanded', 'true')
  })

  it('selecting a node navigates and mirrors the folder into the URL callback', () => {
    setFolderTree(tree)
    const onFolderChange = vi.fn()
    render(<DocumentsPanel initialFolderId="f1" onFolderChange={onFolderChange} />)

    fireEvent.click(screen.getByRole('treeitem', { name: /Operations Manual/ }))
    expect(onFolderChange).toHaveBeenLastCalledWith('m1')
    fireEvent.click(screen.getByRole('treeitem', { name: 'Documents' }))
    expect(onFolderChange).toHaveBeenLastCalledWith(null)
    expect(mockedUseDocuments).toHaveBeenLastCalledWith(null)
  })

  it('the Unfiled notes node switches the view without touching the folder', () => {
    setFolderTree(tree)
    mockedUseNotes.mockReturnValue(makeNotesMock({ notes: [note({ id: 'n1', title: 'Ring Dave' })] }))
    const onFolderChange = vi.fn()
    render(<DocumentsPanel onFolderChange={onFolderChange} />)

    fireEvent.click(screen.getByRole('treeitem', { name: /Unfiled notes/ }))
    expect(screen.getByText('Ring Dave')).toBeInTheDocument()
    expect(screen.getByRole('treeitem', { name: /Unfiled notes/ })).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByRole('heading', { name: 'Unfiled notes' })).toBeInTheDocument()
    expect(onFolderChange).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole('treeitem', { name: 'Documents' }))
    expect(screen.queryByText('Ring Dave')).not.toBeInTheDocument()
  })

  it('the folder menu renames a folder', async () => {
    setFolderTree(tree)
    const renameFolder = vi.fn().mockResolvedValue({})
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({ renameFolder }))
    render(<DocumentsPanel />)

    fireEvent.click(screen.getByRole('button', { name: 'Actions for Manuals' }))
    fireEvent.click(screen.getByRole('menuitem', { name: /rename/i }))
    const input = screen.getByDisplayValue('Manuals')
    fireEvent.change(input, { target: { value: 'Handbooks' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(renameFolder).toHaveBeenCalledWith('f1', 'Handbooks'))
  })

  it('Move… on a folder moves the folder, not documents', async () => {
    setFolderTree(tree)
    const moveFolder = vi.fn().mockResolvedValue({})
    const moveDocuments = vi.fn()
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({ moveFolder, moveDocuments }))
    render(<DocumentsPanel />)

    fireEvent.click(screen.getByRole('button', { name: 'Actions for Manuals' }))
    fireEvent.click(screen.getByRole('menuitem', { name: /move/i }))
    fireEvent.click(await screen.findByRole('button', { name: /move here/i }))

    await waitFor(() => expect(moveFolder).toHaveBeenCalledWith('f1', null))
    expect(moveDocuments).not.toHaveBeenCalled()
  })

  it('deleting the folder being browsed steps out to its parent', async () => {
    setFolderTree(tree)
    const deleteFolder = vi.fn().mockResolvedValue(undefined)
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({ deleteFolder, path: [{ id: 'f1', name: 'Manuals', parent_id: null }, { id: 'f2', name: 'Engine', parent_id: 'f1' }] }))
    const onFolderChange = vi.fn()
    render(<DocumentsPanel initialFolderId="f2" onFolderChange={onFolderChange} />)

    fireEvent.click(screen.getByRole('button', { name: 'Actions for Engine' }))
    fireEvent.click(screen.getByRole('menuitem', { name: /delete/i }))
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))

    await waitFor(() => expect(deleteFolder).toHaveBeenCalledWith('f2'))
    await waitFor(() => expect(onFolderChange).toHaveBeenLastCalledWith('f1'))
  })

  it('New folder still creates inside the current folder', async () => {
    setFolderTree(tree)
    const createFolder = vi.fn().mockResolvedValue({ id: 'new', name: 'Spares', parent_id: 'f1' })
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({ createFolder, path: [{ id: 'f1', name: 'Manuals', parent_id: null }] }))
    render(<DocumentsPanel initialFolderId="f1" />)

    fireEvent.click(screen.getByRole('button', { name: 'More actions' }))
    fireEvent.click(screen.getByRole('menuitem', { name: /new folder/i }))
    fireEvent.change(screen.getByLabelText(/name/i), { target: { value: 'Spares' } })
    fireEvent.click(screen.getByRole('button', { name: /create/i }))

    await waitFor(() => expect(createFolder).toHaveBeenCalledWith('Spares', 'f1'))
  })

  it('dragging a document row onto a folder moves it', async () => {
    setFolderTree(tree)
    const moveDocuments = vi.fn().mockResolvedValue(undefined)
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({ moveDocuments, documents: [doc({ id: 'd1', filename: 'a.pdf' })] }))
    render(<DocumentsPanel />)

    const row = screen.getByText('a.pdf').closest('tr')!
    const store: Record<string, string> = {}
    const dt = {
      get types() { return Object.keys(store) },
      getData: (t: string) => store[t] ?? '',
      setData: (t: string, v: string) => { store[t] = v },
      effectAllowed: '',
      dropEffect: '',
    }
    fireEvent.dragStart(row, { dataTransfer: dt })
    const target = screen.getByRole('treeitem', { name: /Operations Manual/ })
    fireEvent.dragOver(target, { dataTransfer: dt })
    expect(target.className).toContain('ring-primary')
    fireEvent.drop(target, { dataTransfer: dt })

    await waitFor(() => expect(moveDocuments).toHaveBeenCalledWith(['d1'], 'm1'))
  })

  it('dragging a selected row carries the whole selection; Documents is the root', async () => {
    setFolderTree(tree)
    const moveDocuments = vi.fn().mockResolvedValue(undefined)
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({
      moveDocuments,
      documents: [doc({ id: 'd1', filename: 'a.pdf' }), doc({ id: 'd2', filename: 'b.pdf' }), doc({ id: 'd3', filename: 'c.pdf' })],
    }))
    render(<DocumentsPanel />)

    fireEvent.click(screen.getByRole('checkbox', { name: 'Select a.pdf' }))
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select b.pdf' }))
    const store: Record<string, string> = {}
    const dt = {
      get types() { return Object.keys(store) },
      getData: (t: string) => store[t] ?? '',
      setData: (t: string, v: string) => { store[t] = v },
      effectAllowed: '',
      dropEffect: '',
    }
    fireEvent.dragStart(screen.getByText('a.pdf').closest('tr')!, { dataTransfer: dt })
    fireEvent.drop(screen.getByRole('treeitem', { name: 'Documents' }), { dataTransfer: dt })

    await waitFor(() => expect(moveDocuments).toHaveBeenCalledWith(expect.arrayContaining(['d1', 'd2']), null))
    expect(moveDocuments.mock.calls[0][0]).toHaveLength(2)
  })

  it('does not accept a drop of something that is not a document row', () => {
    setFolderTree(tree)
    const moveDocuments = vi.fn()
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({ moveDocuments }))
    render(<DocumentsPanel />)

    const dt = { types: ['Files'], getData: () => '', dropEffect: '' }
    fireEvent.drop(screen.getByRole('treeitem', { name: /Operations Manual/ }), { dataTransfer: dt })
    expect(moveDocuments).not.toHaveBeenCalled()
  })

  it('on a phone the pane is behind a Folders button in a sheet that closes on selection', () => {
    setViewportWidth(500)
    setFolderTree(tree)
    const onFolderChange = vi.fn()
    render(<DocumentsPanel onFolderChange={onFolderChange} />)

    expect(screen.queryByRole('tree', { name: 'Folders' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Folders' }))
    fireEvent.click(screen.getByRole('treeitem', { name: /Operations Manual/ }))

    expect(onFolderChange).toHaveBeenCalledWith('m1')
    return waitFor(() => expect(screen.queryByRole('tree', { name: 'Folders' })).not.toBeInTheDocument())
  })

  it('ignores a drop whose payload is not a non-empty list of ids', async () => {
    setFolderTree(tree)
    const moveDocuments = vi.fn()
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({ moveDocuments }))
    render(<DocumentsPanel />)

    const target = screen.getByRole('treeitem', { name: /Operations Manual/ })
    for (const payload of ['[]', '[1,2]', '"x"', '{"a":1}']) {
      const dt = { types: ['application/x-helmcentral-documents'], getData: () => payload, dropEffect: '' }
      fireEvent.drop(target, { dataTransfer: dt })
    }
    await new Promise((r) => setTimeout(r, 0))
    expect(moveDocuments).not.toHaveBeenCalled()
  })

  it('keeps the highlight while a drag crosses a child of the row', () => {
    setFolderTree(tree)
    render(<DocumentsPanel />)
    const target = screen.getByRole('treeitem', { name: /Operations Manual/ })
    const dt = { types: ['application/x-helmcentral-documents'], getData: () => '', dropEffect: '' }
    fireEvent.dragOver(target, { dataTransfer: dt })
    fireEvent(target, new MouseEvent('dragleave', { bubbles: true, relatedTarget: target.querySelector('span')! }))
    expect(target.className).toContain('ring-primary')
    fireEvent(target, new MouseEvent('dragleave', { bubbles: true, relatedTarget: document.body }))
    expect(target.className).not.toContain('ring-primary')
  })

  it('treats a folder as a manual by the tree\'s is_manual, not the manuals list', () => {
    setFolderTree(tree)
    mockedUseManuals.mockReturnValue(makeManualsMock({
      tree: { type: 'folder', id: 'm1', name: 'Operations Manual', sort_index: 0, updated_at: '2026-01-01T00:00:00Z', children: [docNode()] },
    }))
    render(<DocumentsPanel initialFolderId="m1" />)
    expect(screen.getByRole('button', { name: 'Arrange' })).toBeInTheDocument()
  })

  it('shows the tree\'s error instead of an empty pane', () => {
    mockedUseFolderTree.mockReturnValue({ folders: [], loading: false, error: 'HTTP 500', refresh: refreshFolderTree })
    render(<DocumentsPanel />)
    expect(screen.getByRole('alert')).toHaveTextContent('HTTP 500')
  })
})
