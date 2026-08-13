import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useAnchorAlarm } from '@/hooks/use-anchor-alarm'
import type { ActiveAlarm } from '@/hooks/use-alarms'

describe('useAnchorAlarm', () => {
  interface MockOscillator {
    type: string
    frequency: { setValueAtTime: ReturnType<typeof vi.fn> }
    connect: ReturnType<typeof vi.fn>
    start: ReturnType<typeof vi.fn>
    stop: ReturnType<typeof vi.fn>
  }

  interface MockGain {
    gain: { setValueAtTime: ReturnType<typeof vi.fn> }
    connect: ReturnType<typeof vi.fn>
  }

  interface MockAudioContext {
    state: string
    currentTime: number
    createOscillator: ReturnType<typeof vi.fn>
    createGain: ReturnType<typeof vi.fn>
    destination: object
    resume: ReturnType<typeof vi.fn>
  }

  let mockAudioContext: MockAudioContext
  let mockOscillator: MockOscillator
  let mockGain: MockGain
  let originalAudioContext: typeof AudioContext | undefined

  beforeEach(() => {
    // Mock Web Audio API
    mockOscillator = {
      type: 'square',
      frequency: { setValueAtTime: vi.fn() },
      connect: vi.fn(function (this: MockOscillator) {
        return this
      }),
      start: vi.fn(),
      stop: vi.fn(),
    }

    mockGain = {
      gain: { setValueAtTime: vi.fn() },
      connect: vi.fn(function (this: MockGain) {
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
    ;(window as unknown as { AudioContext: typeof AudioContext }).AudioContext = vi.fn(function () {
      return mockAudioContext
    }) as unknown as typeof AudioContext
  })

  afterEach(() => {
    // Restore original
    if (originalAudioContext) {
      window.AudioContext = originalAudioContext
    } else {
      delete (window as unknown as { AudioContext?: typeof AudioContext }).AudioContext
    }
    vi.clearAllMocks()
  })

  const dragging: ActiveAlarm = {
    rule_id: 'helmcentral:anchor-drag',
    label: 'Anchor dragging',
    path: 'notifications.navigation.anchor',
    phase: 'active',
    state: 'alarm',
    value: 62,
    message: 'Anchor dragging: 62m from where it was set',
  }
  const acknowledged: ActiveAlarm = { ...dragging, phase: 'acknowledged' }

  it('does not alarm when the server reports no drag', () => {
    const { result } = renderHook(() => useAnchorAlarm(null))

    expect(result.current.isAlarming).toBe(false)
    expect(result.current.isSilenced).toBe(false)
  })

  it('alarms when the server raises a drag', () => {
    const { result } = renderHook(() => useAnchorAlarm(dragging))

    expect(result.current.isAlarming).toBe(true)
    expect(result.current.isSilenced).toBe(false)
  })

  it('reports silenced once the server has acknowledged it', () => {
    const { result } = renderHook(() => useAnchorAlarm(acknowledged))

    expect(result.current.isAlarming).toBe(false)
    expect(result.current.isSilenced).toBe(true)
  })

  it('silences by acknowledging server-side so every screen agrees', async () => {
    const acknowledge = vi.fn().mockResolvedValue(undefined)
    const { result } = renderHook(() => useAnchorAlarm(dragging, acknowledge))

    await act(async () => {
      result.current.silence()
    })

    expect(acknowledge).toHaveBeenCalledWith('helmcentral:anchor-drag')
  })

  // Regression: the klaxon loop used to be created only on the transition into
  // 'dragging', so after un-silencing it was never recreated and the alarm
  // stayed quiet for the rest of the drag.
  it('sounds again when an acknowledged alarm becomes active once more', async () => {
    const { rerender, result } = renderHook(
      ({ alarm }: { alarm: ActiveAlarm | null }) => useAnchorAlarm(alarm),
      { initialProps: { alarm: acknowledged } },
    )

    expect(result.current.isAlarming).toBe(false)
    const callsWhileSilenced = mockAudioContext.createOscillator.mock.calls.length

    // playKlaxon awaits ensureAudioContext, so the oscillators are created on a
    // microtask rather than synchronously with the rerender.
    await act(async () => {
      rerender({ alarm: dragging })
    })

    expect(result.current.isAlarming).toBe(true)
    expect(mockAudioContext.createOscillator.mock.calls.length).toBeGreaterThan(callsWhileSilenced)
  })

  it('stops sounding when the drag clears', () => {
    const { rerender, result } = renderHook(
      ({ alarm }: { alarm: ActiveAlarm | null }) => useAnchorAlarm(alarm),
      { initialProps: { alarm: dragging as ActiveAlarm | null } },
    )

    expect(result.current.isAlarming).toBe(true)

    act(() => {
      rerender({ alarm: null })
    })

    expect(result.current.isAlarming).toBe(false)
  })
})
