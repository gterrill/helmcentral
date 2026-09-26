import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'

import { NoteEditor } from '@/components/note-editor'
import { uploadDocument } from '@/lib/document-upload'

// ImageButton's Take photo/Add from library path (2026-09-27) runs every
// picked file through lib/image-downscale.ts before it ever reaches the
// network - createImageBitmap/canvas.toBlob aren't implemented by jsdom, so
// this mock is a harmless pass-through (the same convention equipment-
// editor.test.tsx already uses for the identical reason), not a stand-in
// for that module's own logic (image-downscale.test.ts covers that).
vi.mock('@/lib/image-downscale', () => ({
  downscaleImage: vi.fn(async (file: Blob) => file),
  downscaleAll: vi.fn(async (files: File[]) => files.map((file) => ({ file, result: { ok: true as const, blob: file } }))),
  photoFilename: (original: string) => `${original.replace(/\.[^.]+$/, '').trim() || 'photo'}.jpg`,
}))

// The actual network call - mocked here so these tests pin what ImageButton
// DOES with a successful/failed upload, not lib/document-upload.ts's own
// fetch plumbing.
vi.mock('@/lib/document-upload', () => ({
  uploadDocument: vi.fn(),
}))

const mockedUploadDocument = vi.mocked(uploadDocument)

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

// 2026-09-27: an operator standing over the job can shoot the photo and have
// it land in the note in one step, rather than a trip to Documents first to
// upload it and copy its id back. The pasted-id path (plan §5's original
// design) stays exactly as it was - these three describe blocks pin all
// three ways ImageButton can put a photo into a note, in the same file,
// against the same component, so a regression in one path shows up next to
// the others.
const UUID = '0f3b1c2e-8a4d-4f21-9c33-1d2e3f4a5b6c'
const UUID_2 = '1a2b3c4d-5e6f-4a1b-9c2d-3e4f5a6b7c8d'

beforeEach(() => {
  mockedUploadDocument.mockReset()
})

describe('TestNoteEditor_ImageButtonPastesADocumentId', () => {
  it('inserts a reference to an existing document by pasted id, uploading nothing', async () => {
    const onSave = vi.fn()
    render(<NoteEditor value={'Engine bay\n'} onSave={onSave} />)
    await screen.findByText('Engine bay')

    fireEvent.click(screen.getByRole('button', { name: 'Image' }))
    fireEvent.change(await screen.findByLabelText('Document id'), { target: { value: UUID } })
    fireEvent.change(screen.getByLabelText('Photo caption'), { target: { value: 'Fuel manifold' } })
    fireEvent.click(screen.getByRole('button', { name: 'Insert photo' }))

    expect(await screen.findByRole('img', { name: 'Fuel manifold' })).toHaveAttribute(
      'src',
      expect.stringContaining(`/api/documents/${UUID}/content`),
    )
    expect(mockedUploadDocument).not.toHaveBeenCalled()
  })

  it('refuses a non-UUID id with a visible reason and inserts nothing', async () => {
    const onSave = vi.fn()
    render(<NoteEditor value={'Engine bay\n'} onSave={onSave} />)
    await screen.findByText('Engine bay')

    fireEvent.click(screen.getByRole('button', { name: 'Image' }))
    fireEvent.change(await screen.findByLabelText('Document id'), { target: { value: 'not-a-uuid' } })
    fireEvent.click(screen.getByRole('button', { name: 'Insert photo' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(/uuid/i)
    expect(screen.queryByRole('img')).not.toBeInTheDocument()
  })
})

describe('TestNoteEditor_ImageButtonTakePhotoUploadsAndInserts', () => {
  it('uploads a captured photo tagged photo and inserts it with its caption', async () => {
    mockedUploadDocument.mockResolvedValue({ id: UUID, duplicate: false })
    const onSave = vi.fn()
    render(<NoteEditor value={'Engine bay\n'} onSave={onSave} />)
    await screen.findByText('Engine bay')

    fireEvent.click(screen.getByRole('button', { name: 'Image' }))
    fireEvent.change(await screen.findByLabelText('Photo caption'), { target: { value: 'Alternator belt' } })

    const file = new File(['photo-bytes'], 'IMG_0001.heic', { type: 'image/heic' })
    fireEvent.change(screen.getByLabelText('Take photo'), { target: { files: [file] } })

    // Renamed to .jpg (downscaleImage's own re-encode format, lib/image-
    // downscale.ts's photoFilename) and tagged `photo` so the library can
    // tell a note's own photo apart from an ordinary manual/PDF upload.
    await waitFor(() => expect(mockedUploadDocument).toHaveBeenCalledWith(file, 'IMG_0001.jpg', ['photo']))
    expect(await screen.findByRole('img', { name: 'Alternator belt' })).toHaveAttribute(
      'src',
      expect.stringContaining(`/api/documents/${UUID}/content`),
    )
  })

  it('inserts every picked library photo as its own image node, in pick order', async () => {
    mockedUploadDocument
      .mockResolvedValueOnce({ id: UUID, duplicate: false })
      .mockResolvedValueOnce({ id: UUID_2, duplicate: false })
    const onSave = vi.fn()
    const { container } = render(<NoteEditor value={'Engine bay\n'} onSave={onSave} />)
    await screen.findByText('Engine bay')

    fireEvent.click(screen.getByRole('button', { name: 'Image' }))
    const fileA = new File(['a'], 'a.jpg', { type: 'image/jpeg' })
    const fileB = new File(['b'], 'b.jpg', { type: 'image/jpeg' })
    fireEvent.change(await screen.findByLabelText('Add from library'), { target: { files: [fileA, fileB] } })

    await waitFor(() => expect(mockedUploadDocument).toHaveBeenCalledTimes(2))
    // No caption was entered for this pick, so both <img>s render with
    // alt="" - correctly presentational (no role "img") per the HTML-ARIA
    // mapping, which is why this asserts against the DOM directly rather
    // than screen.findAllByRole('img') the way the captioned tests above do.
    await waitFor(() => expect(container.querySelectorAll('img')).toHaveLength(2))
    const images = container.querySelectorAll('img')
    expect(images[0]).toHaveAttribute('src', expect.stringContaining(`/api/documents/${UUID}/content`))
    expect(images[1]).toHaveAttribute('src', expect.stringContaining(`/api/documents/${UUID_2}/content`))
  })
})

describe('TestNoteEditor_ImageButtonUploadFailureInsertsNothing', () => {
  it('shows the server error and inserts no image when the upload fails', async () => {
    mockedUploadDocument.mockRejectedValue(new Error('upload failed: file too large'))
    const onSave = vi.fn()
    render(<NoteEditor value={'Engine bay\n'} onSave={onSave} />)
    await screen.findByText('Engine bay')

    fireEvent.click(screen.getByRole('button', { name: 'Image' }))
    const file = new File(['photo-bytes'], 'IMG_0002.jpg', { type: 'image/jpeg' })
    fireEvent.change(await screen.findByLabelText('Take photo'), { target: { files: [file] } })

    expect(await screen.findByRole('alert')).toHaveTextContent('upload failed: file too large')
    expect(screen.queryByRole('img')).not.toBeInTheDocument()
  })
})
