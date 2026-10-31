import 'maplibre-gl/dist/maplibre-gl.css'
import maplibregl from 'maplibre-gl'
import { Maximize, Minus, Mountain, Plus } from 'lucide-react'
import type { ReactNode } from 'react'
import { forwardRef, Fragment, useCallback, useEffect, useImperativeHandle, useMemo, useRef, useState } from 'react'
import type { MapRef } from 'react-map-gl/maplibre'
import { Layer, Map, Marker, Source } from 'react-map-gl/maplibre'

import { MapPlaceLabels, warnIfBaseVectorSourceMissing } from '@/components/map-place-labels'
import { MapIconButton } from '@/components/map-icon-button'
import { MapToggleButton } from '@/components/map-toggle-button'
import { ImageryToggleButton, WorldImageryLayer } from '@/components/world-imagery'
import { useCollapsedMapAttribution } from '@/hooks/use-collapsed-map-attribution'
import type { BoatLayer } from '@/lib/boat-layer'
import { boatMarkerKind, type BoatHull } from '@/lib/boat-model'
import { OPENSEAMAP_TILES, STYLE_DARK, STYLE_LIGHT } from '@/lib/basemap'
import { use3dPreference } from '@/lib/passage-3d-preference'
import { useImageryPreference } from '@/lib/passage-imagery-preference'
import { insetZoom } from '@/lib/passage-export-style'
import { offsetInsetZoom, type InsetCorner } from '@/lib/passage-inset-view'
import { fitZoomForPitch } from '@/lib/pitch-fit'
import { CameraGate, outsideCentral, type Camera, type CameraResult } from '@/lib/replay-camera'
import { legsOf, legSegments, type PassageLeg } from '@/lib/passage-legs'
import { createSailedCache, segmentBounds, segmentsToGeoJSON, type TimedSegment } from '@/lib/passages'
import {
  hillshadePaint,
  REPLAY_3D_MAX_PITCH,
  REPLAY_3D_PITCH,
  skyFor,
  TERRAIN_SOURCE_ID,
  terrainSetting,
  terrainSource,
} from '@/lib/terrain'
import { hasWebGL2 } from '@/lib/webgl'
import { SAT_HANDOFF_END_ZOOM, SAT_HANDOFF_START_ZOOM, WORLD_IMAGERY_OPACITY_EXPRESSION } from '@/lib/world-imagery'
import { cn } from '@/lib/utils'

// Map line colours cannot be CSS tokens (MapLibre paints with literal
// colours), so the passages share one fixed palette, light enough for the
// dark basemap and dark enough for Positron. A single passage uses the first.
export const PASSAGE_COLOURS = ['#0284c7', '#d97706', '#059669', '#c026d3', '#e11d48', '#4f46e5'] as const
export const passageColour = (index: number) => PASSAGE_COLOURS[index % PASSAGE_COLOURS.length]

export interface MapPassage {
  id: string
  segments: TimedSegment[]
}

export interface PassageMapHandle {
  /** The opening move: jump high over the track and come down onto it.
   * Resolves when the camera has settled. */
  flyIn: () => Promise<void>
  /** The camera as the operator has it, for a new camera shot. `pitch` is null
   * while the map is flat, since a flat map has no pitch of its own to keep. */
  getView: () => { zoom: number; pitch: number | null; bearing: number } | null
}

/** What the replay asks of the camera. The map places the camera when `clock`
 * changes: not when a shot is edited, so editing never moves the view. */
export interface CameraDriver {
  clock: number
  /** The camera for the clock, given the map's whole-track framing, or null
   * when no shot applies. */
  resolve: (env: { flat: boolean; overview: Camera; current: Camera }) => CameraResult | null
  /** Reduced motion: cut to a framing at each shot start instead of moving. */
  reduced: boolean
  /** Bumped when the operator takes control back (Play, a scrub). */
  resume: number
}

/** How far a passage has been sailed: the real instant, and the boat marker's
 * position when the boat is on this passage, which the line ends on. */
export interface SailedTo {
  at: number
  end?: { lat: number; lon: number }
}

