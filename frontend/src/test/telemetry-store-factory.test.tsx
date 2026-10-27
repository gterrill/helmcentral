/**
 * What the store factory owns (ADR 0215): decoding the frame, finding the
 * stream, and knowing whether the server has been heard yet. A store supplies
 * its event, its initial state and one apply step that receives the decoded
 * JSON. The real stream runs over the fake EventSource.
 */
import { act, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { createTelemetryStore, resetTelemetryStoresForTest, type TelemetryUpdate } from '@/lib/telemetry-store'
import { installFakeTelemetryStream } from './helpers/fake-telemetry-stream'

const stream = installFakeTelemetryStream()

interface Counter { n: number }

function makeStore() {
  const seen: Array<{ data: unknown; previous: Counter; receivedAt: number }> = []
  const store = createTelemetryStore<Counter>('factory-test', { n: 0 }, (previous, data, receivedAt): TelemetryUpdate<Counter> | null => {
    seen.push({ data, previous, receivedAt })
    const n = (data as { n: number }).n
    return n === previous.n ? null : { state: { n } }
  })
  return { store, seen }
}

afterEach(() => { vi.restoreAllMocks() })

describe('createTelemetryStore', () => {
  it('hands apply the decoded JSON, the previous state and the arrival time', () => {
    const { store, seen } = makeStore()
    renderHook(() => store.useSelector((s) => s.n))
    stream.emit('factory-test', { n: 3 })
    expect(seen).toHaveLength(1)
    expect(seen[0].data).toEqual({ n: 3 })
    expect(seen[0].previous).toEqual({ n: 0 })
    expect(typeof seen[0].receivedAt).toBe('number')
    expect(store.getSnapshot()).toEqual({ n: 3 })
  })

  it('logs a payload that is not JSON, per frame, with the event name, and changes nothing', () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => {})
    const { store, seen } = makeStore()
    const { result } = renderHook(() => ({ n: store.useSelector((s) => s.n), heard: store.useHeardSelector((s) => s.n) !== null }))
    stream.emit('factory-test', 'not json')
    expect(error).toHaveBeenCalledTimes(1)
    expect(String(error.mock.calls[0][0])).toContain('factory-test')
    expect(seen).toHaveLength(0)
    expect(result.current).toEqual({ n: 0, heard: false })
  })

  it('has not heard the server until the first event, and has after it', () => {
    const { store } = makeStore()
    const { result } = renderHook(() => store.useHeardSelector((s) => s.n))
    expect(result.current).toBeNull()
    stream.emit('factory-test', { n: 0 })
    expect(result.current).toBe(0)
  })

  it('counts an event that changes nothing as heard', () => {
    const { store } = makeStore()
    const { result } = renderHook(() => store.useHeardSelector((s) => s.n))
    stream.emit('factory-test', { n: 0 })
    expect(result.current).toBe(0)
    expect(store.getSnapshot()).toEqual({ n: 0 })
  })

  it('forgets that it has heard when the stores are reset', () => {
    const { store } = makeStore()
    const { result } = renderHook(() => store.useHeardSelector((s) => s.n))
    stream.emit('factory-test', { n: 1 })
    expect(result.current).toBe(1)
    act(() => { resetTelemetryStoresForTest() })
    expect(store.getSnapshot()).toEqual({ n: 0 })
    const again = renderHook(() => store.useHeardSelector((s) => s.n))
    expect(again.result.current).toBeNull()
  })

  it('rejects valid JSON that is not an object, logging the event name and staying unheard', () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => {})
    const { store, seen } = makeStore()
    const { result } = renderHook(() => store.useHeardSelector((s) => s.n))
    for (const frame of ['null', '7', '"text"', '[1,2]']) stream.emit('factory-test', frame)
    expect(error).toHaveBeenCalledTimes(4)
    expect(String(error.mock.calls[0][0])).toContain('factory-test')
    expect(seen).toHaveLength(0)
    expect(result.current).toBeNull()
  })

  it('tells keyed listeners when the first event changes nothing', () => {
    const { store } = makeStore()
    const listener = vi.fn()
    const unsubscribe = store.subscribeKeys(['k'], listener)
    stream.emit('factory-test', { n: 0 })
    expect(listener).toHaveBeenCalledTimes(1)
    stream.emit('factory-test', { n: 0 })
    expect(listener).toHaveBeenCalledTimes(1)
    unsubscribe()
  })

  it('gives a heard selection the same reference while the state holds', () => {
    const { store } = makeStore()
    const { result, rerender } = renderHook(() => store.useHeardSelector((s) => s))
    stream.emit('factory-test', { n: 2 })
    const first = result.current
    rerender()
    expect(result.current).toBe(first)
  })
})
