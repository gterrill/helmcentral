/**
 * Drafts the update reload must ask about: a note being edited, a note being
 * captured, and a service log entry being entered. Each reports unsaved work
 * to the router (a guard that never holds a move) while it holds some, and
 * stops when saved, emptied or closed.
 */
import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'

vi.mock('@/hooks/use-notes')
vi.mock('@/hooks/use-inventory', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/hooks/use-inventory')>()
  return { ...actual, useEquipment: () => ({ items: [], loading: false, error: null, refresh: vi.fn() }) }
})

import { NoteEditor } from '@/components/note-editor'
import { NoteCaptureSheet } from '@/components/documents/note-capture-sheet'
import { MaintenanceCompleteDialog, MaintenanceLogEntryDialog } from '@/components/inventory/maintenance-log-dialogs'
import { useNotes } from '@/hooks/use-notes'
import type { MaintenanceLogEntry, MaintenanceRule } from '@/hooks/use-maintenance'
import { LAZY_NOTE_EDITOR_TIMEOUT_MS } from './helpers/lazy-note-editor'
import { ITEM_RULE_PROVENANCE } from './maintenance-fixtures'
import { testRouter } from './helpers/router'

async function typeInSource(text: string) {
  fireEvent.click(await screen.findByRole('button', { name: 'Markdown source' }, { timeout: LAZY_NOTE_EDITOR_TIMEOUT_MS }))
  fireEvent.change(await screen.findByLabelText('Note markdown source'), { target: { value: text } })
}

describe('a note being edited', () => {
  it('counts as unsaved work once edited, and not before', async () => {
    const { wrapper, router } = testRouter('/documents')
    render(<NoteEditor value={'- [ ] Todo\n'} onSave={vi.fn().mockResolvedValue(undefined)} />, { wrapper })
    await screen.findByText('Todo', {}, { timeout: LAZY_NOTE_EDITOR_TIMEOUT_MS })
    expect(router.hasGuards()).toBe(false)

    await typeInSource('half a thought')
    expect(router.hasGuards()).toBe(true)

    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(router.hasGuards()).toBe(false))
  })

  it('never holds a move: leaving the note is still allowed', async () => {
    const { wrapper, router } = testRouter('/documents')
    render(<NoteEditor value={'- [ ] Todo\n'} onSave={vi.fn()} />, { wrapper })
    await screen.findByText('Todo', {}, { timeout: LAZY_NOTE_EDITOR_TIMEOUT_MS })
    await typeInSource('half a thought')
    expect(router.navigate({ panel: 'radar' })).toBe(true)
  })
})

describe('the new-note sheet', () => {
  it('counts as unsaved work while it holds text, and not once emptied or closed', async () => {
    vi.mocked(useNotes).mockReturnValue({
      notes: [], loading: false, error: null, refresh: vi.fn(), getNote: vi.fn(), createNote: vi.fn(), patchNote: vi.fn(),
    } as unknown as ReturnType<typeof useNotes>)
    const { wrapper, router } = testRouter('/documents')
    const { rerender } = render(<NoteCaptureSheet open onOpenChange={vi.fn()} />, { wrapper })
    fireEvent.click(await screen.findByRole('button', { name: 'Markdown source' }, { timeout: LAZY_NOTE_EDITOR_TIMEOUT_MS }))
    const source = await screen.findByRole('textbox', { name: 'Note markdown source' })
    expect(router.hasGuards()).toBe(false)

    fireEvent.change(source, { target: { value: 'Ring Dave' } })
    expect(router.hasGuards()).toBe(true)

    fireEvent.change(source, { target: { value: '' } })
    expect(router.hasGuards()).toBe(false)

    fireEvent.change(source, { target: { value: 'Ring Dave again' } })
    expect(router.hasGuards()).toBe(true)
    rerender(<NoteCaptureSheet open={false} onOpenChange={vi.fn()} />)
    expect(router.hasGuards()).toBe(false)
  })
})

const rule = (): MaintenanceRule => ({
  id: 'rule-1', equipment_id: 'eq-1', equipment_name: 'Main engine', system: 'propulsion',
  description: 'Oil change', interval_hours: null, interval_months: null,
  due_soon_hours: null, due_soon_months: null, fixed_due_date: '', last_done_at: '',
  last_done_hours: null, profile_service_id: '', procedure_note_id: '',
  ack_reason: '', acknowledged: false, created_at: '', updated_at: '',
  status: 'due_soon', remaining_hours: null, remaining_days: null,
  hours_unknown: false, has_hour_meter_path: true, hours_as_of: null, current_hours: null,
  ...ITEM_RULE_PROVENANCE,
})

const entry = (): MaintenanceLogEntry => ({
  id: 'log-1', equipment_id: 'eq-1', rule_id: null, performed_at: '2026-06-01',
  hours: 100, kind: 'repair', description: 'Replaced belt', who: 'Skipper',
  cost: null, currency: '', created_at: '', updated_at: '', parts: [], photo_ids: [],
})

describe('the maintenance log dialogs', () => {
  it('Complete: counts as unsaved work once something is entered, and not when opened blank', () => {
    const { wrapper, router } = testRouter('/inventory')
    render(<MaintenanceCompleteDialog rule={rule()} onCancel={vi.fn()} onComplete={vi.fn()} />, { wrapper })
    expect(router.hasGuards()).toBe(false)

    fireEvent.change(screen.getByLabelText('Notes'), { target: { value: 'Changed oil and filter' } })
    expect(router.hasGuards()).toBe(true)
  })

  it('Complete: stops once closed', () => {
    const { wrapper, router } = testRouter('/inventory')
    const { rerender } = render(<MaintenanceCompleteDialog rule={rule()} onCancel={vi.fn()} onComplete={vi.fn()} />, { wrapper })
    fireEvent.change(screen.getByLabelText('Notes'), { target: { value: 'Changed oil' } })
    expect(router.hasGuards()).toBe(true)
    rerender(<MaintenanceCompleteDialog rule={null} onCancel={vi.fn()} onComplete={vi.fn()} />)
    expect(router.hasGuards()).toBe(false)
  })

  it('Entry: counts as unsaved work after an edit, and not after it is saved', async () => {
    const { wrapper, router } = testRouter('/inventory')
    const onSave = vi.fn().mockResolvedValue(entry())
    render(<MaintenanceLogEntryDialog entry={entry()} equipmentId="eq-1" open onCancel={vi.fn()} onSave={onSave} onDelete={vi.fn()} />, { wrapper })
    expect(router.hasGuards()).toBe(false)

    fireEvent.change(screen.getByLabelText('Who'), { target: { value: 'Mate' } })
    expect(router.hasGuards()).toBe(true)

    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(router.hasGuards()).toBe(false))
  })
})
