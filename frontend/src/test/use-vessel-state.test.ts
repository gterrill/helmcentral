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

// ADR 0129: the Current Conditions tile's true-wind readout. windSpeedTrueKts/
// windDirectionTrueDeg/maxTrueWindKts1h follow the same -1-sentinel-becomes-null
// contract every other sentinel-bearing field in this hook already follows
// (see windSpeedApparentKts above), so a boat with no true-wind source reads
// as null, never a fabricated 0.
describe('useVesselState true wind fields', () => {
  it('parses windSpeedTrueKts, windDirectionTrueDeg and maxTrueWindKts1h when present', () => {
    const { result } = renderHook(() => useVesselState())

    emit({
      ...basePayload,
      wind_speed_true_kts: 13.4,
      wind_direction_true_deg: 126,
      max_true_wind_kts_1h: 15.9,
    })

    expect(result.current.windSpeedTrueKts).toBe(13.4)
    expect(result.current.windDirectionTrueDeg).toBe(126)
    expect(result.current.maxTrueWindKts1h).toBe(15.9)
  })

  it('reads the -1 sentinel as null rather than a negative reading, and never falls back to apparent', () => {
    const { result } = renderHook(() => useVesselState())

    emit({
      ...basePayload,
      wind_speed_apparent_kts: 9.5,
      wind_speed_true_kts: -1,
      wind_direction_true_deg: -1,
      max_true_wind_kts_1h: -1,
    })

    expect(result.current.windSpeedTrueKts).toBeNull()
    expect(result.current.windDirectionTrueDeg).toBeNull()
    expect(result.current.maxTrueWindKts1h).toBeNull()
  })

  it('starts null before any event arrives', () => {
    const { result } = renderHook(() => useVesselState())

    expect(result.current.windSpeedTrueKts).toBeNull()
    expect(result.current.windDirectionTrueDeg).toBeNull()
    expect(result.current.maxTrueWindKts1h).toBeNull()
  })
})
