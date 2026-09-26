import { createRef } from 'react'
import { act, fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi, afterEach } from 'vitest'
import { AnchorWatchMap, type AnchorWatchMapHandle, type AnchorAdjustDraft } from '@/components/anchor-watch-map'
import { zoomForRingRadius, ringRadiusPx } from '@/lib/anchor-adjust'

// Adjust mode (ADR 0136) at the AnchorWatchMap level: Move icon gating, the
// fixed crosshair/ring overlay, camera lock (setMinZoom/setMaxZoom,
// touchZoomRotate), and keyboard scoped to the map container rather than
// window. Follows the mocking pattern already established in
// anchor-watch-map-ui.test.tsx and anchor-watch-drawer-header.test.tsx.

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
          // Mirrors real react-map-gl: MapRef proxies convenience methods
          // (jumpTo/easeTo) straight to the underlying instance, the same
          // one getMap().jumpTo below reaches — attemptFit (anchor-watch-
          // map.tsx's own zoom-to-fit dance) calls this top-level jumpTo
          // directly, not through getMap(), and needs the mock to answer
          // once a test gives the wrapper a real (non-zero) measured rect.
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

describe('AnchorWatchMap Adjust mode — camera lock and overlays', () => {
  afterEach(() => vi.clearAllMocks())

  it('locks the camera (zoom bounds, disables touch rotation) and jumps to the anchor on entry', () => {
    render(<AnchorWatchMap {...baseProps} adjustActive adjustRadiusBounds={{ minM: 5, maxM: 162, maxReason: null }} />)
    expect(setMinZoomMock).toHaveBeenCalled()
    expect(setMaxZoomMock).toHaveBeenCalled()
    expect(disableRotationMock).toHaveBeenCalled()
    expect(jumpToMock).toHaveBeenCalledWith(expect.objectContaining({ center: [152.9102, -25.2938] }))
  })

  it('restores the camera bounds and re-enables rotation on exit', () => {
    const { rerender } = render(<AnchorWatchMap {...baseProps} adjustActive />)
    vi.clearAllMocks()
    rerender(<AnchorWatchMap {...baseProps} adjustActive={false} />)
    expect(setMinZoomMock).toHaveBeenCalledWith(10)
    expect(enableRotationMock).toHaveBeenCalled()
  })

  it('never restores/focuses on the very first mount (adjustActive starts false)', () => {
    render(<AnchorWatchMap {...baseProps} adjustActive={false} />)
    expect(setMinZoomMock).not.toHaveBeenCalled()
    expect(enableRotationMock).not.toHaveBeenCalled()
  })

  it('renders the fixed crosshair and the screen-space ring, and hides the ordinary anchor marker', () => {
    render(<AnchorWatchMap {...baseProps} adjustActive />)
    expect(screen.getByTestId('anchor-adjust-crosshair')).toBeInTheDocument()
    expect(screen.getByTestId('anchor-adjust-ring')).toBeInTheDocument()
    expect(screen.queryByLabelText('Anchor position')).not.toBeInTheDocument()
    // The faded original-anchor reference marker takes its place.
    expect(screen.getByLabelText('Original anchor position')).toBeInTheDocument()
  })

  it('reports the committed position/radius as the starting draft on entry', () => {
    const onAdjustDraftChange = vi.fn()
    render(<AnchorWatchMap {...baseProps} adjustActive onAdjustDraftChange={onAdjustDraftChange} />)
    expect(onAdjustDraftChange).toHaveBeenCalledWith({ lat: -25.2938, lon: 152.9102, radiusM: 20 })
  })

  // Code-review finding: the entry effect reported the raw committed radius
  // as the starting draft, overwriting the clamped draft the drawer's own
  // openAdjust already produced (openAdjust's own clampRadiusM call) —
  // saved 250 m against a 162 m chain+LOA ceiling showed a 162 m ring but
  // reported (and so would Set/PATCH) 250 m.
  it('clamps the draft radius to the adjust bounds ceiling on entry, not the raw committed radius', () => {
    const onAdjustDraftChange = vi.fn()
    render(
      <AnchorWatchMap
        {...baseProps}
        radiusMeters={250}
        adjustActive
        adjustRadiusBounds={{ minM: 5, maxM: 162, maxReason: null }}
        onAdjustDraftChange={onAdjustDraftChange}
      />,
    )
    expect(onAdjustDraftChange).toHaveBeenCalledWith({ lat: -25.2938, lon: 152.9102, radiusM: 162 })
  })

  it('the 5s anchor-watch poll does not reset the camera or the draft while Adjust stays open', () => {
    const onAdjustDraftChange = vi.fn()
    const { rerender } = render(<AnchorWatchMap {...baseProps} adjustActive onAdjustDraftChange={onAdjustDraftChange} />)
    expect(onAdjustDraftChange).toHaveBeenCalledTimes(1)
    vi.clearAllMocks()
    // A poll landing with a changed committed radius/position — adjustActive
    // itself hasn't changed, so the entry effect must not re-fire.
    rerender(<AnchorWatchMap {...baseProps} adjustActive anchorLat={-25.3} anchorLon={152.95} radiusMeters={99} onAdjustDraftChange={onAdjustDraftChange} />)
    expect(jumpToMock).not.toHaveBeenCalled()
    expect(onAdjustDraftChange).not.toHaveBeenCalled()
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

  it('the map container is not keyboard-scoped outside Adjust mode', () => {
    render(<AnchorWatchMap {...baseProps} adjustActive={false} />)
    const wrapper = screen.getByTestId('anchor-watch-map-wrapper')
    expect(wrapper).not.toHaveAttribute('tabindex')
  })
})

describe('AnchorWatchMap Adjust mode — imperative setAdjustRadius', () => {
  afterEach(() => vi.clearAllMocks())

  it('eases the camera zoom when the host (the bottom bar) calls setAdjustRadius', () => {
    const ref = createRef<AnchorWatchMapHandle>()
    render(<AnchorWatchMap {...baseProps} ref={ref} adjustActive adjustRadiusBounds={{ minM: 5, maxM: 162, maxReason: null }} />)
    easeToMock.mockClear()
    ref.current?.setAdjustRadius(40)
    expect(easeToMock).toHaveBeenCalledWith(expect.objectContaining({ zoom: expect.any(Number) }))
  })

  it('does nothing when Adjust is not active', () => {
    const ref = createRef<AnchorWatchMapHandle>()
    render(<AnchorWatchMap {...baseProps} ref={ref} adjustActive={false} />)
    easeToMock.mockClear()
    ref.current?.setAdjustRadius(40)
    expect(easeToMock).not.toHaveBeenCalled()
  })
})

// Container resize mid-Adjust (code-review finding): a browser window
// resize, a tile resize, or a kiosk rotation used to update only the ring's
// own pixel size, leaving the zoom bounds and the reported draft radius
// stale until the next pan/pinch silently snapped them to whatever the new
// ring now represented — a resize the operator didn't ask for must never
// itself change the radius Set would commit.
describe('AnchorWatchMap Adjust mode — container resize keeps the chosen radius', () => {
  afterEach(() => {
    vi.clearAllMocks()
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('recomputes the ring and zoom bounds and re-aims the camera so the radius does not drift on resize', () => {
    const observerCallbacks: Array<() => void> = []
    class FakeResizeObserver {
      constructor(cb: () => void) { observerCallbacks.push(cb) }
      observe() {}
      disconnect() {}
    }
    vi.stubGlobal('ResizeObserver', FakeResizeObserver)

    let rectWidth = 400
    let rectHeight = 300
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(() => ({
      width: rectWidth,
      height: rectHeight,
      top: 0,
      left: 0,
      right: rectWidth,
      bottom: rectHeight,
      x: 0,
      y: 0,
      toJSON: () => {},
    }))

    const onAdjustDraftChange = vi.fn()
    render(
      <AnchorWatchMap
        {...baseProps}
        adjustActive
        adjustRadiusBounds={{ minM: 5, maxM: 200, maxReason: null }}
        onAdjustDraftChange={onAdjustDraftChange}
      />,
    )

    expect(onAdjustDraftChange).toHaveBeenLastCalledWith({ lat: -25.2938, lon: 152.9102, radiusM: 20 })
    setMinZoomMock.mockClear()
    setMaxZoomMock.mockClear()
    jumpToMock.mockClear()
    onAdjustDraftChange.mockClear()

    // The container's short side doubles (a rotation, a browser resize, the
    // phone full-screen layout taking over) — simulate the ResizeObserver
    // firing.
    rectWidth = 800
    rectHeight = 600
    act(() => { observerCallbacks.forEach((cb) => cb()) })

    // The zoom bounds must be recomputed against the NEW ring, not left at
    // whatever the entry effect originally set.
    expect(setMinZoomMock).toHaveBeenCalled()
    expect(setMaxZoomMock).toHaveBeenCalled()
    // The camera re-aims (a bigger ring needs a smaller zoom for the same
    // ground radius) rather than leaving the old zoom in place under a
    // resized ring.
    expect(jumpToMock).toHaveBeenCalledWith(expect.objectContaining({ zoom: expect.any(Number) }))
    // Above all: the resize itself must not change the radius the operator
    // chose.
    expect(onAdjustDraftChange).toHaveBeenLastCalledWith({ lat: -25.2938, lon: 152.9102, radiusM: 20 })
  })

  // Code-review finding: the resize handler re-reported the existing draft
  // radius unclamped, so if the bounds narrow while Adjust is open (a
  // settings change to chain onboard/LOA landing mid-session) a resize would
  // keep re-asserting an over-ceiling radius instead of pulling it back down
  // to the new maximum, the same way entry now does.
  it('clamps the reported draft radius to a narrower bounds on resize', () => {
    const observerCallbacks: Array<() => void> = []
    class FakeResizeObserver {
      constructor(cb: () => void) { observerCallbacks.push(cb) }
      observe() {}
      disconnect() {}
    }
    vi.stubGlobal('ResizeObserver', FakeResizeObserver)

    let rectWidth = 400
    let rectHeight = 300
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(() => ({
      width: rectWidth,
      height: rectHeight,
      top: 0,
      left: 0,
      right: rectWidth,
      bottom: rectHeight,
      x: 0,
      y: 0,
      toJSON: () => {},
    }))

    const onAdjustDraftChange = vi.fn()
    const { rerender } = render(
      <AnchorWatchMap
        {...baseProps}
        radiusMeters={150}
        adjustActive
        adjustRadiusBounds={{ minM: 5, maxM: 200, maxReason: null }}
        onAdjustDraftChange={onAdjustDraftChange}
      />,
    )
    expect(onAdjustDraftChange).toHaveBeenLastCalledWith({ lat: -25.2938, lon: 152.9102, radiusM: 150 })
    onAdjustDraftChange.mockClear()

    // Bounds narrow below the draft's own radius (e.g. chain onboard/LOA
    // changed mid-session) — adjustActive itself doesn't change, so nothing
    // re-fires the entry effect; the resize handler is what has to catch it.
    rerender(
      <AnchorWatchMap
        {...baseProps}
        radiusMeters={150}
        adjustActive
        adjustRadiusBounds={{ minM: 5, maxM: 90, maxReason: null }}
        onAdjustDraftChange={onAdjustDraftChange}
      />,
    )
    rectWidth = 800
    rectHeight = 600
    act(() => { observerCallbacks.forEach((cb) => cb()) })

    expect(onAdjustDraftChange).toHaveBeenLastCalledWith({ lat: -25.2938, lon: 152.9102, radiusM: 90 })
  })
})

// Code-review finding: applyAdjustRadius eases the zoom over 150ms, but
// usePressRepeat (the bar's own +/- buttons) fires every 80ms and the map's
// own keyboard +/- computes its next target from the CURRENT draft — which,
// mid-ease, is whatever the last 'move' event reported, not necessarily the
// last commanded target. A held +/- could therefore land off the exact step
// grid (74/78/83 ft instead of 75/80/85). This mock's easeTo never fires a
// synthetic 'move' event (matching a real ease still in flight when the next
// tick arrives before the previous one settles), so two rapid steps with
// nothing in between is exactly the scenario that used to drift.
describe('AnchorWatchMap Adjust mode — repeated radius steps land on the exact step grid', () => {
  afterEach(() => {
    vi.clearAllMocks()
    vi.restoreAllMocks()
  })

  it('bases each keyboard step on the last commanded target, not the still-easing draft', () => {
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
    render(
      <AnchorWatchMap
        {...baseProps}
        radiusMeters={20}
        adjustActive
        adjustRadiusBounds={{ minM: 5, maxM: 200, maxReason: null }}
      />,
    )
    easeToMock.mockClear()

    const wrapper = screen.getByTestId('anchor-watch-map-wrapper')
    fireEvent.keyDown(wrapper, { key: '+' })
    fireEvent.keyDown(wrapper, { key: '+' })

    expect(easeToMock).toHaveBeenCalledTimes(2)
    const ringPx = ringRadiusPx(300) // the short side of the 400x300 stubbed rect
    const firstZoom = easeToMock.mock.calls[0][0].zoom
    const secondZoom = easeToMock.mock.calls[1][0].zoom
    expect(firstZoom).toBeCloseTo(zoomForRingRadius(21, -25.2938, ringPx), 5)
    // Without the fix, this second step re-derives from the draft (still
    // reporting 20 m, since no 'move' event landed in between) and lands
    // back on 21 m again instead of advancing to 22 m.
    expect(secondZoom).toBeCloseTo(zoomForRingRadius(22, -25.2938, ringPx), 5)
  })
})

// Code-review finding (round 2): handleAdjustMove fires on EVERY animation
// frame of the 150ms easeTo a step itself starts, not only on a genuine
// pan/pinch — and it now writes the (still mid-flight, not-yet-settled)
// snapped radius into pendingTargetRadiusMRef, undoing the fix above for a
// real held +/- or keyboard auto-repeat: the next step, arriving before that
// ease has reached its target, reads back a stale intermediate value instead
// of the target the previous step actually commanded. MapLibre only ever
// attaches `originalEvent` to a move event it fired FOR a genuine user
// gesture (drag/wheel/touch) — a programmatic easeTo/jumpTo call like ours
// (no eventData argument) fires 'move' with `originalEvent: undefined`. The
// fix gates the pendingTargetRadiusMRef write on that.
describe('AnchorWatchMap Adjust mode — a still-easing move event does not clobber the pending target', () => {
  afterEach(() => {
    vi.clearAllMocks()
    vi.restoreAllMocks()
    latestOnMove = undefined
  })

  it('ignores an intermediate move event with no originalEvent (a still-flight easeTo frame), so a second step still lands on the next grid value', () => {
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
    render(
      <AnchorWatchMap
        {...baseProps}
        radiusMeters={20}
        adjustActive
        adjustRadiusBounds={{ minM: 5, maxM: 200, maxReason: null }}
      />,
    )
    easeToMock.mockClear()
    const ringPx = ringRadiusPx(300) // the short side of the 400x300 stubbed rect

    const wrapper = screen.getByTestId('anchor-watch-map-wrapper')
    fireEvent.keyDown(wrapper, { key: '+' }) // commands 21 m; pendingTargetRadiusMRef = 21

    // An early, still-in-flight frame of that SAME ease (80ms repeat beats
    // the 150ms ease) — the camera has barely moved off its starting zoom
    // (20 m's own zoom), and this event carries no originalEvent, exactly
    // like MapLibre's own easeTo/jumpTo (called with no eventData) does.
    act(() => {
      latestOnMove?.({
        viewState: { latitude: -25.2938, longitude: 152.9102, zoom: zoomForRingRadius(20, -25.2938, ringPx) },
        originalEvent: undefined,
      })
    })

    fireEvent.keyDown(wrapper, { key: '+' }) // must still command 22 m, not 21 m again

    expect(easeToMock).toHaveBeenCalledTimes(2)
    expect(easeToMock.mock.calls[0][0].zoom).toBeCloseTo(zoomForRingRadius(21, -25.2938, ringPx), 5)
    expect(easeToMock.mock.calls[1][0].zoom).toBeCloseTo(zoomForRingRadius(22, -25.2938, ringPx), 5)
  })

  it('still updates the pending target from a genuine gesture (a move event WITH originalEvent), so a step right after continues from the pinch, not a stale command', () => {
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
    render(
      <AnchorWatchMap
        {...baseProps}
        radiusMeters={20}
        adjustActive
        adjustRadiusBounds={{ minM: 5, maxM: 200, maxReason: null }}
      />,
    )
    easeToMock.mockClear()
    const ringPx = ringRadiusPx(300)

    // A real pinch to 50 m — carries a genuine originalEvent, the way an
    // actual touch/wheel-driven MapLibre gesture would.
    act(() => {
      latestOnMove?.({
        viewState: { latitude: -25.2938, longitude: 152.9102, zoom: zoomForRingRadius(50, -25.2938, ringPx) },
        originalEvent: new WheelEvent('wheel'),
      })
    })

    const wrapper = screen.getByTestId('anchor-watch-map-wrapper')
    fireEvent.keyDown(wrapper, { key: '+' }) // must continue from the pinch's 50 m, landing on 51 m

    expect(easeToMock).toHaveBeenCalledTimes(1)
    expect(easeToMock.mock.calls[0][0].zoom).toBeCloseTo(zoomForRingRadius(51, -25.2938, ringPx), 5)
  })
})

// Type-only sanity check that the draft shape exported for hosts matches
// what the component actually reports — a compile-time guard, not a runtime
// assertion.
function _typeCheck(draft: AnchorAdjustDraft) {
  return draft.lat + draft.lon + draft.radiusM
}
void _typeCheck
