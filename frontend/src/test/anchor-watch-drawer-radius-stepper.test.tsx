import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { AnchorWatchDrawer } from '@/components/anchor-watch-drawer'
import { AnchorRequestError } from '@/lib/anchor-request'
import { toast } from 'sonner'

// Interim radius control (Phase 1 of the anchor-adjust-sheet plan): the
// operator's only way to change the alarm radius this release, since the
// map's own edge-drag editing is gone (impeccable P0: it fired a radius PATCH
// on an ordinary tap). Each press is one immediate updateRadius call — no
// hold-to-repeat until the Phase 2 Adjust sheet.
vi.mock('sonner', () => ({ toast: { error: vi.fn() } }))

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
  onPlanningDepthChange: () => undefined,
}

describe('AnchorWatchDrawer radius stepper', () => {
  beforeEach(() => {
    vi.mocked(toast.error).mockClear()
  })

  it('sends radiusMeters + 5 on Increase, in metric', () => {
    const onRadiusChange = vi.fn().mockResolvedValue(undefined)
    render(<AnchorWatchDrawer {...baseProps} radiusMeters={20} onRadiusChange={onRadiusChange} />)

    fireEvent.click(screen.getByRole('button', { name: 'Increase alarm radius' }))
    expect(onRadiusChange).toHaveBeenCalledWith(25)
  })

  it('sends radiusMeters - 5 on Decrease, in metric', () => {
    const onRadiusChange = vi.fn().mockResolvedValue(undefined)
    render(<AnchorWatchDrawer {...baseProps} radiusMeters={20} onRadiusChange={onRadiusChange} />)

    fireEvent.click(screen.getByRole('button', { name: 'Decrease alarm radius' }))
    expect(onRadiusChange).toHaveBeenCalledWith(15)
  })

  it('sends one updateRadius call per press, not more', async () => {
    const onRadiusChange = vi.fn().mockResolvedValue(undefined)
    render(<AnchorWatchDrawer {...baseProps} radiusMeters={20} onRadiusChange={onRadiusChange} />)

    fireEvent.click(screen.getByRole('button', { name: 'Increase alarm radius' }))
    expect(onRadiusChange).toHaveBeenCalledTimes(1)
  })

  it('steps up by 15 ft (converted back to metres) under imperial units', () => {
    const onRadiusChange = vi.fn().mockResolvedValue(undefined)
    // 20 m starting point; step is 15 ft = 4.572 m exactly.
    render(<AnchorWatchDrawer {...baseProps} radiusMeters={20} isImperial onRadiusChange={onRadiusChange} />)

    fireEvent.click(screen.getByRole('button', { name: 'Increase alarm radius' }))
    expect(onRadiusChange).toHaveBeenCalledWith(24.572)
  })

  it('steps down by 15 ft (converted back to metres) under imperial units', () => {
    const onRadiusChange = vi.fn().mockResolvedValue(undefined)
    render(<AnchorWatchDrawer {...baseProps} radiusMeters={20} isImperial onRadiusChange={onRadiusChange} />)

    fireEvent.click(screen.getByRole('button', { name: 'Decrease alarm radius' }))
    expect(onRadiusChange).toHaveBeenCalledWith(15.428)
  })

  it('clamps Decrease at the 5 m floor rather than going negative', () => {
    const onRadiusChange = vi.fn().mockResolvedValue(undefined)
    render(<AnchorWatchDrawer {...baseProps} radiusMeters={7} onRadiusChange={onRadiusChange} />)

    fireEvent.click(screen.getByRole('button', { name: 'Decrease alarm radius' }))
    expect(onRadiusChange).toHaveBeenCalledWith(5)
  })

  it('does not go below the 5 m floor even when already at it', () => {
    const onRadiusChange = vi.fn().mockResolvedValue(undefined)
    render(<AnchorWatchDrawer {...baseProps} radiusMeters={5} onRadiusChange={onRadiusChange} />)

    fireEvent.click(screen.getByRole('button', { name: 'Decrease alarm radius' }))
    expect(onRadiusChange).toHaveBeenCalledWith(5)
  })

  it('shows a Retry toast with the failure message on a network-ish error, and Retry re-sends the same value', async () => {
    const onRadiusChange = vi.fn()
      .mockRejectedValueOnce(new Error('network down'))
      .mockResolvedValueOnce(undefined)
    render(<AnchorWatchDrawer {...baseProps} radiusMeters={20} onRadiusChange={onRadiusChange} />)

    fireEvent.click(screen.getByRole('button', { name: 'Increase alarm radius' }))

    await vi.waitFor(() => expect(toast.error).toHaveBeenCalledTimes(1))
    expect(toast.error).toHaveBeenCalledWith('Could not set alarm radius', {
      description: 'network down',
      action: { label: 'Retry', onClick: expect.any(Function) },
    })

    const call = vi.mocked(toast.error).mock.calls[0]
    const options = call[1] as unknown as { action: { onClick: () => void } }
    options.action.onClick()

    await vi.waitFor(() => expect(onRadiusChange).toHaveBeenCalledTimes(2))
    expect(onRadiusChange).toHaveBeenNthCalledWith(2, 25)
  })

  // code-review finding: applyRadius's catch used to discard the rejection
  // entirely — a hardcoded title, no description, and Retry offered
  // unconditionally even for a failure retrying could never fix (a bad
  // radius, or no active watch to PATCH — both 4xx from the backend).
  it('shows the server message and offers no Retry for a non-retryable (4xx) failure', async () => {
    const onRadiusChange = vi.fn().mockRejectedValueOnce(new AnchorRequestError('no active anchor watch', 404))
    render(<AnchorWatchDrawer {...baseProps} radiusMeters={20} onRadiusChange={onRadiusChange} />)

    fireEvent.click(screen.getByRole('button', { name: 'Increase alarm radius' }))

    await vi.waitFor(() => expect(toast.error).toHaveBeenCalledTimes(1))
    expect(toast.error).toHaveBeenCalledWith('Could not set alarm radius', {
      description: 'no active anchor watch',
      action: undefined,
    })
  })

  it('offers Retry for a retryable 5xx failure, with the server message', async () => {
    const onRadiusChange = vi.fn()
      .mockRejectedValueOnce(new AnchorRequestError('SignalK publish failed', 502))
      .mockResolvedValueOnce(undefined)
    render(<AnchorWatchDrawer {...baseProps} radiusMeters={20} onRadiusChange={onRadiusChange} />)

    fireEvent.click(screen.getByRole('button', { name: 'Increase alarm radius' }))

    await vi.waitFor(() => expect(toast.error).toHaveBeenCalledTimes(1))
    expect(toast.error).toHaveBeenCalledWith('Could not set alarm radius', {
      description: 'SignalK publish failed',
      action: { label: 'Retry', onClick: expect.any(Function) },
    })
  })

  it('does not toast on a successful radius change', async () => {
    const onRadiusChange = vi.fn().mockResolvedValue(undefined)
    render(<AnchorWatchDrawer {...baseProps} radiusMeters={20} onRadiusChange={onRadiusChange} />)

    fireEvent.click(screen.getByRole('button', { name: 'Increase alarm radius' }))
    await vi.waitFor(() => expect(onRadiusChange).toHaveBeenCalledTimes(1))

    expect(toast.error).not.toHaveBeenCalled()
  })

  // code-review bug: radiusMeters (the prop) only updates once the server
  // echoes the change back — it does not move on a mere press. Stepping
  // from that stale prop meant three quick presses all computed "20 + 5"
  // and sent 25 three times in a row, instead of walking 25, 30, 35.
  it('steps from the latest requested target while a save is in flight, not the stale radiusMeters prop', () => {
    const onRadiusChange = vi.fn().mockReturnValue(new Promise<void>(() => {})) // never resolves in this test
    render(<AnchorWatchDrawer {...baseProps} radiusMeters={20} onRadiusChange={onRadiusChange} />)

    const increase = screen.getByRole('button', { name: 'Increase alarm radius' })
    fireEvent.click(increase)
    fireEvent.click(increase)
    fireEvent.click(increase)

    expect(onRadiusChange).toHaveBeenCalledTimes(3)
    expect(onRadiusChange).toHaveBeenNthCalledWith(1, 25)
    expect(onRadiusChange).toHaveBeenNthCalledWith(2, 30)
    expect(onRadiusChange).toHaveBeenNthCalledWith(3, 35)
  })

  it('shows the pending target in the readout while a save is in flight', () => {
    const onRadiusChange = vi.fn().mockReturnValue(new Promise<void>(() => {}))
    render(<AnchorWatchDrawer {...baseProps} radiusMeters={20} onRadiusChange={onRadiusChange} />)

    fireEvent.click(screen.getByRole('button', { name: 'Increase alarm radius' }))

    expect(screen.getByTestId('anchor-radius-stepper')).toHaveTextContent('25')
  })

  // code-review finding: pendingRadiusM was only ever cleared by an effect
  // keyed on the radiusMeters prop *changing*. A request whose target equals
  // the value already showing (pressing - at the 5 m floor, where
  // max(5, 5-5) is still 5) settles with the prop never moving, so that
  // effect never fires and pendingRadiusM is stuck at 5 forever. A later,
  // genuinely different radius from elsewhere (e.g. the Rode Planner's
  // "Apply as alarm radius") then gets hidden behind the stale pending value.
  it('picks up a later external radius change even after a settled request whose target equalled the value already showing', async () => {
    const onRadiusChange = vi.fn().mockResolvedValue(undefined)
    const { rerender } = render(<AnchorWatchDrawer {...baseProps} radiusMeters={5} onRadiusChange={onRadiusChange} />)

    fireEvent.click(screen.getByRole('button', { name: 'Decrease alarm radius' }))
    expect(onRadiusChange).toHaveBeenCalledWith(5)
    await vi.waitFor(() => expect(screen.getByTestId('anchor-radius-stepper')).toHaveTextContent('5'))

    // The Rode Planner applies a genuinely different radius; App.tsx passes
    // the new server-echoed value down as a prop change.
    rerender(<AnchorWatchDrawer {...baseProps} radiusMeters={40} onRadiusChange={onRadiusChange} />)

    expect(screen.getByTestId('anchor-radius-stepper')).toHaveTextContent('40')
  })

  // code-review finding: a request still in flight (or stuck pending, per
  // the settle bug above) when the anchor is raised belonged to a session
  // that no longer exists — re-dropping must not resume from it.
  it('clears a pending target on Raise, so a re-drop starts from the new watch\'s own radius', () => {
    const onRadiusChange = vi.fn().mockReturnValue(new Promise<void>(() => {})) // never resolves
    const { rerender } = render(<AnchorWatchDrawer {...baseProps} radiusMeters={20} onRadiusChange={onRadiusChange} />)

    fireEvent.click(screen.getByRole('button', { name: 'Increase alarm radius' }))
    expect(screen.getByTestId('anchor-radius-stepper')).toHaveTextContent('25')

    // Raise: anchorState goes to 'none', the stepper itself unmounts.
    rerender(<AnchorWatchDrawer {...baseProps} anchorState="none" radiusMeters={20} onRadiusChange={onRadiusChange} />)
    // Re-drop, at the new watch's own default radius — nothing left over
    // from the raised session's in-flight request.
    rerender(<AnchorWatchDrawer {...baseProps} anchorState="set" radiusMeters={20} onRadiusChange={onRadiusChange} />)

    expect(screen.getByTestId('anchor-radius-stepper')).toHaveTextContent('20')
  })

  it('resets the base to the server value once a pending request fails, rather than stepping from the failed target', async () => {
    const onRadiusChange = vi.fn().mockRejectedValueOnce(new Error('network down'))
    render(<AnchorWatchDrawer {...baseProps} radiusMeters={20} onRadiusChange={onRadiusChange} />)

    fireEvent.click(screen.getByRole('button', { name: 'Increase alarm radius' }))
    await vi.waitFor(() => expect(toast.error).toHaveBeenCalledTimes(1))

    // radiusMeters (the server value) never moved from 20 — the failed
    // PATCH for 25 never landed. Stepping again must resume from 20, not
    // from 25 (the value that just failed).
    onRadiusChange.mockResolvedValueOnce(undefined)
    fireEvent.click(screen.getByRole('button', { name: 'Increase alarm radius' }))
    expect(onRadiusChange).toHaveBeenNthCalledWith(2, 25)
  })
})
