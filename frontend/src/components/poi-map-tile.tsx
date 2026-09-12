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
import { formatBearing, poiCategoryById, topPoi, zoomForRangeNm, type PoiFeature } from '@/lib/poi'
import type { PoiMapWidgetConfig } from '@/lib/dashboard-widgets'
import { usePoi } from '@/hooks/use-poi'
import type { NearbyVessel } from '@/hooks/use-nearby-vessels'
import type { AlarmState } from '@/hooks/use-alarms'
import type { TrailPoint } from '@/hooks/use-server-trails'
import type { DistanceUnits } from '@/config/app-config'
import { formatDataAge, isStale } from '@/lib/staleness'
import { cn } from '@/lib/utils'
import { hasWebGL2 } from '@/lib/webgl'

export interface PoiMapTileProps {
  config: PoiMapWidgetConfig
  editing: boolean
  onConfigure?: () => void
  latitude: number | null
  longitude: number | null
  headingTrue: number | null
  gnssCriticalAlert: boolean
  positionLastUpdateAgeS: number | null
  nearbyVessels: NearbyVessel[]
  aisCollisionAlarms?: ReadonlyMap<string, AlarmState>
  getSelfTrail: () => TrailPoint[]
  isDarkTheme: boolean
  /** The instrument skin forces dark regardless of the app-wide theme. */
  forceDark?: boolean
  distanceUnits: DistanceUnits
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

// Fallback box for the one frame before ResizeObserver reports a real size
// (or in an environment, such as jsdom, that never fires it at all) — the
// same technique sea-state-tile.tsx's useMeasuredBox uses.
const FALLBACK_HEIGHT = 240

function useMeasuredHeight() {
  const ref = useRef<HTMLDivElement>(null)
  const [height, setHeight] = useState(0)

  useEffect(() => {
    const el = ref.current
    if (!el) return
    const observer = new ResizeObserver(([entry]) => {
      setHeight(entry.contentRect.height)
    })
    observer.observe(el)
    return () => observer.disconnect()
  }, [])

  return [ref, height] as const
}

function trailToGeoJSON(points: TrailPoint[]): GeoJSON.Feature<GeoJSON.LineString> {
  return {
    type: 'Feature',
    geometry: { type: 'LineString', coordinates: points.map((p) => [p.lon, p.lat]) },
    properties: {},
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
export function PoiMapTile({
  config,
  editing,
  onConfigure,
  latitude,
  longitude,
  headingTrue,
  gnssCriticalAlert,
  positionLastUpdateAgeS,
  nearbyVessels,
  aisCollisionAlarms,
  getSelfTrail,
  isDarkTheme,
  forceDark = false,
  distanceUnits,
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

  const dark = isDarkTheme || forceDark
  const mapStyle = dark ? STYLE_DARK : STYLE_LIGHT

  // WPE WebKit 2.38 (the wall-display kiosk browser) has no WebGL2, and
  // MapLibre 5 throws synchronously when it can't get a context — a single
  // Nearby tile mounting a map there would blank the whole feed (ADR 0089
  // §11). The ranked list beside it needs no WebGL at all, so only the map
  // pane itself degrades.
  const canRenderMap = hasWebGL2()

  const mapRef = useRef<MapRef | null>(null)
  const [mapContainerRef, measuredHeight] = useMeasuredHeight()
  const heightPx = measuredHeight > 0 ? measuredHeight : FALLBACK_HEIGHT
  const zoom = zoomForRangeNm(config.rangeNm, latitude ?? 0, heightPx)

  const hasCenteredRef = useRef(false)
  const lastEaseAtRef = useRef(0)

  // Follow the vessel: first fix jumps straight there, later fixes ease in,
  // throttled to at most one per FOLLOW_THROTTLE_MS. Holding still while
  // gnssCriticalAlert is set (last good centre, no jump/ease at all) is the
  // same rule the anchor-watch gate uses for the position sentinel.
  useEffect(() => {
    if (gnssCriticalAlert) return
    if (latitude === null || longitude === null) return
    const map = mapRef.current
    if (!map) return

    if (!hasCenteredRef.current) {
      hasCenteredRef.current = true
      lastEaseAtRef.current = Date.now()
      map.jumpTo({ center: [longitude, latitude], zoom })
      return
    }

    const now = Date.now()
    if (now - lastEaseAtRef.current < FOLLOW_THROTTLE_MS) return
    lastEaseAtRef.current = now
    map.easeTo({ center: [longitude, latitude], zoom, duration: FOLLOW_EASE_DURATION_MS })
  }, [latitude, longitude, gnssCriticalAlert, zoom])

  // Declutter POI marker labels on move-end, priority by rank then distance
  // (an unranked feature always yields to a ranked one, matching the ranked
  // list's own ordering) — see resolveMarkerLabelSuppression's own doc for
  // why this is a pure, projected-screen-space computation.
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
                  zoom,
                }}
                style={{ width: '100%', height: '100%' }}
                mapStyle={mapStyle}
                dragRotate={false}
                onLoad={declutterLabels}
                onMoveEnd={declutterLabels}
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
                  return (
                    <Marker key={feature.id} latitude={feature.lat} longitude={feature.lon}>
                      <div className="flex flex-col items-center" aria-label={`Point of interest: ${feature.name || category?.label || feature.category}`}>
                        <div className="relative flex h-7 w-7 items-center justify-center rounded-full border border-border bg-card shadow-md">
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
                  className="absolute left-2 top-2 z-20 rounded-sm border border-amber-500/40 bg-amber-500/10 px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-amber-600 dark:text-amber-400"
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
                <PoiListRow key={feature.id} rank={index + 1} feature={feature} distanceUnits={distanceUnits} />
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

function PoiListRow({ rank, feature, distanceUnits }: { rank: number; feature: PoiFeature; distanceUnits: DistanceUnits }) {
  const category = poiCategoryById(feature.category)
  const Icon = category?.icon ?? MapPin
  return (
    <div className="flex items-start gap-1.5 rounded-sm px-1 py-0.5" data-testid="poi-list-row">
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
        {feature.detail && (
          <div className="truncate text-[10px] text-muted-foreground">{feature.detail}</div>
        )}
      </div>
    </div>
  )
}
