import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'

import { ManualFolderView } from '@/components/documents/manual-folder-view'
import { useManuals } from '@/hooks/use-manuals'
import { useNotes } from '@/hooks/use-notes'
import type { ManualTreeNode } from '@/hooks/use-manuals'

// Revision "one panel, not three" (2026-09-20): ManualFolderView is what's
// left of the deleted manuals-panel.tsx once its none/one/several state
// machine and switcher move out to Documents' own folder browsing (a
// manual IS a folder, so switching between manuals is just navigating
// between folders the ordinary way) - this file re-expresses that panel's
// surviving coverage (tree order, Arrange, reading a section, promote/
// demote) against the standalone component. Same mocking convention as the
// deleted manuals-panel.test.tsx: useManuals and useNotes mocked,
// findManualTreeNode left real (a pure helper the component calls
// directly, not through the hook).
vi.mock('@/hooks/use-manuals', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/hooks/use-manuals')>()
  return { ...actual, useManuals: vi.fn() }
})
vi.mock('@/hooks/use-notes')

// note-editor.tsx is the lazy-loaded Plate-based WYSIWYG editor (ADR 0117) -
// its own round-trip/golden-file coverage lives in note-editor.test.tsx.
// This file only needs to prove ManualFolderView's Edit toggle reaches it
// and that Save goes through use-notes.ts's patchNote, so it stands in a
// lightweight fake exposing exactly that contract (value/onSave), the same
// way a heavy lazy dependency is stubbed anywhere else in this suite.
vi.mock('@/components/note-editor', () => ({
  NoteEditor: ({ value, onSave }: { value: string; onSave: (body: string) => void | Promise<void> }) => (
    <div>
      <p>editor: {value}</p>
      <button type="button" onClick={() => { void onSave(`${value} edited`) }}>Save</button>
    </div>
  ),
}))

// checklist-runner.tsx has its own full coverage (checklist-runner.test.tsx)
// against a mocked use-checklist-run - this file only needs to prove
// ManualFolderView's "Start checklist" reaches it with the right note, and
// that Back exits the runner mode back to the reading view, so it stands in
// the same lightweight fake shape as the NoteEditor mock above.
vi.mock('@/components/documents/checklist-runner', () => ({
  ChecklistRunner: ({ noteId, noteTitle, onExit }: { noteId: string; noteTitle: string; onExit: () => void }) => (
    <div>
      <p>checklist runner: {noteId} ({noteTitle})</p>
      <button type="button" onClick={onExit}>Back</button>
    </div>
  ),
}))

const mockedUseManuals = vi.mocked(useManuals)
const mockedUseNotes = vi.mocked(useNotes)

type ManualsMock = ReturnType<typeof useManuals>
type NotesMock = ReturnType<typeof useNotes>

function makeManualsMock(overrides: Partial<ManualsMock> = {}): ManualsMock {
  return {
    manuals: [],
    manualsLoading: false,
    manualsError: null,
    refreshManuals: vi.fn(),
    tree: null,
    treeLoading: false,
    treeError: null,
    refreshTree: vi.fn(),
    createManual: vi.fn(),
    flagManual: vi.fn(),
    clearManual: vi.fn(),
    reorder: vi.fn(),
    ...overrides,
  }
}

function makeNotesMock(overrides: Partial<NotesMock> = {}): NotesMock {
  return {
    notes: [],
    loading: false,
    error: null,
    refresh: vi.fn(),
    getNote: vi.fn(),
    createNote: vi.fn(),
    patchNote: vi.fn(),
    ...overrides,
  }
}

function docNode(overrides: Partial<ManualTreeNode> = {}): ManualTreeNode {
  return {
    type: 'document', id: 'n1', name: 'Genset start-up', sort_index: 0, updated_at: '2026-01-01T00:00:00Z',
    kind: 'note', note_type: 'procedure',
    ...overrides,
  }
}

function rootTree(children: ManualTreeNode[], overrides: Partial<ManualTreeNode> = {}): ManualTreeNode {
  return {
    type: 'folder', id: 'm1', name: 'Operations Manual', sort_index: 0, updated_at: '2026-01-01T00:00:00Z',
    children,
    ...overrides,
  }
}

