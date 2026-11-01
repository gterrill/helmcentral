// The place-name layers drawn over the basemap, as plain layer specs, so the
// live map (components/map-place-labels.tsx) and a map made for a video draw
// the same names. Nothing here knows about React.

import type { ExpressionSpecification, FilterSpecification } from 'maplibre-gl'

/**
 * The one vector source both Carto styles (Positron light, Dark Matter dark)
 * declare. Carto's Protomaps-schema styles name it "protomaps"; the backend
 * passes the id through from the upstream style untouched (docs/adr/0201). Both maps already load one of these styles as their base, so by
 * the time these <Layer> elements try to mount, a source with this id exists
 * in the loaded style. There is deliberately no <Source> wrapper in this
 * file - only layers attached to a source somebody else already created.
 * @vis.gl/react-maplibre's createLayer guards on map.getSource(props.source)
 * before calling addLayer, so a layer here is a no-op rather than a crash if
 * that source is ever missing (see warnIfBaseVectorSourceMissing below for
 * the fail-fast check both maps run against exactly that condition).
 */
export const BASE_VECTOR_SOURCE_ID = 'protomaps'

/**
 * Font stacks copied verbatim from the Carto style documents' own
 * place/water label layers (place_hamlet and watername_lake in
 * positron-gl-style and dark-matter-gl-style, which share them). Reusing the
 * exact stack means the glyph PBFs these labels need are ones the map
 * fetches already for the basemap's own label layers - a stack the style has
 * never referenced would mean a fresh glyph request the first time one of
 * these layers paints.
 */
export const PLACE_FONT = ['Montserrat Medium', 'Open Sans Regular', 'Noto Sans Regular', 'Noto Sans CJK JP Regular']
const WATER_FONT = ['Montserrat Regular Italic', 'Open Sans Italic', 'Noto Sans Regular', 'Noto Sans CJK JP Regular']

export const PLACE_LABEL_LAYER_IDS = [
  'place-names-water',
  'place-names-strait',
  'place-names-island',
  'place-names-islet',
  'place-names-harbor',
  'place-names-peak',
  'place-names-town-topup',
] as const

// Same text everywhere: the tile's local name where the source has one,
// falling back to the English rendering (the Protomaps schema carries it as
// "name:en").
const TEXT_FIELD: ExpressionSpecification = ['coalesce', ['get', 'name'], ['get', 'name:en']]

// Land labels run one step larger than water labels at every stop, matching
// place_town's stops vs. watername_lake's - water names read as secondary
// information next to an island or town name at the same zoom.
const LAND_TEXT_SIZE: ExpressionSpecification = ['interpolate', ['linear'], ['zoom'], 10, 10, 14, 12, 17, 13]
const WATER_TEXT_SIZE: ExpressionSpecification = ['interpolate', ['linear'], ['zoom'], 10, 9, 14, 11, 17, 12]

// Protomaps basemap schema (see docs/adr/0201): features carry `kind` (and
// sometimes `kind_detail`) instead of OpenMapTiles' `class`. Each filter
// below was checked against real tiles around the Whitsundays, Sydney and
// Auckland.

// Bays are named Point features in the `water` layer. Carto's own
// watername_* layers draw only ocean, sea and lake/water kinds, so bays never
// draw without this layer.
const WATER_FILTER: FilterSpecification = [
  'all',
  ['has', 'name'],
  ['==', ['geometry-type'], 'Point'],
  ['==', ['get', 'kind'], 'bay'],
]

// Straits and channels are named LineStrings in the `water` layer, so they
// need line placement rather than a point anchor.
const STRAIT_FILTER: FilterSpecification = [
  'all',
  ['has', 'name'],
  ['==', ['geometry-type'], 'LineString'],
  ['==', ['get', 'kind'], 'strait'],
]

// Island names are Point features of kind island in the `earth` layer.
const ISLAND_FILTER: FilterSpecification = [
  'all',
  ['has', 'name'],
  ['==', ['geometry-type'], 'Point'],
  ['==', ['get', 'kind'], 'island'],
]

// Smaller islands (Daydream, Henning, Mid Molle, Ross Islet, Surprise Rock)
// have no `earth` point at all: they are `pois` of kind coastline. Checked
// over the Whitsundays and Magnetic Island, `earth` carries only kind island
// (85 named points), and 39 of 63 named coastline pois had no `earth`
// counterpart.
const ISLET_FILTER: FilterSpecification = ['all', ['has', 'name'], ['==', ['get', 'kind'], 'coastline']]

// Marinas are `pois` of kind marina (OpenMapTiles called this class harbor).
const HARBOR_FILTER: FilterSpecification = ['all', ['has', 'name'], ['==', ['get', 'kind'], 'marina']]

const PEAK_FILTER: FilterSpecification = [
  'all',
  ['has', 'name'],
  ['in', ['get', 'kind'], ['literal', ['peak', 'volcano']]],
]

