import { forwardRef, lazy, Suspense } from 'react'

import type { NoteEditorBodyProps, NoteEditorHandle } from './note-editor-impl'

// Same kiosk bundle-split reasoning as note-markdown.tsx / help-markdown.tsx
// (commit 70acb53), turned up to eleven: note-editor-impl.tsx pulls in
// `platejs` and every `@platejs/*` node package - Slate plus Plate, ADR
// 0117's Risk 7, by far the largest dependency graph this project has ever
// added behind a lazy boundary. The wall-display kiosk never opens a note
// editor at all (Anchor Watch, the dashboard tiles and the kiosk route never
// mount NoteEditor), so a lazy import here keeps that whole graph out of
// every chunk the kiosk loads eagerly - scripts/check-entry-chunk.mjs's
// editor-vendor assertion is the mechanical guard that keeps it that way.
//
// ADR 0124: ONE loader function, shared by both the `NoteEditorImpl` lazy
// boundary below and `prefetchNoteEditor()` - `import()` against the same
// specifier always resolves to the same module instance/promise, so calling
// this from two places doesn't risk a second fetch, but sharing the actual
// function (rather than two textually-identical `import('./note-editor-
// impl')` call sites) is what makes that guarantee obvious on inspection
// rather than something a reader has to trust the bundler on.
const loadNoteEditorImpl = () => import('./note-editor-impl')

const NoteEditorImpl = lazy(loadNoteEditorImpl)

// The type-only imports above cost nothing at runtime (erased at compile
// time - see this file's own header comment on why nothing else about
// note-editor-impl.tsx may be imported eagerly) but let this module hand
// out NoteEditorBodyProps/NoteEditorHandle without ever pulling Plate in to
// do it.
let noteEditorPrefetch: Promise<unknown> | null = null

/**
 * Warms the editor-vendor chunk before anything actually needs it (ADR
 * 0124: "the chunk is prefetched, not awaited"). Memoised - the first call
 * kicks off both fetches and every call after that (a second hover, the
 * idle callback firing after a hover already started it, App.tsx's own
 * idle-triggered call racing a keen operator) is a no-op against the same
 * in-flight promise, never a second network request.
 *
 * Fetches note-capture-sheet.tsx alongside note-editor-impl.tsx: the sheet
 * is itself behind its own React.lazy() boundary (documents-panel.tsx) for
 * an unrelated reason (nothing to do before New → Note is ever picked), but
 * from the operator's chair the two chunks arriving together is what "the
 * editor is ready" actually means - prefetching one without the other would
 * still show the sheet's own Suspense fallback while the second one
 * finishes.
 */
export function prefetchNoteEditor(): void {
  if (noteEditorPrefetch) return
  noteEditorPrefetch = Promise.all([
    loadNoteEditorImpl(),
    import('./documents/note-capture-sheet'),
  ])
}

const EDITOR_LOADING_FALLBACK = (
  <div className="min-w-0 text-sm leading-relaxed text-muted-foreground">Loading editor…</div>
)

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
    <Suspense fallback={EDITOR_LOADING_FALLBACK}>
      <NoteEditorImpl {...props} />
    </Suspense>
  )
}

// ADR 0124: the body-only editor, lazy over the same loader (and so the
// same module cache) as NoteEditor above. This is what note-capture-sheet.tsx
// embeds - no Save button, no dirty line, the caret can land in it on mount.
// The Suspense boundary lives HERE, inside this wrapper, rather than being
// left for every caller to add for itself - a cold open (the chunk hasn't
// finished prefetching yet) falls back to the editor's own "Loading editor…"
// line, never a plain textarea standing in for it: ADR 0124 is explicit that
// there is no plain-field fallback path, so the one thing this boundary may
// never show in its place is a second, different authoring surface.
const LazyNoteEditorBodyImpl = lazy(() =>
  loadNoteEditorImpl().then((mod) => ({ default: mod.NoteEditorBody })),
)

export const NoteEditorBody = forwardRef<NoteEditorHandle, NoteEditorBodyProps>(function NoteEditorBody(props, ref) {
  return (
    <Suspense fallback={EDITOR_LOADING_FALLBACK}>
      <LazyNoteEditorBodyImpl {...props} ref={ref} />
    </Suspense>
  )
})

export type { NoteEditorBodyProps, NoteEditorHandle }
