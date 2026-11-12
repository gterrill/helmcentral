import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { renderHook, act } from '@testing-library/react'

import { useLightning } from '@/hooks/use-lightning'

const T0 = Date.UTC(2026, 10, 13, 0, 0, 0)

function answer(overrides: Record<string, unknown> = {}) {
  return {
    ok: true,
    json: async () => ({
      provider: 'ukmo-lightning',
      run: '2026-11-13T00:00:00Z',
      windows: [
        { start: '2026-11-13T00:00:00Z', end: '2026-11-13T03:00:00Z', probability: 0.4, run: '2026-11-13T00:00:00Z' },
        { start: '2026-11-13T03:00:00Z', end: '2026-11-13T06:00:00Z', probability: 0.05, run: '2026-11-13T00:00:00Z' },
      ],
      updated_at: '2026-11-13T01:00:00Z',
      ttl_seconds: 21600,
      refreshing: false,
      ...overrides,
    }),
  }
}

describe('useLightning', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('maps the windows, provider and freshness', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(answer()))
    const { result } = renderHook(() => useLightning())
    await act(async () => { await Promise.resolve() })

    expect(result.current.provider).toBe('ukmo-lightning')
    expect(result.current.windows).toEqual([
      { startMs: T0, endMs: T0 + 3 * 3_600_000, probability: 0.4 },
      { startMs: T0 + 3 * 3_600_000, endMs: T0 + 6 * 3_600_000, probability: 0.05 },
    ])
    expect(result.current.updatedAt).toBe('2026-11-13T01:00:00Z')
    expect(result.current.ttlSeconds).toBe(21600)
    expect(result.current.refreshing).toBe(false)
    expect(result.current.error).toBeNull()
    expect(result.current.refreshError).toBeNull()
  })

  it('treats no provider as an empty answer, not a failure', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({ provider: '', run: '', windows: [], updated_at: '', ttl_seconds: 0, refreshing: false }) }))
    const { result } = renderHook(() => useLightning())
    await act(async () => { await Promise.resolve() })

    expect(result.current.provider).toBeNull()
    expect(result.current.providerSeen).toBe(false)
    expect(result.current.windows).toEqual([])
    expect(result.current.error).toBeNull()
    expect(result.current.updatedAt).toBeNull()
  })

  it('remembers that a provider was named, through later errors and empty answers', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(answer({ ttl_seconds: 60 }))
      .mockResolvedValue({ ok: false, status: 502, json: async () => ({ error: 'no run found' }) })
    vi.stubGlobal('fetch', fetchMock)
    const { result } = renderHook(() => useLightning())
    await act(async () => { await Promise.resolve() })
    expect(result.current.providerSeen).toBe(true)

    await act(async () => { await vi.advanceTimersByTimeAsync(60_000) })
    expect(result.current.error).toBe('no run found')
    expect(result.current.providerSeen).toBe(true)
  })

  it('does not count an error before any answer as a provider', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 502, json: async () => ({ error: 'nope' }) }))
    const { result } = renderHook(() => useLightning())
    await act(async () => { await Promise.resolve() })

    expect(result.current.error).toBe('nope')
    expect(result.current.providerSeen).toBe(false)
  })

  it('carries the refresh error and the refreshing flag', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(answer({ windows: [], refreshing: true, refresh_error: 'bucket unreachable' })))
    const { result } = renderHook(() => useLightning())
    await act(async () => { await Promise.resolve() })

    expect(result.current.refreshing).toBe(true)
    expect(result.current.refreshError).toBe('bucket unreachable')
  })

  it('surfaces a 502 as an error with the server words', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: false,
      status: 502,
      json: async () => ({ error: 'lightning provider "ukmo-lightning" unavailable: no run found' }),
    }))
    const { result } = renderHook(() => useLightning())
    await act(async () => { await Promise.resolve() })

    expect(result.current.error).toBe('lightning provider "ukmo-lightning" unavailable: no run found')
    expect(result.current.windows).toEqual([])
  })

  it('drops a window it cannot read and says so, keeping the rest', async () => {
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {})
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(answer({
      windows: [
        { start: '2026-11-13T00:00:00Z', end: '2026-11-13T03:00:00Z', probability: 7, run: '' },
        { start: '2026-11-13T03:00:00Z', end: '2026-11-13T06:00:00Z', probability: 0.5, run: '' },
      ],
    })))
    const { result } = renderHook(() => useLightning())
    await act(async () => { await Promise.resolve() })

    expect(result.current.windows).toHaveLength(1)
    expect(result.current.windows[0].probability).toBe(0.5)
    expect(consoleError).toHaveBeenCalled()
    consoleError.mockRestore()
  })

  it('asks again every ten seconds while a refresh runs, then on the TTL', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(answer({ windows: [], refreshing: true }))
      .mockResolvedValueOnce(answer({ windows: [], refreshing: true }))
      .mockResolvedValue(answer())
    vi.stubGlobal('fetch', fetchMock)
    renderHook(() => useLightning())
    await act(async () => { await Promise.resolve() })
    expect(fetchMock).toHaveBeenCalledTimes(1)

    await act(async () => { await vi.advanceTimersByTimeAsync(10_000) })
    expect(fetchMock).toHaveBeenCalledTimes(2)
    await act(async () => { await vi.advanceTimersByTimeAsync(10_000) })
    expect(fetchMock).toHaveBeenCalledTimes(3)

    // Windows are held now: the next ask is a TTL away, not ten seconds.
    await act(async () => { await vi.advanceTimersByTimeAsync(60_000) })
    expect(fetchMock).toHaveBeenCalledTimes(3)
    await act(async () => { await vi.advanceTimersByTimeAsync(21_600_000) })
    expect(fetchMock).toHaveBeenCalledTimes(4)
  })

  it('lets a new error win over the stale refreshing answer it replaces', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(answer({ windows: [], refreshing: true }))
      .mockResolvedValue({
        ok: false,
        status: 502,
        json: async () => ({ error: 'no run found' }),
      })
    vi.stubGlobal('fetch', fetchMock)
    const { result } = renderHook(() => useLightning())
    await act(async () => { await Promise.resolve() })
    expect(fetchMock).toHaveBeenCalledTimes(1)

    await act(async () => { await vi.advanceTimersByTimeAsync(10_000) })
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(result.current.error).toBe('no run found')

    // The poll is the error interval now, not ten seconds.
    await act(async () => { await vi.advanceTimersByTimeAsync(60_000) })
    expect(fetchMock).toHaveBeenCalledTimes(2)
    await act(async () => { await vi.advanceTimersByTimeAsync(540_000) })
    expect(fetchMock).toHaveBeenCalledTimes(3)
  })

  it('asks every two minutes, not ten seconds, while refreshing behind windows it already holds', async () => {
    const fetchMock = vi.fn().mockResolvedValue(answer({ refreshing: true }))
    vi.stubGlobal('fetch', fetchMock)
    renderHook(() => useLightning())
    await act(async () => { await Promise.resolve() })
    expect(fetchMock).toHaveBeenCalledTimes(1)

    await act(async () => { await vi.advanceTimersByTimeAsync(110_000) })
    expect(fetchMock).toHaveBeenCalledTimes(1)
    await act(async () => { await vi.advanceTimersByTimeAsync(10_000) })
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })
})

