import { act, render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { PoiMapTile } from '@/components/poi-map-tile'
import { POI_MAP_FIT_PADDING_PX } from '@/components/poi-map-tile-impl'
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
const sourceData: Record<string, unknown> = {}
let lastMapProps: { interactive?: boolean; attributionControl?: unknown } | null = null
// The map events the tile subscribes to through useMap().getMap().on(...), so a
// test can fire a 'move' the way a pan does.
const moveHandlers = new Set<() => void>()
const fireMapMove = () => { for (const h of [...moveHandlers]) h() }
const fakeMap = {
  on: (event: string, handler: () => void) => { if (event === 'move') moveHandlers.add(handler) },
  off: (event: string, handler: () => void) => { if (event === 'move') moveHandlers.delete(handler) },
  project: (lngLat: [number, number]) => projectImpl(lngLat),
}
// react-maplibre creates the map in a Promise, so the Map's ref is empty until
// the map has loaded. True leaves the handle null until onLoad fires.
let deferMapHandle = false
// How many times each marker has rendered, keyed "lat,lon".
const markerRenders = new globalThis.Map<string, number>()

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
        const [loaded, setLoaded] = React.useState(!deferMapHandle)
        React.useImperativeHandle(ref, () => (loaded ? {
          getCanvas: () => ({ style: {} }),
          getZoom: () => 12,
          easeTo: easeToMock,
          jumpTo: jumpToMock,
          flyTo: flyToMock,
          project: (lngLat: [number, number]) => projectImpl(lngLat),
        } : null), [loaded])
        React.useEffect(() => {
          setLoaded(true)
          onLoad?.()
        }, [onLoad])
        return <div data-testid="map-root">{children}</div>
      },
    ),
    Marker: ({ children, latitude, longitude, style }: { children?: React.ReactNode; latitude?: number; longitude?: number; style?: React.CSSProperties }) => {
      markerRenders.set(`${latitude},${longitude}`, (markerRenders.get(`${latitude},${longitude}`) ?? 0) + 1)
      // react-map-gl applies `style` to the maplibre marker element itself.
      return <div data-testid="marker" style={style}>{children}</div>
    },
    useMap: () => ({ current: { getMap: () => fakeMap } }),
    Source: ({ children, id, data }: { children?: React.ReactNode; id: string; data?: unknown }) => {
      sourceData[id] = data
      return <div data-testid={`source-${id}`}>{children}</div>
    },
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
  projectImpl = ([lng, lat]) => ({ x: lng, y: lat })
  mockHasWebGL2 = true
  deferMapHandle = false
  moveHandlers.clear()
  markerRenders.clear()
  lastMapProps = null
  for (const k of Object.keys(sourceData)) delete sourceData[k]
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
  selfTrail: [],
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

