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

// ADR 0129 — maxGustTrueKts is a second, independent gust ladder alongside
// maxGustKts, so it needs the same unchanged-object memo (see the comment on
// setMaxGustTrueKts in use-vessel-state.ts) for the same reason: it arrives
// on the same 1Hz tick and must not defeat WindTile's own memoization when
// True mode is active and nothing in the true ladder has actually moved.
describe('useVesselState maxGustTrueKts identity', () => {
  it('keeps the same maxGustTrueKts object reference across ticks with identical values', () => {
    const payload = { ...basePayload, max_gust_true_kts: { '10m': 9.1, '30m': 10.4, '1h': 11.2, '24h': 16.0 } }
    const { result } = renderHook(() => useVesselState())

    emit(payload)
    const first = result.current.maxGustTrueKts

    emit({ ...payload, datetime: '2026-09-14T00:00:01Z' })
    const second = result.current.maxGustTrueKts

    expect(second).toBe(first)
  })

  it('returns a new maxGustTrueKts object once a true gust value actually changes', () => {
    const payload = { ...basePayload, max_gust_true_kts: { '10m': 9.1, '30m': 10.4, '1h': 11.2, '24h': 16.0 } }
    const { result } = renderHook(() => useVesselState())

    emit(payload)
    const first = result.current.maxGustTrueKts

    emit({ ...payload, max_gust_true_kts: { ...payload.max_gust_true_kts, '10m': 9.9 } })
    const second = result.current.maxGustTrueKts

    expect(second).not.toBe(first)
    expect(second['10m']).toBe(9.9)
  })

  it('starts with a stable all-null object before any event arrives', () => {
    const { result } = renderHook(() => useVesselState())

    expect(result.current.maxGustTrueKts).toEqual({ '10m': null, '30m': null, '1h': null, '24h': null })
  })

  // Code-review fix (2026-09-25): backend/main.go's max_gust_true_kts clamp
  // used to turn a window with no true-wind samples into 0 kt, which read
  // as a confidently calm reading rather than "unknown". The fix keeps -1
  // for a window with nothing recorded; this proves the -1-to-null mapping
  // already used for every other sentinel-bearing field also covers this
  // one end to end, so WindTile's True mode shows '—', not "0.0 kts".
  it('maps the backend\'s -1 "no data" sentinel to null per window, not to a false 0kt reading', () => {
    const payload = { ...basePayload, max_gust_true_kts: { '10m': -1, '30m': -1, '1h': 9.4, '24h': 9.4 } }
    const { result } = renderHook(() => useVesselState())

    emit(payload)

    expect(result.current.maxGustTrueKts).toEqual({ '10m': null, '30m': null, '1h': 9.4, '24h': 9.4 })
  })
})

// ADR 0129 — true wind parses the same -1/absent-is-null way as apparent,
// but into its own fields, and must not borrow apparent's values when true
// wind itself is absent from the payload.
describe('useVesselState true wind', () => {
  it('parses true wind fields when the payload carries them', () => {
    const payload = {
      ...basePayload,
      wind_speed_apparent_kts: 8,
      wind_angle_apparent_deg: 40,
      wind_side: 'port',
      wind_angle_relative_deg: 40,
      wind_speed_true_kts: 14,
      wind_angle_true_deg: 55,
      wind_side_true: 'starboard',
      wind_angle_true_relative_deg: 55,
      wind_direction_true_deg: 210,
    }
    const { result } = renderHook(() => useVesselState())

    emit(payload)

    expect(result.current.windSpeedTrueKts).toBe(14)
    expect(result.current.windAngleTrueDeg).toBe(55)
    expect(result.current.windSideTrue).toBe('starboard')
    expect(result.current.windAngleTrueRelativeDeg).toBe(55)
    expect(result.current.windDirectionTrueDeg).toBe(210)
    // Apparent must have parsed independently, not been overwritten by true.
    expect(result.current.windSpeedApparentKts).toBe(8)
    expect(result.current.windSide).toBe('port')
  })

  it('reads true wind as null when the backend sends the -1/"" absent sentinel, without borrowing apparent', () => {
    const payload = {
      ...basePayload,
      wind_speed_apparent_kts: 8,
      wind_angle_apparent_deg: 40,
      wind_side: 'port',
      wind_angle_relative_deg: 40,
      wind_speed_true_kts: -1,
      wind_angle_true_deg: -1,
      wind_side_true: '',
      wind_angle_true_relative_deg: -1,
      wind_direction_true_deg: -1,
    }
    const { result } = renderHook(() => useVesselState())

    emit(payload)

    expect(result.current.windSpeedTrueKts).toBeNull()
    expect(result.current.windAngleTrueDeg).toBeNull()
    expect(result.current.windSideTrue).toBeNull()
    expect(result.current.windAngleTrueRelativeDeg).toBeNull()
    expect(result.current.windDirectionTrueDeg).toBeNull()
    // Not silently substituted with apparent's numbers.
    expect(result.current.windSpeedTrueKts).not.toBe(result.current.windSpeedApparentKts)
  })

  it('starts with true wind fields null before any event arrives', () => {
    const { result } = renderHook(() => useVesselState())

    expect(result.current.windSpeedTrueKts).toBeNull()
    expect(result.current.windSideTrue).toBeNull()
  })
})
