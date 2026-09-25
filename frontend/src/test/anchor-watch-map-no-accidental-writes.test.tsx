import { describe, it, expect, vi, beforeEach } from 'vitest'
import { act, fireEvent, render, screen } from '@testing-library/react'
import { AnchorWatchMap } from '@/components/anchor-watch-map'

// Phase 1 of the anchor-adjust-sheet plan: after the map-gesture editing
// removal, nothing on this map can write to the watch. The impeccable
// critique of the pre-Phase-1 map found three P0s, all through these exact
// gestures — a tap on the anchor marker firing a radius PATCH, a stray
// touchstart committing whatever the previous drag left in liveRadius/
// ghostAnchor, and a double-tap doing the same. This suite is the negative
// space: every one of those gestures, replayed against the map as it is now,
// must produce nothing — no fetch, no mode/overlay, no state change.
interface MapProps {
  onMouseDown?: unknown
  onTouchStart?: unknown
  onTouchMove?: unknown
  onDblClick?: unknown
  onMouseMove?: unknown
  onClick: (event: { lngLat: { lat: number; lng: number } }) => void
  dragPan?: boolean
  children?: React.ReactNode
}

const mapState = vi.hoisted(() => ({ props: null as MapProps | null }))

vi.mock('maplibre-gl', () => ({
  default: {},
}))

vi.mock('react-map-gl/maplibre', async () => {
  const React = await import('react')
  return {
    Map: React.forwardRef((props: MapProps, ref: React.Ref<unknown>) => {
      mapState.props = props
      React.useImperativeHandle(ref, () => ({
        getCanvas: () => ({ style: { cursor: 'grab' } }),
        getZoom: () => 14,
        easeTo: () => undefined,
        jumpTo: () => undefined,
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
  scopeRecommendation: null,
  isImperial: false,
  vesselTrail: () => [],
  aisVessels: [],
  aisTrails: () => new Map(),
  isDarkTheme: false,
}

describe('AnchorWatchMap: no accidental writes (Phase 1, impeccable P0s)', () => {
  let fetchMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    mapState.props = null
    fetchMock = vi.fn().mockResolvedValue({ ok: false, json: async () => ({}) })
    vi.stubGlobal('fetch', fetchMock)
  })

  it('wires no onMouseDown, onTouchStart, onTouchMove, onDblClick or onMouseMove handler at all', () => {
    render(<AnchorWatchMap {...baseProps} />)

    // Structural proof, not just a behavioural one: the P0s were a
    // mousedown/touchstart hit-test on the circle edge and a double-click
    // commit. None of those handlers exist to be triggered by anything —
    // not a real drag, not a second touch, not a dblclick.
    expect(mapState.props!.onMouseDown).toBeUndefined()
    expect(mapState.props!.onTouchStart).toBeUndefined()
    expect(mapState.props!.onTouchMove).toBeUndefined()
    expect(mapState.props!.onDblClick).toBeUndefined()
    expect(mapState.props!.onMouseMove).toBeUndefined()
  })

  it('leaves dragPan on unconditionally — there is no edit mode left to borrow it', () => {
    render(<AnchorWatchMap {...baseProps} />)

    expect(mapState.props!.dragPan).toBe(true)
  })

  it('tapping the anchor marker fires no fetch and raises no overlay', () => {
    render(<AnchorWatchMap {...baseProps} />)

    const marker = screen.getByLabelText('Anchor position')
    act(() => {
      fireEvent.click(marker)
    })

    expect(fetchMock).not.toHaveBeenCalled()
    expect(screen.queryByText(/Tap map to place anchor/i)).not.toBeInTheDocument()
    expect(screen.queryByText(/Tap map to set radius/i)).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Confirm anchor reposition')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Cancel' })).not.toBeInTheDocument()
  })

  it('the anchor marker is a plain, non-interactive element — not a button offering an action', () => {
    render(<AnchorWatchMap {...baseProps} />)

    const marker = screen.getByLabelText('Anchor position')
    expect(marker.tagName).not.toBe('BUTTON')
    expect(screen.queryByRole('button', { name: /Anchor position/i })).not.toBeInTheDocument()
  })

  it('a map click that lands on the anchor marker does not also raise a placemark pin tooltip', () => {
    // The marker swallows the click (stopPropagation + suppressNextMapClickRef)
    // so it never falls through to the map's own "place a pin here" handler —
    // the same treatment every other marker on this map gets.
    render(<AnchorWatchMap {...baseProps} />)

    fireEvent.click(screen.getByLabelText('Anchor position'))

    expect(screen.queryByTestId('pin-candidate')).not.toBeInTheDocument()
  })

  it('Enter and Escape on window do nothing — there is no keydown listener left to catch them', () => {
    render(<AnchorWatchMap {...baseProps} />)

    fireEvent.keyDown(window, { key: 'Enter' })
    fireEvent.keyDown(window, { key: 'Escape' })

    expect(fetchMock).not.toHaveBeenCalled()
    expect(screen.queryByText(/Tap map to place anchor/i)).not.toBeInTheDocument()
    expect(screen.queryByText(/Tap map to set radius/i)).not.toBeInTheDocument()
  })

  it('reports the actual configured radius in the metric overlay, unaffected by any of the above', () => {
    render(<AnchorWatchMap {...baseProps} radiusMeters={34} />)

    const metrics = screen.getByTestId('anchor-watch-metrics')
    fireEvent.click(screen.getByLabelText('Anchor position'))
    fireEvent.keyDown(window, { key: 'Enter' })

    expect(metrics).toHaveTextContent('34')
  })
})
