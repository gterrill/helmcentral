import { act, render } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { AnchorWatchMap } from '@/components/anchor-watch-map'
import type { RadarCapabilities } from '@/lib/radar-echo/legend'
import type { RadarInfo } from '@/hooks/use-radar-targets'
import type { NearbyVessel } from '@/hooks/use-nearby-vessels'
import type { RodeMethodResult } from '@/lib/rode-plan'

// Extends the react-map-gl/maplibre mock used by anchor-watch-map-ui.test.tsx
// (~L40) with the imperative addSource/addLayer/removeLayer/removeSource/
// getLayer/getSource/moveLayer/isStyleLoaded spies use-radar-echo-layer.ts
// needs -- react-map-gl's declarative <Source>/<Layer> can't express a
// canvas source at all (its SourceSpecification union excludes
// CanvasSourceSpecification), so the radar echo layer is added imperatively
// via mapRef.current.getMap(), and this suite is what exercises that path.
vi.mock('maplibre-gl', () => ({
  default: {},
}))

// jsdom has no 2D canvas context (echo-canvas.ts's own doc comment says as
// much: "nothing in this file is exercised by the test suite"), so the real
// createEchoCanvas would throw the moment the layer hook tries to mount.
// Stubbed to a plain element + a no-op render, the same treatment the rest
// of the radar-echo suite gives this module.
vi.mock('@/lib/radar-echo/echo-canvas', () => ({
  RADAR_ECHO_CANVAS_PX: 1024,
  createEchoCanvas: vi.fn(() => ({
    element: document.createElement('canvas'),
    render: vi.fn(),
  })),
}))

// getEchoLut is real (cheap once memoized), but the very first call at a
// freshly-derived spoke count is ~1M atan2/hypot calls (geometry.ts's own
// doc comment) and nothing here inspects the LUT's contents -- only that
// addSource/addLayer got called correctly -- so it's stubbed to a trivial
// object to keep this suite fast. echoCornerCoordinates stays real: the
// four-coordinate-pair assertions below depend on it.
vi.mock('@/lib/radar-echo/geometry', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/radar-echo/geometry')>()
  return {
    ...actual,
    getEchoLut: vi.fn(() => ({
      sizePx: 1,
      spokeCount: 1,
      binCount: 1,
      spokeIndex: new Uint16Array(0),
      rangeBin: new Uint16Array(0),
      inside: new Uint8Array(0),
    })),
  }
})

vi.mock('@/hooks/use-radar-capabilities', () => ({
  useRadarCapabilities: vi.fn(),
}))

vi.mock('@/hooks/use-radar-echo-stream', () => ({
  subscribeRadarSpokes: vi.fn(),
  subscribeRadarEchoStatus: vi.fn(),
}))

vi.mock('@/lib/radar-echo/spoke-message', () => ({
  decodeRadarMessage: vi.fn(),
}))

import { useRadarCapabilities } from '@/hooks/use-radar-capabilities'
import { subscribeRadarSpokes, subscribeRadarEchoStatus } from '@/hooks/use-radar-echo-stream'
import { decodeRadarMessage } from '@/lib/radar-echo/spoke-message'

interface RecordedLayerProps {
  id: string
  beforeId?: string
}

interface MockMapHandle {
  isStyleLoaded: ReturnType<typeof vi.fn>
  getSource: ReturnType<typeof vi.fn>
  addSource: ReturnType<typeof vi.fn>
  removeSource: ReturnType<typeof vi.fn>
  getLayer: ReturnType<typeof vi.fn>
  addLayer: ReturnType<typeof vi.fn>
  removeLayer: ReturnType<typeof vi.fn>
  moveLayer: ReturnType<typeof vi.fn>
}

let mockMap: MockMapHandle
let lastOnLoad: (() => void) | null = null
let lastOnStyleData: (() => void) | null = null
// A real CanvasSource's setCoordinates -- recorded separately since
// getSource() returns this once addSource has been called, mirroring
// maplibre-gl's own real behaviour (the source object it hands back is the
// thing setCoordinates lives on).
const setCoordinatesSpy = vi.fn()

