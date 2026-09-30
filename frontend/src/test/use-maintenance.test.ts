import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'

import {
  maintenanceLogExportURL,
  previewProfileChange,
  resetMaintenanceRuleOverride,
  setMaintenanceRuleOverrides,
  useMaintenanceRules,
  MAINTENANCE_DEFAULT_DUE_SOON_HOURS,
  MAINTENANCE_DEFAULT_DUE_SOON_MONTHS,
} from '@/hooks/use-maintenance'

afterEach(() => { vi.restoreAllMocks() })

function jsonResponse(status: number, body: unknown) {
  return { ok: status >= 200 && status < 300, status, json: async () => body } as Response
}

describe('useMaintenanceRules', () => {
  it('builds the query string from equipment/system/includeStored', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, { rules: [] }))
    vi.stubGlobal('fetch', fetchMock)

    renderHook(() => useMaintenanceRules({ equipment: 'eq-1', system: 'propulsion', includeStored: true }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalled())
    const url = String(fetchMock.mock.calls[0][0])
    expect(url).toContain('/api/inventory/maintenance/rules?')
    expect(url).toContain('equipment=eq-1')
    expect(url).toContain('system=propulsion')
    expect(url).toContain('include_stored=true')
  })

  it('exposes removed jobs and schedule errors from the response', async () => {
    const removed = [{ id: 'job:eq-1:old', description: 'Old job' }]
    const errors = [{ equipment_id: 'eq-1', equipment_name: 'Main engine', profile_id: 'gone', error: 'profile not found' }]
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse(200, { rules: [], removed, schedule_errors: errors })))

    const { result } = renderHook(() => useMaintenanceRules({ equipment: 'eq-1' }))
    await waitFor(() => expect(result.current.scheduleErrors).toEqual(errors))
    expect(result.current.removed).toEqual(removed)
  })

  it('fetches nothing at all when filter is null', () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useMaintenanceRules(null))
    expect(result.current.loading).toBe(false)
    expect(result.current.rules).toEqual([])
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('surfaces the server\'s own error message rather than a generic one', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(500, { error: 'helmcentral.sqlite is locked' }))
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useMaintenanceRules({}))
    await waitFor(() => expect(result.current.error).toBe('helmcentral.sqlite is locked'))
  })
})

describe('maintenanceLogExportURL', () => {
  it('is the bare export endpoint with no filter', () => {
    expect(maintenanceLogExportURL()).toBe('/api/inventory/maintenance/log/export.csv')
  })

  it('carries the equipment filter when given', () => {
    expect(maintenanceLogExportURL('eq-1')).toBe('/api/inventory/maintenance/log/export.csv?equipment=eq-1')
  })
})

describe('due-soon defaults', () => {
  it('match the backend\'s own constants (maintenance_status.go)', () => {
    expect(MAINTENANCE_DEFAULT_DUE_SOON_HOURS).toBe(50)
    expect(MAINTENANCE_DEFAULT_DUE_SOON_MONTHS).toBe(1)
  })
})

describe('profile job writes', () => {
  it('setMaintenanceRuleOverrides PUTs the subset with today and returns the rule', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, { rule: { id: 'job:eq-1:oil' } }))
    vi.stubGlobal('fetch', fetchMock)

    const rule = await setMaintenanceRuleOverrides('job:eq-1:oil', { interval_hours: 100, not_applicable: false })

    const [url, init] = fetchMock.mock.calls[0]
    expect(String(url)).toMatch(/\/rules\/job%3Aeq-1%3Aoil\/overrides\?today=\d{4}-\d{2}-\d{2}$/)
    expect(init.method).toBe('PUT')
    expect(JSON.parse(init.body)).toEqual({ interval_hours: 100, not_applicable: false })
    expect(rule.id).toBe('job:eq-1:oil')
  })

  it('resetMaintenanceRuleOverride DELETEs one field', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, { rule: { id: 'job:eq-1:oil' } }))
    vi.stubGlobal('fetch', fetchMock)

    await resetMaintenanceRuleOverride('job:eq-1:oil', 'interval_hours')

    const [url, init] = fetchMock.mock.calls[0]
    expect(String(url)).toContain('/overrides/interval_hours?today=')
    expect(init.method).toBe('DELETE')
  })

  it('previewProfileChange asks with the new profile id and fills missing lists', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, { kept: [{ service_id: 'a', description: 'A' }] }))
    vi.stubGlobal('fetch', fetchMock)

    const preview = await previewProfileChange('eq-1', 'new-profile')

    expect(String(fetchMock.mock.calls[0][0])).toContain('/equipment/eq-1/maintenance/profile-change-preview?profile_id=new-profile')
    expect(preview).toEqual({ kept: [{ service_id: 'a', description: 'A' }], leaving: [], new: [] })
  })
})
