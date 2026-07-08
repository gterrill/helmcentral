import 'maplibre-gl/dist/maplibre-gl.css'
import maplibregl from 'maplibre-gl'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { MapRef, MarkerDragEvent } from 'react-map-gl/maplibre'
import { Map, Marker, Source, Layer } from 'react-map-gl/maplibre'
import { Crosshair, Minus, Plus, Satellite } from 'lucide-react'
import { cn } from '@/lib/utils'
import { haversineMeters, bearingDeg, destinationPoint } from '@/lib/geo'
import { formatNm } from '@/lib/route-calc'
import { computeWorldImageryOpacity } from '@/components/anchor-watch-map'
import type { RouteWaypoint } from '@/hooks/use-routes'
import { useGshhgCoastline } from '@/hooks/use-gshhg-coastline'
import { isChartAvailable } from '@/lib/chart-availability'
import type { SatChart } from '@/hooks/use-sat-charts'

const STYLE_LIGHT = 'https://basemaps.cartocdn.com/gl/positron-gl-style/style.json'
const STYLE_DARK = 'https://basemaps.cartocdn.com/gl/dark-matter-gl-style/style.json'
const OPENSEAMAP_TILES = 'https://tiles.openseamap.org/seamark/{z}/{x}/{y}.png'
const WORLD_IMAGERY_TILES = '/api/world-imagery/{z}/{x}/{y}'
const WORLD_IMAGERY_MAX_ZOOM = 18
const IMAGERY_ENABLED_KEY = 'routePlanner.imagery.enabled'

/**
 * Fill/background layer ids to hide from the already-loaded Carto base
 * style (Positron/Dark Matter) when hybrid satellite mode is on, so
 * satellite imagery can show through in their place while roads, paths,
 * and labels (not in this list) stay legible on top. Identical in both
 * light and dark base styles.
 */
export const HYBRID_HIDDEN_LAYER_IDS = [
  'background', 'landcover', 'park_national_park', 'park_nature_reserve',
  'landuse_residential', 'landuse', 'water', 'water_shadow',
  'building', 'building-top',
] as const

// Both STYLE_LIGHT and STYLE_DARK declare "background" as their first
// layer (confirmed directly against both style documents) - a stable
// constant rather than a value computed from the loaded style and stored
// in state, since a state-driven beforeId raced with react-map-gl's
// internal re-add-managed-layers handling on style reload (switching
// STYLE_LIGHT/STYLE_DARK), intermittently dropping world-imagery entirely.
const BASE_STYLE_FIRST_LAYER_ID = 'background'

/**
 * All text-label layers (place names, road names, water names, POI) in the
 * base style. Both STYLE_LIGHT and STYLE_DARK style these for a plain
 * basemap background - muted colors with a thin, often semi-transparent
 * halo (e.g. Positron's place labels use `rgba(255,255,255,0.5)`, a halo
 * meant to soften edges against an already-near-white background). Over
 * photographic satellite imagery of varying brightness, that gives almost
 * no contrast. When hybrid satellite mode is on, these get a strong
 * white-text/dark-halo override instead (the same convention real hybrid
 * map styles use, e.g. Google/Mapbox/Apple satellite-hybrid views),
 * applied regardless of the app's own light/dark theme since satellite
 * brightness at a given location has nothing to do with that setting.
 */
export const HYBRID_LABEL_LAYER_IDS = [
  'watername_ocean', 'watername_sea', 'watername_lake', 'watername_lake_line',
  'place_hamlet', 'place_suburbs', 'place_villages', 'place_town',
  'place_country_2', 'place_country_1', 'place_state', 'place_continent',
  'place_city_r6', 'place_city_r5', 'place_city_dot_r7', 'place_city_dot_r4',
  'place_city_dot_r2', 'place_city_dot_z7', 'place_capital_dot_z7',
  'poi_stadium', 'poi_park',
  'roadname_minor', 'roadname_sec', 'roadname_pri', 'roadname_major',
] as const

