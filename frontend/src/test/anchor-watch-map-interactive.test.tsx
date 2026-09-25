import { act, render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { AnchorWatchMap } from '@/components/anchor-watch-map'
import type { NearbyVessel } from '@/hooks/use-nearby-vessels'
import type { RodeMethodResult } from '@/lib/rode-plan'

// Kiosk maps are display-only (the operator can put e.g. Nearby on the wall
// display, but there's no touchscreen to zoom/pan/rotate it with). These
// tests cover AnchorWatchMap's own half of that: the `interactive` prop
// hides every on-map control and drops maplibre's own interactive handlers,
// while the map keeps following the anchor session and updating markers.

vi.mock('maplibre-gl', () => ({
  default: {},
}))

let lastMapProps: { interactive?: boolean; onZoom?: unknown } | null = null
let lastMoveEndHandler: ((e: { viewState: { latitude: number; longitude: number; zoom: number } }) => void) | null = null

vi.mock('react-map-gl/maplibre', async () => {
  const React = await import('react')
  return {
    Map: React.forwardRef(
      (
        props: {
          children?: React.ReactNode
          interactive?: boolean
          onZoom?: unknown
          onMoveEnd?: (e: { viewState: { latitude: number; longitude: number; zoom: number } }) => void
        },
        ref: React.Ref<unknown>,
      ) => {
        lastMapProps = { interactive: props.interactive, onZoom: props.onZoom }
        lastMoveEndHandler = props.onMoveEnd ?? null
        React.useImperativeHandle(ref, () => ({
          getCanvas: () => ({ style: { cursor: 'grab' } }),
          getZoom: () => 14,
          easeTo: vi.fn(),
          getMap: () => ({
            isStyleLoaded: () => true,
            getSource: () => ({}),
          }),
        }))
        return <div data-testid="map-root">{props.children}</div>
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

beforeEach(() => {
  lastMapProps = null
  lastMoveEndHandler = null
  localStorage.clear()
})

const defaultAisVessels: NearbyVessel[] = [
  { id: 'urn:mrn:imo:mmsi:100000001', name: 'SPLURGE', lat: -25.2939, lon: 152.9103, range_m: 61, age_seconds: 5 },
]

const defaultScopeRecommendation: RodeMethodResult = {
  id: 'ratio',
  label: 'Ratio Method',
  recommendedRodeM: 30,
  scopeRatio: 5,
  note: 'note',
}

function renderMap(overrides: Partial<React.ComponentProps<typeof AnchorWatchMap>> = {}) {
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
      scopeRecommendation={defaultScopeRecommendation}
      isImperial={false}
      vesselTrail={() => []}
      aisVessels={defaultAisVessels}
      aisTrails={() => new Map()}
      radarTargets={[]}
      isDarkTheme={false}
      showImageryLayer
      onImageryToggle={() => undefined}
      onFullscreen={() => undefined}
      expandedControls
      {...overrides}
    />,
  )
}

describe('AnchorWatchMap interactive prop', () => {
  it('defaults to an interactive map with every control visible', () => {
    renderMap()

    expect(lastMapProps?.interactive).toBe(true)
    const controls = screen.getByTestId('anchor-watch-controls')
    expect(within(controls).getByRole('button', { name: 'Zoom in' })).toBeInTheDocument()
    expect(within(controls).getByRole('button', { name: 'Full screen' })).toBeInTheDocument()
    expect(within(controls).getByRole('button', { name: 'Re-centre on anchor' })).toBeInTheDocument()
  })

  it('passes interactive={false} straight through to the underlying map', () => {
    renderMap({ interactive: false })

    expect(lastMapProps?.interactive).toBe(false)
  })

  // Zoom, fullscreen, satellite, radar-echo and recentre are all
  // interaction-only controls - nothing about them is informational, so the
  // whole in-map control stack is dropped rather than picked apart one
  // button at a time.
  it('renders no on-map controls at all when not interactive', () => {
    renderMap({ interactive: false })

    expect(screen.queryByTestId('anchor-watch-controls')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Zoom in' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Zoom out' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Full screen' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Toggle satellite imagery' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Toggle radar echo overlay' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Re-centre on anchor' })).not.toBeInTheDocument()
  })

  // The metric overlay is purely informational (bearing/radius/depth/
  // current/scope readouts) and must keep showing regardless of interactivity.
  it('still shows the informational metric overlay when not interactive', () => {
    renderMap({ interactive: false })

    expect(screen.getByTestId('anchor-watch-metrics')).toBeInTheDocument()
  })
})

describe('AnchorWatchMap zoom tracking (per-tick cost)', () => {
  // onZoom used to fire setCurrentZoom on every animation frame during a
  // zoom gesture; this map has no need for that granularity (marker scale
  // and the satellite-imagery fade only need to settle once the gesture
  // ends), and every such update was also forcing a localStorage write (see
  // below). Fixed by tracking zoom only via onMoveEnd, which already fires
  // once per gesture rather than once per frame.
  it('does not wire a per-frame onZoom handler to the map', () => {
    renderMap()

    expect(lastMapProps?.onZoom).toBeUndefined()
  })

  it('writes zoom to localStorage only once the gesture ends (via onMoveEnd), not per frame', () => {
    renderMap()
    expect(lastMoveEndHandler).not.toBeNull()

    act(() => {
      lastMoveEndHandler!({ viewState: { latitude: -25.2939, longitude: 152.9103, zoom: 16 } })
    })

    // Tagged with the session the view is following, same shape as the
    // centre — this map is mounted with no anchorSetAt, so sessionId is null.
    expect(JSON.parse(localStorage.getItem('anchor-watch-map-zoom')!)).toEqual({ zoom: 16, sessionId: null })
  })

  it('does not persist zoom to localStorage at all when not interactive', () => {
    renderMap({ interactive: false })
    expect(lastMoveEndHandler).not.toBeNull()

    act(() => {
      lastMoveEndHandler!({ viewState: { latitude: -25.2939, longitude: 152.9103, zoom: 16 } })
    })

    expect(localStorage.getItem('anchor-watch-map-zoom')).toBeNull()
  })

  // Center persistence (the pan position, separate from zoom) is unaffected
  // by `interactive`: it's driven by the same onMoveEnd, which still fires
  // from a programmatic anchor-session recentre even with no user gesture
  // possible, and the operator still benefits from it restoring on reload.
  it('still persists the pan centre to localStorage when not interactive', () => {
    renderMap({ interactive: false })

    act(() => {
      lastMoveEndHandler!({ viewState: { latitude: -25.305, longitude: 152.915, zoom: 15 } })
    })

    expect(JSON.parse(localStorage.getItem('anchor-watch-map-center')!)).toEqual({
      latitude: -25.305,
      longitude: 152.915,
      sessionId: null,
    })
  })
})
