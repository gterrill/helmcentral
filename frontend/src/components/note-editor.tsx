import { lazy, Suspense } from 'react'

// Same kiosk bundle-split reasoning as note-markdown.tsx / help-markdown.tsx
// (commit 70acb53), turned up to eleven: note-editor-impl.tsx pulls in
// `platejs` and every `@platejs/*` node package - Slate plus Plate, ADR
// 0117's Risk 7, by far the largest dependency graph this project has ever
// added behind a lazy boundary. The wall-display kiosk never opens a note
// editor at all (Anchor Watch, the dashboard tiles and the kiosk route never
// mount NoteEditor), so a lazy import here keeps that whole graph out of
// every chunk the kiosk loads eagerly - scripts/check-entry-chunk.mjs's
// editor-vendor assertion is the mechanical guard that keeps it that way.
const NoteEditorImpl = lazy(() => import('./note-editor-impl'))

export interface NoteEditorProps {
  /** The note's current Markdown body - the ONLY source of truth on mount.
   * Re-deserialised into the editor whenever this changes identity (the
   * caller opening a different note), never diffed against the editor's
   * own live value - see note-editor-impl.tsx's own comment on why opening
   * a note must never write. */
  value: string
  /** Called with the serialised Markdown body when the operator presses
   * Save. Never called automatically - not on mount, not on an
   * unrelated re-render, not on unmount (plan §7: "Opening a note must
   * never write"; this task's own #5: "Dirty state comes from user edits
   * only, never from a serialisation diff. Mount, unmount, no save."). */
  onSave: (markdown: string) => void | Promise<void>
  /** True while a save request from a PREVIOUS click is still in flight -
   * disables the Save button and shows a busy state rather than letting a
   * second click queue a second write. */
  saving?: boolean
}

export function NoteEditor(props: NoteEditorProps) {
  return (
    <Suspense fallback={<div className="min-w-0 text-sm leading-relaxed text-muted-foreground">Loading editor…</div>}>
      <NoteEditorImpl {...props} />
    </Suspense>
  )
}
