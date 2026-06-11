import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useAnchorAlarm } from '@/hooks/use-anchor-alarm'
import type { AnchorWatchState } from '@/hooks/use-anchor-watch'

describe('useAnchorAlarm', () => {
  let mockAudioContext: any
  let mockOscillator: any
  let mockGain: any
  let originalAudioContext: any

  beforeEach(() => {
    // Mock Web Audio API
    mockOscillator = {
      type: 'square',
      frequency: { setValueAtTime: vi.fn() },
      connect: vi.fn(function (this: any) {
        return this
      }),
      start: vi.fn(),
      stop: vi.fn(),
    }

    mockGain = {
      gain: { setValueAtTime: vi.fn() },
      connect: vi.fn(function (this: any) {
        return this
      }),
    }

    mockAudioContext = {
      state: 'running',
      currentTime: 0,
      createOscillator: vi.fn(() => ({ ...mockOscillator })),
      createGain: vi.fn(() => ({ ...mockGain })),
      destination: {},
      resume: vi.fn().mockResolvedValue(undefined),
    }

    // Store original and replace with mock - use vi.fn with implementation
    originalAudioContext = window.AudioContext
    ;(window as any).AudioContext = vi.fn(function () {
      return mockAudioContext
    })
  })

  afterEach(() => {
    // Restore original
    if (originalAudioContext) {
      window.AudioContext = originalAudioContext
    } else {
      delete (window as any).AudioContext
    }
    vi.clearAllMocks()
  })

  it('should not alarm when anchorState is "none"', () => {
    const { result } = renderHook(() => useAnchorAlarm('none'))

    expect(result.current.isAlarming).toBe(false)
    expect(result.current.isSilenced).toBe(false)
  })

  it('should not alarm when anchorState is "set"', () => {
    const { result } = renderHook(() => useAnchorAlarm('set'))

    expect(result.current.isAlarming).toBe(false)
    expect(result.current.isSilenced).toBe(false)
  })

  it('should start alarm when transitioning to "dragging"', () => {
    const { rerender, result } = renderHook(
      ({ state }: { state: AnchorWatchState }) => useAnchorAlarm(state),
      { initialProps: { state: 'set' as AnchorWatchState } },
    )

    act(() => {
      rerender({ state: 'dragging' as AnchorWatchState })
    })

    expect(result.current.isAlarming).toBe(true)
    expect(result.current.isSilenced).toBe(false)
  })

  it('should start alarm when transitioning to "critical"', () => {
    const { rerender, result } = renderHook(
      ({ state }: { state: AnchorWatchState }) => useAnchorAlarm(state),
      { initialProps: { state: 'set' as AnchorWatchState } },
    )

    act(() => {
      rerender({ state: 'critical' as AnchorWatchState })
    })

    expect(result.current.isAlarming).toBe(true)
    expect(result.current.isSilenced).toBe(false)
  })

  it('should silence alarm when silence() is called', () => {
    const { result } = renderHook(
      ({ state }: { state: AnchorWatchState }) => useAnchorAlarm(state),
      { initialProps: { state: 'dragging' as AnchorWatchState } },
    )

    expect(result.current.isAlarming).toBe(true)
    expect(result.current.isSilenced).toBe(false)

    act(() => {
      result.current.silence()
    })

    expect(result.current.isSilenced).toBe(true)
  })

  it('should reset silence flag when state exits alarm zone and re-enters', () => {
    const { rerender, result } = renderHook(
      ({ state }: { state: AnchorWatchState }) => useAnchorAlarm(state),
      { initialProps: { state: 'dragging' as AnchorWatchState } },
    )

    // Silence the alarm
    act(() => {
      result.current.silence()
    })
    expect(result.current.isSilenced).toBe(true)

    // Exit alarm zone
    act(() => {
      rerender({ state: 'set' as AnchorWatchState })
    })
    expect(result.current.isAlarming).toBe(false)

    // Re-enter alarm zone
    act(() => {
      rerender({ state: 'critical' as AnchorWatchState })
    })
    expect(result.current.isAlarming).toBe(true)
    expect(result.current.isSilenced).toBe(false) // Should reset
  })

  it('should stop alarm when transitioning out of alarm state', () => {
    const { rerender, result } = renderHook(
      ({ state }: { state: AnchorWatchState }) => useAnchorAlarm(state),
      { initialProps: { state: 'dragging' as AnchorWatchState } },
    )

    expect(result.current.isAlarming).toBe(true)

    act(() => {
      rerender({ state: 'set' as AnchorWatchState })
    })

    expect(result.current.isAlarming).toBe(false)
  })

  it('should handle state transitions between dragging and critical', () => {
    const { rerender, result } = renderHook(
      ({ state }: { state: AnchorWatchState }) => useAnchorAlarm(state),
      { initialProps: { state: 'dragging' as AnchorWatchState } },
    )

    expect(result.current.isAlarming).toBe(true)

    // Transition to critical (both are alarm states)
    act(() => {
      rerender({ state: 'critical' as AnchorWatchState })
    })

    expect(result.current.isAlarming).toBe(true)
    expect(result.current.isSilenced).toBe(false) // Silence should reset
  })
})
