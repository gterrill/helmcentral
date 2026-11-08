import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { AnchorWatchDrawer } from '@/components/anchor-watch-drawer'
import type { LowWaterClearanceResult } from '@/lib/low-water-clearance'
import { installFakeTelemetryStream } from './helpers/fake-telemetry-stream'

// Times are the boat's. The runner is on UTC.
installFakeTelemetryStream({ seeds: { 'vessel-state': { timezone: 'UTC', timezone_source: 'fix' } } })

// ADR 0135, ADR 0272: the depth at the boat's current position, projected to
// the next low tide, against the operator's configured margin. The server works
// the verdict out (backend/anchor_low_water_test.go holds the rules); this
// covers how the page words each verdict it is given.
let mockLowWater: { lowWater: LowWaterClearanceResult | null; workedOutAt: string | null; failed: boolean } = { lowWater: null, workedOutAt: null, failed: false }
vi.mock('@/hooks/use-anchor-low-water', () => ({ useAnchorLowWater: () => mockLowWater }))

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
  depthLastUpdateAgeS: null,
  currentDriftKts: null,
  currentSetDeg: null,
  distanceMeters: 10,
  bearingDeg: 80,
  vesselTrail: [],
  aisVessels: [],
  aisTrails: new Map(),
  isDarkTheme: false,
  showImageryLayer: false,
  onImageryToggle: () => undefined,
  onAnchorReposition: () => undefined,
  onRadiusChange: async () => undefined,
  adjustAnchor: async () => undefined,
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
  hasGpsFix: true,
  windBandId: null,
  onWindBandChange: () => undefined,
  onUpdateRodeAndConditions: async () => undefined,
  planningDepthM: null,
  planningTideHeightFt: null,
  onPlanningDepthChange: () => Promise.resolve(),
}

const LOW = '2026-11-08T04:45:00Z'
const WORKED = '2026-11-08T04:31:00Z'

describe('AnchorWatchDrawer low water clearance', () => {
  beforeEach(() => { mockLowWater = { lowWater: null, workedOutAt: null, failed: false } })

  it('shows nothing until the server has answered', () => {
    render(<AnchorWatchDrawer {...baseProps} />)

    expect(screen.queryByTestId('low-water-clearance')).not.toBeInTheDocument()
  })

  it('names the missing input when there is no draft', () => {
    mockLowWater = { lowWater: { status: 'unknown', reason: 'no_draft' }, workedOutAt: WORKED, failed: false }
    render(<AnchorWatchDrawer {...baseProps} />)

    expect(screen.getByTestId('low-water-clearance')).toHaveTextContent('No draft from the boat')
  })

  it('names no_depth when there is no live depth reading', () => {
    mockLowWater = { lowWater: { status: 'unknown', reason: 'no_depth' }, workedOutAt: WORKED, failed: false }
    render(<AnchorWatchDrawer {...baseProps} depthMeters={null} />)

    expect(screen.getByTestId('low-water-clearance')).toHaveTextContent('No depth')
  })

  it('names a missing tide station and an unavailable tide apart', () => {
    mockLowWater = { lowWater: { status: 'unknown', reason: 'no_tide_station' }, workedOutAt: WORKED, failed: false }
    const { unmount } = render(<AnchorWatchDrawer {...baseProps} />)
    expect(screen.getByTestId('low-water-clearance')).toHaveTextContent('No tide station')
    unmount()

    mockLowWater = { lowWater: { status: 'unknown', reason: 'tide_unreachable' }, workedOutAt: WORKED, failed: false }
    render(<AnchorWatchDrawer {...baseProps} />)
    expect(screen.getByTestId('low-water-clearance')).toHaveTextContent('Tide unavailable')
  })

  it('shows no clearance line when the boat is not anchored and no watch is set', () => {
    mockLowWater = { lowWater: { status: 'unknown', reason: 'not_anchored' }, workedOutAt: WORKED, failed: false }
    render(<AnchorWatchDrawer {...baseProps} />)

    expect(screen.queryByTestId('low-water-clearance')).not.toBeInTheDocument()
  })

  it('shows a quiet clearance line when the depth at low water clears the margin', () => {
    mockLowWater = { lowWater: { status: 'ok', depthAtLowM: 3.39, clearanceM: 2.19, lowTideTime: LOW }, workedOutAt: WORKED, failed: false }
    render(<AnchorWatchDrawer {...baseProps} />)

    const readout = screen.getByTestId('low-water-clearance')
    expect(readout).toHaveTextContent('2.2 m under keel at low water')
    expect(readout).not.toHaveTextContent('Too shallow')
  })

  it('warns when the projected depth at low water is under the configured margin', () => {
    mockLowWater = { lowWater: { status: 'too_shallow', depthAtLowM: 1.39, clearanceM: 0.19, lowTideTime: LOW }, workedOutAt: WORKED, failed: false }
    render(<AnchorWatchDrawer {...baseProps} />)

    const readout = screen.getByTestId('low-water-clearance')
    expect(readout).toHaveTextContent('Too shallow at low water')
    expect(readout).toHaveTextContent('0.2 m under keel')
  })

  it('shows "Tide forecast out of date" when the server says the tide is stale', () => {
    mockLowWater = { lowWater: { status: 'unknown', reason: 'tide_stale' }, workedOutAt: WORKED, failed: false }
    render(<AnchorWatchDrawer {...baseProps} />)

    expect(screen.getByTestId('low-water-clearance')).toHaveTextContent('Tide forecast out of date')
  })

  // A live warning is neither hidden nor shown as current when a poll fails.
  it('keeps "too shallow" visible, marked as not updated since, after one failed poll', () => {
    mockLowWater = { lowWater: { status: 'too_shallow', depthAtLowM: 1.39, clearanceM: 0.19, lowTideTime: LOW }, workedOutAt: WORKED, failed: true }
    render(<AnchorWatchDrawer {...baseProps} />)

    const readouts = screen.getAllByTestId('low-water-clearance')
    expect(readouts).toHaveLength(1)
    expect(readouts[0]).toHaveTextContent('Too shallow at low water')
    expect(readouts[0]).toHaveTextContent('not updated since 4.31 AM')
    expect(readouts[0].className).toMatch(/opacity-70/)
  })

  it('does not mark a current verdict as stale', () => {
    mockLowWater = { lowWater: { status: 'too_shallow', depthAtLowM: 1.39, clearanceM: 0.19, lowTideTime: LOW }, workedOutAt: WORKED, failed: false }
    render(<AnchorWatchDrawer {...baseProps} />)

    expect(screen.getByTestId('low-water-clearance')).not.toHaveTextContent('not updated since')
  })

  it('says so when the server could not give a verdict at all, rather than showing nothing', () => {
    mockLowWater = { lowWater: null, workedOutAt: null, failed: true }
    render(<AnchorWatchDrawer {...baseProps} />)

    expect(screen.getByTestId('low-water-clearance')).toHaveTextContent('Low-water clearance unavailable')
  })
})
