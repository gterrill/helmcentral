import { useCallback, useEffect, useMemo, useReducer, useState } from 'react'

import { ReplayPlayer } from '@/lib/replay-controller'
import type { PaceSpan } from '@/lib/replay-clock'
import { buildReplayTime, type ReplayMoment } from '@/lib/replay-time'

/** Replay lengths: how long the whole timeline takes to watch at full pace, in seconds. */
export const REPLAY_SPEEDS = [
  { id: '60s', label: '60 s', seconds: 60 },
  { id: '30s', label: '30 s', seconds: 30 },
  { id: '15s', label: '15 s', seconds: 15 },
  { id: '8s', label: '8 s', seconds: 8 },
] as const
export type ReplaySpeedId = (typeof REPLAY_SPEEDS)[number]['id']

export const replaySeconds = (speed: ReplaySpeedId) => REPLAY_SPEEDS.find((s) => s.id === speed)?.seconds ?? 15

/** Advances a replay of `total` timeline seconds, sailed in `lengthS` seconds
 * of watching, slowed through any `spans`, and held for each photo moment in
 * `moments` (each adds its hold on top of the length). `spans` and `moments`
 * should keep their identity while they are unchanged, since a new list builds
 * the timing again. `clock` is 0..total and stays in timeline units whatever the
 * pace. The place and whether it is playing belong to a ReplayPlayer; this hook
 * feeds it requestAnimationFrame times and mirrors it into state. */
export function useReplay(total: number, lengthS: number, spans: readonly PaceSpan[], moments: readonly ReplayMoment[]) {
  const time = useMemo(() => buildReplayTime({ total, lengthS, spans, moments }), [total, lengthS, spans, moments])
  const [player] = useState(() => new ReplayPlayer(time))
  // Re-render when the player has moved.
  const [, bump] = useReducer((n: number) => n + 1, 0)
  // New timing (a shot's pace edited mid-play, a sign gaining a moment) keeps the
  // place, and is applied now, in the render that has it, so that no render
  // pairs the new timing with a place that belongs to the old. (A different
  // timeline starts again from the beginning.)
  if (player.timing !== time) player.setTime(time, performance.now())

  const setClock = useCallback((value: number, opts?: { onSign?: boolean }) => {
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
