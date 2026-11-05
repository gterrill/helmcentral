import { cleanup, render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { PoiMapTile } from '@/components/poi-map-tile'
import type { PoiMapWidgetConfig } from '@/lib/dashboard-widgets'
import type { PoiFeature } from '@/lib/poi'
import type { NearbyVessel, NearbyVesselFeed } from '@/hooks/use-nearby-vessels'
import type { AlarmState } from '@/hooks/use-alarms'
import type { UsePoiResult } from '@/hooks/use-poi'

vi.mock('maplibre-gl', () => ({ default: {} }))
vi.mock('@/lib/webgl', () => ({ hasWebGL2: () => mockHasWebGL2 }))
let mockHasWebGL2 = true

const jumpToMock = vi.fn()
let projectImpl: (lngLat: [number, number]) => { x: number; y: number } = ([lng, lat]) => ({ x: lng, y: lat })
let projectCalls = 0
const fakeMap = {
  on: () => {},
  off: () => {},
  project: (lngLat: [number, number]) => { projectCalls++; return projectImpl(lngLat) },
}
const markerRenders = new globalThis.Map<string, number>()

vi.mock('react-map-gl/maplibre', async () => {
  const React = await import('react')
  return {
    Map: React.forwardRef(({ children, onLoad }: { children?: React.ReactNode; onLoad?: () => void }, ref: React.Ref<unknown>) => {
      React.useImperativeHandle(ref, () => ({ getCanvas: () => ({ style: {} }), getZoom: () => 12, jumpTo: jumpToMock, project: fakeMap.project }), [])
      React.useEffect(() => { onLoad?.() }, [onLoad])
      return <div data-testid="map-root">{children}</div>
    }),
    Marker: ({ children, latitude, longitude }: { children?: React.ReactNode; latitude?: number; longitude?: number }) => {
      markerRenders.set(`${latitude},${longitude}`, (markerRenders.get(`${latitude},${longitude}`) ?? 0) + 1)
      return <div data-testid="marker">{children}</div>
    },
    useMap: () => ({ current: { getMap: () => fakeMap } }),
    Source: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
    Layer: () => null,
  }
})
vi.mock('@/components/map-place-labels', () => ({ MapPlaceLabels: () => null }))

const usePoiMock = vi.fn()
vi.mock('@/hooks/use-poi', () => ({ usePoi: (...args: unknown[]) => usePoiMock(...args) }))

const useCyclingIndexMock = vi.fn<(count: number, s: number, key: string) => number | null>()
vi.mock('@/hooks/use-cycling-index', () => ({
  useCyclingIndex: (c: number, s: number, k: string) => useCyclingIndexMock(c, s, k),
}))

// Reports a list pane of the height a test chooses; the map container gets a fixed box.
let listPaneHeight = 225
let listPaneWidth = 700
const OriginalResizeObserver = globalThis.ResizeObserver
beforeEach(() => {
  globalThis.ResizeObserver = class {
    constructor(private cb: (entries: unknown[]) => void) {}
    observe(el: Element) {
      const isList = el.getAttribute('data-testid') === 'poi-list-pane'
      this.cb([{ contentRect: { width: isList ? listPaneWidth : 400, height: isList ? listPaneHeight : 300 } }])
    }
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver
  useCyclingIndexMock.mockImplementation((count) => (count > 0 ? 0 : null))
})
afterEach(() => {
  globalThis.ResizeObserver = OriginalResizeObserver
  vi.clearAllMocks()
  markerRenders.clear()
  projectImpl = ([lng, lat]) => ({ x: lng, y: lat })
  projectCalls = 0
  listPaneHeight = 225
  listPaneWidth = 700
  mockHasWebGL2 = true
})

function feature(id: string, name: string, distanceM: number): PoiFeature {
  return { id, category: 'anchorage', name, lat: -20.28, lon: 148.95, distanceM, bearingDeg: 45, detail: '', sourceUrl: '' }
}
function poiResult(features: PoiFeature[]): UsePoiResult {
  return { features, center: { lat: -20.27, lon: 148.94 }, provider: 'osm-overpass', cached: false, truncated: [], unsupported: [], loading: false, fetchedAt: '2026-09-11T00:00:00Z', error: null, retryAt: null }
}
function config(overrides: Partial<PoiMapWidgetConfig> = {}): PoiMapWidgetConfig {
  return { title: 'Nearby', rangeNm: 5, categories: ['anchorage'], layout: 'split', showAis: true, showTrail: false, ...overrides }
}
function ves(id: string, rangeM: number, overrides: Partial<NearbyVessel> = {}): NearbyVessel {
  return { id, name: id.toUpperCase(), range_m: rangeM, age_seconds: 5, lat: -20.271, lon: 148.941, seen_count: 0, ...overrides }
}
const liveFeed: NearbyVesselFeed = { source: 'signalk', maxRangeM: 5000 }

const defaults: Omit<React.ComponentProps<typeof PoiMapTile>, 'config'> = {
  editing: false, latitude: -20.27, longitude: 148.94, headingTrue: 90, gnssCriticalAlert: false,
  positionLastUpdateAgeS: 5, nearbyVessels: [], nearbyVesselFeed: liveFeed, selfTrail: [], isDarkTheme: false, distanceUnits: 'metric',
}
const tile = (props: Partial<React.ComponentProps<typeof PoiMapTile>> = {}) => <PoiMapTile {...defaults} config={config()} {...props} />
async function renderTile(props: Partial<React.ComponentProps<typeof PoiMapTile>> = {}) {
  const result = render(tile(props))
  await screen.findByTestId('poi-map-container')
  return result
}
const rowNames = () => screen.queryAllByTestId('poi-vessel-row').map((r) => within(r).getAllByText(/^[A-Z0-9]+$/)[0].textContent)

describe('Nearby list Vessels group', () => {
  beforeEach(() => { usePoiMock.mockReturnValue(poiResult([feature('p1', 'Cid Harbour', 1200)])) })

  it('is present with showAis in the split layout, and absent without showAis or in the map layout', async () => {
    const { rerender } = await renderTile({ nearbyVessels: [ves('a', 100)] })
    expect(screen.getByTestId('poi-vessel-group')).toBeInTheDocument()
    rerender(tile({ config: config({ showAis: false }), nearbyVessels: [ves('a', 100)] }))
    expect(screen.queryByTestId('poi-vessel-group')).toBeNull()
    expect(screen.queryByTestId('poi-list-groups')).toBeNull()
    rerender(tile({ config: config({ layout: 'map' }), nearbyVessels: [ves('a', 100)] }))
    expect(screen.queryByTestId('poi-vessel-group')).toBeNull()
  })

  it('keeps place ranks and gives vessels no rank number', async () => {
    await renderTile({ nearbyVessels: [ves('a', 100)] })
    expect(screen.getAllByTestId('poi-list-row')).toHaveLength(1)
    expect(screen.queryByTestId('poi-marker-rank-a')).toBeNull()
    expect(within(screen.getByTestId('poi-vessel-row')).queryByText('1')).toBeNull()
  })

  it('shows four vessels and a +N more line', async () => {
    await renderTile({ nearbyVessels: ['a', 'b', 'c', 'd', 'e', 'f'].map((id, i) => ves(id, 100 + i)) })
    expect(rowNames()).toEqual(['A', 'B', 'C', 'D'])
    expect(screen.getByTestId('poi-vessel-more').textContent).toBe('+2 more')
  })

  it('lists alarmed vessels first, worst first, then nearest', async () => {
    const alarms = new globalThis.Map<string, AlarmState>([['far', 'warn'], ['mid', 'alarm']])
    await renderTile({
      nearbyVessels: [ves('near', 100), ves('mid', 300), ves('far', 900), ves('near2', 200)],
      aisCollisionAlarms: alarms,
    })
    expect(rowNames()).toEqual(['MID', 'FAR', 'NEAR', 'NEAR2'])
  })

  it('drops vessels beyond the configured range and beyond the feed range', async () => {
    // 1 nm = 1852 m configured, feed 5000 m: the smaller limit applies.
    const { rerender } = await renderTile({ config: config({ rangeNm: 1 }), nearbyVessels: [ves('in', 1800), ves('out', 1900)] })
    expect(rowNames()).toEqual(['IN'])
    rerender(tile({ config: config({ rangeNm: 10 }), nearbyVessels: [ves('in', 4900), ves('out', 5100)] }))
    expect(rowNames()).toEqual(['IN'])
  })

  it('lists a vessel in collision alarm or warning beyond the configured range', async () => {
    const alarms = new globalThis.Map<string, AlarmState>([['far', 'alarm'], ['farther', 'warn'], ['normal', 'normal']])
    await renderTile({
      config: config({ rangeNm: 1 }),
      nearbyVessels: [ves('near', 500), ves('far', 3000), ves('farther', 4000), ves('normal', 3500)],
      aisCollisionAlarms: alarms,
    })
    expect(rowNames()).toEqual(['FAR', 'FARTHER', 'NEAR'])
  })

  it('still draws a marker for a vessel the list leaves out', async () => {
    await renderTile({ nearbyVessels: [ves('out', 9000)] })
    expect(screen.getByLabelText('AIS vessel: OUT')).toBeInTheDocument()
    expect(screen.queryAllByTestId('poi-vessel-row')).toHaveLength(0)
  })

  it('shows sighting text only when seen_count is above zero', async () => {
    const last = new Date(Date.now() - 2 * 86400_000).toISOString()
    await renderTile({ nearbyVessels: [ves('a', 100, { seen_count: 7, last_seen_at: last }), ves('b', 200, { seen_count: 0 })] })
    const [a, b] = screen.getAllByTestId('poi-vessel-row')
    expect(a.textContent).toContain('Seen 7x before, last 2d ago')
    expect(b.textContent).not.toMatch(/seen|sighting/i)
  })

  it('gives distance and bearing, and omits the bearing without a position', async () => {
    const { rerender } = await renderTile({ nearbyVessels: [ves('a', 1500, { lat: -20.26, lon: 148.94 })] })
    expect(screen.getByTestId('poi-vessel-row').textContent).toContain('1.5 km 000° N')
    rerender(tile({ nearbyVessels: [ves('a', 1500, { lat: undefined, lon: undefined })] }))
    expect(screen.getByTestId('poi-vessel-row').textContent).toContain('1.5 km')
    expect(screen.getByTestId('poi-vessel-row').textContent).not.toContain('°')
    rerender(tile({ latitude: null, longitude: null, nearbyVessels: [ves('a', 1500)] }))
    expect(screen.getByTestId('poi-vessel-row').textContent).not.toContain('°')
  })

  it('gives metres under a kilometre', async () => {
    await renderTile({ nearbyVessels: [ves('a', 335, { lat: -20.26, lon: 148.94 })] })
    expect(screen.getByTestId('poi-vessel-row').textContent).toContain('335 m')
    expect(screen.getByTestId('poi-vessel-row').textContent).not.toContain('0.3 km')
  })

  it('gives whole feet under 0.1 nm and nautical miles above in imperial', async () => {
    const { rerender } = await renderTile({ distanceUnits: 'imperial', nearbyVessels: [ves('a', 80, { lat: -20.26, lon: 148.94 })] })
    expect(screen.getByTestId('poi-vessel-row').textContent).toContain('262 ft')
    expect(screen.getByTestId('poi-vessel-row').textContent).not.toContain('0.0 nm')
    rerender(tile({ distanceUnits: 'imperial', nearbyVessels: [ves('a', 1000, { lat: -20.26, lon: 148.94 })] }))
    expect(screen.getByTestId('poi-vessel-row').textContent).toMatch(/\d nm/)
    expect(screen.getByTestId('poi-vessel-row').textContent).not.toContain(' ft')
    rerender(tile({ distanceUnits: 'metric', nearbyVessels: [ves('a', 335, { lat: -20.26, lon: 148.94 })] }))
    expect(screen.getByTestId('poi-vessel-row').textContent).toContain('335 m')
  })

  it('styles an alarmed row and names the tier', async () => {
    const alarms = new globalThis.Map<string, AlarmState>([['a', 'emergency'], ['b', 'warn']])
    await renderTile({ nearbyVessels: [ves('a', 100), ves('b', 200)], aisCollisionAlarms: alarms })
    const alarm = screen.getByLabelText('A, collision alarm')
    expect(alarm.className).toContain('border-red-500')
    expect(alarm.className).not.toContain('border-l-2')
    expect(alarm.textContent).toContain('Collision alarm')
    const warn = screen.getByLabelText('B, collision warning')
    expect(warn.className).toContain('border-amber-500')
    expect(warn.textContent).toContain('Collision warning')
  })

  it('says a position is old only past 180 seconds', async () => {
    const { rerender } = await renderTile({ nearbyVessels: [ves('a', 100, { age_seconds: 180 })] })
    expect(screen.getByTestId('poi-vessel-row').textContent).not.toContain('old')
    rerender(tile({ nearbyVessels: [ves('a', 100, { age_seconds: 181 })] }))
    expect(screen.getByTestId('poi-vessel-row').textContent).toContain('position 3m old')
    rerender(tile({ nearbyVessels: [ves('a', 100, { age_seconds: 300 })] }))
    expect(screen.getByTestId('poi-vessel-row').textContent).toContain('position 5m old')
  })

  it('heads the places group when vessels are shown, and not when the list is places only', async () => {
    const { rerender } = await renderTile({ nearbyVessels: [ves('a', 100)] })
    expect(within(screen.getByTestId('poi-places-group')).getByText('Places')).toBeInTheDocument()
    rerender(tile({ config: config({ showAis: false }), nearbyVessels: [ves('a', 100)] }))
    expect(screen.queryByText('Places')).toBeNull()
  })

  it('spaces the place rows the same with or without the Vessels group', async () => {
    const gap = () => screen.getAllByTestId('poi-list-row')[0].parentElement?.className
    const { rerender } = await renderTile({ nearbyVessels: [ves('a', 100)] })
    expect(gap()).toContain('gap-0.5')
    rerender(tile({ config: config({ showAis: false }), nearbyVessels: [ves('a', 100)] }))
    expect(gap()).toContain('gap-0.5')
  })
})

describe('Vessels group states', () => {
  beforeEach(() => { usePoiMock.mockReturnValue(poiResult([feature('p1', 'Cid Harbour', 1200)])) })
  const state = () => screen.getByTestId('poi-vessel-state').textContent

  it('asks for a GNSS fix, with no rows, ahead of an empty list and of rows', async () => {
    const { rerender } = await renderTile({ gnssCriticalAlert: true, nearbyVessels: [ves('a', 100)] })
    expect(state()).toBe('Vessels need a GNSS fix')
    expect(screen.queryAllByTestId('poi-vessel-row')).toHaveLength(0)
    rerender(tile({ gnssCriticalAlert: true, nearbyVessels: [] }))
    expect(state()).toBe('Vessels need a GNSS fix')
  })

  it('says AIS unavailable ahead of the GNSS message when the source is not signalk', async () => {
    await renderTile({ gnssCriticalAlert: true, nearbyVesselFeed: { source: 'backend-fallback', maxRangeM: 5000 }, nearbyVessels: [ves('a', 100)] })
    expect(state()).toBe('AIS unavailable')
  })

  it('says loading before the first event, never no vessels', async () => {
    await renderTile({ nearbyVesselFeed: null })
    expect(state()).toBe('Loading…')
  })

  it('says AIS unavailable when the source is not signalk, ahead of an empty list', async () => {
    await renderTile({ nearbyVesselFeed: { source: 'backend-fallback', maxRangeM: 5000 } })
    expect(state()).toBe('AIS unavailable')
    await Promise.resolve()
  })

  it('says AIS unavailable when the source is unknown', async () => {
    await renderTile({ nearbyVesselFeed: { source: null, maxRangeM: null } })
    expect(state()).toBe('AIS unavailable')
  })

  it('names the smaller range when nothing is in range', async () => {
    const { rerender } = await renderTile()
    expect(state()).toBe('No vessels within 5 km')
    rerender(tile({ config: config({ rangeNm: 1 }), distanceUnits: 'imperial' }))
    expect(state()).toBe('No vessels within 1 nm')
  })

  it('names no range when the feed has not said how far it looks', async () => {
    await renderTile({ nearbyVesselFeed: { source: 'signalk', maxRangeM: null } })
    expect(state()).toBe('No vessels in range')
  })
})

describe('Vessels group layout', () => {
  beforeEach(() => { usePoiMock.mockReturnValue(poiResult([feature('p1', 'Cid Harbour', 1200)])) })

  it('sits beside the places on a short list pane and stacks on a tall one', async () => {
    await renderTile()
    expect(screen.getByTestId('poi-list-groups').dataset.stacked).toBe('false')
  })

  it('stacks from 460 px', async () => {
    listPaneHeight = 460
    await renderTile()
    expect(screen.getByTestId('poi-list-groups').dataset.stacked).toBe('true')
  })

  it('stays beside at 459 px', async () => {
    listPaneHeight = 459
    await renderTile()
    expect(screen.getByTestId('poi-list-groups').dataset.stacked).toBe('false')
  })

  it('holds the tile content to the card, so a tall list cannot stretch the map', async () => {
    await renderTile()
    expect(screen.getByTestId('poi-map-container').closest('[data-slot="card-content"]')?.className).toContain('min-h-0')
  })

  it('lets a tall list push the footer down rather than run under it', async () => {
    await renderTile()
    expect(screen.getByTestId('poi-list-groups').className).not.toContain('min-h-0')
  })

  it('stacks a short but narrow pane', async () => {
    listPaneWidth = 599
    await renderTile()
    expect(screen.getByTestId('poi-list-groups').dataset.stacked).toBe('true')
  })

  it('stays beside on a short, wide pane', async () => {
    listPaneWidth = 600
    await renderTile()
    expect(screen.getByTestId('poi-list-groups').dataset.stacked).toBe('false')
  })
})

describe('Vessels group order on a pane that cannot hold both groups', () => {
  beforeEach(() => { usePoiMock.mockReturnValue(poiResult([feature('p1', 'Cid Harbour', 1200)])) })
  const alarms = new globalThis.Map<string, AlarmState>([['a', 'alarm']])

  it('puts the Vessels group first on a short narrow pane when a listed vessel is alarmed', async () => {
    listPaneWidth = 500
    await renderTile({ nearbyVessels: [ves('a', 100)], aisCollisionAlarms: alarms })
    const groups = screen.getByTestId('poi-list-groups')
    expect(groups.dataset.vesselsFirst).toBe('true')
    expect(groups.firstElementChild?.querySelector('[data-testid="poi-vessel-row"]')).not.toBeNull()
  })

  it('keeps the places first when the group shows a message instead of its rows', async () => {
    listPaneWidth = 500
    await renderTile({ nearbyVessels: [ves('a', 100)], aisCollisionAlarms: alarms, gnssCriticalAlert: true })
    expect(screen.getByTestId('poi-list-groups').dataset.vesselsFirst).toBe('false')
    cleanup()
    await renderTile({ nearbyVessels: [ves('a', 100)], aisCollisionAlarms: alarms, nearbyVesselFeed: { source: 'other', maxRangeM: 5000 } as NearbyVesselFeed })
    expect(screen.getByTestId('poi-list-groups').dataset.vesselsFirst).toBe('false')
  })

  it('puts the divider between the two groups, whichever comes first', async () => {
    listPaneWidth = 500
    const { rerender } = await renderTile({ nearbyVessels: [ves('a', 100)] })
    expect(screen.getByTestId('poi-vessel-group').className).toContain('border-t')
    rerender(tile({ nearbyVessels: [ves('a', 100)], aisCollisionAlarms: alarms }))
    const group = screen.getByTestId('poi-vessel-group')
    expect(group.className).toContain('border-b')
    expect(group.className).not.toContain('border-t')
  })

  it('keeps the rows mounted when the order changes', async () => {
    listPaneWidth = 500
    const { rerender } = await renderTile({ nearbyVessels: [ves('a', 100)] })
    const row = screen.getByTestId('poi-vessel-row')
    rerender(tile({ nearbyVessels: [ves('a', 100)], aisCollisionAlarms: alarms }))
    expect(screen.getByTestId('poi-list-groups').dataset.vesselsFirst).toBe('true')
    expect(screen.getByTestId('poi-vessel-row')).toBe(row)
  })

  it('keeps the places first when no listed vessel is alarmed or warned', async () => {
    listPaneWidth = 500
    await renderTile({ nearbyVessels: [ves('a', 100)] })
    expect(screen.getByTestId('poi-list-groups').dataset.vesselsFirst).toBe('false')
  })

  it('keeps the places first on a tall narrow pane, where everything fits', async () => {
    listPaneWidth = 500
    listPaneHeight = 460
    await renderTile({ nearbyVessels: [ves('a', 100)], aisCollisionAlarms: alarms })
    expect(screen.getByTestId('poi-list-groups').dataset.vesselsFirst).toBe('false')
  })

  it('keeps the places first on a short wide pane, where the groups sit side by side', async () => {
    await renderTile({ nearbyVessels: [ves('a', 100)], aisCollisionAlarms: alarms })
    expect(screen.getByTestId('poi-list-groups').dataset.vesselsFirst).toBe('false')
  })
})

describe('Vessels and the places-only behaviour', () => {
  beforeEach(() => { usePoiMock.mockReturnValue(poiResult([feature('p1', 'Cid Harbour', 1200), feature('p2', 'Marina', 2000)])) })

  it('does not reset the cycle, jump the camera or add to the cycle when vessels change', async () => {
    const { rerender } = await renderTile({ nearbyVessels: [ves('a', 100)] })
    const cycleCalls = useCyclingIndexMock.mock.calls
    expect(cycleCalls.every(([count]) => count === 2)).toBe(true)
    const keys = new Set(cycleCalls.map(([, , key]) => key))
    const jumps = jumpToMock.mock.calls.length
    expect(jumps).toBeGreaterThan(0)
    rerender(tile({ nearbyVessels: [ves('a', 100), ves('b', 200, { lat: -20.5, lon: 149.5 })] }))
    rerender(tile({ nearbyVessels: [ves('a', 100, { age_seconds: 10 })] }))
    expect(new Set(useCyclingIndexMock.mock.calls.map(([, , key]) => key))).toEqual(keys)
    expect(useCyclingIndexMock.mock.calls.every(([count]) => count === 2)).toBe(true)
    expect(jumpToMock.mock.calls.length).toBe(jumps)
  })
})

describe('Vessel map labels', () => {
  beforeEach(() => { usePoiMock.mockReturnValue(poiResult([])) })

  it('shows the vessel name under its marker in both layouts', async () => {
    const { rerender } = await renderTile({ nearbyVessels: [ves('a', 100)] })
    expect(screen.getByTestId('poi-vessel-label').textContent).toBe('A')
    rerender(tile({ config: config({ layout: 'map' }), nearbyVessels: [ves('a', 100)] }))
    expect(screen.getByTestId('poi-vessel-label').textContent).toBe('A')
    rerender(tile({ config: config({ showAis: false }), nearbyVessels: [ves('a', 100)] }))
    expect(screen.queryByTestId('poi-vessel-label')).toBeNull()
  })

  it('yields to a place label that lands on top of it', async () => {
    usePoiMock.mockReturnValue(poiResult([feature('far', 'Far Place', 99999)]))
    projectImpl = () => ({ x: 100, y: 100 })
    await renderTile({ config: config({ layout: 'map' }), nearbyVessels: [ves('a', 100)] })
    expect(screen.getByText('Far Place')).toBeInTheDocument()
    expect(screen.queryByTestId('poi-vessel-label')).toBeNull()
  })

  it('does not hide a vessel label because a place with the same id was suppressed', async () => {
    // Places p0 and a sit on the same point, so a yields. The vessel a is far away.
    usePoiMock.mockReturnValue(poiResult([
      { ...feature('p0', 'Place Zero', 100), lat: 1, lon: 1 },
      { ...feature('a', 'Place A', 200), lat: 1, lon: 1 },
    ]))
    projectImpl = ([lng, lat]) => (lat === 1 ? { x: 10, y: 10 } : { x: lng * 100000, y: 0 })
    await renderTile({ config: config({ layout: 'map' }), nearbyVessels: [ves('a', 100)] })
    expect(screen.queryByText('Place A')).toBeNull()
    expect(screen.getByTestId('poi-vessel-label')).toBeInTheDocument()
  })

  it('keeps the label of an alarmed vessel over a nearer unalarmed one it overlaps', async () => {
    const alarms = new globalThis.Map<string, AlarmState>([['far', 'alarm']])
    await renderTile({
      config: config({ layout: 'map' }),
      nearbyVessels: [ves('near', 100, { lat: -20.2711, lon: 148.9411 }), ves('far', 900, { lat: -20.2712, lon: 148.9412 })],
      aisCollisionAlarms: alarms,
    })
    const labels = screen.getAllByTestId('poi-vessel-label').map((l) => l.textContent)
    expect(labels).toEqual(['FAR'])
  })

  it('projects a contact at its real position, not a rounded one', async () => {
    const seen: [number, number][] = []
    projectImpl = (lngLat) => { seen.push(lngLat); return { x: 0, y: 0 } }
    await renderTile({ config: config({ layout: 'map' }), nearbyVessels: [ves('a', 100, { lat: -20.27123456, lon: 148.94123456 })] })
    expect(seen).toContainEqual([148.94123456, -20.27123456])
  })

  it('does not re-project or re-render markers when only ages tick', async () => {
    const { rerender } = await renderTile({ config: config({ layout: 'map' }), nearbyVessels: [ves('a', 100, { age_seconds: 1 })] })
    const projected = projectCalls
    const renders = markerRenders.get('-20.271,148.941')
    rerender(tile({ config: config({ layout: 'map' }), nearbyVessels: [ves('a', 100, { age_seconds: 6 })] }))
    expect(projectCalls).toBe(projected)
    expect(markerRenders.get('-20.271,148.941')).toBe(renders)
  })
})

describe('Vessels group without WebGL2', () => {
  it('still renders the list', async () => {
    mockHasWebGL2 = false
    usePoiMock.mockReturnValue(poiResult([feature('p1', 'Cid Harbour', 1200)]))
    await renderTile({ nearbyVessels: [ves('a', 100)] })
    expect(screen.getByTestId('poi-vessel-row')).toBeInTheDocument()
  })
})
