import { describe, expect, it, vi, beforeEach } from 'vitest'
import { act, renderHook } from '@testing-library/react'

// use-vessel-state.ts is a thin telemetry consumer over the shared SSE
// connection, same shape as use-radar-targets.ts / use-gauge-values.ts. Mock
// the shared telemetry module rather than standing up a real EventSource --
// use-telemetry-stream.test.ts already covers the transport.
const subscribeTelemetryMock = vi.fn()
let capturedListener: ((raw: string) => void) | null = null

vi.mock('@/hooks/use-telemetry-stream', () => ({
  subscribeTelemetry: (event: string, cb: (raw: string) => void) => {
    capturedListener = cb
    subscribeTelemetryMock(event, cb)
    return () => {}
  },
}))

import { useVesselState } from '@/hooks/use-vessel-state'

function emit(payload: unknown) {
  act(() => {
    capturedListener?.(JSON.stringify(payload))
  })
}

const basePayload = {
  status: 'anchored',
  datetime: '2026-09-14T00:00:00Z',
  depth: 4.6,
  latitude: -20.07,
  longitude: 148.43,
  heading_true: 70,
  speed_over_ground_kts: 0.4,
  max_gust_kts: { '10m': 12.3, '30m': 14.1, '1h': 15.2, '24h': 20.1 },
}

beforeEach(() => {
  subscribeTelemetryMock.mockClear()
  capturedListener = null
})

// Item D: every SSE vessel-state tick (1Hz) re-renders App and, before this
// fix, handed every downstream consumer of maxGustKts a brand new object
// literal even when the gust figures themselves hadn't moved -- defeating
// any memoized tile reading it (e.g. WindTile).
describe('useVesselState maxGustKts identity', () => {
  it('keeps the same maxGustKts object reference across ticks with identical values', () => {
    const { result } = renderHook(() => useVesselState())

    emit(basePayload)
    const first = result.current.maxGustKts

    emit({ ...basePayload, datetime: '2026-09-14T00:00:01Z' })
    const second = result.current.maxGustKts

    expect(second).toBe(first)
  })

  it('returns a new maxGustKts object once a gust value actually changes', () => {
    const { result } = renderHook(() => useVesselState())

    emit(basePayload)
    const first = result.current.maxGustKts

    emit({ ...basePayload, max_gust_kts: { ...basePayload.max_gust_kts, '10m': 13.0 } })
    const second = result.current.maxGustKts

    expect(second).not.toBe(first)
    expect(second['10m']).toBe(13.0)
  })

  it('starts with a stable all-null object before any event arrives', () => {
    const { result } = renderHook(() => useVesselState())

    expect(result.current.maxGustKts).toEqual({ '10m': null, '30m': null, '1h': null, '24h': null })
  })
})
