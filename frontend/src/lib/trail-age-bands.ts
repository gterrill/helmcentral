// Age bands for the anchor map's trails (ADR 0263). A trail is drawn in a
// handful of bands by how old each stretch is, newest brightest, rather than as
// one gradient along the line: a gradient over line-progress fades with the
// distance travelled, which says nothing about time. The lines come back in
// time order; the map draws them by `line-sort-key` on the band, so the newest
// sits on top whatever the order.

import { hexAlpha } from '@/lib/hex-mix'

/** How long AIS tracks are kept and drawn: the server serves 24 hours, and the bands never span more. */
export const AIS_TRACK_WINDOW_MS = 24 * 60 * 60 * 1000

/** How many age bands a trail is split into. */
export const TRAIL_BANDS = 5

export interface AgedPoint {
  lat: number
  lon: number
  timestampMs: number
  /** True where the line must not join this point to the one before it. */
  newSegment?: boolean
}

export interface AgeBandedLine {
  /** 0 is the oldest band, TRAIL_BANDS - 1 the newest. */
  band: number
  /** [lon, lat] pairs. */
  coordinates: [number, number][]
}

// Amber at rising opacity: the oldest is nearly clear, the newest solid.
const AIS_AMBER = '#f59e0b'
export const AIS_BAND_COLORS: readonly string[] = [0.15, 0.35, 0.55, 0.78, 1].map(alpha => hexAlpha(AIS_AMBER, alpha))

// Own swing trail: dim yellow to bright yellow, solid all the way so the old
// end stays visible, and a yellow of its own so it reads apart from AIS amber.
export const SELF_BAND_COLORS: readonly string[] = ['#8a6f0d', '#a8870f', '#c9a111', '#e6b912', '#facc15']

/** The AIS bands are worked out against a clock that moves in steps this long. */
export const BAND_CLOCK_MS = 5 * 60 * 1000

/** `nowMs` rounded down to the band clock's grid, so the same bands come out between marks. */
export function bandClock(nowMs: number): number {
  return Math.floor(nowMs / BAND_CLOCK_MS) * BAND_CLOCK_MS
}

/** How long until the band clock's next mark. */
export function msToNextBandClock(nowMs: number): number {
  return bandClock(nowMs) + BAND_CLOCK_MS - nowMs
}

/**
 * The shortest AIS window. A boat seen for minutes would otherwise fade its
 * first minutes to clear, and against the five minute band clock its bands
 * would jump a large share of the track at every mark.
 */
export const AIS_TRAIL_MIN_WINDOW_MS = 60 * 60 * 1000

/**
 * The age that fills all the bands for a trail: its own span, oldest point to
 * `nowMs`, so a short history still fades from dim to bright. `maxMs` stops a
 * long one at the longest age worth drawing, `minMs` keeps a very short one
 * from spreading minutes over every band. At least 1 ms, so a lone or
 * future-stamped point never gives a zero-width band.
 */
export function trailWindowMs(
  points: readonly AgedPoint[],
  nowMs: number,
  { minMs = 1, maxMs = Infinity }: { minMs?: number; maxMs?: number } = {},
): number {
  const span = nowMs - (points[0]?.timestampMs ?? nowMs)
  return Math.max(Math.min(span, maxMs), minMs, 1)
}

// How many band-widths old an age is: 0 for the newest band. A point stamped
// slightly ahead of the clock counts as the newest.
function bucketForAge(ageMs: number, windowMs: number): number {
  return Math.min(TRAIL_BANDS - 1, Math.max(0, Math.floor(ageMs / (windowMs / TRAIL_BANDS))))
}

/** The band an age falls in, 0 oldest to TRAIL_BANDS - 1 newest. */
export function bandForAge(ageMs: number, windowMs: number): number {
  return TRAIL_BANDS - 1 - bucketForAge(ageMs, windowMs)
}

/**
 * Splits a time-ordered trail into lines, one per run of one band within one
 * segment. `windowMs` is the age that fills all the bands (see `trailWindowMs`).
 * A stretch ending older than the window is left out. Each line is banded by
 * its newer end and begins at the point before it, so neighbouring bands meet.
 * One pass over the points.
 */
export function groupLinesByAge(
  points: readonly AgedPoint[],
  { nowMs, windowMs }: { nowMs: number; windowMs: number },
): AgeBandedLine[] {
  const lines: AgeBandedLine[] = []
  let current: AgeBandedLine | null = null
  for (let i = 1; i < points.length; i++) {
    const p = points[i]
    const age = nowMs - p.timestampMs
    if (p.newSegment || age >= windowMs) {
      current = null
      continue
    }
    const band = bandForAge(age, windowMs)
    if (current && current.band === band) {
      current.coordinates.push([p.lon, p.lat])
    } else {
      const prev = points[i - 1]
      current = { band, coordinates: [[prev.lon, prev.lat], [p.lon, p.lat]] }
      lines.push(current)
    }
  }
  return lines
}
