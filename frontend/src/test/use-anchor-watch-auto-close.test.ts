import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useAnchorWatchAutoClose } from '@/hooks/use-anchor-watch-auto-close'

describe('useAnchorWatchAutoClose', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('should not arm when feature is disabled', () => {
    const { result } = renderHook(() =>
      useAnchorWatchAutoClose(
        'motoring', // navigationState
        30, // distanceMeters (outside circle)
        20, // radiusMeters
        true, // anchorWatchActive
        false, // isEnabled (disabled)
      ),
    )

    expect(result.current.isAutoCloseArmed).toBe(false)
    expect(result.current.motoringSecondsElapsed).toBe(0)
  })

  it('should not arm when anchor watch is not active', () => {
    const { result } = renderHook(() =>
      useAnchorWatchAutoClose(
        'motoring',
        30,
        20,
        false, // anchorWatchActive
        true, // isEnabled
      ),
    )

    expect(result.current.isAutoCloseArmed).toBe(false)
    expect(result.current.motoringSecondsElapsed).toBe(0)
  })

  it('should arm when engines are running and vessel is outside circle', () => {
    const { result } = renderHook(() =>
      useAnchorWatchAutoClose(
        'motoring', // Engines running
        30, // distanceMeters > radiusMeters + 4.572
        20, // radiusMeters
        true, // anchorWatchActive
        true, // isEnabled
      ),
    )

    expect(result.current.isAutoCloseArmed).toBe(true)
    expect(result.current.motoringSecondsElapsed).toBe(0)
  })

  it('should not arm when inside circle even with engines running', () => {
    const { result } = renderHook(() =>
      useAnchorWatchAutoClose(
        'motoring', // Engines running
        15, // distanceMeters < radiusMeters + 4.572
        20, // radiusMeters
        true, // anchorWatchActive
        true, // isEnabled
      ),
    )

    expect(result.current.isAutoCloseArmed).toBe(false)
  })

  it('should not arm when engines are off even if outside circle', () => {
    const { result } = renderHook(() =>
      useAnchorWatchAutoClose(
        'anchored', // Engines off
        30, // distanceMeters (outside circle)
        20, // radiusMeters
        true, // anchorWatchActive
        true, // isEnabled
      ),
    )

    expect(result.current.isAutoCloseArmed).toBe(false)
  })

  it('should count down elapsed seconds while armed', () => {
    const { result } = renderHook(() =>
      useAnchorWatchAutoClose(
        'motoring',
        30,
        20,
        true,
        true,
      ),
    )

    expect(result.current.isAutoCloseArmed).toBe(true)
    expect(result.current.motoringSecondsElapsed).toBe(0)

    // Advance time by 1 second
    act(() => {
      vi.advanceTimersByTime(1100)
    })

    expect(result.current.motoringSecondsElapsed).toBe(1)

    // Advance to 3 seconds
    act(() => {
      vi.advanceTimersByTime(2000)
    })

    expect(result.current.motoringSecondsElapsed).toBe(3)
  })

  it('should disarm when vessel re-enters circle', () => {
    const { result, rerender } = renderHook(
      ({
        navigationState,
        distanceMeters,
      }: {
        navigationState: string
        distanceMeters: number
      }) =>
        useAnchorWatchAutoClose(
          navigationState,
          distanceMeters,
          20,
          true,
          true,
        ),
      {
        initialProps: {
          navigationState: 'motoring',
          distanceMeters: 30, // Outside
        },
      },
    )

    expect(result.current.isAutoCloseArmed).toBe(true)

    // Re-enter circle
    act(() => {
      rerender({
        navigationState: 'motoring',
        distanceMeters: 15, // Inside (less than 20 + 4.572)
      })
    })

    expect(result.current.isAutoCloseArmed).toBe(false)
    expect(result.current.motoringSecondsElapsed).toBe(0)
  })

  it('should disarm when engines stop', () => {
    const { result, rerender } = renderHook(
      ({ navigationState }: { navigationState: string }) =>
        useAnchorWatchAutoClose(
          navigationState,
          30,
          20,
          true,
          true,
        ),
      {
        initialProps: {
          navigationState: 'motoring',
        },
      },
    )

    expect(result.current.isAutoCloseArmed).toBe(true)

    // Engines stop
    act(() => {
      rerender({ navigationState: 'anchored' })
    })

    expect(result.current.isAutoCloseArmed).toBe(false)
    expect(result.current.motoringSecondsElapsed).toBe(0)
  })

  it('should trigger auto-close API call after 5 seconds', async () => {
    const fetchSpy = vi.spyOn(global, 'fetch').mockResolvedValue({
      ok: true,
      statusText: 'OK',
    } as Response)

    const { unmount } = renderHook(() =>
      useAnchorWatchAutoClose(
        'motoring',
        30,
        20,
        true,
        true,
      ),
    )

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5100)
    })

    expect(fetchSpy).toHaveBeenCalledWith('/api/anchor-watch', { method: 'DELETE' })

    unmount()
    fetchSpy.mockRestore()
  })

  it('should dispatch custom event when auto-closing', async () => {
    const eventSpy = vi.spyOn(window, 'dispatchEvent')

    const fetchSpy = vi.spyOn(global, 'fetch').mockResolvedValue({
      ok: true,
      statusText: 'OK',
    } as Response)

    const { unmount } = renderHook(() =>
      useAnchorWatchAutoClose(
        'motoring',
        30,
        20,
        true,
        true,
      ),
    )

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5100)
    })

    const customEventCall = eventSpy.mock.calls.find((call) => {
      const event = call[0] as CustomEvent
      return event.type === 'anchor-watch-auto-closed'
    })
    expect(customEventCall).toBeDefined()

    unmount()
    fetchSpy.mockRestore()
    eventSpy.mockRestore()
  })

  it('should not trigger auto-close before 5 seconds', async () => {
    const fetchSpy = vi.spyOn(global, 'fetch')

    const { unmount } = renderHook(() =>
      useAnchorWatchAutoClose(
        'motoring',
        30,
        20,
        true,
        true,
      ),
    )

    await act(async () => {
      await vi.advanceTimersByTimeAsync(4900)
    })

    expect(fetchSpy).not.toHaveBeenCalled()

    unmount()
    fetchSpy.mockRestore()
  })

  it('should reset elapsed time when disarmed', () => {
    const { result, rerender } = renderHook(
      ({ navigationState }: { navigationState: string }) =>
        useAnchorWatchAutoClose(
          navigationState,
          30,
          20,
          true,
          true,
        ),
      {
        initialProps: {
          navigationState: 'motoring',
        },
      },
    )

    expect(result.current.isAutoCloseArmed).toBe(true)

    // Advance time
    act(() => {
      vi.advanceTimersByTime(2000)
    })

    expect(result.current.motoringSecondsElapsed).toBe(2)

    // Disarm
    act(() => {
      rerender({ navigationState: 'anchored' })
    })

    expect(result.current.isAutoCloseArmed).toBe(false)
    expect(result.current.motoringSecondsElapsed).toBe(0)
  })
})
