// Plan "Notes and the Boat's Manual" §7 (Phase 2b) / ADR 0117: the Plate
// plugin set and Markdown serialiser configuration for the note editor,
// pulled into their own module so the golden-file round-trip corpus
// (frontend/src/test/note-editor-markdown.test.ts) can exercise the exact
// same headless editor the real, React-mounted editor uses - one
// configuration, never two that could drift apart.
//
// Framework-light but not framework-free: this file imports `platejs` and
// every `@platejs/*` node package, which is a large dependency graph (Slate
// plus Plate - ADR 0117, Risk 7). It must only ever be imported from
// note-editor-impl.tsx (itself reached only through note-editor.tsx's
// React.lazy()) and from test files, which are not part of the production
// bundle graph at all. Importing this from anywhere eagerly loaded would
// pull the whole editor into the kiosk's startup chunk -
// scripts/check-entry-chunk.mjs's editor-vendor assertion is the mechanical
// backstop for that.

import { createSlateEditor, KEYS, type SlateEditor, type Value } from 'platejs'
import { deserializeMd, serializeMd } from '@platejs/markdown'
import {
  BaseBlockquotePlugin,
  BaseBoldPlugin,
  BaseCodePlugin,
  BaseH1Plugin,
  BaseH2Plugin,
  BaseH3Plugin,
  BaseHorizontalRulePlugin,
  BaseItalicPlugin,
} from '@platejs/basic-nodes'
import { BaseCodeBlockPlugin, BaseCodeLinePlugin } from '@platejs/code-block'
import { BaseLinkPlugin } from '@platejs/link'
import { BaseListPlugin } from '@platejs/list'
import { BaseImagePlugin } from '@platejs/media'
import { BaseTableCellHeaderPlugin, BaseTableCellPlugin, BaseTablePlugin, BaseTableRowPlugin } from '@platejs/table'
import remarkGfm from 'remark-gfm'

// The enabled node set (plan §7 / this task's "What to build" #2): headings
// h1-h3, bold, italic, inline code, bulleted/numbered/task lists (all three
// via ONE plugin - see the comment on NOTE_EDITOR_PLUGINS below), tables,
// links, images, blockquote, horizontal rule, code block. Nothing else is
// registered, which IS the enforcement: an unregistered mdast node type
// deserialises as plain text (or is dropped) rather than round-tripping, so
// there is no separate "disallow" list to keep in sync - the plugin list
// below and the toolbar's own button set (note-editor-impl.tsx) are the
// whole contract. Deliberately absent: footnotes, reference-style links,
// raw HTML, mentions, comments - every one of them is something Plate can
// represent but Markdown cannot carry losslessly (or, for raw HTML,
// something this project's threat model already excludes - ADR 0116's
// `disallowedElements={['img']}` reasoning extends to "no raw HTML at all"
// for the identical reason).
//
// @platejs/list's ListPlugin is the "indent list" model, not the classic
// nested ul/li tree: a bulleted/numbered/task list item is an ordinary `p`
// node carrying `indent` (1-based nesting depth) and `listStyleType`
// ('disc' | 'decimal' | 'todo', KEYS.ul/KEYS.ol/KEYS.listTodo), plus
// `checked` for a todo item. @platejs/markdown's serialiser and deserialiser
// both understand this shape natively (verified empirically against the
// golden-file corpus - see note-editor-markdown.test.ts), so ONE plugin
// covers bulleted, numbered AND task lists; there is no separate
// "TaskListPlugin" to register.
export const NOTE_EDITOR_PLUGINS = [
  BaseH1Plugin,
  BaseH2Plugin,
  BaseH3Plugin,
  BaseBoldPlugin,
  BaseItalicPlugin,
  BaseCodePlugin,
  BaseBlockquotePlugin,
  BaseHorizontalRulePlugin,
  BaseListPlugin,
  BaseTablePlugin,
  BaseTableRowPlugin,
  BaseTableCellPlugin,
  BaseTableCellHeaderPlugin,
  BaseLinkPlugin,
  BaseImagePlugin,
  BaseCodeBlockPlugin,
  BaseCodeLinePlugin,
] as const

