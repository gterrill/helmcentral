import { renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { inboxWaitingOnMate, useInbox } from '@/hooks/use-inbox'
import { resetQueryStore } from '@/lib/api-query'
import { stubApi } from './helpers/stub-api'

beforeEach(() => resetQueryStore())

const row = (id: string, over: Record<string, unknown> = {}) => ({
  id, filename: `${id}.jpg`, mime: 'image/jpeg', size_bytes: 1, capture_ms: null, bearing_deg: null, fingerprint: '', sharpness: 0,
  created_ms: 0, content_url: `/${id}`, group: { kind: 'no_time' }, gps_mismatch: false, suggested_kind: null, ...over,
})

describe('useInbox', () => {
  it('reports whether Mate is sorting alongside the items', async () => {
    stubApi({ 'GET /api/inbox': { items: [row('a', { suggested_kind: 'document' })], mate_sorting: true } })
    const { result } = renderHook(() => useInbox())
    await waitFor(() => expect(result.current.items).toHaveLength(1))
    expect(result.current.mateSorting).toBe(true)
  })

  it('carries the notice when the box could not read Mate\'s settings, and does not treat Mate as sorting', async () => {
    stubApi({ 'GET /api/inbox': { items: [row('a')], mate_sorting: null, mate_error: "Mate's settings couldn't be read; sorting is paused" } })
    const { result } = renderHook(() => useInbox())
    await waitFor(() => expect(result.current.items).toHaveLength(1))
    expect(result.current.mateSorting).toBe(false)
    expect(result.current.mateError).toBe("Mate's settings couldn't be read; sorting is paused")
  })

  it('has no notice when Mate is fine', async () => {
    stubApi({ 'GET /api/inbox': { items: [row('a')], mate_sorting: true } })
    const { result } = renderHook(() => useInbox())
    await waitFor(() => expect(result.current.items).toHaveLength(1))
    expect(result.current.mateError).toBeNull()
  })

  it('reads a list from a box that does not say as Mate not sorting', async () => {
    stubApi({ 'GET /api/inbox': { items: [row('a')] } })
    const { result } = renderHook(() => useInbox())
    await waitFor(() => expect(result.current.items).toHaveLength(1))
    expect(result.current.mateSorting).toBe(false)
  })
})

describe('useInbox polling while Mate sorts', () => {
  afterEach(() => vi.useRealTimers())

  const reads = (mock: ReturnType<typeof stubApi>) => mock.mock.calls.filter(([u]) => new URL(String(u), 'http://x').pathname === '/api/inbox').length

  it('backs off 4 s, 8 s, then 15 s, and gives up after 10 minutes with nothing changing', async () => {
    vi.useFakeTimers()
    const mock = stubApi({ 'GET /api/inbox': { items: [row('a')], mate_sorting: true } })
    renderHook(() => useInbox())
    await vi.advanceTimersByTimeAsync(0)
    expect(reads(mock)).toBe(1)

    await vi.advanceTimersByTimeAsync(3_900)
    expect(reads(mock)).toBe(1)
    await vi.advanceTimersByTimeAsync(100) // 4 s
    expect(reads(mock)).toBe(2)
    await vi.advanceTimersByTimeAsync(7_900)
    expect(reads(mock)).toBe(2)
    await vi.advanceTimersByTimeAsync(100) // 4 + 8 = 12 s
    expect(reads(mock)).toBe(3)
    await vi.advanceTimersByTimeAsync(14_900)
    expect(reads(mock)).toBe(3)
    await vi.advanceTimersByTimeAsync(100) // 12 + 15 = 27 s
    expect(reads(mock)).toBe(4)

    await vi.advanceTimersByTimeAsync(11 * 60_000)
    const atGiveUp = reads(mock)
    expect(atGiveUp).toBeGreaterThan(30)
    await vi.advanceTimersByTimeAsync(60 * 60_000)
    expect(reads(mock)).toBe(atGiveUp)
  })

  it('starts again from 4 s when something changes', async () => {
    vi.useFakeTimers()
    let items = [row('a')]
    const mock = stubApi({ 'GET /api/inbox': () => ({ items, mate_sorting: true }) })
    renderHook(() => useInbox())
    await vi.advanceTimersByTimeAsync(4_000 + 8_000 + 15_000) // into the 15 s steps
    const before = reads(mock)
    items = [row('a'), row('b')] // a new photo arrives; the next read sees it
    await vi.advanceTimersByTimeAsync(15_000)
    expect(reads(mock)).toBe(before + 1)
    await vi.advanceTimersByTimeAsync(4_000) // the new item restarts the steps
    expect(reads(mock)).toBe(before + 2)
  })

  it('does not poll when Mate has nothing left to sort', async () => {
    vi.useFakeTimers()
    const mock = stubApi({ 'GET /api/inbox': { items: [row('a', { suggested_kind: 'scenery' })], mate_sorting: true } })
    renderHook(() => useInbox())
    await vi.advanceTimersByTimeAsync(60_000)
    expect(reads(mock)).toBe(1)
  })
})

describe('useInbox polling while a sort waits on a retry', () => {
  afterEach(() => vi.useRealTimers())

  const reads = (mock: ReturnType<typeof stubApi>) => mock.mock.calls.filter(([u]) => new URL(String(u), 'http://x').pathname === '/api/inbox').length
  const failed = (id: string, nextTryMs: number) => row(id, { classify_error: `Mate couldn't sort this yet. Trying again at 3.40 PM`, next_try_ms: nextTryMs })

  it('keeps reading about every 60 s for as long as an item waits on a retry, with no give-up', async () => {
    vi.useFakeTimers()
    const mock = stubApi({ 'GET /api/inbox': () => ({ items: [failed('a', Date.now() + 6 * 60 * 60_000)], mate_sorting: true }) })
    renderHook(() => useInbox())
    await vi.advanceTimersByTimeAsync(0)
    expect(reads(mock)).toBe(1)
    await vi.advanceTimersByTimeAsync(59_000)
    expect(reads(mock)).toBe(1)
    await vi.advanceTimersByTimeAsync(1_000)
    expect(reads(mock)).toBe(2)
    await vi.advanceTimersByTimeAsync(60_000)
    expect(reads(mock)).toBe(3)
    await vi.advanceTimersByTimeAsync(2 * 60 * 60_000) // far past the 10 minutes the quick polling gives up after
    expect(reads(mock)).toBeGreaterThan(100)
  })

  it('reads again just after the next try is due, not a minute later', async () => {
    vi.useFakeTimers()
    const mock = stubApi({ 'GET /api/inbox': () => ({ items: [failed('a', Date.now() + 20_000)], mate_sorting: true }) })
    renderHook(() => useInbox())
    await vi.advanceTimersByTimeAsync(0)
    expect(reads(mock)).toBe(1)
    await vi.advanceTimersByTimeAsync(19_000)
    expect(reads(mock)).toBe(1)
    await vi.advanceTimersByTimeAsync(5_000) // 24 s: the retry was due at 20 s
    expect(reads(mock)).toBe(2)
  })

  it('stops reading once no item waits on a retry', async () => {
    vi.useFakeTimers()
    let items = [failed('a', Date.now() + 6 * 60 * 60_000)]
    const mock = stubApi({ 'GET /api/inbox': () => ({ items, mate_sorting: true }) })
    renderHook(() => useInbox())
    await vi.advanceTimersByTimeAsync(60_000)
    expect(reads(mock)).toBe(2)
    items = [row('a', { suggested_kind: 'scenery' })]
    await vi.advanceTimersByTimeAsync(60_000) // this read sees it sorted
    const atSorted = reads(mock)
    await vi.advanceTimersByTimeAsync(10 * 60_000)
    expect(reads(mock)).toBe(atSorted)
  })
})

describe('inboxWaitingOnMate', () => {
  it('is true while Mate is set up and an item has neither a kind nor a recorded failure', () => {
    expect(inboxWaitingOnMate({ mate_sorting: true, items: [row('a'), row('b', { suggested_kind: 'scenery' })] } as never)).toBe(true)
  })

  it('is false once every item is sorted or has failed, when Mate is off, and with nothing waiting', () => {
    expect(inboxWaitingOnMate({ mate_sorting: true, items: [row('a', { classify_error: 'x' }), row('b', { suggested_kind: 'scenery' })] } as never)).toBe(false)
    expect(inboxWaitingOnMate({ mate_sorting: false, items: [row('a')] } as never)).toBe(false)
    expect(inboxWaitingOnMate(undefined)).toBe(false)
  })
})
