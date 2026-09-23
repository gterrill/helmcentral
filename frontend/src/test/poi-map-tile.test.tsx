import { act, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { PoiMapTile } from '@/components/poi-map-tile'
import { POI_MAP_FIT_PADDING_PX } from '@/components/poi-map-tile-impl'
import type { PoiMapWidgetConfig } from '@/lib/dashboard-widgets'
import { fitCameraToPoints, zoomForRangeNm, type PoiFeature } from '@/lib/poi'
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
  // every expectation below computes fitCameraToPoints/zoomForRangeNm
  // against that same 240x240, not a guessed number.
  describe('camera fit (vessel + ranked POIs)', () => {
    it('fits the camera to the vessel plus the ranked POIs, not just the vessel', async () => {
      const vessel = { lat: -20.27, lon: 148.94 }
      const farPoi = { lat: -20.27, lon: 149.5 } // ~30nm east - well outside a 5nm range
      usePoiMock.mockReturnValue(poiResult({
        features: [feature({ id: 'a', name: 'A', lat: farPoi.lat, lon: farPoi.lon })],
      }))

      await renderTile({ latitude: vessel.lat, longitude: vessel.lon, config: config({ rangeNm: 5 }) })

      const fit = fitCameraToPoints([vessel, farPoi], 240, 240, POI_MAP_FIT_PADDING_PX)
      const rangeFloor = zoomForRangeNm(5, vessel.lat, 240)
      const expectedZoom = Math.min(fit.zoom, rangeFloor)

      // The far-off POI pulls the fit zoom well below the range floor, so
      // this is actually exercising fitCameraToPoints, not just re-deriving
      // the old vessel-centred behaviour by coincidence.
      expect(fit.zoom).toBeLessThan(rangeFloor)

      expect(jumpToMock).toHaveBeenCalledTimes(1)
      const call = jumpToMock.mock.calls[0][0] as { center: [number, number]; zoom: number }
      expect(call.center[0]).toBeCloseTo(fit.center.lon, 6)
      expect(call.center[1]).toBeCloseTo(fit.center.lat, 6)
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

    it('does not move the camera when only the cycling index changes', async () => {
      const features = [
        feature({ id: 'a', name: 'A', detail: 'About A.' }),
        feature({ id: 'b', name: 'B', detail: 'About B.' }),
      ]
      usePoiMock.mockReturnValue(poiResult({ features }))
      const { rerender } = await renderTile({ config: config({ layout: 'split' }) })

      expect(jumpToMock).toHaveBeenCalledTimes(1)
      expect(easeToMock).not.toHaveBeenCalled()

      useCyclingIndexMock.mockReturnValue(1)
      act(() => {
        rerender(<PoiMapTile {...renderTileDefaultProps} config={config({ layout: 'split' })} />)
      })

      expect(jumpToMock).toHaveBeenCalledTimes(1)
      expect(easeToMock).not.toHaveBeenCalled()
    })
  })

  describe('cycling POI summary (split layout)', () => {
    it('shows only one summary at a time, for the highest-ranked POI that has a detail', async () => {
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

    it('shows whatever POI useCyclingIndex points at, skipping ranked POIs without a detail', async () => {
      const features = [
        feature({ id: 'a', name: 'A', distanceM: 100, detail: 'About A.' }),
        feature({ id: 'b', name: 'B', distanceM: 200, detail: '' }),
        feature({ id: 'c', name: 'C', distanceM: 300, detail: 'About C.' }),
      ]
      usePoiMock.mockReturnValue(poiResult({ features }))
      // 'b' has no detail, so the cyclable set is [a, c] - index 1 is 'c',
      // not the ranked list's own third row.
      useCyclingIndexMock.mockReturnValue(1)

      await renderTile({ config: config({ layout: 'split', summaryCycleSeconds: 5 }) })

      expect(screen.getByTestId('poi-list-row-summary')).toHaveTextContent('About C.')
      expect(useCyclingIndexMock).toHaveBeenCalledWith(2, 5, 'a|c')
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

    it('shows no summary at all when no ranked POI has a detail', async () => {
      const features = [
        feature({ id: 'a', name: 'A', distanceM: 100, detail: '' }),
        feature({ id: 'b', name: 'B', distanceM: 200, detail: '' }),
      ]
      usePoiMock.mockReturnValue(poiResult({ features }))

      await renderTile({ config: config({ layout: 'split' }) })

      expect(screen.queryByTestId('poi-list-row-summary')).toBeNull()
      expect(screen.queryByTestId('poi-marker-expanded')).toBeNull()
    })

    // useCyclingIndex's own tests cover actually resetting the index when its
    // resetKey argument changes (use-cycling-index.test.ts); this is the half
    // of that contract that's this tile's job - computing a resetKey that
    // changes exactly when the cyclable set's shape genuinely does, and not
    // on every poll's fresh-but-identical array.
    it('recomputes the resetKey only when the cyclable set actually changes shape', async () => {
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
