import { useMemo } from 'react'
import { Layer } from 'react-map-gl/maplibre'

import { BASE_VECTOR_SOURCE_ID, PLACE_FONT, PLACE_LABEL_LAYER_IDS, placeLabelLayers } from '@/lib/place-label-layers'

// The layer specs (and the reasoning behind each filter) live in
// lib/place-label-layers.ts, shared with the video export's plain map.
export { BASE_VECTOR_SOURCE_ID, PLACE_FONT, PLACE_LABEL_LAYER_IDS }

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
  // Memoised so react-map-gl's updateLayer deep-compare is comparing the same
  // objects across renders that don't change theme or imagery state.
  const layers = useMemo(() => placeLabelLayers(isDarkTheme, overImagery), [isDarkTheme, overImagery])
  return (
    <>
      {layers.map((l) => (
        <Layer key={l.id} {...l} />
      ))}
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
