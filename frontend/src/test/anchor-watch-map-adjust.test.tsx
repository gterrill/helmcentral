import { act, fireEvent, render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi, afterEach } from 'vitest'
import { AnchorWatchMap, type AnchorAdjustPosition } from '@/components/anchor-watch-map'
import { haversineMeters, bearingDeg as geoBearingDeg } from '@/lib/geo'

// Adjust mode (ADR 0136, decoupled from zoom by ADR 0143) at the
// AnchorWatchMap level: Move icon gating and its pressed/toggle state, the
// fixed crosshair and the geographic draft-radius circle (no more
// fixed-screen-size ring), camera behaviour on entry (a one-time fit, no
// ongoing min/max zoom clamp — zoom is free), the corner radius toolbar, and
// keyboard scoped to the map container rather than window.

vi.mock('maplibre-gl', () => ({ default: {} }))

let mockHasWebGL2 = true
vi.mock('@/lib/webgl', () => ({ hasWebGL2: () => mockHasWebGL2 }))

const easeToMock = vi.fn()
const jumpToMock = vi.fn()
const setMinZoomMock = vi.fn()
const setMaxZoomMock = vi.fn()
const resizeMock = vi.fn()
const panByMock = vi.fn()
const disableRotationMock = vi.fn()
const enableRotationMock = vi.fn()

// Captured so a test can fire a synthetic 'move' event directly — real
// MapLibre calls this on every animation frame of a pan/pinch AND of a
// programmatic easeTo/jumpTo transition, which this inert mock (unlike the
// real Map) never does on its own.
let latestOnMove: ((e: { viewState: { latitude: number; longitude: number; zoom: number }; originalEvent?: unknown }) => void) | undefined

vi.mock('react-map-gl/maplibre', async () => {
  const React = await import('react')
  return {
    Map: React.forwardRef(
      (
        { children, onKeyDown, onMove }: {
          children?: React.ReactNode
          onKeyDown?: (e: unknown) => void
          onMove?: (e: { viewState: { latitude: number; longitude: number; zoom: number }; originalEvent?: unknown }) => void
        },
        ref: React.Ref<unknown>,
      ) => {
        void onKeyDown
        latestOnMove = onMove
        React.useImperativeHandle(ref, () => ({
          getCanvas: () => ({ style: { cursor: 'grab' } }),
          getZoom: () => 14,
          easeTo: easeToMock,
          jumpTo: jumpToMock,
          getMap: () => ({
            isStyleLoaded: () => true,
            getSource: () => ({}),
            setMinZoom: setMinZoomMock,
            setMaxZoom: setMaxZoomMock,
            jumpTo: jumpToMock,
            resize: resizeMock,
            panBy: panByMock,
            getZoom: () => 14,
            touchZoomRotate: { disableRotation: disableRotationMock, enableRotation: enableRotationMock },
          }),
        }))
        return <div data-testid="map-root">{children}</div>
      },
    ),
    Marker: ({ children, onClick }: { children?: React.ReactNode; onClick?: () => void }) => (
      <div onClick={onClick}>{children}</div>
    ),
    Source: ({ children, id }: { children?: React.ReactNode; id: string }) => (
      <div data-testid={`source-${id}`}>{children}</div>
    ),
    Layer: ({ id }: { id: string }) => <div data-testid={`layer-${id}`} />,
  }
})

const baseProps = {
  vesselLat: -25.29,
  vesselLon: 152.91,
  vesselHeadingDeg: 45,
  anchorLat: -25.2938,
  anchorLon: 152.9102,
  radiusMeters: 20,
  depthMeters: 3,
  currentDriftKts: null,
  currentSetDeg: null,
  distanceMeters: 10,
  bearingDeg: 80,
  scopeRecommendation: null,
  isImperial: false,
  vesselTrail: () => [],
  aisVessels: [],
  aisTrails: () => new Map(),
  isDarkTheme: false,
}

