import { createRef } from 'react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'

import { DocumentDetailsPage, type DocumentDetailsPageHandle } from '@/components/document-details-page'
import { useDocument } from '@/hooks/use-documents'

// ADR 0115 §3: DocumentDetailsPage is tested against a mocked
// useDocument, the same way documents-panel.test.tsx mocks useDocuments -
// use-documents.test.ts already exercises the real fetch/PATCH wiring, so
// this file is about what the page does with whatever the hook reports.

vi.mock('@/hooks/use-documents')

const mockedUseDocument = vi.mocked(useDocument)

type DocumentMock = ReturnType<typeof useDocument>

function makeDocumentMock(overrides: Partial<DocumentMock> = {}): DocumentMock {
  return {
    document: null,
    loading: false,
    error: null,
    refresh: vi.fn(),
    patch: vi.fn(),
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
    ...overrides,
  }
}

beforeEach(() => {
  vi.clearAllMocks()
  mockedUseDocument.mockReturnValue(makeDocumentMock())
})

describe('DocumentDetailsPage', () => {
  it('shows a full date, not a bare time, for Uploaded and Last indexed', () => {
    mockedUseDocument.mockReturnValue(makeDocumentMock({
      document: doc({ created_at: '2026-09-20T06:28:00', indexed_at: '2026-09-20T06:28:00' }),
    }))

    render(<DocumentDetailsPage documentId="doc-1" onBack={vi.fn()} />)

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

    render(<DocumentDetailsPage documentId="doc-1" onBack={vi.fn()} />)

    expect(screen.getByText('Not yet')).toBeInTheDocument()
  })

  it('labels the reader row "Read by" and translates indexed_with to operator wording', () => {
    mockedUseDocument.mockReturnValue(makeDocumentMock({ document: doc({ indexed_with: 'mate' }) }))
    const { rerender } = render(<DocumentDetailsPage documentId="doc-1" onBack={vi.fn()} />)

    expect(screen.getByText('Read by')).toBeInTheDocument()
    expect(screen.getByText('Mate')).toBeInTheDocument()

    mockedUseDocument.mockReturnValue(makeDocumentMock({ document: doc({ indexed_with: 'local' }) }))
    rerender(<DocumentDetailsPage documentId="doc-1" onBack={vi.fn()} />)

    expect(screen.getByText('On board')).toBeInTheDocument()
  })

  it('shows the indexing cost to four places', () => {
    mockedUseDocument.mockReturnValue(makeDocumentMock({ document: doc({ index_cost_usd: 0.0123 }) }))

    render(<DocumentDetailsPage documentId="doc-1" onBack={vi.fn()} />)

    expect(screen.getByText('$0.0123')).toBeInTheDocument()
  })

  it('disables Save until something changes', () => {
    mockedUseDocument.mockReturnValue(makeDocumentMock({ document: doc({ title: 'Impeller kit' }) }))

    render(<DocumentDetailsPage documentId="doc-1" onBack={vi.fn()} />)

    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Discard' })).toBeDisabled()

    fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'Impeller kit v2' } })

    expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled()
    expect(screen.getByRole('button', { name: 'Discard' })).toBeEnabled()
  })

  it('editing the title and notes and saving sends both in one patch', async () => {
    const patch = vi.fn().mockResolvedValue(doc())
    mockedUseDocument.mockReturnValue(makeDocumentMock({ document: doc(), patch }))

    render(<DocumentDetailsPage documentId="doc-1" onBack={vi.fn()} />)

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

    render(<DocumentDetailsPage documentId="doc-1" onBack={vi.fn()} />)

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

    render(<DocumentDetailsPage documentId="doc-1" onBack={vi.fn()} />)

    fireEvent.change(screen.getByLabelText('Add tag'), { target: { value: '   ' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))
    fireEvent.change(screen.getByLabelText('Add tag'), { target: { value: 'engine' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))

    // Neither the blank entry nor the duplicate created a second "engine"
    // badge or dirtied the draft.
    expect(screen.getAllByText('engine')).toHaveLength(1)
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
  })

  it('a rejected save shows the server\'s message and leaves the draft intact', async () => {
    const patch = vi.fn().mockRejectedValue(new Error('title already used'))
    mockedUseDocument.mockReturnValue(makeDocumentMock({ document: doc({ title: 'Impeller kit' }), patch }))

    render(<DocumentDetailsPage documentId="doc-1" onBack={vi.fn()} />)

    fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'Impeller kit v2' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('title already used')
    expect(screen.getByLabelText('Title')).toHaveValue('Impeller kit v2')
    // Nothing was cleared - Save/Discard are still live for a retry.
    expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled()
  })

  it('the breadcrumb\'s Documents link calls onBack', () => {
    const onBack = vi.fn()
    mockedUseDocument.mockReturnValue(makeDocumentMock({ document: doc() }))

    render(<DocumentDetailsPage documentId="doc-1" onBack={onBack} />)

    fireEvent.click(screen.getByRole('link', { name: 'Documents' }))

    expect(onBack).toHaveBeenCalled()
  })

  it('shows a quiet loading state while the document has not arrived yet', () => {
    mockedUseDocument.mockReturnValue(makeDocumentMock({ loading: true, document: null }))

    render(<DocumentDetailsPage documentId="doc-1" onBack={vi.fn()} />)

    expect(screen.getByText(/loading/i)).toBeInTheDocument()
  })

  it('shows the server\'s error message on a failed fetch', () => {
    mockedUseDocument.mockReturnValue(makeDocumentMock({ loading: false, document: null, error: 'document not found' }))

    render(<DocumentDetailsPage documentId="doc-1" onBack={vi.fn()} />)

    expect(screen.getByRole('alert')).toHaveTextContent('document not found')
  })

  it('shows a not-found message when the fetch resolved to nothing', () => {
    mockedUseDocument.mockReturnValue(makeDocumentMock({ loading: false, document: null, error: null }))

    render(<DocumentDetailsPage documentId="doc-1" onBack={vi.fn()} />)

    expect(screen.getByText('This document could not be found.')).toBeInTheDocument()
  })

  // ADR 0115 §2 review finding: App.tsx's dirty-navigation guard (already
  // built for Settings) generalizes to cover this page too, reported the
  // same way SettingsPage reports settingsDirty.
  describe('onDirtyChange', () => {
    it('reports false while clean, true once the draft diverges, and false again after Discard', () => {
      const onDirtyChange = vi.fn()
      mockedUseDocument.mockReturnValue(makeDocumentMock({ document: doc({ title: 'Impeller kit' }) }))

      render(<DocumentDetailsPage documentId="doc-1" onBack={vi.fn()} onDirtyChange={onDirtyChange} />)

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

      render(<DocumentDetailsPage documentId="doc-1" onBack={vi.fn()} onDirtyChange={onDirtyChange} />)

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

      render(<DocumentDetailsPage ref={ref} documentId="doc-1" onBack={vi.fn()} />)
      fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'Impeller kit v2' } })

      await expect(ref.current!.save()).rejects.toThrow('title already used')
      expect(patch).toHaveBeenCalledWith({ title: 'Impeller kit v2', notes: '', tags: [] })
    })

    it('saves the current draft on success, the same fields the on-page Save button sends', async () => {
      const patch = vi.fn().mockResolvedValue(doc({ title: 'Impeller kit v2' }))
      mockedUseDocument.mockReturnValue(makeDocumentMock({ document: doc({ title: 'Impeller kit' }), patch }))
      const ref = createRef<DocumentDetailsPageHandle>()

      render(<DocumentDetailsPage ref={ref} documentId="doc-1" onBack={vi.fn()} />)
      fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'Impeller kit v2' } })

      await ref.current!.save()

      expect(patch).toHaveBeenCalledWith({ title: 'Impeller kit v2', notes: '', tags: [] })
    })
  })
})
