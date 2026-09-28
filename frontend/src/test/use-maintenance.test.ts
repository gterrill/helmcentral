import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'

import {
  maintenanceLogExportURL,
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
