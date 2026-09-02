import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { act } from '@testing-library/react'

// Coverage for use-radar-echo-stream.ts, modelled closely on
// use-telemetry-stream.test.ts. The most important test here is the same
// one that file exists to pin: a subscriber registered before a drop must
// still receive data after the reconnect, because the connection instance
// underneath a subscription is not stable across reconnects and an
// unsubscribe closure that captures it directly ends up detached from the
// live socket. use-telemetry-stream.ts documents this for EventSource; a
// reconnecting WebSocket has the identical trap.
//
// FakeWebSocket lets these tests drive open/message/close the way a real
// browser would, without a real network connection. Real close events are
// asynchronous (dispatched on a later task), but firing them synchronously
// here is fine: the module under test never assumes otherwise, and doing so
// keeps every test free of extra sleeps/microtask flushes.

class FakeWebSocket {
  static readonly CONNECTING = 0
  static readonly OPEN = 1
  static readonly CLOSING = 2
  static readonly CLOSED = 3

  readyState = FakeWebSocket.CONNECTING
  // Real WebSockets default binaryType to 'blob'. Leaving the fake at that
  // same default (rather than pre-seeding 'arraybuffer') is what makes the
  // "set before any frame can arrive" assertion meaningful.
  binaryType: 'blob' | 'arraybuffer' = 'blob'
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
    // Idempotent, like the real thing: closing an already-closed socket
    // must not re-fire 'close' and re-trigger reconnect handling.
    if (this.readyState === FakeWebSocket.CLOSED) return
    this.readyState = FakeWebSocket.CLOSED
    for (const l of this.listeners.get('close') ?? []) l(new Event('close'))
  }

  // --- test helpers, not part of the real WebSocket API ---

  emitOpen() {
    this.readyState = FakeWebSocket.OPEN
    for (const l of this.listeners.get('open') ?? []) l(new Event('open'))
  }

  emitMessage(data: ArrayBuffer) {
    for (const l of this.listeners.get('message') ?? []) l({ data } as MessageEvent)
  }

  /** Simulates the upstream/relay dropping the connection. */
  emitClose() {
    this.readyState = FakeWebSocket.CLOSED
    for (const l of this.listeners.get('close') ?? []) l(new Event('close'))
  }
}

let instances: FakeWebSocket[] = []

async function loadModule() {
  vi.resetModules()
  return import('@/hooks/use-radar-echo-stream')
}

