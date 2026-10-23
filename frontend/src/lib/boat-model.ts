// The pure side of the 3D boat on the passage replay: how big it is, how big
// it is drawn, which way it points, and whether it is shown at all. No three.js
// here, so none of it pulls the 3D library into the main bundle.

import type { HullType } from '@/config/app-config'
import { metersPerPixel } from '@/lib/anchor-adjust'
import { bearingDeg, haversineMeters } from '@/lib/geo'
import type { TimedSegment } from '@/lib/passages'

/** What the map needs to know to draw this boat's hull. */
export interface BoatHull {
  type: HullType
  /** Length overall and beam in metres, or null when the vessel has none. */
  loaM: number | null
  beamM: number | null
}

// A typical length-to-beam ratio per hull type, used only to give a boat of
// known length a plausible beam when the vessel details have none. Catamarans
// are far beamier for their length than monohulls.
const LENGTH_TO_BEAM: Record<HullType, number> = {
  sail_mono: 3.2,
  power_mono: 3.0,
  sail_cat: 1.9,
  power_cat: 2.0,
}

const positive = (n: number | null | undefined): n is number => typeof n === 'number' && Number.isFinite(n) && n > 0

export interface BoatDimensions {
  /** Null when the vessel's length is not known: the boat is then drawn
   * as a unit-length shape at a fixed size on screen, not to scale. */
  loaM: number | null
  /** For a boat of unknown length, the beam of the unit-length shape. */
  beamM: number
  /** Which of 'length' and 'beam' the vessel details did not give. */
  missing: ('length' | 'beam')[]
}

/** No length, no scale: nothing here invents a boat's size. A missing beam
 * comes from the typical ratio for the hull type; a missing length leaves a
 * unit-length shape that the layer draws at the minimum on-screen size. */
export function resolveBoatDimensions(type: HullType, loaM: number | null | undefined, beamM: number | null | undefined): BoatDimensions {
  const ratio = LENGTH_TO_BEAM[type]
  if (!positive(loaM)) return { loaM: null, beamM: 1 / ratio, missing: ['length'] }
  if (!positive(beamM)) return { loaM, beamM: loaM / ratio, missing: ['beam'] }
  return { loaM, beamM, missing: [] }
}

/** The boat is never drawn shorter than this on screen, in CSS pixels. */
export const MIN_BOAT_PX = 40

/** The scale the model is drawn at. With the length known, how many times true
 * size: 1 once true size is at least MIN_BOAT_PX long on screen, more below
 * that so it stays findable. With no length the model is one metre long and
 * the result is the metres that make it MIN_BOAT_PX long at this zoom. */
export function boatScaleFactor(zoom: number, latDeg: number, loaM: number | null): number {
  const mpp = metersPerPixel(latDeg, zoom)
  if (loaM === null) return MIN_BOAT_PX * mpp
  return Math.max(1, MIN_BOAT_PX / (loaM / mpp))
}

const rad = (d: number) => (d * Math.PI) / 180

/** Mean of compass bearings, correct across north (359 and 1 give 0). */
export function circularMeanDeg(values: number[]): number {
  const x = values.reduce((s, v) => s + Math.cos(rad(v)), 0)
  const y = values.reduce((s, v) => s + Math.sin(rad(v)), 0)
  return (((Math.atan2(y, x) * 180) / Math.PI) + 360) % 360
}

const KNOT_MS = 0.514444
/** Below this speed over the ground the boat is taken to be stopped. */
export const STILL_SOG_KTS = 0.5
/** The track is chorded over these spans, in seconds, and the bearings averaged. */
const HEADING_SPANS_S = [30, 60, 90]

// A track indexed by time, so the position at an instant is a binary search
// rather than a filter and a scan of every segment on every frame. Answers
// exactly as positionAt does. Built once per track and remembered.
export interface TrackIndex {
  positionAt: (t: number) => { lat: number; lon: number } | null
}

const indexes = new WeakMap<TimedSegment[], TrackIndex>()

