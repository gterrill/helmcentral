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

import {
  useEngineStatus,
  useGaugeAge,
  useGaugeSubset,
  useGaugeValue,
} from '@/hooks/use-gauge-values'

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
 * ADR 0083: ages ride the same gauge-values event the values do, rather than
 * a stream of their own -- the backend computes both from the same sample in
 * the same tick.
 */
describe('useGaugeAge', () => {
  it('subscribes to the same gauge-values event the values ride', () => {
    renderHook(() => useGaugeAge('a'))
    expect(subscribeTelemetryMock).toHaveBeenCalledWith('gauge-values', expect.any(Function))
  })

  it('starts unknown', () => {
    const { result } = renderHook(() => useGaugeAge('propulsion.port.revolutions'))
    expect(result.current).toBeNull()
  })

  it('reads the age off the payload', () => {
    const { result } = renderHook(() => useGaugeAge('propulsion.port.revolutions'))
    emit({ values: {}, ages: { 'propulsion.port.revolutions': 45.2 } })
    expect(result.current).toBe(45.2)
  })

  // The backend's -1 sentinel (ADR 0068) means "no timestamp at all", which
  // must read as unknown rather than as a very fresh negative age.
  it('maps -1 to null through ageFromPayload, so unknown never reads as fresh', () => {
    const { result } = renderHook(() => useGaugeSubset(['environment.outside.pressure', 'navigation.speedOverGround']))
    emit({ values: {}, ages: { 'environment.outside.pressure': -1, 'navigation.speedOverGround': 12 } })
    expect(result.current.ages).toEqual({ 'environment.outside.pressure': null, 'navigation.speedOverGround': 12 })
  })

  it('ignores a malformed payload rather than throwing', () => {
    const { result } = renderHook(() => useGaugeAge('a'))
    act(() => {
      capturedListener?.('not json')
    })
    expect(result.current).toBeNull()
  })
})

describe('useGaugeValue', () => {
  it('subscribes to the gauge-values event', () => {
    renderHook(() => useGaugeValue('a'))
    expect(subscribeTelemetryMock).toHaveBeenCalledWith('gauge-values', expect.any(Function))
  })

  it('starts with no value', () => {
    const { result } = renderHook(() => useGaugeValue('a'))
    expect(result.current).toBeNull()
  })

  it('reads the value off the payload', () => {
    const { result } = renderHook(() => useGaugeValue('propulsion.port.revolutions'))
    emit({ values: { 'propulsion.port.revolutions': 1200 }, ages: {} })
    expect(result.current).toBe(1200)
  })

  it('ignores a malformed payload rather than throwing', () => {
    const { result } = renderHook(() => useGaugeValue('a'))
    act(() => {
      capturedListener?.('not json')
    })
    expect(result.current).toBeNull()
  })
})

describe('shared subscription', () => {
  it('subscribes to gauge-values exactly once when several readers are mounted together', () => {
    renderHook(() => {
      useGaugeValue('a')
      useGaugeAge('a')
    })

    expect(subscribeTelemetryMock).toHaveBeenCalledTimes(1)
  })

  it('keeps values and ages in sync from one shared emit', () => {
    const { result } = renderHook(() => useGaugeSubset(['a.path']))

    emit({ values: { 'a.path': 1 }, ages: { 'a.path': 3 } })

    expect(result.current).toEqual({ values: { 'a.path': 1 }, ages: { 'a.path': 3 } })
  })
})

describe('useEngineStatus', () => {
  it('starts with no status', () => {
    const { result } = renderHook(() => useEngineStatus('port'))
    expect(result.current).toBeUndefined()
  })

  it('reads the engine off the payload, mapping last_update', () => {
    const { result } = renderHook(() => ({ port: useEngineStatus('port'), starboard: useEngineStatus('starboard') }))
    emit({
      values: {}, ages: {},
      engines: { port: { state: 'off', last_update: '2026-10-06T04:30:00Z' }, starboard: { state: 'running' } },
    })
    expect(result.current).toEqual({
      port: { state: 'off', lastUpdate: '2026-10-06T04:30:00Z' },
      starboard: { state: 'running' },
    })
  })

  it('drops an unrecognised state rather than guessing', () => {
    const { result } = renderHook(() => useEngineStatus('port'))
    emit({ values: {}, ages: {}, engines: { port: { state: 'asleep' } } })
    expect(result.current).toBeUndefined()
  })
})

