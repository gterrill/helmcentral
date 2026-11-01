// The map the video is drawn from: a plain MapLibre map at the video's size, in
// a container nobody sees, with the same base map, imagery, terrain, tint, place
// names, track, speed colours, wake and boat as the replay's own map
// (components/passages/passage-map.tsx), built from the same lib modules. It
// holds no React and no clock: the render loop tells it where the camera is and
// what the frame shows, then waits for it to finish drawing.
//
// Every frame is a few changes to a map that is then waited on, so what a frame
// costs is what it changes: nothing is set that is already so (the light, the
// tint, the whole-track opacity, a sailed line that has not moved), and the
// style's own 300 ms transitions are turned off, since the next picture is
// taken only when the map says it is still.

import maplibregl from 'maplibre-gl'
import type { GeoJSONSource, LayerSpecification, RasterDEMSourceSpecification } from 'maplibre-gl'

import type { BoatLayer } from '@/lib/boat-layer'
import { OPENSEAMAP_TILES, SEAMARK_SOURCE_ID, STYLE_DARK, STYLE_LIGHT } from '@/lib/basemap'
import { boatMarkerKind, type BoatHull } from '@/lib/boat-model'
import { openOffscreenMap } from '@/lib/offscreen-map'
import type { DrawnPiece } from '@/lib/passage-legs'
import { passageColour } from '@/lib/passage-colours'
import { FRAME_TIMEOUT_MS, PassageVideoError, cancelled, ignoredMapError, sourceFailureMessage, timeoutMessage } from '@/lib/passage-video/errors'
import type { Point } from '@/lib/passage-video/overlay'
import { createSailedCache, segmentsToGeoJSON } from '@/lib/passages'
import type { Camera } from '@/lib/replay-camera'
import type { ReplayFrame } from '@/lib/replay-controller'
import { overviewCameraFor } from '@/lib/replay-framing'
import type { MapProjection } from '@/lib/replay-intro'
import {
  TINT_LAYER_ID, hillshadeLayer, imageryLayerProps, lightController, plainSailedOverlay, sailedEndFor, seamarkLayer,
  speedLineOverlay, tintLayer, wakeOverlay, wholeLineLayers, wholeLineOpacity,
  type LineLayerSpec, type OverlaySpec,
} from '@/lib/replay-map-style'
import { hasWebGL2 } from '@/lib/webgl'
import { REPLAY_3D_MAX_PITCH, REPLAY_3D_PITCH, TERRAIN_SOURCE_ID, terrainSetting, terrainSource } from '@/lib/terrain'
import { IMAGERY_SOURCE_ID, WORLD_IMAGERY_LAYER_ID, worldImageryPaint, worldImagerySource } from '@/lib/world-imagery'
import { placeLabelLayers, BASE_VECTOR_SOURCE_ID } from '@/lib/place-label-layers'

/** What the map is made from. */
export interface SceneSpec {
  width: number
  height: number
  isDarkTheme: boolean
  satellite: boolean
  threeD: boolean
  hull: BoatHull | null
  pieces: readonly DrawnPiece[]
  bounds: [number, number, number, number]
  /** Where the camera starts: high over the track. */
  start: Camera
}

/** One frame of the map: where the camera is and what the replay shows. */
export interface SceneFrame {
  camera: Camera
  /** Set only when the projection changes. */
  projection: MapProjection | null
  frame: ReplayFrame
  /** The passages the replay has begun. */
  playedIds: ReadonlySet<string>
}

/** The map the video is drawn from. The render loop asks for nothing else of it. */
export interface VideoScene {
  /** The map's picture, as it stands after `settle`. */
  readonly canvas: CanvasImageSource
  /** The whole track framed in this map's size and shape: portrait refits it. Null if it cannot be worked out. */
  framing(): { overview: Camera } | null
  camera(): Camera
  zoom(): number
  /** The boat is drawn as a dot over the map, since there is no model. */
  readonly boatDot: boolean
  /** Where the map puts a point, in its pixels. */
  project(lon: number, lat: number): Point
  show(frame: SceneFrame): void
  /** Resolves once the map has drawn what `show` set and everything it asked for has arrived. Rejects if a source fails, or the frame takes too long. */
  settle(frameNo: number, total: number): Promise<void>
  /** Takes the map down. */
  dispose(): void
}

const LOAD_TIMEOUT_MS = 60_000
/** The fly-in crosses a dozen zoom levels; the tiles it passes stay, so the track's own are not fetched again. */
const TILE_CACHE_SIZE = 600
const TILE_CACHE_ZOOM_LEVELS = 12

const absolute = (path: string) => (/^https?:/.test(path) ? path : new URL(path, window.location.origin).href)

/** A gradient expression, or anything else a paint property is set to, as a string to compare by. */
const gradientKey = (g: unknown) => (typeof g === 'string' ? g : JSON.stringify(g))

