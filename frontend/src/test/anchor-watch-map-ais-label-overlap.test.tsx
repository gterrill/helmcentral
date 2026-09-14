import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import {
  AnchorWatchMap,
  LABEL_COLLISION_RADIUS_PX,
  resolveMarkerLabelSuppression,
  type MarkerLabelPoint,
  type ScreenRect,
} from '@/components/anchor-watch-map'
import type { NearbyVessel } from '@/hooks/use-nearby-vessels'
import type { RodeMethodResult } from '@/lib/rode-plan'

// Design critique item 4: the detector found real AIS-label occlusion —
// "KINGFISH" half-covered by an opaque map control, a vessel label
// two-thirds covered by another vessel's label, a range figure the same.
// resolveMarkerLabelSuppression is the pure decision function behind the
// fix: given already-projected screen points and the overlay's own
// footprint, which vessel ids should render without their text label (the
// marker dot itself always stays put). Pure and synchronous, so it's
// tested directly with no maplibre/react-map-gl mocking at all.
describe('resolveMarkerLabelSuppression', () => {
  it('suppresses nothing when there are no avoid zones and no collisions', () => {
    const points: MarkerLabelPoint[] = [
      { id: 'a', x: 0, y: 0, priority: 10 },
      { id: 'b', x: 500, y: 500, priority: 20 },
    ]
    expect(resolveMarkerLabelSuppression(points, [])).toEqual(new Set())
  })

  it('suppresses a label whose point falls inside an avoid zone (the metric panel or control stack footprint)', () => {
    const zone: ScreenRect = { left: 0, top: 0, right: 100, bottom: 40 }
    const points: MarkerLabelPoint[] = [
      { id: 'under-panel', x: 50, y: 20, priority: 1 },
      { id: 'clear', x: 500, y: 500, priority: 2 },
    ]
    expect(resolveMarkerLabelSuppression(points, [zone])).toEqual(new Set(['under-panel']))
  })

  it('treats the avoid zone boundary as inclusive', () => {
    const zone: ScreenRect = { left: 0, top: 0, right: 100, bottom: 40 }
    const points: MarkerLabelPoint[] = [{ id: 'on-edge', x: 100, y: 40, priority: 1 }]
    expect(resolveMarkerLabelSuppression(points, [zone])).toEqual(new Set(['on-edge']))
  })

  it('keeps the closer (lower-priority-value) vessel and suppresses the farther one when two labels collide', () => {
    const points: MarkerLabelPoint[] = [
      { id: 'far', x: 20, y: 0, priority: 400 },
      { id: 'near', x: 0, y: 0, priority: 50 },
    ]
    // 20px apart, well inside LABEL_COLLISION_RADIUS_PX.
    expect(resolveMarkerLabelSuppression(points, [])).toEqual(new Set(['far']))
  })

  it('leaves both labels alone once they clear the collision radius', () => {
    const points: MarkerLabelPoint[] = [
      { id: 'a', x: 0, y: 0, priority: 1 },
      { id: 'b', x: LABEL_COLLISION_RADIUS_PX + 5, y: 0, priority: 2 },
    ]
    expect(resolveMarkerLabelSuppression(points, [])).toEqual(new Set())
  })

  it('does not re-suppress past an already-suppressed vessel in a three-way pile-up', () => {
    // P1 (closest) sits next to P2, which sits next to P3 — but P1 and P3
    // are far apart. Only P2 collides with the winner (P1) directly; P3
    // never collides with P1, and a hidden P2 has no label left for P3 to
    // clash with, so P3 should stay visible.
    const p1: MarkerLabelPoint = { id: 'p1', x: 0, y: 0, priority: 1 }
    const p2: MarkerLabelPoint = { id: 'p2', x: 20, y: 0, priority: 2 }
    const p3: MarkerLabelPoint = { id: 'p3', x: 200, y: 0, priority: 3 }
    expect(resolveMarkerLabelSuppression([p1, p2, p3], [])).toEqual(new Set(['p2']))
  })

  it('never suppresses the same id via both an avoid zone and a label collision redundantly', () => {
    const zone: ScreenRect = { left: 0, top: 0, right: 10, bottom: 10 }
    const points: MarkerLabelPoint[] = [
      { id: 'under-panel-and-close', x: 5, y: 5, priority: 1 },
      { id: 'clear-neighbour', x: 15, y: 5, priority: 2 },
    ]
    const suppressed = resolveMarkerLabelSuppression(points, [zone])
    expect(suppressed.has('under-panel-and-close')).toBe(true)
    // The clear neighbour is close to a suppressed point, but nothing is
    // left on screen for it to visually collide with, so it stays.
    expect(suppressed.has('clear-neighbour')).toBe(false)
  })
})

// --- Integration: the component wires resolveMarkerLabelSuppression to the
// live map's own project() and the metric overlay/control refs.

vi.mock('maplibre-gl', () => ({
  default: {},
}))

let lastProject: ((lngLat: [number, number]) => { x: number; y: number }) | null = null

