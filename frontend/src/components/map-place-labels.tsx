import { useMemo } from 'react'
import { Layer } from 'react-map-gl/maplibre'
import type { ExpressionSpecification, FilterSpecification } from 'maplibre-gl'

/**
 * The one vector source both Carto styles (Positron light, Dark Matter dark)
 * declare. Both maps already load one of these styles as their base, so by
 * the time these <Layer> elements try to mount, a source with this id exists
 * in the loaded style. There is deliberately no <Source> wrapper in this
 * file - only layers attached to a source somebody else already created.
 * @vis.gl/react-maplibre's createLayer guards on map.getSource(props.source)
 * before calling addLayer, so a layer here is a no-op rather than a crash if
 * that source is ever missing (see warnIfBaseVectorSourceMissing below for
 * the fail-fast check both maps run against exactly that condition).
 */
export const BASE_VECTOR_SOURCE_ID = 'carto'

/**
 * Font stacks copied verbatim from the Carto style documents' own
 * place/water label layers (positron-gl-style and dark-matter-gl-style
 * share these). Reusing the exact stack means the glyph PBFs these labels
 * need are ones the map fetches already for the basemap's own label layers
 * (place_town, watername_ocean, etc.) - a stack the style has never
 * referenced would mean a fresh glyph request the first time one of these
 * layers paints.
 */
const PLACE_FONT = ['Montserrat Medium', 'Open Sans Bold', 'Noto Sans Regular', 'HanWangHeiLight Regular', 'NanumBarunGothic Regular']
const WATER_FONT = ['Montserrat Regular Italic', 'Open Sans Italic', 'Noto Sans Regular', 'HanWangHeiLight Regular', 'NanumBarunGothic Regular']

export const PLACE_LABEL_LAYER_IDS = [
  'place-names-water',
  'place-names-island',
  'place-names-harbor',
  'place-names-peak',
  'place-names-town-topup',
] as const

// Same text everywhere: the tile's local name where the source has one,
// falling back to the English rendering. Carto's own label layers use this
// exact coalesce.
const TEXT_FIELD: ExpressionSpecification = ['coalesce', ['get', 'name'], ['get', 'name_en']]

// Land labels run one step larger than water labels at every stop, matching
// place_town's stops vs. watername_lake's - water names read as secondary
// information next to an island or town name at the same zoom.
const LAND_TEXT_SIZE: ExpressionSpecification = ['interpolate', ['linear'], ['zoom'], 10, 10, 14, 12, 17, 13]
const WATER_TEXT_SIZE: ExpressionSpecification = ['interpolate', ['linear'], ['zoom'], 10, 9, 14, 11, 17, 12]

const WATER_FILTER: FilterSpecification = [
  'all',
  ['has', 'name'],
  ['==', ['geometry-type'], 'Point'],
  ['in', ['get', 'class'], ['literal', ['bay', 'strait']]],
]

const ISLAND_FILTER: FilterSpecification = ['all', ['has', 'name'], ['==', ['get', 'class'], 'island']]

const HARBOR_FILTER: FilterSpecification = ['all', ['has', 'name'], ['==', ['get', 'class'], 'harbor']]

const PEAK_FILTER: FilterSpecification = [
  'all',
  ['has', 'name'],
  ['in', ['get', 'class'], ['literal', ['peak', 'volcano']]],
]

const TOWN_TOPUP_FILTER: FilterSpecification = [
  'all',
  ['has', 'name'],
  ['in', ['get', 'class'], ['literal', ['city', 'town', 'village', 'hamlet', 'suburb']]],
]

interface LabelPaint {
  'text-color': string
  'text-halo-color': string
  'text-halo-width': number
}

// The hybrid-satellite treatment: white text on a heavy black halo, matching
// HYBRID_LABEL_TEXT_COLOR/HALO_COLOR/HALO_WIDTH in route-planner-map.tsx
// (lines 72-74), which applies the same values to the base style's own
// label layers when satellite imagery shows through. These labels need to
// read the same way against the same photographic imagery.
const HYBRID_PAINT: LabelPaint = { 'text-color': '#ffffff', 'text-halo-color': '#000000', 'text-halo-width': 1.5 }

// Light and dark values lifted from the corresponding Carto style layers
// (place_town / place_city_r5 for land, watername_lake / watername_ocean for
// water, in each of positron-gl-style.json and dark-matter-gl-style.json) so
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
  /** True when a raster satellite layer is showing through the basemap - see route-planner-map's showHybridSatellite / anchor-watch-map's showImageryLayer. */
  overImagery: boolean
}

/**
 * Symbol layers that draw the place names Carto's own style never draws:
 * bays/inlets/channels, islands, marina POIs, peaks, and a top-up of
 * town/village names past Carto's own maxzoom cutoffs. All of it is already
 * sitting in the vector tiles the map downloads for the basemap - see
 * docs/adr/0066-basemap-place-name-labels.md for the tile-level evidence and
 * the reasoning against taking on a second tile source.
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
      {/* Cid Harbour, Nara Inlet, Macona Inlet, Hayman Channel, The Narrows -
          water_name's Point-geometry bay/strait class. Carto's own
          watername_* layers filter to ocean/sea/lake only, so these never
          draw without this layer. */}
      <Layer
        id="place-names-water"
        type="symbol"
        source={BASE_VECTOR_SOURCE_ID}
        source-layer="water_name"
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

      {/* Whitsunday, Hook, Border, Cid, Dent, Hamilton Island. Carto only
          draws place class island via place_city_dot_z7, clamped minzoom
          7/maxzoom 8 - a bare dot with no label outside one narrow, mostly
          unreachable zoom band. */}
      <Layer
        id="place-names-island"
        type="symbol"
        source={BASE_VECTOR_SOURCE_ID}
        source-layer="place"
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

      {/* Coral Sea Marina, Port of Airlie Marina, Whitsunday Sailing Club.
          No Carto layer references poi class harbor at all - poi_stadium and
          poi_park are the only poi-source-layer layers in either style.
          minzoom 14 because OpenMapTiles only generates poi features from
          z14 up; setting it lower would just filter nothing in and cost a
          layer evaluation every frame below that zoom. */}
      <Layer
        id="place-names-harbor"
        type="symbol"
        source={BASE_VECTOR_SOURCE_ID}
        source-layer="poi"
        minzoom={14}
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

      {/* Whitsunday Peak, Hook Peak, Bolton Hill. Zero layers in either
          Carto style reference mountain_peak - it is decoded straight off
          the wire, unfiltered by any existing style rule. */}
      <Layer
        id="place-names-peak"
        type="symbol"
        source={BASE_VECTOR_SOURCE_ID}
        source-layer="mountain_peak"
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

      {/* Airlie Beach, Cannonvale, and every other town/village/hamlet/
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
        source-layer="place"
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