const vessel = { lat: -20.27, lon: 148.94 }
const nearbyFeatures = [
  feature({ id: 'a', name: 'A', lat: -20.27, lon: 148.96 }),
  feature({ id: 'b', name: 'B', lat: -20.26, lon: 148.97 }),
]
const tile = (props: Partial<React.ComponentProps<typeof PoiMapTile>> = {}) => (
  <PoiMapTile {...renderTileDefaultProps} config={config()} {...props} />
)

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
        selfTrail={[]}
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
        selfTrail={[]}
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
        selfTrail={[]}
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

  // Kiosk maps are display-only (the Nearby map is the kiosk's own primary
  // map page): no zoom/pan/rotate/gesture handling, since a wall display has
  // no touchscreen to drive them with. The camera still re-frames and the
  // markers, trail and arrow still update regardless (covered below) - see
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

  // The compact "i" attribution icon (ADR: same as anchor-watch-map.tsx).
  // useCollapsedMapAttribution has its own dedicated
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

    it('zooms to fit the ranked POIs even when the configured range is much wider', async () => {
      const vessel = { lat: -20.27, lon: 148.94 }
      const nearPoi = { lat: -20.27, lon: 148.96 } // about 2 km east, in a 25 nm range
      usePoiMock.mockReturnValue(poiResult({
        features: [feature({ id: 'a', name: 'A', lat: nearPoi.lat, lon: nearPoi.lon })],
      }))

      await renderTile({ latitude: vessel.lat, longitude: vessel.lon, config: config({ rangeNm: 25 }) })

      const fit = fitCameraAroundPoint(vessel, [nearPoi], 240, 240, POI_MAP_FIT_PADDING_PX)
      expect(fit.zoom).toBeGreaterThan(zoomForRangeNm(25, vessel.lat, 240) + 1)
      const call = jumpToMock.mock.calls[0][0] as { zoom: number }
      expect(call.zoom).toBeCloseTo(fit.zoom, 6)
    })

    it('stops at the fit zoom cap for a single very close POI', async () => {
      const vessel = { lat: -20.27, lon: 148.94 }
      usePoiMock.mockReturnValue(poiResult({
        features: [feature({ id: 'a', name: 'A', lat: -20.2701, lon: 148.9401 })], // a few metres away
      }))

      await renderTile({ latitude: vessel.lat, longitude: vessel.lon, config: config({ rangeNm: 5 }) })

      const call = jumpToMock.mock.calls[0][0] as { zoom: number }
      expect(call.zoom).toBe(POI_MAP_MAX_ZOOM)
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

  // The map never animates: the wall's processor cannot keep up with camera
  // flights, so the camera only ever jumps, and only when it must.
  describe('static camera', () => {
    const features = nearbyFeatures

    it('jumps to the overview on the first fix and never eases or flies', async () => {
      usePoiMock.mockReturnValue(poiResult({ features }))
      await renderTile()

      expect(jumpToMock).toHaveBeenCalledTimes(1)
      expect(easeToMock).not.toHaveBeenCalled()
      expect(flyToMock).not.toHaveBeenCalled()
      // Level and north-up, which pointInCameraFrame assumes; touch rotate is still on.
      expect(jumpToMock.mock.calls[0][0]).toMatchObject({ pitch: 0, bearing: 0 })
    })

    it('makes no camera call when the highlight moves to another place', async () => {
      usePoiMock.mockReturnValue(poiResult({ features }))
      const { rerender } = await renderTile()
      jumpToMock.mockClear()

      useCyclingIndexMock.mockReturnValue(1)
      act(() => { rerender(tile()) })

      expect(jumpToMock).not.toHaveBeenCalled()
      expect(easeToMock).not.toHaveBeenCalled()
      expect(flyToMock).not.toHaveBeenCalled()
    })

    it('makes no camera call for fixes that move the boat inside the frame', async () => {
      usePoiMock.mockReturnValue(poiResult({ features }))
      const { rerender } = await renderTile()
      jumpToMock.mockClear()

      for (let i = 1; i <= 4; i++) {
        act(() => { rerender(tile({ latitude: vessel.lat - i * 0.0001, longitude: vessel.lon + i * 0.0001 })) })
      }

      expect(jumpToMock).not.toHaveBeenCalled()
    })

    it('does not jump when the same places swap rank', async () => {
      usePoiMock.mockReturnValue(poiResult({ features }))
      const { rerender } = await renderTile()
      jumpToMock.mockClear()

      usePoiMock.mockReturnValue(poiResult({ features: [features[1], features[0]] }))
      act(() => { rerender(tile()) })

      expect(jumpToMock).not.toHaveBeenCalled()
    })

    it('frames the map when it only becomes available after the first render, even if the position never moves', async () => {
      deferMapHandle = true
      usePoiMock.mockReturnValue(poiResult({ features }))
      await renderTile()

      expect(jumpToMock).toHaveBeenCalledTimes(1)
    })

    it('jumps once, centred on the boat, when the boat leaves the padded frame', async () => {
      usePoiMock.mockReturnValue(poiResult({ features }))
      const { rerender } = await renderTile()
      jumpToMock.mockClear()

      act(() => { rerender(tile({ latitude: vessel.lat, longitude: vessel.lon + 0.5 })) })

      expect(jumpToMock).toHaveBeenCalledTimes(1)
      const call = jumpToMock.mock.calls[0][0] as { center: [number, number] }
      expect(call.center).toEqual([vessel.lon + 0.5, vessel.lat])

      // The new camera is the one that counts: a nudge from there stays put.
      act(() => { rerender(tile({ latitude: vessel.lat, longitude: vessel.lon + 0.5001 })) })
      expect(jumpToMock).toHaveBeenCalledTimes(1)
    })

    it('jumps once when the ranked list changes', async () => {
      usePoiMock.mockReturnValue(poiResult({ features }))
      const { rerender } = await renderTile()
      jumpToMock.mockClear()

      usePoiMock.mockReturnValue(poiResult({ features: [features[0]] }))
      act(() => { rerender(tile()) })
      expect(jumpToMock).toHaveBeenCalledTimes(1)

      // The same ids in a fresh array (every poll) frame nothing new.
      usePoiMock.mockReturnValue(poiResult({ features: [{ ...features[0] }] }))
      act(() => { rerender(tile()) })
      expect(jumpToMock).toHaveBeenCalledTimes(1)
    })

    it('jumps once when the configured range changes', async () => {
      usePoiMock.mockReturnValue(poiResult({ features: [] }))
      const { rerender } = await renderTile({ config: config({ rangeNm: 5 }) })
      jumpToMock.mockClear()

      act(() => { rerender(tile({ config: config({ rangeNm: 8 }) })) })

      expect(jumpToMock).toHaveBeenCalledTimes(1)
    })

    it('holds the camera under a GNSS alert, then re-frames once when it clears', async () => {
      usePoiMock.mockReturnValue(poiResult({ features }))
      const { rerender } = await renderTile()
      jumpToMock.mockClear()

      // The boat is far outside the frame, but the position may not be real.
      act(() => { rerender(tile({ gnssCriticalAlert: true, latitude: vessel.lat, longitude: vessel.lon + 0.5 })) })
      act(() => { rerender(tile({ gnssCriticalAlert: true, latitude: vessel.lat, longitude: vessel.lon + 0.6 })) })
      expect(jumpToMock).not.toHaveBeenCalled()

      act(() => { rerender(tile({ gnssCriticalAlert: false, latitude: vessel.lat, longitude: vessel.lon + 0.6 })) })
      expect(jumpToMock).toHaveBeenCalledTimes(1)
      expect((jumpToMock.mock.calls[0][0] as { center: [number, number] }).center).toEqual([vessel.lon + 0.6, vessel.lat])
    })

    it('re-frames a list that changed during a GNSS alert when it clears', async () => {
      usePoiMock.mockReturnValue(poiResult({ features }))
      const { rerender } = await renderTile()
      jumpToMock.mockClear()

      usePoiMock.mockReturnValue(poiResult({ features: [features[0]] }))
      act(() => { rerender(tile({ gnssCriticalAlert: true })) })
      expect(jumpToMock).not.toHaveBeenCalled()

      act(() => { rerender(tile({ gnssCriticalAlert: false })) })
      expect(jumpToMock).toHaveBeenCalledTimes(1)
    })

  })

  // A curved arrow from the boat to the highlighted place stands in for the
  // camera flight: a screen overlay, so a change of place never redraws the map.
  describe('arrow to the highlighted place', () => {
    // 10000 px per degree around the vessel, which sits at (120, 120).
    const pixels = ([lng, lat]: [number, number]) => ({ x: (lng - vessel.lon) * 10000 + 120, y: (vessel.lat - lat) * 10000 + 120 })
    const features = nearbyFeatures
    const arrowPath = () => screen.getByTestId('poi-map-arrow-path').getAttribute('d')

    it('is drawn over the map in the split layout, aria-hidden and out of the way of the pointer', async () => {
      projectImpl = pixels
      usePoiMock.mockReturnValue(poiResult({ features }))
      await renderTile()

      const arrow = screen.getByTestId('poi-map-arrow')
      expect(arrow.getAttribute('aria-hidden')).toBe('true')
      expect(arrow.getAttribute('class')).toContain('pointer-events-none')
      expect(arrow.getAttribute('class')).toContain('z-10')
      expect(screen.getByTestId('poi-map-container').contains(arrow)).toBe(true)
      expect(arrowPath()).toMatch(/^M/)
    })

    it('is absent in the map layout, where no list says what it points at', async () => {
      projectImpl = pixels
      usePoiMock.mockReturnValue(poiResult({ features }))
      await renderTile({ config: config({ layout: 'map' }) })

      expect(screen.queryByTestId('poi-map-arrow')).toBeNull()
    })

    it('is absent under a GNSS alert, with no position and with no highlight', async () => {
      projectImpl = pixels
      usePoiMock.mockReturnValue(poiResult({ features }))
      const { rerender } = await renderTile({ gnssCriticalAlert: true })
      expect(screen.queryByTestId('poi-map-arrow')).toBeNull()

      act(() => { rerender(tile({ latitude: null, longitude: null })) })
      expect(screen.queryByTestId('poi-map-arrow')).toBeNull()

      act(() => { rerender(tile()) })
      expect(screen.getByTestId('poi-map-arrow')).toBeInTheDocument()

      usePoiMock.mockReturnValue(poiResult({ features: [] }))
      act(() => { rerender(tile()) })
      expect(screen.queryByTestId('poi-map-arrow')).toBeNull()
    })

    it('is absent without WebGL2', async () => {
      mockHasWebGL2 = false
      usePoiMock.mockReturnValue(poiResult({ features }))
      await renderTile()

      expect(screen.queryByTestId('poi-map-arrow')).toBeNull()
    })

    it('raises the highlighted marker above the others, and drops it again when the highlight moves', async () => {
      projectImpl = pixels
      usePoiMock.mockReturnValue(poiResult({ features }))
      const { rerender } = await renderTile()
      const zIndexOf = (name: string) => Number(
        screen.getByLabelText(`Point of interest: ${name}`).closest<HTMLElement>('[data-testid="marker"]')!.style.zIndex,
      )
      expect(zIndexOf('A')).toBeGreaterThan(zIndexOf('B'))

      useCyclingIndexMock.mockReturnValue(1)
      act(() => { rerender(tile()) })
      expect(zIndexOf('B')).toBeGreaterThan(zIndexOf('A'))
    })

    it('draws no arc when the place is too close to the boat, leaving only the ring', async () => {
      projectImpl = pixels
      usePoiMock.mockReturnValue(poiResult({ features: [feature({ id: 'near', name: 'Near', lat: -20.2701, lon: 148.9402 })] }))
      await renderTile()

      expect(screen.queryByTestId('poi-map-arrow-path')).toBeNull()
      expect(screen.getByTestId('poi-marker-expanded')).toBeInTheDocument()
    })

    it('switches to the new place when the highlight moves, without touching the camera', async () => {
      projectImpl = pixels
      usePoiMock.mockReturnValue(poiResult({ features }))
      const { rerender } = await renderTile()
      const first = arrowPath()
      jumpToMock.mockClear()

      useCyclingIndexMock.mockReturnValue(1)
      act(() => { rerender(tile()) })

      expect(arrowPath()).not.toBe(first)
      expect(jumpToMock).not.toHaveBeenCalled()
    })

    it('reprojects when the map moves, and when the boat moves', async () => {
      projectImpl = pixels
      usePoiMock.mockReturnValue(poiResult({ features }))
      const { rerender } = await renderTile()
      const first = arrowPath()

      // A pan: everything shifts down by 50 px on screen.
      projectImpl = (lngLat) => ({ ...pixels(lngLat), y: pixels(lngLat).y + 50 })
      act(() => { fireMapMove() })
      const panned = arrowPath()
      expect(panned).not.toBe(first)

      // The boat steps 0.005 degrees (50 px) west with the camera static.
      act(() => { rerender(tile({ longitude: vessel.lon - 0.005 })) })
      expect(arrowPath()).not.toBe(panned)
    })

    it('projects the place at the copy of its longitude nearest the boat across the antimeridian', async () => {
      const seen: number[] = []
      projectImpl = ([lng, lat]) => { seen.push(lng); return pixels([lng - 179.9 + vessel.lon, lat]) }
      usePoiMock.mockReturnValue(poiResult({
        features: [feature({ id: 'x', name: 'X', lat: -20.27, lon: -179.9 })],
      }))
      await renderTile({ latitude: -20.27, longitude: 179.9 })

      expect(seen).toContain(180.1)
    })

    it('renders no POI marker for a position fix or a poll that brings the same places back', async () => {
      projectImpl = pixels
      usePoiMock.mockReturnValue(poiResult({ features }))
      const { rerender } = await renderTile()
      markerRenders.clear()

      // A fresh set of objects, as every poll produces, and a boat that moved.
      usePoiMock.mockReturnValue(poiResult({ features: features.map((f) => ({ ...f })) }))
      act(() => { rerender(tile({ latitude: vessel.lat - 0.0001, longitude: vessel.lon + 0.0001 })) })

      expect(markerRenders.get(`${features[0].lat},${features[0].lon}`) ?? 0).toBe(0)
      expect(markerRenders.get(`${features[1].lat},${features[1].lon}`) ?? 0).toBe(0)
    })

    it('does not re-render the tile when the map moves', async () => {
      projectImpl = pixels
      usePoiMock.mockReturnValue(poiResult({ features }))
      await renderTile()
      const before = usePoiMock.mock.calls.length

      projectImpl = (lngLat) => ({ ...pixels(lngLat), x: pixels(lngLat).x + 30 })
      act(() => { fireMapMove() })

      expect(usePoiMock.mock.calls.length).toBe(before)
    })
  })

  // The place the arrow points at is the one the operator must be able to
  // name, so in the split layout its label outranks every neighbour. The
  // map-only layout has no arrow or list to explain labels swapping, so it
  // keeps the rank order.
  describe('highlighted label', () => {
    const features = [
      feature({ id: 'near', name: 'Near', distanceM: 100 }),
      feature({ id: 'far', name: 'Far', distanceM: 5000 }),
    ]
    // The list rows repeat the names, so ask the marker, not the page.
    const labelShown = (poiName: string) =>
      within(screen.getByLabelText(`Point of interest: ${poiName}`)).queryByText(poiName) !== null

    it('is never suppressed by a higher-ranked neighbour in the split layout, and re-resolves when the highlight moves', async () => {
      projectImpl = () => ({ x: 100, y: 100 })
      usePoiMock.mockReturnValue(poiResult({ features }))
      useCyclingIndexMock.mockReturnValue(1)
      const { rerender } = await renderTile({ config: config({ layout: 'split' }) })

      expect(labelShown('Far')).toBe(true)
      expect(labelShown('Near')).toBe(false)

      useCyclingIndexMock.mockReturnValue(0)
      act(() => { rerender(tile({ config: config({ layout: 'split' }) })) })
      expect(labelShown('Near')).toBe(true)
      expect(labelShown('Far')).toBe(false)
    })

    it('keeps the rank order in the map-only layout when the highlight moves', async () => {
      projectImpl = () => ({ x: 100, y: 100 })
      usePoiMock.mockReturnValue(poiResult({ features }))
      useCyclingIndexMock.mockReturnValue(1)
      const { rerender } = await renderTile({ config: config({ layout: 'map' }) })
      expect(labelShown('Near')).toBe(true)
      expect(labelShown('Far')).toBe(false)

      useCyclingIndexMock.mockReturnValue(0)
      act(() => { rerender(tile({ config: config({ layout: 'map' }) })) })
      expect(labelShown('Near')).toBe(true)
      expect(labelShown('Far')).toBe(false)
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
    it('recomputes the resetKey only when the set of ranked places changes', async () => {
      const initial = [
        feature({ id: 'a', name: 'A', distanceM: 100, detail: 'About A.' }),
        feature({ id: 'b', name: 'B', distanceM: 200, detail: 'About B.' }),
      ]
      usePoiMock.mockReturnValue(poiResult({ features: initial }))
      const { rerender } = await renderTile({ config: config({ layout: 'split' }) })

      const firstResetKey = useCyclingIndexMock.mock.calls.at(-1)![2]
      expect(firstResetKey).toBe('a|b')

      // The same places swapping rank change nothing on screen, so they must
      // not restart the cycle and send the arrow back to the first place.
      usePoiMock.mockReturnValue(poiResult({ features: [initial[1], initial[0]] }))
      act(() => { rerender(<PoiMapTile {...renderTileDefaultProps} config={config({ layout: 'split' })} />) })
      expect(useCyclingIndexMock.mock.calls.at(-1)![2]).toBe(firstResetKey)

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

describe('PoiMapTile own-ship trail', () => {
  const trailPoint = (i: number) => ({ lat: -20.27 + i / 1000, lon: 148.94, timestampMs: 1000 * i })
  const trailCoords = () => (sourceData['poi-map-own-trail'] as GeoJSON.Feature<GeoJSON.LineString>).geometry.coordinates

  it('redraws when a trail poll lands new points while the nearby places have not changed', async () => {
    // One result object, so the places do not change between renders: the
    // trail alone must be enough to redraw (a failing place fetch used to
    // freeze the boat's trail).
    usePoiMock.mockReturnValue(poiResult({ features: [feature({})] }))
    const props = { ...renderTileDefaultProps, config: config({ showTrail: true }) }
    const { rerender } = await renderTile({ selfTrail: [trailPoint(0), trailPoint(1)], config: config({ showTrail: true }) })
    expect(trailCoords()).toHaveLength(2)

    rerender(<PoiMapTile {...props} selfTrail={[trailPoint(0), trailPoint(1), trailPoint(2)]} />)
    expect(trailCoords()).toHaveLength(3)
  })

  it('keeps the trail GeoJSON identity when nothing about the trail changed', async () => {
    usePoiMock.mockReturnValue(poiResult({ features: [feature({})] }))
    const trail = [trailPoint(0), trailPoint(1)]
    const props = { ...renderTileDefaultProps, config: config({ showTrail: true }) }
    const { rerender } = await renderTile({ selfTrail: trail, config: config({ showTrail: true }) })
    const first = sourceData['poi-map-own-trail']

    usePoiMock.mockReturnValue(poiResult({ features: [feature({})] }))
    rerender(<PoiMapTile {...props} selfTrail={trail} />)
    expect(sourceData['poi-map-own-trail']).toBe(first)
  })
})
