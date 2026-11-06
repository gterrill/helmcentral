import 'maplibre-gl/dist/maplibre-gl.css'
import maplibregl from 'maplibre-gl'
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react'
import type { MapRef } from 'react-map-gl/maplibre'
import { Layer, Map, Marker, NavigationControl, Source } from 'react-map-gl/maplibre'

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
/** A press this long without moving starts a block selection. */
const HOLD_MS = 400
/** A press that moves further than this before the hold fires is a pan. */
const HOLD_SLOP_PX = 8

export interface OfflineCoastlineMapProps {
  status: OfflineCoastlineStatus
  /**
   * Select mode: a tap toggles a square; press and hold then drag on a touch screen, or
   * Shift-drag with a mouse, selects a block. A plain drag still pans and pinch or wheel zooms.
   */
  selecting?: boolean
  selected?: ReadonlySet<string>
  onSelectedChange?: (next: Set<string>) => void
}

const EMPTY_SELECTION: ReadonlySet<string> = new Set()

// A finger on the map pans, zooms or selects while selecting, so the page must
// not scroll, select text or show a callout under it.
const SELECTING_STYLE = {
  touchAction: 'none',
  userSelect: 'none',
  WebkitUserSelect: 'none',
  WebkitTouchCallout: 'none',
} as const

function squareFeature(lat: number, lon: number) {
  return { type: 'Feature' as const, properties: {}, geometry: squarePolygon(lat, lon) }
}

interface DragState {
  start: { lat: number; lon: number }
  /** The square the pointer was last over. */
  last: { lat: number; lon: number }
  base: Set<string>
  /** pending: a tap, or a hold not yet taken; block: selecting a block. */
  mode: 'pending' | 'block'
  pointerId: number
  /** Where the press began, for measuring the slop. */
  x: number
  y: number
  /** The latest pointer position, so the block starts under the finger. */
  lastX: number
  lastY: number
  timer: ReturnType<typeof setTimeout> | null
  /** Removes the window listeners that end a press released outside the map. */
  unlisten: (() => void) | null
  /** The map whose drag a block turned off, so it can be given back even after unmount. */
  map: { dragPan: { enable: () => void; disable: () => void } } | null
}

