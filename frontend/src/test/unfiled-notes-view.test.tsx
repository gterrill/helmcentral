import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'

import { UnfiledNotesView } from '@/components/documents/unfiled-notes-view'
import type { NoteRecord } from '@/hooks/use-notes'

// Revision "one panel, not three" (2026-09-20): the saved view behind
// Documents' "Unfiled notes" filter (documents-panel.tsx) - the same row
// gesture the deleted notes-panel.tsx's own inbox list had, re-expressed
// as a standalone, presentational component that only ever receives
// already-fetched data + callbacks, so it needs no hook mocking at all.

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

describe('UnfiledNotesView', () => {
  it("shows the empty state when there's nothing unfiled", () => {
    render(<UnfiledNotesView notes={[]} loading={false} onOpen={vi.fn()} onSetType={vi.fn()} onFile={vi.fn()} />)
    expect(screen.getByText('Nothing captured yet.')).toBeInTheDocument()
  })

  it("renders each note's title, line-clamped, with a type icon button", () => {
    render(<UnfiledNotesView
      notes={[note({ id: 'note-1', title: 'Genset start-up', note_type: 'procedure' })]}
      loading={false}
      onOpen={vi.fn()}
      onSetType={vi.fn()}
      onFile={vi.fn()}
    />)

    expect(screen.getByText('Genset start-up')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /change type: currently procedure/i })).toBeInTheDocument()
  })

  it('tapping a title calls onOpen with that note id', () => {
    const onOpen = vi.fn()
    render(<UnfiledNotesView
      notes={[note({ id: 'note-1', title: 'Genset start-up' })]}
      loading={false}
      onOpen={onOpen}
      onSetType={vi.fn()}
      onFile={vi.fn()}
    />)

    fireEvent.click(screen.getByRole('button', { name: 'Genset start-up' }))
    expect(onOpen).toHaveBeenCalledWith('note-1')
  })

  it('tapping the type icon opens a six-item menu; picking one calls onSetType', () => {
    const onSetType = vi.fn()
    render(<UnfiledNotesView
      notes={[note({ id: 'note-1', note_type: 'procedure' })]}
      loading={false}
      onOpen={vi.fn()}
      onSetType={onSetType}
      onFile={vi.fn()}
    />)

    fireEvent.click(screen.getByRole('button', { name: /change type: currently procedure/i }))
    const menuItems = screen.getAllByRole('menuitem')
    expect(menuItems).toHaveLength(6)

    fireEvent.click(screen.getByRole('menuitem', { name: /contact/i }))
    expect(onSetType).toHaveBeenCalledWith('note-1', 'contact')
  })

  it('File… calls onFile with the note id and its display title', () => {
    const onFile = vi.fn()
    render(<UnfiledNotesView
      notes={[note({ id: 'note-1', title: 'Genset start-up' })]}
      loading={false}
      onOpen={vi.fn()}
      onSetType={vi.fn()}
      onFile={onFile}
    />)

    fireEvent.click(screen.getByRole('button', { name: 'File "Genset start-up"' }))
    expect(onFile).toHaveBeenCalledWith('note-1', 'Genset start-up')
  })
})
