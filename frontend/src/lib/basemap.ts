// Shared basemap constants, imported by the maps so a style or
// tile URL only ever needs changing in one place.

// Carto basemap styles, served by our own backend rather than fetched
// from basemaps.cartocdn.com directly: the backend rewrites the style's
// tile/glyph/sprite URLs to same-origin /api/basemap paths and caches
// every asset in SQLite, so the chart still draws with no uplink. See
// docs/adr/0067-carto-basemap-proxy-and-offline-cache.md.
export const STYLE_LIGHT = '/api/basemap/style/positron'
export const STYLE_DARK = '/api/basemap/style/dark-matter'
export const OPENSEAMAP_TILES = 'https://tiles.openseamap.org/seamark/{z}/{x}/{y}.png'
/** The sea marks overlay on the live maps: one source, one raster layer. */
export const SEAMARK_SOURCE_ID = 'openseamap'
export const SEAMARK_LAYER_ID = 'openseamap-layer'
export const SEAMARK_OPACITY = 0.85
