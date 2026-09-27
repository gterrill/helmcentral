import { KEYS } from 'platejs'
import type { SlateEditor } from 'platejs'

// Code-review finding: the note editor's own Image button (note-editor-
// impl.tsx) hands off to the phone's camera/photo-library UI for Take
// photo/Add from library - an interruption long enough that the editor's
// selection at the moment the popover opened cannot be trusted to still
// name a real spot in the document by the time the upload resolves (a
// markdown-source round-trip, or any other edit made in the meantime, can
// shrink the tree out from under it). insertUploadedPhotos is the pure
// insertion logic this feeds, tested headlessly against
// createNoteSlateEditor() the same way note-editor-dictation.ts's
// insertDictatedText is - no DOM, no React, and no dependency on jsdom's
// own (poor) Slate selection support.
//
// Only note-editor-impl.tsx imports this - see note-editor-dictation.ts's
// own header comment for why this stays out of the editor-vendor chunk's
// eager import graph.

export interface UploadedPhoto {
  id: string
  /** The original filename (Take photo/Add from library) or the pasted
   * document id itself (the "paste a document id" path never had a
   * filename to begin with) - named in describePhotoInsertFailure below if
   * this specific photo's insertion fails. */
  name: string
}

export interface InsertUploadedPhotosResult {
  /** False only when insertNodes itself threw - every one of `photos` is
   * still a real Document either way (the upload already happened before
   * this ever runs); false just means none of them made it INTO this note. */
  ok: boolean
  /** True only when insertion succeeded but the saved selection no longer
   * named a real spot in the document, so the photos landed at the end of
   * the note instead - never true for a genuinely selection-less note
   * (nothing to have moved away from) or for a failed insertion. */
  movedToEnd: boolean
  /** role="alert" wording, set only when !ok. */
  error?: string
}

/** Where a fresh set of image nodes should land: `saved` (the popover's
 * own selection when it opened), if both its anchor and focus still name a
 * real spot in `editor`'s CURRENT document, or the end of the note
 * otherwise. `saved === null` (the popover opened against a genuinely
 * untouched note, same case insertDictatedText's own doc comment
 * describes) always resolves to the end, with movedToEnd left false. */
function resolveInsertionPoint(editor: SlateEditor, saved: SlateEditor['selection']) {
  const stillValid = saved !== null && editor.api.hasPath(saved.anchor.path) && editor.api.hasPath(saved.focus.path)
  if (stillValid) return { at: saved as NonNullable<SlateEditor['selection']>, movedToEnd: false }
  return { at: editor.api.end([]), movedToEnd: saved !== null }
}

/**
 * Inserts one image node per photo, in order, at `saved`'s own spot if it
 * survived, or the end of the note otherwise (resolveInsertionPoint above) -
 * a single insertNodes call, never one per photo, so a multi-photo pick
 * lands as sibling nodes in pick order without threading selection state
 * between calls by hand.
 *
 * Every photo named in `photos` is already a real Document by the time
 * this runs (the upload itself always happens first, in the caller) - an
 * insertNodes failure here never means a photo was lost, only that it
 * isn't in THIS note, which is exactly what the caught error says rather
 * than an unhandled rejection that leaves the popover simply frozen on
 * "Uploading…" with no explanation.
 */
export function insertUploadedPhotos(
  editor: SlateEditor,
  photos: UploadedPhoto[],
  caption: string,
  saved: SlateEditor['selection'],
): InsertUploadedPhotosResult {
  if (photos.length === 0) return { ok: true, movedToEnd: false }

  const { at, movedToEnd } = resolveInsertionPoint(editor, saved)
  const trimmedCaption = caption.trim()
  try {
    editor.tf.insertNodes(
      photos.map(({ id }) => ({
        type: KEYS.img,
        url: `hc-doc:${id}`,
        caption: trimmedCaption === '' ? [] : [{ text: trimmedCaption }],
        children: [{ text: '' }],
      })),
      { at, select: true },
    )
  } catch (err) {
    return { ok: false, movedToEnd: false, error: describePhotoInsertFailure(photos.map((p) => p.name), err) }
  }
  editor.tf.focus()
  return { ok: true, movedToEnd }
}

/** role="alert" wording for a photo (or photos) that uploaded successfully
 * but could not be placed into the note - names it/them, so the operator
 * never has to wonder whether the upload itself was lost (it wasn't: it's
 * a Document, just not linked into this note's body). */
export function describePhotoInsertFailure(names: string[], err: unknown): string {
  const label = names.length === 1 ? names[0] : `${names.length} photos`
  const reason = err instanceof Error ? err.message : String(err)
  return `${label} uploaded, but could not be placed in the note here: ${reason}. Nothing was lost - find it in Documents.`
}
