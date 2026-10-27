/**
 * Stores over the real shared stream (ADR 0218). The server seeds a new
 * connection with every event's current payload, and the stream remembers it,
 * so a store whose first reader mounts after the seed shows the seeded data
 * without waiting for the event to change. Nothing holds a store open at boot.
 */
import { act, render, renderHook, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

class FakeEventSource {
  static readonly CONNECTING = 0
  static readonly OPEN = 1
  static readonly CLOSED = 2
  static instances: FakeEventSource[] = []
  readyState = FakeEventSource.CONNECTING
  onerror: ((ev: Event) => void) | null = null
  private listeners = new Map<string, Set<(e: Event) => void>>()
  constructor(public url: string) { FakeEventSource.instances.push(this) }
  addEventListener(type: string, listener: (e: Event) => void) {
    let set = this.listeners.get(type)
    if (!set) this.listeners.set(type, (set = new Set()))
    set.add(listener)
  }
  removeEventListener(type: string, listener: (e: Event) => void) { this.listeners.get(type)?.delete(listener) }
  close() { this.readyState = FakeEventSource.CLOSED }
  emit(event: string, payload: unknown) {
    for (const l of this.listeners.get(event) ?? []) l({ data: JSON.stringify(payload), currentTarget: this } as unknown as MessageEvent)
  }
}

const houseBankLow = {
  rule_id: 'house-bank-low', label: 'House Bank Low', path: 'electrical.batteries.house.capacity.stateOfCharge',
  phase: 'active', state: 'alarm', value: 0.18, message: 'House bank 18%',
  silenced: false, can_silence: false, can_acknowledge: true,
}

vi.mock('@/hooks/use-alarm-rules', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/hooks/use-alarm-rules')>()),
  useAlarmLog: () => ({ entries: [], refresh: vi.fn().mockResolvedValue(undefined) }),
}))

beforeEach(() => {
  FakeEventSource.instances = []
  vi.resetModules()
  vi.stubGlobal('EventSource', FakeEventSource)
  vi.stubGlobal('fetch', vi.fn(async () => ({ ok: false, json: async () => ({}) })))
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('a store reader that mounts after the seed', () => {
  it('shows the seeded alarm list with no new event', async () => {
    const { useVesselStateSelector } = await import('@/hooks/use-vessel-state')
    const { LiveAlarmsDrawer } = await import('@/components/live-surfaces')
    // Something else opens the connection first, as on the boat.
    renderHook(() => useVesselStateSelector((s) => s.latitude))
    expect(FakeEventSource.instances).toHaveLength(1)
    act(() => { FakeEventSource.instances[0].emit('alarms', { alarms: [houseBankLow], worst: 'alarm' }) })

    render(<LiveAlarmsDrawer />)

    expect(await screen.findByText('House Bank Low')).toBeInTheDocument()
    expect(screen.queryByText('Waiting for the alarm list')).not.toBeInTheDocument()
  })

  it('shows the seeded all-clear, not the waiting state, for a list that was empty at the seed', async () => {
    const { useVesselStateSelector } = await import('@/hooks/use-vessel-state')
    const { useKnownAlarmList } = await import('@/hooks/use-alarms')
    renderHook(() => useVesselStateSelector((s) => s.latitude))
    act(() => { FakeEventSource.instances[0].emit('alarms', { alarms: [], worst: 'normal' }) })

    const { result } = renderHook(() => useKnownAlarmList())
    expect(result.current).toEqual([])
  })

  it('shows the seeded vessel state and gauge value to a tile mounted later', async () => {
    const { useGaugeValue } = await import('@/hooks/use-gauge-values')
    const { useVesselStateSelector } = await import('@/hooks/use-vessel-state')
    renderHook(() => useGaugeValue('a.path'))
    act(() => {
      FakeEventSource.instances[0].emit('vessel-state', { latitude: -20.2747, longitude: 149.0737 })
      FakeEventSource.instances[0].emit('gauge-values', { values: { 'a.path': 12 }, ages: { 'a.path': 1 } })
    })

    const lat = renderHook(() => useVesselStateSelector((s) => s.latitude))
    const gauge = renderHook(() => useGaugeValue('a.path'))
    expect(lat.result.current).toBe(-20.2747)
    expect(gauge.result.current).toBe(12)
  })

  it('does not show a list from before an outage', async () => {
    const { useVesselStateSelector } = await import('@/hooks/use-vessel-state')
    const { useKnownAlarmList } = await import('@/hooks/use-alarms')
    renderHook(() => useVesselStateSelector((s) => s.latitude))
    act(() => { FakeEventSource.instances[0].emit('alarms', { alarms: [houseBankLow], worst: 'alarm' }) })
    act(() => { FakeEventSource.instances[0].onerror?.(new Event('error')) })

    // A store that never started has no snapshot to keep; the old frame is gone.
    const { result } = renderHook(() => useKnownAlarmList())
    expect(result.current).toBeNull()
  })
})

describe('the boat clock behind a late store', () => {
  it('reads the replayed frame\'s datetime plus the time it was remembered, not the datetime as if it just arrived', async () => {
    vi.useFakeTimers()
    try {
      const { useKnownAlarmList } = await import('@/hooks/use-alarms')
      const { useVesselClock } = await import('@/hooks/use-vessel-clock')
      renderHook(() => useKnownAlarmList()) // opens the connection; nothing reads vessel-state yet
      act(() => { FakeEventSource.instances[0].emit('vessel-state', { datetime: '2026-10-27T10:00:00Z', timezone: 'UTC' }) })

      // Five quiet minutes of heartbeats, so the connection stays up.
      for (let i = 0; i < 10; i++) act(() => { vi.advanceTimersByTime(30_000); FakeEventSource.instances[0].emit('heartbeat', {}) })
      const { result } = renderHook(() => useVesselClock())

      expect(result.current.now?.toISOString()).toBe('2026-10-27T10:05:00.000Z')
    } finally {
      vi.useRealTimers()
    }
  })
})

