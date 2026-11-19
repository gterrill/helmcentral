import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { stubApi } from '@/test/helpers/stub-api'

import { useJobsCount } from '@/hooks/use-jobs-count'
import { useOpenJobs } from '@/hooks/use-open-jobs'
import { createMaintenanceLogEntry } from '@/hooks/use-maintenance'
import { deleteEquipment, equipmentRecordApplied, updateEquipment, type EquipmentInput } from '@/hooks/use-inventory'
import { resetQueryStore } from '@/lib/api-query'

// ADR 0293: deleting an item cascades its open jobs and storing it hides them,
// so the Jobs page's lists and the badge's count read again after either.

vi.mock('@/hooks/use-vessel-clock', () => ({ useBoatDayKey: () => '2026-11-19' }))

beforeEach(() => { resetQueryStore() })
afterEach(() => { vi.unstubAllGlobals() })

function stubReads(extra: Record<string, unknown>) {
  const reads = { jobs: 0, count: 0 }
  stubApi({
    'GET /api/inventory/open-jobs': () => { reads.jobs += 1; return { jobs: [] } },
    'GET /api/inventory/jobs/count': () => { reads.count += 1; return { count: reads.count, incomplete: false } },
    ...extra,
  })
  return reads
}

function mountJobsPageReads() {
  renderHook(() => ({ jobs: useOpenJobs({}), count: useJobsCount() }))
}

describe('the Jobs reads after an equipment write', () => {
  it('read again after an item is deleted', async () => {
    const reads = stubReads({ 'DELETE /api/inventory/equipment/eq-1': {}, 'GET /api/inventory/equipment': { items: [] } })
    mountJobsPageReads()
    await waitFor(() => expect(reads).toEqual({ jobs: 1, count: 1 }))
    await deleteEquipment('eq-1')
    await waitFor(() => expect(reads).toEqual({ jobs: 2, count: 2 }))
  })

  it('read again after an item is stored', async () => {
    const reads = stubReads({ 'PUT /api/inventory/equipment/eq-1': { item: { id: 'eq-1' } } })
    mountJobsPageReads()
    await waitFor(() => expect(reads).toEqual({ jobs: 1, count: 1 }))
    await updateEquipment('eq-1', {} as EquipmentInput)
    await waitFor(() => expect(reads).toEqual({ jobs: 2, count: 2 }))
  })

  it('read again after Mate applies an equipment delete', async () => {
    const reads = stubReads({ 'GET /api/inventory/equipment': { items: [] } })
    mountJobsPageReads()
    await waitFor(() => expect(reads).toEqual({ jobs: 1, count: 1 }))
    equipmentRecordApplied('delete', 'eq-1')
    await waitFor(() => expect(reads).toEqual({ jobs: 2, count: 2 }))
  })

  it('read again after a service log write', async () => {
    const reads = stubReads({ 'POST /api/inventory/maintenance/log': { entry: { id: 'l1' } } })
    mountJobsPageReads()
    await waitFor(() => expect(reads).toEqual({ jobs: 1, count: 1 }))
    await createMaintenanceLogEntry({} as never)
    await waitFor(() => expect(reads.count).toBe(2))
  })
})
