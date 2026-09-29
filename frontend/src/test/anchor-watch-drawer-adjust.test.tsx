import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, act } from '@testing-library/react'
import { AnchorWatchDrawer } from '@/components/anchor-watch-drawer'
import type { TideToday } from '@/hooks/use-tide-today'

// Adjust mode (ADR 0136, radius decoupled from zoom and the bottom bar
// replaced by a corner toolbar + floating Escape/Save by ADR 0143) at the
// drawer level: the radius toolbar's disabled reasons, the warning
// double-tap, Escape/Save, and the 5s poll not resetting the draft. Run
// throughout without WebGL2 (mockHasWebGL2 = false) so the whole suite
// exercises the same radius/commit logic the with-map path shares, without
// needing to simulate map pan/zoom gestures — those are covered at the
// AnchorWatchMap level in anchor-watch-map-adjust.test.tsx. The header's own
// Adjust text button is Adjust's entry point in this path (see
// anchor-watch-drawer-header.test.tsx for its own gating tests).

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

vi.mock('@/lib/webgl', () => ({ hasWebGL2: () => false }))

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
  distanceMeters: 15,
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
  adjustAnchor: vi.fn(async () => undefined),
  onClearAnchor: () => undefined,
  isImperial: false,
  onDropAnchor: () => undefined,
  canDrop: true,
  anchorState: 'set' as const,
  rodeDeployedM: 0,
  seaState: 'calm' as const,
  seabedType: 'sand' as const,
  windSpeedApparentKts: null as number | null,
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
  vesselLengthOverallM: null as number | null,
  vesselDraftM: null as number | null,
  hasGpsFix: true,
  windBandId: null as string | null,
  onWindBandChange: () => undefined,
  onUpdateRodeAndConditions: async () => undefined,
  planningDepthM: null as number | null,
  planningTideHeightFt: null as number | null,
  onPlanningDepthChange: () => Promise.resolve(),
}

function openAdjust() {
  fireEvent.click(screen.getByRole('button', { name: 'Adjust' }))
}

beforeEach(() => { vi.clearAllMocks() })

describe('AnchorWatchDrawer Adjust mode (ADR 0136) — entry gating', () => {
  it('does not open with no anchor down — there is no entry point at all', () => {
    render(<AnchorWatchDrawer {...baseProps} anchorState="none" anchorLat={null} anchorLon={null} />)
    expect(screen.queryByRole('button', { name: 'Adjust' })).not.toBeInTheDocument()
    expect(screen.queryByTestId('anchor-adjust-actions')).not.toBeInTheDocument()
  })

  it('opens the radius toolbar and Escape/Save once anchored and the entry point is tapped', () => {
    render(<AnchorWatchDrawer {...baseProps} />)
    openAdjust()
    expect(screen.getByTestId('anchor-adjust-radius-toolbar')).toBeInTheDocument()
    expect(screen.getByTestId('anchor-adjust-actions')).toBeInTheDocument()
  })

  it('closing (raising the anchor mid-Adjust, or the watch disappearing) exits Adjust', () => {
    const { rerender } = render(<AnchorWatchDrawer {...baseProps} />)
    openAdjust()
    expect(screen.getByTestId('anchor-adjust-actions')).toBeInTheDocument()

    rerender(<AnchorWatchDrawer {...baseProps} anchorState="none" anchorLat={null} anchorLon={null} />)
    expect(screen.queryByTestId('anchor-adjust-actions')).not.toBeInTheDocument()
  })
})

