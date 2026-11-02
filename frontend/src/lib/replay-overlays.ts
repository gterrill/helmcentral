// What the replay puts on a plain MapLibre map, apart from the base map: the
// whole track, and for each frame the time of day, the faint whole track, the
// sailed track, the speed colours and the wake. Sources and layers are added
// when a frame first wants them, the data and the gradient changed only when
// they have, and taken off when a frame no longer does. The video's map
// (lib/passage-video/scene) and the exported page's (export-player/main) both
// paint here, so they draw the same thing the replay does and neither pays for
// a change that is not one.

import type { GeoJSONSource, LayerSpecification, Map as MapLibreMap } from 'maplibre-gl'

import type { DrawnPiece } from '@/lib/passage-legs'
import { createSailedCache, segmentsToGeoJSON } from '@/lib/passages'
import type { ReplayFrame } from '@/lib/replay-controller'
import {
  TINT_LAYER_ID, plainSailedOverlay, sailedEndFor, speedLineOverlay, tintLayer, wakeOverlay, wholeLineLayers, wholeLineOpacity,
  type LightController, type OverlaySpec,
} from '@/lib/replay-map-style'

/** What the overlays ask of a map. A maplibre-gl Map satisfies it. */
export type OverlayMap = Pick<MapLibreMap, 'addSource' | 'addLayer' | 'removeLayer' | 'removeSource' | 'getSource' | 'setPaintProperty'>

/** A layer spec carries `beforeId` as the page's <Layer> takes it; MapLibre takes it as the second argument of addLayer. */
export function addSpecLayer<L extends { id: string; type: string; beforeId?: string }>(map: Pick<MapLibreMap, 'addLayer'>, layer: L, source?: string) {
  const { beforeId, ...rest } = layer
  map.addLayer((source ? { ...rest, source } : rest) as unknown as LayerSpecification, beforeId)
}

/** A gradient expression, or anything else a paint property is set to, as a string to compare by. */
const gradientKey = (g: unknown) => (typeof g === 'string' ? g : JSON.stringify(g))

/** One overlay as it is on the map. */
interface DrawnOverlay {
  data: GeoJSON.Feature
  layerIds: string[]
  /** What each gradient layer was last given: the expression it was, and its key. */
  gradients: Map<string, { ref: unknown; key: string }>
}

/** Two features that are the same line: the same object, or (for a short one, such as the wake) the same points. */
function sameFeature(a: GeoJSON.Feature, b: GeoJSON.Feature): boolean {
  if (a === b) return true
  if (a.geometry.type !== 'LineString' || b.geometry.type !== 'LineString') return false
  const [ca, cb] = [a.geometry.coordinates, b.geometry.coordinates]
  return ca.length <= 64 && ca.length === cb.length && JSON.stringify(ca) === JSON.stringify(cb)
}

export interface Overlays {
  /** Puts the overlay on the map, or brings what is there up to date. */
  want(o: OverlaySpec): void
  /** Takes off every overlay not wanted since the last call. */
  settle(): void
}

export function createOverlays(map: OverlayMap): Overlays {
  const overlays = new Map<string, DrawnOverlay>()
  let wanted = new Set<string>()
  return {
    want(o) {
      wanted.add(o.source.id)
      const have = overlays.get(o.source.id)
      if (!have) {
        map.addSource(o.source.id, { type: 'geojson', data: o.source.data, ...(o.source.lineMetrics ? { lineMetrics: true } : {}) })
        for (const l of o.layers) addSpecLayer(map, l, o.source.id)
        overlays.set(o.source.id, {
          data: o.source.data,
          layerIds: o.layers.map((l) => l.id),
          gradients: new Map(o.layers.filter((l) => l.paint['line-gradient'] !== undefined).map((l) => [l.id, { ref: l.paint['line-gradient'], key: gradientKey(l.paint['line-gradient']) }])),
        })
        return
      }
      if (!sameFeature(have.data, o.source.data)) {
        (map.getSource(o.source.id) as GeoJSONSource).setData(o.source.data)
        have.data = o.source.data
      }
      for (const l of o.layers) {
        const g = l.paint['line-gradient']
        const last = have.gradients.get(l.id)
        if (g === undefined || !last || last.ref === g) continue
        last.ref = g
        const key = gradientKey(g)
        if (key === last.key) continue
        last.key = key
        map.setPaintProperty(l.id, 'line-gradient', g)
      }
    },
    settle() {
      for (const [id, have] of [...overlays]) {
        if (wanted.has(id)) continue
        for (const layerId of have.layerIds) map.removeLayer(layerId)
        map.removeSource(id)
        overlays.delete(id)
      }
      wanted = new Set()
    },
  }
}

