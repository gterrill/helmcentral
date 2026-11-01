// The replay's time. Two clocks run through it: the timeline (the slider,
// the boat, the passages end to end) and the watching (seconds the operator
// sits through). The length the operator picks, 8, 15, 30 or 60 s, is the
// sailing; a camera shot's pace slows the sailing, and each photo moment adds
// its hold on top, during which the timeline stands still.
//
// Everything here is pure, so the live replay and a video of it can share it.
// (The HTML export player keeps its own timing and does not use this yet.)

import { easeInOut } from '@/lib/replay-camera'
import { timelineAtWall, wallAtTimeline, type PaceSpan } from '@/lib/replay-clock'

// ── a photo moment ───────────────────────────────────────────────────────

/** Seconds the photo takes to grow from the sign's card to fill the frame. */
export const MOMENT_GROW_S = 0.4
/** Seconds it stays, zooming slowly. */
const MOMENT_HOLD_S = 1.5
/** Seconds it takes to shrink back into the card. */
const MOMENT_SHRINK_S = 0.4
/** The whole hold, which the replay adds to its length for each moment. */
export const MOMENT_TOTAL_S = MOMENT_GROW_S + MOMENT_HOLD_S + MOMENT_SHRINK_S
/** How much of the frame the photo fills when it is grown (each way). */
export const MOMENT_FRAME_FRACTION = 0.7
/** The slow zoom across the hold. */
export const MOMENT_KEN_BURNS = 1.06
/** The photo's shape: the same 4 by 3 the sign's card shows it in. */
export const PHOTO_ASPECT = 4 / 3

/** Seconds of watching under which two places count as one: the sum of holds and sailing is not exact. */
const WALL_EPS = 1e-9

type MomentPhase = 'grow' | 'hold' | 'shrink'

/** A moment, as it stands: what to draw, to be drawn by the page or on a canvas. */
export interface MomentView {
  signId: string
  phase: MomentPhase
  /** From the sign's card (0) to the photo at full size (1), eased. */
  fill: number
  /** The zoom inside the photo's frame, 1 to 1.06. */
  scale: number
}

/** The moment `elapsedS` seconds in. Reduced motion has no growing, shrinking
 * or zoom: the photo is full and still for the whole moment. */
export function momentView(signId: string, elapsedS: number, reduced: boolean): MomentView {
  const e = Math.max(0, Math.min(MOMENT_TOTAL_S, elapsedS))
  let phase: MomentPhase = 'hold'
  let fill = 1
  let scale = 1
  if (!reduced) {
    if (e < MOMENT_GROW_S) {
      phase = 'grow'
      fill = easeInOut(e / MOMENT_GROW_S)
    } else if (e < MOMENT_GROW_S + MOMENT_HOLD_S) {
      scale = 1 + (MOMENT_KEN_BURNS - 1) * ((e - MOMENT_GROW_S) / MOMENT_HOLD_S)
    } else {
      phase = 'shrink'
      fill = easeInOut(1 - (e - MOMENT_GROW_S - MOMENT_HOLD_S) / MOMENT_SHRINK_S)
      scale = MOMENT_KEN_BURNS
    }
  }
  return { signId, phase, fill, scale }
}

export interface Rect { x: number; y: number; w: number; h: number }

/** Where the photo is, `view` through its moment: from the card's photo to the
 * photo at full size (70 percent of the frame, in its own shape, in the
 * middle), by how far it has grown. All in the frame's pixels. The zoom inside
 * the photo's frame goes with it. */
export function momentRect(view: MomentView, card: Rect, frame: { w: number; h: number }): Rect & { scale: number } {
  const w = Math.min(frame.w * MOMENT_FRAME_FRACTION, frame.h * MOMENT_FRAME_FRACTION * PHOTO_ASPECT)
  const h = w / PHOTO_ASPECT
  const full: Rect = { x: (frame.w - w) / 2, y: (frame.h - h) / 2, w, h }
  const t = view.fill
  return {
    x: card.x + (full.x - card.x) * t,
    y: card.y + (full.y - card.y) * t,
    w: card.w + (full.w - card.w) * t,
    h: card.h + (full.h - card.h) * t,
    scale: view.scale,
  }
}

// ── the time map ─────────────────────────────────────────────────────────

