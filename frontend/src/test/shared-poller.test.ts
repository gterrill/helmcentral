import { act, renderHook } from '@testing-library/react'
import { createElement, type ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { createSharedPoller, type PollStore } from '@/lib/shared-poller'
import { WallPageActiveContext } from '@/lib/wall-page-active'
import { resetTelemetryStoresForTest } from '@/lib/test-reset'

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

  it('treats a snapshot equal by isEqual as no change: it keeps the held one and wakes nobody', async () => {
    vi.useFakeTimers()
    const poll = vi.fn<(signal: AbortSignal, store: PollStore<number[]>) => Promise<void>>(async () => {})
    const poller = createSharedPoller<number[]>([], poll, () => {}, { isEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]) })
    const listener = vi.fn()
    poller.subscribe(listener)
    poll.mockImplementationOnce(async (_s, store) => { store.publish([1]) })
    poll.mockImplementationOnce(async (_s, store) => { store.publish([1]) })
    poll.mockImplementationOnce(async (_s, store) => { store.publish([1, 2]) })
    renderHook(() => poller.useSnapshot(1000))
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })
    const held = poller.getSnapshot()
    await act(async () => { await vi.advanceTimersByTimeAsync(1000) })
    expect(poller.getSnapshot()).toBe(held)
    expect(listener).toHaveBeenCalledTimes(1)
    await act(async () => { await vi.advanceTimersByTimeAsync(1000) })
    expect(poller.getSnapshot()).toEqual([1, 2])
    expect(listener).toHaveBeenCalledTimes(2)
  })

  it('with resetOnIdle, the snapshot goes back to its initial value when the last reader leaves', async () => {
    vi.useFakeTimers()
    const poll = vi.fn<(signal: AbortSignal, store: PollStore<number[]>) => Promise<void>>(async (_s, store) => { store.publish([7]) })
    const initial: number[] = []
    const poller = createSharedPoller<number[]>(initial, poll, () => {}, { resetOnIdle: true })
    const listener = vi.fn()
    poller.subscribe(listener)
    const a = renderHook(() => poller.useSnapshot(1000))
    const b = renderHook(() => poller.useSnapshot(1000))
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })
    expect(a.result.current).toEqual([7])
    a.unmount()
    expect(poller.getSnapshot()).toEqual([7])
    b.unmount()
    expect(poller.getSnapshot()).toBe(initial)
    expect(listener).toHaveBeenCalledTimes(2)
  })

  it('without resetOnIdle, the snapshot survives the last reader leaving', async () => {
    vi.useFakeTimers()
    const poll = vi.fn<(signal: AbortSignal, store: PollStore<number[]>) => Promise<void>>(async (_s, store) => { store.publish([7]) })
    const poller = createSharedPoller<number[]>([], poll, () => {})
    const { unmount } = renderHook(() => poller.useSnapshot(1000))
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })
    unmount()
    expect(poller.getSnapshot()).toEqual([7])
  })

  it('a snapshot reader on a resting wall page is not re-rendered, and shows the latest value once its page wakes', async () => {
    vi.useFakeTimers()
    const poll = vi.fn<(signal: AbortSignal, store: PollStore<number[]>) => Promise<void>>(async () => {})
    const poller = createSharedPoller<number[]>([], poll, () => {})
    poll.mockImplementationOnce(async (_s, store) => { store.publish([1]) })
    poll.mockImplementationOnce(async (_s, store) => { store.publish([1, 2]) })
    let active = false
    const wrapper = ({ children }: { children: ReactNode }) =>
      createElement(WallPageActiveContext.Provider, { value: active }, children)
    let renders = 0
    const { result, rerender } = renderHook(() => { renders += 1; return poller.useSnapshot(1000) }, { wrapper })
    // The first read happened, so the tile has a value; later polls are not pushed.
    const settled = renders
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })
    await act(async () => { await vi.advanceTimersByTimeAsync(1000) })
    expect(poller.getSnapshot()).toEqual([1, 2])
    expect(renders).toBe(settled)
    expect(result.current).toEqual([])
    active = true
    rerender()
    expect(result.current).toEqual([1, 2])
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
