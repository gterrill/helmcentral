import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { usePixelShift } from '@/hooks/use-pixel-shift'
import { PIXEL_SHIFT_AMPLITUDE_PX } from '@/lib/displays'

// ADR 0110 §5a: a slow four-position shift of the wall board, to spare an
// OLED panel a static image. Four minutes per position, sixteen minutes for
// the full loop back to the origin.

const STEP_MS = 4 * 60 * 1000
const A = PIXEL_SHIFT_AMPLITUDE_PX

describe('usePixelShift', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('returns {0,0} immediately when disabled', () => {
    const { result } = renderHook(() => usePixelShift(false))
    expect(result.current).toEqual({ dx: 0, dy: 0 })
  })

  it('never advances while disabled, however long it runs', () => {
    const { result } = renderHook(() => usePixelShift(false))
    act(() => { vi.advanceTimersByTime(STEP_MS * 10) })
    expect(result.current).toEqual({ dx: 0, dy: 0 })
  })

  it('cycles the four positions in order, one per four-minute step, and wraps back to the origin', () => {
    const { result } = renderHook(() => usePixelShift(true))
    expect(result.current).toEqual({ dx: 0, dy: 0 })

    act(() => { vi.advanceTimersByTime(STEP_MS) })
    expect(result.current).toEqual({ dx: A, dy: 0 })

    act(() => { vi.advanceTimersByTime(STEP_MS) })
    expect(result.current).toEqual({ dx: A, dy: A })

    act(() => { vi.advanceTimersByTime(STEP_MS) })
    expect(result.current).toEqual({ dx: 0, dy: A })

    // Fourth step: the sixteen-minute mark, back to the origin.
    act(() => { vi.advanceTimersByTime(STEP_MS) })
    expect(result.current).toEqual({ dx: 0, dy: 0 })

    // A second lap runs the same sequence.
    act(() => { vi.advanceTimersByTime(STEP_MS) })
    expect(result.current).toEqual({ dx: A, dy: 0 })
  })

  it('clears its interval on unmount, so it can never fire into an unmounted component', () => {
    const clearSpy = vi.spyOn(globalThis, 'clearInterval')
    const { unmount } = renderHook(() => usePixelShift(true))
    unmount()
    expect(clearSpy).toHaveBeenCalled()
  })

  it('returns to {0,0} when enabled turns false mid-cycle', () => {
    const { result, rerender } = renderHook(({ enabled }) => usePixelShift(enabled), {
      initialProps: { enabled: true },
    })
    act(() => { vi.advanceTimersByTime(STEP_MS * 2) })
    expect(result.current).toEqual({ dx: A, dy: A })

    rerender({ enabled: false })
    expect(result.current).toEqual({ dx: 0, dy: 0 })
  })
})
