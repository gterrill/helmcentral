import 'maplibre-gl/dist/maplibre-gl.css'
import maplibregl, { type SymbolLayerSpecification } from 'maplibre-gl'
import { Store } from 'lucide-react'
import { useMemo, useRef, useState } from 'react'
import type { MapRef } from 'react-map-gl/maplibre'
import { Layer, Map, Source } from 'react-map-gl/maplibre'

import { PLACE_FONT } from '@/components/map-place-labels'
import { MapToggleButton } from '@/components/map-toggle-button'
import { ImageryToggleButton, WorldImageryLayer } from '@/components/world-imagery'
import { useCollapsedMapAttribution } from '@/hooks/use-collapsed-map-attribution'
import { OPENSEAMAP_TILES, STYLE_DARK, STYLE_LIGHT } from '@/lib/basemap'
import { marinaFeatures, marinaFrame } from '@/lib/marina-card'
import { themeColour, useIsDarkTheme } from '@/lib/map-theme'
import type { MarinaCard } from '@/lib/mate-cards'
import { WORLD_IMAGERY_MAX_ZOOM, WORLD_IMAGERY_OPACITY_EXPRESSION } from '@/lib/world-imagery'
import { hasWebGL2 } from '@/lib/webgl'

const FIT_PADDING = 28
const ROUND_LINE_LAYOUT = { 'line-cap': 'round', 'line-join': 'round' } as const
/** A facility or service label under its dot. */
const POINT_LABEL_LAYOUT: SymbolLayerSpecification['layout'] = {
  'text-field': ['get', 'label'],
  'text-font': PLACE_FONT,
  'text-size': 12,
  'text-anchor': 'top',
  'text-offset': [0, 0.8],
}

/**
 * The marina map on a Mate card (ADR 0242). The base is the same Carto style
 * the Anchor watch map uses, with Esri World Imagery over it and the OpenSeaMap
 * raster above that: OpenSeaMap draws the lateral marks and the beacons, so
 * this map does not draw them again. On top go the marina's own outline, its
 * pontoons with their arm names, and a dot for each facility. Shops and
 * services ashore are off until the operator asks.
 *
 * The map body reads the theme tokens, which throw when one is missing, so it
 * is mounted only once WebGL2 is known to be there. A browser without it gets
 * the note and nothing else.
 */
export default function AssistantMarinaMapImpl({ card }: { card: MarinaCard }) {
  if (!hasWebGL2()) {
    return (
      <div data-testid="marina-map-fallback" className="flex h-full items-center justify-center px-3 text-center text-xs text-muted-foreground">
        The marina map needs WebGL2, which this browser does not provide
      </div>
    )
  }
  return <MarinaMap card={card} />
}

