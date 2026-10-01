import { act, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { PoiMapTile } from '@/components/poi-map-tile'
import { placeTourTimings, POI_MAP_FIT_PADDING_PX } from '@/components/poi-map-tile-impl'
import type { PoiMapWidgetConfig } from '@/lib/dashboard-widgets'
import { fitCameraAroundPoint, POI_MAP_MAX_ZOOM, zoomForRangeNm, type PoiFeature } from '@/lib/poi'
import type { NearbyVessel } from '@/hooks/use-nearby-vessels'
import type { UsePoiResult } from '@/hooks/use-poi'

vi.mock('maplibre-gl', () => ({ default: {} }))

// True by default so every existing test below keeps mounting the real
// (mocked) <Map> — only the "without WebGL2" tests flip this, resetting it
// in afterEach so no other test in this file is affected.
let mockHasWebGL2 = true
vi.mock('@/lib/webgl', () => ({
  hasWebGL2: () => mockHasWebGL2,
}))

const easeToMock = vi.fn()
const jumpToMock = vi.fn()
const flyToMock = vi.fn()
// Distinct-per-marker projection so two markers can be made to "overlap" on
// screen for the label-declutter test without touching the real projector.
let projectImpl: (lngLat: [number, number]) => { x: number; y: number } = ([lng, lat]) => ({ x: lng, y: lat })
// Captures every prop the last-mounted <Map> received, so the `interactive`
// and attribution tests can assert on exactly what reached the underlying
// maplibre map.
let lastMapProps: { interactive?: boolean; attributionControl?: unknown } | null = null

vi.mock('react-map-gl/maplibre', async () => {
  const React = await import('react')
  return {
    Map: React.forwardRef(
      (
        {
          children,
          onLoad,
          interactive,
          attributionControl,
        }: { children?: React.ReactNode; onLoad?: () => void; interactive?: boolean; attributionControl?: unknown },
        ref: React.Ref<unknown>,
      ) => {
        lastMapProps = { interactive, attributionControl }
        React.useImperativeHandle(ref, () => ({
          getCanvas: () => ({ style: {} }),
          getZoom: () => 12,
          easeTo: easeToMock,
          jumpTo: jumpToMock,
          flyTo: flyToMock,
          project: (lngLat: [number, number]) => projectImpl(lngLat),
        }))
        React.useEffect(() => { onLoad?.() }, [onLoad])
        return <div data-testid="map-root">{children}</div>
      },
    ),
    Marker: ({ children }: { children?: React.ReactNode }) => <div data-testid="marker">{children}</div>,
    Source: ({ children, id }: { children?: React.ReactNode; id: string }) => (
      <div data-testid={`source-${id}`}>{children}</div>
    ),
    Layer: ({ id }: { id: string }) => <div data-testid={`layer-${id}`} />,
  }
})

vi.mock('@/components/map-place-labels', () => ({
  MapPlaceLabels: () => null,
}))

const usePoiMock = vi.fn()
vi.mock('@/hooks/use-poi', () => ({
  usePoi: (...args: unknown[]) => usePoiMock(...args),
}))

// useCyclingIndex has its own dedicated fake-timer tests (advances, wraps,
// resets on resetKey change, cleans up on unmount - use-cycling-index.test.ts)
// entirely outside this lazy-loaded tile, where fake timers can be installed
// from the very first render with no Suspense boundary in the way. Mocked
// here to a plain "always index 0, unless a POI is told otherwise" so these
// tests can check the tile's own wiring (which POIs it hands the hook, what
// interval and resetKey it computes) without needing a live interval to fire
// inside a component that mounts under real timers (renderTile awaits the
// lazy chunk resolving, which findByTestId's own polling needs real timers
// for - see renderTile's comment above).
const useCyclingIndexMock = vi.fn<(count: number, intervalSeconds: number, resetKey: string) => number | null>()
useCyclingIndexMock.mockImplementation((count) => (count > 0 ? 0 : null))
vi.mock('@/hooks/use-cycling-index', () => ({
  useCyclingIndex: (count: number, intervalSeconds: number, resetKey: string) =>
    useCyclingIndexMock(count, intervalSeconds, resetKey),
}))

afterEach(() => {
  vi.clearAllMocks()
  // clearAllMocks only clears recorded calls, not a mockReturnValue/
  // mockImplementation override some test above may have set - reassign the
  // default here so a test that overrides useCyclingIndexMock's return value
  // can't leak that override into the next test.
  useCyclingIndexMock.mockImplementation((count) => (count > 0 ? 0 : null))
  vi.useRealTimers()
  projectImpl = ([lng, lat]) => ({ x: lng, y: lat })
  mockHasWebGL2 = true
  lastMapProps = null
})

function feature(overrides: Partial<PoiFeature>): PoiFeature {
  return {
    id: 'f1', category: 'anchorage', name: 'Cid Harbour', lat: -20.28, lon: 148.95,
    distanceM: 1200, bearingDeg: 45, detail: 'A sheltered bay.', sourceUrl: '',
    ...overrides,
  }
}

function poiResult(overrides: Partial<UsePoiResult>): UsePoiResult {
  return {
    features: [],
    center: { lat: -20.27, lon: 148.94 },
    provider: 'osm-overpass',
    cached: false,
    truncated: [],
    unsupported: [],
    loading: false,
    fetchedAt: '2026-09-11T00:00:00Z',
    error: null,
    retryAt: null,
    ...overrides,
  }
}

function config(overrides: Partial<PoiMapWidgetConfig> = {}): PoiMapWidgetConfig {
  return {
    title: 'Nearby',
    rangeNm: 5,
    categories: ['anchorage', 'marina', 'fuel'],
    layout: 'split',
    showAis: true,
    showTrail: false,
    ...overrides,
  }
}

const aisVessel: NearbyVessel = {
  id: 'urn:mrn:imo:mmsi:100000001', name: 'SPLURGE', lat: -20.281, lon: 148.951, range_m: 100, age_seconds: 2,
}

// PoiMapTile is a thin React.lazy wrapper over poi-map-tile-impl.tsx (kiosk
// bundle-split follow-up: this is the only eager path to map-vendor on
// every route, including /kiosk pages with no map at all). render() alone
// only shows the wrapper's own loading fallback; every test needs to wait
// for the lazy chunk to resolve before asserting on the real content, so
// this helper does that once, here, rather than in every test below.
// 'poi-map-container' is present in both the WebGL2 and no-WebGL2 branches
// of the impl, so it's a stable "the real component is up" signal either way.
// Shared with a couple of tests below that need to build a <PoiMapTile />
// for `rerender` directly, rather than through this helper.
const renderTileDefaultProps: Omit<React.ComponentProps<typeof PoiMapTile>, 'config'> = {
  editing: false,
  latitude: -20.27,
  longitude: 148.94,
  headingTrue: 90,
  gnssCriticalAlert: false,
  positionLastUpdateAgeS: 5,
  nearbyVessels: [],
  getSelfTrail: () => [],
  isDarkTheme: false,
  distanceUnits: 'metric',
}

async function renderTile(props: Partial<React.ComponentProps<typeof PoiMapTile>> = {}) {
  const result = render(
    <PoiMapTile
      {...renderTileDefaultProps}
      config={config()}
      {...props}
    />,
  )
  await screen.findByTestId('poi-map-container')
  return result
}

describe('PoiMapTile', () => {
  // Same guard as the anchor and route maps: the map container owns its
  // own stacking context so nothing drawn over the canvas can escape above
  // a sheet or the mobile sidebar.
  it('isolates its overlays so they cannot draw above a sheet', async () => {
    usePoiMock.mockReturnValue(poiResult({}))

    await renderTile()

    expect(screen.getByTestId('poi-map-container').className.split(/\s+/)).toContain('isolate')
  })

  it('renders one marker per feature and rank badges matching the ranked list rows', async () => {
    const features = [
      feature({ id: 'a', name: 'A', distanceM: 100 }),
      feature({ id: 'b', name: 'B', distanceM: 200 }),
      feature({ id: 'c', name: 'C', distanceM: 300 }),
    ]
    usePoiMock.mockReturnValue(poiResult({ features }))

    await renderTile()

    expect(screen.getAllByLabelText(/^Point of interest:/)).toHaveLength(3)
    expect(screen.getAllByTestId('poi-list-row')).toHaveLength(3)

    // The list's own row order is the rank order, and the matching marker
    // carries the same number.
    expect(screen.getByTestId('poi-marker-rank-a').textContent).toBe('1')
    expect(screen.getByTestId('poi-marker-rank-b').textContent).toBe('2')
    expect(screen.getByTestId('poi-marker-rank-c').textContent).toBe('3')
  })

  it('shows only the map in "map" layout, and adds the ranked list in "split"', async () => {
    usePoiMock.mockReturnValue(poiResult({ features: [feature({})] }))

    const { rerender } = render(
      <PoiMapTile
        config={config({ layout: 'map' })}
        editing={false}
        latitude={-20.27}
        longitude={148.94}
        headingTrue={90}
        gnssCriticalAlert={false}
        positionLastUpdateAgeS={5}
        nearbyVessels={[]}
        getSelfTrail={() => []}
        isDarkTheme={false}
        distanceUnits="metric"
      />,
    )
    await screen.findByTestId('poi-map-container')
    expect(screen.queryAllByTestId('poi-list-row')).toHaveLength(0)

    rerender(
      <PoiMapTile
        config={config({ layout: 'split' })}
        editing={false}
        latitude={-20.27}
        longitude={148.94}
        headingTrue={90}
        gnssCriticalAlert={false}
        positionLastUpdateAgeS={5}
        nearbyVessels={[]}
        getSelfTrail={() => []}
        isDarkTheme={false}
        distanceUnits="metric"
      />,
    )
    expect(screen.getAllByTestId('poi-list-row')).toHaveLength(1)
  })

  it('passes the configured range and categories through to usePoi (category filter)', async () => {
    usePoiMock.mockReturnValue(poiResult({}))
    await renderTile({ config: config({ rangeNm: 8, categories: ['dive', 'trail'] }) })

    expect(usePoiMock).toHaveBeenCalledWith(8, ['dive', 'trail'], expect.any(Number))
  })

  it('renders AIS markers only when showAis is on', async () => {
    usePoiMock.mockReturnValue(poiResult({}))
    const { rerender } = await renderTile({ config: config({ showAis: false }), nearbyVessels: [aisVessel] })
    expect(screen.queryByLabelText(/^AIS vessel:/)).toBeNull()

    rerender(
      <PoiMapTile
        config={config({ showAis: true })}
        editing={false}
        latitude={-20.27}
        longitude={148.94}
        headingTrue={90}
        gnssCriticalAlert={false}
        positionLastUpdateAgeS={5}
        nearbyVessels={[aisVessel]}
        getSelfTrail={() => []}
        isDarkTheme={false}
        distanceUnits="metric"
      />,
    )
    expect(screen.getByLabelText('AIS vessel: SPLURGE')).toBeInTheDocument()
  })

  it('shows the amber GNSS badge and never calls easeTo or jumpTo while gnssCriticalAlert is set', async () => {
    usePoiMock.mockReturnValue(poiResult({}))
    await renderTile({ gnssCriticalAlert: true })

    expect(screen.getByTestId('poi-map-gnss-badge')).toBeInTheDocument()
    expect(easeToMock).not.toHaveBeenCalled()
    expect(jumpToMock).not.toHaveBeenCalled()
  })

  it('jumps to the first fix, then eases at most once per 2-second window', async () => {
    usePoiMock.mockReturnValue(poiResult({}))
    // The lazy chunk resolves under real timers - flip to fake ones only
    // after that await settles, or findByTestId's own internal polling
    // (which relies on a real setTimeout) would never come back.
    const { rerender } = await renderTile({ latitude: -20.27, longitude: 148.94 })
    vi.useFakeTimers()

    expect(jumpToMock).toHaveBeenCalledTimes(1)
    expect(easeToMock).not.toHaveBeenCalled()

    const rerenderAt = (lat: number, lon: number) => rerender(
      <PoiMapTile
        config={config()}
        editing={false}
        latitude={lat}
        longitude={lon}
        headingTrue={90}
        gnssCriticalAlert={false}
        positionLastUpdateAgeS={5}
        nearbyVessels={[]}
        getSelfTrail={() => []}
        isDarkTheme={false}
        distanceUnits="metric"
      />,
    )

    // Real time has to actually pass since the throttle compares Date.now()
    // against the moment of the initial jumpTo.
    act(() => { vi.advanceTimersByTime(2000) })
    act(() => { rerenderAt(-20.271, 148.941) })
    expect(easeToMock).toHaveBeenCalledTimes(1)

    // A second fix inside the same 2s window does not trigger another ease.
    act(() => { rerenderAt(-20.272, 148.942) })
    expect(easeToMock).toHaveBeenCalledTimes(1)

    act(() => { vi.advanceTimersByTime(2000) })
    act(() => { rerenderAt(-20.273, 148.943) })
    expect(easeToMock).toHaveBeenCalledTimes(2)
  })

  it('suppresses a marker label that collides on screen with a higher-ranked one (project mock)', async () => {
    // Both features project to almost the same screen point, so the
    // lower-ranked (farther) one should yield its label. "map" layout keeps
    // the assertion unambiguous — no ranked-list row to also match on name.
    projectImpl = () => ({ x: 100, y: 100 })
    const features = [
      feature({ id: 'near', name: 'Near', distanceM: 100 }),
      feature({ id: 'far', name: 'Far', distanceM: 5000 }),
    ]
    usePoiMock.mockReturnValue(poiResult({ features }))

    await renderTile({ config: config({ layout: 'map' }) })

    expect(screen.getByText('Near')).toBeInTheDocument()
    expect(screen.queryByText('Far')).toBeNull()
  })

  it('keeps showing the last good list and its age/error when the feed is unavailable (no masking fallback)', async () => {
    usePoiMock.mockReturnValue(poiResult({
      features: [feature({ id: 'stale-1', name: 'Stale Anchorage' })],
      error: 'POI provider unavailable (502)',
      fetchedAt: '2026-09-11T00:00:00Z',
    }))

    await renderTile()

    // The name appears twice (the marker label and the list row); the point
    // is that it is never zero, i.e. the feature is never dropped on error.
    expect(screen.getAllByText('Stale Anchorage').length).toBeGreaterThan(0)
    expect(screen.getByText('POI provider unavailable (502)')).toBeInTheDocument()
  })

  it('shows "POI feed unavailable" rather than "No points of interest in range" when the feed has never succeeded', async () => {
    usePoiMock.mockReturnValue(poiResult({
      features: [],
      loading: false,
      error: 'POI provider unavailable (502)',
      fetchedAt: null,
    }))

    await renderTile()

    expect(screen.getByText('POI feed unavailable')).toBeInTheDocument()
    expect(screen.queryByText('No points of interest in range')).toBeNull()
    expect(screen.getByText('POI provider unavailable (502)')).toBeInTheDocument()
  })

  it('shows "No points of interest in range" only once the fetch has actually succeeded with zero features', async () => {
    usePoiMock.mockReturnValue(poiResult({
      features: [],
      loading: false,
      error: null,
      fetchedAt: '2026-09-11T00:00:00Z',
    }))

    await renderTile()

    expect(screen.getByText('No points of interest in range')).toBeInTheDocument()
  })

  // Kiosk maps are display-only (the Nearby moving map is the kiosk's own
  // primary map page): no zoom/pan/rotate/gesture handling, since a wall
  // display has no touchscreen to drive them with. The map still follows
  // the vessel and updates markers/trails regardless (covered above) - see
  // poi-map-tile-impl.tsx's own comment on the prop.
  describe('interactive', () => {
    it('defaults to an interactive map', async () => {
      usePoiMock.mockReturnValue(poiResult({}))
      await renderTile()

      expect(lastMapProps?.interactive).toBe(true)
    })

    it('passes interactive={false} straight through to the underlying map when told to', async () => {
      usePoiMock.mockReturnValue(poiResult({}))
      await renderTile({ interactive: false })

      expect(lastMapProps?.interactive).toBe(false)
    })
  })

  describe('without WebGL2', () => {
    it('does not mount the map, shows the fallback panel, and keeps the ranked list working ("split" layout)', async () => {
      mockHasWebGL2 = false
      const features = [feature({ id: 'a', name: 'A' }), feature({ id: 'b', name: 'B' })]
      usePoiMock.mockReturnValue(poiResult({ features }))

      await renderTile({ config: config({ layout: 'split' }) })

      expect(screen.queryByTestId('map-root')).not.toBeInTheDocument()
      expect(screen.getByTestId('poi-map-webgl2-fallback')).toHaveTextContent(
        'Map needs WebGL2, which this browser does not provide',
      )
      expect(screen.getAllByTestId('poi-list-row')).toHaveLength(2)
    })

    it('fills the tile with the fallback panel in "map" layout', async () => {
      mockHasWebGL2 = false
      usePoiMock.mockReturnValue(poiResult({}))

      await renderTile({ config: config({ layout: 'map' }) })

      expect(screen.queryByTestId('map-root')).not.toBeInTheDocument()
      const fallback = screen.getByTestId('poi-map-webgl2-fallback')
      expect(fallback).toHaveTextContent('Map needs WebGL2, which this browser does not provide')
      expect(fallback.className).toContain('h-full')
    })
  })

  // The compact "i" attribution icon (ADR: same as anchor-watch-map.tsx and
  // route-planner-map.tsx). useCollapsedMapAttribution has its own dedicated
  // unit tests for the collapse behaviour itself; this only checks the tile
  // wires the control on in the first place.
  describe('attribution', () => {
    it('shows the compact attribution control', async () => {
      usePoiMock.mockReturnValue(poiResult({}))
      await renderTile()

      expect(lastMapProps?.attributionControl).toEqual({ compact: true })
    })
  })

  describe('active route layer', () => {
    it('draws the route line, waypoint circles, and a brighter next-leg highlight', async () => {
      usePoiMock.mockReturnValue(poiResult({}))
      await renderTile({
        activeRoute: {
          name: 'Test Route',
          waypoints: [
            { lat: -20.30, lon: 148.90 },
            { lat: -20.28, lon: 148.93 },
            { lat: -20.26, lon: 148.96 },
          ],
          nextIndex: 1,
        },
      })

      expect(screen.getByTestId('layer-poi-map-route-line-layer')).toBeInTheDocument()
      expect(screen.getByTestId('layer-poi-map-route-next-leg-layer')).toBeInTheDocument()
      expect(screen.getByTestId('layer-poi-map-route-waypoints-layer')).toBeInTheDocument()
      expect(screen.getByTestId('layer-poi-map-route-next-waypoint-layer')).toBeInTheDocument()
    })

    it('draws no next-leg highlight when the next waypoint is the first one in the traversal', async () => {
      usePoiMock.mockReturnValue(poiResult({}))
      await renderTile({
        activeRoute: {
          name: 'Test Route',
          waypoints: [
            { lat: -20.30, lon: 148.90 },
            { lat: -20.28, lon: 148.93 },
          ],
          nextIndex: 0,
        },
      })

      expect(screen.getByTestId('layer-poi-map-route-line-layer')).toBeInTheDocument()
      expect(screen.queryByTestId('layer-poi-map-route-next-leg-layer')).toBeNull()
      expect(screen.getByTestId('layer-poi-map-route-next-waypoint-layer')).toBeInTheDocument()
    })

    it('draws nothing when there is no active route', async () => {
      usePoiMock.mockReturnValue(poiResult({}))
      await renderTile({ activeRoute: null })

      expect(screen.queryByTestId('layer-poi-map-route-line-layer')).toBeNull()
      expect(screen.queryByTestId('layer-poi-map-route-next-leg-layer')).toBeNull()
      expect(screen.queryByTestId('layer-poi-map-route-waypoints-layer')).toBeNull()
      expect(screen.queryByTestId('layer-poi-map-route-next-waypoint-layer')).toBeNull()
    })

    it('draws nothing when activeRoute is left unset (the default every existing host gets)', async () => {
      usePoiMock.mockReturnValue(poiResult({}))
      await renderTile()

      expect(screen.queryByTestId('layer-poi-map-route-line-layer')).toBeNull()
    })
  })

  // jsdom's global ResizeObserver stub (src/test/setup.ts) never fires, so
  // the tile's measured box always falls back to its 240x240 constant here -
  // every expectation below computes fitCameraAroundPoint/zoomForRangeNm
  // against that same 240x240, not a guessed number.
  describe('camera fit (vessel + ranked POIs)', () => {
    it('keeps the vessel at the centre and zooms out far enough to show the ranked POIs', async () => {
      const vessel = { lat: -20.27, lon: 148.94 }
      const farPoi = { lat: -20.27, lon: 149.5 } // ~30nm east - well outside a 5nm range
      usePoiMock.mockReturnValue(poiResult({
        features: [feature({ id: 'a', name: 'A', lat: farPoi.lat, lon: farPoi.lon })],
      }))

      await renderTile({ latitude: vessel.lat, longitude: vessel.lon, config: config({ rangeNm: 5 }) })

      const fit = fitCameraAroundPoint(vessel, [farPoi], 240, 240, POI_MAP_FIT_PADDING_PX)
      const rangeFloor = zoomForRangeNm(5, vessel.lat, 240)
      const expectedZoom = Math.min(fit.zoom, rangeFloor)

      // The far-off POI pulls the fit zoom well below the range floor, so
      // this is actually exercising fitCameraAroundPoint, not just re-deriving
      // the old vessel-centred behaviour by coincidence.
      expect(fit.zoom).toBeLessThan(rangeFloor)

      expect(jumpToMock).toHaveBeenCalledTimes(1)
      const call = jumpToMock.mock.calls[0][0] as { center: [number, number]; zoom: number }
      expect(call.center).toEqual([vessel.lon, vessel.lat])
      expect(call.zoom).toBeCloseTo(expectedZoom, 6)
    })

    it('never zooms in tighter than the configured range, even for a single very close POI', async () => {
      const vessel = { lat: -20.27, lon: 148.94 }
      const closePoi = { lat: -20.2701, lon: 148.9401 } // a few metres away
      usePoiMock.mockReturnValue(poiResult({
        features: [feature({ id: 'a', name: 'A', lat: closePoi.lat, lon: closePoi.lon })],
      }))

      await renderTile({ latitude: vessel.lat, longitude: vessel.lon, config: config({ rangeNm: 5 }) })

      const rangeFloor = zoomForRangeNm(5, vessel.lat, 240)
      const call = jumpToMock.mock.calls[0][0] as { zoom: number }
      expect(call.zoom).toBeCloseTo(rangeFloor, 6)
    })

    it('falls back to the vessel-centred range zoom when there are no ranked POIs', async () => {
      usePoiMock.mockReturnValue(poiResult({ features: [] }))

      await renderTile({ latitude: -20.27, longitude: 148.94, config: config({ rangeNm: 8 }) })

      const rangeFloor = zoomForRangeNm(8, -20.27, 240)
      const call = jumpToMock.mock.calls[0][0] as { center: [number, number]; zoom: number }
      expect(call.center).toEqual([148.94, -20.27])
      expect(call.zoom).toBeCloseTo(rangeFloor, 6)
    })
  })

  // Fly in, hold, fly back: each time the highlight moves to a place the
  // camera flies to it, then returns to the boat-centred overview at 60% of
  // the cycle period (default cycle 10 s, so 6 s) before the next place.
  describe('highlight dive and pull-out', () => {
    const vessel = { lat: -20.27, lon: 148.94 }
    const features = [
      feature({ id: 'a', name: 'A', lat: -20.27, lon: 148.96 }),
      feature({ id: 'b', name: 'B', lat: -20.26, lon: 148.97 }),
    ]
    const tile = (props: Partial<React.ComponentProps<typeof PoiMapTile>> = {}) => (
      <PoiMapTile {...renderTileDefaultProps} config={config()} {...props} />
    )
    const overviewZoom = () => (jumpToMock.mock.calls[0][0] as { zoom: number }).zoom
    const moveHighlightToB = (rerender: (ui: React.ReactElement) => void, props: Partial<React.ComponentProps<typeof PoiMapTile>> = {}) => {
      useCyclingIndexMock.mockReturnValue(1)
      act(() => { rerender(tile(props)) })
    }

    it('dives to the highlighted place at the close place zoom, tilted, with an eased long flight', async () => {
      usePoiMock.mockReturnValue(poiResult({ features }))
      const { rerender } = await renderTile()
      const zoom = overviewZoom()

      moveHighlightToB(rerender)

      expect(flyToMock).toHaveBeenCalledTimes(1)
      const call = flyToMock.mock.calls[0][0] as {
        center: [number, number]; zoom: number; duration: number; pitch: number; bearing: number; easing: (t: number) => number
      }
      expect(call.center).toEqual([148.97, -20.26])
      // Street-level close-up: past the overview's own zoom cap, which only
      // limits how tight the boat-centred fit may go.
      expect(call.zoom).toBeCloseTo(Math.max(17, zoom + 2), 6)
      expect(call.zoom).toBeGreaterThan(POI_MAP_MAX_ZOOM)
      expect(call.pitch).toBe(30)
      expect(call.bearing).toBe(0)
      expect(call.duration).toBe(placeTourTimings(10).diveMs)
      expect(call.easing(0)).toBe(0)
      expect(call.easing(1)).toBe(1)
      expect(call.easing(0.25)).toBeLessThan(0.25)
      expect(call.easing(0.75)).toBeGreaterThan(0.75)
    })

    it('pulls out to the boat-centred overview, level, ending 1.5 s before the next highlight', async () => {
      usePoiMock.mockReturnValue(poiResult({ features }))
      const { rerender } = await renderTile()
      const overview = jumpToMock.mock.calls[0][0] as { center: [number, number]; zoom: number }
      vi.useFakeTimers()

      moveHighlightToB(rerender)
      expect(flyToMock).toHaveBeenCalledTimes(1)
      const t = placeTourTimings(10)
      expect(t.pullOutStartMs + t.pullOutMs + t.pauseMs).toBe(10000)

      act(() => { vi.advanceTimersByTime(t.pullOutStartMs - 100) })
      expect(flyToMock).toHaveBeenCalledTimes(1)

      act(() => { vi.advanceTimersByTime(200) })
      expect(flyToMock).toHaveBeenCalledTimes(2)
      const back = flyToMock.mock.calls[1][0] as {
        center: [number, number]; zoom: number; pitch: number; duration: number; easing: (t: number) => number
      }
      expect(back.center).toEqual(overview.center)
      expect(back.center).toEqual([vessel.lon, vessel.lat])
      expect(back.zoom).toBeCloseTo(overview.zoom, 6)
      expect(back.pitch).toBe(0)
      expect(back.duration).toBe(t.pullOutMs)
      expect(back.easing(0.25)).toBeLessThan(0.25)

      // One place only pulls out once: nothing loops.
      act(() => { vi.advanceTimersByTime(60000) })
      expect(flyToMock).toHaveBeenCalledTimes(2)
    })

    it('times the pull-out from the configured cycle', async () => {
      usePoiMock.mockReturnValue(poiResult({ features }))
      const { rerender } = await renderTile({ config: config({ summaryCycleSeconds: 20 }) })
      vi.useFakeTimers()

      moveHighlightToB(rerender, { config: config({ summaryCycleSeconds: 20 }) })
      const start = placeTourTimings(20).pullOutStartMs
      expect(start).toBeGreaterThan(placeTourTimings(10).pullOutStartMs)
      act(() => { vi.advanceTimersByTime(start - 100) })
      expect(flyToMock).toHaveBeenCalledTimes(1)
      act(() => { vi.advanceTimersByTime(200) })
      expect(flyToMock).toHaveBeenCalledTimes(2)
    })

    it('does not ease back to the boat while a place is held or the pull-out flies, and follows again after', async () => {
      usePoiMock.mockReturnValue(poiResult({ features }))
      const { rerender } = await renderTile()
      vi.useFakeTimers()
      const t = placeTourTimings(10)

      moveHighlightToB(rerender)
      act(() => { vi.advanceTimersByTime(3000) })
      moveHighlightToB(rerender, { latitude: -20.271, longitude: 148.941 })
      expect(easeToMock).not.toHaveBeenCalled()

      act(() => { vi.advanceTimersByTime(t.pullOutStartMs - 3000 + 100) })
      expect(flyToMock).toHaveBeenCalledTimes(2)
      const back = flyToMock.mock.calls[1][0] as { center: [number, number] }
      // The pull-out goes to where the boat is now, not where it was.
      expect(back.center[0]).toBeCloseTo(148.941, 6)

      // A fix arriving mid pull-out must not cancel the flight.
      act(() => { rerender(tile({ latitude: -20.2705, longitude: 148.9415 })) })
      expect(easeToMock).not.toHaveBeenCalled()

      act(() => { vi.advanceTimersByTime(t.pullOutMs + 2100) })
      act(() => { rerender(tile({ latitude: -20.272, longitude: 148.942 })) })
      expect(easeToMock).toHaveBeenCalledTimes(1)
      expect(easeToMock.mock.calls[0][0]).toMatchObject({ pitch: 0 })
    })

    it('flies back to the boat-centred overview, level, when the highlight clears while a place is held', async () => {
      usePoiMock.mockReturnValue(poiResult({ features }))
      const { rerender } = await renderTile()
      vi.useFakeTimers()

      moveHighlightToB(rerender)
      act(() => { vi.advanceTimersByTime(3000) })
      expect(flyToMock).toHaveBeenCalledTimes(1)

      // A poll comes back empty mid-hold: no highlight, so no pull-out runs.
      usePoiMock.mockReturnValue(poiResult({ features: [] }))
      useCyclingIndexMock.mockReturnValue(null)
      act(() => { rerender(tile()) })

      expect(flyToMock).toHaveBeenCalledTimes(2)
      const back = flyToMock.mock.calls[1][0] as { center: [number, number]; pitch: number }
      expect(back.center).toEqual([vessel.lon, vessel.lat])
      expect(back.pitch).toBe(0)

      // Following resumes once that flight has ended.
      act(() => { vi.advanceTimersByTime(placeTourTimings(10).pullOutMs + 2100) })
      act(() => { rerender(tile({ latitude: -20.272, longitude: 148.942 })) })
      expect(easeToMock).toHaveBeenCalled()
      for (const [view] of easeToMock.mock.calls) expect(view).toMatchObject({ pitch: 0 })
    })

    it('releases the hold after a pull-out even if the highlighted place stops being the last mid-flight', async () => {
      usePoiMock.mockReturnValue(poiResult({ features }))
      const { rerender } = await renderTile()
      vi.useFakeTimers()
      const t = placeTourTimings(10)

      moveHighlightToB(rerender)
      act(() => { vi.advanceTimersByTime(t.pullOutStartMs + 100) })
      expect(flyToMock).toHaveBeenCalledTimes(2)

      // A place ranked after B appears mid pull-out: B is no longer last,
      // but the highlight (B) is unchanged.
      const c = feature({ id: 'c', name: 'C', lat: -20.25, lon: 148.98 })
      usePoiMock.mockReturnValue(poiResult({ features: [...features, c] }))
      act(() => { rerender(tile()) })

      act(() => { vi.advanceTimersByTime(t.pullOutMs + 2100) })
      act(() => { rerender(tile({ latitude: -20.272, longitude: 148.942 })) })
      expect(easeToMock).toHaveBeenCalledTimes(1)
    })

    it('does not fly in for the highlight already showing at mount', async () => {
      usePoiMock.mockReturnValue(poiResult({ features }))
      await renderTile()

      expect(jumpToMock).toHaveBeenCalledTimes(1)
      expect(flyToMock).not.toHaveBeenCalled()
    })

    it('flies in once and back once for a single place that appears after mount', async () => {
      usePoiMock.mockReturnValue(poiResult({ features: [] }))
      const { rerender } = await renderTile()
      vi.useFakeTimers()

      usePoiMock.mockReturnValue(poiResult({ features: [features[0]] }))
      act(() => { rerender(tile()) })
      expect(flyToMock).toHaveBeenCalledTimes(1)

      act(() => { vi.advanceTimersByTime(60000) })
      expect(flyToMock).toHaveBeenCalledTimes(2)
    })

    it('does nothing while gnssCriticalAlert is set', async () => {
      usePoiMock.mockReturnValue(poiResult({ features }))
      const { rerender } = await renderTile({ gnssCriticalAlert: true })
      vi.useFakeTimers()

      moveHighlightToB(rerender, { gnssCriticalAlert: true })
      act(() => { vi.advanceTimersByTime(60000) })

      expect(flyToMock).not.toHaveBeenCalled()
      expect(jumpToMock).not.toHaveBeenCalled()
      expect(easeToMock).not.toHaveBeenCalled()
    })

    it('levels the camera instead of pulling out when the position goes bad while a place is held', async () => {
      usePoiMock.mockReturnValue(poiResult({ features }))
      const { rerender } = await renderTile()
      vi.useFakeTimers()

      moveHighlightToB(rerender)
      act(() => { rerender(tile({ gnssCriticalAlert: true })) })
      act(() => { vi.advanceTimersByTime(60000) })

      expect(flyToMock).toHaveBeenCalledTimes(1)
      const last = jumpToMock.mock.calls[jumpToMock.mock.calls.length - 1][0] as { pitch: number; center?: unknown }
      expect(last.pitch).toBe(0)
      expect(last.center).toBeUndefined()
    })

    it('jumps instead of flying under prefers-reduced-motion', async () => {
      const original = window.matchMedia
      window.matchMedia = ((query: string) => ({
        matches: query === '(prefers-reduced-motion: reduce)',
        media: query,
        addEventListener: () => {},
        removeEventListener: () => {},
      })) as unknown as typeof window.matchMedia
      try {
        usePoiMock.mockReturnValue(poiResult({ features }))
        const { rerender } = await renderTile()
        vi.useFakeTimers()

        moveHighlightToB(rerender)
        expect(flyToMock).not.toHaveBeenCalled()
        expect(jumpToMock).toHaveBeenCalledTimes(2)
        expect((jumpToMock.mock.calls[1][0] as { center: [number, number] }).center).toEqual([148.97, -20.26])
        expect((jumpToMock.mock.calls[1][0] as { pitch: number }).pitch).toBe(0)

        act(() => { vi.advanceTimersByTime(placeTourTimings(10).pullOutStartMs + 100) })
        expect(flyToMock).not.toHaveBeenCalled()
        expect(jumpToMock).toHaveBeenCalledTimes(3)
        expect((jumpToMock.mock.calls[2][0] as { pitch: number }).pitch).toBe(0)
        expect((jumpToMock.mock.calls[2][0] as { center: [number, number] }).center).toEqual([vessel.lon, vessel.lat])
      } finally {
        window.matchMedia = original
      }
    })

    it('does not fly in the Map only layout, where no list shows which place is highlighted', async () => {
      usePoiMock.mockReturnValue(poiResult({ features }))
      const mapOnly = { config: config({ layout: 'map' }) }
      const { rerender } = await renderTile(mapOnly)
      moveHighlightToB(rerender, mapOnly)
      expect(flyToMock).not.toHaveBeenCalled()
    })

    it('flies for a non-interactive (kiosk) map too', async () => {
      usePoiMock.mockReturnValue(poiResult({ features }))
      const { rerender } = await renderTile({ interactive: false })
      moveHighlightToB(rerender, { interactive: false })
      expect(flyToMock).toHaveBeenCalledTimes(1)
    })
  })

  // Fly between places: the first place is a dive from the overview, each
  // later one a single high arc from the previous place whose peak is the
  // overview altitude, and only the last place pulls out to the overview.
  describe('hops between places', () => {
    const vessel = { lat: -20.27, lon: 148.94 }
    const four = [
      feature({ id: 'a', name: 'A', lat: -20.27, lon: 148.96 }),
      feature({ id: 'b', name: 'B', lat: -20.26, lon: 148.97 }),
      feature({ id: 'c', name: 'C', lat: -20.25, lon: 148.98 }),
      feature({ id: 'd', name: 'D', lat: -20.24, lon: 148.99 }),
    ]
    const tile = (props: Partial<React.ComponentProps<typeof PoiMapTile>> = {}) => (
      <PoiMapTile {...renderTileDefaultProps} config={config()} {...props} />
    )
    const goto = (rerender: (ui: React.ReactElement) => void, index: number, props: Partial<React.ComponentProps<typeof PoiMapTile>> = {}) => {
      useCyclingIndexMock.mockReturnValue(index)
      act(() => { rerender(tile(props)) })
    }
    type Fly = { center: [number, number]; zoom: number; pitch: number; duration: number; minZoom?: number }
    const fly = (n: number) => flyToMock.mock.calls[n][0] as Fly

    it('dives from the overview with no minZoom, then hops place to place through the overview altitude', async () => {
      usePoiMock.mockReturnValue(poiResult({ features: four }))
      const { rerender } = await renderTile()
      const overview = jumpToMock.mock.calls[0][0] as { center: [number, number]; zoom: number }
      vi.useFakeTimers()
      const t = placeTourTimings(10)

      goto(rerender, 1)
      expect(flyToMock).toHaveBeenCalledTimes(1)
      expect(fly(0).minZoom).toBeUndefined()
      expect(fly(0).duration).toBe(t.diveMs)

      // Held on a non-last place: nothing pulls out however long it takes.
      act(() => { vi.advanceTimersByTime(10000) })
      expect(flyToMock).toHaveBeenCalledTimes(1)

      goto(rerender, 2)
      expect(flyToMock).toHaveBeenCalledTimes(2)
      expect(fly(1).center).toEqual([148.98, -20.25])
      expect(fly(1).minZoom).toBeCloseTo(overview.zoom, 6)
      expect(fly(1).zoom).toBeCloseTo(Math.max(17, overview.zoom + 2), 6)
      expect(fly(1).pitch).toBe(30)
      expect(fly(1).duration).toBe(t.hopMs)
      expect(t.hopMs).toBeGreaterThan(t.diveMs)

      // No pull-out before the next highlight.
      act(() => { vi.advanceTimersByTime(60000) })
      expect(flyToMock).toHaveBeenCalledTimes(2)
    })

    it('pulls out to the overview only after the last place, finishing 1.5 s before the cycle wraps', async () => {
      usePoiMock.mockReturnValue(poiResult({ features: four }))
      const { rerender } = await renderTile()
      const overview = jumpToMock.mock.calls[0][0] as { center: [number, number]; zoom: number }
      vi.useFakeTimers()
      const t = placeTourTimings(10)

      goto(rerender, 2)
      goto(rerender, 3)
      expect(flyToMock).toHaveBeenCalledTimes(2)
      expect(fly(1).minZoom).toBeCloseTo(overview.zoom, 6)
      expect(t.pullOutStartMs + t.pullOutMs + t.pauseMs).toBe(10000)

      act(() => { vi.advanceTimersByTime(t.pullOutStartMs - 100) })
      expect(flyToMock).toHaveBeenCalledTimes(2)
      act(() => { vi.advanceTimersByTime(200) })
      expect(flyToMock).toHaveBeenCalledTimes(3)
      expect(fly(2).center).toEqual([vessel.lon, vessel.lat])
      expect(fly(2).zoom).toBeCloseTo(overview.zoom, 6)
      expect(fly(2).pitch).toBe(0)
      expect(fly(2).duration).toBe(t.pullOutMs)
      expect(fly(2).minZoom).toBeUndefined()

      // Cycle wraps to the first place: a dive from the overview, no arc.
      act(() => { vi.advanceTimersByTime(t.pullOutMs + t.pauseMs) })
      goto(rerender, 0)
      expect(flyToMock).toHaveBeenCalledTimes(4)
      expect(fly(3).minZoom).toBeUndefined()
      expect(fly(3).duration).toBe(t.diveMs)
    })

    it('holds off the follow ease across a whole tour and follows again after the final pull-out', async () => {
      usePoiMock.mockReturnValue(poiResult({ features: four }))
      const { rerender } = await renderTile()
      vi.useFakeTimers()
      const t = placeTourTimings(10)

      goto(rerender, 1)
      act(() => { vi.advanceTimersByTime(10000) })
      // Well past any single place's own pull-out time, still on a place.
      act(() => { rerender(tile({ latitude: -20.2705, longitude: 148.9405 })) })
      expect(easeToMock).not.toHaveBeenCalled()
      goto(rerender, 2, { latitude: -20.271, longitude: 148.941 })
      act(() => { vi.advanceTimersByTime(10000) })
      act(() => { rerender(tile({ latitude: -20.2715, longitude: 148.9415 })) })
      expect(easeToMock).not.toHaveBeenCalled()
      goto(rerender, 3, { latitude: -20.272, longitude: 148.942 })
      act(() => { vi.advanceTimersByTime(t.pullOutStartMs + 100) })
      // Mid pull-out: a fix must not cancel the flight.
      act(() => { rerender(tile({ latitude: -20.2725, longitude: 148.9425 })) })
      expect(easeToMock).not.toHaveBeenCalled()

      act(() => { vi.advanceTimersByTime(t.pullOutMs + 2100) })
      act(() => { rerender(tile({ latitude: -20.273, longitude: 148.943 })) })
      expect(easeToMock).toHaveBeenCalledTimes(1)
      expect(easeToMock.mock.calls[0][0]).toMatchObject({ pitch: 0 })
    })

    it('hops without a stale pull-out from an interrupted tour, and releases the hold when the highlight clears', async () => {
      usePoiMock.mockReturnValue(poiResult({ features: four }))
      const { rerender } = await renderTile()
      vi.useFakeTimers()

      goto(rerender, 3)
      act(() => { vi.advanceTimersByTime(100) })
      goto(rerender, 0)
      expect(flyToMock).toHaveBeenCalledTimes(2)
      expect(fly(1).minZoom).toBeDefined()
      act(() => { vi.advanceTimersByTime(60000) })
      expect(flyToMock).toHaveBeenCalledTimes(2)

      usePoiMock.mockReturnValue(poiResult({ features: [] }))
      useCyclingIndexMock.mockReturnValue(null)
      act(() => { rerender(tile()) })
      // Clearing flies back to the level overview, then following resumes.
      expect(flyToMock).toHaveBeenCalledTimes(3)
      expect(fly(2)).toMatchObject({ pitch: 0 })
      act(() => { vi.advanceTimersByTime(10000) })
      act(() => { rerender(tile({ latitude: -20.272, longitude: 148.942 })) })
      expect(easeToMock).toHaveBeenCalledTimes(1)
      expect(easeToMock.mock.calls[0][0]).toMatchObject({ pitch: 0 })
    })

    it('pulls out once the held place becomes the last because later places dropped out of range', async () => {
      usePoiMock.mockReturnValue(poiResult({ features: four }))
      const { rerender } = await renderTile()
      vi.useFakeTimers()
      const t = placeTourTimings(10)

      goto(rerender, 1)
      act(() => { vi.advanceTimersByTime(4000) })
      expect(flyToMock).toHaveBeenCalledTimes(1)

      // c and d drop out; the highlight index is still 1, the same place.
      usePoiMock.mockReturnValue(poiResult({ features: four.slice(0, 2) }))
      act(() => { rerender(tile()) })
      // No second flight to the place it is already on.
      expect(flyToMock).toHaveBeenCalledTimes(1)

      act(() => { vi.advanceTimersByTime(t.holdMs - 100) })
      expect(flyToMock).toHaveBeenCalledTimes(1)
      act(() => { vi.advanceTimersByTime(200) })
      expect(flyToMock).toHaveBeenCalledTimes(2)
      expect(fly(1).pitch).toBe(0)
      expect(fly(1).center).toEqual([vessel.lon, vessel.lat])

      act(() => { vi.advanceTimersByTime(t.pullOutMs + 2100) })
      act(() => { rerender(tile({ latitude: -20.272, longitude: 148.942 })) })
      expect(easeToMock).toHaveBeenCalledTimes(1)
    })

    it('drops the pull-out when a new place appears after the held last one, and hops onward', async () => {
      usePoiMock.mockReturnValue(poiResult({ features: four }))
      const { rerender } = await renderTile()
      vi.useFakeTimers()
      const t = placeTourTimings(10)

      goto(rerender, 3)
      act(() => { vi.advanceTimersByTime(1000) })
      const five = [...four, feature({ id: 'e', name: 'E', lat: -20.23, lon: 149.0 })]
      usePoiMock.mockReturnValue(poiResult({ features: five }))
      act(() => { rerender(tile()) })
      act(() => { vi.advanceTimersByTime(60000) })
      expect(flyToMock).toHaveBeenCalledTimes(1)

      goto(rerender, 4)
      expect(flyToMock).toHaveBeenCalledTimes(2)
      expect(fly(1).center).toEqual([149.0, -20.23])
      // The overview now fits five places, so its zoom is the current one.
      expect(fly(1).minZoom).toBeLessThan(fly(1).zoom)
      expect(fly(1).duration).toBe(t.hopMs)
    })

    it('jumps between places under reduced motion and jumps to the overview only after the last', async () => {
      const original = window.matchMedia
      window.matchMedia = ((query: string) => ({
        matches: query === '(prefers-reduced-motion: reduce)',
        media: query,
        addEventListener: () => {},
        removeEventListener: () => {},
      })) as unknown as typeof window.matchMedia
      try {
        usePoiMock.mockReturnValue(poiResult({ features: four }))
        const { rerender } = await renderTile()
        vi.useFakeTimers()
        const t = placeTourTimings(10)

        goto(rerender, 1)
        goto(rerender, 2)
        expect(flyToMock).not.toHaveBeenCalled()
        expect(jumpToMock).toHaveBeenCalledTimes(3)
        expect((jumpToMock.mock.calls[2][0] as { center: [number, number]; pitch: number }).center).toEqual([148.98, -20.25])
        expect((jumpToMock.mock.calls[2][0] as { pitch: number }).pitch).toBe(0)
        act(() => { vi.advanceTimersByTime(60000) })
        expect(jumpToMock).toHaveBeenCalledTimes(3)

        goto(rerender, 3)
        expect(jumpToMock).toHaveBeenCalledTimes(4)
        act(() => { vi.advanceTimersByTime(t.pullOutStartMs + 100) })
        expect(jumpToMock).toHaveBeenCalledTimes(5)
        expect((jumpToMock.mock.calls[4][0] as { center: [number, number] }).center).toEqual([vessel.lon, vessel.lat])
        expect((jumpToMock.mock.calls[4][0] as { pitch: number }).pitch).toBe(0)
      } finally {
        window.matchMedia = original
      }
    })
  })

  describe('cycling POI summary (split layout)', () => {
    // The row itself must show it is the cycled-to one independently of the
    // summary text - a POI with no detail renders no summary block at all,
    // so without this the map marker's ring would be the only visible sign
    // of the cycle and the matching list row would look identical to every
    // other row.
    it('highlights the cycled-to row itself, not only the map marker ring, even with no detail to show', async () => {
      const features = [
        feature({ id: 'a', name: 'A', distanceM: 100, detail: '' }),
        feature({ id: 'b', name: 'B', distanceM: 200, detail: '' }),
      ]
      usePoiMock.mockReturnValue(poiResult({ features }))

      await renderTile({ config: config({ layout: 'split' }) })

      const rows = screen.getAllByTestId('poi-list-row')
      expect(rows[0]).toHaveAttribute('data-expanded', 'true')
      expect(rows[1]).toHaveAttribute('data-expanded', 'false')
    })

    it('highlights the ranked list row useCyclingIndex points at, and shows its summary when it has one', async () => {
      const features = [
        feature({ id: 'a', name: 'A', distanceM: 100, detail: 'About A.' }),
        feature({ id: 'b', name: 'B', distanceM: 200, detail: '' }),
        feature({ id: 'c', name: 'C', distanceM: 300, detail: 'About C.' }),
      ]
      usePoiMock.mockReturnValue(poiResult({ features }))

      await renderTile({ config: config({ layout: 'split' }) })

      const summaries = screen.getAllByTestId('poi-list-row-summary')
      expect(summaries).toHaveLength(1)
      expect(summaries[0]).toHaveTextContent('About A.')
      expect(screen.getByTestId('poi-marker-expanded')).toBeInTheDocument()
    })

    // The bug this covers: cycling used to skip any ranked POI with no
    // detail entirely, so on a live feed where only one of several POIs has
    // one, the cycle got a set of size 1 and never advanced at all. It must
    // now cycle through every ranked POI - the highlight/ring lands on 'b'
    // here even though 'b' has no detail to show.
    it('highlights whatever POI useCyclingIndex points at in the full ranked list, even with no detail', async () => {
      const features = [
        feature({ id: 'a', name: 'A', distanceM: 100, detail: 'About A.' }),
        feature({ id: 'b', name: 'B', distanceM: 200, detail: '' }),
        feature({ id: 'c', name: 'C', distanceM: 300, detail: 'About C.' }),
      ]
      usePoiMock.mockReturnValue(poiResult({ features }))
      // Index 1 of the full ranked list [a, b, c] is 'b', which has no
      // detail - the cycle must still land on it and show no summary text.
      useCyclingIndexMock.mockReturnValue(1)

      await renderTile({ config: config({ layout: 'split', summaryCycleSeconds: 5 }) })

      expect(screen.getByTestId('poi-marker-rank-b').closest('[data-testid="poi-marker-expanded"]')).toBeTruthy()
      expect(screen.queryByTestId('poi-list-row-summary')).toBeNull()
      expect(useCyclingIndexMock).toHaveBeenCalledWith(3, 5, 'a|b|c')
    })

    it('passes a configured summaryCycleSeconds through to useCyclingIndex', async () => {
      const features = [feature({ id: 'a', name: 'A', detail: 'About A.' })]
      usePoiMock.mockReturnValue(poiResult({ features }))

      await renderTile({ config: config({ layout: 'split', summaryCycleSeconds: 42 }) })
      expect(useCyclingIndexMock).toHaveBeenLastCalledWith(1, 42, 'a')
    })

    it('defaults to a 10 second interval when summaryCycleSeconds is unset', async () => {
      const features = [feature({ id: 'a', name: 'A', detail: 'About A.' })]
      usePoiMock.mockReturnValue(poiResult({ features }))

      await renderTile({ config: config({ layout: 'split', summaryCycleSeconds: undefined }) })
      expect(useCyclingIndexMock).toHaveBeenLastCalledWith(1, 10, 'a')
    })

    // The row/marker still gets highlighted and cycled even when nothing in
    // range has a detail at all - only the summary text itself is absent.
    // Cycling is not conditional on any POI having a detail to show.
    it('still highlights the cycled-to row when no ranked POI has a detail, showing no summary text', async () => {
      const features = [
        feature({ id: 'a', name: 'A', distanceM: 100, detail: '' }),
        feature({ id: 'b', name: 'B', distanceM: 200, detail: '' }),
      ]
      usePoiMock.mockReturnValue(poiResult({ features }))

      await renderTile({ config: config({ layout: 'split' }) })

      expect(screen.queryByTestId('poi-list-row-summary')).toBeNull()
      expect(screen.getByTestId('poi-marker-expanded')).toBeInTheDocument()
      expect(useCyclingIndexMock).toHaveBeenCalledWith(2, 10, 'a|b')
    })

    // useCyclingIndex's own tests cover actually resetting the index when its
    // resetKey argument changes (use-cycling-index.test.ts); this is the half
    // of that contract that's this tile's job - computing a resetKey that
    // changes exactly when the ranked list's shape genuinely does, and not
    // on every poll's fresh-but-identical array.
    it('recomputes the resetKey only when the ranked list actually changes shape', async () => {
      const initial = [
        feature({ id: 'a', name: 'A', distanceM: 100, detail: 'About A.' }),
        feature({ id: 'b', name: 'B', distanceM: 200, detail: 'About B.' }),
      ]
      usePoiMock.mockReturnValue(poiResult({ features: initial }))
      const { rerender } = await renderTile({ config: config({ layout: 'split' }) })

      const firstResetKey = useCyclingIndexMock.mock.calls.at(-1)![2]
      expect(firstResetKey).toBe('a|b')

      // A fresh array with the same ids, exactly what a real poll produces -
      // must not look like a change.
      usePoiMock.mockReturnValue(poiResult({ features: [{ ...initial[0] }, { ...initial[1] }] }))
      act(() => { rerender(<PoiMapTile {...renderTileDefaultProps} config={config({ layout: 'split' })} />) })
      expect(useCyclingIndexMock.mock.calls.at(-1)![2]).toBe(firstResetKey)

      // "b" drops out of range entirely - a genuine change of shape.
      usePoiMock.mockReturnValue(poiResult({ features: [initial[0]] }))
      act(() => { rerender(<PoiMapTile {...renderTileDefaultProps} config={config({ layout: 'split' })} />) })
      expect(useCyclingIndexMock.mock.calls.at(-1)![2]).not.toBe(firstResetKey)
    })
  })
})

describe('placeTourTimings', () => {
  it('gives a 10 second cycle the full dive, pull-out and overview pause', () => {
    expect(placeTourTimings(10)).toEqual({
      diveMs: 2800, holdMs: 3200, pullOutStartMs: 6000, pullOutMs: 2500, pauseMs: 1500,
      hopMs: 3500, hopHoldMs: 6500,
    })
  })

  it('scales the three moves down together on a short cycle and keeps the hold positive', () => {
    const t = placeTourTimings(3)
    expect(t.diveMs + t.pullOutMs + t.pauseMs).toBeLessThanOrEqual(3000 * 0.85 + 1e-6)
    expect(t.holdMs).toBeGreaterThan(0)
    expect(t.diveMs / t.pullOutMs).toBeCloseTo(2800 / 2500, 6)
    expect(t.hopMs / t.pullOutMs).toBeCloseTo(3500 / 2500, 6)
    // The longest cycle is a hop that is also the last place: hop, pull-out, pause.
    expect(t.hopMs + t.pullOutMs + t.pauseMs).toBeLessThanOrEqual(3000 * 0.85 + 1e-6)
    expect(t.hopHoldMs).toBeGreaterThan(0)
    expect(t.hopMs + t.hopHoldMs).toBeCloseTo(3000, 6)
    expect(t.diveMs + t.holdMs + t.pullOutMs + t.pauseMs).toBeCloseTo(3000, 6)
    for (const v of Object.values(t)) expect(v).toBeGreaterThanOrEqual(0)
  })

  it('never stretches the moves on a longer cycle, only the hold', () => {
    const t = placeTourTimings(20)
    expect(t.diveMs).toBe(2800)
    expect(t.pullOutMs).toBe(2500)
    expect(t.pauseMs).toBe(1500)
    expect(t.holdMs).toBe(13200)
    expect(t.hopMs).toBe(3500)
    expect(t.hopHoldMs).toBe(16500)
  })
})