describe('AnchorWatchMap Adjust mode (ADR 0136) — entry gating', () => {
  afterEach(() => { mockHasWebGL2 = true; vi.clearAllMocks() })

  it('shows no Move icon when the caller does not pass onAdjust (the dashboard tile, the kiosk)', () => {
    render(<AnchorWatchMap {...baseProps} />)
    expect(screen.queryByRole('button', { name: 'Adjust anchor' })).not.toBeInTheDocument()
  })

  it('shows no Move icon with no anchor down, even when onAdjust is given', () => {
    render(<AnchorWatchMap {...baseProps} anchorLat={null} anchorLon={null} onAdjust={() => {}} />)
    expect(screen.queryByRole('button', { name: 'Adjust anchor' })).not.toBeInTheDocument()
  })

  it('shows the Move icon once there is a map, an anchor, and onAdjust — and clicking it calls onAdjust', () => {
    const onAdjust = vi.fn()
    render(<AnchorWatchMap {...baseProps} onAdjust={onAdjust} />)
    const button = screen.getByRole('button', { name: 'Adjust anchor' })
    fireEvent.click(button)
    expect(onAdjust).toHaveBeenCalledTimes(1)
  })

  it('shows no Move icon at all without WebGL2 — the header text button is that state\'s entry point instead', () => {
    mockHasWebGL2 = false
    render(<AnchorWatchMap {...baseProps} onAdjust={() => {}} />)
    expect(screen.queryByRole('button', { name: 'Adjust anchor' })).not.toBeInTheDocument()
  })
})

describe('AnchorWatchMap Adjust mode — the icon stays visible, pressed, and toggles off', () => {
  afterEach(() => vi.clearAllMocks())

  it('carries aria-pressed=false when inactive and true while active', () => {
    const { rerender } = render(<AnchorWatchMap {...baseProps} onAdjust={() => {}} adjustActive={false} />)
    expect(screen.getByRole('button', { name: 'Adjust anchor' })).toHaveAttribute('aria-pressed', 'false')
    rerender(<AnchorWatchMap {...baseProps} onAdjust={() => {}} adjustActive onAdjustCancelKey={() => {}} />)
    expect(screen.getByRole('button', { name: 'Adjust anchor' })).toHaveAttribute('aria-pressed', 'true')
  })

  it('stays on screen while Adjust is active, and so does the rest of the control stack', () => {
    render(<AnchorWatchMap {...baseProps} onAdjust={() => {}} adjustActive onAdjustCancelKey={() => {}} expandedControls />)
    expect(screen.getByRole('button', { name: 'Adjust anchor' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Zoom in' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Re-centre on anchor' })).toBeInTheDocument()
  })

  it('clicking the icon while active calls onAdjustCancelKey (behaves like Escape), not onAdjust', () => {
    const onAdjust = vi.fn()
    const onAdjustCancelKey = vi.fn()
    render(<AnchorWatchMap {...baseProps} onAdjust={onAdjust} adjustActive onAdjustCancelKey={onAdjustCancelKey} />)
    fireEvent.click(screen.getByRole('button', { name: 'Adjust anchor' }))
    expect(onAdjustCancelKey).toHaveBeenCalledTimes(1)
    expect(onAdjust).not.toHaveBeenCalled()
  })
})

