import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { LOG_BUFFER_CAPACITY, useLogs } from '@/hooks/use-logs'

type MockEventListener = (event: { data: string }) => void

let mockEventSources: MockEventSource[] = []

class MockEventSource {
  url: string
  onopen: ((event: Event) => void) | null = null
  onerror: ((event: Event) => void) | null = null
  onmessage: ((event: MessageEvent) => void) | null = null
  listeners: Record<string, MockEventListener[]> = {}
  readyState = 1

  constructor(url: string) {
    this.url = url
    mockEventSources.push(this)
  }

  addEventListener(event: string, cb: MockEventListener) {
    this.listeners[event] = this.listeners[event] || []
    this.listeners[event].push(cb)
  }

  removeEventListener(event: string, cb: MockEventListener) {
    if (this.listeners[event]) {
      this.listeners[event] = this.listeners[event].filter((f) => f !== cb)
    }
  }

  close() {
    this.readyState = 2
  }

  emit(event: string, data: unknown) {
    if (this.listeners[event]) {
      this.listeners[event].forEach((cb) => cb({ data: JSON.stringify(data) }))
    }
  }
}

describe('useLogs', () => {
  beforeEach(() => {
    mockEventSources = []
    vi.stubGlobal('EventSource', MockEventSource)

    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => [
        { id: 1, timestamp: '2026-09-13T10:00:00Z', message: 'line 1' },
      ],
    }))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('fetches initial logs and connects to stream when isLive is true', async () => {
    const { result } = renderHook(() => useLogs())

    expect(result.current.isLive).toBe(true)

    // Simulate SSE message arrival
    act(() => {
      if (mockEventSources[0]) {
        mockEventSources[0].emit('log', {
          id: 2,
          timestamp: '2026-09-13T10:00:01Z',
          message: 'line 2 from sse',
        })
      }
    })

    expect(result.current.logs.some((l) => l.message === 'line 2 from sse')).toBe(true)
  })

  it('closes EventSource when isLive is paused', () => {
    const { result } = renderHook(() => useLogs())
    expect(mockEventSources.length).toBe(1)
    const es = mockEventSources[0]

    act(() => {
      result.current.setIsLive(false)
    })

    expect(result.current.isLive).toBe(false)
    expect(es.readyState).toBe(2) // closed
  })

  // F-2 (security audit): a Settings -> Logs panel left open with live update
  // on used to grow this array forever — every arriving SSE entry was kept,
  // and a chatty backend (SSE reconnect storms on a flaky boat LAN) made it a
  // steady leak. Capping at LOG_BUFFER_CAPACITY, FIFO, bounds it the same way
  // the server's own log ring buffer (defaultLogBufferCapacity in
  // backend/log_buffer.go) already bounds what it retains.
  it('caps the live buffer at LOG_BUFFER_CAPACITY entries, dropping the oldest first', () => {
    const { result } = renderHook(() => useLogs())

    // id 1 already came from the initial fetch in beforeEach; stream in
    // enough more to overflow the cap by 5.
    const extraEntries = LOG_BUFFER_CAPACITY + 5
    const lastId = 1 + extraEntries
    act(() => {
      for (let id = 2; id <= lastId; id += 1) {
        mockEventSources[0].emit('log', { id, timestamp: '2026-09-19T00:00:00Z', message: `line ${id}` })
      }
    })

    expect(result.current.logs).toHaveLength(LOG_BUFFER_CAPACITY)
    expect(result.current.logs[0].id).toBe(lastId - LOG_BUFFER_CAPACITY + 1)
    expect(result.current.logs.at(-1)?.id).toBe(lastId)
  })

  // F-2: the dedup guard used to be `previous.some(...)`, an O(n) scan of the
  // whole array on every arriving entry (O(n^2) over a session). The
  // behaviour it protects — a duplicate id, e.g. from an SSE reconnect
  // re-delivering the tail of the stream, is kept once — must survive
  // switching that scan for an O(1) id lookup.
  it('ignores a duplicate id arriving again over the stream', () => {
    const { result } = renderHook(() => useLogs())

    act(() => {
      mockEventSources[0].emit('log', { id: 2, timestamp: '2026-09-19T00:00:00Z', message: 'first' })
      mockEventSources[0].emit('log', { id: 2, timestamp: '2026-09-19T00:00:00Z', message: 'duplicate' })
    })

    expect(result.current.logs.filter((entry) => entry.id === 2)).toHaveLength(1)
  })

  // F-2 regression guard: the O(1) dedup needs a side table of seen ids
  // alongside the array. clearLogs() must reset both, or an id seen before
  // the clear would silently never be re-accepted afterwards.
  it('lets an id seen before clearLogs arrive again afterwards', () => {
    const { result } = renderHook(() => useLogs())

    act(() => {
      mockEventSources[0].emit('log', { id: 2, timestamp: '2026-09-19T00:00:00Z', message: 'first' })
    })
    act(() => {
      result.current.clearLogs()
    })
    act(() => {
      mockEventSources[0].emit('log', { id: 2, timestamp: '2026-09-19T00:00:00Z', message: 'again' })
    })

    expect(result.current.logs).toHaveLength(1)
    expect(result.current.logs[0].message).toBe('again')
  })
})
