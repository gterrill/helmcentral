import { describe, it, expect, vi } from 'vitest'
import { render, screen, within } from '@testing-library/react'
import { AnchorWatchDrawer } from '@/components/anchor-watch-drawer'

// Design critique item 1: the map's own metric overlay dropped its Distance
// row (it duplicated the KPI anchor-watch-tile.tsx now promotes above the
// map). The drawer doesn't get that tile-level promotion for free — it's a
// separate host of the same AnchorWatchMap — so without an equivalent here,
// dropping Distance from the shared overlay would leave the fullscreen
// drawer with no distance readout at all. This suite covers the drawer's
// own promoted KPI that keeps that from happening.
vi.mock('react-map-gl/maplibre', async () => {
  const React = await import('react')
  return {
    Map: React.forwardRef(({ children }: { children?: React.ReactNode }, ref) => {
      React.useImperativeHandle(ref, () => ({
        getCanvas: () => ({ style: { cursor: 'grab' } }),
        getZoom: () => 14,
        easeTo: () => undefined,
      }))
      return <div data-testid="map-root">{children}</div>
    }),
    Marker: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
    Source: ({ children, id }: { children?: React.ReactNode; id: string }) => (
      <div data-testid={`source-${id}`}>{children}</div>
    ),
    Layer: ({ id }: { id: string }) => <div data-testid={`layer-${id}`} />,
  }
})

const baseProps = {
  vesselLat: -25.2939,
  vesselLon: 152.9103,
  vesselHeadingDeg: 45,
  anchorLat: -25.2938,
  anchorLon: 152.9102,
  anchorSetAt: '2026-08-20T06:30:00Z',
  anchorStateKnown: true,
  radiusMeters: 20,
  depthMeters: 3.2,
  currentDriftKts: null,
  currentSetDeg: null,
  distanceMeters: 38,
  bearingDeg: 80,
  vesselTrail: () => [],
  aisVessels: [],
  aisTrails: () => new Map(),
  isDarkTheme: false,
  showImageryLayer: false,
  onImageryToggle: () => undefined,
  showRadarEcho: false,
  onRadarEchoToggle: () => undefined,
  onRadiusChange: async () => undefined,
  onClearAnchor: () => undefined,
  isImperial: false,
  onDropAnchor: () => undefined,
  canDrop: true,
  anchorState: 'set' as const,
  rodeDeployedM: 0,
  seaState: 'calm' as const,
  seabedType: 'sand' as const,
  windSpeedApparentKts: null,
  maxGustKts: { '10m': null, '30m': null, '1h': null, '24h': null },
  tide: null,
  anchorConfig: {
    bowRollerHeightM: 1,
    chainSizeMm: 10,
    chainOnboardM: 50,
    hullType: 'power_cat' as const,
    scopeMethod: 'ratio' as const,
    windageAreaM2: 20,
    gpsFromBowM: 2,
    loaM: 10,
  },
  vesselLengthOverallM: null,
  windBandId: null,
  onWindBandChange: () => undefined,
  onUpdateRodeAndConditions: async () => undefined,
  planningDepthM: null,
  planningTideHeightFt: null,
  onPlanningDepthChange: () => Promise.resolve(),
}

describe('AnchorWatchDrawer promoted distance KPI', () => {
  it('shows the distance-of-radius KPI above the map while anchored', () => {
    render(<AnchorWatchDrawer {...baseProps} anchorState="set" distanceMeters={38} radiusMeters={30} />)

    const kpi = screen.getByTestId('anchor-distance-kpi')
    expect(within(kpi).getByText('Distance')).toBeInTheDocument()
    expect(kpi).toHaveTextContent('38')
    expect(kpi).toHaveTextContent('of 30 m')
  })

  it('converts to feet under imperial units', () => {
    render(<AnchorWatchDrawer {...baseProps} anchorState="set" distanceMeters={38} radiusMeters={30} isImperial />)

    const kpi = screen.getByTestId('anchor-distance-kpi')
    // 38 m * 3.28084 = 124.7 ft, rounded to 125; 30 m -> 98 ft.
    expect(kpi).toHaveTextContent('125')
    expect(kpi).toHaveTextContent('of 98 ft')
  })

  it('does not render the KPI with no watch active', () => {
    render(<AnchorWatchDrawer {...baseProps} anchorState="none" anchorLat={null} anchorLon={null} />)

    expect(screen.queryByTestId('anchor-distance-kpi')).not.toBeInTheDocument()
  })

  it('shows a dash rather than a stale figure when anchored with no live distance yet', () => {
    render(<AnchorWatchDrawer {...baseProps} anchorState="set" distanceMeters={null} />)

    const kpi = screen.getByTestId('anchor-distance-kpi')
    expect(within(kpi).getByText('—')).toBeInTheDocument()
  })
})
