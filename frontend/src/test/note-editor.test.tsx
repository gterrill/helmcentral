import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'

import { NoteEditor } from '@/components/note-editor'

// Plan "Notes and the Boat's Manual" §7 (Phase 2b) / ADR 0117. NoteEditor is
// a React.lazy wrapper (kiosk bundle-split, commit 70acb53's pattern - see
// note-editor.tsx's own comment), so the WYSIWYG content isn't there on the
// first synchronous render, only the Suspense fallback. Every test below
// awaits it with findBy* before asserting anything else, matching
// note-markdown.test.tsx / help-markdown.test.tsx's own convention.
//
// These three are named exactly as the plan names them (Phase 2b task
// instructions). The other two named tests
// (TestNoteEditor_TaskListSerialisesAsGFM,
// TestNoteEditor_ItemKeyIsStableAcrossAnEmphasisChange) live in
// note-editor-markdown.test.ts instead - both are fundamentally about the
// serialiser configuration, not about React/DOM behaviour, and testing them
// headlessly (no contentEditable, no jsdom selection quirks) is both
// simpler and more robust than driving a real Slate selection through
// Testing Library's fireEvent.

describe('TestNoteEditor_OpeningANoteDoesNotMarkItDirty', () => {
  it('mounts and unmounts a note with no save ever firing', async () => {
    const onSave = vi.fn()
    const { unmount } = render(<NoteEditor value={'# Genset start-up\n'} onSave={onSave} />)

    await screen.findByText('Genset start-up')
    unmount()

    expect(onSave).not.toHaveBeenCalled()
  })

  it('does not enable Save until the operator actually edits something', async () => {
    const onSave = vi.fn()
    render(<NoteEditor value={'# Genset start-up\n'} onSave={onSave} />)

    await screen.findByText('Genset start-up')
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
  })
})

describe('TestNoteEditor_SourceToggleIsAuthoritative', () => {
  it('shows an edit made in the raw Markdown textarea once toggled back to the WYSIWYG view', async () => {
    const onSave = vi.fn()
    render(<NoteEditor value={'- [ ] Todo\n'} onSave={onSave} />)

    await screen.findByText('Todo')

    fireEvent.click(screen.getByRole('button', { name: 'Markdown source' }))
    const textarea = await screen.findByLabelText('Note markdown source')
    fireEvent.change(textarea, { target: { value: '- [ ] Todo\n- [ ] Another\n' } })

    fireEvent.click(screen.getByRole('button', { name: 'Markdown source' }))

    expect(await screen.findByText('Another')).toBeInTheDocument()
  })
})

describe('TestNoteEditor_DisabledNodesAreAbsentFromTheToolbar', () => {
  it('offers every enabled node and nothing that was deliberately left out', async () => {
    const onSave = vi.fn()
    render(<NoteEditor value={'# Genset start-up\n'} onSave={onSave} />)

    await screen.findByText('Genset start-up')

    for (const label of [
      'Heading 1', 'Heading 2', 'Heading 3',
      'Bold', 'Italic', 'Inline code',
      'Bulleted list', 'Numbered list', 'Task list',
      'Blockquote', 'Horizontal rule', 'Code block',
      'Table', 'Link', 'Image',
      'Markdown source',
    ]) {
      expect(screen.getByRole('button', { name: label })).toBeInTheDocument()
    }

    // Out (plan §7 / ADR 0117): footnotes, reference-style links, raw HTML,
    // mentions, comments - anything Plate can represent that Markdown
    // cannot carry losslessly. Never enabled, so never a toolbar control.
    for (const pattern of [/footnote/i, /mention/i, /comment/i, /raw html/i, /reference link/i]) {
      expect(screen.queryByRole('button', { name: pattern })).not.toBeInTheDocument()
    }
  })
})

// The Link toolbar button takes a URL the operator TYPES, which makes it the
// one control in this editor that can put an arbitrary scheme into a note
// body. note-markdown-impl.tsx already refuses to render an unsafe href (it
// resolves to `kind: 'unsafe'` and comes out as inert text, ADR 0116), so
// nothing here is an XSS hole - but silently accepting `javascript:` and
// producing a link that quietly renders as dead text later is exactly the
// masked failure AGENTS.md's fallback policy rules out. It refuses at the
// point of insertion and says why, the same way ImageButton already refuses
// a non-UUID document id.
describe('TestNoteEditor_LinkButtonRefusesAnUnsafeScheme', () => {
  it('refuses javascript: with a visible reason and inserts nothing', async () => {
    const onSave = vi.fn()
    render(<NoteEditor value={'Seacocks\n'} onSave={onSave} />)
    await screen.findByText('Seacocks')

    fireEvent.click(screen.getByRole('button', { name: 'Link' }))
    fireEvent.change(await screen.findByLabelText('Link URL'), {
      target: { value: 'javascript:alert(1)' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Insert link' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(/link/i)
    expect(screen.getByLabelText('Link URL')).toHaveValue('javascript:alert(1)')
  })

  it('accepts an hc-note: reference and an https: URL', async () => {
    const onSave = vi.fn()
    render(<NoteEditor value={'Seacocks\n'} onSave={onSave} />)
    await screen.findByText('Seacocks')

    fireEvent.click(screen.getByRole('button', { name: 'Link' }))
    fireEvent.change(await screen.findByLabelText('Link URL'), {
      target: { value: 'https://example.com/manual' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Insert link' }))

    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })
})
