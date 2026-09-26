import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, within, fireEvent } from '@testing-library/react'
import { AnchorWatchDrawer } from '@/components/anchor-watch-drawer'
import type { TideToday } from '@/hooks/use-tide-today'

// ADR 0133's amendment (the anchor-adjust-sheet plan, part 1): Depth
// replaces Distance as the Anchor Watch page's hero KPI, with tide context
// beside it — the Distance hero and the interim Alarm Radius stepper (ADR
// 0133 Phase 1) are both gone from this header. Distance itself moved back
// into the map's own metrics panel as its top row (see
// anchor-watch-map-ui.test.tsx); the low-water clearance line (PR #39)
// moved in here from below the map.
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

let mockHasWebGL2 = true
vi.mock('@/lib/webgl', () => ({
  hasWebGL2: () => mockHasWebGL2,
}))

function makeTide(overrides: Partial<TideToday> = {}): TideToday {
  const now = Date.now()
  return {
    datetime: new Date(now - 5 * 60 * 1000).toISOString(),
    current_tide_height_ft: 3,
    tide_direction: 'Falling',
    // Both extremes in the future and unambiguous: low arrives sooner, so
    // it is "next" regardless of any anchor-state staleness rule — distinct
    // from anchor-watch-drawer-low-water-clearance.test.tsx's own fixture,
    // which only needs the low tide's own timing to be right.
    high_tide_time: new Date(now + 90 * 60 * 1000).toISOString(),
    high_tide_height_ft: 5,
    low_tide_time: new Date(now + 30 * 60 * 1000).toISOString(),
    low_tide_height_ft: 1,
    station_name: 'Goold Island',
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
  tide: null as TideToday | null,
  depthLastUpdateAgeS: null as number | null,
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
  vesselDraftM: null,
  hasGpsFix: true,
  windBandId: null,
  onWindBandChange: () => undefined,
  onUpdateRodeAndConditions: async () => undefined,
  planningDepthM: null,
  planningTideHeightFt: null,
  onPlanningDepthChange: () => Promise.resolve(),
}

describe('AnchorWatchDrawer header — removed controls', () => {
  it('no longer renders the old Distance hero KPI', () => {
    render(<AnchorWatchDrawer {...baseProps} />)
    expect(screen.queryByTestId('anchor-distance-kpi')).not.toBeInTheDocument()
  })

  it('no longer renders the interim Alarm Radius stepper', () => {
    render(<AnchorWatchDrawer {...baseProps} />)
    expect(screen.queryByTestId('anchor-radius-stepper')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Increase alarm radius' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Decrease alarm radius' })).not.toBeInTheDocument()
  })
})

describe('AnchorWatchDrawer header — depth hero', () => {
  it('shows the live depth as the hero readout, in metric', () => {
    render(<AnchorWatchDrawer {...baseProps} depthMeters={3.2} />)

    const header = screen.getByTestId('anchor-watch-header')
    expect(within(header).getByText('Depth')).toBeInTheDocument()
    expect(header).toHaveTextContent('3.2')
    expect(header).toHaveTextContent('m')
  })

  it('converts to feet under imperial units', () => {
    render(<AnchorWatchDrawer {...baseProps} depthMeters={3} isImperial />)

    const header = screen.getByTestId('anchor-watch-header')
    // 3 m * 3.28084 = 9.84252 ft
    expect(header).toHaveTextContent('9.8')
    expect(header).toHaveTextContent('ft')
  })

  it('shows a dash rather than a stale figure with no live depth reading', () => {
    render(<AnchorWatchDrawer {...baseProps} depthMeters={null} />)

    const header = screen.getByTestId('anchor-watch-header')
    expect(header).toHaveTextContent('—')
    expect(header).toHaveTextContent('unavailable')
  })

  it('flags the depth stale once the feed age passes the threshold, and grayscales the reading', () => {
    render(<AnchorWatchDrawer {...baseProps} depthMeters={3.2} depthLastUpdateAgeS={5940} />)

    const badge = screen.getByTestId('anchor-watch-depth-stale-badge')
    expect(badge).toHaveTextContent('1h 39m')
  })

  it('does not flag stale when no update age is known', () => {
    render(<AnchorWatchDrawer {...baseProps} depthMeters={3.2} depthLastUpdateAgeS={null} />)

    expect(screen.queryByTestId('anchor-watch-depth-stale-badge')).not.toBeInTheDocument()
  })
})

