import { describe, it, expect, afterEach, beforeEach, vi } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { useAnchorLowWater } from '@/hooks/use-anchor-low-water'
import { resetQueryStore } from '@/lib/api-query'

// ADR 0272: the low-water clearance is worked out once, on the server, and the
// Anchor Watch page only reads it. The hook turns the wire answer into the
// shape the readings use and refuses an answer it cannot read.
function answer(body: unknown) {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => body }))
}

describe('useAnchorLowWater', () => {
  beforeEach(() => resetQueryStore())
  afterEach(() => vi.unstubAllGlobals())

  it('reads a resolved verdict from /api/anchor-watch/low-water', async () => {
    answer({
      status: 'too_shallow',
      depth_now_m: 2,
      tide_fall_m: 0.61,
      depth_at_low_m: 1.39,
      draft_m: 1.2,
      clearance_m: 0.19,
      margin_m: 0.5,
      low_tide_time: '2026-11-08T04:45:00Z',
      worked_out_at: '2026-11-08T04:00:00Z',
    })
    const { result } = renderHook(() => useAnchorLowWater())

    await waitFor(() => expect(result.current.lowWater).not.toBeNull())
    expect(result.current.lowWater).toEqual({
      status: 'too_shallow',
      depthAtLowM: 1.39,
      clearanceM: 0.19,
      lowTideTime: '2026-11-08T04:45:00Z',
    })
    expect(result.current.failed).toBe(false)
    expect((fetch as unknown as ReturnType<typeof vi.fn>).mock.calls[0][0]).toContain('/api/anchor-watch/low-water')
  })

  it('reads an unknown verdict with its reason', async () => {
    answer({ status: 'unknown', reason: 'no_draft', margin_m: 0.5, worked_out_at: '2026-11-08T04:00:00Z' })
    const { result } = renderHook(() => useAnchorLowWater())

    await waitFor(() => expect(result.current.lowWater).not.toBeNull())
    expect(result.current.lowWater).toEqual({ status: 'unknown', reason: 'no_draft' })
    expect(result.current.workedOutAt).toBe('2026-11-08T04:00:00Z')
  })

  it('refuses a verdict it cannot read rather than guessing one', async () => {
    answer({ status: 'unknown', reason: 'no_such_reason', worked_out_at: '2026-11-08T04:00:00Z' })
    const { result } = renderHook(() => useAnchorLowWater())

    await waitFor(() => expect(result.current.failed).toBe(true))
    expect(result.current.lowWater).toBeNull()
  })

  it('refuses a resolved verdict that carries no figures', async () => {
    answer({ status: 'ok', worked_out_at: '2026-11-08T04:00:00Z' })
    const { result } = renderHook(() => useAnchorLowWater())

    await waitFor(() => expect(result.current.failed).toBe(true))
    expect(result.current.lowWater).toBeNull()
  })

  it('reports a failed request as an error with no verdict', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 502, json: async () => ({ error: 'the boat is unavailable' }) }))
    const { result } = renderHook(() => useAnchorLowWater())

    await waitFor(() => expect(result.current.failed).toBe(true))
    expect(result.current.lowWater).toBeNull()
  })

  it('refuses an answer with no worked_out_at', async () => {
    answer({ status: 'unknown', reason: 'no_draft' })
    const { result } = renderHook(() => useAnchorLowWater())

    await waitFor(() => expect(result.current.failed).toBe(true))
    expect(result.current.lowWater).toBeNull()
  })

  it('keeps the last verdict and its time, marked failed, when a later poll fails', async () => {
    const ok = {
      ok: true,
      json: async () => ({ status: 'unknown', reason: 'no_tide_station', worked_out_at: '2026-11-08T04:00:00Z' }),
    }
    const bad = { ok: false, status: 502, json: async () => ({ error: 'the boat is unavailable' }) }
    // The second poll waits until the verdict has been seen, so the test sees it.
    let release: () => void = () => {}
    const gate = new Promise<void>((resolve) => { release = resolve })
    const fetchMock = vi.fn().mockResolvedValueOnce(ok).mockImplementation(async () => { await gate; return bad })
    vi.stubGlobal('fetch', fetchMock)
    const { result } = renderHook(() => useAnchorLowWater(0.05))

    await waitFor(() => expect(result.current.lowWater).not.toBeNull())
    expect(result.current.failed).toBe(false)
    release()
    await waitFor(() => expect(result.current.failed).toBe(true))
    expect(result.current.lowWater).toEqual({ status: 'unknown', reason: 'no_tide_station' })
    expect(result.current.workedOutAt).toBe('2026-11-08T04:00:00Z')
  })
})
