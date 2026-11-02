// The overview framing of a replay: the camera that fits the whole track in
// the view, flat or tilted. It needs only a map-shaped object that can fit
// bounds and say how big it is, so the live map and a plain MapLibre map made
// for an export both build it the same way.

import { fitZoomForPitch } from '@/lib/pitch-fit'
import { isMultiPassage, segmentBounds, type TimedSegment, type Timeline } from '@/lib/passages'
import { blendCameras, easeInOut, type Camera } from '@/lib/replay-camera'

export type Bounds = [number, number, number, number]

export const FIT_PADDING = { top: 48, bottom: 80, left: 48, right: 48 }

/** What the framing asks of a map. A maplibre-gl Map satisfies it. */
export interface FitMap {
  cameraForBounds(
    bounds: [[number, number], [number, number]],
    options: { padding: typeof FIT_PADDING; maxZoom: number },
  ): { center?: unknown; zoom?: number } | undefined
  getContainer(): { clientWidth: number; clientHeight: number }
}

/** MapLibre's own fit of the bounds as a flat camera: the centre as lon/lat and
 * the zoom, or null when it cannot be worked out. */
export function flatFit(map: FitMap, bounds: Bounds): { centre: { lon: number; lat: number }; zoom: number } | null {
  const fit = map.cameraForBounds([[bounds[0], bounds[1]], [bounds[2], bounds[3]]], { padding: FIT_PADDING, maxZoom: 14 })
  if (!fit || !fit.center) return null
  const fc = fit.center as [number, number] | { lng: number; lat: number } | { lon: number; lat: number }
  const centre = Array.isArray(fc) ? { lon: fc[0], lat: fc[1] } : 'lng' in fc ? { lon: fc.lng, lat: fc.lat } : { lon: fc.lon, lat: fc.lat }
  return { centre, zoom: fit.zoom ?? 0 }
}

/** The whole track framed at `pitch`. Pitched, MapLibre's flat fit loses the
 * near and far ends, so the zoom is backed off until every corner is inside
 * the padding. Null when flat (pitch 0) or when no fit can be worked out. */
export function pitchedFit(map: FitMap, bounds: Bounds, pitch: number): Camera | null {
  if (pitch === 0) return null
  const flat = flatFit(map, bounds)
  if (!flat) return null
  const c = map.getContainer()
  const zoom = fitZoomForPitch({
    bounds, centre: flat.centre, zoom: flat.zoom, pitch, width: c.clientWidth, height: c.clientHeight, padding: FIT_PADDING,
  })
  return { center: [flat.centre.lon, flat.centre.lat], zoom, pitch, bearing: 0 }
}

/** The whole-track framing as a camera, flat or pitched; null when it cannot
 * be worked out. */
export function overviewCameraFor(map: FitMap, bounds: Bounds, pitch: number): Camera | null {
  const pitched = pitchedFit(map, bounds, pitch)
  if (pitched) return pitched
  const flat = flatFit(map, bounds)
  return flat ? { center: [flat.centre.lon, flat.centre.lat], zoom: flat.zoom, pitch: 0, bearing: 0 } : null
}

// ── framing a cruise ─────────────────────────────────────────────────────
// A replay of several passages frames each passage on its own, and pulls out
// to the whole cruise during a stay and back in on the next passage. These are
// pure functions of where the replay stands (the passage, the stay and how far
// through it), given the cameras the map worked out for its size: so the live
// map and a video's map, in landscape or portrait, place the same camera.

/** The whole-cruise framing, and each passage framed on its own. */
export interface CruiseFraming {
  overview: Camera
  /** By passage id (the timeline entry's). */
  passages: ReadonlyMap<string, Camera>
}

/** Each passage's own bounds, for a replay of several; none for a single
 * passage, whose one framing is the whole track. A passage with no drawn track
 * has no bounds. The live map and a video both plan with this. */
