import 'maplibre-gl/dist/maplibre-gl.css'
import maplibregl from 'maplibre-gl'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { MapRef } from 'react-map-gl/maplibre'
import { Map, Marker, Source, Layer } from 'react-map-gl/maplibre'
import { Anchor, ArrowUp, Crosshair, Expand, MapPin, Minus, Plus, Satellite, Ship, X } from 'lucide-react'
import type { AnchorPlacemark } from '@/hooks/use-anchor-placemarks'
import { cn } from '@/lib/utils'
import { haversineMeters, bearingDeg, destinationPoint } from '@/lib/geo'
import type { NearbyVessel } from '@/hooks/use-nearby-vessels'
import type { RadarTarget } from '@/hooks/use-radar-targets'
import type { TrailPoint } from '@/hooks/use-server-trails'
import type { RodeMethodResult } from '@/lib/rode-plan'
import { MapPlaceLabels, warnIfBaseVectorSourceMissing } from '@/components/map-place-labels'

// Carto basemap styles, served by our own backend rather than fetched
// from basemaps.cartocdn.com directly: the backend rewrites the style's
// tile/glyph/sprite URLs to same-origin /api/basemap paths and caches
// every asset in SQLite, so the chart still draws with no uplink. See
// docs/adr/0067-carto-basemap-proxy-and-offline-cache.md.
const STYLE_LIGHT = '/api/basemap/style/positron'
const STYLE_DARK = '/api/basemap/style/dark-matter'
const OPENSEAMAP_TILES = 'https://tiles.openseamap.org/seamark/{z}/{x}/{y}.png'
const WORLD_IMAGERY_TILES = '/api/world-imagery/{z}/{x}/{y}'
const SAT_HANDOFF_START_ZOOM = 9
const SAT_HANDOFF_END_ZOOM = 10
const WORLD_IMAGERY_MAX_ZOOM = 18
const ANCHOR_WATCH_ZOOM_STORAGE_KEY = 'anchor-watch-map-zoom'
const ANCHOR_WATCH_CENTER_STORAGE_KEY = 'anchor-watch-map-center'
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

// The centre the operator last left the map at, tagged with the anchor
// session it was made in. An untagged record (or one written with no watch
// running) carries a null session, which matches no live session and so
// gets re-centred the moment one arrives.
interface StoredCenter {
  latitude: number
  longitude: number
  sessionId: string | null
}

function readStoredCenter(): StoredCenter | null {
  if (typeof window === 'undefined') return null
  const raw = window.localStorage.getItem(ANCHOR_WATCH_CENTER_STORAGE_KEY)
  if (!raw) return null
  try {
    const parsed = JSON.parse(raw) as { latitude?: unknown; longitude?: unknown; sessionId?: unknown }
    if (
      typeof parsed === 'object' &&
      parsed !== null &&
      typeof parsed.latitude === 'number' &&
      Number.isFinite(parsed.latitude) &&
      typeof parsed.longitude === 'number' &&
      Number.isFinite(parsed.longitude) &&
      parsed.latitude >= -90 &&
      parsed.latitude <= 90 &&
      parsed.longitude >= -180 &&
      parsed.longitude <= 180
    ) {
      return {
        latitude: parsed.latitude,
        longitude: parsed.longitude,
        sessionId: typeof parsed.sessionId === 'string' ? parsed.sessionId : null,
      }
    }
  } catch {
    // Ignore JSON parse errors and return null
  }
  return null
}