vi.mock('react-map-gl/maplibre', async () => {
  const React = await import('react')
  return {
    Map: React.forwardRef(
      ({ children }: { children?: React.ReactNode }, ref: React.Ref<unknown>) => {
        React.useImperativeHandle(ref, () => ({
          getCanvas: () => ({ style: { cursor: 'grab' } }),
          getZoom: () => 14,
          easeTo: vi.fn(),
          project: (lngLat: [number, number]) => lastProject!(lngLat),
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
    Layer: ({ id }: { id: string }) => <div data-testid={`layer-${id}`} />,
  }
})

const scopeRecommendation: RodeMethodResult = {
  id: 'ratio',
  label: 'Ratio Method',
  recommendedRodeM: 30,
  scopeRatio: 5,
  note: 'note',
}

function renderMap(aisVessels: NearbyVessel[]) {
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
      scopeRecommendation={scopeRecommendation}
      isImperial={false}
      vesselTrail={() => []}
      aisVessels={aisVessels}
      aisTrails={() => new Map()}
      isDarkTheme={false}
      onAnchorReposition={() => undefined}
      onRadiusChange={() => undefined}
    />,
  )
}

describe('AnchorWatchMap AIS label suppression wiring', () => {
  it('hides the farther vessel label when two AIS markers project close together on screen', async () => {
    // lon offsets translate 1:1 to x pixels for this test's project() —
    // 0.00001° apart is nowhere near 50px in the real world, but the point
    // is exercising the wiring, not maplibre's real projection math.
    lastProject = ([lon]) => ({ x: (lon - 152.9103) * 1_000_000, y: 100 })

    const vessels: NearbyVessel[] = [
      { id: 'near', name: 'NEARBOAT', lat: -25.294, lon: 152.9103, range_m: 50, age_seconds: 1 },
      { id: 'far-collision', name: 'FARBOAT', lat: -25.30, lon: 152.91031, range_m: 800, age_seconds: 1 },
    ]

    renderMap(vessels)

    // Both markers render (the dot never disappears)...
    expect(screen.getByRole('button', { name: 'AIS vessel: NEARBOAT' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'AIS vessel: FARBOAT' })).toBeInTheDocument()

    // ...but only the closer vessel's own name label is showing; the
    // farther, colliding one had its label suppressed.
    expect(await screen.findByText('NEARBOAT')).toBeInTheDocument()
    expect(screen.queryByText('FARBOAT')).not.toBeInTheDocument()
  })

  it('leaves both labels alone when the two markers project far apart', async () => {
    lastProject = ([lon]) => ({ x: (lon - 152.9103) * 1_000_000, y: 100 })

    const vessels: NearbyVessel[] = [
      { id: 'a', name: 'ALPHA', lat: -25.294, lon: 152.9103, range_m: 50, age_seconds: 1 },
      { id: 'b', name: 'BRAVO', lat: -25.30, lon: 152.9113, range_m: 800, age_seconds: 1 },
    ]

    renderMap(vessels)

    expect(await screen.findByText('ALPHA')).toBeInTheDocument()
    expect(screen.getByText('BRAVO')).toBeInTheDocument()
  })

  // Not every MapRef test double implements project() — only a live
  // maplibre map does (see the guard in anchor-watch-map.tsx's suppression
  // effect). Every other suite that mounts AnchorWatchMap with a lighter
  // mock (anchor-watch-map-ui.test.tsx, anchor-watch-map-radar-markers.
  // test.tsx, anchor-watch-map-stale-closure.test.tsx, anchor-watch-drawer-
  // bow-offset.test.tsx, among others) omits project() entirely and still
  // renders every AIS label unsuppressed — that's the "degrades to today's
  // behaviour" case, already covered there rather than duplicated here.
})

// The suppression effect used to depend on vesselLat/vesselLon, so it
// re-measured the metrics/controls overlay panels (getBoundingClientRect,
// forced synchronous layout) on every own-ship position tick even though
// those panels essentially never move on their own. This proves the fix:
// the avoid-zone rects are cached and only re-measured on an actual trigger
// (a container resize, or the metrics panel gaining/losing rows), not on
// every GPS fix.
describe('AnchorWatchMap AIS label suppression per-tick cost', () => {
  it('does not re-measure the overlay panels when only the vessel position ticks, with the same AIS vessels', async () => {
    lastProject = ([lon]) => ({ x: (lon - 152.9103) * 1_000_000, y: 100 })
    const vessels: NearbyVessel[] = [
      { id: 'a', name: 'ALPHA', lat: -25.294, lon: 152.9103, range_m: 50, age_seconds: 1 },
    ]

    const element = (vesselLat: number, anchorLat: number | null) => (
      <AnchorWatchMap
        vesselLat={vesselLat}
        vesselLon={152.9103}
        vesselHeadingDeg={265}
        anchorLat={anchorLat}
        anchorLon={anchorLat === null ? null : 152.9103}
        radiusMeters={34}
        depthMeters={3.2}
        currentDriftKts={0.1}
        currentSetDeg={120}
        distanceMeters={10}
        bearingDeg={80}
        scopeRecommendation={scopeRecommendation}
        isImperial={false}
        vesselTrail={() => []}
        aisVessels={vessels}
        aisTrails={() => new Map()}
        isDarkTheme={false}
        onAnchorReposition={() => undefined}
        onRadiusChange={() => undefined}
      />
    )

    const { rerender } = render(element(-25.2939, -25.2939))
    await screen.findByText('ALPHA')

    const rectSpy = vi.spyOn(Element.prototype, 'getBoundingClientRect')

    // Five simulated GPS fixes: only vesselLat changes, same AIS vessels,
    // same anchor state.
    for (let i = 1; i <= 5; i++) {
      rerender(element(-25.2939 - i * 0.00001, -25.2939))
    }
    expect(rectSpy).not.toHaveBeenCalled()

    // A real trigger — the anchor clearing, which changes the metrics
    // panel's own row count — still re-measures.
    rerender(element(-25.2939, null))
    expect(rectSpy).toHaveBeenCalled()

    rectSpy.mockRestore()
  })
})
