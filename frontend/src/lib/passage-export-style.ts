// The look of an exported passage: one map style, shared by the blog embed and
// the static images so both read alike. Everything here is public: Carto's
// basemap and OpenSeaMap's seamarks are fetched by the reader's own browser.
// Nothing points back at the boat (ADR 0174).

import type { LayerSpecification, SourceSpecification } from 'maplibre-gl'

import {
  SAT_HANDOFF_START_ZOOM,
  WORLD_IMAGERY_OPACITY_EXPRESSION,
  worldImageryPaint,
  worldImagerySource,
} from '@/lib/world-imagery'

export const MAPLIBRE_VERSION = '5.24.0'
export const MAPLIBRE_JS_URL = `https://cdn.jsdelivr.net/npm/maplibre-gl@${MAPLIBRE_VERSION}/dist/maplibre-gl.js`
export const MAPLIBRE_CSS_URL = `https://cdn.jsdelivr.net/npm/maplibre-gl@${MAPLIBRE_VERSION}/dist/maplibre-gl.css`
// sha384 of the 5.24.0 files. They match the jsDelivr copies byte for byte, and
// a test recomputes them from node_modules so an upgrade cannot leave them stale.
export const MAPLIBRE_JS_SRI = 'sha384-5+cfbwT0iiub6VsQAdn6yz16nr6sDiQoHx6tm4O8OVYXHYOxcffFmCJBL0dgdvGp'
export const MAPLIBRE_CSS_SRI = 'sha384-uTttxo/aOKbdE5RlD/SPzSDoDmNvGlUYPjONi2MN/b7c9HPSvW07OIuyP7uL6jxK'

export const THREE_VERSION = '0.170.0'
export const THREE_URL = `https://cdn.jsdelivr.net/npm/three@${THREE_VERSION}/build/three.module.min.js`
// sha384 of three.module.min.js 0.170.0, the version the app pins. Obtained by
// hashing node_modules/three/build/three.module.min.js and checked against the
// bytes jsDelivr serves (curl the URL above and hash the response: they are
// identical). A test recomputes it from node_modules so an upgrade cannot leave
// it stale. The card fetches the file with this hash as its integrity value
// and only then imports it.
export const THREE_SRI = 'sha384-IDC7sAMAIMB/TZ6dgKKPPAKZ2bXXXP8+FBMBC8cU319eBhKITx+PaalhfDkDNH28'

export const EXPORT_BASE_STYLE_URL = 'https://basemaps.cartocdn.com/gl/voyager-gl-style/style.json'
export const EXPORT_SEAMARK_TILES = 'https://tiles.openseamap.org/seamark/{z}/{x}/{y}.png'
export const EXPORT_SEAMARK_ATTRIBUTION = '© OpenSeaMap contributors'
/** Drawn onto static images, which have no attribution control. */
export const EXPORT_IMAGE_ATTRIBUTION = '© CARTO · © OpenStreetMap contributors · © OpenSeaMap'
/** The same, with the imagery credit, for images drawn over satellite imagery. */
export const EXPORT_IMAGE_ATTRIBUTION_SATELLITE = `${EXPORT_IMAGE_ATTRIBUTION} · Esri, Maxar, Earthstar Geographics`
export const exportImageAttribution = (satellite: boolean) => (satellite ? EXPORT_IMAGE_ATTRIBUTION_SATELLITE : EXPORT_IMAGE_ATTRIBUTION)

export const IMAGERY_SOURCE_ID = 'hcp-imagery'
export const IMAGERY_LAYER_ID = 'hcp-imagery'

/** Adds the satellite raster to a still image's map, under `beforeId` (the
 * style's first label layer) so names stay on top. The tiles are asked for
 * exactly, so a stand-in tile fails the image instead of being drawn. They come through the boat's own proxy, which works because the image is
 * drawn in the operator's browser. The blog embed is read elsewhere, so it never
 * carries this. `origin` makes the tile address absolute. */
export function addExportImagery(
  map: { addSource: (id: string, spec: SourceSpecification) => unknown; addLayer: (spec: LayerSpecification, beforeId?: string) => unknown },
  origin: string,
  beforeId?: string,
): void {
  map.addSource(IMAGERY_SOURCE_ID, worldImagerySource({ origin, exact: true }))
  map.addLayer({
    id: IMAGERY_LAYER_ID,
    type: 'raster',
    source: IMAGERY_SOURCE_ID,
    minzoom: SAT_HANDOFF_START_ZOOM,
    paint: worldImageryPaint(WORLD_IMAGERY_OPACITY_EXPRESSION) as never,
  }, beforeId)
}

/** Fixed palette: dark enough to read on the light Voyager basemap. */
export const EXPORT_COLOURS = ['#0369a1', '#c2410c', '#047857', '#a21caf', '#be123c', '#4338ca'] as const
export const exportColour = (index: number) => EXPORT_COLOURS[index % EXPORT_COLOURS.length]

export const TRACK_SOURCE_ID = 'hcp-track'
export const DONE_SOURCE_ID = 'hcp-done'
export const SEAMARK_SOURCE_ID = 'hcp-seamarks'
export const SEAMARK_LAYER_ID = 'hcp-seamarks'

const lineLayout = { 'line-cap': 'round', 'line-join': 'round' } as const

function trackLayerPair(source: string, prefix: string) {
  return [
    { id: `${prefix}-casing`, type: 'line', source, layout: lineLayout, paint: { 'line-color': '#ffffff', 'line-width': 8, 'line-opacity': 0.95 } },
    { id: `${prefix}-line`, type: 'line', source, layout: lineLayout, paint: { 'line-color': ['get', 'c'], 'line-width': 4.5, 'line-opacity': 1 } },
  ]
}

/** The whole track: a white casing under a bold coloured line. */
export const TRACK_LAYERS = trackLayerPair(TRACK_SOURCE_ID, 'hcp-track')
/** The part the boat has already covered, drawn over a dimmed whole track. */
export const DONE_LAYERS = trackLayerPair(DONE_SOURCE_ID, 'hcp-done')

/** The zoom of the small overview map: the track's bounds about 25 times
 * over, so the coast and enough of the continent show to place it, between
 * zoom 2.5 and 5. */
export function insetZoom(bounds: [number, number, number, number]): number {
  const span = Math.max(bounds[2] - bounds[0], (bounds[3] - bounds[1]) * 1.6, 0.05)
  return Math.max(2.5, Math.min(5, Math.log2(360 / (span * 25))))
}

/** Whether the track's box would be too small to see on the overview (under
 * 6 px), so a dot is drawn in its place. */
export function insetNeedsDot(bounds: [number, number, number, number], zoom: number): boolean {
  const span = Math.max(bounds[2] - bounds[0], bounds[3] - bounds[1])
  return (256 * 2 ** zoom * span) / 360 < 6
}
