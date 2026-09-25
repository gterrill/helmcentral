import { describe, it, expect, vi, beforeEach } from 'vitest'
import { act, render, screen } from '@testing-library/react'
import { AnchorWatchMap } from '@/components/anchor-watch-map'

// Dashboard tile hosts AnchorWatchMap with no onAnchorReposition/onRadiusChange
// (editing lives only on the full-page Anchor Watch view — see
// anchor-watch-tile.tsx). Every handler this map wires to the underlying
// maplibre map must become a no-op once those callbacks are absent, rather
// than silently entering an edit mode the host has nowhere to confirm from.
//
// Same capture-the-latest-Map-props approach as
// anchor-watch-map-stale-closure.test.tsx, so these tests can invoke
// onMouseMove/onMouseDown/onTouchStart directly, exactly as maplibre would.
interface MapProps {
  onClick: (event: { lngLat: { lat: number; lng: number } }) => void
  onMouseMove: (event: { lngLat: { lat: number; lng: number }; point: { x: number; y: number } }) => void
  onMouseDown: (event: { preventDefault: () => void; point: { x: number; y: number } }) => void
  onTouchStart: (event: { point: { x: number; y: number } }) => void
  children?: React.ReactNode
}

interface MapHandle {
  getCanvas: () => { style: { cursor: string } }
  getZoom: () => number
  easeTo: () => undefined
  project: () => { x: number; y: number }
  dragPan: { disable: () => void; enable: () => void }
}

const mapState = vi.hoisted(() => ({
  props: null as MapProps | null,
  canvas: { style: { cursor: 'grab' } },
  dragPanDisable: vi.fn(),
}))

vi.mock('maplibre-gl', () => ({
  default: {},
}))

vi.mock('react-map-gl/maplibre', async () => {
  const React = await import('react')
  return {
    Map: React.forwardRef((props: MapProps, ref: React.Ref<MapHandle>) => {
      mapState.props = props
      React.useImperativeHandle(ref, () => ({
        getCanvas: () => mapState.canvas,
        getZoom: () => 14,
        easeTo: () => undefined,
        // The edge hit-test always reports a "hit" (project() returns the
        // same point for both the anchor and the radius edge, so pixelDist
        // is 0 and well inside the 15/25px threshold) — the point is to
        // prove the *callback presence* gate, not the geometry.
        project: () => ({ x: 50, y: 50 }),
        dragPan: { disable: mapState.dragPanDisable, enable: vi.fn() },
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

describe('AnchorWatchMap with no editing callbacks (view-only, e.g. the dashboard tile)', () => {
  beforeEach(() => {
    mapState.props = null
    mapState.canvas = { style: { cursor: 'grab' } }
    mapState.dragPanDisable.mockClear()
  })

  it('gives the anchor marker a neutral label and no grab cursor', () => {
    render(<AnchorWatchMap {...baseProps} />)

    expect(screen.queryByLabelText('Anchor position — click to reposition')).not.toBeInTheDocument()
    const marker = screen.getByLabelText('Anchor position')
    expect(marker.style.cursor).toBe('default')
  })

  it('does not enter reposition mode when the anchor marker is clicked', () => {
    render(<AnchorWatchMap {...baseProps} />)

    const marker = screen.getByLabelText('Anchor position')
    act(() => {
      marker.click()
    })

    // Entering reposition mode renders a ghost-anchor "Confirm anchor
    // reposition" marker — its absence is the proof nothing happened.
    expect(screen.queryByLabelText('Confirm anchor reposition')).not.toBeInTheDocument()
    // The cursor stays at rest rather than flipping to 'grabbing'.
    expect(marker.style.cursor).toBe('default')
  })

  it('does not enter radius edit mode on circle-edge mouse-down', () => {
    render(<AnchorWatchMap {...baseProps} />)

    act(() => {
      mapState.props!.onMouseDown({ preventDefault: vi.fn(), point: { x: 50, y: 50 } })
    })

    // Entering radius edit disables map drag-panning imperatively — its
    // absence proves the mode change never happened.
    expect(mapState.dragPanDisable).not.toHaveBeenCalled()
    expect(mapState.canvas.style.cursor).not.toBe('ew-resize')
  })

  it('does not enter radius edit mode on circle-edge touch-start', () => {
    render(<AnchorWatchMap {...baseProps} />)

    act(() => {
      mapState.props!.onTouchStart({ point: { x: 50, y: 50 } })
    })

    expect(mapState.dragPanDisable).not.toHaveBeenCalled()
    expect(mapState.canvas.style.cursor).not.toBe('ew-resize')
  })

  it('never shows the ew-resize hover cursor near the circle edge', () => {
    render(<AnchorWatchMap {...baseProps} />)

    act(() => {
      mapState.props!.onMouseMove({ lngLat: { lat: -25.2939, lng: 152.9103 }, point: { x: 50, y: 50 } })
    })

    expect(mapState.canvas.style.cursor).not.toBe('ew-resize')
  })
})
