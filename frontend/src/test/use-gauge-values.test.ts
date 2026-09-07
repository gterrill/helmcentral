import { describe, expect, it, vi, beforeEach } from 'vitest'
import { act, renderHook } from '@testing-library/react'

// use-gauge-values.ts is a thin telemetry consumer, same shape as
// use-radar-targets.ts: it subscribes to one SSE event and maps the payload.
// Mock the shared telemetry module rather than standing up a real
// EventSource -- use-telemetry-stream.test.ts already covers the transport.
const subscribeTelemetryMock = vi.fn()
let capturedListener: ((raw: string) => void) | null = null

vi.mock('@/hooks/use-telemetry-stream', () => ({
  subscribeTelemetry: (event: string, cb: (raw: string) => void) => {
    capturedListener = cb
    subscribeTelemetryMock(event, cb)
    return () => {}
  },
}))

import { useGaugeAges } from '@/hooks/use-gauge-values'

function emit(payload: unknown) {
  act(() => {
    capturedListener?.(JSON.stringify(payload))
  })
}

beforeEach(() => {
  subscribeTelemetryMock.mockClear()
  capturedListener = null
})

/**
 * ADR 0083: ages ride the same gauge-values event useGaugeValues does,
 * rather than a stream of their own -- the backend computes both from the
 * same sample in the same tick, so a sibling hook is all a widget needs.
 */
describe('useGaugeAges', () => {
  it('subscribes to the same gauge-values event useGaugeValues does', () => {
    renderHook(() => useGaugeAges())
    expect(subscribeTelemetryMock).toHaveBeenCalledWith('gauge-values', expect.any(Function))
  })

  it('starts empty', () => {
    const { result } = renderHook(() => useGaugeAges())
    expect(result.current).toEqual({})
  })

  it('reads the ages map off the payload', () => {
    const { result } = renderHook(() => useGaugeAges())
    emit({ values: {}, ages: { 'propulsion.port.revolutions': 45.2 } })
    expect(result.current).toEqual({ 'propulsion.port.revolutions': 45.2 })
  })

  // The backend's -1 sentinel (ADR 0068) means "no timestamp at all", which
  // must read as unknown rather than as a very fresh negative age.
  it('maps -1 to null through ageFromPayload, so unknown never reads as fresh', () => {
    const { result } = renderHook(() => useGaugeAges())
    emit({ values: {}, ages: { 'environment.outside.pressure': -1, 'navigation.speedOverGround': 12 } })
    expect(result.current).toEqual({ 'environment.outside.pressure': null, 'navigation.speedOverGround': 12 })
  })

  it('ignores a malformed payload rather than throwing', () => {
    const { result } = renderHook(() => useGaugeAges())
    act(() => {
      capturedListener?.('not json')
    })
    expect(result.current).toEqual({})
  })
})
