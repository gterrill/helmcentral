import { createRef } from 'react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react'
import type { ReactElement } from 'react'
import { SaveBarSlot } from '@/components/patterns/save-bar-slot'

import { DocumentDetailsPage, type DocumentDetailsPageHandle } from '@/components/document-details-page'
import { useDocument, useFolderPath } from '@/hooks/use-documents'

// ADR 0115 §3: DocumentDetailsPage is tested against a mocked
// useDocument, the same way documents-panel.test.tsx mocks useDocuments -
// use-documents.test.ts already exercises the real fetch/PATCH wiring, so
// this file is about what the page does with whatever the hook reports.

vi.mock('@/hooks/use-documents')

const mockedUseDocument = vi.mocked(useDocument)
const mockedUseFolderPath = vi.mocked(useFolderPath)

// The Move dialog's picker browses folders through useDocuments.
vi.mock('@/components/documents/move-to-folder-dialog', () => ({
  MoveToFolderDialog: ({ open, onPick }: { open: boolean; onPick: (id: string | null) => void }) =>
    open ? <button onClick={() => onPick('f-new')}>Pick f-new</button> : null,
}))

type DocumentMock = ReturnType<typeof useDocument>

function makeDocumentMock(overrides: Partial<DocumentMock> = {}): DocumentMock {
  return {
    document: null,
    loading: false,
    error: null,
    refresh: vi.fn(),
    patch: vi.fn(),
    move: vi.fn().mockResolvedValue(undefined),
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
    indexed_at: '2026-01-02T00:00:00Z',
    kind: 'file',
    note_type: '',
    note_type_source: '',
    pinned: false,
    sort_index: 0,
    ...overrides,
  }
}

// ADR 0142: the Save/Discard bar now takes over the app header (Polaris'
// contextual save bar) by portalling into the SaveBarSlot the shell mounts,
// so every render here carries the same stand-in header App.tsx provides.
function renderPage(ui: ReactElement) {
  const wrap = (page: ReactElement) => (
    <div>
      <header data-testid="header"><SaveBarSlot /></header>
      {page}
    </div>
  )
  const result = render(wrap(ui))
  return { ...result, rerender: (next: ReactElement) => result.rerender(wrap(next)) }
}

beforeEach(() => {
  vi.clearAllMocks()
  mockedUseDocument.mockReturnValue(makeDocumentMock())
  mockedUseFolderPath.mockReturnValue({ path: [], error: null })
})

