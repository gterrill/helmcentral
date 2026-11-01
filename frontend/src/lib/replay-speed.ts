// The sailed track coloured by speed, and the wake behind the boat. Pure
// functions of the track and of how far the replay has sailed it, so the live
// map and a video of it draw the same line.
//
// The speed is read from the track itself, distance over time between fixes,
// since a passage's track carries positions and times only. It is the same
// reading the server makes when it imports a track.

import type { ExpressionSpecification } from 'maplibre-gl'

import { metersPerPixel } from '@/lib/anchor-adjust'
import { haversineMeters, mercatorX, mercatorY } from '@/lib/geo'
import { hexAlpha, mixHex } from '@/lib/hex-mix'
import type { DrawnPiece } from '@/lib/passage-legs'
import { sailedLines, type TimedSegment } from '@/lib/passages'
import type { SailedTo } from '@/lib/replay-controller'

const METRES_PER_KNOT_SECOND = 1852 / 3600
/** The shortest stretch of track a speed is read over, in seconds: a fix a second after the last says little. */
const MIN_SPAN_S = 20
/** The domain never runs under this, so a slow passage does not paint every slow drift as the fastest. */
const MIN_DOMAIN_KTS = 5
/** A gradient texture holds only so many stops. */
const MAX_STOPS = 200
const MIN_STOP_GAP = 1 / MAX_STOPS
/** A new gradient is built once the sailed length has grown by this much. */
const REBUILD_GROWTH = 1.03
/** The wake is this share of the passage's length, or WAKE_MIN_PX on the screen if that is longer. */
const WAKE_FRACTION = 0.04
const WAKE_MIN_PX = 60
/** How far the wake colour is lightened toward white. */
const WAKE_FOAM = 0.7
/** A gradient follows one line, so the speed colours take a source per segment; past this many the plain line stays. */
export const MAX_SPEED_SEGMENTS = 12

// Slow to fast, sequential and read by lightness as well as hue, so it holds
// for colour-blind readers. Each theme leaves out the end that would vanish
// into its map: the pale yellow on the light map, the deep purple on the dark.
export const SPEED_RAMP_LIGHT = ['#472d7b', '#3b528b', '#21908d', '#27ad81', '#5cc863'] as const
export const SPEED_RAMP_DARK = ['#3b528b', '#21908d', '#27ad81', '#85d54a', '#fde725'] as const

const lerp = (a: number, b: number, f: number) => a + (b - a) * f
/** How far x is from x0 to x1, 0 to 1; 1 for no span. */
const fraction = (x0: number, x1: number, x: number) => (x1 > x0 ? (x - x0) / (x1 - x0) : 1)
/** The first index whose value is at or past `x` in a rising list (the list's length if none). */
function lowerBound(xs: readonly number[], x: number): number {
  let lo = 0
  let hi = xs.length
  while (lo < hi) {
    const mid = (lo + hi) >> 1
    if (xs[mid] < x) lo = mid + 1
    else hi = mid
  }
  return lo
}

export interface SpeedProfile {
  /** Unix seconds at each fix. */
  t: number[]
  /** Metres from the start of the segment, at each fix. */
  dist: number[]
  /** The same length as the map draws the line, in Mercator units of the world's width: what `line-progress` runs along. */
  merc: number[]
  /** Knots at each fix. */
  sog: number[]
  totalM: number
}

export function speedProfile(seg: TimedSegment): SpeedProfile {
  const dist: number[] = [0]
  for (let i = 1; i < seg.length; i++) dist.push(dist[i - 1] + haversineMeters(seg[i - 1].lat, seg[i - 1].lon, seg[i].lat, seg[i].lon))
  const merc: number[] = [0]
  for (let i = 1; i < seg.length; i++) {
    merc.push(merc[i - 1] + Math.hypot(mercatorX(seg[i].lon) - mercatorX(seg[i - 1].lon), mercatorY(seg[i].lat) - mercatorY(seg[i - 1].lat)))
  }
  const sog = seg.map((_, i) => {
    let lo = i
    let hi = i
    while (seg[hi].t - seg[lo].t < MIN_SPAN_S && (lo > 0 || hi < seg.length - 1)) {
      if (hi < seg.length - 1) hi++
      if (seg[hi].t - seg[lo].t < MIN_SPAN_S && lo > 0) lo--
    }
    const dt = seg[hi].t - seg[lo].t
    return dt > 0 ? (dist[hi] - dist[lo]) / dt / METRES_PER_KNOT_SECOND : 0
  })
  return { t: seg.map((p) => p.t), dist, merc, sog, totalM: dist[dist.length - 1] ?? 0 }
}

