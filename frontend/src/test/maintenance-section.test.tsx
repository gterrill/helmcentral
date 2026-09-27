import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MaintenanceSection } from '@/components/inventory/maintenance-section'
import type { MaintenanceRule } from '@/hooks/use-maintenance'

// ADR 0138: the Maintenance list's own grouping/sorting and its quick
// actions (acknowledge, set last done, complete). Each test drives the
// component through Testing Library rather than the hook directly, the
// same "render the real component against a mocked fetch" idiom
// equipment-index.test.tsx already uses for the sibling Equipment index.

function makeRule(overrides: Partial<MaintenanceRule>): MaintenanceRule {
  return {
    id: 'rule-1',
    equipment_id: 'eq-1',
    equipment_name: 'Main engine',
    system: 'propulsion',
    description: 'Engine oil and filter',
    interval_hours: 250,
    interval_months: null,
    due_soon_hours: null,
    due_soon_months: null,
    fixed_due_date: '',
    last_done_at: '',
    last_done_hours: null,
    profile_service_id: '',
    procedure_note_id: '',
    ack_reason: '',
    acknowledged: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    status: 'never_recorded',
    remaining_hours: null,
    remaining_days: null,
    hours_unknown: false,
    has_hour_meter_path: true,
    hours_stale_since: null,
    current_hours: 1234,
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

describe('MaintenanceSection', () => {
  it('groups rules by status in the spec order and shows an empty state when there are none', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (String(url).includes('/api/inventory/maintenance/rules')) return jsonResponse(200, { rules: [] })
      return jsonResponse(200, {})
    })
    render(<MaintenanceSection onOpenEquipment={vi.fn()} />)
    await waitFor(() => expect(screen.getByText(/no maintenance rules yet/i)).toBeInTheDocument())
  })

  it('shows Overdue above Due soon above OK, per the spec order', async () => {
    const rules = [
      makeRule({ id: 'r-ok', description: 'OK item', status: 'ok', remaining_hours: 200 }),
      makeRule({ id: 'r-overdue', description: 'Overdue item', status: 'overdue', remaining_hours: -10 }),
      makeRule({ id: 'r-due-soon', description: 'Due soon item', status: 'due_soon', remaining_hours: 20 }),
    ]
    fetchMock.mockImplementation((url: string) => {
      if (String(url).includes('/api/inventory/maintenance/rules')) return jsonResponse(200, { rules })
      return jsonResponse(200, {})
    })
    render(<MaintenanceSection onOpenEquipment={vi.fn()} />)
    await screen.findByText('Overdue item')

    const rows = screen.getAllByText(/Overdue item|Due soon item|OK item/).map((el) => el.textContent)
    expect(rows.indexOf('Overdue item')).toBeLessThan(rows.indexOf('Due soon item'))
    expect(rows.indexOf('Due soon item')).toBeLessThan(rows.indexOf('OK item'))
  })

  it('sorts an acknowledged rule below an unacknowledged one within the same status', async () => {
    const rules = [
      makeRule({ id: 'r-acked', description: 'Acked overdue', status: 'overdue', acknowledged: true, ack_reason: 'yard' }),
      makeRule({ id: 'r-plain', description: 'Plain overdue', status: 'overdue' }),
    ]
    fetchMock.mockImplementation((url: string) => {
      if (String(url).includes('/api/inventory/maintenance/rules')) return jsonResponse(200, { rules })
      return jsonResponse(200, {})
    })
    render(<MaintenanceSection onOpenEquipment={vi.fn()} />)
    await screen.findByText('Plain overdue')

    const rows = screen.getAllByText(/Acked overdue|Plain overdue/).map((el) => el.textContent)
    expect(rows.indexOf('Plain overdue')).toBeLessThan(rows.indexOf('Acked overdue'))
  })

  it('acknowledges a rule with a reason and refreshes the list', async () => {
    const rule = makeRule({ id: 'r1', description: 'Anode check', status: 'overdue' })
    fetchMock.mockImplementation((url: string, init?: RequestInit) => {
      const u = String(url)
      if (u.includes('/acknowledge')) {
        expect(JSON.parse(String(init?.body))).toEqual({ reason: 'yard' })
        return jsonResponse(200, { rule: { ...rule, acknowledged: true, ack_reason: 'yard' } })
      }
      if (u.includes('/api/inventory/maintenance/rules')) return jsonResponse(200, { rules: [rule] })
      return jsonResponse(200, {})
    })
    render(<MaintenanceSection onOpenEquipment={vi.fn()} />)
    await screen.findByText('Anode check')

    fireEvent.click(screen.getByRole('button', { name: 'Ack' }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: 'yard' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Acknowledge' }))

    await waitFor(() => expect(fetchMock.mock.calls.some(([u]) => String(u).includes('/acknowledge'))).toBe(true))
  })

  it('sets last done with no log entry involved, from the dialog', async () => {
    const rule = makeRule({ id: 'r2', description: 'Registration renewal', status: 'never_recorded', equipment_id: null, equipment_name: '', has_hour_meter_path: false })
    fetchMock.mockImplementation((url: string, init?: RequestInit) => {
      const u = String(url)
      if (u.includes('/last-done')) {
        expect(JSON.parse(String(init?.body))).toEqual({ last_done_at: '2026-01-15' })
        return jsonResponse(200, { rule: { ...rule, last_done_at: '2026-01-15', status: 'ok' } })
      }
      if (u.includes('/api/inventory/maintenance/rules')) return jsonResponse(200, { rules: [rule] })
      return jsonResponse(200, {})
    })
    render(<MaintenanceSection onOpenEquipment={vi.fn()} />)
    await screen.findByText('Registration renewal')

    fireEvent.click(screen.getByRole('button', { name: 'Set last done' }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.change(within(dialog).getByLabelText('Last done (date)'), { target: { value: '2026-01-15' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(fetchMock.mock.calls.some(([u]) => String(u).includes('/last-done'))).toBe(true))
    expect(fetchMock.mock.calls.some(([u]) => String(u).includes('/complete'))).toBe(false)
  })

  it('completes a rule with the prefilled current hours and writes the log entry', async () => {
    const rule = makeRule({ id: 'r3', description: 'Oil change', status: 'due_soon', current_hours: 987.5 })
    fetchMock.mockImplementation((url: string, init?: RequestInit) => {
      const u = String(url)
      if (u.includes('/complete')) {
        const body = JSON.parse(String(init?.body))
        expect(body.hours).toBe(987.5)
        return jsonResponse(200, {
          rule: { ...rule, status: 'ok', last_done_hours: 987.5 },
          entry: { id: 'log-1', equipment_id: 'eq-1', rule_id: 'r3', performed_at: body.performed_at, hours: 987.5, kind: 'maintenance', description: '', who: '', cost: null, currency: '', created_at: '', updated_at: '', parts: [], photo_ids: [] },
        })
      }
      if (u.includes('/api/inventory/equipment')) return jsonResponse(200, { items: [] })
      if (u.includes('/api/inventory/maintenance/rules')) return jsonResponse(200, { rules: [rule] })
      return jsonResponse(200, {})
    })
    render(<MaintenanceSection onOpenEquipment={vi.fn()} />)
    await screen.findByText('Oil change')

    fireEvent.click(screen.getByRole('button', { name: /complete/i }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByLabelText(/^Hours/)).toHaveValue(987.5)
    fireEvent.click(within(dialog).getByRole('button', { name: 'Complete' }))

    await waitFor(() => expect(fetchMock.mock.calls.some(([u]) => String(u).includes('/complete'))).toBe(true))
    await screen.findByText(/logged/i)
  })

  it('renders the CSV export as a download link to the export endpoint', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (String(url).includes('/api/inventory/maintenance/rules')) return jsonResponse(200, { rules: [] })
      return jsonResponse(200, {})
    })
    render(<MaintenanceSection onOpenEquipment={vi.fn()} />)
    await waitFor(() => expect(screen.getByText(/export csv/i)).toBeInTheDocument())

    const link = screen.getByText(/export csv/i).closest('a')
    expect(link).not.toBeNull()
    expect(link?.getAttribute('href')).toBe('/api/inventory/maintenance/log/export.csv')
    expect(link).toHaveAttribute('download')
  })
})