const HYBRID_LABEL_PAINT_PROPS = ['text-color', 'text-halo-color', 'text-halo-width'] as const
const HYBRID_LABEL_TEXT_COLOR = '#ffffff'
const HYBRID_LABEL_HALO_COLOR = '#000000'
const HYBRID_LABEL_HALO_WIDTH = 1.5

function readStoredImageryEnabled(): boolean {
  if (typeof window === 'undefined') return false
  return window.localStorage.getItem(IMAGERY_ENABLED_KEY) === 'true'
}

function routeToGeoJSON(waypoints: RouteWaypoint[]): GeoJSON.Feature<GeoJSON.LineString> {
  return {
    type: 'Feature',
    geometry: {
      type: 'LineString',
      coordinates: waypoints.map((wp) => [wp.lon, wp.lat]),
    },
    properties: {},
  }
}

function legMidpoint(from: RouteWaypoint, to: RouteWaypoint): [number, number] {
  const distanceM = haversineMeters(from.lat, from.lon, to.lat, to.lon)
  const bearingRad = (bearingDeg(from.lat, from.lon, to.lat, to.lon) * Math.PI) / 180
  return destinationPoint(from.lat, from.lon, bearingRad, distanceM / 2)
}

export interface RoutePlannerMapProps {
  waypoints: RouteWaypoint[]
  onWaypointsChange: (waypoints: RouteWaypoint[]) => void
  isDarkTheme: boolean
  vesselLat?: number | null
  vesselLon?: number | null
  className?: string
  /**
   * Whether a navigation-grade chart is available for the current view.
   * STUB: real S-57 coverage detection doesn't exist yet — defaults to the
   * placeholder in lib/chart-availability.ts, which always reports false.
   * See docs/adr/0009-gshhg-coastline-fallback.md.
   */
  chartAvailable?: boolean
  /** User-uploaded MBTiles satellite charts to render, if any. */
  satCharts?: SatChart[]
}

