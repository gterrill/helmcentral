import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { REPLAY_SPEEDS, useReplay } from '@/hooks/use-replay'

beforeEach(() => { vi.useFakeTimers({ toFake: ['requestAnimationFrame', 'cancelAnimationFrame', 'performance'] }) })
afterEach(() => { vi.useRealTimers() })

describe('useReplay', () => {
  it('offers a 60 second length beside the existing ones', () => {
    expect(REPLAY_SPEEDS.map((s) => [s.label, s.seconds])).toEqual([['60 s', 60], ['Slow', 30], ['Normal', 15], ['Fast', 8]])
  })

  it('plays the timeline in the chosen length', () => {
    const { result } = renderHook(() => useReplay(1000, 10, []))
    act(() => result.current.toggle())
    act(() => { vi.advanceTimersByTime(5016) })
    expect(result.current.clock).toBeGreaterThan(480)
    expect(result.current.clock).toBeLessThan(520)
  })

  it('a slowed stretch stretches the watching time, and the scrubber stays in timeline units', () => {
    const spans = [{ from: 500, pace: 0.5 }]
    const { result } = renderHook(() => useReplay(1000, 10, spans))
    act(() => result.current.toggle())
    // 5 s to reach 500, then 5 s more covers only 250 more.
    act(() => { vi.advanceTimersByTime(10_016) })
    expect(result.current.clock).toBeGreaterThan(730)
    expect(result.current.clock).toBeLessThan(770)
    expect(result.current.playing).toBe(true)
    act(() => { vi.advanceTimersByTime(10_000) })
    expect(result.current.clock).toBe(1000)
    expect(result.current.playing).toBe(false)
  })

  it('a seek sets the clock directly', () => {
    const { result } = renderHook(() => useReplay(1000, 10, [{ from: 100, pace: 0.25 }]))
    act(() => result.current.setClock(640))
    expect(result.current.clock).toBe(640)
  })
})
