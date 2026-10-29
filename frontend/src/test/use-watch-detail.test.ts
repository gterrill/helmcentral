import { afterEach, describe, expect, it, vi } from 'vitest'
import { act, renderHook, waitFor } from '@testing-library/react'

import { invalidateWatches, useWatchDetail } from '@/hooks/use-watches'
import { resetQueryStore } from '@/lib/api-query'
import type { WatchRun } from '@/lib/watches'

// The run history pages by cursor (ADR 0227): a run made between two page
// loads must neither repeat nor drop one, and a deleted run leaves every page.

const watch = { id: 'w1', name: 'W', kind: 'scheduled', sections: [], thresholds: {}, enabled: true, schedule_sentence: '', last_run: null, has_finding: false }

function run(n: number): WatchRun {
  const at = new Date(Date.UTC(2026, 9, 29, n, 0, 0)).toISOString()
  return { id: `r${n}`, watch_id: 'w1', started_at: at, finished_at: at, outcome: 'ran', report_kind: 'sections', findings: [], review: null, review_problem: null, cost_usd: 0, cost_known: true, boat_time: `n${n}`, timezone: '', notified: [] }
}

// The server's runs, newest first.
let server: WatchRun[]
const urls: string[] = []

function stub() {
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    urls.push(String(url))
    const u = new URL(String(url), 'http://x')
    const limit = Number(u.searchParams.get('limit'))
    const before = u.searchParams.get('before')
    let list = [...server].sort((a, b) => b.started_at.localeCompare(a.started_at))
    if (before) {
      const [at, id] = before.split(',')
      list = list.filter((r) => r.started_at < at || (r.started_at === at && r.id < id))
    }
    const page = list.slice(0, limit)
    return { ok: true, status: 200, json: async () => ({ watch, runs: page, has_older: list.length > limit }) }
  }))
}

afterEach(() => {
  vi.unstubAllGlobals()
  resetQueryStore()
  urls.length = 0
})

const ids = (runs: WatchRun[]) => runs.map((r) => r.id)

describe('useWatchDetail paging', () => {
  it('asks for older runs by what follows the oldest shown, not by a count', async () => {
    server = Array.from({ length: 25 }, (_, i) => run(i))
    stub()
    const { result } = renderHook(() => useWatchDetail('w1'))
    await waitFor(() => expect(result.current.runs).toHaveLength(20))
    expect(result.current.hasOlder).toBe(true)
    await act(async () => { await result.current.showOlder() })
    expect(result.current.runs).toHaveLength(25)
    expect(result.current.hasOlder).toBe(false)
    expect(urls.some((u) => u.includes('offset'))).toBe(false)
    expect(urls.at(-1)).toContain(`before=${encodeURIComponent(run(5).started_at)},r5`)
  })

  it('loses and repeats nothing when a run is made between two page loads', async () => {
    server = Array.from({ length: 25 }, (_, i) => run(i))
    stub()
    const { result } = renderHook(() => useWatchDetail('w1'))
    await waitFor(() => expect(result.current.runs).toHaveLength(20))
    // A new run lands, and the first page is read again before "Show older".
    server = [...server, run(30)]
    await act(async () => { await invalidateWatches() })
    await waitFor(() => expect(result.current.runs[0].id).toBe('r30'))
    await act(async () => { await result.current.showOlder() })
    const got = ids(result.current.runs)
    expect(new Set(got).size).toBe(got.length)
    expect(got).toEqual(ids(server.slice().sort((a, b) => b.started_at.localeCompare(a.started_at))))
  })

  it('drops a deleted run from an older page and does not bring it back on a refresh that still lists it', async () => {
    server = Array.from({ length: 25 }, (_, i) => run(i))
    stub()
    const { result } = renderHook(() => useWatchDetail('w1'))
    await waitFor(() => expect(result.current.runs).toHaveLength(20))
    await act(async () => { await result.current.showOlder() })
    expect(ids(result.current.runs)).toContain('r2')
    act(() => { result.current.dropRun('r2') })
    expect(ids(result.current.runs)).not.toContain('r2')
    expect(result.current.runs).toHaveLength(24)
    server = server.filter((r) => r.id !== 'r2')
    await act(async () => { await invalidateWatches() })
    expect(ids(result.current.runs)).not.toContain('r2')
  })

  it('starts again for another watch', async () => {
    server = Array.from({ length: 3 }, (_, i) => run(i))
    stub()
    const { result, rerender } = renderHook(({ id }) => useWatchDetail(id), { initialProps: { id: 'w1' } })
    await waitFor(() => expect(result.current.runs).toHaveLength(3))
    server = [run(9)]
    rerender({ id: 'w2' })
    await waitFor(() => expect(ids(result.current.runs)).toEqual(['r9']))
  })
})