export function RoutePlannerMap({
  waypoints,
  onWaypointsChange,
  isDarkTheme,
  vesselLat = null,
  vesselLon = null,
  className,
  chartAvailable = isChartAvailable(),
  satCharts = [],
}: RoutePlannerMapProps) {
  const mapRef = useRef<MapRef | null>(null)
  const suppressNextMapClickRef = useRef(false)
  const hasCenteredOnVesselRef = useRef(false)
  const [selectedIndex, setSelectedIndex] = useState<number | null>(null)
  const [showHybridSatellite, setShowHybridSatellite] = useState(readStoredImageryEnabled)
  const [currentZoom, setCurrentZoom] = useState(waypoints.length > 0 ? 12 : vesselLat !== null && vesselLon !== null ? 11 : 2)
  const worldImageryOpacity = computeWorldImageryOpacity(currentZoom, showHybridSatellite)
  const { data: coastlineData } = useGshhgCoastline()
  const showCoastlineFallback = !chartAvailable && coastlineData !== null

  useEffect(() => {
    if (showCoastlineFallback) {
      console.info('[gshhg-coastline-fallback] No chart available for current view — rendering GSHHG reference coastline fallback')
    }
  }, [showCoastlineFallback])

  const handleZoomChange = useCallback(() => {
    const z = mapRef.current?.getZoom()
    if (z !== undefined) setCurrentZoom(z)
  }, [])

  const handleHybridToggle = useCallback(() => {
    setShowHybridSatellite((prev) => {
      const next = !prev
      window.localStorage.setItem(IMAGERY_ENABLED_KEY, String(next))
      return next
    })
  }, [])

  // Captures each label layer's own theme-native paint values (text-color/
  // text-halo-color/text-halo-width) the first time it's seen after a style
  // load, before any hybrid override is ever applied - setPaintProperty(id,
  // name, undefined) does NOT restore a layer's stylesheet-defined value,
  // it resets to MapLibre's generic spec default (confirmed by reading
  // maplibre-gl's source: an undefined value falls through to
  // `property.specification.default`, e.g. plain black text with no halo),
  // so reverting requires explicitly stored original values. Cleared on
  // every theme switch (see the isDarkTheme effect below) so it re-captures
  // fresh from whichever style just loaded, rather than reapplying stale
  // values from the previous theme.
  const originalLabelPaintRef = useRef<Record<string, Partial<Record<typeof HYBRID_LABEL_PAINT_PROPS[number], unknown>>>>({})

  const applyHybridVisibility = useCallback(() => {
    const map = mapRef.current?.getMap()
    if (!map || !map.isStyleLoaded()) return
    const visibility = showHybridSatellite ? 'none' : 'visible'
    for (const id of HYBRID_HIDDEN_LAYER_IDS) {
      if (map.getLayer(id)) map.setLayoutProperty(id, 'visibility', visibility)
    }
    for (const id of HYBRID_LABEL_LAYER_IDS) {
      if (!map.getLayer(id)) continue
      if (!originalLabelPaintRef.current[id]) {
        const original: Partial<Record<typeof HYBRID_LABEL_PAINT_PROPS[number], unknown>> = {}
        for (const prop of HYBRID_LABEL_PAINT_PROPS) {
          original[prop] = map.getPaintProperty(id, prop)
        }
        originalLabelPaintRef.current[id] = original
      }
      if (showHybridSatellite) {
        map.setPaintProperty(id, 'text-color', HYBRID_LABEL_TEXT_COLOR)
        map.setPaintProperty(id, 'text-halo-color', HYBRID_LABEL_HALO_COLOR)
        map.setPaintProperty(id, 'text-halo-width', HYBRID_LABEL_HALO_WIDTH)
      } else {
        const original = originalLabelPaintRef.current[id]
        for (const prop of HYBRID_LABEL_PAINT_PROPS) {
          map.setPaintProperty(id, prop, original[prop])
        }
      }
    }
  }, [showHybridSatellite])

  useEffect(() => {
    applyHybridVisibility()
  }, [applyHybridVisibility])

  // Switching STYLE_LIGHT/STYLE_DARK is a full MapLibre style reload
  // (setStyle), and satellite imagery reliably fails to redraw afterward
  // even though the layer/source end up correctly configured (confirmed via
  // direct inspection: correct z-order, opacity, visibility, tiles fetched
  // successfully) - a MapLibre-level rendering quirk after a full style
  // swap, not a bug in this component's own logic, and not fixable from
  // here with a repaint/resize nudge or a forced remount (both tried,
  // neither reliably worked). Turning hybrid mode off on theme change
  // avoids ever showing that broken state; the user just re-toggles it.
  const isDarkThemeRef = useRef(isDarkTheme)
  useEffect(() => {
    if (isDarkThemeRef.current !== isDarkTheme) {
      isDarkThemeRef.current = isDarkTheme
      // The new style's label layers have their own native paint values,
      // distinct from the previous theme's - forget what was captured so
      // applyHybridVisibility re-captures fresh instead of reapplying the
      // old theme's colors onto the new one.
      originalLabelPaintRef.current = {}
      setShowHybridSatellite((prev) => {
        if (!prev) return prev
        window.localStorage.setItem(IMAGERY_ENABLED_KEY, 'false')
        return false
      })
    }
  }, [isDarkTheme])

  const routeGeoJSON = useMemo(() => routeToGeoJSON(waypoints), [waypoints])

  const legs = useMemo(() => {
    const result: Array<{ key: string; midLat: number; midLon: number; distanceM: number; bearing: number }> = []
    for (let i = 0; i < waypoints.length - 1; i++) {
      const from = waypoints[i]
      const to = waypoints[i + 1]
      const [midLat, midLon] = legMidpoint(from, to)
      result.push({
        key: `leg-${i}`,
        midLat,
        midLon,
        distanceM: haversineMeters(from.lat, from.lon, to.lat, to.lon),
        bearing: Math.round(bearingDeg(from.lat, from.lon, to.lat, to.lon)),
      })
    }
    return result
  }, [waypoints])

  const handleMapClick = useCallback(
    (e: maplibregl.MapMouseEvent) => {
      if (suppressNextMapClickRef.current) {
        suppressNextMapClickRef.current = false
        return
      }
      const { lat, lng } = e.lngLat
      onWaypointsChange([...waypoints, { lat, lon: lng }])
    },
    [waypoints, onWaypointsChange],
  )

  const handleMarkerClick = useCallback((idx: number) => {
    suppressNextMapClickRef.current = true
    setSelectedIndex((current) => (current === idx ? null : idx))
  }, [])

  const handleMarkerDragEnd = useCallback(
    (idx: number, e: MarkerDragEvent) => {
      const next = waypoints.map((wp, i) => (i === idx ? { ...wp, lat: e.lngLat.lat, lon: e.lngLat.lng } : wp))
      onWaypointsChange(next)
    },
    [waypoints, onWaypointsChange],
  )

  const handleDeleteSelected = useCallback(() => {
    if (selectedIndex === null) return
    onWaypointsChange(waypoints.filter((_, i) => i !== selectedIndex))
    setSelectedIndex(null)
  }, [selectedIndex, waypoints, onWaypointsChange])

  const handleZoomIn = useCallback(() => {
    mapRef.current?.easeTo({ zoom: (mapRef.current.getZoom() ?? 12) + 1, duration: 250 })
  }, [])

  const handleZoomOut = useCallback(() => {
    mapRef.current?.easeTo({ zoom: (mapRef.current.getZoom() ?? 12) - 1, duration: 250 })
  }, [])

  const handleFitBounds = useCallback(() => {
    const map = mapRef.current
    if (!map) return
    if (waypoints.length === 0) {
      if (vesselLat !== null && vesselLon !== null) {
        map.easeTo({ center: [vesselLon, vesselLat], zoom: 11, duration: 400 })
      }
      return
    }
    if (waypoints.length === 1) {
      map.easeTo({ center: [waypoints[0].lon, waypoints[0].lat], duration: 400 })
      return
    }
    let minLat = waypoints[0].lat
    let maxLat = waypoints[0].lat
    let minLon = waypoints[0].lon
    let maxLon = waypoints[0].lon
    for (const wp of waypoints) {
      minLat = Math.min(minLat, wp.lat)
      maxLat = Math.max(maxLat, wp.lat)
      minLon = Math.min(minLon, wp.lon)
      maxLon = Math.max(maxLon, wp.lon)
    }
    map.fitBounds(
      [[minLon, minLat], [maxLon, maxLat]],
      { padding: 60, duration: 400 },
    )
  }, [waypoints, vesselLat, vesselLon])

  const initialViewState = useMemo(
    () => ({
      longitude: waypoints[0]?.lon ?? vesselLon ?? 0,
      latitude: waypoints[0]?.lat ?? vesselLat ?? 0,
      zoom: waypoints.length > 0 ? 12 : vesselLat !== null && vesselLon !== null ? 11 : 2,
    }),
    // Only used as initial value — no deps to avoid re-centering on every update
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [],
  )

  // If the vessel's GPS fix arrives shortly after the map mounts (it's polled
  // asynchronously), recenter once — but only while the user hasn't started
  // placing waypoints, so we never fight with their own panning.
  useEffect(() => {
    if (hasCenteredOnVesselRef.current) return
    if (waypoints.length > 0) return
    if (vesselLat === null || vesselLon === null) return
    hasCenteredOnVesselRef.current = true
    mapRef.current?.easeTo({ center: [vesselLon, vesselLat], zoom: 11, duration: 400 })
  }, [vesselLat, vesselLon, waypoints.length])

  const mapStyle = isDarkTheme ? STYLE_DARK : STYLE_LIGHT

  return (
    <div className={cn('relative overflow-hidden rounded-lg', className)} data-testid="route-planner-map">
      <Map
        ref={mapRef}
        mapLib={maplibregl}
        initialViewState={initialViewState}
        style={{ width: '100%', height: '100%' }}
        mapStyle={mapStyle}
        onClick={handleMapClick}
        onZoom={handleZoomChange}
        onLoad={applyHybridVisibility}
        onStyleData={applyHybridVisibility}
        attributionControl={false}
        dragRotate={false}
        touchPitch={false}
      >
        {/*
          Invisible, sourceless anchor layer. react-map-gl's addLayer call
          has no awareness of JSX sibling order, so a layer that mounts
          later (e.g. world-imagery toggled on mid-session, or a satellite
          chart uploaded after the route line is already showing) is
          otherwise appended on top of the whole stack, covering the route
          line and coastline fallback outright. Unlike anchor-watch-map.tsx,
          this map has no vector layer that's unconditionally present (route
          line needs 2+ waypoints, the coastline fallback needs chart data
          loaded) to anchor against, so this dedicated layer exists purely
          to be that always-present anchor - a "background" layer needs no
          source/data, so it mounts as soon as the style loads, before
          anything else has a chance to.
        */}
        <Layer id="raster-overlay-anchor" type="background" paint={{ 'background-opacity': 0 }} />

        {showHybridSatellite && worldImageryOpacity > 0 && (
          <Source
            id="world-imagery"
            type="raster"
            tiles={[WORLD_IMAGERY_TILES]}
            tileSize={256}
            maxzoom={WORLD_IMAGERY_MAX_ZOOM}
            attribution="Source: Esri, Maxar, Earthstar Geographics"
          >
            <Layer
              id="world-imagery-layer"
              type="raster"
              beforeId={BASE_STYLE_FIRST_LAYER_ID}
              paint={{
                'raster-opacity': worldImageryOpacity,
                'raster-fade-duration': 250,
              }}
            />
          </Source>
        )}

        <Source id="openseamap" type="raster" tiles={[OPENSEAMAP_TILES]} tileSize={256} attribution="© OpenSeaMap contributors">
          <Layer id="openseamap-layer" type="raster" beforeId="raster-overlay-anchor" paint={{ 'raster-opacity': 0.85 }} />
        </Source>

        {satCharts.map((chart) => (
          <Source
            key={chart.id}
            id={`sat-chart-${chart.id}`}
            type="raster"
            tiles={[`/api/sat-charts/${chart.id}/{z}/{x}/{y}`]}
            tileSize={256}
            bounds={chart.bounds}
            minzoom={chart.minzoom}
            maxzoom={chart.maxzoom}
            attribution="User-supplied satellite chart"
          >
            <Layer
              id={`sat-chart-${chart.id}-layer`}
              type="raster"
              beforeId="raster-overlay-anchor"
              paint={{ 'raster-opacity': 1, 'raster-fade-duration': 250 }}
            />
          </Source>
        ))}

        {waypoints.length >= 2 && (
          <Source id="route-line" type="geojson" data={routeGeoJSON}>
            <Layer
              id="route-line-layer"
              type="line"
              paint={{
                'line-color': isDarkTheme ? '#38bdf8' : '#0284c7',
                'line-width': 3,
                'line-opacity': 0.85,
              }}
              layout={{ 'line-join': 'round', 'line-cap': 'round' }}
            />
          </Source>
        )}

        {showCoastlineFallback && (
          <Source id="gshhg-coastline" type="geojson" data={coastlineData}>
            <Layer
              id="gshhg-coastline-fill"
              type="fill"
              paint={{
                'fill-color': isDarkTheme ? '#7c6f57' : '#d9c8a3',
                'fill-opacity': 0.35,
              }}
            />
            <Layer
              id="gshhg-coastline-outline"
              type="line"
              paint={{
                'line-color': isDarkTheme ? '#a89968' : '#9c8a5c',
                'line-width': 1,
                'line-dasharray': [2, 2],
              }}
            />
          </Source>
        )}

        {legs.map((leg) => (
          <Marker key={leg.key} latitude={leg.midLat} longitude={leg.midLon}>
            <div className="pointer-events-none whitespace-nowrap rounded bg-black/60 px-1.5 py-0.5 text-[10px] text-white">
              {formatNm(leg.distanceM)} / {leg.bearing}°
            </div>
          </Marker>
        ))}

        {waypoints.map((wp, idx) => (
          <Marker
            key={`wp-${idx}`}
            latitude={wp.lat}
            longitude={wp.lon}
            draggable
            onDragEnd={(e) => handleMarkerDragEnd(idx, e)}
            style={{ zIndex: selectedIndex === idx ? 1000 : 100 }}
          >
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation()
                handleMarkerClick(idx)
              }}
              className={cn(
                'flex h-7 w-7 items-center justify-center rounded-full text-xs font-bold text-white shadow-lg ring-2 ring-white/80',
                selectedIndex === idx ? 'bg-red-600' : 'bg-primary',
              )}
              aria-label={`Waypoint ${idx + 1}${wp.name ? `: ${wp.name}` : ''}`}
            >
              {idx + 1}
            </button>
          </Marker>
        ))}
      </Map>

      {selectedIndex !== null && (
        <div className="pointer-events-none absolute inset-x-0 bottom-4 flex justify-center">
          <div className="pointer-events-auto flex items-center gap-2 rounded-full bg-black/70 px-3 py-1.5 text-xs text-white backdrop-blur">
            <span>Waypoint {selectedIndex + 1}</span>
            <button
              type="button"
              onClick={handleDeleteSelected}
              className="rounded-full bg-red-600/90 px-2 py-0.5 hover:bg-red-600"
            >
              Delete
            </button>
            <button
              type="button"
              onClick={() => setSelectedIndex(null)}
              className="rounded-full bg-white/20 px-2 py-0.5 hover:bg-white/30"
            >
              Done
            </button>
          </div>
        </div>
      )}

      <div className="pointer-events-auto absolute right-3 top-3 flex flex-col gap-1" style={{ zIndex: 2100 }} data-testid="route-planner-controls">
        <button
          onClick={handleZoomIn}
          aria-label="Zoom in"
          className="flex h-9 w-9 items-center justify-center rounded-lg bg-black/65 text-white shadow backdrop-blur hover:bg-black/80 active:scale-95"
        >
          <Plus className="h-4 w-4" />
        </button>
        <button
          onClick={handleZoomOut}
          aria-label="Zoom out"
          className="flex h-9 w-9 items-center justify-center rounded-lg bg-black/65 text-white shadow backdrop-blur hover:bg-black/80 active:scale-95"
        >
          <Minus className="h-4 w-4" />
        </button>
        <button
          onClick={handleHybridToggle}
          aria-label="Toggle satellite imagery"
          className={cn(
            'flex h-9 w-9 items-center justify-center rounded-lg text-white shadow backdrop-blur active:scale-95',
            showHybridSatellite ? 'bg-sky-600/90 hover:bg-sky-500/90' : 'bg-black/65 hover:bg-black/80',
          )}
          style={{ transition: 'background-color 150ms ease-out' }}
        >
          <Satellite className="h-4 w-4" />
        </button>
        <button
          onClick={handleFitBounds}
          aria-label={waypoints.length > 0 ? 'Fit route in view' : 'Center on current position'}
          className="flex h-9 w-9 items-center justify-center rounded-lg bg-black/65 text-white shadow backdrop-blur hover:bg-black/80 active:scale-95"
        >
          <Crosshair className="h-4 w-4" />
        </button>
      </div>

      {waypoints.length === 0 && (
        <div className="pointer-events-none absolute inset-x-0 top-3 flex justify-center">
          <div className="rounded-full bg-black/55 px-3 py-1.5 text-xs text-white/80 backdrop-blur">
            Click the map to add your first waypoint
          </div>
        </div>
      )}

      {showCoastlineFallback && (
        <div className="pointer-events-none absolute bottom-4 left-3" style={{ zIndex: 2000 }}>
          <div className="rounded-full bg-black/55 px-3 py-1.5 text-xs text-white/80 backdrop-blur">
            No chart data — reference coastline only
          </div>
        </div>
      )}
    </div>
  )
}
