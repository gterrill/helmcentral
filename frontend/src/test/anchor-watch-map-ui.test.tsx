import { act, fireEvent, render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { AnchorWatchMap } from '@/components/anchor-watch-map'
import type { NearbyVessel } from '@/hooks/use-nearby-vessels'

vi.mock('maplibre-gl', () => ({
  default: {},
}))

let lastInitialViewState: { latitude: number; longitude: number; zoom: number } | null = null
let lastMoveEndHandler: ((e: { viewState: { latitude: number; longitude: number; zoom: number } }) => void) | null = null
const easeToMock = vi.fn()

vi.mock('react-map-gl/maplibre', async () => {
  const React = await import('react')
  return {
    Map: React.forwardRef(
      (
        {
          children,
          initialViewState,
          onMoveEnd,
        }: {
          children?: React.ReactNode
          initialViewState?: { latitude: number; longitude: number; zoom: number }
          onMoveEnd?: (e: { viewState: { latitude: number; longitude: number; zoom: number } }) => void
        },
        ref: React.Ref<unknown>,
      ) => {
        lastInitialViewState = initialViewState ?? null
        lastMoveEndHandler = onMoveEnd ?? null
        React.useImperativeHandle(ref, () => ({
          getCanvas: () => ({ style: { cursor: 'grab' } }),
          getZoom: () => 14,
          easeTo: easeToMock,
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
    Layer: ({ id, beforeId }: { id: string; beforeId?: string }) => (
      <div data-testid={`layer-${id}`} data-before-id={beforeId} />
    ),
  }
})

const defaultAisVessels: NearbyVessel[] = [
  { id: 'urn:mrn:imo:mmsi:100000001', name: 'SPLURGE', lat: -25.2939, lon: 152.9103, range_m: 61, age_seconds: 5 },
]

function renderMap(aisVessels: NearbyVessel[] = defaultAisVessels, overrides: Partial<React.ComponentProps<typeof AnchorWatchMap>> = {}) {
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
      aisVessels={aisVessels}
      aisTrails={() => new Map()}
      isDarkTheme={false}
      showImageryLayer
      onImageryToggle={() => undefined}
      onFullscreen={() => undefined}
      onAnchorReposition={() => undefined}
      onRadiusChange={() => undefined}
      onClearAnchor={() => undefined}
      {...overrides}
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
    expect(arrow?.classList.contains('invisible')).toBe(false)
  })

  it('preserves layout space for the Current arrow when current is 0.0 or unavailable to prevent jitter', () => {
    const { unmount } = renderMap(defaultAisVessels, { currentDriftKts: 0.0, currentSetDeg: 120 })

    const metricsZero = screen.getByTestId('anchor-watch-metrics')
    const arrowZero = metricsZero.querySelector('svg.lucide-arrow-up') as SVGElement | null
    expect(arrowZero).not.toBeNull()
    expect(arrowZero?.classList.contains('invisible')).toBe(true)

    unmount()

    renderMap(defaultAisVessels, { currentDriftKts: null, currentSetDeg: null })
    const metricsNull = screen.getByTestId('anchor-watch-metrics')
    const arrowNull = metricsNull.querySelector('svg.lucide-arrow-up') as SVGElement | null
    expect(arrowNull).not.toBeNull()
    expect(arrowNull?.classList.contains('invisible')).toBe(true)
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

  // Regression test: selection previously matched on transient?.label ===
  // vessel.name, so two vessels sharing a name both lit up as "selected"
  // when either was clicked. Selection now compares on vessel.id instead.
  it('selects only the clicked marker when two AIS vessels share a name', () => {
    vi.useFakeTimers()

    renderMap([
      { id: 'urn:mrn:imo:mmsi:100000001', name: 'TWIN', lat: -25.2939, lon: 152.9103, range_m: 61, age_seconds: 5 },
      { id: 'urn:mrn:imo:mmsi:100000002', name: 'TWIN', lat: -25.295, lon: 152.911, range_m: 140, age_seconds: 8 },
    ])

    const buttons = screen.getAllByRole('button', { name: 'AIS vessel: TWIN' })
    expect(buttons).toHaveLength(2)

    fireEvent.click(buttons[0])

    // Exactly one marker shows the selected (ring-2, larger) styling, and
    // the transient distance/bearing readout appears exactly once.
    expect(document.querySelectorAll('.h-11.w-11.ring-2')).toHaveLength(1)
    expect(screen.getAllByText('0 m · 0°')).toHaveLength(1)

    vi.useRealTimers()
  })

  it('restores center and zoom positioning from localStorage on mount', () => {
    localStorage.setItem('anchor-watch-map-center', JSON.stringify({ latitude: -25.2900, longitude: 152.9200 }))
    localStorage.setItem('anchor-watch-map-zoom', '16')

    renderMap()

    expect(lastInitialViewState).toEqual({
      latitude: -25.2900,
      longitude: 152.9200,
      zoom: 16,
    })

    localStorage.clear()
  })

  it('persists map positioning to localStorage on pan/zoom (onMoveEnd)', () => {
    localStorage.clear()
    renderMap()

    expect(lastMoveEndHandler).not.toBeNull()

    act(() => {
      lastMoveEndHandler!({
        viewState: {
          latitude: -25.3050,
          longitude: 152.9150,
          zoom: 15,
        },
      })
    })

    expect(JSON.parse(localStorage.getItem('anchor-watch-map-center')!)).toEqual({
      latitude: -25.3050,
      longitude: 152.9150,
    })
    expect(localStorage.getItem('anchor-watch-map-zoom')).toBe('15')

    localStorage.clear()
  })

  it('clears persisted center on re-centre button click so future loads center on anchor', () => {
    localStorage.setItem('anchor-watch-map-center', JSON.stringify({ latitude: -25.2900, longitude: 152.9200 }))

    renderMap()

    const recenterBtn = screen.getByRole('button', { name: 'Re-centre on anchor' })
    fireEvent.click(recenterBtn)

    expect(localStorage.getItem('anchor-watch-map-center')).toBeNull()

    localStorage.clear()
  })
})
