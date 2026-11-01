// Esri World Imagery (Maxar aerial photography), fetched through the boat's own
// proxy so every map that shows it shares one tile address, one fade and one
// credit. The anchor map and the passage map both draw from here.

export const WORLD_IMAGERY_LAYER_ID = 'world-imagery-layer'
export const WORLD_IMAGERY_TILES = '/api/world-imagery/{z}/{x}/{y}'
export const WORLD_IMAGERY_MAX_ZOOM = 18
export const WORLD_IMAGERY_ATTRIBUTION = 'Source: Esri, Maxar, Earthstar Geographics'
export const SAT_HANDOFF_START_ZOOM = 9
export const SAT_HANDOFF_END_ZOOM = 10

// World Imagery fades in between zoom 9-10 rather than appearing abruptly,
// since its low-zoom tiles are lower quality.
export function computeWorldImageryOpacity(currentZoom: number, enabled: boolean): number {
  if (!enabled) return 0
  return Math.max(
    0,
    Math.min(1, (currentZoom - SAT_HANDOFF_START_ZOOM) / (SAT_HANDOFF_END_ZOOM - SAT_HANDOFF_START_ZOOM)),
  )
}

/** The same fade as a style expression, for maps that let MapLibre follow the
 * zoom itself instead of re-rendering on every zoom step. */
export const WORLD_IMAGERY_OPACITY_EXPRESSION = [
  'interpolate', ['linear'], ['zoom'], SAT_HANDOFF_START_ZOOM, 0, SAT_HANDOFF_END_ZOOM, 1,
] as const

/** Whether any imagery shows at this zoom: the layer begins at the handoff start. */
export const worldImageryVisibleAtZoom = (zoom: number): boolean => zoom >= SAT_HANDOFF_START_ZOOM

export interface WorldImagerySourceOptions {
  /** Makes the tile address absolute, for maps drawn outside the page's own style. */
  origin?: string
  /** Ask the proxy for the real tile or an error, never a stand-in. */
  exact?: boolean
}

/** The one raster source spec for World Imagery, shared by the live maps and
 * the still images so tile size, zoom limit and credit cannot drift apart. */
export function worldImagerySource({ origin = '', exact = false }: WorldImagerySourceOptions) {
  return {
    type: 'raster' as const,
    tiles: [`${origin}${WORLD_IMAGERY_TILES}${exact ? '?exact=1' : ''}`],
    tileSize: 256,
    maxzoom: WORLD_IMAGERY_MAX_ZOOM,
    attribution: WORLD_IMAGERY_ATTRIBUTION,
  }
}

/** The one paint spec for the raster layer. */
export function worldImageryPaint(opacity: number | readonly unknown[]) {
  return { 'raster-opacity': opacity as never, 'raster-fade-duration': 250 }
}