/** The whole track, faint, a source and the lines of each piece of it. The sailed part is painted over it as it is sailed. */
export function addWholeTrack(map: Pick<MapLibreMap, 'addSource' | 'addLayer'>, pieces: readonly DrawnPiece[], colourOf: (piece: DrawnPiece) => string, satellite: boolean) {
  const opacity = wholeLineOpacity(false, false)
  for (const p of pieces) {
    const source = `passage-${p.key}`
    map.addSource(source, { type: 'geojson', data: segmentsToGeoJSON(p.segments) })
    for (const l of wholeLineLayers(p.key, colourOf(p), opacity.line, opacity.casing, satellite)) addSpecLayer(map, l, source)
  }
}

export interface FramePainterOptions {
  map: OverlayMap & Pick<MapLibreMap, 'getLayer' | 'getLayersOrder' | 'moveLayer'>
  /** Puts the sky, the light and the shading for the sun on the map (lightController). */
  light: LightController
  pieces: readonly DrawnPiece[]
  /** The leg's own colour, for a track too cut up for the speed colours. */
  colourOf: (piece: DrawnPiece) => string
  satellite: boolean
  /** The boat model's layer when there is one on the map; it is kept above everything painted. */
  boatLayerId: () => string | null
}

/** Paints a frame of the replay onto the map: the time of day (sky, light and
 * the tint over the chart), the faint whole track, the sailed track coloured by
 * speed or in the leg's colour, and the wake. Nothing is set that is already
 * so. The boat itself, its pose or its dot, is the caller's. */
export function createFramePainter(o: FramePainterOptions): (frame: ReplayFrame, played: ReadonlySet<string>) => void {
  const { map, light, pieces, colourOf, satellite } = o
  const overlays = createOverlays(map)
  const sailed = createSailedCache()
  const wholeOpacity = new Map<string, string>()
  let lastLight: unknown = Symbol('none')
  let lastTint = ''
  return (frame, played) => {
    if (frame.light !== lastLight) {
      lastLight = frame.light
      light.set(frame.light)
      const tint = tintLayer(frame.light, satellite).paint
      const key = `${tint['background-color']}|${tint['background-opacity']}`
      if (key !== lastTint) {
        lastTint = key
        map.setPaintProperty(TINT_LAYER_ID, 'background-color', tint['background-color'])
        map.setPaintProperty(TINT_LAYER_ID, 'background-opacity', tint['background-opacity'])
      }
    }

    // The faint whole track: brighter before the replay, dimmer once the sailed part is drawn over it.
    for (const p of pieces) {
      const { line, casing } = wholeLineOpacity(frame.sailedTo?.has(p.passageId) ?? false, played.has(p.passageId))
      const key = `${line}|${casing}`
      if (wholeOpacity.get(p.key) === key) continue
      wholeOpacity.set(p.key, key)
      map.setPaintProperty(`passage-line-${p.key}`, 'line-opacity', line)
      if (satellite) wholeLineLayers(p.key, '', line, casing, true).slice(1).forEach((l) => map.setPaintProperty(l.id, 'line-opacity', l.paint['line-opacity']))
    }

    for (const p of pieces) {
      const reached = frame.sailedTo?.get(p.passageId)
      if (!reached) continue
      if (frame.speed) frame.speed.lines.get(p.key)?.forEach((line, k) => { if (line) overlays.want(speedLineOverlay(p.key, k, line, satellite)) })
      else overlays.want(plainSailedOverlay(p.key, sailed.get(p.key, p.segments, reached.at, sailedEndFor(reached, p)), colourOf(p), satellite))
    }
    if (frame.speed?.wake) overlays.want(wakeOverlay(frame.speed.wake))
    overlays.settle()

    const boat = o.boatLayerId()
    if (boat && map.getLayer(boat) && map.getLayersOrder().at(-1) !== boat) map.moveLayer(boat)
  }
}