function MarinaMap({ card }: { card: MarinaCard }) {
  const mapRef = useRef<MapRef>(null)
  const collapseAttribution = useCollapsedMapAttribution(mapRef)
  const dark = useIsDarkTheme()
  const [imagery, setImagery] = useState(true)
  const [showAshore, setShowAshore] = useState(false)

  const features = useMemo(() => marinaFeatures(card), [card])
  const bounds = useMemo(() => marinaFrame(card), [card])

  // The colours are read again when the page changes between day and night.
  const colours = useMemo(() => {
    const background = themeColour('--background')
    return {
      primary: themeColour('--primary'),
      outline: themeColour('--gauge-primary'),
      background,
      muted: themeColour('--muted-foreground'),
      labelPaint: { 'text-color': themeColour('--foreground'), 'text-halo-color': background, 'text-halo-width': 1.5 },
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- `dark` is the cue to read the tokens again
  }, [dark])

  if (!bounds) throw new Error('the marina map was given a card with nothing to frame')

  const { primary, outline, background, muted, labelPaint } = colours
  const hasAshore = card.geometry.ashore.length > 0

  return (
    <div className="relative h-full w-full">
      <Map
        ref={mapRef}
        mapLib={maplibregl}
        initialViewState={{ bounds, fitBoundsOptions: { padding: FIT_PADDING, maxZoom: WORLD_IMAGERY_MAX_ZOOM } }}
        style={{ width: '100%', height: '100%' }}
        mapStyle={dark ? STYLE_DARK : STYLE_LIGHT}
        attributionControl={{ compact: true }}
        onIdle={collapseAttribution}
        // The Mate thread scrolls, and a map under the finger would swallow it.
        cooperativeGestures
        dragRotate={false}
        touchPitch={false}
      >
        {/* The marina's outline: a thin stroke on a light casing, so it reads on imagery and on the chart. */}
        <Source id="marina-outline" type="geojson" data={features.outline}>
          <Layer id="marina-outline-casing" type="line" paint={{ 'line-color': background, 'line-width': 3.5, 'line-opacity': 0.7 }} />
          <Layer id="marina-outline-line" type="line" paint={{ 'line-color': outline, 'line-width': 1.5 }} />
        </Source>

        {/* Pontoons. A private one is dashed and unlabelled. */}
        <Source id="marina-pontoons" type="geojson" data={features.pontoons}>
          <Layer
            id="marina-pontoons-casing"
            type="line"
            layout={ROUND_LINE_LAYOUT}
            paint={{ 'line-color': background, 'line-width': 4.5, 'line-opacity': 0.7 }}
          />
          <Layer
            id="marina-pontoons-line"
            type="line"
            filter={['!', ['get', 'private']]}
            layout={ROUND_LINE_LAYOUT}
            paint={{ 'line-color': primary, 'line-width': 2.5 }}
          />
          <Layer
            id="marina-pontoons-private-line"
            type="line"
            filter={['get', 'private']}
            layout={{ 'line-join': 'round' }}
            paint={{ 'line-color': muted, 'line-width': 2, 'line-dasharray': [2, 2] }}
          />
        </Source>
        <Source id="marina-pontoon-labels" type="geojson" data={features.pontoonLabels}>
          <Layer
            id="marina-pontoon-label"
            type="symbol"
            layout={{
              'text-field': ['get', 'name'],
              'text-font': PLACE_FONT,
              'text-size': 12,
              'text-offset': [0, -1],
              'text-allow-overlap': false,
            }}
            paint={labelPaint}
          />
        </Source>

        <Source id="marina-facilities" type="geojson" data={features.facilities}>
          <Layer
            id="marina-facility-dot"
            type="circle"
            paint={{ 'circle-radius': 5, 'circle-color': primary, 'circle-stroke-color': background, 'circle-stroke-width': 1.5 }}
          />
          <Layer
            id="marina-facility-label"
            type="symbol"
            layout={POINT_LABEL_LAYOUT}
            paint={labelPaint}
          />
        </Source>

        {showAshore && (
          <Source id="marina-ashore" type="geojson" data={features.ashore}>
            <Layer
              id="marina-ashore-dot"
              type="circle"
              paint={{ 'circle-radius': 4, 'circle-color': muted, 'circle-stroke-color': background, 'circle-stroke-width': 1.5 }}
            />
            <Layer
              id="marina-ashore-label"
              type="symbol"
              layout={POINT_LABEL_LAYOUT}
              paint={labelPaint}
            />
          </Source>
        )}

        {/* Rasters are declared last and anchored under the marina's own
            layers, with the imagery under the OpenSeaMap marks: a layer that
            mounts later (imagery toggled back on) would otherwise land on top. */}
        <Source id="openseamap" type="raster" tiles={[OPENSEAMAP_TILES]} tileSize={256} attribution="© OpenSeaMap contributors">
          <Layer id="openseamap-layer" type="raster" beforeId="marina-outline-casing" paint={{ 'raster-opacity': 0.85 }} />
        </Source>
        {imagery && <WorldImageryLayer opacity={WORLD_IMAGERY_OPACITY_EXPRESSION} beforeId="openseamap-layer" />}
      </Map>

      <div className="absolute right-2 top-2 flex flex-col gap-2">
        <ImageryToggleButton on={imagery} onToggle={() => setImagery((v) => !v)} />
        {hasAshore && (
          <MapToggleButton icon={Store} label="Toggle shops and services ashore" on={showAshore} onToggle={() => setShowAshore((v) => !v)} />
        )}
      </div>
    </div>
  )
}
