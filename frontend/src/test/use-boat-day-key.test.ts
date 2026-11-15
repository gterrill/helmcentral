/**
 * The boat's calendar day as the server states it (ADR 0230): the browser never
 * works it out, so a zone its Intl data lacks cannot stop the day turning.
 * These run through the real vessel-state store, so the field name on the
 * wire is covered too.
 */
import { renderHook } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { useBoatDayKey } from '@/hooks/use-vessel-clock'
import { installFakeTelemetryStream } from './helpers/fake-telemetry-stream'

const stream = installFakeTelemetryStream()

const event = (extra: Record<string, unknown>) => ({ source: 'signalk', datetime: '2026-11-15T13:59:00Z', ...extra })

describe('useBoatDayKey', () => {
  it('is the day the server sent', () => {
    stream.seed('vessel-state', event({ timezone: 'Australia/Sydney', timezone_source: 'fix', boat_date: '2026-11-16' }))
    const { result } = renderHook(() => useBoatDayKey())
    expect(result.current).toBe('2026-11-16')
  })

  it('still has the day when this browser does not know the boat\'s zone', () => {
    stream.seed('vessel-state', event({ timezone: 'Not/AZone', timezone_source: 'fix', boat_date: '2026-11-16' }))
    const { result } = renderHook(() => useBoatDayKey())
    expect(result.current).toBe('2026-11-16')
  })

  it('follows the server when the day turns', () => {
    stream.seed('vessel-state', event({ timezone: 'Not/AZone', timezone_source: 'fix', boat_date: '2026-11-15' }))
    const { result } = renderHook(() => useBoatDayKey())
    expect(result.current).toBe('2026-11-15')
    stream.emit('vessel-state', event({ timezone: 'Not/AZone', timezone_source: 'fix', boat_date: '2026-11-16' }))
    expect(result.current).toBe('2026-11-16')
  })

  it('is null before the first fix, when the server sends no day', () => {
    stream.seed('vessel-state', event({ timezone: 'UTC', timezone_source: 'none', boat_date: null }))
    const { result } = renderHook(() => useBoatDayKey())
    expect(result.current).toBeNull()
  })

  it('is null from a server that sends no day at all', () => {
    stream.seed('vessel-state', event({ timezone: 'Australia/Sydney', timezone_source: 'fix' }))
    const { result } = renderHook(() => useBoatDayKey())
    expect(result.current).toBeNull()
  })
})
