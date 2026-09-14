/**
 * Fake-timer coverage for the wall-display rotation (ADR 0089). Each test
 * drives kiosk_seconds directly through vitest's fake timers rather than
 * mounting a page-rendering tree — this hook only decides which page id is
 * showing, handed off through onShow (App.tsx passes setActivePageId).
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useKioskRotation } from '@/hooks/use-kiosk-rotation'
import type { KioskEligiblePage } from '@/lib/kiosk'
import type { DashboardLayoutItem } from '@/lib/dashboard-widgets'

const WIDGET: DashboardLayoutItem = { id: 'depth-tide', x: 0, y: 0, w: 4, h: 7 }

function page(id: string, overrides: Partial<KioskEligiblePage> = {}): KioskEligiblePage {
  return { id, widgets: [WIDGET], kiosk: true, kiosk_seconds: 10, ...overrides }
}

describe('useKioskRotation', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it('shows the first page in the feed immediately, without refetching', () => {
    const onShow = vi.fn()
    const refetch = vi.fn().mockResolvedValue(undefined)
    const pages = [page('a'), page('b')]
    renderHook(() => useKioskRotation({ enabled: true, pages, navigationState: null, pinnedPageId: null, refetch, onShow }))

    expect(onShow).toHaveBeenCalledWith('a')
    expect(refetch).not.toHaveBeenCalled()
  })

  it('advances to the next page after its duration, without refetching', async () => {
    const onShow = vi.fn()
    const refetch = vi.fn().mockResolvedValue(undefined)
    const pages = [page('a'), page('b'), page('c')]
    renderHook(() => useKioskRotation({ enabled: true, pages, navigationState: null, pinnedPageId: null, refetch, onShow }))

    await act(async () => { await vi.advanceTimersByTimeAsync(10_000) })

    expect(onShow).toHaveBeenLastCalledWith('b')
    expect(refetch).not.toHaveBeenCalled()
  })

  it('refetches exactly once, at the wrap back to the first page', async () => {
    const onShow = vi.fn()
    const refetch = vi.fn().mockResolvedValue(undefined)
    const pages = [page('a'), page('b')]
    renderHook(() => useKioskRotation({ enabled: true, pages, navigationState: null, pinnedPageId: null, refetch, onShow }))

    await act(async () => { await vi.advanceTimersByTimeAsync(10_000) }) // a -> b
    expect(refetch).not.toHaveBeenCalled()

    await act(async () => { await vi.advanceTimersByTimeAsync(10_000) }) // b -> wraps to a
    expect(refetch).toHaveBeenCalledTimes(1)
    expect(onShow).toHaveBeenLastCalledWith('a')
  })

  it("finishes a page's own slot before an unflag mid-lap takes effect", async () => {
    const onShow = vi.fn()
    const refetch = vi.fn().mockResolvedValue(undefined)
    let pages = [page('a'), page('b')]
    const { rerender } = renderHook(
      (props: { pages: KioskEligiblePage[] }) =>
        useKioskRotation({ enabled: true, pages: props.pages, navigationState: null, pinnedPageId: null, refetch, onShow }),
      { initialProps: { pages } },
    )
    expect(onShow).toHaveBeenLastCalledWith('a')

    // "b" is unflagged while "a" is still showing.
    pages = [page('a'), page('b', { kiosk: false })]
    rerender({ pages })

    // "a"'s own slot still runs its full duration - the unflag elsewhere
    // doesn't cut the page currently on screen short.
    await act(async () => { await vi.advanceTimersByTimeAsync(9_999) })
    expect(onShow).not.toHaveBeenCalledWith('b')

    // Once "a"'s slot ends, "b" has already dropped out of the feed.
    await act(async () => { await vi.advanceTimersByTimeAsync(1) })
    expect(onShow).not.toHaveBeenCalledWith('b')
  })

  it('drops a state-conditioned page after its slot and re-admits it once navigation state matches again', async () => {
    const onShow = vi.fn()
    const refetch = vi.fn().mockResolvedValue(undefined)
    const pages = [page('a'), page('x', { kiosk_when: 'anchored' })]
    let navigationState: string | null = 'anchored'
    const { rerender } = renderHook(
      (props: { navigationState: string | null }) =>
        useKioskRotation({ enabled: true, pages, navigationState: props.navigationState, pinnedPageId: null, refetch, onShow }),
      { initialProps: { navigationState } },
    )
    expect(onShow).toHaveBeenLastCalledWith('a')

    await act(async () => { await vi.advanceTimersByTimeAsync(10_000) })
    expect(onShow).toHaveBeenLastCalledWith('x') // anchored state, so "x" is in the feed

    // State moves away from anchored mid-slot; "x" still finishes showing
    // before it drops out on the next advance.
    navigationState = 'moored'
    rerender({ navigationState })
    await act(async () => { await vi.advanceTimersByTimeAsync(10_000) })
    expect(onShow).toHaveBeenLastCalledWith('a') // wrapped past "x", which is gone

    navigationState = 'anchored'
    rerender({ navigationState })
    await act(async () => { await vi.advanceTimersByTimeAsync(10_000) })
    expect(onShow).toHaveBeenLastCalledWith('x') // re-admitted
  })

  it('only includes motoring pages while navigation state is motoring', async () => {
    const onShow = vi.fn()
    const refetch = vi.fn().mockResolvedValue(undefined)
    const pages = [page('a'), page('m', { kiosk_when: 'motoring' })]
    let navigationState: string | null = 'moored'
    const { rerender } = renderHook(
      (props: { navigationState: string | null }) =>
        useKioskRotation({ enabled: true, pages, navigationState: props.navigationState, pinnedPageId: null, refetch, onShow }),
      { initialProps: { navigationState } },
    )

    await act(async () => { await vi.advanceTimersByTimeAsync(10_000) })
    expect(onShow).toHaveBeenLastCalledWith('a')

    navigationState = 'motoring'
    rerender({ navigationState })
    await act(async () => { await vi.advanceTimersByTimeAsync(10_000) })
    expect(onShow).toHaveBeenLastCalledWith('m')
  })

  it('recovers immediately once pages become available, without waiting for the poll', async () => {
    // Regression: on a real mount, useDashboardPages' own fetch is still in
    // flight when this hook first runs, so `pages` starts as `[]`. Without
    // reacting to that arriving, a kiosk that loads with an already-flagged
    // page would show "nothing flagged" for up to 15s on every reload.
    const onShow = vi.fn()
    const refetch = vi.fn().mockResolvedValue(undefined)
    let pages: KioskEligiblePage[] = []
    const { result, rerender } = renderHook(
      (props: { pages: KioskEligiblePage[] }) =>
        useKioskRotation({ enabled: true, pages: props.pages, navigationState: null, pinnedPageId: null, refetch, onShow }),
      { initialProps: { pages } },
    )
    expect(result.current.feedEmpty).toBe(true)
    expect(onShow).not.toHaveBeenCalled()

    pages = [page('a')]
    rerender({ pages })

    // No fake-timer advance at all: the page appears on the very next
    // render, not on the next scheduled poll tick.
    expect(onShow).toHaveBeenCalledWith('a')
    expect(result.current.feedEmpty).toBe(false)
    expect(refetch).not.toHaveBeenCalled()
  })

  it('polls with refetch every 15s while the feed stays empty with no local pages change', async () => {
    // Covers the complementary case: a page gets flagged from another
    // device, so nothing about this kiosk's own `pages` prop changes on its
    // own. The interval has to actively ask the server itself.
    const onShow = vi.fn()
    const refetch = vi.fn().mockResolvedValue(undefined)
    const pages: KioskEligiblePage[] = []
    renderHook(() => useKioskRotation({ enabled: true, pages, navigationState: null, pinnedPageId: null, refetch, onShow }))

    await act(async () => { await vi.advanceTimersByTimeAsync(15_000) })
    expect(refetch).toHaveBeenCalledTimes(1)

    await act(async () => { await vi.advanceTimersByTimeAsync(15_000) })
    expect(refetch).toHaveBeenCalledTimes(2)
    expect(onShow).not.toHaveBeenCalled()
  })

  it('pins one page and never advances', async () => {
    const onShow = vi.fn()
    const refetch = vi.fn().mockResolvedValue(undefined)
    const pages = [page('a', { kiosk_seconds: 5 }), page('b', { kiosk_seconds: 5 })]
    renderHook(() => useKioskRotation({ enabled: true, pages, navigationState: null, pinnedPageId: 'b', refetch, onShow }))

    expect(onShow).toHaveBeenCalledWith('b')
    await act(async () => { await vi.advanceTimersByTimeAsync(60_000) })
    expect(onShow).toHaveBeenCalledTimes(1)
    expect(refetch).not.toHaveBeenCalled()
  })

  it('does nothing while disabled', async () => {
    const onShow = vi.fn()
    const refetch = vi.fn().mockResolvedValue(undefined)
    const pages = [page('a')]
    renderHook(() => useKioskRotation({ enabled: false, pages, navigationState: null, pinnedPageId: null, refetch, onShow }))

    await act(async () => { await vi.advanceTimersByTimeAsync(60_000) })
    expect(onShow).not.toHaveBeenCalled()
  })
})