function clearHold(drag: DragState) {
  if (drag.timer) clearTimeout(drag.timer)
  drag.timer = null
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
  /** Set between the end of a pan and the end of its glide. */
  const glidingRef = useRef(false)
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
  const canSelectRef = useRef(canSelect)
  useLayoutEffect(() => {
    canSelectRef.current = canSelect
  }, [canSelect])

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
  // The hold timer outlives the render that started it, so it reads this.
  const selectableRef = useRef(selectable)
  useLayoutEffect(() => {
    selectableRef.current = selectable
  }, [selectable])
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
  // browser's emulated mouse events and toggle the square twice. A plain drag
  // pans the map and pinch and wheel zoom it, so a block selection starts only
  // on a deliberate gesture: Shift held with a mouse, or a press that holds
  // still for HOLD_MS. The browser dispatches pointerdown before the mouse and
  // touch events the map listens to, so switching dragPan off here stops a
  // block drag from also panning. Pointer capture then keeps the drag alive
  // when the pointer crosses the marker or the attribution on its way out. A
  // press that moves before the hold fires is a pan: the press ends there and
  // the map keeps the gesture. The handlers do nothing unless a press began,
  // and a press begins only while squares can be selected.
  const squareAtPoint = (clientX: number, clientY: number) => {
    const map = mapRef.current?.getMap?.()
    const box = containerRef.current?.getBoundingClientRect()
    if (!map || !box) return null
    // Unwrapped, so a drag across the 180° meridian keeps its corners in order.
    const ll = map.unproject([clientX - box.left, clientY - box.top])
    return { lat: Math.floor(ll.lat), lon: Math.floor(ll.lng) }
  }
  const endGesture = useCallback(() => {
    const drag = dragRef.current
    if (drag) {
      clearHold(drag)
      drag.unlisten?.()
      drag.unlisten = null
      if (drag.mode === 'block') drag.map?.dragPan.enable()
    }
    dragRef.current = null
    setPreview(null)
  }, [])
  const blockSelection = (drag: DragState, end: { lat: number; lon: number }) =>
    new Set([...drag.base, ...squaresInBox(drag.start, end, selectableRef.current)])
  const beginBlock = (drag: DragState, target: HTMLDivElement) => {
    if (dragRef.current !== drag || drag.mode !== 'pending') return
    clearHold(drag)
    // The view may have zoomed out past the limit since the press.
    if (!canSelectRef.current) {
      endGesture()
      return
    }
    // The pointer may have drifted inside the slop since the press.
    const here = squareAtPoint(drag.lastX, drag.lastY)
    if (here) {
      drag.start = here
      drag.last = here
    }
    try {
      target.setPointerCapture?.(drag.pointerId)
    } catch {
      // The pointer is already gone (released elsewhere): leave the map the gesture.
      endGesture()
      return
    }
    drag.mode = 'block'
    drag.map = mapRef.current?.getMap?.() ?? null
    drag.map?.dragPan.disable()
    // Show the start square at once, so the operator sees the hold took.
    setPreview(blockSelection(drag, drag.start))
  }
  const onPointerDown = (e: ReactPointerEvent<HTMLDivElement>) => {
    if (!canSelect || (e.pointerType === 'mouse' && e.button !== 0)) return
    // The attribution and other map controls keep their own clicks.
    if ((e.target as Element).closest('.maplibregl-ctrl')) return
    if (!e.isPrimary) {
      endGesture() // a second finger is a pinch, not a selection
      return
    }
    const here = squareAtPoint(e.clientX, e.clientY)
    if (!here) return
    endGesture()
    const map = mapRef.current?.getMap?.()
    // Only the glide after a pan counts: a zoom easing from the buttons or the
    // wheel leaves a press alone.
    const gliding = glidingRef.current && (map?.isMoving() ?? false)
    if (gliding) map?.stop()
    const shiftBlock = e.pointerType === 'mouse' && e.shiftKey
    // A press that stops a gliding map is not a tap or a hold; a mouse block
    // is Shift-drag, whatever the map was doing.
    if (gliding && !shiftBlock) return
    // Keep Shift from extending a text selection across the page.
    if (shiftBlock) e.preventDefault()
    const drag: DragState = {
      start: here,
      last: here,
      base: new Set(selected),
      mode: 'pending',
      pointerId: e.pointerId,
      x: e.clientX,
      y: e.clientY,
      lastX: e.clientX,
      lastY: e.clientY,
      timer: null,
      unlisten: null,
      map: null,
    }
    dragRef.current = drag
    // Until the hold takes there is no pointer capture, so a release outside
    // the container reaches only the window. Other pointers' releases are not
    // ours. Ending the gesture removes these listeners.
    const onLost = (ev: PointerEvent) => {
      if (ev.pointerId === drag.pointerId && dragRef.current === drag) endGesture()
    }
    window.addEventListener('pointerup', onLost)
    window.addEventListener('pointercancel', onLost)
    drag.unlisten = () => {
      window.removeEventListener('pointerup', onLost)
      window.removeEventListener('pointercancel', onLost)
    }
    const target = e.currentTarget
    if (shiftBlock) {
      beginBlock(drag, target)
    } else if (e.pointerType !== 'mouse') {
      drag.timer = setTimeout(() => {
        beginBlock(drag, target)
        // A cue on phones that the hold took.
        if (dragRef.current === drag && drag.mode === 'block') navigator.vibrate?.(10)
      }, HOLD_MS)
    }
  }
  const onPointerMove = (e: ReactPointerEvent<HTMLDivElement>) => {
    const drag = dragRef.current
    if (!drag) return
    if (drag.mode === 'pending') {
      // Moving before the hold fires hands the gesture to the map.
      if (Math.hypot(e.clientX - drag.x, e.clientY - drag.y) > HOLD_SLOP_PX) {
        endGesture()
      } else {
        drag.lastX = e.clientX
        drag.lastY = e.clientY
      }
      return
    }
    const here = squareAtPoint(e.clientX, e.clientY)
    // Rebuild the preview only when the pointer enters another square.
    if (!here || (here.lat === drag.last.lat && here.lon === drag.last.lon)) return
    drag.last = here
    setPreview(blockSelection(drag, here))
  }
  const onPointerUp = (e: ReactPointerEvent<HTMLDivElement>) => {
    const drag = dragRef.current
    if (!drag || drag.pointerId !== e.pointerId) return
    endGesture()
    if (drag.mode === 'block') {
      // A hold released without moving keeps its square, which the preview
      // showed selected: a block adds and never removes.
      onSelectedChange?.(blockSelection(drag, squareAtPoint(e.clientX, e.clientY) ?? drag.last))
      return
    }
    // A tap: toggle the one square in the selection as it is now.
    const tapped = squareContaining(drag.start.lat, drag.start.lon)
    const key = squareKey(tapped.lat, tapped.lon)
    const next = new Set(selected)
    if (next.has(key)) next.delete(key)
    else if (squareSelectable(selectable, tapped.lat, tapped.lon)) next.add(key)
    onSelectedChange?.(next)
  }
  const onPointerCancel = (e: ReactPointerEvent<HTMLDivElement>) => {
    if (dragRef.current && dragRef.current.pointerId !== e.pointerId) return
    endGesture()
  }
  useEffect(() => endGesture, [endGesture])

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
    if (!selecting) endGesture()
  }, [applyGestures, selecting, endGesture])

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
    <div
      ref={containerRef}
      className="relative h-full w-full"
      style={selecting ? SELECTING_STYLE : undefined}
      // A held finger must not open the browser's own long-press menu.
      onContextMenu={selecting ? (e) => e.preventDefault() : undefined}
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
        onDragEnd={() => {
          glidingRef.current = true
        }}
        onMoveEnd={() => {
          glidingRef.current = false
          readView()
        }}
        boxZoom={!selecting}
        // Nothing pans until a press has moved past the slop, so a pending hold
        // never has the map moving under it.
        clickTolerance={HOLD_SLOP_PX}
        // Quick taps select squares; they must not also zoom.
        doubleClickZoom={!selecting}
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
        <NavigationControl position="top-right" showCompass={false} />
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
