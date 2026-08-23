import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useCZoneSwitches } from '@/hooks/use-czone-switches'

describe('useCZoneSwitches', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  // The Go handler (czone.go getCZoneSwitchesHandler) maps a fetch error to a
  // 502 with {"error": "..."} — the frontend must surface that exact message
  // rather than swallowing it into console.error, which is what masked the
  // "bank" vs "banks" backend bug from the UI in the first place.
  it('surfaces the backend error message from a 502 response', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: false,
      status: 502,
      json: async () => ({
        error: 'electrical/switches payload has no "bank" object (keys present: gx, venus-0)',
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useCZoneSwitches(5))

    await act(async () => {
      await Promise.resolve()
    })

    expect(result.current.error).toBe(
      'electrical/switches payload has no "bank" object (keys present: gx, venus-0)',
    )
  })

  it('clears the error once a later poll succeeds', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce({
        ok: false,
        status: 502,
        json: async () => ({ error: 'unable to fetch CZone switches: signalk returned status 500' }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          switches: [{ id: 'banks.0.2', display_name: 'Bank 0 Circuit 2', state: 0 }],
        }),
      })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useCZoneSwitches(5))

    await act(async () => {
      await Promise.resolve()
    })
    expect(result.current.error).toBeTruthy()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5 * 1000)
    })

    expect(result.current.error).toBeNull()
    expect(result.current.switches).toHaveLength(1)
  })

  it('keeps the previously loaded switches when a later poll fails', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          switches: [{ id: 'banks.0.2', display_name: 'Bank 0 Circuit 2', state: 0 }],
        }),
      })
      .mockResolvedValueOnce({
        ok: false,
        status: 502,
        json: async () => ({ error: 'signalk returned status 500' }),
      })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useCZoneSwitches(5))

    await act(async () => {
      await Promise.resolve()
    })
    expect(result.current.switches).toHaveLength(1)
    expect(result.current.error).toBeNull()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5 * 1000)
    })

    expect(result.current.error).toBeTruthy()
    // The poll runs every 5s; flashing the tile empty on one blip would be
    // worse than useless, so stale data stays visible and the error state
    // communicates staleness instead.
    expect(result.current.switches).toHaveLength(1)
  })

  // writable is a control surface: most bank circuits from the live vessel
  // are status indicators (PGN 127501), not controllable outputs, and don't
  // carry a "writable" field in the API payload at all. Fail safe rather than
  // dropping the switch or defaulting it to controllable.
  it('coerces a missing writable field to false without dropping the switch', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        switches: [{ id: 'bank.0.2', display_name: 'Bank 0 Circuit 2', state: 0 }],
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useCZoneSwitches(5))

    await act(async () => {
      await Promise.resolve()
    })

    expect(result.current.switches).toHaveLength(1)
    expect(result.current.switches[0].writable).toBe(false)
  })

  it('coerces a non-boolean writable field to false', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        switches: [{ id: 'venus-0', display_name: 'Venus 0', state: 1, writable: 'yes' }],
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useCZoneSwitches(5))

    await act(async () => {
      await Promise.resolve()
    })

    expect(result.current.switches[0].writable).toBe(false)
  })

  it('passes through writable: true unchanged', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        switches: [{ id: 'venus-0', display_name: 'Venus 0', state: 1, writable: true }],
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useCZoneSwitches(5))

    await act(async () => {
      await Promise.resolve()
    })

    expect(result.current.switches[0].writable).toBe(true)
  })
})
