/**
 * Fake-timer coverage for the wall-display rotation (ADR 0110, superseding
 * ADR 0089). Each test drives dwell_seconds directly through vitest's fake
 * timers rather than mounting a page-rendering tree — this hook only decides
 * which page id is showing, handed off through onShow (App.tsx passes
 * setActivePageId).
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useDisplayRotation } from '@/hooks/use-display-rotation'
import type { DisplayEligiblePage } from '@/lib/displays'
import type { DashboardLayoutItem } from '@/lib/dashboard-widgets'

const WIDGET: DashboardLayoutItem = { id: 'depth-tide', x: 0, y: 0, w: 4, h: 7 }
const FLYBRIDGE = 'flybridge'

function page(id: string, overrides: Partial<DisplayEligiblePage> = {}): DisplayEligiblePage {
  return { id, widgets: [WIDGET], display_id: FLYBRIDGE, dwell_seconds: 10, ...overrides }
}

describe('useDisplayRotation', () => {
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
    renderHook(() => useDisplayRotation({ enabled: true, displayId: FLYBRIDGE, pages, navigationState: null, pinnedPageId: null, refetch, onShow }))

    expect(onShow).toHaveBeenCalledWith('a')
    expect(refetch).not.toHaveBeenCalled()
  })

  it('advances to the next page after its duration, without refetching', async () => {
    const onShow = vi.fn()
    const refetch = vi.fn().mockResolvedValue(undefined)
    const pages = [page('a'), page('b'), page('c')]
    renderHook(() => useDisplayRotation({ enabled: true, displayId: FLYBRIDGE, pages, navigationState: null, pinnedPageId: null, refetch, onShow }))

    await act(async () => { await vi.advanceTimersByTimeAsync(10_000) })

    expect(onShow).toHaveBeenLastCalledWith('b')
    expect(refetch).not.toHaveBeenCalled()
  })

  it('refetches exactly once, at the wrap back to the first page', async () => {
    const onShow = vi.fn()
    const refetch = vi.fn().mockResolvedValue(undefined)
    const pages = [page('a'), page('b')]
    renderHook(() => useDisplayRotation({ enabled: true, displayId: FLYBRIDGE, pages, navigationState: null, pinnedPageId: null, refetch, onShow }))

    await act(async () => { await vi.advanceTimersByTimeAsync(10_000) }) // a -> b
    expect(refetch).not.toHaveBeenCalled()

    await act(async () => { await vi.advanceTimersByTimeAsync(10_000) }) // b -> wraps to a
    expect(refetch).toHaveBeenCalledTimes(1)
    expect(onShow).toHaveBeenLastCalledWith('a')
  })

  it("finishes a page's own slot before an unassignment mid-lap takes effect", async () => {
    const onShow = vi.fn()
    const refetch = vi.fn().mockResolvedValue(undefined)
    let pages = [page('a'), page('b')]
    const { rerender } = renderHook(
      (props: { pages: DisplayEligiblePage[] }) =>
        useDisplayRotation({ enabled: true, displayId: FLYBRIDGE, pages: props.pages, navigationState: null, pinnedPageId: null, refetch, onShow }),
      { initialProps: { pages } },
    )
    expect(onShow).toHaveBeenLastCalledWith('a')

    // "b" is unassigned from the display while "a" is still showing.
    pages = [page('a'), page('b', { display_id: undefined })]
    rerender({ pages })

    // "a"'s own slot still runs its full duration - the unassignment
    // elsewhere doesn't cut the page currently on screen short.
    await act(async () => { await vi.advanceTimersByTimeAsync(9_999) })
    expect(onShow).not.toHaveBeenCalledWith('b')

    // Once "a"'s slot ends, "b" has already dropped out of the feed.
    await act(async () => { await vi.advanceTimersByTimeAsync(1) })
    expect(onShow).not.toHaveBeenCalledWith('b')
  })

  it('never shows a page assigned to a different display', async () => {
    const onShow = vi.fn()
    const refetch = vi.fn().mockResolvedValue(undefined)
    const pages = [page('a'), page('other', { display_id: 'saloon-tv' }), page('c')]
    renderHook(() => useDisplayRotation({ enabled: true, displayId: FLYBRIDGE, pages, navigationState: null, pinnedPageId: null, refetch, onShow }))

    for (let i = 0; i < 6; i++) {
      await act(async () => { await vi.advanceTimersByTimeAsync(10_000) })
      expect(onShow).not.toHaveBeenCalledWith('other')
    }
  })

  it('drops a state-conditioned page after its slot and re-admits it once navigation state matches again', async () => {
    const onShow = vi.fn()
    const refetch = vi.fn().mockResolvedValue(undefined)
    const pages = [page('a'), page('x', { show_when: 'anchored' })]
    let navigationState: string | null = 'anchored'
    const { rerender } = renderHook(
      (props: { navigationState: string | null }) =>
        useDisplayRotation({ enabled: true, displayId: FLYBRIDGE, pages, navigationState: props.navigationState, pinnedPageId: null, refetch, onShow }),
      { initialProps: { navigationState } },
    )
    expect(onShow).toHaveBeenLastCalledWith('a')

    await act(async () => { await vi.advanceTimersByTimeAsync(10_000) })
    expect(onShow).toHaveBeenLastCalledWith('x') // anchored state, so "x" is in the feed

    navigationState = 'moored'
    rerender({ navigationState })
    await act(async () => { await vi.advanceTimersByTimeAsync(10_000) })
    expect(onShow).toHaveBeenLastCalledWith('a') // wrapped past "x", which is gone

    navigationState = 'anchored'
    rerender({ navigationState })
    await act(async () => { await vi.advanceTimersByTimeAsync(10_000) })
    expect(onShow).toHaveBeenLastCalledWith('x') // re-admitted
  })

  it('recovers immediately once pages become available, without waiting for the poll', async () => {
    const onShow = vi.fn()
    const refetch = vi.fn().mockResolvedValue(undefined)
    let pages: DisplayEligiblePage[] = []
    const { result, rerender } = renderHook(
      (props: { pages: DisplayEligiblePage[] }) =>
        useDisplayRotation({ enabled: true, displayId: FLYBRIDGE, pages: props.pages, navigationState: null, pinnedPageId: null, refetch, onShow }),
      { initialProps: { pages } },
    )
    expect(result.current.feedEmpty).toBe(true)
    expect(onShow).not.toHaveBeenCalled()

    pages = [page('a')]
    rerender({ pages })

    expect(onShow).toHaveBeenCalledWith('a')
    expect(result.current.feedEmpty).toBe(false)
    expect(refetch).not.toHaveBeenCalled()
  })

  it('polls with refetch every 15s while the feed stays empty with no local pages change', async () => {
    const onShow = vi.fn()
    const refetch = vi.fn().mockResolvedValue(undefined)
    const pages: DisplayEligiblePage[] = []
    renderHook(() => useDisplayRotation({ enabled: true, displayId: FLYBRIDGE, pages, navigationState: null, pinnedPageId: null, refetch, onShow }))

    await act(async () => { await vi.advanceTimersByTimeAsync(15_000) })
    expect(refetch).toHaveBeenCalledTimes(1)

    await act(async () => { await vi.advanceTimersByTimeAsync(15_000) })
    expect(refetch).toHaveBeenCalledTimes(2)
    expect(onShow).not.toHaveBeenCalled()
  })

  it('pins one page and never advances', async () => {
    const onShow = vi.fn()
    const refetch = vi.fn().mockResolvedValue(undefined)
    const pages = [page('a', { dwell_seconds: 5 }), page('b', { dwell_seconds: 5 })]
    renderHook(() => useDisplayRotation({ enabled: true, displayId: FLYBRIDGE, pages, navigationState: null, pinnedPageId: 'b', refetch, onShow }))

    expect(onShow).toHaveBeenCalledWith('b')
    await act(async () => { await vi.advanceTimersByTimeAsync(60_000) })
    expect(onShow).toHaveBeenCalledTimes(1)
    expect(refetch).not.toHaveBeenCalled()
  })

  it('does nothing while disabled', async () => {
    const onShow = vi.fn()
    const refetch = vi.fn().mockResolvedValue(undefined)
    const pages = [page('a')]
    renderHook(() => useDisplayRotation({ enabled: false, displayId: FLYBRIDGE, pages, navigationState: null, pinnedPageId: null, refetch, onShow }))

    await act(async () => { await vi.advanceTimersByTimeAsync(60_000) })
    expect(onShow).not.toHaveBeenCalled()
  })

  // --- ADR 0110 additions: displayId resolution, pause/resume/step -------

  it('does nothing while displayId has not resolved yet, even if enabled is true', async () => {
    const onShow = vi.fn()
    const refetch = vi.fn().mockResolvedValue(undefined)
    const pages = [page('a')]
    renderHook(() => useDisplayRotation({ enabled: true, displayId: null, pages, navigationState: null, pinnedPageId: null, refetch, onShow }))

    await act(async () => { await vi.advanceTimersByTimeAsync(60_000) })
    expect(onShow).not.toHaveBeenCalled()
  })

  it('starts the lap the instant displayId resolves from null to an id', () => {
    const onShow = vi.fn()
    const refetch = vi.fn().mockResolvedValue(undefined)
    const pages = [page('a'), page('b')]
    const { rerender } = renderHook(
      (props: { displayId: string | null }) =>
        useDisplayRotation({ enabled: true, displayId: props.displayId, pages, navigationState: null, pinnedPageId: null, refetch, onShow }),
      { initialProps: { displayId: null as string | null } },
    )
    expect(onShow).not.toHaveBeenCalled()

    rerender({ displayId: FLYBRIDGE })
    expect(onShow).toHaveBeenCalledWith('a')
  })

  it('reports position as the current page\'s index and the feed length', async () => {
    const onShow = vi.fn()
    const refetch = vi.fn().mockResolvedValue(undefined)
    const pages = [page('a'), page('b'), page('c')]
    const { result } = renderHook(() => useDisplayRotation({ enabled: true, displayId: FLYBRIDGE, pages, navigationState: null, pinnedPageId: null, refetch, onShow }))

    expect(result.current.position).toEqual({ index: 0, total: 3 })

    await act(async () => { await vi.advanceTimersByTimeAsync(10_000) })
    expect(result.current.position).toEqual({ index: 1, total: 3 })
  })

  it('next() steps immediately and restarts the dwell, without a refetch mid-lap', async () => {
    const onShow = vi.fn()
    const refetch = vi.fn().mockResolvedValue(undefined)
    const pages = [page('a'), page('b'), page('c')]
    const { result } = renderHook(() => useDisplayRotation({ enabled: true, displayId: FLYBRIDGE, pages, navigationState: null, pinnedPageId: null, refetch, onShow }))
    expect(onShow).toHaveBeenLastCalledWith('a')

    act(() => { result.current.next() })
    expect(onShow).toHaveBeenLastCalledWith('b')
    expect(refetch).not.toHaveBeenCalled()

    // The dwell restarted from this manual step - advancing just short of a
    // full dwell from here must not yet move past "b".
    await act(async () => { await vi.advanceTimersByTimeAsync(9_999) })
    expect(onShow).toHaveBeenLastCalledWith('b')
    await act(async () => { await vi.advanceTimersByTimeAsync(1) })
    expect(onShow).toHaveBeenLastCalledWith('c')
  })

  it('previous() steps backward immediately and restarts the dwell', async () => {
    const onShow = vi.fn()
    const refetch = vi.fn().mockResolvedValue(undefined)
    const pages = [page('a'), page('b'), page('c')]
    const { result } = renderHook(() => useDisplayRotation({ enabled: true, displayId: FLYBRIDGE, pages, navigationState: null, pinnedPageId: null, refetch, onShow }))
    expect(onShow).toHaveBeenLastCalledWith('a')

    act(() => { result.current.next() }) // a(0) -> b(1)
    act(() => { result.current.next() }) // b(1) -> c(2)
    // c(2) -> b(1) is not a wrap (doesn't land on index 0), so this stays synchronous.
    act(() => { result.current.previous() })
    expect(onShow).toHaveBeenLastCalledWith('b')
    expect(refetch).not.toHaveBeenCalled()
  })

  it('a manual next() that lands on index 0 refetches first, same as the automatic top-of-lap wrap', async () => {
    const onShow = vi.fn()
    const refetch = vi.fn().mockResolvedValue(undefined)
    const pages = [page('a'), page('b')]
    const { result } = renderHook(() => useDisplayRotation({ enabled: true, displayId: FLYBRIDGE, pages, navigationState: null, pinnedPageId: null, refetch, onShow }))
    expect(onShow).toHaveBeenLastCalledWith('a')

    act(() => { result.current.next() }) // a -> b: not a wrap
    expect(refetch).not.toHaveBeenCalled()

    await act(async () => { result.current.next() }) // b -> wraps to a
    expect(refetch).toHaveBeenCalledTimes(1)
    expect(onShow).toHaveBeenLastCalledWith('a')
  })

  it('a manual previous() that lands on index 0 also counts as a wrap', async () => {
    const onShow = vi.fn()
    const refetch = vi.fn().mockResolvedValue(undefined)
    const pages = [page('a'), page('b')]
    const { result } = renderHook(() => useDisplayRotation({ enabled: true, displayId: FLYBRIDGE, pages, navigationState: null, pinnedPageId: null, refetch, onShow }))

    act(() => { result.current.next() }) // a -> b
    expect(refetch).not.toHaveBeenCalled()

    await act(async () => { result.current.previous() }) // b -> a, lands on index 0
    expect(refetch).toHaveBeenCalledTimes(1)
    expect(onShow).toHaveBeenLastCalledWith('a')
  })

  it('pause() stops the timer, and the top-of-lap refetch never fires while paused', async () => {
    const onShow = vi.fn()
    const refetch = vi.fn().mockResolvedValue(undefined)
    const pages = [page('a'), page('b')]
    const { result } = renderHook(() => useDisplayRotation({ enabled: true, displayId: FLYBRIDGE, pages, navigationState: null, pinnedPageId: null, refetch, onShow }))
    expect(onShow).toHaveBeenLastCalledWith('a')

    act(() => { result.current.pause() })
    expect(result.current.paused).toBe(true)

    // Two full laps' worth of time: if the timer were still running this
    // would advance past "b" and wrap (refetching) at least once.
    await act(async () => { await vi.advanceTimersByTimeAsync(40_000) })
    expect(onShow).toHaveBeenLastCalledWith('a')
    expect(refetch).not.toHaveBeenCalled()
  })

  it('resume() starts a full dwell for the page currently showing, not the remainder of the interrupted one', async () => {
    const onShow = vi.fn()
    const refetch = vi.fn().mockResolvedValue(undefined)
    const pages = [page('a'), page('b')]
    const { result } = renderHook(() => useDisplayRotation({ enabled: true, displayId: FLYBRIDGE, pages, navigationState: null, pinnedPageId: null, refetch, onShow }))

    // Let 9s of "a"'s 10s dwell elapse, then pause with 1s left.
    await act(async () => { await vi.advanceTimersByTimeAsync(9_000) })
    act(() => { result.current.pause() })

    act(() => { result.current.resume() })
    expect(result.current.paused).toBe(false)

    // If the remainder (1s) had resumed rather than a full dwell, this would
    // already have advanced to "b".
    await act(async () => { await vi.advanceTimersByTimeAsync(1_000) })
    expect(onShow).toHaveBeenLastCalledWith('a')

    // The full 10s dwell does eventually elapse from the resume point.
    await act(async () => { await vi.advanceTimersByTimeAsync(9_000) })
    expect(onShow).toHaveBeenLastCalledWith('b')
  })

  it('arrow-key steps still work while paused, but do not resume automatic rotation', async () => {
    const onShow = vi.fn()
    const refetch = vi.fn().mockResolvedValue(undefined)
    const pages = [page('a'), page('b'), page('c')]
    const { result } = renderHook(() => useDisplayRotation({ enabled: true, displayId: FLYBRIDGE, pages, navigationState: null, pinnedPageId: null, refetch, onShow }))

    act(() => { result.current.pause() })
    act(() => { result.current.next() }) // a -> b, while paused
    expect(onShow).toHaveBeenLastCalledWith('b')
    expect(result.current.paused).toBe(true)

    // No timer was scheduled for "b" because the rotation is still paused.
    await act(async () => { await vi.advanceTimersByTimeAsync(60_000) })
    expect(onShow).toHaveBeenLastCalledWith('b')
  })

  it('next/previous/pause/resume are no-ops while disabled', async () => {
    const onShow = vi.fn()
    const refetch = vi.fn().mockResolvedValue(undefined)
    const pages = [page('a'), page('b')]
    const { result } = renderHook(() => useDisplayRotation({ enabled: false, displayId: FLYBRIDGE, pages, navigationState: null, pinnedPageId: null, refetch, onShow }))

    act(() => {
      result.current.next()
      result.current.previous()
      result.current.pause()
      result.current.resume()
    })
    expect(onShow).not.toHaveBeenCalled()
    expect(result.current.paused).toBe(false)
  })

  it('next/previous/pause/resume are no-ops while a page is pinned', () => {
    const onShow = vi.fn()
    const refetch = vi.fn().mockResolvedValue(undefined)
    const pages = [page('a'), page('b')]
    const { result } = renderHook(() => useDisplayRotation({ enabled: true, displayId: FLYBRIDGE, pages, navigationState: null, pinnedPageId: 'a', refetch, onShow }))
    expect(onShow).toHaveBeenCalledWith('a')
    onShow.mockClear()

    act(() => {
      result.current.next()
      result.current.pause()
    })
    expect(onShow).not.toHaveBeenCalled()
    expect(result.current.paused).toBe(false)
  })
})
