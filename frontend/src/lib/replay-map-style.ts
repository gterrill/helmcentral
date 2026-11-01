// How the replay looks on a MapLibre map, apart from any React: the ids, the
// paint and the stacking order of what it adds to the map, and how the sky and
// light are put on it. The live map renders from these, and a plain MapLibre
// map made for a video can do the same.
//
// From the bottom of the stack to the top:
//   the base map, the sea marks, the hillshade, the satellite imagery,
//   TINT (the time of day over all of those),
//   the place names, the whole track (faint),
//   the sailed track (speed-coloured, or plain), WAKE, the boat.
// MapLibre stacks layers in the order they are added, so a caller adds the
// tint after the imagery and before the place names, and the sailed track and
// the wake after the whole track.

import type { ExpressionSpecification } from 'maplibre-gl'

import type { ReplayLight } from '@/lib/replay-light'
import type { SpeedLine, SpeedWake } from '@/lib/replay-speed'
import { HILLSHADE_LAYER_ID, hillshadePaint, skyFor } from '@/lib/terrain'
import { SAT_HANDOFF_END_ZOOM, SAT_HANDOFF_START_ZOOM, WORLD_IMAGERY_LAYER_ID } from '@/lib/world-imagery'

export const TINT_LAYER_ID = 'replay-tint'
export const WAKE_SOURCE_ID = 'replay-wake'
export const WAKE_LAYER_ID = 'replay-wake-line'

// ── the sky and light ────────────────────────────────────────────────────

type MapLight = { anchor: 'map' | 'viewport'; position: [number, number, number]; color: string; intensity: number }

/** MapLibre's own light, which the map goes back to when the replay's is taken away. */
const WHITE = '#ffffff'
const DEFAULT_LIGHT: MapLight = { anchor: 'viewport', color: WHITE, intensity: 0.5, position: [1.15, 210, 30] }
/** The imagery paint the replay changes, and MapLibre's values for it. */
const IMAGERY_DEFAULTS = { 'raster-brightness-max': 1, 'raster-saturation': 0 }
/** The relief shading the replay changes: the app's own paint, lit from the usual north-west. */
const HILLSHADE_DEFAULTS = { 'hillshade-illumination-anchor': 'viewport', 'hillshade-illumination-direction': 335, 'hillshade-exaggeration': hillshadePaint['hillshade-exaggeration'] }

/** The parts of a MapLibre map the sky and light touch. */
export interface StyledMap {
  setSky(sky: never): void
  setLight(light: MapLight): void
  getLayer(id: string): unknown
  getPaintProperty(layer: string, name: string): unknown
  setPaintProperty(layer: string, name: string, value: unknown): void
  on(type: 'style.load' | 'styledata', listener: () => void): unknown
  off(type: 'style.load' | 'styledata', listener: () => void): unknown
}

export interface LightScene {
  isDarkTheme: boolean
  /** The satellite imagery is on. */
  satellite: boolean
  /** The terrain is up, so there is relief to shade. */
  terrain: boolean
  /** The camera is tilted over the terrain, so there is a sky to paint. */
  camera3d: boolean
}

/** Paints a layer's property, if the layer is there and does not have it already. */
function paint(map: StyledMap, layer: string, values: Record<string, unknown>) {
  if (!map.getLayer(layer)) return
  for (const [name, value] of Object.entries(values)) {
    if (map.getPaintProperty(layer, name) !== value) map.setPaintProperty(layer, name, value)
  }
}

/** Puts the sky, the map light, the satellite dimming and the relief shading
 * for `light` on the map. The sky shows only over the tilted terrain, and
 * follows the app theme while there is no light; the light, the imagery and
 * the shading follow the sun. A new map style (light to dark) drops them: the
 * sky and light are put back on `style.load`, and the imagery and relief,
 * which the style adds a moment after that, whenever the style changes and
 * they are not as they should be. Returns the way to stop, which also puts
 * everything the light changed back to the map's defaults, so a night does
 * not outlast the replay. */
export function applyLight(map: StyledMap, light: ReplayLight | null, scene: LightScene): () => void {
  const layers = () => {
    if (!light) return
    if (scene.satellite) paint(map, WORLD_IMAGERY_LAYER_ID, light.raster)
    if (scene.terrain) {
      paint(map, HILLSHADE_LAYER_ID, {
        'hillshade-illumination-anchor': 'map',
        'hillshade-illumination-direction': light.hillshade.direction,
        'hillshade-exaggeration': light.hillshade.exaggeration,
      })
    }
  }
  const apply = () => {
    // The typings leave undefined (no sky) out; the runtime clears the sky with it.
    map.setSky((scene.camera3d ? (light ? light.sky : skyFor(scene.isDarkTheme)) : undefined) as never)
    if (!light) return
    map.setLight(light.light)
    layers()
  }
  apply()
  map.on('style.load', apply)
  // The layers a style adds after it has loaded are painted as they appear.
  if (light) map.on('styledata', layers)
  return () => {
    map.off('style.load', apply)
    map.off('styledata', layers)
    if (!light) return
    try {
      map.setLight(DEFAULT_LIGHT)
      paint(map, WORLD_IMAGERY_LAYER_ID, IMAGERY_DEFAULTS)
      paint(map, HILLSHADE_LAYER_ID, HILLSHADE_DEFAULTS)
    } catch {
      // The map is being taken down with the page; there is nothing left to put back.
    }
  }
}