describe('useLightning polling after a failed poll', () => {
  beforeEach(() => { vi.useFakeTimers() })
  afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals() })

  it('retries after ten minutes even while windows are held', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(answer({ ttl_seconds: 60 }))
      .mockResolvedValueOnce({ ok: false, status: 502, json: async () => ({ error: 'no run found' }) })
      .mockResolvedValue(answer({ ttl_seconds: 60 }))
    vi.stubGlobal('fetch', fetchMock)
    const { result } = renderHook(() => useLightning())
    await act(async () => { await Promise.resolve() })
    expect(result.current.windows).toHaveLength(2)

    await act(async () => { await vi.advanceTimersByTimeAsync(60_000) })
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(result.current.error).toBe('no run found')
    expect(result.current.windows).toHaveLength(2)

    await act(async () => { await vi.advanceTimersByTimeAsync(540_000) })
    expect(fetchMock).toHaveBeenCalledTimes(2)
    await act(async () => { await vi.advanceTimersByTimeAsync(60_000) })
    expect(fetchMock).toHaveBeenCalledTimes(3)
  })
})

describe('useLightning polling with a refresh error', () => {
  beforeEach(() => { vi.useFakeTimers() })
  afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals() })

  it('asks at the ten-minute error interval, not the TTL, when the answer carries refresh_error', async () => {
    const fetchMock = vi.fn().mockResolvedValue(answer({ ttl_seconds: 60, refresh_error: 'bucket gone' }))
    vi.stubGlobal('fetch', fetchMock)
    renderHook(() => useLightning())
    await act(async () => { await Promise.resolve() })
    expect(fetchMock).toHaveBeenCalledTimes(1)

    await act(async () => { await vi.advanceTimersByTimeAsync(540_000) })
    expect(fetchMock).toHaveBeenCalledTimes(1)
    await act(async () => { await vi.advanceTimersByTimeAsync(60_000) })
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })
})
