import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, within, fireEvent, act } from '@testing-library/react'
import { toast } from 'sonner'
import { AnchorWatchDrawer } from '@/components/anchor-watch-drawer'
import type { TideToday } from '@/hooks/use-tide-today'
import { zoomForRingRadius, ringRadiusPx } from '@/lib/anchor-adjust'

// Only used by the Undo/restore describe block further down — every other
// test in this file predates any toast-driven flow, so mocking this file-wide
// (vi.mock is hoisted regardless of where it's written) has no effect on them.
vi.mock('sonner', () => ({ toast: Object.assign(vi.fn(), { error: vi.fn() }) }))

// sonner's own ExternalToast type allows `action` to be a plain ReactNode as
// well as an {label, onClick} object; useAnchorAdjustCommit only ever passes
// the object form (mirrors use-anchor-adjust-commit.test.ts's own helper),
// so this narrows to it once rather than threading `as` casts through every
// call site below.
interface ToastActionOptions {
  action: { label: string; onClick: () => void }
}
function actionOf(options: unknown): ToastActionOptions['action'] {
  return (options as ToastActionOptions).action
}

// ADR 0133's amendment (the anchor-adjust-sheet plan, part 1): Depth
// replaces Distance as the Anchor Watch page's hero KPI, with tide context
// beside it — the Distance hero and the interim Alarm Radius stepper (ADR
// 0133 Phase 1) are both gone from this header. Distance itself moved back
// into the map's own metrics panel as its top row (see
// anchor-watch-map-ui.test.tsx); the low-water clearance line (PR #39)
// moved in here from below the map.
const easeToMock = vi.fn()
const jumpToMock = vi.fn()
// Captured so a test can fire a synthetic 'move' event directly — a genuine
// pan/pinch gesture, distinguished from a programmatic easeTo/jumpTo frame
// by carrying a real originalEvent (see anchor-watch-map.tsx's own
// handleAdjustMove).
let latestOnMove: ((e: { viewState: { latitude: number; longitude: number; zoom: number }; originalEvent?: unknown }) => void) | undefined
vi.mock('react-map-gl/maplibre', async () => {
  const React = await import('react')
  return {
    Map: React.forwardRef(
      (
        { children, onMove }: {
          children?: React.ReactNode
          onMove?: (e: { viewState: { latitude: number; longitude: number; zoom: number }; originalEvent?: unknown }) => void
        },
        ref: React.Ref<unknown>,
      ) => {
        latestOnMove = onMove
        React.useImperativeHandle(ref, () => ({
          getCanvas: () => ({ style: { cursor: 'grab' } }),
          getZoom: () => 14,
          easeTo: easeToMock,
          jumpTo: jumpToMock,
          getMap: () => ({
            isStyleLoaded: () => true,
            getSource: () => ({}),
            setMinZoom: () => undefined,
            setMaxZoom: () => undefined,
            jumpTo: jumpToMock,
            resize: () => undefined,
            panBy: () => undefined,
            getZoom: () => 14,
            touchZoomRotate: { disableRotation: () => undefined, enableRotation: () => undefined },
          }),
        }))
        return <div data-testid="map-root">{children}</div>
      },
    ),
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

  // Code-review finding: tideToday sends the -1 height sentinel alongside an
  // empty time for an extreme the provider has none of - the old behaviour
  // (a fabricated "now" time) rendered here as a fake "High · <now>". A
  // configured station with neither extreme left to report must fall back
  // to the same quiet "No tide station" message as no station at all,
  // rather than inventing a turn.
  it('falls back to "No tide station" when the tide has neither a real high nor a real low', () => {
    const tide = makeTide({ high_tide_time: '', high_tide_height_ft: -1, low_tide_time: '', low_tide_height_ft: -1 })
    render(<AnchorWatchDrawer {...baseProps} tide={tide} />)

    const header = screen.getByTestId('anchor-watch-header')
    expect(header).toHaveTextContent('No tide station')
  })

  it('still names the one real extreme when only the other is missing', () => {
    const tide = makeTide({ low_tide_time: '', low_tide_height_ft: -1 })
    render(<AnchorWatchDrawer {...baseProps} tide={tide} />)

    const header = screen.getByTestId('anchor-watch-header')
    expect(header).toHaveTextContent('High')
    expect(header).not.toHaveTextContent('Low')
  })

  it('shows a negative next-turn height rather than hiding it', () => {
    // low_tide_time (+30min) sorts before high_tide_time (+90min), so the
    // low is next - a real reading below chart datum must still show its
    // figure, not just the word "Low" with nothing after it.
    const tide = makeTide({ low_tide_height_ft: -0.4 })
    render(<AnchorWatchDrawer {...baseProps} tide={tide} />)

    const header = screen.getByTestId('anchor-watch-header')
    // -0.4 ft -> -0.12 m (metric, isImperial false in baseProps)
    expect(header).toHaveTextContent('-0.1')
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

// Code-review finding: the bar's own +/- drove the map's easeTo (150ms),
// but a rapid second tap/hold — usePressRepeat's 80ms repeat interval — used
// to recompute its next target from adjustDraft.radiusM, which only updates
// once that ease reports back. Two steps with nothing landing in between
// (this mock's easeTo never fires a synthetic 'move' event, exactly like a
// real ease still in flight) is the scenario that used to drift off the
// step grid; the bar now reads AnchorWatchMap's own pending-target handle
// instead (getAdjustRadiusTarget) so it continues from where the previous
// step was actually headed.
describe('AnchorWatchDrawer Adjust bar — repeated steps land on the exact grid (with a map)', () => {
  afterEach(() => {
    vi.clearAllMocks()
    vi.restoreAllMocks()
  })

  it("bases the bar's second step on the last commanded target, not the still-easing draft", () => {
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(() => ({
      width: 400,
      height: 300,
      top: 0,
      left: 0,
      right: 400,
      bottom: 300,
      x: 0,
      y: 0,
      toJSON: () => {},
    }))
    render(<AnchorWatchDrawer {...baseProps} radiusMeters={20} />)
    fireEvent.click(screen.getByRole('button', { name: 'Adjust anchor' }))

    const increment = screen.getByRole('button', { name: 'Increase radius' })
    fireEvent.pointerDown(increment)
    fireEvent.pointerUp(increment)
    fireEvent.pointerDown(increment)
    fireEvent.pointerUp(increment)

    expect(easeToMock).toHaveBeenCalledTimes(2)
    const ringPx = ringRadiusPx(300) // the short side of the 400x300 stubbed rect
    const firstZoom = easeToMock.mock.calls[0][0].zoom
    const secondZoom = easeToMock.mock.calls[1][0].zoom
    expect(firstZoom).toBeCloseTo(zoomForRingRadius(21, -25.2938, ringPx), 5)
    // Without the fix, this second tap re-derives from the draft (still
    // reporting 20 m) and lands back on 21 m again instead of 22 m.
    expect(secondZoom).toBeCloseTo(zoomForRingRadius(22, -25.2938, ringPx), 5)
  })
})

// Code-review finding (round 2): Undo re-sends the pre-Adjust position as
// another position-changing PATCH, which the backend's own "placed by hand
// in Adjust" defaults would otherwise stamp onto it - wrongly marking a
// genuine bow-corrected drop as unapplied and losing its resolved place
// name, even though Undo is putting the anchor back exactly where it was.
// The drawer now captures the watch's own per-point facts the instant
// Adjust opens (adjustOpenedFromRef) and threads them onto Undo's own
// target as `restore` (buildAdjustCommitTargets), never Set's.
describe('AnchorWatchDrawer Adjust — Undo carries the pre-Adjust per-point facts (restore)', () => {
  afterEach(() => {
    vi.clearAllMocks()
    vi.restoreAllMocks()
    latestOnMove = undefined
  })

  it("Undo of a move sends restore with the pre-Adjust bow-offset/heading/place-name facts, and Set itself never carries restore", async () => {
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(() => ({
      width: 400, height: 300, top: 0, left: 0, right: 400, bottom: 300, x: 0, y: 0, toJSON: () => {},
    }))
    const adjustAnchorMock = vi.fn().mockResolvedValue(undefined)
    render(
      <AnchorWatchDrawer
        {...baseProps}
        radiusMeters={20}
        bowOffsetM={8}
        bowOffsetApplied
        bowOffsetReason=""
        headingAtSetDeg={45}
        placeName="Goldsmith Island"
        adjustAnchor={adjustAnchorMock}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Adjust anchor' }))

    // A genuine gesture (a real originalEvent) that moves the draft position
    // but keeps the same radius (20 m) — isolates the position-vs-facts
    // behaviour from the warning/double-tap mechanic.
    const ringPx = ringRadiusPx(300)
    act(() => {
      latestOnMove?.({
        viewState: {
          latitude: baseProps.anchorLat + 0.00002,
          longitude: baseProps.anchorLon,
          zoom: zoomForRingRadius(20, baseProps.anchorLat, ringPx),
        },
        originalEvent: new WheelEvent('wheel'),
      })
    })

    fireEvent.click(screen.getByRole('button', { name: /^Set/ }))
    await act(async () => { await Promise.resolve() })

    expect(adjustAnchorMock).toHaveBeenCalledTimes(1)
    const setTarget = adjustAnchorMock.mock.calls[0][0]
    expect(setTarget.lat).toBeCloseTo(baseProps.anchorLat + 0.00002, 6)
    expect(setTarget.restore).toBeUndefined() // Set itself never carries restore

    adjustAnchorMock.mockClear()
    const [, options] = vi.mocked(toast).mock.calls[0]
    const undoAction = actionOf(options)
    await act(async () => { undoAction.onClick() })

    expect(adjustAnchorMock).toHaveBeenCalledTimes(1)
    const undoTarget = adjustAnchorMock.mock.calls[0][0]
    expect(undoTarget.lat).toBe(baseProps.anchorLat)
    expect(undoTarget.lon).toBe(baseProps.anchorLon)
    expect(undoTarget.restore).toEqual({
      bowOffsetM: 8,
      bowOffsetApplied: true,
      bowOffsetReason: '',
      headingAtSetDeg: 45,
      placeName: 'Goldsmith Island',
    })
  })

  it('Undo of a radius-only Set sends no position and no restore', async () => {
    const adjustAnchorMock = vi.fn().mockResolvedValue(undefined)
    render(
      <AnchorWatchDrawer
        {...baseProps}
        radiusMeters={20}
        bowOffsetM={8}
        bowOffsetApplied
        bowOffsetReason=""
        headingAtSetDeg={45}
        placeName="Goldsmith Island"
        adjustAnchor={adjustAnchorMock}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Adjust anchor' }))
    const increment = screen.getByRole('button', { name: 'Increase radius' })
    fireEvent.pointerDown(increment)
    fireEvent.pointerUp(increment)

    fireEvent.click(screen.getByRole('button', { name: /^Set/ }))
    await act(async () => { await Promise.resolve() })

    expect(adjustAnchorMock).toHaveBeenCalledTimes(1)
    const setTarget = adjustAnchorMock.mock.calls[0][0]
    expect(setTarget.lat).toBeUndefined()
    expect(setTarget.restore).toBeUndefined()

    adjustAnchorMock.mockClear()
    const [, options] = vi.mocked(toast).mock.calls[0]
    const undoAction = actionOf(options)
    await act(async () => { undoAction.onClick() })

    expect(adjustAnchorMock).toHaveBeenCalledTimes(1)
    const undoTarget = adjustAnchorMock.mock.calls[0][0]
    expect(undoTarget.lat).toBeUndefined()
    expect(undoTarget.lon).toBeUndefined()
    expect(undoTarget.restore).toBeUndefined()
  })
})