/** A sign whose photo gets a moment, where the boat meets it. */
export interface ReplayMoment {
  /** Timeline seconds. */
  clock: number
  id: string
}

export interface ReplayTimeInput {
  /** Timeline seconds. */
  total: number
  /** Seconds of sailing the operator chose. */
  lengthS: number
  spans: readonly PaceSpan[]
  moments: readonly ReplayMoment[]
}

export interface MomentAt {
  id: string
  /** Seconds into the hold. */
  elapsed: number
}

export interface ReplayTime {
  /** Every moment, in order, with the watching time its hold starts at. */
  moments: readonly (ReplayMoment & { startWall: number })[]
  /** Seconds of watching from end to end: the sailing and every hold. */
  totalWall: number
  /** The timeline position, and the moment if the watching is in a hold, `wall` seconds in. */
  at(wall: number): { clock: number; moment: MomentAt | null }
  /** The watching time to put a timeline position at. A seek (the slider, a
   * shot's tick) lands clear of every hold, after those of any signs standing
   * at that clock, so reaching the end is the end. A jump to a sign itself
   * (`onSign`) lands at the start of its moment. */
  wallAtClock(clock: number, opts?: { onSign?: boolean }): number
  /** The timeline's length, in timeline seconds. */
  total: number
  /** Seconds of sailing alone (no holds) to reach a timeline position. */
  sailingWallAt(clock: number): number
  /** Where the hold the watching is in ends; the watching itself if it is in none. */
  holdEnd(wall: number): number
}

/** How many of the items in a list sorted by `key` come before `x` (or, with `orAt`, up to and including it). */
function lowerBound<T>(list: readonly T[], key: (item: T) => number, x: number, orAt = false): number {
  let lo = 0
  let hi = list.length
  while (lo < hi) {
    const mid = (lo + hi) >> 1
    if (key(list[mid]) < x || (orAt && key(list[mid]) === x)) lo = mid + 1
    else hi = mid
  }
  return lo
}

export function buildReplayTime({ total, lengthS, spans, moments }: ReplayTimeInput): ReplayTime {
  const sailWall = (clock: number) => wallAtTimeline(total, lengthS, spans, clock)
  const ordered = [...moments].sort((a, b) => a.clock - b.clock).map((m, i) => ({ ...m, startWall: sailWall(m.clock) + MOMENT_TOTAL_S * i }))
  const totalWall = sailWall(total) + MOMENT_TOTAL_S * ordered.length

  /** How many moments have started by `wall`. */
  const startedBy = (wall: number) => lowerBound(ordered, (m) => m.startWall, wall, true)

  return {
    moments: ordered,
    totalWall,
    at(wall) {
      if (wall >= totalWall) return { clock: total, moment: null }
      const w = Math.max(0, wall)
      // The last moment that has started by now: in its hold, or past it.
      const i = startedBy(w) - 1
      if (i >= 0 && w < ordered[i].startWall + MOMENT_TOTAL_S - WALL_EPS) {
        return { clock: ordered[i].clock, moment: { id: ordered[i].id, elapsed: w - ordered[i].startWall } }
      }
      const sailing = w - MOMENT_TOTAL_S * (i + 1)
      // Past the hold of moment i, so no earlier than its clock, whatever the rounding says.
      const clock = Math.min(total, timelineAtWall(total, lengthS, spans, sailing))
      return { clock: i >= 0 ? Math.max(clock, ordered[i].clock) : clock, moment: null }
    },
    wallAtClock(clock, opts) {
      const c = Math.max(0, Math.min(total, clock))
      return sailWall(c) + MOMENT_TOTAL_S * lowerBound(ordered, (m) => m.clock, c, !opts?.onSign)
    },
    total,
    sailingWallAt: sailWall,
    holdEnd(wall) {
      const i = startedBy(wall) - 1
      return i >= 0 && wall >= ordered[i].startWall && wall < ordered[i].startWall + MOMENT_TOTAL_S - WALL_EPS ? ordered[i].startWall + MOMENT_TOTAL_S : wall
    },
  }
}

/** The passages the replay has begun by `clock` (timeline seconds), as the page marks them. */
export const playedIds = (timeline: { entries: readonly { id: string; offset: number }[] }, clock: number): ReadonlySet<string> =>
  new Set(timeline.entries.filter((e) => e.offset <= clock).map((e) => e.id))