describe('DocumentDetailsPage', () => {
  it('shows a full date, not a bare time, for Uploaded and Last indexed', () => {
    mockedUseDocument.mockReturnValue(makeDocumentMock({
      document: doc({ created_at: '2026-09-20T06:28:00', indexed_at: '2026-09-20T06:28:00' }),
    }))

    renderPage(<DocumentDetailsPage documentId="doc-1" onOpenFolder={vi.fn()} />)

    // formatAlarmTime would print a bare "06:28" for a same-day timestamp -
    // the Details page needs formatDocumentTime's full date instead, since
    // an eighteen-month-old upload is the normal case here, not the
    // exception.
    expect(screen.getAllByText('20 Sep 2026 06:28')).toHaveLength(2)
  })

  it('shows "Not yet" for Last indexed when the document has never been indexed', () => {
    mockedUseDocument.mockReturnValue(makeDocumentMock({
      document: doc({ indexed_at: null }),
    }))

    renderPage(<DocumentDetailsPage documentId="doc-1" onOpenFolder={vi.fn()} />)

    expect(screen.getByText('Not yet')).toBeInTheDocument()
  })

  it('shows an operator-facing message for a failed document, not the raw stored error', () => {
    mockedUseDocument.mockReturnValue(makeDocumentMock({
      document: doc({
        status: 'failed',
        stage: 'extract',
        mime: 'application/pdf',
        error: 'pdf page 1: panic: pred',
      }),
    }))

    renderPage(<DocumentDetailsPage documentId="doc-1" onOpenFolder={vi.fn()} />)

    // Scoped to the Status row specifically: the raw error legitimately
    // does still appear elsewhere on the page, collapsed under "Error
    // details" (next test) - it just must not be what Status itself says.
    const statusDd = screen.getByText('Status').nextElementSibling
    expect(statusDd?.textContent).toContain("Couldn't read the text in this PDF")
    expect(statusDd?.textContent).not.toContain('panic')
  })

  it('keeps the raw stored error available under a collapsed "Error details" disclosure', () => {
    mockedUseDocument.mockReturnValue(makeDocumentMock({
      document: doc({
        status: 'failed',
        stage: 'extract',
        mime: 'application/pdf',
        error: 'pdf page 1: panic: pred',
      }),
    }))

    renderPage(<DocumentDetailsPage documentId="doc-1" onOpenFolder={vi.fn()} />)

    const summary = screen.getByText('Error details')
    expect(summary.closest('details')).not.toHaveAttribute('open')
    expect(screen.getByText('pdf page 1: panic: pred')).toBeInTheDocument()
  })

  it('has no "Error details" disclosure for a document that has not failed', () => {
    mockedUseDocument.mockReturnValue(makeDocumentMock({ document: doc({ status: 'indexed' }) }))

    renderPage(<DocumentDetailsPage documentId="doc-1" onOpenFolder={vi.fn()} />)

    expect(screen.queryByText('Error details')).not.toBeInTheDocument()
  })

  it('labels the reader row "Read by" and translates indexed_with to operator wording', () => {
    mockedUseDocument.mockReturnValue(makeDocumentMock({ document: doc({ indexed_with: 'mate' }) }))
    const { rerender } = renderPage(<DocumentDetailsPage documentId="doc-1" onOpenFolder={vi.fn()} />)

    expect(screen.getByText('Read by')).toBeInTheDocument()
    expect(screen.getByText('Mate')).toBeInTheDocument()

    mockedUseDocument.mockReturnValue(makeDocumentMock({ document: doc({ indexed_with: 'local' }) }))
    rerender(<DocumentDetailsPage documentId="doc-1" onOpenFolder={vi.fn()} />)

    expect(screen.getByText('On board')).toBeInTheDocument()
  })

  it('shows the indexing cost to four places', () => {
    mockedUseDocument.mockReturnValue(makeDocumentMock({ document: doc({ index_cost_usd: 0.0123 }) }))

    renderPage(<DocumentDetailsPage documentId="doc-1" onOpenFolder={vi.fn()} />)

    expect(screen.getByText('$0.0123')).toBeInTheDocument()
  })

  it('shows the save bar in the header only once something changes', () => {
    mockedUseDocument.mockReturnValue(makeDocumentMock({ document: doc({ title: 'Impeller kit' }) }))

    renderPage(<DocumentDetailsPage documentId="doc-1" onOpenFolder={vi.fn()} />)

    expect(screen.queryByRole('button', { name: 'Save' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Discard' })).not.toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'Impeller kit v2' } })

    const header = screen.getByTestId('header')
    expect(within(header).getByRole('button', { name: 'Save' })).toBeEnabled()
    expect(within(header).getByRole('button', { name: 'Discard' })).toBeEnabled()
  })

  it('Discard restores the loaded values and takes the bar away', () => {
    mockedUseDocument.mockReturnValue(makeDocumentMock({ document: doc({ title: 'Impeller kit' }) }))
    renderPage(<DocumentDetailsPage documentId="doc-1" onOpenFolder={vi.fn()} />)

    fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'Impeller kit v2' } })
    fireEvent.click(screen.getByRole('button', { name: 'Discard' }))

    expect(screen.getByLabelText('Title')).toHaveValue('Impeller kit')
    expect(screen.queryByRole('button', { name: 'Save' })).not.toBeInTheDocument()
  })

  // Main column edits; the aside holds the read-only facts about the file.
  it('puts the editable fields in the main column and the file and indexing facts in the aside', () => {
    mockedUseDocument.mockReturnValue(makeDocumentMock({ document: doc({ title: 'Impeller kit', size_bytes: 2048, page_count: 5 }) }))
    renderPage(<DocumentDetailsPage documentId="doc-1" onOpenFolder={vi.fn()} />)

    const aside = screen.getByRole('complementary')
    for (const label of ['File', 'Type', 'Size', 'Pages', 'Uploaded', 'Status', 'Read by', 'Model', 'Indexing cost', 'Last indexed']) {
      expect(within(aside).getByText(label)).toBeInTheDocument()
    }
    expect(within(aside).queryByLabelText('Title')).not.toBeInTheDocument()
    expect(screen.getByLabelText('Title')).toBeInTheDocument()
    expect(screen.getByLabelText('Notes')).toBeInTheDocument()
    expect(screen.getByLabelText('Add tag')).toBeInTheDocument()
  })

  it('titles the page with the document name', () => {
    mockedUseDocument.mockReturnValue(makeDocumentMock({ document: doc({ title: 'Impeller kit' }) }))
    renderPage(<DocumentDetailsPage documentId="doc-1" onOpenFolder={vi.fn()} />)
    expect(screen.getByRole('heading', { name: 'Impeller kit' })).toBeInTheDocument()
  })

  it('editing the title and notes and saving sends both in one patch', async () => {
    const patch = vi.fn().mockResolvedValue(doc())
    mockedUseDocument.mockReturnValue(makeDocumentMock({ document: doc(), patch }))

    renderPage(<DocumentDetailsPage documentId="doc-1" onOpenFolder={vi.fn()} />)

    fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'Impeller kit' } })
    fireEvent.change(screen.getByLabelText('Notes'), { target: { value: 'spares aboard' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(patch).toHaveBeenCalledTimes(1))
    expect(patch).toHaveBeenCalledWith({ title: 'Impeller kit', notes: 'spares aboard', tags: [] })
  })

  it('adding a tag, removing a tag and keeping a suggested tag all land in the saved tags array', async () => {
    const patch = vi.fn().mockResolvedValue(doc())
    mockedUseDocument.mockReturnValue(makeDocumentMock({
      document: doc({
        tags: [
          { tag: 'engine', source: 'operator' },
          { tag: 'old', source: 'operator' },
          { tag: 'oil-filter', source: 'suggested' },
          { tag: 'winterizing', source: 'suggested' },
        ],
      }),
      patch,
    }))

    renderPage(<DocumentDetailsPage documentId="doc-1" onOpenFolder={vi.fn()} />)

    // Remove an existing operator tag.
    fireEvent.click(screen.getByRole('button', { name: 'Remove tag old' }))
    // Add a brand new tag by typing into the input and clicking Add.
    fireEvent.change(screen.getByLabelText('Add tag'), { target: { value: 'spares' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))
    // Keep one of the two suggested tags, leaving the other merely suggested.
    fireEvent.click(screen.getByRole('button', { name: 'Keep tag oil-filter' }))

    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(patch).toHaveBeenCalledTimes(1))
    const sentTags = (patch.mock.calls[0][0] as { tags: string[] }).tags
    expect(sentTags).toHaveLength(3)
    expect(sentTags).toEqual(expect.arrayContaining(['engine', 'spares', 'oil-filter']))
    expect(sentTags).not.toContain('old')
    expect(sentTags).not.toContain('winterizing')
  })

  it('a blank or duplicate tag entry is ignored', () => {
    mockedUseDocument.mockReturnValue(makeDocumentMock({
      document: doc({ tags: [{ tag: 'engine', source: 'operator' }] }),
    }))

    renderPage(<DocumentDetailsPage documentId="doc-1" onOpenFolder={vi.fn()} />)

    fireEvent.change(screen.getByLabelText('Add tag'), { target: { value: '   ' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))
    fireEvent.change(screen.getByLabelText('Add tag'), { target: { value: 'engine' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))

    // Neither the blank entry nor the duplicate created a second "engine"
    // badge or dirtied the draft.
    expect(screen.getAllByText('engine')).toHaveLength(1)
    expect(screen.queryByRole('button', { name: 'Save' })).not.toBeInTheDocument()
  })

  it('a rejected save shows the server\'s message and leaves the draft intact', async () => {
    const patch = vi.fn().mockRejectedValue(new Error('title already used'))
    mockedUseDocument.mockReturnValue(makeDocumentMock({ document: doc({ title: 'Impeller kit' }), patch }))

    renderPage(<DocumentDetailsPage documentId="doc-1" onOpenFolder={vi.fn()} />)

    fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'Impeller kit v2' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('title already used')
    expect(screen.getByLabelText('Title')).toHaveValue('Impeller kit v2')
    // Nothing was cleared - the bar stays, with its Save live for a retry.
    expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled()
    expect(within(screen.getByTestId('header')).getByRole('alert')).toHaveTextContent('title already used')
  })

  it('shows the full folder path as a breadcrumb ending at the document', () => {
    mockedUseFolderPath.mockReturnValue({ path: [
      { id: 'f1', name: 'Boat', parent_id: null },
      { id: 'f2', name: 'Manuals', parent_id: 'f1' },
    ], error: null })
    mockedUseDocument.mockReturnValue(makeDocumentMock({ document: doc({ folder_id: 'f2', title: 'Impeller kit' }) }))

    renderPage(<DocumentDetailsPage documentId="doc-1" onOpenFolder={vi.fn()} />)

    const nav = screen.getByRole('navigation', { name: 'breadcrumb' })
    expect(within(nav).getAllByRole('link').filter((l) => !l.hasAttribute('aria-current')).map((l) => l.textContent)).toEqual(['Documents', 'Boat', 'Manuals'])
    expect(within(nav).getByText('Impeller kit')).toBeInTheDocument()
  })

  it('the Documents crumb opens the root and a folder crumb opens that folder', () => {
    const onOpenFolder = vi.fn()
    mockedUseFolderPath.mockReturnValue({ path: [{ id: 'f1', name: 'Boat', parent_id: null }], error: null })
    mockedUseDocument.mockReturnValue(makeDocumentMock({ document: doc({ folder_id: 'f1' }) }))

    renderPage(<DocumentDetailsPage documentId="doc-1" onOpenFolder={onOpenFolder} />)

    fireEvent.click(screen.getByRole('link', { name: 'Boat' }))
    expect(onOpenFolder).toHaveBeenLastCalledWith('f1')
    fireEvent.click(screen.getByRole('link', { name: 'Documents' }))
    expect(onOpenFolder).toHaveBeenLastCalledWith(null)
  })

  it('shows the current folder in the About the file column', () => {
    mockedUseFolderPath.mockReturnValue({ path: [{ id: 'f1', name: 'Boat', parent_id: null }], error: null })
    mockedUseDocument.mockReturnValue(makeDocumentMock({ document: doc({ folder_id: 'f1' }) }))

    renderPage(<DocumentDetailsPage documentId="doc-1" onOpenFolder={vi.fn()} />)

    expect(screen.getByText('Folder').nextElementSibling).toHaveTextContent('Boat')
  })

  it('says Documents for a document filed at the top level', () => {
    mockedUseDocument.mockReturnValue(makeDocumentMock({ document: doc({ folder_id: null }) }))

    renderPage(<DocumentDetailsPage documentId="doc-1" onOpenFolder={vi.fn()} />)

    expect(screen.getByText('Folder').nextElementSibling).toHaveTextContent('Documents')
  })

  it('Move opens the folder picker and moves the document, then reports the new folder', async () => {
    const move = vi.fn().mockResolvedValue(undefined)
    const onFolderContextChange = vi.fn()
    mockedUseDocument.mockReturnValue(makeDocumentMock({ document: doc(), move }))

    renderPage(<DocumentDetailsPage documentId="doc-1" onOpenFolder={vi.fn()} onFolderContextChange={onFolderContextChange} />)

    fireEvent.click(screen.getByRole('button', { name: 'Move' }))
    fireEvent.click(screen.getByRole('button', { name: 'Pick f-new' }))

    await waitFor(() => expect(move).toHaveBeenCalledWith('f-new'))
    await waitFor(() => expect(onFolderContextChange).toHaveBeenCalledWith('f-new'))
  })

  it('a rejected move shows the server message and stays put', async () => {
    const move = vi.fn().mockRejectedValue(new Error('folder not found'))
    mockedUseDocument.mockReturnValue(makeDocumentMock({ document: doc(), move }))

    renderPage(<DocumentDetailsPage documentId="doc-1" onOpenFolder={vi.fn()} />)

    fireEvent.click(screen.getByRole('button', { name: 'Move' }))
    fireEvent.click(screen.getByRole('button', { name: 'Pick f-new' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('folder not found')
  })

  it('reports the document name so Mate knows what is on screen', () => {
    const onTitleChange = vi.fn()
    mockedUseDocument.mockReturnValue(makeDocumentMock({ document: doc({ title: 'Tide tables' }) }))

    renderPage(<DocumentDetailsPage documentId="doc-1" onOpenFolder={vi.fn()} onTitleChange={onTitleChange} />)

    expect(onTitleChange).toHaveBeenLastCalledWith('Tide tables')
  })

  it('shows a quiet loading state while the document has not arrived yet', () => {
    mockedUseDocument.mockReturnValue(makeDocumentMock({ loading: true, document: null }))

    renderPage(<DocumentDetailsPage documentId="doc-1" onOpenFolder={vi.fn()} />)

    expect(screen.getByText(/loading/i)).toBeInTheDocument()
  })

  it('shows the server\'s error message on a failed fetch', () => {
    mockedUseDocument.mockReturnValue(makeDocumentMock({ loading: false, document: null, error: 'document not found' }))

    renderPage(<DocumentDetailsPage documentId="doc-1" onOpenFolder={vi.fn()} />)

    expect(screen.getByRole('alert')).toHaveTextContent('document not found')
  })

  it('shows a not-found message when the fetch resolved to nothing', () => {
    mockedUseDocument.mockReturnValue(makeDocumentMock({ loading: false, document: null, error: null }))

    renderPage(<DocumentDetailsPage documentId="doc-1" onOpenFolder={vi.fn()} />)

    expect(screen.getByText('This document could not be found.')).toBeInTheDocument()
  })

  // ADR 0115 §2 review finding: App.tsx's dirty-navigation guard (already
  // built for Settings) generalizes to cover this page too, reported the
  // same way SettingsPage reports settingsDirty.
  describe('onDirtyChange', () => {
    it('reports false while clean, true once the draft diverges, and false again after Discard', () => {
      const onDirtyChange = vi.fn()
      mockedUseDocument.mockReturnValue(makeDocumentMock({ document: doc({ title: 'Impeller kit' }) }))

      renderPage(<DocumentDetailsPage documentId="doc-1" onOpenFolder={vi.fn()} onDirtyChange={onDirtyChange} />)

      expect(onDirtyChange).toHaveBeenLastCalledWith(false)

      fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'Impeller kit v2' } })
      expect(onDirtyChange).toHaveBeenLastCalledWith(true)

      fireEvent.click(screen.getByRole('button', { name: 'Discard' }))
      expect(onDirtyChange).toHaveBeenLastCalledWith(false)
    })

    it('reports false again after a successful Save', async () => {
      const onDirtyChange = vi.fn()
      // The mocked hook's own `document` has to move the same way the real
      // useDocument's does once patch() resolves (it sets its `document`
      // state to the server's echoed-back response) - otherwise the draft
      // would keep comparing against the stale pre-save title forever.
      let currentDoc = doc({ title: 'Impeller kit' })
      const patch = vi.fn(async (patchBody: import('@/hooks/use-documents').DocumentPatch) => {
        currentDoc = { ...currentDoc, title: patchBody.title ?? currentDoc.title, notes: patchBody.notes ?? currentDoc.notes }
        return currentDoc
      })
      mockedUseDocument.mockImplementation(() => makeDocumentMock({ document: currentDoc, patch }))

      renderPage(<DocumentDetailsPage documentId="doc-1" onOpenFolder={vi.fn()} onDirtyChange={onDirtyChange} />)

      fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'Impeller kit v2' } })
      expect(onDirtyChange).toHaveBeenLastCalledWith(true)

      fireEvent.click(screen.getByRole('button', { name: 'Save' }))

      await waitFor(() => expect(onDirtyChange).toHaveBeenLastCalledWith(false))
    })
  })

  // ADR 0115 §2 review finding: exposed so App.tsx's "Save and Continue"
  // (the generalized Settings dirty-navigation guard) can save this page's
  // draft itself, on the same contract SettingsPageHandle's save() already
  // gives it.
  describe('the imperative save() handle', () => {
    it('saves the current draft and rejects with the server\'s message on failure', async () => {
      const patch = vi.fn().mockRejectedValue(new Error('title already used'))
      mockedUseDocument.mockReturnValue(makeDocumentMock({ document: doc({ title: 'Impeller kit' }), patch }))
      const ref = createRef<DocumentDetailsPageHandle>()

      renderPage(<DocumentDetailsPage ref={ref} documentId="doc-1" onOpenFolder={vi.fn()} />)
      fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'Impeller kit v2' } })

      await expect(ref.current!.save()).rejects.toThrow('title already used')
      expect(patch).toHaveBeenCalledWith({ title: 'Impeller kit v2', notes: '', tags: [] })
    })

    it('saves the current draft on success, the same fields the on-page Save button sends', async () => {
      const patch = vi.fn().mockResolvedValue(doc({ title: 'Impeller kit v2' }))
      mockedUseDocument.mockReturnValue(makeDocumentMock({ document: doc({ title: 'Impeller kit' }), patch }))
      const ref = createRef<DocumentDetailsPageHandle>()

      renderPage(<DocumentDetailsPage ref={ref} documentId="doc-1" onOpenFolder={vi.fn()} />)
      fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'Impeller kit v2' } })

      await ref.current!.save()

      expect(patch).toHaveBeenCalledWith({ title: 'Impeller kit v2', notes: '', tags: [] })
    })
  })
})
