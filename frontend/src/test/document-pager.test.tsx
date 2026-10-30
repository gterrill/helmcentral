import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, renderHook, screen, waitFor } from '@testing-library/react'

import { useDocumentPager } from '@/hooks/use-document-pager'

import { SaveBarSlot } from '@/components/patterns/save-bar-slot'
import { DocumentsRoutePanel } from '@/components/documents-route-panel'
import { resetQueryStore } from '@/lib/api-query'
import { testRouter } from './helpers/router'
import { stubApi } from './helpers/stub-api'

// The real Details page and the real hooks over a stubbed server: the arrows
// step through the list the page was opened from.

function doc(id: string, title: string, created_at: string, size_bytes = 100, extra: Record<string, unknown> = {}) {
  return {
    id, sha256: id, folder_id: 'f1', filename: `${id}.pdf`, title, notes: '', mime: 'application/pdf', size_bytes,
    page_count: 1, summary: '', status: 'indexed', stage: 'done', indexed_with: 'local', error: '', index_model: '',
    index_cost_usd: 0, tags: [], created_at, updated_at: created_at, indexed_at: created_at, kind: 'file',
    note_type: '', note_type_source: '', pinned: false, sort_index: 0, ...extra,
  }
}

const A = doc('a', 'Anchor manual', '2026-01-01T00:00:00Z', 300)
const B = doc('b', 'Bilge pump', '2026-02-01T00:00:00Z', 100)
const C = doc('c', 'Chart notes', '2026-03-01T00:00:00Z', 200)
const ALL = [A, B, C]

beforeEach(() => {
  localStorage.clear()
  stubApi({
    'GET /api/document-folders': { path: [{ id: 'f1', name: 'Manuals', parent_id: null }], folders: [], documents: ALL },
    'GET /api/document-folders/tree': { folders: [{ id: 'f1', name: 'Manuals', parent_id: null, is_manual: false, document_count: 3, subtree_document_count: 3 }] },
    'GET /api/documents': ({ url }: { url: string }) => {
      const tag = new URL(url, 'http://x').searchParams.get('tag')
      return { documents: tag === 'engine' ? [C, A] : [] }
    },
    // A manual reads in the order of its table of contents: C, then a folder holding A, then B.
    'GET /api/manuals/f1/tree': {
      type: 'folder', id: 'f1', name: 'Manuals', sort_index: 0, updated_at: '',
      children: [
        { type: 'document', id: 'c', name: 'Chart notes', sort_index: 0, updated_at: '' },
        { type: 'folder', id: 'sub', name: 'Engine', sort_index: 1, updated_at: '', children: [{ type: 'document', id: 'a', name: 'Anchor manual', sort_index: 0, updated_at: '' }] },
        { type: 'document', id: 'b', name: 'Bilge pump', sort_index: 2, updated_at: '' },
      ],
    },
    // A document filed in another folder, reached from a search over every folder.
    'GET /api/documents/z': doc('z', 'Zinc anode', '2026-05-01T00:00:00Z', 5, { folder_id: 'f2' }),
    'GET /api/document-folders?parent=f2': { path: [], folders: [], documents: [doc('y', 'Yard quote', '2026-06-01T00:00:00Z', 5, { folder_id: 'f2' }), doc('z', 'Zinc anode', '2026-05-01T00:00:00Z', 5, { folder_id: 'f2' })] },
    'GET /api/notes': { notes: [doc('n1', 'Gas locker note', '2026-04-01T00:00:00Z', 10, { kind: 'note', folder_id: null }), doc('n2', 'Another note', '2026-04-02T00:00:00Z', 10, { kind: 'note', folder_id: null })] },
    ...Object.fromEntries(ALL.map((d) => [`GET /api/documents/${d.id}`, d])),
    'GET /api/documents/n1': doc('n1', 'Gas locker note', '2026-04-01T00:00:00Z', 10, { kind: 'note', folder_id: null }),
  })
})
afterEach(() => { resetQueryStore() })

function mount(path: string) {
  const harness = testRouter(path)
  render(
    <div className="relative">
      <SaveBarSlot />
      <DocumentsRoutePanel
        initialDocumentId={null} onOpenHelp={() => {}} onOpenAssistantSettings={() => {}} onAskMate={() => {}} onDetailsTitleChange={() => {}}
      />
    </div>,
    { wrapper: harness.wrapper as never },
  )
  return harness
}

describe('the Details page arrows', () => {
  it('step through the folder newest first, the listing\'s default sort', async () => {
    const { history } = mount('/documents/b?folder=f1')
    // Chart notes (March), Bilge pump (February), Anchor manual (January).
    fireEvent.click(await screen.findByRole('button', { name: 'Previous: Chart notes' }))
    await waitFor(() => expect(history.path()).toBe('/documents/c?folder=f1'))
  })

  it('follow the sort this device remembers for the listing', async () => {
    localStorage.setItem('helmcentral.documents.sort', JSON.stringify([{ id: 'size', desc: false }]))
    const { history } = mount('/documents/b?folder=f1')
    // Bilge pump (100), Chart notes (200), Anchor manual (300).
    expect(await screen.findByRole('button', { name: 'Previous' })).toBeDisabled()
    fireEvent.click(screen.getByRole('button', { name: 'Next: Chart notes' }))
    await waitFor(() => expect(history.path()).toBe('/documents/c?folder=f1'))
  })

  it('stay inside the tag the listing was filtered by, and keep the tag when they step', async () => {
    const { history } = mount('/documents/a?folder=f1&tag=engine')
    // Filtered: Chart notes, Anchor manual. Bilge pump is not in it.
    fireEvent.click(await screen.findByRole('button', { name: 'Previous: Chart notes' }))
    await waitFor(() => expect(history.path()).toBe('/documents/c?folder=f1&tag=engine'))
    expect(screen.queryByRole('button', { name: /Bilge pump/ })).not.toBeInTheDocument()
  })

  it('step through the Unfiled notes in the order they are listed', async () => {
    const { history } = mount('/documents/n1?view=unfiled')
    fireEvent.click(await screen.findByRole('button', { name: 'Next: Another note' }))
    await waitFor(() => expect(history.path()).toBe('/documents/n2?view=unfiled'))
  })

  it('step through a manual in the order of its contents when the document was opened from one', async () => {
    const open = vi.fn()
    const { result } = renderHook(() => useDocumentPager('a', { folderId: 'f1', tag: null, unfiled: false, sectionId: 'a' }, open))
    await waitFor(() => expect(result.current.previous?.label).toBe('Chart notes'))
    expect(result.current.next?.label).toBe('Bilge pump')
    result.current.next?.onSelect()
    expect(open).toHaveBeenLastCalledWith('b')
  })

  it('step through the document\'s own folder when it is not in the list the page was opened from', async () => {
    // Opened from a search over every folder, with the root listing and no tag.
    const { result } = renderHook(() => useDocumentPager('z', { folderId: 'f1', tag: null, unfiled: false, sectionId: null }, vi.fn()))
    await waitFor(() => expect(result.current.previous?.label).toBe('Yard quote'))
    expect(result.current.next).toBeNull()
  })

  it('does the same when a tag filter excludes the document', async () => {
    const { result } = renderHook(() => useDocumentPager('b', { folderId: 'f1', tag: 'engine', unfiled: false, sectionId: null }, vi.fn()))
    // Bilge pump has no engine tag, so its own folder is stepped: newest first, Chart notes before it.
    await waitFor(() => expect(result.current.previous?.label).toBe('Chart notes'))
    expect(result.current.next?.label).toBe('Anchor manual')
  })

})
