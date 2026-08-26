import { act, fireEvent, render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { AnchorWatchMap } from '@/components/anchor-watch-map'
import type { NearbyVessel } from '@/hooks/use-nearby-vessels'
import type { RodeMethodResult } from '@/lib/rode-plan'

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

const defaultScopeRecommendation: RodeMethodResult = {
  id: 'ratio',
  label: 'Ratio Method',
  recommendedRodeM: 30,
  scopeRatio: 5,
  note: 'Depth 3.2 m (sounder only) · Wind 15 kts · 5:1',
}

function mapElement(aisVessels: NearbyVessel[] = defaultAisVessels, overrides: Partial<React.ComponentProps<typeof AnchorWatchMap>> = {}) {
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
      scopeRecommendation={defaultScopeRecommendation}
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
      {...overrides}
    />
  )
}

function renderMap(aisVessels: NearbyVessel[] = defaultAisVessels, overrides: Partial<React.ComponentProps<typeof AnchorWatchMap>> = {}) {
  return render(mapElement(aisVessels, overrides))
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

  // Both hosts are gaining a labeled Raise button with its own confirm
  // dialog; a one-tap unlabeled destructive icon next to that would be
  // inconsistent, so the map no longer offers its own stop control at all.
  it('does not render a Stop anchor watch button', () => {
    renderMap()

    expect(screen.queryByRole('button', { name: 'Stop anchor watch' })).not.toBeInTheDocument()
  })

  it('expands a clicked AIS vessel for three seconds, and keeps its range on show', () => {
    vi.useFakeTimers()

    renderMap()

    // Range and bearing are always on, the same as a placemark's — you
    // should not have to tap a vessel to find out how close it is.
    expect(screen.getByText('0 m · 0°')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'AIS vessel: SPLURGE' }))
    expect(document.querySelector('.h-11.w-11')).not.toBeNull()

    act(() => {
      vi.advanceTimersByTime(3000)
    })

    // Only the expansion is transient; the range stays put.
    expect(document.querySelector('.h-11.w-11')).toBeNull()
    expect(screen.getByText('0 m · 0°')).toBeInTheDocument()

    vi.useRealTimers()
  })

  it('updates an AIS vessel range as the boats move, rather than freezing it', () => {
    // ~100m north of us, then ~150m north: the number has to follow, the
    // same way a placemark's does.
    const vesselAt = (lat: number) => [
      { id: 'urn:mrn:imo:mmsi:100000001', name: 'SPLURGE', lat, lon: 152.9103, range_m: 0, age_seconds: 5 },
    ]

    const { rerender } = renderMap(vesselAt(-25.29300))
    expect(screen.getByText('100 m · 0°')).toBeInTheDocument()

    rerender(mapElement(vesselAt(-25.29255)))
    expect(screen.getByText('150 m · 0°')).toBeInTheDocument()
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

  // Regression test: selection previously matched on a label that carried
  // the vessel's name, so two vessels sharing a name both lit up as
  // "selected" when either was clicked. Selection compares on vessel.id.
  it('selects only the clicked marker when two AIS vessels share a name', () => {
    vi.useFakeTimers()

    renderMap([
      { id: 'urn:mrn:imo:mmsi:100000001', name: 'TWIN', lat: -25.2939, lon: 152.9103, range_m: 61, age_seconds: 5 },
      { id: 'urn:mrn:imo:mmsi:100000002', name: 'TWIN', lat: -25.295, lon: 152.911, range_m: 140, age_seconds: 8 },
    ])

    const buttons = screen.getAllByRole('button', { name: 'AIS vessel: TWIN' })
    expect(buttons).toHaveLength(2)

    fireEvent.click(buttons[0])

    // Exactly one marker shows the selected (ring-2, larger) styling. Both
    // still show their own range, which was never selection-dependent.
    expect(document.querySelectorAll('.h-11.w-11.ring-2')).toHaveLength(1)
    expect(screen.getAllByText('0 m · 0°')).toHaveLength(1)
    expect(screen.getByText('141 m · 150°')).toBeInTheDocument()

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

// Groundwork for an always-on anchor map: the host still gates on
// anchor-set today, so this path only runs under test for now.
describe('AnchorWatchMap with no anchor set', () => {
  it('renders no anchor marker', () => {
    renderMap(defaultAisVessels, { anchorLat: null, anchorLon: null })

    expect(
      screen.queryByRole('button', { name: 'Anchor position — click to reposition' }),
    ).not.toBeInTheDocument()
  })

  it('keeps the alarm-circle layer mounted so the raster layers still have a beforeId target', () => {
    // If this layer unmounted with no anchor, world-imagery and openseamap
    // (which pin beforeId="alarm-circle-fill") would never attach at all,
    // and layer order would scramble the moment an anchor later appeared.
    renderMap(defaultAisVessels, { anchorLat: null, anchorLon: null })

    expect(screen.getByTestId('layer-alarm-circle-fill')).toBeInTheDocument()
    expect(screen.getByTestId('layer-world-imagery-layer').dataset.beforeId).toBe('alarm-circle-fill')
    expect(screen.getByTestId('layer-openseamap-layer').dataset.beforeId).toBe('alarm-circle-fill')
  })

  it('re-centres on the vessel rather than the absent anchor', () => {
    renderMap(defaultAisVessels, { anchorLat: null, anchorLon: null })

    fireEvent.click(screen.getByRole('button', { name: 'Re-centre on anchor' }))

    expect(easeToMock).toHaveBeenLastCalledWith({ center: [152.9103, -25.2939], duration: 600 })
  })

  it('shows — for Radius rather than the inactive radiusMeters default', () => {
    renderMap(defaultAisVessels, { anchorLat: null, anchorLon: null })

    const metrics = screen.getByTestId('anchor-watch-metrics')
    const radiusLabel = within(metrics).getByText('Radius')
    expect(radiusLabel.nextElementSibling).toHaveTextContent('—')
  })
})

// The recommended-scope readout used to live in a box below the map (ADR
// 0059 §3); it now renders as a Scope row inside this metric overlay,
// directly under Current, so both hosts (tile and fullscreen drawer) get it
// for free by passing scopeRecommendation through.
describe('AnchorWatchMap Scope row', () => {
  function scopeRow() {
    const metrics = screen.getByTestId('anchor-watch-metrics')
    const currentRow = within(metrics).getByText('Current').parentElement as HTMLElement
    return currentRow.nextElementSibling as HTMLElement
  }

  it('renders Scope directly under Current with the recommended rode figure and its plain unit', () => {
    renderMap()

    const row = scopeRow()
    expect(within(row).getByText('Scope')).toBeInTheDocument()
    expect(row).toHaveTextContent('30')
    expect(row).toHaveTextContent('m')
    expect(row).not.toHaveTextContent('5:1')
    expect(row).not.toHaveTextContent('min')
  })

  // The merged "3:1 min" tag is gone from the row itself — the ratio and the
  // MIN_SCOPE_RATIO floor marker are only reachable via the row's tooltip now,
  // riding along on the same `note` the row already carried as its title.
  it('still surfaces the ratio and the floor marker for a floored recommendation, via the row tooltip', () => {
    const floored: RodeMethodResult = {
      id: 'catenary',
      label: 'Catenary Method',
      recommendedRodeM: 34.5,
      scopeRatio: 3,
      note: 'Depth 10.0 m (tide-corrected) · Wind 3 kts · Chain 12mm · Hull power cat · 3:1 minimum',
    }
    renderMap(defaultAisVessels, { scopeRecommendation: floored })

    const row = scopeRow()
    expect(row).toHaveAttribute('title', floored.note)
    expect(row).not.toHaveTextContent('3:1 minimum')
    expect(row).not.toHaveTextContent('min')
  })

  it('converts the Scope rode figure to feet under imperial units', () => {
    renderMap(defaultAisVessels, { isImperial: true })

    const row = scopeRow()
    // 30 m * 3.28084 = 98.4 ft, rounded to 98.
    expect(row).toHaveTextContent('98')
    expect(row).toHaveTextContent('ft')
    expect(row).not.toHaveTextContent('5:1')
  })

  it('carries the full note as a tooltip on the row when the recommendation is available', () => {
    renderMap()

    expect(scopeRow()).toHaveAttribute('title', defaultScopeRecommendation.note)
  })

  it('shows the reason instead of a dash — never a bare dash — when the recommendation is unavailable', () => {
    const unavailable: RodeMethodResult = {
      id: 'ratio',
      label: 'Ratio Method',
      recommendedRodeM: 0,
      scopeRatio: 0,
      note: '',
      unavailableReason: 'bow roller height not configured',
    }
    renderMap(defaultAisVessels, { scopeRecommendation: unavailable })

    const row = scopeRow()
    expect(within(row).getByText('—')).toBeInTheDocument()
    const reasonEl = within(row).getByText('bow roller height not configured')
    expect(reasonEl).toHaveAttribute('title', 'bow roller height not configured')
    expect(reasonEl.className).toContain('truncate')
  })

  it('shows a dash with no crash when the scopeRecommendation prop itself is null', () => {
    renderMap(defaultAisVessels, { scopeRecommendation: null })

    expect(within(scopeRow()).getByText('—')).toBeInTheDocument()
  })
})
