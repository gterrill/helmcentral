import { act, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { PoiMapTile } from '@/components/poi-map-tile'
import type { PoiMapWidgetConfig } from '@/lib/dashboard-widgets'
import type { PoiFeature } from '@/lib/poi'
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

vi.mock('react-map-gl/maplibre', async () => {
  const React = await import('react')
  return {
    Map: React.forwardRef(
      (
        { children, onLoad }: { children?: React.ReactNode; onLoad?: () => void },
        ref: React.Ref<unknown>,
      ) => {
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

afterEach(() => {
  vi.clearAllMocks()
  vi.useRealTimers()
  projectImpl = ([lng, lat]) => ({ x: lng, y: lat })
  mockHasWebGL2 = true
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

function renderTile(props: Partial<React.ComponentProps<typeof PoiMapTile>> = {}) {
  return render(
    <PoiMapTile
      config={config()}
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
      {...props}
    />,
  )
}

describe('PoiMapTile', () => {
  // Same guard as the anchor and route maps: the map container owns its
  // own stacking context so nothing drawn over the canvas can escape above
  // a sheet or the mobile sidebar.
  it('isolates its overlays so they cannot draw above a sheet', () => {
    usePoiMock.mockReturnValue(poiResult({}))

    renderTile()

    expect(screen.getByTestId('poi-map-container').className.split(/\s+/)).toContain('isolate')
  })

  it('renders one marker per feature and rank badges matching the ranked list rows', () => {
    const features = [
      feature({ id: 'a', name: 'A', distanceM: 100 }),
      feature({ id: 'b', name: 'B', distanceM: 200 }),
      feature({ id: 'c', name: 'C', distanceM: 300 }),
    ]
    usePoiMock.mockReturnValue(poiResult({ features }))

    renderTile()

    expect(screen.getAllByLabelText(/^Point of interest:/)).toHaveLength(3)
    expect(screen.getAllByTestId('poi-list-row')).toHaveLength(3)

    // The list's own row order is the rank order, and the matching marker
    // carries the same number.
    expect(screen.getByTestId('poi-marker-rank-a').textContent).toBe('1')
    expect(screen.getByTestId('poi-marker-rank-b').textContent).toBe('2')
    expect(screen.getByTestId('poi-marker-rank-c').textContent).toBe('3')
  })

  it('shows only the map in "map" layout, and adds the ranked list in "split"', () => {
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

  it('passes the configured range and categories through to usePoi (category filter)', () => {
    usePoiMock.mockReturnValue(poiResult({}))
    renderTile({ config: config({ rangeNm: 8, categories: ['dive', 'trail'] }) })

    expect(usePoiMock).toHaveBeenCalledWith(8, ['dive', 'trail'], expect.any(Number))
  })

  it('renders AIS markers only when showAis is on', () => {
    usePoiMock.mockReturnValue(poiResult({}))
    const { rerender } = renderTile({ config: config({ showAis: false }), nearbyVessels: [aisVessel] })
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

  it('shows the amber GNSS badge and never calls easeTo or jumpTo while gnssCriticalAlert is set', () => {
    usePoiMock.mockReturnValue(poiResult({}))
    renderTile({ gnssCriticalAlert: true })

    expect(screen.getByTestId('poi-map-gnss-badge')).toBeInTheDocument()
    expect(easeToMock).not.toHaveBeenCalled()
    expect(jumpToMock).not.toHaveBeenCalled()
  })

  it('jumps to the first fix, then eases at most once per 2-second window', () => {
    vi.useFakeTimers()
    usePoiMock.mockReturnValue(poiResult({}))
    const { rerender } = renderTile({ latitude: -20.27, longitude: 148.94 })

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

  it('suppresses a marker label that collides on screen with a higher-ranked one (project mock)', () => {
    // Both features project to almost the same screen point, so the
    // lower-ranked (farther) one should yield its label. "map" layout keeps
    // the assertion unambiguous — no ranked-list row to also match on name.
    projectImpl = () => ({ x: 100, y: 100 })
    const features = [
      feature({ id: 'near', name: 'Near', distanceM: 100 }),
      feature({ id: 'far', name: 'Far', distanceM: 5000 }),
    ]
    usePoiMock.mockReturnValue(poiResult({ features }))

    renderTile({ config: config({ layout: 'map' }) })

    expect(screen.getByText('Near')).toBeInTheDocument()
    expect(screen.queryByText('Far')).toBeNull()
  })

  it('keeps showing the last good list and its age/error when the feed is unavailable (no masking fallback)', () => {
    usePoiMock.mockReturnValue(poiResult({
      features: [feature({ id: 'stale-1', name: 'Stale Anchorage' })],
      error: 'POI provider unavailable (502)',
      fetchedAt: '2026-09-11T00:00:00Z',
    }))

    renderTile()

    // The name appears twice (the marker label and the list row); the point
    // is that it is never zero, i.e. the feature is never dropped on error.
    expect(screen.getAllByText('Stale Anchorage').length).toBeGreaterThan(0)
    expect(screen.getByText('POI provider unavailable (502)')).toBeInTheDocument()
  })

  it('shows "POI feed unavailable" rather than "No points of interest in range" when the feed has never succeeded', () => {
    usePoiMock.mockReturnValue(poiResult({
      features: [],
      loading: false,
      error: 'POI provider unavailable (502)',
      fetchedAt: null,
    }))

    renderTile()

    expect(screen.getByText('POI feed unavailable')).toBeInTheDocument()
    expect(screen.queryByText('No points of interest in range')).toBeNull()
    expect(screen.getByText('POI provider unavailable (502)')).toBeInTheDocument()
  })

  it('shows "No points of interest in range" only once the fetch has actually succeeded with zero features', () => {
    usePoiMock.mockReturnValue(poiResult({
      features: [],
      loading: false,
      error: null,
      fetchedAt: '2026-09-11T00:00:00Z',
    }))

    renderTile()

    expect(screen.getByText('No points of interest in range')).toBeInTheDocument()
  })

  describe('without WebGL2', () => {
    it('does not mount the map, shows the fallback panel, and keeps the ranked list working ("split" layout)', () => {
      mockHasWebGL2 = false
      const features = [feature({ id: 'a', name: 'A' }), feature({ id: 'b', name: 'B' })]
      usePoiMock.mockReturnValue(poiResult({ features }))

      renderTile({ config: config({ layout: 'split' }) })

      expect(screen.queryByTestId('map-root')).not.toBeInTheDocument()
      expect(screen.getByTestId('poi-map-webgl2-fallback')).toHaveTextContent(
        'Map needs WebGL2, which this browser does not provide',
      )
      expect(screen.getAllByTestId('poi-list-row')).toHaveLength(2)
    })

    it('fills the tile with the fallback panel in "map" layout', () => {
      mockHasWebGL2 = false
      usePoiMock.mockReturnValue(poiResult({}))

      renderTile({ config: config({ layout: 'map' }) })

      expect(screen.queryByTestId('map-root')).not.toBeInTheDocument()
      const fallback = screen.getByTestId('poi-map-webgl2-fallback')
      expect(fallback).toHaveTextContent('Map needs WebGL2, which this browser does not provide')
      expect(fallback.className).toContain('h-full')
    })
  })
})
