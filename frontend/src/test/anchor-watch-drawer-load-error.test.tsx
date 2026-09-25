import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { AnchorWatchDrawer } from '@/components/anchor-watch-drawer'

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
  anchorLat: null,
  anchorLon: null,
  anchorSetAt: null,
  anchorStateKnown: true,
  radiusMeters: 20,
  depthMeters: null,
  currentDriftKts: null,
  currentSetDeg: null,
  distanceMeters: null,
  bearingDeg: null,
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
  anchorState: 'none' as const,
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
  onPlanningDepthChange: () => undefined,
}

// The backend puts a damaged anchor_watch.json into an explicit error state
// rather than an invented or empty watch (GET /api/anchor-watch reports it
// as `error`, naming the file path and the parse error). The Anchor Watch
// page (this drawer) must show that plainly, using "tile" wording, not
// silently render the ordinary empty "no watch set" view as if nothing were
// wrong.
describe('AnchorWatchDrawer load-error banner', () => {
  it('shows the problem naming the file path when the watch is unreadable', () => {
    render(
      <AnchorWatchDrawer
        {...baseProps}
        error="parsing anchor watch state (data/anchor_watch.json): unexpected end of JSON input"
      />,
    )

    expect(screen.getByText(/saved anchor watch unreadable/i)).toBeInTheDocument()
    expect(screen.getByText(/data\/anchor_watch\.json/)).toBeInTheDocument()
  })

  it('shows nothing extra once the watch loads normally', () => {
    render(<AnchorWatchDrawer {...baseProps} error={null} />)

    expect(screen.queryByText(/saved anchor watch unreadable/i)).toBeNull()
  })

  it('omitting error entirely (every other existing caller) shows nothing extra', () => {
    render(<AnchorWatchDrawer {...baseProps} />)

    expect(screen.queryByText(/saved anchor watch unreadable/i)).toBeNull()
  })
})