describe('AnchorWatchMap Adjust mode — camera on entry, and free zoom', () => {
  afterEach(() => vi.clearAllMocks())

  it('centres on the anchor on entry without touching the zoom, and disables touch rotation', () => {
    render(<AnchorWatchMap {...baseProps} adjustActive adjustRadiusM={20} />)
    expect(disableRotationMock).toHaveBeenCalled()
    expect(jumpToMock).toHaveBeenCalledWith({ center: [152.9102, -25.2938] })
  })

  it('never calls setMinZoom/setMaxZoom — zoom is not clamped by the radius any more', () => {
    render(<AnchorWatchMap {...baseProps} adjustActive adjustRadiusM={20} />)
    expect(setMinZoomMock).not.toHaveBeenCalled()
    expect(setMaxZoomMock).not.toHaveBeenCalled()
  })

  it('restores rotation on exit', () => {
    const { rerender } = render(<AnchorWatchMap {...baseProps} adjustActive onAdjustCancelKey={() => {}} />)
    vi.clearAllMocks()
    rerender(<AnchorWatchMap {...baseProps} adjustActive={false} />)
    expect(enableRotationMock).toHaveBeenCalled()
  })

  it('never restores/focuses on the very first mount (adjustActive starts false)', () => {
    render(<AnchorWatchMap {...baseProps} adjustActive={false} />)
    expect(enableRotationMock).not.toHaveBeenCalled()
    expect(jumpToMock).not.toHaveBeenCalled()
  })

  it('a zoom-only move (a pinch, no pan) reports no radius step and does not itself move the draft position', () => {
    const onAdjustPositionChange = vi.fn()
    const onAdjustRadiusStep = vi.fn()
    render(
      <AnchorWatchMap
        {...baseProps}
        adjustActive
        adjustRadiusM={20}
        onAdjustPositionChange={onAdjustPositionChange}
        onAdjustRadiusStep={onAdjustRadiusStep}
      />,
    )
    onAdjustPositionChange.mockClear()
    act(() => {
      latestOnMove?.({ viewState: { latitude: -25.2938, longitude: 152.9102, zoom: 17 }, originalEvent: new WheelEvent('wheel') })
    })
    expect(onAdjustRadiusStep).not.toHaveBeenCalled()
    // Position is unchanged (still the anchor), but the callback contract is
    // "reports the live centre" — zoom changing alone must never touch the
    // radius, which onAdjustRadiusStep not firing already proves.
    expect(onAdjustPositionChange).toHaveBeenCalledWith({ lat: -25.2938, lon: 152.9102 })
  })

  it('a pan reports the new centre as the draft position', () => {
    const onAdjustPositionChange = vi.fn()
    render(<AnchorWatchMap {...baseProps} adjustActive adjustRadiusM={20} onAdjustPositionChange={onAdjustPositionChange} />)
    onAdjustPositionChange.mockClear()
    act(() => {
      latestOnMove?.({ viewState: { latitude: -25.30, longitude: 152.95, zoom: 15 }, originalEvent: new WheelEvent('wheel') })
    })
    expect(onAdjustPositionChange).toHaveBeenCalledWith({ lat: -25.30, lon: 152.95 })
  })
})

describe('AnchorWatchMap Adjust mode — overlays', () => {
  afterEach(() => vi.clearAllMocks())

  it('renders the fixed crosshair and the geographic draft circle, and hides the ordinary anchor marker', () => {
    render(<AnchorWatchMap {...baseProps} adjustActive adjustRadiusM={20} />)
    expect(screen.getByTestId('anchor-adjust-crosshair')).toBeInTheDocument()
    expect(screen.getByTestId('source-adjust-draft-circle')).toBeInTheDocument()
    expect(screen.queryByLabelText('Anchor position')).not.toBeInTheDocument()
    // The faded original-anchor reference marker takes its place.
    expect(screen.getByLabelText('Original anchor position')).toBeInTheDocument()
  })

  it('reports the anchor as the starting draft position on entry', () => {
    const onAdjustPositionChange = vi.fn()
    render(<AnchorWatchMap {...baseProps} adjustActive adjustRadiusM={20} onAdjustPositionChange={onAdjustPositionChange} />)
    expect(onAdjustPositionChange).toHaveBeenCalledWith({ lat: -25.2938, lon: 152.9102 })
  })

  it('the 5s anchor-watch poll does not reset the camera or the draft while Adjust stays open', () => {
    const onAdjustPositionChange = vi.fn()
    const { rerender } = render(
      <AnchorWatchMap {...baseProps} adjustActive adjustRadiusM={20} onAdjustPositionChange={onAdjustPositionChange} />,
    )
    expect(onAdjustPositionChange).toHaveBeenCalledTimes(1)
    vi.clearAllMocks()
    // A poll landing with a changed committed radius/position — adjustActive
    // itself hasn't changed, so the entry effect must not re-fire.
    rerender(
      <AnchorWatchMap
        {...baseProps}
        adjustActive
        adjustRadiusM={20}
        anchorLat={-25.3}
        anchorLon={152.95}
        radiusMeters={99}
        onAdjustPositionChange={onAdjustPositionChange}
      />,
    )
    expect(jumpToMock).not.toHaveBeenCalled()
    expect(onAdjustPositionChange).not.toHaveBeenCalled()
  })
})

