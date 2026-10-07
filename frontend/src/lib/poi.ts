import {
  Anchor,
  ArrowDownToLine,
  Binoculars,
  CircleDot,
  Fish,
  Footprints,
  Fuel,
  Landmark,
  Sailboat,
  TreePalm,
  Waves,
  type LucideIcon,
} from 'lucide-react'

/**
 * A point-of-interest feature exactly as GET /api/poi returns it (see
 * backend/poi_providers.go's poiFeature), mapped from the wire's snake_case
 * into the app's usual camelCase.
 */
export interface PoiFeature {
  id: string
  category: string
  name: string
  lat: number
  lon: number
  distanceM: number
  bearingDeg: number
  detail: string
  sourceUrl: string
}

export interface PoiCategory {
  id: string
  label: string
  icon: LucideIcon
}

/**
 * The eleven POI categories, in the same order as poiCategoryIDs in
 * backend/poi_providers.go — kept in step by the backend-parity test below.
 * A small closed set with an icon per id, mirroring CLUSTER_ICONS
 * (lib/cluster-icons.ts): the category id is persisted and validated
 * server-side, so an allowlist means a saved config can never name a
 * category that does not exist.
 */
export const POI_CATEGORIES: PoiCategory[] = [
  { id: 'anchorage', label: 'Anchorage', icon: Anchor },
  { id: 'bay', label: 'Bay', icon: Waves },
  { id: 'island', label: 'Island', icon: TreePalm },
  { id: 'marina', label: 'Marina', icon: Sailboat },
  { id: 'fuel', label: 'Fuel', icon: Fuel },
  { id: 'ramp', label: 'Boat ramp', icon: ArrowDownToLine },
  { id: 'mooring', label: 'Mooring', icon: CircleDot },
  { id: 'historic', label: 'Historic', icon: Landmark },
  { id: 'viewpoint', label: 'Lookout', icon: Binoculars },
  { id: 'dive', label: 'Dive & snorkel', icon: Fish },
  { id: 'trail', label: 'Trail', icon: Footprints },
]

export const POI_CATEGORY_IDS = POI_CATEGORIES.map((c) => c.id)

const POI_CATEGORY_BY_ID = new Map(POI_CATEGORIES.map((c) => [c.id, c]))

export function poiCategoryById(id: string): PoiCategory | undefined {
  return POI_CATEGORY_BY_ID.get(id)
}

// MapLibre GL tiles the world in 512px tiles (Transform.worldSize =
// 512 * 2**zoom), not the 256px tiles most "zoom to fit" formulas assume. The
// commonly quoted 156543.03392 (circumference / 256) would leave this map
// twice as zoomed in as intended; the constant for MapLibre's own zoom is the
// equatorial circumference over 512: 40075016.68557849 / 512. Same value and
// reason as anchor-view.ts.
const MAPLIBRE_METERS_PER_PIXEL_AT_ZOOM_0 = 78271.51696402048
const METERS_PER_NM = 1852
export const POI_MAP_MIN_ZOOM = 5
export const POI_MAP_MAX_ZOOM = 16

/**
 * The zoom level whose visible span holds a circle of the given range,
 * centred at `lat`, inside a map `heightPx` tall — i.e. the range's full
 * diameter fits the tile's height. Clamped to [5, 16]: the floor leaves room
 * for the maximum 25 nm range on a short map at high latitude (about zoom 7.8
 * on a 300px map at 27 degrees south, at MapLibre's 512px tile scale), and
 * below 5 the vessel's own position is barely a dot; above 16 the raster
 * tiles this app draws (OpenSeaMap, the Carto basemap) are already past their
 * native resolution.
 */
export function zoomForRangeNm(rangeNm: number, lat: number, heightPx: number): number {
  if (!(rangeNm > 0) || !(heightPx > 0)) return POI_MAP_MIN_ZOOM
  const diameterM = rangeNm * METERS_PER_NM * 2
  const targetMetersPerPixel = diameterM / heightPx
  const latRad = (lat * Math.PI) / 180
  const zoom = Math.log2(
    (MAPLIBRE_METERS_PER_PIXEL_AT_ZOOM_0 * Math.cos(latRad)) / targetMetersPerPixel,
  )
  if (!Number.isFinite(zoom)) return POI_MAP_MIN_ZOOM
  return Math.max(POI_MAP_MIN_ZOOM, Math.min(POI_MAP_MAX_ZOOM, zoom))
}

