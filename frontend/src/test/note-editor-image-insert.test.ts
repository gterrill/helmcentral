import { describe, expect, it } from 'vitest'

import { createNoteSlateEditor, deserializeNoteMarkdown } from '@/lib/note-editor-config'
import { describePhotoInsertFailure, insertUploadedPhotos } from '@/lib/note-editor-image-insert'

// Code-review finding: a photo already uploaded (it's a real Document
// either way) used to be silently orphaned from the note itself whenever
// the popover's saved selection no longer named a real spot by the time
// the upload resolved (insertNodes threw, the rejection went unhandled,
// and the popover just stopped saying "Uploading…" with no explanation).
// Tested headlessly against createNoteSlateEditor() - no DOM, no React,
// the same way note-editor-dictation.test.ts covers insertDictatedText.

function hasImageNode(children: unknown, id: string): boolean {
  return JSON.stringify(children).includes(`hc-doc:${id}`)
}

describe('insertUploadedPhotos', () => {
  it('inserts at the end of a genuinely untouched note (no selection at all)', () => {
    const editor = createNoteSlateEditor()
    expect(editor.selection).toBeNull()

    const result = insertUploadedPhotos(editor, [{ id: 'doc-1', name: 'a.jpg' }], '', null)

    expect(result).toEqual({ ok: true, movedToEnd: false })
    expect(hasImageNode(editor.children, 'doc-1')).toBe(true)
  })

  it('inserts at the saved selection when it still names a real spot', () => {
    const editor = createNoteSlateEditor()
    editor.tf.setValue(deserializeNoteMarkdown(editor, 'First\n\nSecond\n'))
    const saved = { anchor: { path: [1, 0], offset: 0 }, focus: { path: [1, 0], offset: 0 } }

    const result = insertUploadedPhotos(editor, [{ id: 'doc-1', name: 'a.jpg' }], 'Caption', saved)

    expect(result).toEqual({ ok: true, movedToEnd: false })
    expect(hasImageNode(editor.children, 'doc-1')).toBe(true)
  })

  // The actual bug: the saved selection was captured while the note had
  // two paragraphs, but the note changed underneath (a markdown-source
  // round-trip, in the real component) before the upload resolved - by the
  // time insertUploadedPhotos runs, that path no longer exists.
  it('falls back to the end of the note, and says so, when the saved spot no longer exists', () => {
    const editor = createNoteSlateEditor()
    editor.tf.setValue(deserializeNoteMarkdown(editor, 'First\n\nSecond\n'))
    const saved = { anchor: { path: [1, 0], offset: 0 }, focus: { path: [1, 0], offset: 0 } }
    expect(editor.api.hasPath(saved.anchor.path)).toBe(true)

    // The note shrinks to a single paragraph - path [1, 0] is now gone.
    editor.tf.setValue(deserializeNoteMarkdown(editor, 'Only one paragraph now\n'))
    expect(editor.api.hasPath(saved.anchor.path)).toBe(false)

    const result = insertUploadedPhotos(editor, [{ id: 'doc-1', name: 'a.jpg' }], '', saved)

    expect(result).toEqual({ ok: true, movedToEnd: true })
    expect(hasImageNode(editor.children, 'doc-1')).toBe(true)
  })

  it('inserts every photo in the pick, in one call', () => {
    const editor = createNoteSlateEditor()

    const result = insertUploadedPhotos(
      editor,
      [{ id: 'doc-1', name: 'a.jpg' }, { id: 'doc-2', name: 'b.jpg' }],
      '',
      null,
    )

    expect(result.ok).toBe(true)
    expect(hasImageNode(editor.children, 'doc-1')).toBe(true)
    expect(hasImageNode(editor.children, 'doc-2')).toBe(true)
  })

  it('does nothing for an empty pick', () => {
    const editor = createNoteSlateEditor()
    const before = editor.children

    const result = insertUploadedPhotos(editor, [], '', null)

    expect(result).toEqual({ ok: true, movedToEnd: false })
    expect(editor.children).toBe(before)
  })

  // The other half of the finding: insertNodes itself can still throw (a
  // malformed location, an editor in some other bad state) - this must
  // never become an unhandled rejection. The photo is already a real
  // Document (uploaded before this ever runs), so the caller is told
  // exactly that rather than left to guess whether it's lost.
  it('catches an insertNodes failure and names the photo, without throwing', () => {
    const editor = createNoteSlateEditor()
    editor.tf.insertNodes = () => {
      throw new Error('boom')
    }

    const result = insertUploadedPhotos(editor, [{ id: 'doc-1', name: 'IMG_0007.jpg' }], '', null)

    expect(result.ok).toBe(false)
    expect(result.movedToEnd).toBe(false)
    expect(result.error).toContain('IMG_0007.jpg')
    expect(result.error).toContain('boom')
  })
})

describe('describePhotoInsertFailure', () => {
  it('names a single photo by its own filename', () => {
    expect(describePhotoInsertFailure(['IMG_0007.jpg'], new Error('boom'))).toContain('IMG_0007.jpg')
  })

  it('names a count, not every filename, for several photos at once', () => {
    const message = describePhotoInsertFailure(['a.jpg', 'b.jpg'], new Error('boom'))
    expect(message).toContain('2 photos')
    expect(message).not.toContain('a.jpg')
  })

  it('carries the underlying reason', () => {
    expect(describePhotoInsertFailure(['a.jpg'], new Error('network down'))).toContain('network down')
  })
})
