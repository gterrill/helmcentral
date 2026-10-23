// Signs on the replay (ADR 0204). A sign is a note pinned to a real instant;
// these functions say where each one stands on the track and which ones are on
// screen at a given moment of watching. Everything here is pure.

import { positionAt, type Timeline } from '@/lib/passages'

export type SignVisibility = 'always' | 'passing' | 'after'

export const SIGN_VISIBILITIES: readonly { id: SignVisibility; label: string }[] = [
  { id: 'always', label: 'Always' },
  { id: 'passing', label: 'While passing' },
  { id: 'after', label: 'After passing' },
]

export const SIGN_TITLE_MAX = 80
export const SIGN_NOTE_MAX = 280
export const SIGN_SHOW_MIN = 1
export const SIGN_SHOW_MAX = 30
export const SIGN_SHOW_DEFAULT = 4

/** One sign as the server holds it. The photo fields are null when there is no
 * photo or its document has been deleted. */
export interface ReplaySign {
  id: string
  t_ms: number
  title: string
  note: string
  document_id: string | null
  photo_url: string | null
  visibility: SignVisibility
  show_s: number
}

/** A sign placed on the replay. */
export interface ResolvedSign {
  sign: ReplaySign
  /** Timeline seconds where the sign is. */
  clock: number
  /** Unix seconds of the sign. */
  at: number
  entryId: string
  lat: number
  lon: number
}

/** How long before the boat arrives a passing sign appears, in seconds of watching. */
export const SIGN_LEAD_S = 0.5
/** The most cards on screen at once. */
export const MAX_SIGN_CARDS = 2

/** The signs whose time falls inside a replayed passage, oldest first, each at
 * the track position for its time. A sign in a trimmed-out stretch, in the gap
 * between passages, or where the track has no position is not placed; it is
 * left as it is, not deleted. */
export function resolveSigns(signs: readonly ReplaySign[], timeline: Timeline): ResolvedSign[] {
  const out: ResolvedSign[] = []
  for (const sign of signs) {
    const at = sign.t_ms / 1000
    const entry = timeline.entries.find((e) => at >= e.start && at <= e.end)
    if (!entry) continue
    const pos = positionAt(entry.segments, at)
    if (!pos) continue
    out.push({ sign, at, entryId: entry.id, clock: entry.offset + (at - entry.start), lat: pos.lat, lon: pos.lon })
  }
  return out.sort((a, b) => a.sign.t_ms - b.sign.t_ms || a.sign.id.localeCompare(b.sign.id))
}

/** When a sign is up, in seconds of watching: from (inclusive) to (exclusive). */
function window(r: ResolvedSign, reachedAt: number): { from: number; to: number } {
  switch (r.sign.visibility) {
    case 'always': return { from: -Infinity, to: Infinity }
    case 'after': return { from: reachedAt, to: Infinity }
    case 'passing': return { from: reachedAt - SIGN_LEAD_S, to: reachedAt + r.sign.show_s }
  }
}

/**
 * The cards on screen after `wall` seconds of watching, in the order they
 * stand: always signs first, then the rest oldest to newest. At most
 * MAX_SIGN_CARDS.
 *
 * Signs take their places in the order they appear. When a third would be up,
 * the one that appeared earliest gives way for good: it does not come back when
 * a later one expires. Always signs are never the one that gives way: with two
 * of them nothing else shows, and with more than two the earliest in time show.
 * The answer is worked out from the signs alone, replaying the appearances up
 * to `wall`, so a seek lands on exactly what continuous play would show.
 * Watching time is wall-clock: `wallAt` (the replay clock's mapping) is what
 * makes a slowed stretch last as long as it feels.
 */
export function visibleSigns(resolved: readonly ResolvedSign[], wall: number, wallAt: (clock: number) => number): ResolvedSign[] {
  interface Entry { r: ResolvedSign; from: number; to: number }
  const entries: Entry[] = []
  for (const r of resolved) {
    const w = window(r, wallAt(r.clock))
    if (w.from <= wall) entries.push({ r, ...w })
  }
  entries.sort((a, b) => a.from - b.from || a.r.sign.t_ms - b.r.sign.t_ms || a.r.sign.id.localeCompare(b.r.sign.id))
  let up: Entry[] = []
  for (const e of entries) {
    up = up.filter((u) => u.to > e.from)
    up.push(e)
    if (up.length > MAX_SIGN_CARDS) {
      const give = up.find((u) => u.r.sign.visibility !== 'always') ?? up[up.length - 1]
      up = up.filter((u) => u !== give)
    }
  }
  const live = up.filter((u) => u.to > wall)
  const always = live.filter((u) => u.r.sign.visibility === 'always')
  return [...always, ...live.filter((u) => u.r.sign.visibility !== 'always')].map((u) => u.r)
}
