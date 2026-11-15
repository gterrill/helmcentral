/**
 * The boat's calendar day as the browser reads it (ADR 0230): the server clock
 * in the boat's zone, used only to know when the day has turned.
 */
import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { feedServerTime } from '@/lib/server-clock'

let zone: string | undefined = 'Australia/Sydney'
let zoneSource: 'fix' | 'last_fix' | 'none' | undefined = 'fix'
vi.mock('@/hooks/use-vessel-state', () => ({
  useVesselStateSelector: <T,>(select: (state: { timeZone?: string; timeZoneSource?: string }) => T) =>
    select({ timeZone: zone, timeZoneSource: zoneSource }),
}))

const { useBoatDayKey } = await import('@/hooks/use-vessel-clock')

beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date('2030-01-01T00:00:00Z')) // the browser's clock is deliberately wrong
})

afterEach(() => {
  vi.useRealTimers()
  zone = 'Australia/Sydney'
  zoneSource = 'fix'
})

describe('useBoatDayKey', () => {
  it('is the day on the server clock in the boat\'s zone, not the browser\'s', () => {
    const { result } = renderHook(() => useBoatDayKey())
    // 13:59 UTC on 15 Nov is 00:59 on 16 Nov in Sydney (AEDT, UTC+11).
    act(() => { feedServerTime('2026-11-15T13:59:00Z') })
    expect(result.current).toBe('2026-11-16')
  })

  it('turns at the boat\'s midnight', () => {
    const { result } = renderHook(() => useBoatDayKey())
    act(() => { feedServerTime('2026-11-15T12:59:58Z') })
    expect(result.current).toBe('2026-11-15')
    act(() => { vi.advanceTimersByTime(3000) })
    expect(result.current).toBe('2026-11-16')
  })

  it('is null before the first fix, when the server sends UTC as a stand-in zone', () => {
    zone = 'UTC'
    zoneSource = 'none'
    const { result } = renderHook(() => useBoatDayKey())
    act(() => { feedServerTime('2026-11-15T12:00:00Z') })
    expect(result.current).toBeNull()
  })

  it('counts the zone of the last fix', () => {
    zoneSource = 'last_fix'
    const { result } = renderHook(() => useBoatDayKey())
    act(() => { feedServerTime('2026-11-15T13:59:00Z') })
    expect(result.current).toBe('2026-11-16')
  })

  it('is null until the boat has a zone', () => {
    zone = undefined
    const { result } = renderHook(() => useBoatDayKey())
    act(() => { feedServerTime('2026-11-15T12:00:00Z') })
    expect(result.current).toBeNull()
  })
})
