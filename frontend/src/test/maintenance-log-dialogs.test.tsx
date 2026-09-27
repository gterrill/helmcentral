import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MaintenanceCompleteDialog, MaintenanceLogEntryDialog } from '@/components/inventory/maintenance-log-dialogs'
import type { MaintenanceLogEntry, MaintenanceRule } from '@/hooks/use-maintenance'

// ADR 0138: code-review fixes to the service log's two write dialogs -
// editing an existing entry actually saves (issue 1), a failed delete is
// surfaced rather than silently doing nothing (issue 8), and the Complete
// dialog enforces hours-required and next-due-date-required the same way
// the backend does (issues 5/6).

vi.mock('@/hooks/use-inventory', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/hooks/use-inventory')>()
  return { ...actual, useEquipment: () => ({ items: [], loading: false, error: null, refresh: vi.fn() }) }
})

function makeEntry(overrides: Partial<MaintenanceLogEntry> = {}): MaintenanceLogEntry {
  return {
    id: 'log-1', equipment_id: 'eq-1', rule_id: null, performed_at: '2026-06-01',
    hours: 100, kind: 'repair', description: 'Replaced belt', who: 'Skipper',
    cost: null, currency: '', created_at: '', updated_at: '', parts: [], photo_ids: [],
    ...overrides,
  }
}

function makeRule(overrides: Partial<MaintenanceRule> = {}): MaintenanceRule {
  return {
    id: 'rule-1', equipment_id: 'eq-1', equipment_name: 'Main engine', system: 'propulsion',
    description: 'Oil change', interval_hours: null, interval_months: null,
    due_soon_hours: null, due_soon_months: null, fixed_due_date: '', last_done_at: '',
    last_done_hours: null, profile_service_id: '', procedure_note_id: '',
    ack_reason: '', acknowledged: false, created_at: '', updated_at: '',
    status: 'due_soon', remaining_hours: null, remaining_days: null,
    hours_unknown: false, has_hour_meter_path: true, hours_as_of: null, current_hours: null,
    ...overrides,
  }
}

