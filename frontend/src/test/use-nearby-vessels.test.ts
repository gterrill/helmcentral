import { renderHook } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { useNearbyVesselFeed, useNearbyVessels } from '@/hooks/use-nearby-vessels'
import { installFakeTelemetryStream } from './helpers/fake-telemetry-stream'

const stream = installFakeTelemetryStream()
const emit = (payload: object) => stream.emit('nearby-vessels', payload)

describe('useNearbyVessels maxRangeM', () => {
  it('is null until a payload carries it', () => {
    const { result } = renderHook(() => useNearbyVessels())
    expect(result.current.maxRangeM).toBeNull()
    emit({ vessels: [] })
    expect(result.current.maxRangeM).toBeNull()
  })

  it('carries max_range_m from the payload', () => {
    const { result } = renderHook(() => useNearbyVessels())
    emit({ vessels: [], max_range_m: 5000 })
    expect(result.current.maxRangeM).toBe(5000)
  })

  it('ignores a malformed value rather than trusting it', () => {
    const { result } = renderHook(() => useNearbyVessels())
    emit({ vessels: [], max_range_m: '5000' })
    expect(result.current.maxRangeM).toBeNull()
  })
})

describe('nearby vessel feed status', () => {
  it('is null until the first event, so a tile can say loading rather than no vessels', () => {
    const { result } = renderHook(() => useNearbyVesselFeed())
    expect(result.current).toBeNull()
    emit({ vessels: [], source: 'signalk', max_range_m: 5000 })
    expect(result.current).toEqual({ source: 'signalk', maxRangeM: 5000 })
  })

  it('carries the source, and reads a missing or malformed one as null', () => {
    const { result } = renderHook(() => useNearbyVesselFeed())
    emit({ vessels: [], source: 'backend-fallback' })
    expect(result.current?.source).toBe('backend-fallback')
    emit({ vessels: [], source: 42 })
    expect(result.current?.source).toBeNull()
  })

  it('keeps the same object when only vessel ages tick', () => {
    const { result } = renderHook(() => useNearbyVesselFeed())
    const v = { id: 'a', name: 'A', range_m: 10, age_seconds: 1 }
    emit({ vessels: [v], source: 'signalk', max_range_m: 5000 })
    const first = result.current
    emit({ vessels: [{ ...v, age_seconds: 6 }], source: 'signalk', max_range_m: 5000 })
    expect(result.current).toBe(first)
  })
})