describe('AnchorWatchMap Adjust mode — keyboard scoped to the map container', () => {
  afterEach(() => vi.clearAllMocks())

  it('a keydown on window does nothing', () => {
    const onAdjustCancelKey = vi.fn()
    const onAdjustSetKey = vi.fn()
    render(<AnchorWatchMap {...baseProps} adjustActive onAdjustCancelKey={onAdjustCancelKey} onAdjustSetKey={onAdjustSetKey} />)
    fireEvent.keyDown(window, { key: 'Escape' })
    fireEvent.keyDown(window, { key: 'Enter' })
    expect(onAdjustCancelKey).not.toHaveBeenCalled()
    expect(onAdjustSetKey).not.toHaveBeenCalled()
  })

  it('Escape on the map container calls onAdjustCancelKey', () => {
    const onAdjustCancelKey = vi.fn()
    render(<AnchorWatchMap {...baseProps} adjustActive onAdjustCancelKey={onAdjustCancelKey} />)
    fireEvent.keyDown(screen.getByTestId('anchor-watch-map-wrapper'), { key: 'Escape' })
    expect(onAdjustCancelKey).toHaveBeenCalledTimes(1)
  })

  it('Enter on the map container calls onAdjustSetKey', () => {
    const onAdjustSetKey = vi.fn()
    render(<AnchorWatchMap {...baseProps} adjustActive onAdjustSetKey={onAdjustSetKey} />)
    fireEvent.keyDown(screen.getByTestId('anchor-watch-map-wrapper'), { key: 'Enter' })
    expect(onAdjustSetKey).toHaveBeenCalledTimes(1)
  })

  it('arrow keys pan the camera (panBy) rather than doing nothing', () => {
    render(<AnchorWatchMap {...baseProps} adjustActive />)
    fireEvent.keyDown(screen.getByTestId('anchor-watch-map-wrapper'), { key: 'ArrowUp' })
    expect(panByMock).toHaveBeenCalled()
  })

  it('+ and - step the radius by radiusStepM via onAdjustRadiusStep, not by touching zoom', () => {
    const onAdjustRadiusStep = vi.fn()
    render(<AnchorWatchMap {...baseProps} adjustActive onAdjustRadiusStep={onAdjustRadiusStep} />)
    // The entry effect's own one-time camera fit already fired on mount —
    // clear it so this only asserts about the keyboard +/- that follows.
    easeToMock.mockClear()
    jumpToMock.mockClear()
    const wrapper = screen.getByTestId('anchor-watch-map-wrapper')
    fireEvent.keyDown(wrapper, { key: '+' })
    fireEvent.keyDown(wrapper, { key: '-' })
    expect(onAdjustRadiusStep).toHaveBeenNthCalledWith(1, 1)
    expect(onAdjustRadiusStep).toHaveBeenNthCalledWith(2, -1)
    expect(easeToMock).not.toHaveBeenCalled()
    expect(jumpToMock).not.toHaveBeenCalled()
  })

  it('the map container is not keyboard-scoped outside Adjust mode', () => {
    render(<AnchorWatchMap {...baseProps} adjustActive={false} />)
    const wrapper = screen.getByTestId('anchor-watch-map-wrapper')
    expect(wrapper).not.toHaveAttribute('tabindex')
  })
})

describe('AnchorWatchMap Adjust mode — the corner radius toolbar', () => {
  afterEach(() => vi.clearAllMocks())

  it('is absent when Adjust is inactive', () => {
    render(<AnchorWatchMap {...baseProps} onAdjust={() => {}} adjustActive={false} />)
    expect(screen.queryByTestId('anchor-adjust-radius-toolbar')).not.toBeInTheDocument()
  })

  it('appears under the Adjust icon once Adjust is active, showing the draft radius', () => {
    render(
      <AnchorWatchMap
        {...baseProps}
        onAdjust={() => {}}
        adjustActive
        adjustRadiusM={24}
        onAdjustCancelKey={() => {}}
      />,
    )
    expect(screen.getByTestId('anchor-adjust-radius-toolbar')).toBeInTheDocument()
    expect(screen.getByTestId('anchor-adjust-radius-value')).toHaveTextContent('24')
  })

  it('tapping + or - calls onAdjustRadiusStep with the signed step, disabled state reflects the atMin/atMax props', () => {
    const onAdjustRadiusStep = vi.fn()
    render(
      <AnchorWatchMap
        {...baseProps}
        onAdjust={() => {}}
        adjustActive
        adjustRadiusM={24}
        adjustRadiusAtMin={false}
        adjustRadiusAtMax
        adjustRadiusMaxDisabledReason="Chain onboard + boat length"
        onAdjustRadiusStep={onAdjustRadiusStep}
        onAdjustCancelKey={() => {}}
      />,
    )
    const increment = screen.getByRole('button', { name: 'Increase radius' })
    expect(increment).toBeDisabled()
    const decrement = screen.getByRole('button', { name: 'Decrease radius' })
    fireEvent.pointerDown(decrement)
    fireEvent.pointerUp(decrement)
    expect(onAdjustRadiusStep).toHaveBeenCalledWith(-1)
  })
})

