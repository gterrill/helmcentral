import { describe, expect, it } from 'vitest'

import { createNoteSlateEditor, deserializeNoteMarkdown, serializeNoteMarkdown } from '@/lib/note-editor-config'
import { insertDictatedText } from '@/lib/note-editor-dictation'

// ADR 0124: the note editor's dictation insertion sink, tested headlessly
// against the same createNoteSlateEditor() note-editor-markdown.test.ts
// builds for the serialiser corpus - no DOM, no React, just the
// editor.tf/editor.api surface the mounted editor's mic (dictation.tsx's
// useDictation, wired through NoteEditorBody's insertDictation handle in
// note-editor-impl.tsx) calls through to.

describe('insertDictatedText', () => {
  it('lands in the body with no leading space when the note has no selection at all', () => {
    const editor = createNoteSlateEditor()
    expect(editor.selection).toBeNull()

    insertDictatedText(editor, 'fuel return is the inboard valve')

    expect(serializeNoteMarkdown(editor, editor.children)).toBe('fuel return is the inboard valve\n')
  })

  it('inserts at the caret with a leading space when the caret sits mid-sentence', () => {
    const editor = createNoteSlateEditor()
    editor.tf.setValue(deserializeNoteMarkdown(editor, 'Seacocks open\n'))
    // Right after "Seacocks", before the space that already separates it
    // from "open".
    editor.tf.select({ anchor: { path: [0, 0], offset: 8 }, focus: { path: [0, 0], offset: 8 } })

    insertDictatedText(editor, 'stiff')

    expect(serializeNoteMarkdown(editor, editor.children)).toBe('Seacocks stiff open\n')
  })

  it('inserts with no leading space when the caret sits at the start of a block', () => {
    const editor = createNoteSlateEditor()
    editor.tf.setValue(deserializeNoteMarkdown(editor, 'open valve\n'))
    editor.tf.select({ anchor: { path: [0, 0], offset: 0 }, focus: { path: [0, 0], offset: 0 } })

    insertDictatedText(editor, 'Seacocks')

    expect(serializeNoteMarkdown(editor, editor.children)).toBe('Seacocksopen valve\n')
  })

  it('does not add a second space when the caret already sits right after whitespace', () => {
    const editor = createNoteSlateEditor()
    editor.tf.setValue(deserializeNoteMarkdown(editor, 'Seacocks \n'))
    editor.tf.select({ anchor: { path: [0, 0], offset: 9 }, focus: { path: [0, 0], offset: 9 } })

    insertDictatedText(editor, 'open')

    expect(serializeNoteMarkdown(editor, editor.children)).toBe('Seacocks open\n')
  })

  it('inserts nothing for an empty final', () => {
    const editor = createNoteSlateEditor()

    insertDictatedText(editor, '')

    expect(editor.selection).toBeNull()
    expect(serializeNoteMarkdown(editor, editor.children)).toBe('')
  })

  it('inserts nothing for a whitespace-only final', () => {
    const editor = createNoteSlateEditor()
    editor.tf.setValue(deserializeNoteMarkdown(editor, 'Seacocks open\n'))

    insertDictatedText(editor, '   ')

    expect(serializeNoteMarkdown(editor, editor.children)).toBe('Seacocks open\n')
  })

  it('a second dictated final continues to append with a single space, mirroring the plain-field append rule', () => {
    const editor = createNoteSlateEditor()

    insertDictatedText(editor, 'fuel return is the inboard valve')
    insertDictatedText(editor, 'the one with the scratched handle')

    expect(serializeNoteMarkdown(editor, editor.children))
      .toBe('fuel return is the inboard valve the one with the scratched handle\n')
  })

  // A table is itself one top-level block (a `tr`/`td` tree underneath it),
  // so charBeforeInBlock's old `[point.path[0]]` lookup treated the whole
  // TABLE as "the block" - the start of an empty second cell read back
  // through the previous cell's own text and borrowed its last character
  // across the cell boundary. `editor.api.block({ at: point })` returns the
  // cell's own paragraph instead (verified empirically against a real
  // editor instance - see this file's own header comment), so this is
  // mid-table but start-of-cell, and must read exactly like the very first
  // cell in the note.
  it('inserts with no leading space at the start of an empty second table cell whose previous cell has text', () => {
    const editor = createNoteSlateEditor()
    editor.tf.setValue(deserializeNoteMarkdown(editor, '| Tank | Notes |\n| - | - |\n| Fuel port | |\n'))
    // The empty "Notes" cell of the data row, not the very start of the
    // table - "Fuel port" comes before it.
    editor.tf.select({ anchor: { path: [0, 1, 1, 0, 0], offset: 0 }, focus: { path: [0, 1, 1, 0, 0], offset: 0 } })

    insertDictatedText(editor, 'Gauge reads high')

    expect(serializeNoteMarkdown(editor, editor.children))
      .toBe('| Tank | Notes |\n| - | - |\n| Fuel port | Gauge reads high |\n')
  })

  it('still adds the leading space when the caret sits after text within a cell', () => {
    const editor = createNoteSlateEditor()
    editor.tf.setValue(deserializeNoteMarkdown(editor, '| Tank | Notes |\n| - | - |\n| Fuel port | |\n'))
    // End of "Fuel port", inside its own cell - mid-cell, same as the
    // existing "mid-sentence" case above, just nested in a table.
    editor.tf.select({ anchor: { path: [0, 1, 0, 0, 0], offset: 9 }, focus: { path: [0, 1, 0, 0, 0], offset: 9 } })

    insertDictatedText(editor, 'topped up')

    expect(serializeNoteMarkdown(editor, editor.children))
      .toBe('| Tank | Notes |\n| - | - |\n| Fuel port topped up | |\n')
  })
})
