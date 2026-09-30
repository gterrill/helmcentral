import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MaintenanceEquipmentBlock } from '@/components/inventory/maintenance-equipment-block'
import type { MaintenanceRule } from '@/hooks/use-maintenance'
import { ITEM_RULE_PROVENANCE, makeProfileJob } from './maintenance-fixtures'

// ADR 0138 / 0148: the equipment editor's own Maintenance block - the item's
// schedule grouped by where each rule comes from (its profile, or added by
// hand), plus the jobs that have left the profile and any profile error.

function makeRule(overrides: Partial<MaintenanceRule>): MaintenanceRule {
  return {
    id: 'rule-1', equipment_id: 'eq-1', equipment_name: 'Main engine', system: 'propulsion',
    description: 'Engine oil and filter', interval_hours: 250, interval_months: null,
    due_soon_hours: null, due_soon_months: null, fixed_due_date: '', last_done_at: '',
    last_done_hours: null, profile_service_id: 'engine-oil', procedure_note_id: '',
    ack_reason: '', acknowledged: false, created_at: '', updated_at: '',
    status: 'never_recorded', remaining_hours: null, remaining_days: null,
    hours_unknown: false, has_hour_meter_path: true, hours_as_of: null, current_hours: null,
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

describe('MaintenanceEquipmentBlock', () => {
  function mockSchedule(body: Record<string, unknown>) {
    fetchMock.mockImplementation((url: string) => {
      const u = String(url)
      if (u.includes('/api/equipment-profiles')) {
        return jsonResponse(200, { profiles: [{ id: 'cummins-qsb67-550', name: 'Cummins QSB 6.7' }], problems: [] })
      }
      if (u.includes('/maintenance/rules')) return jsonResponse(200, { rules: [], removed: [], schedule_errors: [], ...body })
      if (u.includes('/maintenance/log')) return jsonResponse(200, { entries: [] })
      if (u.includes('/maintenance/meter-resets')) return jsonResponse(200, { resets: [] })
      return jsonResponse(200, {})
    })
  }

  it('no longer offers Use profile schedule', async () => {
    mockSchedule({ rules: [makeProfileJob()] })
    render(<MaintenanceEquipmentBlock equipmentId="eq-1" hourMeterPath="propulsion.main.runTime" />)
    await screen.findByText('Engine oil and filter')
    expect(screen.queryByRole('button', { name: /use profile schedule/i })).not.toBeInTheDocument()
  })

  it('groups profile jobs under the profile name and hand rules under Added for this item', async () => {
    mockSchedule({ rules: [makeProfileJob(), makeRule({ id: 'hand-1', description: 'Wash the decks' })] })
    render(<MaintenanceEquipmentBlock equipmentId="eq-1" hourMeterPath="propulsion.main.runTime" />)

    expect(await screen.findByText('From profile: Cummins QSB 6.7')).toBeInTheDocument()
    expect(screen.getByText('Added for this item')).toBeInTheDocument()
    expect(screen.getByText('Engine oil and filter')).toBeInTheDocument()
    expect(screen.getByText('Wash the decks')).toBeInTheDocument()
  })

  it('marks an overridden job and names what differs from the profile', async () => {
    mockSchedule({ rules: [makeProfileJob({ interval_hours: 100, overridden_fields: ['interval_hours'] })] })
    render(<MaintenanceEquipmentBlock equipmentId="eq-1" hourMeterPath="x" />)

    const marker = await screen.findByText('Edited')
    expect(marker).toHaveAttribute('title', expect.stringMatching(/hours interval/i))
  })

  it('shows Interval not set with an action that opens the job to set it', async () => {
    mockSchedule({ rules: [makeProfileJob({ interval_hours: null, interval_months: null, status: 'interval_not_set', profile_values: { description: 'Engine oil and filter', interval_hours: null, interval_months: null } })] })
    render(<MaintenanceEquipmentBlock equipmentId="eq-1" hourMeterPath="x" />)

    expect(await screen.findByText('Interval not set')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Set interval for Engine oil and filter' }))
    expect(await screen.findByRole('heading', { name: 'Edit profile job' })).toBeInTheDocument()
  })

  it('mutes a job marked not applicable', async () => {
    mockSchedule({ rules: [makeProfileJob({ not_applicable: true, status: 'not_applicable', overridden_fields: ['not_applicable'] })] })
    render(<MaintenanceEquipmentBlock equipmentId="eq-1" hourMeterPath="x" />)
    expect(await screen.findByText('Not applicable')).toBeInTheDocument()
  })

  it('keeps jobs that left the profile in a collapsed group with Delete', async () => {
    const gone = makeProfileJob({ id: 'job:eq-1:old', description: 'Old zinc check', removed_from_profile: true, status: 'ok' })
    mockSchedule({ removed: [gone] })
    render(<MaintenanceEquipmentBlock equipmentId="eq-1" hourMeterPath="x" />)

    const toggle = await screen.findByRole('button', { name: /No longer in the profile \(1\)/ })
    expect(screen.queryByText('Old zinc check')).not.toBeInTheDocument()
    fireEvent.click(toggle)
    expect(screen.getByText('Old zinc check')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Delete Old zinc check' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(fetchMock.mock.calls.some(([u, init]) => String(u).includes('/rules/job%3Aeq-1%3Aold') && init?.method === 'DELETE')).toBe(true))
  })

  it('shows an explicit error, not jobs, when the profile is missing or invalid', async () => {
    mockSchedule({
      rules: [makeRule({ id: 'hand-1', description: 'Wash the decks' })],
      schedule_errors: [{ equipment_id: 'eq-1', equipment_name: 'Main engine', profile_id: 'gone-profile', error: 'profile not found' }],
    })
    render(<MaintenanceEquipmentBlock equipmentId="eq-1" hourMeterPath="x" />)

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent(/profile is missing or invalid/i)
    expect(alert).toHaveTextContent('gone-profile')
    expect(within(alert).getByRole('link', { name: /profiles/i })).toHaveAttribute('href', expect.stringContaining('/inventory/profiles'))
    expect(screen.queryByText(/From profile/)).not.toBeInTheDocument()
    expect(screen.getByText('Wash the decks')).toBeInTheDocument()
  })

  it('says so when there are no rules', async () => {
    mockSchedule({})
    render(<MaintenanceEquipmentBlock equipmentId="eq-1" hourMeterPath="" />)
    await waitFor(() => expect(screen.getByText(/no maintenance rules/i)).toBeInTheDocument())
  })

  // Rules counted in hours cannot count without a meter: say so, in the
  // block (amber alert semantics), keyed to the meter the editor currently
  // holds - including an unsaved edit.
  describe('missing hour meter warning', () => {
    function mockRules(rules: MaintenanceRule[]) {
      fetchMock.mockImplementation((url: string) => {
        const u = String(url)
        if (u.includes('/maintenance/rules')) return jsonResponse(200, { rules })
        if (u.includes('/maintenance/log')) return jsonResponse(200, { entries: [] })
        if (u.includes('/maintenance/meter-resets')) return jsonResponse(200, { resets: [] })
        return jsonResponse(200, {})
      })
    }

    it('warns when a rule counts hours and the item has no hour meter', async () => {
      mockRules([makeRule({ interval_hours: 250, interval_months: null })])
      render(<MaintenanceEquipmentBlock equipmentId="eq-1" hourMeterPath="" />)
      const warning = await screen.findByRole('status')
      expect(warning).toHaveTextContent("can't count hours until an hour meter is set")
      expect(warning).toHaveClass('text-amber-700')
    })

    it('does not warn when the item has an hour meter', async () => {
      mockRules([makeRule({ interval_hours: 250 })])
      render(<MaintenanceEquipmentBlock equipmentId="eq-1" hourMeterPath="propulsion.main.runTime" />)
      await screen.findByText('Engine oil and filter')
      expect(screen.queryByRole('status')).not.toBeInTheDocument()
    })

    it('does not warn when no rule has an hours interval', async () => {
      mockRules([makeRule({ interval_hours: null, interval_months: 12 })])
      render(<MaintenanceEquipmentBlock equipmentId="eq-1" hourMeterPath="" />)
      await screen.findByText('Engine oil and filter')
      expect(screen.queryByRole('status')).not.toBeInTheDocument()
    })
  })
})