// Same tile size MAPLIBRE_METERS_PER_PIXEL_AT_ZOOM_0 above is derived from
// (earth's equatorial circumference / 512), so fitCameraAroundPoint below
// stays in the same projection zoomForRangeNm already uses.
const MERCATOR_TILE_SIZE_PX = 512

/** Fraction of the world's width, 0 (antimeridian, west) to 1 (antimeridian, east). */
function mercatorX(lon: number): number {
  return (lon + 180) / 360
}

/** Fraction of the world's height, 0 (north) to 1 (south) - Web Mercator's characteristic latitude stretch. */
function mercatorY(lat: number): number {
  const latRad = (lat * Math.PI) / 180
  return 0.5 - Math.log(Math.tan(Math.PI / 4 + latRad / 2)) / (2 * Math.PI)
}

export interface MapPoint {
  lat: number
  lon: number
}

/**
 * The camera (centre + zoom) that keeps `center` in the middle of a
 * `widthPx` x `heightPx` viewport while fitting every one of `points` inside
 * it, with `paddingPx` of clearance on every edge for marker size and labels.
 * The boat stays at the centre so the operator reads every POI relative to
 * it; fitting a bounding box instead would slide the boat off to one side
 * whenever the POIs lie mostly in one direction. Done in the same Web
 * Mercator projection zoomForRangeNm above uses, so it needs no live map
 * instance and can be unit-tested without a WebGL context
 * (poi-map-tile-impl.tsx still uses the real map's easeTo/jumpTo to actually
 * move the camera - this only computes the zoom to move it to).
 *
 * With `center` fixed, the viewport has to reach the farthest point on each
 * axis in both directions, so the span on an axis is twice the largest
 * offset from `center`. Zoom is clamped to POI_MAP_MIN_ZOOM/MAX_ZOOM, same as
 * zoomForRangeNm. When no point is offset from `center` on either axis (none
 * given, or all coincident with it) there is nothing to fit against and the
 * result is POI_MAP_MAX_ZOOM. Callers that also want a floor (never zoom in
 * tighter than some minimum range) apply that themselves, e.g.
 * `Math.min(fitCameraAroundPoint(...).zoom, zoomForRangeNm(...))`.
 */
export function fitCameraAroundPoint(
  center: MapPoint,
  points: MapPoint[],
  widthPx: number,
  heightPx: number,
  paddingPx: number,
): { center: MapPoint; zoom: number } {
  const cx = mercatorX(center.lon)
  const cy = mercatorY(center.lat)
  const spanX = 2 * Math.max(0, ...points.map((p) => Math.abs(mercatorX(p.lon) - cx)))
  const spanY = 2 * Math.max(0, ...points.map((p) => Math.abs(mercatorY(p.lat) - cy)))

  const availableWidthPx = Math.max(1, widthPx - 2 * paddingPx)
  const availableHeightPx = Math.max(1, heightPx - 2 * paddingPx)

  const worldSizeCandidates: number[] = []
  if (spanX > 0) worldSizeCandidates.push(availableWidthPx / spanX)
  if (spanY > 0) worldSizeCandidates.push(availableHeightPx / spanY)

  if (worldSizeCandidates.length === 0) {
    return { center, zoom: POI_MAP_MAX_ZOOM }
  }

  const worldSizePx = Math.min(...worldSizeCandidates)
  const zoom = Math.log2(worldSizePx / MERCATOR_TILE_SIZE_PX)
  const clampedZoom = Number.isFinite(zoom)
    ? Math.max(POI_MAP_MIN_ZOOM, Math.min(POI_MAP_MAX_ZOOM, zoom))
    : POI_MAP_MIN_ZOOM

  return { center, zoom: clampedZoom }
}

