import { act, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { useCyclingIndex } from '@/hooks/use-cycling-index'

afterEach(() => {
  vi.useRealTimers()
})

describe('useCyclingIndex', () => {
  it('starts at 0 and advances by one every intervalSeconds', () => {
    vi.useFakeTimers()
    const { result } = renderHook(() => useCyclingIndex(3, 5, 'a|b|c'))

    expect(result.current).toBe(0)

    act(() => { vi.advanceTimersByTime(5000) })
    expect(result.current).toBe(1)

    act(() => { vi.advanceTimersByTime(5000) })
    expect(result.current).toBe(2)
  })

  it('wraps back to 0 after the last index', () => {
    vi.useFakeTimers()
    const { result } = renderHook(() => useCyclingIndex(2, 10, 'a|b'))

    act(() => { vi.advanceTimersByTime(10000) })
    expect(result.current).toBe(1)

    act(() => { vi.advanceTimersByTime(10000) })
    expect(result.current).toBe(0)
  })

  it('does not advance before a full interval has passed', () => {
    vi.useFakeTimers()
    const { result } = renderHook(() => useCyclingIndex(3, 10, 'a|b|c'))

    act(() => { vi.advanceTimersByTime(9999) })
    expect(result.current).toBe(0)
  })

  it('returns null when there is nothing to cycle through', () => {
    const { result } = renderHook(() => useCyclingIndex(0, 10, ''))
    expect(result.current).toBeNull()
  })

  it('never advances when there is only one item, and starts no timer for it', () => {
    vi.useFakeTimers()
    const setIntervalSpy = vi.spyOn(window, 'setInterval')
    const { result } = renderHook(() => useCyclingIndex(1, 1, 'only'))

    expect(setIntervalSpy).not.toHaveBeenCalled()

    act(() => { vi.advanceTimersByTime(10000) })
    expect(result.current).toBe(0)
  })

  it('resets to 0 when resetKey changes, even mid-cycle', () => {
    vi.useFakeTimers()
    const { result, rerender } = renderHook(
      ({ resetKey }: { resetKey: string }) => useCyclingIndex(3, 5, resetKey),
      { initialProps: { resetKey: 'a|b|c' } },
    )

    act(() => { vi.advanceTimersByTime(5000) })
    expect(result.current).toBe(1)

    rerender({ resetKey: 'a|c' })
    expect(result.current).toBe(0)
  })

  it('does not reset when the same resetKey is passed again (a fresh array, same ids)', () => {
    vi.useFakeTimers()
    const { result, rerender } = renderHook(
      ({ resetKey }: { resetKey: string }) => useCyclingIndex(3, 5, resetKey),
      { initialProps: { resetKey: 'a|b|c' } },
    )

    act(() => { vi.advanceTimersByTime(5000) })
    expect(result.current).toBe(1)

    rerender({ resetKey: 'a|b|c' })
    expect(result.current).toBe(1)
  })

  it('clears its interval on unmount', () => {
    vi.useFakeTimers()
    const clearIntervalSpy = vi.spyOn(window, 'clearInterval')
    const { unmount } = renderHook(() => useCyclingIndex(3, 5, 'a|b|c'))

    unmount()

    expect(clearIntervalSpy).toHaveBeenCalled()
  })

  it('picks up a new interval length without losing the current index', () => {
    vi.useFakeTimers()
    const { result, rerender } = renderHook(
      ({ seconds }: { seconds: number }) => useCyclingIndex(3, seconds, 'a|b|c'),
      { initialProps: { seconds: 10 } },
    )

    act(() => { vi.advanceTimersByTime(10000) })
    expect(result.current).toBe(1)

    rerender({ seconds: 5 })
    act(() => { vi.advanceTimersByTime(5000) })
    expect(result.current).toBe(2)
  })
})
