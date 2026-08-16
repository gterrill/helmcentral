import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { act, renderHook, waitFor } from '@testing-library/react'

// Mirrors use-telemetry-stream.test.ts's FakeEventSource: lets these tests
// drive the shared SSE connection without a real network, and lets us fire an
// 'autopilot' event by hand.
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

  emitMessage(eventName: string, data: string) {
    for (const l of this.listeners.get(eventName) ?? []) {
      l({ data } as MessageEvent)
    }
  }
}

let instances: FakeEventSource[] = []

async function loadModule() {
  vi.resetModules()
  return import('@/hooks/use-autopilot')
}

beforeEach(() => {
  instances = []
  vi.stubGlobal('EventSource', FakeEventSource)
  // The hook probes /api/autopilot once for the pilot's capability list
  // (which modes it supports — that is not on the delta stream). Tests that
  // care about it re-stub fetch; the rest just need it not to explode.
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({ present: false }) }))
})

afterEach(() => {
  vi.unstubAllGlobals()
})

function emitAutopilot(payload: unknown) {
  act(() => {
    instances[0].emitMessage('autopilot', JSON.stringify(payload))
  })
}

describe('useAutopilot', () => {
  it('starts absent before any event has arrived', async () => {
    const { useAutopilot } = await loadModule()
    const { result } = renderHook(() => useAutopilot())

    expect(result.current.state.present).toBe(false)
    expect(result.current.state.engaged).toBe(false)
  })

  it('reflects present/engaged/mode/target/availableActions/stale from the SSE stream', async () => {
    const { useAutopilot } = await loadModule()
    const { result } = renderHook(() => useAutopilot())

    emitAutopilot({
      present: true,
      engaged: true,
      state: 'auto',
      mode: 'compass',
      target: 135,
      available_actions: ['disengage', 'tack'],
      stale: false,
    })

    expect(result.current.state).toEqual({
      present: true,
      engaged: true,
      state: 'auto',
      mode: 'compass',
      target: 135,
      availableActions: ['disengage', 'tack'],
      stale: false,
    })
  })

  it('resets to absent when the stream reports no autopilot present', async () => {
    const { useAutopilot } = await loadModule()
    const { result } = renderHook(() => useAutopilot())

    emitAutopilot({ present: true, engaged: true, state: 'auto', mode: 'compass', target: 1, available_actions: [], stale: false })
    expect(result.current.state.present).toBe(true)

    emitAutopilot({ present: false })
    expect(result.current.state.present).toBe(false)
    expect(result.current.state.engaged).toBe(false)
  })

  it('does not optimistically flip engaged when engage() is called — only a stream event may change displayed state', async () => {
    const { useAutopilot } = await loadModule()
    vi.stubGlobal('fetch', vi.fn().mockImplementation(() => new Promise(() => {
      /* never resolves within this test — proves no optimistic update happens before the response */
    })))

    const { result } = renderHook(() => useAutopilot())
    emitAutopilot({ present: true, engaged: false, state: 'standby', mode: 'compass', target: 0, available_actions: ['engage'], stale: false })
    expect(result.current.state.engaged).toBe(false)

    act(() => {
      void result.current.engage()
    })

    // Still false: the fetch is deliberately unresolved, so if the hook ever
    // set engaged optimistically it would show up here.
    expect(result.current.state.engaged).toBe(false)
  })

  it('tracks a pending action while its request is in flight, keyed so unrelated actions are unaffected', async () => {
    const { useAutopilot } = await loadModule()
    let resolveFetch!: (value: unknown) => void
    vi.stubGlobal('fetch', vi.fn().mockImplementation(() => new Promise((resolve) => { resolveFetch = resolve })))

    const { result } = renderHook(() => useAutopilot())

    act(() => {
      void result.current.engage()
    })
    expect(result.current.pending.has('engage')).toBe(true)
    expect(result.current.pending.has('disengage')).toBe(false)

    await act(async () => {
      resolveFetch({ ok: true, json: async () => ({}) })
      await Promise.resolve()
    })

    await waitFor(() => expect(result.current.pending.has('engage')).toBe(false))
  })

  it('surfaces the server error message on a failed command without changing displayed state', async () => {
    const { useAutopilot } = await loadModule()
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: false,
      status: 502,
      json: async () => ({ error: 'signalk returned status 503: provider offline' }),
    }))

    const { result } = renderHook(() => useAutopilot())
    emitAutopilot({ present: true, engaged: false, state: 'standby', mode: 'compass', target: 0, available_actions: ['engage'], stale: false })

    await act(async () => {
      await result.current.engage()
    })

    expect(result.current.error).toBe('signalk returned status 503: provider offline')
    // A failed command must never leave the tile claiming the boat is under autopilot.
    expect(result.current.state.engaged).toBe(false)
  })

  it('posts to the engage/disengage/tack/gybe endpoints with no request body', async () => {
    const { useAutopilot } = await loadModule()
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ ok: true }) })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useAutopilot())

    await act(async () => { await result.current.engage() })
    expect(fetchMock).toHaveBeenLastCalledWith('/api/autopilot/engage', expect.objectContaining({ method: 'POST' }))

    await act(async () => { await result.current.disengage() })
    expect(fetchMock).toHaveBeenLastCalledWith('/api/autopilot/disengage', expect.objectContaining({ method: 'POST' }))

    await act(async () => { await result.current.tack('port') })
    expect(fetchMock).toHaveBeenLastCalledWith('/api/autopilot/tack/port', expect.objectContaining({ method: 'POST' }))

    await act(async () => { await result.current.gybe('starboard') })
    expect(fetchMock).toHaveBeenLastCalledWith('/api/autopilot/gybe/starboard', expect.objectContaining({ method: 'POST' }))
  })

  it('PUTs a heading adjustment with a JSON {value} body', async () => {
    const { useAutopilot } = await loadModule()
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ ok: true }) })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useAutopilot())

    await act(async () => { await result.current.adjustHeading(-10) })

    expect(fetchMock).toHaveBeenLastCalledWith('/api/autopilot/target/adjust', expect.objectContaining({
      method: 'PUT',
      body: JSON.stringify({ value: -10 }),
    }))
  })

  it('PUTs a mode change with a JSON {mode} body, matching the backend handler', async () => {
    const { useAutopilot } = await loadModule()
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ ok: true }) })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useAutopilot())

    await act(async () => { await result.current.setMode('wind') })

    expect(fetchMock).toHaveBeenLastCalledWith('/api/autopilot/mode', expect.objectContaining({
      method: 'PUT',
      body: JSON.stringify({ mode: 'wind' }),
    }))
  })

  it('PUTs a dodge in degrees and DELETEs to clear it', async () => {
    const { useAutopilot } = await loadModule()
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ ok: true }) })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useAutopilot())

    await act(async () => { await result.current.dodge(5) })
    expect(fetchMock).toHaveBeenLastCalledWith('/api/autopilot/dodge', expect.objectContaining({
      method: 'PUT',
      body: JSON.stringify({ value: 5 }),
    }))

    await act(async () => { await result.current.clearDodge() })
    expect(fetchMock).toHaveBeenLastCalledWith('/api/autopilot/dodge', expect.objectContaining({ method: 'DELETE' }))
  })

  it('does not optimistically change mode when setMode() is called', async () => {
    const { useAutopilot } = await loadModule()
    vi.stubGlobal('fetch', vi.fn().mockImplementation(() => new Promise(() => {
      /* never resolves — an optimistic write would be visible before it does */
    })))

    const { result } = renderHook(() => useAutopilot())
    emitAutopilot({ present: true, engaged: true, state: 'auto', mode: 'compass', target: 90, available_actions: [], stale: false })

    act(() => {
      void result.current.setMode('wind')
    })

    expect(result.current.state.mode).toBe('compass')
  })

  it('reads the supported mode list from the capability probe, not the stream', async () => {
    const { useAutopilot } = await loadModule()
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ present: true, id: 'raymarine', options: { modes: ['compass', 'wind', 'gps'], states: [] } }),
    }))

    const { result } = renderHook(() => useAutopilot())

    await waitFor(() => expect(result.current.availableModes).toEqual(['compass', 'wind', 'gps']))
  })

  it('leaves the mode list empty and records why when the capability probe fails', async () => {
    const { useAutopilot } = await loadModule()
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: false,
      status: 502,
      json: async () => ({ error: 'signalk unreachable' }),
    }))

    const { result } = renderHook(() => useAutopilot())

    await waitFor(() => expect(result.current.capabilityError).toBe('signalk unreachable'))
    // Empty rather than a guessed default set: the tile disables the mode
    // selector instead of offering modes the pilot may not support.
    expect(result.current.availableModes).toEqual([])
  })
})
