import { readdirSync, readFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

import { normalizeChecklistItemText } from '@/lib/checklist-item-text'
import {
  createNoteSlateEditor,
  deserializeNoteMarkdown,
  serializeNoteMarkdown,
} from '@/lib/note-editor-config'

// Plan "Notes and the Boat's Manual" §7 (Phase 2b) / ADR 0117, Risk 6: the
// golden-file corpus. The bytes of a note ARE its identity (ADR 0116) - a
// WYSIWYG editor that reflows a note on every save turns an edit that didn't
// change meaning into a new blob, a re-index and an embedding charge. Every
// fixture here must parse -> Plate value -> serialise back to the EXACT
// same bytes it started with, under the one serialiser configuration this
// project ships (note-editor-config.ts's REMARK_STRINGIFY_OPTIONS plus
// `preserveEmptyParagraphs: false`).
//
// This is deliberately not a test of Plate/remark's defaults - it tests
// THIS project's configuration, because that's the thing that has to stay
// correct. Measured once, by hand, against @platejs/markdown's own
// defaults: 6 of these 13 fixtures failed byte-identity (bullet marker,
// emphasis marker, thematic-break style, and a ZERO WIDTH SPACE injected
// into the empty table cell). See ADR 0117 for the full measurement.
//
// Any fixture that does NOT round-trip is a scope question, not a bug to
// patch around (plan Risk 6: "when a node will not round-trip, remove the
// node, do not patch the serialiser") - if this test ever fails on a new
// fixture, the fix under discussion should almost always be "don't enable
// that node", not a new remark-stringify option.

const fixturesDir = resolve(dirname(fileURLToPath(import.meta.url)), 'fixtures/notes')
const fixtureNames = readdirSync(fixturesDir).filter((name) => name.endsWith('.md'))

describe('note editor Markdown round-trip (golden-file corpus)', () => {
  // Sanity check on the corpus itself, so a typo'd fixtures/ path (empty
  // directory, wrong dirname) fails loudly here instead of the loop below
  // silently reporting zero test cases as "all passing".
  it('found the 13-fixture corpus', () => {
    expect(fixtureNames.length).toBe(13)
  })

  it.each(fixtureNames)('%s round-trips byte-identical', (name) => {
    const editor = createNoteSlateEditor()
    const original = readFileSync(join(fixturesDir, name), 'utf8')

    const value = deserializeNoteMarkdown(editor, original)
    const serialized = serializeNoteMarkdown(editor, value)

    expect(serialized).toBe(original)
  })

  it('never emits a U+200B into an empty table cell', () => {
    const editor = createNoteSlateEditor()
    const original = readFileSync(join(fixturesDir, 'table.md'), 'utf8')

    const value = deserializeNoteMarkdown(editor, original)
    const serialized = serializeNoteMarkdown(editor, value)

    expect(serialized).not.toContain('​')
  })
})

// These two are named exactly as the plan names them (task instructions for
// Phase 2b). Both are fundamentally about the SERIALISER configuration this
// module owns rather than about React/DOM behaviour, so they live here,
// headless, alongside the fixture corpus, rather than in note-editor.test.tsx
// - no contentEditable, no jsdom selection quirks, just the same
// editor.tf/editor.api surface the real component calls.
describe('TestNoteEditor_TaskListSerialisesAsGFM', () => {
  it('serialises a task list with GFM checkbox syntax, because the Phase 4 checklist runner parses `- [ ]`', () => {
    const editor = createNoteSlateEditor()
    const original = readFileSync(join(fixturesDir, 'task-list.md'), 'utf8')

    const value = deserializeNoteMarkdown(editor, original)
    const serialized = serializeNoteMarkdown(editor, value)

    expect(serialized).toContain('- [ ] ')
    expect(serialized).toContain('- [x] ')
  })
})

describe('TestNoteEditor_ItemKeyIsStableAcrossAnEmphasisChange', () => {
  it("bolding a checklist item's whole text leaves its normalised plain text unchanged - the tick-safety contract (plan §3)", () => {
    const md = '- [ ] Seacocks open\n'
    const seedEditor = createNoteSlateEditor()
    const value = deserializeNoteMarkdown(seedEditor, md)
    const beforeNormalized = normalizeChecklistItemText(md.trim())

    // A second editor seeded with the deserialised value, so the transform
    // below (select + bold) runs against a real, mounted-shape document -
    // the same editor.tf/editor.api surface note-editor-impl.tsx's toolbar
    // calls, not a hand-built mock of it.
    const editor = createNoteSlateEditor(value)
    const range = editor.api.range([0]) // the whole (only) checklist item
    editor.tf.select(range)
    // `bold` is a dynamic key registered by BaseBoldPlugin - same cast as
    // note-editor-impl.tsx's toolbar uses for the identical reason (the
    // static SlateEditor['tf'] type has no way to know about a specific
    // plugin's own registered key).
    ;(editor.tf as unknown as Record<string, { toggle: () => void }>).bold.toggle()

    const afterMd = serializeNoteMarkdown(editor, editor.children)
    // Sanity check that the edit actually happened - otherwise the
    // assertion below would trivially pass for the wrong reason.
    expect(afterMd).toBe('- [ ] **Seacocks open**\n')

    expect(normalizeChecklistItemText(afterMd.trim())).toBe(beforeNormalized)
  })
})
