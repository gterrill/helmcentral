import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useInboxCount } from '@/hooks/use-inbox'
import { resetQueryStore } from '@/lib/api-query'
import { inboxApi } from '@/lib/inbox-api'
import { stubApi } from './helpers/stub-api'

// The badge on the Inbox row asks the box how many items wait. It is a
// background read, so it polls slowly and not at all while nobody is looking.

const FIVE_MINUTES = 5 * 60_000

function setHidden(hidden: boolean) {
  Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => (hidden ? 'hidden' : 'visible') })
  Object.defineProperty(document, 'hidden', { configurable: true, get: () => hidden })
}

const settle = (ms: number) => act(async () => { await vi.advanceTimersByTimeAsync(ms) })

beforeEach(() => {
  vi.useFakeTimers()
  setHidden(false)
})
afterEach(() => {
  resetQueryStore()
  vi.useRealTimers()
  vi.unstubAllGlobals()
  setHidden(false)
})

describe('useInboxCount', () => {
  it('asks once on mount, then every five minutes', async () => {
    let n = 0
    const mock = stubApi({ 'GET /api/inbox/count': () => ({ count: ++n }) })
    const { result } = renderHook(() => useInboxCount())
    await settle(0)
    expect(result.current).toBe(1)
    await settle(FIVE_MINUTES - 1_000)
    expect(mock).toHaveBeenCalledTimes(1)
    await settle(1_000)
    expect(mock).toHaveBeenCalledTimes(2)
    expect(result.current).toBe(2)
  })

  it('does not poll while the tab is hidden, and asks again as it comes back', async () => {
    const mock = stubApi({ 'GET /api/inbox/count': { count: 3 } })
    renderHook(() => useInboxCount())
    await settle(0)
    setHidden(true)
    await settle(FIVE_MINUTES * 3)
    expect(mock).toHaveBeenCalledTimes(1)

    setHidden(false)
    await act(async () => { document.dispatchEvent(new Event('visibilitychange')) })
    await settle(0)
    expect(mock).toHaveBeenCalledTimes(2)
  })

  it('asks again when the window regains focus', async () => {
    const mock = stubApi({ 'GET /api/inbox/count': { count: 3 } })
    renderHook(() => useInboxCount())
    await settle(0)
    await act(async () => { window.dispatchEvent(new Event('focus')) })
    await settle(0)
    expect(mock).toHaveBeenCalledTimes(2)
  })

  it('is read again when a batch of uploads ends or a count checkpoint passes', async () => {
    let n = 0
    const mock = stubApi({ 'GET /api/inbox/count': () => ({ count: ++n }), 'GET /api/inbox': { items: [] } })
    const { result } = renderHook(() => useInboxCount())
    await settle(0)
    await act(async () => { await inboxApi.refreshCount() })
    expect(result.current).toBe(2)
    await act(async () => { await inboxApi.refresh() })
    expect(result.current).toBe(3)
    expect(mock.mock.calls.filter(([url]) => String(url).endsWith('/api/inbox/count'))).toHaveLength(3)
  })
})