export function trackIndex(segments: TimedSegment[]): TrackIndex {
  const cached = indexes.get(segments)
  if (cached) return cached
  const usable = segments.filter((s) => s.length > 0)
  const firstOf = (s: TimedSegment) => s[0].t
  const lastOf = (s: TimedSegment) => s[s.length - 1].t
  const pt = (p: { lat: number; lon: number }) => ({ lat: p.lat, lon: p.lon })
  const index: TrackIndex = {
    positionAt(t) {
      if (usable.length === 0) return null
      if (t <= firstOf(usable[0])) return pt(usable[0][0])
      // The first segment that has not ended before t.
      let lo = 0
      let hi = usable.length
      while (lo < hi) {
        const mid = (lo + hi) >> 1
        if (lastOf(usable[mid]) >= t) hi = mid
        else lo = mid + 1
      }
      if (lo === usable.length) return pt(usable[lo - 1][usable[lo - 1].length - 1])
      const seg = usable[lo]
      if (t < firstOf(seg)) return pt(usable[lo - 1][usable[lo - 1].length - 1])
      // The first point at or after t; its predecessor starts the span.
      let a = 1
      let b = seg.length - 1
      while (a < b) {
        const mid = (a + b) >> 1
        if (seg[mid].t >= t) b = mid
        else a = mid + 1
      }
      if (seg.length === 1) return pt(seg[0])
      const p = seg[a - 1]
      const q = seg[a]
      const span = q.t - p.t
      const f = span > 0 ? (t - p.t) / span : 1
      return { lat: p.lat + (q.lat - p.lat) * f, lon: p.lon + (q.lon - p.lon) * f }
    },
  }
  indexes.set(segments, index)
  return index
}

function chordBearings(index: TrackIndex, at: number, direction: 1 | -1): number[] {
  const out: number[] = []
  const here = index.positionAt(at)
  if (!here) return out
  for (const span of HEADING_SPANS_S) {
    const other = index.positionAt(at + direction * span)
    if (!other) continue
    const metres = haversineMeters(here.lat, here.lon, other.lat, other.lon)
    if (metres / span < STILL_SOG_KTS * KNOT_MS) continue
    out.push(direction === 1 ? bearingDeg(here.lat, here.lon, other.lat, other.lon) : bearingDeg(other.lat, other.lon, here.lat, here.lon))
  }
  return out
}

/** The bow's bearing at replay instant `at`: the course over ground of the
 * track just sailed, averaged over a few spans. The replay data carries
 * positions and times, not a course, so it is read from the positions. With
 * nothing sailed behind her (a start, or a stop) the way she is about to go is
 * used, and only if that is nothing too is `previous` held. Null when there is
 * no movement to read and nothing to hold. */
export function trackHeading(segments: TimedSegment[], at: number, previous: number | null): number | null {
  const index = trackIndex(segments)
  const behind = chordBearings(index, at, -1)
  if (behind.length > 0) return circularMeanDeg(behind)
  const ahead = chordBearings(index, at, 1)
  if (ahead.length > 0) return circularMeanDeg(ahead)
  return previous
}

export interface HeadingState {
  id: string
  at: number
  heading: number | null
}

/** A held heading only carries on while playback moves forward, a little at a
 * time, within one passage. A seek, a backwards scrub or the next passage
 * starts from the track again. */
const HOLD_MAX_STEP_S = 600

export function nextHeading(last: HeadingState | null, cursor: { id: string; at: number }, segments: TimedSegment[]): HeadingState {
  const continuing = last !== null && last.id === cursor.id && cursor.at >= last.at && cursor.at - last.at <= HOLD_MAX_STEP_S
  return { id: cursor.id, at: cursor.at, heading: trackHeading(segments, cursor.at, continuing ? last.heading : null) }
}

export interface BoatMarkerInput {
  threeD: boolean
  terrain: 'ok' | 'failed' | 'retrying'
  hull: BoatHull | null
  /** The 3D layer has loaded and been added to the map. */
  layerReady: boolean
}

/** The model stands in for the dot only when everything it needs is there. */
export function boatMarkerKind({ threeD, terrain, hull, layerReady }: BoatMarkerInput): 'model' | 'dot' {
  return threeD && terrain === 'ok' && hull !== null && layerReady ? 'model' : 'dot'
}