beforeEach(() => {
  instances = []
  vi.useFakeTimers()
  vi.stubGlobal('WebSocket', FakeWebSocket)
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('use-radar-echo-stream', () => {
  it('shares one socket between two subscribers for the same radar id', async () => {
    const { subscribeRadarSpokes } = await loadModule()

    const onFrameA = vi.fn()
    const onFrameB = vi.fn()
    subscribeRadarSpokes('fur6424A', onFrameA)
    subscribeRadarSpokes('fur6424A', onFrameB)

    expect(instances).toHaveLength(1)

    const frame = new ArrayBuffer(4)
    act(() => {
      instances[0].emitMessage(frame)
    })

    expect(onFrameA).toHaveBeenCalledWith(frame)
    expect(onFrameB).toHaveBeenCalledWith(frame)
  })

  it('opens a separate socket per radar id', async () => {
    const { subscribeRadarSpokes } = await loadModule()

    subscribeRadarSpokes('fur6424A', vi.fn())
    subscribeRadarSpokes('fur6424B', vi.fn())

    expect(instances).toHaveLength(2)
    expect(instances[0].url).toContain('radar=fur6424A')
    expect(instances[1].url).toContain('radar=fur6424B')
  })

  it('sets binaryType to arraybuffer synchronously, before any frame can arrive', async () => {
    const { subscribeRadarSpokes } = await loadModule()

    subscribeRadarSpokes('fur6424A', vi.fn())

    expect(instances).toHaveLength(1)
    expect(instances[0].binaryType).toBe('arraybuffer')
  })

  it('passes a frame through to subscribers as the exact same ArrayBuffer, undecoded', async () => {
    const { subscribeRadarSpokes } = await loadModule()

    const onFrame = vi.fn()
    subscribeRadarSpokes('fur6424A', onFrame)

    const frame = new ArrayBuffer(16)
    new Uint8Array(frame).set([1, 2, 3, 4])
    act(() => {
      instances[0].emitMessage(frame)
    })

    expect(onFrame).toHaveBeenCalledTimes(1)
    // Reference equality, not just structural: this hook must not copy,
    // decode, or otherwise touch the frame -- that is the caller's job.
    expect(onFrame.mock.calls[0][0]).toBe(frame)
  })

  it('keeps a subscriber registered before a drop receiving frames after the reconnect', async () => {
    const { subscribeRadarSpokes } = await loadModule()

    const onFrame = vi.fn()
    subscribeRadarSpokes('fur6424A', onFrame)

    expect(instances).toHaveLength(1)
    const dead = instances[0]

    act(() => {
      dead.emitClose()
    })

    // Nothing new before the backoff elapses.
    act(() => {
      vi.advanceTimersByTime(999)
    })
    expect(instances).toHaveLength(1)

    act(() => {
      vi.advanceTimersByTime(1)
    })
    expect(instances).toHaveLength(2)
    const revived = instances[1]
    expect(revived).not.toBe(dead)

    // The subscriber registered before the drop must still receive frames --
    // delivered on the NEW socket, since the old one is dead. This is the
    // registry-vs-instance bug use-telemetry-stream.ts documents.
    const frame = new ArrayBuffer(8)
    act(() => {
      revived.emitMessage(frame)
    })
    expect(onFrame).toHaveBeenCalledWith(frame)
  })

  it('closes the socket once the last subscriber unsubscribes, and does not reconnect afterward', async () => {
    const { subscribeRadarSpokes } = await loadModule()

    const unsubA = subscribeRadarSpokes('fur6424A', vi.fn())
    const unsubB = subscribeRadarSpokes('fur6424A', vi.fn())

    expect(instances).toHaveLength(1)
    expect(instances[0].readyState).not.toBe(FakeWebSocket.CLOSED)

    act(() => {
      unsubA()
    })
    expect(instances[0].readyState).not.toBe(FakeWebSocket.CLOSED)

    act(() => {
      unsubB()
    })
    expect(instances[0].readyState).toBe(FakeWebSocket.CLOSED)

    act(() => {
      vi.advanceTimersByTime(60_000)
    })
    expect(instances).toHaveLength(1)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('grows the backoff on repeated drops and caps it at 30s', async () => {
    const { subscribeRadarSpokes } = await loadModule()

    subscribeRadarSpokes('fur6424A', vi.fn())
    expect(instances).toHaveLength(1)

    const expectedDelays = [1_000, 2_000, 4_000, 8_000, 16_000, 30_000, 30_000]

    for (const delay of expectedDelays) {
      const current = instances.at(-1)!
      act(() => {
        current.emitClose()
      })

      act(() => {
        vi.advanceTimersByTime(delay - 1)
      })
      const countBefore = instances.length
      expect(instances).toHaveLength(countBefore)

      act(() => {
        vi.advanceTimersByTime(1)
      })
      expect(instances).toHaveLength(countBefore + 1)
    }
  })

  it('resets the backoff once a connection has stayed open past the 10s stability window', async () => {
    const { subscribeRadarSpokes } = await loadModule()

    subscribeRadarSpokes('fur6424A', vi.fn())
    const first = instances[0]

    // Drop before ever opening: backoff stays at its 1s floor, next attempt due at 1s.
    act(() => {
      first.emitClose()
    })
    act(() => {
      vi.advanceTimersByTime(1_000)
    })
    expect(instances).toHaveLength(2)

    const second = instances[1]
    act(() => {
      second.emitOpen()
    })
    // Stays open past the 10s stability window before dropping again.
    act(() => {
      vi.advanceTimersByTime(10_000)
    })
    act(() => {
      second.emitClose()
    })

    // Without the reset, the next attempt would be due at 2s (the doubled
    // value already queued up by the first drop). With it, it's back to 1s.
    act(() => {
      vi.advanceTimersByTime(999)
    })
    expect(instances).toHaveLength(2)
    act(() => {
      vi.advanceTimersByTime(1)
    })
    expect(instances).toHaveLength(3)
  })

  it('transitions status idle -> connected -> reconnecting -> disconnected', async () => {
    const { subscribeRadarSpokes, subscribeRadarEchoStatus } = await loadModule()

    const statuses: string[] = []
    subscribeRadarEchoStatus((s) => statuses.push(s))
    expect(statuses).toEqual(['idle'])

    const unsubscribe = subscribeRadarSpokes('fur6424A', vi.fn())
    expect(statuses).toEqual(['idle'])

    act(() => {
      instances[0].emitOpen()
    })
    expect(statuses).toEqual(['idle', 'connected'])

    act(() => {
      instances[0].emitClose()
    })
    expect(statuses).toEqual(['idle', 'connected', 'reconnecting'])

    act(() => {
      unsubscribe()
    })
    expect(statuses).toEqual(['idle', 'connected', 'reconnecting', 'disconnected'])
  })

  it('builds a same-origin URL, choosing ws: on an http: page', async () => {
    const { subscribeRadarSpokes } = await loadModule()

    subscribeRadarSpokes('fur6424A', vi.fn())

    expect(instances[0].url).toMatch(/^ws:\/\//)
    expect(instances[0].url).toContain('/api/radar/spokes?radar=fur6424A')
  })

  it('chooses wss: on an https: page', async () => {
    vi.stubGlobal('location', { ...window.location, protocol: 'https:', host: 'boat.example.ts.net' })

    const { subscribeRadarSpokes } = await loadModule()
    subscribeRadarSpokes('fur6424A', vi.fn())

    expect(instances[0].url).toMatch(/^wss:\/\/boat\.example\.ts\.net\//)
  })
})
