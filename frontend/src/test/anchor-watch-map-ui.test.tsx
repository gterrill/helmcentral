import { act, fireEvent, render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { AnchorWatchMap } from '@/components/anchor-watch-map'
import { PLACE_LABEL_LAYER_IDS } from '@/components/map-place-labels'
import type { NearbyVessel } from '@/hooks/use-nearby-vessels'
import type { RodeMethodResult } from '@/lib/rode-plan'

vi.mock('maplibre-gl', () => ({
  default: {},
}))

// True by default so every existing test below keeps mounting the real
// (mocked) <Map> — only the "without WebGL2" block flips this, and resets
// it in its own afterEach so no other test in this file is affected.
let mockHasWebGL2 = true
vi.mock('@/lib/webgl', () => ({
  hasWebGL2: () => mockHasWebGL2,
}))

let lastInitialViewState: { latitude: number; longitude: number; zoom: number } | null = null
let lastAttributionControl: unknown = undefined
let lastMoveEndHandler: ((e: { viewState: { latitude: number; longitude: number; zoom: number } }) => void) | null = null
let lastStyleDataHandler: (() => void) | null = null
const easeToMock = vi.fn()
// Toggled per-test to exercise the map-place-labels fail-fast guard - every
// other test wants the source present so its layers behave like the real
// Carto style.
let mockCartoSourcePresent = true

// Every prop a <Layer> mounts with, captured in full so tests can inspect
// the label layers' filter/paint/source-layer/minzoom without the mock
// having to hand-pick which fields matter. data-testid/data-before-id below
// stay exactly as they were so every pre-existing assertion keeps passing
// unchanged.
interface RecordedLayerProps {
  id: string
  type?: string
  source?: string
  'source-layer'?: string
  minzoom?: number
  maxzoom?: number
  beforeId?: string
  filter?: unknown
  layout?: Record<string, unknown>
  paint?: Record<string, unknown>
}
let recordedLayers: RecordedLayerProps[] = []

vi.mock('react-map-gl/maplibre', async () => {
  const React = await import('react')
  return {
    Map: React.forwardRef(
      (
        {
          children,
          initialViewState,
          attributionControl,
          onMoveEnd,
          onStyleData,
        }: {
          children?: React.ReactNode
          initialViewState?: { latitude: number; longitude: number; zoom: number }
          attributionControl?: unknown
          onMoveEnd?: (e: { viewState: { latitude: number; longitude: number; zoom: number } }) => void
          onStyleData?: () => void
        },
        ref: React.Ref<unknown>,
      ) => {
        lastInitialViewState = initialViewState ?? null
        lastAttributionControl = attributionControl
        lastMoveEndHandler = onMoveEnd ?? null
        lastStyleDataHandler = onStyleData ?? null
        React.useImperativeHandle(ref, () => ({
          getCanvas: () => ({ style: { cursor: 'grab' } }),
          getZoom: () => 14,
          easeTo: easeToMock,
          getMap: () => ({
            isStyleLoaded: () => true,
            getSource: (id: string) => (id === 'carto' && !mockCartoSourcePresent ? undefined : {}),
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
    Layer: (props: RecordedLayerProps) => {
      recordedLayers.push(props)
      return <div data-testid={`layer-${props.id}`} data-before-id={props.beforeId} />
    },
  }
})

beforeEach(() => {
  mockCartoSourcePresent = true
  recordedLayers = []
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
      radarTargets={[]}
      isDarkTheme={false}
      showImageryLayer
      onImageryToggle={() => undefined}
      onFullscreen={() => undefined}
      {...overrides}
    />
  )
}

function renderMap(aisVessels: NearbyVessel[] = defaultAisVessels, overrides: Partial<React.ComponentProps<typeof AnchorWatchMap>> = {}) {
  return render(mapElement(aisVessels, overrides))
}

describe('AnchorWatchMap controls and AIS selection', () => {
  // Design critique item 3: six buttons stacked in-tile clipped the bottom
  // two at tile height. The default (no expandedControls) collapses the
  // in-tile stack to fullscreen + zoom only; satellite, radar and recentre
  // are only reachable via expandedControls, which the fullscreen drawer
  // passes (anchor-watch-drawer.tsx) — every control stays reachable, just
  // relocated rather than deleted.
  it('collapses to fullscreen and zoom only when expandedControls is unset (the tile default)', () => {
    renderMap()

    const controls = screen.getByTestId('anchor-watch-controls')
    expect(within(controls).getByRole('button', { name: 'Full screen' })).toBeInTheDocument()
    expect(within(controls).getByRole('button', { name: 'Zoom in' })).toBeInTheDocument()
    expect(within(controls).getByRole('button', { name: 'Zoom out' })).toBeInTheDocument()
    expect(within(controls).queryByRole('button', { name: 'Toggle satellite imagery' })).not.toBeInTheDocument()
    expect(within(controls).queryByRole('button', { name: 'Toggle radar echo overlay' })).not.toBeInTheDocument()
    expect(within(controls).queryByRole('button', { name: 'Re-centre on anchor' })).not.toBeInTheDocument()
  })

  it('puts satellite below the zoom controls and removes control dividers, once expandedControls is set (the fullscreen drawer)', () => {
    renderMap(defaultAisVessels, { expandedControls: true })

    const controls = screen.getByTestId('anchor-watch-controls')
    const zoomOut = within(controls).getByRole('button', { name: 'Zoom out' })
    const satellite = within(controls).getByRole('button', { name: 'Toggle satellite imagery' })

    expect(zoomOut.nextElementSibling).toBe(satellite)
    expect(within(controls).getByRole('button', { name: 'Toggle radar echo overlay' })).toBeInTheDocument()
    expect(within(controls).getByRole('button', { name: 'Re-centre on anchor' })).toBeInTheDocument()
    expect(screen.getByTestId('anchor-watch-metrics').style.zIndex).toBe('2000')
  })

  // Both hosts are gaining a labeled Raise button with its own confirm
  // dialog; a one-tap unlabeled destructive icon next to that would be
  // inconsistent, so the map no longer offers its own stop control at all.
  // Same obligation as the routes map: the backend proxies Carto/OSM tiles,
  // so the credit has to be shown here too.
  // See docs/adr/0067-carto-basemap-proxy-and-offline-cache.md.
  it('shows the attribution control', () => {
    renderMap()
    expect(lastAttributionControl).not.toBe(false)
    expect(lastAttributionControl).toEqual({ compact: true })
  })

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
    localStorage.setItem('anchor-watch-map-zoom', JSON.stringify({ zoom: 16, sessionId: null }))

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

    // sessionId tags the pan with the anchorage it was made in; this map is
    // mounted without a set_at, so there is no session to tag it with.
    expect(JSON.parse(localStorage.getItem('anchor-watch-map-center')!)).toEqual({
      latitude: -25.3050,
      longitude: 152.9150,
      sessionId: null,
    })
    expect(JSON.parse(localStorage.getItem('anchor-watch-map-zoom')!)).toEqual({ zoom: 15, sessionId: null })

    localStorage.clear()
  })

  it('clears persisted center on re-centre button click so future loads center on anchor', () => {
    localStorage.setItem('anchor-watch-map-center', JSON.stringify({ latitude: -25.2900, longitude: 152.9200 }))

    renderMap(defaultAisVessels, { expandedControls: true })

    const recenterBtn = screen.getByRole('button', { name: 'Re-centre on anchor' })
    fireEvent.click(recenterBtn)

    expect(localStorage.getItem('anchor-watch-map-center')).toBeNull()

    localStorage.clear()
  })
})

describe('AnchorWatchMap place-name labels', () => {
  it('mounts all five label layers on the existing carto source, and overImagery tracks showImageryLayer', () => {
    const { rerender } = renderMap(defaultAisVessels, { showImageryLayer: false })

    for (const id of PLACE_LABEL_LAYER_IDS) {
      expect(screen.getByTestId(`layer-${id}`)).toBeInTheDocument()
    }

    const water = recordedLayers.filter((l) => l.id === 'place-names-water').at(-1)
    expect(water?.source).toBe('carto')
    // showImageryLayer is off: theme-native water color, not the hybrid
    // white-on-black override.
    expect(water?.paint).toMatchObject({ 'text-color': '#7a96a0' })

    recordedLayers = []
    rerender(mapElement(defaultAisVessels, { showImageryLayer: true }))

    const waterOverImagery = recordedLayers.filter((l) => l.id === 'place-names-water').at(-1)
    expect(waterOverImagery?.paint).toMatchObject({
      'text-color': '#ffffff',
      'text-halo-color': '#000000',
      'text-halo-width': 1.5,
    })
  })

  it('gives each label layer its source-layer, filter, and minzoom', () => {
    renderMap()

    const byId = (id: string) => recordedLayers.find((l) => l.id === id)

    expect(byId('place-names-water')).toMatchObject({ 'source-layer': 'water_name', minzoom: 9 })
    expect(byId('place-names-island')).toMatchObject({ 'source-layer': 'place', minzoom: 9 })
    expect(byId('place-names-harbor')).toMatchObject({ 'source-layer': 'poi', minzoom: 14 })
    expect(byId('place-names-peak')).toMatchObject({ 'source-layer': 'mountain_peak', minzoom: 12 })
    expect(byId('place-names-town-topup')).toMatchObject({ 'source-layer': 'place', minzoom: 14 })
  })

  it('mounts the label layers above alarm-circle-fill and its rasters - no beforeId means it lands on top of everything mounted before it', () => {
    // This map pins its rasters beforeId="alarm-circle-fill", i.e. above
    // the whole base style, so with imagery on, Carto's own labels (never
    // drawn anyway - see ADR 0065) would be buried under the raster
    // regardless. Mounting these label layers with no beforeId, right after
    // the alarm-circle Source, is what keeps names visible above the
    // satellite raster on this map.
    renderMap()

    for (const id of PLACE_LABEL_LAYER_IDS) {
      expect(screen.getByTestId(`layer-${id}`).dataset.beforeId).toBeUndefined()
    }
  })

  it('logs an explicit error once when the carto vector source is missing from the loaded style', () => {
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    mockCartoSourcePresent = false

    renderMap()
    act(() => {
      lastStyleDataHandler?.()
    })
    act(() => {
      lastStyleDataHandler?.()
    })

    expect(errorSpy).toHaveBeenCalledWith(expect.stringContaining('[map-place-labels]'))
    // Guarded by a ref so a repeated styledata event (which fires many
    // times per style load) doesn't spam the console once per tick.
    expect(errorSpy).toHaveBeenCalledTimes(1)
    errorSpy.mockRestore()
  })

  it('does not log a missing-source error when the carto source is present', () => {
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => undefined)

    renderMap()
    act(() => {
      lastStyleDataHandler?.()
    })

    expect(errorSpy).not.toHaveBeenCalled()
    errorSpy.mockRestore()
  })
})

// Groundwork for an always-on anchor map: the host still gates on
// anchor-set today, so this path only runs under test for now.
describe('AnchorWatchMap with no anchor set', () => {
  it('renders no anchor marker', () => {
    renderMap(defaultAisVessels, { anchorLat: null, anchorLon: null })

    expect(screen.queryByLabelText('Anchor position')).not.toBeInTheDocument()
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
    renderMap(defaultAisVessels, { anchorLat: null, anchorLon: null, expandedControls: true })

    fireEvent.click(screen.getByRole('button', { name: 'Re-centre on anchor' }))

    expect(easeToMock).toHaveBeenLastCalledWith({ center: [152.9103, -25.2939], duration: 600 })
  })

  // Design critique item 2: Bearing/Radius previously rendered "— °"/"— m"
  // at the same visual weight as a live reading whenever no anchor was
  // down. Both rows are dropped entirely in that state now, rather than
  // dashed — there's nothing to promote to a placeholder yet.
  it('drops the Bearing and Radius rows entirely rather than showing a dash', () => {
    renderMap(defaultAisVessels, { anchorLat: null, anchorLon: null })

    const metrics = screen.getByTestId('anchor-watch-metrics')
    expect(within(metrics).queryByText('Bearing')).not.toBeInTheDocument()
    expect(within(metrics).queryByText('Radius')).not.toBeInTheDocument()
  })

  // Depth/Current/Scope can be genuinely unavailable for reasons that have
  // nothing to do with anchor state (no sounder, no scope recommendation
  // yet) — those rows must keep rendering, dash and all, rather than being
  // swept up by the no-anchor suppression above.
  it('keeps Depth, Current and Scope rendering with no anchor set', () => {
    renderMap(defaultAisVessels, {
      anchorLat: null,
      anchorLon: null,
      depthMeters: null,
      currentDriftKts: null,
      scopeRecommendation: null,
    })

    const metrics = screen.getByTestId('anchor-watch-metrics')
    expect(within(metrics).getByText('Depth')).toBeInTheDocument()
    expect(within(metrics).getByText('Current')).toBeInTheDocument()
    expect(within(metrics).getByText('Scope')).toBeInTheDocument()
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

// Design critique item 1: the panel's translucent ground (bg-black/50, or
// bg-black/35 while editing) measured 4.1:1 against real satellite imagery,
// short of the 4.5:1 AGENTS.md requires for text this small. It's also
// where Distance used to live — dropped now that both hosts promote a
// distance KPI above the map (anchor-watch-tile.tsx, anchor-watch-drawer.tsx),
// so the map's own overlay isn't repeating the headline number it no longer
// owns.
describe('AnchorWatchMap metric overlay contrast and duplication', () => {
  it('drops the Distance row entirely — that reading is promoted above the map on every host now', () => {
    renderMap()

    expect(within(screen.getByTestId('anchor-watch-metrics')).queryByText('Distance')).not.toBeInTheDocument()
  })

  it('uses a near-opaque scrim rather than the old translucent ground', () => {
    renderMap()
    const metrics = screen.getByTestId('anchor-watch-metrics')
    expect(metrics.className).not.toMatch(/bg-black\/[0-8]?[0-9](?!\d)/)
    expect(metrics.className).toMatch(/bg-black\/9\d/)

    // Editing (reposition, radius drag) used to drop the panel to
    // bg-black/35 while active — even more transparent during exactly the
    // state where the operator was reading the overlay most closely. That
    // whole edit mode is gone now (impeccable P0s), and so is its lighter
    // ground — tapping the anchor marker (now a non-interactive "Anchor
    // position" div) does nothing at all, including to this panel.
    fireEvent.click(screen.getByLabelText('Anchor position'))
    expect(screen.getByTestId('anchor-watch-metrics').className).toMatch(/bg-black\/9\d/)
  })
})

describe('AnchorWatchMap without WebGL2', () => {
  afterEach(() => {
    mockHasWebGL2 = true
  })

  it('shows the one-line fallback panel instead of mounting a map, and keeps the metric overlay live', () => {
    mockHasWebGL2 = false

    renderMap()

    expect(screen.queryByTestId('map-root')).not.toBeInTheDocument()
    expect(screen.getByTestId('anchor-watch-map-webgl2-fallback')).toHaveTextContent(
      'Map needs WebGL2, which this browser does not provide',
    )
    // The overlay reads straight off props (depth, current, scope), not off
    // any map state, so it keeps reporting real numbers with no map mounted.
    expect(within(screen.getByTestId('anchor-watch-metrics')).getByText('3.2')).toBeInTheDocument()
  })
})
