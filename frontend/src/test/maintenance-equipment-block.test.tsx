import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MaintenanceEquipmentBlock } from '@/components/inventory/maintenance-equipment-block'
import type { MaintenanceRule } from '@/hooks/use-maintenance'

// ADR 0138: the equipment editor's own Maintenance block - "Use profile
// schedule" is the one action unique to this surface (the plain
// Maintenance list has no per-item "copy from profile" button), so this is
// where it gets exercised.

function makeRule(overrides: Partial<MaintenanceRule>): MaintenanceRule {
  return {
    id: 'rule-1', equipment_id: 'eq-1', equipment_name: 'Main engine', system: 'propulsion',
    description: 'Engine oil and filter', interval_hours: 250, interval_months: null,
    due_soon_hours: null, due_soon_months: null, fixed_due_date: '', last_done_at: '',
    last_done_hours: null, profile_service_id: 'engine-oil', procedure_note_id: '',
    ack_reason: '', acknowledged: false, created_at: '', updated_at: '',
    status: 'never_recorded', remaining_hours: null, remaining_days: null,
    hours_unknown: false, has_hour_meter_path: true, hours_stale_since: null, current_hours: null,
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
  it('offers Use profile schedule only when the item has a profile, and copies it', async () => {
    let rulesCallCount = 0
    fetchMock.mockImplementation((url: string) => {
      const u = String(url)
      if (u.includes('/copy-profile-schedule')) {
        return jsonResponse(201, { rules: [makeRule({})] })
      }
      if (u.includes('/maintenance/rules')) {
        rulesCallCount += 1
        return jsonResponse(200, { rules: rulesCallCount > 1 ? [makeRule({})] : [] })
      }
      if (u.includes('/maintenance/log')) return jsonResponse(200, { entries: [] })
      if (u.includes('/maintenance/meter-resets')) return jsonResponse(200, { resets: [] })
      return jsonResponse(200, {})
    })

    render(<MaintenanceEquipmentBlock equipmentId="eq-1" profileId="cummins-qsb67-550" hourMeterPath="propulsion.main.runTime" />)

    const copyButton = await screen.findByRole('button', { name: /use profile schedule/i })
    fireEvent.click(copyButton)

    await waitFor(() => expect(fetchMock.mock.calls.some(([u]) => String(u).includes('/copy-profile-schedule'))).toBe(true))
    await screen.findByText('Engine oil and filter')
  })

  it('hides Use profile schedule when the item has no profile', async () => {
    fetchMock.mockImplementation((url: string) => {
      const u = String(url)
      if (u.includes('/maintenance/rules')) return jsonResponse(200, { rules: [] })
      if (u.includes('/maintenance/log')) return jsonResponse(200, { entries: [] })
      return jsonResponse(200, {})
    })
    render(<MaintenanceEquipmentBlock equipmentId="eq-1" profileId="" hourMeterPath="" />)
    await waitFor(() => expect(screen.getByText(/no maintenance rules/i)).toBeInTheDocument())
    expect(screen.queryByRole('button', { name: /use profile schedule/i })).not.toBeInTheDocument()
  })
})
