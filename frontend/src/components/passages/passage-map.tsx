import 'maplibre-gl/dist/maplibre-gl.css'
import maplibregl from 'maplibre-gl'
import type { ReactNode } from 'react'
import { forwardRef, useCallback, useEffect, useImperativeHandle, useMemo, useRef } from 'react'
import type { MapRef } from 'react-map-gl/maplibre'
import { Layer, Map, Marker, Source } from 'react-map-gl/maplibre'

import { MapPlaceLabels, warnIfBaseVectorSourceMissing } from '@/components/map-place-labels'
import { ImageryToggleButton, WorldImageryLayer } from '@/components/world-imagery'
import { useCollapsedMapAttribution } from '@/hooks/use-collapsed-map-attribution'
import { OPENSEAMAP_TILES, STYLE_DARK, STYLE_LIGHT } from '@/lib/basemap'
import { useImageryPreference } from '@/lib/passage-imagery-preference'
import { segmentBounds, segmentsToGeoJSON, type TimedSegment } from '@/lib/passages'
import { hasWebGL2 } from '@/lib/webgl'
import { SAT_HANDOFF_END_ZOOM, SAT_HANDOFF_START_ZOOM, WORLD_IMAGERY_OPACITY_EXPRESSION } from '@/lib/world-imagery'
import { cn } from '@/lib/utils'

// Map line colours cannot be CSS tokens (MapLibre paints with literal
// colours), so the passages share one fixed palette, light enough for the
// dark basemap and dark enough for Positron. A single passage uses the first.
export const PASSAGE_COLOURS = ['#0284c7', '#d97706', '#059669', '#c026d3', '#e11d48', '#4f46e5'] as const
export const passageColour = (index: number) => PASSAGE_COLOURS[index % PASSAGE_COLOURS.length]

export interface MapPassage {
  id: string
  segments: TimedSegment[]
}

export interface PassageMapHandle {
  /** The opening move: jump high over the track and come down onto it.
   * Resolves when the camera has settled. */
  flyIn: () => Promise<void>
}

export interface PassageMapProps {
  passages: MapPassage[]
  /** Passages already replayed in full are drawn solid; the rest are dimmed. */
  playedIds: ReadonlySet<string>
  /** Where the boat marker stands, or null before there is anything to show. */
  boat: { lat: number; lon: number } | null
  isDarkTheme: boolean
  className?: string
  /** Shown in the top right corner, stacked under the satellite button. */
  overlay?: ReactNode
}

const INTRO_START_ZOOM = 1.5
const INTRO_MS = 3200
const FIT_PADDING = { top: 48, bottom: 80, left: 48, right: 48 }

function centreOf(bounds: [number, number, number, number]) {
  return { longitude: (bounds[0] + bounds[2]) / 2, latitude: (bounds[1] + bounds[3]) / 2 }
}

