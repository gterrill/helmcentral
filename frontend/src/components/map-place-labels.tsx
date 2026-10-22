import { useMemo } from 'react'
import { Layer } from 'react-map-gl/maplibre'
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
const PLACE_FONT = ['Montserrat Medium', 'Open Sans Regular', 'Noto Sans Regular', 'Noto Sans CJK JP Regular']
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

interface LabelPaint {
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

function useLabelPaint(isDarkTheme: boolean, overImagery: boolean): { land: LabelPaint; water: LabelPaint } {
  // Memoised so react-map-gl's updateLayer deep-compare (it walks every
  // paint key on each render to decide whether setPaintProperty is needed)
  // is comparing the same object reference across renders that don't
  // actually change theme or imagery state, not a fresh object every time.
  return useMemo(() => {
    if (overImagery) return { land: HYBRID_PAINT, water: HYBRID_PAINT }
    return isDarkTheme
      ? { land: DARK_LAND_PAINT, water: DARK_WATER_PAINT }
      : { land: LIGHT_LAND_PAINT, water: LIGHT_WATER_PAINT }
  }, [isDarkTheme, overImagery])
}

export interface MapPlaceLabelsProps {
  isDarkTheme: boolean
  /** True when a raster satellite layer is showing through the basemap - see anchor-watch-map's showImageryLayer. */
  overImagery: boolean
}

/**
 * Symbol layers that draw the place names Carto's own style never draws:
 * bays and straits, islands, marina POIs, peaks, and a top-up of
 * town/village names past Carto's own maxzoom cutoffs. All of it is already
 * sitting in the vector tiles the map downloads for the basemap - see
 * docs/adr/0066-basemap-place-name-labels.md for the reasoning against taking
 * on a second tile source (the tile schema it describes has since moved to
 * Protomaps' layers and attributes, docs/adr/0201).
 *
 * No beforeId on any of these. Both callers mount this component
 * unconditionally, at the top of their layer stack, before any conditional
 * overlay (satellite raster, sat chart, OpenSeaMap seamark) - so these
 * layers land above whatever raster is pinned beforeId="raster-overlay-
 * anchor" / "alarm-circle-fill" in each map, and below the route line /
 * GSHHG coastline / alarm circle, which mount with no beforeId of their own
 * and so draw on top of everything already in the stack. Text under
 * safety-critical geometry, above raster imagery, is the ordering this
 * component depends on its callers to preserve.
 */
export function MapPlaceLabels({ isDarkTheme, overImagery }: MapPlaceLabelsProps) {
  const paint = useLabelPaint(isDarkTheme, overImagery)

  return (
    <>
      {/* Crab Bay, Sydney Cove, Botany Bay - `water` layer Point features of
          kind bay. Carto's own watername_* layers draw ocean, sea and lake
          kinds only, so these never draw without this layer. */}
      <Layer
        id="place-names-water"
        type="symbol"
        source={BASE_VECTOR_SOURCE_ID}
        source-layer="water"
        minzoom={9}
        filter={WATER_FILTER}
        layout={{
          'text-field': TEXT_FIELD,
          'text-font': WATER_FONT,
          'text-size': WATER_TEXT_SIZE,
          'text-max-width': 8,
          'text-padding': 2,
          'text-anchor': 'center',
        }}
        paint={paint.water}
      />

      {/* Rangitoto Channel, Motukorea Channel - straits arrive as named
          LineStrings in `water`, so the label follows the line. */}
      <Layer
        id="place-names-strait"
        type="symbol"
        source={BASE_VECTOR_SOURCE_ID}
        source-layer="water"
        minzoom={9}
        filter={STRAIT_FILTER}
        layout={{
          'symbol-placement': 'line',
          'text-field': TEXT_FIELD,
          'text-font': WATER_FONT,
          'text-size': WATER_TEXT_SIZE,
          'text-max-width': 8,
          'text-padding': 2,
        }}
        paint={paint.water}
      />

      {/* Whitsunday, Hook, Hayman, Cid, Dent, Hamilton Island - Point
          features of kind island in the `earth` layer. Carto's own style has
          no layer that labels them. */}
      <Layer
        id="place-names-island"
        type="symbol"
        source={BASE_VECTOR_SOURCE_ID}
        source-layer="earth"
        minzoom={9}
        filter={ISLAND_FILTER}
        layout={{
          'text-field': TEXT_FIELD,
          'text-font': PLACE_FONT,
          'text-size': LAND_TEXT_SIZE,
          'text-max-width': 8,
          'text-padding': 2,
          'text-anchor': 'center',
        }}
        paint={paint.land}
      />

      {/* Daydream Island, Henning Island, Ross Islet, Surprise Rock - the
          smaller islands and rocks, which the tiles carry only as `pois` of
          kind coastline, from z11. */}
      <Layer
        id="place-names-islet"
        type="symbol"
        source={BASE_VECTOR_SOURCE_ID}
        source-layer="pois"
        minzoom={11}
        filter={ISLET_FILTER}
        layout={{
          'text-field': TEXT_FIELD,
          'text-font': PLACE_FONT,
          'text-size': LAND_TEXT_SIZE,
          'text-max-width': 8,
          'text-padding': 2,
          'text-anchor': 'center',
        }}
        paint={paint.land}
      />

      {/* Balmain Marina, Sydney Superyacht Marina - `pois` of kind marina.
          No Carto layer references that kind; poi_stadium and poi_park are
          the only pois-layer label layers in either style. The tiles carry
          marinas from z12 up. */}
      <Layer
        id="place-names-harbor"
        type="symbol"
        source={BASE_VECTOR_SOURCE_ID}
        source-layer="pois"
        minzoom={12}
        filter={HARBOR_FILTER}
        layout={{
          'text-field': TEXT_FIELD,
          'text-font': PLACE_FONT,
          'text-size': LAND_TEXT_SIZE,
          'text-max-width': 8,
          'text-padding': 2,
          'text-anchor': 'center',
        }}
        paint={paint.land}
      />

      {/* Whitsunday Peak, Bolton Hill, Mount Robinson - `pois` of kind peak
          or volcano. No layer in either Carto style references them. */}
      <Layer
        id="place-names-peak"
        type="symbol"
        source={BASE_VECTOR_SOURCE_ID}
        source-layer="pois"
        minzoom={12}
        filter={PEAK_FILTER}
        layout={{
          'text-field': TEXT_FIELD,
          'text-font': PLACE_FONT,
          'text-size': LAND_TEXT_SIZE,
          'text-max-width': 8,
          'text-padding': 2,
          'text-anchor': 'center',
        }}
        paint={paint.land}
      />

      {/* Airlie Beach, Cannonvale, and every other city/town/village/hamlet/
          suburb - fills the gap Carto's own maxzoom cutoffs leave behind
          (place_town: 14, place_city_r5/r6: 15, place_villages/hamlet/
          suburbs: 16). In the zoom band where Carto still draws these,
          MapLibre's cross-layer symbol placement runs in style order and
          drops the later, higher layer on collision - same text at the same
          anchor point, so this duplicates nothing, it only shows up once
          Carto's own layer has already stopped drawing. No zoom-window
          arithmetic needed to avoid double labels. */}
      <Layer
        id="place-names-town-topup"
        type="symbol"
        source={BASE_VECTOR_SOURCE_ID}
        source-layer="places"
        minzoom={14}
        filter={TOWN_TOPUP_FILTER}
        layout={{
          'text-field': TEXT_FIELD,
          'text-font': PLACE_FONT,
          'text-size': LAND_TEXT_SIZE,
          'text-max-width': 8,
          'text-padding': 2,
          'text-anchor': 'center',
        }}
        paint={paint.land}
      />
    </>
  )
}

/**
 * Fail-fast guard, per the repo's fallback policy (no masking of upstream
 * problems - surface them explicitly). @vis.gl/react-maplibre's createLayer
 * silently skips addLayer when map.getSource(props.source) comes back
 * falsy, so if the Carto style ever changes its source id, or a future
 * change swaps in a differently-shaped style document, every <Layer> above
 * just never mounts and nothing else says so - the map looks fine, it is
 * only missing every place name. Both callers run this once per style load
 * from their existing style-load path rather than leave that quiet.
 */
export function warnIfBaseVectorSourceMissing(map: { getSource: (id: string) => unknown }): void {
  if (!map.getSource(BASE_VECTOR_SOURCE_ID)) {
    console.error(
      `[map-place-labels] base vector source "${BASE_VECTOR_SOURCE_ID}" missing from the loaded style - place-name labels will not render`,
    )
  }
}