/** The speed the top of the ramp stands for: what the passage stays under for
 * all but its fastest twentieth. */
function speedDomain(profiles: readonly SpeedProfile[]): number {
  const all = profiles.flatMap((p) => p.sog).sort((a, b) => a - b)
  if (all.length === 0) return MIN_DOMAIN_KTS
  return Math.max(MIN_DOMAIN_KTS, all[Math.min(all.length - 1, Math.floor(all.length * 0.95))])
}

/** The colour `t` (0 slow to 1 fast, held at the ends) of the way along a ramp. */
export function rampColour(ramp: readonly string[], t: number): string {
  const x = Math.max(0, Math.min(1, t)) * (ramp.length - 1)
  const i = Math.min(ramp.length - 2, Math.floor(x))
  return x <= 0 ? ramp[0] : x >= ramp.length - 1 ? ramp[ramp.length - 1] : mixHex(ramp[i], ramp[i + 1], x - i)
}

/** Where the segment is at a distance along it: [lon, lat], and the speed there. */
function atDistance(seg: TimedSegment, profile: SpeedProfile, d: number): { lon: number; lat: number; sog: number } {
  const { dist, sog } = profile
  if (d <= 0) return { lon: seg[0].lon, lat: seg[0].lat, sog: sog[0] }
  if (d >= profile.totalM) return { lon: seg[seg.length - 1].lon, lat: seg[seg.length - 1].lat, sog: sog[sog.length - 1] }
  const i = lowerBound(dist, d)
  const f = fraction(dist[i - 1], dist[i], d)
  return {
    lon: lerp(seg[i - 1].lon, seg[i].lon, f),
    lat: lerp(seg[i - 1].lat, seg[i].lat, f),
    sog: lerp(sog[i - 1], sog[i], f),
  }
}

/** The projected length at a distance along the segment. */
function mercAtDistance(profile: SpeedProfile, d: number): number {
  if (d <= 0) return 0
  if (d >= profile.totalM) return profile.merc[profile.merc.length - 1] ?? 0
  const i = lowerBound(profile.dist, d)
  return lerp(profile.merc[i - 1], profile.merc[i], fraction(profile.dist[i - 1], profile.dist[i], d))
}

/** The stops of a `line-gradient` for the first `extentM` metres of a
 * segment, as [progress along that length, colour], rising from 0 to 1. The
 * progress is along the line as the map draws it (projected), so a stop sits
 * where the speed was even on a long north-south run, where the projected line
 * stretches toward the poles; the speed is read between fixes on the ground. */
function speedGradientStops(profile: SpeedProfile, extentM: number, domainKts: number, ramp: readonly string[]): [number, string][] {
  const colour = (kts: number) => rampColour(ramp, kts / domainKts)
  const stops: [number, string][] = []
  const mercExtent = Math.max(mercAtDistance(profile, extentM), 1e-12)
  const lastFix = lowerBound(profile.dist, extentM)
  const upTo = Math.min(lastFix, profile.dist.length - 1)
  for (let i = 0; i < upTo; i++) {
    const p = Math.min(1, profile.merc[i] / mercExtent)
    if (stops.length === 0 || p - stops[stops.length - 1][0] >= MIN_STOP_GAP) stops.push([stops.length === 0 ? 0 : p, colour(profile.sog[i])])
  }
  // The sailed length ends between two fixes: the speed there is worked out between them.
  const endKts = lastFix <= 0 || lastFix >= profile.dist.length || profile.dist[lastFix] === extentM
    ? profile.sog[upTo]
    : lerp(profile.sog[lastFix - 1], profile.sog[lastFix], fraction(profile.dist[lastFix - 1], profile.dist[lastFix], extentM))
  if (stops.length === 0) stops.push([0, colour(profile.sog[0] ?? 0)])
  stops.push([1, colour(endKts)])
  return stops
}

export interface SpeedGradient {
  /** What it was built for: it is reused only for the same. */
  profile: SpeedProfile
  ramp: readonly string[]
  domainKts: number
  extentM: number
  stops: [number, string][]
  /** The paint expression for these stops, made once with them. */
  expression: ExpressionSpecification
}

