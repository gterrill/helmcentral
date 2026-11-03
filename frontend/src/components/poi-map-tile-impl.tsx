import 'maplibre-gl/dist/maplibre-gl.css'
import maplibregl from 'maplibre-gl'
import { memo, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { Map, Marker, Source, Layer, useMap, type MapRef } from 'react-map-gl/maplibre'
import { MapPin, Settings2, Ship } from 'lucide-react'

import { Tile, tilePillButtonClass } from '@/components/ui/tile'
import { MapPlaceLabels } from '@/components/map-place-labels'
import { VesselGroup, vesselGroupMessage } from '@/components/nearby-vessel-group'
import { VesselArrow } from '@/components/vessel-arrow-marker'
import { STYLE_LIGHT, STYLE_DARK, OPENSEAMAP_TILES } from '@/lib/basemap'
import { MAP_LABEL_HALO, resolveMarkerLabelSuppression, suppressedIdsEqual, type MarkerLabelPoint } from '@/lib/marker-labels'
import { fitCameraAroundPoint, formatBearing, poiCategoryById, pointInCameraFrame, topPoi, zoomForRangeNm, type MapPoint, type PoiFeature } from '@/lib/poi'
import { poiArrowGeometry, type PoiArrowGeometry } from '@/lib/poi-arrow'
import { lonNearest, METRES_PER_NM } from '@/lib/geo'
import { POI_MAP_SUMMARY_CYCLE_SECONDS_DEFAULT, type PoiMapWidgetConfig } from '@/lib/dashboard-widgets'
import { usePoi } from '@/hooks/use-poi'
import { useCollapsedMapAttribution } from '@/hooks/use-collapsed-map-attribution'
import { useCyclingIndex } from '@/hooks/use-cycling-index'
import { type NearbyVessel, type NearbyVesselFeed } from '@/hooks/use-nearby-vessels'
import { type AlarmState } from '@/hooks/use-alarms'
import { type TrailPoint } from '@/hooks/use-server-trails'
import { type DistanceUnits } from '@/config/app-config'
import { formatPoiDistance } from '@/lib/format'
import { collisionTier, selectListedVessels, sortByCollisionAlarm } from '@/lib/nearby-vessel-display'
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
 * LivePoiMapTile (poi-map-tile.tsx) derives this once from useRouteActivation and
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
  /**
   * What the nearby-vessels feed said about itself: where the list came from
   * and how far it searches. Null until the first event, which the Vessels
   * group reads as "still loading", never as "no vessels".
   */
  nearbyVesselFeed?: NearbyVesselFeed | null
  aisCollisionAlarms?: ReadonlyMap<string, AlarmState>
  selfTrail: TrailPoint[]
  isDarkTheme: boolean
  /** The instrument skin forces dark regardless of the app-wide theme. */
  forceDark?: boolean
  distanceUnits: DistanceUnits
  /**
   * False on the wall kiosk (ADR: kiosk maps are display-only): passed
   * straight through as maplibre's own `interactive` option, which detaches
   * every mouse/touch/keyboard handler so there's no zoom, pan, rotate or
   * gesture handling left for a display with no touchscreen. The camera
   * still re-frames when it must (the jumpTo effect below is imperative, not
   * a handler) and markers/trails/the arrow still update - there's just
   * nothing for the operator to interact with. Defaults to true so every
   * existing host keeps today's behaviour.
   */
  interactive?: boolean
}

// How many features to ask the server for. The map draws every one of them;
// the ranked list only ever shows the top 5 (topPoi's own default).
const POI_FETCH_LIMIT = 50
const RANKED_LIST_SIZE = 5

// The list pane height from which the Vessels group stacks under the places
// instead of sitting beside them. The kiosk list cannot scroll, so a tall pane
// is the only one where stacking is known to fit everything. Worst case, in pixels: footer
// 24, five place rows 130, the highlighted row's three-line summary 50, group
// heading 16, four two-line vessel rows 160 and gaps 16, which is 396. The
// 272 px wall tile has about 230 px for the list, and a 12 x 22 tile has well
// over this.
const VESSELS_STACK_MIN_HEIGHT_PX = 420
// The list pane width below which the two groups stack even when the pane is
// short, because side by side each column would be too narrow. Such a pane may
// cut off what is stacked last, so it puts the Vessels group first while a
// listed vessel is in collision alarm or warning.
const VESSELS_SIDE_BY_SIDE_MIN_WIDTH_PX = 600

