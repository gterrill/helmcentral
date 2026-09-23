import type { SlateEditor } from 'platejs'

// ADR 0124: the note editor's dictation insertion sink. A pure function
// over `editor.tf`/`editor.api`, not a React hook or a component, so it can
// be exercised headlessly against `createNoteSlateEditor()` the same way
// note-editor-markdown.test.ts already does for the serialiser - see this
// module's own test suite, note-editor-dictation.test.ts.
//
// Only note-editor-impl.tsx imports this. It types against `platejs`'s
// SlateEditor, which is part of the Plate/Slate dependency graph
// note-editor.tsx's React.lazy() boundary exists to keep out of the entry
// chunk (ADR 0117's editor-vendor chunk) - importing it from anywhere else
// eagerly loaded would pull that graph in too, exactly the mistake
// scripts/check-entry-chunk.mjs's editor-vendor assertion catches.

/**
 * The single character immediately before `point`, but never reaching past
 * the start of `point`'s own LOWEST containing block - a point at the very
 * start of some block that isn't the note's first block still reports ""
 * here, not a character borrowed off the end of the previous block.
 * "Lowest", not "top-level" (`[point.path[0]]`, this function's original
 * shape): a table is itself one top-level block, so that lookup treated an
 * entire table as "the block" and read the empty start of a SECOND cell as
 * mid-table, borrowing the previous cell's last character across the cell
 * boundary. `editor.api.block({ at: point })` returns the innermost block
 * actually containing `point` - a table cell's own paragraph where nested,
 * an ordinary top-level paragraph otherwise - which is what "THIS line" has
 * to mean for the leading-space rule this feeds (see insertDictatedText
 * below) to hold: "was there already a word directly behind the caret in
 * THIS line", not "is the document (or the table) non-empty somewhere
 * above".
 */
function charBeforeInBlock(editor: SlateEditor, point: { path: number[]; offset: number }): string {
  const entry = editor.api.block({ at: point })
  if (!entry) return ''
  const [, blockPath] = entry
  const blockStart = editor.api.start(blockPath)
  if (!blockStart) return ''
  if (editor.api.isStart(point, blockPath)) return ''
  const range = editor.api.range(blockStart, point)
  return editor.api.string(range).slice(-1)
}

/**
 * Inserts dictated `text` into `editor` at the current selection - the ADR
 * 0124 mic-in-the-editor decision. Trims and discards an empty (or
 * whitespace-only) final the same way `useDictation`'s `setValue` sink
 * already does for the plain-field case (dictation.tsx), so a stray silent
 * "final" never produces a no-op undo entry.
 *
 * Two rules come straight out of the ADR, and both are the actual
 * implementation risk it calls out:
 *
 * - **No selection.** The operator can press the mic having never clicked
 *   into the body - a genuinely untouched note has `editor.selection ===
 *   null`. Rather than throwing (Plate's insertText has nowhere to insert
 *   without one), this collapses the selection to the end of the document
 *   first, so the first dictated phrase lands in the body. `editor.tf.focus()`
 *   afterwards gives the mounted, DOM-backed editor real focus to match;
 *   verified empirically (see the test suite) to be a harmless no-op
 *   against the headless editor this module is also tested against, so it
 *   costs nothing to always call.
 * - **The leading space.** A single space is added when the caret sits
 *   directly after a non-whitespace character in the same block -
 *   "mid-sentence" - and omitted at the start of a block or an empty note,
 *   so repeated dictation never produces a note that reads " like this" or
 *   double-spaces between phrases.
 */
export function insertDictatedText(editor: SlateEditor, text: string): void {
  const trimmed = text.trim()
  if (trimmed === '') return

  if (!editor.selection) {
    const end = editor.api.end([])
    if (end) editor.tf.select(end)
    editor.tf.focus()
  }

  const point = editor.selection?.focus
  const before = point !== undefined ? charBeforeInBlock(editor, point) : ''
  const needsLeadingSpace = before !== '' && !/\s/.test(before)

  editor.tf.insertText(needsLeadingSpace ? ` ${trimmed}` : trimmed)
}
