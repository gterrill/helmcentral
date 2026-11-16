import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'

import { MaintenanceEquipmentBlock } from '@/components/inventory/maintenance-equipment-block'
import { MaintenanceCompleteDialog, MaintenanceLogEntryDialog } from '@/components/inventory/maintenance-log-dialogs'
import { OpenJobsGroup } from '@/components/inventory/open-jobs-group'
import { handReadAge, handReadLabel, hoursAgeLabel, type MaintenanceRule } from '@/hooks/use-maintenance'
import type { OpenJob } from '@/hooks/use-open-jobs'
import { stubApi } from '@/test/helpers/stub-api'
import { ITEM_RULE_PROVENANCE } from './maintenance-fixtures'

// The maintenance log's date, an open job's age and "(passed)" marker, and a
// hand-read reading's age all follow the boat's calendar day, never the
// device's. With the boat's day unknown the date is blank (Save held) and the
// ages and the marker are left out: no device-date stand-in.

let boatDay: string | null = '2026-11-16'
vi.mock('@/hooks/use-vessel-clock', () => ({ useBoatDayKey: () => boatDay }))
vi.mock('@/hooks/use-inventory', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/hooks/use-inventory')>()
  return { ...actual, useEquipment: () => ({ items: [], loading: false, error: null, refresh: vi.fn() }) }
})

beforeEach(() => {
  boatDay = '2026-11-16'
  // The device is already on the 17th while the boat is still on the 16th.
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(2026, 10, 17, 9, 0, 0))
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

function makeRule(overrides: Partial<MaintenanceRule> = {}): MaintenanceRule {
  return {
    id: 'rule-1', equipment_id: 'eq-1', equipment_name: 'Main engine', system: 'propulsion',
    description: 'Oil change', interval_hours: null, interval_months: 12,
    due_soon_hours: null, due_soon_months: null, fixed_due_date: '2027-01-01', last_done_at: '',
    last_done_hours: null, profile_service_id: '', procedure_note_id: '',
    ack_reason: '', acknowledged: false, created_at: '', updated_at: '',
    status: 'due_soon', remaining_hours: null, remaining_days: null,
    hours_unknown: false, has_hour_meter_path: true, hours_as_of: null, current_hours: null, hours_by_hand: false, hours_read_on: '',
    ...ITEM_RULE_PROVENANCE,
    ...overrides,
  }
}

function makeJob(overrides: Partial<OpenJob> = {}): OpenJob {
  return {
    id: 'job-1', equipment_id: 'eq-1', equipment_name: 'Shower pump', title: 'Leaks at the outlet', notes: '',
    opened_on: '2026-11-06', before_on: '', status: 'open', closed_on: '', drop_reason: '', log_entry_id: null,
    source: 'manual', source_link: '', source_label: '', created_at: '', updated_at: '', photo_ids: [],
    ...overrides,
  }
}

describe('MaintenanceLogEntryDialog on the boat day', () => {
  it('starts the date on the boat day and saves it', async () => {
    const onSave = vi.fn().mockResolvedValue({})
    render(<MaintenanceLogEntryDialog entry={null} equipmentId="eq-1" open onCancel={vi.fn()} onSave={onSave} />)
    expect(screen.getByLabelText('Date')).toHaveValue('2026-11-16')
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(onSave).toHaveBeenCalledTimes(1))
    expect(onSave.mock.calls[0][0]).toMatchObject({ performed_at: '2026-11-16' })
  })

  it('leaves the date blank and holds Save until the boat day is known, then fills it', () => {
    boatDay = null
    const props = { entry: null, equipmentId: 'eq-1', open: true, onCancel: vi.fn(), onSave: vi.fn() }
    const { rerender } = render(<MaintenanceLogEntryDialog {...props} />)
    expect(screen.getByLabelText('Date')).toHaveValue('')
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
    boatDay = '2026-11-16'
    rerender(<MaintenanceLogEntryDialog {...props} />)
    expect(screen.getByLabelText('Date')).toHaveValue('2026-11-16')
    expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled()
  })

  it('holds the saved date still when the boat day turns while the dialog stays open', async () => {
    const onSave = vi.fn().mockResolvedValue({ id: 'log-1', photo_ids: [], parts: [] })
    const props = { entry: null, equipmentId: 'eq-1', open: true, onCancel: vi.fn(), onSave }
    const { rerender } = render(<MaintenanceLogEntryDialog {...props} />)
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(onSave).toHaveBeenCalledTimes(1))
    boatDay = '2026-11-17'
    rerender(<MaintenanceLogEntryDialog {...props} />)
    expect(screen.getByLabelText('Date')).toHaveValue('2026-11-16')
  })

  it('keeps a date the operator typed', () => {
    const props = { entry: null, equipmentId: 'eq-1', open: true, onCancel: vi.fn(), onSave: vi.fn() }
    const { rerender } = render(<MaintenanceLogEntryDialog {...props} />)
    fireEvent.change(screen.getByLabelText('Date'), { target: { value: '2026-11-10' } })
    boatDay = '2026-11-17'
    rerender(<MaintenanceLogEntryDialog {...props} />)
    expect(screen.getByLabelText('Date')).toHaveValue('2026-11-10')
  })
})

