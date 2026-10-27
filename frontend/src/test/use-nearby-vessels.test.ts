import { renderHook } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { useNearbyVessels } from '@/hooks/use-nearby-vessels'
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
