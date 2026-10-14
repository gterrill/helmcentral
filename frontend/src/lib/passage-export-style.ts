// The look of an exported passage: one map style, shared by the blog embed and
// the static images so both read alike. Everything here is public: Carto's
// basemap and OpenSeaMap's seamarks are fetched by the reader's own browser.
// Nothing points back at the boat (ADR 0174).

export const MAPLIBRE_VERSION = '5.24.0'
export const MAPLIBRE_JS_URL = `https://cdn.jsdelivr.net/npm/maplibre-gl@${MAPLIBRE_VERSION}/dist/maplibre-gl.js`
export const MAPLIBRE_CSS_URL = `https://cdn.jsdelivr.net/npm/maplibre-gl@${MAPLIBRE_VERSION}/dist/maplibre-gl.css`
// sha384 of the 5.24.0 files. They match the jsDelivr copies byte for byte, and
// a test recomputes them from node_modules so an upgrade cannot leave them stale.
export const MAPLIBRE_JS_SRI = 'sha384-5+cfbwT0iiub6VsQAdn6yz16nr6sDiQoHx6tm4O8OVYXHYOxcffFmCJBL0dgdvGp'
export const MAPLIBRE_CSS_SRI = 'sha384-uTttxo/aOKbdE5RlD/SPzSDoDmNvGlUYPjONi2MN/b7c9HPSvW07OIuyP7uL6jxK'

export const EXPORT_BASE_STYLE_URL = 'https://basemaps.cartocdn.com/gl/voyager-gl-style/style.json'
export const EXPORT_SEAMARK_TILES = 'https://tiles.openseamap.org/seamark/{z}/{x}/{y}.png'
export const EXPORT_SEAMARK_ATTRIBUTION = '© OpenSeaMap contributors'
/** Drawn onto static images, which have no attribution control. */
export const EXPORT_IMAGE_ATTRIBUTION = '© CARTO · © OpenStreetMap contributors · © OpenSeaMap'

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
