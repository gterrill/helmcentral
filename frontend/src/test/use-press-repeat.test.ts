import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook } from '@testing-library/react'
import { usePressRepeat, PRESS_REPEAT_INITIAL_DELAY_MS, PRESS_REPEAT_INTERVAL_MS } from '@/hooks/use-press-repeat'

// Hold-to-repeat for the Adjust mode's radius +/- buttons: a single tap
// steps once immediately, a held tap starts repeating after an initial
// delay, then repeats faster than the delay so a long hold moves the radius
// quickly. Fake timers throughout — real 400ms/80ms waits would make this
// suite slow for no benefit.
describe('usePressRepeat', () => {
  beforeEach(() => { vi.useFakeTimers() })
  afterEach(() => { vi.useRealTimers() })

  it('steps once immediately on pointer down, before the initial delay elapses', () => {
    const onStep = vi.fn()
    const { result } = renderHook(() => usePressRepeat(onStep))
    result.current.onPointerDown()
    expect(onStep).toHaveBeenCalledTimes(1)
  })

  it('starts repeating only after the initial delay, then repeats on the shorter interval', () => {
    const onStep = vi.fn()
    const { result } = renderHook(() => usePressRepeat(onStep))
    result.current.onPointerDown()
    expect(onStep).toHaveBeenCalledTimes(1)

    vi.advanceTimersByTime(PRESS_REPEAT_INITIAL_DELAY_MS - 1)
    expect(onStep).toHaveBeenCalledTimes(1)

    // The initial delay elapsing starts the repeat interval; the interval's
    // own first tick lands PRESS_REPEAT_INTERVAL_MS after that, not
    // instantly (ordinary setInterval semantics).
    vi.advanceTimersByTime(1)
    expect(onStep).toHaveBeenCalledTimes(1)
    vi.advanceTimersByTime(PRESS_REPEAT_INTERVAL_MS)
    expect(onStep).toHaveBeenCalledTimes(2)

    vi.advanceTimersByTime(PRESS_REPEAT_INTERVAL_MS)
    expect(onStep).toHaveBeenCalledTimes(3)
    vi.advanceTimersByTime(PRESS_REPEAT_INTERVAL_MS)
    expect(onStep).toHaveBeenCalledTimes(4)
  })

  it('stops repeating on pointer up', () => {
    const onStep = vi.fn()
    const { result } = renderHook(() => usePressRepeat(onStep))
    result.current.onPointerDown()
    vi.advanceTimersByTime(PRESS_REPEAT_INITIAL_DELAY_MS + PRESS_REPEAT_INTERVAL_MS)
    expect(onStep).toHaveBeenCalledTimes(2)

    result.current.onPointerUp()
    vi.advanceTimersByTime(PRESS_REPEAT_INTERVAL_MS * 5)
    expect(onStep).toHaveBeenCalledTimes(2)
  })

  it('also stops on pointer leave/cancel (a drag off the button, or an interrupted gesture)', () => {
    const onStep = vi.fn()
    const { result } = renderHook(() => usePressRepeat(onStep))
    result.current.onPointerDown()
    result.current.onPointerLeave()
    vi.advanceTimersByTime(PRESS_REPEAT_INITIAL_DELAY_MS + PRESS_REPEAT_INTERVAL_MS * 3)
    expect(onStep).toHaveBeenCalledTimes(1)

    const { result: result2 } = renderHook(() => usePressRepeat(onStep))
    result2.current.onPointerDown()
    result2.current.onPointerCancel()
    vi.advanceTimersByTime(PRESS_REPEAT_INITIAL_DELAY_MS + PRESS_REPEAT_INTERVAL_MS * 3)
    expect(onStep).toHaveBeenCalledTimes(2)
  })

  it('a second pointer down restarts cleanly rather than stacking intervals', () => {
    const onStep = vi.fn()
    const { result } = renderHook(() => usePressRepeat(onStep))
    result.current.onPointerDown()
    result.current.onPointerUp()
    onStep.mockClear()

    result.current.onPointerDown()
    expect(onStep).toHaveBeenCalledTimes(1)
    vi.advanceTimersByTime(PRESS_REPEAT_INITIAL_DELAY_MS + PRESS_REPEAT_INTERVAL_MS)
    expect(onStep).toHaveBeenCalledTimes(2)
  })

  it('never starts a timer merely from rendering (StrictMode-safe: only pointerdown starts one)', () => {
    const onStep = vi.fn()
    renderHook(() => usePressRepeat(onStep))
    vi.advanceTimersByTime(PRESS_REPEAT_INITIAL_DELAY_MS + PRESS_REPEAT_INTERVAL_MS * 5)
    expect(onStep).not.toHaveBeenCalled()
  })

  it('cleans up its timers on unmount so a late tick cannot fire into an unmounted component', () => {
    const onStep = vi.fn()
    const { result, unmount } = renderHook(() => usePressRepeat(onStep))
    result.current.onPointerDown()
    unmount()
    vi.advanceTimersByTime(PRESS_REPEAT_INITIAL_DELAY_MS + PRESS_REPEAT_INTERVAL_MS * 5)
    expect(onStep).toHaveBeenCalledTimes(1) // only the immediate tap before unmount
  })

  it('always calls the latest onStep, not one captured at mount', () => {
    const first = vi.fn()
    const second = vi.fn()
    const { result, rerender } = renderHook(({ cb }) => usePressRepeat(cb), { initialProps: { cb: first } })
    rerender({ cb: second })
    result.current.onPointerDown()
    expect(first).not.toHaveBeenCalled()
    expect(second).toHaveBeenCalledTimes(1)
  })

  // Code-review finding: holding + until the button hits atMax disables it
  // (pointer-events-none via the disabled attribute), so pointerup never
  // reaches the DOM and the repeat interval this hook started kept firing
  // forever. The hook itself now cancels the repeat the instant its caller
  // reports disabled, rather than relying on a pointer event that a disabled
  // button will never receive.
  describe('disabled', () => {
    it('stops an in-progress repeat the moment disabled flips true mid-hold', () => {
      const onStep = vi.fn()
      const { result, rerender } = renderHook(({ disabled }) => usePressRepeat(onStep, disabled), {
        initialProps: { disabled: false },
      })
      result.current.onPointerDown()
      vi.advanceTimersByTime(PRESS_REPEAT_INITIAL_DELAY_MS + PRESS_REPEAT_INTERVAL_MS)
      expect(onStep).toHaveBeenCalledTimes(2)

      rerender({ disabled: true })
      vi.advanceTimersByTime(PRESS_REPEAT_INTERVAL_MS * 5)
      expect(onStep).toHaveBeenCalledTimes(2)
    })

    it('a later pointerdown after re-enabling starts the repeat again', () => {
      const onStep = vi.fn()
      const { result, rerender } = renderHook(({ disabled }) => usePressRepeat(onStep, disabled), {
        initialProps: { disabled: true },
      })
      rerender({ disabled: false })
      result.current.onPointerDown()
      expect(onStep).toHaveBeenCalledTimes(1)
    })
  })
})
