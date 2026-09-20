// Plan "Notes and the Boat's Manual" §3: a checklist run keys each tick to
// the item's PLAIN TEXT, not its raw Markdown, so `- [ ] **Seacocks** open`
// and `- [ ] Seacocks open` are the same item. The plan's own reasoning
// applies twice over: the instruction didn't change, and "the WYSIWYG editor
// may re-emit emphasis differently from however it was typed" - which is
// exactly this feature. Without this, opening a checklist in the note editor
// and saving it (even with no meaningful edit) could silently invalidate
// every tick in a live run.
//
// This is the FRONTEND half of that contract, used by the editor's own
// round-trip test (note-editor.test.tsx's
// TestNoteEditor_ItemKeyIsStableAcrossAnEmphasisChange) to prove the editor
// doesn't do that. Phase 4 (backend/notes_checklist_store.go, not built in
// this cycle) computes the server-side key as a sha256 of the identical
// normalisation applied to the same source line; the two are independent
// implementations of the same stated rule rather than one importing the
// other; there is no Go/TS shared module in this codebase and both sides
// commit to the plan's own wording ("strip inline formatting, collapse
// internal whitespace, trim") as the spec they implement against.
//
// No lookbehind anywhere (AGENTS.md / check-entry-chunk.mjs): the kiosk's
// WPE WebKit throws a parse error on `(?<=`/`(?<!` and blanks the screen.

// A leading GFM task-list marker: `- [ ]`, `- [x]`, `* [X]`, at any (already
// trimmed-of-indent) start of line. Matches notes_format.go's own
// TestParseChecklistItems fixtures (`- [ ]`, `- [x]`, `* [ ]`) so the same
// line normalises the same way on both sides of the API, even though
// nothing here imports that Go code.
const LEADING_TASK_MARKER = /^[-*]\s*\[[ xX]\]\s*/

// The three inline-mark delimiter characters this editor's enabled node set
// can emit around a word: `**bold**`/`*italic*` (both use `*`, per
// note-editor-config.ts's REMARK_STRINGIFY_OPTIONS) and `` `code` ``.
// Stripping the characters outright (rather than a paired-delimiter regex)
// is deliberate and safe here: a checklist item is a single short line of
// plain prose, not general Markdown, so there is no legitimate use of a
// literal asterisk or backtick inside one that this would wrongly eat -
// and even if there were, the cost of getting that wrong is a spurious
// "re-check this" flag on the next open, not silent data loss.
const INLINE_MARK_CHARS = /[*_`]/g

/**
 * Normalises one checklist item's source line (with or without its leading
 * `- [ ]`/`- [x]` marker) to the plain text a run's tick should be keyed
 * against: markers and inline emphasis/code delimiters stripped, internal
 * whitespace collapsed to single spaces, and the result trimmed.
 */
export function normalizeChecklistItemText(line: string): string {
  const withoutMarker = line.replace(LEADING_TASK_MARKER, '')
  const withoutMarks = withoutMarker.replace(INLINE_MARK_CHARS, '')
  return withoutMarks.replace(/\s+/g, ' ').trim()
}
