import {
  CookingPot,
  Gauge,
  ListChecks,
  StickyNote,
  TriangleAlert,
  User,
  type LucideIcon,
} from 'lucide-react'

import type { NoteType } from '@/hooks/use-notes'

// Revision "one panel, not three" (2026-09-20): moved out of notes-panel.tsx
// so it survives that panel's deletion - Documents now renders the type
// facet (documents-panel.tsx's filter row) and the pre-attentive icon column
// (both there and in the moved manual tree rows, components/documents/
// manual-folder-view.tsx) that this table used to serve alone.
//
// The pre-attentive type table (plan §7, ADR 0116). Order matches the plan's
// own table (and classifyNoteType's ordering traps: a procedure containing a
// phone number is still a procedure, so procedure is deliberately not first
// just because contact happens to be listed first here - the DROPDOWN/facet
// order below is a UI convenience, not a classifier precedence statement,
// which lives entirely in notes_classify.go on the backend).
//
// Every colour here is a semantic token except one: quirk's amber is a raw
// palette colour, and that is deliberate, not an oversight - AGENTS.md
// reserves raw palette colours for alert semantics, and "this will bite
// you" is exactly that, the same amber tempClass() already uses for an
// out-of-range reading. Every other row uses text-primary/text-gauge-*/
// text-muted-foreground so it repaints correctly across themes with no
// dark: overrides needed.
export const NOTE_TYPE_META: Record<NoteType, { label: string; icon: LucideIcon; className: string }> = {
  contact: { label: 'Contact', icon: User, className: 'text-primary' },
  procedure: { label: 'Procedure', icon: ListChecks, className: 'text-gauge-secondary' },
  spec: { label: 'Spec', icon: Gauge, className: 'text-gauge-primary' },
  quirk: { label: 'Quirk', icon: TriangleAlert, className: 'text-amber-600 dark:text-amber-400' },
  recipe: { label: 'Recipe', icon: CookingPot, className: 'text-muted-foreground' },
  note: { label: 'Note', icon: StickyNote, className: 'text-muted-foreground' },
}

// Every type the dropdown/facet offers, in the plan's own table order - the
// same six values classifyNoteType (backend) and validNoteType can ever
// produce.
export const NOTE_TYPE_ORDER: NoteType[] = ['contact', 'procedure', 'spec', 'quirk', 'recipe', 'note']

// A row's note_type is a plain string off the wire (documentJSON's own
// unconditional field, use-documents.ts's DocumentRecord.note_type), not the
// narrowed NoteType union - defensively falls back to the generic "note"
// icon for anything this table doesn't recognise (an empty string on a
// kind='file' row, or a future type this build doesn't know about yet)
// rather than throwing mid-render. This is a display default, not a masked
// error: nothing here silences a failure, it just has to draw SOME icon for
// a row that exists.
export function noteTypeMeta(noteType: string) {
  return NOTE_TYPE_META[noteType as NoteType] ?? NOTE_TYPE_META.note
}
