import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { HandHoursDialog } from '@/components/inventory/maintenance-hand-hours-dialog'
import { MaintenanceCompleteDialog } from '@/components/inventory/maintenance-log-dialogs'
import { MaintenanceLastDoneDialog } from '@/components/inventory/maintenance-quick-dialogs'
import { MaintenanceEquipmentBlock } from '@/components/inventory/maintenance-equipment-block'
import { MaintenanceSection } from '@/components/inventory/maintenance-section'
import { handReadLabel, type MaintenanceRule } from '@/hooks/use-maintenance'
import { todayISO } from '@/lib/local-date'
import { ITEM_RULE_PROVENANCE } from './maintenance-fixtures'

// ADR 0268: hand-read hours. An item with no hour meter takes its current
// hours from the figure the operator last read off the gauge, and every place
// the live reading's age shows says "Read by hand" instead.

vi.mock('@/hooks/use-inventory', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/hooks/use-inventory')>()
  return { ...actual, useEquipment: () => ({ items: [], loading: false, error: null, refresh: vi.fn() }) }
})

function daysAgo(n: number): string {
  const d = new Date()
  d.setDate(d.getDate() - n)
  return todayISO(d)
}

function makeRule(overrides: Partial<MaintenanceRule> = {}): MaintenanceRule {
  return {
    id: 'rule-1', equipment_id: 'eq-1', equipment_name: 'Watermaker', system: 'water',
    description: 'Filter change', interval_hours: 100, interval_months: null,
    due_soon_hours: null, due_soon_months: null, fixed_due_date: '', last_done_at: '',
    last_done_hours: null, profile_service_id: '', procedure_note_id: '',
    ack_reason: '', acknowledged: false, created_at: '', updated_at: '',
    status: 'ok', remaining_hours: 20, remaining_days: null,
    hours_unknown: false, has_hour_meter_path: false, hours_as_of: null,
    current_hours: 120, hours_by_hand: true, hours_read_on: daysAgo(21),
    ...ITEM_RULE_PROVENANCE,
    ...overrides,
  }
}

const fetchMock = vi.fn()
function jsonResponse(status: number, body: unknown) {
  return Promise.resolve({ ok: status >= 200 && status < 300, status, json: async () => body } as Response)
}

beforeEach(() => {
  fetchMock.mockReset()
  vi.stubGlobal('fetch', fetchMock)
})

describe('handReadLabel', () => {
  const today = '2026-11-07'
  it.each([
    ['2026-11-07', 'Read by hand, today'],
    ['2026-11-06', 'Read by hand, yesterday'],
    ['2026-11-04', 'Read by hand, 3 days ago'],
    ['2026-10-17', 'Read by hand, 3 weeks ago'],
    ['2026-07-07', 'Read by hand, 4 months ago'],
  ])('%s reads %s', (readOn, label) => {
    expect(handReadLabel(readOn, today)).toBe(label)
  })

  it('says nothing for a missing or future date', () => {
    expect(handReadLabel('', today)).toBeNull()
    expect(handReadLabel('2026-11-09', today)).toBeNull()
  })
})

describe('MaintenanceCompleteDialog with a hand reading', () => {
  it('prefills the figure and labels it as read by hand, never as live', () => {
    render(<MaintenanceCompleteDialog rule={makeRule()} onCancel={vi.fn()} onComplete={vi.fn()} />)
    expect(screen.getByLabelText(/^Hours/)).toHaveValue(120)
    expect(screen.getByText('Read by hand, 3 weeks ago.')).toBeInTheDocument()
    expect(screen.queryByText(/Live reading/)).not.toBeInTheDocument()
  })
})

