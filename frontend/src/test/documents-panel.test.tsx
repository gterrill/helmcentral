import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, within, act, waitFor } from '@testing-library/react'

import { DocumentsPanel } from '@/components/documents-panel'
import { useDocuments } from '@/hooks/use-documents'
import { useDocumentUploads } from '@/hooks/use-document-uploads'

// ADR 0106 F1: DocumentsPanel is tested against mocked hooks (App-level test
// convention - see e.g. app-sidebar-navigation.test.tsx), not real fetch:
// use-documents.test.ts and use-document-uploads.test.ts already exercise
// the real request/response wiring. This file is about what the panel does
// with whatever the hooks report - rendering, navigation, selection,
// confirmations - not the network.

vi.mock('@/hooks/use-documents')
vi.mock('@/hooks/use-document-uploads')

const mockedUseDocuments = vi.mocked(useDocuments)
const mockedUseDocumentUploads = vi.mocked(useDocumentUploads)

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
    dryRunEmbeddingsBackfill: vi.fn(),
    startEmbeddingsBackfill: vi.fn(),
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
    ...overrides,
  }
}

function embeddingsStatus(overrides: Partial<import('@/hooks/use-documents').DocumentEmbeddingsStatus> = {}) {
  return {
    enabled: true,
    model: 'openai/text-embedding-3-small',
    dimensions: 512,
    counts: { chunks_total: 10, chunks_embedded: 10, chunks_stale: 0, chunks_pending: 0, chars_pending: 0 },
    backfill: { running: false, chunks_embedded: 0, started_at: '' },
    ...overrides,
  }
}

beforeEach(() => {
  vi.clearAllMocks()
  mockedUseDocuments.mockReturnValue(makeDocumentsMock())
  mockedUseDocumentUploads.mockReturnValue(makeUploadsMock())
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

  it('shows a failed document\'s own error', () => {
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({
      documents: [doc({ status: 'failed', error: 'could not read PDF' })],
    }))

    render(<DocumentsPanel />)

    expect(screen.getByText('could not read PDF')).toBeInTheDocument()
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

  // ADR 0106 E1c (F1e): the toolbar's semantic-search status line and its
  // "Index for semantic search" backfill action.
  describe('embeddings status and backfill', () => {
    it('renders no status line at all when semantic search is off', () => {
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({
        embeddingsStatus: embeddingsStatus({ enabled: false, model: '', dimensions: 0, problem: 'No embedding model is configured. Set one in Settings → Assistant.' }),
      }))

      render(<DocumentsPanel />)

      expect(screen.queryByTestId('documents-embeddings-status')).not.toBeInTheDocument()
    })

    it('renders no status line before the initial status load resolves', () => {
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({ embeddingsStatus: null }))

      render(<DocumentsPanel />)

      expect(screen.queryByTestId('documents-embeddings-status')).not.toBeInTheDocument()
    })

    it('shows the pending chunk count and the index action when chunks are pending', () => {
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({
        embeddingsStatus: embeddingsStatus({
          counts: { chunks_total: 20, chunks_embedded: 8, chunks_stale: 0, chunks_pending: 12, chars_pending: 4800 },
        }),
      }))

      render(<DocumentsPanel />)

      expect(screen.getByText(/12 chunks not yet searchable by meaning/i)).toBeInTheDocument()
      expect(screen.getByRole('button', { name: /index for semantic search/i })).toBeEnabled()
    })

    it('the index action runs the dry run first and waits for confirmation before posting anything', async () => {
      const dryRunEmbeddingsBackfill = vi.fn().mockResolvedValue({
        counts: { chunks_total: 20, chunks_embedded: 8, chunks_stale: 0, chunks_pending: 12, chars_pending: 4800 },
        tokens_estimate: 1200,
      })
      const startEmbeddingsBackfill = vi.fn().mockResolvedValue(undefined)
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({
        embeddingsStatus: embeddingsStatus({
          counts: { chunks_total: 20, chunks_embedded: 8, chunks_stale: 0, chunks_pending: 12, chars_pending: 4800 },
        }),
        dryRunEmbeddingsBackfill,
        startEmbeddingsBackfill,
      }))

      render(<DocumentsPanel />)

      fireEvent.click(screen.getByRole('button', { name: /index for semantic search/i }))

      expect(await screen.findByText(/12 pending chunk/i)).toBeInTheDocument()
      expect(screen.getByText(/1200 tokens/i)).toBeInTheDocument()
      expect(dryRunEmbeddingsBackfill).toHaveBeenCalledTimes(1)
      expect(startEmbeddingsBackfill).not.toHaveBeenCalled()

      fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))

      await waitFor(() => expect(screen.queryByText(/1200 tokens/i)).not.toBeInTheDocument())
      expect(startEmbeddingsBackfill).not.toHaveBeenCalled()
    })

    it('confirming the index action posts the backfill', async () => {
      const dryRunEmbeddingsBackfill = vi.fn().mockResolvedValue({
        counts: { chunks_total: 20, chunks_embedded: 8, chunks_stale: 0, chunks_pending: 12, chars_pending: 4800 },
        tokens_estimate: 1200,
      })
      const startEmbeddingsBackfill = vi.fn().mockResolvedValue(undefined)
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({
        embeddingsStatus: embeddingsStatus({
          counts: { chunks_total: 20, chunks_embedded: 8, chunks_stale: 0, chunks_pending: 12, chars_pending: 4800 },
        }),
        dryRunEmbeddingsBackfill,
        startEmbeddingsBackfill,
      }))

      render(<DocumentsPanel />)

      fireEvent.click(screen.getByRole('button', { name: /index for semantic search/i }))
      await screen.findByText(/1200 tokens/i)

      fireEvent.click(screen.getByRole('button', { name: 'Index' }))

      await waitFor(() => expect(startEmbeddingsBackfill).toHaveBeenCalledTimes(1))
    })

    it('a running backfill disables the action button and shows progress', () => {
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({
        embeddingsStatus: embeddingsStatus({
          counts: { chunks_total: 20, chunks_embedded: 15, chunks_stale: 0, chunks_pending: 5, chars_pending: 2000 },
          backfill: { running: true, chunks_embedded: 7, started_at: '2026-09-18T00:00:00Z' },
        }),
      }))

      render(<DocumentsPanel />)

      expect(screen.getByText(/7 embedded this run, 5 left/i)).toBeInTheDocument()
      expect(screen.getByRole('button', { name: /index for semantic search/i })).toBeDisabled()
    })

    it("shows a running backfill's last_error as a quiet warning, since the backfill keeps retrying rather than stopping", () => {
      mockedUseDocuments.mockReturnValue(makeDocumentsMock({
        embeddingsStatus: embeddingsStatus({
          counts: { chunks_total: 20, chunks_embedded: 15, chunks_stale: 0, chunks_pending: 5, chars_pending: 2000 },
          backfill: { running: true, chunks_embedded: 7, started_at: '2026-09-18T00:00:00Z', last_error: 'OpenRouter: 429 rate limited' },
        }),
      }))

      render(<DocumentsPanel />)

      expect(screen.getByText('OpenRouter: 429 rate limited')).toBeInTheDocument()
      expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    })
  })
})
