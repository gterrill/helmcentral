import { FolderInput } from 'lucide-react'

import type { NoteRecord, NoteType } from '@/hooks/use-notes'

import { NoteTypeIconButton } from './note-type-icon-button'

// Revision "one panel, not three" (2026-09-20): the saved view behind
// Documents' "Unfiled notes" filter chip (documents-panel.tsx) -
// kind='note' AND folder_id IS NULL (GET /api/notes?filed=0), the same
// inbox notes-panel.tsx used to own outright. It is now a filter over the
// SAME listing surface, not a place of its own - see the panel's own
// comment on the `view` state for how it is entered and left.
//
// Row shape (title, type icon, File…) is unchanged from the deleted
// notes-panel.tsx's NoteRow - this is the same gesture, re-homed. File…
// reuses Documents' own Move dialog (onFile below just opens it with this
// note's id), per the revision's own "Documents' existing Move is the same
// gesture" - there is deliberately no second, note-specific filing picker
// with its own "position in a manual" step here; Arrange (manual-folder-
// view.tsx) is how a freshly filed note gets positioned afterwards.
function noteDisplayTitle(note: NoteRecord): string {
  return note.title.trim() !== '' ? note.title : note.filename
}

export interface UnfiledNotesViewProps {
  notes: NoteRecord[]
  loading: boolean
  onOpen: (id: string) => void
  onSetType: (id: string, type: NoteType) => void
  onFile: (id: string, title: string) => void
}

export function UnfiledNotesView({ notes, loading, onOpen, onSetType, onFile }: UnfiledNotesViewProps) {
  if (loading && notes.length === 0) {
    return <p className="p-4 text-sm text-muted-foreground">Loading…</p>
  }
  if (notes.length === 0) {
    return <p className="p-4 text-sm text-muted-foreground">Nothing captured yet.</p>
  }
  return (
    <ul className="divide-y divide-border">
      {notes.map((note) => (
        <UnfiledNoteRow key={note.id} note={note} onOpen={onOpen} onSetType={onSetType} onFile={onFile} />
      ))}
    </ul>
  )
}

function UnfiledNoteRow({
  note,
  onOpen,
  onSetType,
  onFile,
}: {
  note: NoteRecord
  onOpen: (id: string) => void
  onSetType: (id: string, type: NoteType) => void
  onFile: (id: string, title: string) => void
}) {
  const title = noteDisplayTitle(note)

  return (
    <li className="flex min-w-0 items-stretch">
      {/* The type icon is its own hit target - tap it to override in one
          tap, independent of opening the note. Two separate interactive
          elements rather than a menu trigger nested inside a bigger
          clickable row: nesting a button inside a button is invalid HTML
          and makes the outer row's click target ambiguous. `lg`: this is
          the mobile-first drain-the-inbox row, so its hit target stays the
          full 44px the deleted notes-panel.tsx's own inbox row used. */}
      <NoteTypeIconButton noteType={note.note_type} onSetType={(type) => onSetType(note.id, type)} size="lg" />
      <button
        type="button"
        onClick={() => onOpen(note.id)}
        className="flex min-h-11 min-w-0 flex-1 items-center px-2 py-2 text-left hover:bg-accent"
      >
        <span className="line-clamp-1 min-w-0 text-sm font-medium text-foreground">{title}</span>
      </button>
      {/* File…: the drain-the-inbox gesture. Its own hit target, same
          reasoning as the type icon above. */}
      <button
        type="button"
        onClick={() => onFile(note.id, title)}
        aria-label={`File "${title}"`}
        title="File…"
        className="flex h-11 w-11 shrink-0 items-center justify-center text-muted-foreground hover:bg-accent hover:text-foreground"
      >
        <FolderInput className="h-4 w-4" aria-hidden="true" />
      </button>
    </li>
  )
}