export function passageBoundsOf(passages: readonly { id: string; segments: TimedSegment[] }[]): { id: string; bounds: Bounds | null }[] {
  return passages.length > 1 ? passages.map((p) => ({ id: p.id, bounds: segmentBounds(p.segments) })) : []
}

/** The whole cruise and each passage fitted to the map the way a single
 * passage is (same padding and pitch rules). Null when the whole cruise cannot
 * be worked out. A passage that cannot be framed on its own (it has no track,
 * or no fit) is left out of `passages`, and the camera uses the whole cruise
 * while it plays. */
export function cruiseFramingFor(map: FitMap, whole: Bounds, passages: readonly { id: string; bounds: Bounds | null }[], pitch: number): CruiseFraming | null {
  const overview = overviewCameraFor(map, whole, pitch)
  if (!overview) return null
  const out = new Map<string, Camera>()
  for (const p of passages) {
    const cam = p.bounds ? overviewCameraFor(map, p.bounds, pitch) : null
    if (cam) out.set(p.id, cam)
  }
  return { overview, passages: out }
}

/** Where a stay is, as the camera needs it. */
export interface StayFrame {
  fromId: string
  toId: string
  /** 0 at the start of the stay to 1 at its end. */
  progress: number
}

/** The stay's three parts: out to the whole cruise, held wide, back in. */
type StayPhase = 'out' | 'wide' | 'in'

function stayPhase(progress: number): StayPhase {
  return progress < 1 / 3 ? 'out' : progress < 2 / 3 ? 'wide' : 'in'
}

export interface CruiseWhere {
  passageId: string
  stay: StayFrame | null
  /** In the last moments of the replay: how far the ease out to the whole cruise has got (0 to 1). */
  endProgress?: number
}

/** The camera for a frame of a replay of several passages. On a passage, that
 * passage framed on its own. In a stay, a third of it pulling out from the
 * camera in force at the arrival (`from`, or the arriving passage's framing) to
 * the whole cruise, a third held wide, a third coming in on the passage that
 * follows, so the route so far builds up on the screen. In the last moments
 * the last passage's framing eases out to the whole cruise, and the finished
 * replay rests on it. Reduced motion cuts between the parts. A passage with no
 * framing of its own is framed on the whole cruise. */
export function cruiseCamera(framing: CruiseFraming, where: CruiseWhere, reduced: boolean, from?: Camera): { camera: Camera; key: string } {
  const { stay, endProgress } = where
  const own = (id: string) => framing.passages.get(id) ?? framing.overview
  if (!stay) {
    const camera = own(where.passageId)
    if (endProgress === undefined) return { camera, key: `passage:${where.passageId}` }
    if (reduced) return endProgress >= 1 ? { camera: framing.overview, key: 'end' } : { camera, key: `passage:${where.passageId}` }
    return { camera: blendCameras(camera, framing.overview, easeInOut(endProgress)), key: 'end' }
  }
  const key = `stay:${stay.fromId}>${stay.toId}`
  const start = from ?? own(stay.fromId)
  const to = own(stay.toId)
  const phase = stayPhase(stay.progress)
  const t = (begin: number) => (reduced ? 1 : easeInOut((stay.progress - begin) * 3))
  if (phase === 'out') return { camera: reduced ? start : blendCameras(start, framing.overview, t(0)), key: reduced ? `${key}:out` : key }
  if (phase === 'wide') return { camera: framing.overview, key: reduced ? `${key}:wide` : key }
  return { camera: reduced ? to : blendCameras(framing.overview, to, t(2 / 3)), key: reduced ? `${key}:in` : key }
}

/** Where the opening fly-in lands: on the first passage of a replay of several,
 * on the whole track for a single one. */
export function introLanding(framing: { overview: Camera; passages?: ReadonlyMap<string, Camera> }, timeline: Timeline): Camera {
  return (isMultiPassage(timeline) ? framing.passages?.get(timeline.entries[0].id) : undefined) ?? framing.overview
}
