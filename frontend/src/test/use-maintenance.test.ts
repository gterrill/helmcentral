import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { stubApi } from '@/test/helpers/stub-api'

import { completeOpenJob } from '@/hooks/use-open-jobs'
import {
  createMaintenanceLogEntry,
  deleteMaintenanceLogEntry,
  maintenanceLogExportURL,
  setMaintenanceRuleLastDone,
  updateMaintenanceLogEntry,
  useHandHourReadings,
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

  it('keeps a rule view for each date in its own entry, and refreshes each on a write', async () => {
    const fetchMock = stubApi({
      'GET /api/inventory/maintenance/rules': { rules: [] },
      'PUT /api/inventory/maintenance/rules/r1/overrides': { rule: { id: 'r1' } },
    })
    vi.useFakeTimers({ toFake: ['Date'] })
    try {
      vi.setSystemTime(new Date(2026, 9, 13, 12))
      renderHook(() => useMaintenanceRules({ equipment: 'eq-1' }))
      await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))
      vi.setSystemTime(new Date(2026, 9, 14, 12))
      renderHook(() => useMaintenanceRules({ equipment: 'eq-1' }))
      await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
      await setMaintenanceRuleOverrides('r1', {} as never)
      await waitFor(() => expect(fetchMock.mock.calls.filter((c) => String(c[0]).includes('rules?') && String(c[0]).includes('today=2026-10-13'))).toHaveLength(2))
      expect(fetchMock.mock.calls.filter((c) => String(c[0]).includes('rules?') && String(c[0]).includes('today=2026-10-14'))).toHaveLength(2)
    } finally {
      vi.useRealTimers()
    }
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

// Hand-read hours (ADR 0268) come from service log entries and the readings
// Set last done keeps, so every write to either must refresh what shows them.
describe('hand-hour cache invalidation', () => {
  const RULES_GET = 'GET /api/inventory/maintenance/rules'
  const entry = { id: 'l1' }
  const input = { equipment_id: 'eq-1' } as never

  const writers: Array<[string, Record<string, unknown>, () => Promise<unknown>]> = [
    ['createMaintenanceLogEntry', { 'POST /api/inventory/maintenance/log': { entry } }, () => createMaintenanceLogEntry(input)],
    ['updateMaintenanceLogEntry', { 'PUT /api/inventory/maintenance/log/l1': { entry } }, () => updateMaintenanceLogEntry('l1', input)],
    ['deleteMaintenanceLogEntry', { 'DELETE /api/inventory/maintenance/log/l1': new Response(null, { status: 204 }) }, () => deleteMaintenanceLogEntry('l1')],
    ['completeOpenJob', { 'POST /api/inventory/open-jobs/j1/done': { job: { id: 'j1' }, entry } }, () => completeOpenJob('j1', {} as never)],
    ['setMaintenanceRuleLastDone', { 'POST /api/inventory/maintenance/rules/r1/last-done': { rule: { id: 'r1', equipment_id: 'eq-1' } } }, () => setMaintenanceRuleLastDone('r1', '2026-11-01', 150)],
  ]

  it.each(writers)('%s refreshes the rules list', async (_name, routes, write) => {
    const fetchMock = stubApi({ [RULES_GET]: { rules: [] }, ...routes })
    renderHook(() => useMaintenanceRules({ equipment: 'eq-1' }))
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))
    await write()
    await waitFor(() => expect(fetchMock.mock.calls.filter((c) => String(c[0]).includes('/maintenance/rules?'))).toHaveLength(2))
  })

  it('setMaintenanceRuleLastDone refreshes the item\'s hand readings', async () => {
    const readings = '/api/inventory/equipment/eq-1/maintenance/hour-readings'
    const fetchMock = stubApi({
      [`GET ${readings}`]: { readings: [] },
      'POST /api/inventory/maintenance/rules/r1/last-done': { rule: { id: 'r1', equipment_id: 'eq-1' } },
    })
    renderHook(() => useHandHourReadings('eq-1'))
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))
    await setMaintenanceRuleLastDone('r1', '2026-11-01', 150)
    await waitFor(() => expect(fetchMock.mock.calls.filter((c) => String(c[0]).includes('/hour-readings'))).toHaveLength(2))
  })
})
