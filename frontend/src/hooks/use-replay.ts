import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'

import { ReplayPlayer } from '@/lib/replay-controller'
import type { PaceSpan } from '@/lib/replay-clock'

/** Replay lengths: how long the whole timeline takes to watch at full pace, in seconds. */
export const REPLAY_SPEEDS = [
  { id: '60s', label: '60 s', seconds: 60 },
  { id: '30s', label: '30 s', seconds: 30 },
  { id: '15s', label: '15 s', seconds: 15 },
  { id: '8s', label: '8 s', seconds: 8 },
] as const
export type ReplaySpeedId = (typeof REPLAY_SPEEDS)[number]['id']

export const replaySeconds = (speed: ReplaySpeedId) => REPLAY_SPEEDS.find((s) => s.id === speed)?.seconds ?? 15

/** Advances a replay clock of `total` timeline seconds, played in `lengthS`
 * seconds of watching, slowed through any `spans`. `clock` is 0..total and
 * stays in timeline units whatever the pace. The clock and whether it is
 * playing belong to a ReplayPlayer; this hook feeds it requestAnimationFrame
 * times and mirrors it into state. */
export function useReplay(total: number, lengthS: number, spans: readonly PaceSpan[]) {
  const [clock, setClockState] = useState(0)
  const [playing, setPlayingState] = useState(false)
  const spansRef = useRef(spans)
  useLayoutEffect(() => { spansRef.current = spans })
  // The wall position is worked out from the clock each frame, so a shot
  // edited mid-play changes the pace from here on without a jump.
  const [player] = useState(() => new ReplayPlayer(total, lengthS, () => spansRef.current))
  const mirror = useCallback(() => {
    setClockState(player.clock)
    setPlayingState(player.playing)
  }, [player])

  const setClock = useCallback((value: number) => {
    player.seek(value)
    setClockState(player.clock)
  }, [player])

  const setPlaying = useCallback((value: boolean) => {
    if (value) player.play(performance.now())
    else player.pause()
    setPlayingState(player.playing)
  }, [player])

  // A new timeline starts again from the beginning; a new length carries on.
  useEffect(() => {
    player.rewind()
    mirror()
  }, [total, player, mirror])
  useEffect(() => {
    player.configure(total, lengthS, performance.now())
  }, [total, lengthS, player])

  useEffect(() => {
    if (!playing || total <= 0) return
    let frame = requestAnimationFrame(function tick(now) {
      player.tick(now)
      mirror()
      if (player.playing) frame = requestAnimationFrame(tick)
    })
    return () => cancelAnimationFrame(frame)
  }, [playing, total, player, mirror])

  const toggle = useCallback(() => {
    if (!player.playing && player.clock >= total) setClock(0)
    setPlaying(!player.playing)
  }, [player, total, setClock, setPlaying])

  return { clock, setClock, playing, setPlaying, toggle }
}