function writeStoredCenter(latitude: number, longitude: number, sessionId: string | null): void {
  if (typeof window === 'undefined') return
  window.localStorage.setItem(
    ANCHOR_WATCH_CENTER_STORAGE_KEY,
    JSON.stringify({ latitude, longitude, sessionId }),
  )
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

// A map click that hasn't been committed to a placemark yet — the tooltip
// offering range, bearing and a Pin button. Unlike the AIS selection
// highlight it has no expiry: it must sit still long enough to be clicked.
interface PinCandidate {
  lat: number
  lon: number
}

export interface AnchorWatchMapProps {
  vesselLat: number
  vesselLon: number
  vesselHeadingDeg: number | null
  // Null before an anchor is set — groundwork for an always-on map. The
  // hosts still gate rendering on anchor-set for now, so the no-anchor path
  // below only runs under test until the tile/drawer rework lands.
  anchorLat: number | null
  anchorLon: number | null
  // The watch's set_at: the anchoring session's identity, minted at the drop
  // and held for the life of the watch (a reposition keeps it). Null while
  // the first /api/anchor-watch poll is in flight and whenever no watch is
  // running. The map centres itself on the anchor whenever this changes, so
  // every client swings to the new anchorage instead of sitting over the
  // water it was left looking at.
  anchorSetAt?: string | null
  radiusMeters: number
  depthMeters: number | null
  currentDriftKts: number | null
  currentSetDeg: number | null
  currentDriftImpactKts?: number | null
  distanceMeters: number | null
  bearingDeg: number | null
  // Rendered as the Scope row in the metric overlay, under Current (ADR 0059
  // §3). Null when the host has nothing to say yet (e.g. no map at all) —
  // distinct from a RodeMethodResult carrying its own unavailableReason,
  // which still renders the row with the reason visible.
  scopeRecommendation: RodeMethodResult | null
  isImperial: boolean
  vesselTrail: () => TrailPoint[]
  aisVessels: NearbyVessel[]
  aisTrails: () => Map<string, TrailPoint[]>
  // Optional, and defaulted below, so every existing caller and test that
  // mounts this map without a mayara integration keeps compiling untouched
  // — the same treatment `placemarks` already gets.
  radarTargets?: RadarTarget[]
  isDarkTheme: boolean
  showImageryLayer?: boolean
  onImageryToggle?: (enabled: boolean) => void
  onAnchorReposition: (lat: number, lon: number) => void
  onRadiusChange: (radiusMeters: number) => void
  onFullscreen?: () => void
  // Session-bound pins shared across every client watching this anchorage.
  placemarks?: AnchorPlacemark[]
  onPlacemarkCreate?: (lat: number, lon: number) => void
  onPlacemarkRemove?: (id: string) => void
  className?: string
}

export function AnchorWatchMap({
  vesselLat,
  vesselLon,
  vesselHeadingDeg,
  anchorLat,
  anchorLon,
  anchorSetAt = null,
  radiusMeters,
  depthMeters,
  currentDriftKts,
  currentSetDeg,
  currentDriftImpactKts = null,
  distanceMeters,
  bearingDeg: bearingDegProp,
  scopeRecommendation,
  isImperial,
  vesselTrail,
  aisVessels,
  aisTrails,
  radarTargets = [],
  isDarkTheme,
  showImageryLayer = false,
  onImageryToggle,
  onAnchorReposition,
  onRadiusChange,
  onFullscreen,
  placemarks = [],
  onPlacemarkCreate,
  onPlacemarkRemove,
  className,
}: AnchorWatchMapProps) {
  const hasAnchor = anchorLat !== null && anchorLon !== null
  const mapRef = useRef<MapRef | null>(null)
  const [editMode, setEditMode] = useState<EditMode>('none')
  const [ghostAnchor, setGhostAnchor] = useState<{ lat: number; lon: number } | null>(null)
  const [liveRadius, setLiveRadius] = useState<number | null>(null)
  const originalRadiusRef = useRef<number>(radiusMeters)
  // Which AIS marker is drawn enlarged. Selection is by id, not name: two
  // vessels sharing a name previously both lit up when either was clicked.
  const [selectedVesselId, setSelectedVesselId] = useState<string | null>(null)
  const selectionTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const [pinCandidate, setPinCandidate] = useState<PinCandidate | null>(null)
  const [selectedPlacemarkId, setSelectedPlacemarkId] = useState<string | null>(null)
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

  // Fail-fast per the repo fallback policy: MapPlaceLabels' <Layer>
  // elements (mounted below, after the alarm-circle Source) attach to a
  // source this component never creates. If the loaded style doesn't carry
  // one named BASE_VECTOR_SOURCE_ID, react-map-gl's createLayer silently
  // skips addLayer and every place name just never shows up, with nothing
  // else saying why. styledata fires many times per style load (tiles
  // arriving, etc.), so this is guarded by a ref to log once per load
  // rather than once per event - reset below when isDarkTheme flips,
  // which is a real style reload (STYLE_LIGHT/STYLE_DARK swap).
  const missingSourceWarnedRef = useRef(false)
  const isDarkThemeRef = useRef(isDarkTheme)
  useEffect(() => {
    if (isDarkThemeRef.current !== isDarkTheme) {
      isDarkThemeRef.current = isDarkTheme
      missingSourceWarnedRef.current = false
    }
  }, [isDarkTheme])
  const handleStyleData = useCallback(() => {
    const map = mapRef.current?.getMap()
    if (!map || !map.isStyleLoaded() || missingSourceWarnedRef.current) return
    missingSourceWarnedRef.current = true
    warnIfBaseVectorSourceMissing(map)
  }, [])

  // Re-render trails on each poll cycle (trails are stored in refs, not state)
  useEffect(() => {
    const timer = setInterval(() => setRenderKey((k) => k + 1), 5000)
    return () => clearInterval(timer)
  }, [])

  // Collapse the enlarged marker after 3 seconds. Only the highlight is
  // transient — every marker's range stays on show permanently.
  const selectVessel = useCallback((id: string) => {
    if (selectionTimerRef.current) clearTimeout(selectionTimerRef.current)
    setSelectedVesselId(id)
    selectionTimerRef.current = setTimeout(() => setSelectedVesselId(null), 3000)
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
  const highDriftImpact = currentDriftImpactKts !== null && Math.abs(currentDriftImpactKts) >= HIGH_DRIFT_IMPACT_KTS
  // Scope row (ADR 0059 §3): unavailable covers both a null prop (host has
  // nothing to say) and a RodeMethodResult carrying its own unavailableReason
  // — either way the row shows the reason, never a bare dash.
  const scopeUnit = isImperial ? 'ft' : 'm'
  const scopeAvailable = scopeRecommendation !== null && !scopeRecommendation.unavailableReason
  const scopeReason = scopeRecommendation?.unavailableReason ?? null
  const scopeRodeDisplay = scopeAvailable
    ? Math.round(isImperial ? scopeRecommendation!.recommendedRodeM * 3.28084 : scopeRecommendation!.recommendedRodeM)
    : null
  const formatRange = useCallback(
    (distanceM: number) => {
      if (isImperial) {
        return `${Math.round(distanceM * 3.28084)} ft`
      }
      return distanceM < 1000 ? `${distanceM} m` : `${(distanceM / 1000).toFixed(1)} km`
    },
    [isImperial],
  )

  // GeoJSON data
  // Empty FeatureCollection with no anchor, rather than unmounting the
  // Source/Layers below — see the raster-layer comment further down for why
  // they have to stay mounted regardless.
  const circleGeoJSON = useMemo<GeoJSON.Feature<GeoJSON.Polygon> | GeoJSON.FeatureCollection>(
    () =>
      hasAnchor
        ? generateCircleGeoJSON(anchorLon, anchorLat, displayRadius)
        : { type: 'FeatureCollection', features: [] },
    [hasAnchor, anchorLon, anchorLat, displayRadius],
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
      // No active watch: POST /api/anchor-watch/placemarks would 409 (see
      // backend/anchor_placemarks.go), so don't even raise the tooltip.
      if (!hasAnchor) return
      // Unoccupied water: offer to pin it. Markers (AIS, anchor, self
      // vessel, placemarks) all set suppressNextMapClickRef, so a click
      // that reaches here landed on open chart.
      const { lat, lng } = e.lngLat
      setSelectedPlacemarkId(null)
      setPinCandidate({ lat, lon: lng })
    },
    [editMode, ghostAnchor, hasAnchor, liveRadius, onAnchorReposition, onRadiusChange, setCursor],
  )

  // ── Placemarks ───────────────────────────────────────────────────────────
  // Entering an edit mode takes over the map click ("Tap map to place
  // anchor"), so any open pin tooltip or selected pin would be stranded —
  // and its Pin/Remove buttons unreachable behind the edit overlay.
  useEffect(() => {
    if (editMode !== 'none') {
      setPinCandidate(null)
      setSelectedPlacemarkId(null)
    }
  }, [editMode])

  // Both tooltip buttons sit inside the map, so their clicks also reach
  // maplibre's own handler — which would immediately reopen the tooltip
  // under the button just pressed. Suppress that follow-on click the same
  // way the AIS and placemark markers do.
  const handleConfirmPin = useCallback((e: React.MouseEvent) => {
    e.stopPropagation()
    suppressNextMapClickRef.current = true
    if (!pinCandidate) return
    onPlacemarkCreate?.(pinCandidate.lat, pinCandidate.lon)
    setPinCandidate(null)
  }, [pinCandidate, onPlacemarkCreate])

  const handleDismissPin = useCallback((e: React.MouseEvent) => {
    e.stopPropagation()
    suppressNextMapClickRef.current = true
    setPinCandidate(null)
  }, [])

  const handleSelfVesselClick = useCallback((e: React.MouseEvent) => {
    e.stopPropagation()
    suppressNextMapClickRef.current = true
  }, [])

  const handlePlacemarkClick = useCallback((e: React.MouseEvent, id: string) => {
    e.stopPropagation()
    suppressNextMapClickRef.current = true
    setPinCandidate(null)
    setSelectedPlacemarkId((current) => (current === id ? null : id))
  }, [])

  const handleRemovePlacemark = useCallback((e: React.MouseEvent, id: string) => {
    e.stopPropagation()
    suppressNextMapClickRef.current = true
    onPlacemarkRemove?.(id)
    setSelectedPlacemarkId(null)
  }, [onPlacemarkRemove])

  // ── Map mouse move handler (for radius drag) ─────────────────────────────
  const handleMouseMove = useCallback(
    (e: maplibregl.MapMouseEvent) => {
      // Radius drag, reposition drag and edge-hover detection all project
      // anchor coords — none of them are reachable without an anchor (radius
      // and reposition mode both require one to enter), but bail explicitly
      // rather than let a NaN projection slip through.
      if (!hasAnchor) return
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
    [editMode, hasAnchor, anchorLat, anchorLon, displayRadius, setCursor],
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
      // No anchor to project — bail before it produces NaN.
      if (!hasAnchor) return
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
    [editMode, hasAnchor, anchorLat, anchorLon, displayRadius, liveRadius, onRadiusChange, radiusMeters, setCursor],
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
      // No anchor to project — bail before it produces NaN.
      if (!hasAnchor) return
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
    [editMode, hasAnchor, liveRadius, anchorLat, anchorLon, displayRadius, radiusMeters, onRadiusChange, setCursor],
  )

  // ── Touch move: update ghost anchor or live radius on tablet ─────────────
  const handleTouchMove = useCallback(
    (e: maplibregl.MapTouchEvent) => {
      // Mirrors handleMouseMove: neither branch is reachable without an
      // anchor (both edit modes require one to enter), but bail explicitly.
      if (!hasAnchor) return
      if (editMode === 'radius') {
        const { lat, lng } = e.lngLat
        const newRadius = haversineMeters(anchorLat, anchorLon, lat, lng)
        setLiveRadius(Math.max(5, newRadius))
      } else if (editMode === 'reposition') {
        const { lat, lng } = e.lngLat
        setGhostAnchor({ lat, lon: lng })
      }
    },
    [editMode, hasAnchor, anchorLat, anchorLon],
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
      setPinCandidate(null)
      setSelectedPlacemarkId(null)
      selectVessel(vessel.id)
    },
    [selectVessel],
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
      // Belt-and-braces: the marker itself only renders when hasAnchor, so
      // this shouldn't be reachable without one.
      if (!hasAnchor) return
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
    [confirmAnchorReposition, editMode, hasAnchor, anchorLat, anchorLon, ghostAnchor, setCursor, fetchMotoringTrail],
  )

  // ── Zoom / Recenter controls ────────────────────────────────────────────
  const handleZoomIn = useCallback(() => {
    mapRef.current?.easeTo({ zoom: (mapRef.current.getZoom() ?? 14) + 1, duration: 250 })
  }, [])

  const handleZoomOut = useCallback(() => {
    mapRef.current?.easeTo({ zoom: (mapRef.current.getZoom() ?? 14) - 1, duration: 250 })
  }, [])

  const handleRecenter = useCallback(() => {
    if (typeof window !== 'undefined') {
      window.localStorage.removeItem(ANCHOR_WATCH_CENTER_STORAGE_KEY)
    }
    // Nothing to swing around without an anchor — recentre on the vessel.
    const center: [number, number] = hasAnchor ? [anchorLon, anchorLat] : [vesselLon, vesselLat]
    mapRef.current?.easeTo({ center, duration: 600 })
  }, [hasAnchor, anchorLat, anchorLon, vesselLat, vesselLon])

  // Resolved once, at mount. A stored centre is the operator's pan, but only
  // for the anchorage it was made in: against a different session it is a
  // view of water the boat has left, so the anchor wins. anchorSetAt is null
  // while the first watch poll is in flight, and that is not evidence of a
  // new session — trust the stored centre for now and let the effect below
  // correct it the moment a session id lands.
  const [mountView] = useState(() => {
    const stored = readStoredCenter()
    const belongsToCurrentSession =
      stored !== null && (anchorSetAt === null || stored.sessionId === anchorSetAt)
    return {
      center: belongsToCurrentSession ? stored : null,
      sessionId: belongsToCurrentSession ? stored.sessionId : anchorSetAt,
    }
  })
  // The anchor session the view on screen is currently following.
  const viewSessionRef = useRef<string | null>(mountView.sessionId)

  const handleMoveEnd = useCallback((e: { viewState: { latitude: number; longitude: number; zoom: number } }) => {
    if (typeof window === 'undefined') return
    const { latitude, longitude, zoom } = e.viewState
    if (Number.isFinite(latitude) && Number.isFinite(longitude)) {
      // Tagged with the session the view is following (a ref, so this stays
      // a stable handler) — a pan is only worth restoring for the anchorage
      // it was made in.
      writeStoredCenter(latitude, longitude, viewSessionRef.current)
    }
    if (Number.isFinite(zoom)) {
      setCurrentZoom(zoom)
    }
  }, [])

  // ── Initial map view ─────────────────────────────────────────────────────
  // mountView, resolved above, has already decided whether the stored centre
  // belongs to the anchorage now under the boat.
  const initialViewState = useMemo(() => {
    // No anchor to open on yet — fall back to the vessel.
    const fallback = hasAnchor
      ? { latitude: anchorLat, longitude: anchorLon }
      : { latitude: vesselLat, longitude: vesselLon }
    return {
      longitude: mountView.center ? mountView.center.longitude : fallback.longitude,
      latitude: mountView.center ? mountView.center.latitude : fallback.latitude,
      zoom: currentZoom,
    }
  },
    // Only used as initial value — no deps to avoid re-centering on every update
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [],
  )

  // A new anchorage pulls every client's view to it. The alternative is a
  // chart still centred on last night's bay: the boat and its swing circle
  // are off-screen, and nothing on the map says so. Only a change of session
  // does this — a reposition drag keeps the same set_at (the backend carries
  // it forward), so nobody's view is yanked while the hook is being nudged
  // around, and a pan made during this session survives a reload.
  useEffect(() => {
    if (anchorSetAt === null || anchorLat === null || anchorLon === null) return
    if (viewSessionRef.current === anchorSetAt) return
    viewSessionRef.current = anchorSetAt
    writeStoredCenter(anchorLat, anchorLon, anchorSetAt)
    mapRef.current?.easeTo({ center: [anchorLon, anchorLat], duration: 600 })
  }, [anchorSetAt, anchorLat, anchorLon])

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
        onMoveEnd={handleMoveEnd}
        onStyleData={handleStyleData}
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

        {/*
          Place-name labels (bays, islands, marinas, peaks, town top-up -
          see docs/adr/0066-basemap-place-name-labels.md) mount here,
          unconditionally and with no beforeId. This map pins its rasters
          beforeId="alarm-circle-fill" - i.e. above the whole base style -
          so with imagery on, Carto's own labels (never drawn anyway, which
          is the whole reason this component exists) would be buried under
          the raster regardless. Mounting these label layers right after
          alarm-circle, with no beforeId, is what keeps names visible above
          the satellite raster on this map; the overImagery white/black-halo
          paint below is load-bearing here, not cosmetic.
        */}
        <MapPlaceLabels isDarkTheme={isDarkTheme} overImagery={showImageryLayer} />

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

          This is exactly why the alarm-circle Source and its two layers
          above stay mounted even with no anchor set, rather than being
          unmounted along with the anchor marker: if they weren't there,
          beforeId="alarm-circle-fill" would have no target to attach to, the
          rasters would never mount at all, and layer order would scramble
          the moment an anchor later appeared mid-session. Instead,
          circleGeoJSON collapses to an empty FeatureCollection with no
          anchor (see above), so the layers stay put and simply draw nothing.
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
          const isSelected = selectedVesselId === vessel.id
          // Recomputed from the live fix on every render, like a placemark's
          // — vessel.range_m is only as fresh as the last AIS poll, and it
          // ignores our own movement between polls.
          const distanceM = Math.round(haversineMeters(vesselLat, vesselLon, vessel.lat, vessel.lon))
          const bearing = Math.round(bearingDeg(vesselLat, vesselLon, vessel.lat, vessel.lon))
          return (
            <Marker
              key={vessel.id}
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
                    <div className="whitespace-nowrap text-[9px] font-medium tracking-normal">
                      {formatRange(distanceM)} · {bearing}°
                    </div>
                  </div>
                </div>
              </button>
            </Marker>
          )
        })}

        {/* Radar target markers (ADR 0062). Shape carries identity, colour
            carries state: AIS keeps its filled circle with the Ship glyph
            above, radar draws a filled triangle oriented along course_rad —
            the ARPA convention, readable without relying on hue. A target
            with no lat/lon (no mayara fix and no own-ship position to
            project from) renders nothing, exactly as the AIS block above. */}
        {radarTargets.map((target) => {
          if (target.lat === undefined || target.lon === undefined) return null
          const distanceM = Math.round(haversineMeters(vesselLat, vesselLon, target.lat, target.lon))
          const bearing = Math.round(bearingDeg(vesselLat, vesselLon, target.lat, target.lon))
          const acquiring = target.status === 'acquiring'
          // course_rad is true course in radians, same convention as the
          // vessel heading rotation just below. Unknown motion (course_rad
          // absent) leaves the triangle pointing north rather than guessing.
          const rotationDeg = target.course_rad !== undefined ? (target.course_rad * 180) / Math.PI : 0
          return (
            <Marker
              key={target.id}
              latitude={target.lat}
              longitude={target.lon}
              style={{ zIndex: target.is_dangerous ? 25 : 8 }}
            >
              <div
                className="flex flex-col items-center"
                style={{ minWidth: 40, minHeight: 40 }}
                aria-label={`Radar target: ${target.id}`}
                onClick={(e) => e.stopPropagation()}
              >
                <div
                  style={{
                    transform: `scale(${markerScale}) rotate(${rotationDeg}deg)`,
                    transformOrigin: 'center',
                    transition: 'transform 150ms ease-out',
                  }}
                >
                  {/*
                    bg-foreground, not bg-secondary. In the day theme
                    --secondary is 0 0% 92%, near-white, and the map tiles
                    underneath are near-white too: the first live run put 20
                    targets on the chart and only the 3 dangerous ones were
                    visible, the other 17 showing as a floating RDR label with
                    no marker above it. --foreground is 0% on day and 92% on
                    night, so it is legible in both by construction.
                  */}
                  <div
                    className={cn(
                      'h-4 w-4 shadow-lg',
                      acquiring
                        ? 'border-2 border-foreground bg-transparent'
                        : target.is_dangerous
                          ? 'bg-red-600 ring-2 ring-red-600/50'
                          : 'bg-foreground',
                    )}
                    style={{ clipPath: 'polygon(50% 0%, 0% 100%, 100% 100%)' }}
                  />
                </div>
                <div className="mt-0.5 max-w-24 text-center font-mono text-[9px] font-semibold uppercase tracking-wider text-white drop-shadow-[0_1px_2px_rgba(0,0,0,0.8)]">
                  <div className="whitespace-nowrap">
                    RDR {formatRange(distanceM)} · {bearing}°
                  </div>
                </div>
              </div>
            </Marker>
          )
        })}

        {/* Vessel marker */}
        <Marker latitude={vesselLat} longitude={vesselLon}>
          <div
            className="flex items-center justify-center"
            style={{ width: 40, height: 40 }}
            // Marker elements sit inside the canvas container, so a click
            // here still reaches the map handler. Swallow it: the boat's own
            // icon is not a place you'd pin, and the range would read 0.
            onClick={handleSelfVesselClick}
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
        {hasAnchor && (
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
        )}

        {/* Session placemarks — bombies, shoreline, anything worth watching
            the swing against. Range and bearing are recomputed from the live
            vessel fix on every render, so they close up as the boat drifts
            towards the hazard. */}
        {placemarks.map((pm) => {
          const distanceM = Math.round(haversineMeters(vesselLat, vesselLon, pm.lat, pm.lon))
          const bearing = Math.round(bearingDeg(vesselLat, vesselLon, pm.lat, pm.lon))
          const isSelected = selectedPlacemarkId === pm.id
          return (
            <Marker key={pm.id} latitude={pm.lat} longitude={pm.lon} style={{ zIndex: isSelected ? 1100 : 900 }}>
              <div className="flex flex-col items-center" data-testid={`placemark-${pm.id}`}>
                <button
                  className="flex flex-col items-center"
                  style={{ minWidth: 40, minHeight: 40 }}
                  aria-label={pm.label ? `Placemark: ${pm.label}` : 'Placemark'}
                  onClick={(e) => handlePlacemarkClick(e, pm.id)}
                >
                  <div
                    style={{
                      transform: `scale(${markerScale * (isSelected ? 1.25 : 1)})`,
                      transformOrigin: 'bottom center',
                      transition: 'transform 150ms ease-out',
                    }}
                  >
                    <MapPin
                      className={cn(
                        'h-6 w-6 drop-shadow-[0_1px_2px_rgba(0,0,0,0.8)]',
                        isSelected ? 'text-fuchsia-300' : 'text-fuchsia-400',
                      )}
                    />
                  </div>
                  {/* Same treatment as an AIS vessel's label — shadowed
                      text, no plate. Persistent map labels are drawn this
                      way so a crowded anchorage doesn't fill up with opaque
                      boxes; only interactive controls (the Pin tooltip, the
                      Remove button) get a solid background. */}
                  <div className="mt-0.5 max-w-28 text-center font-mono text-[9px] font-semibold uppercase tracking-wider text-fuchsia-300 drop-shadow-[0_1px_2px_rgba(0,0,0,0.8)]">
                    {pm.label && <div className="truncate">{pm.label}</div>}
                    <div className="whitespace-nowrap text-[9px] font-medium tracking-normal text-white">
                      {formatRange(distanceM)} · {bearing}°
                    </div>
                  </div>
                </button>
                {isSelected && (
                  <button
                    onClick={(e) => handleRemovePlacemark(e, pm.id)}
                    className="mt-1 rounded-full bg-white/15 px-2 py-0.5 text-[10px] font-medium text-white backdrop-blur hover:bg-white/25 active:scale-95"
                    style={{ transition: 'background-color 150ms ease-out' }}
                  >
                    Remove
                  </button>
                )}
              </div>
            </Marker>
          )
        })}

        {/* Pin candidate — the tooltip raised by clicking open water. It has
            no expiry: it must survive long enough to be clicked. */}
        {pinCandidate && (
          <Marker latitude={pinCandidate.lat} longitude={pinCandidate.lon} style={{ zIndex: 1200 }}>
            <div className="flex flex-col items-center" data-testid="pin-candidate">
              <MapPin className="h-6 w-6 text-white drop-shadow-[0_1px_2px_rgba(0,0,0,0.8)]" />
              <div className="mt-1 flex items-center gap-1.5 rounded-full bg-black/80 py-1 pl-2.5 pr-1 backdrop-blur">
                <span className="whitespace-nowrap font-mono text-[11px] text-white">
                  {formatRange(Math.round(haversineMeters(vesselLat, vesselLon, pinCandidate.lat, pinCandidate.lon)))}
                  {' · '}
                  {Math.round(bearingDeg(vesselLat, vesselLon, pinCandidate.lat, pinCandidate.lon))}°
                </span>
                <button
                  onClick={handleConfirmPin}
                  className="rounded-full bg-fuchsia-500/90 px-2.5 py-0.5 text-[11px] font-semibold text-white hover:bg-fuchsia-400 active:scale-95"
                  style={{ transition: 'background-color 150ms ease-out' }}
                >
                  Pin
                </button>
                <button
                  onClick={handleDismissPin}
                  aria-label="Dismiss"
                  className="flex h-5 w-5 items-center justify-center rounded-full text-white/60 hover:bg-white/15 hover:text-white"
                  style={{ transition: 'background-color 150ms ease-out' }}
                >
                  <X className="h-3 w-3" />
                </button>
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
            // radiusMeters is a required prop, so without an anchor it's
            // whatever inactive default the host passed — gate on hasAnchor
            // instead of a null check, or that default would read as real.
            value: hasAnchor
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
          {
            label: 'Scope',
            value: scopeRodeDisplay !== null ? `${scopeRodeDisplay}` : '—',
            // Just the recommended rode and its unit, same as Distance/
            // Radius/Depth above. The ratio and the MIN_SCOPE_RATIO floor
            // marker used to ride in this suffix too; they're only reachable
            // via the row's tooltip now (rowTitle below), which already
            // carried the full note regardless.
            unit: scopeAvailable ? scopeUnit : '',
            alert: false,
            // The reason (not the unit) renders in the unit slot when
            // unavailable — see the render branch below — so the fallback
            // policy's "surface the reason, never a bare dash" holds here too.
            reason: scopeReason,
            // The full note (depth source, wind, ratio, and the
            // MIN_SCOPE_RATIO floor marker when it binds) rides along as a
            // row tooltip rather than crowding the overlay — only set when
            // there's a result to describe.
            rowTitle: scopeAvailable ? scopeRecommendation!.note : undefined,
          },
        ].map(({ label, value, unit, alert, setDeg, live, reason, rowTitle }, index) => (
          <div
            key={label}
            title={rowTitle}
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
                <ArrowUp
                  className={cn(
                    'shrink-0 text-white',
                    highDriftImpact ? 'h-5 w-5' : 'h-4 w-4',
                    (value === '0.0' || value === '—') && 'invisible',
                  )}
                  strokeWidth={highDriftImpact ? 2.75 : 2}
                  style={{ transform: `rotate(${setDeg ?? 0}deg)` }}
                  aria-hidden="true"
                />
                <p className="font-display tabular-nums leading-tight text-white" style={{ fontSize: '1.1rem' }}>
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
                {reason ? (
                  <span
                    className="ml-1 inline-block max-w-[9rem] truncate align-bottom text-[11px] text-white/80"
                    title={reason}
                  >
                    {reason}
                  </span>
                ) : (
                  <span className="ml-0.5 text-[11px] text-white/80">{unit}</span>
                )}
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
        {/* No stop/clear control here — both hosts are gaining a labeled
            Raise button with its own confirm dialog (anchor-watch-tile.tsx,
            anchor-watch-drawer.tsx); a one-tap unlabeled destructive icon
            next to that would be inconsistent. */}
      </div>

    </div>
  )
}
