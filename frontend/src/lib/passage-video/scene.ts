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
import type { RasterDEMSourceSpecification } from 'maplibre-gl'

import type { BoatLayer } from '@/lib/boat-layer'
import { OPENSEAMAP_TILES, SEAMARK_SOURCE_ID, STYLE_DARK, STYLE_LIGHT } from '@/lib/basemap'
import { boatMarkerKind, type BoatHull } from '@/lib/boat-model'
import { openOffscreenMap } from '@/lib/offscreen-map'
import type { DrawnPiece } from '@/lib/passage-legs'
import { passageColour } from '@/lib/passage-colours'
import { FRAME_TIMEOUT_MS, PassageVideoError, cancelled, ignoredMapError, sourceFailureMessage, timeoutMessage } from '@/lib/passage-video/errors'
import type { Point } from '@/lib/passage-video/overlay'
import { addSpecLayer, addWholeTrack, createFramePainter } from '@/lib/replay-overlays'
import type { Camera } from '@/lib/replay-camera'
import type { ReplayFrame } from '@/lib/replay-controller'
import { cruiseFramingFor, type CruiseFraming } from '@/lib/replay-framing'
import type { MapProjection } from '@/lib/replay-intro'
import { hillshadeLayer, imageryLayerProps, lightController, seamarkLayer, tintLayer } from '@/lib/replay-map-style'
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
  /** Each passage's own bounds, which a cruise frames it by. */
  passageBounds: { id: string; bounds: [number, number, number, number] | null }[]
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
  /** The whole track, and each passage, framed in this map's size and shape: portrait refits them. Null if they cannot be worked out. */
  framing(): CruiseFraming | null
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

    const origin = window.location.origin

    // From the bottom: the sea marks, the relief, the imagery, the time of day, the names, the track.
    map.addSource(SEAMARK_SOURCE_ID, { type: 'raster', tiles: [OPENSEAMAP_TILES], tileSize: 256 })
    addSpecLayer(map, seamarkLayer(), SEAMARK_SOURCE_ID)
    if (spec.threeD) {
      map.addSource(TERRAIN_SOURCE_ID, { ...terrainSource, tiles: [`${origin}${terrainSource.tiles[0]}`] } as RasterDEMSourceSpecification)
      addSpecLayer(map, hillshadeLayer(), TERRAIN_SOURCE_ID)
      map.setTerrain(terrainSetting)
    }
    if (spec.satellite) {
      const imagery = imageryLayerProps()
      map.addSource(IMAGERY_SOURCE_ID, worldImagerySource({ origin }))
      addSpecLayer(map, { id: WORLD_IMAGERY_LAYER_ID, type: 'raster', beforeId: imagery.beforeId, minzoom: imagery.minzoom, paint: worldImageryPaint(imagery.opacity) }, IMAGERY_SOURCE_ID)
    }
    addSpecLayer(map, tintLayer(null, spec.satellite))
    for (const layer of placeLabelLayers(spec.isDarkTheme, spec.satellite)) addSpecLayer(map, layer as never)
    addWholeTrack(map, spec.pieces, (p) => passageColour(p.colourIndex), spec.satellite)

    // The boat: a model on a 3D map with a hull, otherwise the render loop draws a dot over it.
    if (spec.threeD && spec.hull) {
      const [{ createBoatLayer }, three] = await Promise.all([import('@/lib/boat-layer'), import('three')])
      boat = createBoatLayer(spec.hull, { three, MercatorCoordinate: maplibregl.MercatorCoordinate })
      map.addLayer(boat)
    }
    const boatDot = boatMarkerKind({ threeD: spec.threeD, terrain: 'ok', hull: spec.hull, layerReady: boat !== null && !!map.getLayer(boat.id) }) === 'dot'
    const paint = createFramePainter({
      map, light, pieces: spec.pieces, colourOf: (p) => passageColour(p.colourIndex), satellite: spec.satellite, boatLayerId: () => boat?.id ?? null,
    })

    const show = (f: SceneFrame) => {
      map.jumpTo(f.camera)
      if (f.projection) map.setProjection(f.projection)
      paint(f.frame, f.playedIds)
      boat?.setPose(f.frame.boat ? { lat: f.frame.boat.lat, lon: f.frame.boat.lon, headingDeg: f.frame.heading } : null)
    }

    return {
      canvas: map.getCanvas(),
      boatDot,
      framing: () => cruiseFramingFor(map, spec.bounds, spec.passageBounds, spec.threeD ? REPLAY_3D_PITCH : 0),
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
