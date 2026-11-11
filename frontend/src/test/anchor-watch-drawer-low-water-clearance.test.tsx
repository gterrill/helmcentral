import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, within } from '@testing-library/react'
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

// The calibration and shallowest-spot text sit behind the info button by the tide direction.
function openTideInfo() {
  fireEvent.click(screen.getByRole('button', { name: /^Tide details/ }))
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

    const readout = screen.getByTestId('low-water-tide')
    expect(within(readout).getByTestId('low-water-low')).toHaveTextContent('Low 4.45 AM 3.4 m')
    expect(within(readout).getByTestId('low-water-keel')).toHaveTextContent('2.2 m under keel')
    expect(screen.queryByTestId('low-water-clearance')).not.toBeInTheDocument()
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
    // The one high on the wire while the calibration is ready: the calibrated one.
    const HIGH = { highTideTime: '2026-11-08T10:27:00Z', depthAtHighM: 5.1, depthAtHighHalfWidthM: 0.2 }
    const READY = {
      ready: true,
      hoursOfReadings: 4.6,
      shallowestSounded: { depthM: 2.0, halfWidthM: 0.3 },
      shallowestDiffers: true,
      k: 0.83,
      tideMinutesLater: -8,
    }

    it('names the tide model, not a station, when the tide follows the position', () => {
      mockLowWater = {
        lowWater: {
          status: 'ok', depthAtLowM: 2.4, clearanceM: 1.2, lowTideTime: LOW,
          calibration: { ready: false, notReady: 'implausible_fit', hoursOfReadings: 3, shallowestDiffers: false },
          tideIsPoint: true,
        },
        workedOutAt: WORKED, failed: false,
      }
      render(<AnchorWatchDrawer {...baseProps} />)

      openTideInfo()
      const line = screen.getByTestId('low-water-calibration')
      expect(line).toHaveTextContent("Calibration readings don't fit the tide model, not used")
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

    it('shows the station low and its clearance, with the hours so far, while it calibrates', () => {
      mockLowWater = {
        lowWater: {
          status: 'ok', depthAtLowM: 2.4, clearanceM: 1.2, lowTideTime: LOW,
          calibration: { ready: false, notReady: 'too_few_readings', hoursOfReadings: 1.2, shallowestDiffers: false },
        },
        workedOutAt: WORKED, failed: false,
      }
      render(<AnchorWatchDrawer {...baseProps} />)

      expect(screen.getByTestId('low-water-low')).toHaveTextContent('Low 4.45 AM 2.4 m')
      expect(screen.getByTestId('low-water-keel')).toHaveTextContent('1.2 m under keel')
      openTideInfo()
      expect(screen.getByTestId('low-water-calibration')).toHaveTextContent('Calibration in progress (1 h of readings so far)')
      expect(screen.queryByTestId('low-water-shallowest')).not.toBeInTheDocument()
      expect(screen.queryByTestId('tide-match-note')).not.toBeInTheDocument()
      expect(screen.queryByTestId('low-water-method')).not.toBeInTheDocument()
    })

    it('gives the projected ready time on the boat clock when the server projects one', () => {
      mockLowWater = {
        lowWater: {
          status: 'ok', depthAtLowM: 2.4, clearanceM: 1.2, lowTideTime: LOW,
          calibration: { ready: false, notReady: 'too_few_readings', hoursOfReadings: 1.2, readyAt: '2026-11-08T08:40:00Z', shallowestDiffers: false },
        },
        workedOutAt: WORKED, failed: false,
      }
      render(<AnchorWatchDrawer {...baseProps} />)
      openTideInfo()

      expect(screen.getByTestId('low-water-calibration')).toHaveTextContent('Calibration in progress (ready at ~8.40 AM)')
    })

    it('prefixes the weekday when the projected time falls on another day', () => {
      const cal = { ready: false, notReady: 'too_few_readings' as const, hoursOfReadings: 1.2, shallowestDiffers: false }
      mockLowWater = {
        lowWater: { status: 'ok', depthAtLowM: 2.4, clearanceM: 1.2, lowTideTime: LOW, calibration: { ...cal, readyAt: '2026-11-09T02:00:00Z' } },
        workedOutAt: WORKED, failed: false,
      }
      render(<AnchorWatchDrawer {...baseProps} />)
      openTideInfo()
      expect(screen.getByTestId('low-water-calibration')).toHaveTextContent('ready at ~Mon 2.00 AM')
    })

    it('shows the station high the server worked out with the low, before the calibration is ready', () => {
      mockLowWater = {
        lowWater: {
          status: 'ok', depthAtLowM: 2.4, clearanceM: 1.2, lowTideTime: LOW, highTideTime: '2026-11-08T02:00:00Z', depthAtHighM: 5.6,
          calibration: { ready: false, notReady: 'too_few_readings', hoursOfReadings: 1.2, shallowestDiffers: false },
        },
        workedOutAt: WORKED, failed: false,
      }
      render(<AnchorWatchDrawer {...baseProps} />)

      expect(screen.getByTestId('low-water-high')).toHaveTextContent('High 2.00 AM est. 5.6 m')
      expect(screen.getByTestId('low-water-low')).toHaveTextContent('Low 4.45 AM 2.4 m')
      expect(screen.getByTestId('low-water-keel')).toHaveTextContent('1.2 m under keel')
    })

    it('gives no ready time when the server has not said when the figures were worked out', () => {
      mockLowWater = {
        lowWater: {
          status: 'ok', depthAtLowM: 2.4, clearanceM: 1.2, lowTideTime: LOW,
          calibration: { ready: false, notReady: 'too_few_readings', hoursOfReadings: 1.2, readyAt: '2026-11-08T08:40:00Z', shallowestDiffers: false },
        },
        workedOutAt: null, failed: false,
      }
      render(<AnchorWatchDrawer {...baseProps} />)
      openTideInfo()

      expect(screen.getByTestId('low-water-calibration')).toHaveTextContent('Calibration in progress (1 h of readings so far)')
    })

    it('names the tide source from the verdict when the page has no tide of its own, never "No tide station"', () => {
      mockLowWater = {
        lowWater: { status: 'ok', depthAtLowM: 2.4, clearanceM: 1.2, lowTideTime: LOW, tideStationName: 'Middle Island' },
        workedOutAt: WORKED, failed: false,
      }
      const { unmount } = render(<AnchorWatchDrawer {...baseProps} />)
      const header = screen.getByTestId('anchor-watch-hud-header')
      expect(header).toHaveTextContent('Tide')
      expect(header).not.toHaveTextContent('Middle Island')
      expect(header).not.toHaveTextContent('No tide station')
      openTideInfo()
      expect(screen.getByTestId('tide-station-name')).toHaveTextContent('From Middle Island')
      unmount()

      mockLowWater = { lowWater: { status: 'ok', depthAtLowM: 2.4, clearanceM: 1.2, lowTideTime: LOW }, workedOutAt: WORKED, failed: false }
      render(<AnchorWatchDrawer {...baseProps} />)
      expect(screen.getByTestId('anchor-watch-hud-header')).not.toHaveTextContent('No tide station')
      expect(screen.queryByRole('button', { name: /^Tide details/ })).not.toBeInTheDocument()
    })

    it('says a noisy sounder is the reason, in the operator words', () => {
      mockLowWater = {
        lowWater: {
          status: 'ok', depthAtLowM: 2.4, clearanceM: 1.2, lowTideTime: LOW,
          calibration: { ready: false, notReady: 'readings_too_scattered', hoursOfReadings: 3, shallowestDiffers: false },
        },
        workedOutAt: WORKED, failed: false,
      }
      render(<AnchorWatchDrawer {...baseProps} />)
      openTideInfo()

      expect(screen.getByTestId('low-water-calibration')).toHaveTextContent('Calibration readings too scattered to calibrate yet')
    })

    it('shows the calibrated high and low at the boat, the clearance, and one calibration line', () => {
      mockLowWater = {
        lowWater: {
          status: 'ok', depthAtLowM: 2.4, depthAtLowHalfWidthM: 0.2, clearanceM: 1.2, lowTideTime: LOW,
          cautiousSource: 'boat_now', calibration: READY, ...HIGH, highTideTime: '2026-11-08T02:00:00Z',
        },
        workedOutAt: WORKED, failed: false,
      }
      render(<AnchorWatchDrawer {...baseProps} />)

      expect(screen.getByTestId('low-water-high')).toHaveTextContent('High 2.00 AM 5.1 m ±0.2')
      expect(screen.getByTestId('low-water-low')).toHaveTextContent('Low 4.45 AM 2.4 m ±0.2')
      expect(screen.getByTestId('low-water-keel')).toHaveTextContent('1.2 m ±0.2 under keel')
      // Calibration and the shallowest spot are behind the info button until it is clicked.
      expect(screen.queryByTestId('low-water-calibration')).not.toBeInTheDocument()
      expect(screen.queryByTestId('low-water-shallowest')).not.toBeInTheDocument()
      openTideInfo()
      expect(screen.getByTestId('low-water-calibration')).toHaveTextContent('Calibration 83% of range · 8 min earlier · 4 h of readings')
      expect(screen.getByTestId('low-water-shallowest')).toHaveTextContent('Shallowest sounded spot 2.0 m ±0.3')
      for (const gone of ['low-water-method', 'high-water-depth', 'tide-match-note']) {
        expect(screen.queryByTestId(gone)).not.toBeInTheDocument()
      }
    })

    it('orders the high and the low by time and drops the high when the server has none', () => {
      mockLowWater = {
        lowWater: {
          status: 'ok', depthAtLowM: 2.4, depthAtLowHalfWidthM: 0.2, clearanceM: 1.2, lowTideTime: LOW,
          cautiousSource: 'boat_now', calibration: READY, ...HIGH,
        },
        workedOutAt: WORKED, failed: false,
      }
      const { unmount } = render(<AnchorWatchDrawer {...baseProps} />)
      const rows = screen.getByTestId('low-water-tide').querySelectorAll('[data-testid^="low-water-"]')
      expect(Array.from(rows).map((r) => r.getAttribute('data-testid'))).toEqual(['low-water-low', 'low-water-keel', 'low-water-high'])
      unmount()

      mockLowWater = {
        lowWater: {
          status: 'ok', depthAtLowM: 2.4, depthAtLowHalfWidthM: 0.2, clearanceM: 1.2, lowTideTime: LOW,
          cautiousSource: 'boat_now', calibration: READY,
        },
        workedOutAt: WORKED, failed: false,
      }
      render(<AnchorWatchDrawer {...baseProps} />)
      expect(screen.getByTestId('low-water-tide')).not.toHaveTextContent('High')
      expect(screen.queryByTestId('low-water-high')).not.toBeInTheDocument()
    })

    it('never prints a plus-or-minus that reads 0.0, and leaves out a timing that rounds to 0 min', () => {
      mockLowWater = {
        lowWater: {
          status: 'ok', depthAtLowM: 2.5, depthAtLowHalfWidthM: 0.03, clearanceM: 1.3, lowTideTime: LOW,
          cautiousSource: 'boat_now',
          calibration: { ...READY, tideMinutesLater: 0.2 }, highTideTime: '2026-11-08T10:27:00Z', depthAtHighM: 5.7, depthAtHighHalfWidthM: 0.04,
        },
        workedOutAt: WORKED, failed: false,
      }
      render(<AnchorWatchDrawer {...baseProps} />)

      expect(screen.getByTestId('low-water-tide').textContent).not.toContain('±')
      expect(screen.getByTestId('low-water-low')).toHaveTextContent('Low 4.45 AM 2.5 m')
      expect(screen.getByTestId('low-water-keel')).toHaveTextContent('1.3 m under keel')
      openTideInfo()
      expect(screen.getByTestId('low-water-calibration').textContent).not.toMatch(/min (earlier|later)/)
    })

    it('says later when the tide here turns after the station', () => {
      mockLowWater = {
        lowWater: {
          status: 'ok', depthAtLowM: 2.4, depthAtLowHalfWidthM: 0.2, clearanceM: 1.2, lowTideTime: LOW,
          cautiousSource: 'boat_now', calibration: { ...READY, tideMinutesLater: 12 },
        },
        workedOutAt: WORKED, failed: false,
      }
      render(<AnchorWatchDrawer {...baseProps} />)
      openTideInfo()

      expect(screen.getByTestId('low-water-calibration')).toHaveTextContent('12 min later')
    })

    it('marks the figures as not updated since after a failed poll', () => {
      mockLowWater = {
        lowWater: { status: 'ok', depthAtLowM: 2.4, clearanceM: 1.2, lowTideTime: LOW },
        workedOutAt: WORKED, failed: true,
      }
      render(<AnchorWatchDrawer {...baseProps} />)

      const line = screen.getByTestId('low-water-tide')
      expect(line).toHaveTextContent('Not updated since 4.31 AM')
      expect(screen.getByTestId('low-water-low')).not.toHaveTextContent('not updated')
      expect(line.className).toMatch(/opacity-70/)
    })

    it('leaves the shallowest spot off when it is not worth a line', () => {
      mockLowWater = {
        lowWater: {
          status: 'ok', depthAtLowM: 2.4, depthAtLowHalfWidthM: 0.2, clearanceM: 1.2, lowTideTime: LOW,
          cautiousSource: 'boat_now', calibration: { ...READY, shallowestDiffers: false },
        },
        workedOutAt: WORKED, failed: false,
      }
      render(<AnchorWatchDrawer {...baseProps} />)

      // Calibration text is still there, so the button is, but the spot is not worth a line.
      openTideInfo()
      expect(screen.getByTestId('low-water-calibration')).toBeInTheDocument()
      expect(screen.queryByTestId('low-water-shallowest')).not.toBeInTheDocument()
    })

    it('has no info button when there is no station name, calibration text or differing shallowest spot', () => {
      mockLowWater = {
        lowWater: { status: 'ok', depthAtLowM: 2.4, clearanceM: 1.2, lowTideTime: LOW },
        workedOutAt: WORKED, failed: false,
      }
      render(<AnchorWatchDrawer {...baseProps} />)

      expect(screen.queryByRole('button', { name: /^Tide details/ })).not.toBeInTheDocument()
    })

    it('turns the info button amber and says why only when a shallower spot was sounded', () => {
      mockLowWater = {
        lowWater: {
          status: 'ok', depthAtLowM: 2.4, depthAtLowHalfWidthM: 0.2, clearanceM: 1.2, lowTideTime: LOW,
          cautiousSource: 'boat_now', calibration: READY,
        },
        workedOutAt: WORKED, failed: false,
      }
      const { unmount } = render(<AnchorWatchDrawer {...baseProps} />)
      const amber = screen.getByRole('button', { name: 'Tide details, shallower spot sounded nearby' })
      expect(amber.className).toMatch(/text-amber/)
      unmount()

      mockLowWater = {
        lowWater: {
          status: 'ok', depthAtLowM: 2.4, depthAtLowHalfWidthM: 0.2, clearanceM: 1.2, lowTideTime: LOW,
          cautiousSource: 'boat_now', calibration: { ...READY, shallowestDiffers: false },
        },
        workedOutAt: WORKED, failed: false,
      }
      render(<AnchorWatchDrawer {...baseProps} />)
      const quiet = screen.getByRole('button', { name: 'Tide details' })
      expect(quiet.className).not.toMatch(/text-amber/)
    })

    // The button sits inside the map, whose Adjust handler reads Escape as Cancel.
    it('keeps Escape on the info button to itself while its popover is open', () => {
      mockLowWater = {
        lowWater: {
          status: 'ok', depthAtLowM: 2.4, depthAtLowHalfWidthM: 0.2, clearanceM: 1.2, lowTideTime: LOW,
          cautiousSource: 'boat_now', calibration: READY,
        },
        workedOutAt: WORKED, failed: false,
      }
      const onKeyDown = vi.fn()
      render(<div onKeyDown={onKeyDown}><AnchorWatchDrawer {...baseProps} /></div>)
      const button = screen.getByRole('button', { name: /^Tide details/ })

      fireEvent.keyDown(button, { key: 'Escape' })
      expect(onKeyDown).toHaveBeenCalledTimes(1)

      openTideInfo()
      fireEvent.keyDown(button, { key: 'Escape' })
      expect(onKeyDown).toHaveBeenCalledTimes(1)
    })

    it('fades the popover figures with the rest when the verdict is stale', () => {
      mockLowWater = {
        lowWater: {
          status: 'ok', depthAtLowM: 2.4, depthAtLowHalfWidthM: 0.2, clearanceM: 1.2, lowTideTime: LOW,
          cautiousSource: 'boat_now', calibration: READY,
        },
        workedOutAt: WORKED, failed: true,
      }
      render(<AnchorWatchDrawer {...baseProps} />)
      openTideInfo()

      expect(screen.getByTestId('low-water-calibration').className).toMatch(/italic/)
      expect(screen.getByTestId('low-water-shallowest').className).toMatch(/italic/)
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
          calibration: { ready: false, notReady: 'working_it_out', hoursOfReadings: 0, shallowestDiffers: false },
        },
        workedOutAt: WORKED, failed: false,
      }
      render(<AnchorWatchDrawer {...baseProps} />)
      openTideInfo()

      expect(screen.getByTestId('low-water-calibration')).toHaveTextContent('Calibration working out the calibration')
    })

    it('says to set a clearance when none is set', () => {
      mockLowWater = {
        lowWater: {
          status: 'ok', depthAtLowM: 2.4, clearanceM: 1.2, lowTideTime: LOW,
          calibration: { ready: false, notReady: 'no_clearance_set', hoursOfReadings: 3, shallowestDiffers: false },
        },
        workedOutAt: WORKED, failed: false,
      }
      render(<AnchorWatchDrawer {...baseProps} />)
      openTideInfo()

      expect(screen.getByTestId('low-water-calibration')).toHaveTextContent('set a low-water clearance in Anchor Watch settings to judge the calibration')
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

      expect(screen.getByTestId('low-water-low')).toHaveTextContent('Low 4.45 AM 7.9 ft ±0.7')
    })
  })
})
