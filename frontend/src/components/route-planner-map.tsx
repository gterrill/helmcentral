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

const STYLE_LIGHT = 'https://basemaps.cartocdn.com/gl/positron-gl-style/style.json'
const STYLE_DARK = 'https://basemaps.cartocdn.com/gl/dark-matter-gl-style/style.json'
const OPENSEAMAP_TILES = 'https://tiles.openseamap.org/seamark/{z}/{x}/{y}.png'
const WORLD_IMAGERY_TILES = '/api/world-imagery/{z}/{x}/{y}'
const WORLD_IMAGERY_MAX_ZOOM = 18
const IMAGERY_ENABLED_KEY = 'routePlanner.imagery.enabled'

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
}

export function RoutePlannerMap({
  waypoints,
  onWaypointsChange,
  isDarkTheme,
  vesselLat = null,
  vesselLon = null,
  className,
  chartAvailable = isChartAvailable(),
}: RoutePlannerMapProps) {
  const mapRef = useRef<MapRef | null>(null)
  const suppressNextMapClickRef = useRef(false)
  const hasCenteredOnVesselRef = useRef(false)
  const [selectedIndex, setSelectedIndex] = useState<number | null>(null)
  const [showImageryLayer, setShowImageryLayer] = useState(readStoredImageryEnabled)
  const [currentZoom, setCurrentZoom] = useState(waypoints.length > 0 ? 12 : vesselLat !== null && vesselLon !== null ? 11 : 2)
  const worldImageryOpacity = computeWorldImageryOpacity(currentZoom, showImageryLayer)
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

  const handleImageryToggle = useCallback(() => {
    setShowImageryLayer((prev) => {
      const next = !prev
      window.localStorage.setItem(IMAGERY_ENABLED_KEY, String(next))
      return next
    })
  }, [])

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
        attributionControl={false}
        dragRotate={false}
        touchPitch={false}
      >
        {showImageryLayer && worldImageryOpacity > 0 && (
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
              paint={{
                'raster-opacity': worldImageryOpacity,
                'raster-fade-duration': 250,
              }}
            />
          </Source>
        )}

        <Source id="openseamap" type="raster" tiles={[OPENSEAMAP_TILES]} tileSize={256} attribution="© OpenSeaMap contributors">
          <Layer id="openseamap-layer" type="raster" paint={{ 'raster-opacity': 0.85 }} />
        </Source>

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
          onClick={handleImageryToggle}
          aria-label="Toggle satellite imagery"
          className={cn(
            'flex h-10 w-10 items-center justify-center rounded-lg text-white shadow backdrop-blur active:scale-95',
            showImageryLayer ? 'bg-sky-600/90 hover:bg-sky-500/90' : 'bg-black/65 hover:bg-black/80',
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
