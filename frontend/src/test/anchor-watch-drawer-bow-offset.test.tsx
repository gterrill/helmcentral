import { beforeEach, describe, it, expect, vi } from 'vitest'
import { fireEvent, render, screen, within } from '@testing-library/react'
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
  vesselDraftM: null,
  hasGpsFix: true,
  windBandId: null,
  onWindBandChange: () => undefined,
  onUpdateRodeAndConditions: async () => undefined,
  planningDepthM: null,
  planningTideHeightFt: null,
  onPlanningDepthChange: () => Promise.resolve(),
}

// The routine "corrected forward of GPS" banner is gone from the top of the
// drawer. The one case that still needs saying, a mark left at the GPS because
// there was no heading at Drop, sits in the readings.
const NO_HEADING_CARD = 'Anchor mark is at the GPS, not the bow. There was no heading at Drop.'
const NO_HEADING = `${NO_HEADING_CARD} Move it with Adjust.`

// The rode planner's Swing is rode + GPS-to-bow + boat. The server reports a
// bow offset of 0 before Drop, after a Drop with no heading, and after the
// mark is placed by hand, but the GPS still sits that far from the bow, so the
// configured offset must still count.
describe('AnchorWatchDrawer swing includes the GPS to bow offset', () => {
  const wind = {
    windSpeedApparentKts: 12,
    maxGustKts: { '10m': null, '30m': null, '1h': 20, '24h': null },
    planningDepthM: 5,
    planningTideHeightFt: 2,
  }

  beforeEach(() => {
    localStorage.clear()
  })

  function swingLine(): string {
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))
    fireEvent.click(screen.getByRole('tab', { name: /ratio/i }))
    return within(screen.getByRole('tabpanel')).getByText(/ rode/).textContent ?? ''
  }

  it.each([
    ['before Drop', { anchorState: 'none' as const, bowOffsetReason: '' }],
    ['after the mark is placed by hand', { bowOffsetReason: 'placed by hand in Adjust' }],
  ])('uses the configured offset once %s', (_name, extra) => {
    render(<AnchorWatchDrawer {...baseProps} {...wind} bowOffsetApplied={false} bowOffsetM={0} {...extra} />)

    expect(swingLine()).toMatch(/ rode \+ 2 m GPS to bow \+ 10 m boat$/)
  })

  it('uses the configured offset twice after a Drop with no heading, the mark being at the antenna', () => {
    render(<AnchorWatchDrawer {...baseProps} {...wind} bowOffsetApplied={false} bowOffsetM={0} bowOffsetReason="heading unavailable" />)

    expect(swingLine()).toMatch(/ rode \+ 2 × 2 m GPS to bow \+ 10 m boat$/)
  })

  it('uses the offset twice after a Drop made before the distance was set, once it is set later', () => {
    render(
      <AnchorWatchDrawer
        {...baseProps}
        {...wind}
        bowOffsetApplied={false}
        bowOffsetM={0}
        bowOffsetReason="gps_from_bow_m not configured"
      />,
    )

    expect(swingLine()).toMatch(/ rode \+ 2 × 2 m GPS to bow \+ 10 m boat$/)
  })

  it('keeps the offset the Drop applied', () => {
    render(<AnchorWatchDrawer {...baseProps} {...wind} bowOffsetApplied bowOffsetM={8} bowOffsetReason="" />)

    expect(swingLine()).toMatch(/ rode \+ 8 m GPS to bow \+ 10 m boat$/)
  })
})

describe('AnchorWatchDrawer bow-offset readout', () => {
  it('has no corrected-forward banner when the offset was applied', () => {
    render(<AnchorWatchDrawer {...baseProps} bowOffsetApplied bowOffsetM={8} bowOffsetReason="" />)

    expect(screen.queryByText(/forward of GPS/i)).toBeNull()
    expect(screen.queryByText(NO_HEADING)).toBeNull()
  })

  it('warns in the map panel when the offset was not applied for want of a heading', () => {
    render(
      <AnchorWatchDrawer {...baseProps} bowOffsetApplied={false} bowOffsetM={0} bowOffsetReason="heading unavailable" />,
    )

    const note = screen.getByRole('note')
    expect(note).toHaveTextContent(NO_HEADING)
    expect(screen.getByTestId('anchor-watch-hud-header')).toContainElement(note)
    expect(screen.queryByText(/not bow-corrected/i)).toBeNull()
  })

  it('warns in the readings card when there is no map', () => {
    render(
      <AnchorWatchDrawer
        {...baseProps}
        vesselLat={null}
        vesselLon={null}
        hasGpsFix={false}
        bowOffsetApplied={false}
        bowOffsetM={0}
        bowOffsetReason="heading unavailable"
      />,
    )

    expect(screen.getByTestId('anchor-watch-header')).toContainElement(screen.getByText(NO_HEADING_CARD))
    expect(screen.queryByText(/Move it with Adjust/)).toBeNull()
  })

  it('shows nothing when the offset was never configured', () => {
    render(<AnchorWatchDrawer {...baseProps} bowOffsetApplied={false} bowOffsetM={0} bowOffsetReason="" />)

    expect(screen.queryByText(NO_HEADING)).toBeNull()
    expect(screen.queryByText(NO_HEADING_CARD)).toBeNull()
    expect(screen.queryByText(/forward of GPS/i)).toBeNull()
  })
})
