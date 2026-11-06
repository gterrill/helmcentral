import 'maplibre-gl/dist/maplibre-gl.css'
import maplibregl from 'maplibre-gl'
import { useCallback, useEffect, useMemo, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react'
import type { MapRef } from 'react-map-gl/maplibre'
import { Layer, Map, Marker, Source } from 'react-map-gl/maplibre'

import { useCollapsedMapAttribution } from '@/hooks/use-collapsed-map-attribution'
import { STYLE_DARK, STYLE_LIGHT } from '@/lib/basemap'
import { themeColour, useIsDarkTheme } from '@/lib/map-theme'
import {
  coverageRingGeoJSON,
  coverageSquaresGeoJSON,
  gridSquares,
  parseSquareKey,
  selectionAllowedAt,
  squareContaining,
  squareKey,
  squarePolygon,
  squareSelectable,
  squaresInBox,
  type OfflineCoastlineStatus,
  type ViewBounds,
} from '@/lib/offline-coastline'
import { hasWebGL2 } from '@/lib/webgl'

const FIT_PADDING = 32
/** Squares this old or older are drawn at the faintest shade. */
const OLDEST_SHADE_DAYS = 365

export interface OfflineCoastlineMapProps {
  status: OfflineCoastlineStatus
  /** Select mode: a tap toggles a square and a drag selects a block; the map does not pan. */
  selecting?: boolean
  selected?: ReadonlySet<string>
  onSelectedChange?: (next: Set<string>) => void
}

const EMPTY_SELECTION: ReadonlySet<string> = new Set()

function squareFeature(lat: number, lon: number) {
  return { type: 'Feature' as const, properties: {}, geometry: squarePolygon(lat, lon) }
}

interface DragState {
  start: { lat: number; lon: number }
  /** The square the pointer was last over. */
  last: { lat: number; lon: number }
  base: Set<string>
  moved: boolean
}

export default function OfflineCoastlineMapImpl({
  status,
  selecting = false,
  selected = EMPTY_SELECTION,
  onSelectedChange,
}: OfflineCoastlineMapProps) {
  const mapRef = useRef<MapRef>(null)
  const collapseAttribution = useCollapsedMapAttribution(mapRef)
  const dark = useIsDarkTheme()

  const squares = useMemo(() => coverageSquaresGeoJSON(status), [status])
  const ring = useMemo(
    () => (status.position ? coverageRingGeoJSON(status.position.lat, status.position.lon, status.radius_nm) : null),
    [status.position, status.radius_nm],
  )

  // Frame what the operator cares about: the squares inside the ring and the
  // ring itself. Squares held from earlier voyages can lie anywhere and would
  // zoom the map out to a continent. With no fix there is no ring, so fall back
  // to every square.
  const bounds = useMemo(() => {
    const inRange = status.squares.filter((q) => q.in_range)
    const framed = ring ? inRange : status.squares
    const lats = framed.flatMap((q) => [q.lat, q.lat + 1])
    const lons = framed.flatMap((q) => [q.lon, q.lon + 1])
    if (ring) {
      for (const [lon, lat] of ring.geometry.coordinates[0]) {
        lats.push(lat)
        lons.push(lon)
      }
    }
    if (lats.length === 0) return null
    return [[Math.min(...lons), Math.min(...lats)], [Math.max(...lons), Math.max(...lats)]] as [[number, number], [number, number]]
  }, [status.squares, ring])

  // The ring follows the boat and the status arrives every few seconds, so
  // fitting on every update would snap the map back under the operator's hand.
  // Fit when the set of squares first appears or changes, and never once the
  // operator has panned or zoomed.
  const squareSet = useMemo(
    () => status.squares.filter((q) => q.in_range).map((q) => `${q.lat},${q.lon}`).sort().join('|') + (ring ? '+ring' : ''),
    [status.squares, ring],
  )
  const operatorMovedRef = useRef(false)
  const fittedSetRef = useRef<string | null>(null)

  const frame = useCallback(() => {
    const map = mapRef.current?.getMap?.()
    if (!map || !bounds || operatorMovedRef.current || fittedSetRef.current === squareSet) return
    fittedSetRef.current = squareSet
    map.fitBounds(bounds, { padding: FIT_PADDING, duration: 0, maxZoom: 9 })
  }, [bounds, squareSet])

  useEffect(() => {
    frame()
  }, [frame])

  // ── select mode ──
  const [view, setView] = useState<ViewBounds | null>(null)
  const [preview, setPreview] = useState<ReadonlySet<string> | null>(null)
  const dragRef = useRef<DragState | null>(null)
  const containerRef = useRef<HTMLDivElement>(null)

  const readView = useCallback(() => {
    const map = mapRef.current?.getMap?.()
    if (!map) return
    const b = map.getBounds()
    const next = { west: b.getWest(), south: b.getSouth(), east: b.getEast(), north: b.getNorth() }
    setView((prev) =>
      prev && prev.west === next.west && prev.south === next.south && prev.east === next.east && prev.north === next.north
        ? prev
        : next,
    )
  }, [])

  const canSelect = selecting && view !== null && selectionAllowedAt(view)

  // The status is a new object on every poll, so the grid depends on what it
  // is made from: the held squares and the selectable range.
  const heldKey = status.squares
    .filter((q) => q.state === 'ready')
    .map((q) => squareKey(q.lat, q.lon))
    .sort()
    .join('|')
  const held = useMemo(() => new Set(heldKey ? heldKey.split('|') : []), [heldKey])
  const { lat_min, lat_max, lon_min, lon_max } = status.selectable
  const selectable = useMemo(() => ({ lat_min, lat_max, lon_min, lon_max }), [lat_min, lat_max, lon_min, lon_max])
  const grid = useMemo(() => {
    if (!canSelect || !view) return null
    return {
      type: 'FeatureCollection' as const,
      features: gridSquares(view, held, selectable).map((q) => squareFeature(q.lat, q.lon)),
    }
  }, [canSelect, view, held, selectable])
  const shown = preview ?? selected
  const selectedSquares = useMemo(() => {
    if (shown.size === 0) return null
    return {
      type: 'FeatureCollection' as const,
      features: [...shown].map((k) => {
        const { lat, lon } = parseSquareKey(k)
        return squareFeature(lat, lon)
      }),
    }
  }, [shown])

  // Selection uses pointer events on the container, not the map's mouse and
  // touch events: a touch would otherwise fire both its touch events and the
  // browser's emulated mouse events and toggle the square twice, and pointer
  // capture keeps a drag alive when the pointer crosses the marker or the
  // attribution on its way out. The handlers do nothing unless a drag began,
  // and a drag begins only while squares can be selected.
  const squareAtPoint = (clientX: number, clientY: number) => {
    const map = mapRef.current?.getMap?.()
    const box = containerRef.current?.getBoundingClientRect()
    if (!map || !box) return null
    // Unwrapped, so a drag across the 180° meridian keeps its corners in order.
    const ll = map.unproject([clientX - box.left, clientY - box.top])
    return { lat: Math.floor(ll.lat), lon: Math.floor(ll.lng) }
  }
  const cancelDrag = useCallback(() => {
    dragRef.current = null
    setPreview(null)
  }, [])
  const onPointerDown = (e: ReactPointerEvent<HTMLDivElement>) => {
    if (!canSelect || (e.pointerType === 'mouse' && e.button !== 0)) return
    // The attribution and other map controls keep their own clicks.
    if ((e.target as Element).closest('.maplibregl-ctrl')) return
    if (!e.isPrimary) {
      cancelDrag() // a second finger is a pinch, not a selection
      return
    }
    const here = squareAtPoint(e.clientX, e.clientY)
    if (!here) return
    e.currentTarget.setPointerCapture?.(e.pointerId)
    dragRef.current = { start: here, last: here, base: new Set(selected), moved: false }
  }
  const onPointerMove = (e: ReactPointerEvent<HTMLDivElement>) => {
    const drag = dragRef.current
    if (!drag) return
    const here = squareAtPoint(e.clientX, e.clientY)
    // Rebuild the preview only when the pointer enters another square.
    if (!here || (here.lat === drag.last.lat && here.lon === drag.last.lon)) return
    drag.last = here
    if (!drag.moved && here.lat === drag.start.lat && here.lon === drag.start.lon) return
    drag.moved = true
    setPreview(new Set([...drag.base, ...squaresInBox(drag.start, here, selectable)]))
  }
  const onPointerUp = (e: ReactPointerEvent<HTMLDivElement>) => {
    const drag = dragRef.current
    dragRef.current = null
    if (!drag) return
    setPreview(null)
    if (drag.moved) {
      const end = squareAtPoint(e.clientX, e.clientY) ?? drag.last
      onSelectedChange?.(new Set([...drag.base, ...squaresInBox(drag.start, end, selectable)]))
      return
    }
    // A tap: toggle the one square.
    const tapped = squareContaining(drag.start.lat, drag.start.lon)
    const key = squareKey(tapped.lat, tapped.lon)
    const next = new Set(drag.base)
    if (next.has(key)) next.delete(key)
    else if (squareSelectable(selectable, tapped.lat, tapped.lon)) next.add(key)
    onSelectedChange?.(next)
  }
  const onPointerCancel = () => cancelDrag()

  // cooperativeGestures is read once, when the map is built; the React wrapper
  // does not pass later changes on. Selecting needs a single finger to act, so
  // switch it on the map itself, on a change and once the map has loaded.
  const applyGestures = useCallback(() => {
    const map = mapRef.current?.getMap?.()
    if (!map) return
    if (selecting) map.cooperativeGestures.disable()
    else map.cooperativeGestures.enable()
  }, [selecting])
  useEffect(() => {
    applyGestures()
    if (!selecting) cancelDrag()
  }, [applyGestures, selecting, cancelDrag])

  if (!hasWebGL2()) {
    return (
      <div
        data-testid="offline-coastline-map-fallback"
        className="flex h-full items-center justify-center px-3 text-center text-xs text-muted-foreground"
      >
        The coverage map needs WebGL2, which this browser does not provide
      </div>
    )
  }

  const primary = themeColour('--primary')
  const destructive = themeColour('--destructive')
  const muted = themeColour('--muted-foreground')
  const requested = themeColour('--coverage-requested')
  const foreground = themeColour('--foreground')

  return (
    // A finger on the map selects while selecting, so the page must not scroll under it.
    <div
      ref={containerRef}
      className="relative h-full w-full"
      style={selecting ? { touchAction: 'none' } : undefined}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerUp}
      onPointerCancel={onPointerCancel}
    >
      {selecting && view && !canSelect && (
        <p className="pointer-events-none absolute inset-x-0 top-2 z-10 mx-auto w-fit rounded-md bg-background/90 px-2 py-1 text-xs text-foreground shadow-sm">
          Zoom in to select squares
        </p>
      )}
      <Map
        ref={mapRef}
        mapLib={maplibregl}
        initialViewState={{ longitude: status.position?.lon ?? 0, latitude: status.position?.lat ?? 0, zoom: 4 }}
        style={{ width: '100%', height: '100%' }}
        mapStyle={dark ? STYLE_DARK : STYLE_LIGHT}
        attributionControl={{ compact: true }}
        onIdle={collapseAttribution}
        onLoad={() => {
          frame()
          readView()
          applyGestures()
        }}
        onMoveEnd={readView}
        dragPan={!selecting}
        // Only a move the operator made carries the browser event; fitBounds
        // does not.
        onMoveStart={(e) => {
          if (e.originalEvent) operatorMovedRef.current = true
        }}
        // The settings page scrolls, and a map under the wheel would swallow it.
        cooperativeGestures
        dragRotate={false}
        touchPitch={false}
      >
        <Source id="coverage-squares" type="geojson" data={squares}>
          {/* Held squares: the newer the download, the stronger the shade. */}
          <Layer
            id="coverage-held"
            type="fill"
            filter={['all', ['==', ['get', 'state'], 'ready'], ['!', ['get', 'failed']]]}
            paint={{
              'fill-color': primary,
              'fill-opacity': ['interpolate', ['linear'], ['get', 'age_days'], 0, 0.45, OLDEST_SHADE_DAYS, 0.1],
            }}
          />
          {/* Asked for and waiting: its own shade, apart from held coverage. */}
          <Layer
            id="coverage-requested"
            type="fill"
            filter={['all', ['get', 'requested'], ['!', ['get', 'failed']]]}
            paint={{ 'fill-color': requested, 'fill-opacity': 0.4 }}
          />
          <Layer
            id="coverage-failed"
            type="fill"
            filter={['get', 'failed']}
            paint={{ 'fill-color': destructive, 'fill-opacity': 0.4 }}
          />
          {/* Wanted but not held yet: outline only, so it never reads as coverage. */}
          {/* Squares with edits since are outlined heavier. */}
          <Layer
            id="coverage-outline"
            type="line"
            paint={{
              'line-color': ['case', ['get', 'failed'], destructive, ['==', ['get', 'state'], 'pending'], muted, primary],
              'line-width': ['case', ['get', 'has_edits'], 3, 1],
              'line-opacity': 0.8,
            }}
          />
        </Source>
        {grid && (
          <Source id="coverage-grid" type="geojson" data={grid}>
            <Layer id="coverage-grid-line" type="line" paint={{ 'line-color': muted, 'line-width': 0.5, 'line-opacity': 0.6 }} />
          </Source>
        )}
        {selectedSquares && (
          <Source id="coverage-selected" type="geojson" data={selectedSquares}>
            <Layer id="coverage-selected-fill" type="fill" paint={{ 'fill-color': foreground, 'fill-opacity': 0.2 }} />
            <Layer id="coverage-selected-line" type="line" paint={{ 'line-color': foreground, 'line-width': 2 }} />
          </Source>
        )}
        {ring && (
          <Source id="coverage-ring" type="geojson" data={ring}>
            <Layer
              id="coverage-ring-line"
              type="line"
              layout={{ 'line-join': 'round' }}
              paint={{ 'line-color': primary, 'line-width': 2, 'line-dasharray': [3, 2] }}
            />
          </Source>
        )}
        {status.position && (
          <Marker longitude={status.position.lon} latitude={status.position.lat} anchor="center">
            <div
              data-testid="offline-coastline-boat"
              className="h-4 w-4 rounded-full border-2 border-background bg-primary shadow-sm ring-2 ring-primary/40"
            />
          </Marker>
        )}
      </Map>
    </div>
  )
}
