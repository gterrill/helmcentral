import 'maplibre-gl/dist/maplibre-gl.css'
import maplibregl, { type SymbolLayerSpecification } from 'maplibre-gl'
import { useMemo, useRef, useState, type ReactNode } from 'react'
import type { MapRef } from 'react-map-gl/maplibre'
import { Layer, Map, Source } from 'react-map-gl/maplibre'

import { PLACE_FONT } from '@/components/map-place-labels'
import { ImageryToggleButton, WorldImageryLayer } from '@/components/world-imagery'
import { useCollapsedMapAttribution } from '@/hooks/use-collapsed-map-attribution'
import { OPENSEAMAP_TILES, STYLE_DARK, STYLE_LIGHT } from '@/lib/basemap'
import type { Bounds } from '@/lib/marina-card'
import { themeColour, useIsDarkTheme } from '@/lib/map-theme'
import { WORLD_IMAGERY_MAX_ZOOM, WORLD_IMAGERY_OPACITY_EXPRESSION } from '@/lib/world-imagery'
import { hasWebGL2 } from '@/lib/webgl'

// The base the marina and anchorage maps on a Mate card share: the Carto style
// the Anchor watch map uses, Esri World Imagery over it and the OpenSeaMap
// raster above that, with the imagery toggle. The card draws its own layers
// between the two as children.

const FIT_PADDING = 28

export const ROUND_LINE_LAYOUT = { 'line-cap': 'round', 'line-join': 'round' } as const

/** A point label under its dot. */
export const POINT_LABEL_LAYOUT: SymbolLayerSpecification['layout'] = {
  'text-field': ['get', 'label'],
  'text-font': PLACE_FONT,
  'text-size': 12,
  'text-anchor': 'top',
  'text-offset': [0, 0.8],
}

/**
 * The map body reads the theme tokens, which throw when one is missing, so it
 * is mounted only once WebGL2 is known to be there. A browser without it gets
 * the note and nothing else.
 */
export function RequireWebGL2({ name, children }: { name: 'marina' | 'anchorage'; children: ReactNode }) {
  if (!hasWebGL2()) {
    return (
      <div data-testid={`${name}-map-fallback`} className="flex h-full items-center justify-center px-3 text-center text-xs text-muted-foreground">
        The {name} map needs WebGL2, which this browser does not provide
      </div>
    )
  }
  return <>{children}</>
}

export interface MapColours {
  background: string
  /** The paint of a text label: the foreground colour on a halo of the background. */
  labelPaint: { 'text-color': string; 'text-halo-color': string; 'text-halo-width': number }
}

/** The card's own colours, read again when the page changes between day and night. */
export function useThemeColours<T>(read: (base: MapColours) => T): T {
  const dark = useIsDarkTheme()
  return useMemo(() => {
    const background = themeColour('--background')
    return read({ background, labelPaint: { 'text-color': themeColour('--foreground'), 'text-halo-color': background, 'text-halo-width': 1.5 } })
    // eslint-disable-next-line react-hooks/exhaustive-deps -- `dark` is the cue to read the tokens again
  }, [dark])
}

/**
 * The map under a card's layers. `beforeId` is the card's lowest layer: the
 * rasters are declared last and anchored under it, with the imagery under the
 * OpenSeaMap marks, because a layer that mounts later (imagery toggled back
 * on) would otherwise land on top. `controls` sit under the imagery toggle.
 */
export function MateCardMap({ bounds, beforeId, controls, children }: { bounds: Bounds; beforeId: string; controls?: ReactNode; children: ReactNode }) {
  const mapRef = useRef<MapRef>(null)
  const collapseAttribution = useCollapsedMapAttribution(mapRef)
  const dark = useIsDarkTheme()
  const [imagery, setImagery] = useState(true)
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
        {children}
        <Source id="openseamap" type="raster" tiles={[OPENSEAMAP_TILES]} tileSize={256} attribution="© OpenSeaMap contributors">
          <Layer id="openseamap-layer" type="raster" beforeId={beforeId} paint={{ 'raster-opacity': 0.85 }} />
        </Source>
        {imagery && <WorldImageryLayer opacity={WORLD_IMAGERY_OPACITY_EXPRESSION} beforeId="openseamap-layer" />}
      </Map>

      <div className="absolute right-2 top-2 flex flex-col gap-2">
        <ImageryToggleButton on={imagery} onToggle={() => setImagery((v) => !v)} />
        {controls}
      </div>
    </div>
  )
}
