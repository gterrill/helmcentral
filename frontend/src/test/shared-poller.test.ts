import { act, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { createSharedPoller, type PollStore } from '@/lib/shared-poller'
import { resetTelemetryStoresForTest } from '@/lib/telemetry-store'

afterEach(() => {
  vi.useRealTimers()
})

function makePoller() {
  const poll = vi.fn<(signal: AbortSignal, store: PollStore<number[]>) => Promise<void>>(async () => {})
  const poller = createSharedPoller<number[]>([], poll, () => {})
  return { poll, poller }
}

describe('createSharedPoller', () => {
  it('notifies subscribers only when a poll publishes a different snapshot', async () => {
    vi.useFakeTimers()
    const { poll, poller } = makePoller()
    const listener = vi.fn()
    const unsubscribe = poller.subscribe(listener)

    poll.mockImplementationOnce(async (_s, store) => { store.publish([1]) })
    poll.mockImplementationOnce(async (_s, store) => { store.publish(store.get()) })
    poll.mockImplementationOnce(async (_s, store) => { store.publish([...store.get(), 2]) })

    const { unmount } = renderHook(() => poller.useSnapshot(1000))
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })
    expect(listener).toHaveBeenCalledTimes(1)
    await act(async () => { await vi.advanceTimersByTimeAsync(1000) })
    expect(listener).toHaveBeenCalledTimes(1)
    await act(async () => { await vi.advanceTimersByTimeAsync(1000) })
    expect(listener).toHaveBeenCalledTimes(2)
    expect(poller.getSnapshot()).toEqual([1, 2])
    unmount()
    unsubscribe()
  })

  it('makes one poll for two readers, and stops when the last leaves', async () => {
    vi.useFakeTimers()
    const { poll, poller } = makePoller()
    const a = renderHook(() => poller.useSnapshot(1000))
    const b = renderHook(() => poller.useSnapshot(1000))
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })
    expect(poll).toHaveBeenCalledTimes(1)

    a.unmount()
    await act(async () => { await vi.advanceTimersByTimeAsync(1000) })
    expect(poll).toHaveBeenCalledTimes(2)

    b.unmount()
    await act(async () => { await vi.advanceTimersByTimeAsync(5000) })
    expect(poll).toHaveBeenCalledTimes(2)
  })

  it('a disabled reader reads the snapshot and holds no poll', async () => {
    vi.useFakeTimers()
    const { poll, poller } = makePoller()
    const { result } = renderHook(() => poller.useSnapshot(1000, false))
    await act(async () => { await vi.advanceTimersByTimeAsync(5000) })
    expect(poll).not.toHaveBeenCalled()
    expect(result.current).toEqual([])
  })

  it('a slice reader is woken only when its slice changes', async () => {
    vi.useFakeTimers()
    const poll = vi.fn<(signal: AbortSignal, store: PollStore<{ a: number[]; b: number[] }>) => Promise<void>>(async () => {})
    const poller = createSharedPoller({ a: [] as number[], b: [] as number[] }, poll, () => {})
    poll.mockImplementationOnce(async (_s, store) => { store.publish({ ...store.get(), b: [1] }) })
    let renders = 0
    renderHook(() => { renders += 1; return poller.useSlice(s => s.a, 1000) })
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })
    expect(poll).toHaveBeenCalledTimes(1)
    expect(renders).toBe(1)
  })

  it('tells its listeners when a test reset empties the snapshot', async () => {
    const { poller } = makePoller()
    const listener = vi.fn()
    const unsubscribe = poller.subscribe(listener)
    resetTelemetryStoresForTest()
    expect(listener).toHaveBeenCalledTimes(1)
    unsubscribe()
  })
})