// Re-exported so callers (the toolbar, the todo-list element renderer) name
// list styles the same way this module and @platejs/markdown do, rather
// than repeating the magic strings 'disc'/'decimal'/'todo'.
export const LIST_STYLE_TYPE = {
  bulleted: KEYS.ul,
  numbered: KEYS.ol,
  task: KEYS.listTodo,
} as const

// remark-gfm is what makes GFM task lists (`- [ ]`) and tables parse and
// serialise at all - the base CommonMark grammar has neither. `tablePipeAlign:
// false` is deliberate (plan Risk 6 / ADR 0117): padded table columns reflow
// the ENTIRE table whenever one cell's width changes, turning a one-word
// edit into a whole-table diff - a new blob, a reindex and an embedding
// charge for a change that altered one cell. Both serialise and deserialise
// need this plugin registered (the deserialiser needs it to recognise GFM
// syntax at all; the serialiser needs it so round-tripping a table or task
// list doesn't silently fall back to plain CommonMark output).
export const NOTE_MARKDOWN_REMARK_PLUGINS = [[remarkGfm, { tablePipeAlign: false }]] as const

// remark-stringify settings, proven by the golden-file corpus. Defaults
// normalise `*off*` to `_off_`, `- item` to `* item` and `---` to `***`,
// each of which rewrites a note that did not change in meaning - a new
// blob, a reindex and an embedding charge (plan Risk 6). Measured against
// the same 13-fixture corpus this module ships with: 6 of 13 fixtures
// failed byte-identity with @platejs/markdown's own defaults (emphasis,
// bullets, task-list, nested-list, horizontal-rule, and the empty table
// cell - the last of those is a separate, worse defect, see
// `preserveEmptyParagraphs` below). All 13 pass with this configuration.
export const REMARK_STRINGIFY_OPTIONS = {
  bullet: '-', // GFM-canonical, and `- [ ]` is what the Phase 4
  // checklist runner parses. Load-bearing, not cosmetic.
  emphasis: '*',
  strong: '*',
  rule: '-',
  fences: true,
  listItemIndent: 'one',
} as const

/** Builds a headless Slate editor carrying exactly the note editor's node
 * set - no DOM, no React - shared by the real (React-mounted) editor's
 * `usePlateEditor` call and by every round-trip test in this file's test
 * suite, so both are provably running the same plugin configuration. */
export function createNoteSlateEditor(value?: Value): SlateEditor {
  return createSlateEditor({
    plugins: [...NOTE_EDITOR_PLUGINS],
    value,
  })
}

/** Markdown -> Plate value. Fails loudly (throws) rather than returning an
 * empty document on malformed input - AGENTS.md's fallback policy: a note
 * body that fails to parse must surface as an error, not render as a blank
 * editor that looks like an empty note. */
export function deserializeNoteMarkdown(editor: SlateEditor, markdown: string): Value {
  return deserializeMd(editor, markdown, {
    remarkPlugins: NOTE_MARKDOWN_REMARK_PLUGINS as unknown as never,
  })
}

/** Plate value -> Markdown. See REMARK_STRINGIFY_OPTIONS and
 * `preserveEmptyParagraphs` above for why each option is set - this is the
 * one place both are applied, so nothing calls @platejs/markdown's
 * `serializeMd` with any other configuration for a note body. */
export function serializeNoteMarkdown(editor: SlateEditor, value: Value): string {
  return serializeMd(editor, {
    value,
    remarkPlugins: NOTE_MARKDOWN_REMARK_PLUGINS as unknown as never,
    remarkStringifyOptions: REMARK_STRINGIFY_OPTIONS as never,
    // Without this, Plate emits a ZERO WIDTH SPACE (U+200B) into every empty
    // table cell on serialise. That is content corruption, not formatting:
    // the bytes are the note's identity, and an invisible character would
    // reach FTS, the embedding and the next owner's text editor.
    preserveEmptyParagraphs: false,
  })
}
