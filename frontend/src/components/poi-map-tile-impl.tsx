import 'maplibre-gl/dist/maplibre-gl.css'
import maplibregl from 'maplibre-gl'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { MapRef } from 'react-map-gl/maplibre'
import { Map, Marker, Source, Layer } from 'react-map-gl/maplibre'
import { MapPin, Settings2, Ship } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Tile } from '@/components/ui/tile'
import { MapPlaceLabels } from '@/components/map-place-labels'
import { VesselArrow } from '@/components/vessel-arrow-marker'
import { STYLE_LIGHT, STYLE_DARK, OPENSEAMAP_TILES } from '@/lib/basemap'
import { resolveMarkerLabelSuppression, type MarkerLabelPoint } from '@/lib/marker-labels'
import { fitCameraToPoints, formatBearing, poiCategoryById, topPoi, zoomForRangeNm, type MapPoint, type PoiFeature } from '@/lib/poi'
import { POI_MAP_SUMMARY_CYCLE_SECONDS_DEFAULT, type PoiMapWidgetConfig } from '@/lib/dashboard-widgets'
import { usePoi } from '@/hooks/use-poi'
import { useCollapsedMapAttribution } from '@/hooks/use-collapsed-map-attribution'
import { useCyclingIndex } from '@/hooks/use-cycling-index'
import type { NearbyVessel } from '@/hooks/use-nearby-vessels'
import type { AlarmState } from '@/hooks/use-alarms'
import type { TrailPoint } from '@/hooks/use-server-trails'
import type { DistanceUnits } from '@/config/app-config'
import { formatDataAge, isStale } from '@/lib/staleness'
import { cn } from '@/lib/utils'
import { hasWebGL2 } from '@/lib/webgl'

/** One waypoint of the active route, as this tile needs it to draw the route layer. */
export interface PoiMapActiveRouteWaypoint {
  lat: number
  lon: number
  name?: string
}

/**
 * The currently active route (ADR 0092's next-waypoint line, reused here):
 * App.tsx derives this once from useRoutes/useRouteActivation and
 * nextWaypoint, so this tile never has to know route activation exists, only
 * how to draw one. `waypoints` is in traversal order - the order `nextIndex`
 * indexes into - which is not always the route's authored order; see
 * lib/next-waypoint.ts's own comment on why a reversed activation makes the
 * two differ.
 */
export interface PoiMapActiveRoute {
  name: string
  waypoints: PoiMapActiveRouteWaypoint[]
  nextIndex: number | null
}

export interface PoiMapTileProps {
  config: PoiMapWidgetConfig
  editing: boolean
  onConfigure?: () => void
  latitude: number | null
  longitude: number | null
  headingTrue: number | null
  /**
   * Null when no route is active, its id matches nothing in the routes list,
   * or activation status hasn't resolved yet - the route layer draws nothing
   * in every one of those cases.
   */
  activeRoute?: PoiMapActiveRoute | null
  gnssCriticalAlert: boolean
  positionLastUpdateAgeS: number | null
  nearbyVessels: NearbyVessel[]
  aisCollisionAlarms?: ReadonlyMap<string, AlarmState>
  getSelfTrail: () => TrailPoint[]
  isDarkTheme: boolean
  /** The instrument skin forces dark regardless of the app-wide theme. */
  forceDark?: boolean
  distanceUnits: DistanceUnits
  /**
   * False on the wall kiosk (ADR: kiosk maps are display-only): passed
   * straight through as maplibre's own `interactive` option, which detaches
   * every mouse/touch/keyboard handler so there's no zoom, pan, rotate or
   * gesture handling left for a display with no touchscreen. The map still
   * follows the vessel (the easeTo/jumpTo effect below is imperative, not a
   * handler) and markers/trails still update - there's just nothing for the
   * operator to interact with. Defaults to true so every existing host
   * keeps today's behaviour.
   */
  interactive?: boolean
}

// How many features to ask the server for. The map draws every one of them;
// the ranked list only ever shows the top 5 (topPoi's own default).
const POI_FETCH_LIMIT = 50
const RANKED_LIST_SIZE = 5

