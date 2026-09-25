import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { AnchorWatchDrawer } from '@/components/anchor-watch-drawer'
import type { TideToday } from '@/hooks/use-tide-today'

// ADR 0135: the depth at the boat's current position, projected to the next
// low tide, against the operator's configured margin. Same coverage shape
// as anchor-watch-tile.test.tsx's own "low water clearance" describe block.
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

// The drawer computes `now` itself (`new Date()`, live at render time — see
// ADR 0135), so datetime/low_tide_time below are built relative to the real
// clock rather than a fixed calendar date: a hardcoded date would eventually
// (or immediately, depending when the suite runs) fall foul of the 30-minute
// staleness rule or the "low already passed" rule this same ADR adds.
function makeTide(overrides: Partial<TideToday> = {}): TideToday {
  const now = Date.now()
  return {
    datetime: new Date(now - 5 * 60 * 1000).toISOString(),
    current_tide_height_ft: 3,
    tide_direction: 'Falling',
    high_tide_time: new Date(0).toISOString(),
    high_tide_height_ft: 5,
    low_tide_time: new Date(now + 60 * 60 * 1000).toISOString(),
    low_tide_height_ft: 1,
    station_name: 'Test Station',
    provider: 'test',
    ...overrides,
  }
}

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
  distanceMeters: 10,
  bearingDeg: 80,
  vesselTrail: () => [],
  aisVessels: [],
  aisTrails: () => new Map(),
  isDarkTheme: false,
  showImageryLayer: false,
  onImageryToggle: () => undefined,
  showRadarEcho: false,
  onRadarEchoToggle: () => undefined,
  onAnchorReposition: () => undefined,
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
    minClearanceAtLowM: 0.5,
  },
  vesselLengthOverallM: null,
  vesselDraftM: null as number | null,
  windBandId: null,
  onWindBandChange: () => undefined,
  onUpdateRodeAndConditions: async () => undefined,
  planningDepthM: null,
  planningTideHeightFt: null,
  onPlanningDepthChange: () => Promise.resolve(),
}

describe('AnchorWatchDrawer low water clearance', () => {
  it('names the missing input when draft is unavailable (the baseProps default)', () => {
    render(<AnchorWatchDrawer {...baseProps} />)

    expect(screen.getByTestId('low-water-clearance')).toHaveTextContent('No draft from the boat')
  })

  it('names no_depth when there is no live depth reading', () => {
    render(<AnchorWatchDrawer {...baseProps} depthMeters={null} vesselDraftM={1.2} tide={makeTide()} />)

    expect(screen.getByTestId('low-water-clearance')).toHaveTextContent('No depth')
  })

  it('names no_tide when there is no tide station', () => {
    render(<AnchorWatchDrawer {...baseProps} depthMeters={5} vesselDraftM={1.2} tide={null} />)

    expect(screen.getByTestId('low-water-clearance')).toHaveTextContent('No tide station')
  })

  it('shows a quiet clearance line when the depth at low water clears the margin', () => {
    render(<AnchorWatchDrawer {...baseProps} depthMeters={4} vesselDraftM={1.2} tide={makeTide()} />)

    const readout = screen.getByTestId('low-water-clearance')
    expect(readout).toHaveTextContent('under keel at low water')
    expect(readout).not.toHaveTextContent('Too shallow')
  })

  it('warns when the projected depth at low water is under the configured margin', () => {
    render(<AnchorWatchDrawer {...baseProps} depthMeters={2} vesselDraftM={1.2} tide={makeTide()} />)

    const readout = screen.getByTestId('low-water-clearance')
    expect(readout).toHaveTextContent('Too shallow at low water')
    expect(readout).toHaveTextContent('0.2 m under keel')
  })

  it('shows "Tide forecast out of date" when the tide reading is more than 30 minutes old', () => {
    const staleTide = makeTide({ datetime: new Date(Date.now() - 40 * 60 * 1000).toISOString() })
    render(<AnchorWatchDrawer {...baseProps} depthMeters={4} vesselDraftM={1.2} tide={staleTide} />)

    expect(screen.getByTestId('low-water-clearance')).toHaveTextContent('Tide forecast out of date')
  })
})
