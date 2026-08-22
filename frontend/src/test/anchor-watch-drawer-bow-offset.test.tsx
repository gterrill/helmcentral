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
  anchorLat: -25.2938,
  anchorLon: 152.9102,
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
  onAnchorReposition: () => undefined,
  onRadiusChange: () => undefined,
  onClearAnchor: () => undefined,
  isImperial: false,
  isAutoCloseArmed: false,
  motoringSecondsElapsed: 0,
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
    windageAreaM2: 20,
    gpsFromBowM: 2,
    loaM: 10,
  },
  onUpdateRodeAndConditions: async () => undefined,
}

// Test 9: the drawer shows the corrected-by-d readout when the backend
// applied the bow offset, and an explicit warning when it didn't because no
// heading was available — never silence about a missing correction.
describe('AnchorWatchDrawer bow-offset readout', () => {
  it('shows the corrected-by-d readout when applied', () => {
    render(<AnchorWatchDrawer {...baseProps} bowOffsetApplied bowOffsetM={8} bowOffsetReason="" />)

    expect(screen.getByText(/corrected 8 ?m forward of GPS/i)).toBeInTheDocument()
  })

  it('shows an explicit warning when configured but not applied (no heading)', () => {
    render(
      <AnchorWatchDrawer
        {...baseProps}
        bowOffsetApplied={false}
        bowOffsetM={0}
        bowOffsetReason="heading unavailable"
      />,
    )

    expect(screen.getByText(/not bow-corrected/i)).toBeInTheDocument()
    expect(screen.getByText(/no heading from SignalK/i)).toBeInTheDocument()
  })

  it('shows nothing when the offset was never configured', () => {
    render(<AnchorWatchDrawer {...baseProps} bowOffsetApplied={false} bowOffsetM={0} bowOffsetReason="" />)

    expect(screen.queryByText(/bow-corrected/i)).toBeNull()
    expect(screen.queryByText(/corrected .* forward of GPS/i)).toBeNull()
  })
})