// ADR 0215: each tile reads only the paths it shows, so a tick re-renders only
// the tiles whose own reading changed.
describe('per-path selectors', () => {
  it('does not re-render a value subscriber when only another path changes', () => {
    let renders = 0
    const { result } = renderHook(() => { renders += 1; return useGaugeValue('a') })
    emit({ values: { a: 1, b: 1 }, ages: {} })
    expect(result.current).toBe(1)
    const after = renders

    emit({ values: { a: 1, b: 2 }, ages: {} })
    expect(renders).toBe(after)

    emit({ values: { a: 2, b: 2 }, ages: {} })
    expect(renders).toBe(after + 1)
    expect(result.current).toBe(2)
  })

  it('does not re-render an age subscriber when only another path ages', () => {
    let renders = 0
    const { result } = renderHook(() => { renders += 1; return useGaugeAge('a') })
    emit({ values: {}, ages: { a: 3, b: 3 } })
    expect(result.current).toBe(3)
    const after = renders

    emit({ values: {}, ages: { a: 3, b: 4 } })
    expect(renders).toBe(after)
  })

  it('reads null for a path the event does not carry', () => {
    const { result } = renderHook(() => ({ value: useGaugeValue('missing'), age: useGaugeAge('missing') }))
    emit({ values: { a: 1 }, ages: { a: 1 } })
    expect(result.current).toEqual({ value: null, age: null })
  })

  it('keeps the subset reference while its own paths are unchanged', () => {
    let renders = 0
    // A fresh array every render, as a tile computing its paths would pass.
    const { result } = renderHook(() => { renders += 1; return useGaugeSubset(['a', 'b']) })
    emit({ values: { a: 1, b: 2, c: 3 }, ages: { a: 5 } })
    const first = result.current
    expect(first).toEqual({ values: { a: 1, b: 2 }, ages: { a: 5 } })
    const after = renders

    emit({ values: { a: 1, b: 2, c: 4 }, ages: { a: 5, c: 9 } })
    expect(result.current).toBe(first)
    expect(renders).toBe(after)

    emit({ values: { a: 1, b: 3, c: 4 }, ages: { a: 5, c: 9 } })
    expect(result.current).not.toBe(first)
    expect(result.current).toEqual({ values: { a: 1, b: 3 }, ages: { a: 5 } })
  })

  it('keeps one engine\'s status when only another engine changes', () => {
    const { result } = renderHook(() => useEngineStatus('port'))
    emit({ values: {}, ages: {}, engines: { port: { state: 'off', last_update: 't1' }, starboard: { state: 'running' } } })
    const first = result.current
    expect(first).toEqual({ state: 'off', lastUpdate: 't1' })

    emit({ values: {}, ages: {}, engines: { port: { state: 'off', last_update: 't1' }, starboard: { state: 'off' } } })
    expect(result.current).toBe(first)
  })

  it('reads no status for an engine the server does not classify, or for no engine', () => {
    const { result } = renderHook(() => ({ port: useEngineStatus('port'), none: useEngineStatus(null) }))
    emit({ values: {}, ages: {}, engines: { starboard: { state: 'running' } } })
    expect(result.current).toEqual({ port: undefined, none: undefined })
  })

  it('still parses each tick once, whatever the number of subscribers', () => {
    const parse = vi.spyOn(JSON, 'parse')
    renderHook(() => {
      useGaugeValue('a')
      useGaugeAge('a')
      useGaugeSubset(['a', 'b'])
      useEngineStatus('port')
    })
    expect(subscribeTelemetryMock).toHaveBeenCalledTimes(1)
    parse.mockClear()
    emit({ values: { a: 1 }, ages: { a: 1 } })
    expect(parse).toHaveBeenCalledTimes(1)
    parse.mockRestore()
  })
})

