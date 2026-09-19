import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { RoutePlannerMap, HYBRID_HIDDEN_LAYER_IDS, HYBRID_LABEL_LAYER_IDS } from '@/components/route-planner-map'
import { PLACE_LABEL_LAYER_IDS } from '@/components/map-place-labels'

vi.mock('maplibre-gl', () => ({
  default: {},
}))

const startPrefetchMock = vi.fn().mockResolvedValue('job-1')
let mockPrefetching = false
let mockProgress: { done: number; total: number } | null = null
let mockPrefetchError: string | null = null

vi.mock('@/hooks/use-imagery-prefetch', () => ({
  useImageryPrefetch: () => ({
    prefetching: mockPrefetching,
    progress: mockProgress,
    error: mockPrefetchError,
    startPrefetch: startPrefetchMock,
  }),
}))

let mockBounds = { west: -1, south: -1, east: 1, north: 1 }

let lastMapClickHandler: ((e: { lngLat: { lat: number; lng: number } }) => void) | null = null
let lastZoomHandler: (() => void) | null = null
let lastStyleDataHandler: (() => void) | null = null
let lastInitialViewState: { latitude: number; longitude: number; zoom: number } | null = null
let lastMapStyle: string | null = null
let lastAttributionControl: unknown = undefined
const easeToMock = vi.fn()
const setLayoutPropertyMock = vi.fn()
const setPaintPropertyMock = vi.fn()
const getPaintPropertyMock = vi.fn((_id: string, prop: string) => `original-${prop}`)
let mockZoom = 12
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
          onClick,
          onZoom,
          onStyleData,
          initialViewState,
          mapStyle,
          attributionControl,
        }: {
          children?: React.ReactNode
          onClick?: (e: { lngLat: { lat: number; lng: number } }) => void
          onZoom?: () => void
          onLoad?: () => void
          onStyleData?: () => void
          initialViewState?: { latitude: number; longitude: number; zoom: number }
          mapStyle?: unknown
          attributionControl?: unknown
        },
        ref: React.Ref<unknown>,
      ) => {
        lastMapClickHandler = onClick ?? null
        lastZoomHandler = onZoom ?? null
        lastStyleDataHandler = onStyleData ?? null
        lastInitialViewState = initialViewState ?? null
        lastMapStyle = typeof mapStyle === 'string' ? mapStyle : null
        lastAttributionControl = attributionControl
        React.useImperativeHandle(ref, () => ({
          getCanvas: () => ({ style: { cursor: 'grab' } }),
          getZoom: () => mockZoom,
          easeTo: easeToMock,
          fitBounds: () => undefined,
          getBounds: () => ({
            getWest: () => mockBounds.west,
            getSouth: () => mockBounds.south,
            getEast: () => mockBounds.east,
            getNorth: () => mockBounds.north,
          }),
          getMap: () => ({
            isStyleLoaded: () => true,
            getLayer: () => ({}),
            getSource: (id: string) => (id === 'carto' && !mockCartoSourcePresent ? undefined : {}),
            setLayoutProperty: setLayoutPropertyMock,
            setPaintProperty: setPaintPropertyMock,
            getPaintProperty: getPaintPropertyMock,
            getStyle: () => ({ layers: [{ id: 'background' }] }),
          }),
        }))
        return <div data-testid="map-root">{children}</div>
      },
    ),
    Marker: ({
      children,
      onClick,
    }: {
      children?: React.ReactNode
      onClick?: (e: React.MouseEvent) => void
    }) => (
      <div onClick={onClick as React.MouseEventHandler}>{children}</div>
    ),
    Source: ({
      children,
      id,
      tiles,
      bounds,
    }: {
      children?: React.ReactNode
      id: string
      tiles?: string[]
      bounds?: number[]
    }) => (
      <div
        data-testid={`source-${id}`}
        data-tiles={tiles ? tiles.join(',') : undefined}
        data-bounds={bounds ? bounds.join(',') : undefined}
      >
        {children}
      </div>
    ),
    Layer: (props: RecordedLayerProps) => {
      recordedLayers.push(props)
      return <div data-testid={`layer-${props.id}`} data-before-id={props.beforeId} />
    },
  }
})