describe('AnchorWatchDrawer Adjust mode — radius toolbar and disabled reasons', () => {
  it('shows the committed radius as the starting readout', () => {
    render(<AnchorWatchDrawer {...baseProps} radiusMeters={20} />)
    openAdjust()
    expect(screen.getByTestId('anchor-adjust-radius-value')).toHaveTextContent('20')
    expect(screen.getByTestId('anchor-adjust-radius-unit')).toHaveTextContent('m')
  })

  it('disables − at the minimum radius, with a reason surfaced as visible text', () => {
    render(<AnchorWatchDrawer {...baseProps} radiusMeters={5} />)
    openAdjust()
    const decrement = screen.getByRole('button', { name: 'Decrease radius' })
    expect(decrement).toBeDisabled()
    expect(screen.getByTestId('anchor-adjust-reasons')).toHaveTextContent('Minimum 5 m')
  })

  it('disables + at the maximum radius (chain onboard + LOA), with a reason surfaced as visible text', () => {
    // anchorConfig.chainOnboardM 50 + loaM 10 = 60.
    render(<AnchorWatchDrawer {...baseProps} radiusMeters={60} />)
    openAdjust()
    const increment = screen.getByRole('button', { name: 'Increase radius' })
    expect(increment).toBeDisabled()
    expect(screen.getByTestId('anchor-adjust-reasons')).toHaveTextContent('Maximum: Chain onboard + boat length')
  })

  it('names the ceiling differently with no boat length resolved', () => {
    render(<AnchorWatchDrawer {...baseProps} radiusMeters={50} anchorConfig={{ ...baseProps.anchorConfig, loaM: 0 }} vesselLengthOverallM={null} />)
    openAdjust()
    expect(screen.getByRole('button', { name: 'Increase radius' })).toBeDisabled()
    expect(screen.getByTestId('anchor-adjust-reasons')).toHaveTextContent('Maximum: Chain onboard, without boat length')
  })

  // Code-review finding carried forward: a committed radius above the
  // ceiling (possible from before the chain+LOA ceiling existed) must seed
  // Adjust with the CLAMPED draft, and say so.
  it('clamps a committed radius above the maximum on entry and says the original was above it', () => {
    // anchorConfig.chainOnboardM 50 + loaM 10 = 60.
    render(<AnchorWatchDrawer {...baseProps} radiusMeters={180} />)
    openAdjust()
    expect(screen.getByTestId('anchor-adjust-radius-value')).toHaveTextContent('60')
    expect(screen.getByText('Was 180 m, above the maximum')).toBeInTheDocument()
  })

  it('shows no above-maximum notice when the committed radius was already within bounds', () => {
    render(<AnchorWatchDrawer {...baseProps} radiusMeters={20} />)
    openAdjust()
    expect(screen.queryByText(/above the maximum/)).not.toBeInTheDocument()
  })

  it('steps the radius by 1 m on a single tap of +', () => {
    render(<AnchorWatchDrawer {...baseProps} radiusMeters={20} />)
    openAdjust()
    const increment = screen.getByRole('button', { name: 'Increase radius' })
    fireEvent.pointerDown(increment)
    fireEvent.pointerUp(increment)
    expect(screen.getByTestId('anchor-adjust-radius-value')).toHaveTextContent('21')
  })

  it('re-clamps the draft radius if alarmRadiusBounds narrows mid-session (e.g. a chain-onboard settings change)', () => {
    const { rerender } = render(<AnchorWatchDrawer {...baseProps} radiusMeters={55} />)
    openAdjust()
    expect(screen.getByTestId('anchor-adjust-radius-value')).toHaveTextContent('55')

    // Chain onboard drops from 50 to 20 mid-session (20 + loaM 10 = 30 ceiling).
    rerender(<AnchorWatchDrawer {...baseProps} radiusMeters={55} anchorConfig={{ ...baseProps.anchorConfig, chainOnboardM: 20 }} />)
    expect(screen.getByTestId('anchor-adjust-radius-value')).toHaveTextContent('30')
  })
})

describe('AnchorWatchDrawer Adjust mode — the warning and its double-tap', () => {
  it('shows no warning when the boat sits inside the draft radius plus the drag buffer', () => {
    // vessel/anchor are ~15m apart in baseProps; radius 20 + 4.572 buffer clears it.
    render(<AnchorWatchDrawer {...baseProps} radiusMeters={20} />)
    openAdjust()
    expect(screen.queryByTestId('anchor-adjust-warning')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Save' })).toBeInTheDocument()
  })

  it('shows the warning and requires a second tap to Save once the draft radius would leave the boat outside it', async () => {
    // ~15m vessel/anchor separation; radius 5 + buffer 4.572 = 9.572 < 15.
    render(<AnchorWatchDrawer {...baseProps} radiusMeters={5} />)
    openAdjust()
    expect(screen.getByTestId('anchor-adjust-warning')).toBeInTheDocument()

    const saveButton = screen.getByRole('button', { name: 'Save' })
    fireEvent.click(saveButton)
    expect(baseProps.adjustAnchor).not.toHaveBeenCalled()
    expect(screen.getByRole('button', { name: 'Save anyway' })).toBeInTheDocument()

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Save anyway' }))
      await Promise.resolve()
      await Promise.resolve()
    })
    // Position never moved in this no-map path (there's no camera to pan) —
    // lat/lon must be omitted, or the backend treats this radius-only Save
    // as a reposition (resets the self trail, requires a fresh SignalK
    // publish — code-review finding).
    expect(baseProps.adjustAnchor).toHaveBeenCalledWith({ radiusMeters: 5 })
  })
})

