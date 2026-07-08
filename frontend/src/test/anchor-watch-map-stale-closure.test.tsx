import { describe, it, expect, vi, beforeEach } from 'vitest'
import { act, fireEvent, render, screen } from '@testing-library/react'
import { AnchorWatchMap } from '@/components/anchor-watch-map'

// Captures the latest props passed to the mocked <Map> so tests can invoke
// its event handlers (onClick, onMouseDown, onMouseMove) directly, exactly
// as maplibre would when the user interacts with the real map.
interface MapProps {
  onClick: (event: { lngLat: { lat: number; lng: number } }) => void
  onMouseMove: (event: { lngLat: { lat: number; lng: number } }) => void
  onMouseDown: (event: { preventDefault: () => void; point: { x: number; y: number } }) => void
  children?: React.ReactNode
}

interface MapHandle {
  getCanvas: () => { style: { cursor: string } }
  getZoom: () => number
  easeTo: () => undefined
  project: () => { x: number; y: number }
  dragPan: { disable: () => void; enable: () => void }
}

const mapState = vi.hoisted(() => ({ props: null as MapProps | null }))

vi.mock('maplibre-gl', () => ({
  default: {},
}))

vi.mock('react-map-gl/maplibre', async () => {
  const React = await import('react')
  return {
    Map: React.forwardRef((props: MapProps, ref: React.Ref<MapHandle>) => {
      mapState.props = props
      React.useImperativeHandle(ref, () => ({
        getCanvas: () => ({ style: { cursor: 'grab' } }),
        getZoom: () => 14,
        easeTo: () => undefined,
        project: () => ({ x: 50, y: 50 }),
        dragPan: { disable: vi.fn(), enable: vi.fn() },
      }))
      return <div data-testid="map-root">{props.children}</div>
    }),
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
  isImperial: false,
  vesselTrail: () => [],
  aisVessels: [],
  aisTrails: () => new Map(),
  isDarkTheme: false,
  onClearAnchor: () => undefined,
}

describe('AnchorWatchMap stale-closure regressions (exhaustive-deps)', () => {
  beforeEach(() => {
    mapState.props = null
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, json: async () => ({}) }))
  })

  it('commits the latest ghost-anchor position on map click, not the position captured when reposition mode was entered', async () => {
    const onAnchorReposition = vi.fn()

    render(
      <AnchorWatchMap
        {...baseProps}
        onAnchorReposition={onAnchorReposition}
        onRadiusChange={() => undefined}
      />,
    )

    // Enter reposition mode — this seeds ghostAnchor at the current anchor position.
    fireEvent.click(screen.getByLabelText('Anchor position — click to reposition'))

    // Drag the ghost anchor to a new location (mirrors onMouseMove while dragging).
    act(() => {
      mapState.props!.onMouseMove({ lngLat: { lat: -25.301, lng: 152.955 } })
    })

    // Click the map to commit the reposition using whatever handler is currently wired up.
    act(() => {
      mapState.props!.onClick({ lngLat: { lat: -25.301, lng: 152.955 } })
    })

    expect(onAnchorReposition).toHaveBeenCalledTimes(1)
    // Must reflect the dragged-to position, not the stale position captured
    // when handleMapClick was last memoized (the original anchor position).
    expect(onAnchorReposition).toHaveBeenCalledWith(-25.301, 152.955)
  })

  it('commits a radius change using the latest onRadiusChange callback, not one captured before an unrelated rerender', () => {
    const onRadiusChangeStale = vi.fn()
    const onRadiusChangeLatest = vi.fn()

    const { rerender } = render(
      <AnchorWatchMap
        {...baseProps}
        onAnchorReposition={() => undefined}
        onRadiusChange={onRadiusChangeStale}
      />,
    )

    // Enter radius-edit mode via the circle-edge mousedown hit-test (project()
    // is mocked to always report a hit).
    act(() => {
      mapState.props!.onMouseDown({ preventDefault: vi.fn(), point: { x: 50, y: 50 } })
    })

    // Parent rerenders with a new onRadiusChange identity (e.g. a non-memoized
    // inline callback) while nothing else that's tracked as a dependency changes.
    rerender(
      <AnchorWatchMap
        {...baseProps}
        onAnchorReposition={() => undefined}
        onRadiusChange={onRadiusChangeLatest}
      />,
    )

    // Commit the radius change via the same circle-edge handler.
    act(() => {
      mapState.props!.onMouseDown({ preventDefault: vi.fn(), point: { x: 50, y: 50 } })
    })

    expect(onRadiusChangeStale).not.toHaveBeenCalled()
    expect(onRadiusChangeLatest).toHaveBeenCalledTimes(1)
    expect(onRadiusChangeLatest).toHaveBeenCalledWith(34)
  })
})