// One easeTo at most per this interval while following the vessel, so a
// stream of position fixes (SignalK publishes several a second) doesn't
// fight the operator's own pan/zoom or animate constantly.
const FOLLOW_THROTTLE_MS = 2000
const FOLLOW_EASE_DURATION_MS = 500

// Clearance kept around the fitted vessel+ranked-POIs bounding box, in
// screen pixels, so a marker (h-7 w-7, 28px) plus its name label doesn't sit
// flush against the tile's edge. Exported for the test suite, which computes
// the same fit independently to check the tile's jumpTo/easeTo calls against.
export const POI_MAP_FIT_PADDING_PX = 36

// Fallback box for the one frame before ResizeObserver reports a real size
// (or in an environment, such as jsdom, that never fires it at all).
const FALLBACK_HEIGHT = 240
const FALLBACK_WIDTH = 240

// Same technique sea-state-tile.tsx's own useMeasuredBox uses - this tile
// needs both dimensions, not just height, now that the camera fits the
// vessel plus the ranked POIs against the actual viewport shape rather than
// assuming height is always the binding constraint (in "split" layout the
// map pane is half-width, so POIs east/west of the vessel used to fall off
// the edge even though a height-only zoom said they'd fit).
function useMeasuredBox() {
  const ref = useRef<HTMLDivElement>(null)
  const [box, setBox] = useState({ width: 0, height: 0 })

  useEffect(() => {
    const el = ref.current
    if (!el) return
    const observer = new ResizeObserver(([entry]) => {
      setBox({ width: entry.contentRect.width, height: entry.contentRect.height })
    })
    observer.observe(el)
    return () => observer.disconnect()
  }, [])

  return [ref, box] as const
}

function trailToGeoJSON(points: TrailPoint[]): GeoJSON.Feature<GeoJSON.LineString> {
  return {
    type: 'Feature',
    geometry: { type: 'LineString', coordinates: points.map((p) => [p.lon, p.lat]) },
    properties: {},
  }
}

function waypointsToLineGeoJSON(waypoints: PoiMapActiveRouteWaypoint[]): GeoJSON.Feature<GeoJSON.LineString> {
  return {
    type: 'Feature',
    geometry: { type: 'LineString', coordinates: waypoints.map((wp) => [wp.lon, wp.lat]) },
    properties: {},
  }
}

function waypointsToPointsGeoJSON(waypoints: PoiMapActiveRouteWaypoint[]): GeoJSON.FeatureCollection<GeoJSON.Point> {
  return {
    type: 'FeatureCollection',
    features: waypoints.map((wp) => ({
      type: 'Feature',
      geometry: { type: 'Point', coordinates: [wp.lon, wp.lat] },
      properties: {},
    })),
  }
}

function formatPoiDistance(distanceM: number, units: DistanceUnits): string {
  if (units === 'imperial') return `${(distanceM / 1852).toFixed(1)} nm`
  return `${(distanceM / 1000).toFixed(1)} km`
}

/**
 * The Nearby widget (ADR 0091 phase 3b): a moving map of points of interest
 * near the vessel, with an optional ranked list beside it.
 *
 * No masking fallback: use-poi.ts keeps the last good features on a failed
 * poll, and this tile shows their age and the error in muted text rather
 * than an empty or fabricated list (see the footer). When gnssCriticalAlert
 * is set the map holds its last good centre and shows an amber GNSS badge
 * instead of following a position that may not be real.
 */