function clickMap(lat: number, lng: number) {
  fireEvent.click(screen.getByTestId('map-root'))
  lastMapClickHandler?.({ lngLat: { lat, lng } })
}

describe('RoutePlannerMap', () => {
  beforeEach(() => {
    easeToMock.mockClear()
    setLayoutPropertyMock.mockClear()
    setPaintPropertyMock.mockClear()
    getPaintPropertyMock.mockClear()
    startPrefetchMock.mockClear()
    startPrefetchMock.mockResolvedValue('job-1')
    mockPrefetching = false
    mockProgress = null
    mockPrefetchError = null
    mockBounds = { west: -1, south: -1, east: 1, north: 1 }
    localStorage.clear()
    mockZoom = 12
    mockCartoSourcePresent = true
    recordedLayers = []
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({ type: 'FeatureCollection', features: [] }),
      }),
    )
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  // The waypoint markers, controls and scale stack at z-index 100 to 2100
  // to sit above the MapLibre canvas. Without a stacking context on the
  // wrapper those values escape into the page and beat the Sheet
  // primitive's z-[70], so the controls drew through the mobile sidebar
  // and the help sheet. `isolate` keeps them inside.
  it('isolates its overlays so they cannot draw above a sheet', () => {
    render(
      <RoutePlannerMap
        waypoints={[]}
        onWaypointsChange={() => undefined}
        isDarkTheme={false}
        vesselLat={-25.29}
        vesselLon={152.91}
      />,
    )

    expect(screen.getByTestId('route-planner-map').className.split(/\s+/)).toContain('isolate')
  })

  it('centers the initial view on the vessel position when there are no waypoints', () => {
    render(
      <RoutePlannerMap
        waypoints={[]}
        onWaypointsChange={() => undefined}
        isDarkTheme={false}
        vesselLat={-25.29}
        vesselLon={152.91}
      />,
    )

    expect(lastInitialViewState).toEqual({ latitude: -25.29, longitude: 152.91, zoom: 11 })
  })

  it('prefers the first waypoint over the vessel position for the initial view', () => {
    render(
      <RoutePlannerMap
        waypoints={[{ lat: 1, lon: 2 }]}
        onWaypointsChange={() => undefined}
        isDarkTheme={false}
        vesselLat={-25.29}
        vesselLon={152.91}
      />,
    )

    expect(lastInitialViewState).toEqual({ latitude: 1, longitude: 2, zoom: 12 })
  })

  // The basemap style must come from our own backend, not basemaps.cartocdn.com.
  // Reverting this to the CDN URL silently breaks the offline chart: MapLibre
  // fetches the style before anything else, and a failed style fetch means the
  // map never initialises and no cached tile is ever requested.
  // See docs/adr/0067-carto-basemap-proxy-and-offline-cache.md.
  // Carto and OpenStreetMap require attribution, and the backend now proxies
  // their tiles, so displaying the credit is squarely our obligation rather
  // than the CDN's. MapLibre's own control aggregates the attribution every
  // active source declares (Carto/OSM via the proxied TileJSON, OpenSeaMap,
  // Esri), so it must not be disabled.
  // See docs/adr/0067-carto-basemap-proxy-and-offline-cache.md.
  it('shows the attribution control', () => {
    render(<RoutePlannerMap waypoints={[]} onWaypointsChange={() => {}} isDarkTheme={false} />)
    expect(lastAttributionControl).not.toBe(false)
    expect(lastAttributionControl).toEqual({ compact: true })
  })

  it('loads the basemap style from the same-origin proxy', () => {
    render(<RoutePlannerMap waypoints={[]} onWaypointsChange={() => {}} isDarkTheme={false} />)
    expect(lastMapStyle).toBe('/api/basemap/style/positron')

    render(<RoutePlannerMap waypoints={[]} onWaypointsChange={() => {}} isDarkTheme />)
    expect(lastMapStyle).toBe('/api/basemap/style/dark-matter')
  })

  it('recenters on the vessel when the "center on current position" control is used', () => {
    render(
      <RoutePlannerMap
        waypoints={[]}
        onWaypointsChange={() => undefined}
        isDarkTheme={false}
        vesselLat={-25.29}
        vesselLon={152.91}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Center on current position' }))

    expect(easeToMock).toHaveBeenCalledWith({ center: [152.91, -25.29], zoom: 11, duration: 400 })
  })

  it('auto-recenters once if the vessel GPS fix arrives after mount', () => {
    const { rerender } = render(
      <RoutePlannerMap waypoints={[]} onWaypointsChange={() => undefined} isDarkTheme={false} />,
    )

    expect(easeToMock).not.toHaveBeenCalled()

    rerender(
      <RoutePlannerMap
        waypoints={[]}
        onWaypointsChange={() => undefined}
        isDarkTheme={false}
        vesselLat={-25.29}
        vesselLon={152.91}
      />,
    )

    expect(easeToMock).toHaveBeenCalledWith({ center: [152.91, -25.29], zoom: 11, duration: 400 })
  })

  it('does not auto-recenter once the user has started placing waypoints', () => {
    const { rerender } = render(
      <RoutePlannerMap
        waypoints={[{ lat: 5, lon: 6 }]}
        onWaypointsChange={() => undefined}
        isDarkTheme={false}
      />,
    )

    rerender(
      <RoutePlannerMap
        waypoints={[{ lat: 5, lon: 6 }]}
        onWaypointsChange={() => undefined}
        isDarkTheme={false}
        vesselLat={-25.29}
        vesselLon={152.91}
      />,
    )

    expect(easeToMock).not.toHaveBeenCalled()
  })

  it('shows a hint when there are no waypoints', () => {
    render(<RoutePlannerMap waypoints={[]} onWaypointsChange={() => undefined} isDarkTheme={false} />)
    expect(screen.getByText('Click the map to add your first waypoint')).toBeInTheDocument()
  })

  it('appends a waypoint when the map is clicked', () => {
    const onWaypointsChange = vi.fn()
    render(<RoutePlannerMap waypoints={[]} onWaypointsChange={onWaypointsChange} isDarkTheme={false} />)

    clickMap(10, 20)

    expect(onWaypointsChange).toHaveBeenCalledWith([{ lat: 10, lon: 20 }])
  })

  it('renders a numbered marker per waypoint', () => {
    render(
      <RoutePlannerMap
        waypoints={[{ lat: 0, lon: 0 }, { lat: 1, lon: 1 }]}
        onWaypointsChange={() => undefined}
        isDarkTheme={false}
      />,
    )

    expect(screen.getByRole('button', { name: /Waypoint 1/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Waypoint 2/ })).toBeInTheDocument()
  })

  it('shows the route line source once there are 2+ waypoints', () => {
    render(
      <RoutePlannerMap
        waypoints={[{ lat: 0, lon: 0 }, { lat: 1, lon: 1 }]}
        onWaypointsChange={() => undefined}
        isDarkTheme={false}
      />,
    )

    expect(screen.getByTestId('source-route-line')).toBeInTheDocument()
  })

  it('selecting then deleting a waypoint removes it', () => {
    const onWaypointsChange = vi.fn()
    render(
      <RoutePlannerMap
        waypoints={[{ lat: 0, lon: 0 }, { lat: 1, lon: 1 }]}
        onWaypointsChange={onWaypointsChange}
        isDarkTheme={false}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: /Waypoint 1/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))

    expect(onWaypointsChange).toHaveBeenCalledWith([{ lat: 1, lon: 1 }])
  })

  it('does not show the satellite imagery source until toggled on', () => {
    render(<RoutePlannerMap waypoints={[]} onWaypointsChange={() => undefined} isDarkTheme={false} />)

    expect(screen.queryByTestId('source-world-imagery')).not.toBeInTheDocument()
  })

  it('does not show satellite imagery below the zoom handoff even when toggled on', () => {
    render(<RoutePlannerMap waypoints={[]} onWaypointsChange={() => undefined} isDarkTheme={false} />)

    fireEvent.click(screen.getByRole('button', { name: 'Toggle satellite imagery' }))

    expect(screen.queryByTestId('source-world-imagery')).not.toBeInTheDocument()
  })

  it('shows the Esri World Imagery layer once zoomed in past the handoff', () => {
    render(<RoutePlannerMap waypoints={[]} onWaypointsChange={() => undefined} isDarkTheme={false} />)

    fireEvent.click(screen.getByRole('button', { name: 'Toggle satellite imagery' }))
    mockZoom = 12
    act(() => {
      lastZoomHandler?.()
    })

    expect(screen.getByTestId('source-world-imagery')).toBeInTheDocument()
  })

  it('hides satellite imagery again when toggled off', () => {
    render(<RoutePlannerMap waypoints={[]} onWaypointsChange={() => undefined} isDarkTheme={false} />)

    const toggle = screen.getByRole('button', { name: 'Toggle satellite imagery' })
    fireEvent.click(toggle)
    mockZoom = 12
    act(() => {
      lastZoomHandler?.()
    })
    expect(screen.getByTestId('source-world-imagery')).toBeInTheDocument()

    fireEvent.click(toggle)
    expect(screen.queryByTestId('source-world-imagery')).not.toBeInTheDocument()
  })

  it('turns hybrid satellite off when the app theme changes (avoids a real MapLibre reload rendering bug)', () => {
    const { rerender } = render(<RoutePlannerMap waypoints={[]} onWaypointsChange={() => undefined} isDarkTheme={false} />)

    const toggle = screen.getByRole('button', { name: 'Toggle satellite imagery' })
    fireEvent.click(toggle)
    mockZoom = 12
    act(() => {
      lastZoomHandler?.()
    })
    expect(screen.getByTestId('source-world-imagery')).toBeInTheDocument()

    rerender(<RoutePlannerMap waypoints={[]} onWaypointsChange={() => undefined} isDarkTheme={true} />)

    expect(screen.queryByTestId('source-world-imagery')).not.toBeInTheDocument()
    expect(window.localStorage.getItem('routePlanner.imagery.enabled')).toBe('false')
  })

  it('shows the GSHHG coastline fallback when no chart is available', async () => {
    render(
      <RoutePlannerMap
        waypoints={[]}
        onWaypointsChange={() => undefined}
        isDarkTheme={false}
        chartAvailable={false}
      />,
    )

    expect(await screen.findByTestId('source-gshhg-coastline')).toBeInTheDocument()
    expect(screen.getByText('No chart data — reference coastline only')).toBeInTheDocument()
  })

  it('does not show the GSHHG coastline fallback when a chart is available', async () => {
    render(
      <RoutePlannerMap
        waypoints={[]}
        onWaypointsChange={() => undefined}
        isDarkTheme={false}
        chartAvailable={true}
      />,
    )

    await waitFor(() => expect(screen.queryByTestId('layer-gshhg-coastline-fill')).not.toBeInTheDocument())
    expect(screen.queryByTestId('source-gshhg-coastline')).not.toBeInTheDocument()
    expect(screen.queryByText('No chart data — reference coastline only')).not.toBeInTheDocument()
  })

  it('logs an explicit fallback-used message once the coastline layer renders', async () => {
    const infoSpy = vi.spyOn(console, 'info').mockImplementation(() => undefined)

    render(
      <RoutePlannerMap
        waypoints={[]}
        onWaypointsChange={() => undefined}
        isDarkTheme={false}
        chartAvailable={false}
      />,
    )

    await screen.findByTestId('source-gshhg-coastline')
    expect(infoSpy).toHaveBeenCalledWith(expect.stringContaining('[gshhg-coastline-fallback]'))
  })

  it('does not log a fallback message when a chart is available', async () => {
    const infoSpy = vi.spyOn(console, 'info').mockImplementation(() => undefined)

    render(
      <RoutePlannerMap
        waypoints={[]}
        onWaypointsChange={() => undefined}
        isDarkTheme={false}
        chartAvailable={true}
      />,
    )

    await waitFor(() => expect(screen.queryByTestId('source-gshhg-coastline')).not.toBeInTheDocument())
    expect(infoSpy).not.toHaveBeenCalled()
  })

  it('anchors imagery and seamark raster layers below the invisible overlay anchor', () => {
    // react-map-gl's addLayer call has no awareness of JSX sibling order - a
    // raster layer added later (e.g. world-imagery toggled on mid-session)
    // is otherwise appended on top of the whole stack, covering the route
    // line and coastline fallback. beforeId pins each raster layer below
    // the always-present anchor layer instead of relying on mount-order
    // luck.
    render(
      <RoutePlannerMap
        waypoints={[]}
        onWaypointsChange={() => undefined}
        isDarkTheme={false}
      />,
    )

    expect(screen.getByTestId('layer-raster-overlay-anchor')).toBeInTheDocument()
    expect(screen.getByTestId('layer-openseamap-layer').dataset.beforeId).toBe('raster-overlay-anchor')

    fireEvent.click(screen.getByRole('button', { name: 'Toggle satellite imagery' }))
    mockZoom = 12
    act(() => {
      lastZoomHandler?.()
    })
    // World imagery now slots in below the base style's first layer id
    // (computed dynamically from the loaded style, 'background' per the
    // mocked getStyle()), not the raster-overlay-anchor used by the other
    // raster overlays.
    expect(screen.getByTestId('layer-world-imagery-layer').dataset.beforeId).toBe('background')
  })

  it('calls setLayoutProperty with visibility "none" for every hidden layer id when hybrid satellite is toggled on', () => {
    render(<RoutePlannerMap waypoints={[]} onWaypointsChange={() => undefined} isDarkTheme={false} />)

    setLayoutPropertyMock.mockClear()
    fireEvent.click(screen.getByRole('button', { name: 'Toggle satellite imagery' }))

    for (const id of HYBRID_HIDDEN_LAYER_IDS) {
      expect(setLayoutPropertyMock).toHaveBeenCalledWith(id, 'visibility', 'none')
    }
  })

  it('calls setLayoutProperty with visibility "visible" for every hidden layer id when hybrid satellite is toggled back off', () => {
    render(<RoutePlannerMap waypoints={[]} onWaypointsChange={() => undefined} isDarkTheme={false} />)

    const toggle = screen.getByRole('button', { name: 'Toggle satellite imagery' })
    fireEvent.click(toggle)
    setLayoutPropertyMock.mockClear()
    fireEvent.click(toggle)

    for (const id of HYBRID_HIDDEN_LAYER_IDS) {
      expect(setLayoutPropertyMock).toHaveBeenCalledWith(id, 'visibility', 'visible')
    }
  })

  it('reapplies hidden-layer visibility when onStyleData fires again while hybrid satellite is on (theme reload regression)', () => {
    render(<RoutePlannerMap waypoints={[]} onWaypointsChange={() => undefined} isDarkTheme={false} />)

    fireEvent.click(screen.getByRole('button', { name: 'Toggle satellite imagery' }))
    setLayoutPropertyMock.mockClear()

    act(() => {
      lastStyleDataHandler?.()
    })

    for (const id of HYBRID_HIDDEN_LAYER_IDS) {
      expect(setLayoutPropertyMock).toHaveBeenCalledWith(id, 'visibility', 'none')
    }
  })

  it('overrides label text/halo styling for stronger contrast when hybrid satellite is toggled on', () => {
    render(<RoutePlannerMap waypoints={[]} onWaypointsChange={() => undefined} isDarkTheme={false} />)

    setPaintPropertyMock.mockClear()
    fireEvent.click(screen.getByRole('button', { name: 'Toggle satellite imagery' }))

    for (const id of HYBRID_LABEL_LAYER_IDS) {
      expect(setPaintPropertyMock).toHaveBeenCalledWith(id, 'text-color', '#ffffff')
      expect(setPaintPropertyMock).toHaveBeenCalledWith(id, 'text-halo-color', '#000000')
      expect(setPaintPropertyMock).toHaveBeenCalledWith(id, 'text-halo-width', 1.5)
    }
  })

  it('restores each label layer\'s own original paint values (not just the generic default) when toggled back off', () => {
    render(<RoutePlannerMap waypoints={[]} onWaypointsChange={() => undefined} isDarkTheme={false} />)

    const toggle = screen.getByRole('button', { name: 'Toggle satellite imagery' })
    fireEvent.click(toggle)
    // getPaintPropertyMock is stubbed to return `original-${prop}` for any id/prop,
    // simulating "whatever the theme's own stylesheet defines" - the fix under test
    // is that these captured originals get reapplied, not the library's generic
    // spec defaults (which setPaintProperty(id, prop, undefined) would produce).
    setPaintPropertyMock.mockClear()

    fireEvent.click(toggle)

    for (const id of HYBRID_LABEL_LAYER_IDS) {
      expect(setPaintPropertyMock).toHaveBeenCalledWith(id, 'text-color', 'original-text-color')
      expect(setPaintPropertyMock).toHaveBeenCalledWith(id, 'text-halo-color', 'original-text-halo-color')
      expect(setPaintPropertyMock).toHaveBeenCalledWith(id, 'text-halo-width', 'original-text-halo-width')
    }
  })

  it('disables the cache-this-area button until satellite imagery is toggled on', () => {
    render(<RoutePlannerMap waypoints={[]} onWaypointsChange={() => undefined} isDarkTheme={false} />)

    const cacheButton = screen.getByRole('button', { name: 'Cache this area' })
    expect(cacheButton).toBeDisabled()

    fireEvent.click(cacheButton)
    expect(startPrefetchMock).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole('button', { name: 'Toggle satellite imagery' }))
    expect(cacheButton).toBeEnabled()
  })

  it('reads the map\'s real bounds and zoom into the prefetch call when the cache-this-area button is clicked', () => {
    mockBounds = { west: 150.1, south: -25.4, east: 150.6, north: -24.9 }
    mockZoom = 13

    render(<RoutePlannerMap waypoints={[]} onWaypointsChange={() => undefined} isDarkTheme={false} />)

    fireEvent.click(screen.getByRole('button', { name: 'Toggle satellite imagery' }))
    act(() => {
      lastZoomHandler?.()
    })

    fireEvent.click(screen.getByRole('button', { name: 'Cache this area' }))

    expect(startPrefetchMock).toHaveBeenCalledWith(
      { west: 150.1, south: -25.4, east: 150.6, north: -24.9 },
      13,
      20,
    )
  })

  it('shows a progress pill while a prefetch is in progress', () => {
    mockPrefetching = true
    mockProgress = { done: 340, total: 1200 }

    render(<RoutePlannerMap waypoints={[]} onWaypointsChange={() => undefined} isDarkTheme={false} />)

    expect(screen.getByText('Caching… 340/1200')).toBeInTheDocument()
  })

  it('shows a brief "Cached" pill on completion, then self-dismisses', async () => {
    vi.useFakeTimers()
    mockPrefetching = true
    mockProgress = { done: 99, total: 100 }

    const { rerender } = render(
      <RoutePlannerMap waypoints={[]} onWaypointsChange={() => undefined} isDarkTheme={false} />,
    )
    expect(screen.getByText('Caching… 99/100')).toBeInTheDocument()

    mockPrefetching = false
    mockProgress = { done: 100, total: 100 }
    rerender(<RoutePlannerMap waypoints={[]} onWaypointsChange={() => undefined} isDarkTheme={false} />)

    expect(screen.getByText('Cached')).toBeInTheDocument()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(2000)
    })

    expect(screen.queryByText('Cached')).not.toBeInTheDocument()
    vi.useRealTimers()
  })

  describe('place-name labels', () => {
    it('mounts all five label layers on the existing carto source, and overImagery tracks the satellite toggle', () => {
      render(<RoutePlannerMap waypoints={[]} onWaypointsChange={() => undefined} isDarkTheme={false} />)

      for (const id of PLACE_LABEL_LAYER_IDS) {
        expect(screen.getByTestId(`layer-${id}`)).toBeInTheDocument()
      }

      const water = recordedLayers.filter((l) => l.id === 'place-names-water').at(-1)
      // Light theme, no imagery: the theme-native water color, not the
      // hybrid white-on-black override.
      expect(water?.source).toBe('carto')
      expect(water?.paint).toMatchObject({ 'text-color': '#7a96a0' })

      recordedLayers = []
      fireEvent.click(screen.getByRole('button', { name: 'Toggle satellite imagery' }))
      mockZoom = 12
      act(() => {
        lastZoomHandler?.()
      })

      const waterOverImagery = recordedLayers.filter((l) => l.id === 'place-names-water').at(-1)
      expect(waterOverImagery?.paint).toMatchObject({
        'text-color': '#ffffff',
        'text-halo-color': '#000000',
        'text-halo-width': 1.5,
      })
    })

    it('gives each label layer its source-layer, filter, and minzoom', () => {
      render(<RoutePlannerMap waypoints={[]} onWaypointsChange={() => undefined} isDarkTheme={false} />)

      const byId = (id: string) => recordedLayers.find((l) => l.id === id)

      expect(byId('place-names-water')).toMatchObject({ 'source-layer': 'water_name', minzoom: 9 })
      expect(byId('place-names-island')).toMatchObject({ 'source-layer': 'place', minzoom: 9 })
      expect(byId('place-names-harbor')).toMatchObject({ 'source-layer': 'poi', minzoom: 14 })
      expect(byId('place-names-peak')).toMatchObject({ 'source-layer': 'mountain_peak', minzoom: 12 })
      expect(byId('place-names-town-topup')).toMatchObject({ 'source-layer': 'place', minzoom: 14 })

      // The water filter admits both bay and strait classes - Cid Harbour is
      // a bay, Hayman Channel resolves to strait in the tiles.
      expect(JSON.stringify(byId('place-names-water')?.filter)).toContain(JSON.stringify(['bay', 'strait']))
      expect(JSON.stringify(byId('place-names-island')?.filter)).toContain(JSON.stringify(['==', ['get', 'class'], 'island']))
    })

    it('does not set beforeId on any label layer - they mount unconditionally ahead of every conditional overlay', () => {
      render(<RoutePlannerMap waypoints={[]} onWaypointsChange={() => undefined} isDarkTheme={false} />)

      for (const id of PLACE_LABEL_LAYER_IDS) {
        expect(screen.getByTestId(`layer-${id}`).dataset.beforeId).toBeUndefined()
      }
    })

    it('logs an explicit error once when the carto vector source is missing from the loaded style', () => {
      const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => undefined)
      mockCartoSourcePresent = false

      render(<RoutePlannerMap waypoints={[]} onWaypointsChange={() => undefined} isDarkTheme={false} />)
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
    })

    it('does not log a missing-source error when the carto source is present', () => {
      const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => undefined)

      render(<RoutePlannerMap waypoints={[]} onWaypointsChange={() => undefined} isDarkTheme={false} />)

      expect(errorSpy).not.toHaveBeenCalled()
    })
  })
})