describe('MaintenanceLogEntryDialog: editing an existing entry', () => {
  it('keeps Save available and actually saves a change instead of discarding it on Done', async () => {
    const entry = makeEntry()
    const onSave = vi.fn().mockResolvedValue(makeEntry({ description: 'Replaced belt and tensioner' }))
    const onCancel = vi.fn()

    render(
      <MaintenanceLogEntryDialog entry={entry} equipmentId="eq-1" open onCancel={onCancel} onSave={onSave} onDelete={vi.fn()} />,
    )

    // Nothing changed yet - the close button reads Done, and there is
    // nothing to lose by pressing it.
    expect(screen.getByRole('button', { name: 'Done' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Save' })).not.toBeInTheDocument()

    // The moment something changes, Save must appear (and the close
    // button must stop claiming there's nothing to lose) - this is the
    // bug: Save used to never come back at all, and Done silently
    // discarded this edit.
    fireEvent.change(screen.getByLabelText('Description'), { target: { value: 'Replaced belt and tensioner' } })
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(onSave).toHaveBeenCalled())
    expect(onSave.mock.calls[0][0]).toMatchObject({ description: 'Replaced belt and tensioner' })
  })

  it('does not silently discard an edit made after the entry was already saved once', async () => {
    const entry = makeEntry()
    const onSave = vi.fn().mockResolvedValue(makeEntry())
    const onCancel = vi.fn()

    render(
      <MaintenanceLogEntryDialog entry={entry} equipmentId="eq-1" open onCancel={onCancel} onSave={onSave} onDelete={vi.fn()} />,
    )

    // Edit and save once.
    fireEvent.change(screen.getByLabelText('Who'), { target: { value: 'Mate' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(onSave).toHaveBeenCalledTimes(1))

    // A further edit after that first save must bring Save back rather
    // than leaving only a Done that discards it.
    fireEvent.change(screen.getByLabelText('Who'), { target: { value: 'Skipper again' } })
    expect(screen.getByRole('button', { name: 'Save' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(onSave).toHaveBeenCalledTimes(2))
  })
})

describe('MaintenanceLogEntryDialog: creating a brand new standalone entry', () => {
  // Code-review finding: the CALLER used to decide create-vs-update from
  // its own editingEntry state, which stayed null/undefined for the whole
  // life of a brand-new-entry dialog (creatingEntry never became
  // editingEntry after the first save) - so a second Save on the still-open
  // dialog called create AGAIN: a silent duplicate entry, with no photos
  // (they were attached to the first one) and no Delete button (isEditing
  // was keyed off the same stale prop). onSave's second argument -
  // existingId, sourced from this dialog's OWN savedEntry, never the
  // caller's - is what lets the caller update instead the second time.
  it('passes the newly created entry\'s own id as existingId on a second Save, and offers Delete', async () => {
    const onSave = vi.fn()
      .mockResolvedValueOnce(makeEntry({ id: 'log-99', description: 'first' }))
      .mockResolvedValueOnce(makeEntry({ id: 'log-99', description: 'second' }))
    const onCancel = vi.fn()
    const onDelete = vi.fn()

    render(
      <MaintenanceLogEntryDialog entry={undefined} equipmentId="eq-1" open onCancel={onCancel} onSave={onSave} onDelete={onDelete} />,
    )

    expect(screen.queryByRole('button', { name: 'Delete' })).not.toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('Description'), { target: { value: 'first' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(onSave).toHaveBeenCalledTimes(1))
    expect(onSave.mock.calls[0][1]).toBeNull()

    // The entry now exists on the server - Delete must appear, and photos
    // (already covered elsewhere by the savedEntry-gated PhotoStripEditor)
    // are keyed off the same state.
    await screen.findByRole('button', { name: 'Delete' })

    fireEvent.change(screen.getByLabelText('Description'), { target: { value: 'second' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(onSave).toHaveBeenCalledTimes(2))
    expect(onSave.mock.calls[1][1]).toBe('log-99')
  })
})

describe('MaintenanceLogEntryDialog: delete', () => {
  it('surfaces a failed delete as an alert and keeps the dialog open', async () => {
    const entry = makeEntry()
    const onDelete = vi.fn().mockRejectedValue(new Error('log entry not found'))
    const onCancel = vi.fn()

    render(
      <MaintenanceLogEntryDialog entry={entry} equipmentId="eq-1" open onCancel={onCancel} onSave={vi.fn()} onDelete={onDelete} />,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))

    await screen.findByRole('alert')
    expect(screen.getByRole('alert')).toHaveTextContent('log entry not found')
    expect(onCancel).not.toHaveBeenCalled()
    // The dialog itself is still open and showing the entry's own fields.
    expect(screen.getByDisplayValue('Replaced belt')).toBeInTheDocument()
  })
})

describe('MaintenanceCompleteDialog: hours required', () => {
  it('disables Complete until hours are given for an hours-based rule', async () => {
    const rule = makeRule({ interval_hours: 250, current_hours: null, hours_unknown: true })
    const onComplete = vi.fn()

    render(<MaintenanceCompleteDialog rule={rule} onCancel={vi.fn()} onComplete={onComplete} />)

    expect(screen.getByText(/never been received/i)).toBeInTheDocument()
    const completeButton = screen.getByRole('button', { name: 'Complete' })
    expect(completeButton).toBeDisabled()

    fireEvent.change(screen.getByLabelText(/Hours/), { target: { value: '1234' } })
    expect(completeButton).not.toBeDisabled()
    fireEvent.click(completeButton)
    await waitFor(() => expect(onComplete).toHaveBeenCalled())
  })

  it('prefilled hours from a live reading is not blocked', () => {
    const rule = makeRule({ interval_hours: 250, current_hours: 987.5 })
    render(<MaintenanceCompleteDialog rule={rule} onCancel={vi.fn()} onComplete={vi.fn()} />)
    expect(screen.getByRole('button', { name: 'Complete' })).not.toBeDisabled()
  })

  // Code-review finding 3: an old live reading is never a warning - it is
  // shown as "as of <age>", never "unknown or stale". Finding 4: the value
  // shown/prefilled here is explicitly labelled as the GAUGE reading, since
  // that's what current_hours now carries.
  it('shows the live reading\'s own age instead of a staleness warning', () => {
    const sixHoursAgo = new Date(Date.now() - 6 * 60 * 60 * 1000).toISOString()
    const rule = makeRule({ interval_hours: 250, current_hours: 987.5, hours_as_of: sixHoursAgo })
    render(<MaintenanceCompleteDialog rule={rule} onCancel={vi.fn()} onComplete={vi.fn()} />)
    expect(screen.getByLabelText(/Hours \(gauge reading\)/)).toBeInTheDocument()
    expect(screen.getByText(/Live reading, as of/)).toBeInTheDocument()
    expect(screen.queryByText(/unknown or stale/i)).not.toBeInTheDocument()
  })
})

describe('MaintenanceCompleteDialog: fixed due date', () => {
  it('requires a new due date for a fixed-date rule with no monthly interval, and sends it', async () => {
    const rule = makeRule({ interval_hours: null, fixed_due_date: '2026-06-01', status: 'overdue' })
    const onComplete = vi.fn().mockResolvedValue({ rule, entry: makeEntry() })

    render(<MaintenanceCompleteDialog rule={rule} onCancel={vi.fn()} onComplete={onComplete} />)

    const completeButton = screen.getByRole('button', { name: 'Complete' })
    expect(completeButton).toBeDisabled()

    fireEvent.change(screen.getByLabelText(/New due date/), { target: { value: '2027-06-01' } })
    expect(completeButton).not.toBeDisabled()
    fireEvent.click(completeButton)

    await waitFor(() => expect(onComplete).toHaveBeenCalled())
    expect(onComplete.mock.calls[0][0]).toMatchObject({ new_due_date: '2027-06-01' })
  })

  it('shows a computed next-due date and requires no input when interval_months is set', () => {
    const rule = makeRule({ interval_hours: null, fixed_due_date: '2026-06-01', interval_months: 12 })
    render(<MaintenanceCompleteDialog rule={rule} onCancel={vi.fn()} onComplete={vi.fn()} />)

    expect(screen.queryByLabelText(/New due date/)).not.toBeInTheDocument()
    expect(screen.getByText(/Next due:/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Complete' })).not.toBeDisabled()
  })
})