beforeEach(() => {
  vi.clearAllMocks()
  mockedUseManuals.mockReturnValue(makeManualsMock())
  mockedUseNotes.mockReturnValue(makeNotesMock())
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('ManualFolderView: tree order', () => {
  it('renders sections in the order the server sent them (sort_index), not re-sorted by name', () => {
    const tree = rootTree([
      docNode({ id: 'n-z', name: 'Zebra procedure', sort_index: 0 }),
      docNode({ id: 'n-a', name: 'Alpha procedure', sort_index: 1 }),
    ])
    mockedUseManuals.mockReturnValue(makeManualsMock({ tree }))

    render(<ManualFolderView folderId="m1" sectionId={null} onSectionChange={vi.fn()} onDemoted={vi.fn()} onNavigateDocument={vi.fn()} />)

    const names = screen.getAllByText(/procedure$/).map((el) => el.textContent)
    expect(names).toEqual(['Zebra procedure', 'Alpha procedure'])
  })
})

describe('ManualFolderView: Arrange', () => {
  it('turning on Arrange reveals move controls, and moving the second item up commits one renumbered reorder call', async () => {
    const reorder = vi.fn().mockResolvedValue(undefined)
    const tree = rootTree([
      docNode({ id: 'n-1', name: 'Startup', sort_index: 3 }),
      docNode({ id: 'n-2', name: 'Shutdown', sort_index: 7 }),
    ])
    mockedUseManuals.mockReturnValue(makeManualsMock({ tree, reorder }))

    render(<ManualFolderView folderId="m1" sectionId={null} onSectionChange={vi.fn()} onDemoted={vi.fn()} onNavigateDocument={vi.fn()} />)
    expect(screen.queryByRole('button', { name: /Move "Shutdown" up/ })).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Arrange' }))
    fireEvent.click(screen.getByRole('button', { name: 'Move "Shutdown" up' }))

    await waitFor(() => expect(reorder).toHaveBeenCalledWith('m1', [
      { kind: 'document', id: 'n-2', sort_index: 0 },
      { kind: 'document', id: 'n-1', sort_index: 1 },
    ]))
  })
})

describe('ManualFolderView: reading and editing a section', () => {
  it('selecting a note section fetches its body through use-notes.ts and renders it', async () => {
    const getNote = vi.fn().mockResolvedValue({ document: docNode(), body: 'Open the seacock first.' })
    mockedUseManuals.mockReturnValue(makeManualsMock({ tree: rootTree([docNode({ id: 'n1', name: 'Genset start-up' })]) }))
    mockedUseNotes.mockReturnValue(makeNotesMock({ getNote }))

    render(<ManualFolderView folderId="m1" sectionId="n1" onSectionChange={vi.fn()} onDemoted={vi.fn()} onNavigateDocument={vi.fn()} />)

    await waitFor(() => expect(getNote).toHaveBeenCalledWith('n1'))
    expect(await screen.findByText('Open the seacock first.')).toBeInTheDocument()
  })

  it('selecting a non-note (filed document) section shows a placeholder instead of calling getNote', () => {
    const getNote = vi.fn()
    mockedUseManuals.mockReturnValue(makeManualsMock({
      tree: rootTree([docNode({ id: 'd1', name: 'Data plate photo', kind: 'file', note_type: undefined })]),
    }))
    mockedUseNotes.mockReturnValue(makeNotesMock({ getNote }))

    render(<ManualFolderView folderId="m1" sectionId="d1" onSectionChange={vi.fn()} onDemoted={vi.fn()} onNavigateDocument={vi.fn()} />)

    expect(screen.getByText(/filed document, not a note/)).toBeInTheDocument()
    expect(getNote).not.toHaveBeenCalled()
  })

  // ADR 0117 / the revision's own "Editing" requirement: the Edit toggle is
  // the only way NoteEditor is reachable from a manual section, and Save
  // must go through use-notes.ts's patchNote, not a raw fetch (the deleted
  // manuals-panel.tsx's own ManualReadingPane did exactly that raw fetch -
  // this is the regression it must not repeat).
  it('a note section opens in the editor and saves through patchNote', async () => {
    const getNote = vi.fn().mockResolvedValue({ document: docNode(), body: 'Open the seacock first.' })
    const patchNote = vi.fn().mockResolvedValue({ document: docNode(), body: 'Open the seacock first. edited' })
    mockedUseManuals.mockReturnValue(makeManualsMock({ tree: rootTree([docNode({ id: 'n1', name: 'Genset start-up' })]) }))
    mockedUseNotes.mockReturnValue(makeNotesMock({ getNote, patchNote }))

    render(<ManualFolderView folderId="m1" sectionId="n1" onSectionChange={vi.fn()} onDemoted={vi.fn()} onNavigateDocument={vi.fn()} />)
    await screen.findByText('Open the seacock first.')

    fireEvent.click(screen.getByRole('button', { name: 'Edit' }))
    expect(screen.getByText('editor: Open the seacock first.')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(patchNote).toHaveBeenCalledWith('n1', { body: 'Open the seacock first. edited' }))
  })
})

describe('ManualFolderView: checklist runner (plan §7, ADR 0118)', () => {
  it('shows Start checklist when the section has checklist items, and opens the runner', async () => {
    const getNote = vi.fn().mockResolvedValue({
      document: docNode(),
      body: '- [ ] Seacocks open\n- [ ] Check bilge',
      checklist: [
        { item_key: 'k1', occurrence: 0, text: 'Seacocks open', depth: 0 },
        { item_key: 'k2', occurrence: 0, text: 'Check bilge', depth: 0 },
      ],
    })
    mockedUseManuals.mockReturnValue(makeManualsMock({ tree: rootTree([docNode({ id: 'n1', name: 'Genset shutdown' })]) }))
    mockedUseNotes.mockReturnValue(makeNotesMock({ getNote }))

    render(<ManualFolderView folderId="m1" sectionId="n1" onSectionChange={vi.fn()} onDemoted={vi.fn()} onNavigateDocument={vi.fn()} />)
    await screen.findByText('Seacocks open')

    const startButton = screen.getByRole('button', { name: /Start checklist/ })
    fireEvent.click(startButton)

    expect(screen.getByText('checklist runner: n1 (Genset shutdown)')).toBeInTheDocument()

    // Back exits the runner MODE, back to the reading view - it does not
    // navigate away from the section.
    fireEvent.click(screen.getByRole('button', { name: 'Back' }))
    expect(screen.getByText('Seacocks open')).toBeInTheDocument()
  })

  it('hides Start checklist when the section has no checklist items', async () => {
    const getNote = vi.fn().mockResolvedValue({
      document: docNode(),
      body: 'Fuel return is the inboard valve.',
      checklist: [],
    })
    mockedUseManuals.mockReturnValue(makeManualsMock({ tree: rootTree([docNode({ id: 'n1', name: 'Fuel valves' })]) }))
    mockedUseNotes.mockReturnValue(makeNotesMock({ getNote }))

    render(<ManualFolderView folderId="m1" sectionId="n1" onSectionChange={vi.fn()} onDemoted={vi.fn()} onNavigateDocument={vi.fn()} />)
    await screen.findByText('Fuel return is the inboard valve.')

    expect(screen.queryByRole('button', { name: /Start checklist/ })).not.toBeInTheDocument()
  })
})

describe('ManualFolderView: demoting', () => {
  it('"Stop treating as manual" calls clearManual and reports back to the caller', async () => {
    const clearManual = vi.fn().mockResolvedValue(undefined)
    mockedUseManuals.mockReturnValue(makeManualsMock({ tree: rootTree([]), clearManual }))
    const onDemoted = vi.fn()

    render(<ManualFolderView folderId="m1" sectionId={null} onSectionChange={vi.fn()} onDemoted={onDemoted} onNavigateDocument={vi.fn()} />)
    fireEvent.click(screen.getByRole('button', { name: 'Stop treating as manual' }))

    await waitFor(() => expect(clearManual).toHaveBeenCalledWith('m1'))
    expect(onDemoted).toHaveBeenCalled()
  })
})