/** The gradient for the sailed length: the one already built while the length
 * has grown by under 3 per cent, since the line's own progress stretches with
 * it and the colours shift by no more than that; a new one when it has grown
 * more, the replay has gone back, or the profile, the ramp (the theme) or the
 * top of the ramp is not the one it was built for. A caller that holds the last one rebuilds
 * the colours about 150 times over a whole passage instead of every frame. */
export function nextSpeedGradient(
  profile: SpeedProfile,
  extentM: number,
  prev: SpeedGradient | null,
  domainKts: number,
  ramp: readonly string[],
): SpeedGradient {
  if (prev && prev.profile === profile && prev.ramp === ramp && prev.domainKts === domainKts && extentM >= prev.extentM && extentM < prev.extentM * REBUILD_GROWTH) return prev
  const stops = speedGradientStops(profile, extentM, domainKts, ramp)
  return { profile, ramp, domainKts, extentM, stops, expression: ['interpolate', ['linear'], ['line-progress'], ...stops.flat()] as unknown as ExpressionSpecification }
}

/** The distance sailed along a segment by a time (Unix seconds). */
export function extentAt(profile: SpeedProfile, at: number): number {
  const { t, dist } = profile
  if (at <= t[0]) return 0
  if (at >= t[t.length - 1]) return profile.totalM
  const i = lowerBound(t, at)
  return lerp(dist[i - 1], dist[i], fraction(t[i - 1], t[i], at))
}

/** How long the wake is: four per cent of the passage, or 60 pixels at the
 * current zoom if that is longer so it never shrinks to nothing when zoomed
 * out, and never longer than the passage. */
function wakeLengthM(passageM: number, zoom: number, boatLatDeg: number): number {
  return Math.min(passageM, Math.max(passageM * WAKE_FRACTION, WAKE_MIN_PX * metersPerPixel(boatLatDeg, zoom)))
}

/** The wake: `lengthM` of track behind the boat, which has sailed `boatM` of
 * the segment, as [lon, lat] points ending on the boat. Null before the boat
 * has moved on this segment. */
function wakeAt(seg: TimedSegment, profile: SpeedProfile, boatM: number, lengthM: number): [number, number][] | null {
  if (seg.length < 2 || boatM <= 0) return null
  const from = Math.max(0, boatM - lengthM)
  const point = (d: number): [number, number] => { const p = atDistance(seg, profile, d); return [p.lon, p.lat] }
  const out: [number, number][] = [point(from)]
  const end = lowerBound(profile.dist, boatM)
  for (let i = lowerBound(profile.dist, from); i < end; i++) if (profile.dist[i] > from) out.push([seg[i].lon, seg[i].lat])
  out.push(point(boatM))
  return out
}

// ── a frame of it ────────────────────────────────────────────────────────

/** A drawn piece of a passage (one leg of it), as the map holds it. */
export type SpeedPiece = Pick<DrawnPiece, 'key' | 'passageId' | 'segments' | 'from' | 'to'>

export interface SpeedTrack {
  /** Per piece, per segment. */
  profiles: Map<string, SpeedProfile[]>
  /** Metres of track in each passage. */
  passageM: Map<string, number>
  /** Knots the top of the ramp stands for. */
  domainKts: number
}

/** The speeds along the track, or null for a track cut into more segments than
 * the speed colours take (MAX_SPEED_SEGMENTS). */
export function speedTrack(pieces: readonly SpeedPiece[]): SpeedTrack | null {
  if (pieces.reduce((n, p) => n + p.segments.length, 0) > MAX_SPEED_SEGMENTS) return null
  const profiles = new Map<string, SpeedProfile[]>()
  const passageM = new Map<string, number>()
  for (const p of pieces) {
    const mine = p.segments.map(speedProfile)
    profiles.set(p.key, mine)
    passageM.set(p.passageId, (passageM.get(p.passageId) ?? 0) + mine.reduce((m: number, q: SpeedProfile) => m + q.totalM, 0))
  }
  return { profiles, passageM, domainKts: speedDomain([...profiles.values()].flat()) }
}

export interface SpeedLine {
  /** The sailed line as GeoJSON, made once so a map is handed new data only when the line moved. */
  data: GeoJSON.Feature<GeoJSON.LineString>
  gradient: SpeedGradient
  /** Metres of the segment sailed, as of the frame it was drawn for. */
  extentM: number
}