export default function PoiMapTileImpl({
  config,
  editing,
  onConfigure,
  latitude,
  longitude,
  headingTrue,
  activeRoute = null,
  gnssCriticalAlert,
  positionLastUpdateAgeS,
  nearbyVessels,
  aisCollisionAlarms,
  getSelfTrail,
  isDarkTheme,
  forceDark = false,
  distanceUnits,
  interactive = true,
}: PoiMapTileProps) {
  const poi = usePoi(config.rangeNm, config.categories, POI_FETCH_LIMIT)
  const rankedList = useMemo(() => topPoi(poi.features, RANKED_LIST_SIZE), [poi.features])
  // A plain object rather than a JS Map: the Map component imported above
  // from react-map-gl/maplibre shadows the global Map constructor in this
  // module's scope.
  const rankById = useMemo(() => {
    const ranks: Record<string, number> = {}
    rankedList.forEach((f, i) => { ranks[f.id] = i + 1 })
    return ranks
  }, [rankedList])

  // Cycling summary (split layout): only one ranked POI shows its detail at
  // a time, advancing every summaryCycleSeconds. Most POIs never get a
  // detail at all (PoiFeature's own doc: it's an editorial summary or a
  // Wikipedia first sentence, when the provider has one), so those are
  // skipped outright rather than getting an empty turn in the rotation.
  const cyclableFeatures = useMemo(() => rankedList.filter((f) => f.detail !== ''), [rankedList])
  // A plain, order-sensitive key rather than the array itself: identical ids
  // in a new array (every poll gives rankedList a fresh reference) must not
  // reset the cycle, but a real change to the set - a POI gaining/losing its
  // detail, dropping out of range, or the ranking reordering - should. See
  // useCyclingIndex's own doc for why this is what it takes as resetKey.
  const cyclableKey = useMemo(() => cyclableFeatures.map((f) => f.id).join('|'), [cyclableFeatures])
  const summaryCycleSeconds = config.summaryCycleSeconds ?? POI_MAP_SUMMARY_CYCLE_SECONDS_DEFAULT
  const cycleIndex = useCyclingIndex(cyclableFeatures.length, summaryCycleSeconds, cyclableKey)
  const expandedPoiId = cycleIndex !== null ? cyclableFeatures[cycleIndex].id : null

  const dark = isDarkTheme || forceDark
  const mapStyle = dark ? STYLE_DARK : STYLE_LIGHT

  // WPE WebKit 2.38 (the wall-display kiosk browser) has no WebGL2, and
  // MapLibre 5 throws synchronously when it can't get a context — a single
  // Nearby tile mounting a map there would blank the whole feed (ADR 0089
  // §11). The ranked list beside it needs no WebGL at all, so only the map
  // pane itself degrades.
  const canRenderMap = hasWebGL2()

  const mapRef = useRef<MapRef | null>(null)
  const collapseAttribution = useCollapsedMapAttribution(mapRef)
  const [mapContainerRef, measuredBox] = useMeasuredBox()
  const heightPx = measuredBox.height > 0 ? measuredBox.height : FALLBACK_HEIGHT
  const widthPx = measuredBox.width > 0 ? measuredBox.width : FALLBACK_WIDTH

  // The floor: never zoom in tighter than the configured range gives against
  // whichever of width/height is more restrictive - previously this always
  // used height, which is fine in "map" layout (full width) but let POIs
  // fall off the sides in "split" layout's half-width map pane even though
  // this same number said they'd fit.
  const rangeZoomFloor = zoomForRangeNm(config.rangeNm, latitude ?? 0, Math.min(widthPx, heightPx))

  // The camera: fits the vessel plus the ranked list (not every fetched
  // feature - the ranked list is what the operator can actually see named in
  // the split layout's rows, and what topPoi already limits to
  // RANKED_LIST_SIZE) inside the measured viewport, padded for marker size
  // and labels. Falls back to the old vessel-centred range zoom when there's
  // nothing ranked yet (feed still loading, or "map" layout content aside,
  // topPoi is layout-independent). Never tighter than rangeZoomFloor, so a
  // single close POI doesn't zoom in past what the configured range implies.
  // Deliberately excludes cycleIndex/expandedPoiId from its own inputs: the
  // summary cycle changes which row is expanded, not where anything is, and
  // must never move the camera.
  const cameraTarget = useMemo((): { center: MapPoint; zoom: number } | null => {
    if (latitude === null || longitude === null) return null
    const vessel: MapPoint = { lat: latitude, lon: longitude }
    if (rankedList.length === 0) return { center: vessel, zoom: rangeZoomFloor }
    const points: MapPoint[] = [vessel, ...rankedList.map((f) => ({ lat: f.lat, lon: f.lon }))]
    const fit = fitCameraToPoints(points, widthPx, heightPx, POI_MAP_FIT_PADDING_PX)
    return { center: fit.center, zoom: Math.min(fit.zoom, rangeZoomFloor) }
  }, [latitude, longitude, rankedList, widthPx, heightPx, rangeZoomFloor])

  const hasCenteredRef = useRef(false)
  const lastEaseAtRef = useRef(0)

  // Follow the vessel-plus-ranked-POIs fit: first fix jumps straight there,
  // later fixes ease in, throttled to at most one per FOLLOW_THROTTLE_MS.
  // Holding still while gnssCriticalAlert is set (last good centre, no
  // jump/ease at all) is the same rule the anchor-watch gate uses for the
  // position sentinel. This runs regardless of `interactive`: it drives the
  // camera imperatively (map.jumpTo/easeTo), not through one of maplibre's
  // own gesture handlers, so the kiosk's non-interactive map still follows.
  useEffect(() => {
    if (gnssCriticalAlert) return
    if (!cameraTarget) return
    const map = mapRef.current
    if (!map) return

    const { center, zoom } = cameraTarget

    if (!hasCenteredRef.current) {
      hasCenteredRef.current = true
      lastEaseAtRef.current = Date.now()
      map.jumpTo({ center: [center.lon, center.lat], zoom })
      return
    }

    const now = Date.now()
    if (now - lastEaseAtRef.current < FOLLOW_THROTTLE_MS) return
    lastEaseAtRef.current = now
    map.easeTo({ center: [center.lon, center.lat], zoom, duration: FOLLOW_EASE_DURATION_MS })
  }, [cameraTarget, gnssCriticalAlert])

  // Declutter POI marker labels on move-end, priority by rank then distance
  // (an unranked feature always yields to a ranked one, matching the ranked
  // list's own ordering) — see resolveMarkerLabelSuppression's own doc for
  // why this is a pure, projected-screen-space computation. Kept regardless
  // of `interactive`: on the kiosk this only ever fires from the follow
  // effect's own jumpTo/easeTo above, not from a user gesture, and the
  // labels still need to stay decluttered as POIs move on/off screen.
  const [suppressedLabelIds, setSuppressedLabelIds] = useState<Set<string>>(new Set())
  const declutterLabels = useCallback(() => {
    const map = mapRef.current
    if (!map || typeof map.project !== 'function') return
    const points: MarkerLabelPoint[] = poi.features.map((f) => ({
      id: f.id,
      ...map.project([f.lon, f.lat]),
      priority: rankById[f.id] ?? 100000 + f.distanceM,
    }))
    setSuppressedLabelIds(resolveMarkerLabelSuppression(points, []))
  }, [poi.features, rankById])

  // getSelfTrail() reads a ref-backed buffer (use-server-trails.ts) whose
  // identity never changes as new points arrive, so poi.features (which
  // does change, once a minute) stands in as the recompute tick — the same
  // trick anchor-watch-map.tsx's own postAnchorTrailGeoJSON uses with its
  // renderKey.
  const trailGeoJSON = useMemo(
    () => (config.showTrail ? trailToGeoJSON(getSelfTrail()) : null),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [config.showTrail, getSelfTrail, poi.features],
  )

  // The active route (ADR 0092/0091): whole route as one line, plus the leg
  // into the next waypoint drawn again on top in a brighter, thicker stroke
  // so the leg actually being sailed reads at a glance. `activeRoute` is
  // already null whenever no route is active or its waypoints are empty
  // (App.tsx's job, not this tile's), so a plain line needs no extra guard
  // beyond the usual "a line needs two points" one.
  const routeLineGeoJSON = useMemo(
    () => (activeRoute && activeRoute.waypoints.length >= 2 ? waypointsToLineGeoJSON(activeRoute.waypoints) : null),
    [activeRoute],
  )
  const routeNextLegGeoJSON = useMemo(() => {
    if (!activeRoute || activeRoute.nextIndex === null) return null
    const { waypoints, nextIndex } = activeRoute
    if (nextIndex <= 0 || nextIndex >= waypoints.length) return null
    return waypointsToLineGeoJSON([waypoints[nextIndex - 1], waypoints[nextIndex]])
  }, [activeRoute])
  const routeWaypointsGeoJSON = useMemo(
    () => (activeRoute && activeRoute.waypoints.length > 0 ? waypointsToPointsGeoJSON(activeRoute.waypoints) : null),
    [activeRoute],
  )
  const routeNextWaypointGeoJSON = useMemo(() => {
    if (!activeRoute || activeRoute.nextIndex === null) return null
    const waypoint = activeRoute.waypoints[activeRoute.nextIndex]
    return waypoint ? waypointsToPointsGeoJSON([waypoint]) : null
  }, [activeRoute])

  const title = config.title.trim() || 'Nearby'
  const stale = isStale(positionLastUpdateAgeS)

  return (
    <Tile
      title={title}
      icon={<MapPin className="h-3.5 w-3.5 text-gauge-secondary" />}
      stale={stale}
      staleLabel={positionLastUpdateAgeS !== null ? formatDataAge(positionLastUpdateAgeS) : undefined}
      titleExtra={
        editing && onConfigure ? (
          <Button
            variant="ghost"
            size="icon"
            className="h-6 w-6 text-muted-foreground hover:text-foreground"
            onClick={onConfigure}
            aria-label={`Configure nearby map: ${title}`}
          >
            <Settings2 className="h-3.5 w-3.5" />
          </Button>
        ) : undefined
      }
    >
      <div
        className={cn(
          'grid h-full min-h-0 gap-2',
          config.layout === 'split' ? 'grid-cols-2' : 'grid-cols-1',
        )}
      >
        <div
          ref={mapContainerRef}
          data-testid="poi-map-container"
          className="relative isolate h-full min-h-0 overflow-hidden rounded-md"
        >
          {!canRenderMap ? (
            <div
              data-testid="poi-map-webgl2-fallback"
              className="flex h-full items-center justify-center px-3 text-center text-[10px] uppercase tracking-[0.14em] text-muted-foreground"
            >
              Map needs WebGL2, which this browser does not provide
            </div>
          ) : (
            <>
              <Map
                ref={mapRef}
                mapLib={maplibregl}
                initialViewState={{
                  latitude: latitude ?? 0,
                  longitude: longitude ?? 0,
                  // The mount-time jumpTo in the follow effect above fires
                  // right after this first paint and takes over from here
                  // (to the fitted vessel+ranked-POIs camera, once the ranked
                  // list has anything in it) - this is only what's on screen
                  // for that one frame before it does.
                  zoom: rangeZoomFloor,
                }}
                style={{ width: '100%', height: '100%' }}
                mapStyle={mapStyle}
                interactive={interactive}
                dragRotate={false}
                onLoad={declutterLabels}
                onMoveEnd={declutterLabels}
                // Same reasoning as anchor-watch-map.tsx and
                // route-planner-map.tsx: OpenSeaMap requires attribution, and
                // compact keeps it to a single "i" until tapped rather than a
                // permanent credit strip. useCollapsedMapAttribution's own
                // doc covers why this can't be done in CSS.
                attributionControl={{ compact: true }}
                onIdle={collapseAttribution}
              >
                {/* Sourceless anchor layer, mounted first so later rasters
                    (OpenSeaMap) have a stable beforeId to pin themselves under
                    the vector labels — see route-planner-map.tsx's identical
                    comment for the full reasoning. */}
                <Layer id="raster-overlay-anchor" type="background" paint={{ 'background-opacity': 0 }} />

                <MapPlaceLabels isDarkTheme={dark} overImagery={false} />

                <Source id="openseamap" type="raster" tiles={[OPENSEAMAP_TILES]} tileSize={256} attribution="© OpenSeaMap contributors">
                  <Layer id="openseamap-layer" type="raster" beforeId="raster-overlay-anchor" paint={{ 'raster-opacity': 0.85 }} />
                </Source>

                {trailGeoJSON && trailGeoJSON.geometry.coordinates.length >= 2 && (
                  <Source id="poi-map-own-trail" type="geojson" data={trailGeoJSON}>
                    <Layer
                      id="poi-map-own-trail-layer"
                      type="line"
                      paint={{ 'line-color': '#f59e0b', 'line-width': 2, 'line-opacity': 0.85 }}
                      layout={{ 'line-join': 'round', 'line-cap': 'round' }}
                    />
                  </Source>
                )}

                {/* The active route (ADR 0092/0091): above the trail, below
                    every marker below (markers are DOM overlays outside the
                    canvas, so they're always on top regardless of JSX
                    order — this ordering is only what it means for the GL
                    layers among themselves). */}
                {routeLineGeoJSON && (
                  <Source id="poi-map-route-line" type="geojson" data={routeLineGeoJSON}>
                    <Layer
                      id="poi-map-route-line-layer"
                      type="line"
                      paint={{
                        'line-color': dark ? '#38bdf8' : '#0284c7',
                        'line-width': 3,
                        'line-opacity': 0.85,
                      }}
                      layout={{ 'line-join': 'round', 'line-cap': 'round' }}
                    />
                  </Source>
                )}

                {routeNextLegGeoJSON && (
                  <Source id="poi-map-route-next-leg" type="geojson" data={routeNextLegGeoJSON}>
                    <Layer
                      id="poi-map-route-next-leg-layer"
                      type="line"
                      paint={{
                        // One step brighter than the base route line above,
                        // and thicker, so the leg actually being sailed
                        // reads at a glance against the rest of the route.
                        'line-color': dark ? '#7dd3fc' : '#38bdf8',
                        'line-width': 5,
                        'line-opacity': 1,
                      }}
                      layout={{ 'line-join': 'round', 'line-cap': 'round' }}
                    />
                  </Source>
                )}

                {routeWaypointsGeoJSON && (
                  <Source id="poi-map-route-waypoints" type="geojson" data={routeWaypointsGeoJSON}>
                    <Layer
                      id="poi-map-route-waypoints-layer"
                      type="circle"
                      paint={{
                        'circle-radius': 4,
                        'circle-color': dark ? '#38bdf8' : '#0284c7',
                        'circle-stroke-width': 1,
                        'circle-stroke-color': dark ? '#0c1a26' : '#ffffff',
                      }}
                    />
                  </Source>
                )}

                {routeNextWaypointGeoJSON && (
                  <Source id="poi-map-route-next-waypoint" type="geojson" data={routeNextWaypointGeoJSON}>
                    <Layer
                      id="poi-map-route-next-waypoint-layer"
                      type="circle"
                      paint={{
                        'circle-radius': 7,
                        'circle-color': dark ? '#7dd3fc' : '#38bdf8',
                        'circle-stroke-width': 1.5,
                        'circle-stroke-color': dark ? '#0c1a26' : '#ffffff',
                      }}
                    />
                  </Source>
                )}

                {config.showAis && nearbyVessels.map((vessel) => {
                  if (vessel.lat === undefined || vessel.lon === undefined) return null
                  const collisionState = aisCollisionAlarms?.get(vessel.id)
                  const inCollisionAlarm = collisionState !== undefined
                  return (
                    <Marker key={vessel.id} latitude={vessel.lat} longitude={vessel.lon}>
                      <div
                        className={cn(
                          'flex h-7 w-7 items-center justify-center rounded-full shadow-lg',
                          inCollisionAlarm ? 'bg-red-600' : 'bg-amber-500/90',
                        )}
                        aria-label={`AIS vessel: ${vessel.name}`}
                      >
                        <Ship className="h-4 w-4 text-white" />
                      </div>
                    </Marker>
                  )
                })}

                {poi.features.map((feature) => {
                  const rank = rankById[feature.id]
                  const category = poiCategoryById(feature.category)
                  const Icon = category?.icon ?? MapPin
                  const suppressed = suppressedLabelIds.has(feature.id)
                  const expanded = feature.id === expandedPoiId
                  return (
                    <Marker key={feature.id} latitude={feature.lat} longitude={feature.lon}>
                      <div className="flex flex-col items-center" aria-label={`Point of interest: ${feature.name || category?.label || feature.category}`}>
                        <div
                          data-testid={expanded ? 'poi-marker-expanded' : undefined}
                          className={cn(
                            'relative flex h-7 w-7 items-center justify-center rounded-full border border-border bg-card shadow-md',
                            // Subtle: the ranked list's own row already carries the
                            // full summary, this ring only helps the eye match
                            // that row back to its marker on the map.
                            expanded && 'ring-2 ring-primary ring-offset-1 ring-offset-background',
                          )}
                        >
                          <Icon className="h-3.5 w-3.5 text-foreground" />
                          {rank !== undefined && (
                            <span
                              data-testid={`poi-marker-rank-${feature.id}`}
                              className="absolute -right-1 -top-1 flex h-3.5 w-3.5 items-center justify-center rounded-full bg-primary text-[9px] font-bold leading-none text-primary-foreground"
                            >
                              {rank}
                            </span>
                          )}
                        </div>
                        {!suppressed && feature.name && (
                          <div className="mt-0.5 max-w-20 truncate text-center text-[9px] font-medium text-white drop-shadow-[0_1px_2px_rgba(0,0,0,0.8)]">
                            {feature.name}
                          </div>
                        )}
                      </div>
                    </Marker>
                  )
                })}

                {latitude !== null && longitude !== null && (
                  <Marker latitude={latitude} longitude={longitude}>
                    <div className="flex items-center justify-center" style={{ width: 40, height: 40 }}>
                      <VesselArrow headingDeg={headingTrue} scale={1} />
                    </div>
                  </Marker>
                )}
              </Map>

              {gnssCriticalAlert && (
                <div
                  data-testid="poi-map-gnss-badge"
                  className="absolute left-2 top-2 z-20 rounded-xs border border-amber-500/40 bg-amber-500/10 px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-amber-600 dark:text-amber-400"
                >
                  GNSS
                </div>
              )}
            </>
          )}
        </div>

        {config.layout === 'split' && (
          <div className="flex h-full min-h-0 flex-col gap-1 overflow-y-auto pr-1">
            {rankedList.length === 0 ? (
              <div className="flex h-full items-center justify-center text-center text-[10px] uppercase tracking-[0.14em] text-muted-foreground">
                {poi.error && poi.fetchedAt === null
                  ? 'POI feed unavailable'
                  : poi.loading
                    ? 'Loading…'
                    : 'No points of interest in range'}
              </div>
            ) : (
              rankedList.map((feature, index) => (
                <PoiListRow
                  key={feature.id}
                  rank={index + 1}
                  feature={feature}
                  distanceUnits={distanceUnits}
                  expanded={feature.id === expandedPoiId}
                />
              ))
            )}
            <div className="mt-auto border-t border-border/60 pt-1 text-[10px] text-muted-foreground">
              {poi.provider ? <span className="capitalize">{poi.provider.replace(/-/g, ' ')}</span> : null}
              {poi.fetchedAt && (
                <span> · {formatDataAge((Date.now() - Date.parse(poi.fetchedAt)) / 1000)} old</span>
              )}
              {poi.error && (
                <div className="mt-0.5 text-amber-600 dark:text-amber-400">{poi.error}</div>
              )}
            </div>
          </div>
        )}
      </div>
    </Tile>
  )
}

