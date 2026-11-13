import 'maplibre-gl/dist/maplibre-gl.css'
import maplibregl, { type SymbolLayerSpecification } from 'maplibre-gl'
import { Dialog as DialogPrimitive } from '@base-ui/react/dialog'
import { Expand, Minus, Plus, X } from 'lucide-react'
import { useCallback, useMemo, useRef, useState, type MutableRefObject, type ReactNode, type Ref } from 'react'
import type { MapRef, ViewStateChangeEvent } from 'react-map-gl/maplibre'
import { Layer, Map, Source } from 'react-map-gl/maplibre'

import { FullscreenAlarmBanner } from '@/components/fullscreen-alarm-banner'
import { useCardMapExpanded } from '@/components/lazy-card-map'
import { MapIconButton } from '@/components/map-icon-button'
import { BASE_VECTOR_SOURCE_ID, MapPlaceLabels, PLACE_FONT, PLACE_LABEL_LAYER_IDS, warnIfBaseVectorSourceMissing, type PlaceLabelId } from '@/components/map-place-labels'
import { Dialog, DialogClose, DialogPortal, DialogTitle } from '@/components/ui/dialog'
import { ImageryToggleButton, WorldImageryLayer } from '@/components/world-imagery'
import { useCollapsedMapAttribution } from '@/hooks/use-collapsed-map-attribution'
import { OPENSEAMAP_TILES, STYLE_DARK, STYLE_LIGHT } from '@/lib/basemap'
import type { Bounds } from '@/lib/marina-card'
import { themeColour, useIsDarkTheme } from '@/lib/map-theme'
import { WORLD_IMAGERY_MAX_ZOOM, WORLD_IMAGERY_OPACITY_EXPRESSION, worldImageryVisibleAtZoom } from '@/lib/world-imagery'
import { hasWebGL2 } from '@/lib/webgl'

// The base the marina and anchorage maps on a Mate card share: the Carto style
// the Anchor watch map uses, Esri World Imagery over it and the OpenSeaMap
// raster above that, with the imagery toggle. The card draws its own layers
// between the two as children.

