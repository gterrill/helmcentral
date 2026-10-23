import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'

import { timelineAtWall, wallAtTimeline, type PaceSpan } from '@/lib/replay-clock'

/** Replay lengths: how long the whole timeline takes to watch at full pace, in seconds. */
export const REPLAY_SPEEDS = [
  { id: 'long', label: '60 s', seconds: 60 },
  { id: 'slow', label: 'Slow', seconds: 30 },
  { id: 'normal', label: 'Normal', seconds: 15 },
  { id: 'fast', label: 'Fast', seconds: 8 },
] as const
export type ReplaySpeedId = (typeof REPLAY_SPEEDS)[number]['id']

export const replaySeconds = (speed: ReplaySpeedId) => REPLAY_SPEEDS.find((s) => s.id === speed)?.seconds ?? 15

/** Advances a replay clock of `total` timeline seconds, played in `lengthS`
 * seconds of watching, slowed through any `spans`. `clock` is 0..total and
 * stays in timeline units whatever the pace. */
export function useReplay(total: number, lengthS: number, spans: readonly PaceSpan[]) {
  const [clock, setClockState] = useState(0)
  const [playing, setPlaying] = useState(false)
  const clockRef = useRef(0)
  const spansRef = useRef(spans)
  useLayoutEffect(() => { spansRef.current = spans })

  const setClock = useCallback((value: number) => {
    const next = Math.max(0, Math.min(total, value))
    clockRef.current = next
    setClockState(next)
  }, [total])

  // A new timeline starts again from the beginning.
  useEffect(() => {
    clockRef.current = 0
    setClockState(0)
    setPlaying(false)
  }, [total])

  useEffect(() => {
    if (!playing || total <= 0) return
    let last = performance.now()
    let frame = requestAnimationFrame(function tick(now) {
      const dt = (now - last) / 1000
      last = now
      // The wall position is worked out from the clock each frame, so a shot
      // edited mid-play changes the pace from here on without a jump.
      const wall = wallAtTimeline(total, lengthS, spansRef.current, clockRef.current) + dt
      const next = Math.min(total, timelineAtWall(total, lengthS, spansRef.current, wall))
      clockRef.current = next
      setClockState(next)
      if (next >= total) {
        setPlaying(false)
        return
      }
      frame = requestAnimationFrame(tick)
    })
    return () => cancelAnimationFrame(frame)
  }, [playing, lengthS, total])

  const toggle = useCallback(() => {
    if (!playing && clockRef.current >= total) setClock(0)
    setPlaying((p) => !p)
  }, [playing, total, setClock])

  return { clock, setClock, playing, setPlaying, toggle }
}