function PoiListRow({
  rank,
  feature,
  distanceUnits,
  expanded,
}: {
  rank: number
  feature: PoiFeature
  distanceUnits: DistanceUnits
  expanded: boolean
}) {
  const category = poiCategoryById(feature.category)
  const Icon = category?.icon ?? MapPin
  return (
    <div className="flex items-start gap-1.5 rounded-xs px-1 py-0.5" data-testid="poi-list-row">
      <span className="mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-full bg-primary text-[9px] font-bold leading-none text-primary-foreground">
        {rank}
      </span>
      <Icon className="mt-0.5 h-3.5 w-3.5 shrink-0 text-muted-foreground" />
      <div className="min-w-0 flex-1">
        <div className="flex items-baseline justify-between gap-1">
          <span className="truncate text-[11px] font-semibold text-foreground">{feature.name || category?.label || feature.category}</span>
          <span className="shrink-0 text-[10px] tabular-nums text-muted-foreground">
            {formatPoiDistance(feature.distanceM, distanceUnits)} {formatBearing(feature.bearingDeg)}
          </span>
        </div>
        {/* Only the currently cycled-to row shows its detail, and it shows
            the whole thing (wrapped, capped at three lines) rather than the
            single truncated line every row used to carry — see
            PoiMapTileImpl's cycling effects above. */}
        {expanded && (
          <div data-testid="poi-list-row-summary" className="mt-0.5 line-clamp-3 text-[10px] text-muted-foreground">
            {feature.detail}
          </div>
        )}
      </div>
    </div>
  )
}
