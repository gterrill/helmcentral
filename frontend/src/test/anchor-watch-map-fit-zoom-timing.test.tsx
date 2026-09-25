import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { act, render } from '@testing-library/react'
import { AnchorWatchMap } from '@/components/anchor-watch-map'

// react-map-gl constructs the underlying maplibre.Map instance
// asynchronously — mapRef.current can still be null the first time
// AnchorWatchMap's mount-time fit-zoom effect runs (right at mount). The
// mock below models that: the imperative handle (and therefore
// mapRef.current) is only populated once `mapReady.value` is true, exactly
// mirroring "the ref is null at first commit, and becomes available once
// the map is actually constructed" — which in the real library is signalled
// by the map's own 'load' event, wired here to `onLoad`.
vi.mock('maplibre-gl', () => ({
  default: {},
}))

const jumpToMock = vi.fn()
const easeToMock = vi.fn()
const mapReady = vi.hoisted(() => ({ value: false }))
const captured = vi.hoisted(() => ({ onLoad: null as (() => void) | null }))

vi.mock('react-map-gl/maplibre', async () => {
  const React = await import('react')
  return {
    Map: React.forwardRef((props: { onLoad?: () => void; children?: React.ReactNode }, ref: React.Ref<unknown>) => {
      captured.onLoad = props.onLoad ?? null
      // No dependency array: re-evaluated on every render, so a `rerender`
      // after flipping mapReady.value picks up the new state immediately —
      // same as a real library finishing construction between renders.
      React.useImperativeHandle(ref, () => (
        mapReady.value
          ? {
              getCanvas: () => ({ style: { cursor: 'grab' } }),
              getZoom: () => 14,
              easeTo: easeToMock,
              jumpTo: jumpToMock,
            }
          : undefined
      ))
      return <div data-testid="map-root">{props.children}</div>
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
  vesselHeadingDeg: 265,
  anchorLat: -25.2939,
  anchorLon: 152.9103,
  radiusMeters: 34,
  depthMeters: 3.2,
  currentDriftKts: 0.1,
  currentSetDeg: 120,
  distanceMeters: 10,
  bearingDeg: 80,
  scopeRecommendation: null,
  isImperial: false,
  vesselTrail: () => [],
  aisVessels: [],
  aisTrails: () => new Map(),
  isDarkTheme: false,
}

// Independently derived (matches anchor-view.test.ts's own method):
// fitRadiusZoom(34, -25.2939, 390).
const EXPECTED_FIT_ZOOM = 17.89370067127723

describe('AnchorWatchMap fit-zoom timing (map ref not yet available at mount)', () => {
  beforeEach(() => {
    mapReady.value = false
    captured.onLoad = null
    jumpToMock.mockClear()
    localStorage.clear()
    // jsdom's real getBoundingClientRect is always 0x0 (no layout); stub it
    // so the wrapper-ref fallback the mount effect uses can actually
    // measure a container before the map itself exists.
    vi.spyOn(Element.prototype, 'getBoundingClientRect').mockReturnValue({
      width: 390, height: 500, top: 0, left: 0, right: 390, bottom: 500, x: 0, y: 0, toJSON: () => ({}),
    } as DOMRect)
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('does not jump the (nonexistent) map or write storage before the map ref exists', () => {
    render(<AnchorWatchMap {...baseProps} />)

    expect(jumpToMock).not.toHaveBeenCalled()
    // Not yet applied — must not be marked done in storage either, or the
    // fit is permanently skipped for the rest of this session (masked only
    // by a reload, which starts a fresh mount and just repeats the bug).
    expect(localStorage.getItem('anchor-watch-map-zoom')).toBeNull()
  })

  it('applies the fitted zoom via jumpTo once the map becomes available (onLoad)', () => {
    const { rerender } = render(<AnchorWatchMap {...baseProps} />)
    expect(jumpToMock).not.toHaveBeenCalled()

    // The map instance now exists (mirrors react-map-gl finishing its
    // asynchronous construction) — force the mock to re-evaluate its
    // imperative handle, then fire the 'load' event exactly as maplibre
    // would once a real map is ready.
    mapReady.value = true
    rerender(<AnchorWatchMap {...baseProps} />)
    act(() => {
      captured.onLoad?.()
    })

    expect(jumpToMock).toHaveBeenCalledTimes(1)
    expect(jumpToMock.mock.calls[0][0].zoom).toBeCloseTo(EXPECTED_FIT_ZOOM, 6)

    const stored = JSON.parse(localStorage.getItem('anchor-watch-map-zoom')!) as { zoom: number; sessionId: string | null }
    expect(stored.zoom).toBeCloseTo(EXPECTED_FIT_ZOOM, 6)
    expect(stored.sessionId).toBeNull()
  })

  it('does not re-apply once the fit has already landed on a real map', () => {
    const { rerender } = render(<AnchorWatchMap {...baseProps} />)
    mapReady.value = true
    rerender(<AnchorWatchMap {...baseProps} />)
    act(() => {
      captured.onLoad?.()
    })
    expect(jumpToMock).toHaveBeenCalledTimes(1)

    // A second 'load' (style reload, etc.) must not re-fit — this is a
    // once-per-session correction, not a standing override of the zoom the
    // operator may have since chosen themselves.
    act(() => {
      captured.onLoad?.()
    })
    expect(jumpToMock).toHaveBeenCalledTimes(1)
  })
})

// code-review finding: the old fit effect gave up for good the moment the
// container measured 0×0 (a hidden page, kiosk rotation, or the drawer
// opening) — masked only by a reload, which starts a fresh mount and just
// repeats the bug. The container's own ResizeObserver now retries a
// deferred fit once it reports a real size.
describe('AnchorWatchMap fit-zoom retries once the container gets a size (ResizeObserver)', () => {
  let resizeCallback: (() => void) | null = null
  let containerWidth = 0
  let containerHeight = 0

  beforeEach(() => {
    mapReady.value = true
    captured.onLoad = null
    jumpToMock.mockClear()
    localStorage.clear()
    containerWidth = 0
    containerHeight = 0
    vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(() => ({
      width: containerWidth,
      height: containerHeight,
      top: 0,
      left: 0,
      right: containerWidth,
      bottom: containerHeight,
      x: 0,
      y: 0,
      toJSON: () => ({}),
    } as DOMRect))
    resizeCallback = null
    vi.stubGlobal('ResizeObserver', class {
      constructor(cb: () => void) { resizeCallback = cb }
      observe() {}
      unobserve() {}
      disconnect() {}
    })
  })

  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('never fits while the container stays unmeasurable', () => {
    render(<AnchorWatchMap {...baseProps} />)

    expect(jumpToMock).not.toHaveBeenCalled()
    expect(localStorage.getItem('anchor-watch-map-zoom')).toBeNull()
  })

  it('retries the deferred zoom fit once the container reports a real size', () => {
    render(<AnchorWatchMap {...baseProps} />)
    expect(jumpToMock).not.toHaveBeenCalled()

    containerWidth = 390
    containerHeight = 500
    expect(resizeCallback).not.toBeNull()
    act(() => {
      resizeCallback!()
    })

    expect(jumpToMock).toHaveBeenCalledTimes(1)
    expect(jumpToMock.mock.calls[0][0].zoom).toBeCloseTo(EXPECTED_FIT_ZOOM, 6)
    const stored = JSON.parse(localStorage.getItem('anchor-watch-map-zoom')!) as { zoom: number; sessionId: string | null }
    expect(stored.zoom).toBeCloseTo(EXPECTED_FIT_ZOOM, 6)
  })
})

// code-review finding: a session change that lands before react-map-gl has
// finished constructing the underlying map used to write the pending centre
// and zoom to storage and call setCurrentZoom immediately, then try
// mapRef.current?.easeTo — which quietly no-ops against a map that doesn't
// exist yet. The new anchorage's centre was then never actually applied once
// the map did load. Both halves are now deferred together and applied, in
// one easeTo, from handleMapLoad.
describe('AnchorWatchMap defers a session-change fit until the map loads', () => {
  const SESSION = '2026-09-01T00:00:00Z'

  beforeEach(() => {
    mapReady.value = false
    captured.onLoad = null
    jumpToMock.mockClear()
    easeToMock.mockClear()
    localStorage.clear()
    vi.spyOn(Element.prototype, 'getBoundingClientRect').mockReturnValue({
      width: 390, height: 500, top: 0, left: 0, right: 390, bottom: 500, x: 0, y: 0, toJSON: () => ({}),
    } as DOMRect)
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('applies neither the centre nor the zoom while the map has not yet loaded', () => {
    const { rerender } = render(<AnchorWatchMap {...baseProps} anchorLat={null} anchorLon={null} anchorSetAt={null} />)

    // A session starts while the map is still under construction.
    rerender(<AnchorWatchMap {...baseProps} anchorSetAt={SESSION} />)

    expect(easeToMock).not.toHaveBeenCalled()
    expect(jumpToMock).not.toHaveBeenCalled()
    expect(localStorage.getItem('anchor-watch-map-center')).toBeNull()
  })

  it('applies both the pending centre and the fitted zoom, in one easeTo, once the map loads', () => {
    const { rerender } = render(<AnchorWatchMap {...baseProps} anchorLat={null} anchorLon={null} anchorSetAt={null} />)

    rerender(<AnchorWatchMap {...baseProps} anchorSetAt={SESSION} />)
    expect(easeToMock).not.toHaveBeenCalled()

    mapReady.value = true
    rerender(<AnchorWatchMap {...baseProps} anchorSetAt={SESSION} />)
    act(() => {
      captured.onLoad?.()
    })

    expect(easeToMock).toHaveBeenCalledTimes(1)
    expect(jumpToMock).not.toHaveBeenCalled()
    const [call] = easeToMock.mock.calls
    expect(call[0]).toMatchObject({ center: [baseProps.anchorLon, baseProps.anchorLat], duration: 600 })
    expect(call[0].zoom).toBeCloseTo(EXPECTED_FIT_ZOOM, 6)

    expect(JSON.parse(localStorage.getItem('anchor-watch-map-center')!)).toEqual({
      latitude: baseProps.anchorLat,
      longitude: baseProps.anchorLon,
      sessionId: SESSION,
    })
  })
})
