import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, within } from '@testing-library/react'

import { ChecklistRunner } from '@/components/documents/checklist-runner'
import { useChecklistRun } from '@/hooks/use-checklist-run'
import type { ChecklistRun } from '@/hooks/use-checklist-run'

// Plan "Notes and the Boat's Manual" §7, ADR 0118: the checklist runner's
// own coverage. use-checklist-run.ts is mocked wholesale (the same
// convention manual-folder-view.test.tsx uses for useNotes/useManuals) so
// these tests exercise ChecklistRunner's rendering and interaction
// contract directly, without a fetch mock standing in for the backend -
// checklist_runs_handlers_test.go and checklist_runs_store_test.go already
// cover that side.
vi.mock('@/hooks/use-checklist-run')

const mockedUseChecklistRun = vi.mocked(useChecklistRun)

function makeRun(overrides: Partial<ChecklistRun> = {}): ChecklistRun {
  return {
    id: 'run-1',
    document_id: 'note-1',
    started_at: '2026-09-20T14:22:00Z',
    items: [
      { item_key: 'k1', occurrence: 0, text: 'Seacocks open', depth: 0, checked: false },
      { item_key: 'k2', occurrence: 0, text: 'Check bilge', depth: 0, checked: false },
      { item_key: 'k3', occurrence: 0, text: 'Start blower', depth: 0, checked: false },
    ],
    changed: [],
    total: 3,
    checked_count: 0,
    ...overrides,
  }
}

type HookReturn = ReturnType<typeof useChecklistRun>

function makeHook(overrides: Partial<HookReturn> = {}): HookReturn {
  return {
    run: makeRun(),
    resumed: false,
    loading: false,
    error: null,
    tick: vi.fn(),
    complete: vi.fn(),
    abandon: vi.fn(),
    ...overrides,
  }
}

beforeEach(() => {
  mockedUseChecklistRun.mockReset()
})