describe('store lifetime', () => {
  it('keeps the last reading across the last tile unmounting and a new one mounting', () => {
    const tile = renderHook(() => useGaugeValue('a'))
    emit({ values: { a: 7 }, ages: { a: 1 } })
    tile.unmount()

    // A page switch or a wall rotation mounts the next page's tile before
    // the next tick: it must show the reading, not a blank.
    const next = renderHook(() => ({ value: useGaugeValue('a'), age: useGaugeAge('a') }))
    expect(next.result.current).toEqual({ value: 7, age: 1 })
    expect(subscribeTelemetryMock).toHaveBeenCalledTimes(1)

    next.unmount()
  })

  // The store stays subscribed once started, rather than emptying when its
  // last reader leaves: the shared connection would not re-send a
  // change-gated event to a store that came back to it (ADR 0215).
  it('keeps the reading, on the one subscription, after nothing reads it', () => {
    const tile = renderHook(() => useGaugeValue('a'))
    emit({ values: { a: 7 }, ages: {} })
    tile.unmount()
    const next = renderHook(() => useGaugeValue('a'))
    expect(next.result.current).toBe(7)
    expect(subscribeTelemetryMock).toHaveBeenCalledTimes(1)
  })
})

// The server rounds ages to 0.1 s, so on the live stream most bound ages move
// on every 1 s tick while far fewer values do. A tile uses an age only to ask
// whether it is stale and, once stale, to say how old it is, so an age that
// moves within the same answer must not re-render anything.
describe('ages that move without changing what a tile shows', () => {
  const rpm = 'propulsion.port.revolutions'
  const oil = 'propulsion.port.oilPressure'

  it('keeps the subset, and renders nothing, while ages move within the fresh band', () => {
    let renders = 0
    const { result } = renderHook(() => { renders += 1; return useGaugeSubset([rpm, oil]) })
    emit({ values: { [rpm]: 30.5, [oil]: 241325 }, ages: { [rpm]: 0.3, [oil]: 1.2 } })
    const first = result.current
    const after = renders

    emit({ values: { [rpm]: 30.5, [oil]: 241325 }, ages: { [rpm]: 0.7, [oil]: 2.2 } })
    expect(result.current).toBe(first)
    expect(renders).toBe(after)
  })

  it('does not re-render a single-path age reader while the age stays fresh', () => {
    let renders = 0
    renderHook(() => { renders += 1; return useGaugeAge(rpm) })
    emit({ values: {}, ages: { [rpm]: 0.3 } })
    const after = renders
    emit({ values: {}, ages: { [rpm]: 0.7 } })
    expect(renders).toBe(after)
  })

  it('re-renders when an age crosses the stale threshold, with the real age', () => {
    const { result } = renderHook(() => useGaugeSubset([rpm]))
    emit({ values: { [rpm]: 30.5 }, ages: { [rpm]: 119.5 } })
    const first = result.current
    emit({ values: { [rpm]: 30.5 }, ages: { [rpm]: 120.4 } })
    expect(result.current).not.toBe(first)
    expect(result.current.ages[rpm]).toBe(120.4)
  })

  it('re-renders a stale age only when the age it shows changes', () => {
    const { result } = renderHook(() => useGaugeSubset([rpm]))
    emit({ values: {}, ages: { [rpm]: 130 } })
    const first = result.current
    emit({ values: {}, ages: { [rpm]: 130.5 } })
    expect(result.current).toBe(first)
    emit({ values: {}, ages: { [rpm]: 185 } })
    expect(result.current).not.toBe(first)
    expect(result.current.ages[rpm]).toBe(185)
  })

  it('re-renders when an age becomes unknown, or known again', () => {
    const { result } = renderHook(() => useGaugeAge(rpm))
    emit({ values: {}, ages: { [rpm]: 0.3 } })
    emit({ values: {}, ages: { [rpm]: -1 } })
    expect(result.current).toBeNull()
    emit({ values: {}, ages: { [rpm]: 0.7 } })
    expect(result.current).toBe(0.7)
  })
})
