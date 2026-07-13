import 'maplibre-gl/dist/maplibre-gl.css'
import maplibregl from 'maplibre-gl'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { MapRef } from 'react-map-gl/maplibre'
import { Map, Marker, Source, Layer } from 'react-map-gl/maplibre'
import { Anchor, ArrowUp, CircleStop, Crosshair, Expand, MapPin, Minus, Plus, Satellite, Ship } from 'lucide-react'
import { cn } from '@/lib/utils'
import { haversineMeters, bearingDeg, destinationPoint } from '@/lib/geo'
import type { NearbyVessel } from '@/hooks/use-nearby-vessels'
import type { TrailPoint } from '@/hooks/use-server-trails'

// ── Map style URLs (Carto, no API key required) ─────────────────────────────
const STYLE_LIGHT = 'https://basemaps.cartocdn.com/gl/positron-gl-style/style.json'
const STYLE_DARK = 'https://basemaps.cartocdn.com/gl/dark-matter-gl-style/style.json'
const OPENSEAMAP_TILES = 'https://tiles.openseamap.org/seamark/{z}/{x}/{y}.png'
const WORLD_IMAGERY_TILES = '/api/world-imagery/{z}/{x}/{y}'
const SAT_HANDOFF_START_ZOOM = 9
const SAT_HANDOFF_END_ZOOM = 10
const WORLD_IMAGERY_MAX_ZOOM = 18
const ANCHOR_WATCH_ZOOM_STORAGE_KEY = 'anchor-watch-map-zoom'
const AIS_TRAIL_MAX_AGE_MS = 24 * 60 * 60 * 1000
// Threshold above which the current is judged strong enough to visually call out.
const HIGH_DRIFT_IMPACT_KTS = 1.5

// World Imagery (Esri/Maxar aerial photography) fades in between zoom 9-10
// rather than appearing abruptly, since its low-zoom tiles are lower quality.
export function computeWorldImageryOpacity(currentZoom: number, enabled: boolean): number {
  if (!enabled) return 0
  return Math.max(
    0,
    Math.min(1, (currentZoom - SAT_HANDOFF_START_ZOOM) / (SAT_HANDOFF_END_ZOOM - SAT_HANDOFF_START_ZOOM)),
  )
}

function readStoredZoom(): number | null {
  if (typeof window === 'undefined') return null
  const raw = window.localStorage.getItem(ANCHOR_WATCH_ZOOM_STORAGE_KEY)
  if (!raw) return null
  const value = Number(raw)
  if (!Number.isFinite(value)) return null
  // Keep persisted values within map bounds.
  return Math.max(10, Math.min(22, value))
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
  // Approximate: zoom 14 ≈ 300m radius nicely visible.
  // Cap at 14 so nearby AIS vessels (~1-2km) remain visible even with small alarm radii.
  return Math.max(10, Math.min(14, 14 - Math.log2(radiusM / 300)))
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
  depthMeters: number | null
  currentDriftKts: number | null
  currentSetDeg: number | null
  currentDriftImpactKts?: number | null
  distanceMeters: number | null
  bearingDeg: number | null
  isImperial: boolean
  vesselTrail: () => TrailPoint[]
  aisVessels: NearbyVessel[]
  aisTrails: () => Map<string, TrailPoint[]>
  isDarkTheme: boolean
  showImageryLayer?: boolean
  onImageryToggle?: (enabled: boolean) => void
  onAnchorReposition: (lat: number, lon: number) => void
  onRadiusChange: (radiusMeters: number) => void
  onClearAnchor: () => void
  onFullscreen?: () => void
  className?: string
}

