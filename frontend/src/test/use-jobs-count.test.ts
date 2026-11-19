import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { stubApi, jsonResponse } from '@/test/helpers/stub-api'

import { useJobsCount } from '@/hooks/use-jobs-count'
import { dropOpenJob, JOBS_COUNT_QUERY } from '@/hooks/use-open-jobs'
import { acknowledgeMaintenanceRule } from '@/hooks/use-maintenance'
import { refetchQuery, resetQueryStore } from '@/lib/api-query'

// ADR 0293: the number on the Jobs row, counted by the server. It says nothing
// rather than an old number.

let boatDay: string | null = '2026-11-19'
vi.mock('@/hooks/use-vessel-clock', () => ({ useBoatDayKey: () => boatDay }))

beforeEach(() => { resetQueryStore() })
afterEach(() => {
  vi.unstubAllGlobals()
  boatDay = '2026-11-19'
})

const COUNT_PATH = 'GET /api/inventory/jobs/count'
const fail = () => jsonResponse({ error: 'boom' }, 500)

describe('useJobsCount', () => {
  it('reads the server count and nothing else', async () => {
    const fetchMock = stubApi({ [COUNT_PATH]: { count: 3, incomplete: false } })
    const { result } = renderHook(() => useJobsCount())
    await waitFor(() => expect(result.current).toEqual({ count: 3, incomplete: false }))
    const urls = fetchMock.mock.calls.map((call) => String(call[0]))
    expect(urls.every((url) => url.includes('/api/inventory/jobs/count'))).toBe(true)
  })

  it('passes the incomplete flag through', async () => {
    stubApi({ [COUNT_PATH]: { count: 1, incomplete: true } })
    const { result } = renderHook(() => useJobsCount())
    await waitFor(() => expect(result.current).toEqual({ count: 1, incomplete: true }))
  })

  it('is null while the read has not answered', () => {
    stubApi({ [COUNT_PATH]: { count: 3, incomplete: false } })
    const { result } = renderHook(() => useJobsCount())
    expect(result.current).toBeNull()
  })

  it('is null when the read fails, and drops an old count when a later read fails', async () => {
    let failing = false
    stubApi({ [COUNT_PATH]: () => (failing ? fail() : { count: 3, incomplete: false }) })
    const { result } = renderHook(() => useJobsCount())
    await waitFor(() => expect(result.current?.count).toBe(3))
    failing = true
    await refetchQuery(JOBS_COUNT_QUERY.queryKey, JOBS_COUNT_QUERY.path)
    await waitFor(() => expect(result.current).toBeNull())
  })

  it('treats an answer that is not a count as a failed read', async () => {
    stubApi({ [COUNT_PATH]: { nope: true } })
    const { result } = renderHook(() => useJobsCount())
    await waitFor(() => expect(result.current).toBeNull())
  })

  it('reads again after an open job is dropped', async () => {
    let reads = 0
    stubApi({
      [COUNT_PATH]: () => { reads += 1; return { count: reads === 1 ? 2 : 1, incomplete: false } },
      'POST /api/inventory/open-jobs/j1/drop': { job: { id: 'j1' } },
    })
    const { result } = renderHook(() => useJobsCount())
    await waitFor(() => expect(result.current?.count).toBe(2))
    await dropOpenJob('j1', '')
    await waitFor(() => expect(result.current?.count).toBe(1))
  })

  it('reads again after a service write', async () => {
    let reads = 0
    stubApi({
      [COUNT_PATH]: () => { reads += 1; return { count: reads === 1 ? 2 : 1, incomplete: false } },
      'POST /api/inventory/maintenance/rules/r1/acknowledge': { rule: { id: 'r1' } },
    })
    const { result } = renderHook(() => useJobsCount())
    await waitFor(() => expect(result.current?.count).toBe(2))
    await acknowledgeMaintenanceRule('r1', 'later')
    await waitFor(() => expect(result.current?.count).toBe(1))
  })

  it('reads again when the boat has a date after refusing the first read for want of one', async () => {
    boatDay = null
    let reads = 0
    stubApi({
      [COUNT_PATH]: () => {
        reads += 1
        return reads === 1 ? jsonResponse({ error: 'the boat has no date yet' }, 503) : { count: 2, incomplete: false }
      },
    })
    const { result, rerender } = renderHook(() => useJobsCount())
    await waitFor(() => expect(reads).toBe(1))
    expect(result.current).toBeNull()
    boatDay = '2026-11-19'
    rerender()
    await waitFor(() => expect(result.current?.count).toBe(2))
    expect(reads).toBe(2)
  })

  it('reads again when the boat\'s day turns', async () => {
    let reads = 0
    stubApi({ [COUNT_PATH]: () => { reads += 1; return { count: reads === 1 ? 0 : 1, incomplete: false } } })
    const { result, rerender } = renderHook(() => useJobsCount())
    await waitFor(() => expect(reads).toBe(1))
    boatDay = '2026-11-20'
    rerender()
    await waitFor(() => expect(result.current?.count).toBe(1))
    expect(reads).toBe(2)
  })
})