const FIT_PADDING = 28
// The legend sits over the bottom-left corner: its box is about 48 px tall with
// its margin, so a fit leaves that much more room below.
const LEGEND_FIT_ALLOWANCE = 48

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
export function RequireWebGL2({ name, children }: { name: 'marina' | 'anchorage' | 'plan-leg'; children: ReactNode }) {
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
 *
 * The Full screen button opens the same map across the whole screen. Only one
 * map exists at a time: the inline one is unmounted while the dialog is open,
 * and its box stays blank at its size. The imagery choice lives here, so it
 * survives the swap.
 */
export function MateCardMap({ title, bounds, beforeId, controls, legend, omitPlaceLabels, children }: {
  title: string
  bounds: Bounds
  beforeId: string
  controls?: ReactNode
  /** The map's key, drawn bottom-left. `full` is true in the full-screen dialog. */
  legend?: (full: boolean) => ReactNode
  /** Place-name layers this card draws for itself. */
  omitPlaceLabels?: readonly PlaceLabelId[]
  children: ReactNode
}) {
  const [imagery, setImagery] = useState(true)
  const [expanded, setExpanded] = useCardMapExpanded()
  // Where the operator last panned or zoomed to, handed to whichever map mounts
  // next. A ref, not state: nothing draws from it. Null until the map has been
  // moved by hand, so an untouched card still fits its bounds in a bigger box.
  const viewRef = useRef<CardView | null>(null)
  // Full screen unmounts the inline button, so focus goes back to the new one.
  const expandRef = useRef<HTMLButtonElement>(null)
  // Base UI focuses the first tabbable thing, which would be the alarm banner's
  // Acknowledge button; Close is the safe place for a repeated Enter to land.
  const closeRef = useRef<HTMLButtonElement>(null)
  const body = (full: boolean) => (
    <MapBody
      full={full}
      bounds={bounds}
      viewRef={viewRef}
      beforeId={beforeId}
      legend={legend}
      omitPlaceLabels={omitPlaceLabels}
      imagery={imagery}
      onToggleImagery={() => setImagery((v) => !v)}
      onExpand={() => setExpanded(true)}
      expandRef={expandRef}
      controls={controls}
    >
      {children}
    </MapBody>
  )
  return (
    <>
      {!expanded && body(false)}
      <Dialog open={expanded} onOpenChange={setExpanded}>
        <DialogPortal>
          {/* Composed from the primitives rather than DialogContent, whose Close is a 16 px icon. */}
          <DialogPrimitive.Popup initialFocus={closeRef} finalFocus={() => expandRef.current} className="fixed inset-0 z-80 flex flex-col gap-0 bg-background">
            {/* The dialog covers the app's own alarm banner. */}
            {expanded && (
              <div className="shrink-0 p-2">
                <FullscreenAlarmBanner onClose={() => setExpanded(false)} />
              </div>
            )}
            <div className="flex shrink-0 items-center justify-between gap-2 border-b border-border pl-4 pr-2">
              <DialogTitle className="min-w-0 truncate text-base">{title}</DialogTitle>
              <DialogClose ref={closeRef} aria-label="Close" className="flex h-11 w-11 shrink-0 items-center justify-center rounded-lg text-foreground hover:bg-muted focus-visible:outline-2 focus-visible:outline-ring">
                <X className="h-5 w-5" />
              </DialogClose>
            </div>
            <div className="min-h-0 flex-1">{expanded && body(true)}</div>
          </DialogPrimitive.Popup>
        </DialogPortal>
      </Dialog>
    </>
  )
}

/** The centre and zoom of a map the operator has moved. */
interface CardView {
  longitude: number
  latitude: number
  zoom: number
}

interface MapBodyProps {
  full: boolean
  bounds: Bounds
  viewRef: MutableRefObject<CardView | null>
  legend?: (full: boolean) => ReactNode
  omitPlaceLabels?: readonly PlaceLabelId[]
  beforeId: string
  imagery: boolean
  onToggleImagery: () => void
  onExpand: () => void
  expandRef: Ref<HTMLButtonElement>
  controls?: ReactNode
  children: ReactNode
}

/** The map and its button stack, mounted inline or in the full-screen dialog. */
function MapBody({ full, bounds, viewRef, legend, omitPlaceLabels, beforeId, imagery, onToggleImagery, onExpand, expandRef, controls, children }: MapBodyProps) {
  const mapRef = useRef<MapRef | null>(null)
  // Whether the loaded style has the vector source the place names draw from.
  const [hasNames, setHasNames] = useState(false)
  // The zoom buttons move the map from code, which carries no originalEvent, so
  // they tag the move with event data that comes back on its moveend.
  const zoomBy = (zoom: 'zoomIn' | 'zoomOut') => mapRef.current?.[zoom](undefined, { fromButton: true })
  // The satellite fades in from zoom 9, so the names turn white-on-black only from there.
  const [zoomedIn, setZoomedIn] = useState(false)
  const remember = (e: ViewStateChangeEvent & { fromButton?: boolean }) => {
    setZoomedIn(worldImageryVisibleAtZoom(e.viewState.zoom))
    // Only a move the operator made counts: the fit to the bounds on mount does not.
    if (!e.originalEvent && !e.fromButton) return
    const { longitude, latitude, zoom } = e.viewState
    viewRef.current = { longitude, latitude, zoom }
  }
  // The twist of a pinch and Shift with the arrow keys still rotate a map whose
  // dragRotate is off. Locked from the moment the map exists, not on load, which
  // waits for the style and tiles.
  const setMap = useCallback((m: MapRef | null) => {
    mapRef.current = m
    if (!m) return
    const map = m.getMap()
    map.touchZoomRotate.disableRotation()
    map.keyboard.disableRotation()
    // Fail fast: the place names draw from the basemap's own vector source.
    map.on('style.load', () => {
      warnIfBaseVectorSourceMissing(map)
      setHasNames(!!map.getSource(BASE_VECTOR_SOURCE_ID))
    })
    setZoomedIn(worldImageryVisibleAtZoom(map.getZoom()))
  }, [])
  const collapseAttribution = useCollapsedMapAttribution(mapRef)
  const dark = useIsDarkTheme()
  return (
    // A container, so the legend's heading goes by the card's width, not the screen's.
    <div className="@container relative h-full w-full">
      <Map
        ref={setMap}
        mapLib={maplibregl}
        initialViewState={viewRef.current ?? { bounds, fitBoundsOptions: { padding: legend ? { top: FIT_PADDING, right: FIT_PADDING, left: FIT_PADDING, bottom: FIT_PADDING + LEGEND_FIT_ALLOWANCE } : FIT_PADDING, maxZoom: WORLD_IMAGERY_MAX_ZOOM } }}
        onMoveEnd={remember}
        style={{ width: '100%', height: '100%' }}
        mapStyle={dark ? STYLE_DARK : STYLE_LIGHT}
        attributionControl={{ compact: true }}
        onIdle={collapseAttribution}
        // Inline, the Mate thread scrolls and a map under the finger would swallow it.
        // Full screen has nothing behind it to scroll.
        cooperativeGestures={!full}
        dragRotate={false}
        touchPitch={false}
      >
        {children}
        {/* The place names go in just under the card's lowest layer. OpenSeaMap is anchored under the
            lowest of them, and the imagery under OpenSeaMap, so the order is fixed by id and not by which
            layer react-map-gl happens to add first. With no vector source in the style there are no names,
            and OpenSeaMap goes under the card's layer instead. */}
        <MapPlaceLabels isDarkTheme={dark} overImagery={imagery && zoomedIn} beforeId={beforeId} omit={omitPlaceLabels} />
        <Source id="openseamap" type="raster" tiles={[OPENSEAMAP_TILES]} tileSize={256} attribution="© OpenSeaMap contributors">
          <Layer id="openseamap-layer" type="raster" beforeId={hasNames ? PLACE_LABEL_LAYER_IDS.find((id) => !omitPlaceLabels?.includes(id)) : beforeId} paint={{ 'raster-opacity': 0.85 }} />
        </Source>
        {imagery && <WorldImageryLayer opacity={WORLD_IMAGERY_OPACITY_EXPRESSION} beforeId="openseamap-layer" />}
      </Map>

      <div className="absolute right-2 top-2 flex flex-col gap-2">
        {full ? (
          <>
            <MapIconButton icon={Plus} label="Zoom in" onClick={() => zoomBy('zoomIn')} />
            <MapIconButton icon={Minus} label="Zoom out" onClick={() => zoomBy('zoomOut')} />
          </>
        ) : (
          <MapIconButton ref={expandRef} icon={Expand} label="Full screen" onClick={onExpand} />
        )}
        <ImageryToggleButton on={imagery} onToggle={onToggleImagery} />
        {controls}
      </div>
      {/* Bottom-left: the attribution is bottom-right and the buttons top-right. */}
      {legend && <div className="pointer-events-none absolute bottom-2 left-2 max-w-[70%] rounded-md bg-background/85 px-2 py-1.5 shadow-sm">{legend(full)}</div>}
    </div>
  )
}