describe('AnchorWatchDrawer header — tide context', () => {
  it('labels the tide with the station name', () => {
    render(<AnchorWatchDrawer {...baseProps} tide={makeTide({ station_name: 'Goold Island' })} />)

    // The label reads "Tide" in the DOM and is capitalised visually by CSS
    // (text-transform: uppercase, the same convention the rest of this
    // header's own labels — "Depth" — and the map's metrics panel labels
    // already use), so this asserts case-insensitively rather than
    // requiring literal uppercase characters in the markup.
    const header = screen.getByTestId('anchor-watch-header')
    expect(header).toHaveTextContent(/tide/i)
    expect(header).toHaveTextContent(/goold island/i)
  })

  it('shows Falling with the falling arrow', () => {
    render(<AnchorWatchDrawer {...baseProps} tide={makeTide({ tide_direction: 'Falling' })} />)

    expect(screen.getByTestId('anchor-watch-header')).toHaveTextContent('Falling')
  })

  it('shows Rising with the rising arrow', () => {
    render(<AnchorWatchDrawer {...baseProps} tide={makeTide({ tide_direction: 'Rising' })} />)

    expect(screen.getByTestId('anchor-watch-header')).toHaveTextContent('Rising')
  })

  it('names the next turn (whichever of high/low comes first) with its height and time', () => {
    // low_tide_time (+30min) sorts before high_tide_time (+90min) in makeTide().
    const tide = makeTide()
    render(<AnchorWatchDrawer {...baseProps} tide={tide} />)

    const header = screen.getByTestId('anchor-watch-header')
    expect(header).toHaveTextContent('Low')
    expect(header).not.toHaveTextContent(/\bHigh\b(?!er)/)
    // low_tide_height_ft 1 ft -> 0.3 m
    expect(header).toHaveTextContent('0.3')
    const expectedTime = new Date(tide.low_tide_time).toLocaleTimeString('en-US', { hour: 'numeric', minute: '2-digit' })
    expect(header).toHaveTextContent(expectedTime)
  })

  it('shows the estimated depth at the next low when falling', () => {
    render(<AnchorWatchDrawer {...baseProps} depthMeters={4} tide={makeTide({ tide_direction: 'Falling' })} />)

    const header = screen.getByTestId('anchor-watch-header')
    // drop from current (3 ft) to low (1 ft) = 2 ft = 0.6096 m; 4 - 0.6096 = 3.3904 m
    expect(header).toHaveTextContent('Est. low')
    expect(header).toHaveTextContent('3.4')
  })

  it('shows the estimated depth at the next high when rising', () => {
    render(<AnchorWatchDrawer {...baseProps} depthMeters={4} tide={makeTide({ tide_direction: 'Rising' })} />)

    const header = screen.getByTestId('anchor-watch-header')
    // rise from current (3 ft) to high (5 ft) = 2 ft = 0.6096 m; 4 + 0.6096 = 4.6096 m
    expect(header).toHaveTextContent('Est. high')
    expect(header).toHaveTextContent('4.6')
  })

  it('says No tide station with no tide station configured', () => {
    render(<AnchorWatchDrawer {...baseProps} tide={null} />)

    const header = screen.getByTestId('anchor-watch-header')
    expect(header).toHaveTextContent('No tide station')
  })
})

describe('AnchorWatchDrawer header — low-water clearance moved in', () => {
  it('still renders the low-water clearance line, now inside the header', () => {
    render(<AnchorWatchDrawer {...baseProps} depthMeters={2} vesselDraftM={1.2} tide={makeTide()} />)

    const header = screen.getByTestId('anchor-watch-header')
    expect(within(header).getByTestId('low-water-clearance')).toBeInTheDocument()
  })
})

describe('AnchorWatchDrawer header — no-WebGL2 Adjust text button (ADR 0136)', () => {
  afterEach(() => { mockHasWebGL2 = true })

  it('shows a header Adjust text button when there is no WebGL2 and an anchor is down', () => {
    mockHasWebGL2 = false
    render(<AnchorWatchDrawer {...baseProps} />)

    expect(screen.getByRole('button', { name: 'Adjust' })).toBeInTheDocument()
  })

  it('does not show the text button when WebGL2 is available — the map icon is the entry point instead', () => {
    mockHasWebGL2 = true
    render(<AnchorWatchDrawer {...baseProps} />)

    expect(screen.queryByRole('button', { name: 'Adjust' })).not.toBeInTheDocument()
  })

  it('does not show the text button with no anchor down — there is nothing to adjust', () => {
    mockHasWebGL2 = false
    render(<AnchorWatchDrawer {...baseProps} anchorState="none" anchorLat={null} anchorLon={null} />)

    expect(screen.queryByRole('button', { name: 'Adjust' })).not.toBeInTheDocument()
  })

  it('opens the bar (radius only, with the no-map notice) when tapped', () => {
    mockHasWebGL2 = false
    render(<AnchorWatchDrawer {...baseProps} />)

    fireEvent.click(screen.getByRole('button', { name: 'Adjust' }))

    expect(screen.getByTestId('anchor-adjust-bar')).toBeInTheDocument()
    expect(screen.getByTestId('anchor-adjust-bar')).toHaveTextContent('Moving the anchor needs the map')
  })
})