export interface PassageMapProps {
  passages: MapPassage[]
  /** Passages already replayed in full are drawn solid; the rest are dimmed. */
  playedIds: ReadonlySet<string>
  /** While replaying: for each passage, the real instant (Unix seconds) the
   * replay has sailed to. The track is drawn solid up to it and dim beyond.
   * Null when not replaying: tracks are drawn as a whole. */
  sailedTo?: ReadonlyMap<string, SailedTo> | null
  /** Where the boat marker stands, or null before there is anything to show. */
  boat: { lat: number; lon: number } | null
  /** The bow's bearing in degrees, or null when the track has not said yet. */
  boatHeading?: number | null
  /** The hull to draw as a 3D model in place of the dot; null keeps the dot. */
  hull?: BoatHull | null
  /** Camera shots, when the replay has any. Null leaves the camera alone. */
  cameraDriver?: CameraDriver | null
  /** Told when the camera is flat (3D off, or the terrain failed) or tilting. */
  onFlatChange?: (flat: boolean) => void
  isDarkTheme: boolean
  className?: string
  /** The overview map, drawn in `insetCorner` clear of the map's own buttons,
   * notices and credit. */
  inset?: ReactNode
  /** Where `inset` sits; top right when absent. */
  insetCorner?: InsetCorner
  /** Map markers drawn over the track: the replay's signs. */
  markers?: ReactNode
  /** The legs each passage is drawn in, one colour per leg. Without it every
   * passage is one leg, coloured by its place in the list. */
  legs?: readonly PassageLeg[]
  /** Greys the zoom and fit buttons: the replay owns the camera while it plays. */
  controlsDisabled?: boolean
}

const INTRO_START_ZOOM = 1.5
const INTRO_MS = 3200
const FIT_PADDING = { top: 48, bottom: 80, left: 48, right: 48 }
const LEVEL_MS = 600
const ZOOM_STEP_MS = 250
/** How far a pointer moves while down before it counts as a drag. */
const DRAG_YIELD_PX = 4
/** How long a failed terrain waits before the map tries it again. */
export const TERRAIN_RETRY_MS = 60_000

function readCamera(map: MapInstance): Camera {
  const c = map.getCenter()
  return { center: [c.lng, c.lat], zoom: map.getZoom(), pitch: map.getPitch(), bearing: map.getBearing() }
}

type TerrainState = 'ok' | 'failed' | 'retrying'

type MapInstance = ReturnType<NonNullable<MapRef['getMap']>>

/** MapLibre's own fit of the bounds as a flat camera: the centre as lon/lat and
 * the zoom, or null when it cannot be worked out. */
function flatFit(map: MapInstance, bounds: [number, number, number, number]): { centre: { lon: number; lat: number }; zoom: number } | null {
  const fit = map.cameraForBounds([[bounds[0], bounds[1]], [bounds[2], bounds[3]]], { padding: FIT_PADDING, maxZoom: 14 })
  if (!fit || !fit.center) return null
  const fc = fit.center
  const centre = Array.isArray(fc) ? { lon: fc[0], lat: fc[1] } : 'lng' in fc ? { lon: fc.lng, lat: fc.lat } : { lon: fc.lon, lat: fc.lat }
  return { centre, zoom: fit.zoom ?? 0 }
}

function centreOf(bounds: [number, number, number, number]) {
  return { longitude: (bounds[0] + bounds[2]) / 2, latitude: (bounds[1] + bounds[3]) / 2 }
}