// Vessel label ids in the declutter pass carry this prefix, so a vessel id can
// never equal a place id in the suppressed set.
const VESSEL_LABEL_PREFIX = 'vessel:'
// Vessel labels yield to every place label, ranked or not. Unranked places use
// 100000 + their distance in metres, which stays under this for any range the
// tile allows.
const VESSEL_LABEL_PRIORITY_BASE = 1_000_000

// Clearance kept around the farthest ranked POI in the vessel-centred fit, in
// screen pixels, so a marker (h-9 w-9, 36px) plus its name label doesn't sit
// flush against the tile's edge. Exported for the test suite, which computes
// the same fit independently to check the tile's jumpTo calls against.
export const POI_MAP_FIT_PADDING_PX = 44

// Fallback box for the one frame before ResizeObserver reports a real size
// (or in an environment, such as jsdom, that never fires it at all).
const FALLBACK_HEIGHT = 240
const FALLBACK_WIDTH = 240

// Same technique sea-state-tile.tsx's own useMeasuredBox uses - this tile
// needs both dimensions, not just height, now that the camera fits the
// ranked POIs around the vessel against the actual viewport shape rather than
// assuming height is always the binding constraint (in "split" layout the
// map pane is half-width, so POIs east/west of the vessel used to fall off
// the edge even though a height-only zoom said they'd fit).
function useMeasuredBox(active = true) {
  const ref = useRef<HTMLDivElement>(null)
  const [box, setBox] = useState({ width: 0, height: 0 })

  useEffect(() => {
    const el = ref.current
    if (!el || !active) return
    const observer = new ResizeObserver(([entry]) => {
      setBox({ width: entry.contentRect.width, height: entry.contentRect.height })
    })
    observer.observe(el)
    return () => observer.disconnect()
  }, [active])

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

/**
 * The Nearby widget (ADR 0091 phase 3b): a map of points of interest near the
 * vessel, with an optional ranked list beside it. The map holds still while
 * the boat marker moves; in the split layout a curved arrow runs from the boat
 * to the place the list is highlighting.
 *
 * No masking fallback: use-poi.ts keeps the last good features on a failed
 * poll, and this tile shows their age and the error in muted text rather
 * than an empty or fabricated list (see the footer). When gnssCriticalAlert
 * is set the map holds its last good centre, draws no arrow and shows an amber
 * GNSS badge instead of trusting a position that may not be real.
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
  nearbyVesselFeed = null,
  aisCollisionAlarms,
  selfTrail,
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

  // Cycling summary (split layout): one ranked POI at a time gets the
  // highlight - its list row and marker ring - advancing every
  // summaryCycleSeconds through every POI in the ranked list, not only the
  // ones with a detail. Most POIs never get a detail at all (PoiFeature's
  // own doc: it's an editorial summary or a Wikipedia first sentence, when
  // the provider has one); on live OSM data that's often just one POI out
  // of several, and filtering the cycle down to only those used to leave it
  // with nothing to advance through. The highlight always lands somewhere;
  // the summary text underneath it only shows when that POI actually has
  // one (see PoiListRow below).
  // A plain, order-insensitive key rather than the array itself: identical
  // ids in a new array (every poll gives rankedList a fresh reference) must
  // not reset the cycle, nor must two places swapping rank, which changes
  // nothing on screen; a real change to the set - a POI dropping out of range
  // or a new one arriving - should. The camera's framing key uses it too. See
  // useCyclingIndex's own doc for why this is what it takes as resetKey.
  const placeSetKey = useMemo(() => rankedList.map((f) => f.id).sort().join('|'), [rankedList])
  const summaryCycleSeconds = config.summaryCycleSeconds ?? POI_MAP_SUMMARY_CYCLE_SECONDS_DEFAULT
  const cycleIndex = useCyclingIndex(rankedList.length, summaryCycleSeconds, placeSetKey)
  const expandedPoi = cycleIndex !== null ? rankedList[cycleIndex] : null
  const expandedPoiId = expandedPoi?.id ?? null

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
  const [listPaneRef, listPaneBox] = useMeasuredBox(config.layout === 'split')
  const heightPx = measuredBox.height > 0 ? measuredBox.height : FALLBACK_HEIGHT
  const widthPx = measuredBox.width > 0 ? measuredBox.width : FALLBACK_WIDTH

  // The zoom the configured range gives against whichever of width/height is
  // more restrictive. It frames the map only while nothing is ranked yet (the
  // range is how far Helmcentral searches, not how far the map shows), and is
  // what the map shows for the one frame before the camera effect runs.
  const rangeZoomFloor = zoomForRangeNm(config.rangeNm, latitude ?? 0, Math.min(widthPx, heightPx))

  // The camera: stays centred on the vessel and zooms out until the ranked
  // list (not every fetched feature - the ranked list is what the operator
  // can actually see named in the split layout's rows, and what topPoi
  // already limits to RANKED_LIST_SIZE) fits inside the measured viewport,
  // padded for marker size and labels. The fit's own zoom alone: the range no
  // longer limits how far in it goes, so a far-flung search area can still
  // frame the five nearest places closely, and places beyond them can fall off
  // the map. fitCameraAroundPoint's own maximum zoom still stops a lone close
  // place from zooming in without limit. Falls back to the vessel-centred range
  // zoom when there's nothing ranked yet (feed still loading, or "map" layout
  // content aside, topPoi is layout-independent).
  // Deliberately excludes cycleIndex/expandedPoiId from its own inputs: the
  // camera never moves to the highlighted place, an arrow points at it.
  const cameraTarget = useMemo((): { center: MapPoint; zoom: number } | null => {
    if (latitude === null || longitude === null) return null
    const vessel: MapPoint = { lat: latitude, lon: longitude }
    if (rankedList.length === 0) return { center: vessel, zoom: rangeZoomFloor }
    const points: MapPoint[] = rankedList.map((f) => ({ lat: f.lat, lon: f.lon }))
    const fit = fitCameraAroundPoint(vessel, points, widthPx, heightPx, POI_MAP_FIT_PADDING_PX)
    return { center: fit.center, zoom: fit.zoom }
  }, [latitude, longitude, rankedList, widthPx, heightPx, rangeZoomFloor])

  // The camera only ever jumps, and only when it must: the wall's processor
  // is too slow for eased or flown moves (each one also pulls in new tiles).
  // The first fix jumps to the overview. After that the boat marker moves and
  // the map stays still, until (a) the boat leaves the padded frame of the
  // camera the map is on, or (b) the framing key - the set of ranked places,
  // the measured box and the configured range - differs from the one last
  // framed. The places are sorted so that two of them swapping rank, which
  // changes nothing on screen, does not jump; the box is rounded so a
  // fractional resize does not either. Holding still while gnssCriticalAlert
  // is set (last good centre, no jump at all) is the same rule the
  // anchor-watch gate uses for the position sentinel; the framing key is
  // recorded only when a jump really ran, so a re-frame that was due during
  // the alert happens when it clears. This runs regardless of `interactive`:
  // it drives the camera imperatively (map.jumpTo), not through one of
  // maplibre's own gesture handlers.
  const framingKey = `${placeSetKey}|${Math.round(widthPx)}x${Math.round(heightPx)}|${config.rangeNm}`
  const framedRef = useRef<{ key: string; camera: { center: MapPoint; zoom: number } } | null>(null)
  // react-maplibre creates the map in a Promise, so mapRef is still empty when
  // the camera effect first runs. onLoad flips this and runs the effect again,
  // which frames a boat that never moves.
  const [mapReady, setMapReady] = useState(false)
  useEffect(() => {
    if (gnssCriticalAlert) return
    if (!cameraTarget || latitude === null || longitude === null) return
    const map = mapRef.current
    if (!map) return

    const framed = framedRef.current
    if (framed && framed.key === framingKey
      && pointInCameraFrame({ lat: latitude, lon: longitude }, framed.camera, widthPx, heightPx, POI_MAP_FIT_PADDING_PX)) return

    framedRef.current = { key: framingKey, camera: cameraTarget }
    map.jumpTo({ center: [cameraTarget.center.lon, cameraTarget.center.lat], zoom: cameraTarget.zoom, pitch: 0, bearing: 0 })
  }, [cameraTarget, gnssCriticalAlert, framingKey, widthPx, heightPx, latitude, longitude, mapReady])

  // Declutter POI marker labels on move-end, priority by rank then distance, vessels last in the order the feed gives them
  // (an unranked feature always yields to a ranked one, matching the ranked
  // list's own ordering; the highlighted place outranks both) — see
  // resolveMarkerLabelSuppression's own doc for why this is a pure, projected-screen-space computation. Kept regardless
  // of `interactive`: on the kiosk this only ever fires from the camera
  // effect's own jumpTo above, not from a user gesture, and the
  // labels still need to stay decluttered as POIs move on/off screen.
  const [suppressedLabelIds, setSuppressedLabelIds] = useState<Set<string>>(new Set())
  // Only the split layout has an arrow and a list to say which place is
  // current; a map-only tile keeps the plain rank order.
  const labelHighlightId = config.layout === 'split' ? expandedPoiId : null
  // The vessel list changes every five seconds as age_seconds ticks, and the
  // declutter pass projects every point, so the pass must not run on every new
  // list. This key says only when to run it: ids and positions rounded to about
  // 110 m. The grid is coarse on purpose: a contact under way crosses an 11 m
  // boundary on nearly every tick, but 110 m barely moves a label against the
  // 50 px collision radius. The pass itself reads the latest list from a ref and
  // projects real positions, so projection stays exact at any zoom.
  const nearbyVesselsRef = useRef(nearbyVessels)
  useEffect(() => { nearbyVesselsRef.current = nearbyVessels }, [nearbyVessels])
  const vesselLabelKey = useMemo(() => {
    if (!config.showAis) return ''
    return nearbyVessels
      .filter((v) => v.lat !== undefined && v.lon !== undefined && v.name !== '')
      .map((v) => `${v.id}@${Math.round(v.lat! * 1e3)},${Math.round(v.lon! * 1e3)}`)
      .join('|')
  }, [config.showAis, nearbyVessels])
  const declutterLabels = useCallback(() => {
    const map = mapRef.current
    if (!map || typeof map.project !== 'function') return
    const points: MarkerLabelPoint[] = poi.features.map((f) => ({
      id: f.id,
      ...map.project([f.lon, f.lat]),
      // The highlighted place always wins: it is the one the arrow points at,
      // so a neighbour must never hide its name.
      priority: f.id === labelHighlightId ? 0 : rankById[f.id] ?? 100000 + f.distanceM,
    }))
    if (config.showAis) {
      const labelled = nearbyVesselsRef.current.filter((v) => v.lat !== undefined && v.lon !== undefined && v.name !== '')
      // Alarmed vessels first, so a vessel in alarm keeps its name; places still outrank all of them.
      sortByCollisionAlarm(labelled, aisCollisionAlarms).forEach((v, i) => {
        points.push({ id: VESSEL_LABEL_PREFIX + v.id, ...map.project([v.lon!, v.lat!]), priority: VESSEL_LABEL_PRIORITY_BASE + i })
      })
    }
    const next = resolveMarkerLabelSuppression(points, [])
    setSuppressedLabelIds((prev) => (suppressedIdsEqual(prev, next) ? prev : next))
  }, [poi.features, rankById, labelHighlightId, config.showAis, aisCollisionAlarms])
  const handleLoad = useCallback(() => {
    setMapReady(true)
    declutterLabels()
  }, [declutterLabels])
  // Re-resolve when the highlight moves, not only when the map does.
  useEffect(() => { declutterLabels() }, [declutterLabels, vesselLabelKey])

  // selfTrail is a snapshot that is a new array only when a trail poll
  // lands points (use-server-trails.ts), so the GeoJSON follows the trail
  // poll, not the place fetch.
  const trailGeoJSON = useMemo(
    () => (config.showTrail ? trailToGeoJSON(selfTrail) : null),
    [config.showTrail, selfTrail],
  )

  // The active route (ADR 0092/0091): whole route as one line, plus the leg
  // into the next waypoint drawn again on top in a brighter, thicker stroke
  // so the leg actually being sailed reads at a glance. `activeRoute` is
  // already null whenever no route is active or its waypoints are empty
  // (LivePoiMapTile's job in poi-map-tile.tsx, not this tile's), so a plain line needs no extra guard
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

  // The Vessels group: the contacts inside the smaller of the configured range
  // and the feed's own search range, alarmed first and then nearest. The map
  // keeps drawing every contact; only the list is limited. It plays no part in
  // the highlight cycle, the arrow or the camera framing.
  const showVesselGroup = config.layout === 'split' && config.showAis
  const vesselLimitM = Math.min(config.rangeNm * METRES_PER_NM, nearbyVesselFeed?.maxRangeM ?? Infinity)
  const qualifyingVessels = useMemo(() => {
    if (!showVesselGroup) return []
    return selectListedVessels(nearbyVessels, vesselLimitM, aisCollisionAlarms)
  }, [showVesselGroup, nearbyVessels, vesselLimitM, aisCollisionAlarms])
  // Beside the places only when the pane is tall enough to hold everything
  // stacked, or wide enough for two columns. Below VESSELS_SIDE_BY_SIDE_MIN_WIDTH_PX
  // each column is too narrow for a name beside the distance and bearing, and a
  // place row also spends about 45 px on rank and icon. An unmeasured (0 x 0)
  // pane stays side by side.
  const stackVessels = listPaneBox.height >= VESSELS_STACK_MIN_HEIGHT_PX
    || (listPaneBox.width > 0 && listPaneBox.width < VESSELS_SIDE_BY_SIDE_MIN_WIDTH_PX)
  // A narrow pane that is also short cannot hold both groups, and the kiosk
  // cannot scroll to what is cut off. If a listed vessel is in collision alarm
  // or warning, its group goes first so the alarm stays in view.
  // Only when the group lists vessels: a message in their place has no alarm to keep in view.
  const vesselsFirst = stackVessels && listPaneBox.height < VESSELS_STACK_MIN_HEIGHT_PX
    && vesselGroupMessage({ vessels: qualifyingVessels, gnssCriticalAlert, feed: nearbyVesselFeed, limitM: vesselLimitM, distanceUnits }) === null
    && qualifyingVessels.some((v) => collisionTier(aisCollisionAlarms?.get(v.id)) !== null)

  const title = config.title.trim() || 'Nearby'
  const stale = isStale(positionLastUpdateAgeS)

  const placesContent = rankedList.length === 0 ? (
    <div className="flex h-full items-center justify-center text-center text-xs text-muted-foreground">
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
  )

  const placesGroup = <div key="places" className="flex min-h-0 flex-col gap-1">{placesContent}</div>
  const vesselGroup = (
    <VesselGroup
      key="vessels"
      vessels={qualifyingVessels}
      ownLat={latitude}
      ownLon={longitude}
      gnssCriticalAlert={gnssCriticalAlert}
      feed={nearbyVesselFeed}
      limitM={vesselLimitM}
      distanceUnits={distanceUnits}
      aisCollisionAlarms={aisCollisionAlarms}
      stacked={stackVessels}
      first={vesselsFirst}
    />
  )

  return (
    <Tile
      title={title}
      icon={<MapPin className="h-3.5 w-3.5 text-gauge-secondary" />}
      stale={stale}
      staleLabel={positionLastUpdateAgeS !== null ? formatDataAge(positionLastUpdateAgeS) : undefined}
      titleExtra={
        editing && onConfigure ? (
          <button type="button" className={tilePillButtonClass} onClick={onConfigure} aria-label={`Configure nearby map: ${title}`}>
            <Settings2 className="size-3.5" />
          </button>
        ) : undefined
      }
    >
      {/* A fixed row: the list's content must not stretch the map beside it,
          because a change in the map's measured size re-frames the camera. */}
      <div
        className={cn(
          'grid h-full min-h-0 grid-rows-[minmax(0,1fr)] gap-2',
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
                  // The camera effect above jumps to the vessel-centred fitted
                  // camera once the map has loaded (onLoad), and takes over
                  // from here - this is only what's on screen until it does.
                  zoom: rangeZoomFloor,
                }}
                style={{ width: '100%', height: '100%' }}
                mapStyle={mapStyle}
                interactive={interactive}
                dragRotate={false}
                onLoad={handleLoad}
                onMoveEnd={declutterLabels}
                // Same reasoning as anchor-watch-map.tsx: OpenSeaMap requires attribution, and
                // compact keeps it to a single "i" until tapped rather than a
                // permanent credit strip. useCollapsedMapAttribution's own
                // doc covers why this can't be done in CSS.
                attributionControl={{ compact: true }}
                onIdle={collapseAttribution}
              >
                {/* Sourceless anchor layer, mounted first so later rasters
                    (OpenSeaMap) have a stable beforeId to pin themselves under
                    the vector labels. */}
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
                  return (
                    <VesselMarker
                      key={vessel.id}
                      name={vessel.name}
                      lat={vessel.lat}
                      lon={vessel.lon}
                      alarmed={aisCollisionAlarms?.get(vessel.id) !== undefined}
                      suppressed={suppressedLabelIds.has(VESSEL_LABEL_PREFIX + vessel.id)}
                    />
                  )
                })}

                {poi.features.map((feature) => (
                  <PoiMarker
                    key={feature.id}
                    id={feature.id}
                    name={feature.name}
                    category={feature.category}
                    lat={feature.lat}
                    lon={feature.lon}
                    rank={rankById[feature.id]}
                    suppressed={suppressedLabelIds.has(feature.id)}
                    expanded={feature.id === expandedPoiId}
                  />
                ))}

                {latitude !== null && longitude !== null && (
                  <Marker latitude={latitude} longitude={longitude} style={VESSEL_MARKER_STYLE}>
                    <div className="flex items-center justify-center" style={{ width: 40, height: 40 }}>
                      <VesselArrow headingDeg={headingTrue} scale={1} />
                    </div>
                  </Marker>
                )}

                {/* The arrow from the boat to the highlighted place. Split
                    layout only: with no list beside it, a map-only tile has
                    nothing that says what the arrow points at. Not under a
                    GNSS alert, when the boat's position may not be real. */}
                {config.layout === 'split' && !gnssCriticalAlert && latitude !== null && longitude !== null && expandedPoi && (
                  <PoiArrow
                    boatLat={latitude}
                    boatLon={longitude}
                    placeLat={expandedPoi.lat}
                    placeLon={lonNearest(expandedPoi.lon, longitude)}
                    width={widthPx}
                    height={heightPx}
                  />
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
          <div ref={listPaneRef} data-testid="poi-list-pane" className="flex h-full min-h-0 flex-col gap-1 overflow-y-auto pr-1">
            {showVesselGroup ? (
              <div
                data-testid="poi-list-groups"
                data-stacked={stackVessels}
                data-vessels-first={vesselsFirst}
                className={cn('grid min-h-0 flex-1 gap-2', stackVessels ? 'grid-cols-1 content-start' : 'grid-cols-2')}
              >
                {vesselsFirst ? [vesselGroup, placesGroup] : [placesGroup, vesselGroup]}
              </div>
            ) : placesContent}
            <div className="mt-auto border-t border-border/60 pt-1 text-xs text-muted-foreground">
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

/**
 * The curved arrow from the boat to the highlighted place: an SVG over the map
 * rather than a map layer, so a change of place never makes the map redraw.
 * It sits inside <Map> and subscribes to the map's own 'move' event, so a pan
 * redraws this component alone and the tile above it is not involved. Plain
 * strings of whole pixels: a small movement of the boat gives the same strings
 * and React leaves the DOM alone.
 */
function PoiArrow({
  boatLat,
  boatLon,
  placeLat,
  placeLon,
  width,
  height,
}: {
  boatLat: number
  boatLon: number
  placeLat: number
  placeLon: number
  width: number
  height: number
}) {
  const { current: mapRef } = useMap()
  const map = mapRef?.getMap()
  const [geometry, setGeometry] = useState<PoiArrowGeometry | null>(null)
  // The 'move' subscription lives as long as the map, so a position fix does
  // not unsubscribe and resubscribe; it reads the latest inputs from a ref.
  const inputsRef = useRef({ boatLat, boatLon, placeLat, placeLon, width, height })
  const redraw = useCallback(() => {
    if (!map) return
    const i = inputsRef.current
    const next = poiArrowGeometry(
      map.project([i.boatLon, i.boatLat]),
      map.project([i.placeLon, i.placeLat]),
      { width: i.width, height: i.height },
    )
    setGeometry((prev) => (prev?.path === next?.path && prev?.head === next?.head ? prev : next))
  }, [map])

  useEffect(() => {
    if (!map) return
    map.on('move', redraw)
    return () => { map.off('move', redraw) }
  }, [map, redraw])

  // A layout effect, so a frame never paints a new highlight with the old arrow.
  useLayoutEffect(() => {
    inputsRef.current = { boatLat, boatLon, placeLat, placeLon, width, height }
    redraw()
  }, [redraw, boatLat, boatLon, placeLat, placeLon, width, height])

  return (
    <svg data-testid="poi-map-arrow" aria-hidden="true" className="pointer-events-none absolute inset-0 z-10 h-full w-full">
      {geometry && (
        <>
          {/* The wider stroke in the background token lifts the arrow off the chart. */}
          <path d={geometry.path} className="fill-none stroke-background" strokeWidth={5} strokeLinecap="round" />
          <path data-testid="poi-map-arrow-path" d={geometry.path} className="fill-none stroke-primary" strokeWidth={2.5} strokeLinecap="round" />
          <polygon points={geometry.head} className="fill-primary stroke-background" strokeWidth={1.5} strokeLinejoin="round" />
        </>
      )}
    </svg>
  )
}

// react-map-gl applies a Marker's style to maplibre's marker element, which is
// what stacks. The highlighted place is raised so a neighbour cannot cover it.
// An explicit base style is passed too: the library only applies styles it is
// given, so a marker that stops being highlighted would keep its raised index.
const BASE_MARKER_STYLE = { zIndex: 0 }
const RAISED_MARKER_STYLE = { zIndex: 1 }
const VESSEL_MARKER_STYLE = { zIndex: 2 }

// Memoised on flat primitives, not the feature object: usePoi builds new
// objects on every poll, which would defeat memo. A position fix renders no
// POI marker, and a highlight tick renders the two whose ring moves.
const PoiMarker = memo(function PoiMarker({
  id,
  name,
  category: categoryId,
  lat,
  lon,
  rank,
  suppressed,
  expanded,
}: {
  id: string
  name: string
  category: string
  lat: number
  lon: number
  rank: number | undefined
  suppressed: boolean
  expanded: boolean
}) {
  const category = poiCategoryById(categoryId)
  const Icon = category?.icon ?? MapPin
  return (
    <Marker latitude={lat} longitude={lon} style={expanded ? RAISED_MARKER_STYLE : BASE_MARKER_STYLE}>
      <div className="relative" aria-label={`Point of interest: ${name || category?.label || categoryId}`}>
        <div
          data-testid={expanded ? 'poi-marker-expanded' : undefined}
          className={cn(
            'relative flex h-9 w-9 items-center justify-center rounded-full border border-border bg-card shadow-md',
            // Subtle: the ranked list's own row already carries the
            // full summary, this ring only helps the eye match
            // that row back to its marker on the map.
            expanded && 'ring-2 ring-primary ring-offset-1 ring-offset-background',
          )}
        >
          <Icon className="h-5 w-5 text-foreground" />
          {rank !== undefined && (
            <span
              data-testid={`poi-marker-rank-${id}`}
              className="absolute -right-1 -top-1 flex h-5 w-5 items-center justify-center rounded-full bg-primary text-[9px] font-bold leading-none text-primary-foreground"
            >
              {rank}
            </span>
          )}
        </div>
        {/* Out of flow, so the marker box is only the icon: the projected
            point is the icon's centre, whether or not declutter has hidden
            the label, and the arrow's clearances are measured from it. */}
        {!suppressed && name && (
          <div className={`absolute left-1/2 top-full mt-0.5 max-w-28 -translate-x-1/2 truncate text-center text-xs font-semibold text-white ${MAP_LABEL_HALO}`}>
            {name}
          </div>
        )}
      </div>
    </Marker>
  )
})

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
    <div
      data-testid="poi-list-row"
      data-expanded={expanded}
      className={cn(
        'flex items-start gap-1.5 rounded-xs border-l-2 py-0.5 pr-1 pl-0.5',
        // Always a 2px border, transparent when not cycled-to, so the row's
        // own width never shifts as the cycle moves on - only its colour
        // does. This is the row's half of the highlight; the marker ring
        // above is the map's half. Neither depends on the POI having a
        // detail - most don't, and the row must still show which one is
        // current even when there's no summary text to make that obvious.
        expanded ? 'border-primary bg-muted' : 'border-transparent',
      )}
    >
      <span className="mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-primary text-[9px] font-bold leading-none text-primary-foreground">
        {rank}
      </span>
      <Icon className="mt-0.5 h-3.5 w-3.5 shrink-0 text-muted-foreground" />
      <div className="min-w-0 flex-1">
        <div className="flex items-baseline justify-between gap-1">
          <span className="truncate text-sm font-semibold text-foreground">{feature.name || category?.label || feature.category}</span>
          <span className="shrink-0 text-xs tabular-nums text-muted-foreground">
            {formatPoiDistance(feature.distanceM, distanceUnits)} {formatBearing(feature.bearingDeg)}
          </span>
        </div>
        {/* The cycle highlights every row in turn (see PoiMapTileImpl's
            cycling logic above), but most POIs have no detail at all - this
            only renders when the cycled-to row actually has one to show,
            and shows the whole thing (wrapped, capped at three lines)
            rather than the single truncated line every row used to carry. */}
        {expanded && feature.detail !== '' && (
          <div data-testid="poi-list-row-summary" className="mt-0.5 line-clamp-3 text-xs text-muted-foreground">
            {feature.detail}
          </div>
        )}
      </div>
    </div>
  )
}

// Memoised on flat primitives, like PoiMarker: the vessel list is rebuilt every
// five seconds as age_seconds ticks, and the wall's processor cannot afford a
// marker render for each contact each time.
const VesselMarker = memo(function VesselMarker({
  name,
  lat,
  lon,
  alarmed,
  suppressed,
}: {
  name: string
  lat: number
  lon: number
  alarmed: boolean
  suppressed: boolean
}) {
  return (
    <Marker latitude={lat} longitude={lon}>
      <div className="relative">
        <div
          className={cn(
            'flex h-7 w-7 items-center justify-center rounded-full shadow-lg',
            alarmed ? 'bg-red-600' : 'bg-amber-500/90',
          )}
          aria-label={`AIS vessel: ${name}`}
        >
          <Ship className="h-4 w-4 text-white" />
        </div>
        {/* Out of flow, as the place labels are, so the marker box stays the icon. */}
        {!suppressed && name && (
          <div
            data-testid="poi-vessel-label"
            className={`absolute left-1/2 top-full mt-0.5 max-w-28 -translate-x-1/2 truncate text-center text-xs font-semibold text-white ${MAP_LABEL_HALO}`}
          >
            {name}
          </div>
        )}
      </div>
    </Marker>
  )
})
