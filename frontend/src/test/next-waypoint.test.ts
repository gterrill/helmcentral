import { describe, it, expect } from 'vitest'

import { nextWaypoint, etaToWaypoint, traversalOrder, etaToRouteEnd, etaToDestination, computeClockTripEta } from '@/lib/next-waypoint'
import { haversineMeters } from '@/lib/geo'
import type { Route, RouteWaypoint } from '@/hooks/use-routes'
import type { ActiveRouteStatus } from '@/hooks/use-route-activation'

function wp(lat: number, lon: number, name?: string): RouteWaypoint {
  return name === undefined ? { lat, lon } : { lat, lon, name }
}

function route(waypoints: RouteWaypoint[], overrides: Partial<Route> = {}): Route {
  return {
    id: 'route-1',
    name: 'Test Route',
    waypoints,
    planning_speed_kts: 6,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

const FORWARD_ROUTE = route([
  wp(1, 1, 'Start'),
  wp(2, 2),
  wp(3, 3, 'Mid'),
  wp(4, 4),
])

function activeStatus(pointIndex: number, reverse: boolean, routeId: string | null = 'route-1'): ActiveRouteStatus {
  return { state: 'active', routeId, pointIndex, reverse, destination: null }
}

describe('nextWaypoint', () => {
  it('indexes the authored array directly when reverse is false, at pointIndex 0', () => {
    const result = nextWaypoint(FORWARD_ROUTE, activeStatus(0, false))
    expect(result).not.toBeNull()
    expect(result!.index).toBe(0)
    expect(result!.waypoint).toEqual({ lat: 1, lon: 1, name: 'Start' })
    expect(result!.label).toBe('Start')
  })

  it('indexes the authored array directly when reverse is false, at pointIndex 1', () => {
    const result = nextWaypoint(FORWARD_ROUTE, activeStatus(1, false))
    expect(result!.index).toBe(1)
    expect(result!.waypoint).toEqual({ lat: 2, lon: 2 })
    // No name on this waypoint - label falls back to 1-based traversal position.
    expect(result!.label).toBe('WP 2')
  })

  it('indexes the authored array directly when reverse is false, mid-route', () => {
    const result = nextWaypoint(FORWARD_ROUTE, activeStatus(2, false))
    expect(result!.index).toBe(2)
    expect(result!.waypoint).toEqual({ lat: 3, lon: 3, name: 'Mid' })
    expect(result!.label).toBe('Mid')
  })

  it('counts pointIndex from the end of the array when reverse is true, at pointIndex 0', () => {
    // reverse true, pointIndex 0 -> last authored waypoint (lat 4, lon 4).
    const result = nextWaypoint(FORWARD_ROUTE, activeStatus(0, true))
    expect(result!.index).toBe(0)
    expect(result!.waypoint).toEqual({ lat: 4, lon: 4 })
    expect(result!.label).toBe('WP 1')
  })

  it('walks backward through the authored array as pointIndex increases when reverse is true', () => {
    const r1 = nextWaypoint(FORWARD_ROUTE, activeStatus(1, true))
    expect(r1!.waypoint).toEqual({ lat: 3, lon: 3, name: 'Mid' })
    expect(r1!.label).toBe('Mid')

    const r2 = nextWaypoint(FORWARD_ROUTE, activeStatus(2, true))
    expect(r2!.waypoint).toEqual({ lat: 2, lon: 2 })
    expect(r2!.label).toBe('WP 3')

    const r3 = nextWaypoint(FORWARD_ROUTE, activeStatus(3, true))
    expect(r3!.waypoint).toEqual({ lat: 1, lon: 1, name: 'Start' })
    expect(r3!.label).toBe('Start')
  })

  it('clamps a pointIndex past the end of the array rather than throwing', () => {
    const result = nextWaypoint(FORWARD_ROUTE, activeStatus(99, false))
    expect(result).not.toBeNull()
    expect(result!.index).toBe(3)
    expect(result!.waypoint).toEqual({ lat: 4, lon: 4 })
  })

  it('clamps a pointIndex past the end of the array when reverse is true too', () => {
    const result = nextWaypoint(FORWARD_ROUTE, activeStatus(99, true))
    expect(result).not.toBeNull()
    expect(result!.index).toBe(3)
    expect(result!.waypoint).toEqual({ lat: 1, lon: 1, name: 'Start' })
  })

  it('returns null when the status is not active', () => {
    expect(nextWaypoint(FORWARD_ROUTE, { state: 'inactive', destination: null })).toBeNull()
    expect(nextWaypoint(FORWARD_ROUTE, { state: 'unknown' })).toBeNull()
  })

  it('returns null when the active routeId does not match this route', () => {
    const result = nextWaypoint(FORWARD_ROUTE, activeStatus(0, false, 'some-other-route'))
    expect(result).toBeNull()
  })

  it('returns null when the route has no waypoints', () => {
    const result = nextWaypoint(route([]), activeStatus(0, false))
    expect(result).toBeNull()
  })
})

describe('traversalOrder', () => {
  it('returns the authored order when reverse is false', () => {
    expect(traversalOrder(FORWARD_ROUTE, activeStatus(0, false))).toEqual(FORWARD_ROUTE.waypoints)
  })

  it('returns the reversed order when reverse is true', () => {
    expect(traversalOrder(FORWARD_ROUTE, activeStatus(0, true))).toEqual([...FORWARD_ROUTE.waypoints].reverse())
  })

  it('is the array nextWaypoint indexes into: nextWaypoint.index matches traversalOrder[index]', () => {
    const status = activeStatus(2, true)
    const traversal = traversalOrder(FORWARD_ROUTE, status)
    const next = nextWaypoint(FORWARD_ROUTE, status)
    expect(traversal![next!.index]).toEqual(next!.waypoint)
  })

  it('returns null when the status is not active', () => {
    expect(traversalOrder(FORWARD_ROUTE, { state: 'inactive', destination: null })).toBeNull()
    expect(traversalOrder(FORWARD_ROUTE, { state: 'unknown' })).toBeNull()
  })

  it('returns null when the active routeId does not match this route', () => {
    expect(traversalOrder(FORWARD_ROUTE, activeStatus(0, false, 'some-other-route'))).toBeNull()
  })

  it('returns null when the route has no waypoints', () => {
    expect(traversalOrder(route([]), activeStatus(0, false))).toBeNull()
  })
})

describe('etaToWaypoint', () => {
  const now = new Date('2026-01-01T12:00:00Z')
  const target = wp(0, 1) // 1 degree of longitude east at the equator, roughly 111.2km

  it('uses SOG as the basis when SOG is above the 0.5kt threshold', () => {
    const result = etaToWaypoint(0, 0, target, 5, 6, now)
    expect(result.basis).toBe('sog')
    expect(result.distanceM).toBeGreaterThan(111000)
    expect(result.etaAt).not.toBeNull()
  })

  it('falls back to planning speed when SOG is at or below the threshold', () => {
    const result = etaToWaypoint(0, 0, target, 0.5, 6, now)
    expect(result.basis).toBe('plan')
    expect(result.etaAt).not.toBeNull()
  })

  it('falls back to planning speed when SOG is null', () => {
    const result = etaToWaypoint(0, 0, target, null, 6, now)
    expect(result.basis).toBe('plan')
    expect(result.etaAt).not.toBeNull()
  })

  it('produces a slower (later) ETA on plan speed than a faster SOG would, all else equal', () => {
    const bySog = etaToWaypoint(0, 0, target, 10, 4, now)
    const byPlan = etaToWaypoint(0, 0, target, null, 4, now)
    expect(bySog.basis).toBe('sog')
    expect(byPlan.basis).toBe('plan')
    expect(bySog.etaAt!.getTime()).toBeLessThan(byPlan.etaAt!.getTime())
  })

  it('gives a null ETA when the resolved speed is zero via SOG-falls-to-plan-zero', () => {
    const result = etaToWaypoint(0, 0, target, null, 0, now)
    expect(result.etaAt).toBeNull()
  })

  it('gives a null ETA when the resolved speed is zero via SOG itself being zero and plan also zero', () => {
    // SOG 0 is <= 0.5 threshold, falls to plan speed, which is also 0.
    const result = etaToWaypoint(0, 0, target, 0, 0, now)
    expect(result.basis).toBe('plan')
    expect(result.etaAt).toBeNull()
  })

  it('gives a null ETA when the resolved speed is negative', () => {
    const result = etaToWaypoint(0, 0, target, null, -3, now)
    expect(result.etaAt).toBeNull()
  })

  it('gives an ETA equal to now when the distance is 0', () => {
    const samePoint = wp(0, 0)
    const result = etaToWaypoint(0, 0, samePoint, 5, 6, now)
    expect(result.distanceM).toBe(0)
    expect(result.etaAt).toEqual(now)
  })
})

// ADR 0125: the clock tile's ETA is for the whole trip (the route's final
// waypoint in traversal order), not just the next leg.
describe('etaToRouteEnd', () => {
  const now = new Date('2026-01-01T12:00:00Z')

  it('sums the vessel-to-next-waypoint leg plus every remaining leg to the final waypoint, forward order', () => {
    // pointIndex 1 (forward): current target is (2,2); the route continues
    // (2,2) -> (3,3) -> (4,4). Vessel starts at the origin.
    const result = etaToRouteEnd(0, 0, FORWARD_ROUTE, activeStatus(1, false), 10, now)
    expect(result).not.toBeNull()

    const expectedDistance =
      haversineMeters(0, 0, 2, 2) + haversineMeters(2, 2, 3, 3) + haversineMeters(3, 3, 4, 4)
    expect(result!.distanceM).toBeCloseTo(expectedDistance, 3)
    // (4,4) has no name -> falls back to its 1-based traversal position.
    expect(result!.label).toBe('WP 4')
    expect(result!.basis).toBe('sog')
    expect(result!.etaAt).not.toBeNull()
  })

  it('walks the reversed traversal order for its remaining legs and final waypoint, matching nextWaypoint', () => {
    // reverse true, pointIndex 0: current target is (4,4); traversal order
    // from there is (4,4) -> (3,3) -> (2,2) -> (1,1,"Start").
    const result = etaToRouteEnd(0, 0, FORWARD_ROUTE, activeStatus(0, true), 10, now)
    expect(result).not.toBeNull()

    const expectedDistance =
      haversineMeters(0, 0, 4, 4) + haversineMeters(4, 4, 3, 3) + haversineMeters(3, 3, 2, 2) + haversineMeters(2, 2, 1, 1)
    expect(result!.distanceM).toBeCloseTo(expectedDistance, 3)
    expect(result!.label).toBe('Start')
  })

  it('has no remaining legs when the current target is already the final waypoint', () => {
    // pointIndex 3 (forward) targets (4,4), which is also the last waypoint.
    const result = etaToRouteEnd(0, 0, FORWARD_ROUTE, activeStatus(3, false), 10, now)
    expect(result).not.toBeNull()
    expect(result!.distanceM).toBeCloseTo(haversineMeters(0, 0, 4, 4), 3)
    expect(result!.label).toBe('WP 4')
  })

  it('falls back to the route planning speed when SOG is at or below the threshold', () => {
    const result = etaToRouteEnd(0, 0, FORWARD_ROUTE, activeStatus(1, false), 0.5, now)
    expect(result).not.toBeNull()
    expect(result!.basis).toBe('plan')
    expect(result!.etaAt).not.toBeNull()
  })

  it('gives a null ETA when neither SOG nor a usable planning speed exists', () => {
    const becalmedRoute = route(FORWARD_ROUTE.waypoints, { planning_speed_kts: 0 })
    const result = etaToRouteEnd(0, 0, becalmedRoute, activeStatus(1, false), null, now)
    expect(result).not.toBeNull()
    expect(result!.basis).toBe('plan')
    expect(result!.etaAt).toBeNull()
  })

  it('returns null when nextWaypoint itself would (status not active)', () => {
    expect(etaToRouteEnd(0, 0, FORWARD_ROUTE, { state: 'inactive', destination: null }, 10, now)).toBeNull()
  })

  it('returns null when the route has no waypoints', () => {
    expect(etaToRouteEnd(0, 0, route([]), activeStatus(0, false), 10, now)).toBeNull()
  })
})

// ADR 0125: a bare chartplotter go-to (SignalK's Course API nextPoint, no
// Helmcentral route behind it) has no planning speed to fall back to.
describe('etaToDestination', () => {
  const now = new Date('2026-01-01T12:00:00Z')
  const destination = { lat: 0, lon: 1 } // ~111.2km east of the origin at the equator

  it('gives an ETA from SOG when SOG is above the threshold', () => {
    const result = etaToDestination(0, 0, destination, 5, now)
    expect(result.distanceM).toBeGreaterThan(111000)
    expect(result.etaAt).not.toBeNull()
  })

  it('gives no ETA when SOG is at or below the threshold - there is no planning speed to fall back to', () => {
    const result = etaToDestination(0, 0, destination, 0.5, now)
    expect(result.etaAt).toBeNull()
  })

  it('gives no ETA when SOG is null', () => {
    const result = etaToDestination(0, 0, destination, null, now)
    expect(result.etaAt).toBeNull()
  })

  it('gives an ETA equal to now when the distance is 0', () => {
    const result = etaToDestination(0, 0, { lat: 0, lon: 0 }, 5, now)
    expect(result.distanceM).toBe(0)
    expect(result.etaAt).toEqual(now)
  })
})

// ADR 0125: the clock tile's trip-ETA decision, pulled out of App.tsx so it
// is unit-testable without rendering the whole app.
describe('computeClockTripEta', () => {
  const now = new Date('2026-01-01T12:00:00Z')
  const routes = [FORWARD_ROUTE]

  it('returns null with no position fix', () => {
    expect(computeClockTripEta(activeStatus(1, false), routes, null, 0, 10, now)).toBeNull()
    expect(computeClockTripEta(activeStatus(1, false), routes, 0, null, 10, now)).toBeNull()
  })

  it('returns null when there is no status at all', () => {
    expect(computeClockTripEta(null, routes, 0, 0, 10, now)).toBeNull()
  })

  it('returns null for status "unknown"', () => {
    expect(computeClockTripEta({ state: 'unknown' }, routes, 0, 0, 10, now)).toBeNull()
  })

  it('reports the FINAL waypoint via etaToRouteEnd for a known active route', () => {
    const result = computeClockTripEta(activeStatus(1, false), routes, 0, 0, 10, now)
    expect(result).not.toBeNull()
    expect(result!.label).toBe('WP 4') // FORWARD_ROUTE's last waypoint has no name
    expect(result!.etaAt).not.toBeNull()
    expect(result!.basis).toBe('sog')
  })

  it('returns null when the active routeId does not match any route in `routes`', () => {
    const status = activeStatus(0, false, 'some-other-route')
    expect(computeClockTripEta(status, routes, 0, 0, 10, now)).toBeNull()
  })

  it('reports a bare chartplotter destination via etaToDestination when inactive', () => {
    const status = { state: 'inactive' as const, destination: { lat: 0, lon: 1, name: 'Hook Island' } }
    const result = computeClockTripEta(status, routes, 0, 0, 5, now)
    expect(result).not.toBeNull()
    expect(result!.label).toBe('Hook Island')
    expect(result!.basis).toBe('sog')
    expect(result!.etaAt).not.toBeNull()
  })

  it('returns null when inactive with no destination', () => {
    const status = { state: 'inactive' as const, destination: null }
    expect(computeClockTripEta(status, routes, 0, 0, 5, now)).toBeNull()
  })

  // The regression this cycle's fix targets: a route activated OUTSIDE
  // Helmcentral (routeId null) still has the Course API's destination to
  // report an ETA against, read exactly like the inactive case.
  it('reports the destination via etaToDestination when active but the route is not one of ours', () => {
    const status = {
      state: 'active' as const,
      routeId: null,
      pointIndex: 0,
      reverse: false,
      destination: { lat: 0, lon: 1, name: 'Hook Island' },
    }
    const result = computeClockTripEta(status, routes, 0, 0, 5, now)
    expect(result).not.toBeNull()
    expect(result!.label).toBe('Hook Island')
    expect(result!.basis).toBe('sog')
    expect(result!.etaAt).not.toBeNull()
  })

  it('returns null when active with an unrecognized route and no destination either', () => {
    const status = { state: 'active' as const, routeId: null, pointIndex: 0, reverse: false, destination: null }
    expect(computeClockTripEta(status, routes, 0, 0, 5, now)).toBeNull()
  })

  // A known, active Helmcentral route always takes the leg-by-leg ETA, even
  // if the response somehow also carried a destination alongside it.
  it('prefers the known-route ETA over a destination when both are present', () => {
    const status = {
      state: 'active' as const,
      routeId: 'route-1',
      pointIndex: 1,
      reverse: false,
      destination: { lat: 9, lon: 9, name: 'Somewhere Else' },
    }
    const result = computeClockTripEta(status, routes, 0, 0, 10, now)
    expect(result).not.toBeNull()
    expect(result!.label).toBe('WP 4')
  })
})
