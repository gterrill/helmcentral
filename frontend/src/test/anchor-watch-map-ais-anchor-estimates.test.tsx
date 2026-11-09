import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { AnchorWatchMap } from '@/components/anchor-watch-map'
import type { NearbyVessel } from '@/hooks/use-nearby-vessels'
import type { AisAnchorEstimate } from '@/lib/ais-anchor-estimates'

vi.mock('maplibre-gl', () => ({
  default: {},
}))

let lastSourceData: Record<string, unknown> = {}
const layerPaint: Record<string, Record<string, unknown>> = {}

vi.mock('react-map-gl/maplibre', async () => {
  const React = await import('react')
  return {
    Map: React.forwardRef(
      ({ children }: { children?: React.ReactNode }, ref: React.Ref<unknown>) => {
        React.useImperativeHandle(ref, () => ({
          getCanvas: () => ({ style: { cursor: 'grab' } }),
          getZoom: () => 14,
          easeTo: vi.fn(),
        }))
        return <div data-testid="map-root">{children}</div>
      },
    ),
    Marker: ({ children, onClick }: { children?: React.ReactNode; onClick?: () => void }) => (
      <div onClick={onClick}>{children}</div>
    ),
    Source: ({ children, id, data }: { children?: React.ReactNode; id: string; data?: unknown }) => {
      lastSourceData[id] = data
      return <div data-testid={`source-${id}`}>{children}</div>
    },
    Layer: ({ id, paint }: { id: string; paint?: Record<string, unknown> }) => {
      layerPaint[id] = paint ?? {}
      return <div data-testid={`layer-${id}`} />
    },
  }
})

const vessel = (mmsi: string): NearbyVessel => ({
  id: `urn:mrn:imo:mmsi:${mmsi}`, name: `V${mmsi}`, mmsi, lat: -25.2935, lon: 152.9105, range_m: 61, age_seconds: 5,
})

const estimate = (mmsi: string, overlaps: boolean): AisAnchorEstimate => ({
  mmsi, lat: -25.2935, lon: 152.9105, swing_radius_m: 40, drawn_radius_m: 50, swing_arc_deg: 200, points: 50, overlaps_ours: overlaps,
})

function renderMap(overrides: Partial<React.ComponentProps<typeof AnchorWatchMap>> = {}) {
  return render(
    <AnchorWatchMap
      vesselLat={-25.2939}
      vesselLon={152.9103}
      vesselHeadingDeg={265}
      anchorLat={-25.2939}
      anchorLon={152.9103}
      radiusMeters={34}
      depthMeters={3.2}
      currentDriftKts={0.1}
      currentSetDeg={120}
      distanceMeters={10}
      bearingDeg={80}
      isImperial={false}
      vesselTrail={[]}
      aisVessels={[]}
      aisTrails={new Map()}
      isDarkTheme={false}
      {...overrides}
    />,
  )
}

describe('AnchorWatchMap estimated AIS anchors', () => {
  it('draws nothing without estimates', () => {
    renderMap({ aisVessels: [vessel('100000001')] })
    expect(screen.queryByTestId('layer-ais-anchor-circles-stroke')).toBeNull()
    expect(screen.queryByLabelText(/Estimated anchor/)).toBeNull()
  })

  it('draws a marker and a swing circle for each estimate of a boat on the map, flagging overlap', () => {
    lastSourceData = {}
    renderMap({
      aisVessels: [vessel('100000001'), vessel('100000002')],
      aisAnchorEstimates: [estimate('100000001', false), estimate('100000002', true)],
    })
    expect(screen.getByTestId('layer-ais-anchor-circles-stroke')).toBeTruthy()
    // A labelled image, so a screen reader announces it.
    expect(screen.getByRole('img', { name: 'Estimated anchor of AIS vessel 100000001' })).toBeTruthy()
    expect(screen.getByRole('img', { name: 'Estimated anchor of AIS vessel 100000002, swing overlaps ours' })).toBeTruthy()
    const data = lastSourceData['ais-anchor-circles'] as GeoJSON.FeatureCollection
    expect(data.features.map(f => f.properties)).toEqual([
      { mmsi: '100000001', overlaps: false },
      { mmsi: '100000002', overlaps: true },
    ])
  })

  it('draws the circle exactly as our own swing circle is drawn, in the AIS marker amber', () => {
    renderMap({ aisVessels: [vessel('100000001')], aisAnchorEstimates: [estimate('100000001', false)] })
    expect(screen.getByTestId('layer-ais-anchor-circles-fill')).toBeTruthy()
    const ours = { fill: layerPaint['alarm-circle-fill'], stroke: layerPaint['alarm-circle-stroke'] }
    const theirs = { fill: layerPaint['ais-anchor-circles-fill'], stroke: layerPaint['ais-anchor-circles-stroke'] }
    expect(theirs.fill['fill-opacity']).toBe(ours.fill['fill-opacity'])
    for (const key of ['line-width', 'line-dasharray', 'line-opacity']) {
      expect(theirs.stroke[key]).toEqual(ours.stroke[key])
    }
    // Amber, and the red of a collision alarm where it overlaps ours.
    expect(JSON.stringify(theirs.stroke['line-color'])).toBe(JSON.stringify(['case', ['get', 'overlaps'], '#dc2626', '#f59e0b']))
    expect(JSON.stringify(theirs.fill['fill-color'])).toBe(JSON.stringify(['case', ['get', 'overlaps'], '#dc2626', '#f59e0b']))
  })

  it('colours the anchor marker amber, and red when its circle overlaps ours', () => {
    renderMap({
      aisVessels: [vessel('100000001'), vessel('100000002')],
      aisAnchorEstimates: [estimate('100000001', false), estimate('100000002', true)],
    })
    const inner = (label: string) => (screen.getByLabelText(label).firstElementChild as HTMLElement).className
    expect(inner('Estimated anchor of AIS vessel 100000001')).toContain('bg-amber-500/90')
    expect(inner('Estimated anchor of AIS vessel 100000002, swing overlaps ours')).toContain('bg-red-600')
  })

  it('lets taps pass through the estimate marker to what lies under it', () => {
    renderMap({ aisVessels: [vessel('100000001')], aisAnchorEstimates: [estimate('100000001', false)] })
    expect(screen.getByLabelText('Estimated anchor of AIS vessel 100000001').className).toContain('pointer-events-none')
  })

  it('leaves out an estimate whose boat is no longer on the map', () => {
    renderMap({ aisVessels: [vessel('100000001')], aisAnchorEstimates: [estimate('100000002', false)] })
    expect(screen.queryByTestId('layer-ais-anchor-circles-stroke')).toBeNull()
  })
})