export const PassageMap = forwardRef<PassageMapHandle, PassageMapProps>(function PassageMap(
  { passages, playedIds, boat, isDarkTheme, className, overlay },
  ref,
) {
  const mapRef = useRef<MapRef>(null)
  const collapseAttribution = useCollapsedMapAttribution(mapRef)
  const canRender = hasWebGL2()
  const bounds = useMemo(() => segmentBounds(passages.flatMap((p) => p.segments)), [passages])
  const boundsKey = bounds ? bounds.join(',') : ''
  const loadedRef = useRef(false)
  const [satellite, setSatellite] = useImageryPreference()

  // On load, and whenever the passages change, the whole track is framed.
  const frame = useCallback(() => {
    const map = mapRef.current?.getMap?.()
    if (!map || !bounds) return
    map.fitBounds([[bounds[0], bounds[1]], [bounds[2], bounds[3]]], { padding: FIT_PADDING, duration: 0, maxZoom: 14 })
  }, [bounds])

  useImperativeHandle(ref, () => ({
    flyIn: () => new Promise<void>((resolve) => {
      const map = mapRef.current?.getMap?.()
      if (!map || !bounds) { resolve(); return }
      const centre = centreOf(bounds)
      map.jumpTo({ center: [centre.longitude, centre.latitude], zoom: INTRO_START_ZOOM })
      map.once('moveend', () => resolve())
      map.fitBounds([[bounds[0], bounds[1]], [bounds[2], bounds[3]]], { padding: FIT_PADDING, duration: INTRO_MS, maxZoom: 14 })
    }),
  }), [bounds])

  const handleLoad = useCallback(() => {
    loadedRef.current = true
    const map = mapRef.current?.getMap?.()
    // Fail fast: the place labels draw from the style's own vector source.
    if (map) warnIfBaseVectorSourceMissing(map)
    frame()
  }, [frame])

  useEffect(() => {
    if (loadedRef.current) frame()
    // frame is keyed on the bounds, so this runs when the track changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [boundsKey])

  if (!canRender) {
    return (
      <div
        data-testid="passage-map-webgl2-fallback"
        className="flex h-full items-center justify-center px-3 text-center text-xs text-muted-foreground"
      >
        Map needs WebGL2, which this browser does not provide
      </div>
    )
  }

  const centre = bounds ? centreOf(bounds) : { longitude: 0, latitude: 0 }
  return (
    <div className={cn('relative', className)} data-testid="passage-map">
      <Map
        ref={mapRef}
        mapLib={maplibregl}
        initialViewState={{ ...centre, zoom: INTRO_START_ZOOM }}
        style={{ width: '100%', height: '100%' }}
        mapStyle={isDarkTheme ? STYLE_DARK : STYLE_LIGHT}
        attributionControl={{ compact: true }}
        onIdle={collapseAttribution}
        onLoad={handleLoad}
        dragRotate={false}
        touchPitch={false}
      >
        {/* Bottom to top: basemap, imagery, seamarks, place names, then the
            passage lines. Imagery is anchored under the seamarks so toggling it
            mid-session cannot land on top of them. */}
        <Source id="openseamap" type="raster" tiles={[OPENSEAMAP_TILES]} tileSize={256} attribution="© OpenSeaMap contributors">
          <Layer id="openseamap-layer" type="raster" paint={{ 'raster-opacity': 0.85 }} />
        </Source>
        {satellite && (
          <WorldImageryLayer
            opacity={[...WORLD_IMAGERY_OPACITY_EXPRESSION]}
            minzoom={SAT_HANDOFF_START_ZOOM}
            beforeId="openseamap-layer"
          />
        )}
        <MapPlaceLabels isDarkTheme={isDarkTheme} overImagery={satellite} />
        {passages.map((p, i) => (
          <Source key={p.id} id={`passage-${p.id}`} type="geojson" data={segmentsToGeoJSON(p.segments)}>
            <Layer
              id={`passage-line-${p.id}`}
              type="line"
              layout={{ 'line-cap': 'round', 'line-join': 'round' }}
              paint={{
                'line-color': passageColour(i),
                'line-width': 3.5,
                'line-opacity': playedIds.has(p.id) ? 0.95 : 0.45,
              }}
            />
            {satellite && (
              <Layer
                id={`passage-casing-${p.id}`}
                type="line"
                beforeId={`passage-line-${p.id}`}
                layout={{ 'line-cap': 'round', 'line-join': 'round' }}
                minzoom={SAT_HANDOFF_START_ZOOM}
                paint={{
                  'line-color': '#ffffff',
                  'line-width': 6.5,
                  'line-opacity': ['interpolate', ['linear'], ['zoom'], SAT_HANDOFF_START_ZOOM, 0, SAT_HANDOFF_END_ZOOM, playedIds.has(p.id) ? 0.95 : 0.5],
                }}
              />
            )}
          </Source>
        ))}
        {boat && (
          <Marker longitude={boat.lon} latitude={boat.lat} anchor="center">
            <div
              data-testid="passage-boat-marker"
              className="h-4 w-4 rounded-full border-2 border-background bg-primary shadow-sm ring-2 ring-primary/40"
            />
          </Marker>
        )}
      </Map>
      <div className="absolute right-2 top-2 flex flex-col items-end gap-2">
        <ImageryToggleButton on={satellite} onToggle={() => setSatellite(!satellite)} />
        {overlay}
      </div>
    </div>
  )
})

export interface InsetMapProps {
  bounds: [number, number, number, number] | null
  isDarkTheme: boolean
  className?: string
}

// A small overview in the corner: the region around the track, with a box
// where the track is. Not interactive; the main map is the one to move.
export function InsetMap({ bounds, isDarkTheme, className }: InsetMapProps) {
  const canRender = hasWebGL2()
  if (!canRender || !bounds) return null
  const centre = centreOf(bounds)
  const box: GeoJSON.Feature<GeoJSON.Polygon> = {
    type: 'Feature',
    properties: {},
    geometry: {
      type: 'Polygon',
      coordinates: [[[bounds[0], bounds[1]], [bounds[2], bounds[1]], [bounds[2], bounds[3]], [bounds[0], bounds[3]], [bounds[0], bounds[1]]]],
    },
  }
  // Wide enough to show the surrounding coast: about eight times the track's
  // longest side, never closer than zoom 3 or further out than zoom 1.
  const span = Math.max(bounds[2] - bounds[0], (bounds[3] - bounds[1]) * 1.6, 0.05)
  const zoom = Math.max(1, Math.min(8, Math.log2(360 / (span * 8))))
  return (
    <div className={className} data-testid="passage-inset-map">
      <Map
        mapLib={maplibregl}
        initialViewState={{ ...centre, zoom }}
        style={{ width: '100%', height: '100%' }}
        mapStyle={isDarkTheme ? STYLE_DARK : STYLE_LIGHT}
        interactive={false}
        attributionControl={false}
      >
        <Source id="inset-box" type="geojson" data={box}>
          <Layer id="inset-box-fill" type="fill" paint={{ 'fill-color': '#e11d48', 'fill-opacity': 0.2 }} />
          <Layer id="inset-box-line" type="line" paint={{ 'line-color': '#e11d48', 'line-width': 2 }} />
        </Source>
        <Marker longitude={centre.longitude} latitude={centre.latitude} anchor="center">
          <div className="h-2 w-2 rounded-full bg-destructive" />
        </Marker>
      </Map>
    </div>
  )
}
