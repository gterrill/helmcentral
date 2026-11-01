import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { REPLAY_SPEEDS, useReplay } from '@/hooks/use-replay'
import type { ReplayTime } from '@/lib/replay-time'

const NONE: never[] = []
const SLOW = [{ from: 100, pace: 0.25 }]

beforeEach(() => { vi.useFakeTimers({ toFake: ['requestAnimationFrame', 'cancelAnimationFrame', 'performance'] }) })
afterEach(() => { vi.useRealTimers() })

// The photo moment the replay is in.
const momentOf = (r: { time: ReplayTime; wall: number }) => r.time.at(r.wall).moment

describe('useReplay', () => {
  it('offers a 60 second length beside the existing ones', () => {
    expect(REPLAY_SPEEDS.map((s) => [s.label, s.seconds])).toEqual([['60 s', 60], ['30 s', 30], ['15 s', 15], ['8 s', 8]])
  })

  it('plays the timeline in the chosen length', () => {
    const { result } = renderHook(() => useReplay(1000, 10, NONE, NONE))
    act(() => result.current.toggle())
    act(() => { vi.advanceTimersByTime(5016) })
    expect(result.current.clock).toBeGreaterThan(480)
    expect(result.current.clock).toBeLessThan(520)
  })

  it('a slowed stretch stretches the watching time, and the scrubber stays in timeline units', () => {
    const spans = [{ from: 500, pace: 0.5 }]
    const { result } = renderHook(() => useReplay(1000, 10, spans, NONE))
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
    const { result } = renderHook(() => useReplay(1000, 10, SLOW, NONE))
    act(() => result.current.setClock(640))
    expect(result.current.clock).toBe(640)
  })

  describe('with photo moments', () => {
    const moments = [{ clock: 500, id: 'a' }]

    it('holds the clock at the sign for the moment, and the length is the sailing', () => {
      const { result } = renderHook(() => useReplay(1000, 10, NONE, moments))
      act(() => result.current.toggle())
      act(() => { vi.advanceTimersByTime(5016) })
      expect(result.current.clock).toBeCloseTo(500, -1)
      expect(momentOf(result.current)?.id).toBe('a')
      act(() => { vi.advanceTimersByTime(1000) })
      expect(result.current.clock).toBeCloseTo(500, -1)
      expect(momentOf(result.current)).not.toBeNull()
      act(() => { vi.advanceTimersByTime(1500) })
      expect(momentOf(result.current)).toBeNull()
      expect(result.current.clock).toBeGreaterThan(500)
      expect(result.current.playing).toBe(true)
      // 10 s of sailing and 2.3 s of hold.
      act(() => { vi.advanceTimersByTime(8000) })
      expect(result.current.playing).toBe(false)
      expect(result.current.ended).toBe(true)
    })

    it('goes on from the sign when the moment is skipped', () => {
      const { result } = renderHook(() => useReplay(1000, 10, NONE, moments))
      act(() => result.current.toggle())
      act(() => { vi.advanceTimersByTime(5516) })
      expect(momentOf(result.current)).not.toBeNull()
      act(() => result.current.skipMoment())
      expect(momentOf(result.current)).toBeNull()
      expect(result.current.playing).toBe(true)
      expect(result.current.clock).toBeCloseTo(500, -1)
    })

    it('puts the slider over the sailing: a seek lands clear of the hold, or on the sign', () => {
      const { result } = renderHook(() => useReplay(1000, 10, NONE, moments))
      act(() => result.current.setClock(700))
      expect(result.current.clock).toBeCloseTo(700)
      expect(momentOf(result.current)).toBeNull()
      act(() => result.current.setClock(500, { onSign: true }))
      expect(momentOf(result.current)?.id).toBe('a')
    })

    it('keeps its place when a sign gains a moment while it plays', () => {
      const { result, rerender } = renderHook(({ m }) => useReplay(1000, 10, NONE, m), { initialProps: { m: NONE as typeof moments } })
      act(() => result.current.setClock(800))
      rerender({ m: moments })
      expect(result.current.clock).toBeCloseTo(800)
    })
  })

  describe('when the timing changes', () => {
    it('never shows a place that does not belong to the timing of the same render', () => {
      const seen: { clock: number; wall: number; time: ReplayTime }[] = []
      const { result, rerender } = renderHook(({ spans }) => {
        const r = useReplay(1000, 10, spans, NONE)
        seen.push({ clock: r.clock, wall: r.wall, time: r.time })
        return r
      }, { initialProps: { spans: NONE as typeof SLOW } })
      act(() => result.current.setClock(500))
      rerender({ spans: SLOW })
      rerender({ spans: NONE })
      expect(seen.length).toBeGreaterThan(3)
      for (const { clock, wall, time } of seen) expect(time.at(wall).clock).toBeCloseTo(clock, 6)
      // And the place itself was kept.
      expect(result.current.clock).toBeCloseTo(500)
    })

    it('starts again from the beginning for a different timeline', () => {
      const { result, rerender } = renderHook(({ total }) => useReplay(total, 10, NONE, NONE), { initialProps: { total: 1000 } })
      act(() => result.current.setClock(500))
      rerender({ total: 2000 })
      expect(result.current.clock).toBe(0)
      expect(result.current.wall).toBe(0)
      expect(result.current.playing).toBe(false)
    })

    it('seeks onto a sign only when asked to', () => {
      const moments = [{ clock: 500, id: 'a' }]
      const { result } = renderHook(() => useReplay(1000, 10, NONE, moments))
      act(() => result.current.setClock(500))
      expect(momentOf(result.current)).toBeNull()
      act(() => result.current.setClock(500, { onSign: true }))
      expect(momentOf(result.current)?.id).toBe('a')
    })

    it('counts a seek to the end as ended, with a sign standing there', () => {
      const moments = [{ clock: 1000, id: 'a' }]
      const { result } = renderHook(() => useReplay(1000, 10, NONE, moments))
      act(() => result.current.setClock(1000))
      expect(result.current.ended).toBe(true)
    })
  })
})
