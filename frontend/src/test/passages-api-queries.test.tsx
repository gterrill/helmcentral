import { act, renderHook, waitFor } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { useCruiseDetails, usePassageDetail, usePassageList } from '@/hooks/use-passages'
import { passagesApi } from '@/lib/passages-api'
import { stubApi } from './helpers/stub-api'

const stats = {
  distance_nm: 1, underway_seconds: 60, mean_sog_kts: null, max_sog_kts: null, fuel_burned_l: null, tws_mean_kts: null,
  max_gust_kts: null, soc_start_pct: null, soc_end_pct: null, soc_min_pct: null,
}
const record = (id: string) => ({
  id, start: '2026-03-13T23:30:00Z', end: '2026-03-14T02:00:00Z', from_name: 'A', to_name: 'B', title: '', notes: '',
  hidden: false, edited: false, cruise_id: null, created_at: '', stats, start_lon: 149,
})

function calls(mock: ReturnType<typeof stubApi>, method: string, path: string) {
  return mock.mock.calls.filter(([input, init]) => {
    const url = typeof input === 'string' ? input : String(input)
    return (init?.method ?? 'GET') === method && url === path
  }).length
}

describe('passages queries', () => {
  it('reads the passage and cruise lists', async () => {
    stubApi({
      'GET /api/passages': { passages: [record('p1')] },
      'GET /api/cruises': { cruises: [{ id: 'c1', name: 'Trip', notes: '', created_at: '' }] },
    })
    const { result } = renderHook(() => usePassageList())
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.passages.map((p) => p.id)).toEqual(['p1'])
    expect(result.current.cruises.map((c) => c.name)).toEqual(['Trip'])
    expect(result.current.error).toBeNull()
  })

  it('refetches the lists, a passage and a cruise after an edit to a passage', async () => {
    const mock = stubApi({
      'GET /api/passages': { passages: [record('p1')] },
      'GET /api/cruises': { cruises: [] },
      'GET /api/passages/p1': { passage: { ...record('p1'), track: [], track_times: [], replay_track: [], replay_times: [] } },
      'GET /api/cruises/c1': { cruise: { id: 'c1', name: 'T', notes: '', created_at: '' }, passages: [], stats: {} },
      'PATCH /api/passages/p1': { passage: record('p1') },
    })
    const list = renderHook(() => usePassageList())
    const detail = renderHook(() => usePassageDetail('p1'))
    const cruise = renderHook(() => useCruiseDetails('c1'))
    await waitFor(() => expect(detail.result.current.passage).toBeDefined())
    await waitFor(() => expect(cruise.result.current.data).not.toBeNull())
    await waitFor(() => expect(list.result.current.loading).toBe(false))
    await act(async () => { await passagesApi.patch('p1', { notes: 'x' }) })
    await waitFor(() => {
      expect(calls(mock, 'GET', '/api/passages')).toBe(2)
      expect(calls(mock, 'GET', '/api/passages/p1')).toBe(2)
      expect(calls(mock, 'GET', '/api/cruises')).toBe(2)
      expect(calls(mock, 'GET', '/api/cruises/c1')).toBe(2)
    })
  })

  it('a water label refreshes cruises and leaves the passage list alone', async () => {
    const mock = stubApi({
      'GET /api/passages': { passages: [] },
      'GET /api/cruises': { cruises: [] },
      'PUT /api/cruises/c1/water-rises': { ok: true },
    })
    const list = renderHook(() => usePassageList())
    await waitFor(() => expect(list.result.current.loading).toBe(false))
    await act(async () => { await passagesApi.labelWaterRise('c1', '2026-03-14T01:10:00Z', 'Dock fill') })
    expect(calls(mock, 'GET', '/api/passages')).toBe(1)
  })

  it('an import refreshes the passage list', async () => {
    const mock = stubApi({
      'GET /api/passages': { passages: [] },
      'GET /api/cruises': { cruises: [] },
      'POST /api/passages/import': { files: [], passages_created: 1 },
    })
    const list = renderHook(() => usePassageList())
    await waitFor(() => expect(list.result.current.loading).toBe(false))
    await act(async () => { await passagesApi.importGPX([new File(['<gpx/>'], 'a.gpx')]) })
    await waitFor(() => expect(calls(mock, 'GET', '/api/passages')).toBe(2))
  })

  it('deleting a cruise refreshes the lists but not the deleted cruise', async () => {
    const mock = stubApi({
      'GET /api/passages': { passages: [] },
      'GET /api/cruises': { cruises: [] },
      'GET /api/cruises/c1': { cruise: { id: 'c1', name: 'T', notes: '', created_at: '' }, passages: [], stats: {} },
      'DELETE /api/cruises/c1': { deleted: true },
    })
    const list = renderHook(() => usePassageList())
    const cruise = renderHook(() => useCruiseDetails('c1'))
    await waitFor(() => expect(cruise.result.current.data).not.toBeNull())
    await waitFor(() => expect(list.result.current.loading).toBe(false))
    await act(async () => { await passagesApi.deleteCruise('c1') })
    expect(calls(mock, 'GET', '/api/cruises')).toBe(2)
    expect(calls(mock, 'GET', '/api/cruises/c1')).toBe(1)
  })

  it('surfaces the server message when a write is refused', async () => {
    stubApi({
      'POST /api/passages/p1/split': () => new Response(JSON.stringify({ error: 'split point is outside the passage' }), { status: 400 }),
    })
    await expect(passagesApi.split('p1', '2026-01-01T00:00:00Z')).rejects.toThrow('split point is outside the passage')
  })
})