// Type-only sanity check that the position shape exported for hosts matches
// what the component actually reports — a compile-time guard, not a runtime
// assertion.
function _typeCheck(position: AnchorAdjustPosition) {
  return position.lat + position.lon
}
void _typeCheck

describe('AnchorWatchMap Adjust mode — the data cards follow the draft', () => {
  afterEach(() => vi.clearAllMocks())

  const metricValue = (label: string) => {
    const row = within(screen.getByTestId('anchor-watch-metrics')).getByText(label).parentElement!
    return row.textContent!.replace(label, '')
  }

  it('keeps the cards on screen and shows distance/bearing from the draft anchor to the boat, and the draft radius', () => {
    render(<AnchorWatchMap {...baseProps} adjustActive adjustRadiusM={35} onAdjustCancelKey={() => {}} />)
    act(() => {
      latestOnMove?.({ viewState: { latitude: -25.2950, longitude: 152.9080, zoom: 17 }, originalEvent: new MouseEvent('mousemove') })
    })
    const expectedDistance = Math.round(haversineMeters(-25.2950, 152.9080, baseProps.vesselLat, baseProps.vesselLon))
    const expectedBearing = Math.round(geoBearingDeg(-25.2950, 152.9080, baseProps.vesselLat, baseProps.vesselLon))
    expect(metricValue('Distance')).toBe(`${expectedDistance}m`)
    expect(metricValue('Bearing')).toBe(`${expectedBearing}°`)
    expect(metricValue('Radius')).toBe('35m')
  })

  it('shows a dash for distance and bearing when the boat has no live position', () => {
    render(<AnchorWatchMap {...baseProps} distanceMeters={null} bearingDeg={null} adjustActive adjustRadiusM={35} onAdjustCancelKey={() => {}} />)
    expect(metricValue('Distance')).toBe('—m')
    expect(metricValue('Bearing')).toBe('—°')
  })
})

describe('AnchorWatchMap Adjust mode — keys pressed on a map button', () => {
  afterEach(() => vi.clearAllMocks())

  it('Enter on a control button activates that button and does not Save', () => {
    const onAdjustSetKey = vi.fn()
    const onAdjustRadiusStep = vi.fn()
    render(
      <AnchorWatchMap
        {...baseProps}
        onAdjust={() => {}}
        adjustActive
        adjustRadiusM={20}
        onAdjustSetKey={onAdjustSetKey}
        onAdjustCancelKey={() => {}}
        onAdjustRadiusStep={onAdjustRadiusStep}
      />,
    )
    fireEvent.keyDown(screen.getByRole('button', { name: 'Increase radius' }), { key: 'Enter' })
    fireEvent.keyDown(screen.getByRole('button', { name: 'Adjust anchor' }), { key: 'Enter' })
    fireEvent.keyDown(screen.getByRole('button', { name: 'Zoom in' }), { key: '+' })
    expect(onAdjustSetKey).not.toHaveBeenCalled()
    expect(onAdjustRadiusStep).not.toHaveBeenCalled()
  })

  it('Escape on a control button still cancels', () => {
    const onAdjustCancelKey = vi.fn()
    render(<AnchorWatchMap {...baseProps} onAdjust={() => {}} adjustActive adjustRadiusM={20} onAdjustCancelKey={onAdjustCancelKey} />)
    fireEvent.keyDown(screen.getByRole('button', { name: 'Zoom in' }), { key: 'Escape' })
    expect(onAdjustCancelKey).toHaveBeenCalledTimes(1)
  })
})
