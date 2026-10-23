// The replay's clock. The scrubber and the boat run on timeline seconds (the
// passages end to end); the operator watches wall-clock seconds. A camera
// shot with a pace slows the timeline down for as long as it lasts, so the two
// are related by a piecewise rate. Everything here is pure.

import type { Timeline } from '@/lib/passages'

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
