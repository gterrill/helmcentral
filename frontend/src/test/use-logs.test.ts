import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useLogs } from '@/hooks/use-logs'

describe('useLogs', () => {
  let mockEventSources: any[] = []

  beforeEach(() => {
    mockEventSources = []
    vi.stubGlobal('EventSource', class {
      url: string
      onopen: any = null
      onerror: any = null
      onmessage: any = null
      listeners: Record<string, Function[]> = {}
      readyState = 1

      constructor(url: string) {
        this.url = url
        mockEventSources.push(this)
      }

      addEventListener(event: string, cb: Function) {
        this.listeners[event] = this.listeners[event] || []
        this.listeners[event].push(cb)
      }

      removeEventListener(event: string, cb: Function) {
        if (this.listeners[event]) {
          this.listeners[event] = this.listeners[event].filter((f) => f !== cb)
        }
      }

      close() {
        this.readyState = 2
      }

      emit(event: string, data: any) {
        if (this.listeners[event]) {
          this.listeners[event].forEach((cb) => cb({ data: JSON.stringify(data) }))
        }
      }
    })

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
})
