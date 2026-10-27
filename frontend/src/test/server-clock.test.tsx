/**
 * The boat's clock (ADR 0218): server time as the base, ticking locally while
 * a clock reader is mounted, and nothing when none is.
 */
import { act, renderHook } from '@testing-library/react'
import { useSyncExternalStore } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { feedServerTime, readServerClock, subscribeServerClock } from '@/lib/server-clock'

// The module is a source, not a hook; a clock reader (useVesselClock) is the
// only React interface. This stands in for it without the vessel-state store.
const useServerNow = () => useSyncExternalStore(subscribeServerClock, readServerClock)

beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date('2030-01-01T00:00:00Z')) // the browser's clock is deliberately wrong
})

afterEach(() => {
  vi.useRealTimers()
})

describe('server clock', () => {
  it('is not moved by a jump of the browser wall clock (an NTP correction)', () => {
    const { result } = renderHook(() => useServerNow())
    act(() => { feedServerTime('2026-10-27T10:00:00Z') })
    act(() => { vi.advanceTimersByTime(1000) })
    act(() => { vi.setSystemTime(new Date('2031-06-01T00:00:00Z')) }) // Date jumps five years
    act(() => { vi.advanceTimersByTime(1000) })
    expect(result.current?.toISOString()).toBe('2026-10-27T10:00:02.000Z')
  })

  it('counts from when a payload arrived, so a replayed one is aged by how long it was remembered', () => {
    const arrived = performance.now()
    act(() => { vi.advanceTimersByTime(5 * 60_000) })
    feedServerTime('2026-10-27T10:00:00Z', arrived)
    const { result } = renderHook(() => useServerNow())
    expect(result.current?.toISOString()).toBe('2026-10-27T10:05:00.000Z')
  })

  it('does not step the shown time backwards for a small correction, but applies a large one', () => {
    const { result } = renderHook(() => useServerNow())
    act(() => { feedServerTime('2026-10-27T10:00:10Z') })
    // Jitter: the next stamp is 1.5 s behind what the clock already shows.
    act(() => { feedServerTime('2026-10-27T10:00:08.500Z') })
    expect(result.current?.toISOString()).toBe('2026-10-27T10:00:10.000Z')
    act(() => { vi.advanceTimersByTime(1000) })
    expect(result.current?.toISOString()).toBe('2026-10-27T10:00:11.000Z')
    // A real change of the server's time is 3 s behind: applied as it is.
    act(() => { feedServerTime('2026-10-27T10:00:08Z') })
    expect(result.current?.toISOString()).toBe('2026-10-27T10:00:08.000Z')
  })

  it('moves forward to a later stamp at once', () => {
    const { result } = renderHook(() => useServerNow())
    act(() => { feedServerTime('2026-10-27T10:00:10Z') })
    act(() => { feedServerTime('2026-10-27T10:00:13Z') })
    expect(result.current?.toISOString()).toBe('2026-10-27T10:00:13.000Z')
  })

  it('is unknown, not the browser time, before any event carried a datetime', () => {
    const { result } = renderHook(() => useServerNow())
    expect(result.current).toBeNull()
  })

  it('reads the server datetime, not the browser clock', () => {
    const { result } = renderHook(() => useServerNow())
    act(() => { feedServerTime('2026-10-27T10:00:00Z') })
    expect(result.current?.toISOString()).toBe('2026-10-27T10:00:00.000Z')
  })

  it('ticks from the server time each second while a reader is mounted', () => {
    const { result } = renderHook(() => useServerNow())
    act(() => { feedServerTime('2026-10-27T10:00:00Z') })

    act(() => { vi.advanceTimersByTime(1000) })
    expect(result.current?.toISOString()).toBe('2026-10-27T10:00:01.000Z')
    act(() => { vi.advanceTimersByTime(2000) })
    expect(result.current?.toISOString()).toBe('2026-10-27T10:00:03.000Z')
  })

  it('re-bases on each new datetime', () => {
    const { result } = renderHook(() => useServerNow())
    act(() => { feedServerTime('2026-10-27T10:00:00Z') })
    act(() => { vi.advanceTimersByTime(2500) })
    act(() => { feedServerTime('2026-10-27T10:00:07Z') })
    expect(result.current?.toISOString()).toBe('2026-10-27T10:00:07.000Z')
  })

  it('keeps counting from the last good stamp when an event carries none or a bad one', () => {
    const { result } = renderHook(() => useServerNow())
    act(() => { feedServerTime('2026-10-27T10:00:00Z') })
    act(() => { feedServerTime(undefined); feedServerTime('not a date'); feedServerTime(42) })
    act(() => { vi.advanceTimersByTime(1000) })
    expect(result.current?.toISOString()).toBe('2026-10-27T10:00:01.000Z')
  })

  it('runs no timer while no clock reader is mounted, and shows current time to the next one', () => {
    feedServerTime('2026-10-27T10:00:00Z')
    expect(vi.getTimerCount()).toBe(0)

    act(() => { vi.advanceTimersByTime(60_000) })
    const { result, unmount } = renderHook(() => useServerNow())
    expect(result.current?.toISOString()).toBe('2026-10-27T10:01:00.000Z')
    expect(vi.getTimerCount()).toBe(1)

    unmount()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('runs one timer however many readers there are', () => {
    feedServerTime('2026-10-27T10:00:00Z')
    const a = renderHook(() => useServerNow())
    const b = renderHook(() => useServerNow())
    expect(vi.getTimerCount()).toBe(1)
    a.unmount()
    expect(vi.getTimerCount()).toBe(1)
    b.unmount()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('starts ticking when the first datetime arrives after a reader mounted', () => {
    const { result } = renderHook(() => useServerNow())
    expect(vi.getTimerCount()).toBe(0)
    act(() => { feedServerTime('2026-10-27T10:00:00Z') })
    expect(vi.getTimerCount()).toBe(1)
    act(() => { vi.advanceTimersByTime(1000) })
    expect(result.current?.toISOString()).toBe('2026-10-27T10:00:01.000Z')
  })
})
