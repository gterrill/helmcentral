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

// Web Mercator metres-per-pixel at zoom 0, equator: 2*pi*earthRadiusM / tileSizePx.
const WEB_MERCATOR_METERS_PER_PIXEL_AT_ZOOM_0 = 156543.03392
const METERS_PER_NM = 1852
export const POI_MAP_MIN_ZOOM = 8
export const POI_MAP_MAX_ZOOM = 16

/**
 * The zoom level whose visible span holds a circle of the given range,
 * centred at `lat`, inside a map `heightPx` tall — i.e. the range's full
 * diameter fits the tile's height. Clamped to [8, 16]: below 8 the vessel's
 * own position is barely a dot, and above 16 raster tiles this app draws
 * (OpenSeaMap, the Carto basemap) are already past their native resolution.
 */
export function zoomForRangeNm(rangeNm: number, lat: number, heightPx: number): number {
  if (!(rangeNm > 0) || !(heightPx > 0)) return POI_MAP_MIN_ZOOM
  const diameterM = rangeNm * METERS_PER_NM * 2
  const targetMetersPerPixel = diameterM / heightPx
  const latRad = (lat * Math.PI) / 180
  const zoom = Math.log2(
    (WEB_MERCATOR_METERS_PER_PIXEL_AT_ZOOM_0 * Math.cos(latRad)) / targetMetersPerPixel,
  )
  if (!Number.isFinite(zoom)) return POI_MAP_MIN_ZOOM
  return Math.max(POI_MAP_MIN_ZOOM, Math.min(POI_MAP_MAX_ZOOM, zoom))
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
