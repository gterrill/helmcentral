import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { useAnchorSoundedCells } from '@/hooks/use-anchor-sounded-cells'
import { resetQueryStore } from '@/lib/api-query'

const ready = { status: 'ready', cell_m: 2.5, origin_lat: -25.29, origin_lon: 152.91, cells: [[0, 0]] }
const watch = { enabled: true, setAt: '2026-11-09T08:00:00Z', lat: -25.29, lon: 152.91 }

function answer(body: unknown) {
  const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => body })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

describe('useAnchorSoundedCells', () => {
  beforeEach(() => resetQueryStore())
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('reads the cells while a watch is set', async () => {
    const fetchMock = answer(ready)
    const { result } = renderHook(() => useAnchorSoundedCells(watch))
    await waitFor(() => expect(result.current.answer?.status).toBe('ready'))
    expect(fetchMock.mock.calls[0][0]).toContain('/api/anchor-watch/sounded-cells')
  })

  it('does not ask without a watch', () => {
    const fetchMock = answer(ready)
    const { result } = renderHook(() => useAnchorSoundedCells({ ...watch, enabled: false }))
    expect(result.current.answer).toBeNull()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('asks again at once when the anchor is corrected', async () => {
    const fetchMock = answer(ready)
    const { rerender } = renderHook((w) => useAnchorSoundedCells(w), { initialProps: watch })
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))
    rerender({ ...watch, lat: -25.2901 })
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
  })

  it('logs an answer it cannot read once, and returns the error with no cells', async () => {
    answer({ status: 'maybe' })
    const err = vi.spyOn(console, 'error').mockImplementation(() => {})
    const { result, rerender } = renderHook(() => useAnchorSoundedCells(watch))
    await waitFor(() => expect(result.current.error).not.toBeNull())
    expect(result.current.answer).toBeNull()
    rerender()
    rerender()
    expect(err).toHaveBeenCalledTimes(1)
    expect(String(err.mock.calls[0][1])).toContain('unknown status')
  })
})
