import { useMemo } from 'react'
import { Layer, Source } from 'react-map-gl/maplibre'

import { MateCardMap, POINT_LABEL_LAYOUT, RequireWebGL2, ROUND_LINE_LAYOUT, useThemeColours } from '@/components/mate-card-map'
import type { PlanLegMapProps } from '@/components/plan-leg-map'
import { themeColour } from '@/lib/map-theme'
import { pointsBounds } from '@/lib/marina-card'

export default function PlanLegMapImpl(props: PlanLegMapProps) {
  return (
    <RequireWebGL2 name="plan-leg">
      <LegMap {...props} />
    </RequireWebGL2>
  )
}

function LegMap({ from, to, leg }: PlanLegMapProps) {
  const { background, labelPaint, primary } = useThemeColours((base) => ({ ...base, primary: themeColour('--primary') }))

  const marks = useMemo(
    () => [
      { label: from.name, lat: from.lat, lon: from.lon },
      ...from.vias.map((v) => ({ label: v.name, lat: v.lat, lon: v.lon })),
      { label: to.name, lat: to.lat, lon: to.lon },
    ],
    [from, to],
  )
  const bounds = useMemo(() => pointsBounds([...leg.route, ...marks.map((m): [number, number] => [m.lat, m.lon])]), [leg.route, marks])
  const line = useMemo(
    () => ({
      type: 'FeatureCollection' as const,
      features: [{ type: 'Feature' as const, properties: {}, geometry: { type: 'LineString' as const, coordinates: leg.route.map(([lat, lon]) => [lon, lat]) } }],
    }),
    [leg.route],
  )
  const points = useMemo(
    () => ({
      type: 'FeatureCollection' as const,
      features: marks.map((m) => ({ type: 'Feature' as const, properties: { label: m.label }, geometry: { type: 'Point' as const, coordinates: [m.lon, m.lat] } })),
    }),
    [marks],
  )
  if (!bounds) throw new Error('the leg map was given a leg with nothing to frame')

  return (
    <MateCardMap title={`${from.name} to ${to.name}`} bounds={bounds} beforeId="plan-leg-casing">
      <Source id="plan-leg" type="geojson" data={line}>
        <Layer id="plan-leg-casing" type="line" layout={ROUND_LINE_LAYOUT} paint={{ 'line-color': background, 'line-width': 5, 'line-opacity': 0.7 }} />
        <Layer id="plan-leg-line" type="line" layout={ROUND_LINE_LAYOUT} paint={{ 'line-color': primary, 'line-width': 2.5 }} />
      </Source>
      <Source id="plan-leg-marks" type="geojson" data={points}>
        <Layer id="plan-leg-dot" type="circle" paint={{ 'circle-radius': 5, 'circle-color': primary, 'circle-stroke-color': background, 'circle-stroke-width': 1.5 }} />
        <Layer id="plan-leg-label" type="symbol" layout={POINT_LABEL_LAYOUT} paint={labelPaint} />
      </Source>
    </MateCardMap>
  )
}