export const PassageMap = forwardRef<PassageMapHandle, PassageMapProps>(function PassageMap(
  { passages, playedIds, sailedTo = null, boat, boatHeading = null, hull = null, cameraDriver = null, onFlatChange, isDarkTheme, className, inset, insetCorner = 'top-right', markers, legs, controlsDisabled = false },
  ref,
) {
  const mapRef = useRef<MapRef>(null)
  const collapseAttribution = useCollapsedMapAttribution(mapRef)
  const canRender = hasWebGL2()
  const bounds = useMemo(() => segmentBounds(passages.flatMap((p) => p.segments)), [passages])
  const boundsKey = bounds ? bounds.join(',') : ''
  const loadedRef = useRef(false)
  const [satellite, setSatellite] = useImageryPreference()
  const [threeD, setThreeD] = use3dPreference()
  // 'failed': terrain is off and the map is flat. 'retrying': terrain is back
  // on, the notice stays until a terrain tile actually loads.
  const [terrain, setTerrain] = useState<TerrainState>('ok')
  const terrainOn = threeD && terrain !== 'failed'
  // The camera only tilts while the terrain is really there.
  const camera3d = threeD && terrain === 'ok'
  const pitch = camera3d ? REPLAY_3D_PITCH : 0
  // Pitch and rotate stay available until the camera has finished levelling,
  // so turning 3D off eases down instead of snapping.
  const [levelling, setLevelling] = useState(false)
  const tiltable = camera3d || levelling
  // The boat model is a custom layer, loaded on demand: three.js is a
  // separate chunk fetched only when 3D replay is on and a hull type is set.
  const boatLayerRef = useRef<BoatLayer | null>(null)
  const [layerReady, setLayerReady] = useState(false)
  const [mapLoaded, setMapLoaded] = useState(false)
  const wantModel = camera3d && hull !== null && mapLoaded
  const hullType = hull?.type
  const hullLoa = hull?.loaM ?? null
  const hullBeam = hull?.beamM ?? null
  useEffect(() => {
    const map = mapRef.current?.getMap?.()
    if (!wantModel || !map || !hullType) return
    let cancelled = false
    let layer: BoatLayer | null = null
    // Adds the layer if the map does not have it. A new map style (light to
    // dark) drops custom layers, so this runs again on styledata. A failure
    // goes in the console, the dot comes back, and no half-added layer stays.
    // The model stays the top layer: the sailed line mounts when replay
    // starts, after the model, and would otherwise be drawn over it.
    const add = () => {
      if (!layer) return
      if (map.getLayer(layer.id)) {
        try {
          if (map.getLayersOrder().at(-1) !== layer.id) map.moveLayer(layer.id)
        } catch (err) {
          console.error('Boat model could not be kept above the track', err)
        }
        return
      }
      try {
        map.addLayer(layer)
      } catch (err) {
        console.error('Boat model could not be added to the map', err)
        try { if (map.getLayer(layer.id)) map.removeLayer(layer.id) } catch { /* nothing more to undo */ }
        setLayerReady(false)
      }
    }
    void Promise.all([import('@/lib/boat-layer'), import('three')]).then(([{ createBoatLayer }, three]) => {
      if (cancelled) return
      layer = createBoatLayer({ type: hullType, loaM: hullLoa, beamM: hullBeam }, { three, MercatorCoordinate: maplibregl.MercatorCoordinate })
      boatLayerRef.current = layer
      add()
      map.on('styledata', add)
      setLayerReady(!!map.getLayer(layer.id))
    }).catch((err) => {
      console.error('Boat model unavailable', err)
    })
    return () => {
      cancelled = true
      map.off('styledata', add)
      if (layer && map.getLayer(layer.id)) map.removeLayer(layer.id)
      boatLayerRef.current = null
      setLayerReady(false)
    }
  }, [wantModel, hullType, hullLoa, hullBeam])
  // The model follows the replay cursor straight into the layer, which asks the
  // map for a frame itself; React does not draw it.
  useEffect(() => {
    if (layerReady) boatLayerRef.current?.setPose(boat ? { lat: boat.lat, lon: boat.lon, headingDeg: boatHeading } : null)
  }, [layerReady, boat, boatHeading])
  const marker = boatMarkerKind({ threeD, terrain, hull: hull ?? null, layerReady })
  const sailedCache = useMemo(() => createSailedCache(), [])
  // What is drawn: a piece of track per leg. The first piece of a passage is
  // open at its start and the last at its end, as the sailed line reads it.
  const pieces = useMemo(() => {
    const out: { key: string; passageId: string; segments: TimedSegment[]; colourIndex: number; from: number; to: number }[] = []
    passages.forEach((p, i) => {
      const mine = legs ? legsOf(legs, p.id) : []
      if (mine.length === 0) {
        out.push({ key: p.id, passageId: p.id, segments: p.segments, colourIndex: i, from: -Infinity, to: Infinity })
        return
      }
      mine.forEach((leg, k) => out.push({
        key: mine.length === 1 ? p.id : `${p.id}-${k}`,
        passageId: p.id,
        segments: legSegments(p.segments, mine, k),
        colourIndex: leg.colourIndex,
        from: k === 0 ? -Infinity : leg.fromMs / 1000,
        to: k === mine.length - 1 ? Infinity : leg.toMs / 1000,
      }))
    })
    return out
  }, [passages, legs])
  const fullLines = useMemo(() => pieces.map((p) => segmentsToGeoJSON(p.segments)), [pieces])

  // Frames the whole track. Pitched, MapLibre's flat fit loses the near and far
  // ends, so the zoom is backed off until every corner is inside the padding.
  const cameraFor = useCallback((map: MapInstance) => {
    if (!bounds || pitch === 0) return null
    const flat = flatFit(map, bounds)
    if (!flat) return null
    const c = map.getContainer()
    const zoom = fitZoomForPitch({
      bounds, centre: flat.centre, zoom: flat.zoom, pitch, width: c.clientWidth, height: c.clientHeight, padding: FIT_PADDING,
    })
    return { center: [flat.centre.lon, flat.centre.lat] as [number, number], zoom, pitch, bearing: 0 }
  }, [bounds, pitch])

  // On load, and whenever the passages change, the whole track is framed.
  const frame = useCallback(() => {
    const map = mapRef.current?.getMap?.()
    if (!map || !bounds) return
    const cam = cameraFor(map)
    if (cam) { map.jumpTo(cam); return }
    map.fitBounds([[bounds[0], bounds[1]], [bounds[2], bounds[3]]], { padding: FIT_PADDING, duration: 0, maxZoom: 14 })
  }, [bounds, cameraFor])

  const zoomBy = useCallback((step: number) => {
    const map = mapRef.current?.getMap?.()
    if (map) map.easeTo({ zoom: map.getZoom() + step, duration: ZOOM_STEP_MS })
  }, [])

  // The same framing as `frame`, eased there for a button press.
  const fitTrack = useCallback(() => {
    const map = mapRef.current?.getMap?.()
    if (!map || !bounds) return
    const cam = cameraFor(map)
    if (cam) map.easeTo({ ...cam, duration: ZOOM_STEP_MS })
    else map.fitBounds([[bounds[0], bounds[1]], [bounds[2], bounds[3]]], { padding: FIT_PADDING, duration: ZOOM_STEP_MS, maxZoom: 14 })
  }, [bounds, cameraFor])

  // The whole-track framing as a camera, flat or pitched. Remembered per
  // track, pitch and size: working it out searches for a zoom.
  const overviewCache = useRef<{ key: string; camera: Camera } | null>(null)
  const overviewCamera = useCallback((map: MapInstance): Camera | null => {
    if (!bounds) return null
    const c = map.getContainer()
    const key = `${boundsKey}|${pitch}|${c.clientWidth}x${c.clientHeight}`
    if (overviewCache.current?.key === key) return overviewCache.current.camera
    let camera: Camera | null = cameraFor(map)
    if (!camera) {
      const flat = flatFit(map, bounds)
      if (!flat) return null
      camera = { center: [flat.centre.lon, flat.centre.lat], zoom: flat.zoom, pitch: 0, bearing: 0 }
    }
    overviewCache.current = { key, camera }
    return camera
  }, [bounds, boundsKey, pitch, cameraFor])

  useImperativeHandle(ref, () => ({
    getView: () => {
      const map = mapRef.current?.getMap?.()
      if (!map) return null
      return { zoom: map.getZoom(), pitch: camera3d ? map.getPitch() : null, bearing: map.getBearing() }
    },
    flyIn: () => new Promise<void>((resolve) => {
      const map = mapRef.current?.getMap?.()
      if (!map || !bounds) { resolve(); return }
      const centre = centreOf(bounds)
      map.jumpTo({ center: [centre.longitude, centre.latitude], zoom: INTRO_START_ZOOM, pitch: 0 })
      map.once('moveend', () => resolve())
      const cam = cameraFor(map)
      if (cam) map.easeTo({ ...cam, duration: INTRO_MS })
      else map.fitBounds([[bounds[0], bounds[1]], [bounds[2], bounds[3]]], { padding: FIT_PADDING, duration: INTRO_MS, maxZoom: 14 })
    }),
  }), [bounds, cameraFor, camera3d])

  const handleLoad = useCallback(() => {
    loadedRef.current = true
    setMapLoaded(true)
    const map = mapRef.current?.getMap?.()
    // Fail fast: the place labels draw from the style's own vector source.
    if (map) warnIfBaseVectorSourceMissing(map)
    frame()
  }, [frame])

  // The camera tilts up when the terrain is in and levels out when 3D goes off
  // or the terrain fails; the gestures are taken away once it has levelled.
  const previousCamera3d = useRef(camera3d)
  useEffect(() => {
    if (previousCamera3d.current === camera3d) return
    previousCamera3d.current = camera3d
    const map = mapRef.current?.getMap?.()
    if (!loadedRef.current || !map) return
    if (camera3d) {
      setLevelling(false)
      map.easeTo({ pitch: REPLAY_3D_PITCH, duration: LEVEL_MS })
    } else {
      setLevelling(true)
      map.easeTo({ pitch: 0, bearing: 0, duration: LEVEL_MS })
      map.once('moveend', () => setLevelling(false))
    }
  }, [camera3d])

  // Switching 3D itself starts afresh: a failure belongs to the last attempt.
  const previous3d = useRef(threeD)
  useEffect(() => {
    if (previous3d.current === threeD) return
    previous3d.current = threeD
    setTerrain('ok')
  }, [threeD])

  // Fail loud: a terrain tile that cannot be had drops the map back to flat
  // and says so. Nothing stands in for it. The terrain is tried again after a
  // minute, and the notice goes once its tiles load.
  const handleError = useCallback((e: { sourceId?: string; error?: Error }) => {
    if (e.sourceId === TERRAIN_SOURCE_ID) {
      console.error('Terrain unavailable', e.error)
      setTerrain('failed')
    }
  }, [])
  const handleSourceData = useCallback((e: { sourceId?: string; tile?: unknown }) => {
    if (e.sourceId === TERRAIN_SOURCE_ID && e.tile) setTerrain((t) => (t === 'retrying' ? 'ok' : t))
  }, [])
  useEffect(() => {
    if (terrain !== 'failed') return
    const timer = setTimeout(() => setTerrain('retrying'), TERRAIN_RETRY_MS)
    return () => clearTimeout(timer)
  }, [terrain])

  // Camera shots. The operator taking hold of the map takes the camera: the
  // replay stops placing it until the next shot starts, or Play or a scrub
  // hands control back. A tap does not count; a drag past a few pixels, a
  // pinch, the wheel, or a movestart that came from a person (an originalEvent)
  // does. The camera's own jumps carry no originalEvent.
  const driverRef = useRef(cameraDriver)
  const boatRef = useRef(boat)
  const gate = useRef(new CameraGate())
  useEffect(() => {
    driverRef.current = cameraDriver
    boatRef.current = boat
  })
  useEffect(() => {
    const map = mapRef.current?.getMap?.()
    if (!mapLoaded || !map) return
    const surface = map.getCanvasContainer()
    const down = new globalThis.Map<number, { x: number; y: number }>()
    const onDown = (e: PointerEvent) => {
      down.set(e.pointerId ?? 0, { x: e.clientX, y: e.clientY })
      if (down.size > 1) gate.current.yield()
    }
    const onMove = (e: PointerEvent) => {
      const start = down.get(e.pointerId ?? 0)
      if (start && Math.hypot(e.clientX - start.x, e.clientY - start.y) > DRAG_YIELD_PX) gate.current.yield()
    }
    const onUp = (e: PointerEvent) => { down.delete(e.pointerId ?? 0) }
    const onWheel = () => { gate.current.yield() }
    const onMoveStart = (e: { originalEvent?: unknown }) => {
      if (e.originalEvent) gate.current.yield()
    }
    surface.addEventListener('pointerdown', onDown)
    surface.addEventListener('pointermove', onMove)
    surface.addEventListener('pointerup', onUp)
    surface.addEventListener('pointercancel', onUp)
    surface.addEventListener('wheel', onWheel, { passive: true })
    map.on('movestart', onMoveStart)
    return () => {
      surface.removeEventListener('pointerdown', onDown)
      surface.removeEventListener('pointermove', onMove)
      surface.removeEventListener('pointerup', onUp)
      surface.removeEventListener('pointercancel', onUp)
      surface.removeEventListener('wheel', onWheel)
      map.off('movestart', onMoveStart)
    }
  }, [mapLoaded])
  const driverClock = cameraDriver ? cameraDriver.clock : null
  const driverResume = cameraDriver ? cameraDriver.resume : 0
  useEffect(() => {
    const driver = driverRef.current
    const map = mapRef.current?.getMap?.()
    if (!driver || !map || !loadedRef.current) return
    const overview = overviewCamera(map)
    if (!overview) return
    const current = readCamera(map)
    const result = driver.resolve({ flat: !camera3d, overview, current })
    if (!result) return
    const place = gate.current.place({
      result,
      resume: driver.resume,
      reduced: driver.reduced,
      current,
      boatOutside: () => {
        const at = boatRef.current
        if (!at) return false
        const c = map.getContainer()
        return outsideCentral(map.project([at.lon, at.lat]), c.clientWidth, c.clientHeight)
      },
    })
    if (place) map.jumpTo(place)
  // Placed when the replay clock moves or control is handed back.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [driverClock, driverResume])

  // The sky over the tilted map, only while the terrain is really up.
  useEffect(() => {
    const map = mapRef.current?.getMap?.()
    if (!mapLoaded || !map) return
    // The typings leave undefined (no sky) out; the runtime clears the sky with it.
    const apply = () => map.setSky((camera3d ? skyFor(isDarkTheme) : undefined) as never)
    apply()
    // A new map style (light to dark) drops the sky.
    map.on('style.load', apply)
    return () => { map.off('style.load', apply) }
  }, [mapLoaded, camera3d, isDarkTheme])

  useEffect(() => { onFlatChange?.(!camera3d) }, [camera3d, onFlatChange])

  useEffect(() => {
    if (loadedRef.current) frame()
    // frame is keyed on the bounds, so this runs when the track changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [boundsKey])

  if (!canRender) {
    return (
      <div
        data-testid="passage-map-webgl2-fallback"
        className="flex h-full items-center justify-center px-3 text-center text-xs text-muted-foreground"
      >
        Map needs WebGL2, which this browser does not provide
      </div>
    )
  }

  const centre = bounds ? centreOf(bounds) : { longitude: 0, latitude: 0 }
  return (
    <div className={cn('relative', className)} data-testid="passage-map">
      <Map
        ref={mapRef}
        mapLib={maplibregl}
        initialViewState={{ ...centre, zoom: INTRO_START_ZOOM, pitch }}
        maxPitch={tiltable ? REPLAY_3D_MAX_PITCH : 0}
        // Only null clears terrain once set; undefined leaves it as it was. The
        // prop's type leaves null out.
        terrain={(terrainOn ? terrainSetting : null) as typeof terrainSetting}
        onError={handleError}
        onSourceData={handleSourceData}
        style={{ width: '100%', height: '100%' }}
        mapStyle={isDarkTheme ? STYLE_DARK : STYLE_LIGHT}
        attributionControl={{ compact: true }}
        onIdle={collapseAttribution}
        onLoad={handleLoad}
        dragRotate={tiltable}
        touchPitch={tiltable}
      >
        {/* Bottom to top: basemap, imagery, seamarks, place names, then the
            passage lines. Imagery is anchored under the seamarks so toggling it
            mid-session cannot land on top of them. */}
        <Source id="openseamap" type="raster" tiles={[OPENSEAMAP_TILES]} tileSize={256} attribution="© OpenSeaMap contributors">
          <Layer id="openseamap-layer" type="raster" paint={{ 'raster-opacity': 0.85 }} />
        </Source>
        {terrainOn && (
          <Source id={TERRAIN_SOURCE_ID} {...terrainSource}>
            <Layer id="terrain-hillshade" type="hillshade" beforeId="openseamap-layer" paint={hillshadePaint} />
          </Source>
        )}
        {satellite && (
          <WorldImageryLayer
            opacity={[...WORLD_IMAGERY_OPACITY_EXPRESSION]}
            minzoom={SAT_HANDOFF_START_ZOOM}
            beforeId="openseamap-layer"
          />
        )}
        <MapPlaceLabels isDarkTheme={isDarkTheme} overImagery={satellite} />
        {pieces.map((p, i) => {
          const sailed = sailedTo ? sailedTo.get(p.passageId) : undefined
          const progressive = sailed !== undefined
          // While replaying, the whole line is the dim road ahead and the
          // sailed part is drawn over it; otherwise one line, as before.
          const lineOpacity = progressive ? 0.4 : playedIds.has(p.passageId) ? 0.95 : 0.45
          const casingOpacity = progressive ? 0.35 : playedIds.has(p.passageId) ? 0.95 : 0.5
          return (
            <Fragment key={p.key}>
              <Source id={`passage-${p.key}`} type="geojson" data={fullLines[i]}>
                <Layer
                  id={`passage-line-${p.key}`}
                  type="line"
                  layout={{ 'line-cap': 'round', 'line-join': 'round' }}
                  paint={{ 'line-color': passageColour(p.colourIndex), 'line-width': 3.5, 'line-opacity': lineOpacity }}
                />
                {satellite && (
                  <Layer
                    id={`passage-casing-${p.key}`}
                    type="line"
                    beforeId={`passage-line-${p.key}`}
                    layout={{ 'line-cap': 'round', 'line-join': 'round' }}
                    minzoom={SAT_HANDOFF_START_ZOOM}
                    paint={{
                      'line-color': '#ffffff',
                      'line-width': 6.5,
                      'line-opacity': ['interpolate', ['linear'], ['zoom'], SAT_HANDOFF_START_ZOOM, 0, SAT_HANDOFF_END_ZOOM, casingOpacity],
                    }}
                  />
                )}
              </Source>
              {progressive && (
                <Source id={`passage-sailed-${p.key}`} type="geojson" data={sailedCache.get(p.key, p.segments, sailed.at, sailed.end && sailed.at >= p.from && sailed.at <= p.to ? sailed.end : undefined)}>
                  <Layer
                    id={`passage-sailed-line-${p.key}`}
                    type="line"
                    layout={{ 'line-cap': 'round', 'line-join': 'round' }}
                    paint={{ 'line-color': passageColour(p.colourIndex), 'line-width': 3.5, 'line-opacity': 0.95 }}
                  />
                  {satellite && (
                    <Layer
                      id={`passage-sailed-casing-${p.key}`}
                      type="line"
                      beforeId={`passage-sailed-line-${p.key}`}
                      layout={{ 'line-cap': 'round', 'line-join': 'round' }}
                      minzoom={SAT_HANDOFF_START_ZOOM}
                      paint={{
                        'line-color': '#ffffff',
                        'line-width': 6.5,
                        'line-opacity': ['interpolate', ['linear'], ['zoom'], SAT_HANDOFF_START_ZOOM, 0, SAT_HANDOFF_END_ZOOM, 0.95],
                      }}
                    />
                  )}
                </Source>
              )}
            </Fragment>
          )
        })}
        {boat && marker === 'dot' && (
          <Marker longitude={boat.lon} latitude={boat.lat} anchor="center">
            <div
              data-testid="passage-boat-marker"
              className="h-4 w-4 rounded-full border-2 border-background bg-primary shadow-sm ring-2 ring-primary/40"
            />
          </Marker>
        )}
        {markers}
      </Map>
      {/* Each corner keeps what lives there and puts the overview beside it:
          the terrain notice and the toggle buttons stack with it at the top,
          and the bottom corners sit above the map credit. */}
      <div className="absolute left-2 top-2 flex flex-col items-start gap-2">
        {threeD && terrain !== 'ok' && (
          <div
            role="status"
            className="rounded-md bg-black/70 px-2 py-1 text-xs font-medium text-white backdrop-blur-sm"
          >
            Terrain unavailable
          </div>
        )}
        {insetCorner === 'top-left' && inset}
      </div>
      <div className="absolute right-2 top-2 flex flex-col items-end gap-2">
        <div className="flex gap-2">
          <MapToggleButton icon={Mountain} label="Toggle 3D" on={threeD} onToggle={() => setThreeD(!threeD)} />
          <ImageryToggleButton on={satellite} onToggle={() => setSatellite(!satellite)} />
        </div>
        <div className="flex flex-col gap-2">
          <MapIconButton icon={Plus} label="Zoom in" onClick={() => zoomBy(1)} disabled={controlsDisabled} />
          <MapIconButton icon={Minus} label="Zoom out" onClick={() => zoomBy(-1)} disabled={controlsDisabled} />
          <MapIconButton icon={Maximize} label="Fit track" onClick={fitTrack} disabled={controlsDisabled || !bounds} />
        </div>
        {insetCorner === 'top-right' && inset}
      </div>
      {insetCorner === 'bottom-left' && <div className="absolute bottom-8 left-2">{inset}</div>}
      {insetCorner === 'bottom-right' && <div className="absolute bottom-8 right-2">{inset}</div>}
    </div>
  )
})

export interface InsetMapProps {
  bounds: [number, number, number, number] | null
  isDarkTheme: boolean
  className?: string
  /** Zoom steps from the automatic fit: negative is wider, positive closer. */
  zoomOffset?: number
}

// A small overview in the corner: the region around the track, with a box
// where the track is. Not interactive; the main map is the one to move.
export function InsetMap({ bounds, isDarkTheme, className, zoomOffset = 0 }: InsetMapProps) {
  const canRender = hasWebGL2()
  const mapRef = useRef<MapRef>(null)
  const centre = bounds ? centreOf(bounds) : null
  // The same automatic zoom the blog embed and the still images use, so a step
  // means the same framing in all three.
  const zoom = bounds ? offsetInsetZoom(insetZoom(bounds), zoomOffset) : 0
  const lon = centre?.longitude
  const lat = centre?.latitude
  // A map takes its first view only when it is made, so a new step moves the
  // map that is there instead of drawing another.
  useEffect(() => {
    if (lon === undefined || lat === undefined) return
    mapRef.current?.jumpTo({ center: [lon, lat], zoom })
  }, [lon, lat, zoom])
  if (!canRender || !bounds || !centre) return null
  const box: GeoJSON.Feature<GeoJSON.Polygon> = {
    type: 'Feature',
    properties: {},
    geometry: {
      type: 'Polygon',
      coordinates: [[[bounds[0], bounds[1]], [bounds[2], bounds[1]], [bounds[2], bounds[3]], [bounds[0], bounds[3]], [bounds[0], bounds[1]]]],
    },
  }
  return (
    <div className={className} data-testid="passage-inset-map">
      <Map
        mapLib={maplibregl}
        ref={mapRef}
        initialViewState={{ ...centre, zoom }}
        style={{ width: '100%', height: '100%' }}
        mapStyle={isDarkTheme ? STYLE_DARK : STYLE_LIGHT}
        interactive={false}
        attributionControl={false}
      >
        <Source id="inset-box" type="geojson" data={box}>
          <Layer id="inset-box-fill" type="fill" paint={{ 'fill-color': '#e11d48', 'fill-opacity': 0.2 }} />
          <Layer id="inset-box-line" type="line" paint={{ 'line-color': '#e11d48', 'line-width': 2 }} />
        </Source>
        <Marker longitude={centre.longitude} latitude={centre.latitude} anchor="center">
          <div className="h-2 w-2 rounded-full bg-destructive" />
        </Marker>
      </Map>
    </div>
  )
}
