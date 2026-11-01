// The light of the replay, worked out from the height of the sun: the sky and
// fog colours, the map's light and the relief shading. A pure function, so the
// live replay and a video of it paint the same sky. MapLibre paints with literal
// colours, as in lib/terrain.ts.

import { mixHex as mix } from '@/lib/hex-mix'
import type { SunPosition } from '@/lib/sun'
import { SKY_DAY } from '@/lib/terrain'

/** The MapLibre sky spec (see `sky()` in lib/terrain.ts). */
type SkySpec = typeof SKY_DAY

export interface ReplayLight {
  /** Same key, same light: lets a caller change the map only when this changes. */
  key: string
  sky: SkySpec
  /** MapLibre's `light`: placed on the sun, so it is anchored to the map. */
  light: { anchor: 'map'; position: [number, number, number]; color: string; intensity: number }
  hillshade: { direction: number; exaggeration: number }
  /** A colour laid over the whole base map: the dark of night, the amber of dusk. Over satellite imagery, which is dimmed itself, it takes `opacityOverImagery`. */
  tint: { colour: string; opacity: number; opacityOverImagery: number }
  /** Paint for the satellite picture, which a tint over a raster alone would only muddy. */
  raster: { 'raster-brightness-max': number; 'raster-saturation': number }
}

interface Stop {
  /** Sun altitude, degrees. */
  at: number
  sky: string
  horizon: string
  lightColor: string
  intensity: number
  shading: number
  tint: string
  tintOpacity: number
  brightness: number
  saturation: number
}

// Night, civil twilight (the sun 6 degrees down), sunrise or sunset, golden
// hour, day. Dawn and dusk run warm at the horizon; night is deep blue and
// keeps some light so the boat model stays readable.
const STOPS: readonly Stop[] = [
  { at: -12, sky: '#050b16', horizon: '#0f1f33', lightColor: '#8aa4d6', intensity: 0.15, shading: 0.15, tint: '#071433', tintOpacity: 0.75, brightness: 0.32, saturation: -0.4 },
  { at: -6, sky: '#1d2b4f', horizon: '#7a5a7e', lightColor: '#b7a8d9', intensity: 0.25, shading: 0.22, tint: '#363a88', tintOpacity: 0.4, brightness: 0.6, saturation: -0.22 },
  { at: 0, sky: '#4a6fa5', horizon: '#f2a65a', lightColor: '#ffb870', intensity: 0.35, shading: 0.5, tint: '#ffa64d', tintOpacity: 0.18, brightness: 0.95, saturation: 0 },
  { at: 6, sky: '#6f9fcf', horizon: '#f7d9a8', lightColor: '#ffd9a0', intensity: 0.45, shading: 0.5, tint: '#ffb866', tintOpacity: 0.12, brightness: 1, saturation: 0 },
  { at: 18, sky: SKY_DAY['sky-color'], horizon: SKY_DAY['horizon-color'], lightColor: '#ffffff', intensity: 0.5, shading: 0.35, tint: '#ffb866', tintOpacity: 0, brightness: 1, saturation: 0 },
]

/** Hillshade light direction when the sun is down: the app's usual north-west. */
const DEFAULT_DIRECTION = 335
const ALTITUDE_STEP = 0.5
const AZIMUTH_STEP = 2

/** Under satellite imagery the tint takes this share of its strength, since the imagery is dimmed itself. */
const IMAGERY_TINT_SHARE = 0.6

/** The sun rounded to half a degree of height and two of bearing. */
function rounded(sun: SunPosition) {
  return {
    altitude: Math.round(sun.altitudeDeg / ALTITUDE_STEP) * ALTITUDE_STEP,
    azimuth: ((Math.round(sun.azimuthDeg / AZIMUTH_STEP) * AZIMUTH_STEP) % 360 + 360) % 360,
  }
}

/** Names the light for a sun position without working it out: two suns with
 * the same key have the same `lightFor`. */
export function lightKey(sun: SunPosition): string {
  const { altitude, azimuth } = rounded(sun)
  return `${altitude}/${azimuth}`
}

/** The light for a sun position. The sun is rounded first, so a sun that has
 * barely moved gives an identical result. */
export function lightFor(sun: SunPosition): ReplayLight {
  const { altitude, azimuth } = rounded(sun)
  const first = STOPS[0]
  const last = STOPS[STOPS.length - 1]
  let from = first
  let to = first
  if (altitude >= last.at) { from = last; to = last } else if (altitude > first.at) {
    const i = STOPS.findIndex((s) => s.at > altitude)
    from = STOPS[i - 1]
    to = STOPS[i]
  }
  const t = to.at === from.at ? 0 : (altitude - from.at) / (to.at - from.at)
  const lerp = (a: number, b: number) => Math.round((a + (b - a) * t) * 1000) / 1000
  const sky = mix(from.sky, to.sky, t)
  const horizon = mix(from.horizon, to.horizon, t)
  const up = altitude > -6
  return {
    key: lightKey(sun),
    sky: { ...SKY_DAY, 'sky-color': sky, 'horizon-color': horizon, 'fog-color': horizon },
    light: {
      anchor: 'map',
      position: [1.5, azimuth, Math.min(90, Math.max(0, 90 - altitude))],
      color: mix(from.lightColor, to.lightColor, t),
      intensity: lerp(from.intensity, to.intensity),
    },
    hillshade: { direction: up ? azimuth : DEFAULT_DIRECTION, exaggeration: lerp(from.shading, to.shading) },
    tint: { colour: mix(from.tint, to.tint, t), opacity: lerp(from.tintOpacity, to.tintOpacity), opacityOverImagery: lerp(from.tintOpacity * IMAGERY_TINT_SHARE, to.tintOpacity * IMAGERY_TINT_SHARE) },
    raster: { 'raster-brightness-max': lerp(from.brightness, to.brightness), 'raster-saturation': lerp(from.saturation, to.saturation) },
  }
}
