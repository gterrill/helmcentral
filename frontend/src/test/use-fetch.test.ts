import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { renderHook, act } from '@testing-library/react'

import { useFetch } from '@/hooks/use-fetch'

describe('useFetch', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('fetches once on mount and validates the response', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ value: 42 }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const validator = (data: unknown): data is { value: number } =>
      typeof data === 'object' && data !== null && typeof (data as any).value === 'number'

    const { result } = renderHook(() =>
      useFetch('/api/thing', { value: 0 }, { refreshIntervalSeconds: 60, validator }),
    )

    await act(async () => {
      await Promise.resolve()
    })

    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(result.current.data).toEqual({ value: 42 })
  })

  it('uses the latest validator/onError on a polling refetch, not the ones captured on mount', async () => {
    // endpoint and refreshIntervalSeconds (the only deps the effect is
    // allowed to depend on) never change across the two renders below —
    // only the inline `validator`/`onError` callbacks change identity, which
    // is what a normal (non-memoized) caller does on every render.
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ value: 1 }) })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ value: 2 }) })
    vi.stubGlobal('fetch', fetchMock)

    const acceptAll = (_data: unknown): _data is { value: number } => true
    const rejectAll = (_data: unknown): _data is { value: number } => false
    const onErrorMount = vi.fn()
    const onErrorLatest = vi.fn()

    const { result, rerender } = renderHook(
      ({ validator, onError }: { validator: typeof acceptAll; onError: typeof onErrorMount }) =>
        useFetch('/api/thing', { value: 0 }, { refreshIntervalSeconds: 60, validator, onError }),
      { initialProps: { validator: acceptAll, onError: onErrorMount } },
    )

    await act(async () => {
      await Promise.resolve()
    })
    expect(result.current.data).toEqual({ value: 1 })

    // Re-render with brand-new validator/onError identities that now reject
    // every response — endpoint/refreshIntervalSeconds are unchanged.
    rerender({ validator: rejectAll, onError: onErrorLatest })

    // Trigger the polling refetch scheduled by the original effect run.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(60 * 1000)
    })

    expect(fetchMock).toHaveBeenCalledTimes(2)
    // The latest validator (rejectAll) must be the one consulted, so the
    // second response is treated as invalid and never lands in state...
    expect(result.current.data).toEqual({ value: 1 })
    // ...and the latest onError callback must be the one invoked, not the
    // stale callback captured when the effect first ran.
    expect(onErrorLatest).toHaveBeenCalledTimes(1)
    expect(onErrorMount).not.toHaveBeenCalled()
  })
})