describe('AnchorWatchDrawer Adjust mode — Save and Cancel', () => {
  it('Save calls adjustAnchor with the draft and closes Adjust on success', async () => {
    render(<AnchorWatchDrawer {...baseProps} radiusMeters={20} />)
    openAdjust()

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Save' }))
      await Promise.resolve()
      await Promise.resolve()
    })

    // No pan happened in this no-map path — a plain Save with no radius or
    // position change must PATCH radius_meters alone (code-review finding).
    expect(baseProps.adjustAnchor).toHaveBeenCalledWith({ radiusMeters: 20 })
    expect(screen.queryByTestId('anchor-adjust-actions')).not.toBeInTheDocument()
  })

  it('leaves the page layout alone: header, Drop/Raise row and the Rode Planner stay on screen', () => {
    render(<AnchorWatchDrawer {...baseProps} radiusMeters={20} />)
    const plannerHandle = screen.getByRole('button', { name: /rode planner/i })
    openAdjust()
    expect(screen.getByTestId('anchor-watch-header')).not.toHaveClass('hidden')
    expect(screen.getByRole('button', { name: /rode planner/i })).toBe(plannerHandle)
    expect(screen.getByRole('button', { name: /raise/i })).toBeInTheDocument()
    expect(document.querySelector('.fixed.inset-0')).toBeNull()
  })

  it('Cancel writes nothing and closes Adjust', () => {
    render(<AnchorWatchDrawer {...baseProps} radiusMeters={20} />)
    openAdjust()

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))

    expect(baseProps.adjustAnchor).not.toHaveBeenCalled()
    expect(screen.queryByTestId('anchor-adjust-actions')).not.toBeInTheDocument()
  })

  it('a failed Save keeps Adjust open with the draft intact', async () => {
    const failingAdjustAnchor = vi.fn().mockRejectedValue(new Error('Request failed'))
    render(<AnchorWatchDrawer {...baseProps} radiusMeters={20} adjustAnchor={failingAdjustAnchor} />)
    openAdjust()

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Save' }))
      await Promise.resolve()
      await Promise.resolve()
    })

    expect(screen.getByTestId('anchor-adjust-actions')).toBeInTheDocument()
    expect(screen.getByTestId('anchor-adjust-radius-value')).toHaveTextContent('20')
  })
})

// Code-review finding: App.tsx substitutes the anchor point (or the
// backend's -1/-1 "no fix" sentinel) for the boat position when there's no
// live fix, so "Alarm would sound now" must not be evaluated against
// vesselLat/vesselLon without hasGpsFix confirming they're real.
describe('AnchorWatchDrawer Adjust mode — no GPS fix', () => {
  it('shows no warning and needs no second tap, even when the substituted position reads far outside the draft circle', () => {
    render(<AnchorWatchDrawer {...baseProps} radiusMeters={5} hasGpsFix={false} />)
    openAdjust()
    expect(screen.queryByTestId('anchor-adjust-warning')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Save' })).toBeInTheDocument()
  })

  it('shows a muted no-fix line instead of the warning', () => {
    render(<AnchorWatchDrawer {...baseProps} radiusMeters={5} hasGpsFix={false} />)
    openAdjust()
    expect(screen.getByText("No GPS fix: can't check the boat against this circle")).toBeInTheDocument()
  })

  it('a real fix (hasGpsFix defaults true) still shows the warning as before', () => {
    render(<AnchorWatchDrawer {...baseProps} radiusMeters={5} />)
    openAdjust()
    expect(screen.getByTestId('anchor-adjust-warning')).toBeInTheDocument()
    expect(screen.queryByText(/No GPS fix/)).not.toBeInTheDocument()
  })
})

describe('AnchorWatchDrawer Adjust mode — the 5s anchor-watch poll does not reset the draft', () => {
  it('a poll landing mid-Adjust (changed radiusMeters/anchorLat props) does not touch the already-adjusted draft', () => {
    const { rerender } = render(<AnchorWatchDrawer {...baseProps} radiusMeters={20} />)
    openAdjust()

    const increment = screen.getByRole('button', { name: 'Increase radius' })
    fireEvent.pointerDown(increment)
    fireEvent.pointerUp(increment)
    expect(screen.getByTestId('anchor-adjust-radius-value')).toHaveTextContent('21')

    // Simulates the poll's own props changing while Adjust stays open —
    // adjustActive itself is drawer-owned state, untouched by this rerender.
    rerender(<AnchorWatchDrawer {...baseProps} radiusMeters={35} anchorLat={-25.3} anchorLon={152.95} />)

    expect(screen.getByTestId('anchor-adjust-radius-value')).toHaveTextContent('21')
  })
})