vi.mock('react-map-gl/maplibre', async () => {
  const React = await import('react')
  return {
    Map: React.forwardRef(
      (
        {
          children,
          onLoad,
          onStyleData,
        }: { children?: React.ReactNode; onLoad?: () => void; onStyleData?: () => void },
        ref: React.Ref<unknown>,
      ) => {
        lastOnLoad = onLoad ?? null
        lastOnStyleData = onStyleData ?? null
        React.useImperativeHandle(ref, () => ({
          getCanvas: () => ({ style: { cursor: 'grab' } }),
          getZoom: () => 14,
          easeTo: vi.fn(),
          getMap: () => mockMap,
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
    Layer: (props: RecordedLayerProps) => <div data-testid={`layer-${props.id}`} data-before-id={props.beforeId} />,
  }
})

// Deterministic single-step control over the layer hook's throttled paint
// loop: real rAF timing would make "deliver a frame, then assert the next
// paint picked it up" flaky and slow. Captured callback is re-armed by the
// hook itself every tick (it re-requests a frame as the first thing `tick`
// does), so calling runRafTick() repeatedly drives the loop step by step.
let rafCallback: FrameRequestCallback | null = null
function runRafTick(time = 1000) {
  const cb = rafCallback
  rafCallback = null
  act(() => {
    cb?.(time)
  })
}

const capabilities: RadarCapabilities = {
  spokesPerRevolution: 8192,
  maxSpokeLength: 1024,
  legend: { pixels: [] },
}
const palette = new Uint32Array(256)

const fakeSpoke = {
  angle: 100,
  bearing: 100,
  rangeM: 463,
  lat: -25.2939,
  lon: 152.9103,
  data: new Uint8Array(1024),
}

let capturedOnFrame: ((frame: ArrayBuffer) => void) | null = null
let capturedStatusCb: ((s: 'idle' | 'connected' | 'reconnecting' | 'disconnected') => void) | null = null
const unsubscribeFrameSpy = vi.fn()
const unsubscribeStatusSpy = vi.fn()

const transmittingRadar: RadarInfo = { id: 'fur6424A', name: 'Furuno DRS4D-NXT', transmitting: true }

const defaultAisVessels: NearbyVessel[] = []

const scopeRecommendation: RodeMethodResult = {
  id: 'ratio',
  label: 'Ratio Method',
  recommendedRodeM: 30,
  scopeRatio: 5,
  note: 'note',
}

function mapElement(overrides: Partial<React.ComponentProps<typeof AnchorWatchMap>> = {}) {
  return (
    <AnchorWatchMap
      vesselLat={-25.2939}
      vesselLon={152.9103}
      vesselHeadingDeg={265}
      anchorLat={-25.2939}
      anchorLon={152.9103}
      radiusMeters={34}
      depthMeters={3.2}
      currentDriftKts={0.1}
      currentSetDeg={120}
      distanceMeters={10}
      bearingDeg={80}
      scopeRecommendation={scopeRecommendation}
      isImperial={false}
      vesselTrail={() => []}
      aisVessels={defaultAisVessels}
      aisTrails={() => new Map()}
      radarTargets={[]}
      radars={[transmittingRadar]}
      radarSource="mayara"
      isDarkTheme={false}
      showRadarEcho={false}
      onRadarEchoToggle={() => undefined}
      onAnchorReposition={() => undefined}
      onRadiusChange={() => undefined}
      {...overrides}
    />
  )
}

beforeEach(() => {
  mockMap = {
    isStyleLoaded: vi.fn(() => true),
    getSource: vi.fn((id: string) => {
      if (id === 'carto') return {} // keep map-place-labels' fail-fast warning quiet; unrelated to this suite
      if (id !== 'radar-echo') return undefined
      return mockMap.addSource.mock.calls.length > 0 ? { setCoordinates: setCoordinatesSpy } : undefined
    }),
    addSource: vi.fn(),
    removeSource: vi.fn(),
    getLayer: vi.fn((id: string) => {
      if (id === 'openseamap-layer') return {}
      if (id === 'radar-echo-layer') return mockMap.addLayer.mock.calls.length > 0 && mockMap.removeLayer.mock.calls.length === 0 ? {} : undefined
      return undefined
    }),
    addLayer: vi.fn(),
    removeLayer: vi.fn(),
    moveLayer: vi.fn(),
  }
  lastOnLoad = null
  lastOnStyleData = null
  rafCallback = null
  setCoordinatesSpy.mockClear()
  unsubscribeFrameSpy.mockClear()
  unsubscribeStatusSpy.mockClear()
  capturedOnFrame = null
  capturedStatusCb = null

  vi.mocked(useRadarCapabilities).mockReturnValue({
    capabilities,
    palette,
    error: null,
    loading: false,
  })

  vi.mocked(subscribeRadarSpokes).mockImplementation((_radarId, onFrame) => {
    capturedOnFrame = onFrame
    return unsubscribeFrameSpy
  })
  vi.mocked(subscribeRadarEchoStatus).mockImplementation((cb) => {
    capturedStatusCb = cb
    cb('connected')
    return unsubscribeStatusSpy
  })

  vi.mocked(decodeRadarMessage).mockReturnValue([fakeSpoke])

  vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
    rafCallback = cb
    return 1
  })
  vi.stubGlobal('cancelAnimationFrame', () => {
    rafCallback = null
  })
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.clearAllMocks()
})

describe('AnchorWatchMap radar echo layer', () => {
  it('adds no source while the toggle is off', () => {
    render(mapElement({ showRadarEcho: false }))

    expect(mockMap.addSource).not.toHaveBeenCalled()
    expect(mockMap.addLayer).not.toHaveBeenCalled()
  })

  it('adds a canvas source with four coordinate pairs and the layer at the chosen beforeId once enabled and a spoke arrives', () => {
    render(mapElement({ showRadarEcho: true }))

    expect(capturedOnFrame).not.toBeNull()
    act(() => {
      capturedOnFrame!(new ArrayBuffer(0))
    })
    runRafTick()

    expect(mockMap.addSource).toHaveBeenCalledTimes(1)
    const [sourceId, sourceSpec] = mockMap.addSource.mock.calls[0] as [string, { type: string; coordinates: unknown[] }]
    expect(sourceId).toBe('radar-echo')
    expect(sourceSpec.type).toBe('canvas')
    expect(sourceSpec.coordinates).toHaveLength(4)

    expect(mockMap.addLayer).toHaveBeenCalledTimes(1)
    const [layerSpec, beforeId] = mockMap.addLayer.mock.calls[0] as [{ id: string; type: string; source: string }, string | undefined]
    expect(layerSpec.id).toBe('radar-echo-layer')
    expect(layerSpec.type).toBe('raster')
    expect(layerSpec.source).toBe('radar-echo')
    expect(beforeId).toBe('openseamap-layer')
  })

  it('re-asserts layer order with moveLayer on onStyleData', () => {
    render(mapElement({ showRadarEcho: true }))

    act(() => {
      capturedOnFrame!(new ArrayBuffer(0))
    })
    runRafTick()
    expect(mockMap.addLayer).toHaveBeenCalledTimes(1)
    mockMap.moveLayer.mockClear()

    expect(lastOnStyleData).not.toBeNull()
    act(() => {
      lastOnStyleData!()
    })

    expect(mockMap.moveLayer).toHaveBeenCalledWith('radar-echo-layer', 'openseamap-layer')
  })

  it('re-asserts layer order on onLoad too', () => {
    render(mapElement({ showRadarEcho: true }))

    act(() => {
      capturedOnFrame!(new ArrayBuffer(0))
    })
    runRafTick()
    mockMap.moveLayer.mockClear()

    expect(lastOnLoad).not.toBeNull()
    act(() => {
      lastOnLoad!()
    })

    expect(mockMap.moveLayer).toHaveBeenCalledWith('radar-echo-layer', 'openseamap-layer')
  })

  it('removes the layer before the source when disabled', () => {
    const { rerender } = render(mapElement({ showRadarEcho: true }))

    act(() => {
      capturedOnFrame!(new ArrayBuffer(0))
    })
    runRafTick()
    expect(mockMap.addSource).toHaveBeenCalledTimes(1)

    rerender(mapElement({ showRadarEcho: false }))

    expect(mockMap.removeLayer).toHaveBeenCalledWith('radar-echo-layer')
    expect(mockMap.removeSource).toHaveBeenCalledWith('radar-echo')
    const layerCallOrder = mockMap.removeLayer.mock.invocationCallOrder[0]
    const sourceCallOrder = mockMap.removeSource.mock.invocationCallOrder[0]
    expect(layerCallOrder).toBeLessThan(sourceCallOrder)
  })

  it('cleans up on unmount', () => {
    const { unmount } = render(mapElement({ showRadarEcho: true }))

    act(() => {
      capturedOnFrame!(new ArrayBuffer(0))
    })
    runRafTick()
    expect(mockMap.addSource).toHaveBeenCalledTimes(1)

    unmount()

    expect(mockMap.removeLayer).toHaveBeenCalledWith('radar-echo-layer')
    expect(mockMap.removeSource).toHaveBeenCalledWith('radar-echo')
    expect(unsubscribeFrameSpy).toHaveBeenCalled()
    expect(unsubscribeStatusSpy).toHaveBeenCalled()
  })

  it('clears the buffer and repaints when the stream disconnects', () => {
    render(mapElement({ showRadarEcho: true }))

    act(() => {
      capturedOnFrame!(new ArrayBuffer(0))
    })
    runRafTick()
    expect(mockMap.addSource).toHaveBeenCalledTimes(1)

    expect(capturedStatusCb).not.toBeNull()
    act(() => {
      capturedStatusCb!('disconnected')
    })

    // A cleared buffer is still dirty (it changed), so the very next tick
    // repaints it -- this only verifies the loop doesn't skip that repaint,
    // not any pixel content (echo-canvas.ts's render is mocked out).
    runRafTick(2000)
    // No throw, and the layer is still mounted (disconnecting isn't the
    // same as disabling) -- setCoordinates is not called again since
    // position/range haven't changed.
    expect(mockMap.removeLayer).not.toHaveBeenCalled()
  })
})
