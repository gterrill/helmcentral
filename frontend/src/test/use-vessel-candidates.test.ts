import { describe, it, expect, vi } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { useVesselCandidates } from '@/hooks/use-vessel-candidates'

const candidatesBody = {
  engines: [{ instance: 'port', rpm: 1800, coolant_c: 76 }],
  batteries: [],
  detectors: { frozen: { ready: true }, battery: { ready: false, missing: 'Pick your house bank' } },
}

describe('useVesselCandidates', () => {
  it('reads the live candidates and detector status', async () => {
    const fetchMock = vi.fn(async (url: string) => { void url; return { ok: true, json: async () => candidatesBody } })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useVesselCandidates())
    await waitFor(() => expect(result.current.loading).toBe(false))

    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(String(fetchMock.mock.calls[0][0])).toMatch(/\/api\/vessel\/candidates$/)
    expect(result.current.candidates.engines[0].rpm).toBe(1800)
    expect(result.current.candidates.detectors.battery.missing).toBe('Pick your house bank')
    expect(result.current.error).toBeNull()
  })

  it('surfaces a failure rather than masking it', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: false, status: 500, json: async () => ({ error: 'boom' }) })))

    const { result } = renderHook(() => useVesselCandidates())
    await waitFor(() => expect(result.current.loading).toBe(false))

    expect(result.current.error).toBe('boom')
  })

  it('re-reads when the refresh key changes (a save landed)', async () => {
    const fetchMock = vi.fn(async () => ({ ok: true, json: async () => candidatesBody }))
    vi.stubGlobal('fetch', fetchMock)

    const { result, rerender } = renderHook(({ k }) => useVesselCandidates(k), { initialProps: { k: 1 } })
    await waitFor(() => expect(result.current.loading).toBe(false))
    rerender({ k: 2 })
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
  })
})
