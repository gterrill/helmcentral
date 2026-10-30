import { afterEach, describe, expect, it, vi } from 'vitest'
import { act, renderHook, waitFor } from '@testing-library/react'

import { useWatchList } from '@/hooks/use-watches'
import { resetQueryStore } from '@/lib/api-query'

function stub() {
  const fetchMock = vi.fn(async () => ({ ok: true, status: 200, json: async () => ({ watches: [], max: 20, cost_24h: { usd: 0, unknown_runs: 0 }, storage_bytes: 0 }) }))
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
  resetQueryStore()
})

describe('useWatchList', () => {
  it('polls every 30 seconds on the index', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const fetchMock = stub()
    renderHook(() => useWatchList(true, true))
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))
    await act(async () => { await vi.advanceTimersByTimeAsync(31_000) })
    expect(fetchMock.mock.calls.length).toBeGreaterThan(1)
  })

  it('reads once and does not poll when only a details page wants it', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const fetchMock = stub()
    renderHook(() => useWatchList(true, false))
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))
    await act(async () => { await vi.advanceTimersByTimeAsync(95_000) })
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })
})
