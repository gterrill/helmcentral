import { renderHook, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { useEngineProfiles } from '@/hooks/use-engine-profiles'

function jsonResponse(status: number, body: unknown): Response {
  return { ok: status >= 200 && status < 300, status, json: async () => body } as Response
}

afterEach(() => {
  vi.unstubAllGlobals()
})

// use-engine-profiles.ts is the older copy use-equipment-profiles.ts was
// cloned from (engine-cluster-config-dialog.tsx is still its only consumer),
// and it had the same upstream-error-masking flaw. Same fix, same tests.
describe('useEngineProfiles', () => {
  it('loads profiles and problems on a 200', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(
      jsonResponse(200, { profiles: [{ id: 'a', name: 'A', gauges: [] }], problems: [] }),
    ))

    const { result } = renderHook(() => useEngineProfiles(true))

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.error).toBeNull()
    expect(result.current.profiles).toHaveLength(1)
  })

  it('surfaces a non-ok response as an error instead of an empty list', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(
      jsonResponse(500, { error: 'profiles directory unreadable' }),
    ))

    const { result } = renderHook(() => useEngineProfiles(true))

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.error).toBe('profiles directory unreadable')
    expect(result.current.profiles).toEqual([])
  })

  it('falls back to the HTTP status when the error body is not JSON', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: false,
      status: 502,
      json: async () => { throw new Error('not json') },
    } as unknown as Response))

    const { result } = renderHook(() => useEngineProfiles(true))

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.error).toBe('HTTP 502')
  })

  it('surfaces a thrown network failure as an error instead of an empty list', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('network unreachable')))

    const { result } = renderHook(() => useEngineProfiles(true))

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.error).toBe('network unreachable')
    expect(result.current.profiles).toEqual([])
  })
})
