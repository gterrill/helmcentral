import 'maplibre-gl/dist/maplibre-gl.css'
import maplibregl from 'maplibre-gl'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { MapRef } from 'react-map-gl/maplibre'
import { Map, Marker, Source, Layer } from 'react-map-gl/maplibre'
import { Anchor, Crosshair, MapPin, Minus, Plus, Ship } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { NearbyVessel } from '@/hooks/use-nearby-vessels'
import type { TrailPoint } from '@/hooks/use-vessel-trail'

// ── Map style URLs (Carto, no API key required) ─────────────────────────────
const STYLE_LIGHT = 'https://basemaps.cartocdn.com/gl/positron-gl-style/style.json'
const STYLE_DARK = 'https://basemaps.cartocdn.com/gl/dark-matter-gl-style/style.json'
const OPENSEAMAP_TILES = 'https://tiles.openseamap.org/seamark/{z}/{x}/{y}.png'

// ── Haversine distance (m) ──────────────────────────────────────────────────
function haversineMeters(lat1: number, lon1: number, lat2: number, lon2: number): number {
  const R = 6371000
  const toRad = (d: number) => (d * Math.PI) / 180
  const dLat = toRad(lat2 - lat1)
  const dLon = toRad(lon2 - lon1)
  const a =
    Math.sin(dLat / 2) ** 2 +
    Math.cos(toRad(lat1)) * Math.cos(toRad(lat2)) * Math.sin(dLon / 2) ** 2
  return R * 2 * Math.atan2(Math.sqrt(a), Math.sqrt(1 - a))
}

// ── Initial bearing ─────────────────────────────────────────────────────────
function bearingDeg(lat1: number, lon1: number, lat2: number, lon2: number): number {
  const toRad = (d: number) => (d * Math.PI) / 180
  const toDeg = (r: number) => (r * 180) / Math.PI
  const dLon = toRad(lon2 - lon1)
  const y = Math.sin(dLon) * Math.cos(toRad(lat2))
  const x =
    Math.cos(toRad(lat1)) * Math.sin(toRad(lat2)) -
    Math.sin(toRad(lat1)) * Math.cos(toRad(lat2)) * Math.cos(dLon)
  return ((toDeg(Math.atan2(y, x)) + 360) % 360)
}

// ── Destination point given start + bearing + distance ──────────────────────
function destinationPoint(lat: number, lon: number, bearingRad: number, distanceM: number): [number, number] {
  const R = 6371000
  const lat1 = (lat * Math.PI) / 180
  const lon1 = (lon * Math.PI) / 180
  const lat2 = Math.asin(
    Math.sin(lat1) * Math.cos(distanceM / R) +
    Math.cos(lat1) * Math.sin(distanceM / R) * Math.cos(bearingRad),
  )
  const lon2 =
    lon1 +
    Math.atan2(
      Math.sin(bearingRad) * Math.sin(distanceM / R) * Math.cos(lat1),
      Math.cos(distanceM / R) - Math.sin(lat1) * Math.sin(lat2),
    )
  return [(lat2 * 180) / Math.PI, ((lon2 * 180) / Math.PI + 540) % 360 - 180]
}

// ── Generate a circle GeoJSON polygon ──────────────────────────────────────
function generateCircleGeoJSON(anchorLon: number, anchorLat: number, radiusM: number): GeoJSON.Feature<GeoJSON.Polygon> {
  const steps = 64
  const coordinates: [number, number][] = []
  for (let i = 0; i <= steps; i++) {
    const bearing = (i / steps) * 2 * Math.PI
    const [lat, lon] = destinationPoint(anchorLat, anchorLon, bearing, radiusM)
    coordinates.push([lon, lat])
  }
  return {
    type: 'Feature',
    geometry: { type: 'Polygon', coordinates: [coordinates] },
    properties: {},
  }
}

// ── Trail to GeoJSON LineString ─────────────────────────────────────────────
function trailToGeoJSON(points: TrailPoint[]): GeoJSON.Feature<GeoJSON.LineString> {
  return {
    type: 'Feature',
    geometry: {
      type: 'LineString',
      coordinates: points.map((p) => [p.lon, p.lat]),
    },
    properties: {},
  }
}

