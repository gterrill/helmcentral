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
    expect(result.current).toEqual({ source: 'signalk', maxRangeM: 5000, aisHeardAgeS: null })
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

describe('AIS heartbeat and the cap', () => {
  it('reads ais_heard_age_s, with -1 meaning nothing heard and a missing value unknown', () => {
    const { result } = renderHook(() => useNearbyVessels())
    emit({ vessels: [], source: 'signalk' })
    expect(result.current.aisHeardAgeS).toBeNull()
    emit({ vessels: [], source: 'signalk', ais_heard_age_s: -1 })
    expect(result.current.aisHeardAgeS).toBe(-1)
    emit({ vessels: [], source: 'signalk', ais_heard_age_s: 900 })
    expect(result.current.aisHeardAgeS).toBe(900)
    emit({ vessels: [], source: 'signalk', ais_heard_age_s: 'soon' })
    expect(result.current.aisHeardAgeS).toBeNull()
  })

  it('keeps the same feed object while the AIS stays heard, however its age ticks', () => {
    const { result } = renderHook(() => useNearbyVesselFeed())
    emit({ vessels: [], source: 'signalk', max_range_m: 5000, ais_heard_age_s: 5 })
    const first = result.current
    emit({ vessels: [], source: 'signalk', max_range_m: 5000, ais_heard_age_s: 10 })
    expect(result.current).toBe(first)
  })

  it('updates the feed once the AIS has been quiet long enough to say so', () => {
    const { result } = renderHook(() => useNearbyVesselFeed())
    emit({ vessels: [], source: 'signalk', max_range_m: 5000, ais_heard_age_s: 590 })
    emit({ vessels: [], source: 'signalk', max_range_m: 5000, ais_heard_age_s: 700 })
    expect(result.current?.aisHeardAgeS).toBe(700)
  })

  it('reads capped_count, and treats a missing or bad one as zero', () => {
    const { result } = renderHook(() => useNearbyVessels())
    emit({ vessels: [], capped_count: 16 })
    expect(result.current.cappedCount).toBe(16)
    emit({ vessels: [], capped_count: 'many' })
    expect(result.current.cappedCount).toBe(0)
  })
})
