import { act, fireEvent, render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { AnchorWatchMap } from '@/components/anchor-watch-map'

vi.mock('maplibre-gl', () => ({
  default: {},
}))

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
    Marker: ({ children, onClick }: { children?: React.ReactNode; onClick?: () => void }) => (
      <div onClick={onClick}>{children}</div>
    ),
    Source: ({ children, id }: { children?: React.ReactNode; id: string }) => (
      <div data-testid={`source-${id}`}>{children}</div>
    ),
    Layer: ({ id, beforeId }: { id: string; beforeId?: string }) => (
      <div data-testid={`layer-${id}`} data-before-id={beforeId} />
    ),
  }
})

function renderMap() {
  return render(
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
      isImperial={false}
      vesselTrail={() => []}
      aisVessels={[
        { name: 'SPLURGE', lat: -25.2939, lon: 152.9103, range_ft: 200, age_seconds: 5 },
      ]}
      aisTrails={() => new Map()}
      isDarkTheme={false}
      showImageryLayer
      onImageryToggle={() => undefined}
      onFullscreen={() => undefined}
      onAnchorReposition={() => undefined}
      onRadiusChange={() => undefined}
      onClearAnchor={() => undefined}
    />,
  )
}

describe('AnchorWatchMap controls and AIS selection', () => {
  it('puts satellite below the zoom controls and removes control dividers', () => {
    renderMap()

    const controls = screen.getByTestId('anchor-watch-controls')
    const zoomOut = within(controls).getByRole('button', { name: 'Zoom out' })
    const satellite = within(controls).getByRole('button', { name: 'Toggle satellite imagery' })

    expect(zoomOut.nextElementSibling).toBe(satellite)
    expect(screen.getByTestId('anchor-watch-metrics').style.zIndex).toBe('2000')
  })

  it('expands a clicked AIS vessel for three seconds and shows distance and bearing', () => {
    vi.useFakeTimers()

    renderMap()

    fireEvent.click(screen.getByRole('button', { name: 'AIS vessel: SPLURGE' }))

    expect(screen.getByText('0 m · 0°')).toBeInTheDocument()
    expect(document.querySelector('.h-11.w-11')).not.toBeNull()

    act(() => {
      vi.advanceTimersByTime(3000)
    })

    expect(screen.queryByText('0 m · 0°')).not.toBeInTheDocument()

    vi.useRealTimers()
  })

  it('rotates the Current arrow to point in the set direction, matching the Wind tile Set arrow', () => {
    // currentSetDeg is the direction the current flows toward (SignalK
    // setTrue), same convention the Wind tile's Set arrow uses with no
    // offset — this arrow must not add a reciprocal 180° flip.
    renderMap()

    const metrics = screen.getByTestId('anchor-watch-metrics')
    const arrow = metrics.querySelector('svg.lucide-arrow-up') as SVGElement | null
    expect(arrow).not.toBeNull()
    expect(arrow?.style.transform).toBe('rotate(120deg)')
  })

  it('anchors the imagery and seamark raster layers below the alarm circle, regardless of mount order', () => {
    // react-map-gl's addLayer call has no awareness of JSX sibling order - a
    // layer added later (e.g. world-imagery toggled on mid-session, well
    // after the map first loads) is otherwise appended on top of the whole
    // stack, covering the alarm circle and trails. beforeId pins it below
    // the alarm circle explicitly instead of relying on mount-order luck.
    renderMap()

    expect(screen.getByTestId('layer-world-imagery-layer').dataset.beforeId).toBe('alarm-circle-fill')
    expect(screen.getByTestId('layer-openseamap-layer').dataset.beforeId).toBe('alarm-circle-fill')
  })
})
