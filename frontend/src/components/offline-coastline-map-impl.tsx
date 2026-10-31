import 'maplibre-gl/dist/maplibre-gl.css'
import maplibregl from 'maplibre-gl'
import { useCallback, useEffect, useMemo, useRef } from 'react'
import type { MapRef } from 'react-map-gl/maplibre'
import { Layer, Map, Marker, Source } from 'react-map-gl/maplibre'

import { useCollapsedMapAttribution } from '@/hooks/use-collapsed-map-attribution'
import { STYLE_DARK, STYLE_LIGHT } from '@/lib/basemap'
import { themeColour, useIsDarkTheme } from '@/lib/map-theme'
import {
  coverageRingGeoJSON,
  coverageSquaresGeoJSON,
  type OfflineCoastlineStatus,
} from '@/lib/offline-coastline'
import { hasWebGL2 } from '@/lib/webgl'

const FIT_PADDING = 32
/** Squares this old or older are drawn at the faintest shade. */
const OLDEST_SHADE_DAYS = 365

export interface OfflineCoastlineMapProps {
  status: OfflineCoastlineStatus
}

export default function OfflineCoastlineMapImpl({ status }: OfflineCoastlineMapProps) {
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

  return (
    <Map
      ref={mapRef}
      mapLib={maplibregl}
      initialViewState={{ longitude: status.position?.lon ?? 0, latitude: status.position?.lat ?? 0, zoom: 4 }}
      style={{ width: '100%', height: '100%' }}
      mapStyle={dark ? STYLE_DARK : STYLE_LIGHT}
      attributionControl={{ compact: true }}
      onIdle={collapseAttribution}
      onLoad={frame}
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
  )
}
