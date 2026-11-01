// Fitting a track into a tilted map. MapLibre's own fit works out the zoom for
// a flat camera; once the camera is pitched the near edge grows and the far
// edge shrinks and runs up the screen, so a flat fit can lose the ends of the
// track. This projects the corners the way MapLibre's camera does (vertical
// field of view 36.87 degrees, so the camera stands 1.5 screen heights from the
// centre) and backs the zoom off until they are all in.

import { mercatorX, mercatorY } from '@/lib/geo'

const WORLD_TILE = 512
const CAMERA_DISTANCE_PER_HEIGHT = 1.5
const ZOOM_STEP = 0.05

export interface Padding { top: number; bottom: number; left: number; right: number }
export interface LonLat { lon: number; lat: number }
export interface PitchedCamera extends LonLat { zoom: number; pitch: number; width: number; height: number }

function mercator(p: LonLat, zoom: number): { x: number; y: number } {
  const scale = WORLD_TILE * 2 ** zoom
  return { x: mercatorX(p.lon) * scale, y: mercatorY(p.lat) * scale }
}

/** Where a ground point lands on screen (pixels from the top left), north up,
 * or null when it is behind or beyond the horizon of the tilted camera. */
export function projectAtPitch(cam: PitchedCamera, point: LonLat): { x: number; y: number } | null {
  const c = mercator(cam, cam.zoom)
  const p = mercator(point, cam.zoom)
  const dx = p.x - c.x
  const dy = p.y - c.y
  const theta = (cam.pitch * Math.PI) / 180
  const d = CAMERA_DISTANCE_PER_HEIGHT * cam.height
  const depth = d - dy * Math.sin(theta)
  if (depth <= 0) return null
  const k = d / depth
  return { x: cam.width / 2 + dx * k, y: cam.height / 2 + dy * Math.cos(theta) * k }
}

export interface FitZoomInput {
  bounds: [number, number, number, number]
  centre: LonLat
  zoom: number
  pitch: number
  width: number
  height: number
  padding: Padding
}

/** The largest zoom at or below `zoom` that keeps all four corners of the
 * bounds inside the padded viewport at this pitch. A flat camera is returned
 * unchanged: MapLibre has already fitted it. */
export function fitZoomForPitch({ bounds, centre, zoom, pitch, width, height, padding }: FitZoomInput): number {
  if (pitch === 0) return zoom
  const corners: LonLat[] = [
    { lon: bounds[0], lat: bounds[1] }, { lon: bounds[2], lat: bounds[1] },
    { lon: bounds[2], lat: bounds[3] }, { lon: bounds[0], lat: bounds[3] },
  ]
  const fits = (z: number) => corners.every((corner) => {
    const s = projectAtPitch({ ...centre, zoom: z, pitch, width, height }, corner)
    return s !== null && s.x >= padding.left && s.x <= width - padding.right && s.y >= padding.top && s.y <= height - padding.bottom
  })
  let z = zoom
  while (z > 0 && !fits(z)) z = Math.max(0, z - ZOOM_STEP)
  return z
}
