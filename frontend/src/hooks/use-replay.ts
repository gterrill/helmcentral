import { useCallback, useEffect, useMemo, useReducer, useState } from 'react'

import { ReplayPlayer } from '@/lib/replay-controller'
import type { PaceSpan } from '@/lib/replay-clock'
import { buildReplayTime, type ReplayHoldInput } from '@/lib/replay-time'

const NO_STAYS: readonly ReplayHoldInput[] = []

/** Advances a replay of `total` timeline seconds, sailed in `lengthS` seconds
 * of watching, slowed through any `spans`, and held for each photo moment in
 * `moments` (each adds its hold on top of the length). `spans` and `moments`
 * should keep their identity while they are unchanged, since a new list builds
 * the timing again. `clock` is 0..total and stays in timeline units whatever the
 * pace. The place and whether it is playing belong to a ReplayPlayer; this hook
 * feeds it requestAnimationFrame times and mirrors it into state. */
export function useReplay(total: number, lengthS: number, spans: readonly PaceSpan[], moments: readonly ReplayHoldInput[], stays: readonly ReplayHoldInput[] = NO_STAYS) {
  const time = useMemo(() => buildReplayTime({ total, lengthS, spans, moments, stays }), [total, lengthS, spans, moments, stays])
  const [player] = useState(() => new ReplayPlayer(time))
  // Re-render when the player has moved.
  const [, bump] = useReducer((n: number) => n + 1, 0)
  // New timing (a shot's pace edited mid-play, a sign gaining a moment) keeps the
  // place, and is applied now, in the render that has it, so that no render
  // pairs the new timing with a place that belongs to the old. (A different
  // timeline starts again from the beginning.)
  if (player.timing !== time) player.setTime(time, performance.now())

  const setClock = useCallback((value: number, opts?: { onSign?: boolean; at?: number }) => {
    player.seek(value, opts)
    bump()
  }, [player])

  const setPlaying = useCallback((value: boolean) => {
    if (value) player.play(performance.now())
    else player.pause()
    bump()
  }, [player])

  const playing = player.playing
  useEffect(() => {
    if (!playing || total <= 0) return
    let frame = requestAnimationFrame(function tick(now) {
      player.tick(now)
      bump()
      if (player.playing) frame = requestAnimationFrame(tick)
    })
    return () => cancelAnimationFrame(frame)
  }, [playing, total, player])

  const toggle = useCallback(() => {
    if (!player.playing && player.ended) player.seek(0)
    setPlaying(!player.playing)
  }, [player, setPlaying])

  /** Ends the photo moment the replay is in, and carries on. Nothing outside a moment. */
  const skipMoment = useCallback(() => {
    player.skipMoment()
    bump()
  }, [player])

  return { clock: player.clock, wall: player.wall, time, ended: player.ended, setClock, playing, setPlaying, toggle, skipMoment }
}
