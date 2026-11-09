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
  planningTide: null,
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
  // ADR 0274: the station estimate is labelled until the sounder's own readings
  // at this anchorage have narrowed to better than the margin.
  describe('local tide calibration', () => {
    const READY = {
      ready: true,
      hoursOfReadings: 4.6,
      shallowestSounded: { depthM: 2.0, halfWidthM: 0.3 },
      shallowestDiffers: true,
      highTideTime: '2026-11-08T10:27:00Z',
      highAtBoat: { depthM: 5.1, halfWidthM: 0.2 },
      deepestSounded: { depthM: 5.4, halfWidthM: 0.3 },
      deepestDiffers: true,
      stationMatchNote: "The tide here doesn't follow Middle Island (11 km away): it runs 64% of its range.",
    }

    it('calls the estimate tide model prediction when the tide follows the position', () => {
      mockLowWater = {
        lowWater: {
          status: 'ok', depthAtLowM: 2.4, clearanceM: 1.2, lowTideTime: LOW,
          calibration: { ready: false, notReady: 'implausible_fit', hoursOfReadings: 3, shallowestDiffers: false, deepestDiffers: false },
          tideIsPoint: true,
        },
        workedOutAt: WORKED, failed: false,
      }
      render(<AnchorWatchDrawer {...baseProps} />)

      const line = screen.getByTestId('low-water-method')
      expect(line).toHaveTextContent('Depth based on tide model prediction')
      expect(line).toHaveTextContent("readings don't fit the tide model, not used")
      expect(line).not.toHaveTextContent('station')
    })

    it('names the tide model behind the verdict when that set the warning', () => {
      mockLowWater = {
        lowWater: {
          status: 'too_shallow', depthAtLowM: 2.0, depthAtLowHalfWidthM: 0.4, clearanceM: 0.8, lowTideTime: LOW,
          cautiousSource: 'station', verdictClearanceM: 0.3, calibration: READY, tideIsPoint: true,
        },
        workedOutAt: WORKED, failed: false,
      }
      render(<AnchorWatchDrawer {...baseProps} />)

      expect(screen.getByTestId('low-water-clearance')).toHaveTextContent('set by the tide model estimate')
    })

    it('labels the station estimate, with the hours of readings so far while it calibrates', () => {
      mockLowWater = {
        lowWater: {
          status: 'ok', depthAtLowM: 2.4, clearanceM: 1.2, lowTideTime: LOW,
          calibration: { ready: false, notReady: 'too_few_readings', hoursOfReadings: 1.2, shallowestDiffers: false, deepestDiffers: false },
        },
        workedOutAt: WORKED, failed: false,
      }
      render(<AnchorWatchDrawer {...baseProps} />)

      const line = screen.getByTestId('low-water-method')
      expect(line).toHaveTextContent('Depth based on tide station prediction')
      expect(line).toHaveTextContent('2.4 m')
      expect(line).toHaveTextContent('calibrating, 1 h of readings so far')
      expect(screen.queryByTestId('low-water-shallowest')).not.toBeInTheDocument()
      expect(screen.queryByTestId('tide-match-note')).not.toBeInTheDocument()
    })

    it('says a noisy sounder is the reason, in the operator words', () => {
      mockLowWater = {
        lowWater: {
          status: 'ok', depthAtLowM: 2.4, clearanceM: 1.2, lowTideTime: LOW,
          calibration: { ready: false, notReady: 'readings_too_scattered', hoursOfReadings: 3, shallowestDiffers: false, deepestDiffers: false },
        },
        workedOutAt: WORKED, failed: false,
      }
      render(<AnchorWatchDrawer {...baseProps} />)

      expect(screen.getByTestId('low-water-method')).toHaveTextContent('readings too scattered to calibrate yet')
    })

    it('shows the calibrated depth with its interval, the shallowest spot, high water and the tide-match note', () => {
      mockLowWater = {
        lowWater: {
          status: 'ok', depthAtLowM: 2.4, depthAtLowHalfWidthM: 0.2, clearanceM: 1.2, lowTideTime: LOW,
          cautiousSource: 'boat_now', calibration: READY,
        },
        workedOutAt: WORKED, failed: false,
      }
      render(<AnchorWatchDrawer {...baseProps} />)

      expect(screen.getByTestId('low-water-method')).toHaveTextContent('Locally calibrated depth estimate (4 h of readings) · 2.4 m ±0.2')
      expect(screen.getByTestId('low-water-shallowest')).toHaveTextContent('Shallowest sounded spot 2.0 m ±0.3')
      expect(screen.getByTestId('high-water-depth')).toHaveTextContent('High water 5.1 m ±0.2')
      expect(screen.getByTestId('high-water-depth')).toHaveTextContent('deepest sounded spot 5.4 m ±0.3')
      expect(screen.getByTestId('tide-match-note')).toHaveTextContent("The tide here doesn't follow Middle Island (11 km away): it runs 64% of its range.")
    })

    it('leaves the shallowest spot off when it is not worth a line', () => {
      mockLowWater = {
        lowWater: {
          status: 'ok', depthAtLowM: 2.4, depthAtLowHalfWidthM: 0.2, clearanceM: 1.2, lowTideTime: LOW,
          cautiousSource: 'boat_now', calibration: { ...READY, shallowestDiffers: false, deepestDiffers: false, stationMatchNote: undefined },
        },
        workedOutAt: WORKED, failed: false,
      }
      render(<AnchorWatchDrawer {...baseProps} />)

      expect(screen.queryByTestId('low-water-shallowest')).not.toBeInTheDocument()
      expect(screen.queryByTestId('tide-match-note')).not.toBeInTheDocument()
      expect(screen.getByTestId('high-water-depth')).not.toHaveTextContent('deepest')
    })

    it('says when the shallowest sounded spot, not the boat cell, set the warning', () => {
      mockLowWater = {
        lowWater: {
          status: 'too_shallow', depthAtLowM: 2.4, depthAtLowHalfWidthM: 0.2, clearanceM: 1.2, lowTideTime: LOW,
          cautiousSource: 'shallowest_sounded', calibration: READY,
        },
        workedOutAt: WORKED, failed: false,
      }
      render(<AnchorWatchDrawer {...baseProps} />)

      expect(screen.getByTestId('low-water-clearance')).toHaveTextContent('Too shallow at low water')
      expect(screen.getByTestId('low-water-clearance')).toHaveTextContent('set by the shallowest sounded spot')
    })

    it('shows the clearance with its range, and the bottom of the range that the warning rests on', () => {
      mockLowWater = {
        lowWater: {
          status: 'too_shallow', depthAtLowM: 2.0, depthAtLowHalfWidthM: 0.4, clearanceM: 0.8, lowTideTime: LOW,
          cautiousSource: 'boat_now', verdictClearanceM: 0.8, verdictHalfWidthM: 0.4, calibration: { ...READY, shallowestDiffers: false },
        },
        workedOutAt: WORKED, failed: false,
      }
      render(<AnchorWatchDrawer {...baseProps} />)

      expect(screen.getByTestId('low-water-clearance')).toHaveTextContent('Too shallow at low water · 0.8 m ±0.4 under keel, could be 0.4 m at')
    })

    it('shows the station estimate behind the verdict when that set the warning, with no range', () => {
      mockLowWater = {
        lowWater: {
          status: 'too_shallow', depthAtLowM: 2.0, depthAtLowHalfWidthM: 0.4, clearanceM: 0.8, lowTideTime: LOW,
          cautiousSource: 'station', verdictClearanceM: 0.3, calibration: READY,
        },
        workedOutAt: WORKED, failed: false,
      }
      render(<AnchorWatchDrawer {...baseProps} />)

      const readout = screen.getByTestId('low-water-clearance')
      expect(readout).toHaveTextContent('0.3 m under keel')
      expect(readout).not.toHaveTextContent('±')
      expect(readout).toHaveTextContent('set by the tide station estimate')
    })

    it('shows the shallowest spot behind the verdict, with its range, when that set the warning', () => {
      mockLowWater = {
        lowWater: {
          status: 'too_shallow', depthAtLowM: 2.0, depthAtLowHalfWidthM: 0.4, clearanceM: 0.8, lowTideTime: LOW,
          cautiousSource: 'shallowest_sounded', verdictClearanceM: 0.5, verdictHalfWidthM: 0.3, calibration: READY,
        },
        workedOutAt: WORKED, failed: false,
      }
      render(<AnchorWatchDrawer {...baseProps} />)

      const readout = screen.getByTestId('low-water-clearance')
      expect(readout).toHaveTextContent('0.5 m ±0.3 under keel, could be 0.2 m')
      expect(readout).toHaveTextContent('set by the shallowest sounded spot')
    })

    it('writes the range in feet when the operator reads feet', () => {
      mockLowWater = {
        lowWater: {
          status: 'too_shallow', depthAtLowM: 2.0, depthAtLowHalfWidthM: 0.4, clearanceM: 0.8, lowTideTime: LOW,
          cautiousSource: 'boat_now', verdictClearanceM: 0.8, verdictHalfWidthM: 0.4, calibration: READY,
        },
        workedOutAt: WORKED, failed: false,
      }
      render(<AnchorWatchDrawer {...baseProps} isImperial />)

      expect(screen.getByTestId('low-water-clearance')).toHaveTextContent('2.6 ft ±1.3 under keel, could be 1.3 ft')
    })

    it('says the calibration is being worked out before the first fit is published', () => {
      mockLowWater = {
        lowWater: {
          status: 'ok', depthAtLowM: 2.4, clearanceM: 1.2, lowTideTime: LOW,
          calibration: { ready: false, notReady: 'working_it_out', hoursOfReadings: 0, shallowestDiffers: false, deepestDiffers: false },
        },
        workedOutAt: WORKED, failed: false,
      }
      render(<AnchorWatchDrawer {...baseProps} />)

      expect(screen.getByTestId('low-water-method')).toHaveTextContent('working out the calibration')
    })

    it('says to set a clearance when none is set', () => {
      mockLowWater = {
        lowWater: {
          status: 'ok', depthAtLowM: 2.4, clearanceM: 1.2, lowTideTime: LOW,
          calibration: { ready: false, notReady: 'no_clearance_set', hoursOfReadings: 3, shallowestDiffers: false, deepestDiffers: false },
        },
        workedOutAt: WORKED, failed: false,
      }
      render(<AnchorWatchDrawer {...baseProps} />)

      expect(screen.getByTestId('low-water-method')).toHaveTextContent('set a low-water clearance in Anchor Watch settings to judge the calibration')
    })

    it('gives depths in feet when the operator reads feet', () => {
      mockLowWater = {
        lowWater: {
          status: 'ok', depthAtLowM: 2.4, depthAtLowHalfWidthM: 0.2, clearanceM: 1.2, lowTideTime: LOW,
          cautiousSource: 'boat_now', calibration: READY,
        },
        workedOutAt: WORKED, failed: false,
      }
      render(<AnchorWatchDrawer {...baseProps} isImperial />)

      expect(screen.getByTestId('low-water-method')).toHaveTextContent('7.9 ft ±0.7')
    })
  })
})
