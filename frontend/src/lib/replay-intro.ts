// The replay's opening move: a high jump over the track that comes down onto
// the opening framing. It is a pure function of the time spent in it, so the
// live replay applies it a frame at a time with jumpTo and an exporter can ask
// for any frame without a running map.
//
// The path is MapLibre's easeTo, which the replay used before: the same
// bezier(0.25, 0.1, 0.25, 1) easing, zoom and pitch eased directly, bearing the
// short way round, and the centre moved across the map in mercator units,
// sped up while the zoom changes so the target stays in view as the map
// closes in (the `speedup` term in MapLibre's handleEaseTo).

import type { Map as MapLibreMap } from 'maplibre-gl'

import { latOfMercatorY, lonOfMercatorX, mercatorX, mercatorY } from '@/lib/geo'
import { norm360, shortTurn, type Camera } from '@/lib/replay-camera'

/** The zoom the opening move starts from: the whole earth in view. */
export const INTRO_START_ZOOM = 1.5
/** Where the opening move starts: over the middle of the track, the whole earth in view. */
export const introStartCamera = (bounds: readonly [number, number, number, number], bearing = 0): Camera => ({
  center: [(bounds[0] + bounds[2]) / 2, (bounds[1] + bounds[3]) / 2], zoom: INTRO_START_ZOOM, pitch: 0, bearing,
})
/** How long the opening move takes. */
export const INTRO_MS = 3200

// ── the easing ───────────────────────────────────────────────────────────

/** CSS-style cubic bezier through (0,0), (p1x,p1y), (p2x,p2y), (1,1): the y for a given x. */
export function cubicBezier(p1x: number, p1y: number, p2x: number, p2y: number): (x: number) => number {
  const cx = 3 * p1x
  const bx = 3 * (p2x - p1x) - cx
  const ax = 1 - cx - bx
  const cy = 3 * p1y
  const by = 3 * (p2y - p1y) - cy
  const ay = 1 - cy - by
  const sampleX = (t: number) => ((ax * t + bx) * t + cx) * t
  const sampleY = (t: number) => ((ay * t + by) * t + cy) * t
  const slopeX = (t: number) => (3 * ax * t + 2 * bx) * t + cx
  const solveT = (x: number) => {
    let t = x
    for (let i = 0; i < 8; i++) {
      const err = sampleX(t) - x
      if (Math.abs(err) < 1e-6) return t
      const d = slopeX(t)
      if (Math.abs(d) < 1e-6) break
      t -= err / d
    }
    let lo = 0
    let hi = 1
    t = x
    while (lo < hi) {
      const err = sampleX(t)
      if (Math.abs(err - x) < 1e-6) return t
      if (x > err) lo = t
      else hi = t
      t = (hi - lo) * 0.5 + lo
      if (hi - lo < 1e-9) break
    }
    return t
  }
  return (x) => (x <= 0 ? 0 : x >= 1 ? 1 : sampleY(solveT(x)))
}

/** MapLibre's default easing. */
export const defaultEasing = cubicBezier(0.25, 0.1, 0.25, 1)

// ── the path ─────────────────────────────────────────────────────────────

/** `target` brought within half a turn of `from`, as MapLibre does so a move
 * never crosses the world the long way. */
const nearestTurn = (from: number, target: number) => target + 360 * Math.round((from - target) / 360)

/** The opening move from `from` to `to` as a function of the time spent in it.
 * What does not change over the move is worked out once. Before the start it
 * is `from`; from `durationMs` on it is `to`. */
export function makeIntroPath(from: Camera, to: Camera, durationMs: number = INTRO_MS): (elapsedMs: number) => Camera {
  const x0 = mercatorX(from.center[0])
  const y0 = mercatorY(from.center[1])
  const dx = mercatorX(nearestTurn(from.center[0], to.center[0])) - x0
  const dy = mercatorY(to.center[1]) - y0
  const finalScale = 2 ** (to.zoom - from.zoom)
  const base = to.zoom > from.zoom ? Math.min(2, finalScale) : Math.max(0.5, finalScale)
  const turn = shortTurn(from.bearing, to.bearing)
  return (elapsedMs) => {
    if (!(durationMs > 0) || elapsedMs >= durationMs) return to
    const k = defaultEasing(Math.max(0, elapsedMs) / durationMs)
    const factor = k * base ** (1 - k)
    return {
      center: [lonOfMercatorX(x0 + dx * factor), latOfMercatorY(y0 + dy * factor)],
      zoom: from.zoom + (to.zoom - from.zoom) * k,
      pitch: from.pitch + (to.pitch - from.pitch) * k,
      bearing: norm360(from.bearing + turn * k),
    }
  }
}

// ── the globe ────────────────────────────────────────────────────────────

/** A MapLibre projection, as `map.setProjection` takes it. */
export type MapProjection = NonNullable<Parameters<MapLibreMap['setProjection']>[0]>

export const MERCATOR: MapProjection = { type: 'mercator' }
/** The zoom by which the opening move has left the globe behind, at the latest. */
const GLOBE_BLEND_END_ZOOM = 5.5
const GLOBE_BLEND_SPAN = 2

export interface IntroProjection {
  projection: MapProjection
  /** The zoom from which the map is flat whatever else is set, or null when it already is. */
  flatAtZoom: number | null
}

/** The projection `elapsedMs` into the opening move. The move starts on the
 * globe and MapLibre blends it into the flat map by zoom, completing the
 * blend by `GLOBE_BLEND_END_ZOOM` or the zoom the move lands on, whichever is
 * lower, so the hand-off is done well before the camera lands and nothing
 * jumps. Once the move is over (`durationMs`) it is the flat map. */
export function introProjectionAt(to: Camera, elapsedMs: number, durationMs: number = INTRO_MS): IntroProjection {
  const end = Math.min(GLOBE_BLEND_END_ZOOM, to.zoom)
  if (!(durationMs > 0) || elapsedMs >= durationMs || end <= 0.1) return { projection: MERCATOR, flatAtZoom: null }
  return {
    projection: { type: ['interpolate', ['linear'], ['zoom'], Math.max(0, end - GLOBE_BLEND_SPAN), 'vertical-perspective', end, 'mercator'] },
    flatAtZoom: end,
  }
}
