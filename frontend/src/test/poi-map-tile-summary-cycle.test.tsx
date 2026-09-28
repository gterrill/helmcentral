import { act, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

// Renders PoiMapTileImpl directly rather than through the lazy PoiMapTile
// wrapper (poi-map-tile.tsx): the wrapper's Suspense boundary only resolves
// under real timers (see poi-map-tile.test.tsx's own comment on this), which
// would fight the fake timers this file needs to advance the real
// useCyclingIndex hook. Going straight at the impl sidesteps that lazy chunk
// entirely, so fake timers can be installed from the very first render, the
// same way use-cycling-index.test.ts does for the hook alone.
import PoiMapTileImpl from '@/components/poi-map-tile-impl'
import type { PoiMapTileProps } from '@/components/poi-map-tile-impl'
import type { PoiMapWidgetConfig } from '@/lib/dashboard-widgets'
import type { PoiFeature } from '@/lib/poi'
import type { UsePoiResult } from '@/hooks/use-poi'

vi.mock('maplibre-gl', () => ({ default: {} }))
vi.mock('@/lib/webgl', () => ({ hasWebGL2: () => true }))

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
          easeTo: vi.fn(),
          jumpTo: vi.fn(),
          project: ([lng, lat]: [number, number]) => ({ x: lng, y: lat }),
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

vi.mock('@/components/map-place-labels', () => ({ MapPlaceLabels: () => null }))

const usePoiMock = vi.fn()
vi.mock('@/hooks/use-poi', () => ({
  usePoi: (...args: unknown[]) => usePoiMock(...args),
}))

// useCyclingIndex is deliberately NOT mocked here - this file exists to
// exercise the real hook (real fake-timer advances) against the tile's own
// wiring, which the mocked version in poi-map-tile.test.tsx cannot do.

afterEach(() => {
  vi.clearAllMocks()
  vi.useRealTimers()
})

function feature(overrides: Partial<PoiFeature>): PoiFeature {
  return {
    id: 'f1', category: 'anchorage', name: 'Cid Harbour', lat: -20.28, lon: 148.95,
    distanceM: 1200, bearingDeg: 45, detail: '', sourceUrl: '',
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

const defaultProps: Omit<PoiMapTileProps, 'config'> = {
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

describe('PoiMapTileImpl summary cycling (real useCyclingIndex, fake timers)', () => {
  // The bug this reproduces: live boat data is typically 7 POIs with only
  // one carrying a non-empty `detail` (OSM rarely has editorial summaries).
  // The old code fed only detail-bearing POIs to useCyclingIndex, so the
  // cyclable count was 1 and the cycle never advanced - the highlight sat on
  // one row forever instead of moving through the ranked list.
  it('advances the highlighted/expanded row through every ranked POI, not just the one with a detail', () => {
    vi.useFakeTimers()
    const features = [
      feature({ id: 'a', name: 'A', distanceM: 100, detail: '' }),
      feature({ id: 'b', name: 'B', distanceM: 200, detail: 'About B.' }),
      feature({ id: 'c', name: 'C', distanceM: 300, detail: '' }),
    ]
    usePoiMock.mockReturnValue(poiResult({ features }))

    render(
      <PoiMapTileImpl
        {...defaultProps}
        config={config({ layout: 'split', summaryCycleSeconds: 5 })}
      />,
    )

    // t=0: highest-ranked POI ('a') is cycled to first, even though it has
    // no detail - the ring/highlight must not wait for a detail to exist.
    expect(screen.getByTestId('poi-marker-expanded')).toBeInTheDocument()
    expect(screen.getByTestId('poi-marker-rank-a').closest('[data-testid="poi-marker-expanded"]')).toBeTruthy()
    expect(screen.queryByTestId('poi-list-row-summary')).toBeNull()

    // Advancing one full interval must move the cycle on to 'b' - this is
    // the "cycling does nothing" bug: under the old filter, the cyclable
    // set here would have had exactly one member ('b'), and useCyclingIndex
    // never starts a timer for a set of size 1.
    act(() => { vi.advanceTimersByTime(5000) })
    expect(screen.getByTestId('poi-marker-rank-b').closest('[data-testid="poi-marker-expanded"]')).toBeTruthy()
    expect(screen.getByTestId('poi-list-row-summary')).toHaveTextContent('About B.')

    // And on again to 'c', which also has no detail.
    act(() => { vi.advanceTimersByTime(5000) })
    expect(screen.getByTestId('poi-marker-rank-c').closest('[data-testid="poi-marker-expanded"]')).toBeTruthy()
    expect(screen.queryByTestId('poi-list-row-summary')).toBeNull()

    // Wraps back to 'a'.
    act(() => { vi.advanceTimersByTime(5000) })
    expect(screen.getByTestId('poi-marker-rank-a').closest('[data-testid="poi-marker-expanded"]')).toBeTruthy()
  })

  // The ranked list is re-derived from whatever usePoi returns on every
  // poll, so it can shrink or empty out entirely while a cycle is mid-way
  // through - a POI drops out of range, or the feed briefly comes back with
  // nothing at all. The highlight must never keep pointing at an index past
  // the end of a shorter list (which used to read `undefined` off the
  // array), and must clear cleanly - not throw - once there is nothing left
  // to cycle through.
  it('keeps the highlight on a valid row when the ranked list shrinks or empties mid-cycle', () => {
    vi.useFakeTimers()
    const threeFeatures = [
      feature({ id: 'a', name: 'A', distanceM: 100, detail: '' }),
      feature({ id: 'b', name: 'B', distanceM: 200, detail: '' }),
      feature({ id: 'c', name: 'C', distanceM: 300, detail: 'About C.' }),
    ]
    usePoiMock.mockReturnValue(poiResult({ features: threeFeatures }))

    const { rerender } = render(
      <PoiMapTileImpl {...defaultProps} config={config({ layout: 'split', summaryCycleSeconds: 5 })} />,
    )

    // Advance two full intervals to reach the 3rd ranked POI, 'c'.
    act(() => { vi.advanceTimersByTime(10000) })
    expect(screen.getByTestId('poi-marker-rank-c').closest('[data-testid="poi-marker-expanded"]')).toBeTruthy()

    // The feed's next poll comes back shorter - 'c' has dropped out of
    // range entirely.
    const twoFeatures = [threeFeatures[0], threeFeatures[1]]
    usePoiMock.mockReturnValue(poiResult({ features: twoFeatures }))
    act(() => {
      rerender(<PoiMapTileImpl {...defaultProps} config={config({ layout: 'split', summaryCycleSeconds: 5 })} />)
    })

    // Exactly one row/marker is highlighted, and it is one that still
    // exists - never 'c', and never nothing.
    expect(screen.getAllByTestId('poi-marker-expanded')).toHaveLength(1)
    expect(screen.queryByTestId('poi-marker-rank-c')).toBeNull()

    // Advancing further must not throw or resurrect 'c'.
    act(() => { vi.advanceTimersByTime(5000) })
    expect(screen.getAllByTestId('poi-marker-expanded')).toHaveLength(1)
    expect(screen.queryByTestId('poi-marker-rank-c')).toBeNull()

    // The feed's next poll comes back with nothing in range at all.
    usePoiMock.mockReturnValue(poiResult({ features: [] }))
    act(() => {
      rerender(<PoiMapTileImpl {...defaultProps} config={config({ layout: 'split', summaryCycleSeconds: 5 })} />)
    })

    expect(screen.queryByTestId('poi-marker-expanded')).toBeNull()
    expect(screen.queryAllByTestId('poi-list-row')).toHaveLength(0)

    // Advancing the clock with nothing to cycle through must not throw.
    expect(() => {
      act(() => { vi.advanceTimersByTime(10000) })
    }).not.toThrow()
    expect(screen.queryByTestId('poi-marker-expanded')).toBeNull()
  })
})
