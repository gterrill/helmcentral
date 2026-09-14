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

import { useGaugeAges, useGaugeValues } from '@/hooks/use-gauge-values'

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

describe('useGaugeValues', () => {
  it('subscribes to the gauge-values event', () => {
    renderHook(() => useGaugeValues())
    expect(subscribeTelemetryMock).toHaveBeenCalledWith('gauge-values', expect.any(Function))
  })

  it('starts empty', () => {
    const { result } = renderHook(() => useGaugeValues())
    expect(result.current).toEqual({})
  })

  it('reads the values map off the payload', () => {
    const { result } = renderHook(() => useGaugeValues())
    emit({ values: { 'propulsion.port.revolutions': 1200 }, ages: {} })
    expect(result.current).toEqual({ 'propulsion.port.revolutions': 1200 })
  })

  it('ignores a malformed payload rather than throwing', () => {
    const { result } = renderHook(() => useGaugeValues())
    act(() => {
      capturedListener?.('not json')
    })
    expect(result.current).toEqual({})
  })
})

// Item D: values/ages used to be a fresh object literal on every single
// gauge-values tick (1Hz), even on ticks where nothing bound to a gauge
// actually changed -- defeating any memoized tile reading them. Both hooks
// also used to parse the same payload independently; they now share one
// subscription (below).
describe('useGaugeValues/useGaugeAges stable identity and shared subscription', () => {
  it('keeps the same values object reference across ticks with identical values', () => {
    const { result } = renderHook(() => useGaugeValues())

    emit({ values: { 'tanks.fuel.2.currentLevel': 5.6 }, ages: { 'tanks.fuel.2.currentLevel': 1 } })
    const first = result.current

    emit({ values: { 'tanks.fuel.2.currentLevel': 5.6 }, ages: { 'tanks.fuel.2.currentLevel': 2 } })
    const second = result.current

    expect(second).toBe(first)
  })

  it('returns a new values object once a bound value actually changes', () => {
    const { result } = renderHook(() => useGaugeValues())

    emit({ values: { 'tanks.fuel.2.currentLevel': 5.6 }, ages: {} })
    const first = result.current

    emit({ values: { 'tanks.fuel.2.currentLevel': 5.4 }, ages: {} })
    const second = result.current

    expect(second).not.toBe(first)
    expect(second['tanks.fuel.2.currentLevel']).toBe(5.4)
  })

  it('keeps the same ages object reference across ticks with identical ages', () => {
    const { result } = renderHook(() => useGaugeAges())

    emit({ values: {}, ages: { 'propulsion.port.revolutions': 4 } })
    const first = result.current

    emit({ values: {}, ages: { 'propulsion.port.revolutions': 4 } })
    const second = result.current

    expect(second).toBe(first)
  })

  it('subscribes to gauge-values exactly once when both hooks are mounted together', () => {
    renderHook(() => {
      useGaugeValues()
      useGaugeAges()
    })

    expect(subscribeTelemetryMock).toHaveBeenCalledTimes(1)
  })

  it('keeps values and ages in sync from one shared emit', () => {
    const { result } = renderHook(() => ({ values: useGaugeValues(), ages: useGaugeAges() }))

    emit({ values: { 'a.path': 1 }, ages: { 'a.path': 3 } })

    expect(result.current.values).toEqual({ 'a.path': 1 })
    expect(result.current.ages).toEqual({ 'a.path': 3 })
  })
})