describe('HandHoursDialog', () => {
  it('keeps the date, gauge figure and note, and reports the saved reading', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined)
    render(<HandHoursDialog equipmentName="Watermaker" open onCancel={vi.fn()} onSave={onSave} />)

    const save = screen.getByRole('button', { name: 'Save reading' })
    expect(save).toBeDisabled()
    expect(screen.getByLabelText('Date read')).toHaveValue(todayISO())

    fireEvent.change(screen.getByLabelText('Gauge reading (hours)'), { target: { value: '120.5' } })
    fireEvent.change(screen.getByLabelText('Date read'), { target: { value: '2026-11-01' } })
    fireEvent.change(screen.getByLabelText('Note (optional)'), { target: { value: 'At the panel' } })
    fireEvent.click(save)

    await waitFor(() => expect(onSave).toHaveBeenCalledWith({ read_on: '2026-11-01', gauge: 120.5, note: 'At the panel' }))
  })

  it('shows the server refusal and stays open', async () => {
    const onSave = vi.fn().mockRejectedValue(new Error('this item has an hour meter'))
    render(<HandHoursDialog equipmentName="Main engine" open onCancel={vi.fn()} onSave={onSave} />)
    fireEvent.change(screen.getByLabelText('Gauge reading (hours)'), { target: { value: '5' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save reading' }))
    expect(await screen.findByText('this item has an hour meter')).toBeInTheDocument()
  })
})

describe('MaintenanceLastDoneDialog', () => {
  it('offers hours for an hours rule on an item with no meter, since they count as a hand reading', () => {
    render(<MaintenanceLastDoneDialog rule={makeRule({ status: 'never_recorded' })} onCancel={vi.fn()} onConfirm={vi.fn()} />)
    expect(screen.getByLabelText(/Last done \(hours/)).toBeInTheDocument()
  })

  it('does not offer hours for a calendar-only rule on an item with no meter', () => {
    render(<MaintenanceLastDoneDialog rule={makeRule({ interval_hours: null, interval_months: 12 })} onCancel={vi.fn()} onConfirm={vi.fn()} />)
    expect(screen.queryByLabelText(/Last done \(hours/)).not.toBeInTheDocument()
  })
})

describe('MaintenanceSection', () => {
  function mockRules(rules: MaintenanceRule[]) {
    fetchMock.mockImplementation((url: string, init?: RequestInit) => {
      const u = String(url)
      if (u.includes('/maintenance/hour-readings') && init?.method === 'POST') {
        return jsonResponse(201, { reading: { id: 'h1' } })
      }
      if (u.includes('/api/inventory/maintenance/rules')) return jsonResponse(200, { rules })
      return jsonResponse(200, {})
    })
  }

  it('shows the hand-read age in the list and offers Log a reading on an item with no meter', async () => {
    mockRules([makeRule()])
    render(<MaintenanceSection onOpenEquipment={vi.fn()} />)
    await screen.findByText('Filter change')
    expect(screen.getByText('Read by hand, 3 weeks ago')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Log a reading' }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.change(within(dialog).getByLabelText('Gauge reading (hours)'), { target: { value: '130' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save reading' }))

    await waitFor(() => {
      const post = fetchMock.mock.calls.find(([u, init]) => String(u).includes('/equipment/eq-1/maintenance/hour-readings') && (init as RequestInit)?.method === 'POST')
      expect(post).toBeTruthy()
      expect(JSON.parse(String((post![1] as RequestInit).body))).toMatchObject({ gauge: 130, read_on: todayISO() })
      expect(String(post![0])).toContain(`today=${todayISO()}`)
    })
  })

  it('does not offer Log a reading on an item that has an hour meter', async () => {
    mockRules([makeRule({ has_hour_meter_path: true, hours_by_hand: false, hours_read_on: '', hours_as_of: new Date().toISOString() })])
    render(<MaintenanceSection onOpenEquipment={vi.fn()} />)
    await screen.findByText('Filter change')
    expect(screen.queryByRole('button', { name: 'Log a reading' })).not.toBeInTheDocument()
    expect(screen.queryByText(/Read by hand/)).not.toBeInTheDocument()
  })

  it('does not offer Log a reading on a rule with no item', async () => {
    mockRules([makeRule({ equipment_id: null, equipment_name: '', interval_hours: null, interval_months: 12, current_hours: null, hours_by_hand: false, hours_read_on: '' })])
    render(<MaintenanceSection onOpenEquipment={vi.fn()} />)
    await screen.findByText('Filter change')
    expect(screen.queryByRole('button', { name: 'Log a reading' })).not.toBeInTheDocument()
  })

  it('asks for a first reading when hours are unknown on an item with no meter', async () => {
    mockRules([makeRule({ status: 'hours_unknown', hours_unknown: true, current_hours: null, hours_by_hand: false, hours_read_on: '', remaining_hours: null })])
    render(<MaintenanceSection onOpenEquipment={vi.fn()} />)
    await screen.findByText('Filter change')
    expect(screen.getByText('No hour meter and no reading logged')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Log a reading' })).toBeInTheDocument()
  })
})

describe('MaintenanceEquipmentBlock', () => {
  function mockItem(readings: unknown[]) {
    fetchMock.mockImplementation((url: string, init?: RequestInit) => {
      const u = String(url)
      if (u.includes('/api/equipment-profiles')) return jsonResponse(200, { profiles: [], problems: [] })
      if (u.includes('/maintenance/hour-readings') && init?.method === 'DELETE') return Promise.resolve({ ok: true, status: 204, json: async () => ({}) } as Response)
      if (u.includes('/maintenance/hour-readings')) return jsonResponse(200, { readings })
      if (u.includes('/maintenance/rules')) return jsonResponse(200, { rules: [makeRule()], removed: [], schedule_errors: [] })
      if (u.includes('/maintenance/log')) return jsonResponse(200, { entries: [] })
      return jsonResponse(200, {})
    })
  }
  const reading = { id: 'h1', equipment_id: 'eq-1', read_on: daysAgo(21), gauge: 120, hours: 120, note: 'At the panel', created_at: '' }

  it('lists hand readings and offers Log a reading when the item has no meter', async () => {
    mockItem([reading])
    render(<MaintenanceEquipmentBlock equipmentId="eq-1" hourMeterSource="" />)
    expect(await screen.findByText(/At the panel/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Log a reading' })).toBeInTheDocument()
    expect(screen.queryByText(/can't count hours until/)).not.toBeInTheDocument()
  })

  it('deletes a reading', async () => {
    mockItem([reading])
    render(<MaintenanceEquipmentBlock equipmentId="eq-1" hourMeterSource="" />)
    fireEvent.click(await screen.findByRole('button', { name: /Delete reading/ }))
    fireEvent.click(await screen.findByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(fetchMock.mock.calls.some(([u, i]) => String(u).includes('/hour-readings/h1') && (i as RequestInit)?.method === 'DELETE')).toBe(true))
  })

  it('offers no hand readings on an item that has a meter', async () => {
    mockItem([])
    render(<MaintenanceEquipmentBlock equipmentId="eq-1" hourMeterSource="propulsion.main.runTime" />)
    await screen.findByText('Filter change')
    expect(screen.queryByRole('button', { name: 'Log a reading' })).not.toBeInTheDocument()
    expect(fetchMock.mock.calls.some(([u]) => String(u).includes('/hour-readings'))).toBe(false)
  })
})