// ── the layers ───────────────────────────────────────────────────────────

/** A layer as `<Layer>` or `map.addLayer` takes it. */
export interface LineLayerSpec {
  id: string
  type: 'line'
  layout: { 'line-cap': 'round'; 'line-join': 'round' }
  paint: Record<string, unknown>
  beforeId?: string
  minzoom?: number
}

export interface BackgroundLayerSpec {
  id: string
  type: 'background'
  paint: { 'background-color': string; 'background-opacity': number }
}

/** A GeoJSON source with the layers drawn from it, bottom first. */
export interface OverlaySpec {
  source: { id: string; data: GeoJSON.Feature; lineMetrics?: true }
  layers: LineLayerSpec[]
}

const ROUND = { 'line-cap': 'round', 'line-join': 'round' } as const

/** The white edge under a line over satellite imagery, fading in as the imagery comes in. */
function casingLayer(id: string, beforeId: string, opacity: number): LineLayerSpec {
  return {
    id,
    type: 'line',
    beforeId,
    layout: ROUND,
    minzoom: SAT_HANDOFF_START_ZOOM,
    paint: {
      'line-color': '#ffffff',
      'line-width': 6.5,
      'line-opacity': ['interpolate', ['linear'], ['zoom'], SAT_HANDOFF_START_ZOOM, 0, SAT_HANDOFF_END_ZOOM, opacity],
    },
  }
}

/** The time of day laid over the base map. Clear by day, and clear while there
 * is no light; eased where satellite imagery, which is dimmed itself, is under it. */
export function tintLayer(light: ReplayLight | null, satellite: boolean): BackgroundLayerSpec {
  return {
    id: TINT_LAYER_ID,
    type: 'background',
    paint: {
      'background-color': light ? light.tint.colour : '#000000',
      'background-opacity': light ? (satellite ? light.tint.opacityOverImagery : light.tint.opacity) : 0,
    },
  }
}

/** One segment's sailed line, coloured by speed along its own length. */
export function speedLineOverlay(pieceKey: string, segment: number, line: SpeedLine, satellite: boolean): OverlaySpec {
  const lineId = `passage-speed-line-${pieceKey}-${segment}`
  return {
    source: { id: `passage-speed-${pieceKey}-${segment}`, data: line.data, lineMetrics: true },
    layers: [
      { id: lineId, type: 'line', layout: ROUND, paint: { 'line-width': 3.5, 'line-opacity': 0.95, 'line-gradient': line.gradient.expression } },
      ...(satellite ? [casingLayer(`passage-speed-casing-${pieceKey}-${segment}`, lineId, 0.95)] : []),
    ],
  }
}

/** A piece's sailed track in the leg's own colour: what is drawn for a track
 * cut into more segments than the speed colours take. */
export function plainSailedOverlay(pieceKey: string, data: GeoJSON.Feature, colour: string, satellite: boolean): OverlaySpec {
  const lineId = `passage-sailed-line-${pieceKey}`
  return {
    source: { id: `passage-sailed-${pieceKey}`, data },
    layers: [
      { id: lineId, type: 'line', layout: ROUND, paint: { 'line-color': colour, 'line-width': 3.5, 'line-opacity': 0.95 } },
      ...(satellite ? [casingLayer(`passage-sailed-casing-${pieceKey}`, lineId, 0.95)] : []),
    ],
  }
}

/** The wake: wider than the sailed line, foam fading to nothing at its tail. */
export function wakeOverlay(wake: SpeedWake): OverlaySpec {
  return {
    source: { id: WAKE_SOURCE_ID, data: wake.data, lineMetrics: true },
    layers: [{ id: WAKE_LAYER_ID, type: 'line', layout: ROUND, paint: { 'line-width': 8, 'line-gradient': wake.gradient as ExpressionSpecification } }],
  }
}

/** The faint whole-track line of a piece, and its casing under imagery. */
export function wholeLineLayers(pieceKey: string, colour: string, opacity: number, casingOpacity: number, satellite: boolean): LineLayerSpec[] {
  const lineId = `passage-line-${pieceKey}`
  return [
    { id: lineId, type: 'line', layout: ROUND, paint: { 'line-color': colour, 'line-width': 3.5, 'line-opacity': opacity } },
    ...(satellite ? [casingLayer(`passage-casing-${pieceKey}`, lineId, casingOpacity)] : []),
  ]
}