export interface SpeedWake {
  data: GeoJSON.Feature<GeoJSON.LineString>
  /** Foam fading to nothing at the tail: the colour the boat is sailing in, lightened. */
  gradient: ExpressionSpecification
  colour: string
}

export interface SpeedFrame {
  /** Per piece, per segment: the sailed line and its gradient, or null where there is none. */
  lines: Map<string, (SpeedLine | null)[]>
  /** What each piece's lines were drawn for, so a piece the replay has left behind is not cut again. */
  drawnFor: Map<string, { segments: TimedSegment[]; at: number; end: { lat: number; lon: number } | undefined }>
  /** The ramp and the top of it these were coloured with. */
  ramp: readonly string[]
  domainKts: number
  wake: SpeedWake | null
}

/** The map as the wake sees it: its zoom. (Its latitude is the boat's.) */
export interface WakeView {
  zoom: number
}

/** What to draw of the sailed track at a frame: each sailed line with its
 * gradient, and the wake. `prev` is the last frame's, whose lines are kept
 * while nothing has moved and whose gradients are kept while the sailed length
 * has grown by under 3 per cent. `view` sizes the wake. */
export function speedFrameAt(
  pieces: readonly SpeedPiece[],
  track: SpeedTrack,
  sailedTo: ReadonlyMap<string, SailedTo>,
  ramp: readonly string[],
  prev: SpeedFrame | null,
  view: WakeView,
): SpeedFrame {
  const lines = new Map<string, (SpeedLine | null)[]>()
  const drawnFor = new Map<string, { segments: TimedSegment[]; at: number; end: { lat: number; lon: number } | undefined }>()
  // Lines are kept from the last frame only if they were drawn for the same ramp and top of it.
  const same = prev !== null && prev.ramp === ramp && prev.domainKts === track.domainKts
  let wake: SpeedWake | null = null
  for (const p of pieces) {
    const sailed = sailedTo.get(p.passageId)
    if (!sailed) continue
    const end = sailed.end && sailed.at >= p.from && sailed.at <= p.to ? sailed.end : undefined
    const profiles = track.profiles.get(p.key) ?? []
    const before = prev?.lines.get(p.key)
    const was = prev?.drawnFor.get(p.key)
    // A passage the replay has left behind is the lines it was: the same objects.
    const kept = same && before && was && was.segments === p.segments && was.at === sailed.at && was.end?.lat === end?.lat && was.end?.lon === end?.lon
    const mine = kept
      ? before
      : sailedLines(p.segments, sailed.at, end).map((coordinates, s) => {
          if (!coordinates) return null
          const extentM = extentAt(profiles[s], sailed.at)
          return {
            data: { type: 'Feature', properties: {}, geometry: { type: 'LineString', coordinates } } as GeoJSON.Feature<GeoJSON.LineString>,
            gradient: nextSpeedGradient(profiles[s], extentM, before?.[s]?.gradient ?? null, track.domainKts, ramp),
            extentM,
          }
        })
    lines.set(p.key, mine)
    drawnFor.set(p.key, { segments: p.segments, at: sailed.at, end })
    if (end) {
      let s = mine.length - 1
      while (s >= 0 && mine[s] === null) s--
      const line = s >= 0 ? mine[s] : null
      // The wake is the boat's: none once the segment is done and she is in the gap after it.
      const points = line && sailed.at <= profiles[s].t[profiles[s].t.length - 1] ? wakeAt(p.segments[s], profiles[s], line.extentM, wakeLengthM(track.passageM.get(p.passageId) ?? 0, view.zoom, end.lat)) : null
      if (line && points) {
        // Like the sailed line, the wake ends on the boat marker.
        points[points.length - 1] = [end.lon, end.lat]
        const colour = mixHex(line.gradient.stops[line.gradient.stops.length - 1][1], '#ffffff', WAKE_FOAM)
        wake = {
          data: { type: 'Feature', properties: {}, geometry: { type: 'LineString', coordinates: points } },
          gradient: ['interpolate', ['linear'], ['line-progress'], 0, hexAlpha(colour, 0), 1, hexAlpha(colour, 0.95)] as unknown as ExpressionSpecification,
          colour,
        }
      }
    }
  }
  return { lines, drawnFor, ramp, domainKts: track.domainKts, wake }
}
