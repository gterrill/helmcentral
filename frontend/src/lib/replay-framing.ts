// The overview framing of a replay: the camera that fits the whole track in
// the view, flat or tilted. It needs only a map-shaped object that can fit
// bounds and say how big it is, so the live map and a plain MapLibre map made
// for an export both build it the same way.

import { fitZoomForPitch } from '@/lib/pitch-fit'
import type { Camera } from '@/lib/replay-camera'

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