export function AnchorWatchMap({
  vesselLat,
  vesselLon,
  vesselHeadingDeg,
  anchorLat,
  anchorLon,
  radiusMeters,
  depthMeters,
  currentDriftKts,
  currentSetDeg,
  currentDriftImpactKts = null,
  distanceMeters,
  bearingDeg: bearingDegProp,
  isImperial,
  vesselTrail,
  aisVessels,
  aisTrails,
  isDarkTheme,
  showImageryLayer = false,
  onImageryToggle,
  onAnchorReposition,
  onRadiusChange,
  onClearAnchor,
  onFullscreen,
  className,
}: AnchorWatchMapProps) {
  const mapRef = useRef<MapRef | null>(null)
  const [editMode, setEditMode] = useState<EditMode>('none')
  const [ghostAnchor, setGhostAnchor] = useState<{ lat: number; lon: number } | null>(null)
  const [liveRadius, setLiveRadius] = useState<number | null>(null)
  const originalRadiusRef = useRef<number>(radiusMeters)
  const [transient, setTransient] = useState<TransientInfo | null>(null)
  const transientTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const suppressNextMapClickRef = useRef(false)
  const [renderKey, setRenderKey] = useState(0) // bumped each poll cycle to re-render trails
  const [motoringPoints, setMotoringPoints] = useState<TrailPoint[]>([])
  // Track zoom for marker scaling
  const [currentZoom, setCurrentZoom] = useState(() => readStoredZoom() ?? zoomForRadius(radiusMeters))
  const handleZoomChange = useCallback(() => {
    const z = mapRef.current?.getZoom()
    if (z !== undefined) setCurrentZoom(z)
  }, [])
  // Scale markers: full size at zoom 14+, shrink linearly down to 0.45× at zoom 10
  const markerScale = Math.max(0.45, Math.min(1, (currentZoom - 10) / (14 - 10)))
  const worldImageryOpacity = computeWorldImageryOpacity(currentZoom, showImageryLayer)

  useEffect(() => {
    if (typeof window === 'undefined') return
    window.localStorage.setItem(ANCHOR_WATCH_ZOOM_STORAGE_KEY, String(currentZoom))
  }, [currentZoom])

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
  const displayDistanceMeters = editMode === 'reposition' && ghostAnchor
    ? haversineMeters(vesselLat, vesselLon, ghostAnchor.lat, ghostAnchor.lon)
    : distanceMeters
  const displayBearingDeg = editMode === 'reposition' && ghostAnchor
    ? Math.round(bearingDeg(vesselLat, vesselLon, ghostAnchor.lat, ghostAnchor.lon))
    : bearingDegProp
  const showRepositionBreadcrumbs = editMode === 'reposition'
  const currentFavorable = currentDriftImpactKts !== null ? currentDriftImpactKts >= 0 : null
  const currentColorClass = currentFavorable === null ? 'text-white' : currentFavorable ? 'text-teal-400' : 'text-amber-500'
  const highDriftImpact = currentDriftImpactKts !== null && Math.abs(currentDriftImpactKts) >= HIGH_DRIFT_IMPACT_KTS
  const formatTransientDistance = useCallback(
    (distanceM: number) => {
      if (isImperial) {
        return `${Math.round(distanceM * 3.28084)} ft`
      }
      return distanceM < 1000 ? `${distanceM} m` : `${(distanceM / 1000).toFixed(1)} km`
    },
    [isImperial],
  )

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

  // Fetch motoring track on-demand from /api/tracks/motoring when entering reposition mode
  const fetchMotoringTrail = useCallback(async () => {
    try {
      const res = await fetch('/api/tracks/motoring')
      if (!res.ok) return
      const data = (await res.json()) as { points?: Array<{ lat: number; lon: number; timestamp: string }> }
      if (!data.points) return
      const pts: TrailPoint[] = data.points
        .map(p => ({ lat: p.lat, lon: p.lon, timestampMs: Date.parse(p.timestamp) }))
        .sort((a, b) => (a.timestampMs ?? 0) - (b.timestampMs ?? 0))
      setMotoringPoints(pts)
    } catch {
      // silently ignore — trail simply won't render
    }
  }, [])

  // Trail data (re-derived each render triggered by renderKey bump)
  const motoringTrailGeoJSON = useMemo(
    () => trailToGeoJSON(motoringPoints),
    [motoringPoints],
  )

  const postAnchorTrailGeoJSON = useMemo(
    () => trailToGeoJSON(vesselTrail()),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [renderKey, vesselTrail],
  )

  const aisTrailsData = useMemo(() => {
    const trails = aisTrails()
    const activeNames = new Set(aisVessels.map(v => v.name))
    const cutoffMs = Date.now() - AIS_TRAIL_MAX_AGE_MS
    const features: GeoJSON.Feature<GeoJSON.LineString>[] = []
    for (const [name, points] of trails) {
      if (!activeNames.has(name)) continue
      const recentPoints = points.filter((p) => p.timestampMs >= cutoffMs)
      if (recentPoints.length >= 2) features.push(trailToGeoJSON(recentPoints))
    }
    return { type: 'FeatureCollection' as const, features }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [renderKey, aisTrails, aisVessels])

  // ── Cursor management ────────────────────────────────────────────────────
  const setCursor = useCallback((cursor: string) => {
    const canvas = mapRef.current?.getCanvas()
    if (canvas) canvas.style.cursor = cursor
  }, [])

  const handleCancel = useCallback(() => {
    if (editMode === 'radius') setLiveRadius(null)
    setGhostAnchor(null)
    setEditMode('none')
    setCursor('grab')
  }, [editMode, setCursor])

  // ── Keyboard handler ─────────────────────────────────────────────────────
  useEffect(() => {
    const handleKey = (e: KeyboardEvent) => {
      if (editMode === 'none') return
      if (e.key === 'Escape') {
        handleCancel()
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
  }, [editMode, ghostAnchor, liveRadius, onAnchorReposition, onRadiusChange, setCursor, handleCancel])

  // ── Map click handler ────────────────────────────────────────────────────
  const handleMapClick = useCallback(
    (e: maplibregl.MapMouseEvent) => {
      if (suppressNextMapClickRef.current) {
        suppressNextMapClickRef.current = false
        return
      }
      if (editMode === 'reposition' && ghostAnchor) {
        onAnchorReposition(ghostAnchor.lat, ghostAnchor.lon)
        setGhostAnchor(null)
        setEditMode('none')
        setCursor('grab')
        return
      }
      if (editMode === 'radius' && liveRadius !== null) {
        onRadiusChange(liveRadius)
        setLiveRadius(null)
        setEditMode('none')
        setCursor('grab')
        return
      }
      if (editMode !== 'none') return
      const { lat, lng } = e.lngLat
      const dist = Math.round(haversineMeters(vesselLat, vesselLon, lat, lng))
      const bearing = Math.round(bearingDeg(vesselLat, vesselLon, lat, lng))
      showTransient({ lat, lon: lng, distanceM: dist, bearing, label: 'pin' })
    },
    [editMode, ghostAnchor, liveRadius, onAnchorReposition, onRadiusChange, setCursor, vesselLat, vesselLon, showTransient],
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
      if (editMode === 'radius' && liveRadius !== null) {
        e.preventDefault()
        suppressNextMapClickRef.current = true
        onRadiusChange(liveRadius)
        setLiveRadius(null)
        setEditMode('none')
        setCursor('grab')
        return
      }
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
        suppressNextMapClickRef.current = true
        originalRadiusRef.current = radiusMeters
        setLiveRadius(radiusMeters)
        setEditMode('radius')
        setCursor('ew-resize')
        // Imperatively disable drag pan so the ongoing touch/drag doesn't also pan the map
        mapRef.current?.dragPan.disable()
      }
    },
    [editMode, anchorLat, anchorLon, displayRadius, liveRadius, onRadiusChange, radiusMeters, setCursor],
  )

  // ── Touch start: circle-edge detection for tablet radius adjustment ──────
  const handleTouchStartEdge = useCallback(
    (e: maplibregl.MapTouchEvent) => {
      if (editMode === 'radius' && liveRadius !== null) {
        suppressNextMapClickRef.current = true
        onRadiusChange(liveRadius)
        setLiveRadius(null)
        setEditMode('none')
        setCursor('grab')
        return
      }
      if (editMode !== 'none') return
      const map = mapRef.current
      if (!map) return
      const circleEdgePoint = map.project([anchorLon, anchorLat])
      const touchPoint = e.point
      const dx = touchPoint.x - circleEdgePoint.x
      const dy = touchPoint.y - circleEdgePoint.y
      const pixelDist = Math.sqrt(dx * dx + dy * dy)
      const radiusEdge = map.project(
        destinationPoint(anchorLat, anchorLon, 0, displayRadius).reverse() as [number, number],
      )
      const pixelRadius = Math.sqrt(
        (radiusEdge.x - circleEdgePoint.x) ** 2 + (radiusEdge.y - circleEdgePoint.y) ** 2,
      )
      // Larger hit target for touch (25px vs 15px for mouse)
      if (Math.abs(pixelDist - pixelRadius) < 25) {
        suppressNextMapClickRef.current = true
        originalRadiusRef.current = radiusMeters
        setLiveRadius(radiusMeters)
        setEditMode('radius')
        setCursor('ew-resize')
        mapRef.current?.dragPan.disable()
      }
    },
    [editMode, liveRadius, anchorLat, anchorLon, displayRadius, radiusMeters, onRadiusChange, setCursor],
  )

  // ── Touch move: update ghost anchor or live radius on tablet ─────────────
  const handleTouchMove = useCallback(
    (e: maplibregl.MapTouchEvent) => {
      if (editMode === 'radius') {
        const { lat, lng } = e.lngLat
        const newRadius = haversineMeters(anchorLat, anchorLon, lat, lng)
        setLiveRadius(Math.max(5, newRadius))
      } else if (editMode === 'reposition') {
        const { lat, lng } = e.lngLat
        setGhostAnchor({ lat, lon: lng })
      }
    },
    [editMode, anchorLat, anchorLon],
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
    (e: React.MouseEvent, vessel: NearbyVessel) => {
      e.stopPropagation()
      suppressNextMapClickRef.current = true
      if (vessel.lat === undefined || vessel.lon === undefined) return
      const dist = Math.round(haversineMeters(vesselLat, vesselLon, vessel.lat, vessel.lon))
      const bearing = Math.round(bearingDeg(vesselLat, vesselLon, vessel.lat, vessel.lon))
      showTransient({ lat: vessel.lat, lon: vessel.lon, distanceM: dist, bearing, label: vessel.name })
    },
    [vesselLat, vesselLon, showTransient],
  )

  const confirmAnchorReposition = useCallback(() => {
    if (!ghostAnchor) return
    suppressNextMapClickRef.current = true
    onAnchorReposition(ghostAnchor.lat, ghostAnchor.lon)
    setGhostAnchor(null)
    setEditMode('none')
    setCursor('grab')
  }, [ghostAnchor, onAnchorReposition, setCursor])

  // ── Anchor marker click ──────────────────────────────────────────────────
  const handleAnchorMarkerClick = useCallback(
    (e: React.MouseEvent) => {
      e.stopPropagation()
      if (editMode === 'reposition' && ghostAnchor) {
        confirmAnchorReposition()
        return
      }
      if (editMode !== 'none') return
      setGhostAnchor({ lat: anchorLat, lon: anchorLon })
      setEditMode('reposition')
      setCursor('grabbing')
      void fetchMotoringTrail()
    },
    [confirmAnchorReposition, editMode, anchorLat, anchorLon, ghostAnchor, setCursor, fetchMotoringTrail],
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

  // ── Initial map view ─────────────────────────────────────────────────────
  const initialViewState = useMemo(
    () => ({
      longitude: anchorLon,
      latitude: anchorLat,
      zoom: currentZoom,
    }),
    // Only used as initial value — no deps to avoid re-centering on every update
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [],
  )

  const mapStyle = isDarkTheme ? STYLE_DARK : STYLE_LIGHT

  const handleImageryToggle = useCallback(() => {
    onImageryToggle?.(!showImageryLayer)
  }, [showImageryLayer, onImageryToggle])

  return (
    <div className={cn('relative overflow-hidden rounded-lg', className)}>
      <Map
        ref={mapRef}
        mapLib={maplibregl}
        initialViewState={initialViewState}
        style={{ width: '100%', height: '100%' }}
        mapStyle={mapStyle}
        minZoom={10}
        onZoom={handleZoomChange}
        onClick={handleMapClick}
        onMouseMove={handleMouseMove}
        onDblClick={handleDblClick}
        onMouseDown={handleCircleEdgeClick}
        onTouchStart={handleTouchStartEdge}
        onTouchMove={handleTouchMove}
        attributionControl={false}
        dragRotate={false}
        touchPitch={false}
        dragPan={editMode === 'none'}
      >
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
      {aisTrailsData.features.length > 0 && (
        <Source id="ais-trails" type="geojson" lineMetrics data={aisTrailsData}>
          <Layer
            id="ais-trails-layer"
            type="line"
            paint={{
              'line-width': 2.5,
              'line-gradient': [
                'interpolate',
                ['linear'],
                ['line-progress'],
                0, 'rgba(245, 158, 11, 0)',
                0.5, 'rgba(245, 158, 11, 0.4)',
                1, 'rgba(245, 158, 11, 1)',
              ],
              'line-opacity': 0.9,
            }}
            layout={{ 'line-join': 'round', 'line-cap': 'round' }}
          />
        </Source>
      )}

        {/* Motoring breadcrumb trail (pre-anchor) */}
        {showRepositionBreadcrumbs && motoringTrailGeoJSON.geometry.coordinates.length >= 2 && (
          <Source id="vessel-trail-motoring" type="geojson" data={motoringTrailGeoJSON}>
            <Layer
              id="vessel-trail-motoring-layer"
              type="line"
              paint={{
                'line-color': '#ef4444',
                'line-width': 4,
                'line-opacity': 0.95,
                'line-blur': 0.2,
              }}
              layout={{ 'line-join': 'round', 'line-cap': 'round' }}
            />
          </Source>
        )}

        {/* Post-anchor tracking trail (ring buffer) */}
        {postAnchorTrailGeoJSON.geometry.coordinates.length >= 2 && (
          <Source id="vessel-trail-anchor" type="geojson" lineMetrics data={postAnchorTrailGeoJSON}>
            <Layer
              id="vessel-trail-anchor-layer"
              type="line"
              paint={{
                'line-width': 3,
                'line-gradient': [
                  'interpolate',
                  ['linear'],
                  ['line-progress'],
                  0, '#fde047',
                  0.7, '#facc15',
                  1, '#f59e0b',
                ],
                'line-opacity': 0.9,
              }}
              layout={{ 'line-join': 'round', 'line-cap': 'round' }}
            />
          </Source>
        )}

        {/*
          Raster layers (imagery, seamarks) are declared last and explicitly
          anchored with beforeId="alarm-circle-fill" - react-map-gl's addLayer
          call has no awareness of JSX sibling order, so a layer that mounts
          later (e.g. world-imagery toggled on mid-session, well after the
          map's initial load) is otherwise appended on top of the whole
          stack, covering the alarm circle and trails outright. Anchoring to
          alarm-circle-fill, which is unconditional and always mounts first,
          keeps these rasters below the vector layers regardless of when
          they're toggled on.
        */}
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
              beforeId="alarm-circle-fill"
              paint={{
                'raster-opacity': worldImageryOpacity,
                'raster-fade-duration': 250,
              }}
            />
          </Source>
        )}

        <Source
          id="openseamap"
          type="raster"
          tiles={[OPENSEAMAP_TILES]}
          tileSize={256}
          attribution="© OpenSeaMap contributors"
        >
          <Layer id="openseamap-layer" type="raster" beforeId="alarm-circle-fill" paint={{ 'raster-opacity': 0.85 }} />
        </Source>

        {/* AIS vessel markers */}
        {aisVessels.map((vessel) => {
          if (vessel.lat === undefined || vessel.lon === undefined) return null
          const isSelected = transient?.label === vessel.name
          return (
            <Marker
              key={vessel.name}
              latitude={vessel.lat}
              longitude={vessel.lon}
              style={{ zIndex: isSelected ? 30 : 10 }}
            >
              <button
                className="flex flex-col items-center"
                style={{ minWidth: 40, minHeight: 40 }}
                aria-label={`AIS vessel: ${vessel.name}`}
                onClick={(e) => handleAisClick(e, vessel)}
              >
                <div
                  style={{
                    transform: `scale(${markerScale * (isSelected ? 1.35 : 1)})`,
                    transformOrigin: 'center',
                    transition: 'transform 150ms ease-out',
                  }}
                >
                  <div
                    className={cn(
                      'flex items-center justify-center rounded-full bg-amber-500/90 shadow-lg',
                      isSelected ? 'h-11 w-11 ring-2 ring-white/70' : 'h-9 w-9',
                    )}
                  >
                    <Ship className={cn('text-white', isSelected ? 'h-6 w-6' : 'h-5 w-5')} />
                  </div>
                  <div className="mt-0.5 max-w-28 text-center font-mono text-[9px] font-semibold uppercase tracking-wider text-white drop-shadow-[0_1px_2px_rgba(0,0,0,0.8)]">
                    <div className="truncate">{vessel.name}</div>
                    {isSelected && transient && (
                      <div className="whitespace-nowrap text-[9px] font-medium tracking-normal">
                        {formatTransientDistance(transient.distanceM)} · {Math.round(transient.bearing)}°
                      </div>
                    )}
                  </div>
                </div>
              </button>
            </Marker>
          )
        })}

        {/* Vessel marker */}
        <Marker latitude={vesselLat} longitude={vesselLon}>
          <div
            className="flex items-center justify-center"
            style={{ width: 40, height: 40 }}
          >
            <div style={{
              transform: `${vesselHeadingDeg !== null ? `rotate(${vesselHeadingDeg}deg) ` : ''}scale(${markerScale})`,
              transformOrigin: 'center',
              transition: 'transform 150ms ease-out',
              filter: 'drop-shadow(0 1px 3px rgba(0,0,0,0.6))',
            }}>
              <svg width="22" height="32" viewBox="0 0 22 32" fill="none">
                <path d="M11 2 L20 26 L11 22 L2 26 Z" fill="white" stroke="#0ea5e9" strokeWidth="1.5" />
              </svg>
            </div>
          </div>
        </Marker>

        {/* Ghost anchor (during reposition) */}
        {ghostAnchor && (
          <Marker latitude={ghostAnchor.lat} longitude={ghostAnchor.lon}>
            <button
              type="button"
              onClick={confirmAnchorReposition}
              className="flex items-center justify-center"
              style={{ width: 40, height: 40, cursor: 'grab' }}
              aria-label="Confirm anchor reposition"
            >
              <div
                className="flex h-8 w-8 items-center justify-center rounded-full bg-neutral-500/80"
                style={{ transform: `scale(${markerScale})`, transformOrigin: 'center', transition: 'transform 150ms ease-out' }}
              >
                <Anchor className="h-4 w-4 text-white opacity-60" />
              </div>
            </button>
          </Marker>
        )}

        {/* Anchor marker */}
        <Marker latitude={anchorLat} longitude={anchorLon} style={{ zIndex: 1000 }}>
          <button
            onClick={handleAnchorMarkerClick}
            className="flex items-center justify-center"
            style={{ width: 40, height: 40, cursor: editMode === 'none' ? 'grab' : 'default' }}
            aria-label="Anchor position — click to reposition"
          >
            <div
              className="flex h-8 w-8 items-center justify-center rounded-full bg-sky-600/90 shadow-lg"
              style={{ transform: `scale(${markerScale})`, transformOrigin: 'center', transition: 'transform 150ms ease-out' }}
            >
              <Anchor className="h-4 w-4 text-white" />
            </div>
          </button>
        </Marker>

        {/* Transient pin */}
        {transient?.label === 'pin' && (
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
                  {formatTransientDistance(transient.distanceM)}{' '}
                  {Math.round(transient.bearing)}°
                </p>
                <p className="font-mono text-[9px] uppercase text-amber-300">{transient.label}</p>
              </div>
            </div>
          </Marker>
        )}
      </Map>

      {editMode !== 'none' && (
        <div className="pointer-events-auto absolute inset-x-0 bottom-4 flex justify-center">
          <div className="flex items-center gap-2 rounded-full bg-black/65 py-2 pl-4 pr-2 backdrop-blur">
            <span className="text-xs text-white/70">
              {editMode === 'reposition' ? 'Tap map to place anchor' : 'Tap map to set radius'}
            </span>
            <button
              onClick={handleCancel}
              className="rounded-full bg-white/15 px-3 py-1 text-xs font-medium text-white hover:bg-white/25 active:scale-95"
              style={{ transition: 'background-color 150ms ease-out' }}
            >
              Cancel
            </button>
          </div>
        </div>
      )}

      {/* Metric overlay — top of map */}
      <div
        className={cn(
          'pointer-events-none absolute left-3 top-3 overflow-hidden rounded-lg backdrop-blur',
          editMode === 'none' ? 'bg-black/50' : 'bg-black/35',
        )}
        style={{ zIndex: 2000 }}
        data-testid="anchor-watch-metrics"
      >
        {[
          {
            label: 'Distance',
            value: displayDistanceMeters !== null
              ? isImperial
                ? `${Math.round(displayDistanceMeters * 3.28084)}`
                : `${Math.round(displayDistanceMeters)}`
              : '—',
            unit: isImperial ? 'ft' : 'm',
            alert: displayDistanceMeters !== null && displayDistanceMeters > radiusMeters + 4.572,
          },
          {
            label: 'Bearing',
            value: displayBearingDeg !== null ? `${displayBearingDeg}` : '—',
            unit: '°',
            alert: false,
          },
          {
            label: 'Radius',
            value: displayRadius !== null
              ? isImperial
                ? `${Math.round(displayRadius * 3.28084)}`
                : `${Math.round(displayRadius)}`
              : '—',
            unit: isImperial ? 'ft' : 'm',
            live: editMode === 'radius',
            alert: false,
          },
          {
            label: 'Depth',
            value: depthMeters !== null
              ? isImperial
                ? `${(depthMeters * 3.28084).toFixed(1)}`
                : `${depthMeters.toFixed(1)}`
              : '—',
            unit: isImperial ? 'ft' : 'm',
            alert: false,
          },
          {
            label: 'Current',
            value: currentDriftKts !== null ? currentDriftKts.toFixed(1) : '—',
            unit: 'kts',
            alert: false,
            setDeg: currentSetDeg,
          },
        ].map(({ label, value, unit, alert, setDeg, live }, index) => (
          <div
            key={label}
            className={cn(
              'flex items-center justify-between gap-4 px-3 py-1.5',
              index > 0 && 'border-t border-white/10',
              live && 'bg-sky-500/10',
            )}
          >
            <p className={cn('text-[9px] uppercase tracking-[0.16em] text-white/80', live && 'text-sky-200')}>
              {label}
            </p>
            {label === 'Current' ? (
              <div className="flex items-center justify-end gap-2">
                {value !== '0.0' && value !== '—' && (
                  <ArrowUp
                    className={cn('shrink-0', currentColorClass, highDriftImpact ? 'h-5 w-5' : 'h-4 w-4')}
                    strokeWidth={highDriftImpact ? 2.75 : 2}
                    style={{ transform: `rotate(${setDeg ?? 0}deg)` }}
                    aria-hidden="true"
                  />
                )}
                <p className={cn('font-display tabular-nums leading-tight', currentColorClass)} style={{ fontSize: '1.1rem' }}>
                  {value}
                  <span className="ml-0.5 text-[11px] text-white/80">{unit}</span>
                </p>
              </div>
            ) : (
              <p
                className={`font-display tabular-nums leading-tight ${
                  alert ? 'text-red-400' : 'text-white'
                }`}
                style={{ fontSize: '1.1rem' }}
              >
                {value}
                <span className="ml-0.5 text-[11px] text-white/80">{unit}</span>
              </p>
            )}
          </div>
        ))}
      </div>

      {/* Zoom + Recenter controls */}
      <div
        className="pointer-events-auto absolute right-3 top-3 flex flex-col gap-1"
        style={{ zIndex: 2100 }}
        data-testid="anchor-watch-controls"
      >
        {onFullscreen && (
          <button
            onClick={onFullscreen}
            aria-label="Full screen"
            className="flex h-9 w-9 items-center justify-center rounded-lg bg-black/65 text-white shadow backdrop-blur hover:bg-black/80 active:scale-95"
            style={{ transition: 'background-color 150ms ease-out' }}
          >
            <Expand className="h-4 w-4" />
          </button>
        )}
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
        <button
          onClick={handleImageryToggle}
          aria-label="Toggle satellite imagery"
          className={cn(
            'flex h-9 w-9 items-center justify-center rounded-lg text-white shadow backdrop-blur active:scale-95',
            showImageryLayer ? 'bg-sky-600/90 hover:bg-sky-500/90' : 'bg-black/65 hover:bg-black/80',
          )}
          style={{ transition: 'background-color 150ms ease-out' }}
        >
          <Satellite className="h-4 w-4" />
        </button>
        <button
          onClick={handleRecenter}
          aria-label="Re-centre on anchor"
          className="flex h-9 w-9 items-center justify-center rounded-lg bg-black/65 text-white shadow backdrop-blur hover:bg-black/80 active:scale-95"
          style={{ transition: 'background-color 150ms ease-out' }}
        >
          <Crosshair className="h-4 w-4" />
        </button>
        {editMode === 'none' && (
          <button
            onClick={onClearAnchor}
            className="flex h-9 w-9 items-center justify-center rounded-lg bg-black/65 text-white shadow backdrop-blur hover:bg-red-600/80 active:scale-95"
            style={{ transition: 'background-color 150ms ease-out, color 150ms ease-out' }}
            aria-label="Stop anchor watch"
          >
            <CircleStop className="h-4 w-4" />
          </button>
        )}
      </div>

    </div>
  )
}
