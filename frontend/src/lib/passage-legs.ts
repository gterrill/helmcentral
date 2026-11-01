// Legs: a passage is one leg unless the operator (or a merge) marked a sign as
// starting a new one. Colours run leg by leg across everything in view, so a
// passage with no joins looks as it always did. Pure.

import type { TimedPoint, TimedSegment } from '@/lib/passages'

export interface LegPassage {
  id: string
  /** Unix milliseconds. */
  startMs: number
  endMs: number
}

export interface PassageLeg {
  passageId: string
  fromMs: number
  toMs: number
  /** Runs across every leg in the view; the palette wraps it. */
  colourIndex: number
}

export interface LegSign {
  t_ms: number
  starts_leg: boolean
}

/** The legs of the passages in view order. A join strictly inside a passage
 * splits it there; a join at or beyond a passage's ends does nothing. */
export function passageLegs(passages: readonly LegPassage[], signs: readonly LegSign[]): PassageLeg[] {
  const joins = [...new Set(signs.filter((s) => s.starts_leg).map((s) => s.t_ms))].sort((a, b) => a - b)
  const legs: PassageLeg[] = []
  for (const p of passages) {
    const cuts = joins.filter((t) => t > p.startMs && t < p.endMs)
    const edges = [p.startMs, ...cuts, p.endMs]
    for (let i = 0; i < edges.length - 1; i++) {
      legs.push({ passageId: p.id, fromMs: edges[i], toMs: edges[i + 1], colourIndex: legs.length })
    }
  }
  return legs
}

/** The legs of one passage, in order. */
export const legsOf = (legs: readonly PassageLeg[], passageId: string) => legs.filter((l) => l.passageId === passageId)

function between(a: TimedPoint, b: TimedPoint, t: number): TimedPoint {
  const f = (t - a.t) / (b.t - a.t)
  return { t, lat: a.lat + (b.lat - a.lat) * f, lon: a.lon + (b.lon - a.lon) * f }
}

/** The part of the segments between two instants (Unix seconds), cut on the
 * position interpolated between the fixes either side so neighbouring legs
 * meet. A segment with nothing inside is dropped. */
export function clipSegments(segments: TimedSegment[], from: number, to: number): TimedSegment[] {
  const out: TimedSegment[] = []
  for (const seg of segments) {
    const pts: TimedPoint[] = []
    for (let i = 0; i < seg.length; i++) {
      const p = seg[i]
      if (i > 0) {
        const a = seg[i - 1]
        if (a.t < from && p.t > from) pts.push(between(a, p, from))
        if (a.t < to && p.t > to) pts.push(between(a, p, to))
      }
      if (p.t >= from && p.t <= to) pts.push(p)
    }
    if (pts.length > 0) out.push(pts)
  }
  return out
}

/** One leg of a passage's track. The first leg is open at the start and the
 * last at the end, so nothing the passage logged outside its own times is lost. */
export function legSegments(segments: TimedSegment[], passageLegsInOrder: readonly PassageLeg[], index: number): TimedSegment[] {
  if (passageLegsInOrder.length <= 1) return segments
  const leg = passageLegsInOrder[index]
  const from = index === 0 ? -Infinity : leg.fromMs / 1000
  const to = index === passageLegsInOrder.length - 1 ? Infinity : leg.toMs / 1000
  return clipSegments(segments, from, to)
}

/** One leg of an exported passage's track, by the leg's own seconds. */
export function exportLegSegments(segments: TimedSegment[], legs: readonly { a: number; b: number }[], index: number): TimedSegment[] {
  if (legs.length <= 1) return segments
  return clipSegments(segments, index === 0 ? -Infinity : legs[index].a, index === legs.length - 1 ? Infinity : legs[index].b)
}

/** A piece of track as the map draws it: a leg of a passage. */
export interface DrawnPiece {
  key: string
  passageId: string
  segments: TimedSegment[]
  colourIndex: number
  /** The real times (Unix seconds) the piece covers; open at a passage's two ends. */
  from: number
  to: number
}

/** What is drawn: a piece of track per leg. The first piece of a passage is
 * open at its start and the last at its end, as the sailed line reads it. */
export function drawnPieces(passages: readonly { id: string; segments: TimedSegment[] }[], legs?: readonly PassageLeg[]): DrawnPiece[] {
  const out: DrawnPiece[] = []
  passages.forEach((p, i) => {
    const mine = legs ? legsOf(legs, p.id) : []
    if (mine.length === 0) {
      out.push({ key: p.id, passageId: p.id, segments: p.segments, colourIndex: i, from: -Infinity, to: Infinity })
      return
    }
    mine.forEach((leg, k) => out.push({
      key: mine.length === 1 ? p.id : `${p.id}-${k}`,
      passageId: p.id,
      segments: legSegments(p.segments, mine, k),
      colourIndex: leg.colourIndex,
      from: k === 0 ? -Infinity : leg.fromMs / 1000,
      to: k === mine.length - 1 ? Infinity : leg.toMs / 1000,
    }))
  })
  return out
}