// ── Zoom level from radius (show ~4× radius diameter in view) ───────────────
function zoomForRadius(radiusM: number): number {
  // Approximate: zoom 14 ≈ 300m radius nicely visible
  return Math.max(10, Math.min(17, 14 - Math.log2(radiusM / 300)))
}

// ── Nudge distance for arrow keys (metres) ─────────────────────────────────
const NUDGE_METERS = 1.0

type EditMode = 'none' | 'reposition' | 'radius'

interface TransientInfo {
  lat: number
  lon: number
  distanceM: number
  bearing: number
  label: string
  expiresMs: number
}

export interface AnchorWatchMapProps {
  vesselLat: number
  vesselLon: number
  vesselHeadingDeg: number | null
  anchorLat: number
  anchorLon: number
  radiusMeters: number
  distanceMeters: number | null
  bearingDeg: number | null
  isImperial: boolean
  vesselTrail: () => TrailPoint[]
  aisVessels: NearbyVessel[]
  aisTrails: () => Map<string, TrailPoint[]>
  isDarkTheme: boolean
  onAnchorReposition: (lat: number, lon: number) => void
  onRadiusChange: (radiusMeters: number) => void
  onClearAnchor: () => void
  className?: string
}

export function AnchorWatchMap({
  vesselLat,
  vesselLon,
  vesselHeadingDeg,
  anchorLat,
  anchorLon,
  radiusMeters,
  distanceMeters,
  bearingDeg: bearingDegProp,
  isImperial,
  vesselTrail,
  aisVessels,
  aisTrails,
  isDarkTheme,
  onAnchorReposition,
  onRadiusChange,
  onClearAnchor,
  className,
}: AnchorWatchMapProps) {
  const mapRef = useRef<MapRef | null>(null)
  const [editMode, setEditMode] = useState<EditMode>('none')
  const [ghostAnchor, setGhostAnchor] = useState<{ lat: number; lon: number } | null>(null)
  const [liveRadius, setLiveRadius] = useState<number | null>(null)
  const originalRadiusRef = useRef<number>(radiusMeters)
  const [transient, setTransient] = useState<TransientInfo | null>(null)
  const transientTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const [renderKey, setRenderKey] = useState(0) // bumped each poll cycle to re-render trails

  // Re-render trails on each poll cycle (trails are stored in refs, not state)
  useEffect(() => {
    const timer = setInterval(() => setRenderKey((k) => k + 1), 5000)
    return () => clearInterval(timer)
  }, [])

  // Dismiss transient info after 3 seconds
  const showTransient = useCallback((info: Omit<TransientInfo, 'expiresMs'>) => {
    if (transientTimerRef.current) clearTimeout(transientTimerRef.current)
    setTransient({ ...info, expiresMs: Date.now() + 3000 })
    transientTimerRef.current = setTimeout(() => setTransient(null), 3000)
  }, [])

  // Derived display radius (live during resize, else stored)
  const displayRadius = liveRadius ?? radiusMeters

  // GeoJSON data
  const circleGeoJSON = useMemo(
    () => generateCircleGeoJSON(anchorLon, anchorLat, displayRadius),
    [anchorLon, anchorLat, displayRadius],
  )

  const ghostCircleGeoJSON = useMemo(
    () =>
      ghostAnchor
        ? generateCircleGeoJSON(ghostAnchor.lon, ghostAnchor.lat, displayRadius)
        : null,
    [ghostAnchor, displayRadius],
  )

  // Trail data (re-derived each render triggered by renderKey bump)
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const vesselTrailGeoJSON = useMemo(() => trailToGeoJSON(vesselTrail()), [renderKey, vesselTrail])

  const aisTrailsData = useMemo(() => {
    const trails = aisTrails()
    const features: GeoJSON.Feature<GeoJSON.LineString>[] = []
    for (const [, points] of trails) {
      if (points.length >= 2) features.push(trailToGeoJSON(points))
    }
    return { type: 'FeatureCollection' as const, features }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [renderKey, aisTrails])

  // ── Cursor management ────────────────────────────────────────────────────
  const setCursor = useCallback((cursor: string) => {
    const canvas = mapRef.current?.getCanvas()
    if (canvas) canvas.style.cursor = cursor
  }, [])

  // ── Keyboard handler ─────────────────────────────────────────────────────
  useEffect(() => {
    const handleKey = (e: KeyboardEvent) => {
      if (editMode === 'none') return
      if (e.key === 'Escape') {
        if (editMode === 'radius') {
          setLiveRadius(null)
        }
        setEditMode('none')
        setGhostAnchor(null)
        setCursor('grab')
        return
      }
      if (e.key === 'Enter') {
        if (editMode === 'reposition' && ghostAnchor) {
          onAnchorReposition(ghostAnchor.lat, ghostAnchor.lon)
          setGhostAnchor(null)
          setEditMode('none')
          setCursor('grab')
        } else if (editMode === 'radius' && liveRadius !== null) {
          onRadiusChange(liveRadius)
          setLiveRadius(null)
          setEditMode('none')
          setCursor('grab')
        }
        return
      }
      if (editMode === 'reposition' && ghostAnchor) {
        const pos = ghostAnchor
        const nudgeMap: Record<string, [number, number]> = {
          ArrowUp: [0, 0],
          ArrowDown: [Math.PI, 0],
          ArrowLeft: [(3 * Math.PI) / 2, 0],
          ArrowRight: [Math.PI / 2, 0],
        }
        if (e.key in nudgeMap) {
          e.preventDefault()
          const bearings: Record<string, number> = {
            ArrowUp: 0, ArrowDown: Math.PI, ArrowLeft: (3 * Math.PI) / 2, ArrowRight: Math.PI / 2,
          }
          const [newLat, newLon] = destinationPoint(pos.lat, pos.lon, bearings[e.key], NUDGE_METERS)
          setGhostAnchor({ lat: newLat, lon: newLon })
        }
      }
    }
    window.addEventListener('keydown', handleKey)
    return () => window.removeEventListener('keydown', handleKey)
  }, [editMode, ghostAnchor, liveRadius, onAnchorReposition, onRadiusChange, setCursor])

  // ── Map click handler ────────────────────────────────────────────────────
  const handleMapClick = useCallback(
    (e: maplibregl.MapMouseEvent) => {
      if (editMode !== 'none') return
      const { lat, lng } = e.lngLat
      const dist = Math.round(haversineMeters(vesselLat, vesselLon, lat, lng))
      const bearing = Math.round(bearingDeg(vesselLat, vesselLon, lat, lng))
      showTransient({ lat, lon: lng, distanceM: dist, bearing, label: 'pin' })
    },
    [editMode, vesselLat, vesselLon, showTransient],
  )

  // ── Map mouse move handler (for radius drag) ─────────────────────────────
  const handleMouseMove = useCallback(
    (e: maplibregl.MapMouseEvent) => {
      if (editMode === 'radius') {
        const { lat, lng } = e.lngLat
        const newRadius = haversineMeters(anchorLat, anchorLon, lat, lng)
        setLiveRadius(Math.max(5, newRadius))
      } else if (editMode === 'reposition') {
        const { lat, lng } = e.lngLat
        setGhostAnchor({ lat, lon: lng })
      } else {
        // Detect hover over circle edge — within ±15px projected distance
        const map = mapRef.current
        if (!map) return
        const circleEdgePoint = map.project([anchorLon, anchorLat])
        const cursorPoint = e.point
        const dx = cursorPoint.x - circleEdgePoint.x
        const dy = cursorPoint.y - circleEdgePoint.y
        const pixelDist = Math.sqrt(dx * dx + dy * dy)
        // Project radius to pixels
        const radiusEdge = map.project(
          destinationPoint(anchorLat, anchorLon, 0, displayRadius).reverse() as [number, number],
        )
        const pixelRadius = Math.sqrt(
          (radiusEdge.x - circleEdgePoint.x) ** 2 + (radiusEdge.y - circleEdgePoint.y) ** 2,
        )
        if (Math.abs(pixelDist - pixelRadius) < 15) {
          setCursor('ew-resize')
        } else {
          setCursor('grab')
        }
      }
    },
    [editMode, anchorLat, anchorLon, displayRadius, setCursor],
  )

  // ── Circle edge click detection ──────────────────────────────────────────
  const handleCircleEdgeClick = useCallback(
    (e: maplibregl.MapMouseEvent) => {
      if (editMode !== 'none') return
      const map = mapRef.current
      if (!map) return
      const circleEdgePoint = map.project([anchorLon, anchorLat])
      const cursorPoint = e.point
      const dx = cursorPoint.x - circleEdgePoint.x
      const dy = cursorPoint.y - circleEdgePoint.y
      const pixelDist = Math.sqrt(dx * dx + dy * dy)
      const radiusEdge = map.project(
        destinationPoint(anchorLat, anchorLon, 0, displayRadius).reverse() as [number, number],
      )
      const pixelRadius = Math.sqrt(
        (radiusEdge.x - circleEdgePoint.x) ** 2 + (radiusEdge.y - circleEdgePoint.y) ** 2,
      )
      if (Math.abs(pixelDist - pixelRadius) < 15) {
        e.preventDefault()
        originalRadiusRef.current = radiusMeters
        setLiveRadius(radiusMeters)
        setEditMode('radius')
        setCursor('ew-resize')
      }
    },
    [editMode, anchorLat, anchorLon, displayRadius, radiusMeters, setCursor],
  )

  // ── Double-click on map confirms radius edit ─────────────────────────────
  const handleDblClick = useCallback(
    (e: maplibregl.MapMouseEvent) => {
      if (editMode === 'radius' && liveRadius !== null) {
        e.preventDefault()
        onRadiusChange(liveRadius)
        setLiveRadius(null)
        setEditMode('none')
        setCursor('grab')
      }
    },
    [editMode, liveRadius, onRadiusChange, setCursor],
  )

  // ── AIS vessel click ─────────────────────────────────────────────────────
  const handleAisClick = useCallback(
    (vessel: NearbyVessel) => {
      if (vessel.lat === undefined || vessel.lon === undefined) return
      const dist = Math.round(haversineMeters(vesselLat, vesselLon, vessel.lat, vessel.lon))
      const bearing = Math.round(bearingDeg(vesselLat, vesselLon, vessel.lat, vessel.lon))
      showTransient({ lat: vessel.lat, lon: vessel.lon, distanceM: dist, bearing, label: vessel.name })
    },
    [vesselLat, vesselLon, showTransient],
  )

  // ── Anchor marker click ──────────────────────────────────────────────────
  const handleAnchorMarkerClick = useCallback(
    (e: React.MouseEvent) => {
      e.stopPropagation()
      if (editMode !== 'none') return
      setGhostAnchor({ lat: anchorLat, lon: anchorLon })
      setEditMode('reposition')
      setCursor('grabbing')
    },
    [editMode, anchorLat, anchorLon, setCursor],
  )

  // ── Zoom / Recenter controls ────────────────────────────────────────────
  const handleZoomIn = useCallback(() => {
    mapRef.current?.easeTo({ zoom: (mapRef.current.getZoom() ?? 14) + 1, duration: 250 })
  }, [])

  const handleZoomOut = useCallback(() => {
    mapRef.current?.easeTo({ zoom: (mapRef.current.getZoom() ?? 14) - 1, duration: 250 })
  }, [])

  const handleRecenter = useCallback(() => {
    mapRef.current?.easeTo({ center: [anchorLon, anchorLat], duration: 600 })
  }, [anchorLat, anchorLon])

  // ── Confirm / Cancel buttons ─────────────────────────────────────────────
  const handleConfirmReposition = useCallback(() => {
    if (ghostAnchor) {
      onAnchorReposition(ghostAnchor.lat, ghostAnchor.lon)
      setGhostAnchor(null)
    }
    setEditMode('none')
    setCursor('grab')
  }, [ghostAnchor, onAnchorReposition, setCursor])

  const handleCancelEdit = useCallback(() => {
    if (editMode === 'radius') setLiveRadius(null)
    setGhostAnchor(null)
    setEditMode('none')
    setCursor('grab')
  }, [editMode, setCursor])

  const handleConfirmRadius = useCallback(() => {
    if (liveRadius !== null) {
      onRadiusChange(liveRadius)
      setLiveRadius(null)
    }
    setEditMode('none')
    setCursor('grab')
  }, [liveRadius, onRadiusChange, setCursor])

  // ── Initial map view ─────────────────────────────────────────────────────
  const initialViewState = useMemo(
    () => ({
      longitude: anchorLon,
      latitude: anchorLat,
      zoom: zoomForRadius(radiusMeters),
    }),
    // Only used as initial value — no deps to avoid re-centering on every update
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [],
  )

  const mapStyle = isDarkTheme ? STYLE_DARK : STYLE_LIGHT

  return (
    <div className={cn('relative overflow-hidden rounded-lg', className)}>
      <Map
        ref={mapRef}
        mapLib={maplibregl}
        initialViewState={initialViewState}
        style={{ width: '100%', height: '100%' }}
        mapStyle={mapStyle}
        minZoom={10}
        onClick={handleMapClick}
        onMouseMove={handleMouseMove}
        onDblClick={handleDblClick}
        onMouseDown={handleCircleEdgeClick}
        attributionControl={false}
        dragRotate={false}
        touchPitch={false}
      >
        {/* OpenSeaMap nautical overlay */}
        <Source
          id="openseamap"
          type="raster"
          tiles={[OPENSEAMAP_TILES]}
          tileSize={256}
          attribution="© OpenSeaMap contributors"
        >
          <Layer id="openseamap-layer" type="raster" paint={{ 'raster-opacity': 0.85 }} />
        </Source>

        {/* Alarm circle fill */}
        <Source id="alarm-circle" type="geojson" data={circleGeoJSON}>
          <Layer
            id="alarm-circle-fill"
            type="fill"
            paint={{
              'fill-color': isDarkTheme ? '#38bdf8' : '#0ea5e9',
              'fill-opacity': 0.1,
            }}
          />
          <Layer
            id="alarm-circle-stroke"
            type="line"
            paint={{
              'line-color': isDarkTheme ? '#38bdf8' : '#0284c7',
              'line-width': 2,
              'line-dasharray': [4, 3],
              'line-opacity': 0.8,
            }}
          />
        </Source>

        {/* Ghost circle during reposition mode */}
        {ghostCircleGeoJSON && (
          <Source id="ghost-circle" type="geojson" data={ghostCircleGeoJSON}>
            <Layer
              id="ghost-circle-fill"
              type="fill"
              paint={{ 'fill-color': '#a3a3a3', 'fill-opacity': 0.08 }}
            />
            <Layer
              id="ghost-circle-stroke"
              type="line"
              paint={{
                'line-color': '#a3a3a3',
                'line-width': 1.5,
                'line-dasharray': [3, 3],
                'line-opacity': 0.6,
              }}
            />
          </Source>
        )}

        {/* AIS vessel trails */}
        <Source id="ais-trails" type="geojson" data={aisTrailsData}>
          <Layer
            id="ais-trails-layer"
            type="line"
            paint={{
              'line-color': '#f59e0b',
              'line-width': 1.5,
              'line-opacity': 0.5,
            }}
            layout={{ 'line-join': 'round', 'line-cap': 'round' }}
          />
        </Source>

        {/* Vessel swing trail */}
        {vesselTrailGeoJSON.geometry.coordinates.length >= 2 && (
          <Source id="vessel-trail" type="geojson" lineMetrics data={vesselTrailGeoJSON}>
            <Layer
              id="vessel-trail-layer"
              type="line"
              paint={{
                'line-color': '#38bdf8',
                'line-width': 2,
                'line-gradient': [
                  'interpolate',
                  ['linear'],
                  ['line-progress'],
                  0, 'rgba(56,189,248,0)',
                  0.6, 'rgba(56,189,248,0.4)',
                  1, 'rgba(56,189,248,0.9)',
                ],
              }}
              layout={{ 'line-join': 'round', 'line-cap': 'round' }}
            />
          </Source>
        )}

        {/* AIS vessel markers */}
        {aisVessels.map((vessel) => {
          if (vessel.lat === undefined || vessel.lon === undefined) return null
          return (
            <Marker
              key={vessel.name}
              latitude={vessel.lat}
              longitude={vessel.lon}
              onClick={() => handleAisClick(vessel)}
            >
              <button
                className="group flex flex-col items-center"
                style={{ minWidth: 40, minHeight: 40 }}
                aria-label={`AIS vessel: ${vessel.name}`}
              >
                <Ship className="h-5 w-5 text-amber-400 drop-shadow" />
                <span className="mt-0.5 font-mono text-[9px] font-semibold uppercase tracking-wider text-amber-300 drop-shadow">
                  {vessel.name.length > 8 ? vessel.name.slice(0, 8) + '…' : vessel.name}
                </span>
              </button>
            </Marker>
          )
        })}

        {/* Vessel marker */}
        <Marker latitude={vesselLat} longitude={vesselLon}>
          <div
            className="flex items-center justify-center"
            style={{
              width: 40,
              height: 40,
              transform: vesselHeadingDeg !== null ? `rotate(${vesselHeadingDeg}deg)` : undefined,
              filter: 'drop-shadow(0 1px 3px rgba(0,0,0,0.6))',
            }}
          >
            {/* Boat shape SVG */}
            <svg width="22" height="32" viewBox="0 0 22 32" fill="none">
              <path
                d="M11 2 L20 26 L11 22 L2 26 Z"
                fill="white"
                stroke="#0ea5e9"
                strokeWidth="1.5"
              />
            </svg>
          </div>
        </Marker>

        {/* Ghost anchor (during reposition) */}
        {ghostAnchor && (
          <Marker latitude={ghostAnchor.lat} longitude={ghostAnchor.lon}>
            <div className="flex items-center justify-center" style={{ width: 40, height: 40, cursor: 'grabbing' }}>
              <div className="flex h-8 w-8 items-center justify-center rounded-full bg-neutral-500/80">
                <Anchor className="h-4 w-4 text-white opacity-60" />
              </div>
            </div>
          </Marker>
        )}

        {/* Anchor marker */}
        <Marker latitude={anchorLat} longitude={anchorLon}>
          <button
            onClick={handleAnchorMarkerClick}
            className="flex items-center justify-center"
            style={{ width: 40, height: 40, cursor: editMode === 'none' ? 'grab' : 'default' }}
            aria-label="Anchor position — click to reposition"
          >
            <div className="flex h-9 w-9 items-center justify-center rounded-full bg-sky-600/90 shadow-lg">
              <Anchor className="h-5 w-5 text-white" />
            </div>
          </button>
        </Marker>

        {/* Transient pin / AIS info */}
        {transient && (
          <Marker latitude={transient.lat} longitude={transient.lon}>
            <div
              className="pointer-events-none flex flex-col items-center"
              style={{
                opacity: 1,
                transition: 'opacity 300ms ease-out',
              }}
            >
              {transient.label === 'pin' ? (
                <MapPin className="h-6 w-6 text-white drop-shadow-lg" />
              ) : (
                <Ship className="h-5 w-5 text-amber-300 drop-shadow" />
              )}
              <div className="mt-1 rounded bg-black/70 px-2 py-0.5 text-center">
                <p className="font-mono text-[11px] text-white">
                  {transient.distanceM < 1000
                    ? `${transient.distanceM}m`
                    : `${(transient.distanceM / 1000).toFixed(1)}km`}{' '}
                  {Math.round(transient.bearing)}°
                </p>
                {transient.label !== 'pin' && (
                  <p className="font-mono text-[9px] uppercase text-amber-300">{transient.label}</p>
                )}
              </div>
            </div>
          </Marker>
        )}
      </Map>

      {/* Edit mode overlay controls */}
      {editMode !== 'none' && (
        <div className="pointer-events-none absolute inset-x-0 bottom-4 flex justify-center">
          <div
            className="pointer-events-auto flex gap-2 rounded-xl bg-black/75 px-4 py-2 backdrop-blur"
            style={{ transition: 'opacity 200ms ease-out, transform 200ms ease-out' }}
          >
            {editMode === 'reposition' && (
              <span className="self-center text-xs text-neutral-300">
                Drag anchor to new position
              </span>
            )}
            {editMode === 'radius' && (
              <span className="self-center text-xs text-neutral-300">
                Move cursor to resize alarm circle
              </span>
            )}
            <button
              className="rounded-lg bg-sky-600 px-3 py-1.5 text-sm font-semibold text-white hover:bg-sky-500 active:bg-sky-700"
              onClick={editMode === 'reposition' ? handleConfirmReposition : handleConfirmRadius}
            >
              Confirm
            </button>
            <button
              className="rounded-lg bg-neutral-700 px-3 py-1.5 text-sm font-semibold text-neutral-100 hover:bg-neutral-600"
              onClick={handleCancelEdit}
            >
              Cancel
            </button>
          </div>
        </div>
      )}

      {/* Metric overlay — top of map */}
      <div className="pointer-events-none absolute inset-x-0 top-0 flex justify-center gap-2 p-2">
        {[
          {
            label: 'Distance',
            value: distanceMeters !== null
              ? isImperial
                ? `${Math.round(distanceMeters * 3.28084)}`
                : `${Math.round(distanceMeters)}`
              : '—',
            unit: isImperial ? 'ft' : 'm',
            alert: distanceMeters !== null && distanceMeters > radiusMeters + 4.572,
          },
          {
            label: 'Bearing',
            value: bearingDegProp !== null ? `${bearingDegProp}` : '—',
            unit: '°',
            alert: false,
          },
          {
            label: 'Radius',
            value: isImperial
              ? `${Math.round(radiusMeters * 3.28084)}`
              : `${Math.round(radiusMeters)}`,
            unit: isImperial ? 'ft' : 'm',
            alert: false,
          },
        ].map(({ label, value, unit, alert }) => (
          <div
            key={label}
            className="rounded-lg bg-black/50 px-3 py-1.5 text-center backdrop-blur"
          >
            <p className="text-[9px] uppercase tracking-[0.16em] text-white/60">{label}</p>
            <p className={`font-display tabular-nums leading-tight ${
              alert ? 'text-red-400' : 'text-white'
            }`} style={{ fontSize: '1.1rem' }}>
              {value}
              <span className="ml-0.5 text-[11px] text-white/50">{unit}</span>
            </p>
          </div>
        ))}
      </div>

      {/* Clear anchor button — bottom-left, hidden during edit modes */}
      {editMode === 'none' && (
        <div className="pointer-events-auto absolute bottom-4 left-3">
          <button
            onClick={onClearAnchor}
            className="rounded-lg bg-black/50 px-3 py-2 text-sm font-semibold text-white/80 shadow backdrop-blur hover:bg-red-600/80 hover:text-white active:scale-95"
            style={{ transition: 'background-color 150ms ease-out, color 150ms ease-out' }}
            aria-label="Clear anchor watch"
          >
            Clear Anchor Watch
          </button>
        </div>
      )}

      {/* Zoom + Recenter controls */}
      <div className="pointer-events-auto absolute right-3 top-3 flex flex-col gap-1">
        <button
          onClick={handleZoomIn}
          aria-label="Zoom in"
          className="flex h-9 w-9 items-center justify-center rounded-lg bg-black/65 text-white shadow backdrop-blur hover:bg-black/80 active:scale-95"
          style={{ transition: 'background-color 150ms ease-out' }}
        >
          <Plus className="h-4 w-4" />
        </button>
        <button
          onClick={handleZoomOut}
          aria-label="Zoom out"
          className="flex h-9 w-9 items-center justify-center rounded-lg bg-black/65 text-white shadow backdrop-blur hover:bg-black/80 active:scale-95"
          style={{ transition: 'background-color 150ms ease-out' }}
        >
          <Minus className="h-4 w-4" />
        </button>
        <div className="my-0.5 h-px bg-white/20" />
        <button
          onClick={handleRecenter}
          aria-label="Re-centre on anchor"
          className="flex h-9 w-9 items-center justify-center rounded-lg bg-black/65 text-white shadow backdrop-blur hover:bg-black/80 active:scale-95"
          style={{ transition: 'background-color 150ms ease-out' }}
        >
          <Crosshair className="h-4 w-4" />
        </button>
      </div>

      {/* Radius readout during resize */}
      {editMode === 'radius' && liveRadius !== null && (
        <div className="pointer-events-none absolute left-3 top-3 rounded-lg bg-black/70 px-3 py-1.5 text-sm font-mono text-white backdrop-blur">
          Radius: {Math.round(liveRadius)}m
        </div>
      )}
    </div>
  )
}