/** The sailed track's pieces as they are on the map. */
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

export async function createVideoScene(spec: SceneSpec): Promise<VideoScene> {
  if (!hasWebGL2()) throw new PassageVideoError('unsupported', 'Map needs WebGL2, which this browser does not provide.')

  // Every error the map reports is a part of it that did not arrive, except
  // a sea mark tile that does not exist. The first is kept and stops the render.
  const offscreen = await openOffscreenMap({
    width: spec.width,
    height: spec.height,
    pixelRatio: 1,
    hostStyle: { position: 'fixed', left: '0', top: '0', opacity: '0', pointerEvents: 'none', zIndex: '-1' },
    map: {
      style: absolute(spec.isDarkTheme ? STYLE_DARK : STYLE_LIGHT),
      center: spec.start.center,
      zoom: spec.start.zoom,
      pitch: spec.start.pitch,
      bearing: spec.start.bearing,
      maxPitch: REPLAY_3D_MAX_PITCH,
      maxTileCacheSize: TILE_CACHE_SIZE,
      maxTileCacheZoomLevels: TILE_CACHE_ZOOM_LEVELS,
    },
    // A paint change would otherwise start a 300 ms transition, and the map is not still until it ends.
    transformStyle: (_previous, next) => ({ ...next, transition: { duration: 0, delay: 0 } }),
    loadTimeoutMs: LOAD_TIMEOUT_MS,
    loadTimeoutError: () => new PassageVideoError('source', "The map didn't load in a minute, so the boat may be offline. The video wasn't finished."),
    disposedError: cancelled,
    onError: (e) => {
      const status = e.error?.status
      if (ignoredMapError(e.sourceId, status)) return null
      console.error('Map error while making the video', e.sourceId, e.error)
      return new PassageVideoError('source', sourceFailureMessage(e.sourceId, status ? `HTTP ${status}` : e.error?.message ?? 'unknown error'))
    },
  })
  const { map } = offscreen

  const light = lightController(map, { isDarkTheme: spec.isDarkTheme, satellite: spec.satellite, terrain: spec.threeD, camera3d: spec.threeD })
  let boat: BoatLayer | null = null
  const dispose = () => {
    light.stop()
    offscreen.dispose()
  }

  try {
    if (!map.getSource(BASE_VECTOR_SOURCE_ID)) {
      throw new PassageVideoError('source', `The chart has no "${BASE_VECTOR_SOURCE_ID}" source to draw place names from. The video wasn't finished.`)
    }

    // The layer specs carry `beforeId` as the page's <Layer> takes it; MapLibre takes it as the second argument.
    const add = (layer: LineLayerSpec | { id: string; type: string; beforeId?: string; [k: string]: unknown }, source?: string) => {
      const { beforeId, ...rest } = layer
      map.addLayer((source ? { ...rest, source } : rest) as unknown as LayerSpecification, beforeId)
    }
    const origin = window.location.origin

    // From the bottom: the sea marks, the relief, the imagery, the time of day, the names, the track.
    map.addSource(SEAMARK_SOURCE_ID, { type: 'raster', tiles: [OPENSEAMAP_TILES], tileSize: 256 })
    add(seamarkLayer(), SEAMARK_SOURCE_ID)
    if (spec.threeD) {
      map.addSource(TERRAIN_SOURCE_ID, { ...terrainSource, tiles: [`${origin}${terrainSource.tiles[0]}`] } as RasterDEMSourceSpecification)
      add(hillshadeLayer(), TERRAIN_SOURCE_ID)
      map.setTerrain(terrainSetting)
    }
    if (spec.satellite) {
      const imagery = imageryLayerProps()
      map.addSource(IMAGERY_SOURCE_ID, worldImagerySource({ origin }))
      add({ id: WORLD_IMAGERY_LAYER_ID, type: 'raster', beforeId: imagery.beforeId, minzoom: imagery.minzoom, paint: worldImageryPaint(imagery.opacity) }, IMAGERY_SOURCE_ID)
    }
    add(tintLayer(null, spec.satellite) as never)
    for (const layer of placeLabelLayers(spec.isDarkTheme, spec.satellite)) add(layer as never)
    spec.pieces.forEach((p) => {
      map.addSource(`passage-${p.key}`, { type: 'geojson', data: segmentsToGeoJSON(p.segments) })
      for (const l of wholeLineLayers(p.key, passageColour(p.colourIndex), 0.45, 0.5, spec.satellite)) add(l, `passage-${p.key}`)
    })

    // The boat: a model on a 3D map with a hull, otherwise the render loop draws a dot over it.
    if (spec.threeD && spec.hull) {
      const [{ createBoatLayer }, three] = await Promise.all([import('@/lib/boat-layer'), import('three')])
      boat = createBoatLayer(spec.hull, { three, MercatorCoordinate: maplibregl.MercatorCoordinate })
      map.addLayer(boat)
    }
    const boatDot = boatMarkerKind({ threeD: spec.threeD, terrain: 'ok', hull: spec.hull, layerReady: boat !== null && !!map.getLayer(boat.id) }) === 'dot'
    const keepBoatOnTop = () => { if (boat && map.getLayer(boat.id) && map.getLayersOrder().at(-1) !== boat.id) map.moveLayer(boat.id) }

    // The sailed track, the speed colours and the wake come and go with the replay.
    const overlays = new Map<string, DrawnOverlay>()
    const syncOverlay = (o: OverlaySpec) => {
      const have = overlays.get(o.source.id)
      if (!have) {
        map.addSource(o.source.id, { type: 'geojson', data: o.source.data, ...(o.source.lineMetrics ? { lineMetrics: true } : {}) })
        for (const l of o.layers) add(l, o.source.id)
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
    }
    const dropOverlaysExcept = (keep: Set<string>) => {
      for (const [id, have] of [...overlays]) {
        if (keep.has(id)) continue
        for (const layerId of have.layerIds) map.removeLayer(layerId)
        map.removeSource(id)
        overlays.delete(id)
      }
    }

    const sailedCache = createSailedCache()
    const wholeOpacity = new Map<string, string>()
    let lastLight: unknown = Symbol('none')
    let lastTint = ''

    const show = (f: SceneFrame) => {
      const { frame } = f
      map.jumpTo(f.camera)
      if (f.projection) map.setProjection(f.projection)

      // The time of day: the sky, the light, the imagery and relief, and the tint over the chart.
      if (frame.light !== lastLight) {
        lastLight = frame.light
        light.set(frame.light)
        const tint = tintLayer(frame.light, spec.satellite).paint
        const key = `${tint['background-color']}|${tint['background-opacity']}`
        if (key !== lastTint) {
          lastTint = key
          map.setPaintProperty(TINT_LAYER_ID, 'background-color', tint['background-color'])
          map.setPaintProperty(TINT_LAYER_ID, 'background-opacity', tint['background-opacity'])
        }
      }

      // The faint whole track: brighter before the replay, dimmer once the sailed part is drawn over it.
      for (const p of spec.pieces) {
        const { line, casing } = wholeLineOpacity(frame.sailedTo?.has(p.passageId) ?? false, f.playedIds.has(p.passageId))
        const key = `${line}|${casing}`
        if (wholeOpacity.get(p.key) === key) continue
        wholeOpacity.set(p.key, key)
        map.setPaintProperty(`passage-line-${p.key}`, 'line-opacity', line)
        if (spec.satellite) {
          wholeLineLayers(p.key, '', line, casing, true).slice(1).forEach((l) => map.setPaintProperty(l.id, 'line-opacity', l.paint['line-opacity']))
        }
      }

      const wanted = new Set<string>()
      const want = (o: OverlaySpec) => { wanted.add(o.source.id); syncOverlay(o) }
      for (const p of spec.pieces) {
        const sailed = frame.sailedTo?.get(p.passageId)
        if (!sailed) continue
        if (frame.speed) frame.speed.lines.get(p.key)?.forEach((line, k) => { if (line) want(speedLineOverlay(p.key, k, line, spec.satellite)) })
        else want(plainSailedOverlay(p.key, sailedCache.get(p.key, p.segments, sailed.at, sailedEndFor(sailed, p)), passageColour(p.colourIndex), spec.satellite))
      }
      if (frame.speed?.wake) want(wakeOverlay(frame.speed.wake))
      dropOverlaysExcept(wanted)
      keepBoatOnTop()
      boat?.setPose(frame.boat ? { lat: frame.boat.lat, lon: frame.boat.lon, headingDeg: frame.heading } : null)
    }

    return {
      canvas: map.getCanvas(),
      boatDot,
      framing: () => {
        const overview = overviewCameraFor(map, spec.bounds, spec.threeD ? REPLAY_3D_PITCH : 0)
        return overview ? { overview } : null
      },
      camera: () => {
        const c = map.getCenter()
        return { center: [c.lng, c.lat], zoom: map.getZoom(), pitch: map.getPitch(), bearing: map.getBearing() }
      },
      zoom: () => map.getZoom(),
      project: (lon, lat) => {
        const p = map.project([lon, lat])
        return { x: p.x, y: p.y }
      },
      show,
      settle: (frameNo, total) => offscreen.settle(FRAME_TIMEOUT_MS, () => new PassageVideoError('timeout', timeoutMessage(frameNo + 1, total))),
      dispose,
    }
  } catch (err) {
    dispose()
    throw err
  }
}
