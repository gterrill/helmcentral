import { act, renderHook } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

let listener: ((raw: string) => void) | null = null
vi.mock('@/hooks/use-telemetry-stream', () => ({
  subscribeTelemetry: (_event: string, cb: (raw: string) => void) => {
    listener = cb
    return () => {}
  },
}))

import { useNearbyVessels } from '@/hooks/use-nearby-vessels'

const emit = (payload: object) => act(() => { listener?.(JSON.stringify(payload)) })

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