describe('MaintenanceCompleteDialog on the boat day', () => {
  it('starts the date on the boat day and counts the next due date from it', () => {
    render(<MaintenanceCompleteDialog rule={makeRule()} onCancel={vi.fn()} onComplete={vi.fn()} />)
    expect(screen.getByLabelText('Date')).toHaveValue('2026-11-16')
    expect(screen.getByText('Next due: 2027-11-16')).toBeInTheDocument()
  })

  it('leaves the date blank, holds Complete and shows no next due date while the boat day is unknown', () => {
    boatDay = null
    render(<MaintenanceCompleteDialog rule={makeRule()} onCancel={vi.fn()} onComplete={vi.fn()} />)
    expect(screen.getByLabelText('Date')).toHaveValue('')
    expect(screen.getByRole('button', { name: 'Complete' })).toBeDisabled()
    expect(screen.queryByText(/Next due:/)).not.toBeInTheDocument()
  })
})

describe('OpenJobsGroup on the boat day', () => {
  const JOBS = 'GET /api/inventory/open-jobs'

  it('counts the age and the passed marker from the boat day, not the device', async () => {
    stubApi({ [JOBS]: { jobs: [makeJob({ before_on: '2026-11-16' })] } })
    render(<OpenJobsGroup onOpenEquipment={vi.fn()} />)
    expect(await screen.findByText('10 d')).toBeInTheDocument()
    expect(screen.getByText('16 Nov')).toBeInTheDocument()
  })

  it('shows no age and no passed marker while the boat day is unknown', async () => {
    boatDay = null
    stubApi({ [JOBS]: { jobs: [makeJob({ before_on: '2026-11-01' })] } })
    render(<OpenJobsGroup onOpenEquipment={vi.fn()} />)
    expect(await screen.findByText('Leaks at the outlet')).toBeInTheDocument()
    expect(screen.queryByText(/\d+ d$/)).not.toBeInTheDocument()
    expect(screen.getByText('1 Nov')).toBeInTheDocument()
    expect(screen.queryByText(/passed/)).not.toBeInTheDocument()
  })
})

describe('OpenJobsGroup closed jobs without a boat day', () => {
  it('still gives a closed job the age it had when it closed', async () => {
    boatDay = null
    stubApi({ 'GET /api/inventory/open-jobs': { jobs: [makeJob({ status: 'done', opened_on: '2026-11-01', closed_on: '2026-11-04' })] } })
    render(<OpenJobsGroup onOpenEquipment={vi.fn()} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Show closed' }))
    expect(await screen.findByText(/open 3 d/)).toBeInTheDocument()
  })
})

describe('hand-read age', () => {
  it('needs the day it is counted from and says nothing without one', () => {
    expect(handReadAge('2026-11-15', '2026-11-16')).toBe('yesterday')
    expect(handReadAge('2026-11-15', null)).toBeNull()
    expect(hoursAgeLabel(makeRule({ hours_by_hand: true, hours_read_on: '2026-11-15' }), '2026-11-16')).toBe('Read by hand, yesterday')
    // The figure is still marked as typed, never passed off as live.
    expect(hoursAgeLabel(makeRule({ hours_by_hand: true, hours_read_on: '2026-11-15' }), null)).toBe('Read by hand')
    expect(handReadLabel('', null)).toBeNull()
  })

  it('counts a reading in the equipment block from the boat day, and shows no age while it is unknown', async () => {
    const readings = { readings: [{ id: 'r1', equipment_id: 'eq-1', read_on: '2026-11-15', gauge: 130, hours: 130, note: '', created_at: '' }] }
    const routes = {
      'GET /api/equipment-profiles': { profiles: [], problems: [] },
      'GET /api/inventory/equipment/eq-1/maintenance/rules': { rules: [], removed: [], schedule_errors: [] },
      'GET /api/inventory/equipment/eq-1/maintenance/log': { entries: [] },
      'GET /api/inventory/equipment/eq-1/maintenance/hour-readings': readings,
    }
    stubApi(routes)
    const { unmount } = render(<MaintenanceEquipmentBlock equipmentId="eq-1" hourMeterSource="" />)
    expect(await screen.findByText(/2026-11-15 \(yesterday\)/)).toBeInTheDocument()
    unmount()

    boatDay = null
    stubApi(routes)
    render(<MaintenanceEquipmentBlock equipmentId="eq-1" hourMeterSource="" />)
    const line = await screen.findByText(/2026-11-15/)
    expect(line.textContent).not.toMatch(/\(/)
  })
})
