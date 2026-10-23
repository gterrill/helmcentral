import 'maplibre-gl/dist/maplibre-gl.css'
import maplibregl from 'maplibre-gl'
import { useCallback, useEffect, useMemo, useRef, useSyncExternalStore } from 'react'
import type { MapRef } from 'react-map-gl/maplibre'
import { Layer, Map, Marker, Source } from 'react-map-gl/maplibre'

import { useCollapsedMapAttribution } from '@/hooks/use-collapsed-map-attribution'
import { STYLE_DARK, STYLE_LIGHT } from '@/lib/basemap'
import {
  coverageRingGeoJSON,
  coverageSquaresGeoJSON,
  type OfflineCoastlineStatus,
} from '@/lib/offline-coastline'
import { hasWebGL2 } from '@/lib/webgl'

// MapLibre paints with literal colours, not CSS classes, so the layers read
// the app's own theme tokens at draw time. They follow day and night with the
// rest of the page and nothing here picks a colour of its own.
function token(name: string): string {
  const channels = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  if (!channels) throw new Error(`theme token ${name} is not defined`)
  // The tokens hold space-separated channels; MapLibre's colour parser wants commas.
  return `hsl(${channels.split(/\s+/).join(', ')})`
}

function subscribeTheme(onChange: () => void) {
  const observer = new MutationObserver(onChange)
  observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
  return () => observer.disconnect()
}
const isDarkNow = () => document.documentElement.classList.contains('dark')

const FIT_PADDING = 32
/** Squares this old or older are drawn at the faintest shade. */
const OLDEST_SHADE_DAYS = 365

export interface OfflineCoastlineMapProps {
  status: OfflineCoastlineStatus
}

export default function OfflineCoastlineMapImpl({ status }: OfflineCoastlineMapProps) {
  const mapRef = useRef<MapRef>(null)
  const collapseAttribution = useCollapsedMapAttribution(mapRef)
  const dark = useSyncExternalStore(subscribeTheme, isDarkNow, () => false)

  const squares = useMemo(() => coverageSquaresGeoJSON(status), [status])
  const ring = useMemo(
    () => (status.position ? coverageRingGeoJSON(status.position.lat, status.position.lon, status.radius_nm) : null),
    [status.position, status.radius_nm],
  )

  // Frame the squares (and the ring when there is a fix) once they are known.
  const boundsKey = useMemo(() => {
    const lats = status.squares.flatMap((q) => [q.lat, q.lat + 1])
    const lons = status.squares.flatMap((q) => [q.lon, q.lon + 1])
    if (ring) {
      for (const [lon, lat] of ring.geometry.coordinates[0]) {
        lats.push(lat)
        lons.push(lon)
      }
    }
    if (lats.length === 0) return ''
    return [Math.min(...lons), Math.min(...lats), Math.max(...lons), Math.max(...lats)].join(',')
  }, [status.squares, ring])

  const frame = useCallback(() => {
    const map = mapRef.current?.getMap?.()
    if (!map || !boundsKey) return
    const [w, s, e, n] = boundsKey.split(',').map(Number)
    map.fitBounds([[w, s], [e, n]], { padding: FIT_PADDING, duration: 0, maxZoom: 9 })
  }, [boundsKey])

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

  const primary = token('--primary')
  const destructive = token('--destructive')
  const muted = token('--muted-foreground')

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
        <Layer
          id="coverage-pending"
          type="fill"
          filter={['==', ['get', 'state'], 'pending']}
          paint={{ 'fill-color': muted, 'fill-opacity': 0.12 }}
        />
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