/**
 * The camera that shows `place` as close as `targetZoom` allows while keeping
 * `boat` in a `widthPx` x `heightPx` viewport, both inside `paddingPx` of
 * clearance. The place stays centred while the boat fits around it; once it
 * doesn't, the camera pans off the place toward the boat, just far enough to
 * bring the boat inside the padding edge, rather than zooming out with the
 * place still centred. That holds the view about a level tighter for a far-off
 * place. A tilted view magnifies its near half, so `headroomZoom` comes off
 * both the zoom (never tighter than both points fit, less the headroom) and
 * the box the boat is panned into (the padded box shrunk by the same factor);
 * otherwise the pan would put the boat on the edge whatever the zoom. The
 * zoom never goes wider than `minZoom`.
 * Same Web Mercator projection as fitCameraAroundPoint.
 */
export function framePlaceWithBoat(
  place: MapPoint,
  boat: MapPoint,
  widthPx: number,
  heightPx: number,
  paddingPx: number,
  targetZoom: number,
  headroomZoom: number,
  minZoom: number,
): { center: MapPoint; zoom: number } {
  const offsetX = mercatorX(boat.lon) - mercatorX(place.lon)
  const offsetY = mercatorY(boat.lat) - mercatorY(place.lat)
  const availableWidthPx = Math.max(1, widthPx - 2 * paddingPx)
  const availableHeightPx = Math.max(1, heightPx - 2 * paddingPx)

  const worldSizeCandidates: number[] = []
  if (offsetX !== 0) worldSizeCandidates.push(availableWidthPx / Math.abs(offsetX))
  if (offsetY !== 0) worldSizeCandidates.push(availableHeightPx / Math.abs(offsetY))
  const bothFitZoom = worldSizeCandidates.length === 0
    ? Infinity
    : Math.log2(Math.min(...worldSizeCandidates) / MERCATOR_TILE_SIZE_PX)
  const zoom = Math.max(minZoom, Math.min(targetZoom, bothFitZoom - headroomZoom))

  // Pan only by however far the boat would sit past the headroom-shrunk box.
  const worldSizePx = MERCATOR_TILE_SIZE_PX * 2 ** zoom
  const boxScale = 2 ** -headroomZoom
  const pan = (offset: number, halfPx: number) =>
    Math.sign(offset) * Math.max(0, Math.abs(offset) * worldSizePx - halfPx) / worldSizePx
  const cx = mercatorX(place.lon) + pan(offsetX, (availableWidthPx / 2) * boxScale)
  const cy = mercatorY(place.lat) + pan(offsetY, (availableHeightPx / 2) * boxScale)
  if (cx === mercatorX(place.lon) && cy === mercatorY(place.lat)) return { center: place, zoom }
  return {
    center: { lat: (Math.atan(Math.sinh(Math.PI * (1 - 2 * cy))) * 180) / Math.PI, lon: cx * 360 - 180 },
    zoom,
  }
}

/**
 * The top `n` features for the ranked list, one per name. The server already
 * sorts by distance ascending (decorateAndRankPOIFeatures in
 * backend/poi_providers.go), so this only needs to fold near-duplicate names
 * the server's own dedupe missed (a marina and its fuel dock sharing a
 * name, for instance) — an unnamed feature is never folded against anything,
 * matching the server's own dedupe rule.
 */
export function topPoi<T extends { name: string }>(features: readonly T[], n = 5): T[] {
  const seen = new Set<string>()
  const result: T[] = []
  for (const feature of features) {
    const key = feature.name.trim().toLowerCase()
    if (key !== '') {
      if (seen.has(key)) continue
      seen.add(key)
    }
    result.push(feature)
    if (result.length >= n) break
  }
  return result
}

const COMPASS_POINTS = [
  'N', 'NNE', 'NE', 'ENE', 'E', 'ESE', 'SE', 'SSE',
  'S', 'SSW', 'SW', 'WSW', 'W', 'WNW', 'NW', 'NNW',
]

/** Three-figure compass bearing plus point, e.g. "045° NE". */
export function formatBearing(deg: number): string {
  const normalized = ((deg % 360) + 360) % 360
  const point = COMPASS_POINTS[Math.round(normalized / 22.5) % 16]
  return `${String(Math.round(normalized)).padStart(3, '0')}° ${point}`
}