const TOWN_TOPUP_FILTER: FilterSpecification = [
  'all',
  ['has', 'name'],
  [
    'any',
    ['all', ['==', ['get', 'kind'], 'locality'], ['in', ['get', 'kind_detail'], ['literal', ['city', 'town', 'village', 'hamlet']]]],
    ['all', ['==', ['get', 'kind'], 'neighbourhood'], ['==', ['get', 'kind_detail'], 'suburb']],
  ],
]

export interface LabelPaint {
  'text-color': string
  'text-halo-color': string
  'text-halo-width': number
}

// The hybrid-satellite treatment: white text on a heavy black halo, so labels
// stay legible against photographic imagery.
const HYBRID_PAINT: LabelPaint = { 'text-color': '#ffffff', 'text-halo-color': '#000000', 'text-halo-width': 1.5 }

// Light and dark values lifted from the corresponding Carto style layers
// (place_town for land, watername_lake for water, in each of
// positron-gl-style.json and dark-matter-gl-style.json) so
// these labels read as native basemap type rather than an overlay bolted on
// top of it.
const LIGHT_LAND_PAINT: LabelPaint = { 'text-color': '#697b89', 'text-halo-color': 'rgba(255,255,255,0.6)', 'text-halo-width': 1 }
const LIGHT_WATER_PAINT: LabelPaint = { 'text-color': '#7a96a0', 'text-halo-color': 'rgba(255,255,255,0.6)', 'text-halo-width': 1 }
const DARK_LAND_PAINT: LabelPaint = { 'text-color': 'rgba(204,208,228,1)', 'text-halo-color': '#181818', 'text-halo-width': 1 }
const DARK_WATER_PAINT: LabelPaint = { 'text-color': 'rgba(155,155,155,1)', 'text-halo-color': '#181818', 'text-halo-width': 1 }


/** The paint for land and water names: white on black over imagery, otherwise the theme's own. */
export function placeLabelPaint(isDarkTheme: boolean, overImagery: boolean): { land: LabelPaint; water: LabelPaint } {
  if (overImagery) return { land: HYBRID_PAINT, water: HYBRID_PAINT }
  return isDarkTheme ? { land: DARK_LAND_PAINT, water: DARK_WATER_PAINT } : { land: LIGHT_LAND_PAINT, water: LIGHT_WATER_PAINT }
}

export interface PlaceLabelLayer {
  id: (typeof PLACE_LABEL_LAYER_IDS)[number]
  type: 'symbol'
  source: string
  'source-layer': string
  minzoom: number
  filter: FilterSpecification
  layout: Record<string, unknown>
  paint: LabelPaint
}

const POINT_LAYOUT = { 'text-field': TEXT_FIELD, 'text-font': PLACE_FONT, 'text-size': LAND_TEXT_SIZE, 'text-max-width': 8, 'text-padding': 2, 'text-anchor': 'center' }

/** The seven layers, bottom first. Bays, straits, islands, islets, marinas,
 * peaks, and a top-up of town names past the basemap's own cutoffs. */
export function placeLabelLayers(isDarkTheme: boolean, overImagery: boolean): PlaceLabelLayer[] {
  const paint = placeLabelPaint(isDarkTheme, overImagery)
  const layer = (id: PlaceLabelLayer['id'], sourceLayer: string, minzoom: number, filter: FilterSpecification, layout: Record<string, unknown>, p: LabelPaint): PlaceLabelLayer => (
    { id, type: 'symbol', source: BASE_VECTOR_SOURCE_ID, 'source-layer': sourceLayer, minzoom, filter, layout, paint: p }
  )
  return [
    // Crab Bay, Sydney Cove, Botany Bay: `water` Point features of kind bay.
    layer('place-names-water', 'water', 9, WATER_FILTER, { 'text-field': TEXT_FIELD, 'text-font': WATER_FONT, 'text-size': WATER_TEXT_SIZE, 'text-max-width': 8, 'text-padding': 2, 'text-anchor': 'center' }, paint.water),
    // Straits arrive as named LineStrings in `water`, so the label follows the line.
    layer('place-names-strait', 'water', 9, STRAIT_FILTER, { 'symbol-placement': 'line', 'text-field': TEXT_FIELD, 'text-font': WATER_FONT, 'text-size': WATER_TEXT_SIZE, 'text-max-width': 8, 'text-padding': 2 }, paint.water),
    // Point features of kind island in the `earth` layer.
    layer('place-names-island', 'earth', 9, ISLAND_FILTER, POINT_LAYOUT, paint.land),
    // The smaller islands and rocks, carried only as `pois` of kind coastline, from z11.
    layer('place-names-islet', 'pois', 11, ISLET_FILTER, POINT_LAYOUT, paint.land),
    // `pois` of kind marina, from z12.
    layer('place-names-harbor', 'pois', 12, HARBOR_FILTER, POINT_LAYOUT, paint.land),
    // `pois` of kind peak or volcano.
    layer('place-names-peak', 'pois', 12, PEAK_FILTER, POINT_LAYOUT, paint.land),
    // Every city, town, village, hamlet and suburb: fills the gap the basemap's own maxzoom cutoffs leave behind.
    layer('place-names-town-topup', 'places', 14, TOWN_TOPUP_FILTER, POINT_LAYOUT, paint.land),
  ]
}
