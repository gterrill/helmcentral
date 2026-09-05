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
    silenced: false,
    // A rule-driven alarm has no silence action; the engine only acknowledges.
    can_silence: false,
    can_acknowledge: true,
  }
  const acknowledged: ActiveAlarm = { ...dragging, phase: 'acknowledged', silenced: true, can_acknowledge: false }

  it('does not alarm when the server reports no drag', () => {
    const { result } = renderHook(() => useAnchorAlarm(null))

    expect(result.current.isAlarming).toBe(false)
    expect(result.current.isSilenced).toBe(false)
    expect(result.current.isActive).toBe(false)
  })

  it('alarms when the server raises a drag', () => {
    const { result } = renderHook(() => useAnchorAlarm(dragging))

    expect(result.current.isAlarming).toBe(true)
    expect(result.current.isSilenced).toBe(false)
    expect(result.current.isActive).toBe(true)
  })

  it('reports silenced once the server has acknowledged it', () => {
    const { result } = renderHook(() => useAnchorAlarm(acknowledged))

    expect(result.current.isAlarming).toBe(false)
    expect(result.current.isSilenced).toBe(true)
    // A silenced drag is still a live condition, not the absence of one —
    // callers must be able to tell "silenced" apart from "no alarm at all"
    // without inspecting the raw alarm object themselves.
    expect(result.current.isActive).toBe(true)
  })

  // P0 regression: a silenced drag used to be visually indistinguishable
  // from no alarm at all, because callers had only isAlarming/isSilenced to
  // go on and isSilenced was easy to read as "nothing to show". isActive is
  // the explicit third state.
  it('distinguishes a silenced-but-active drag from no alarm at all via isActive', () => {
    const { result: noAlarm } = renderHook(() => useAnchorAlarm(null))
    const { result: silencedAlarm } = renderHook(() => useAnchorAlarm(acknowledged))

    expect(noAlarm.current.isActive).toBe(false)
    expect(silencedAlarm.current.isActive).toBe(true)
    expect(silencedAlarm.current.isAlarming).toBe(false)
    expect(silencedAlarm.current.isSilenced).toBe(true)
  })

  describe('unsilence', () => {
    it('resumes the local klaxon for an acknowledged alarm without calling the server', async () => {
      const acknowledge = vi.fn().mockResolvedValue(undefined)
      const { result } = renderHook(() => useAnchorAlarm(acknowledged, acknowledge))

      expect(result.current.isAlarming).toBe(false)
      const callsBefore = mockAudioContext.createOscillator.mock.calls.length

      await act(async () => {
        result.current.unsilence()
      })

      expect(result.current.isAlarming).toBe(true)
      expect(result.current.isSilenced).toBe(false)
      expect(mockAudioContext.createOscillator.mock.calls.length).toBeGreaterThan(callsBefore)
      // There is no server-side "un-acknowledge" — the engine treats
      // acknowledged as terminal until the condition clears and re-raises.
      // This is a same-device override of the local audio only.
      expect(acknowledge).not.toHaveBeenCalled()
    })

    it('silencing again after unsilence stops the klaxon without re-calling acknowledge', async () => {
      const acknowledge = vi.fn().mockResolvedValue(undefined)
      const { result } = renderHook(() => useAnchorAlarm(acknowledged, acknowledge))

      await act(async () => {
        result.current.unsilence()
      })
      expect(result.current.isAlarming).toBe(true)

      act(() => {
        result.current.silence()
      })

      expect(result.current.isAlarming).toBe(false)
      expect(result.current.isSilenced).toBe(true)
      expect(acknowledge).not.toHaveBeenCalled()
    })

    it('drops the local override once the drag clears, so a later drag does not start pre-unsilenced', async () => {
      const acknowledge = vi.fn().mockResolvedValue(undefined)
      const { result, rerender } = renderHook(
        ({ alarm }: { alarm: ActiveAlarm | null }) => useAnchorAlarm(alarm, acknowledge),
        { initialProps: { alarm: acknowledged as ActiveAlarm | null } },
      )

      await act(async () => {
        result.current.unsilence()
      })
      expect(result.current.isAlarming).toBe(true)

      act(() => {
        rerender({ alarm: null })
      })
      expect(result.current.isActive).toBe(false)
      expect(result.current.isAlarming).toBe(false)

      await act(async () => {
        rerender({ alarm: acknowledged })
      })
      expect(result.current.isSilenced).toBe(true)
      expect(result.current.isAlarming).toBe(false)
    })
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
