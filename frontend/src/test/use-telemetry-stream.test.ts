import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { act, renderHook } from '@testing-library/react'

// Regression coverage for the production bug where the shared EventSource in
// use-telemetry-stream.ts had no `error` handler. Per the WHATWG spec, a
// non-200 response or wrong content-type (exactly what the dev proxy and
// nginx answer with while the Go backend restarts) is a *permanent* failure:
// readyState goes to CLOSED and the browser never retries on its own. That
// permanently killed the stream - including the position feed the anchor
// drag alarm reads (use-anchor-watch.ts) - with nothing in the UI to say so.
//
// FakeEventSource lets these tests drive readyState and fire `error` the way
// a real browser would, without a real network connection.

class FakeEventSource {
  static readonly CONNECTING = 0
  static readonly OPEN = 1
  static readonly CLOSED = 2

  readyState = FakeEventSource.CONNECTING
  onerror: ((ev: Event) => void) | null = null
  private listeners = new Map<string, Set<(e: Event) => void>>()

  constructor(public url: string) {
    instances.push(this)
  }

  addEventListener(type: string, listener: (e: Event) => void) {
    let set = this.listeners.get(type)
    if (!set) {
      set = new Set()
      this.listeners.set(type, set)
    }
    set.add(listener)
  }

  removeEventListener(type: string, listener: (e: Event) => void) {
    this.listeners.get(type)?.delete(listener)
  }

  close() {
    this.readyState = FakeEventSource.CLOSED
  }

  // --- test helpers, not part of the real EventSource API ---

  emitOpen() {
    this.readyState = FakeEventSource.OPEN
    for (const l of this.listeners.get('open') ?? []) l(new Event('open'))
  }

  /** Simulates the browser setting readyState and firing `error`. */
  emitError(readyState: number) {
    this.readyState = readyState
    this.onerror?.(new Event('error'))
  }

  emitMessage(eventName: string, data: string) {
    for (const l of this.listeners.get(eventName) ?? []) {
      l({ data } as MessageEvent)
    }
  }
}

let instances: FakeEventSource[] = []

async function loadModule() {
  vi.resetModules()
  return import('@/hooks/use-telemetry-stream')
}

beforeEach(() => {
  instances = []
  vi.useFakeTimers()
  vi.stubGlobal('EventSource', FakeEventSource)
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('use-telemetry-stream reconnection', () => {
  it('reconnects after a permanent close and redelivers events to a subscriber registered beforehand', async () => {
    const { subscribeTelemetry } = await loadModule()

    const onData = vi.fn()
    subscribeTelemetry('vessel-state', onData)

    expect(instances).toHaveLength(1)
    const dead = instances[0]

    // Permanent failure: non-200/wrong content-type -> readyState CLOSED.
    act(() => { dead.emitError(FakeEventSource.CLOSED) })

    // Nothing should be constructed before the backoff elapses.
    act(() => { vi.advanceTimersByTime(999) })
    expect(instances).toHaveLength(1)

    act(() => { vi.advanceTimersByTime(1) })
    expect(instances).toHaveLength(2)
    const revived = instances[1]
    expect(revived).not.toBe(dead)

    // The subscriber registered before the failure must still receive events
    // - delivered on the NEW instance, since the old one is dead.
    act(() => { revived.emitMessage('vessel-state', '{"depth":1}') })
    expect(onData).toHaveBeenCalledWith('{"depth":1}')
  })

  it('leaves a CONNECTING-state error alone (browser already retrying that instance)', async () => {
    const { subscribeTelemetry } = await loadModule()

    subscribeTelemetry('vessel-state', vi.fn())
    expect(instances).toHaveLength(1)
    const es = instances[0]

    act(() => { es.emitError(FakeEventSource.CONNECTING) })
    act(() => { vi.advanceTimersByTime(60_000) })

    expect(instances).toHaveLength(1)
  })

  it('reports a CONNECTING-state drop as reconnecting rather than leaving the status at connected', async () => {
    const { subscribeTelemetry, useTelemetryStatus } = await loadModule()

    subscribeTelemetry('vessel-state', vi.fn())
    const { result } = renderHook(() => useTelemetryStatus())

    act(() => { instances[0].emitOpen() })
    expect(result.current).toBe('connected')

    // A transport-level drop: the browser retries this instance on its own, so
    // no reconnect of ours is due - but the tiles are frozen for as long as it
    // takes, and the badge must not keep claiming the feed is live.
    act(() => { instances[0].emitError(FakeEventSource.CONNECTING) })
    expect(result.current).toBe('reconnecting')

    act(() => { instances[0].emitOpen() })
    expect(result.current).toBe('connected')
  })

  it('grows the backoff on repeated permanent failures and caps it at 30s', async () => {
    const { subscribeTelemetry } = await loadModule()

    subscribeTelemetry('vessel-state', vi.fn())
    expect(instances).toHaveLength(1)

    const expectedDelays = [1_000, 2_000, 4_000, 8_000, 16_000, 30_000, 30_000]

    for (const delay of expectedDelays) {
      const current = instances.at(-1)!
      act(() => { current.emitError(FakeEventSource.CLOSED) })

      act(() => { vi.advanceTimersByTime(delay - 1) })
      const countBefore = instances.length
      expect(instances).toHaveLength(countBefore)

      act(() => { vi.advanceTimersByTime(1) })
      expect(instances).toHaveLength(countBefore + 1)
    }
  })

  it('closes the connection and clears the pending reconnect timer once the last subscriber unsubscribes', async () => {
    const { subscribeTelemetry } = await loadModule()

    const unsubscribe = subscribeTelemetry('vessel-state', vi.fn())
    expect(instances).toHaveLength(1)

    act(() => { instances[0].emitError(FakeEventSource.CLOSED) })

    // Unsubscribe mid-backoff, before the reconnect timer fires.
    act(() => { unsubscribe() })

    act(() => { vi.advanceTimersByTime(60_000) })

    // No reconnect attempt should have happened, and no timer should be left running.
    expect(instances).toHaveLength(1)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('reports connection status through useTelemetryStatus, and recovers to connected after reconnecting', async () => {
    const { subscribeTelemetry, useTelemetryStatus } = await loadModule()

    subscribeTelemetry('vessel-state', vi.fn())
    const { result } = renderHook(() => useTelemetryStatus())

    expect(instances).toHaveLength(1)
    act(() => { instances[0].emitOpen() })
    expect(result.current).toBe('connected')

    act(() => { instances[0].emitError(FakeEventSource.CLOSED) })
    expect(result.current).not.toBe('connected')

    act(() => { vi.advanceTimersByTime(1_000) })
    expect(instances).toHaveLength(2)

    act(() => { instances[1].emitOpen() })
    expect(result.current).toBe('connected')
  })
})
