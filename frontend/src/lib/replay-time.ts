// The replay's time. Two clocks run through it: the timeline (the slider,
// the boat, the passages end to end) and the watching (seconds the operator
// sits through). The length the operator picks, 8 s to 3 min, is the
// sailing; a camera shot's pace slows the sailing, and each hold (a photo
// moment, or a stay between two passages) adds its time on top, during which
// the timeline stands still.
//
// Everything here is pure, so the live replay and a video of it can share it.
// (The HTML export player keeps its own timing and does not use this yet.)

import { easeInOut } from '@/lib/replay-camera'
import { timelineAtWall, wallAtTimeline, type PaceSpan } from '@/lib/replay-clock'
import { firstWhere, lowerBound } from '@/lib/sorted-search'

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

/** Seconds a stay between two passages holds the replay. */
export const STAY_S = 1.5

/** Where the replay holds: the timeline position, and a name for the hold
 * (a sign's id for a photo moment, a stay's id for a stay). */
export interface ReplayHoldInput {
  /** Timeline seconds. */
  clock: number
  id: string
  /** The real instant (Unix seconds) it happens at. Holds at one clock play in the order of their real times: at a join, the sign at the end of the arriving passage, then the stay, then the sign at the start of the next. */
  at?: number
}

export interface ReplayTimeInput {
  /** Timeline seconds. */
  total: number
  /** Seconds of sailing the operator chose. */
  lengthS: number
  spans: readonly PaceSpan[]
  /** Photo moments, each holding the replay for MOMENT_TOTAL_S. */
  moments: readonly ReplayHoldInput[]
  /** Stays between passages, each holding it for STAY_S. */
  stays?: readonly ReplayHoldInput[]
}

/** A hold: the timeline stands still at `clock` for `durationS` seconds of watching. */
export interface Hold {
  kind: 'moment' | 'stay'
  id: string
  clock: number
  /** The real instant it happens at; 0 when the input gave none. */
  at: number
  durationS: number
  /** The watching time the hold starts at. */
  startWall: number
}

/** The hold the watching is in. */
export interface HoldAt {
  kind: Hold['kind']
  id: string
  /** Seconds into the hold. */
  elapsed: number
  durationS: number
}

export interface ReplayTime {
  /** Every hold, a photo moment or a stay, in order. The time map has no special case for either. */
  holds: readonly Hold[]
  /** Seconds of watching from end to end: the sailing and every hold. */
  totalWall: number
  /** The timeline position, and the hold if the watching is in one, `wall` seconds in. */
  at(wall: number): { clock: number; hold: HoldAt | null }
  /** The watching time to put a timeline position at. A seek (the slider, a
   * shot's tick) lands clear of every hold, after those of any signs standing
   * at that clock and the stay that ends there, so reaching the end is the end.
   * A jump to a sign itself (`onSign`) lands before the holds at that clock,
   * and, given the sign's real time (`at`), after those that happen before it:
   * a sign at the start of a passage lands after the stay that came first. */
  wallAtClock(clock: number, opts?: { onSign?: boolean; at?: number }): number
  /** The timeline's length, in timeline seconds. */
  total: number
  /** Seconds of sailing alone (no holds) to reach a timeline position. */
  sailingWallAt(clock: number): number
  /** Where the hold the watching is in ends; the watching itself if it is in none. */
  holdEnd(wall: number): number
}

export function buildReplayTime({ total, lengthS, spans, moments, stays = [] }: ReplayTimeInput): ReplayTime {
  const sailWall = (clock: number) => wallAtTimeline(total, lengthS, spans, clock)
  // At one clock, by real time; with the same time, a moment before a stay (a sign at the arrival shows, then the stay begins).
  const drafts = [
    ...moments.map((m) => ({ kind: 'moment' as const, id: m.id, clock: m.clock, at: m.at ?? 0, durationS: MOMENT_TOTAL_S })),
    ...stays.map((s) => ({ kind: 'stay' as const, id: s.id, clock: s.clock, at: s.at ?? 0, durationS: STAY_S })),
  ].sort((a, b) => a.clock - b.clock || a.at - b.at || (a.kind === b.kind ? 0 : a.kind === 'moment' ? -1 : 1))
  // before[i] is the watching time the holds ahead of hold i add.
  const before: number[] = [0]
  for (const h of drafts) before.push(before[before.length - 1] + h.durationS)
  const holds: Hold[] = drafts.map((h, i) => ({ ...h, startWall: sailWall(h.clock) + before[i] }))
  const totalWall = sailWall(total) + before[holds.length]

  /** The last hold that has started by `wall`, or -1: the watching is in it, or past it. */
  const lastStarted = (wall: number) => lowerBound(holds, (h) => h.startWall, wall, true) - 1
  /** Whether the watching is inside hold `i`. */
  const within = (i: number, wall: number) => i >= 0 && wall < holds[i].startWall + holds[i].durationS - WALL_EPS

  return {
    holds,
    totalWall,
    at(wall) {
      if (wall >= totalWall) return { clock: total, hold: null }
      const w = Math.max(0, wall)
      const i = lastStarted(w)
      if (within(i, w)) {
        const h = holds[i]
        return { clock: h.clock, hold: { kind: h.kind, id: h.id, elapsed: w - h.startWall, durationS: h.durationS } }
      }
      const clock = Math.min(total, timelineAtWall(total, lengthS, spans, w - before[i + 1]))
      // Past the hold i, so no earlier than its clock, whatever the rounding says.
      return { clock: i >= 0 ? Math.max(clock, holds[i].clock) : clock, hold: null }
    },
    wallAtClock(clock, opts) {
      const c = Math.max(0, Math.min(total, clock))
      if (opts?.onSign && opts.at !== undefined) {
        // Before the sign: the holds at earlier clocks, and at this clock the ones that happen earlier in real time.
        const at = opts.at
        return sailWall(c) + before[firstWhere(holds, (h) => h.clock > c || (h.clock === c && h.at >= at))]
      }
      return sailWall(c) + before[lowerBound(holds, (h) => h.clock, c, !opts?.onSign)]
    },
    total,
    sailingWallAt: sailWall,
    holdEnd(wall) {
      const i = lastStarted(wall)
      return within(i, wall) ? holds[i].startWall + holds[i].durationS : wall
    },
  }
}

/** The passages the replay has begun by `clock` (timeline seconds), as the page marks them. */
export const playedIds = (timeline: { entries: readonly { id: string; offset: number }[] }, clock: number): ReadonlySet<string> =>
  new Set(timeline.entries.filter((e) => e.offset <= clock).map((e) => e.id))
