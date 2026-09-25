// The zoom level at which an anchor watch's swing circle reads as a circle,
// not a dot under the anchor marker (impeccable critique P0: zoomForRadius's
// old 10-14 range hid a 40m circle under a 32px marker). fitRadiusZoom below
// replaces it.

// MapLibre GL (and the Mapbox GL it forked) tiles the world in 512px tiles,
// not the 256px tiles Leaflet/Google Maps/most "zoom to fit" formulas you'll
// find online assume — confirmed against maplibre-gl's own source
// (Transform.worldSize = tileSize(512) * 2**zoom). The commonly quoted
// constant 156543.03392 (Earth's equatorial circumference / 256) is for that
// 256px convention; feeding it straight into a MapLibre zoom computes a
// value one zoom level too high (the map ends up twice as zoomed in as
// intended, showing half the requested area). The correct constant for
// MapLibre's actual zoom is the circumference over its real 512px tile:
// 40075016.68557849 / 512.
const MAPLIBRE_METERS_PER_PIXEL_AT_ZOOM_0 = 78271.51696402048

export const ANCHOR_VIEW_MIN_ZOOM = 10
export const ANCHOR_VIEW_MAX_ZOOM = 20

/**
 * The MapLibre zoom at which a circle of radius `radiusM`, centred at
 * `latDeg`, has a diameter equal to `fraction` of the map's shorter side
 * (`shortSidePx`) — i.e. the whole swing circle sits comfortably inside the
 * viewport with headroom on every side, rather than being clipped or reduced
 * to a speck under the anchor marker.
 *
 * Clamped to [10, 20]: below 10 the vessel is barely a dot and AIS context is
 * lost; above 20 this app's raster overlays (OpenSeaMap, world imagery) are
 * already past their native resolution.
 */
export function fitRadiusZoom(
  radiusM: number,
  latDeg: number,
  shortSidePx: number,
  fraction = 0.6,
): number {
  if (!(radiusM > 0) || !(shortSidePx > 0) || !(fraction > 0)) {
    return ANCHOR_VIEW_MIN_ZOOM
  }
  const diameterM = radiusM * 2
  const targetSpanPx = fraction * shortSidePx
  const targetMetersPerPixel = diameterM / targetSpanPx
  const latRad = (latDeg * Math.PI) / 180
  const zoom = Math.log2(
    (MAPLIBRE_METERS_PER_PIXEL_AT_ZOOM_0 * Math.cos(latRad)) / targetMetersPerPixel,
  )
  if (!Number.isFinite(zoom)) return ANCHOR_VIEW_MIN_ZOOM
  return Math.max(ANCHOR_VIEW_MIN_ZOOM, Math.min(ANCHOR_VIEW_MAX_ZOOM, zoom))
}
