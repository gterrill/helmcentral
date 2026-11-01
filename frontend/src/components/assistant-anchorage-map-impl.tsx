import type { ExpressionSpecification, SymbolLayerSpecification } from 'maplibre-gl'
import { useMemo } from 'react'
import { Layer, Source } from 'react-map-gl/maplibre'

import { MateCardMap, POINT_LABEL_LAYOUT, RequireWebGL2, ROUND_LINE_LAYOUT, useThemeColours } from '@/components/mate-card-map'
import { useAppConfig } from '@/hooks/use-app-config'
import { anchorageFeatures, anchorageFrame, GRADE_TOKEN } from '@/lib/anchorage-card'
import { themeColour } from '@/lib/map-theme'
import type { AnchorageCard } from '@/lib/mate-cards'

const IS_POINT: ExpressionSpecification = ['==', ['geometry-type'], 'Point']
const MOORING_LABEL_LAYOUT: SymbolLayerSpecification['layout'] = { ...POINT_LABEL_LAYOUT, 'text-optional': true }

/**
 * The anchorage map on a Mate card (ADR 0244), on the base it shares with the
 * marina map. On top go the 36 fetch rays coloured by shelter grade, the
 * public moorings labelled with their vessel size limit, the swing circle
 * when a depth made one, restricted areas
 * filled in the alert colour, and anchorage and seabed areas outlined. The
 * shore the rays stop at is not drawn: the rays say where it is.
 */
export default function AssistantAnchorageMapImpl({ card }: { card: AnchorageCard }) {
  return (
    <RequireWebGL2 name="anchorage">
      <AnchorageMap card={card} />
    </RequireWebGL2>
  )
}

function AnchorageMap({ card }: { card: AnchorageCard }) {
  const { ui } = useAppConfig()
  const features = useMemo(() => anchorageFeatures(card, ui.distanceUnits), [card, ui.distanceUnits])
  const bounds = useMemo(() => anchorageFrame(card), [card])

  const { primary, muted, foreground, background, rayColour, restrictedColour, labelPaint } = useThemeColours((base) => {
    const alert = themeColour(GRADE_TOKEN.exposed)
    const rayColour: ExpressionSpecification = [
      'match',
      ['get', 'grade'],
      'sheltered', themeColour(GRADE_TOKEN.sheltered),
      'partial', themeColour(GRADE_TOKEN.partial),
      'exposed', alert,
      themeColour(GRADE_TOKEN.reef),
    ]
    const restrictedColour: ExpressionSpecification = ['case', ['get', 'prohibits'], alert, themeColour('--gauge-primary')]
    return {
      ...base,
      primary: themeColour('--primary'),
      muted: themeColour('--muted-foreground'),
      foreground: themeColour('--foreground'),
      rayColour,
      restrictedColour,
    }
  })

  return (
    <MateCardMap bounds={bounds} beforeId="anchorage-restricted-fill">
      <Source id="anchorage-restricted" type="geojson" data={features.restrictedAreas}>
        <Layer id="anchorage-restricted-fill" type="fill" paint={{ 'fill-color': restrictedColour, 'fill-opacity': 0.28 }} />
        <Layer id="anchorage-restricted-edge" type="line" paint={{ 'line-color': restrictedColour, 'line-width': 2 }} />
      </Source>
      <Source id="anchorage-restricted-other" type="geojson" data={features.restrictedLines}>
        <Layer id="anchorage-restricted-line" type="line" filter={['!', IS_POINT]} layout={ROUND_LINE_LAYOUT} paint={{ 'line-color': restrictedColour, 'line-width': 2.5 }} />
        <Layer id="anchorage-restricted-point" type="circle" filter={IS_POINT} paint={{ 'circle-radius': 6, 'circle-color': restrictedColour, 'circle-stroke-color': background, 'circle-stroke-width': 1.5 }} />
      </Source>

      <Source id="anchorage-areas" type="geojson" data={features.anchorageAreas}>
        <Layer id="anchorage-areas-line" type="line" filter={['!', IS_POINT]} layout={ROUND_LINE_LAYOUT} paint={{ 'line-color': primary, 'line-width': 2 }} />
        <Layer id="anchorage-areas-point" type="circle" filter={IS_POINT} paint={{ 'circle-radius': 5, 'circle-color': background, 'circle-stroke-color': primary, 'circle-stroke-width': 2 }} />
      </Source>
      <Source id="anchorage-seabed" type="geojson" data={features.seabedAreas}>
        <Layer id="anchorage-seabed-line" type="line" filter={['!', IS_POINT]} layout={{ 'line-join': 'round' }} paint={{ 'line-color': muted, 'line-width': 2, 'line-dasharray': [2, 2] }} />
        <Layer id="anchorage-seabed-point" type="circle" filter={IS_POINT} paint={{ 'circle-radius': 4, 'circle-color': background, 'circle-stroke-color': muted, 'circle-stroke-width': 2 }} />
      </Source>

      <Source id="anchorage-swing" type="geojson" data={features.swing}>
        <Layer id="anchorage-swing-fill" type="fill" paint={{ 'fill-color': foreground, 'fill-opacity': 0.08 }} />
        <Layer id="anchorage-swing-edge" type="line" paint={{ 'line-color': foreground, 'line-width': 1.5, 'line-dasharray': [3, 2] }} />
      </Source>

      <Source id="anchorage-rays" type="geojson" data={features.rays}>
        <Layer id="anchorage-rays-casing" type="line" layout={ROUND_LINE_LAYOUT} paint={{ 'line-color': background, 'line-width': 3.5, 'line-opacity': 0.5 }} />
        <Layer id="anchorage-rays-line" type="line" layout={ROUND_LINE_LAYOUT} paint={{ 'line-color': rayColour, 'line-width': 1.5, 'line-opacity': 0.9 }} />
      </Source>

      <Source id="anchorage-moorings" type="geojson" data={features.moorings}>
        <Layer id="anchorage-mooring-dot" type="circle" paint={{ 'circle-radius': 4, 'circle-color': primary, 'circle-stroke-color': background, 'circle-stroke-width': 1.5 }} />
        <Layer id="anchorage-mooring-label" type="symbol" layout={MOORING_LABEL_LAYOUT} paint={labelPaint} />
      </Source>

      <Source id="anchorage-point" type="geojson" data={features.anchorage}>
        <Layer id="anchorage-point-dot" type="circle" paint={{ 'circle-radius': 6, 'circle-color': foreground, 'circle-stroke-color': background, 'circle-stroke-width': 2 }} />
      </Source>
    </MateCardMap>
  )
}
