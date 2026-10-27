/**
 * The fake EventSource adapter itself: what it promises the tests built on it.
 */
import { act, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { useVesselStateSelector } from '@/hooks/use-vessel-state'
import { installFakeTelemetryStream } from './helpers/fake-telemetry-stream'

const stream = installFakeTelemetryStream()
const useLatitude = () => useVesselStateSelector((s) => s.latitude)

afterEach(() => { vi.useRealTimers() })

describe('seed', () => {
  it('is what a reader that mounts first sees', () => {
    stream.seed('vessel-state', { latitude: -20.1 })
    const { result } = renderHook(useLatitude)
    expect(result.current).toBe(-20.1)
  })

  it('is delivered once per connection: a later reader does not rewind a newer frame', () => {
    stream.seed('vessel-state', { latitude: -20.1 })
    const first = renderHook(useLatitude)
    stream.emit('vessel-state', { latitude: -20.2 })
    expect(first.result.current).toBe(-20.2)

    const second = renderHook(useLatitude)

    expect(second.result.current).toBe(-20.2)
    expect(first.result.current).toBe(-20.2)
  })

  it('is delivered again to a reconnection, as the hub seeds every new connection', () => {
    vi.useFakeTimers()
    stream.seed('vessel-state', { latitude: -20.1 })
    const { result } = renderHook(useLatitude)
    stream.emit('vessel-state', { latitude: -20.2 })

    stream.error({ closed: true })
    act(() => { vi.advanceTimersByTime(1_000) })

    expect(stream.connections).toBe(2)
    expect(result.current).toBe(-20.1)
  })
})

describe('teardown', () => {
  it('leaves no connection, timer or listener behind for the next test', async () => {
    vi.useFakeTimers()
    renderHook(useLatitude)
    expect(stream.isConnected).toBe(true)
    expect(vi.getTimerCount()).toBeGreaterThan(0) // the stream's silence watchdog

    await stream.teardown()

    expect(stream.isConnected).toBe(false)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('also cleans a copy of the modules loaded after vi.resetModules', async () => {
    vi.useFakeTimers()
    vi.resetModules()
    const fresh = await import('@/hooks/use-vessel-state')
    renderHook(() => fresh.useVesselStateSelector((s) => s.latitude))
    expect(vi.getTimerCount()).toBeGreaterThan(0)

    await stream.teardown()

    expect(vi.getTimerCount()).toBe(0)
  })
})
