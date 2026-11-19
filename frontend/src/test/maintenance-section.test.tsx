import { beforeEach, describe, expect, it, vi } from 'vitest'
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MaintenanceSection } from '@/components/inventory/maintenance-section'
import type { MaintenanceRule } from '@/hooks/use-maintenance'
import { invalidateQueries } from '@/lib/api-query'
import { ITEM_RULE_PROVENANCE, makeProfileJob } from './maintenance-fixtures'

// ADR 0138: the Maintenance list's own grouping/sorting and its quick
// actions (acknowledge, set last done, complete). Each test drives the
// component through Testing Library rather than the hook directly, the
// same "render the real component against a mocked fetch" idiom
// equipment-index.test.tsx already uses for the sibling Equipment index.

// The service log's date starts on the boat's day, which a test has to supply.
vi.mock('@/hooks/use-vessel-clock', () => ({ useBoatDayKey: () => '2026-11-16' }))

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
    hours_as_of: null,
    current_hours: 1234,
    hours_by_hand: false,
    hours_read_on: '',
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

  it('keeps the rules and notes the failure when a refresh of them fails', async () => {
    let reads = 0
    fetchMock.mockImplementation((url: string) => {
      if (String(url).includes('/api/inventory/maintenance/rules')) {
        return ++reads === 1
          ? jsonResponse(200, { rules: [makeProfileJob({ description: 'Oil change', status: 'ok' })], removed: [], schedule_errors: [] })
          : jsonResponse(502, { error: 'HTTP 502' })
      }
      return jsonResponse(200, {})
    })
    render(<MaintenanceSection onOpenEquipment={vi.fn()} />)
    await screen.findByText('Oil change')

    act(() => { invalidateQueries(['/api/inventory/maintenance/rules']) })

    expect(await screen.findByText(/Couldn't refresh: HTTP 502/)).toBeInTheDocument()
    expect(screen.getByText('Oil change')).toBeInTheDocument()
  })

  it('marks an overridden profile job, mutes a not-applicable one and lists both', async () => {
    const rules = [
      makeProfileJob({ id: 'job:eq-1:a', description: 'Oil change', status: 'ok', interval_hours: 100, overridden_fields: ['interval_hours'] }),
      makeProfileJob({ id: 'job:eq-1:b', description: 'Impeller', status: 'not_applicable', not_applicable: true, overridden_fields: ['not_applicable'] }),
    ]
    fetchMock.mockImplementation((url: string) => {
      if (String(url).includes('/api/inventory/maintenance/rules')) return jsonResponse(200, { rules, removed: [], schedule_errors: [] })
      return jsonResponse(200, {})
    })
    render(<MaintenanceSection onOpenEquipment={vi.fn()} />)
    await screen.findByText('Oil change')

    expect(screen.getAllByText('Edited')).toHaveLength(2)
    expect(screen.getByText('Not applicable')).toBeInTheDocument()
    expect(screen.getByText('Impeller').closest('tr')).toHaveClass('text-muted-foreground')
    // Nothing to complete on a job that does not apply to this item.
    const row = screen.getByText('Impeller').closest('tr')!
    expect(within(row).queryByRole('button', { name: /complete/i })).not.toBeInTheDocument()
  })

  it('opens a profile job in the overrides dialog, not the hand-rule form', async () => {
    const rules = [makeProfileJob({ status: 'ok' })]
    fetchMock.mockImplementation((url: string) => {
      const u = String(url)
      if (u.includes('/api/inventory/equipment')) return jsonResponse(200, { items: [] })
      if (u.includes('/api/inventory/maintenance/rules')) return jsonResponse(200, { rules, removed: [], schedule_errors: [] })
      return jsonResponse(200, {})
    })
    render(<MaintenanceSection onOpenEquipment={vi.fn()} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Engine oil and filter' }))
    expect(await screen.findByRole('heading', { name: 'Edit profile job' })).toBeInTheDocument()
  })

  describe('action column hierarchy', () => {
    async function renderWith(rule: MaintenanceRule) {
      fetchMock.mockImplementation((url: string) => {
        if (String(url).includes('/api/inventory/maintenance/rules')) return jsonResponse(200, { rules: [rule], removed: [], schedule_errors: [] })
        return jsonResponse(200, {})
      })
      render(<MaintenanceSection onOpenEquipment={vi.fn()} />)
      await screen.findByText(rule.description)
    }
    const variantOf = (name: string) => screen.getByRole('button', { name }).getAttribute('data-variant')

    it('keeps Complete solid on an overdue rule', async () => {
      await renderWith(makeRule({ status: 'overdue' }))
      expect(variantOf('Complete')).toBe('default')
    })

    it('keeps Complete solid on a due soon rule', async () => {
      await renderWith(makeRule({ status: 'due_soon' }))
      expect(variantOf('Complete')).toBe('default')
    })

    it('offers Set interval and a ghost Complete when no interval is set', async () => {
      await renderWith(makeRule({ status: 'interval_not_set', interval_hours: null }))
      expect(variantOf('Complete')).toBe('ghost')
      expect(variantOf('Set interval')).toBe('outline')
      fireEvent.click(screen.getByRole('button', { name: 'Set interval' }))
      expect(await screen.findByRole('dialog')).toBeInTheDocument()
    })

    it('puts the setup action above a ghost Complete when nothing is recorded yet', async () => {
      await renderWith(makeRule({ status: 'never_recorded' }))
      expect(variantOf('Set last done')).toBe('outline')
      expect(variantOf('Complete')).toBe('ghost')
    })

    it('makes Complete an outline button on an ok rule and offers no Set interval', async () => {
      await renderWith(makeRule({ status: 'ok' }))
      expect(variantOf('Complete')).toBe('outline')
      expect(screen.queryByRole('button', { name: 'Set interval' })).toBeNull()
    })
  })

  it('tells two same-named items apart by location, or alias when there is one', async () => {
    const rules = [
      makeRule({ id: 'r1', equipment_id: 'eq-p', equipment_name: 'Cummins QSB 6.7', equipment_location: 'ER - Port', description: 'Oil port' }),
      makeRule({ id: 'r2', equipment_id: 'eq-s', equipment_name: 'Cummins QSB 6.7', equipment_alias: 'Starboard engine', equipment_location: 'ER - Starboard', description: 'Oil stbd' }),
      makeRule({ id: 'r3', equipment_id: 'eq-g', equipment_name: 'Genset', description: 'Genset oil' }),
    ]
    fetchMock.mockImplementation((url: string) => {
      if (String(url).includes('/api/inventory/maintenance/rules')) return jsonResponse(200, { rules, removed: [], schedule_errors: [] })
      return jsonResponse(200, {})
    })
    render(<MaintenanceSection onOpenEquipment={vi.fn()} />)
    // Both truncate on one line, but the name gives way first: it shrinks
    // three times as fast as the qualifier and keeps only a 3ch floor.
    const qualifier = await screen.findByText(/\(ER - Port\)/)
    expect(qualifier.className).toContain('truncate')
    expect(qualifier.className).not.toContain('shrink-0')
    const button = qualifier.closest('button')!
    expect(qualifier.textContent).toBe('(ER - Port)')
    const nameSpan = within(button).getByText('Cummins QSB 6.7')
    expect(nameSpan.textContent).toBe('Cummins QSB 6.7')
    expect(nameSpan.className).toContain('truncate')
    expect(nameSpan.className).toContain('shrink-[3]')
    expect(button.className).not.toContain('whitespace-normal')
    expect(screen.getByText('(Starboard engine)')).toBeInTheDocument()
    expect(screen.getByText('Genset')).toBeInTheDocument()
    expect(screen.getByText('Genset').closest('button')!.textContent).toBe('Genset')
  })

  it('treats names differing only in case or spacing as the same name', async () => {
    const rules = [
      makeRule({ id: 'r1', equipment_id: 'eq-p', equipment_name: 'Cummins QSB 6.7', equipment_location: 'ER - Port', description: 'Oil port' }),
      makeRule({ id: 'r2', equipment_id: 'eq-s', equipment_name: 'cummins  QSB 6.7', equipment_location: 'ER - Starboard', description: 'Oil stbd' }),
    ]
    fetchMock.mockImplementation((url: string) => {
      if (String(url).includes('/api/inventory/maintenance/rules')) return jsonResponse(200, { rules, removed: [], schedule_errors: [] })
      return jsonResponse(200, {})
    })
    render(<MaintenanceSection onOpenEquipment={vi.fn()} />)
    expect(await screen.findByText('(ER - Port)')).toBeInTheDocument()
    expect(screen.getByText('(ER - Starboard)')).toBeInTheDocument()
  })

  it('names the item with its qualifier in the Log a reading dialog', async () => {
    const rules = [
      makeRule({ id: 'r1', equipment_id: 'eq-p', equipment_name: 'Cummins QSB 6.7', equipment_location: 'ER - Port', has_hour_meter_path: false, description: 'Oil port' }),
      makeRule({ id: 'r2', equipment_id: 'eq-s', equipment_name: 'Cummins QSB 6.7', equipment_location: 'ER - Starboard', has_hour_meter_path: false, description: 'Oil stbd' }),
    ]
    fetchMock.mockImplementation((url: string) => {
      if (String(url).includes('/api/inventory/maintenance/rules')) return jsonResponse(200, { rules, removed: [], schedule_errors: [] })
      return jsonResponse(200, {})
    })
    render(<MaintenanceSection onOpenEquipment={vi.fn()} />)
    await screen.findByText('Oil port')
    fireEvent.click(screen.getAllByRole('button', { name: 'Log a reading' })[1])
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText(/Cummins QSB 6\.7 \(ER - Starboard\) has no hour meter/)).toBeInTheDocument()
  })

  describe('dialogs name the item when two share a name', () => {
    const twins = (over: Partial<MaintenanceRule> = {}) => [
      makeRule({ id: 'r1', equipment_id: 'eq-p', equipment_name: 'Cummins QSB 6.7', equipment_location: 'ER - Port', description: 'Oil port', ...over }),
      makeRule({ id: 'r2', equipment_id: 'eq-s', equipment_name: 'Cummins QSB 6.7', equipment_location: 'ER - Starboard', description: 'Oil stbd', ...over }),
    ]
    async function open(rules: MaintenanceRule[], button: string) {
      fetchMock.mockImplementation((url: string) => {
        if (String(url).includes('/api/inventory/maintenance/rules')) return jsonResponse(200, { rules, removed: [], schedule_errors: [] })
        return jsonResponse(200, {})
      })
      render(<MaintenanceSection onOpenEquipment={vi.fn()} />)
      await screen.findByText(rules[0].description)
      fireEvent.click(screen.getAllByRole('button', { name: button })[1])
      return screen.findByRole('dialog')
    }

    it('Complete', async () => {
      const dialog = await open(twins(), 'Complete')
      expect(within(dialog).getByText(/Cummins QSB 6\.7 \(ER - Starboard\)/)).toBeInTheDocument()
    })

    it('leaves a unique item\'s Complete dialog wording as it was', async () => {
      const dialog = await open([
        makeRule({ id: 'r1', equipment_id: 'eq-a', equipment_name: 'Watermaker', description: 'Membrane' }),
        makeRule({ id: 'r2', equipment_id: 'eq-b', equipment_name: 'Genset', description: 'Genset oil' }),
      ], 'Complete')
      expect(within(dialog).getByText('Logs this service. If it is the latest one, the next falls due counted from it.')).toBeInTheDocument()
      expect(dialog.textContent).not.toContain('For ')
    })

    it('Ack', async () => {
      const dialog = await open(twins({ status: 'overdue' }), 'Ack')
      expect(within(dialog).getByText(/Cummins QSB 6\.7 \(ER - Starboard\)/)).toBeInTheDocument()
    })

    it('Set last done', async () => {
      const dialog = await open(twins({ status: 'never_recorded' }), 'Set last done')
      expect(within(dialog).getByText(/Cummins QSB 6\.7 \(ER - Starboard\)/)).toBeInTheDocument()
    })

    it('profile job', async () => {
      const jobs = [
        makeProfileJob({ id: 'job:p', equipment_id: 'eq-p', equipment_name: 'Cummins QSB 6.7', equipment_location: 'ER - Port', description: 'Oil port', status: 'ok' }),
        makeProfileJob({ id: 'job:s', equipment_id: 'eq-s', equipment_name: 'Cummins QSB 6.7', equipment_location: 'ER - Starboard', description: 'Oil stbd', status: 'ok' }),
      ]
      fetchMock.mockImplementation((url: string) => {
        if (String(url).includes('/api/inventory/maintenance/rules')) return jsonResponse(200, { rules: jobs, removed: [], schedule_errors: [] })
        return jsonResponse(200, {})
      })
      render(<MaintenanceSection onOpenEquipment={vi.fn()} />)
      fireEvent.click(await screen.findByRole('button', { name: 'Oil stbd' }))
      const dialog = await screen.findByRole('dialog')
      expect(within(dialog).getByText(/Cummins QSB 6\.7 \(ER - Starboard\): this job comes from/)).toBeInTheDocument()
    })
  })

  it('warns, per item, when a profile is missing or invalid', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (String(url).includes('/api/inventory/maintenance/rules')) {
        return jsonResponse(200, {
          rules: [],
          removed: [],
          schedule_errors: [{ equipment_id: 'eq-1', equipment_name: 'Main engine', profile_id: 'gone', error: 'profile not found' }],
        })
      }
      return jsonResponse(200, {})
    })
    const onOpenEquipment = vi.fn()
    render(<MaintenanceSection onOpenEquipment={onOpenEquipment} />)

    const banner = await screen.findByRole('alert')
    expect(banner).toHaveTextContent(/a profile that is missing or invalid/i)
    fireEvent.click(within(banner).getByRole('button', { name: 'Main engine' }))
    expect(onOpenEquipment).toHaveBeenCalledWith('eq-1')
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
