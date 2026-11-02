// The replay's clock. The scrubber and the boat run on timeline seconds (the
// passages end to end); the operator watches wall-clock seconds. A camera
// shot with a pace slows the timeline down for as long as it lasts, so the two
// are related by a piecewise rate. Everything here is pure.

import { isMultiPassage, locate, type Timeline } from '@/lib/passages'
import { lowerBound } from '@/lib/sorted-search'

export type CameraMode = 'overview' | 'follow' | 'orbit'
export const CAMERA_PACES = [1, 0.5, 0.25] as const

/** One saved camera shot, as the server holds it. It belongs to a real
 * instant, not to a passage. */
export interface CameraShot {
  id: string
  t_ms: number
  mode: CameraMode
  zoom: number
  pitch: number
  bearing_offset_deg: number
  orbit_deg_per_s: number
  pace: number
}

/** A shot placed on the replay: its clock position and real time. */
export interface ResolvedShot {
  shot: CameraShot
  /** Timeline seconds where the shot starts. */
  clock: number
  /** Unix seconds of the shot. */
  at: number
  /** The passage the shot's time falls in: the boat at the shot is read there,
   * whatever the clock says at a boundary. */
  entryId: string
}

/** The shots whose time falls inside a replayed passage, oldest first. A shot
 * in a trimmed-out stretch or in the gap between passages is not played. */
export function resolveShots(shots: readonly CameraShot[], timeline: Timeline): ResolvedShot[] {
  const out: ResolvedShot[] = []
  for (const shot of shots) {
    const at = shot.t_ms / 1000
    const entry = timeline.entries.find((e) => at >= e.start && at <= e.end)
    if (!entry) continue
    out.push({ shot, at, entryId: entry.id, clock: entry.offset + (at - entry.start) })
  }
  return out.sort((a, b) => a.clock - b.clock || a.shot.t_ms - b.shot.t_ms)
}

export interface PaceSpan {
  /** Timeline seconds where this pace starts. */
  from: number
  pace: number
}

export function paceSpans(resolved: readonly ResolvedShot[]): PaceSpan[] {
  return resolved.map((r) => ({ from: r.clock, pace: r.shot.pace }))
}

interface Piece { c0: number; c1: number; w0: number; w1: number }

function buildPieces(total: number, lengthS: number, spans: readonly PaceSpan[]): Piece[] {
  if (!(total > 0) || !(lengthS > 0)) return []
  const rate = total / lengthS // timeline seconds per wall second at pace 1
  const sorted = spans.filter((s) => s.from < total && s.pace > 0).map((s) => ({ from: Math.max(0, s.from), pace: s.pace })).sort((a, b) => a.from - b.from)
  const out: Piece[] = []
  let c = 0
  let w = 0
  let pace = 1
  const push = (to: number) => {
    if (to <= c) return
    const wEnd = w + (to - c) / (rate * pace)
    out.push({ c0: c, c1: to, w0: w, w1: wEnd })
    c = to
    w = wEnd
  }
  for (const s of sorted) {
    push(s.from)
    pace = s.pace
  }
  push(total)
  return out
}

// The table is built once for a given (total, length, spans) and looked up by
// binary search: the replay asks for it every frame. Keyed on the spans array
// itself, so a changed set of shots builds a new one.
const tables = new WeakMap<readonly PaceSpan[], Map<string, Piece[]>>()

function pieces(total: number, lengthS: number, spans: readonly PaceSpan[]): Piece[] {
  let byKey = tables.get(spans)
  if (!byKey) {
    byKey = new Map()
    tables.set(spans, byKey)
  }
  const key = `${total}|${lengthS}`
  let table = byKey.get(key)
  if (!table) {
    table = buildPieces(total, lengthS, spans)
    byKey.set(key, table)
  }
  return table
}

/** The first piece whose `end` is at or past `value`, or the last. */
function find(p: Piece[], value: number, end: 'c1' | 'w1'): Piece {
  let lo = 0
  let hi = p.length - 1
  while (lo < hi) {
    const mid = (lo + hi) >> 1
    if (p[mid][end] >= value) hi = mid
    else lo = mid + 1
  }
  return p[lo]
}

/** How long the whole replay takes to watch, in wall-clock seconds. */
export function replayWallDuration(total: number, lengthS: number, spans: readonly PaceSpan[]): number {
  const p = pieces(total, lengthS, spans)
  return p.length ? p[p.length - 1].w1 : 0
}