describe('ChecklistRunner', () => {
  it('renders items as full-width tappable rows with the standard hit-target sizing', () => {
    mockedUseChecklistRun.mockReturnValue(makeHook())
    render(<ChecklistRunner noteId="note-1" noteTitle="Shutdown" onExit={vi.fn()} />)

    const row = screen.getByText('Seacocks open').closest('button')
    expect(row).not.toBeNull()
    expect(row).toHaveClass('min-h-14')
    expect(row).toHaveClass('p-4')
  })

  it('shows the progress count and started time in the sticky header', () => {
    mockedUseChecklistRun.mockReturnValue(makeHook({ run: makeRun({ checked_count: 1 }) }))
    render(<ChecklistRunner noteId="note-1" noteTitle="Shutdown" onExit={vi.fn()} />)

    expect(screen.getByText('1 / 3')).toBeInTheDocument()
  })

  it('a tick posts through the hook and re-renders from the response, not local state', () => {
    const tick = vi.fn().mockResolvedValue(undefined)
    mockedUseChecklistRun.mockReturnValue(makeHook({ tick }))
    const { rerender } = render(<ChecklistRunner noteId="note-1" noteTitle="Shutdown" onExit={vi.fn()} />)

    fireEvent.click(screen.getByText('Seacocks open'))
    expect(tick).toHaveBeenCalledWith('k1', 0, true)

    // The component never flips its own local checked flag - it only ever
    // reflects whatever useChecklistRun's `run` currently is. Simulate the
    // hook re-rendering with the SERVER's response (exactly what the real
    // hook does inside tick()) and assert the row now reads as checked.
    const updated = makeRun({
      items: [
        { item_key: 'k1', occurrence: 0, text: 'Seacocks open', depth: 0, checked: true, checked_at: '2026-09-20T14:25:00Z' },
        { item_key: 'k2', occurrence: 0, text: 'Check bilge', depth: 0, checked: false },
        { item_key: 'k3', occurrence: 0, text: 'Start blower', depth: 0, checked: false },
      ],
      checked_count: 1,
    })
    mockedUseChecklistRun.mockReturnValue(makeHook({ tick, run: updated }))
    rerender(<ChecklistRunner noteId="note-1" noteTitle="Shutdown" onExit={vi.fn()} />)

    expect(screen.getByText('1 / 3')).toBeInTheDocument()
    expect(screen.getByText('Seacocks open').closest('button')).toHaveClass('line-through')
  })

  it('completed rows stay in place rather than reordering to the bottom', () => {
    // The middle item is the one that's checked - if the runner ever
    // re-sorted by checked state, it would move to the top or bottom of
    // the list. It must not: this is plan §7's own "the list must never
    // reorder under a thumb."
    const run = makeRun({
      items: [
        { item_key: 'k1', occurrence: 0, text: 'Seacocks open', depth: 0, checked: false },
        { item_key: 'k2', occurrence: 0, text: 'Check bilge', depth: 0, checked: true, checked_at: '2026-09-20T14:23:00Z' },
        { item_key: 'k3', occurrence: 0, text: 'Start blower', depth: 0, checked: false },
      ],
      checked_count: 1,
    })
    mockedUseChecklistRun.mockReturnValue(makeHook({ run }))
    render(<ChecklistRunner noteId="note-1" noteTitle="Shutdown" onExit={vi.fn()} />)

    const rows = screen.getAllByRole('button').filter((el) => el.hasAttribute('aria-pressed'))
    const texts = rows.map((row) => row.textContent)
    expect(texts).toEqual(['Seacocks open', 'Check bilge', 'Start blower'])
    expect(rows[1]).toHaveClass('line-through')
  })

  it('shows a Re-check group for changed items with the text as it was ticked', () => {
    const run = makeRun({
      changed: [
        { item_key: 'old-key', occurrence: 0, text: 'Check bilge', checked_at: '2026-09-20T14:23:00Z' },
      ],
    })
    mockedUseChecklistRun.mockReturnValue(makeHook({ run }))
    render(<ChecklistRunner noteId="note-1" noteTitle="Shutdown" onExit={vi.fn()} />)

    expect(screen.getByText('Re-check')).toBeInTheDocument()
    const reCheckSection = screen.getByText('Re-check').closest('div')
    expect(reCheckSection).not.toBeNull()
    expect(within(reCheckSection as HTMLElement).getByText('Check bilge')).toBeInTheDocument()
  })

  it('does not show a Re-check group when nothing has changed', () => {
    mockedUseChecklistRun.mockReturnValue(makeHook())
    render(<ChecklistRunner noteId="note-1" noteTitle="Shutdown" onExit={vi.fn()} />)
    expect(screen.queryByText('Re-check')).not.toBeInTheDocument()
  })

  it('resuming shows "Started HH:MM, x of y done" and lands on the first unchecked item', () => {
    const run = makeRun({
      items: [
        { item_key: 'k1', occurrence: 0, text: 'Seacocks open', depth: 0, checked: true, checked_at: '2026-09-20T14:23:00Z' },
        { item_key: 'k2', occurrence: 0, text: 'Check bilge', depth: 0, checked: true, checked_at: '2026-09-20T14:24:00Z' },
        { item_key: 'k3', occurrence: 0, text: 'Start blower', depth: 0, checked: false },
      ],
      checked_count: 2,
    })
    mockedUseChecklistRun.mockReturnValue(makeHook({ run, resumed: true }))
    const scrollSpy = vi.spyOn(Element.prototype, 'scrollIntoView').mockImplementation(() => {})

    render(<ChecklistRunner noteId="note-1" noteTitle="Shutdown" onExit={vi.fn()} />)

    expect(screen.getByText(/Started \d{1,2}:\d{2}.*2 of 3 done/)).toBeInTheDocument()

    const blowerButton = screen.getByText('Start blower').closest('button')
    expect(scrollSpy).toHaveBeenCalled()
    expect(scrollSpy.mock.instances).toContain(blowerButton)

    scrollSpy.mockRestore()
  })

  it('Back exits the runner without abandoning the run', () => {
    const abandon = vi.fn()
    const onExit = vi.fn()
    mockedUseChecklistRun.mockReturnValue(makeHook({ abandon }))
    render(<ChecklistRunner noteId="note-1" noteTitle="Shutdown" onExit={onExit} />)

    fireEvent.click(screen.getByText('Back'))
    expect(onExit).toHaveBeenCalled()
    expect(abandon).not.toHaveBeenCalled()
  })

  it('Abandon checklist calls abandon and then exits', async () => {
    const abandon = vi.fn().mockResolvedValue(undefined)
    const onExit = vi.fn()
    mockedUseChecklistRun.mockReturnValue(makeHook({ abandon }))
    render(<ChecklistRunner noteId="note-1" noteTitle="Shutdown" onExit={onExit} />)

    fireEvent.click(screen.getByText('Abandon checklist'))
    await vi.waitFor(() => expect(abandon).toHaveBeenCalled())
    await vi.waitFor(() => expect(onExit).toHaveBeenCalled())
  })

  it('Complete checklist calls complete', async () => {
    const complete = vi.fn().mockResolvedValue(undefined)
    mockedUseChecklistRun.mockReturnValue(makeHook({ complete }))
    render(<ChecklistRunner noteId="note-1" noteTitle="Shutdown" onExit={vi.fn()} />)

    fireEvent.click(screen.getByText('Complete checklist'))
    await vi.waitFor(() => expect(complete).toHaveBeenCalled())
  })

  it('shows a loading state before the run arrives', () => {
    mockedUseChecklistRun.mockReturnValue(makeHook({ run: null, loading: true }))
    render(<ChecklistRunner noteId="note-1" noteTitle="Shutdown" onExit={vi.fn()} />)
    expect(screen.getByText(/starting checklist/i)).toBeInTheDocument()
  })

  it('surfaces an error rather than masking it', () => {
    mockedUseChecklistRun.mockReturnValue(makeHook({ run: null, loading: false, error: 'note has no checklist items' }))
    render(<ChecklistRunner noteId="note-1" noteTitle="Shutdown" onExit={vi.fn()} />)
    expect(screen.getByRole('alert')).toHaveTextContent('note has no checklist items')
  })
})
