import { Store } from 'lucide-react'
import { useMemo, useState } from 'react'
import { Layer, Source } from 'react-map-gl/maplibre'

import { PLACE_FONT } from '@/components/map-place-labels'
import { MapToggleButton } from '@/components/map-toggle-button'
import { MateCardMap, POINT_LABEL_LAYOUT, RequireWebGL2, ROUND_LINE_LAYOUT, useThemeColours } from '@/components/mate-card-map'
import { marinaFeatures, marinaFrame } from '@/lib/marina-card'
import { themeColour } from '@/lib/map-theme'
import type { MarinaCard } from '@/lib/mate-cards'

/**
 * The marina map on a Mate card (ADR 0242). The base is shared with the
 * anchorage map: OpenSeaMap draws the lateral marks and the beacons, so this
 * map does not draw them again. On top go the marina's own outline, its
 * pontoons with their arm names, and a dot for each facility. Shops and
 * services ashore are off until the operator asks.
 */
export default function AssistantMarinaMapImpl({ card }: { card: MarinaCard }) {
  return (
    <RequireWebGL2 name="marina">
      <MarinaMap card={card} />
    </RequireWebGL2>
  )
}

function MarinaMap({ card }: { card: MarinaCard }) {
  const [showAshore, setShowAshore] = useState(false)

  const features = useMemo(() => marinaFeatures(card), [card])
  const bounds = useMemo(() => marinaFrame(card), [card])
  const { background, labelPaint, primary, outline, muted } = useThemeColours((base) => ({
    ...base,
    primary: themeColour('--primary'),
    outline: themeColour('--gauge-primary'),
    muted: themeColour('--muted-foreground'),
  }))

  if (!bounds) throw new Error('the marina map was given a card with nothing to frame')

  const hasAshore = card.geometry.ashore.length > 0

  return (
    <MateCardMap
      bounds={bounds}
      beforeId="marina-outline-casing"
      controls={hasAshore && <MapToggleButton icon={Store} label="Toggle shops and services ashore" on={showAshore} onToggle={() => setShowAshore((v) => !v)} />}
    >
      {/* The marina's outline: a thin stroke on a light casing, so it reads on imagery and on the chart. */}
      <Source id="marina-outline" type="geojson" data={features.outline}>
        <Layer id="marina-outline-casing" type="line" paint={{ 'line-color': background, 'line-width': 3.5, 'line-opacity': 0.7 }} />
        <Layer id="marina-outline-line" type="line" paint={{ 'line-color': outline, 'line-width': 1.5 }} />
      </Source>

      {/* Pontoons. A private one is dashed and unlabelled. */}
      <Source id="marina-pontoons" type="geojson" data={features.pontoons}>
        <Layer id="marina-pontoons-casing" type="line" layout={ROUND_LINE_LAYOUT} paint={{ 'line-color': background, 'line-width': 4.5, 'line-opacity': 0.7 }} />
        <Layer id="marina-pontoons-line" type="line" filter={['!', ['get', 'private']]} layout={ROUND_LINE_LAYOUT} paint={{ 'line-color': primary, 'line-width': 2.5 }} />
        <Layer
          id="marina-pontoons-private-line"
          type="line"
          filter={['get', 'private']}
          layout={{ 'line-join': 'round' }}
          paint={{ 'line-color': muted, 'line-width': 2, 'line-dasharray': [2, 2] }}
        />
      </Source>
      <Source id="marina-pontoon-labels" type="geojson" data={features.pontoonLabels}>
        <Layer
          id="marina-pontoon-label"
          type="symbol"
          layout={{
            'text-field': ['get', 'name'],
            'text-font': PLACE_FONT,
            'text-size': 12,
            'text-offset': [0, -1],
            'text-allow-overlap': false,
          }}
          paint={labelPaint}
        />
      </Source>

      <Source id="marina-facilities" type="geojson" data={features.facilities}>
        <Layer id="marina-facility-dot" type="circle" paint={{ 'circle-radius': 5, 'circle-color': primary, 'circle-stroke-color': background, 'circle-stroke-width': 1.5 }} />
        <Layer id="marina-facility-label" type="symbol" layout={POINT_LABEL_LAYOUT} paint={labelPaint} />
      </Source>

      {showAshore && (
        <Source id="marina-ashore" type="geojson" data={features.ashore}>
          <Layer id="marina-ashore-dot" type="circle" paint={{ 'circle-radius': 4, 'circle-color': muted, 'circle-stroke-color': background, 'circle-stroke-width': 1.5 }} />
          <Layer id="marina-ashore-label" type="symbol" layout={POINT_LABEL_LAYOUT} paint={labelPaint} />
        </Source>
      )}
    </MateCardMap>
  )
}