/** The timeline position after `wall` seconds of watching, clamped to the replay. */
export function timelineAtWall(total: number, lengthS: number, spans: readonly PaceSpan[], wall: number): number {
  const p = pieces(total, lengthS, spans)
  if (p.length === 0 || wall <= 0) return 0
  if (wall >= p[p.length - 1].w1) return total
  const piece = find(p, wall, 'w1')
  return piece.c0 + ((wall - piece.w0) / (piece.w1 - piece.w0)) * (piece.c1 - piece.c0)
}

/** Seconds of watching it takes to reach a timeline position. */
export function wallAtTimeline(total: number, lengthS: number, spans: readonly PaceSpan[], clock: number): number {
  const p = pieces(total, lengthS, spans)
  if (p.length === 0 || clock <= 0) return 0
  if (clock >= total) return p[p.length - 1].w1
  const piece = find(p, clock, 'c1')
  return piece.w0 + ((clock - piece.c0) / (piece.c1 - piece.c0)) * (piece.w1 - piece.w0)
}

// ── sharing the length between passages ──────────────────────────────────

/** The least a passage gets of the replay's length in a cruise, in seconds of sailing at full pace. */
export const MIN_PASSAGE_SHARE_S = 4

/** Shares the replay's length (seconds of sailing) between passages, by their
 * sailing time. Each passage gets at least `minS`: the short ones are lifted
 * to it and the others scaled down to fit what is left, until none of them
 * falls below it. When the minimums alone come to more than the length, every
 * passage gets an equal share. A passage with no sailing time gets none. The
 * shares add up to `lengthS`. */
export function passageShares(durations: readonly number[], lengthS: number, minS: number = MIN_PASSAGE_SHARE_S): number[] {
  const sailed = durations.map((d) => (d > 0 ? d : 0))
  const count = sailed.filter((d) => d > 0).length
  if (count === 0 || !(lengthS > 0)) return sailed.map(() => 0)
  if (count * minS >= lengthS) return sailed.map((d) => (d > 0 ? lengthS / count : 0))
  const pinned = new Set<number>()
  for (;;) {
    const free = sailed.map((d, i) => (d > 0 && !pinned.has(i) ? d : 0))
    const freeTotal = free.reduce((a, b) => a + b, 0)
    const left = lengthS - pinned.size * minS
    const shares = sailed.map((d, i) => (d <= 0 ? 0 : pinned.has(i) ? minS : (left * free[i]) / freeTotal))
    const low = shares.map((s, i) => (sailed[i] > 0 && !pinned.has(i) && s < minS ? i : -1)).filter((i) => i >= 0)
    if (low.length === 0) return shares
    // Every one below the minimum is lifted to it at once; the rest are shared out again.
    low.forEach((i) => pinned.add(i))
  }
}

/** The pace spans of a replay of several passages: the camera shots' own
 * paces, with each passage's share of the length worked in. A passage given
 * more or less of the length than its sailing time earns runs slower or faster
 * by that ratio, on top of any shot's pace. A single passage is the shots'
 * spans as they are. */
export function cruisePaceSpans(resolved: readonly ResolvedShot[], timeline: Timeline, lengthS: number): PaceSpan[] {
  const shots = paceSpans(resolved)
  if (!isMultiPassage(timeline) || !(timeline.total > 0) || !(lengthS > 0)) return shots
  const lengths = timeline.entries.map((e) => Math.max(0, e.end - e.start))
  const shares = passageShares(lengths, lengthS)
  // Relative to the pace the whole length would run at: 1 for a passage that gets what it earns.
  const base = timeline.total / lengthS
  const relative = lengths.map((d, i) => (d > 0 && shares[i] > 0 ? d / shares[i] / base : 1))
  const sortedShots = [...shots].sort((a, b) => a.from - b.from)
  // The pace in force at a position: that of the last shot at or before it.
  const shotPaceAt = (clock: number) => {
    const n = lowerBound(sortedShots, (s) => s.from, clock, true)
    return n > 0 ? sortedShots[n - 1].pace : 1
  }
  const points = [...new Set([...timeline.entries.map((e) => e.offset), ...sortedShots.map((s) => Math.max(0, s.from))])].filter((c) => c < timeline.total).sort((a, b) => a - b)
  // The passage a clock position is in, by the rule the replay locates the boat by.
  const indexOf = new Map(timeline.entries.map((e, i) => [e.id, i]))
  const passageAt = (clock: number) => indexOf.get(locate(timeline, clock)!.entry.id)!
  return points.map((from) => ({ from, pace: shotPaceAt(from) * relative[passageAt(from)] }))
}
