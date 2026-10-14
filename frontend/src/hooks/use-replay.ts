import { useCallback, useEffect, useRef, useState } from 'react'

/** Replay speeds: how long the whole timeline takes to play, in seconds. */
export const REPLAY_SPEEDS = [
  { id: 'slow', label: 'Slow', seconds: 120 },
  { id: 'normal', label: 'Normal', seconds: 60 },
  { id: 'fast', label: 'Fast', seconds: 30 },
] as const
export type ReplaySpeedId = (typeof REPLAY_SPEEDS)[number]['id']

/** Advances a replay clock of `total` seconds. `clock` is 0..total. */
export function useReplay(total: number, speed: ReplaySpeedId) {
  const [clock, setClockState] = useState(0)
  const [playing, setPlaying] = useState(false)
  const clockRef = useRef(0)

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
    const seconds = REPLAY_SPEEDS.find((s) => s.id === speed)?.seconds ?? 60
    let last = performance.now()
    let frame = requestAnimationFrame(function tick(now) {
      const dt = (now - last) / 1000
      last = now
      const next = Math.min(total, clockRef.current + dt * (total / seconds))
      clockRef.current = next
      setClockState(next)
      if (next >= total) {
        setPlaying(false)
        return
      }
      frame = requestAnimationFrame(tick)
    })
    return () => cancelAnimationFrame(frame)
  }, [playing, speed, total])

  const toggle = useCallback(() => {
    if (!playing && clockRef.current >= total) setClock(0)
    setPlaying((p) => !p)
  }, [playing, total, setClock])

  return { clock, setClock, playing, setPlaying, toggle }
}
